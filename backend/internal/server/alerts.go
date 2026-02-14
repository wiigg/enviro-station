package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Alert struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Message  string `json:"message"`
}

type AlertAnalyzer interface {
	Analyze(ctx context.Context, readings []SensorReading) ([]Alert, error)
	Source() string
}

type cachedAlertAnalyzer struct {
	next      AlertAnalyzer
	ttl       time.Duration
	lastAt    time.Time
	lastValue []Alert
	mu        sync.Mutex
}

func NewCachedAlertAnalyzer(next AlertAnalyzer, ttl time.Duration) AlertAnalyzer {
	return &cachedAlertAnalyzer{next: next, ttl: ttl}
}

func (analyzer *cachedAlertAnalyzer) Source() string {
	return analyzer.next.Source()
}

func (analyzer *cachedAlertAnalyzer) Analyze(
	ctx context.Context,
	readings []SensorReading,
) ([]Alert, error) {
	if analyzer.ttl <= 0 {
		return analyzer.next.Analyze(ctx, readings)
	}

	now := time.Now()

	analyzer.mu.Lock()
	defer analyzer.mu.Unlock()

	if now.Sub(analyzer.lastAt) < analyzer.ttl {
		cached := cloneAlerts(analyzer.lastValue)
		return cached, nil
	}

	alerts, err := analyzer.next.Analyze(ctx, readings)
	if err != nil {
		return nil, err
	}

	analyzer.lastAt = now
	analyzer.lastValue = cloneAlerts(alerts)
	return alerts, nil
}

func cloneAlerts(alerts []Alert) []Alert {
	output := make([]Alert, len(alerts))
	copy(output, alerts)
	return output
}

type openAIAlertAnalyzer struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	maxAlerts  int
}

func NewOpenAIAlertAnalyzer(apiKey string, model string, baseURL string, maxAlerts int) AlertAnalyzer {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		trimmedModel = "gpt-5-mini"
	}

	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "https://api.openai.com/v1"
	}

	if maxAlerts < 1 {
		maxAlerts = 4
	}
	if maxAlerts > 20 {
		maxAlerts = 20
	}

	return &openAIAlertAnalyzer{
		// Request deadline is controlled by the caller context timeout.
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(trimmedBaseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      trimmedModel,
		maxAlerts:  maxAlerts,
	}
}

func (analyzer *openAIAlertAnalyzer) Source() string {
	return "openai"
}

func (analyzer *openAIAlertAnalyzer) Analyze(
	ctx context.Context,
	readings []SensorReading,
) ([]Alert, error) {
	if len(readings) == 0 {
		return []Alert{}, nil
	}

	payload, err := json.Marshal(buildAlertSummary(readings))
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}

	requestPayload := map[string]any{
		"model": analyzer.model,
		"input": []map[string]any{
			{
				"role": "system",
				"content": []map[string]any{
					{
						"type": "input_text",
						"text": systemPrompt(analyzer.maxAlerts),
					},
				},
			},
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "input_text",
						"text": "Analyze this telemetry summary and return insights only as JSON.\n" + string(payload),
					},
				},
			},
		},
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "enviro_alerts",
				"strict": true,
				"schema": alertSchema(analyzer.maxAlerts),
			},
		},
	}

	requestBody, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		analyzer.baseURL+"/responses",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+analyzer.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := analyzer.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("openai status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var modelResponse struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}

	if err = json.Unmarshal(body, &modelResponse); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	text := strings.TrimSpace(modelResponse.OutputText)
	if text == "" {
		for _, output := range modelResponse.Output {
			for _, content := range output.Content {
				if content.Type == "output_text" || content.Type == "text" {
					text = strings.TrimSpace(content.Text)
					if text != "" {
						break
					}
				}
			}
			if text != "" {
				break
			}
		}
	}

	if text == "" {
		return nil, fmt.Errorf("openai response did not include text output")
	}

	var envelope struct {
		Alerts []Alert `json:"alerts"`
	}

	if err = json.Unmarshal([]byte(text), &envelope); err != nil {
		extracted := extractJSONObject(text)
		if extracted == "" {
			return nil, fmt.Errorf("invalid alert payload: %w", err)
		}
		if retryErr := json.Unmarshal([]byte(extracted), &envelope); retryErr != nil {
			return nil, fmt.Errorf("invalid alert payload: %w", retryErr)
		}
	}

	alerts := normalizeAlerts(envelope.Alerts, analyzer.maxAlerts)
	if len(alerts) == 0 {
		return []Alert{fallbackStableAlert(readings)}, nil
	}

	return alerts, nil
}

func systemPrompt(maxAlerts int) string {
	return fmt.Sprintf(
		"You are an indoor air quality analyst. Return up to %d concise actionable insights "+
			"for a home environment. Include a mix of alert, insight, and tip when useful. "+
			"Use severities critical, warn, or info. Always return at least one insight. "+
			"If conditions are stable, return one concise info insight describing stable conditions. "+
			"Keep title under 60 characters and message under 180 characters.",
		maxAlerts,
	)
}

func alertSchema(maxAlerts int) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"alerts"},
		"properties": map[string]any{
			"alerts": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": maxAlerts,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"kind", "severity", "title", "message"},
					"properties": map[string]any{
						"kind": map[string]any{
							"type": "string",
							"enum": []string{"alert", "insight", "tip"},
						},
						"severity": map[string]any{
							"type": "string",
							"enum": []string{"critical", "warn", "info"},
						},
						"title": map[string]any{
							"type":      "string",
							"minLength": 3,
							"maxLength": 60,
						},
						"message": map[string]any{
							"type":      "string",
							"minLength": 6,
							"maxLength": 180,
						},
					},
				},
			},
		},
	}
}

func extractJSONObject(input string) string {
	start := strings.Index(input, "{")
	end := strings.LastIndex(input, "}")
	if start == -1 || end == -1 || start >= end {
		return ""
	}
	return input[start : end+1]
}

func normalizeAlerts(alerts []Alert, maxAlerts int) []Alert {
	output := make([]Alert, 0, len(alerts))

	for _, alert := range alerts {
		kind := strings.ToLower(strings.TrimSpace(alert.Kind))
		switch kind {
		case "alert", "insight", "tip":
		default:
			kind = ""
		}

		severity := strings.ToLower(strings.TrimSpace(alert.Severity))
		switch severity {
		case "critical", "warn", "info":
		default:
			severity = "info"
		}
		if kind == "" {
			if severity == "critical" || severity == "warn" {
				kind = "alert"
			} else {
				kind = "insight"
			}
		}

		title := strings.TrimSpace(alert.Title)
		message := strings.TrimSpace(alert.Message)
		if title == "" || message == "" {
			continue
		}

		output = append(output, Alert{
			Kind:     kind,
			Severity: severity,
			Title:    trimToLength(title, 60),
			Message:  trimToLength(message, 180),
		})

		if len(output) >= maxAlerts {
			break
		}
	}

	return output
}

func trimToLength(input string, maxLength int) string {
	if len(input) <= maxLength {
		return input
	}
	return strings.TrimSpace(input[:maxLength])
}

func fallbackStableAlert(readings []SensorReading) Alert {
	summary := buildAlertSummary(readings)
	message := fmt.Sprintf(
		"Air is stable. PM2.5 %.1f ug/m3, PM10 %.1f ug/m3, humidity %.0f%%, temperature %.1fC.",
		summary.Latest.PM2,
		summary.Latest.PM10,
		summary.Latest.Humidity,
		summary.Latest.Temperature,
	)

	return Alert{
		Kind:     "insight",
		Severity: "info",
		Title:    "Air quality stable",
		Message:  trimToLength(message, 180),
	}
}

type alertSummary struct {
	SampleCount int            `json:"sample_count"`
	WindowMin   int64          `json:"window_minutes"`
	LatestTS    int64          `json:"latest_timestamp"`
	Latest      metricSnapshot `json:"latest"`
	Averages    metricSnapshot `json:"averages"`
	Minimums    metricSnapshot `json:"minimums"`
	Maximums    metricSnapshot `json:"maximums"`
	Delta10m    metricSnapshot `json:"delta_10m"`
	Delta60m    metricSnapshot `json:"delta_60m"`
}

type metricSnapshot struct {
	PM2         float64 `json:"pm2"`
	PM10        float64 `json:"pm10"`
	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
}

func buildAlertSummary(readings []SensorReading) alertSummary {
	latest := readings[len(readings)-1]
	oldest := readings[0]
	windowMin := int64(0)
	if latest.Timestamp > oldest.Timestamp {
		windowMin = (latest.Timestamp - oldest.Timestamp) / 60000
	}

	return alertSummary{
		SampleCount: len(readings),
		WindowMin:   windowMin,
		LatestTS:    latest.Timestamp,
		Latest: metricSnapshot{
			PM2:         round1(latest.PM2),
			PM10:        round1(latest.PM10),
			Temperature: round1(latest.Temperature),
			Humidity:    round1(latest.Humidity),
		},
		Averages: metricSnapshot{
			PM2:         round1(avgMetric(readings, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(avgMetric(readings, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(avgMetric(readings, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(avgMetric(readings, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Minimums: metricSnapshot{
			PM2:         round1(minMetric(readings, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(minMetric(readings, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(minMetric(readings, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(minMetric(readings, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Maximums: metricSnapshot{
			PM2:         round1(maxMetric(readings, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(maxMetric(readings, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(maxMetric(readings, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(maxMetric(readings, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Delta10m: metricSnapshot{
			PM2:         round1(deltaAtMinutes(readings, 10, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(deltaAtMinutes(readings, 10, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(deltaAtMinutes(readings, 10, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(deltaAtMinutes(readings, 10, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Delta60m: metricSnapshot{
			PM2:         round1(deltaAtMinutes(readings, 60, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(deltaAtMinutes(readings, 60, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(deltaAtMinutes(readings, 60, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(deltaAtMinutes(readings, 60, func(reading SensorReading) float64 { return reading.Humidity })),
		},
	}
}

func avgMetric(readings []SensorReading, metric func(SensorReading) float64) float64 {
	if len(readings) == 0 {
		return 0
	}

	sum := 0.0
	for _, reading := range readings {
		sum += metric(reading)
	}
	return sum / float64(len(readings))
}

func minMetric(readings []SensorReading, metric func(SensorReading) float64) float64 {
	if len(readings) == 0 {
		return 0
	}

	minimum := metric(readings[0])
	for _, reading := range readings[1:] {
		value := metric(reading)
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func maxMetric(readings []SensorReading, metric func(SensorReading) float64) float64 {
	if len(readings) == 0 {
		return 0
	}

	maximum := metric(readings[0])
	for _, reading := range readings[1:] {
		value := metric(reading)
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func deltaAtMinutes(
	readings []SensorReading,
	minutes int64,
	metric func(SensorReading) float64,
) float64 {
	if len(readings) < 2 {
		return 0
	}

	latest := readings[len(readings)-1]
	target := latest.Timestamp - (minutes * 60 * 1000)
	reference := readings[0]

	for index := len(readings) - 1; index >= 0; index-- {
		candidate := readings[index]
		if candidate.Timestamp <= target {
			reference = candidate
			break
		}
	}

	return metric(latest) - metric(reference)
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}
