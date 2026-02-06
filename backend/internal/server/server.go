package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	maxIngestBodyBytes = 1 << 20
	maxBatchBodyBytes  = 4 << 20
	maxBatchSize       = 1000
)

type API struct {
	store        Store
	ingestAPIKey string
	stream       *streamHub
}

func NewAPI(store Store, ingestAPIKey string) *API {
	return &API{store: store, ingestAPIKey: ingestAPIKey, stream: newStreamHub()}
}

func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/ready", api.handleReady)
	mux.HandleFunc("/api/ingest", api.handleIngest)
	mux.HandleFunc("/api/ingest/batch", api.handleIngestBatch)
	mux.HandleFunc("/api/readings", api.handleReadings)
	mux.HandleFunc("/api/stream", api.handleStream)
	return mux
}

func (api *API) handleHealth(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) handleReady(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := api.store.Ping(request.Context()); err != nil {
		writeError(response, http.StatusServiceUnavailable, "not ready")
		return
	}

	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (api *API) handleIngest(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !api.authorizeIngestRequest(response, request) {
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxIngestBodyBytes)
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid request body")
		return
	}

	reading, err := DecodeReading(payload)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}

	if err := api.store.Add(request.Context(), reading); err != nil {
		writeError(response, http.StatusInternalServerError, "failed to persist reading")
		return
	}

	api.stream.publish(reading)
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (api *API) handleIngestBatch(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !api.authorizeIngestRequest(response, request) {
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxBatchBodyBytes)
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid request body")
		return
	}

	readings, err := DecodeReadingsBatch(payload, maxBatchSize)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}

	if err := api.store.AddBatch(request.Context(), readings); err != nil {
		writeError(response, http.StatusInternalServerError, "failed to persist readings")
		return
	}

	for _, reading := range readings {
		api.stream.publish(reading)
	}

	writeJSON(response, http.StatusAccepted, map[string]any{
		"status":   "accepted",
		"ingested": len(readings),
	})
}

func (api *API) handleReadings(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 100
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit < 1 || parsedLimit > 10000 {
			writeError(response, http.StatusBadRequest, "limit must be between 1 and 10000")
			return
		}
		limit = parsedLimit
	}

	readings, err := api.store.Latest(request.Context(), limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "failed to read data")
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{"readings": readings})
}

func (api *API) handleStream(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "streaming not supported")
		return
	}

	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")

	channel, unsubscribe := api.stream.subscribe()
	defer unsubscribe()

	heartbeatTicker := time.NewTicker(25 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-request.Context().Done():
			return
		case reading := <-channel:
			payload, err := json.Marshal(reading)
			if err != nil {
				continue
			}
			if _, err = fmt.Fprintf(response, "event: reading\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeatTicker.C:
			if _, err := io.WriteString(response, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (api *API) authorizeIngestRequest(response http.ResponseWriter, request *http.Request) bool {
	providedKey := request.Header.Get("X-API-Key")
	if subtle.ConstantTimeCompare([]byte(providedKey), []byte(api.ingestAPIKey)) != 1 {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func writeJSON(response http.ResponseWriter, statusCode int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(payload)
}

func writeError(response http.ResponseWriter, statusCode int, message string) {
	writeJSON(response, statusCode, map[string]string{"error": message})
}
