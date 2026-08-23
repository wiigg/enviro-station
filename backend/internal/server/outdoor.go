package server

import (
	"context"
	"encoding/json"
	"errors"
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
)

const (
	defaultOutdoorRefreshInterval = 2 * time.Hour
	defaultOutdoorMaxAge          = 3 * time.Hour
	defaultOutdoorRequestTimeout  = 20 * time.Second
	defaultOutdoorInitialRetry    = 5 * time.Minute
	outdoorRefreshGrace           = 15 * time.Minute
	outdoorObservationTolerance   = 2 * time.Hour
	defaultPostcodeBaseURL        = "https://api.postcodes.io"
	defaultWeatherBaseURL         = "https://api.open-meteo.com"
	defaultAirQualityBaseURL      = "https://air-quality-api.open-meteo.com"
	outdoorCoordinatePrecision    = 2
)

type OutdoorConditions struct {
	TemperatureC          *float64      `json:"temperature_c"`
	RelativeHumidity      *float64      `json:"relative_humidity"`
	PM2                   *float64      `json:"pm2"`
	PM10                  *float64      `json:"pm10"`
	AirQualityCategory    string        `json:"air_quality_category"`
	TemperatureObservedAt *string       `json:"temperature_observed_at"`
	HumidityObservedAt    *string       `json:"humidity_observed_at"`
	AirQualityObservedAt  *string       `json:"air_quality_observed_at"`
	DataQuality           string        `json:"data_quality"`
	FetchedAt             int64         `json:"fetched_at"`
	Sources               []AlertSource `json:"-"`
	TemperatureSources    []AlertSource `json:"-"`
	HumiditySources       []AlertSource `json:"-"`
	AirQualitySources     []AlertSource `json:"-"`
}

type OutdoorContextSource interface {
	Snapshot() (OutdoorConditions, bool)
}

type OutdoorContextMonitor interface {
	OutdoorContextSource
	Start(ctx context.Context, onUpdate func(initial bool))
}

type OutdoorContextRefresher interface {
	OutdoorContextSource
	EnsureFresh(ctx context.Context) (OutdoorConditions, bool)
}

type OutdoorProviderConfig struct {
	Location          string
	RefreshInterval   time.Duration
	MaxAge            time.Duration
	RequestTimeout    time.Duration
	PostcodeBaseURL   string
	WeatherBaseURL    string
	AirQualityBaseURL string
}

type privateOutdoorLocation string

func (privateOutdoorLocation) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[redacted]")
}

func (privateOutdoorLocation) MarshalJSON() ([]byte, error) {
	return json.Marshal("[redacted]")
}

type OpenMeteoOutdoorProvider struct {
	httpClient        *http.Client
	location          privateOutdoorLocation
	refreshInterval   time.Duration
	maxAge            time.Duration
	requestTimeout    time.Duration
	initialRetry      time.Duration
	postcodeBaseURL   string
	weatherBaseURL    string
	airQualityBaseURL string
	now               func() time.Time

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

func NewOpenMeteoOutdoorProvider(config OutdoorProviderConfig) *OpenMeteoOutdoorProvider {
	refreshInterval := config.RefreshInterval
	if refreshInterval < time.Minute {
		refreshInterval = defaultOutdoorRefreshInterval
	}
	maxAge := config.MaxAge
	if maxAge <= 0 {
		maxAge = defaultOutdoorMaxAge
	}
	minimumMaxAge := refreshInterval + outdoorRefreshGrace
	if maxAge < minimumMaxAge {
		maxAge = minimumMaxAge
	}
	requestTimeout := config.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultOutdoorRequestTimeout
	}
	initialRetry := defaultOutdoorInitialRetry
	if refreshInterval < initialRetry {
		initialRetry = refreshInterval
	}

	return &OpenMeteoOutdoorProvider{
		httpClient:        &http.Client{Timeout: requestTimeout},
		location:          privateOutdoorLocation(strings.TrimSpace(config.Location)),
		refreshInterval:   refreshInterval,
		maxAge:            maxAge,
		requestTimeout:    requestTimeout,
		initialRetry:      initialRetry,
		postcodeBaseURL:   baseURLOrDefault(config.PostcodeBaseURL, defaultPostcodeBaseURL),
		weatherBaseURL:    baseURLOrDefault(config.WeatherBaseURL, defaultWeatherBaseURL),
		airQualityBaseURL: baseURLOrDefault(config.AirQualityBaseURL, defaultAirQualityBaseURL),
		now:               time.Now,
	}
}

func (provider *OpenMeteoOutdoorProvider) Start(
	ctx context.Context,
	onUpdate func(initial bool),
) {
	provider.startOnce.Do(func() {
		log.Printf("outdoor context monitor started provider=open-meteo")
		workerStarted := make(chan struct{})
		go func() {
			close(workerStarted)
			initialNotified := false
			initialUsable, initialComplete := provider.refreshInitial(
				ctx,
				onUpdate,
				false,
				true,
			)
			initialNotified = initialUsable
			nextRefresh := provider.refreshInterval
			if !initialComplete {
				nextRefresh = provider.initialRetry
			}
			timer := time.NewTimer(nextRefresh)
			defer timer.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if initialComplete {
						initialComplete = provider.refresh(ctx, onUpdate)
					} else {
						initialUsable, initialComplete = provider.refreshInitial(
							ctx,
							onUpdate,
							true,
							!initialNotified,
						)
						initialNotified = initialNotified || initialUsable
					}
					nextRefresh = provider.refreshInterval
					if !initialComplete {
						nextRefresh = provider.initialRetry
					}
					timer.Reset(nextRefresh)
				}
			}
		}()
		select {
		case <-workerStarted:
		case <-ctx.Done():
		}
	})
}

func (provider *OpenMeteoOutdoorProvider) refreshInitial(
	parent context.Context,
	onUpdate func(initial bool),
	force bool,
	notifyFirst bool,
) (bool, bool) {
	log.Printf("outdoor context initial refresh started timeout=%s provider=open-meteo", provider.requestTimeout)
	conditions, changed, err := provider.fetchAndStore(parent, force)
	if err != nil {
		log.Printf("outdoor context initial refresh failed: %v", err)
		return false, false
	}
	log.Printf(
		"outdoor context initial refresh completed temperature_available=%t humidity_available=%t pm_available=%t quality=%s provider=open-meteo",
		conditions.TemperatureC != nil,
		conditions.RelativeHumidity != nil,
		conditions.PM2 != nil || conditions.PM10 != nil,
		conditions.AirQualityCategory,
	)
	if onUpdate != nil && (notifyFirst || changed) {
		onUpdate(notifyFirst)
	}
	return true, hasCompleteOutdoorData(conditions)
}

func (provider *OpenMeteoOutdoorProvider) Snapshot() (OutdoorConditions, bool) {
	provider.mu.RLock()
	conditions := provider.latest
	hasLatest := provider.hasLatest
	provider.mu.RUnlock()

	if !hasLatest || provider.currentTime().Sub(time.UnixMilli(conditions.FetchedAt)) > provider.maxAge {
		return OutdoorConditions{}, false
	}
	conditions.Sources = cloneAlertSources(conditions.Sources)
	conditions.TemperatureSources = cloneAlertSources(conditions.TemperatureSources)
	conditions.HumiditySources = cloneAlertSources(conditions.HumiditySources)
	conditions.AirQualitySources = cloneAlertSources(conditions.AirQualitySources)
	return conditions, true
}

func (provider *OpenMeteoOutdoorProvider) refresh(
	parent context.Context,
	onUpdate func(initial bool),
) bool {
	log.Printf("outdoor context refresh started timeout=%s provider=open-meteo", provider.requestTimeout)
	conditions, changed, err := provider.fetchAndStore(parent, true)
	if err != nil {
		log.Printf("outdoor context refresh failed: %v", err)
		return false
	}

	log.Printf(
		"outdoor context refreshed temperature_available=%t humidity_available=%t pm_available=%t quality=%s material_change=%t provider=open-meteo",
		conditions.TemperatureC != nil,
		conditions.RelativeHumidity != nil,
		conditions.PM2 != nil || conditions.PM10 != nil,
		conditions.AirQualityCategory,
		changed,
	)
	if onUpdate != nil {
		onUpdate(false)
	}
	return hasCompleteOutdoorData(conditions)
}

func (provider *OpenMeteoOutdoorProvider) EnsureFresh(parent context.Context) (OutdoorConditions, bool) {
	if conditions, ok := provider.Snapshot(); ok {
		return conditions, true
	}

	conditions, _, err := provider.fetchAndStore(parent, false)
	if err != nil {
		log.Printf("on-demand outdoor context refresh failed: %v", err)
		return OutdoorConditions{}, false
	}
	log.Printf("on-demand outdoor context refresh completed provider=open-meteo")
	return conditions, true
}

func (provider *OpenMeteoOutdoorProvider) fetchAndStore(
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
	provider.mu.RLock()
	previous := provider.latest
	hadLatest := provider.hasLatest
	provider.mu.RUnlock()
	previousWasFresh := hadLatest &&
		provider.currentTime().Sub(time.UnixMilli(previous.FetchedAt)) <= provider.maxAge

	ctx, cancel := context.WithTimeout(parent, provider.requestTimeout)
	defer cancel()

	conditions, err := provider.fetch(ctx)
	if err != nil {
		return OutdoorConditions{}, false, err
	}

	provider.mu.Lock()
	changed := hadLatest &&
		(!previousWasFresh || outdoorConditionsMateriallyChanged(previous, conditions))
	provider.latest = conditions
	provider.hasLatest = true
	provider.mu.Unlock()
	return conditions, changed, nil
}

func (provider *OpenMeteoOutdoorProvider) fetch(ctx context.Context) (OutdoorConditions, error) {
	if provider.location == "" {
		return OutdoorConditions{}, fmt.Errorf("outdoor context is not configured")
	}

	now := provider.currentTime().UTC()
	latitude, longitude, err := provider.resolveCoordinates(ctx)
	if err != nil {
		return OutdoorConditions{}, err
	}

	type weatherResult struct {
		temperature      *float64
		relativeHumidity *float64
		observedAt       string
		err              error
	}
	type airQualityResult struct {
		pm2        *float64
		pm10       *float64
		category   string
		observedAt string
		err        error
	}
	weatherResults := make(chan weatherResult, 1)
	airQualityResults := make(chan airQualityResult, 1)
	go func() {
		temperature, relativeHumidity, observedAt, fetchErr := provider.fetchCurrentWeather(
			ctx,
			now,
			latitude,
			longitude,
		)
		weatherResults <- weatherResult{
			temperature: temperature, relativeHumidity: relativeHumidity, observedAt: observedAt, err: fetchErr,
		}
	}()
	go func() {
		pm2, pm10, category, observedAt, fetchErr := provider.fetchCurrentAirQuality(ctx, now, latitude, longitude)
		airQualityResults <- airQualityResult{
			pm2: pm2, pm10: pm10, category: category, observedAt: observedAt, err: fetchErr,
		}
	}()

	weather := <-weatherResults
	airQuality := <-airQualityResults
	conditions := OutdoorConditions{
		AirQualityCategory: "unknown",
		DataQuality:        "forecast",
		FetchedAt:          now.UnixMilli(),
	}
	if weather.err != nil {
		log.Printf("current outdoor weather unavailable: %v", weather.err)
	} else {
		weatherSource := AlertSource{
			Title: "Open-Meteo",
			URL:   "https://open-meteo.com/en/docs",
		}
		if weather.temperature != nil {
			conditions.TemperatureC = weather.temperature
			conditions.TemperatureObservedAt = &weather.observedAt
			conditions.TemperatureSources = []AlertSource{weatherSource}
		}
		if weather.relativeHumidity != nil {
			conditions.RelativeHumidity = weather.relativeHumidity
			conditions.HumidityObservedAt = &weather.observedAt
			conditions.HumiditySources = []AlertSource{weatherSource}
		}
	}
	if airQuality.err != nil {
		log.Printf("current outdoor air quality unavailable: %v", airQuality.err)
	} else {
		conditions.PM2 = airQuality.pm2
		conditions.PM10 = airQuality.pm10
		conditions.AirQualityCategory = airQuality.category
		conditions.AirQualityObservedAt = &airQuality.observedAt
		conditions.AirQualitySources = []AlertSource{
			{Title: "Open-Meteo", URL: "https://open-meteo.com/en/docs/air-quality-api"},
			{Title: "CAMS ENSEMBLE", URL: "https://atmosphere.copernicus.eu/"},
		}
	}

	if !hasUsefulOutdoorData(conditions) {
		return OutdoorConditions{}, fmt.Errorf(
			"outdoor data unavailable: weather=%v; air_quality=%v",
			weather.err,
			airQuality.err,
		)
	}

	conditions.Sources = mergeAlertSources(
		conditions.TemperatureSources,
		conditions.HumiditySources,
		conditions.AirQualitySources,
	)
	return conditions, nil
}

func (provider *OpenMeteoOutdoorProvider) fetchCurrentWeather(
	ctx context.Context,
	now time.Time,
	latitude float64,
	longitude float64,
) (*float64, *float64, string, error) {
	endpoint, err := url.Parse(provider.weatherBaseURL + "/v1/forecast")
	if err != nil {
		return nil, nil, "", fmt.Errorf("build weather URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(latitude, 'f', outdoorCoordinatePrecision, 64))
	query.Set("longitude", strconv.FormatFloat(longitude, 'f', outdoorCoordinatePrecision, 64))
	query.Set("current", "temperature_2m,relative_humidity_2m")
	query.Set("timeformat", "unixtime")
	endpoint.RawQuery = query.Encode()

	var payload struct {
		Current struct {
			Time             int64    `json:"time"`
			TemperatureC     *float64 `json:"temperature_2m"`
			RelativeHumidity *float64 `json:"relative_humidity_2m"`
		} `json:"current"`
	}
	if err = provider.getJSON(ctx, endpoint.String(), &payload); err != nil {
		return nil, nil, "", fmt.Errorf("fetch current weather: %w", err)
	}
	temperature := boundedOutdoorMetric(payload.Current.TemperatureC, -60, 60)
	relativeHumidity := boundedOutdoorMetric(payload.Current.RelativeHumidity, 0, 100)
	if temperature == nil && relativeHumidity == nil {
		return nil, nil, "", fmt.Errorf("current weather values were invalid")
	}
	observedAt := normalizeOutdoorUnixTimestamp(payload.Current.Time, now)
	if observedAt == nil {
		return nil, nil, "", fmt.Errorf("current weather timestamp was stale")
	}
	return temperature, relativeHumidity, *observedAt, nil
}

func (provider *OpenMeteoOutdoorProvider) fetchCurrentAirQuality(
	ctx context.Context,
	now time.Time,
	latitude float64,
	longitude float64,
) (*float64, *float64, string, string, error) {
	endpoint, err := url.Parse(provider.airQualityBaseURL + "/v1/air-quality")
	if err != nil {
		return nil, nil, "unknown", "", fmt.Errorf("build air-quality URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(latitude, 'f', outdoorCoordinatePrecision, 64))
	query.Set("longitude", strconv.FormatFloat(longitude, 'f', outdoorCoordinatePrecision, 64))
	query.Set("current", "pm2_5,pm10,european_aqi")
	query.Set("domains", "cams_europe")
	query.Set("timeformat", "unixtime")
	endpoint.RawQuery = query.Encode()

	var payload struct {
		Current struct {
			Time        int64    `json:"time"`
			PM2         *float64 `json:"pm2_5"`
			PM10        *float64 `json:"pm10"`
			EuropeanAQI *float64 `json:"european_aqi"`
		} `json:"current"`
	}
	if err = provider.getJSON(ctx, endpoint.String(), &payload); err != nil {
		return nil, nil, "unknown", "", fmt.Errorf("fetch current air quality: %w", err)
	}

	observedAt := normalizeOutdoorUnixTimestamp(payload.Current.Time, now)
	if observedAt == nil {
		return nil, nil, "unknown", "", fmt.Errorf("current air-quality timestamp was stale")
	}
	pm2 := boundedOutdoorMetric(payload.Current.PM2, 0, 2000)
	pm10 := boundedOutdoorMetric(payload.Current.PM10, 0, 2000)
	category := outdoorCategoryFromEuropeanAQI(payload.Current.EuropeanAQI)
	if pm2 == nil && pm10 == nil && category == "unknown" {
		return nil, nil, "unknown", "", fmt.Errorf("current air-quality values were unavailable")
	}
	return pm2, pm10, category, *observedAt, nil
}

func (provider *OpenMeteoOutdoorProvider) resolveCoordinates(ctx context.Context) (float64, float64, error) {
	provider.coordinatesMu.Lock()
	defer provider.coordinatesMu.Unlock()
	if provider.hasCoordinates {
		return provider.latitude, provider.longitude, nil
	}

	postcode := strings.ReplaceAll(strings.ToUpper(string(provider.location)), " ", "")
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

func (provider *OpenMeteoOutdoorProvider) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request failed")
	}
	request.Header.Set("Accept", "application/json")

	response, err := provider.httpClient.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return fmt.Errorf("request failed")
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return fmt.Errorf("decode response failed")
	}
	return nil
}

func cloneAlertSources(sources []AlertSource) []AlertSource {
	output := make([]AlertSource, len(sources))
	copy(output, sources)
	return output
}

func mergeAlertSources(sourceGroups ...[]AlertSource) []AlertSource {
	var merged []AlertSource
	seen := make(map[string]struct{})
	for _, sources := range sourceGroups {
		for _, source := range sources {
			key := source.Title + "\x00" + source.URL
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, source)
		}
	}
	return merged
}

func outdoorSourcesForTopic(conditions OutdoorConditions, topic string) []AlertSource {
	var sources []AlertSource
	switch topic {
	case "temperature":
		sources = conditions.TemperatureSources
	case "humidity":
		sources = conditions.HumiditySources
	case "air_quality":
		sources = conditions.AirQualitySources
	}
	if len(sources) > 0 {
		return cloneAlertSources(sources)
	}
	if len(conditions.TemperatureSources) > 0 || len(conditions.HumiditySources) > 0 ||
		len(conditions.AirQualitySources) > 0 {
		return nil
	}
	return cloneAlertSources(conditions.Sources)
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

func normalizeOutdoorUnixTimestamp(timestamp int64, now time.Time) *string {
	if timestamp <= 0 {
		return nil
	}
	value := time.Unix(timestamp, 0).UTC().Format(time.RFC3339)
	return normalizeOutdoorObservedAt(&value, now)
}

func (provider *OpenMeteoOutdoorProvider) currentTime() time.Time {
	if provider.now != nil {
		return provider.now()
	}
	return time.Now()
}

func baseURLOrDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return strings.TrimRight(trimmed, "/")
	}
	return fallback
}

func hasUsefulOutdoorData(conditions OutdoorConditions) bool {
	return conditions.TemperatureC != nil || conditions.RelativeHumidity != nil ||
		conditions.PM2 != nil || conditions.PM10 != nil ||
		conditions.AirQualityCategory != "unknown"
}

func hasCompleteOutdoorData(conditions OutdoorConditions) bool {
	return conditions.TemperatureC != nil && conditions.RelativeHumidity != nil &&
		conditions.PM2 != nil && conditions.PM10 != nil
}

func outdoorCategoryFromEuropeanAQI(value *float64) string {
	aqi := boundedOutdoorMetric(value, 0, 1000)
	if aqi == nil {
		return "unknown"
	}
	switch {
	case *aqi <= 20:
		return "good"
	case *aqi <= 40:
		return "fair"
	case *aqi <= 60:
		return "moderate"
	case *aqi <= 80:
		return "poor"
	case *aqi <= 100:
		return "very_poor"
	default:
		return "extremely_poor"
	}
}

func outdoorConditionsMateriallyChanged(previous, current OutdoorConditions) bool {
	return nullableMetricChanged(previous.TemperatureC, current.TemperatureC, 2) ||
		nullableMetricChanged(previous.RelativeHumidity, current.RelativeHumidity, 8) ||
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
