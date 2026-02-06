package server

import "context"

type Store interface {
	Add(ctx context.Context, reading SensorReading) error
	Latest(ctx context.Context, limit int) ([]SensorReading, error)
	Ping(ctx context.Context) error
	Close()
}
