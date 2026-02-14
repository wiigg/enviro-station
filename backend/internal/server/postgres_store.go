package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
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

func (store *PostgresStore) DeleteOlderThan(ctx context.Context, cutoffTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}

	const query = `
WITH expired AS (
  SELECT id
  FROM sensor_readings
  WHERE timestamp < $1
  ORDER BY timestamp
  LIMIT $2
)
DELETE FROM sensor_readings AS readings
USING expired
WHERE readings.id = expired.id
`

	result, err := store.pool.Exec(ctx, query, cutoffTimestamp, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (store *PostgresStore) SaveInsightsSnapshot(ctx context.Context, snapshot InsightsSnapshot) error {
	insightsJSON, err := json.Marshal(snapshot.Insights)
	if err != nil {
		return err
	}

	const query = `
INSERT INTO insights_snapshots (
  snapshot_key, insights, source, generated_at, analyzed_samples, analysis_limit, trigger
) VALUES ('latest', $1, $2, $3, $4, $5, $6)
ON CONFLICT (snapshot_key) DO UPDATE SET
  insights = EXCLUDED.insights,
  source = EXCLUDED.source,
  generated_at = EXCLUDED.generated_at,
  analyzed_samples = EXCLUDED.analyzed_samples,
  analysis_limit = EXCLUDED.analysis_limit,
  trigger = EXCLUDED.trigger,
  updated_at = NOW()
`

	_, err = store.pool.Exec(
		ctx,
		query,
		insightsJSON,
		snapshot.Source,
		snapshot.GeneratedAt,
		snapshot.AnalyzedSamples,
		snapshot.AnalysisLimit,
		snapshot.Trigger,
	)
	return err
}

func (store *PostgresStore) LatestInsightsSnapshot(ctx context.Context) (InsightsSnapshot, bool, error) {
	const query = `
SELECT insights, source, generated_at, analyzed_samples, analysis_limit, trigger
FROM insights_snapshots
WHERE snapshot_key = 'latest'
`

	var insightsJSON []byte
	var snapshot InsightsSnapshot
	err := store.pool.QueryRow(ctx, query).Scan(
		&insightsJSON,
		&snapshot.Source,
		&snapshot.GeneratedAt,
		&snapshot.AnalyzedSamples,
		&snapshot.AnalysisLimit,
		&snapshot.Trigger,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InsightsSnapshot{}, false, nil
		}
		return InsightsSnapshot{}, false, err
	}

	if err = json.Unmarshal(insightsJSON, &snapshot.Insights); err != nil {
		return InsightsSnapshot{}, false, err
	}

	return snapshot, true, nil
}

func (store *PostgresStore) AddOpsEvent(ctx context.Context, event OpsEvent) error {
	const query = `
INSERT INTO ops_events (
  timestamp, kind, title, detail
) VALUES ($1, $2, $3, $4)
`

	_, err := store.pool.Exec(ctx, query, event.Timestamp, event.Kind, event.Title, event.Detail)
	return err
}

func (store *PostgresStore) LatestOpsEvents(ctx context.Context, limit int) ([]OpsEvent, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}

	const query = `
SELECT id, timestamp, kind, title, detail
FROM ops_events
ORDER BY id DESC
LIMIT $1
`

	rows, err := store.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]OpsEvent, 0, limit)
	for rows.Next() {
		var event OpsEvent
		if err = rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.Kind,
			&event.Title,
			&event.Detail,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
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
var _ InsightsSnapshotStore = (*PostgresStore)(nil)
var _ OpsEventStore = (*PostgresStore)(nil)
