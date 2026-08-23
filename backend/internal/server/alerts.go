package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Alert struct {
	Topic              string        `json:"topic,omitempty"`
	Kind               string        `json:"kind"`
	Severity           string        `json:"severity"`
	Title              string        `json:"title"`
	Message            string        `json:"message"`
	UsesOutdoorContext bool          `json:"uses_outdoor_context,omitempty"`
	Sources            []AlertSource `json:"sources,omitempty"`
}

type AlertSource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
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

const (
	criticalPM2Threshold             = 15.0
	criticalPM10Threshold            = 45.0
	criticalHumidityLowThreshold     = 25.0
	criticalHumidityHighThreshold    = 70.0
	criticalTemperatureLowThreshold  = 15.0
	criticalTemperatureHighThreshold = 30.0
	alertMessageMaxLength            = 320
	secondsPerMinute                 = int64(60)
	defaultInsightsDailyRequestLimit = 48
	insightsMaxOutputTokens          = 1200
	outdoorCoolingHighMargin         = 0.5
)

const locationCoordinateNumberPattern = `[+-]?[0-9]{1,3}\.[0-9]{2,}`

var (
	errGeneratedAlertPrivacyCheck = errors.New("generated alert failed privacy checks")
	ukPostcodePattern             = regexp.MustCompile(`(?i)\b(GIR[[:space:]]*0AA|([A-PR-UWYZ][0-9][0-9A-HJKSTUW]?|[A-PR-UWYZ][A-HK-Y][0-9][0-9ABEHMNPRV-Y]?)[[:space:]]*[0-9][ABD-HJLNP-UW-Z]{2})\b`)
	bareCoordinatePairPattern     = regexp.MustCompile(`(` + locationCoordinateNumberPattern + `)[[:space:]]*[,;/][[:space:]]*(` + locationCoordinateNumberPattern + `)`)
	latitudeLongitudePattern      = regexp.MustCompile(`(?i)\b(latitude|lat)\b[^0-9+\-]{0,16}(` + locationCoordinateNumberPattern + `)[^0-9+\-]{0,32}\b(longitude|long|lon|lng)\b[^0-9+\-]{0,16}(` + locationCoordinateNumberPattern + `)`)
	longitudeLatitudePattern      = regexp.MustCompile(`(?i)\b(longitude|long|lon|lng)\b[^0-9+\-]{0,16}(` + locationCoordinateNumberPattern + `)[^0-9+\-]{0,32}\b(latitude|lat)\b[^0-9+\-]{0,16}(` + locationCoordinateNumberPattern + `)`)
	compassCoordinatePairPattern  = regexp.MustCompile(`(?i)(` + locationCoordinateNumberPattern + `)[[:space:]°]*(N|S)\b[[:space:]]*[,;/]?[[:space:]]*(` + locationCoordinateNumberPattern + `)[[:space:]°]*(E|W)\b`)
)

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
	for index, alert := range alerts {
		output[index] = alert
		output[index].Sources = cloneAlertSources(alert.Sources)
	}
	return output
}

type openAIAlertAnalyzer struct {
	httpClient      *http.Client
	baseURL         string
	apiKey          string
	model           string
	reasoningEffort string
	maxAlerts       int
	thresholds      AlertThresholds
	outdoorContext  OutdoorContextSource
}

type dailyLimitedAlertAnalyzer struct {
	analyzer   AlertAnalyzer
	budget     *dailyRequestBudget
	maxAlerts  int
	thresholds AlertThresholds
	mu         sync.RWMutex
	lastSource string
}

type outdoorAlertGuidance interface {
	applyOutdoorGuidance(
		ctx context.Context,
		alerts []Alert,
		summary alertSummary,
	) []Alert
}

func NewDailyLimitedAlertAnalyzer(
	analyzer AlertAnalyzer,
	dailyLimit int,
	maxAlerts int,
	thresholds AlertThresholds,
) AlertAnalyzer {
	if dailyLimit < 1 {
		dailyLimit = defaultInsightsDailyRequestLimit
	}
	if maxAlerts < 1 || maxAlerts > maxInsightsLimit {
		maxAlerts = maxInsightsLimit
	}
	return &dailyLimitedAlertAnalyzer{
		analyzer:   analyzer,
		budget:     newDailyRequestBudget(dailyLimit),
		maxAlerts:  maxAlerts,
		thresholds: thresholds,
	}
}

func (analyzer *dailyLimitedAlertAnalyzer) Analyze(
	ctx context.Context,
	readings []SensorReading,
) ([]Alert, error) {
	if len(readings) == 0 {
		return []Alert{}, nil
	}
	if analyzer.budget.take(time.Now()) {
		alerts, err := analyzer.analyzer.Analyze(ctx, readings)
		if err == nil {
			analyzer.setSource(analyzer.analyzer.Source())
		}
		return alerts, err
	}
	if analyzer.budget.markExhaustionLogged() {
		log.Printf("insights model daily request limit reached; using deterministic insights")
	}
	analyzer.setSource("rules")
	summary := buildAlertSummary(readings)
	alerts := fallbackAlerts(summary, analyzer.maxAlerts, analyzer.thresholds)
	if guidance, ok := analyzer.analyzer.(outdoorAlertGuidance); ok {
		alerts = guidance.applyOutdoorGuidance(ctx, alerts, summary)
	}
	if alertsContainPrivateLocation(alerts) {
		return nil, errGeneratedAlertPrivacyCheck
	}
	return alerts, nil
}

func (analyzer *dailyLimitedAlertAnalyzer) Source() string {
	analyzer.mu.RLock()
	source := analyzer.lastSource
	analyzer.mu.RUnlock()
	if source == "" {
		return analyzer.analyzer.Source()
	}
	return source
}

func (analyzer *dailyLimitedAlertAnalyzer) setSource(source string) {
	analyzer.mu.Lock()
	analyzer.lastSource = source
	analyzer.mu.Unlock()
}

func NewOpenAIAlertAnalyzer(
	apiKey string,
	model string,
	reasoningEffort string,
	baseURL string,
	maxAlerts int,
	thresholds AlertThresholds,
) AlertAnalyzer {
	return newOpenAIAlertAnalyzer(
		apiKey,
		model,
		reasoningEffort,
		baseURL,
		maxAlerts,
		thresholds,
		nil,
	)
}

func NewOpenAIAlertAnalyzerWithOutdoor(
	apiKey string,
	model string,
	reasoningEffort string,
	baseURL string,
	maxAlerts int,
	thresholds AlertThresholds,
	outdoorContext OutdoorContextSource,
) AlertAnalyzer {
	return newOpenAIAlertAnalyzer(
		apiKey,
		model,
		reasoningEffort,
		baseURL,
		maxAlerts,
		thresholds,
		outdoorContext,
	)
}

func newOpenAIAlertAnalyzer(
	apiKey string,
	model string,
	reasoningEffort string,
	baseURL string,
	maxAlerts int,
	thresholds AlertThresholds,
	outdoorContext OutdoorContextSource,
) *openAIAlertAnalyzer {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		trimmedModel = "gpt-5.6-luna"
	}

	trimmedReasoningEffort := strings.TrimSpace(reasoningEffort)
	if trimmedReasoningEffort == "" {
		trimmedReasoningEffort = "medium"
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
		httpClient:      &http.Client{},
		baseURL:         strings.TrimRight(trimmedBaseURL, "/"),
		apiKey:          strings.TrimSpace(apiKey),
		model:           trimmedModel,
		reasoningEffort: trimmedReasoningEffort,
		maxAlerts:       maxAlerts,
		thresholds:      normalizedThresholds,
		outdoorContext:  outdoorContext,
	}
}

func (analyzer *openAIAlertAnalyzer) Source() string {
	return "openai"
}

func (analyzer *openAIAlertAnalyzer) applyOutdoorGuidance(
	ctx context.Context,
	alerts []Alert,
	summary alertSummary,
) []Alert {
	if analyzer.outdoorContext == nil {
		return alerts
	}
	conditions, ok := analyzer.outdoorContext.Snapshot()
	if !ok {
		if refresher, refreshable := analyzer.outdoorContext.(OutdoorContextRefresher); refreshable {
			conditions, ok = refresher.EnsureFresh(ctx)
		}
	}
	return applyOutdoorTemperatureGuidance(
		alerts,
		summary,
		conditions,
		ok,
		analyzer.maxAlerts,
		analyzer.thresholds,
	)
}

func (analyzer *openAIAlertAnalyzer) Analyze(
	ctx context.Context,
	readings []SensorReading,
) ([]Alert, error) {
	if len(readings) == 0 {
		return []Alert{}, nil
	}

	summary := buildAlertSummary(readings)
	var outdoorConditions OutdoorConditions
	hasOutdoorConditions := false
	if analyzer.outdoorContext != nil {
		outdoorConditions, hasOutdoorConditions = analyzer.outdoorContext.Snapshot()
		if !hasOutdoorConditions {
			if refresher, ok := analyzer.outdoorContext.(OutdoorContextRefresher); ok {
				outdoorConditions, hasOutdoorConditions = refresher.EnsureFresh(ctx)
			}
		}
	}
	analysisPayload := any(summary)
	if hasOutdoorConditions {
		analysisPayload = map[string]any{
			"indoor":  summary,
			"outdoor": outdoorConditions,
		}
	}

	payload, err := json.Marshal(analysisPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}

	requestPayload := map[string]any{
		"model":             analyzer.model,
		"max_output_tokens": insightsMaxOutputTokens,
		"reasoning": map[string]any{
			"effort": analyzer.reasoningEffort,
		},
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
		return nil, fmt.Errorf("openai status %d", response.StatusCode)
	}

	var modelResponse responsesAPIResponse

	if err = json.Unmarshal(body, &modelResponse); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	text := responseOutputText(modelResponse)

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
	if alertsContainPrivateLocation(envelope.Alerts) {
		return nil, errGeneratedAlertPrivacyCheck
	}

	alerts := normalizeAlerts(envelope.Alerts, analyzer.maxAlerts, summary, analyzer.thresholds)
	for index := range alerts {
		ventilationDecision := enforceOutdoorVentilationSafety(
			&alerts[index],
			summary,
			outdoorConditions,
			analyzer.thresholds,
		)
		if hasOutdoorConditions && alerts[index].UsesOutdoorContext {
			if ventilationDecision != ventilationNotApplicable {
				alerts[index].Sources = mergeAlertSources(
					outdoorConditions.TemperatureSources,
					outdoorConditions.AirQualitySources,
					outdoorConditions.Sources,
				)
			} else {
				alerts[index].Sources = outdoorSourcesForTopic(outdoorConditions, alerts[index].Topic)
			}
			if len(alerts[index].Sources) == 0 {
				alerts[index].UsesOutdoorContext = false
			}
		} else {
			alerts[index].UsesOutdoorContext = false
			alerts[index].Sources = nil
		}
	}
	alerts = applyOutdoorTemperatureGuidance(
		alerts,
		summary,
		outdoorConditions,
		hasOutdoorConditions,
		analyzer.maxAlerts,
		analyzer.thresholds,
	)
	if alertsContainPrivateLocation(alerts) {
		return nil, errGeneratedAlertPrivacyCheck
	}
	if len(alerts) == 0 {
		fallback := fallbackAlerts(summary, analyzer.maxAlerts, analyzer.thresholds)
		if len(fallback) > 0 {
			return fallback, nil
		}
		return []Alert{fallbackStableAlert(readings)}, nil
	}

	return alerts, nil
}

func alertsContainPrivateLocation(alerts []Alert) bool {
	for _, alert := range alerts {
		if containsPrivateLocation(strings.Join([]string{
			alert.Topic,
			alert.Kind,
			alert.Severity,
			alert.Title,
			alert.Message,
		}, " ")) {
			return true
		}
		for _, source := range alert.Sources {
			if containsPrivateLocation(source.Title + " " + source.URL) {
				return true
			}
		}
	}
	return false
}

func containsPrivateLocation(text string) bool {
	if containsPrivateLocationPlainText(text) {
		return true
	}
	decoded, err := url.QueryUnescape(text)
	return err == nil && decoded != text && containsPrivateLocationPlainText(decoded)
}

func containsPrivateLocationPlainText(text string) bool {
	if ukPostcodePattern.MatchString(text) {
		return true
	}
	for _, match := range bareCoordinatePairPattern.FindAllStringSubmatch(text, -1) {
		if isLatitudeLongitudePair(match[1], match[2]) || isLatitudeLongitudePair(match[2], match[1]) {
			return true
		}
	}
	for _, match := range latitudeLongitudePattern.FindAllStringSubmatch(text, -1) {
		if isLatitudeLongitudePair(match[2], match[4]) {
			return true
		}
	}
	for _, match := range longitudeLatitudePattern.FindAllStringSubmatch(text, -1) {
		if isLatitudeLongitudePair(match[4], match[2]) {
			return true
		}
	}
	for _, match := range compassCoordinatePairPattern.FindAllStringSubmatch(text, -1) {
		if isLatitudeLongitudePair(match[1], match[3]) {
			return true
		}
	}
	return false
}

func isLatitudeLongitudePair(latitudeText string, longitudeText string) bool {
	latitude, latitudeErr := strconv.ParseFloat(latitudeText, 64)
	longitude, longitudeErr := strconv.ParseFloat(longitudeText, 64)
	return latitudeErr == nil && longitudeErr == nil &&
		math.Abs(latitude) <= 90 && math.Abs(longitude) <= 180
}

type ventilationSafetyDecision uint8

const (
	ventilationNotApplicable ventilationSafetyDecision = iota
	ventilationAllowed
	ventilationBlocked
)

func enforceOutdoorVentilationSafety(
	alert *Alert,
	summary alertSummary,
	conditions OutdoorConditions,
	thresholds AlertThresholds,
) ventilationSafetyDecision {
	if alert == nil {
		return ventilationNotApplicable
	}
	recommendsOutdoorAir := recommendsOpeningWindows(alert.Title + " " + alert.Message)
	if !recommendsOutdoorAir && !alert.UsesOutdoorContext {
		return ventilationNotApplicable
	}
	if (alert.Topic == "air_quality" || alert.Topic == "temperature") &&
		outdoorAirSupportsVentilation(summary, conditions, thresholds) &&
		outdoorTemperatureSupportsVentilation(summary, conditions, thresholds) {
		alert.UsesOutdoorContext = true
		return ventilationAllowed
	}

	alert.UsesOutdoorContext = true
	switch alert.Topic {
	case "air_quality":
		alert.Title = "Keep windows closed"
		alert.Message = "Keep windows closed. Reduce indoor particle sources or use filtration while levels settle."
	case "temperature":
		alert.Title = "Use indoor temperature controls"
		alert.Message = "Keep windows closed. Use heating, cooling, or fans while conditions settle."
	default:
		alert.UsesOutdoorContext = false
		alert.Title = "Use indoor controls"
		alert.Message = "Use indoor controls and keep monitoring; the available data does not support ventilation."
	}
	alert.Message = trimToLength(alert.Message, alertMessageMaxLength)
	return ventilationBlocked
}

func applyOutdoorTemperatureGuidance(
	alerts []Alert,
	summary alertSummary,
	conditions OutdoorConditions,
	hasOutdoorConditions bool,
	maxAlerts int,
	thresholds AlertThresholds,
) []Alert {
	if !hasOutdoorConditions ||
		!outdoorCoolingOpportunity(summary, conditions, thresholds) {
		return alerts
	}

	indoorTemperature := summary.Latest.Temperature
	outdoorTemperature := *conditions.TemperatureC
	severity := normalizeAlertSeverity("temperature", "info", summary, thresholds)
	kind := "tip"
	if severity == "warn" || severity == "critical" {
		kind = "alert"
	}
	sources := mergeAlertSources(
		conditions.TemperatureSources,
		conditions.AirQualitySources,
		conditions.Sources,
	)
	if len(sources) == 0 {
		return alerts
	}

	guidance := Alert{
		Topic:              "temperature",
		Kind:               kind,
		Severity:           severity,
		UsesOutdoorContext: true,
		Sources:            sources,
	}
	temperatureContext := fmt.Sprintf(
		"It is %.1fC indoors and %.1fC outside.",
		indoorTemperature,
		outdoorTemperature,
	)
	if trend := temperatureTrendSummary(summary, thresholds); trend != "" {
		temperatureContext += " " + trend
	}
	outdoorAirSummary := outdoorAirQualitySummary(conditions)
	if outdoorAirSupportsVentilation(summary, conditions, thresholds) {
		guidance.Title = "Cooler air outside"
		guidance.Message = fmt.Sprintf(
			"%s Outdoor air quality is %s and supports opening windows briefly to help cool the room.",
			temperatureContext,
			outdoorAirSummary,
		)
	} else {
		guidance.Title = "Keep windows closed for now"
		guidance.Message = fmt.Sprintf(
			"%s Outdoor air quality is %s and does not support ventilation right now. Keep windows closed and use fans or other indoor cooling.",
			temperatureContext,
			outdoorAirSummary,
		)
	}
	guidance.Message = trimToLength(guidance.Message, alertMessageMaxLength)

	for index := range alerts {
		if alerts[index].Topic == "temperature" {
			guidance.Severity = bumpAlertSeverity(guidance.Severity, alerts[index].Severity)
			if alerts[index].Kind == "alert" || guidance.Severity != "info" {
				guidance.Kind = "alert"
			}
			alerts[index] = guidance
			return alerts
		}
	}
	for index := range alerts {
		if alerts[index].Topic == "general" {
			alerts[index] = guidance
			return alerts
		}
	}
	if maxAlerts > 0 && len(alerts) < maxAlerts {
		return append(alerts, guidance)
	}
	return alerts
}

func temperatureTrendSummary(summary alertSummary, thresholds AlertThresholds) string {
	delta := summary.Delta10m.Temperature
	switch {
	case delta >= thresholds.TemperatureDeltaTrigger:
		return fmt.Sprintf("Temperature rose %.1fC over 10 min.", delta)
	case delta <= -thresholds.TemperatureDeltaTrigger:
		return fmt.Sprintf("Temperature fell %.1fC over 10 min.", math.Abs(delta))
	default:
		return ""
	}
}

func outdoorAirQualitySummary(conditions OutdoorConditions) string {
	category := strings.ReplaceAll(normalizedOutdoorAirQualityCategory(conditions.AirQualityCategory), "_", " ")
	if category == "unknown" {
		category = "unavailable"
	}
	if conditions.PM2 != nil && conditions.PM10 != nil {
		return fmt.Sprintf(
			"%s (PM2.5 %.1f and PM10 %.1f ug/m3)",
			category,
			*conditions.PM2,
			*conditions.PM10,
		)
	}
	return category + " with incomplete particulate data"
}

func outdoorCoolingOpportunity(
	summary alertSummary,
	conditions OutdoorConditions,
	thresholds AlertThresholds,
) bool {
	if conditions.TemperatureC == nil {
		return false
	}
	indoor := summary.Latest.Temperature
	outdoor := *conditions.TemperatureC
	return indoor >= thresholds.TemperatureHighThreshold-outdoorCoolingHighMargin &&
		outdoor >= thresholds.TemperatureLowThreshold &&
		outdoor < thresholds.TemperatureHighThreshold &&
		indoor-outdoor >= thresholds.TemperatureDeltaTrigger &&
		outdoorTemperatureSupportsVentilation(summary, conditions, thresholds)
}

func recommendsOpeningWindows(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	for _, safePhrase := range []string{
		"avoid opening",
		"do not open",
		"don't open",
		"never open",
		"do not use",
		"don't use",
		"never use",
		"avoid using",
		"do not bring",
		"don't bring",
		"never bring",
		"avoid bringing",
		"do not draw",
		"don't draw",
		"never draw",
		"do not pull",
		"don't pull",
		"never pull",
		"do not let",
		"don't let",
		"never let",
		"avoid ventilat",
		"do not ventilat",
		"don't ventilat",
		"never ventilat",
		"avoid natural ventilation",
		"avoid outdoor ventilation",
		"do not run ventilation",
		"don't run ventilation",
		"avoid running ventilation",
		"keep ventilation off",
		"ventilation is not recommended",
		"does not support ventilation",
		"keep the window closed",
		"keep the windows closed",
		"keep window closed",
		"keep windows closed",
		"keep the window shut",
		"keep the windows shut",
		"keep window shut",
		"keep windows shut",
		"window closed",
		"windows closed",
		"door closed",
		"doors closed",
		"window remains closed",
		"windows remain closed",
		"door remains closed",
		"doors remain closed",
		"window shut",
		"windows shut",
		"door shut",
		"doors shut",
		"keep the outside-air intake off",
		"keep the outdoor-air intake off",
		"keep the fresh-air intake off",
		"outside-air intake is closed",
		"outdoor-air intake is closed",
		"fresh-air intake is closed",
	} {
		normalized = strings.ReplaceAll(normalized, safePhrase, "")
	}
	outdoorAirNamed := strings.Contains(normalized, "outside air") ||
		strings.Contains(normalized, "outside-air") ||
		strings.Contains(normalized, "outdoor air") ||
		strings.Contains(normalized, "outdoor-air") ||
		strings.Contains(normalized, "fresh air") ||
		strings.Contains(normalized, "fresh-air")
	outdoorExchangeAction := containsAnyWord(normalized,
		"open", "opened", "opening", "leave", "leaving", "crack", "cracking", "raise", "raising", "lift", "lifting",
		"bring", "bringing", "let", "letting", "draw", "drawing", "pull", "pulling",
		"flush", "flushing", "exchange", "exchanging", "switch", "switching", "run",
		"running", "increase", "increasing", "maximise", "maximising", "maximize",
		"maximizing", "activate", "activating", "enable", "enabling", "turn", "turning",
		"allow", "allowing", "admit", "admitting",
	) ||
		strings.Contains(normalized, "ventilat")
	if outdoorAirNamed && outdoorExchangeAction {
		return true
	}
	for _, indoorPhrase := range []string{
		"filtered mechanical ventilation",
		"filtered ventilation",
		"recirculating ventilation",
		"recirculation ventilation",
		"extractor fan",
		"recirculating fan",
		"recirculation fan",
		"use fans to increase airflow",
		"use a fan to increase airflow",
		"increase indoor airflow",
		"improve indoor airflow",
	} {
		normalized = strings.ReplaceAll(normalized, indoorPhrase, "")
	}
	for _, recommendation := range []string{
		"open a window",
		"open the window",
		"open windows",
		"open the windows",
		"opening a window",
		"opening the window",
		"opening windows",
		"opening the windows",
		"increase ventilation",
		"improve ventilation",
		"brief ventilation",
		"ventilate the room",
		"natural ventilation",
		"air out the room",
		"air the room out",
		"air out the home",
		"air out your home",
		"air the home out",
		"air your home out",
		"let fresh air in",
		"bring fresh air in",
		"bring outside air in",
		"bring outdoor air in",
		"cross-breeze",
		"cross breeze",
		"vent the room",
		"vent your room",
		"vent the home",
		"vent your home",
	} {
		if strings.Contains(normalized, recommendation) {
			return true
		}
	}
	if strings.Contains(normalized, "ventilat") {
		return true
	}
	outdoorOpening := containsAnyWord(normalized,
		"open", "opened", "opening", "leave", "leaving", "crack", "cracking", "ajar", "raise", "raising", "lift",
		"lifting", "flush", "flushing", "draw", "drawing", "exchange", "exchanging",
		"switch", "switching", "run", "running", "activate", "activating", "enable",
		"enabling", "turn", "turning", "allow", "allowing", "admit", "admitting",
	)
	outdoorOpeningObject := containsAnyWord(normalized,
		"window", "windows", "sash", "sashes", "door", "doors", "vent", "vents",
		"intake", "intakes", "supply", "supplies",
	)
	if outdoorOpening && outdoorOpeningObject {
		return true
	}
	return false
}

func containsAnyWord(text string, words ...string) bool {
	wanted := make(map[string]struct{}, len(words))
	for _, word := range words {
		wanted[word] = struct{}{}
	}
	for _, word := range strings.FieldsFunc(text, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if _, ok := wanted[word]; ok {
			return true
		}
	}
	return false
}

func outdoorAirSupportsVentilation(
	summary alertSummary,
	conditions OutdoorConditions,
	thresholds AlertThresholds,
) bool {
	category := normalizedOutdoorAirQualityCategory(conditions.AirQualityCategory)
	if category != "good" && category != "fair" {
		return false
	}
	if conditions.PM2 == nil || conditions.PM10 == nil {
		return false
	}
	return outdoorParticulateSupportsVentilation(
		*conditions.PM2,
		summary.Latest.PM2,
		thresholds.PM2Threshold,
		summary.ParticulateAvailable,
	) && outdoorParticulateSupportsVentilation(
		*conditions.PM10,
		summary.Latest.PM10,
		thresholds.PM10Threshold,
		summary.ParticulateAvailable,
	)
}

func outdoorParticulateSupportsVentilation(
	outdoor float64,
	indoor float64,
	threshold float64,
	indoorAvailable bool,
) bool {
	if outdoor < threshold {
		return true
	}
	return indoorAvailable && indoor >= threshold && outdoor < indoor
}

func normalizedOutdoorAirQualityCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "good":
		return "good"
	case "fair":
		return "fair"
	case "moderate":
		return "moderate"
	case "poor":
		return "poor"
	case "very_poor":
		return "very_poor"
	case "extremely_poor":
		return "extremely_poor"
	default:
		return "unknown"
	}
}

func outdoorTemperatureSupportsVentilation(
	summary alertSummary,
	conditions OutdoorConditions,
	thresholds AlertThresholds,
) bool {
	if conditions.TemperatureC == nil {
		return false
	}
	indoor := summary.Latest.Temperature
	outdoor := *conditions.TemperatureC
	if comfortDistance(outdoor, thresholds.TemperatureLowThreshold, thresholds.TemperatureHighThreshold) >
		comfortDistance(indoor, thresholds.TemperatureLowThreshold, thresholds.TemperatureHighThreshold) {
		return false
	}
	switch {
	case indoor < thresholds.TemperatureLowThreshold:
		return outdoor > indoor
	case indoor >= thresholds.TemperatureHighThreshold:
		return outdoor < indoor
	default:
		return outdoor >= thresholds.TemperatureLowThreshold &&
			outdoor < thresholds.TemperatureHighThreshold
	}
}

func systemPrompt(maxAlerts int, thresholds AlertThresholds) string {
	topics := focusedAlertTopics()
	return fmt.Sprintf(
		"You are an indoor air quality analyst. Return between 1 and %d concise actionable insights "+
			"for a home environment. Use focused topics at most once each and prefer this order: %s. "+
			"If all monitored conditions are stable, return exactly one info insight with topic general. "+
			"Otherwise return only the noteworthy topics and omit stable ones. "+
			"Each non-general alert must focus on one topic only and must not bundle unrelated metrics. "+
			"When particulate_available is false, do not discuss, infer, or generate alerts from PM values because they are unavailable. "+
			"When an outdoor object is provided, use it only in an air_quality insight when current outdoor particulate values or air-quality category changes ventilation advice, or in a temperature insight when outdoor temperature changes whether bringing outdoor air inside would move the room toward the 18-26C comfort range. Never use outdoor context for humidity, merely mention the weather, or make a comparison unsupported by available outdoor fields. Never recommend opening windows, doors, vents, or otherwise bringing outdoor air inside unless outdoor air quality is good or fair, both PM values are available, and each PM value is below its configured alert threshold or improves on an already-elevated indoor value. Never recommend it when outdoor temperature would move the room farther from comfort. "+
			"When indoor temperature is near the upper comfort boundary and outdoor air is materially cooler within the comfort range, explicitly advise whether brief window opening is suitable based on outdoor air quality; do not merely call the indoor temperature comfortable. "+
			"If outdoor context does not change the recommended action, ignore it. "+
			"Never mention or infer a postcode, address, town, coordinates, or station location. Set uses_outdoor_context true only when the insight materially relies on the outdoor object; otherwise set it false. "+
			"For air_quality discuss PM2.5 and PM10 only, treating PM2.5 at or above %.1f ug/m3, PM10 at or above %.1f ug/m3, or 10 minute moves of %.1f/%.1f ug/m3 as noteworthy. "+
			"For humidity discuss humidity only, treating values below %.0f%% or above %.0f%% and 10 minute moves of %.1f points as noteworthy. "+
			"For temperature discuss temperature only, treating values below %.1fC or above %.1fC and 10 minute moves of %.1fC as noteworthy. "+
			"Treat both worsening and improvement as noteworthy when the change is material. "+
			"Use critical only for PM2.5 above %.1f ug/m3, PM10 above %.1f ug/m3, humidity below %.0f%% or at/above %.0f%%, or temperature at/below %.1fC or at/above %.1fC; otherwise use warn for noteworthy non-critical conditions. "+
			"Use info only for neutral observations or material improvements. "+
			"Do not use severity labels such as critical, warn, watch, or action in titles or messages; the UI displays severity separately. "+
			"Keep title under 60 characters and message under 320 characters. End every message with a complete sentence.",
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
		criticalPM2Threshold,
		criticalPM10Threshold,
		criticalHumidityLowThreshold,
		criticalHumidityHighThreshold,
		criticalTemperatureLowThreshold,
		criticalTemperatureHighThreshold,
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
					"required":             []string{"topic", "kind", "severity", "title", "message", "uses_outdoor_context"},
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
							"maxLength": alertMessageMaxLength,
						},
						"uses_outdoor_context": map[string]any{
							"type": "boolean",
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
		if !summary.ParticulateAvailable && (topic == "air_quality" || topic == "general") {
			continue
		}
		severity = normalizeAlertSeverity(topic, severity, summary, thresholds)
		if severity == "critical" || severity == "warn" {
			kind = "alert"
		}
		normalized := Alert{
			Topic:              topic,
			Kind:               kind,
			Severity:           severity,
			Title:              trimToLength(normalizeAlertMessageForSeverity(title, severity), 60),
			Message:            trimToLength(normalizeAlertMessageForSeverity(message, severity), alertMessageMaxLength),
			UsesOutdoorContext: alert.UsesOutdoorContext,
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
	if topicHasCriticalValue(topic, summary) {
		return bumpAlertSeverity(severity, "critical")
	}

	switch topic {
	case "air_quality":
		if !summary.ParticulateAvailable {
			return severity
		}
		if summary.Latest.PM2 >= thresholds.PM2Threshold ||
			summary.Latest.PM10 >= thresholds.PM10Threshold {
			return "warn"
		}
	case "humidity":
		if summary.Latest.Humidity < thresholds.HumidityLowThreshold ||
			summary.Latest.Humidity >= thresholds.HumidityHighThreshold {
			return "warn"
		}
	case "temperature":
		if summary.Latest.Temperature <= thresholds.TemperatureLowThreshold ||
			summary.Latest.Temperature >= thresholds.TemperatureHighThreshold {
			return "warn"
		}
	}

	if severity == "critical" {
		return "warn"
	}
	return severity
}

func topicHasCriticalValue(topic string, summary alertSummary) bool {
	switch topic {
	case "air_quality":
		if !summary.ParticulateAvailable {
			return false
		}
		return summary.Latest.PM2 > criticalPM2Threshold ||
			summary.Latest.PM10 > criticalPM10Threshold
	case "humidity":
		return summary.Latest.Humidity < criticalHumidityLowThreshold ||
			summary.Latest.Humidity >= criticalHumidityHighThreshold
	case "temperature":
		return summary.Latest.Temperature <= criticalTemperatureLowThreshold ||
			summary.Latest.Temperature >= criticalTemperatureHighThreshold
	default:
		return false
	}
}

func normalizeAlertMessageForSeverity(message string, severity string) string {
	if severity == "critical" {
		return message
	}
	return strings.NewReplacer(
		"Critical threshold", "Threshold",
		"critical threshold", "threshold",
		"Critical range", "Noteworthy range",
		"critical range", "noteworthy range",
		"Critically", "Very",
		"critically", "very",
		"Critical", "Watch",
		"critical", "watch",
		"Action recommended", "Watch",
		"action recommended", "watch",
		"Action required", "Watch",
		"action required", "watch",
		"Take action", "Check",
		"take action", "check",
	).Replace(message)
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
		if !summary.ParticulateAvailable {
			return false
		}
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
				alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
					alertMessageMaxLength,
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
	trimmed := strings.TrimSpace(input)
	if maxLength <= 0 {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= maxLength {
		return trimmed
	}
	if maxLength == 1 {
		return "…"
	}

	cutoffRunes := runes[:maxLength-1]
	lastSpace := -1
	for index, character := range cutoffRunes {
		if unicode.IsSpace(character) {
			lastSpace = index
		}
	}
	if lastSpace >= maxLength/2 {
		cutoffRunes = cutoffRunes[:lastSpace]
	}
	return strings.TrimSpace(string(cutoffRunes)) + "…"
}

func fallbackStableAlert(readings []SensorReading) Alert {
	return fallbackStableAlertFromSummary(buildAlertSummary(readings))
}

func fallbackStableAlertFromSummary(summary alertSummary) Alert {
	message := ""
	if summary.ParticulateAvailable {
		message = fmt.Sprintf(
			"Conditions are stable. PM2.5 %.1f ug/m3, PM10 %.1f ug/m3, humidity %.0f%%, temperature %.1fC.",
			summary.Latest.PM2,
			summary.Latest.PM10,
			summary.Latest.Humidity,
			summary.Latest.Temperature,
		)
	} else {
		message = fmt.Sprintf(
			"Temperature %.1fC and humidity %.0f%% are stable. Particle data is unavailable and was excluded.",
			summary.Latest.Temperature,
			summary.Latest.Humidity,
		)
	}

	return Alert{
		Topic:    "general",
		Kind:     "insight",
		Severity: "info",
		Title:    "Home conditions stable",
		Message:  trimToLength(message, alertMessageMaxLength),
	}
}

type alertSummary struct {
	SampleCount          int            `json:"sample_count"`
	ParticulateSamples   int            `json:"particulate_samples"`
	ParticulateAvailable bool           `json:"particulate_available"`
	WindowMin            int64          `json:"window_minutes"`
	LatestTS             int64          `json:"latest_timestamp"`
	Latest               metricSnapshot `json:"latest"`
	Averages             metricSnapshot `json:"averages"`
	Minimums             metricSnapshot `json:"minimums"`
	Maximums             metricSnapshot `json:"maximums"`
	Delta10m             metricSnapshot `json:"delta_10m"`
	Delta60m             metricSnapshot `json:"delta_60m"`
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
	pmReadings := availableParticulateReadings(readings)
	pmAvailable := particulateAvailable(latest)
	latestPM2 := 0.0
	latestPM10 := 0.0
	if pmAvailable {
		latestPM2 = latest.PM2
		latestPM10 = latest.PM10
	}
	windowMin := int64(0)
	if latest.Timestamp > oldest.Timestamp {
		windowMin = (latest.Timestamp - oldest.Timestamp) / secondsPerMinute
	}

	return alertSummary{
		SampleCount:          len(readings),
		ParticulateSamples:   len(pmReadings),
		ParticulateAvailable: pmAvailable,
		WindowMin:            windowMin,
		LatestTS:             latest.Timestamp,
		Latest: metricSnapshot{
			PM2:         round1(latestPM2),
			PM10:        round1(latestPM10),
			Temperature: round1(latest.Temperature),
			Humidity:    round1(latest.Humidity),
		},
		Averages: metricSnapshot{
			PM2:         round1(avgMetric(pmReadings, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(avgMetric(pmReadings, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(avgMetric(readings, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(avgMetric(readings, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Minimums: metricSnapshot{
			PM2:         round1(minMetric(pmReadings, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(minMetric(pmReadings, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(minMetric(readings, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(minMetric(readings, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Maximums: metricSnapshot{
			PM2:         round1(maxMetric(pmReadings, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(maxMetric(pmReadings, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(maxMetric(readings, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(maxMetric(readings, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Delta10m: metricSnapshot{
			PM2:         round1(pmDeltaAtMinutes(pmReadings, pmAvailable, 10, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(pmDeltaAtMinutes(pmReadings, pmAvailable, 10, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(deltaAtMinutes(readings, 10, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(deltaAtMinutes(readings, 10, func(reading SensorReading) float64 { return reading.Humidity })),
		},
		Delta60m: metricSnapshot{
			PM2:         round1(pmDeltaAtMinutes(pmReadings, pmAvailable, 60, func(reading SensorReading) float64 { return reading.PM2 })),
			PM10:        round1(pmDeltaAtMinutes(pmReadings, pmAvailable, 60, func(reading SensorReading) float64 { return reading.PM10 })),
			Temperature: round1(deltaAtMinutes(readings, 60, func(reading SensorReading) float64 { return reading.Temperature })),
			Humidity:    round1(deltaAtMinutes(readings, 60, func(reading SensorReading) float64 { return reading.Humidity })),
		},
	}
}

func availableParticulateReadings(readings []SensorReading) []SensorReading {
	output := make([]SensorReading, 0, len(readings))
	for _, reading := range readings {
		if particulateAvailable(reading) {
			output = append(output, reading)
		}
	}
	return output
}

func pmDeltaAtMinutes(
	readings []SensorReading,
	latestAvailable bool,
	minutes int64,
	metric func(SensorReading) float64,
) float64 {
	if !latestAvailable {
		return 0
	}
	return deltaAtMinutes(readings, minutes, metric)
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
	target := latest.Timestamp - (minutes * secondsPerMinute)
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
