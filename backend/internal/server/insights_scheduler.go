package server

import (
	"context"
	"log"
	"math"
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

type InsightsSchedulerConfig struct {
	AnalysisLimit    int
	RefreshInterval  time.Duration
	EventMinInterval time.Duration
	PM2Threshold     float64
	PM10Threshold    float64
	PM2DeltaTrigger  float64
	PM10DeltaTrigger float64
	AnalyzeTimeout   time.Duration
}

func DefaultInsightsSchedulerConfig() InsightsSchedulerConfig {
	return InsightsSchedulerConfig{
		AnalysisLimit:    900,
		RefreshInterval:  time.Hour,
		EventMinInterval: 10 * time.Minute,
		PM2Threshold:     8.0,
		PM10Threshold:    30.0,
		PM2DeltaTrigger:  3.0,
		PM10DeltaTrigger: 10.0,
		AnalyzeTimeout:   15 * time.Second,
	}
}

type InsightsScheduler struct {
	store         Store
	snapshotStore InsightsSnapshotStore
	analyzer      AlertAnalyzer
	config        InsightsSchedulerConfig

	mu               sync.RWMutex
	snapshot         InsightsSnapshot
	hasSnapshot      bool
	lastReading      *SensorReading
	lastEventTrigger time.Time
	running          bool
	pending          bool
}

func NewInsightsScheduler(
	store Store,
	analyzer AlertAnalyzer,
	config InsightsSchedulerConfig,
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
	if cfg.AnalyzeTimeout <= 0 {
		cfg.AnalyzeTimeout = defaults.AnalyzeTimeout
	}

	return &InsightsScheduler{
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
}

func (scheduler *InsightsScheduler) Start(ctx context.Context) {
	scheduler.loadSnapshotFromStore()
	scheduler.requestRecompute("startup")

	go func() {
		ticker := time.NewTicker(scheduler.config.RefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				scheduler.requestRecompute("interval")
			}
		}
	}()
}

func (scheduler *InsightsScheduler) loadSnapshotFromStore() {
	if scheduler.snapshotStore == nil {
		return
	}

	loadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, ok, err := scheduler.snapshotStore.LatestInsightsSnapshot(loadCtx)
	if err != nil {
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
	if !scheduler.shouldTriggerFromReading(reading) {
		return
	}
	scheduler.requestRecompute("event")
}

func (scheduler *InsightsScheduler) OnBatch(readings []SensorReading) {
	for _, reading := range readings {
		if scheduler.shouldTriggerFromReading(reading) {
			scheduler.requestRecompute("event")
			return
		}
	}
}

func (scheduler *InsightsScheduler) shouldTriggerFromReading(reading SensorReading) bool {
	now := time.Now()

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

	if scheduler.lastReading == nil {
		latest := reading
		scheduler.lastReading = &latest
		return false
	}

	previous := *scheduler.lastReading
	latest := reading
	scheduler.lastReading = &latest

	pm2Crossed := previous.PM2 < scheduler.config.PM2Threshold &&
		reading.PM2 >= scheduler.config.PM2Threshold
	pm10Crossed := previous.PM10 < scheduler.config.PM10Threshold &&
		reading.PM10 >= scheduler.config.PM10Threshold

	pm2Jumped := math.Abs(reading.PM2-previous.PM2) >= scheduler.config.PM2DeltaTrigger
	pm10Jumped := math.Abs(reading.PM10-previous.PM10) >= scheduler.config.PM10DeltaTrigger

	if !(pm2Crossed || pm10Crossed || pm2Jumped || pm10Jumped) {
		return false
	}

	if !scheduler.lastEventTrigger.IsZero() &&
		now.Sub(scheduler.lastEventTrigger) < scheduler.config.EventMinInterval {
		return false
	}

	scheduler.lastEventTrigger = now
	return true
}

func (scheduler *InsightsScheduler) requestRecompute(trigger string) {
	scheduler.mu.Lock()
	if scheduler.running {
		scheduler.pending = true
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
		if scheduler.pending {
			scheduler.pending = false
			scheduler.mu.Unlock()
			nextTrigger = "pending"
			continue
		}
		scheduler.running = false
		scheduler.mu.Unlock()
		return
	}
}

func (scheduler *InsightsScheduler) recompute(trigger string) {
	ctx, cancel := context.WithTimeout(context.Background(), scheduler.config.AnalyzeTimeout)
	defer cancel()

	readings, err := scheduler.store.Latest(ctx, scheduler.config.AnalysisLimit)
	if err != nil {
		log.Printf("insights recompute failed to load readings: %v", err)
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

	scheduler.mu.Lock()
	scheduler.snapshot = snapshot
	scheduler.hasSnapshot = true
	scheduler.mu.Unlock()

	if scheduler.snapshotStore != nil {
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := scheduler.snapshotStore.SaveInsightsSnapshot(saveCtx, snapshot); err != nil {
			log.Printf("insights snapshot persist failed: %v", err)
		}
		cancel()
	}

	log.Printf(
		"insights recomputed trigger=%s samples=%d insights=%d",
		trigger,
		len(readings),
		len(alerts),
	)
}
