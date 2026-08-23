package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingAlertAnalyzer struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
}

type scriptedAlertAnalyzer struct {
	mu        sync.Mutex
	calls     int
	errors    []error
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
}

type fixedAlertAnalyzer struct {
	alerts []Alert
}

func (analyzer *fixedAlertAnalyzer) Analyze(_ context.Context, _ []SensorReading) ([]Alert, error) {
	return cloneAlerts(analyzer.alerts), nil
}

func (*fixedAlertAnalyzer) Source() string {
	return "test"
}

type controlledOutdoorMonitor struct {
	mu         sync.RWMutex
	conditions OutdoorConditions
	ready      bool
	onUpdate   func(initial bool)
}

func (monitor *controlledOutdoorMonitor) Snapshot() (OutdoorConditions, bool) {
	monitor.mu.RLock()
	defer monitor.mu.RUnlock()
	return monitor.conditions, monitor.ready
}

func (monitor *controlledOutdoorMonitor) Start(
	_ context.Context,
	onUpdate func(initial bool),
) {
	monitor.mu.Lock()
	monitor.onUpdate = onUpdate
	monitor.mu.Unlock()
}

func (monitor *controlledOutdoorMonitor) publishInitial(conditions OutdoorConditions) {
	monitor.mu.Lock()
	monitor.conditions = conditions
	monitor.ready = true
	onUpdate := monitor.onUpdate
	monitor.mu.Unlock()
	if onUpdate != nil {
		onUpdate(true)
	}
}

func (analyzer *scriptedAlertAnalyzer) Analyze(_ context.Context, _ []SensorReading) ([]Alert, error) {
	analyzer.mu.Lock()
	callIndex := analyzer.calls
	analyzer.calls++
	var callErr error
	if callIndex < len(analyzer.errors) {
		callErr = analyzer.errors[callIndex]
	}
	shouldBlock := callIndex == 0 && analyzer.release != nil
	analyzer.mu.Unlock()
	if shouldBlock {
		analyzer.startOnce.Do(func() { close(analyzer.started) })
		<-analyzer.release
	}
	if callErr != nil {
		return nil, callErr
	}
	return []Alert{{Kind: "insight", Severity: "info", Title: "Test", Message: "Test."}}, nil
}

func (analyzer *scriptedAlertAnalyzer) Source() string {
	return "test"
}

func (analyzer *scriptedAlertAnalyzer) callCount() int {
	analyzer.mu.Lock()
	defer analyzer.mu.Unlock()
	return analyzer.calls
}

func newBlockingAlertAnalyzer() *blockingAlertAnalyzer {
	return &blockingAlertAnalyzer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (analyzer *blockingAlertAnalyzer) Analyze(_ context.Context, _ []SensorReading) ([]Alert, error) {
	analyzer.mu.Lock()
	analyzer.calls++
	analyzer.active++
	if analyzer.active > analyzer.maxActive {
		analyzer.maxActive = analyzer.active
	}
	analyzer.mu.Unlock()
	analyzer.startOnce.Do(func() { close(analyzer.started) })
	<-analyzer.release
	analyzer.mu.Lock()
	analyzer.active--
	analyzer.mu.Unlock()
	return []Alert{{Kind: "insight", Severity: "info", Title: "Test", Message: "Test."}}, nil
}

func (analyzer *blockingAlertAnalyzer) Source() string {
	return "test"
}

func (analyzer *blockingAlertAnalyzer) stats() (int, int) {
	analyzer.mu.Lock()
	defer analyzer.mu.Unlock()
	return analyzer.calls, analyzer.maxActive
}

func waitForInsightsSchedulerIdle(t *testing.T, scheduler *InsightsScheduler) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		scheduler.mu.RLock()
		idle := !scheduler.running && scheduler.pendingTrigger == ""
		scheduler.mu.RUnlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for insights scheduler to become idle")
}

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

func TestDefaultInsightsCadence(t *testing.T) {
	config := DefaultInsightsSchedulerConfig()

	if config.RefreshInterval != time.Hour {
		t.Fatalf("expected hourly scheduled refresh, got %s", config.RefreshInterval)
	}
	if config.EventMinInterval != 30*time.Minute {
		t.Fatalf("expected 30 minute material-change cooldown, got %s", config.EventMinInterval)
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

func TestFirstReadingWarmsRestoredSnapshotWithoutOutdoorContext(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	scheduler.hasSnapshot = true
	scheduler.snapshot.GeneratedAt = time.Now().UnixMilli()

	trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp: 1738886400, Temperature: 30.1, Humidity: 45, PM2: 3, PM10: 5,
	})
	if trigger != "warmup" {
		t.Fatalf("expected first restored-snapshot reading to refresh the indoor baseline, got %q", trigger)
	}
}

func TestFirstReadingRefreshesRestoredSnapshotWhenOutdoorContextIsReady(t *testing.T) {
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		&fakeAlertAnalyzer{},
		testInsightsSchedulerConfig(),
		WithInsightsOutdoorContext(staticOutdoorContext{conditions: OutdoorConditions{
			AirQualityCategory: "good",
		}}),
	)
	scheduler.hasSnapshot = true

	trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp:   1738886400,
		Temperature: 22,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	})

	if trigger != "warmup" {
		t.Fatalf("expected first reading to warm restored insight with outdoor context, got %q", trigger)
	}
}

func TestInitialOutdoorReadyRefreshesRestoredSnapshotAfterEarlyReading(t *testing.T) {
	reading := SensorReading{
		Timestamp: 1738886400, Temperature: 27, Humidity: 45, PM2: 3, PM10: 5,
	}
	monitor := &controlledOutdoorMonitor{}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading { return []SensorReading{reading} }),
		WithInsightsOutdoorContext(monitor),
	)
	scheduler.hasSnapshot = true
	scheduler.snapshot.GeneratedAt = time.Now().UnixMilli()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)

	if trigger := scheduler.triggerFromReading(reading); trigger != "warmup" {
		t.Fatalf("expected early reading to warm the restored snapshot safely, got %q", trigger)
	}
	monitor.publishInitial(OutdoorConditions{AirQualityCategory: "good"})
	waitForInsightsSchedulerIdle(t, scheduler)

	if analyzer.calls != 1 {
		t.Fatalf("expected initial outdoor readiness to refresh restored snapshot, got %d calls", analyzer.calls)
	}
	if snapshot, ok := scheduler.Snapshot(1); !ok || snapshot.Trigger != "outdoor_initial" {
		t.Fatalf("expected initial-outdoor snapshot, got %#v", snapshot)
	}
}

func TestInitialOutdoorRetryReanalyzesUnchangedTelemetry(t *testing.T) {
	reading := SensorReading{
		Timestamp: 1738886400, Temperature: 27, Humidity: 45, PM2: 3, PM10: 5,
	}
	monitor := &controlledOutdoorMonitor{}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading { return []SensorReading{reading} }),
		WithInsightsOutdoorContext(monitor),
	)
	scheduler.hasSnapshot = true
	scheduler.snapshot.GeneratedAt = time.Now().UnixMilli()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)
	scheduler.recompute("warmup")
	if analyzer.calls != 1 {
		t.Fatal("expected first analysis to complete without unavailable outdoor context")
	}

	monitor.publishInitial(OutdoorConditions{AirQualityCategory: "good"})
	waitForInsightsSchedulerIdle(t, scheduler)

	if analyzer.calls != 2 {
		t.Fatalf("expected newly ready outdoor context to reanalyze unchanged telemetry, got %d calls", analyzer.calls)
	}
	if snapshot, ok := scheduler.Snapshot(1); !ok || snapshot.Trigger != "outdoor_initial" {
		t.Fatalf("expected initial-outdoor retry snapshot, got %#v", snapshot)
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

func TestUnavailableParticulateValuesDoNotTriggerInsights(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	baseTimestamp := int64(1738886400)

	scheduler.triggerFromReading(SensorReading{
		Timestamp:   baseTimestamp,
		Temperature: 22,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
		PMAvailable: boolPtr(false),
	})
	trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp:   baseTimestamp + 60,
		Temperature: 22,
		Humidity:    45,
		PM2:         100,
		PM10:        150,
		PMAvailable: boolPtr(false),
	})

	if trigger != "" {
		t.Fatalf("expected cached PM changes to be ignored, got trigger %q", trigger)
	}
}

func TestParticulateAvailabilityChangeRefreshesInsights(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	baseTimestamp := int64(1738886400)

	scheduler.triggerFromReading(SensorReading{Timestamp: baseTimestamp, PM2: 3, PM10: 5})
	trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp:   baseTimestamp + 60,
		PM2:         3,
		PM10:        5,
		PMAvailable: boolPtr(false),
	})

	if trigger != "event" {
		t.Fatalf("expected sensor availability change to refresh insights, got %q", trigger)
	}
}

func TestParticulateLossBypassesActiveCooldown(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	baseTimestamp := int64(1738886400)

	scheduler.triggerFromReading(SensorReading{
		Timestamp: baseTimestamp, Temperature: 25.9, Humidity: 45, PM2: 3, PM10: 5,
	})
	if trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp: baseTimestamp + 60, Temperature: 26.1, Humidity: 45, PM2: 3, PM10: 5,
	}); trigger != "event" {
		t.Fatalf("expected temperature crossing to start cooldown, got %q", trigger)
	}
	if trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp:   baseTimestamp + 120,
		Temperature: 26.1,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
		PMAvailable: boolPtr(false),
	}); trigger != "event" {
		t.Fatalf("expected PM sensor loss to bypass active cooldown, got %q", trigger)
	}
}

func TestParticulateRecoveryPersistsUntilCooldownExpires(t *testing.T) {
	config := testInsightsSchedulerConfig()
	var liveMu sync.RWMutex
	live := []SensorReading{{
		Timestamp: 1738886400, Temperature: 22, Humidity: 45, PM2: 3, PM10: 5,
	}}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		&fakeAlertAnalyzer{},
		config,
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			liveMu.RLock()
			defer liveMu.RUnlock()
			return append([]SensorReading(nil), live...)
		}),
	)
	scheduler.hasSnapshot = true
	scheduler.triggerFromReading(live[0])
	loss := SensorReading{
		Timestamp:   1738886460,
		Temperature: 22,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
		PMAvailable: boolPtr(false),
	}
	liveMu.Lock()
	live = append(live, loss)
	liveMu.Unlock()
	if trigger := scheduler.triggerFromReading(loss); trigger != "event" {
		t.Fatalf("expected PM loss event, got %q", trigger)
	}
	scheduler.recompute("event")

	recovery := SensorReading{
		Timestamp: 1738886520, Temperature: 22, Humidity: 45, PM2: 3, PM10: 5,
	}
	if trigger := scheduler.triggerFromReading(recovery); trigger != "" {
		t.Fatalf("expected noncritical PM recovery inside cooldown to coalesce, got %q", trigger)
	}
	scheduler.lastEventTrigger = time.Now().Add(-scheduler.config.EventMinInterval)
	recovery.Timestamp += 60
	if trigger := scheduler.triggerFromReading(recovery); trigger != "event" {
		t.Fatalf("expected persistent PM recovery after cooldown, got %q", trigger)
	}
}

func TestCriticalParticulateRecoveryBypassesActiveCooldown(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	baseTimestamp := int64(1738886400)

	scheduler.triggerFromReading(SensorReading{
		Timestamp:   baseTimestamp,
		Temperature: 25.9,
		Humidity:    45,
		PMAvailable: boolPtr(false),
	})
	if trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp:   baseTimestamp + 60,
		Temperature: 26.1,
		Humidity:    45,
		PMAvailable: boolPtr(false),
	}); trigger != "event" {
		t.Fatalf("expected temperature crossing to start cooldown, got %q", trigger)
	}
	if trigger := scheduler.triggerFromReading(SensorReading{
		Timestamp: baseTimestamp + 120, Temperature: 26.1, Humidity: 45, PM2: 15.1, PM10: 5,
	}); trigger != "event" {
		t.Fatalf("expected critical PM recovery to bypass active cooldown, got %q", trigger)
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

func TestWarningThresholdClearWaitsForCooldownThenTriggers(t *testing.T) {
	config := testInsightsSchedulerConfig()
	config.PM2Threshold = 8
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, config)
	baseTimestamp := int64(1738886400)

	scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp, PM2: 7, PM10: 5})
	if !scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 60, PM2: 8.1, PM10: 5}) {
		t.Fatal("expected crossing above warning threshold to trigger")
	}
	if scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 120, PM2: 7.9, PM10: 5}) {
		t.Fatal("expected warning clear inside cooldown to be coalesced")
	}

	scheduler.lastEventTrigger = time.Now().Add(-scheduler.config.EventMinInterval)
	if !scheduler.shouldTriggerFromReading(SensorReading{Timestamp: baseTimestamp + 180, PM2: 7.8, PM10: 5}) {
		t.Fatal("expected a persistent warning clear to trigger after cooldown")
	}
}

func TestWarningBoundaryChatterDoesNotTriggerRepeatedEvents(t *testing.T) {
	config := testInsightsSchedulerConfig()
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, config)
	baseTimestamp := int64(1738886400)

	scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   baseTimestamp,
		Temperature: 25.9,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	})
	if !scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp:   baseTimestamp + 30,
		Temperature: 26.1,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}) {
		t.Fatal("expected initial high-temperature crossing to trigger")
	}

	for index, temperature := range []float64{25.9, 26.1, 25.8, 26.05, 25.95, 26.1} {
		if scheduler.shouldTriggerFromReading(SensorReading{
			Timestamp:   baseTimestamp + int64(index+2)*30,
			Temperature: temperature,
			Humidity:    45,
			PM2:         3,
			PM10:        5,
		}) {
			t.Fatalf("expected boundary chatter at %.2fC to be coalesced", temperature)
		}
	}
}

func TestFailedEventRetriesPersistentCrossingAfterCooldown(t *testing.T) {
	config := testInsightsSchedulerConfig()
	var liveMu sync.RWMutex
	live := []SensorReading{{
		Timestamp:   1738886400,
		Temperature: 25.9,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}}
	analyzer := &fakeAlertAnalyzer{err: errors.New("temporary model failure")}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		analyzer,
		config,
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			liveMu.RLock()
			defer liveMu.RUnlock()
			return append([]SensorReading(nil), live...)
		}),
	)
	scheduler.hasSnapshot = true
	scheduler.triggerFromReading(live[0])
	crossing := SensorReading{
		Timestamp:   1738886430,
		Temperature: 26.1,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}
	liveMu.Lock()
	live = append(live, crossing)
	liveMu.Unlock()
	if trigger := scheduler.triggerFromReading(crossing); trigger != "event" {
		t.Fatalf("expected threshold crossing event, got %q", trigger)
	}
	scheduler.recompute("event")
	scheduler.lastEventTrigger = time.Now().Add(-scheduler.config.EventMinInterval)

	persistent := crossing
	persistent.Timestamp += 30
	if trigger := scheduler.triggerFromReading(persistent); trigger != "event" {
		t.Fatalf("expected failed persistent crossing to retry, got %q", trigger)
	}
}

func TestFailedEventsRespectCooldownThenRetryLatestConditions(t *testing.T) {
	tests := []struct {
		name       string
		event      SensorReading
		retryAfter int64
	}{
		{
			name: "particulate loss",
			event: SensorReading{
				Timestamp: 1738886460, Temperature: 22, Humidity: 45,
				PM2: 3, PM10: 5, PMAvailable: boolPtr(false),
			},
			retryAfter: 120,
		},
		{
			name: "critical particulate escalation",
			event: SensorReading{
				Timestamp: 1738886460, Temperature: 22, Humidity: 45, PM2: 15.1, PM10: 5,
			},
			retryAfter: 120,
		},
		{
			name: "delta only change after trend window expires",
			event: SensorReading{
				Timestamp: 1738886460, Temperature: 22, Humidity: 45, PM2: 8.2, PM10: 5,
			},
			retryAfter: int64((31 * time.Minute) / time.Second),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseline := SensorReading{
				Timestamp: 1738886400, Temperature: 22, Humidity: 45, PM2: 3, PM10: 5,
			}
			live := []SensorReading{baseline, test.event}
			scheduler := NewInsightsScheduler(
				&fakeStore{},
				&fakeAlertAnalyzer{err: errors.New("temporary model failure")},
				testInsightsSchedulerConfig(),
				WithInsightsLiveReadings(func(_ int) []SensorReading {
					return append([]SensorReading(nil), live...)
				}),
			)
			scheduler.hasSnapshot = true
			scheduler.triggerFromReading(baseline)
			if trigger := scheduler.triggerFromReading(test.event); trigger != "event" {
				t.Fatalf("expected initial event, got %q", trigger)
			}
			scheduler.recompute("event")

			insideCooldown := test.event
			insideCooldown.Timestamp += 30
			if trigger := scheduler.triggerFromReading(insideCooldown); trigger != "" {
				t.Fatalf("expected failed event retry to remain bounded by cooldown, got %q", trigger)
			}

			scheduler.lastEventTrigger = time.Now().Add(-scheduler.config.EventMinInterval)
			afterCooldown := test.event
			afterCooldown.Timestamp += test.retryAfter
			if afterCooldown.Timestamp <= insideCooldown.Timestamp {
				afterCooldown.Timestamp = insideCooldown.Timestamp + 1
			}
			if trigger := scheduler.triggerFromReading(afterCooldown); trigger != "event" {
				t.Fatalf("expected latest conditions to retry after cooldown, got %q", trigger)
			}
		})
	}
}

func TestOlderEventFailureCannotMarkNewGenerationFailed(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	scheduler.hasSnapshot = true
	scheduler.triggerFromReading(SensorReading{
		DeviceID: "device-a", Timestamp: 100, Temperature: 25.9, Humidity: 45, PM2: 3, PM10: 5,
	})
	scheduler.triggerFromReading(SensorReading{
		DeviceID: "device-a", Timestamp: 102, Temperature: 26.1, Humidity: 45, PM2: 3, PM10: 5,
	})
	olderGeneration := scheduler.acceptedGeneration

	scheduler.triggerFromReading(SensorReading{
		DeviceID: "device-b", Timestamp: 100, Temperature: 25.9, Humidity: 45, PM2: 3, PM10: 5,
	})
	scheduler.lastEventTrigger = time.Time{}
	scheduler.triggerFromReading(SensorReading{
		DeviceID: "device-b", Timestamp: 102, Temperature: 26.1, Humidity: 45, PM2: 3, PM10: 5,
	})
	newerGeneration := scheduler.acceptedGeneration
	if newerGeneration == 0 || newerGeneration == olderGeneration {
		t.Fatalf("expected a distinct monotonic event generation, old=%d new=%d", olderGeneration, newerGeneration)
	}

	scheduler.failEventAttempt(olderGeneration)
	if scheduler.acceptedGeneration != newerGeneration || scheduler.acceptedFailed {
		t.Fatal("expected an older failed attempt not to affect the newer accepted event")
	}
}

func TestSuccessfulOutdoorAnalysisReportsSuppressedClear(t *testing.T) {
	config := testInsightsSchedulerConfig()
	var liveMu sync.RWMutex
	live := []SensorReading{{
		Timestamp:   1738886400,
		Temperature: 25.9,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		&fakeAlertAnalyzer{},
		config,
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			liveMu.RLock()
			defer liveMu.RUnlock()
			return append([]SensorReading(nil), live...)
		}),
	)
	scheduler.hasSnapshot = true
	scheduler.triggerFromReading(live[0])
	warning := SensorReading{
		Timestamp:   1738886430,
		Temperature: 26.1,
		Humidity:    45,
		PM2:         3,
		PM10:        5,
	}
	if trigger := scheduler.triggerFromReading(warning); trigger != "event" {
		t.Fatalf("expected threshold crossing event, got %q", trigger)
	}
	clear := warning
	clear.Timestamp += 30
	clear.Temperature = 25.8
	if trigger := scheduler.triggerFromReading(clear); trigger != "" {
		t.Fatalf("expected clear inside cooldown to be coalesced, got %q", trigger)
	}
	liveMu.Lock()
	live = append(live, warning, clear)
	liveMu.Unlock()
	scheduler.recompute("outdoor")
	scheduler.lastEventTrigger = time.Now().Add(-scheduler.config.EventMinInterval)

	stillClear := clear
	stillClear.Timestamp += 30
	if trigger := scheduler.triggerFromReading(stillClear); trigger != "" {
		t.Fatalf("expected successful outdoor analysis to report the clear, got %q", trigger)
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

func TestScheduledRefreshBacksOffAfterAnAttempt(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	now := time.Now()
	scheduler.hasSnapshot = true
	scheduler.snapshot.GeneratedAt = now.Add(-scheduler.config.RefreshInterval).UnixMilli()
	scheduler.lastIntervalAttempt = now.Add(-10 * time.Minute)

	if scheduler.needsScheduledRefresh(now) {
		t.Fatal("expected a failed scheduled attempt to back off")
	}
	scheduler.lastIntervalAttempt = now.Add(-insightsScheduleRetryInterval)
	if !scheduler.needsScheduledRefresh(now) {
		t.Fatal("expected scheduled refresh after retry interval")
	}
}

func TestScheduledWarmupBacksOffAfterAnAttempt(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	now := time.Now()
	scheduler.lastIntervalAttempt = now.Add(-10 * time.Minute)

	if scheduler.needsScheduledRefresh(now) {
		t.Fatal("expected an unsuccessful warmup attempt to back off")
	}
}

func TestScheduledRetryBackoffIsArmedByDueTriggers(t *testing.T) {
	for _, trigger := range []string{"startup", "warmup", "interval"} {
		t.Run(trigger, func(t *testing.T) {
			scheduler := NewInsightsScheduler(
				&fakeStore{},
				&fakeAlertAnalyzer{},
				testInsightsSchedulerConfig(),
			)
			scheduler.running = true
			scheduler.requestRecompute(trigger)
			if scheduler.lastIntervalAttempt.IsZero() {
				t.Fatalf("expected %s trigger to arm scheduled retry backoff", trigger)
			}
		})
	}
}

func TestWarmupRequestRespectsActiveRetryBackoff(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())
	scheduler.lastIntervalAttempt = time.Now()
	scheduler.requestRecompute("warmup")

	scheduler.mu.RLock()
	running := scheduler.running
	pending := scheduler.pendingTrigger
	scheduler.mu.RUnlock()
	if running || pending != "" {
		t.Fatalf("expected warmup inside retry backoff to be suppressed, running=%t pending=%q", running, pending)
	}
}

func TestFirstUsableLiveReadingBypassesEmptyStartupBackoff(t *testing.T) {
	var liveMu sync.RWMutex
	var live []SensorReading
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			liveMu.RLock()
			defer liveMu.RUnlock()
			return append([]SensorReading(nil), live...)
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)
	waitForInsightsSchedulerIdle(t, scheduler)
	if analyzer.calls != 0 {
		t.Fatalf("expected empty startup not to analyze, got %d calls", analyzer.calls)
	}

	reading := SensorReading{
		Timestamp: 1738886400, Temperature: 22, Humidity: 45, PM2: 3, PM10: 5,
	}
	liveMu.Lock()
	live = []SensorReading{reading}
	liveMu.Unlock()
	scheduler.OnReading(reading)
	waitForInsightsSchedulerIdle(t, scheduler)

	if analyzer.calls != 1 {
		t.Fatalf("expected first usable live reading to bypass startup backoff, got %d calls", analyzer.calls)
	}
	if snapshot, ok := scheduler.Snapshot(1); !ok || snapshot.Trigger != "warmup" {
		t.Fatalf("expected first-live-reading warmup snapshot, got %#v", snapshot)
	}
}

func TestWarmupDoesNotImmediatelyRetryStartupAttemptOfSameSample(t *testing.T) {
	reading := SensorReading{
		Timestamp: 1738886400, Temperature: 22, Humidity: 45, PM2: 3, PM10: 5,
	}
	analyzer := &scriptedAlertAnalyzer{
		errors:  []error{errors.New("temporary model failure")},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading { return []SensorReading{reading} }),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)
	select {
	case <-analyzer.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for startup analysis")
	}
	scheduler.OnReading(reading)
	close(analyzer.release)
	waitForInsightsSchedulerIdle(t, scheduler)

	if calls := analyzer.callCount(); calls != 1 {
		t.Fatalf("expected failed startup sample to respect retry backoff, got %d calls", calls)
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

func TestWarmupRecomputeSkipsTelemetryAlreadyAnalyzedByStartup(t *testing.T) {
	store := &fakeStore{latest: []SensorReading{{Timestamp: 1738886400, PM2: 3, PM10: 5}}}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(store, analyzer, testInsightsSchedulerConfig())

	scheduler.recompute("startup")
	scheduler.recompute("warmup")

	if analyzer.calls != 1 {
		t.Fatalf("expected unchanged warmup telemetry to skip analysis, got %d calls", analyzer.calls)
	}
}

func TestEventRecomputeSkipsTelemetryAlreadyAnalyzedByPendingRun(t *testing.T) {
	store := &fakeStore{latest: []SensorReading{{Timestamp: 1738886400, PM2: 3, PM10: 5}}}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(store, analyzer, testInsightsSchedulerConfig())

	scheduler.recompute("startup")
	scheduler.recompute("event")

	if analyzer.calls != 1 {
		t.Fatalf("expected an identical pending event to be coalesced, got %d calls", analyzer.calls)
	}
}

func TestOutdoorRecomputeCanReanalyzeUnchangedTelemetry(t *testing.T) {
	store := &fakeStore{latest: []SensorReading{{Timestamp: 1738886400, PM2: 3, PM10: 5}}}
	analyzer := &fakeAlertAnalyzer{}
	scheduler := NewInsightsScheduler(store, analyzer, testInsightsSchedulerConfig())

	scheduler.recompute("startup")
	scheduler.recompute("outdoor")

	if analyzer.calls != 2 {
		t.Fatalf("expected outdoor changes to reanalyze unchanged telemetry, got %d calls", analyzer.calls)
	}
}

func TestOutdoorTriggerOutranksEventTrigger(t *testing.T) {
	if triggerPriority("outdoor") <= triggerPriority("event") {
		t.Fatal("expected an outdoor refresh to survive trigger coalescing")
	}
}

func TestPendingEventWithIdenticalTelemetryIsCoalesced(t *testing.T) {
	reading := SensorReading{Timestamp: 1738886400, PM2: 3, PM10: 5}
	analyzer := newBlockingAlertAnalyzer()
	scheduler := NewInsightsScheduler(
		&fakeStore{latest: []SensorReading{reading}},
		analyzer,
		testInsightsSchedulerConfig(),
	)

	scheduler.requestRecompute("startup")
	select {
	case <-analyzer.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first analysis")
	}
	scheduler.requestRecompute("event")
	close(analyzer.release)
	waitForInsightsSchedulerIdle(t, scheduler)

	calls, maxActive := analyzer.stats()
	if calls != 1 {
		t.Fatalf("expected identical pending telemetry to use one analysis, got %d", calls)
	}
	if maxActive != 1 {
		t.Fatalf("expected single-flight analysis, got %d concurrent calls", maxActive)
	}
}

func TestPendingEventWithNewerTelemetryRunsOneFollowUp(t *testing.T) {
	initial := SensorReading{Timestamp: 1738886400, PM2: 3, PM10: 5}
	var liveMu sync.RWMutex
	live := []SensorReading{initial}
	analyzer := newBlockingAlertAnalyzer()
	scheduler := NewInsightsScheduler(
		&fakeStore{latest: []SensorReading{initial}},
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			liveMu.RLock()
			defer liveMu.RUnlock()
			return append([]SensorReading(nil), live...)
		}),
	)

	scheduler.requestRecompute("startup")
	select {
	case <-analyzer.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first analysis")
	}
	liveMu.Lock()
	live = append(live, SensorReading{Timestamp: 1738886460, PM2: 9, PM10: 5})
	liveMu.Unlock()
	scheduler.requestRecompute("event")
	close(analyzer.release)
	waitForInsightsSchedulerIdle(t, scheduler)

	calls, maxActive := analyzer.stats()
	if calls != 2 {
		t.Fatalf("expected exactly one follow-up for newer telemetry, got %d calls", calls)
	}
	if maxActive != 1 {
		t.Fatalf("expected single-flight analysis, got %d concurrent calls", maxActive)
	}
}

func TestFailingOutdoorTriggerDoesNotStrandSupersededEvent(t *testing.T) {
	baseline := SensorReading{
		Timestamp: 1738886400, Temperature: 22, Humidity: 45, PM2: 3, PM10: 5,
	}
	var liveMu sync.RWMutex
	live := []SensorReading{baseline}
	analyzer := &scriptedAlertAnalyzer{
		errors:  []error{nil, errors.New("outdoor analysis failed"), nil},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	scheduler := NewInsightsScheduler(
		&fakeStore{},
		analyzer,
		testInsightsSchedulerConfig(),
		WithInsightsLiveReadings(func(_ int) []SensorReading {
			liveMu.RLock()
			defer liveMu.RUnlock()
			return append([]SensorReading(nil), live...)
		}),
	)
	scheduler.hasSnapshot = true
	scheduler.triggerFromReading(baseline)
	scheduler.requestRecompute("outdoor")
	select {
	case <-analyzer.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first outdoor analysis")
	}

	loss := baseline
	loss.Timestamp += 60
	loss.PMAvailable = boolPtr(false)
	liveMu.Lock()
	live = append(live, loss)
	liveMu.Unlock()
	scheduler.OnReading(loss)
	scheduler.requestRecompute("outdoor")
	close(analyzer.release)
	waitForInsightsSchedulerIdle(t, scheduler)

	if calls := analyzer.callCount(); calls != 3 {
		t.Fatalf("expected initial, failing outdoor, and preserved event analyses; got %d", calls)
	}
	if snapshot, ok := scheduler.Snapshot(1); !ok || snapshot.Trigger != "event" {
		t.Fatalf("expected preserved event to produce the final snapshot, got %#v", snapshot)
	}
	scheduler.mu.RLock()
	accepted := scheduler.acceptedSeverity
	scheduler.mu.RUnlock()
	if accepted != nil {
		t.Fatal("expected preserved event to be fully analyzed")
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

func TestRecomputeRejectsPrivateLocationBeforeSnapshot(t *testing.T) {
	privatePostcode := strings.Join([]string{"A", "A", "1", " ", "1", "A", "A"}, "")
	store := &fakeStore{latest: []SensorReading{{
		Timestamp: 1738886400, Temperature: 22, Humidity: 45,
	}}}
	analyzer := &fixedAlertAnalyzer{alerts: []Alert{{
		Kind:     "insight",
		Severity: "info",
		Title:    "Local outdoor conditions",
		Message:  "Conditions near " + privatePostcode + " are stable.",
	}}}
	scheduler := NewInsightsScheduler(store, analyzer, testInsightsSchedulerConfig())

	scheduler.recompute("startup")

	if _, ok := scheduler.Snapshot(1); ok {
		t.Fatal("private location data reached the insights snapshot")
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
