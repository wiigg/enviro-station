package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var outdoorTestNow = time.Date(2026, time.July, 12, 12, 34, 0, 0, time.UTC)

func TestOutdoorProviderCachesOnDemandRefresh(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(response, request)
			return
		}
		requestCount++
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
          "output": [
            {"type":"web_search_call","action":{"sources":[{"url":"https://www.metoffice.gov.uk/weather"}]}},
			{"type":"message","content":[{"type":"output_text","text":"{\"temperature_c\":null,\"pm2\":null,\"pm10\":null,\"air_quality_category\":\"good\",\"observed_at\":\"2026-07-12T12:00:00Z\",\"data_quality\":\"forecast\"}"}]}
          ]
        }`))
	}))
	defer server.Close()

	provider := NewOpenAIOutdoorProvider(OutdoorSearchConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		PostcodeBaseURL: server.URL,
		WeatherBaseURL:  server.URL,
		Location:        "TEST 1AA",
		RequestTimeout:  time.Second,
	})
	provider.now = func() time.Time { return outdoorTestNow }
	if _, ok := provider.EnsureFresh(context.Background()); !ok {
		t.Fatal("expected first on-demand refresh to populate cache")
	}
	if _, ok := provider.EnsureFresh(context.Background()); !ok {
		t.Fatal("expected second on-demand refresh to use cache")
	}
	if requestCount != 1 {
		t.Fatalf("expected one web request, got %d", requestCount)
	}
}

func TestOutdoorProviderEnforcesDailyRequestLimit(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(response, request)
			return
		}
		requestCount++
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
          "output": [
            {"type":"web_search_call","action":{"sources":[{"url":"https://www.metoffice.gov.uk/weather"}]}},
			{"type":"message","content":[{"type":"output_text","text":"{\"temperature_c\":null,\"pm2\":null,\"pm10\":null,\"air_quality_category\":\"good\",\"observed_at\":\"2026-07-12T12:00:00Z\",\"data_quality\":\"forecast\"}"}]}
          ]
        }`))
	}))
	defer server.Close()

	provider := NewOpenAIOutdoorProvider(OutdoorSearchConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		PostcodeBaseURL: server.URL,
		WeatherBaseURL:  server.URL,
		Location:        "TEST 1AA",
		RequestTimeout:  time.Second,
		DailyLimit:      1,
	})
	provider.now = func() time.Time { return outdoorTestNow }
	if _, err := provider.fetch(context.Background()); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if _, err := provider.fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "daily request limit") {
		t.Fatalf("expected daily request limit error, got %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one OpenAI request, got %d", requestCount)
	}
}

func TestOutdoorProviderKeepsLocationPrivateAndSanitizesSources(t *testing.T) {
	const privateLocation = "TEST 1AA"
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(response, request)
			return
		}
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		requestBody = string(payload)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
          "output": [
            {
              "type": "web_search_call",
              "action": {
                "sources": [
                  {"type":"url","url":"https://www.metoffice.gov.uk/weather/forecast/TEST-1AA?location=TEST+1AA"},
                  {"type":"url","url":"https://weather.metoffice.gov.uk/forecast/TEST-1AA"},
                  {"type":"url","url":"https://uk-air.defra.gov.uk/latest/current"}
                ]
              }
            },
            {
              "type": "message",
              "content": [{
                "type": "output_text",
				"text": "{\"temperature_c\":null,\"pm2\":4.0,\"pm10\":9.0,\"air_quality_category\":\"good\",\"observed_at\":\"2026-07-12T12:00:00Z\",\"data_quality\":\"observed\"}"
              }]
            }
          ]
        }`))
	}))
	defer server.Close()

	provider := NewOpenAIOutdoorProvider(OutdoorSearchConfig{
		APIKey:          "test-key",
		Model:           "test-model",
		ReasoningEffort: "medium",
		BaseURL:         server.URL,
		PostcodeBaseURL: server.URL,
		WeatherBaseURL:  server.URL,
		Location:        privateLocation,
		RequestTimeout:  time.Second,
	})
	provider.now = func() time.Time { return outdoorTestNow }
	conditions, err := provider.fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch outdoor conditions: %v", err)
	}
	if !strings.Contains(requestBody, privateLocation) {
		t.Fatal("expected private location in server-side OpenAI request")
	}
	for _, expected := range []string{
		`"model":"test-model"`,
		`"effort":"medium"`,
		`"type":"web_search"`,
		`"external_web_access":true`,
		`"tool_choice":"required"`,
		`"max_tool_calls":1`,
		`"max_output_tokens":800`,
		"You must use web search now",
		"The current instant is 2026-07-12T12:34:00Z UTC",
		"current UK local instant is Sunday 12 July 2026 13:34 BST (UTC+01:00)",
		"Temperature is retrieved separately by a deterministic weather service",
		"set temperature_c to null",
		"Do not substitute daily values",
		"do not provide general health advice",
	} {
		if !strings.Contains(requestBody, expected) {
			t.Fatalf("expected web-search request instruction %q", expected)
		}
	}
	if len(conditions.Sources) != 2 {
		t.Fatalf("expected duplicate providers to collapse to two sources, got %d", len(conditions.Sources))
	}
	if conditions.Sources[0].URL != "https://www.metoffice.gov.uk/" {
		t.Fatalf("expected location-bearing source path to be removed, got %q", conditions.Sources[0].URL)
	}

	publicPayload, err := json.Marshal(conditions)
	if err != nil {
		t.Fatalf("marshal public conditions: %v", err)
	}
	if strings.Contains(strings.ToUpper(string(publicPayload)), privateLocation) ||
		strings.Contains(strings.ToLower(string(publicPayload)), "test1aa") {
		t.Fatalf("private location leaked into public payload: %s", publicPayload)
	}
}

func TestOutdoorProviderUsesDeterministicTemperatureInsteadOfSearchValue(t *testing.T) {
	postcodeRequests := 0
	weatherRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/responses":
			_, _ = response.Write([]byte(`{
              "output": [
                {"type":"web_search_call","action":{"sources":[{"url":"https://uk-air.defra.gov.uk/latest/current"}]}},
                {"type":"message","content":[{"type":"output_text","text":"{\"temperature_c\":16,\"pm2\":4,\"pm10\":9,\"air_quality_category\":\"good\",\"observed_at\":\"2026-07-12T12:00:00Z\",\"data_quality\":\"observed\"}"}]}
              ]
            }`))
		case "/postcodes/TEST1AA":
			postcodeRequests++
			_, _ = response.Write([]byte(`{"status":200,"result":{"latitude":51.410159,"longitude":-0.838339}}`))
		case "/v1/forecast":
			weatherRequests++
			if request.URL.Query().Get("current") != "temperature_2m" || request.URL.Query().Get("timeformat") != "unixtime" {
				t.Errorf("unexpected weather query: %s", request.URL.RawQuery)
			}
			_, _ = fmt.Fprintf(
				response,
				`{"current":{"time":%d,"temperature_2m":27.9}}`,
				outdoorTestNow.Truncate(15*time.Minute).Unix(),
			)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	provider := NewOpenAIOutdoorProvider(OutdoorSearchConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		PostcodeBaseURL: server.URL,
		WeatherBaseURL:  server.URL,
		Location:        "TEST 1AA",
		RequestTimeout:  time.Second,
	})
	provider.now = func() time.Time { return outdoorTestNow }

	conditions, err := provider.fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch conditions: %v", err)
	}
	if conditions.TemperatureC == nil || *conditions.TemperatureC != 27.9 {
		t.Fatalf("expected deterministic 27.9°C temperature, got %v", conditions.TemperatureC)
	}
	if conditions.ObservedAt == nil || *conditions.ObservedAt != "2026-07-12T12:30:00Z" {
		t.Fatalf("expected deterministic current timestamp, got %v", conditions.ObservedAt)
	}
	if postcodeRequests != 1 || weatherRequests != 1 {
		t.Fatalf("expected one postcode and weather request, got postcode=%d weather=%d", postcodeRequests, weatherRequests)
	}
	if !containsSourceTitle(conditions.Sources, "Open-Meteo") {
		t.Fatalf("expected Open-Meteo attribution, got %#v", conditions.Sources)
	}
}

func TestOutdoorProviderRejectsWrongHourTemperature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
          "output": [
            {"type":"web_search_call","action":{"sources":[{"url":"https://weather.metoffice.gov.uk/forecast/test"}]}},
            {"type":"message","content":[{"type":"output_text","text":"{\"temperature_c\":16,\"pm2\":null,\"pm10\":null,\"air_quality_category\":\"good\",\"observed_at\":\"2026-07-12T03:00:00Z\",\"data_quality\":\"forecast\"}"}]}
          ]
        }`))
	}))
	defer server.Close()

	provider := NewOpenAIOutdoorProvider(OutdoorSearchConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		PostcodeBaseURL: server.URL,
		WeatherBaseURL:  server.URL,
		Location:        "TEST 1AA",
		RequestTimeout:  time.Second,
	})
	provider.now = func() time.Time { return outdoorTestNow }

	_, err := provider.fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "current observation timestamp") {
		t.Fatalf("expected wrong-hour temperature to be rejected, got %v", err)
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
	base := OutdoorConditions{
		TemperatureC:       &previousTemperature,
		AirQualityCategory: "good",
	}

	if outdoorConditionsMateriallyChanged(base, OutdoorConditions{
		TemperatureC:       &minorTemperatureChange,
		AirQualityCategory: "good",
	}) {
		t.Fatal("expected minor temperature change not to trigger insights")
	}
	if !outdoorConditionsMateriallyChanged(base, OutdoorConditions{
		TemperatureC:       &materialTemperatureChange,
		AirQualityCategory: "good",
	}) {
		t.Fatal("expected two-degree temperature change to trigger insights")
	}
}

func containsSourceTitle(sources []AlertSource, title string) bool {
	for _, source := range sources {
		if source.Title == title {
			return true
		}
	}
	return false
}
