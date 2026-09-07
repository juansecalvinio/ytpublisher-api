package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a per-client sliding-window rate limiter, held in memory. It is
// correct only for a single process — see the design spec's non-goals for
// what changes if this API ever runs as more than one instance.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{
		max:    max,
		window: window,
		hits:   make(map[string][]time.Time),
	}
}

func (l *Limiter) Allow(clientID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	var recent []time.Time
	for _, t := range l.hits[clientID] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= l.max {
		l.hits[clientID] = recent
		return false
	}

	l.hits[clientID] = append(recent, now)
	return true
}
