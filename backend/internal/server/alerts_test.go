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

func TestOpenAIAlertAnalyzerUsesTerraWithLowReasoningByDefault(t *testing.T) {
	var requestPayload struct {
		Model     string `json:"model"`
		Reasoning struct {
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

	if requestPayload.Model != "gpt-5.6-terra" {
		t.Fatalf("expected gpt-5.6-terra, got %q", requestPayload.Model)
	}
	if requestPayload.Reasoning.Effort != "low" {
		t.Fatalf("expected low reasoning effort, got %q", requestPayload.Reasoning.Effort)
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
		Latest: metricSnapshot{
			PM2: 16.0,
		},
	}
	if got := normalizeAlertSeverity("air_quality", "warn", actionPM, thresholds); got != "critical" {
		t.Fatalf("expected action PM to normalize to critical, got %q", got)
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
