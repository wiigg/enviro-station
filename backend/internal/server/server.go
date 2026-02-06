package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

type API struct {
	store *Store
}

func NewAPI(store *Store) *API {
	return &API{store: store}
}

func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/ready", api.handleReady)
	mux.HandleFunc("/api/ingest", api.handleIngest)
	mux.HandleFunc("/api/readings", api.handleReadings)
	return mux
}

func (api *API) handleHealth(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"status":  "ok",
		"records": api.store.Count(),
	})
}

func (api *API) handleReady(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(response, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

func (api *API) handleIngest(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, 1<<20)
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

	api.store.Add(reading)

	writeJSON(response, http.StatusAccepted, map[string]any{
		"status":  "accepted",
		"records": api.store.Count(),
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

	writeJSON(response, http.StatusOK, map[string]any{
		"readings": api.store.Latest(limit),
	})
}

func writeJSON(response http.ResponseWriter, statusCode int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(payload)
}

func writeError(response http.ResponseWriter, statusCode int, message string) {
	writeJSON(response, statusCode, map[string]string{"error": message})
}
