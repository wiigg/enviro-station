package server

import (
	"context"
	"time"
)

type OpsEvent struct {
	ID        int64  `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
}

type OpsEventStore interface {
	AddOpsEvent(ctx context.Context, event OpsEvent) error
	LatestOpsEvents(ctx context.Context, limit int) ([]OpsEvent, error)
}

type OpsConfig struct {
	DeviceOfflineTimeout time.Duration
	MonitorInterval      time.Duration
}

func DefaultOpsConfig() OpsConfig {
	return OpsConfig{
		DeviceOfflineTimeout: 45 * time.Second,
		MonitorInterval:      5 * time.Second,
	}
}
