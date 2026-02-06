package server

import "sync"

type Store struct {
	mu          sync.RWMutex
	maxReadings int
	readings    []SensorReading
}

func NewStore(maxReadings int) *Store {
	if maxReadings <= 0 {
		maxReadings = 10000
	}

	return &Store{
		maxReadings: maxReadings,
		readings:    make([]SensorReading, 0, maxReadings),
	}
}

func (store *Store) Add(reading SensorReading) {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.readings = append(store.readings, reading)
	if len(store.readings) > store.maxReadings {
		store.readings = append([]SensorReading(nil), store.readings[len(store.readings)-store.maxReadings:]...)
	}
}

func (store *Store) Count() int {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.readings)
}

func (store *Store) Latest(limit int) []SensorReading {
	store.mu.RLock()
	defer store.mu.RUnlock()

	if limit <= 0 || limit > len(store.readings) {
		limit = len(store.readings)
	}

	start := len(store.readings) - limit
	output := make([]SensorReading, limit)
	copy(output, store.readings[start:])
	return output
}
