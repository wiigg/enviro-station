package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleIngestAcceptsStringAndNumberPayloads(t *testing.T) {
	store := NewStore(100)
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

	count, err := store.Count(context.Background())
	if err != nil {
		t.Fatalf("count readings: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one reading, got %d", count)
	}
}

func TestHandleIngestRejectsInvalidPayload(t *testing.T) {
	store := NewStore(100)
	api := NewAPI(store)
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/ingest", bytes.NewBufferString(`{"timestamp":"oops"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}

	count, err := store.Count(context.Background())
	if err != nil {
		t.Fatalf("count readings: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected zero readings, got %d", count)
	}
}

func TestHandleHealth(t *testing.T) {
	store := NewStore(100)
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
	store := NewStore(100)
	api := NewAPI(store)
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}
