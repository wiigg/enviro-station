package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
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
	maxReadingsLimit   = 2500
	maxOpsEventsLimit  = 200
	maxInsightsLimit   = 3
)

type readingsRangeStore interface {
	Range(
		ctx context.Context,
		fromTimestamp int64,
		toTimestamp int64,
		deviceID string,
		maxPoints int,
	) ([]SensorReading, error)
}

type API struct {
	store                   Store
	ingestAPIKey            string
	readAPIKey              string
	trustProxyIP            bool
	stream                  *streamHub
	live                    *liveBuffer
	ops                     *opsEventBuffer
	readRateLimiter         *readRateLimiter
	alertAnalyzer           AlertAnalyzer
	insightsEngine          InsightsEngine
	insightsSchedulerConfig InsightsSchedulerConfig
	outdoorContext          OutdoorContextSource
	opsEventStore           OpsEventStore
	opsConfig               OpsConfig

	opsMu            sync.Mutex
	deviceStateKnown bool
	deviceConnected  bool
	lastDeviceSeenAt time.Time
	liveBufferLimit  int
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

func WithOutdoorContext(source OutdoorContextSource) APIOption {
	return func(api *API) {
		api.outdoorContext = source
	}
}

func WithTrustProxyIP(enabled bool) APIOption {
	return func(api *API) {
		api.trustProxyIP = enabled
	}
}

func WithReadAPIKey(readAPIKey string) APIOption {
	return func(api *API) {
		api.readAPIKey = strings.TrimSpace(readAPIKey)
	}
}

func WithReadRateLimit(config ReadRateLimitConfig) APIOption {
	return func(api *API) {
		api.readRateLimiter = newReadRateLimiter(config)
	}
}

func WithOpsConfig(config OpsConfig) APIOption {
	return func(api *API) {
		api.opsConfig = config
	}
}

func WithLiveBufferLimit(limit int) APIOption {
	return func(api *API) {
		api.liveBufferLimit = limit
	}
}

func NewAPI(store Store, ingestAPIKey string, options ...APIOption) *API {
	normalizedIngestAPIKey := strings.TrimSpace(ingestAPIKey)
	api := &API{
		store:                   store,
		ingestAPIKey:            normalizedIngestAPIKey,
		stream:                  newStreamHub(),
		ops:                     newOpsEventBuffer(maxOpsEventsLimit),
		readRateLimiter:         newReadRateLimiter(DefaultReadRateLimitConfig()),
		insightsSchedulerConfig: DefaultInsightsSchedulerConfig(),
		opsConfig:               DefaultOpsConfig(),
		liveBufferLimit:         3600,
	}
	for _, option := range options {
		option(api)
	}
	api.live = newLiveBuffer(api.liveBufferLimit)

	if api.insightsEngine == nil && api.alertAnalyzer != nil {
		scheduler := NewInsightsScheduler(
			store,
			api.alertAnalyzer,
			api.insightsSchedulerConfig,
			WithInsightsLiveReadings(api.live.latest),
			WithInsightsOutdoorContext(api.outdoorContext),
		)
		scheduler.Start(context.Background())
		api.insightsEngine = scheduler
	}

	if opsStore, ok := store.(OpsEventStore); ok {
		api.opsEventStore = opsStore
		api.startDeviceMonitor(context.Background())
		if durableStoreReady(store) {
			api.initializeDeviceState()
			api.persistOpsEvent(
				"backend_restarted",
				"Backend restarted",
				"Ops event monitoring is active.",
				time.Now().UnixMilli(),
			)
		}
	}

	return api
}

func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.handleHealth)
	mux.HandleFunc("/ready", api.handleReady)
	mux.HandleFunc("/api/ingest", api.handleIngest)
	mux.HandleFunc("/api/ingest/batch", api.handleIngestBatch)
	mux.HandleFunc("/api/live", api.handleLive)
	mux.HandleFunc("/api/live/status", api.handleLiveStatus)
	mux.HandleFunc("/api/readings", api.handleReadings)
	mux.HandleFunc("/api/stream", api.handleStream)
	mux.HandleFunc("/api/insights", api.handleInsights)
	mux.HandleFunc("/api/ops/events", api.handleOpsEvents)
	return mux
}

func durableStoreReady(store Store) bool {
	availability, ok := store.(interface{ HasStore() bool })
	if !ok {
		return true
	}
	return availability.HasStore()
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
		if errors.Is(err, ErrStoreUnavailable) {
			api.onTelemetryReceived(time.Now())
			api.publishLive(reading)
			if api.insightsEngine != nil {
				api.insightsEngine.OnReading(reading)
			}
			log.Printf(
				"reading accepted path=%s persisted=false timestamp=%d remote=%s",
				request.URL.Path,
				reading.Timestamp,
				request.RemoteAddr,
			)
			writeJSON(response, http.StatusAccepted, map[string]any{
				"status":    "accepted",
				"persisted": false,
			})
			return
		}
		writeError(response, http.StatusInternalServerError, "failed to persist reading")
		return
	}

	api.onTelemetryReceived(time.Now())
	api.publishLive(reading)
	if api.insightsEngine != nil {
		api.insightsEngine.OnReading(reading)
	}
	log.Printf(
		"reading accepted path=%s persisted=true timestamp=%d remote=%s",
		request.URL.Path,
		reading.Timestamp,
		request.RemoteAddr,
	)
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
		if errors.Is(err, ErrStoreUnavailable) {
			if len(readings) > 0 {
				api.onTelemetryReceived(time.Now())
			}
			for _, reading := range readings {
				api.publishLive(reading)
			}
			if api.insightsEngine != nil {
				api.insightsEngine.OnBatch(readings)
			}
			log.Printf(
				"batch accepted path=%s persisted=false count=%d remote=%s",
				request.URL.Path,
				len(readings),
				request.RemoteAddr,
			)
			writeJSON(response, http.StatusAccepted, map[string]any{
				"status":    "accepted",
				"ingested":  len(readings),
				"persisted": false,
			})
			return
		}
		writeError(response, http.StatusInternalServerError, "failed to persist readings")
		return
	}

	if len(readings) > 0 {
		api.onTelemetryReceived(time.Now())
	}
	for _, reading := range readings {
		api.publishLive(reading)
	}
	if api.insightsEngine != nil {
		api.insightsEngine.OnBatch(readings)
	}
	log.Printf(
		"batch accepted path=%s persisted=true count=%d remote=%s",
		request.URL.Path,
		len(readings),
		request.RemoteAddr,
	)

	writeJSON(response, http.StatusAccepted, map[string]any{
		"status":   "accepted",
		"ingested": len(readings),
	})
}

func (api *API) handleLive(response http.ResponseWriter, request *http.Request) {
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

	api.onTelemetryReceived(time.Now())
	api.publishLive(reading)
	if api.insightsEngine != nil {
		api.insightsEngine.OnReading(reading)
	}
	log.Printf(
		"reading accepted path=%s persisted=false timestamp=%d remote=%s",
		request.URL.Path,
		reading.Timestamp,
		request.RemoteAddr,
	)
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (api *API) handleLiveStatus(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !api.authorizeIngestRequest(response, request) {
		return
	}

	deviceID := strings.TrimSpace(request.URL.Query().Get("device_id"))
	writeJSON(response, http.StatusOK, map[string]any{
		"subscriber_count": api.stream.subscriberCount(deviceID),
	})
}

func (api *API) handleReadings(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !api.authorizeReadRequest(response, request) || !api.allowReadRequest(response, request) {
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
			strings.TrimSpace(request.URL.Query().Get("device_id")),
			maxPoints,
		)
		if readingsErr != nil {
			if errors.Is(readingsErr, ErrStoreUnavailable) {
				writeError(response, http.StatusServiceUnavailable, "durable history unavailable")
				return
			}
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

	source := strings.TrimSpace(request.URL.Query().Get("source"))
	deviceID := strings.TrimSpace(request.URL.Query().Get("device_id"))
	var readings []SensorReading
	if source == "live" {
		readings = api.live.latestForDevice(limit, deviceID)
	} else {
		var err error
		readings, err = api.store.Latest(request.Context(), limit)
		if err != nil {
			if errors.Is(err, ErrStoreUnavailable) {
				writeError(response, http.StatusServiceUnavailable, "durable history unavailable")
				return
			}
			writeError(response, http.StatusInternalServerError, "failed to read data")
			return
		}
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
	if !api.authorizeReadRequest(response, request) || !api.allowReadRequest(response, request) {
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

	deviceID := strings.TrimSpace(request.URL.Query().Get("device_id"))
	channel, unsubscribe := api.stream.subscribe(deviceID)
	defer unsubscribe()

	if _, err := io.WriteString(response, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

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
	if !api.authorizeReadRequest(response, request) || !api.allowReadRequest(response, request) {
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
	if !api.authorizeReadRequest(response, request) || !api.allowReadRequest(response, request) {
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

	if strings.TrimSpace(request.URL.Query().Get("source")) == "live" || api.opsEventStore == nil {
		writeJSON(response, http.StatusOK, map[string]any{"events": api.ops.latest(limit)})
		return
	}

	events, err := api.opsEventStore.LatestOpsEvents(request.Context(), limit)
	if err != nil {
		if errors.Is(err, ErrStoreUnavailable) {
			writeJSON(response, http.StatusOK, map[string]any{"events": api.ops.latest(limit)})
			return
		}
		writeError(response, http.StatusInternalServerError, "failed to load ops events")
		return
	}

	writeJSON(response, http.StatusOK, map[string]any{"events": events})
}

func (api *API) authorizeIngestRequest(response http.ResponseWriter, request *http.Request) bool {
	providedKey := request.Header.Get("X-API-Key")
	if subtle.ConstantTimeCompare([]byte(providedKey), []byte(api.ingestAPIKey)) != 1 {
		log.Printf("ingest unauthorized path=%s remote=%s", request.URL.Path, request.RemoteAddr)
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (api *API) authorizeReadRequest(response http.ResponseWriter, request *http.Request) bool {
	if api.readAPIKey == "" {
		return true
	}

	providedKey := strings.TrimSpace(request.Header.Get("X-Read-API-Key"))
	if providedKey == "" {
		providedKey = strings.TrimSpace(request.Header.Get("X-API-Key"))
	}
	if providedKey == "" {
		providedKey = strings.TrimSpace(request.URL.Query().Get("read_key"))
	}
	if providedKey == "" {
		if cookie, err := request.Cookie("read_api_key"); err == nil {
			providedKey = strings.TrimSpace(cookie.Value)
		}
	}
	if subtle.ConstantTimeCompare([]byte(providedKey), []byte(api.readAPIKey)) != 1 {
		log.Printf("read unauthorized path=%s remote=%s", request.URL.Path, request.RemoteAddr)
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (api *API) allowReadRequest(response http.ResponseWriter, request *http.Request) bool {
	if api.readRateLimiter == nil {
		return true
	}

	allowed, retryAfter := api.readRateLimiter.allow(api.clientIP(request))
	if allowed {
		return true
	}

	retrySeconds := int((retryAfter + time.Second - 1) / time.Second)
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	response.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
	writeError(response, http.StatusTooManyRequests, "rate limit exceeded")
	return false
}

func (api *API) clientIP(request *http.Request) string {
	if api.trustProxyIP {
		forwardedFor := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
		if forwardedFor != "" {
			clientIP, _, _ := strings.Cut(forwardedFor, ",")
			clientIP = strings.TrimSpace(clientIP)
			if parsedIP := net.ParseIP(clientIP); parsedIP != nil {
				return parsedIP.String()
			}
		}

		realIP := strings.TrimSpace(request.Header.Get("X-Real-IP"))
		if parsedIP := net.ParseIP(realIP); parsedIP != nil {
			return parsedIP.String()
		}
	}

	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func (api *API) publishLive(reading SensorReading) {
	if !api.live.addIfNewer(reading) {
		return
	}
	api.stream.publish(reading)
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
	event := OpsEvent{
		Timestamp: timestamp,
		Kind:      kind,
		Title:     title,
		Detail:    detail,
	}
	api.ops.add(event)

	if api.opsEventStore == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := api.opsEventStore.AddOpsEvent(ctx, event); err != nil {
			if errors.Is(err, ErrStoreUnavailable) {
				return
			}
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
