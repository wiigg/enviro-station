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
