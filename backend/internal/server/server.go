package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxIngestBodyBytes = 1 << 20
	maxBatchBodyBytes  = 4 << 20
	maxBatchSize       = 1000
	maxReadingsLimit   = 100000
	maxOpsEventsLimit  = 200
	maxInsightsLimit   = 3
)

type readingsRangeStore interface {
	Range(ctx context.Context, fromTimestamp int64, toTimestamp int64, maxPoints int) ([]SensorReading, error)
}

type API struct {
	store                   Store
	ingestAPIKey            string
	trustProxyIP            bool
	stream                  *streamHub
	alertAnalyzer           AlertAnalyzer
	insightsEngine          InsightsEngine
	insightsSchedulerConfig InsightsSchedulerConfig
	opsEventStore           OpsEventStore
	opsConfig               OpsConfig

	opsMu            sync.Mutex
	deviceStateKnown bool
	deviceConnected  bool
	lastDeviceSeenAt time.Time
}

type APIOption func(*API)

func WithAlertAnalyzer(analyzer AlertAnalyzer) APIOption {
	return func(api *API) {
		api.alertAnalyzer = analyzer
	}
}

func WithInsightsEngine(engine InsightsEngine) APIOption {
	return func(api *API) {
		api.insightsEngine = engine
	}
}

func WithInsightsSchedulerConfig(config InsightsSchedulerConfig) APIOption {
	return func(api *API) {
		api.insightsSchedulerConfig = config
	}
}

func WithTrustProxyIP(enabled bool) APIOption {
	return func(api *API) {
		api.trustProxyIP = enabled
	}
}

func WithOpsConfig(config OpsConfig) APIOption {
	return func(api *API) {
		api.opsConfig = config
	}
}

func NewAPI(store Store, ingestAPIKey string, options ...APIOption) *API {
	normalizedIngestAPIKey := strings.TrimSpace(ingestAPIKey)
	api := &API{
		store:                   store,
		ingestAPIKey:            normalizedIngestAPIKey,
		stream:                  newStreamHub(),
		insightsSchedulerConfig: DefaultInsightsSchedulerConfig(),
		opsConfig:               DefaultOpsConfig(),
	}
	for _, option := range options {
		option(api)
	}

	if api.insightsEngine == nil && api.alertAnalyzer != nil {
		scheduler := NewInsightsScheduler(store, api.alertAnalyzer, api.insightsSchedulerConfig)
		scheduler.Start(context.Background())
		api.insightsEngine = scheduler
	}

	if opsStore, ok := store.(OpsEventStore); ok {
		api.opsEventStore = opsStore
		api.initializeDeviceState()
		api.startDeviceMonitor(context.Background())
		api.persistOpsEvent(
			"backend_restarted",
			"Backend restarted",
			"Ops event monitoring is active.",
			time.Now().UnixMilli(),
		)
	}

	return api
}

func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/ready", api.handleReady)
	mux.HandleFunc("/api/ingest", api.handleIngest)
	mux.HandleFunc("/api/ingest/batch", api.handleIngestBatch)
	mux.HandleFunc("/api/readings", api.handleReadings)
	mux.HandleFunc("/api/stream", api.handleStream)
	mux.HandleFunc("/api/insights", api.handleInsights)
	mux.HandleFunc("/api/ops/events", api.handleOpsEvents)
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

	api.onTelemetryReceived(time.Now())
	api.stream.publish(reading)
	if api.insightsEngine != nil {
		api.insightsEngine.OnReading(reading)
	}
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

	if len(readings) > 0 {
		api.onTelemetryReceived(time.Now())
	}
	for _, reading := range readings {
		api.stream.publish(reading)
	}
	if api.insightsEngine != nil {
		api.insightsEngine.OnBatch(readings)
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

	rawFrom := request.URL.Query().Get("from")
	rawTo := request.URL.Query().Get("to")
	if rawFrom != "" || rawTo != "" {
		if rawFrom == "" || rawTo == "" {
			writeError(response, http.StatusBadRequest, "from and to must be provided together")
			return
		}

		rangeStore, ok := api.store.(readingsRangeStore)
		if !ok {
			writeError(response, http.StatusNotImplemented, "readings range query is not supported")
			return
		}

		fromTimestamp, err := parseReadingsTimestamp(rawFrom)
		if err != nil {
			writeError(response, http.StatusBadRequest, "from must be a valid unix timestamp")
			return
		}
		toTimestamp, err := parseReadingsTimestamp(rawTo)
		if err != nil {
			writeError(response, http.StatusBadRequest, "to must be a valid unix timestamp")
			return
		}
		if fromTimestamp >= toTimestamp {
			writeError(response, http.StatusBadRequest, "from must be less than to")
			return
		}

		maxPoints := 1000
		if rawMaxPoints := request.URL.Query().Get("max_points"); rawMaxPoints != "" {
			parsedMaxPoints, maxPointsErr := strconv.Atoi(rawMaxPoints)
			if maxPointsErr != nil || parsedMaxPoints < 1 || parsedMaxPoints > maxReadingsLimit {
				writeError(
					response,
					http.StatusBadRequest,
					fmt.Sprintf("max_points must be between 1 and %d", maxReadingsLimit),
				)
				return
			}
			maxPoints = parsedMaxPoints
		}

		readings, readingsErr := rangeStore.Range(
			request.Context(),
			fromTimestamp,
			toTimestamp,
			maxPoints,
		)
		if readingsErr != nil {
			writeError(response, http.StatusInternalServerError, "failed to read data")
			return
		}

		writeJSON(response, http.StatusOK, map[string]any{"readings": readings})
		return
	}

	limit := 100
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit < 1 || parsedLimit > maxReadingsLimit {
			writeError(
				response,
				http.StatusBadRequest,
				fmt.Sprintf("limit must be between 1 and %d", maxReadingsLimit),
			)
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

func parseReadingsTimestamp(rawValue string) (int64, error) {
	parsedValue, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		return 0, err
	}
	// Frontend sends milliseconds. Stored readings are unix seconds.
	if parsedValue >= 1_000_000_000_000 {
		return parsedValue / 1000, nil
	}
	return parsedValue, nil
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

func (api *API) handleInsights(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if api.insightsEngine == nil {
		writeError(response, http.StatusServiceUnavailable, "insights engine is not configured")
		return
	}

	alertLimit := maxInsightsLimit
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit < 1 || parsedLimit > maxInsightsLimit {
			writeError(
				response,
				http.StatusBadRequest,
				fmt.Sprintf("limit must be between 1 and %d", maxInsightsLimit),
			)
			return
		}
		alertLimit = parsedLimit
	}

	snapshot, ok := api.insightsEngine.Snapshot(alertLimit)
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "insights are warming up")
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"insights":         snapshot.Insights,
		"source":           snapshot.Source,
		"generated_at":     snapshot.GeneratedAt,
		"analyzed_samples": snapshot.AnalyzedSamples,
		"analysis_limit":   snapshot.AnalysisLimit,
		"trigger":          snapshot.Trigger,
	})
}

func (api *API) handleOpsEvents(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if api.opsEventStore == nil {
		writeJSON(response, http.StatusOK, map[string]any{"events": []OpsEvent{}})
		return
	}

	limit := 30
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit < 1 || parsedLimit > maxOpsEventsLimit {
			writeError(
				response,
				http.StatusBadRequest,
				fmt.Sprintf("limit must be between 1 and %d", maxOpsEventsLimit),
			)
			return
		}
		limit = parsedLimit
	}

	events, err := api.opsEventStore.LatestOpsEvents(request.Context(), limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "failed to load ops events")
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{"events": events})
}

func (api *API) authorizeIngestRequest(response http.ResponseWriter, request *http.Request) bool {
	providedKey := request.Header.Get("X-API-Key")
	if subtle.ConstantTimeCompare([]byte(providedKey), []byte(api.ingestAPIKey)) != 1 {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (api *API) initializeDeviceState() {
	if api.opsEventStore == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	events, err := api.opsEventStore.LatestOpsEvents(ctx, 20)
	if err != nil {
		log.Printf("ops events initialization failed: %v", err)
		return
	}
	for _, event := range events {
		lastEventTime := time.UnixMilli(event.Timestamp)

		api.opsMu.Lock()
		switch event.Kind {
		case "device_connected":
			api.deviceStateKnown = true
			api.deviceConnected = true
			api.lastDeviceSeenAt = lastEventTime
			api.opsMu.Unlock()
			return
		case "device_disconnected":
			api.deviceStateKnown = true
			api.deviceConnected = false
			api.lastDeviceSeenAt = lastEventTime
			api.opsMu.Unlock()
			return
		default:
			api.opsMu.Unlock()
		}
	}
}

func (api *API) startDeviceMonitor(ctx context.Context) {
	if api.opsEventStore == nil {
		return
	}
	if api.opsConfig.DeviceOfflineTimeout <= 0 || api.opsConfig.MonitorInterval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(api.opsConfig.MonitorInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				api.evaluateDeviceDisconnect(now)
			}
		}
	}()
}

func (api *API) onTelemetryReceived(observedAt time.Time) {
	if api.opsEventStore == nil {
		return
	}

	shouldLogConnected := false

	api.opsMu.Lock()
	if !api.deviceStateKnown || !api.deviceConnected {
		shouldLogConnected = true
	}
	api.deviceStateKnown = true
	api.deviceConnected = true
	api.lastDeviceSeenAt = observedAt
	api.opsMu.Unlock()

	if shouldLogConnected {
		api.persistOpsEvent(
			"device_connected",
			"Device connected",
			"Telemetry ingest resumed.",
			observedAt.UnixMilli(),
		)
	}
}

func (api *API) evaluateDeviceDisconnect(now time.Time) {
	shouldLogDisconnected := false

	api.opsMu.Lock()
	if api.deviceStateKnown &&
		api.deviceConnected &&
		!api.lastDeviceSeenAt.IsZero() &&
		now.Sub(api.lastDeviceSeenAt) >= api.opsConfig.DeviceOfflineTimeout {
		api.deviceConnected = false
		shouldLogDisconnected = true
	}
	api.opsMu.Unlock()

	if shouldLogDisconnected {
		api.persistOpsEvent(
			"device_disconnected",
			"Device disconnected",
			fmt.Sprintf("No telemetry received for %s.", api.opsConfig.DeviceOfflineTimeout),
			now.UnixMilli(),
		)
	}
}

func (api *API) persistOpsEvent(kind string, title string, detail string, timestamp int64) {
	if api.opsEventStore == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := api.opsEventStore.AddOpsEvent(ctx, OpsEvent{
			Timestamp: timestamp,
			Kind:      kind,
			Title:     title,
			Detail:    detail,
		}); err != nil {
			log.Printf("ops event persist failed kind=%s: %v", kind, err)
		}
	}()
}

func writeJSON(response http.ResponseWriter, statusCode int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(payload)
}

func writeError(response http.ResponseWriter, statusCode int, message string) {
	writeJSON(response, statusCode, map[string]string{"error": message})
}
