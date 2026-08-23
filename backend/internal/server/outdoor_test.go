package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var outdoorTestNow = time.Date(2026, time.July, 12, 12, 34, 0, 0, time.UTC)

func TestOutdoorProviderFetchesDeterministicCurrentConditions(t *testing.T) {
	var postcodeRequests atomic.Int32
	var weatherRequests atomic.Int32
	var airQualityRequests atomic.Int32
	server := newOutdoorTestServer(t, outdoorTestServerOptions{
		onPostcode: func() { postcodeRequests.Add(1) },
		onWeather:  func() { weatherRequests.Add(1) },
		onAir:      func() { airQualityRequests.Add(1) },
	})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	conditions, err := provider.fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch outdoor conditions: %v", err)
	}

	if conditions.TemperatureC == nil || *conditions.TemperatureC != 27.9 {
		t.Fatalf("expected 27.9°C temperature, got %v", conditions.TemperatureC)
	}
	if conditions.RelativeHumidity == nil || *conditions.RelativeHumidity != 68 {
		t.Fatalf("expected 68%% relative humidity, got %v", conditions.RelativeHumidity)
	}
	if conditions.PM2 == nil || *conditions.PM2 != 6.8 {
		t.Fatalf("expected PM2.5 6.8, got %v", conditions.PM2)
	}
	if conditions.PM10 == nil || *conditions.PM10 != 11.5 {
		t.Fatalf("expected PM10 11.5, got %v", conditions.PM10)
	}
	if conditions.AirQualityCategory != "fair" {
		t.Fatalf("expected fair European AQI, got %q", conditions.AirQualityCategory)
	}
	if conditions.TemperatureObservedAt == nil || *conditions.TemperatureObservedAt != "2026-07-12T12:30:00Z" {
		t.Fatalf("expected current temperature timestamp, got %v", conditions.TemperatureObservedAt)
	}
	if conditions.HumidityObservedAt == nil || *conditions.HumidityObservedAt != "2026-07-12T12:30:00Z" {
		t.Fatalf("expected current humidity timestamp, got %v", conditions.HumidityObservedAt)
	}
	if conditions.AirQualityObservedAt == nil || *conditions.AirQualityObservedAt != "2026-07-12T12:30:00Z" {
		t.Fatalf("expected current air-quality timestamp, got %v", conditions.AirQualityObservedAt)
	}
	if conditions.DataQuality != "forecast" {
		t.Fatalf("expected modelled forecast quality, got %q", conditions.DataQuality)
	}
	for _, title := range []string{"Open-Meteo", "CAMS ENSEMBLE"} {
		if !containsSourceTitle(conditions.Sources, title) {
			t.Fatalf("expected %s attribution, got %#v", title, conditions.Sources)
		}
	}
	if len(conditions.HumiditySources) != 1 || conditions.HumiditySources[0].Title != "Open-Meteo" {
		t.Fatalf("expected Open-Meteo humidity attribution, got %#v", conditions.HumiditySources)
	}
	if postcodeRequests.Load() != 1 || weatherRequests.Load() != 1 || airQualityRequests.Load() != 1 {
		t.Fatalf(
			"expected one request per endpoint, got postcode=%d weather=%d air_quality=%d",
			postcodeRequests.Load(),
			weatherRequests.Load(),
			airQualityRequests.Load(),
		)
	}

	publicPayload, err := json.Marshal(conditions)
	if err != nil {
		t.Fatalf("marshal public conditions: %v", err)
	}
	for _, privateIdentifier := range []string{
		"TEST 1AA",
		"TEST1AA",
		"51.410159",
		"-0.838339",
		"51.41",
		"-0.84",
	} {
		if strings.Contains(strings.ToUpper(string(publicPayload)), strings.ToUpper(privateIdentifier)) {
			t.Fatal("private location data leaked into the public outdoor payload")
		}
	}
}

func TestOutdoorProviderCachesConditionsAndCoordinates(t *testing.T) {
	var postcodeRequests atomic.Int32
	var weatherRequests atomic.Int32
	var airQualityRequests atomic.Int32
	server := newOutdoorTestServer(t, outdoorTestServerOptions{
		onPostcode: func() { postcodeRequests.Add(1) },
		onWeather:  func() { weatherRequests.Add(1) },
		onAir:      func() { airQualityRequests.Add(1) },
	})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	if _, ok := provider.EnsureFresh(context.Background()); !ok {
		t.Fatal("expected first on-demand refresh to populate cache")
	}
	if _, ok := provider.EnsureFresh(context.Background()); !ok {
		t.Fatal("expected second on-demand refresh to use cache")
	}
	if _, err := provider.fetch(context.Background()); err != nil {
		t.Fatalf("forced fetch: %v", err)
	}

	if postcodeRequests.Load() != 1 {
		t.Fatalf("expected coordinates to be cached, got %d postcode requests", postcodeRequests.Load())
	}
	if weatherRequests.Load() != 2 || airQualityRequests.Load() != 2 {
		t.Fatalf(
			"expected cached refresh plus forced component calls, got weather=%d air_quality=%d",
			weatherRequests.Load(),
			airQualityRequests.Load(),
		)
	}
}

func TestOutdoorProviderDoesNotTreatInitialCacheFillAsMaterialChange(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	if _, changed, err := provider.fetchAndStore(context.Background(), true); err != nil {
		t.Fatalf("initial outdoor refresh: %v", err)
	} else if changed {
		t.Fatal("expected initial cache fill not to enqueue a duplicate insight analysis")
	}
}

func TestOutdoorProviderSignalsRecoveryFromStaleCacheWithSameValues(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	currentTime := outdoorTestNow
	provider.now = func() time.Time { return currentTime }
	provider.maxAge = time.Hour
	if _, ok := provider.EnsureFresh(context.Background()); !ok {
		t.Fatal("expected initial outdoor context")
	}
	currentTime = currentTime.Add(90 * time.Minute)
	if _, ok := provider.Snapshot(); ok {
		t.Fatal("expected cached outdoor context to be stale")
	}

	updates := 0
	provider.refresh(context.Background(), func(initial bool) {
		if initial {
			t.Error("expected stale recovery to be a regular outdoor update")
		}
		updates++
	})
	if updates != 1 {
		t.Fatalf("expected stale-to-fresh recovery callback, got %d", updates)
	}
	if _, ok := provider.Snapshot(); !ok {
		t.Fatal("expected refreshed outdoor context to be usable")
	}
}

func TestOutdoorProviderSignalsEverySuccessfulRegularRefresh(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	if _, ok := provider.EnsureFresh(context.Background()); !ok {
		t.Fatal("expected initial outdoor context")
	}

	updates := 0
	provider.refresh(context.Background(), func(initial bool) {
		if initial {
			t.Error("expected a regular outdoor update")
		}
		updates++
	})
	if updates != 1 {
		t.Fatalf("expected every successful refresh to trigger insight re-evaluation, got %d", updates)
	}
}

func TestOutdoorProviderStartCoalescesInitialAndOnDemandRefresh(t *testing.T) {
	var weatherRequests atomic.Int32
	var airQualityRequests atomic.Int32
	var callbacks atomic.Int32
	server := newOutdoorTestServer(t, outdoorTestServerOptions{
		onWeather: func() { weatherRequests.Add(1) },
		onAir:     func() { airQualityRequests.Add(1) },
	})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.Start(ctx, func(initial bool) {
		if !initial {
			t.Error("expected the startup callback to be marked initial")
		}
		callbacks.Add(1)
	})
	if _, ok := provider.EnsureFresh(ctx); !ok {
		t.Fatal("expected initial outdoor refresh to populate cache")
	}

	if weatherRequests.Load() != 1 || airQualityRequests.Load() != 1 {
		t.Fatalf(
			"expected startup and on-demand refreshes to coalesce, got weather=%d air_quality=%d",
			weatherRequests.Load(),
			airQualityRequests.Load(),
		)
	}
	deadline := time.Now().Add(time.Second)
	for callbacks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if callbacks.Load() != 1 {
		t.Fatalf("expected one initial-ready callback, got %d", callbacks.Load())
	}
}

func TestOutdoorProviderRetriesFailedInitialRefreshAndSignalsReadiness(t *testing.T) {
	var weatherRequests atomic.Int32
	var airQualityRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/postcodes/TEST1AA":
			_, _ = response.Write([]byte(`{"status":200,"result":{"latitude":51.410159,"longitude":-0.838339}}`))
		case "/v1/forecast":
			if weatherRequests.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"temperature_2m":27.9,"relative_humidity_2m":68}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
			)
		case "/v1/air-quality":
			if airQualityRequests.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"pm2_5":6.8,"pm10":11.5,"european_aqi":28}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
			)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	provider.initialRetry = 10 * time.Millisecond
	updates := make(chan bool, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.Start(ctx, func(initial bool) { updates <- initial })

	select {
	case initial := <-updates:
		if !initial {
			t.Fatal("expected retry success to signal initial readiness")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial outdoor retry")
	}
	if weatherRequests.Load() != 2 || airQualityRequests.Load() != 2 {
		t.Fatalf(
			"expected each failed component to retry once, got weather=%d air_quality=%d",
			weatherRequests.Load(),
			airQualityRequests.Load(),
		)
	}
}

func TestOutdoorProviderRetriesMissingInitialComponent(t *testing.T) {
	var weatherRequests atomic.Int32
	var airQualityRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/postcodes/TEST1AA":
			_, _ = response.Write([]byte(`{"status":200,"result":{"latitude":51.410159,"longitude":-0.838339}}`))
		case "/v1/forecast":
			weatherRequests.Add(1)
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"temperature_2m":27.9,"relative_humidity_2m":68}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
			)
		case "/v1/air-quality":
			if airQualityRequests.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"pm2_5":6.8,"pm10":11.5,"european_aqi":28}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
			)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	provider.initialRetry = 10 * time.Millisecond
	updates := make(chan bool, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider.Start(ctx, func(initial bool) { updates <- initial })

	for update := 0; update < 2; update++ {
		select {
		case initial := <-updates:
			if initial != (update == 0) {
				t.Fatalf("unexpected initial marker for update %d: %t", update, initial)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for missing outdoor component to recover")
		}
	}
	conditions, ok := provider.Snapshot()
	if !ok || !hasCompleteOutdoorData(conditions) {
		t.Fatalf("expected complete outdoor context after component retry, got %#v", conditions)
	}
	if weatherRequests.Load() != 2 || airQualityRequests.Load() != 2 {
		t.Fatalf(
			"expected partial refresh to retry both components once, got weather=%d air_quality=%d",
			weatherRequests.Load(),
			airQualityRequests.Load(),
		)
	}
}

func TestOutdoorProviderKeepsWeatherWhenAirQualityFails(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{airQualityStatus: http.StatusBadGateway})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	conditions, err := provider.fetch(context.Background())
	if err != nil {
		t.Fatalf("expected weather-only context to remain useful: %v", err)
	}
	if conditions.TemperatureC == nil || conditions.RelativeHumidity == nil ||
		conditions.PM2 != nil || conditions.PM10 != nil {
		t.Fatalf("expected only weather values, got %#v", conditions)
	}
	if conditions.AirQualityCategory != "unknown" {
		t.Fatalf("expected unknown air quality, got %q", conditions.AirQualityCategory)
	}
	if len(conditions.Sources) != 1 || conditions.Sources[0].Title != "Open-Meteo" {
		t.Fatalf("expected only Open-Meteo attribution, got %#v", conditions.Sources)
	}
}

func TestOutdoorProviderKeepsOtherMetricsWhenHumidityIsUnavailable(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{omitWeatherHumidity: true})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	conditions, err := provider.fetch(context.Background())
	if err != nil {
		t.Fatalf("expected partial weather context to remain useful: %v", err)
	}
	if conditions.TemperatureC == nil || conditions.PM2 == nil || conditions.PM10 == nil {
		t.Fatalf("expected temperature and air-quality values, got %#v", conditions)
	}
	if conditions.RelativeHumidity != nil || conditions.HumidityObservedAt != nil ||
		len(conditions.HumiditySources) != 0 {
		t.Fatalf("expected unavailable humidity to remain absent, got %#v", conditions)
	}
	if hasCompleteOutdoorData(conditions) {
		t.Fatal("expected missing humidity to mark outdoor context incomplete for retry")
	}
}

func TestOutdoorProviderKeepsAirQualityWhenWeatherFails(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{weatherStatus: http.StatusBadGateway})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	conditions, err := provider.fetch(context.Background())
	if err != nil {
		t.Fatalf("expected air-quality-only context to remain useful: %v", err)
	}
	if conditions.TemperatureC != nil || conditions.RelativeHumidity != nil ||
		conditions.PM2 == nil || conditions.PM10 == nil {
		t.Fatalf("expected only air-quality values, got %#v", conditions)
	}
	if conditions.AirQualityCategory != "fair" || conditions.AirQualityObservedAt == nil {
		t.Fatalf("expected current fair air quality, got %#v", conditions)
	}
	if len(conditions.AirQualitySources) != 2 ||
		!containsSourceTitle(conditions.AirQualitySources, "CAMS ENSEMBLE") {
		t.Fatalf("expected Open-Meteo and CAMS attribution, got %#v", conditions.AirQualitySources)
	}
	if len(conditions.TemperatureSources) != 0 {
		t.Fatalf("expected no weather attribution, got %#v", conditions.TemperatureSources)
	}
}

func TestOutdoorProviderFailsWhenBothComponentsFail(t *testing.T) {
	server := newOutdoorTestServer(t, outdoorTestServerOptions{
		weatherStatus:    http.StatusBadGateway,
		airQualityStatus: http.StatusServiceUnavailable,
	})
	defer server.Close()

	provider := newTestOutdoorProvider(server.URL)
	_, err := provider.fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "outdoor data unavailable") {
		t.Fatalf("expected combined outdoor failure, got %v", err)
	}
}

func TestOutdoorProviderSanitizesSensitiveTransportErrors(t *testing.T) {
	const privateLocation = "PRIVATE-OUTDOOR-LOCATION"
	const privateLatitude = 12.345678
	const privateLongitude = -98.765432

	provider := NewOpenMeteoOutdoorProvider(OutdoorProviderConfig{
		Location:          privateLocation,
		PostcodeBaseURL:   "https://location.invalid",
		WeatherBaseURL:    "https://weather.invalid",
		AirQualityBaseURL: "https://air.invalid",
		RequestTimeout:    time.Second,
	})
	provider.now = func() time.Time { return outdoorTestNow }
	provider.httpClient = &http.Client{Transport: outdoorRoundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("synthetic transport failure for %s", request.URL.String())
		},
	)}
	formattedLocation := fmt.Sprintf("%s %v %+v %#v %q", provider.location, provider.location, provider.location, provider.location, provider.location)
	if strings.Contains(formattedLocation, privateLocation) {
		t.Fatal("private location formatting was not redacted")
	}
	marshaledLocation, err := json.Marshal(provider.location)
	if err != nil {
		t.Fatal("marshal private location")
	}
	if strings.Contains(string(marshaledLocation), privateLocation) {
		t.Fatal("private location JSON was not redacted")
	}
	var capturedLogs bytes.Buffer
	originalLogWriter := log.Writer()
	originalLogFlags := log.Flags()
	log.SetOutput(&capturedLogs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalLogWriter)
		log.SetFlags(originalLogFlags)
	})

	var target map[string]any
	err = provider.getJSON(
		context.Background(),
		provider.postcodeBaseURL+"/postcodes/"+privateLocation,
		&target,
	)
	if err == nil {
		t.Fatal("expected resolver transport failure")
	}
	if strings.Contains(err.Error(), privateLocation) || strings.Contains(err.Error(), provider.postcodeBaseURL) {
		t.Fatal("transport error exposed the private location URL")
	}
	provider.httpClient = &http.Client{Transport: outdoorRoundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: outdoorErrorReadCloser{
					err: fmt.Errorf("synthetic response failure for %s", request.URL.String()),
				},
				Header: make(http.Header),
			}, nil
		},
	)}
	if _, ok := provider.EnsureFresh(context.Background()); ok {
		t.Fatal("expected location-resolution refresh to fail")
	}

	provider.coordinatesMu.Lock()
	provider.latitude = privateLatitude
	provider.longitude = privateLongitude
	provider.hasCoordinates = true
	provider.coordinatesMu.Unlock()

	if _, ok := provider.EnsureFresh(context.Background()); ok {
		t.Fatal("expected outdoor refresh to fail")
	}
	for _, sensitiveValue := range []string{
		privateLocation,
		"12.345678",
		"-98.765432",
		"12.35",
		"-98.77",
		provider.weatherBaseURL,
		provider.airQualityBaseURL,
	} {
		if strings.Contains(capturedLogs.String(), sensitiveValue) {
			t.Fatal("outdoor refresh log exposed private request data")
		}
	}
}

func TestOutdoorProviderRejectsStaleAirQuality(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/air-quality" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"current":{"time":1783828800,"pm2_5":6.8,"pm10":11.5,"european_aqi":28}
		}`))
	}))
	defer server.Close()

	provider := NewOpenMeteoOutdoorProvider(OutdoorProviderConfig{
		Location:          "TEST 1AA",
		AirQualityBaseURL: server.URL,
		RequestTimeout:    time.Second,
	})
	_, _, _, _, err := provider.fetchCurrentAirQuality(
		context.Background(),
		outdoorTestNow,
		51.410159,
		-0.838339,
	)
	if err == nil || !strings.Contains(err.Error(), "timestamp was stale") {
		t.Fatalf("expected stale air-quality timestamp error, got %v", err)
	}
}

func TestOutdoorProviderClampsMaxAgePastRefreshInterval(t *testing.T) {
	provider := NewOpenMeteoOutdoorProvider(OutdoorProviderConfig{
		Location:        "TEST 1AA",
		RefreshInterval: 2 * time.Hour,
		MaxAge:          2*time.Hour + 5*time.Minute,
	})
	minimumMaxAge := provider.refreshInterval + outdoorRefreshGrace
	if provider.maxAge < minimumMaxAge {
		t.Fatalf("expected max age with refresh grace, got minimum=%s max_age=%s", minimumMaxAge, provider.maxAge)
	}
}

func TestOutdoorSourcesMatchInsightTopic(t *testing.T) {
	openMeteo := AlertSource{Title: "Open-Meteo", URL: "https://open-meteo.com/en/docs"}
	cams := AlertSource{Title: "CAMS ENSEMBLE", URL: "https://atmosphere.copernicus.eu/"}
	conditions := OutdoorConditions{
		Sources:            []AlertSource{openMeteo, cams},
		TemperatureSources: []AlertSource{openMeteo},
		HumiditySources:    []AlertSource{openMeteo},
		AirQualitySources:  []AlertSource{openMeteo, cams},
	}

	temperatureSources := outdoorSourcesForTopic(conditions, "temperature")
	if len(temperatureSources) != 1 || temperatureSources[0].Title != "Open-Meteo" {
		t.Fatalf("expected weather attribution only, got %#v", temperatureSources)
	}
	humiditySources := outdoorSourcesForTopic(conditions, "humidity")
	if len(humiditySources) != 1 || humiditySources[0].Title != "Open-Meteo" {
		t.Fatalf("expected humidity attribution only, got %#v", humiditySources)
	}
	airQualitySources := outdoorSourcesForTopic(conditions, "air_quality")
	if len(airQualitySources) != 2 || !containsSourceTitle(airQualitySources, "CAMS ENSEMBLE") {
		t.Fatalf("expected Open-Meteo and CAMS attribution, got %#v", airQualitySources)
	}
	if generalSources := outdoorSourcesForTopic(conditions, "general"); len(generalSources) != 0 {
		t.Fatalf("expected no outdoor attribution for general insight, got %#v", generalSources)
	}
}

func TestOutdoorCategoryFromEuropeanAQI(t *testing.T) {
	tests := []struct {
		name     string
		value    *float64
		expected string
	}{
		{name: "missing", value: nil, expected: "unknown"},
		{name: "good", value: floatPointer(20), expected: "good"},
		{name: "fair", value: floatPointer(21), expected: "fair"},
		{name: "moderate", value: floatPointer(41), expected: "moderate"},
		{name: "poor", value: floatPointer(61), expected: "poor"},
		{name: "very poor", value: floatPointer(81), expected: "very_poor"},
		{name: "extremely poor", value: floatPointer(101), expected: "extremely_poor"},
		{name: "invalid", value: floatPointer(-1), expected: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if category := outdoorCategoryFromEuropeanAQI(test.value); category != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, category)
			}
		})
	}
}

func TestNormalizeOutdoorObservedAtAllowsOnlyCurrentWindow(t *testing.T) {
	withinWindow := "2026-07-12T13:00:00+01:00"
	if normalized := normalizeOutdoorObservedAt(&withinWindow, outdoorTestNow); normalized == nil || *normalized != "2026-07-12T12:00:00Z" {
		t.Fatalf("expected recent timestamp to normalize, got %v", normalized)
	}

	outsideWindow := "2026-07-12T09:59:59Z"
	if normalized := normalizeOutdoorObservedAt(&outsideWindow, outdoorTestNow); normalized != nil {
		t.Fatalf("expected stale timestamp to be rejected, got %q", *normalized)
	}
}

func TestOutdoorConditionsMaterialChangeThresholds(t *testing.T) {
	previousTemperature := 12.0
	minorTemperatureChange := 13.9
	materialTemperatureChange := 14.0
	previousHumidity := 45.0
	minorHumidityChange := 52.9
	materialHumidityChange := 53.0
	base := OutdoorConditions{
		TemperatureC:       &previousTemperature,
		RelativeHumidity:   &previousHumidity,
		AirQualityCategory: "good",
	}

	if outdoorConditionsMateriallyChanged(base, OutdoorConditions{
		TemperatureC:       &minorTemperatureChange,
		RelativeHumidity:   &previousHumidity,
		AirQualityCategory: "good",
	}) {
		t.Fatal("expected minor temperature change not to trigger insights")
	}
	if !outdoorConditionsMateriallyChanged(base, OutdoorConditions{
		TemperatureC:       &materialTemperatureChange,
		RelativeHumidity:   &previousHumidity,
		AirQualityCategory: "good",
	}) {
		t.Fatal("expected two-degree temperature change to trigger insights")
	}
	if outdoorConditionsMateriallyChanged(base, OutdoorConditions{
		TemperatureC:       &previousTemperature,
		RelativeHumidity:   &minorHumidityChange,
		AirQualityCategory: "good",
	}) {
		t.Fatal("expected minor humidity change not to trigger insights")
	}
	if !outdoorConditionsMateriallyChanged(base, OutdoorConditions{
		TemperatureC:       &previousTemperature,
		RelativeHumidity:   &materialHumidityChange,
		AirQualityCategory: "good",
	}) {
		t.Fatal("expected eight-point humidity change to trigger insights")
	}
}

type outdoorTestServerOptions struct {
	onPostcode          func()
	onWeather           func()
	onAir               func()
	weatherStatus       int
	airQualityStatus    int
	omitWeatherHumidity bool
}

func newOutdoorTestServer(t *testing.T, options outdoorTestServerOptions) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/postcodes/TEST1AA":
			if options.onPostcode != nil {
				options.onPostcode()
			}
			_, _ = response.Write([]byte(`{"status":200,"result":{"latitude":51.410159,"longitude":-0.838339}}`))
		case "/v1/forecast":
			if options.onWeather != nil {
				options.onWeather()
			}
			assertOutdoorLocationQuery(t, request)
			if request.URL.Query().Get("current") != "temperature_2m,relative_humidity_2m" ||
				request.URL.Query().Get("timeformat") != "unixtime" {
				t.Error("unexpected weather query")
			}
			if options.weatherStatus != 0 {
				response.WriteHeader(options.weatherStatus)
				return
			}
			humidityField := `,"relative_humidity_2m":68`
			if options.omitWeatherHumidity {
				humidityField = ""
			}
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"temperature_2m":27.9%s}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
				humidityField,
			)
		case "/v1/air-quality":
			if options.onAir != nil {
				options.onAir()
			}
			assertOutdoorLocationQuery(t, request)
			if request.URL.Query().Get("current") != "pm2_5,pm10,european_aqi" ||
				request.URL.Query().Get("domains") != "cams_europe" ||
				request.URL.Query().Get("timeformat") != "unixtime" {
				t.Error("unexpected air-quality query")
			}
			if options.airQualityStatus != 0 {
				response.WriteHeader(options.airQualityStatus)
				return
			}
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"pm2_5":6.8,"pm10":11.5,"european_aqi":28}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
			)
		default:
			http.NotFound(response, request)
		}
	}))
}

func assertOutdoorLocationQuery(t *testing.T, request *http.Request) {
	t.Helper()
	if request.URL.Query().Get("latitude") != "51.41" ||
		request.URL.Query().Get("longitude") != "-0.84" {
		t.Error("unexpected outdoor coordinates")
	}
	if strings.Contains(strings.ToUpper(request.URL.RawQuery), "TEST1AA") {
		t.Error("postcode leaked into data-provider query")
	}
}

type outdoorRoundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip outdoorRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type outdoorErrorReadCloser struct {
	err error
}

func (reader outdoorErrorReadCloser) Read(_ []byte) (int, error) {
	return 0, reader.err
}

func (outdoorErrorReadCloser) Close() error {
	return nil
}

func newTestOutdoorProvider(baseURL string) *OpenMeteoOutdoorProvider {
	provider := NewOpenMeteoOutdoorProvider(OutdoorProviderConfig{
		Location:          "TEST 1AA",
		PostcodeBaseURL:   baseURL,
		WeatherBaseURL:    baseURL,
		AirQualityBaseURL: baseURL,
		RequestTimeout:    time.Second,
	})
	provider.now = func() time.Time { return outdoorTestNow }
	return provider
}

func containsSourceTitle(sources []AlertSource, title string) bool {
	for _, source := range sources {
		if source.Title == title {
			return true
		}
	}
	return false
}

func floatPointer(value float64) *float64 {
	return &value
}
