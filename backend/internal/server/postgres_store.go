package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

func NewPostgresStore(ctx context.Context, databaseURL string, maxConns int32) (*PostgresStore, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	if maxConns > 0 {
		config.MaxConns = maxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	store := &PostgresStore{pool: pool}
	if err := store.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	return store, nil
}

func (store *PostgresStore) migrate(ctx context.Context) error {
	const migrationTableQuery = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

	if _, err := store.pool.Exec(ctx, migrationTableQuery); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		version := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		applied, err := store.isMigrationApplied(ctx, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		filePath := filepath.Join("migrations", entry.Name())
		sqlBytes, err := migrationFiles.ReadFile(filePath)
		if err != nil {
			return err
		}

		tx, err := store.pool.Begin(ctx)
		if err != nil {
			return err
		}

		if _, err = tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}

		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}

		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (store *PostgresStore) isMigrationApplied(ctx context.Context, version string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`
	var exists bool
	if err := store.pool.QueryRow(ctx, query, version).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (store *PostgresStore) Add(ctx context.Context, reading SensorReading) error {
	const insertReadingQuery = `
INSERT INTO sensor_readings (
  timestamp, temperature, pressure, humidity, oxidised, reduced, nh3, pm1, pm2, pm10
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`

	_, err := store.pool.Exec(
		ctx,
		insertReadingQuery,
		reading.Timestamp,
		reading.Temperature,
		reading.Pressure,
		reading.Humidity,
		reading.Oxidised,
		reading.Reduced,
		reading.Nh3,
		reading.PM1,
		reading.PM2,
		reading.PM10,
	)
	return err
}

func (store *PostgresStore) AddBatch(ctx context.Context, readings []SensorReading) error {
	if len(readings) == 0 {
		return nil
	}

	const insertReadingQuery = `
INSERT INTO sensor_readings (
  timestamp, temperature, pressure, humidity, oxidised, reduced, nh3, pm1, pm2, pm10
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`

	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}

	batch := &pgx.Batch{}
	for _, reading := range readings {
		batch.Queue(
			insertReadingQuery,
			reading.Timestamp,
			reading.Temperature,
			reading.Pressure,
			reading.Humidity,
			reading.Oxidised,
			reading.Reduced,
			reading.Nh3,
			reading.PM1,
			reading.PM2,
			reading.PM10,
		)
	}

	batchResults := tx.SendBatch(ctx, batch)
	if err = batchResults.Close(); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return err
	}

	return nil
}

func (store *PostgresStore) Latest(ctx context.Context, limit int) ([]SensorReading, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `
SELECT timestamp, temperature, pressure, humidity, oxidised, reduced, nh3, pm1, pm2, pm10
FROM sensor_readings
ORDER BY id DESC
LIMIT $1
`

	rows, err := store.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	readings := make([]SensorReading, 0, limit)
	for rows.Next() {
		var reading SensorReading
		if err := rows.Scan(
			&reading.Timestamp,
			&reading.Temperature,
			&reading.Pressure,
			&reading.Humidity,
			&reading.Oxidised,
			&reading.Reduced,
			&reading.Nh3,
			&reading.PM1,
			&reading.PM2,
			&reading.PM10,
		); err != nil {
			return nil, err
		}
		readings = append(readings, reading)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	for left, right := 0, len(readings)-1; left < right; left, right = left+1, right-1 {
		readings[left], readings[right] = readings[right], readings[left]
	}

	return readings, nil
}

func (store *PostgresStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return store.pool.Ping(pingCtx)
}

func (store *PostgresStore) Close() {
	store.pool.Close()
}

var _ Store = (*PostgresStore)(nil)
