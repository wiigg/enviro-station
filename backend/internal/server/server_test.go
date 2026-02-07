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
	latestErr   error
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
	if store.latestErr != nil {
		return nil, store.latestErr
	}

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

type fakeAlertAnalyzer struct {
	alerts []Alert
	err    error
	source string
	calls  int
}

func (analyzer *fakeAlertAnalyzer) Analyze(_ context.Context, _ []SensorReading) ([]Alert, error) {
	analyzer.calls++
	if analyzer.err != nil {
		return nil, analyzer.err
	}
	output := make([]Alert, len(analyzer.alerts))
	copy(output, analyzer.alerts)
	return output, nil
}

func (analyzer *fakeAlertAnalyzer) Source() string {
	if analyzer.source == "" {
		return "test"
	}
	return analyzer.source
}

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

func TestHandleAlertsReturnsServiceUnavailableWithoutAnalyzer(t *testing.T) {
	store := &fakeStore{}
	api := NewAPI(store, "secret")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/api/insights", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
}

func TestHandleAlertsReturnsAnalyzedAlerts(t *testing.T) {
	store := &fakeStore{
		latest: []SensorReading{
			{Timestamp: 1738886400000, PM2: 3.2, PM10: 5.7, Temperature: 21.4, Humidity: 45},
			{Timestamp: 1738886460000, PM2: 14.8, PM10: 22.1, Temperature: 24.2, Humidity: 58},
		},
	}
	analyzer := &fakeAlertAnalyzer{
		source: "openai",
		alerts: []Alert{
			{
				Kind:     "alert",
				Severity: "warn",
				Title:    "PM2.5 rising",
				Message:  "PM2.5 is elevated compared with baseline.",
			},
			{
				Kind:     "insight",
				Severity: "info",
				Title:    "Humidity climbing",
				Message:  "Humidity is trending up over the last hour.",
			},
		},
	}
	api := NewAPI(store, "secret", WithAlertAnalyzer(analyzer))
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/api/insights?analysis_limit=100&limit=1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if analyzer.calls != 1 {
		t.Fatalf("expected analyzer to be called once, got %d", analyzer.calls)
	}

	var payload struct {
		Insights []Alert `json:"insights"`
		Source   string  `json:"source"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload.Source != "openai" {
		t.Fatalf("expected source openai, got %q", payload.Source)
	}
	if len(payload.Insights) != 1 {
		t.Fatalf("expected exactly one insight, got %d", len(payload.Insights))
	}
	if payload.Insights[0].Severity != "warn" {
		t.Fatalf("expected warn severity, got %q", payload.Insights[0].Severity)
	}
	if payload.Insights[0].Kind != "alert" {
		t.Fatalf("expected alert kind, got %q", payload.Insights[0].Kind)
	}
}

func TestHandleAlertsReturnsBadGatewayWhenAnalyzerFails(t *testing.T) {
	store := &fakeStore{
		latest: []SensorReading{
			{Timestamp: 1738886400000, PM2: 3.2, PM10: 5.7, Temperature: 21.4, Humidity: 45},
		},
	}
	analyzer := &fakeAlertAnalyzer{err: errors.New("analyzer unavailable")}
	api := NewAPI(store, "secret", WithAlertAnalyzer(analyzer))
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "/api/insights", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, response.Code)
	}
}

func TestHandleInsightsRateLimit(t *testing.T) {
	store := &fakeStore{
		latest: []SensorReading{
			{Timestamp: 1738886400000, PM2: 8.2, PM10: 14.7, Temperature: 22.1, Humidity: 47},
		},
	}
	analyzer := &fakeAlertAnalyzer{
		source: "openai",
		alerts: []Alert{
			{Kind: "tip", Severity: "info", Title: "Ventilation tip", Message: "Open windows for 10 minutes."},
		},
	}
	api := NewAPI(store, "secret", WithAlertAnalyzer(analyzer))
	handler := api.Handler()

	for index := 0; index < 30; index++ {
		request := httptest.NewRequest(http.MethodGet, "/api/insights", nil)
		request.RemoteAddr = "203.0.113.2:5050"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("expected status %d for request %d, got %d", http.StatusOK, index+1, response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/insights", nil)
	request.RemoteAddr = "203.0.113.2:5050"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, response.Code)
	}
}
