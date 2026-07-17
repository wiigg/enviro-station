package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const (
	defaultOutdoorRefreshInterval = 2 * time.Hour
	defaultOutdoorMaxAge          = 90 * time.Minute
	defaultOutdoorRequestTimeout  = 20 * time.Second
	defaultOutdoorDailyLimit      = 12
	outdoorObservationTolerance   = 2 * time.Hour
	outdoorMaxOutputTokens        = 800
	defaultPostcodeBaseURL        = "https://api.postcodes.io"
	defaultWeatherBaseURL         = "https://api.open-meteo.com"
)

var outdoorAllowedDomains = []string{
	"metoffice.gov.uk",
	"uk-air.defra.gov.uk",
	"gov.uk",
}

type OutdoorConditions struct {
	TemperatureC       *float64      `json:"temperature_c"`
	PM2                *float64      `json:"pm2"`
	PM10               *float64      `json:"pm10"`
	AirQualityCategory string        `json:"air_quality_category"`
	ObservedAt         *string       `json:"observed_at"`
	DataQuality        string        `json:"data_quality"`
	FetchedAt          int64         `json:"fetched_at"`
	Sources            []AlertSource `json:"-"`
}

type OutdoorContextSource interface {
	Snapshot() (OutdoorConditions, bool)
}

type OutdoorContextMonitor interface {
	OutdoorContextSource
	Start(ctx context.Context, onMaterialChange func())
}

type OutdoorContextRefresher interface {
	OutdoorContextSource
	EnsureFresh(ctx context.Context) (OutdoorConditions, bool)
}

type OutdoorSearchConfig struct {
	APIKey          string
	Model           string
	ReasoningEffort string
	BaseURL         string
	Location        string
	RefreshInterval time.Duration
	MaxAge          time.Duration
	RequestTimeout  time.Duration
	DailyLimit      int
	PostcodeBaseURL string
	WeatherBaseURL  string
}

type OpenAIOutdoorProvider struct {
	httpClient      *http.Client
	apiKey          string
	model           string
	reasoningEffort string
	baseURL         string
	location        string
	refreshInterval time.Duration
	maxAge          time.Duration
	requestTimeout  time.Duration
	requestBudget   *dailyRequestBudget
	postcodeBaseURL string
	weatherBaseURL  string
	now             func() time.Time

	coordinatesMu  sync.Mutex
	latitude       float64
	longitude      float64
	hasCoordinates bool

	startOnce sync.Once
	refreshMu sync.Mutex
	mu        sync.RWMutex
	latest    OutdoorConditions
	hasLatest bool
}

func NewOpenAIOutdoorProvider(config OutdoorSearchConfig) *OpenAIOutdoorProvider {
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = "gpt-5.6-luna"
	}
	reasoningEffort := strings.TrimSpace(config.ReasoningEffort)
	if reasoningEffort == "" {
		reasoningEffort = "medium"
	}
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	refreshInterval := config.RefreshInterval
	if refreshInterval < time.Minute {
		refreshInterval = defaultOutdoorRefreshInterval
	}
	maxAge := config.MaxAge
	if maxAge < refreshInterval {
		maxAge = defaultOutdoorMaxAge
	}
	requestTimeout := config.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultOutdoorRequestTimeout
	}
	location := strings.TrimSpace(config.Location)
	dailyLimit := config.DailyLimit
	if dailyLimit < 1 {
		dailyLimit = defaultOutdoorDailyLimit
	}

	return &OpenAIOutdoorProvider{
		httpClient:      &http.Client{Timeout: requestTimeout},
		apiKey:          strings.TrimSpace(config.APIKey),
		model:           model,
		reasoningEffort: reasoningEffort,
		baseURL:         strings.TrimRight(baseURL, "/"),
		location:        location,
		refreshInterval: refreshInterval,
		maxAge:          maxAge,
		requestTimeout:  requestTimeout,
		requestBudget:   newDailyRequestBudget(dailyLimit),
		postcodeBaseURL: baseURLOrDefault(config.PostcodeBaseURL, defaultPostcodeBaseURL),
		weatherBaseURL:  baseURLOrDefault(config.WeatherBaseURL, defaultWeatherBaseURL),
		now:             time.Now,
	}
}

func (provider *OpenAIOutdoorProvider) Start(
	ctx context.Context,
	onMaterialChange func(),
) {
	provider.startOnce.Do(func() {
		log.Printf("outdoor context monitor started")
		workerStarted := make(chan struct{})
		go func() {
			close(workerStarted)
			provider.refresh(ctx, onMaterialChange)
			ticker := time.NewTicker(provider.refreshInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					provider.refresh(ctx, onMaterialChange)
				}
			}
		}()
		select {
		case <-workerStarted:
		case <-ctx.Done():
		}
	})
}

func (provider *OpenAIOutdoorProvider) Snapshot() (OutdoorConditions, bool) {
	provider.mu.RLock()
	conditions := provider.latest
	hasLatest := provider.hasLatest
	provider.mu.RUnlock()

	if !hasLatest || provider.currentTime().Sub(time.UnixMilli(conditions.FetchedAt)) > provider.maxAge {
		return OutdoorConditions{}, false
	}
	conditions.Sources = cloneAlertSources(conditions.Sources)
	return conditions, true
}

func (provider *OpenAIOutdoorProvider) refresh(
	parent context.Context,
	onMaterialChange func(),
) {
	log.Printf("outdoor context refresh started timeout=%s", provider.requestTimeout)
	conditions, changed, err := provider.fetchAndStore(parent, true)
	if err != nil {
		log.Printf("outdoor context refresh failed: %v", err)
		return
	}

	log.Printf(
		"outdoor context refreshed temperature_available=%t pm_available=%t quality=%s material_change=%t",
		conditions.TemperatureC != nil,
		conditions.PM2 != nil || conditions.PM10 != nil,
		conditions.AirQualityCategory,
		changed,
	)
	if changed && onMaterialChange != nil {
		onMaterialChange()
	}
}

func (provider *OpenAIOutdoorProvider) EnsureFresh(parent context.Context) (OutdoorConditions, bool) {
	if conditions, ok := provider.Snapshot(); ok {
		return conditions, true
	}

	conditions, _, err := provider.fetchAndStore(parent, false)
	if err != nil {
		log.Printf("on-demand outdoor context refresh failed: %v", err)
		return OutdoorConditions{}, false
	}
	log.Printf("on-demand outdoor context refresh completed")
	return conditions, true
}

func (provider *OpenAIOutdoorProvider) fetchAndStore(
	parent context.Context,
	force bool,
) (OutdoorConditions, bool, error) {
	provider.refreshMu.Lock()
	defer provider.refreshMu.Unlock()

	if !force {
		if conditions, ok := provider.Snapshot(); ok {
			return conditions, false, nil
		}
	}

	ctx, cancel := context.WithTimeout(parent, provider.requestTimeout)
	defer cancel()

	conditions, err := provider.fetch(ctx)
	if err != nil {
		return OutdoorConditions{}, false, err
	}

	provider.mu.Lock()
	changed := !provider.hasLatest || outdoorConditionsMateriallyChanged(provider.latest, conditions)
	provider.latest = conditions
	provider.hasLatest = true
	provider.mu.Unlock()
	return conditions, changed, nil
}

func (provider *OpenAIOutdoorProvider) fetch(ctx context.Context) (OutdoorConditions, error) {
	if provider.apiKey == "" || provider.location == "" {
		return OutdoorConditions{}, fmt.Errorf("outdoor search is not configured")
	}
	now := provider.currentTime().UTC()
	localNow := outdoorLocalTime(now)
	if !provider.requestBudget.take(now) {
		return OutdoorConditions{}, fmt.Errorf("outdoor daily request limit reached")
	}

	requestPayload := map[string]any{
		"model":             provider.model,
		"max_output_tokens": outdoorMaxOutputTokens,
		"max_tool_calls":    1,
		"reasoning": map[string]any{
			"effort": provider.reasoningEffort,
		},
		"tools": []map[string]any{{
			"type":                "web_search",
			"search_context_size": "low",
			"external_web_access": true,
			"filters": map[string]any{
				"allowed_domains": outdoorAllowedDomains,
			},
			"user_location": map[string]any{
				"type":     "approximate",
				"country":  "GB",
				"timezone": "Europe/London",
			},
		}},
		"tool_choice": "required",
		"include":     []string{"web_search_call.action.sources"},
		"input": []map[string]any{{
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": fmt.Sprintf(
					"This lookup exists only to provide current outdoor air-quality context for indoor insights. You must use web search now because the cached context is missing or stale. The current instant is %s UTC and the current UK local instant is %s. Search current official UK air-quality sources for postcode %s; do not provide general health advice or recommendations. Temperature is retrieved separately by a deterministic weather service: do not search for it and set temperature_c to null. Return current PM2.5 and PM10 in ug/m3 when reliably available, the UK air-quality category, the observation or forecast time, and whether values are observed or forecast. Do not substitute daily values or readings from another time. Use null for unavailable particulate measurements or time. Do not return or repeat the postcode, town, address, coordinates, or any other location identifier.",
					now.Format(time.RFC3339),
					localNow.Format("Monday 2 January 2006 15:04 MST (UTCZ07:00)"),
					provider.location,
				),
			}},
		}},
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "outdoor_conditions",
				"strict": true,
				"schema": outdoorConditionsSchema(),
			},
		},
	}

	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return OutdoorConditions{}, fmt.Errorf("marshal outdoor request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		provider.baseURL+"/responses",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return OutdoorConditions{}, fmt.Errorf("build outdoor request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+provider.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := provider.httpClient.Do(request)
	if err != nil {
		return OutdoorConditions{}, fmt.Errorf("outdoor request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return OutdoorConditions{}, fmt.Errorf("read outdoor response: %w", err)
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		return OutdoorConditions{}, fmt.Errorf("openai outdoor status %d", response.StatusCode)
	}

	var modelResponse responsesAPIResponse
	if err = json.Unmarshal(body, &modelResponse); err != nil {
		return OutdoorConditions{}, fmt.Errorf("decode outdoor response: %w", err)
	}
	text := responseOutputText(modelResponse)
	if text == "" {
		return OutdoorConditions{}, fmt.Errorf("outdoor response did not include text output")
	}

	var conditions OutdoorConditions
	if err = json.Unmarshal([]byte(text), &conditions); err != nil {
		return OutdoorConditions{}, fmt.Errorf("invalid outdoor payload: %w", err)
	}
	conditions.TemperatureC = nil
	conditions.PM2 = boundedOutdoorMetric(conditions.PM2, 0, 2000)
	conditions.PM10 = boundedOutdoorMetric(conditions.PM10, 0, 2000)
	conditions.AirQualityCategory = normalizeOutdoorCategory(conditions.AirQualityCategory)
	conditions.DataQuality = normalizeOutdoorDataQuality(conditions.DataQuality)
	conditions.ObservedAt = normalizeOutdoorObservedAt(conditions.ObservedAt, now)
	temperature, observedAt, weatherErr := provider.fetchCurrentTemperature(ctx, now)
	if weatherErr != nil {
		log.Printf("current outdoor temperature unavailable: %v", weatherErr)
	} else {
		conditions.TemperatureC = &temperature
		conditions.ObservedAt = &observedAt
		if conditions.PM2 != nil || conditions.PM10 != nil || conditions.AirQualityCategory != "unknown" {
			conditions.DataQuality = "mixed"
		} else {
			conditions.DataQuality = "forecast"
		}
	}
	conditions.FetchedAt = now.UnixMilli()
	conditions.Sources = provider.sanitizedSources(responseSourceURLs(modelResponse))
	if weatherErr == nil {
		conditions.Sources = appendOutdoorSource(conditions.Sources, AlertSource{
			Title: "Open-Meteo",
			URL:   "https://open-meteo.com/",
		})
	}
	if len(conditions.Sources) == 0 {
		return OutdoorConditions{}, fmt.Errorf("outdoor response did not include a safe citation")
	}
	if conditions.ObservedAt == nil {
		return OutdoorConditions{}, fmt.Errorf("outdoor response did not include a current observation timestamp")
	}
	if !hasUsefulOutdoorData(conditions) {
		return OutdoorConditions{}, fmt.Errorf("outdoor response did not include usable conditions")
	}
	return conditions, nil
}

func outdoorConditionsSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"temperature_c",
			"pm2",
			"pm10",
			"air_quality_category",
			"observed_at",
			"data_quality",
		},
		"properties": map[string]any{
			"temperature_c": map[string]any{"type": []string{"number", "null"}},
			"pm2":           map[string]any{"type": []string{"number", "null"}},
			"pm10":          map[string]any{"type": []string{"number", "null"}},
			"air_quality_category": map[string]any{
				"type": "string",
				"enum": []string{"good", "moderate", "poor", "very_poor", "unknown"},
			},
			"observed_at": map[string]any{"type": []string{"string", "null"}},
			"data_quality": map[string]any{
				"type": "string",
				"enum": []string{"observed", "forecast", "mixed", "unknown"},
			},
		},
	}
}

func (provider *OpenAIOutdoorProvider) fetchCurrentTemperature(
	ctx context.Context,
	now time.Time,
) (float64, string, error) {
	latitude, longitude, err := provider.resolveCoordinates(ctx)
	if err != nil {
		return 0, "", err
	}

	endpoint, err := url.Parse(provider.weatherBaseURL + "/v1/forecast")
	if err != nil {
		return 0, "", fmt.Errorf("build weather URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(latitude, 'f', 6, 64))
	query.Set("longitude", strconv.FormatFloat(longitude, 'f', 6, 64))
	query.Set("current", "temperature_2m")
	query.Set("timeformat", "unixtime")
	endpoint.RawQuery = query.Encode()

	var payload struct {
		Current struct {
			Time         int64   `json:"time"`
			TemperatureC float64 `json:"temperature_2m"`
		} `json:"current"`
	}
	if err = provider.getJSON(ctx, endpoint.String(), &payload); err != nil {
		return 0, "", fmt.Errorf("fetch current weather: %w", err)
	}
	temperature := boundedOutdoorMetric(&payload.Current.TemperatureC, -60, 60)
	if temperature == nil {
		return 0, "", fmt.Errorf("current weather temperature was invalid")
	}
	observed := time.Unix(payload.Current.Time, 0).UTC().Format(time.RFC3339)
	normalizedObserved := normalizeOutdoorObservedAt(&observed, now)
	if normalizedObserved == nil {
		return 0, "", fmt.Errorf("current weather timestamp was stale")
	}
	return *temperature, *normalizedObserved, nil
}

func (provider *OpenAIOutdoorProvider) resolveCoordinates(ctx context.Context) (float64, float64, error) {
	provider.coordinatesMu.Lock()
	defer provider.coordinatesMu.Unlock()
	if provider.hasCoordinates {
		return provider.latitude, provider.longitude, nil
	}

	postcode := strings.ReplaceAll(strings.ToUpper(provider.location), " ", "")
	endpoint := provider.postcodeBaseURL + "/postcodes/" + url.PathEscape(postcode)
	var payload struct {
		Result *struct {
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
		} `json:"result"`
	}
	if err := provider.getJSON(ctx, endpoint, &payload); err != nil {
		return 0, 0, fmt.Errorf("resolve outdoor location: %w", err)
	}
	if payload.Result == nil || payload.Result.Latitude == nil || payload.Result.Longitude == nil ||
		*payload.Result.Latitude < -90 || *payload.Result.Latitude > 90 ||
		*payload.Result.Longitude < -180 || *payload.Result.Longitude > 180 {
		return 0, 0, fmt.Errorf("outdoor location did not resolve to valid coordinates")
	}
	provider.latitude = *payload.Result.Latitude
	provider.longitude = *payload.Result.Longitude
	provider.hasCoordinates = true
	return provider.latitude, provider.longitude, nil
}

func (provider *OpenAIOutdoorProvider) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := provider.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (provider *OpenAIOutdoorProvider) sanitizedSources(rawURLs []string) []AlertSource {
	sources := make([]AlertSource, 0, 3)
	seenProviders := make(map[string]struct{})
	for _, rawURL := range rawURLs {
		source, ok := sanitizeOutdoorSource(rawURL)
		if !ok {
			continue
		}
		if _, exists := seenProviders[source.Title]; exists {
			continue
		}
		seenProviders[source.Title] = struct{}{}
		sources = append(sources, source)
		if len(sources) == 3 {
			break
		}
	}
	return sources
}

func sanitizeOutdoorSource(rawURL string) (AlertSource, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return AlertSource{}, false
	}
	host := strings.ToLower(parsed.Hostname())
	if !outdoorDomainAllowed(host) {
		return AlertSource{}, false
	}
	parsed.Path = "/"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return AlertSource{Title: outdoorSourceTitle(host), URL: parsed.String()}, true
}

func outdoorDomainAllowed(host string) bool {
	for _, domain := range outdoorAllowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func outdoorSourceTitle(host string) string {
	switch {
	case strings.HasSuffix(host, "metoffice.gov.uk"):
		return "Met Office"
	case strings.HasSuffix(host, "uk-air.defra.gov.uk"):
		return "UK-AIR"
	case strings.HasSuffix(host, "gov.uk"):
		return "GOV.UK"
	default:
		return "Outdoor source"
	}
}

func cloneAlertSources(sources []AlertSource) []AlertSource {
	output := make([]AlertSource, len(sources))
	copy(output, sources)
	return output
}

func appendOutdoorSource(sources []AlertSource, source AlertSource) []AlertSource {
	for _, existing := range sources {
		if existing.Title == source.Title {
			return sources
		}
	}
	if len(sources) >= 3 {
		sources = sources[:2]
	}
	return append(sources, source)
}

func boundedOutdoorMetric(value *float64, minimum, maximum float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < minimum || *value > maximum {
		return nil
	}
	return value
}

func normalizeOutdoorObservedAt(value *string, now time.Time) *string {
	if value == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	if err != nil {
		return nil
	}
	if difference := parsed.Sub(now); difference < -outdoorObservationTolerance || difference > outdoorObservationTolerance {
		return nil
	}
	normalized := parsed.UTC().Format(time.RFC3339)
	return &normalized
}

func (provider *OpenAIOutdoorProvider) currentTime() time.Time {
	if provider.now != nil {
		return provider.now()
	}
	return time.Now()
}

func outdoorLocalTime(now time.Time) time.Time {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		return now.UTC()
	}
	return now.In(location)
}

func baseURLOrDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return strings.TrimRight(trimmed, "/")
	}
	return fallback
}

func hasUsefulOutdoorData(conditions OutdoorConditions) bool {
	return conditions.TemperatureC != nil || conditions.PM2 != nil || conditions.PM10 != nil ||
		conditions.AirQualityCategory != "unknown"
}

func normalizeOutdoorCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "good", "moderate", "poor", "very_poor":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func normalizeOutdoorDataQuality(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "observed", "forecast", "mixed":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func outdoorConditionsMateriallyChanged(previous, current OutdoorConditions) bool {
	return nullableMetricChanged(previous.TemperatureC, current.TemperatureC, 2) ||
		nullableMetricChanged(previous.PM2, current.PM2, 5) ||
		nullableMetricChanged(previous.PM10, current.PM10, 15) ||
		previous.AirQualityCategory != current.AirQualityCategory
}

func nullableMetricChanged(previous, current *float64, threshold float64) bool {
	if previous == nil || current == nil {
		return previous != nil || current != nil
	}
	return math.Abs(*current-*previous) >= threshold
}
