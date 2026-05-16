package ratelimit

import (
	"context"
	"sync"
	"time"
)

type LocalLimiter struct {
	windows map[string]*localWindow
	mu      sync.RWMutex
}

type localWindow struct {
	start    time.Time
	end      time.Time
	requests int
	mu       sync.Mutex
}

func NewLocalLimiter() *LocalLimiter {
	l := &LocalLimiter{
		windows: make(map[string]*localWindow),
	}
	go l.cleanup()
	return l
}

func (l *LocalLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (*LimitResult, error) {
	l.mu.RLock()
	w, exists := l.windows[key]
	l.mu.RUnlock()

	now := time.Now()

	if !exists {
		l.mu.Lock()
		w = &localWindow{
			start:    now,
			end:      now.Add(window),
			requests: 0,
		}
		l.windows[key] = w
		l.mu.Unlock()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if now.After(w.end) {
		w.start = now
		w.end = now.Add(window)
		w.requests = 0
	}

	remaining := limit - w.requests
	result := &LimitResult{
		Allowed:   remaining > 0,
		Remaining: max(0, remaining-1),
		Reset:     w.end,
		Limit:     limit,
		Window:    window,
	}

	if result.Allowed {
		w.requests++
	}

	return result, nil
}

func (l *LocalLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for key, w := range l.windows {
			w.mu.Lock()
			if now.After(w.end.Add(1 * time.Minute)) {
				delete(l.windows, key)
			}
			w.mu.Unlock()
		}
		l.mu.Unlock()
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
