package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		"Make one ventilation decision and keep every topic consistent",
		"Metric ownership is strict even when explaining the shared ventilation decision",
		"A temperature or humidity message must never mention particles, PM, or air quality",
		"When another metric blocks ventilation, say only that outdoor conditions are unsuitable overall",
		"Whenever matching outdoor data is available for a returned temperature, humidity, or air_quality insight",
		"even if another metric controls ventilation",
		"Never put outdoor humidity values in a temperature insight",
		"message to one complete sentence under 160 characters",
		"Do not mention outdoor data on a general insight or without the matching outdoor metric",
		"Set severity from the latest indoor value only; outdoor context must never raise severity",
		"always use info and kind insight even if an outdoor comparison or indoor trend is noteworthy",
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

func newOutdoorTemperatureGuidanceTestAnalyzer(
	t *testing.T,
	conditions OutdoorConditions,
) AlertAnalyzer {
	t.Helper()

	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"general\",\"kind\":\"insight\",\"severity\":\"info\",\"title\":\"Home conditions comfortable\",\"message\":\"Temperature is at a comfortable level.\",\"uses_outdoor_context\":false}]}"}`))
	}))
	t.Cleanup(modelServer.Close)

	return NewOpenAIAlertAnalyzerWithOutdoor(
		"test-key",
		"test-model",
		"low",
		modelServer.URL,
		3,
		defaultAlertThresholds(),
		staticOutdoorContext{conditions: conditions},
	)
}

func outdoorGuidanceTestSources() ([]AlertSource, []AlertSource) {
	return []AlertSource{{Title: "Open-Meteo weather", URL: "https://open-meteo.com/en/docs"}}, []AlertSource{
		{Title: "Open-Meteo air quality", URL: "https://open-meteo.com/en/docs/air-quality-api"},
		{Title: "CAMS", URL: "https://atmosphere.copernicus.eu/"},
	}
}

func TestOutdoorParticulateSupportsVentilation(t *testing.T) {
	tests := []struct {
		name            string
		outdoor         float64
		indoor          float64
		threshold       float64
		materialDelta   float64
		indoorAvailable bool
		want            bool
	}{
		{
			name:            "below threshold",
			outdoor:         7,
			indoor:          3,
			threshold:       8,
			materialDelta:   5,
			indoorAvailable: true,
			want:            true,
		},
		{
			name:            "at threshold with normal indoor air",
			outdoor:         8,
			indoor:          3,
			threshold:       8,
			materialDelta:   5,
			indoorAvailable: true,
			want:            false,
		},
		{
			name:            "elevated indoor air and lower outdoor air",
			outdoor:         3,
			indoor:          10,
			threshold:       8,
			materialDelta:   5,
			indoorAvailable: true,
			want:            true,
		},
		{
			name:            "elevated indoor air without lower outdoor air",
			outdoor:         10,
			indoor:          10,
			threshold:       8,
			materialDelta:   5,
			indoorAvailable: true,
			want:            false,
		},
		{
			name:            "unavailable indoor air and outdoor below threshold",
			outdoor:         7,
			indoor:          0,
			threshold:       8,
			materialDelta:   5,
			indoorAvailable: false,
			want:            true,
		},
		{
			name:            "unavailable indoor air and outdoor at threshold",
			outdoor:         8,
			indoor:          0,
			threshold:       8,
			materialDelta:   5,
			indoorAvailable: false,
			want:            false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := outdoorParticulateSupportsVentilation(
				test.outdoor,
				test.indoor,
				test.threshold,
				test.materialDelta,
				test.indoorAvailable,
			)
			if got != test.want {
				t.Fatalf("outdoorParticulateSupportsVentilation() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestApplyOutdoorTemperatureGuidancePreservesHigherPriorityAlertAtLimit(t *testing.T) {
	outdoorTemperature := 23.0
	outdoorHumidity := 55.0
	outdoorPM2 := 5.0
	outdoorPM10 := 10.0
	temperatureSources, airQualitySources := outdoorGuidanceTestSources()
	existing := Alert{
		Topic:    "humidity",
		Kind:     "alert",
		Severity: "warn",
		Title:    "Humidity high",
		Message:  "Humidity remains above the comfortable range.",
	}
	summary := alertSummary{
		ParticulateAvailable: true,
		Latest: metricSnapshot{
			PM2:         3,
			PM10:        5,
			Temperature: 25.8,
			Humidity:    65,
		},
	}

	alerts := applyOutdoorTemperatureGuidance(
		[]Alert{existing},
		summary,
		OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			RelativeHumidity:   &outdoorHumidity,
			PM2:                &outdoorPM2,
			PM10:               &outdoorPM10,
			AirQualityCategory: "good",
			TemperatureSources: temperatureSources,
			HumiditySources:    temperatureSources,
			AirQualitySources:  airQualitySources,
		},
		true,
		1,
		defaultAlertThresholds(),
	)

	if len(alerts) != 1 {
		t.Fatalf("expected maxAlerts=1 to be respected, got %#v", alerts)
	}
	if alerts[0].Topic != existing.Topic ||
		alerts[0].Kind != existing.Kind ||
		alerts[0].Severity != existing.Severity ||
		alerts[0].Title != existing.Title ||
		alerts[0].Message != existing.Message {
		t.Fatalf("expected higher-priority humidity alert to be preserved, got %#v", alerts[0])
	}
}

func TestApplyOutdoorTemperatureGuidancePreservesExistingTemperatureWarning(t *testing.T) {
	outdoorTemperature := 23.0
	outdoorHumidity := 55.0
	outdoorPM2 := 5.0
	outdoorPM10 := 10.0
	temperatureSources, airQualitySources := outdoorGuidanceTestSources()
	existing := Alert{
		Topic:    "temperature",
		Kind:     "alert",
		Severity: "warn",
		Title:    "Temperature rising",
		Message:  "Temperature rose materially over the last 10 minutes.",
	}
	summary := alertSummary{
		ParticulateAvailable: true,
		Latest: metricSnapshot{
			PM2:         3,
			PM10:        5,
			Temperature: 25.8,
			Humidity:    45,
		},
		Delta10m: metricSnapshot{Temperature: 1.5},
	}

	alerts := applyOutdoorTemperatureGuidance(
		[]Alert{existing},
		summary,
		OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			RelativeHumidity:   &outdoorHumidity,
			PM2:                &outdoorPM2,
			PM10:               &outdoorPM10,
			AirQualityCategory: "good",
			TemperatureSources: temperatureSources,
			HumiditySources:    temperatureSources,
			AirQualitySources:  airQualitySources,
		},
		true,
		1,
		defaultAlertThresholds(),
	)

	if len(alerts) != 1 {
		t.Fatalf("expected one temperature alert, got %#v", alerts)
	}
	if alerts[0].Topic != "temperature" || alerts[0].Severity != "warn" || alerts[0].Kind != "alert" {
		t.Fatalf("expected existing warning severity to be preserved, got %#v", alerts[0])
	}
	if !recommendsOpeningWindows(alerts[0].Title + " " + alerts[0].Message) {
		t.Fatalf("expected warning to include the safe cooling action, got %#v", alerts[0])
	}
}

func TestOpenAIAlertAnalyzerPreservesModelInsightWithoutTemperatureOverride(t *testing.T) {
	outdoorTemperature := 23.0
	outdoorHumidity := 55.0
	// Outdoor air can be slightly more polluted than pristine indoor air while
	// remaining safely below both configured alert thresholds.
	outdoorPM2 := 5.0
	outdoorPM10 := 10.0
	temperatureSources, airQualitySources := outdoorGuidanceTestSources()
	analyzer := newOutdoorTemperatureGuidanceTestAnalyzer(t, OutdoorConditions{
		TemperatureC:       &outdoorTemperature,
		RelativeHumidity:   &outdoorHumidity,
		PM2:                &outdoorPM2,
		PM10:               &outdoorPM10,
		AirQualityCategory: "fair",
		TemperatureSources: temperatureSources,
		HumiditySources:    temperatureSources,
		AirQualitySources:  airQualitySources,
	})

	alerts, err := analyzer.Analyze(context.Background(), []SensorReading{{
		Timestamp:   1738886400,
		Temperature: 25.8,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected one model insight, got %#v", alerts)
	}
	alert := alerts[0]
	if alert.Topic != "general" || alert.Title != "Home conditions comfortable" ||
		alert.Message != "Temperature is at a comfortable level." {
		t.Fatalf("expected the short model copy to remain unchanged, got %#v", alert)
	}
	if alert.UsesOutdoorContext || len(alert.Sources) != 0 {
		t.Fatalf("expected unused outdoor context to remain unattributed, got %#v", alert)
	}
}

func TestOpenAIAlertAnalyzerKeepsAdviceInlineAndAttributesHumidity(t *testing.T) {
	const temperatureMessage = "26.2C inside, 23.1C outside; open windows briefly because outdoor conditions are suitable."
	const humidityMessage = "Humidity is 28%; outside air is similarly dry, so add moisture after airing."
	userInput := ""
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, input := range payload.Input {
			if input.Role == "user" && len(input.Content) > 0 {
				userInput = input.Content[0].Text
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"temperature\",\"kind\":\"alert\",\"severity\":\"warn\",\"title\":\"Cooler outside\",\"message\":\"26.2C inside, 23.1C outside; open windows briefly because outdoor conditions are suitable.\",\"uses_outdoor_context\":true},{\"topic\":\"humidity\",\"kind\":\"alert\",\"severity\":\"warn\",\"title\":\"Air remains dry\",\"message\":\"Humidity is 28%; outside air is similarly dry, so add moisture after airing.\",\"uses_outdoor_context\":true}]}"}`))
	}))
	defer modelServer.Close()

	outdoorTemperature := 23.1
	outdoorHumidity := 34.0
	outdoorPM2 := 5.9
	outdoorPM10 := 8.2
	weatherSources, airQualitySources := outdoorGuidanceTestSources()
	analyzer := NewOpenAIAlertAnalyzerWithOutdoor(
		"test-key",
		"test-model",
		"low",
		modelServer.URL,
		3,
		defaultAlertThresholds(),
		staticOutdoorContext{conditions: OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			RelativeHumidity:   &outdoorHumidity,
			PM2:                &outdoorPM2,
			PM10:               &outdoorPM10,
			AirQualityCategory: "fair",
			TemperatureSources: weatherSources,
			HumiditySources:    weatherSources,
			AirQualitySources:  airQualitySources,
		}},
	)

	alerts, err := analyzer.Analyze(context.Background(), []SensorReading{{
		Timestamp:   1738886400,
		Temperature: 26.2,
		Humidity:    28,
		PM2:         3,
		PM10:        4,
	}})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if strings.Contains(userInput, "recommendation_plan") || !strings.Contains(userInput, `"relative_humidity":34`) {
		t.Fatalf("expected outdoor humidity without a recommendation plan, got %q", userInput)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected two concise insights, got %#v", alerts)
	}
	for _, alert := range alerts {
		switch alert.Topic {
		case "temperature":
			if alert.Message != temperatureMessage || strings.Contains(strings.ToLower(alert.Message), "humidity") {
				t.Fatalf("expected concise temperature-owned copy, got %#v", alert)
			}
			if len(alert.Sources) != 3 {
				t.Fatalf("expected combined ventilation sources, got %#v", alert.Sources)
			}
		case "humidity":
			if alert.Message != humidityMessage {
				t.Fatalf("expected model humidity comparison to survive, got %#v", alert)
			}
			if len(alert.Sources) != 1 || alert.Sources[0].Title != "Open-Meteo weather" {
				t.Fatalf("expected humidity-only source attribution, got %#v", alert.Sources)
			}
		default:
			t.Fatalf("unexpected topic %#v", alert)
		}
	}
}

func TestOpenAIAlertAnalyzerKeepsWindowsClosedWhenOutdoorAirIsUnsafeForCooling(t *testing.T) {
	outdoorTemperature := 23.0
	outdoorHumidity := 55.0
	safePM2 := 5.0
	safePM10 := 10.0
	highPM2 := 9.0
	temperatureSources, airQualitySources := outdoorGuidanceTestSources()
	tests := []struct {
		name       string
		conditions OutdoorConditions
	}{
		{
			name: "poor category",
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				RelativeHumidity:   &outdoorHumidity,
				PM2:                &safePM2,
				PM10:               &safePM10,
				AirQualityCategory: "poor",
				TemperatureSources: temperatureSources,
				AirQualitySources:  airQualitySources,
			},
		},
		{
			name: "particulate above safe threshold",
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				RelativeHumidity:   &outdoorHumidity,
				PM2:                &highPM2,
				PM10:               &safePM10,
				AirQualityCategory: "good",
				TemperatureSources: temperatureSources,
				AirQualitySources:  airQualitySources,
			},
		},
		{
			name: "incomplete particulate data",
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				RelativeHumidity:   &outdoorHumidity,
				PM2:                &safePM2,
				AirQualityCategory: "good",
				TemperatureSources: temperatureSources,
				AirQualitySources:  airQualitySources,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := buildAlertSummary([]SensorReading{{
				Timestamp:   1738886400,
				Temperature: 25.8,
				Humidity:    45,
				PM2:         3,
				PM10:        5,
			}})
			alert := Alert{Topic: "temperature", Title: "Cooler outside", Message: "Open windows briefly to cool the room."}
			decision := enforceOutdoorVentilationSafety(&alert, summary, test.conditions, defaultAlertThresholds())
			if decision != ventilationBlocked {
				t.Fatalf("expected ventilation to be blocked, got %v", decision)
			}
			if recommendsOpeningWindows(alert.Title + " " + alert.Message) {
				t.Fatalf("expected window-opening advice to be blocked, got %#v", alert)
			}
			if !strings.Contains(strings.ToLower(alert.Message), "keep windows closed") {
				t.Fatalf("expected explicit keep-closed guidance, got %#v", alert)
			}
		})
	}
}

func TestOpenAIAlertAnalyzerRetainsComfortInsightWithoutUsefulOutdoorCooling(t *testing.T) {
	outdoorPM2 := 5.0
	outdoorPM10 := 10.0
	temperatureSources, airQualitySources := outdoorGuidanceTestSources()
	tests := []struct {
		name               string
		indoorTemperature  float64
		outdoorTemperature float64
	}{
		{
			name:               "temperature gap below trigger",
			indoorTemperature:  25.8,
			outdoorTemperature: 24.4,
		},
		{
			name:               "indoor temperature below opportunity threshold",
			indoorTemperature:  25.4,
			outdoorTemperature: 23.0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analyzer := newOutdoorTemperatureGuidanceTestAnalyzer(t, OutdoorConditions{
				TemperatureC:       &test.outdoorTemperature,
				PM2:                &outdoorPM2,
				PM10:               &outdoorPM10,
				AirQualityCategory: "good",
				TemperatureSources: temperatureSources,
				AirQualitySources:  airQualitySources,
			})
			alerts, err := analyzer.Analyze(context.Background(), []SensorReading{{
				Timestamp:   1738886400,
				Temperature: test.indoorTemperature,
				Humidity:    45,
				PM2:         3,
				PM10:        5,
			}})
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			if len(alerts) != 1 || alerts[0].Topic != "general" {
				t.Fatalf("expected original comfort insight, got %#v", alerts)
			}
			if alerts[0].Title != "Home conditions comfortable" ||
				alerts[0].Message != "Temperature is at a comfortable level." {
				t.Fatalf("expected model comfort copy to remain unchanged, got %#v", alerts[0])
			}
			if alerts[0].UsesOutdoorContext || len(alerts[0].Sources) != 0 {
				t.Fatalf("expected unused outdoor context to remain unattributed, got %#v", alerts[0])
			}
		})
	}
}

func TestDailyLimitedAnalyzerRetainsOutdoorCoolingGuidanceAfterModelBudgetIsExhausted(t *testing.T) {
	requestCount := 0
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requestCount++
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"general\",\"kind\":\"insight\",\"severity\":\"info\",\"title\":\"Home conditions comfortable\",\"message\":\"Temperature is at a comfortable level.\",\"uses_outdoor_context\":false}]}"}`))
	}))
	defer modelServer.Close()

	outdoorTemperature := 23.0
	outdoorHumidity := 55.0
	outdoorPM2 := 5.0
	outdoorPM10 := 10.0
	temperatureSources, airQualitySources := outdoorGuidanceTestSources()
	thresholds := defaultAlertThresholds()
	delegate := NewOpenAIAlertAnalyzerWithOutdoor(
		"test-key",
		"test-model",
		"low",
		modelServer.URL,
		3,
		thresholds,
		staticOutdoorContext{conditions: OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			RelativeHumidity:   &outdoorHumidity,
			PM2:                &outdoorPM2,
			PM10:               &outdoorPM10,
			AirQualityCategory: "good",
			TemperatureSources: temperatureSources,
			HumiditySources:    temperatureSources,
			AirQualitySources:  airQualitySources,
		}},
	)
	analyzer := NewDailyLimitedAlertAnalyzer(delegate, 1, 3, thresholds)
	readings := []SensorReading{{
		Timestamp:   1738886400,
		Temperature: 25.8,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}}

	if _, err := analyzer.Analyze(context.Background(), readings); err != nil {
		t.Fatalf("first analysis: %v", err)
	}
	alerts, err := analyzer.Analyze(context.Background(), readings)
	if err != nil {
		t.Fatalf("fallback analysis: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one model request, got %d", requestCount)
	}
	if analyzer.Source() != "rules" {
		t.Fatalf("expected rules source after budget exhaustion, got %q", analyzer.Source())
	}
	if len(alerts) != 1 || alerts[0].Topic != "temperature" ||
		!recommendsOpeningWindows(alerts[0].Title+" "+alerts[0].Message) {
		t.Fatalf("expected deterministic outdoor cooling guidance, got %#v", alerts)
	}
	if !alerts[0].UsesOutdoorContext || len(alerts[0].Sources) != 3 {
		t.Fatalf("expected outdoor attribution on fallback guidance, got %#v", alerts[0])
	}
}

func TestOpenAIAlertAnalyzerAttachesSourcesOnlyWhenOutdoorContextIsUsed(t *testing.T) {
	userInput := ""
	requestBody := []byte(nil)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		var err error
		requestBody, err = io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(requestBody, &payload); err != nil {
			t.Errorf("decode request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, input := range payload.Input {
			if input.Role == "user" && len(input.Content) > 0 {
				userInput = input.Content[0].Text
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"temperature\",\"kind\":\"tip\",\"severity\":\"info\",\"title\":\"Cooler air outside\",\"message\":\"Outdoor air is cooler, so brief ventilation may help.\",\"uses_outdoor_context\":false}]}"}`))
	}))
	defer server.Close()

	outdoorTemperature := 22.0
	outdoorHumidity := 50.0
	outdoorPM2 := 1.0
	outdoorPM10 := 2.0
	privatePostcode := strings.Join([]string{"A", "A", "1", " ", "1", "A", "A"}, "")
	privateLatitude := strings.Join([]string{"51", ".", "507351"}, "")
	privateLongitude := strings.Join([]string{"-0", ".", "127758"}, "")
	outdoorNow := time.Unix(1738886400, 0)
	outdoorProvider := NewOpenMeteoOutdoorProvider(OutdoorProviderConfig{Location: privatePostcode})
	outdoorProvider.now = func() time.Time { return outdoorNow }
	outdoorProvider.latitude = 51.507351
	outdoorProvider.longitude = -0.127758
	outdoorProvider.hasCoordinates = true
	outdoorProvider.latest = OutdoorConditions{
		TemperatureC:       &outdoorTemperature,
		RelativeHumidity:   &outdoorHumidity,
		PM2:                &outdoorPM2,
		PM10:               &outdoorPM10,
		AirQualityCategory: "good",
		FetchedAt:          outdoorNow.UnixMilli(),
		TemperatureSources: []AlertSource{{Title: "Open-Meteo weather", URL: "https://open-meteo.com/en/docs"}},
		HumiditySources:    []AlertSource{{Title: "Open-Meteo weather", URL: "https://open-meteo.com/en/docs"}},
		AirQualitySources: []AlertSource{
			{Title: "Open-Meteo air quality", URL: "https://open-meteo.com/en/docs/air-quality-api"},
			{Title: "CAMS", URL: "https://atmosphere.copernicus.eu/"},
		},
	}
	outdoorProvider.hasLatest = true
	analyzer := NewOpenAIAlertAnalyzerWithOutdoor(
		"test-key",
		"test-model",
		"low",
		server.URL,
		1,
		defaultAlertThresholds(),
		outdoorProvider,
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
	if len(alerts) != 1 || len(alerts[0].Sources) != 3 {
		t.Fatalf("expected outdoor source on insight, got %#v", alerts)
	}
	if !alerts[0].UsesOutdoorContext {
		t.Fatalf("expected validated ventilation advice to force outdoor attribution, got %#v", alerts[0])
	}
	for _, expected := range []string{`"outdoor"`, `"temperature_c":22`, `"relative_humidity":50`, `"pm2":1`, `"pm10":2`} {
		if !strings.Contains(userInput, expected) {
			t.Fatalf("expected model input to contain outdoor metric %s", expected)
		}
	}
	inputParts := strings.SplitN(userInput, "\n", 2)
	if len(inputParts) != 2 {
		t.Fatal("expected model input to contain a JSON telemetry payload")
	}
	var analysisPayload struct {
		Outdoor map[string]any `json:"outdoor"`
	}
	if err := json.Unmarshal([]byte(inputParts[1]), &analysisPayload); err != nil {
		t.Fatal("expected model telemetry payload to be valid JSON")
	}
	allowedOutdoorFields := map[string]struct{}{
		"temperature_c": {}, "relative_humidity": {}, "pm2": {}, "pm10": {}, "air_quality_category": {},
		"temperature_observed_at": {}, "humidity_observed_at": {}, "air_quality_observed_at": {},
		"data_quality": {}, "fetched_at": {},
	}
	for field := range analysisPayload.Outdoor {
		if _, allowed := allowedOutdoorFields[field]; !allowed {
			t.Fatal("model input included a non-metric outdoor field")
		}
	}
	for _, privateValue := range []string{privatePostcode, privateLatitude, privateLongitude} {
		if strings.Contains(string(requestBody), privateValue) {
			t.Fatal("expected the complete model request to exclude private location data")
		}
	}
}

func TestOpenAIAlertAnalyzerRejectsPrivateLocationInGeneratedText(t *testing.T) {
	postcode := strings.Join([]string{"A", "A", "1", " ", "1", "A", "A"}, "")
	coordinates := strings.Join([]string{"51", ".", "51", ", ", "-0", ".", "13"}, "")
	tests := []struct {
		name      string
		title     string
		message   string
		sensitive string
	}{
		{
			name:      "postcode",
			title:     "Local conditions near " + postcode,
			message:   "Keep monitoring indoor air.",
			sensitive: postcode,
		},
		{
			name:      "precise coordinate pair",
			title:     "Local conditions",
			message:   "Outdoor readings were resolved at " + coordinates + ".",
			sensitive: coordinates,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				alertPayload, err := json.Marshal(map[string]any{
					"alerts": []Alert{{
						Topic: "general", Kind: "insight", Severity: "info",
						Title: test.title, Message: test.message,
					}},
				})
				if err != nil {
					t.Error("encode alert payload")
					response.WriteHeader(http.StatusInternalServerError)
					return
				}
				response.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(response).Encode(map[string]string{
					"output_text": string(alertPayload),
				})
			}))
			defer server.Close()

			analyzer := NewOpenAIAlertAnalyzer(
				"test-key",
				"test-model",
				"low",
				server.URL,
				1,
				defaultAlertThresholds(),
			)
			alerts, err := analyzer.Analyze(context.Background(), []SensorReading{{
				Timestamp: 1738886400, Temperature: 22, Humidity: 45,
			}})
			if err != errGeneratedAlertPrivacyCheck || alerts != nil {
				t.Fatal("expected generated private location data to be rejected")
			}
			if strings.Contains(err.Error(), test.sensitive) {
				t.Fatal("expected privacy-check error to exclude private location data")
			}
		})
	}
}

func TestPrivateLocationCheckDecodesURLValues(t *testing.T) {
	privatePostcode := strings.Join([]string{"A", "A", "1", " ", "1", "A", "A"}, "")
	for _, encodedPostcode := range []string{
		strings.ReplaceAll(privatePostcode, " ", "%20"),
		strings.ReplaceAll(privatePostcode, " ", "+"),
	} {
		if !containsPrivateLocation("https://example.invalid/?area=" + encodedPostcode) {
			t.Fatal("expected URL-encoded private location data to be rejected")
		}
	}
}

func TestOpenAIAlertAnalyzerDoesNotExposeErrorResponseBody(t *testing.T) {
	privatePostcode := strings.Join([]string{"A", "A", "1", " ", "1", "A", "A"}, "")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = response.Write([]byte("upstream echoed " + privatePostcode))
	}))
	defer server.Close()

	analyzer := NewOpenAIAlertAnalyzer(
		"test-key",
		"test-model",
		"low",
		server.URL,
		1,
		defaultAlertThresholds(),
	)
	_, err := analyzer.Analyze(context.Background(), []SensorReading{{
		Timestamp: 1738886400, Temperature: 22, Humidity: 45,
	}})
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if strings.Contains(err.Error(), privatePostcode) {
		t.Fatal("upstream error body exposed private location data")
	}
}

func TestOpenAIAlertAnalyzerBlocksWindowAdviceWhenOutdoorAirIsPoor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"temperature\",\"kind\":\"tip\",\"severity\":\"info\",\"title\":\"Open windows now\",\"message\":\"Outdoor air is cooler than the room.\",\"uses_outdoor_context\":true}]}"}`))
	}))
	defer server.Close()

	outdoorTemperature := 20.0
	outdoorHumidity := 50.0
	outdoorPM2 := 45.0
	outdoorPM10 := 60.0
	analyzer := NewOpenAIAlertAnalyzerWithOutdoor(
		"test-key",
		"test-model",
		"low",
		server.URL,
		1,
		defaultAlertThresholds(),
		staticOutdoorContext{conditions: OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			RelativeHumidity:   &outdoorHumidity,
			PM2:                &outdoorPM2,
			PM10:               &outdoorPM10,
			AirQualityCategory: "poor",
			TemperatureSources: []AlertSource{{Title: "Open-Meteo", URL: "https://open-meteo.com/en/docs"}},
			HumiditySources:    []AlertSource{{Title: "Open-Meteo", URL: "https://open-meteo.com/en/docs"}},
			AirQualitySources:  []AlertSource{{Title: "CAMS", URL: "https://atmosphere.copernicus.eu/"}},
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
	if len(alerts) != 1 {
		t.Fatalf("expected one alert, got %#v", alerts)
	}
	if recommendsOpeningWindows(alerts[0].Message) {
		t.Fatalf("expected unsafe window advice to be removed, got %q", alerts[0].Message)
	}
	if !strings.Contains(alerts[0].Message, "Keep windows closed") {
		t.Fatalf("expected explicit closed-window advice, got %q", alerts[0].Message)
	}
	if alerts[0].Title != "Use indoor temperature controls" {
		t.Fatalf("expected unsafe title to be replaced, got %q", alerts[0].Title)
	}
	if !alerts[0].UsesOutdoorContext || len(alerts[0].Sources) != 2 {
		t.Fatalf("expected guarded advice to retain both outdoor sources, got %#v", alerts[0])
	}
}

func TestOpenAIAlertAnalyzerBlocksWindowAdviceWhenOutdoorDataIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"output_text":"{\"alerts\":[{\"topic\":\"temperature\",\"kind\":\"tip\",\"severity\":\"info\",\"title\":\"Cool the room\",\"message\":\"Brief ventilation by opening a window may help.\",\"uses_outdoor_context\":false}]}"}`))
	}))
	defer server.Close()

	analyzer := NewOpenAIAlertAnalyzer(
		"test-key",
		"test-model",
		"low",
		server.URL,
		1,
		defaultAlertThresholds(),
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
	if len(alerts) != 1 || recommendsOpeningWindows(alerts[0].Message) {
		t.Fatalf("expected window advice without outdoor data to be removed, got %#v", alerts)
	}
	if alerts[0].UsesOutdoorContext || len(alerts[0].Sources) != 0 {
		t.Fatalf("expected unavailable outdoor context to remain uncited, got %#v", alerts[0])
	}
}

func TestDeterministicFallbackDoesNotRecommendVentilationWithoutOutdoorContext(t *testing.T) {
	summary := buildAlertSummary([]SensorReading{{
		Timestamp:   1738886400,
		Temperature: 28,
		Humidity:    65,
		PM2:         20,
		PM10:        50,
	}})
	for _, alert := range fallbackAlerts(summary, 3, defaultAlertThresholds()) {
		if recommendsOpeningWindows(alert.Message) {
			t.Fatalf("expected deterministic fallback to avoid ventilation advice, got %#v", alert)
		}
	}
}

func TestRecommendsOpeningWindowsRecognizesCommonVentilationAdvice(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{message: "Open the windows for ten minutes.", want: true},
		{message: "Crack a window to let fresh air in.", want: true},
		{message: "Use brief natural ventilation.", want: true},
		{message: "Air out your home via the patio door.", want: true},
		{message: "Bring outside air in through the French doors.", want: true},
		{message: "Raise the sash for a cross-breeze.", want: true},
		{message: "Vent the room for ten minutes.", want: true},
		{message: "Open the front door to flush the room with outside air.", want: true},
		{message: "Open the roof vents to exchange the air.", want: true},
		{message: "Switch the outside-air intake to maximum.", want: true},
		{message: "Leave the windows ajar.", want: true},
		{message: "The windows should be opened for ten minutes.", want: true},
		{message: "Enable the fresh-air intake.", want: true},
		{message: "Turn on the outside-air supply.", want: true},
		{message: "Run the ventilation system to draw outside air indoors.", want: true},
		{message: "Use unfiltered mechanical ventilation with outdoor air.", want: true},
		{message: "Do not open windows during traffic; open the windows later.", want: true},
		{message: "Keep windows closed while outdoor air is poor.", want: false},
		{message: "Ventilation is not recommended right now.", want: false},
		{message: "Avoid natural ventilation.", want: false},
		{message: "Do not use the patio door.", want: false},
		{message: "Keep the outside-air intake off.", want: false},
		{message: "The outdoor-air intake is closed.", want: false},
		{message: "Do not bring fresh air in.", want: false},
		{message: "Use thermal curtains on the window.", want: false},
		{message: "Run the extractor fan with windows closed.", want: false},
		{message: "Activate the purifier while the windows remain closed.", want: false},
		{message: "Use filtered mechanical ventilation.", want: false},
		{message: "Use fans to increase airflow.", want: false},
		{message: "Use a filtered air purifier.", want: false},
	}
	for _, test := range tests {
		if got := recommendsOpeningWindows(test.message); got != test.want {
			t.Errorf("recommendsOpeningWindows(%q) = %t, want %t", test.message, got, test.want)
		}
	}
}

func TestOutdoorVentilationSafetyBlocksUnsupportedTopicsAndPartialData(t *testing.T) {
	outdoorTemperature := 22.0
	outdoorPM2 := 5.0
	outdoorPM10 := 5.0
	summary := buildAlertSummary([]SensorReading{{
		Timestamp:   1738886400,
		Temperature: 27,
		Humidity:    65,
		PM2:         10,
		PM10:        10,
	}})
	tests := []struct {
		name       string
		alert      Alert
		conditions OutdoorConditions
	}{
		{
			name:  "humidity has no outdoor humidity context",
			alert: Alert{Topic: "humidity", Title: "Humidity high", Message: "Increase ventilation."},
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				PM2:                &outdoorPM2,
				PM10:               &outdoorPM10,
				AirQualityCategory: "good",
			},
		},
		{
			name:  "partial particulate data",
			alert: Alert{Topic: "temperature", Title: "Cool the room", Message: "Crack a window."},
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				PM2:                &outdoorPM2,
				AirQualityCategory: "unknown",
			},
		},
		{
			name:  "partial particulate data with fair category",
			alert: Alert{Topic: "temperature", Title: "Cool the room", Message: "Open a window."},
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				PM2:                &outdoorPM2,
				AirQualityCategory: "fair",
			},
		},
		{
			name:  "category only cannot establish cleaner outdoor air",
			alert: Alert{Topic: "air_quality", Title: "Clear particles", Message: "Bring outside air in."},
			conditions: OutdoorConditions{
				TemperatureC:       &outdoorTemperature,
				AirQualityCategory: "good",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := enforceOutdoorVentilationSafety(
				&test.alert,
				summary,
				test.conditions,
				defaultAlertThresholds(),
			)
			if decision != ventilationBlocked || recommendsOpeningWindows(test.alert.Title+" "+test.alert.Message) {
				t.Fatalf("expected unsupported ventilation to be blocked, got %#v", test.alert)
			}
		})
	}
}

func TestOutdoorVentilationSafetyBlocksWorseAirAndUnsuitableTemperature(t *testing.T) {
	summary := buildAlertSummary([]SensorReading{{
		Timestamp:   1738886400,
		Temperature: 27,
		Humidity:    45,
		PM2:         10,
		PM10:        10,
	}})
	safeTemperature := 22.0
	worsePM2 := 11.0
	lowerPM10 := 5.0
	lowerPM2 := 5.0
	unsuitableTemperature := 35.0
	tests := []struct {
		name       string
		conditions OutdoorConditions
	}{
		{
			name: "outdoor PM is worse despite fair category",
			conditions: OutdoorConditions{
				TemperatureC:       &safeTemperature,
				PM2:                &worsePM2,
				PM10:               &lowerPM10,
				AirQualityCategory: "fair",
			},
		},
		{
			name: "outdoor temperature moves farther from comfort",
			conditions: OutdoorConditions{
				TemperatureC:       &unsuitableTemperature,
				PM2:                &lowerPM2,
				PM10:               &lowerPM10,
				AirQualityCategory: "good",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alert := Alert{Topic: "temperature", Title: "Cool the room", Message: "Let fresh air in."}
			decision := enforceOutdoorVentilationSafety(
				&alert,
				summary,
				test.conditions,
				defaultAlertThresholds(),
			)
			if decision != ventilationBlocked || recommendsOpeningWindows(alert.Title+" "+alert.Message) {
				t.Fatalf("expected unsafe outdoor conditions to block ventilation, got %#v", alert)
			}
		})
	}
}

func TestOutdoorVentilationSafetyTreatsDeclaredOutdoorRelianceConservatively(t *testing.T) {
	outdoorTemperature := 20.0
	outdoorPM2 := 45.0
	outdoorPM10 := 60.0
	summary := buildAlertSummary([]SensorReading{{
		Timestamp: 1738886400, Temperature: 27, Humidity: 45, PM2: 3, PM10: 5,
	}})
	alert := Alert{
		Topic:              "temperature",
		Title:              "Cooler conditions outside",
		Message:            "Route the outside intake through the patio access.",
		UsesOutdoorContext: true,
	}
	decision := enforceOutdoorVentilationSafety(
		&alert,
		summary,
		OutdoorConditions{
			TemperatureC:       &outdoorTemperature,
			PM2:                &outdoorPM2,
			PM10:               &outdoorPM10,
			AirQualityCategory: "poor",
		},
		defaultAlertThresholds(),
	)
	if decision != ventilationBlocked || alert.Title != "Use indoor temperature controls" {
		t.Fatalf("expected declared outdoor reliance to be blocked conservatively, got %#v", alert)
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

func TestNormalizeAlertsCapsLongCopy(t *testing.T) {
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
	if alerts[0].Message == message || utf8.RuneCountInString(alerts[0].Message) > alertMessageMaxLength ||
		!strings.HasSuffix(alerts[0].Message, "…") {
		t.Fatalf("expected concise truncated message, got %q", alerts[0].Message)
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
