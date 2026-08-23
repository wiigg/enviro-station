package server

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"
)

type InsightsSnapshot struct {
	Insights        []Alert
	Source          string
	GeneratedAt     int64
	AnalyzedSamples int
	AnalysisLimit   int
	Trigger         string
}

type InsightsEngine interface {
	Snapshot(limit int) (InsightsSnapshot, bool)
	OnReading(reading SensorReading)
	OnBatch(readings []SensorReading)
}

type InsightsSnapshotStore interface {
	SaveInsightsSnapshot(ctx context.Context, snapshot InsightsSnapshot) error
	LatestInsightsSnapshot(ctx context.Context) (InsightsSnapshot, bool, error)
}

type insightsAnalysisSource string

const (
	insightsAnalysisSourceDurable insightsAnalysisSource = "durable"
	insightsAnalysisSourceLive    insightsAnalysisSource = "live"
	insightsTrendWindow                                  = 10 * time.Minute
	insightsScheduleCheckInterval                        = 5 * time.Minute
	insightsScheduleRetryInterval                        = 30 * time.Minute
)

type InsightsSchedulerOption func(*InsightsScheduler)

func WithInsightsLiveReadings(source func(limit int) []SensorReading) InsightsSchedulerOption {
	return func(scheduler *InsightsScheduler) {
		scheduler.liveReadings = source
	}
}

func WithInsightsOutdoorContext(source OutdoorContextSource) InsightsSchedulerOption {
	return func(scheduler *InsightsScheduler) {
		scheduler.outdoorContext = source
	}
}

type InsightsSchedulerConfig struct {
	AnalysisLimit            int
	RefreshInterval          time.Duration
	EventMinInterval         time.Duration
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
	AnalyzeTimeout           time.Duration
}

func DefaultInsightsSchedulerConfig() InsightsSchedulerConfig {
	return InsightsSchedulerConfig{
		AnalysisLimit:            900,
		RefreshInterval:          time.Hour,
		EventMinInterval:         30 * time.Minute,
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
		AnalyzeTimeout:           40 * time.Second,
	}
}

type InsightsScheduler struct {
	store          Store
	snapshotStore  InsightsSnapshotStore
	analyzer       AlertAnalyzer
	config         InsightsSchedulerConfig
	liveReadings   func(limit int) []SensorReading
	outdoorContext OutdoorContextSource

	mu                             sync.RWMutex
	snapshot                       InsightsSnapshot
	hasSnapshot                    bool
	lastReading                    *SensorReading
	recentReadings                 []SensorReading
	lastAnalyzedSampleAt           int64
	reportedSeverity               *insightsSeverityState
	acceptedSeverity               *insightsSeverityState
	acceptedSeverityAt             int64
	acceptedDeviceID               string
	acceptedGeneration             uint64
	acceptedFailed                 bool
	eventGeneration                uint64
	lastEventTrigger               time.Time
	lastIntervalAttempt            time.Time
	lastScheduledAttemptedSampleAt int64
	running                        bool
	pendingTrigger                 string
}

func NewInsightsScheduler(
	store Store,
	analyzer AlertAnalyzer,
	config InsightsSchedulerConfig,
	options ...InsightsSchedulerOption,
) *InsightsScheduler {
	cfg := config
	defaults := DefaultInsightsSchedulerConfig()

	if cfg.AnalysisLimit < 30 {
		cfg.AnalysisLimit = defaults.AnalysisLimit
	}
	if cfg.RefreshInterval < time.Minute {
		cfg.RefreshInterval = defaults.RefreshInterval
	}
	if cfg.EventMinInterval < time.Second {
		cfg.EventMinInterval = defaults.EventMinInterval
	}
	if cfg.PM2Threshold <= 0 {
		cfg.PM2Threshold = defaults.PM2Threshold
	}
	if cfg.PM10Threshold <= 0 {
		cfg.PM10Threshold = defaults.PM10Threshold
	}
	if cfg.PM2DeltaTrigger <= 0 {
		cfg.PM2DeltaTrigger = defaults.PM2DeltaTrigger
	}
	if cfg.PM10DeltaTrigger <= 0 {
		cfg.PM10DeltaTrigger = defaults.PM10DeltaTrigger
	}
	if cfg.HumidityLowThreshold <= 0 {
		cfg.HumidityLowThreshold = defaults.HumidityLowThreshold
	}
	if cfg.HumidityHighThreshold <= cfg.HumidityLowThreshold {
		cfg.HumidityHighThreshold = defaults.HumidityHighThreshold
	}
	if cfg.HumidityDeltaTrigger <= 0 {
		cfg.HumidityDeltaTrigger = defaults.HumidityDeltaTrigger
	}
	if cfg.TemperatureLowThreshold <= 0 {
		cfg.TemperatureLowThreshold = defaults.TemperatureLowThreshold
	}
	if cfg.TemperatureHighThreshold <= cfg.TemperatureLowThreshold {
		cfg.TemperatureHighThreshold = defaults.TemperatureHighThreshold
	}
	if cfg.TemperatureDeltaTrigger <= 0 {
		cfg.TemperatureDeltaTrigger = defaults.TemperatureDeltaTrigger
	}
	if cfg.AnalyzeTimeout <= 0 {
		cfg.AnalyzeTimeout = defaults.AnalyzeTimeout
	}

	scheduler := &InsightsScheduler{
		store:    store,
		analyzer: analyzer,
		config:   cfg,
		snapshotStore: func() InsightsSnapshotStore {
			if snapshotStore, ok := store.(InsightsSnapshotStore); ok {
				return snapshotStore
			}
			return nil
		}(),
	}
	for _, option := range options {
		option(scheduler)
	}
	return scheduler
}

func (scheduler *InsightsScheduler) Start(ctx context.Context) {
	scheduler.loadSnapshotFromStore()
	if scheduler.needsScheduledRefresh(time.Now()) {
		scheduler.requestRecompute("startup")
	}
	if monitor, ok := scheduler.outdoorContext.(OutdoorContextMonitor); ok {
		monitor.Start(ctx, func(initial bool) {
			if initial {
				scheduler.requestRecompute("outdoor_initial")
				return
			}
			scheduler.requestRecompute("outdoor")
		})
	}

	go func() {
		checkInterval := scheduler.config.RefreshInterval
		if checkInterval > insightsScheduleCheckInterval {
			checkInterval = insightsScheduleCheckInterval
		}
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if scheduler.needsScheduledRefresh(time.Now()) {
					scheduler.requestRecompute("interval")
				}
			}
		}
	}()
}

func (scheduler *InsightsScheduler) needsScheduledRefresh(now time.Time) bool {
	scheduler.mu.RLock()
	defer scheduler.mu.RUnlock()

	due := !scheduler.hasSnapshot || scheduler.snapshot.GeneratedAt <= 0
	if !due {
		generatedAt := time.UnixMilli(scheduler.snapshot.GeneratedAt)
		due = !now.Before(generatedAt.Add(scheduler.config.RefreshInterval))
	}
	if !due {
		return false
	}
	return scheduler.lastIntervalAttempt.IsZero() ||
		!now.Before(scheduler.lastIntervalAttempt.Add(insightsScheduleRetryInterval))
}

func (scheduler *InsightsScheduler) loadSnapshotFromStore() {
	if scheduler.snapshotStore == nil {
		return
	}

	loadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, ok, err := scheduler.snapshotStore.LatestInsightsSnapshot(loadCtx)
	if err != nil {
		if errors.Is(err, ErrStoreUnavailable) {
			return
		}
		log.Printf("insights snapshot load failed: %v", err)
		return
	}
	if !ok {
		return
	}
	if insightsSnapshotContainsPrivateLocation(snapshot) {
		log.Printf("insights snapshot rejected by privacy checks")
		return
	}

	scheduler.mu.Lock()
	scheduler.snapshot = snapshot
	scheduler.hasSnapshot = true
	scheduler.mu.Unlock()

	log.Printf(
		"insights snapshot restored generated_at=%d",
		snapshot.GeneratedAt,
	)
}

func (scheduler *InsightsScheduler) Snapshot(limit int) (InsightsSnapshot, bool) {
	scheduler.mu.RLock()
	defer scheduler.mu.RUnlock()

	if !scheduler.hasSnapshot {
		return InsightsSnapshot{}, false
	}

	snapshot := scheduler.snapshot
	snapshot.Insights = cloneAlerts(snapshot.Insights)
	if insightsSnapshotContainsPrivateLocation(snapshot) {
		return InsightsSnapshot{}, false
	}

	if limit > 0 && len(snapshot.Insights) > limit {
		snapshot.Insights = snapshot.Insights[:limit]
	}

	return snapshot, true
}

func insightsSnapshotContainsPrivateLocation(snapshot InsightsSnapshot) bool {
	return containsPrivateLocation(snapshot.Source+" "+snapshot.Trigger) ||
		alertsContainPrivateLocation(snapshot.Insights)
}

func (scheduler *InsightsScheduler) OnReading(reading SensorReading) {
	trigger := scheduler.triggerFromReading(reading)
	if trigger == "" {
		return
	}
	scheduler.requestRecompute(trigger)
}

func (scheduler *InsightsScheduler) OnBatch(readings []SensorReading) {
	for _, reading := range readings {
		if trigger := scheduler.triggerFromReading(reading); trigger != "" {
			scheduler.requestRecompute(trigger)
			return
		}
	}
}

func (scheduler *InsightsScheduler) shouldTriggerFromReading(reading SensorReading) bool {
	return scheduler.triggerFromReading(reading) == "event"
}

func (scheduler *InsightsScheduler) triggerFromReading(reading SensorReading) string {
	now := time.Now()

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

	if scheduler.lastReading == nil {
		latest := reading
		scheduler.lastReading = &latest
		scheduler.recentReadings = []SensorReading{reading}
		severity := scheduler.severityState(reading)
		scheduler.reportedSeverity = &severity
		scheduler.clearAcceptedSeverityLocked()
		return "warmup"
	}

	previous := *scheduler.lastReading
	if readingsFromDifferentDevices(previous, reading) {
		latest := reading
		scheduler.lastReading = &latest
		scheduler.recentReadings = []SensorReading{reading}
		severity := scheduler.severityState(reading)
		scheduler.reportedSeverity = &severity
		scheduler.clearAcceptedSeverityLocked()
		return ""
	}
	if reading.Timestamp <= previous.Timestamp {
		return ""
	}

	latest := reading
	scheduler.lastReading = &latest
	windowReference := scheduler.recordRecentReading(reading)
	currentSeverity := scheduler.severityState(reading)

	reportedSeverity := scheduler.severityState(previous)
	if scheduler.reportedSeverity != nil {
		reportedSeverity = *scheduler.reportedSeverity
	}
	retryFailedEvent := false
	if scheduler.acceptedSeverity != nil {
		cooldownExpired := scheduler.lastEventTrigger.IsZero() ||
			now.Sub(scheduler.lastEventTrigger) >= scheduler.config.EventMinInterval
		if scheduler.acceptedFailed && cooldownExpired {
			scheduler.clearAcceptedSeverityLocked()
			retryFailedEvent = true
		} else {
			reportedSeverity = *scheduler.acceptedSeverity
		}
	}
	if retryFailedEvent {
		scheduler.acceptEventLocked(currentSeverity, reading, now)
		return "event"
	}
	previousPMAvailable := reportedSeverity.pmAvailable
	currentPMAvailable := currentSeverity.pmAvailable
	pm2Delta := 0.0
	pm10Delta := 0.0
	pm2SeverityChange := 0
	pm10SeverityChange := 0
	if previousPMAvailable && currentPMAvailable {
		pm2SeverityChange = severityChange(
			reportedSeverity.pm2,
			currentSeverity.pm2,
		)
		pm10SeverityChange = severityChange(
			reportedSeverity.pm10,
			currentSeverity.pm10,
		)
	}
	if currentPMAvailable {
		if pmReference, ok := firstAvailableParticulateReading(scheduler.recentReadings); ok {
			pm2Delta = reading.PM2 - pmReference.PM2
			pm10Delta = reading.PM10 - pmReference.PM10
		}
	}
	humiditySeverityChange := severityChange(
		reportedSeverity.humidity,
		currentSeverity.humidity,
	)
	temperatureSeverityChange := severityChange(
		reportedSeverity.temperature,
		currentSeverity.temperature,
	)
	worsening := (previousPMAvailable && !currentPMAvailable) ||
		pm2SeverityChange > 0 ||
		pm10SeverityChange > 0 ||
		humiditySeverityChange > 0 ||
		temperatureSeverityChange > 0 ||
		pm2Delta >= scheduler.config.PM2DeltaTrigger ||
		pm10Delta >= scheduler.config.PM10DeltaTrigger ||
		movedFurtherFromComfort(windowReference.Humidity, reading.Humidity, scheduler.config.HumidityLowThreshold, scheduler.config.HumidityHighThreshold, scheduler.config.HumidityDeltaTrigger) ||
		movedFurtherFromComfort(windowReference.Temperature, reading.Temperature, scheduler.config.TemperatureLowThreshold, scheduler.config.TemperatureHighThreshold, scheduler.config.TemperatureDeltaTrigger)
	improving := (!previousPMAvailable && currentPMAvailable) ||
		pm2SeverityChange < 0 ||
		pm10SeverityChange < 0 ||
		humiditySeverityChange < 0 ||
		temperatureSeverityChange < 0 ||
		pm2Delta <= -scheduler.config.PM2DeltaTrigger ||
		pm10Delta <= -scheduler.config.PM10DeltaTrigger ||
		movedTowardComfort(windowReference.Humidity, reading.Humidity, scheduler.config.HumidityLowThreshold, scheduler.config.HumidityHighThreshold, scheduler.config.HumidityDeltaTrigger) ||
		movedTowardComfort(windowReference.Temperature, reading.Temperature, scheduler.config.TemperatureLowThreshold, scheduler.config.TemperatureHighThreshold, scheduler.config.TemperatureDeltaTrigger)

	if !(worsening || improving) {
		return ""
	}

	criticalEscalation :=
		(pm2SeverityChange > 0 && currentSeverity.pm2 == metricCritical) ||
			(pm10SeverityChange > 0 && currentSeverity.pm10 == metricCritical) ||
			(humiditySeverityChange > 0 && currentSeverity.humidity == metricCritical) ||
			(temperatureSeverityChange > 0 && currentSeverity.temperature == metricCritical)
	if !previousPMAvailable && currentPMAvailable &&
		(currentSeverity.pm2 == metricCritical || currentSeverity.pm10 == metricCritical) {
		criticalEscalation = true
	}
	urgentChange := (previousPMAvailable && !currentPMAvailable) || criticalEscalation
	if !urgentChange &&
		!scheduler.lastEventTrigger.IsZero() &&
		now.Sub(scheduler.lastEventTrigger) < scheduler.config.EventMinInterval {
		return ""
	}

	scheduler.acceptEventLocked(currentSeverity, reading, now)
	return "event"
}

func (scheduler *InsightsScheduler) acceptEventLocked(
	severity insightsSeverityState,
	reading SensorReading,
	now time.Time,
) {
	scheduler.eventGeneration++
	if scheduler.eventGeneration == 0 {
		scheduler.eventGeneration++
	}
	scheduler.acceptedSeverity = &severity
	scheduler.acceptedSeverityAt = reading.Timestamp
	scheduler.acceptedDeviceID = reading.DeviceID
	scheduler.acceptedGeneration = scheduler.eventGeneration
	scheduler.acceptedFailed = false
	scheduler.lastEventTrigger = now
}

func readingsFromDifferentDevices(previous, current SensorReading) bool {
	return previous.DeviceID != "" && current.DeviceID != "" && previous.DeviceID != current.DeviceID
}

func firstAvailableParticulateReading(readings []SensorReading) (SensorReading, bool) {
	for _, reading := range readings {
		if particulateAvailable(reading) {
			return reading, true
		}
	}
	return SensorReading{}, false
}

func (scheduler *InsightsScheduler) recordRecentReading(reading SensorReading) SensorReading {
	scheduler.recentReadings = append(scheduler.recentReadings, reading)
	cutoff := reading.Timestamp - int64(insightsTrendWindow/time.Second)
	referenceIndex := 0
	for index, candidate := range scheduler.recentReadings {
		if candidate.Timestamp > cutoff {
			break
		}
		referenceIndex = index
	}
	if referenceIndex > 0 {
		scheduler.recentReadings = scheduler.recentReadings[referenceIndex:]
	}
	return scheduler.recentReadings[0]
}

type metricSeverity int

const (
	metricOK metricSeverity = iota
	metricWarn
	metricCritical
)

type insightsSeverityState struct {
	pmAvailable bool
	pm2         metricSeverity
	pm10        metricSeverity
	humidity    metricSeverity
	temperature metricSeverity
}

func (scheduler *InsightsScheduler) severityState(reading SensorReading) insightsSeverityState {
	state := insightsSeverityState{
		pmAvailable: particulateAvailable(reading),
		humidity: humiditySeverity(
			reading.Humidity,
			scheduler.config.HumidityLowThreshold,
			scheduler.config.HumidityHighThreshold,
		),
		temperature: temperatureSeverity(
			reading.Temperature,
			scheduler.config.TemperatureLowThreshold,
			scheduler.config.TemperatureHighThreshold,
		),
	}
	if state.pmAvailable {
		state.pm2 = pmSeverity(reading.PM2, scheduler.config.PM2Threshold, criticalPM2Threshold)
		state.pm10 = pmSeverity(reading.PM10, scheduler.config.PM10Threshold, criticalPM10Threshold)
	}
	return state
}

func severityChange(previous, current metricSeverity) int {
	return int(current) - int(previous)
}

func pmSeverity(value, warnThreshold, criticalThreshold float64) metricSeverity {
	if value > criticalThreshold {
		return metricCritical
	}
	if value >= warnThreshold {
		return metricWarn
	}
	return metricOK
}

func humiditySeverity(value, lowThreshold, highThreshold float64) metricSeverity {
	if value < criticalHumidityLowThreshold || value >= criticalHumidityHighThreshold {
		return metricCritical
	}
	if value < lowThreshold || value >= highThreshold {
		return metricWarn
	}
	return metricOK
}

func temperatureSeverity(value, lowThreshold, highThreshold float64) metricSeverity {
	if value <= criticalTemperatureLowThreshold || value >= criticalTemperatureHighThreshold {
		return metricCritical
	}
	if value <= lowThreshold || value >= highThreshold {
		return metricWarn
	}
	return metricOK
}

func movedFurtherFromComfort(previous, current, low, high, trigger float64) bool {
	return comfortDistance(current, low, high) >= comfortDistance(previous, low, high)+trigger
}

func movedTowardComfort(previous, current, low, high, trigger float64) bool {
	previousDistance := comfortDistance(previous, low, high)
	return previousDistance > 0 && comfortDistance(current, low, high) <= previousDistance-trigger
}

func comfortDistance(value, low, high float64) float64 {
	if value < low {
		return low - value
	}
	if value >= high {
		return value - high
	}
	return 0
}

func (scheduler *InsightsScheduler) requestRecompute(trigger string) {
	scheduler.mu.Lock()
	now := time.Now()
	hasNewLiveReading := scheduler.lastReading != nil &&
		scheduler.lastReading.Timestamp > scheduler.lastScheduledAttemptedSampleAt
	if trigger == "warmup" && !scheduler.lastIntervalAttempt.IsZero() &&
		now.Before(scheduler.lastIntervalAttempt.Add(insightsScheduleRetryInterval)) &&
		!hasNewLiveReading {
		scheduler.mu.Unlock()
		return
	}
	if trigger == "interval" || trigger == "startup" || trigger == "warmup" {
		scheduler.lastIntervalAttempt = now
	}
	if scheduler.running {
		if scheduler.pendingTrigger == "" || triggerPriority(trigger) > triggerPriority(scheduler.pendingTrigger) {
			scheduler.pendingTrigger = trigger
		}
		scheduler.mu.Unlock()
		return
	}
	scheduler.running = true
	scheduler.mu.Unlock()

	go scheduler.recomputeLoop(trigger)
}

func (scheduler *InsightsScheduler) recomputeLoop(trigger string) {
	nextTrigger := trigger
	for {
		scheduler.recompute(nextTrigger)

		scheduler.mu.Lock()
		if scheduler.pendingTrigger != "" {
			nextTrigger = scheduler.pendingTrigger
			scheduler.pendingTrigger = ""
			scheduler.mu.Unlock()
			continue
		}
		if scheduler.acceptedSeverity != nil && !scheduler.acceptedFailed {
			nextTrigger = "event"
			scheduler.mu.Unlock()
			continue
		}
		scheduler.running = false
		scheduler.mu.Unlock()
		return
	}
}

func triggerPriority(trigger string) int {
	switch trigger {
	case "outdoor", "outdoor_initial":
		return 4
	case "event":
		return 3
	case "warmup":
		return 2
	case "startup":
		return 1
	default:
		return 0
	}
}

func (scheduler *InsightsScheduler) recompute(trigger string) {
	eventAttemptGeneration := uint64(0)
	if trigger == "event" {
		scheduler.mu.RLock()
		eventAttemptGeneration = scheduler.acceptedGeneration
		scheduler.mu.RUnlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), scheduler.config.AnalyzeTimeout)
	defer cancel()

	readings, analysisSource, err := scheduler.analysisReadings(ctx, trigger)
	if err != nil {
		scheduler.failEventAttempt(eventAttemptGeneration)
		if errors.Is(err, ErrStoreUnavailable) {
			return
		}
		log.Printf("insights recompute failed to load readings: %v", err)
		return
	}
	if len(readings) == 0 {
		scheduler.failEventAttempt(eventAttemptGeneration)
		return
	}

	latestSampleAt := latestTimestamp(readings)
	scheduler.mu.Lock()
	if trigger == "warmup" &&
		latestSampleAt <= scheduler.lastScheduledAttemptedSampleAt &&
		!scheduler.lastIntervalAttempt.IsZero() &&
		time.Now().Before(scheduler.lastIntervalAttempt.Add(insightsScheduleRetryInterval)) {
		scheduler.mu.Unlock()
		return
	}
	if trigger == "startup" || trigger == "warmup" || trigger == "interval" {
		if latestSampleAt > scheduler.lastScheduledAttemptedSampleAt {
			scheduler.lastScheduledAttemptedSampleAt = latestSampleAt
		}
	}
	alreadyAnalyzed := scheduler.lastAnalyzedSampleAt > 0 && latestSampleAt <= scheduler.lastAnalyzedSampleAt
	scheduler.mu.Unlock()
	if (trigger == "interval" || trigger == "event" || trigger == "warmup") && alreadyAnalyzed {
		scheduler.completeEventAttempt(eventAttemptGeneration, readings[len(readings)-1])
		return
	}

	alerts, err := scheduler.analyzer.Analyze(ctx, readings)
	if err != nil {
		scheduler.failEventAttempt(eventAttemptGeneration)
		log.Printf("insights recompute failed to analyze readings: %v", err)
		return
	}
	if alertsContainPrivateLocation(alerts) {
		scheduler.failEventAttempt(eventAttemptGeneration)
		log.Printf("insights recompute rejected by privacy checks")
		return
	}

	snapshot := InsightsSnapshot{
		Insights:        cloneAlerts(alerts),
		Source:          scheduler.analyzer.Source(),
		GeneratedAt:     time.Now().UnixMilli(),
		AnalyzedSamples: len(readings),
		AnalysisLimit:   scheduler.config.AnalysisLimit,
		Trigger:         trigger,
	}
	var latestAnalyzed *SensorReading
	if len(readings) > 0 {
		latest := readings[len(readings)-1]
		latestAnalyzed = &latest
	}
	scheduler.mu.Lock()
	scheduler.snapshot = snapshot
	scheduler.hasSnapshot = true
	if latestAnalyzed != nil {
		if latestAnalyzed.Timestamp > scheduler.lastAnalyzedSampleAt {
			scheduler.lastAnalyzedSampleAt = latestAnalyzed.Timestamp
		}
		analysisMatchesCurrentDevice := scheduler.lastReading == nil ||
			!readingsFromDifferentDevices(*scheduler.lastReading, *latestAnalyzed)
		if analysisMatchesCurrentDevice {
			severity := scheduler.severityState(*latestAnalyzed)
			scheduler.reportedSeverity = &severity
			if scheduler.acceptedSeverity != nil && scheduler.readingCoversAcceptedEvent(*latestAnalyzed) {
				scheduler.clearAcceptedSeverityLocked()
			}
		}
		analysisIsCurrent := analysisMatchesCurrentDevice && (scheduler.lastReading == nil ||
			latestAnalyzed.Timestamp >= scheduler.lastReading.Timestamp)
		if analysisIsCurrent {
			scheduler.lastReading = latestAnalyzed
			scheduler.recentReadings = []SensorReading{*latestAnalyzed}
		}
	}
	scheduler.mu.Unlock()
	scheduler.failEventAttempt(eventAttemptGeneration)

	if scheduler.snapshotStore != nil && analysisSource != insightsAnalysisSourceLive {
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := scheduler.snapshotStore.SaveInsightsSnapshot(saveCtx, snapshot); err != nil {
			log.Printf("insights snapshot persist failed: %v", err)
		}
		cancel()
	}

	log.Printf(
		"insights recomputed trigger=%s analysis_source=%s samples=%d insights=%d",
		trigger,
		analysisSource,
		len(readings),
		len(alerts),
	)
}

func (scheduler *InsightsScheduler) failEventAttempt(generation uint64) {
	if generation == 0 {
		return
	}
	scheduler.mu.Lock()
	if scheduler.acceptedGeneration == generation {
		scheduler.acceptedFailed = true
	}
	scheduler.mu.Unlock()
}

func (scheduler *InsightsScheduler) completeEventAttempt(
	generation uint64,
	latestAnalyzed SensorReading,
) {
	if generation == 0 {
		return
	}
	scheduler.mu.Lock()
	if scheduler.acceptedGeneration == generation {
		if scheduler.readingCoversAcceptedEvent(latestAnalyzed) {
			scheduler.clearAcceptedSeverityLocked()
		} else {
			scheduler.acceptedFailed = true
		}
	}
	scheduler.mu.Unlock()
}

func (scheduler *InsightsScheduler) readingCoversAcceptedEvent(reading SensorReading) bool {
	if reading.Timestamp < scheduler.acceptedSeverityAt {
		return false
	}
	return scheduler.acceptedDeviceID == "" || reading.DeviceID == "" ||
		reading.DeviceID == scheduler.acceptedDeviceID
}

func (scheduler *InsightsScheduler) clearAcceptedSeverityLocked() {
	scheduler.acceptedSeverity = nil
	scheduler.acceptedSeverityAt = 0
	scheduler.acceptedDeviceID = ""
	scheduler.acceptedGeneration = 0
	scheduler.acceptedFailed = false
}

func (scheduler *InsightsScheduler) analysisReadings(
	ctx context.Context,
	trigger string,
) ([]SensorReading, insightsAnalysisSource, error) {
	liveReadings := []SensorReading(nil)
	if scheduler.liveReadings != nil {
		liveReadings = trimLatestReadings(
			scheduler.liveReadings(scheduler.config.AnalysisLimit),
			scheduler.config.AnalysisLimit,
		)
	}
	if !durableStoreReady(scheduler.store) {
		if len(liveReadings) > 0 {
			return liveReadings, insightsAnalysisSourceLive, nil
		}
		return nil, insightsAnalysisSourceDurable, ErrStoreUnavailable
	}
	if (trigger == "event" || trigger == "warmup" || trigger == "outdoor" || trigger == "outdoor_initial") && len(liveReadings) > 0 {
		return liveReadings, insightsAnalysisSourceLive, nil
	}

	durableReadings, durableErr := scheduler.store.Latest(ctx, scheduler.config.AnalysisLimit)
	if len(liveReadings) > 0 {
		if len(durableReadings) == 0 || latestTimestamp(liveReadings) > latestTimestamp(durableReadings) {
			return liveReadings, insightsAnalysisSourceLive, nil
		}
	}

	if durableErr != nil {
		if len(liveReadings) > 0 && errors.Is(durableErr, ErrStoreUnavailable) {
			return liveReadings, insightsAnalysisSourceLive, nil
		}
		return nil, insightsAnalysisSourceDurable, durableErr
	}

	return trimLatestReadings(durableReadings, scheduler.config.AnalysisLimit), insightsAnalysisSourceDurable, nil
}

func latestTimestamp(readings []SensorReading) int64 {
	if len(readings) == 0 {
		return 0
	}
	return readings[len(readings)-1].Timestamp
}

func trimLatestReadings(readings []SensorReading, limit int) []SensorReading {
	if len(readings) == 0 {
		return []SensorReading{}
	}

	output := make([]SensorReading, len(readings))
	copy(output, readings)
	sort.SliceStable(output, func(left, right int) bool {
		if output[left].Timestamp == output[right].Timestamp {
			return output[left].DeviceID < output[right].DeviceID
		}
		return output[left].Timestamp < output[right].Timestamp
	})

	if limit > 0 && len(output) > limit {
		output = output[len(output)-limit:]
	}
	return output
}
