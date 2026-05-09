package server

import (
	"strings"
	"testing"
)

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
			Timestamp:   1738886400000,
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
