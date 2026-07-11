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
)

type InsightsSchedulerOption func(*InsightsScheduler)

func WithInsightsLiveReadings(source func(limit int) []SensorReading) InsightsSchedulerOption {
	return func(scheduler *InsightsScheduler) {
		scheduler.liveReadings = source
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
		RefreshInterval:          6 * time.Hour,
		EventMinInterval:         10 * time.Minute,
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
		AnalyzeTimeout:           15 * time.Second,
	}
}

type InsightsScheduler struct {
	store         Store
	snapshotStore InsightsSnapshotStore
	analyzer      AlertAnalyzer
	config        InsightsSchedulerConfig
	liveReadings  func(limit int) []SensorReading

	mu                   sync.RWMutex
	snapshot             InsightsSnapshot
	hasSnapshot          bool
	lastReading          *SensorReading
	recentReadings       []SensorReading
	lastAnalyzedSampleAt int64
	lastEventTrigger     time.Time
	lastEventDirection   string
	running              bool
	pendingTrigger       string
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

	if !scheduler.hasSnapshot || scheduler.snapshot.GeneratedAt <= 0 {
		return true
	}

	generatedAt := time.UnixMilli(scheduler.snapshot.GeneratedAt)
	return !now.Before(generatedAt.Add(scheduler.config.RefreshInterval))
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

	scheduler.mu.Lock()
	scheduler.snapshot = snapshot
	scheduler.hasSnapshot = true
	scheduler.mu.Unlock()

	log.Printf(
		"insights snapshot restored source=%s generated_at=%d",
		snapshot.Source,
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

	if limit > 0 && len(snapshot.Insights) > limit {
		snapshot.Insights = snapshot.Insights[:limit]
	}

	return snapshot, true
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
		if !scheduler.hasSnapshot {
			return "warmup"
		}
		return ""
	}

	previous := *scheduler.lastReading
	if readingsFromDifferentDevices(previous, reading) {
		latest := reading
		scheduler.lastReading = &latest
		scheduler.recentReadings = []SensorReading{reading}
		return ""
	}
	if reading.Timestamp <= previous.Timestamp {
		return ""
	}

	latest := reading
	scheduler.lastReading = &latest
	windowReference := scheduler.recordRecentReading(reading)

	pm2Delta := reading.PM2 - windowReference.PM2
	pm10Delta := reading.PM10 - windowReference.PM10
	pm2SeverityChange := severityChange(
		pmSeverity(previous.PM2, scheduler.config.PM2Threshold, criticalPM2Threshold),
		pmSeverity(reading.PM2, scheduler.config.PM2Threshold, criticalPM2Threshold),
	)
	pm10SeverityChange := severityChange(
		pmSeverity(previous.PM10, scheduler.config.PM10Threshold, criticalPM10Threshold),
		pmSeverity(reading.PM10, scheduler.config.PM10Threshold, criticalPM10Threshold),
	)
	humiditySeverityChange := severityChange(
		humiditySeverity(previous.Humidity, scheduler.config.HumidityLowThreshold, scheduler.config.HumidityHighThreshold),
		humiditySeverity(reading.Humidity, scheduler.config.HumidityLowThreshold, scheduler.config.HumidityHighThreshold),
	)
	temperatureSeverityChange := severityChange(
		temperatureSeverity(previous.Temperature, scheduler.config.TemperatureLowThreshold, scheduler.config.TemperatureHighThreshold),
		temperatureSeverity(reading.Temperature, scheduler.config.TemperatureLowThreshold, scheduler.config.TemperatureHighThreshold),
	)
	severityChanged := pm2SeverityChange != 0 ||
		pm10SeverityChange != 0 ||
		humiditySeverityChange != 0 ||
		temperatureSeverityChange != 0

	worsening := pm2SeverityChange > 0 ||
		pm10SeverityChange > 0 ||
		humiditySeverityChange > 0 ||
		temperatureSeverityChange > 0 ||
		pm2Delta >= scheduler.config.PM2DeltaTrigger ||
		pm10Delta >= scheduler.config.PM10DeltaTrigger ||
		movedFurtherFromComfort(windowReference.Humidity, reading.Humidity, scheduler.config.HumidityLowThreshold, scheduler.config.HumidityHighThreshold, scheduler.config.HumidityDeltaTrigger) ||
		movedFurtherFromComfort(windowReference.Temperature, reading.Temperature, scheduler.config.TemperatureLowThreshold, scheduler.config.TemperatureHighThreshold, scheduler.config.TemperatureDeltaTrigger)
	improving := pm2SeverityChange < 0 ||
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

	eventDirection := "mixed"
	switch {
	case worsening && !improving:
		eventDirection = "worsening"
	case improving && !worsening:
		eventDirection = "improving"
	}

	if !severityChanged &&
		!scheduler.lastEventTrigger.IsZero() &&
		now.Sub(scheduler.lastEventTrigger) < scheduler.config.EventMinInterval &&
		scheduler.lastEventDirection == eventDirection {
		return ""
	}

	scheduler.lastEventTrigger = now
	scheduler.lastEventDirection = eventDirection
	return "event"
}

func readingsFromDifferentDevices(previous, current SensorReading) bool {
	return previous.DeviceID != "" && current.DeviceID != "" && previous.DeviceID != current.DeviceID
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
		scheduler.running = false
		scheduler.mu.Unlock()
		return
	}
}

func triggerPriority(trigger string) int {
	switch trigger {
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
	ctx, cancel := context.WithTimeout(context.Background(), scheduler.config.AnalyzeTimeout)
	defer cancel()

	readings, analysisSource, err := scheduler.analysisReadings(ctx, trigger)
	if err != nil {
		if errors.Is(err, ErrStoreUnavailable) {
			return
		}
		log.Printf("insights recompute failed to load readings: %v", err)
		return
	}
	if len(readings) == 0 {
		return
	}

	latestSampleAt := latestTimestamp(readings)
	scheduler.mu.RLock()
	alreadyAnalyzed := scheduler.lastAnalyzedSampleAt > 0 && latestSampleAt <= scheduler.lastAnalyzedSampleAt
	scheduler.mu.RUnlock()
	if trigger == "interval" && alreadyAnalyzed {
		return
	}

	alerts, err := scheduler.analyzer.Analyze(ctx, readings)
	if err != nil {
		log.Printf("insights recompute failed to analyze readings: %v", err)
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
		scheduler.lastAnalyzedSampleAt = latestAnalyzed.Timestamp
		if scheduler.lastReading == nil || latestAnalyzed.Timestamp >= scheduler.lastReading.Timestamp {
			scheduler.lastReading = latestAnalyzed
			scheduler.recentReadings = []SensorReading{*latestAnalyzed}
		}
	}
	scheduler.mu.Unlock()

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
	if (trigger == "event" || trigger == "warmup") && len(liveReadings) > 0 {
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
