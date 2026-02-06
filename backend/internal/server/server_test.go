package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeStore struct {
	added   []SensorReading
	latest  []SensorReading
	pingErr error
	addErr  error
}

func (store *fakeStore) Add(_ context.Context, reading SensorReading) error {
	if store.addErr != nil {
		return store.addErr
	}
	store.added = append(store.added, reading)
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

func TestHandleIngestAcceptsStringAndNumberPayloads(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store)
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
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}

	if len(store.added) != 1 {
		t.Fatalf("expected one stored reading, got %d", len(store.added))
	}
}

func TestHandleIngestRejectsInvalidPayload(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store)
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewBufferString(`{"timestamp":"oops"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}

	if len(store.added) != 0 {
		t.Fatalf("expected no stored readings, got %d", len(store.added))
	}
}

func TestHandleIngestReturnsInternalErrorWhenStoreFails(t *testing.T) {
	store := &fakeStore{addErr: errors.New("write failed")}
	api := NewAPI(store)
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
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, response.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store)
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
	api := NewAPI(store)
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
	api := NewAPI(store)
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
}
