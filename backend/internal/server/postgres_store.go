package server

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

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
	const schema = `
CREATE TABLE IF NOT EXISTS sensor_readings (
  id BIGSERIAL PRIMARY KEY,
  timestamp BIGINT NOT NULL,
  temperature DOUBLE PRECISION NOT NULL,
  pressure DOUBLE PRECISION NOT NULL,
  humidity DOUBLE PRECISION NOT NULL,
  oxidised DOUBLE PRECISION NOT NULL,
  reduced DOUBLE PRECISION NOT NULL,
  nh3 DOUBLE PRECISION NOT NULL,
  pm1 DOUBLE PRECISION NOT NULL,
  pm2 DOUBLE PRECISION NOT NULL,
  pm10 DOUBLE PRECISION NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sensor_readings_timestamp ON sensor_readings(timestamp DESC);
`

	_, err := store.pool.Exec(ctx, schema)
	return err
}

func (store *PostgresStore) Add(ctx context.Context, reading SensorReading) error {
	const query = `
INSERT INTO sensor_readings (
  timestamp, temperature, pressure, humidity, oxidised, reduced, nh3, pm1, pm2, pm10
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`

	_, err := store.pool.Exec(
		ctx,
		query,
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
