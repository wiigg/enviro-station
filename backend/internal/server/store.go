package server

import (
	"context"
	"sync"
)

type Store interface {
	Add(ctx context.Context, reading SensorReading) error
	Count(ctx context.Context) (int, error)
	Latest(ctx context.Context, limit int) ([]SensorReading, error)
	Ping(ctx context.Context) error
	Close()
}

type MemoryStore struct {
	mu          sync.RWMutex
	maxReadings int
	readings    []SensorReading
}

func NewStore(maxReadings int) *MemoryStore {
	if maxReadings <= 0 {
		maxReadings = 10000
	}

	return &MemoryStore{
		maxReadings: maxReadings,
		readings:    make([]SensorReading, 0, maxReadings),
	}
}

func (store *MemoryStore) Add(_ context.Context, reading SensorReading) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.readings = append(store.readings, reading)
	if len(store.readings) > store.maxReadings {
		store.readings = append([]SensorReading(nil), store.readings[len(store.readings)-store.maxReadings:]...)
	}

	return nil
}

func (store *MemoryStore) Count(_ context.Context) (int, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.readings), nil
}

func (store *MemoryStore) Latest(_ context.Context, limit int) ([]SensorReading, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	if limit <= 0 || limit > len(store.readings) {
		limit = len(store.readings)
	}

	start := len(store.readings) - limit
	output := make([]SensorReading, limit)
	copy(output, store.readings[start:])
	return output, nil
}

func (store *MemoryStore) Ping(_ context.Context) error {
	return nil
}

func (store *MemoryStore) Close() {}

var _ Store = (*MemoryStore)(nil)
