package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOpenAIAlertAnalyzerUsesLunaWithMediumReasoningByDefault(t *testing.T) {
	var requestPayload struct {
		Model           string `json:"model"`
		MaxOutputTokens int    `json:"max_output_tokens"`
		Reasoning       struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&requestPayload); err != nil {
			t.Errorf("decode request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"air_quality\",\"kind\":\"insight\",\"severity\":\"info\",\"title\":\"Air quality stable\",\"message\":\"No material changes detected.\"}]}"}`))
	}))
	defer server.Close()

	analyzer := NewOpenAIAlertAnalyzer(
		"test-key",
		"",
		"",
		server.URL,
		1,
		defaultAlertThresholds(),
	)
	if _, err := analyzer.Analyze(context.Background(), []SensorReading{{Timestamp: 1738886400}}); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if requestPayload.Model != "gpt-5.6-luna" {
		t.Fatalf("expected gpt-5.6-luna, got %q", requestPayload.Model)
	}
	if requestPayload.Reasoning.Effort != "medium" {
		t.Fatalf("expected medium reasoning effort, got %q", requestPayload.Reasoning.Effort)
	}
	if requestPayload.MaxOutputTokens != insightsMaxOutputTokens {
		t.Fatalf("expected output limit %d, got %d", insightsMaxOutputTokens, requestPayload.MaxOutputTokens)
	}
}

func TestDailyLimitedAnalyzerUsesDeterministicInsightsAfterLimit(t *testing.T) {
	delegate := &fakeAlertAnalyzer{alerts: []Alert{{
		Topic: "general", Kind: "insight", Severity: "info", Title: "AI", Message: "Generated.",
	}}}
	analyzer := NewDailyLimitedAlertAnalyzer(delegate, 1, 3, defaultAlertThresholds())
	readings := []SensorReading{{Timestamp: 1738886400, PM2: 12, PM10: 5, Humidity: 45, Temperature: 22}}

	if _, err := analyzer.Analyze(context.Background(), readings); err != nil {
		t.Fatalf("first analysis: %v", err)
	}
	alerts, err := analyzer.Analyze(context.Background(), readings)
	if err != nil {
		t.Fatalf("fallback analysis: %v", err)
	}
	if delegate.calls != 1 {
		t.Fatalf("expected one OpenAI call, got %d", delegate.calls)
	}
	if len(alerts) != 1 || alerts[0].Topic != "air_quality" {
		t.Fatalf("expected deterministic threshold insight, got %#v", alerts)
	}
	if analyzer.Source() != "rules" {
		t.Fatalf("expected deterministic source after budget exhaustion, got %q", analyzer.Source())
	}
}

func TestSystemPromptDefinesWhenOutdoorContextIsUseful(t *testing.T) {
	prompt := systemPrompt(3, defaultAlertThresholds())
	for _, expected := range []string{
		"only in an air_quality insight",
		"or in a temperature insight",
		"Never use outdoor context for humidity",
		"If outdoor context does not change the recommended action, ignore it",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected outdoor-context instruction %q", expected)
		}
	}
}

type staticOutdoorContext struct {
	conditions OutdoorConditions
}

func (source staticOutdoorContext) Snapshot() (OutdoorConditions, bool) {
	return source.conditions, true
}

func TestOpenAIAlertAnalyzerAttachesSourcesOnlyWhenOutdoorContextIsUsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"temperature\",\"kind\":\"tip\",\"severity\":\"info\",\"title\":\"Cooler air outside\",\"message\":\"Outdoor air is cooler, so brief ventilation may help.\",\"uses_outdoor_context\":true}]}"}`))
	}))
	defer server.Close()

	outdoorTemperature := 15.0
	analyzer := NewOpenAIAlertAnalyzerWithOutdoor(
		"test-key",
		"test-model",
		"low",
		server.URL,
		1,
		defaultAlertThresholds(),
		staticOutdoorContext{conditions: OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			AirQualityCategory: "good",
			Sources: []AlertSource{{
				Title: "Met Office",
				URL:   "https://www.metoffice.gov.uk/",
			}},
		}},
	)

	alerts, err := analyzer.Analyze(context.Background(), []SensorReading{{
		Timestamp:   1738886400,
		Temperature: 27,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(alerts) != 1 || len(alerts[0].Sources) != 1 {
		t.Fatalf("expected outdoor source on insight, got %#v", alerts)
	}
	if alerts[0].Sources[0].Title != "Met Office" {
		t.Fatalf("unexpected outdoor source: %#v", alerts[0].Sources[0])
	}
}

func TestAlertSchemaRequiresAtLeastOneInsight(t *testing.T) {
	schema := alertSchema(4)
	propertiesRaw, ok := schema["properties"]
	if !ok {
		t.Fatalf("schema missing properties")
	}

	properties, ok := propertiesRaw.(map[string]any)
	if !ok {
		t.Fatalf("schema properties has unexpected type %T", propertiesRaw)
	}

	alertsRaw, ok := properties["alerts"]
	if !ok {
		t.Fatalf("schema missing alerts property")
	}

	alertsSchema, ok := alertsRaw.(map[string]any)
	if !ok {
		t.Fatalf("alerts schema has unexpected type %T", alertsRaw)
	}

	minItems, ok := alertsSchema["minItems"]
	if !ok {
		t.Fatalf("alerts schema missing minItems")
	}

	if minItems != 1 {
		t.Fatalf("expected minItems=1, got %v", minItems)
	}
}

func TestFallbackStableAlertProducesInsight(t *testing.T) {
	alert := fallbackStableAlert([]SensorReading{
		{
			Timestamp:   1738886400,
			Temperature: 22.3,
			Humidity:    41.2,
			PM2:         3.5,
			PM10:        5.1,
		},
	})

	if alert.Kind != "insight" {
		t.Fatalf("expected kind insight, got %q", alert.Kind)
	}
	if alert.Severity != "info" {
		t.Fatalf("expected severity info, got %q", alert.Severity)
	}
	if strings.TrimSpace(alert.Title) == "" {
		t.Fatalf("expected non-empty title")
	}
	if strings.TrimSpace(alert.Message) == "" {
		t.Fatalf("expected non-empty message")
	}
}

func TestBuildAlertSummaryUsesUnixSecondsForWindows(t *testing.T) {
	baseTimestamp := int64(1738886400)
	summary := buildAlertSummary([]SensorReading{
		{Timestamp: baseTimestamp, PM2: 1},
		{Timestamp: baseTimestamp + 10*secondsPerMinute, PM2: 4},
		{Timestamp: baseTimestamp + 20*secondsPerMinute, PM2: 10},
	})

	if summary.WindowMin != 20 {
		t.Fatalf("expected a 20 minute window, got %d", summary.WindowMin)
	}
	if summary.Delta10m.PM2 != 6 {
		t.Fatalf("expected 10 minute PM2 delta 6, got %.1f", summary.Delta10m.PM2)
	}
	if summary.Delta60m.PM2 != 9 {
		t.Fatalf("expected 60 minute PM2 delta to use oldest sample, got %.1f", summary.Delta60m.PM2)
	}
}

func TestNormalizeAlertSeverityUsesDashboardThresholds(t *testing.T) {
	thresholds := defaultAlertThresholds()

	watchHumidity := alertSummary{
		Latest: metricSnapshot{
			Humidity: 25.4,
		},
	}
	if got := normalizeAlertSeverity("humidity", "critical", watchHumidity, thresholds); got != "warn" {
		t.Fatalf("expected humidity critical to normalize to warn, got %q", got)
	}

	actionHumidity := alertSummary{
		Latest: metricSnapshot{
			Humidity: 24.9,
		},
	}
	if got := normalizeAlertSeverity("humidity", "warn", actionHumidity, thresholds); got != "critical" {
		t.Fatalf("expected action humidity to normalize to critical, got %q", got)
	}

	watchTemperature := alertSummary{
		Latest: metricSnapshot{
			Temperature: 29.2,
		},
	}
	if got := normalizeAlertSeverity("temperature", "critical", watchTemperature, thresholds); got != "warn" {
		t.Fatalf("expected temperature critical to normalize to warn, got %q", got)
	}

	actionPM := alertSummary{
		ParticulateAvailable: true,
		Latest: metricSnapshot{
			PM2: 16.0,
		},
	}
	if got := normalizeAlertSeverity("air_quality", "warn", actionPM, thresholds); got != "critical" {
		t.Fatalf("expected action PM to normalize to critical, got %q", got)
	}
}

func TestUnavailableParticulateDataIsExcludedFromSummaryAndFallback(t *testing.T) {
	summary := buildAlertSummary([]SensorReading{
		{Timestamp: 1738886400, Temperature: 21, Humidity: 45, PM2: 5, PM10: 8},
		{
			Timestamp:   1738887000,
			Temperature: 21.2,
			Humidity:    45.4,
			PM2:         100,
			PM10:        120,
			PMAvailable: boolPtr(false),
		},
	})

	if summary.ParticulateAvailable {
		t.Fatal("expected latest particulate state to be unavailable")
	}
	if summary.ParticulateSamples != 1 {
		t.Fatalf("expected one valid particulate sample, got %d", summary.ParticulateSamples)
	}
	if summary.Latest.PM2 != 0 || summary.Delta10m.PM2 != 0 {
		t.Fatalf("expected unavailable PM values to be excluded, got latest %.1f delta %.1f", summary.Latest.PM2, summary.Delta10m.PM2)
	}

	alert := fallbackStableAlertFromSummary(summary)
	if strings.Contains(alert.Message, "PM2.5") || !strings.Contains(alert.Message, "unavailable") {
		t.Fatalf("expected fallback to disclose unavailable PM data, got %q", alert.Message)
	}
}

func TestNormalizeAlertMessageRemovesCriticalCopyForWatchSeverity(t *testing.T) {
	message := "Critically dry air reached a critical range and critical threshold. Action recommended."
	got := normalizeAlertMessageForSeverity(message, "warn")
	if strings.Contains(strings.ToLower(got), "critical") {
		t.Fatalf("expected critical copy to be removed, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "action") {
		t.Fatalf("expected action copy to be removed, got %q", got)
	}

	critical := normalizeAlertMessageForSeverity(message, "critical")
	if critical != message {
		t.Fatalf("expected critical message to remain unchanged, got %q", critical)
	}
}

func TestNormalizeAlertsPreservesCompleteActionCopy(t *testing.T) {
	message := "Indoor temperature is 26.8°C and rose 1.6°C in 10 minutes. Consider lowering the thermostat, increasing ventilation, or using fans to bring the room back into the comfortable 18–26°C range. Continue monitoring until it settles."
	summary := alertSummary{
		Latest: metricSnapshot{
			Temperature: 26.8,
		},
	}

	alerts := normalizeAlerts(
		[]Alert{{
			Topic:    "temperature",
			Kind:     "alert",
			Severity: "warn",
			Title:    "Temperature slightly high",
			Message:  message,
		}},
		1,
		summary,
		defaultAlertThresholds(),
	)

	if len(alerts) != 1 {
		t.Fatalf("expected one alert, got %d", len(alerts))
	}
	if alerts[0].Message != message {
		t.Fatalf("expected complete message, got %q", alerts[0].Message)
	}
}

func TestTrimToLengthUsesValidUnicodeAndAVisibleEllipsis(t *testing.T) {
	message := strings.Repeat("particulate µg/m³ improved ", 20)
	trimmed := trimToLength(message, 80)

	if !utf8.ValidString(trimmed) {
		t.Fatalf("expected valid UTF-8, got %q", trimmed)
	}
	if utf8.RuneCountInString(trimmed) > 80 {
		t.Fatalf("expected at most 80 characters, got %d", utf8.RuneCountInString(trimmed))
	}
	if !strings.HasSuffix(trimmed, "…") {
		t.Fatalf("expected visible ellipsis, got %q", trimmed)
	}
}
