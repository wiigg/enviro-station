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
)

type Alert struct {
	Topic    string `json:"topic,omitempty"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Message  string `json:"message"`
}

type AlertAnalyzer interface {
	Analyze(ctx context.Context, readings []SensorReading) ([]Alert, error)
	Source() string
}

type AlertThresholds struct {
	PM2Threshold             float64
	PM10Threshold            float64
	PM2DeltaTrigger          float64
	PM10DeltaTrigger         float64
	HumidityLowThreshold     float64
	HumidityHighThreshold    float64
	HumidityDeltaTrigger     float64
	TemperatureLowThreshold  float64
	TemperatureHighThreshold float64
	TemperatureDeltaTrigger  float64
}

func defaultAlertThresholds() AlertThresholds {
	return AlertThresholds{
		PM2Threshold:             8.0,
		PM10Threshold:            30.0,
		PM2DeltaTrigger:          5.0,
		PM10DeltaTrigger:         15.0,
		HumidityLowThreshold:     40.0,
		HumidityHighThreshold:    60.0,
		HumidityDeltaTrigger:     8.0,
		TemperatureLowThreshold:  18.0,
		TemperatureHighThreshold: 26.0,
		TemperatureDeltaTrigger:  1.5,
	}
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
	thresholds AlertThresholds
}

func NewOpenAIAlertAnalyzer(
	apiKey string,
	model string,
	baseURL string,
	maxAlerts int,
	thresholds AlertThresholds,
) AlertAnalyzer {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		trimmedModel = "gpt-5-mini"
	}

	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "https://api.openai.com/v1"
	}

	if maxAlerts < 1 {
		maxAlerts = maxInsightsLimit
	}
	if maxAlerts > maxInsightsLimit {
		maxAlerts = maxInsightsLimit
	}

	normalizedThresholds := thresholds
	defaults := defaultAlertThresholds()
	if normalizedThresholds.PM2Threshold <= 0 {
		normalizedThresholds.PM2Threshold = defaults.PM2Threshold
	}
	if normalizedThresholds.PM10Threshold <= 0 {
		normalizedThresholds.PM10Threshold = defaults.PM10Threshold
	}
	if normalizedThresholds.PM2DeltaTrigger <= 0 {
		normalizedThresholds.PM2DeltaTrigger = defaults.PM2DeltaTrigger
	}
	if normalizedThresholds.PM10DeltaTrigger <= 0 {
		normalizedThresholds.PM10DeltaTrigger = defaults.PM10DeltaTrigger
	}
	if normalizedThresholds.HumidityLowThreshold <= 0 {
		normalizedThresholds.HumidityLowThreshold = defaults.HumidityLowThreshold
	}
	if normalizedThresholds.HumidityHighThreshold <= normalizedThresholds.HumidityLowThreshold {
		normalizedThresholds.HumidityHighThreshold = defaults.HumidityHighThreshold
	}
	if normalizedThresholds.HumidityDeltaTrigger <= 0 {
		normalizedThresholds.HumidityDeltaTrigger = defaults.HumidityDeltaTrigger
	}
	if normalizedThresholds.TemperatureLowThreshold <= 0 {
		normalizedThresholds.TemperatureLowThreshold = defaults.TemperatureLowThreshold
	}
	if normalizedThresholds.TemperatureHighThreshold <= normalizedThresholds.TemperatureLowThreshold {
		normalizedThresholds.TemperatureHighThreshold = defaults.TemperatureHighThreshold
	}
	if normalizedThresholds.TemperatureDeltaTrigger <= 0 {
		normalizedThresholds.TemperatureDeltaTrigger = defaults.TemperatureDeltaTrigger
	}

	return &openAIAlertAnalyzer{
		// Request deadline is controlled by the caller context timeout.
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(trimmedBaseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      trimmedModel,
		maxAlerts:  maxAlerts,
		thresholds: normalizedThresholds,
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

	summary := buildAlertSummary(readings)

	payload, err := json.Marshal(summary)
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
						"text": systemPrompt(analyzer.maxAlerts, analyzer.thresholds),
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

	alerts := normalizeAlerts(envelope.Alerts, analyzer.maxAlerts, summary, analyzer.thresholds)
	if len(alerts) == 0 {
		fallback := fallbackAlerts(summary, analyzer.maxAlerts, analyzer.thresholds)
		if len(fallback) > 0 {
			return fallback, nil
		}
		return []Alert{fallbackStableAlert(readings)}, nil
	}

	return alerts, nil
}

func systemPrompt(maxAlerts int, thresholds AlertThresholds) string {
	topics := focusedAlertTopics()
	return fmt.Sprintf(
		"You are an indoor air quality analyst. Return between 1 and %d concise actionable insights "+
			"for a home environment. Use focused topics at most once each and prefer this order: %s. "+
			"If all monitored conditions are stable, return exactly one info insight with topic general. "+
			"Otherwise return only the noteworthy topics and omit stable ones. "+
			"Each non-general alert must focus on one topic only and must not bundle unrelated metrics. "+
			"For air_quality discuss PM2.5 and PM10 only, treating PM2.5 at or above %.1f ug/m3, PM10 at or above %.1f ug/m3, or 10 minute moves of %.1f/%.1f ug/m3 as noteworthy. "+
			"For humidity discuss humidity only, treating values below %.0f%% or above %.0f%% and 10 minute moves of %.1f points as noteworthy. "+
			"For temperature discuss temperature only, treating values below %.1fC or above %.1fC and 10 minute moves of %.1fC as noteworthy. "+
			"Treat both worsening and improvement as noteworthy when the change is material. "+
			"Use severities critical, warn, or info. "+
			"Keep title under 60 characters and message under 180 characters.",
		maxAlerts,
		strings.Join(topics, ", "),
		thresholds.PM2Threshold,
		thresholds.PM10Threshold,
		thresholds.PM2DeltaTrigger,
		thresholds.PM10DeltaTrigger,
		thresholds.HumidityLowThreshold,
		thresholds.HumidityHighThreshold,
		thresholds.HumidityDeltaTrigger,
		thresholds.TemperatureLowThreshold,
		thresholds.TemperatureHighThreshold,
		thresholds.TemperatureDeltaTrigger,
	)
}

func alertSchema(maxAlerts int) map[string]any {
	topics := alertTopics()

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
					"required":             []string{"topic", "kind", "severity", "title", "message"},
					"properties": map[string]any{
						"topic": map[string]any{
							"type": "string",
							"enum": topics,
						},
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

func alertTopics() []string {
	return []string{"general", "air_quality", "humidity", "temperature"}
}

func focusedAlertTopics() []string {
	return []string{"air_quality", "humidity", "temperature"}
}

func normalizeAlerts(
	alerts []Alert,
	maxAlerts int,
	summary alertSummary,
	thresholds AlertThresholds,
) []Alert {
	if maxAlerts <= 0 {
		return []Alert{}
	}

	topics := focusedAlertTopics()
	allowedTopics := make(map[string]struct{}, len(topics))
	for _, topic := range alertTopics() {
		allowedTopics[topic] = struct{}{}
	}

	alertsByTopic := make(map[string]Alert, len(topics))
	var generalAlert *Alert

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

		topic := normalizeAlertTopic(alert.Topic, title, message, allowedTopics)
		if topic == "" {
			continue
		}
		severity = normalizeAlertSeverity(topic, severity, summary, thresholds)
		if severity == "critical" || severity == "warn" {
			kind = "alert"
		}
		normalized := Alert{
			Topic:    topic,
			Kind:     kind,
			Severity: severity,
			Title:    trimToLength(title, 60),
			Message:  trimToLength(message, 180),
		}
		if topic == "general" {
			if generalAlert == nil {
				alertCopy := normalized
				generalAlert = &alertCopy
			}
			continue
		}
		if _, exists := alertsByTopic[topic]; exists {
			continue
		}

		alertsByTopic[topic] = normalized
		if len(alertsByTopic) >= len(topics) {
			break
		}
	}

	output := make([]Alert, 0, len(topics))
	for _, topic := range topics {
		if alert, ok := alertsByTopic[topic]; ok {
			output = append(output, alert)
			if len(output) >= maxAlerts {
				break
			}
		}
	}

	if isStableSummary(summary, thresholds) {
		if generalAlert != nil {
			return []Alert{*generalAlert}
		}
		return []Alert{fallbackStableAlertFromSummary(summary)}
	}

	for _, topic := range topics {
		if len(output) >= maxAlerts {
			break
		}
		if _, ok := alertsByTopic[topic]; ok {
			continue
		}
		if !topicNeedsFallbackAlert(summary, topic, thresholds) {
			continue
		}
		output = append(output, fallbackAlertForTopic(summary, topic, thresholds))
	}

	if len(output) > 0 {
		return output
	}
	return fallbackAlerts(summary, maxAlerts, thresholds)
}

func normalizeAlertTopic(
	rawTopic string,
	title string,
	message string,
	allowedTopics map[string]struct{},
) string {
	topic := strings.ToLower(strings.TrimSpace(rawTopic))
	if _, ok := allowedTopics[topic]; ok {
		return topic
	}

	combined := strings.ToLower(title + " " + message)
	switch {
	case strings.Contains(combined, "pm2"),
		strings.Contains(combined, "pm10"),
		strings.Contains(combined, "partic"),
		strings.Contains(combined, "air quality"):
		if _, ok := allowedTopics["air_quality"]; ok {
			return "air_quality"
		}
	case strings.Contains(combined, "humid"):
		if _, ok := allowedTopics["humidity"]; ok {
			return "humidity"
		}
	case strings.Contains(combined, "temp"),
		strings.Contains(combined, "warm"),
		strings.Contains(combined, "cool"):
		if _, ok := allowedTopics["temperature"]; ok {
			return "temperature"
		}
	case strings.Contains(combined, "stable"),
		strings.Contains(combined, "steady"),
		strings.Contains(combined, "overall"),
		strings.Contains(combined, "conditions"),
		strings.Contains(combined, "environment"),
		strings.Contains(combined, "home"):
		if _, ok := allowedTopics["general"]; ok {
			return "general"
		}
	}

	return ""
}

func normalizeAlertSeverity(
	topic string,
	severity string,
	summary alertSummary,
	thresholds AlertThresholds,
) string {
	switch topic {
	case "air_quality":
		if summary.Latest.PM2 >= thresholds.PM2Threshold ||
			summary.Latest.PM10 >= thresholds.PM10Threshold {
			return bumpAlertSeverity(severity, "warn")
		}
	case "humidity":
		if summary.Latest.Humidity < thresholds.HumidityLowThreshold ||
			summary.Latest.Humidity >= thresholds.HumidityHighThreshold {
			return bumpAlertSeverity(severity, "warn")
		}
	case "temperature":
		if summary.Latest.Temperature <= thresholds.TemperatureLowThreshold ||
			summary.Latest.Temperature >= thresholds.TemperatureHighThreshold {
			return bumpAlertSeverity(severity, "warn")
		}
	}

	return severity
}

func bumpAlertSeverity(current string, target string) string {
	severityRank := map[string]int{
		"info":     1,
		"warn":     2,
		"critical": 3,
	}

	if severityRank[target] > severityRank[current] {
		return target
	}
	return current
}

func fallbackAlerts(summary alertSummary, maxAlerts int, thresholds AlertThresholds) []Alert {
	if isStableSummary(summary, thresholds) {
		return []Alert{fallbackStableAlertFromSummary(summary)}
	}

	output := make([]Alert, 0, maxAlerts)
	for _, topic := range focusedAlertTopics() {
		if !topicNeedsFallbackAlert(summary, topic, thresholds) {
			continue
		}
		output = append(output, fallbackAlertForTopic(summary, topic, thresholds))
		if len(output) >= maxAlerts {
			break
		}
	}
	if len(output) == 0 {
		return []Alert{fallbackStableAlertFromSummary(summary)}
	}
	return output
}

func isStableSummary(summary alertSummary, thresholds AlertThresholds) bool {
	return !topicNeedsFallbackAlert(summary, "air_quality", thresholds) &&
		!topicNeedsFallbackAlert(summary, "humidity", thresholds) &&
		!topicNeedsFallbackAlert(summary, "temperature", thresholds)
}

func topicNeedsFallbackAlert(summary alertSummary, topic string, thresholds AlertThresholds) bool {
	switch topic {
	case "air_quality":
		return summary.Latest.PM2 >= thresholds.PM2Threshold ||
			summary.Latest.PM10 >= thresholds.PM10Threshold ||
			math.Abs(summary.Delta10m.PM2) >= thresholds.PM2DeltaTrigger ||
			math.Abs(summary.Delta10m.PM10) >= thresholds.PM10DeltaTrigger
	case "humidity":
		return summary.Latest.Humidity >= thresholds.HumidityHighThreshold ||
			summary.Latest.Humidity < thresholds.HumidityLowThreshold ||
			math.Abs(summary.Delta10m.Humidity) >= thresholds.HumidityDeltaTrigger
	case "temperature":
		return summary.Latest.Temperature >= thresholds.TemperatureHighThreshold ||
			summary.Latest.Temperature <= thresholds.TemperatureLowThreshold ||
			math.Abs(summary.Delta10m.Temperature) >= thresholds.TemperatureDeltaTrigger
	default:
		return false
	}
}

func fallbackAlertForTopic(summary alertSummary, topic string, thresholds AlertThresholds) Alert {
	switch topic {
	case "air_quality":
		pm2Delta := summary.Delta10m.PM2
		pm10Delta := summary.Delta10m.PM10

		if pm2Delta <= -thresholds.PM2DeltaTrigger || pm10Delta <= -thresholds.PM10DeltaTrigger {
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Air quality improving",
				Message: trimToLength(
					fmt.Sprintf(
						"PM2.5 fell %.1f ug/m3 and PM10 fell %.1f ug/m3 over 10 min; now %.1f/%.1f ug/m3.",
						math.Abs(pm2Delta),
						math.Abs(pm10Delta),
						summary.Latest.PM2,
						summary.Latest.PM10,
					),
					180,
				),
			}
		}
		if summary.Latest.PM2 >= thresholds.PM2Threshold ||
			summary.Latest.PM10 >= thresholds.PM10Threshold ||
			pm2Delta >= thresholds.PM2DeltaTrigger ||
			pm10Delta >= thresholds.PM10DeltaTrigger {
			return Alert{
				Topic:    topic,
				Kind:     "alert",
				Severity: "warn",
				Title:    "Particulates rising",
				Message: trimToLength(
					fmt.Sprintf(
						"PM2.5 is %.1f ug/m3 and PM10 is %.1f ug/m3, with a 10 min change of %.1f/%.1f ug/m3.",
						summary.Latest.PM2,
						summary.Latest.PM10,
						pm2Delta,
						pm10Delta,
					),
					180,
				),
			}
		}
		return Alert{
			Topic:    topic,
			Kind:     "insight",
			Severity: "info",
			Title:    "Air quality steady",
			Message: trimToLength(
				fmt.Sprintf(
					"PM2.5 is %.1f ug/m3 and PM10 is %.1f ug/m3 with no sharp change over the last 10 min.",
					summary.Latest.PM2,
					summary.Latest.PM10,
				),
				180,
			),
		}
	case "humidity":
		delta := summary.Delta10m.Humidity

		switch {
		case summary.Latest.Humidity >= thresholds.HumidityHighThreshold:
			return Alert{
				Topic:    topic,
				Kind:     "alert",
				Severity: "warn",
				Title:    "Humidity high",
				Message: trimToLength(
					fmt.Sprintf(
						"Humidity is %.0f%% and may feel muggy. The 10 min change is %.1f points.",
						summary.Latest.Humidity,
						delta,
					),
					180,
				),
			}
		case summary.Latest.Humidity < thresholds.HumidityLowThreshold:
			return Alert{
				Topic:    topic,
				Kind:     "alert",
				Severity: "warn",
				Title:    "Humidity low",
				Message: trimToLength(
					fmt.Sprintf(
						"Humidity is %.0f%% and may feel dry. The 10 min change is %.1f points.",
						summary.Latest.Humidity,
						delta,
					),
					180,
				),
			}
		case delta <= -thresholds.HumidityDeltaTrigger:
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Humidity easing",
				Message: trimToLength(
					fmt.Sprintf(
						"Humidity dropped %.1f points over 10 min and is now %.0f%%.",
						math.Abs(delta),
						summary.Latest.Humidity,
					),
					180,
				),
			}
		case delta >= thresholds.HumidityDeltaTrigger:
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Humidity rising",
				Message: trimToLength(
					fmt.Sprintf(
						"Humidity rose %.1f points over 10 min and is now %.0f%%.",
						delta,
						summary.Latest.Humidity,
					),
					180,
				),
			}
		default:
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Humidity steady",
				Message: trimToLength(
					fmt.Sprintf("Humidity is holding around %.0f%%.", summary.Latest.Humidity),
					180,
				),
			}
		}
	case "temperature":
		delta := summary.Delta10m.Temperature

		switch {
		case summary.Latest.Temperature >= thresholds.TemperatureHighThreshold:
			return Alert{
				Topic:    topic,
				Kind:     "alert",
				Severity: "warn",
				Title:    "Temperature high",
				Message: trimToLength(
					fmt.Sprintf(
						"Temperature is %.1fC and may feel warm. The 10 min change is %.1fC.",
						summary.Latest.Temperature,
						delta,
					),
					180,
				),
			}
		case summary.Latest.Temperature <= thresholds.TemperatureLowThreshold:
			return Alert{
				Topic:    topic,
				Kind:     "alert",
				Severity: "warn",
				Title:    "Temperature low",
				Message: trimToLength(
					fmt.Sprintf(
						"Temperature is %.1fC and may feel cool. The 10 min change is %.1fC.",
						summary.Latest.Temperature,
						delta,
					),
					180,
				),
			}
		case delta <= -thresholds.TemperatureDeltaTrigger:
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Temperature cooling",
				Message: trimToLength(
					fmt.Sprintf(
						"Temperature dropped %.1fC over 10 min and is now %.1fC.",
						math.Abs(delta),
						summary.Latest.Temperature,
					),
					180,
				),
			}
		case delta >= thresholds.TemperatureDeltaTrigger:
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Temperature rising",
				Message: trimToLength(
					fmt.Sprintf(
						"Temperature rose %.1fC over 10 min and is now %.1fC.",
						delta,
						summary.Latest.Temperature,
					),
					180,
				),
			}
		default:
			return Alert{
				Topic:    topic,
				Kind:     "insight",
				Severity: "info",
				Title:    "Temperature steady",
				Message: trimToLength(
					fmt.Sprintf("Temperature is holding around %.1fC.", summary.Latest.Temperature),
					180,
				),
			}
		}
	default:
		return Alert{
			Topic:    topic,
			Kind:     "insight",
			Severity: "info",
			Title:    "Conditions stable",
			Message:  "Telemetry is stable across the monitored window.",
		}
	}
}

func trimToLength(input string, maxLength int) string {
	if len(input) <= maxLength {
		return input
	}
	return strings.TrimSpace(input[:maxLength])
}

func fallbackStableAlert(readings []SensorReading) Alert {
	return fallbackStableAlertFromSummary(buildAlertSummary(readings))
}

func fallbackStableAlertFromSummary(summary alertSummary) Alert {
	message := fmt.Sprintf(
		"Conditions are stable. PM2.5 %.1f ug/m3, PM10 %.1f ug/m3, humidity %.0f%%, temperature %.1fC.",
		summary.Latest.PM2,
		summary.Latest.PM10,
		summary.Latest.Humidity,
		summary.Latest.Temperature,
	)

	return Alert{
		Topic:    "general",
		Kind:     "insight",
		Severity: "info",
		Title:    "Home conditions stable",
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
