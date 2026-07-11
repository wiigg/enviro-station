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

func TestFirstReadingRequestsWarmupWithoutReportingAnEvent(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp:   1738886400,
		Temperature: 22,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	})

	if trigger != "warmup" {
		t.Fatalf("expected first reading to warm insights, got %q", trigger)
	}
}

func TestShouldTriggerFromReadingUsesRollingTenMinuteChange(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	baseTimestamp := int64(1738886400)

	scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp, PM2: 2, PM10: 4})
	if scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 3*60, PM2: 4, PM10: 4}) {
		t.Fatalf("expected small partial PM2 change not to trigger")
	}
	if scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 6*60, PM2: 6, PM10: 4}) {
		t.Fatalf("expected cumulative PM2 change below threshold not to trigger")
	}
	if !scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 9*60, PM2: 7.2, PM10: 4}) {
		t.Fatalf("expected cumulative PM2 change over ten-minute window to trigger")
	}
}

func TestShouldTriggerFromReadingIgnoresDelayedReadings(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	baseTimestamp := int64(1738886400)

	scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp, PM2: 3, PM10: 5})
	if scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp - 60, PM2: 30, PM10: 50}) {
		t.Fatalf("expected delayed reading not to trigger")
	}
	if scheduler.lastReading.Timestamp != baseTimestamp {
		t.Fatalf("expected delayed reading not to rewind baseline, got %d", scheduler.lastReading.Timestamp)
	}
	if scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 60, PM2: 3.2, PM10: 5.1}) {
		t.Fatalf("expected stable reading after delayed batch not to trigger")
	}
}

func TestSeverityEscalationBypassesMaterialChangeCooldown(t *testing.T) {
	config := testInsightsSchedulerConfig()
	config.PM2Threshold = 8
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, config)
	baseTimestamp := int64(1738886400)

	scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp, PM2: 7, PM10: 5})
	if !scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 60, PM2: 8.1, PM10: 5}) {
		t.Fatalf("expected warning threshold crossing to trigger")
	}
	if !scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 120, PM2: 15.1, PM10: 5}) {
		t.Fatalf("expected critical escalation inside cooldown to trigger")
	}
}

func TestNeedsScheduledRefreshUsesSnapshotAge(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	now := time.Now()
	scheduler.hasSnapshot = true
	scheduler.snapshot.GeneratedAt = now.UnixMilli()

	if scheduler.needsScheduledRefresh(now) {
		t.Fatalf("expected fresh event snapshot to defer scheduled refresh")
	}
	scheduler.snapshot.GeneratedAt = now.Add(-scheduler.config.RefreshInterval).UnixMilli()
	if !scheduler.needsScheduledRefresh(now) {
		t.Fatalf("expected expired snapshot to require scheduled refresh")
	}
}

func TestIntervalRecomputeSkipsUnchangedTelemetry(t *testing.T) {
	store := &fakeStore{latest: []SensorReading{{Timestamp: 1738886400, PM2: 3, PM10: 5}}}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(store, analyzer, testInsightsSchedulerConfig())

	scheduler.recompute("startup")
	scheduler.recompute("interval")

	if analyzer.calls != 1 {
		t.Fatalf("expected unchanged scheduled refresh to skip analysis, got %d calls", analyzer.calls)
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

func TestRecomputeSeedsEventBaselineFromAnalyzedReadings(t *testing.T) {
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
	scheduler := NewInsightsScheduler(store, analyzer, testInsightsSchedulerConfig())

	scheduler.recompute("startup")

	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   1738886460,
		Temperature: 22.1,
		Humidity:    30,
		PM2:         4,
		PM10:        7,
	}) {
		t.Fatalf("expected first fresh live reading to invalidate stale startup analysis")
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
