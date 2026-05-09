package server

import (
	"testing"
	"time"
)

func testInsightsSchedulerConfig() InsightsSchedulerConfig {
	return InsightsSchedulerConfig{
		AnalysisLimit:            900,
		RefreshInterval:          time.Hour,
		EventMinInterval:         5 * time.Minute,
		PM2Threshold:             100,
		PM10Threshold:            200,
		PM2DeltaTrigger:          5,
		PM10DeltaTrigger:         15,
		HumidityLowThreshold:     40,
		HumidityHighThreshold:    60,
		HumidityDeltaTrigger:     8,
		TemperatureLowThreshold:  18,
		TemperatureHighThreshold: 26,
		TemperatureDeltaTrigger:  1.5,
		AnalyzeTimeout:           5 * time.Second,
	}
}

func TestShouldTriggerFromReadingPM2IncreaseJump(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886400000,
		PM2:       3,
		PM10:      5,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886460000,
		PM2:       8.2,
		PM10:      5.1,
	}) {
		t.Fatalf("expected rapid PM2 increase to trigger recompute")
	}
}

func TestShouldTriggerFromReadingPM2DecreaseJump(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886400000,
		PM2:       15,
		PM10:      12,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886460000,
		PM2:       8.5,
		PM10:      11.7,
	}) {
		t.Fatalf("expected material PM2 drop to trigger recompute")
	}
}

func TestShouldTriggerFromReadingThrottlesEventBursts(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886400000,
		PM2:       2,
		PM10:      4,
	})

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886460000,
		PM2:       8,
		PM10:      4,
	}) {
		t.Fatalf("expected first PM2 jump to trigger recompute")
	}

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886520000,
		PM2:       14,
		PM10:      4,
	}) {
		t.Fatalf("expected second jump inside min interval to be throttled")
	}
}

func TestShouldTriggerFromReadingTemperatureImproves(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886400000,
		Temperature: 30.8,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886460000,
		Temperature: 25.0,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected rapid temperature improvement to trigger recompute")
	}
}

func TestShouldTriggerFromReadingHumidityWorsens(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886400000,
		Temperature: 22,
		Humidity:    42,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886460000,
		Temperature: 22,
		Humidity:    32,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected rapid humidity drop to trigger recompute")
	}
}

func TestShouldTriggerFromReadingTemperatureCriticalBoundary(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886400000,
		Temperature: 30.8,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886460000,
		Temperature: 29.9,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected critical-to-warn temperature change to trigger recompute")
	}
}

func TestShouldTriggerFromReadingHumidityCriticalBoundary(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886400000,
		Temperature: 22,
		Humidity:    25.4,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886460000,
		Temperature: 22,
		Humidity:    24.9,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatalf("expected warn-to-critical humidity change to trigger recompute")
	}
}

func TestShouldTriggerFromReadingPMCriticalBoundary(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886400000,
		PM2:       14.9,
		PM10:      5,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886460000,
		PM2:       15.1,
		PM10:      5,
	}) {
		t.Fatalf("expected warn-to-critical PM2 change to trigger recompute")
	}
}

func TestRecomputePrefersNewerLiveReadings(t *testing.T) {
	store := &fakeStore{
		latest: []SensorReading{
			{
				Timestamp:   1738886400,
				Temperature: 30.8,
				Humidity:    23,
				PM2:         4,
				PM10:        7,
			},
		},
	}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(
		store,
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			return []SensorReading{
				{
					Timestamp:   1738886460,
					Temperature: 22.1,
					Humidity:    30,
					PM2:         4,
					PM10:        7,
				},
			}
		}),
	)

	scheduler.recompute("test")

	if analyzer.calls != 1 {
		t.Fatalf("expected one analysis call, got %d", analyzer.calls)
	}
	if len(analyzer.lastReadings) != 1 {
		t.Fatalf("expected one live reading for analysis, got %d", len(analyzer.lastReadings))
	}
	if got := analyzer.lastReadings[0].Temperature; got != 22.1 {
		t.Fatalf("expected live temperature to drive insights, got %.1f", got)
	}
}

func TestEventRecomputeUsesLiveReadingsWithoutDurableRead(t *testing.T) {
	store := &fakeStore{}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(
		store,
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			return []SensorReading{
				{
					Timestamp:   1738886460,
					Temperature: 22.1,
					Humidity:    30,
					PM2:         4,
					PM10:        7,
				},
			}
		}),
	)

	scheduler.recompute("event")

	if store.latestCalls != 0 {
		t.Fatalf("expected live event recompute to avoid durable reads, got %d", store.latestCalls)
	}
	if analyzer.calls != 1 {
		t.Fatalf("expected one analysis call, got %d", analyzer.calls)
	}
}
