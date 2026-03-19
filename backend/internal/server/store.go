package server

import (
	"context"
	"errors"
	"sync"
)

var ErrStoreUnavailable = errors.New("durable store unavailable")

type Store interface {
	Add(ctx context.Context, reading SensorReading) error
	AddBatch(ctx context.Context, readings []SensorReading) error
	Latest(ctx context.Context, limit int) ([]SensorReading, error)
	Ping(ctx context.Context) error
	Close()
}

type RuntimeStore struct {
	mu    sync.RWMutex
	store Store
}

func NewRuntimeStore(store Store) *RuntimeStore {
	return &RuntimeStore{store: store}
}

func (store *RuntimeStore) HasStore() bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.store != nil
}

func (store *RuntimeStore) Set(next Store) {
	store.mu.Lock()
	store.store = next
	store.mu.Unlock()
}

func (store *RuntimeStore) Add(ctx context.Context, reading SensorReading) error {
	current := store.current()
	if current == nil {
		return ErrStoreUnavailable
	}
	return current.Add(ctx, reading)
}

func (store *RuntimeStore) AddBatch(ctx context.Context, readings []SensorReading) error {
	current := store.current()
	if current == nil {
		return ErrStoreUnavailable
	}
	return current.AddBatch(ctx, readings)
}

func (store *RuntimeStore) Latest(ctx context.Context, limit int) ([]SensorReading, error) {
	current := store.current()
	if current == nil {
		return nil, ErrStoreUnavailable
	}
	return current.Latest(ctx, limit)
}

func (store *RuntimeStore) Range(
	ctx context.Context,
	fromTimestamp int64,
	toTimestamp int64,
	maxPoints int,
) ([]SensorReading, error) {
	current := store.current()
	if current == nil {
		return nil, ErrStoreUnavailable
	}

	rangeStore, ok := current.(readingsRangeStore)
	if !ok {
		return nil, ErrStoreUnavailable
	}

	return rangeStore.Range(ctx, fromTimestamp, toTimestamp, maxPoints)
}

func (store *RuntimeStore) Ping(ctx context.Context) error {
	current := store.current()
	if current == nil {
		return ErrStoreUnavailable
	}
	return current.Ping(ctx)
}

func (store *RuntimeStore) Close() {
	current := store.current()
	if current == nil {
		return
	}
	current.Close()
}

func (store *RuntimeStore) SaveInsightsSnapshot(
	ctx context.Context,
	snapshot InsightsSnapshot,
) error {
	current := store.current()
	if current == nil {
		return ErrStoreUnavailable
	}

	snapshotStore, ok := current.(InsightsSnapshotStore)
	if !ok {
		return ErrStoreUnavailable
	}

	return snapshotStore.SaveInsightsSnapshot(ctx, snapshot)
}

func (store *RuntimeStore) LatestInsightsSnapshot(
	ctx context.Context,
) (InsightsSnapshot, bool, error) {
	current := store.current()
	if current == nil {
		return InsightsSnapshot{}, false, ErrStoreUnavailable
	}

	snapshotStore, ok := current.(InsightsSnapshotStore)
	if !ok {
		return InsightsSnapshot{}, false, ErrStoreUnavailable
	}

	return snapshotStore.LatestInsightsSnapshot(ctx)
}

func (store *RuntimeStore) AddOpsEvent(ctx context.Context, event OpsEvent) error {
	current := store.current()
	if current == nil {
		return ErrStoreUnavailable
	}

	opsStore, ok := current.(OpsEventStore)
	if !ok {
		return ErrStoreUnavailable
	}

	return opsStore.AddOpsEvent(ctx, event)
}

func (store *RuntimeStore) LatestOpsEvents(ctx context.Context, limit int) ([]OpsEvent, error) {
	current := store.current()
	if current == nil {
		return nil, ErrStoreUnavailable
	}

	opsStore, ok := current.(OpsEventStore)
	if !ok {
		return nil, ErrStoreUnavailable
	}

	return opsStore.LatestOpsEvents(ctx, limit)
}

func (store *RuntimeStore) current() Store {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.store
}
