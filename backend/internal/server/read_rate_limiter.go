package server

import (
	"sync"
	"time"
)

type ReadRateLimitConfig struct {
	Requests int
	Window   time.Duration
}

func DefaultReadRateLimitConfig() ReadRateLimitConfig {
	return ReadRateLimitConfig{
		Requests: 60,
		Window:   time.Minute,
	}
}

type readRateLimitEntry struct {
	count   int
	resetAt time.Time
}

type readRateLimiter struct {
	mu          sync.Mutex
	requests    int
	window      time.Duration
	entries     map[string]readRateLimitEntry
	nextCleanup time.Time
	now         func() time.Time
}

func newReadRateLimiter(config ReadRateLimitConfig) *readRateLimiter {
	if config.Requests <= 0 || config.Window <= 0 {
		return nil
	}

	return &readRateLimiter{
		requests: config.Requests,
		window:   config.Window,
		entries:  make(map[string]readRateLimitEntry),
		now:      time.Now,
	}
}

func (limiter *readRateLimiter) allow(clientKey string) (bool, time.Duration) {
	if clientKey == "" {
		clientKey = "unknown"
	}

	now := limiter.now()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	limiter.cleanupExpiredEntries(now)

	entry, ok := limiter.entries[clientKey]
	if !ok || !now.Before(entry.resetAt) {
		limiter.entries[clientKey] = readRateLimitEntry{
			count:   1,
			resetAt: now.Add(limiter.window),
		}
		return true, 0
	}

	if entry.count >= limiter.requests {
		return false, entry.resetAt.Sub(now)
	}

	entry.count++
	limiter.entries[clientKey] = entry
	return true, 0
}

func (limiter *readRateLimiter) cleanupExpiredEntries(now time.Time) {
	if !limiter.nextCleanup.IsZero() && now.Before(limiter.nextCleanup) {
		return
	}

	for clientKey, entry := range limiter.entries {
		if !now.Before(entry.resetAt) {
			delete(limiter.entries, clientKey)
		}
	}
	limiter.nextCleanup = now.Add(limiter.window)
}
