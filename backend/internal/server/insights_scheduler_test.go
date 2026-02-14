package server

import (
	"testing"
	"time"
)

func testInsightsSchedulerConfig() InsightsSchedulerConfig {
	return InsightsSchedulerConfig{
		AnalysisLimit:    900,
		RefreshInterval:  time.Hour,
		EventMinInterval: 5 * time.Minute,
		PM2Threshold:     100,
		PM10Threshold:    200,
		PM2DeltaTrigger:  5,
		PM10DeltaTrigger: 15,
		AnalyzeTimeout:   5 * time.Second,
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

func TestShouldNotTriggerFromReadingPM2DecreaseJump(t *testing.T) {
	scheduler := NewInsightsScheduler(&fakeStore{}, &fakeAlertAnalyzer{}, testInsightsSchedulerConfig())

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886400000,
		PM2:       15,
		PM10:      12,
	}) {
		t.Fatalf("expected first reading to initialize state without triggering")
	}

	if scheduler.shouldTriggerFromReading(SensorReading{
		Timestamp: 1738886460000,
		PM2:       8.5,
		PM10:      11.7,
	}) {
		t.Fatalf("expected PM2 drop not to trigger recompute")
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
