package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStore struct {
	added       []SensorReading
	latest      []SensorReading
	pingErr     error
	addErr      error
	addBatchErr error
}

func (store *fakeStore) Add(_ context.Context, reading SensorReading) error {
	if store.addErr != nil {
		return store.addErr
	}
	store.added = append(store.added, reading)
	return nil
}

func (store *fakeStore) AddBatch(_ context.Context, readings []SensorReading) error {
	if store.addBatchErr != nil {
		return store.addBatchErr
	}
	store.added = append(store.added, readings...)
	return nil
}

func (store *fakeStore) Latest(_ context.Context, limit int) ([]SensorReading, error) {
	if limit <= 0 || limit > len(store.latest) {
		limit = len(store.latest)
	}
	start := len(store.latest) - limit
	output := make([]SensorReading, limit)
	copy(output, store.latest[start:])
	return output, nil
}

func (store *fakeStore) Ping(_ context.Context) error {
	return store.pingErr
}

func (store *fakeStore) Close() {}

func TestHandleIngestRejectsUnauthorized(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestHandleIngestAcceptsStringAndNumberPayloads(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	payload := map[string]any{
		"timestamp":   "1738886400",
		"temperature": "22.4",
		"pressure":    101305.2,
		"humidity":    "40.1",
		"oxidised":    "1.2",
		"reduced":     "1.1",
		"nh3":         "0.7",
		"pm1":         "2",
		"pm2":         3,
		"pm10":        "4",
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", "secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}

	if len(store.added) != 1 {
		t.Fatalf("expected one stored reading, got %d", len(store.added))
	}
}

func TestHandleIngestBatchAcceptsMultipleReadings(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/ingest/batch", bytes.NewBufferString(`[
		{"timestamp":"1738886400","temperature":"22.4","pressure":"101305","humidity":"40.1","oxidised":"1.2","reduced":"1.1","nh3":"0.7","pm1":"2","pm2":"3","pm10":"4"},
		{"timestamp":"1738886401","temperature":"22.5","pressure":"101300","humidity":"40.2","oxidised":"1.3","reduced":"1.0","nh3":"0.8","pm1":"3","pm2":"4","pm10":"5"}
	]`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", "secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}

	if len(store.added) != 2 {
		t.Fatalf("expected two stored readings, got %d", len(store.added))
	}
}

func TestHandleIngestBatchRejectsOversizedBatch(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	items := make([]string, 0, maxBatchSize+1)
	for i := 0; i <= maxBatchSize; i++ {
		items = append(items, `{"timestamp":"1738886400","temperature":"22.4","pressure":"101305","humidity":"40.1","oxidised":"1.2","reduced":"1.1","nh3":"0.7","pm1":"2","pm2":"3","pm10":"4"}`)
	}
	payload := "[" + strings.Join(items, ",") + "]"

	request := httptest.NewRequest(http.MethodPost, "/api/ingest/batch", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", "secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestHandleIngestReturnsInternalErrorWhenStoreFails(t *testing.T) {
	store := &fakeStore{addErr: errors.New("write failed")}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewBufferString(`{
		"timestamp":"1738886400",
		"temperature":"22.4",
		"pressure":"101305",
		"humidity":"40.1",
		"oxidised":"1.2",
		"reduced":"1.1",
		"nh3":"0.7",
		"pm1":"2",
		"pm2":"3",
		"pm10":"4"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", "secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, response.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestHandleReady(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestHandleReadyReturnsServiceUnavailableWhenStoreUnreachable(t *testing.T) {
	store := &fakeStore{pingErr: errors.New("db down")}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
}
