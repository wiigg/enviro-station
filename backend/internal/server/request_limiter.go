package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type requestLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]requestWindow
}

type requestWindow struct {
	start time.Time
	count int
}

func newRequestLimiter(limit int, window time.Duration) *requestLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}

	return &requestLimiter{
		limit:   limit,
		window:  window,
		entries: map[string]requestWindow{},
	}
}

func (limiter *requestLimiter) Allow(key string, now time.Time) bool {
	if key == "" {
		key = "unknown"
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	window := limiter.entries[key]
	if window.start.IsZero() || now.Sub(window.start) >= limiter.window {
		window = requestWindow{start: now, count: 0}
	}

	if window.count >= limiter.limit {
		limiter.entries[key] = window
		return false
	}

	window.count++
	limiter.entries[key] = window
	limiter.cleanup(now)
	return true
}

func (limiter *requestLimiter) cleanup(now time.Time) {
	if len(limiter.entries) < 512 {
		return
	}

	expiry := limiter.window * 3
	for key, window := range limiter.entries {
		if now.Sub(window.start) > expiry {
			delete(limiter.entries, key)
		}
	}
}

func clientIdentity(request *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		forwardedFor := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
		if forwardedFor != "" {
			firstHop, _, _ := strings.Cut(forwardedFor, ",")
			if ip := strings.TrimSpace(firstHop); ip != "" {
				return ip
			}
		}

		realIP := strings.TrimSpace(request.Header.Get("X-Real-IP"))
		if realIP != "" {
			return realIP
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil && host != "" {
		return host
	}

	return strings.TrimSpace(request.RemoteAddr)
}
