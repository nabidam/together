package auth

import (
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	maxThrottleKeys = 10_000
	throttleIdle    = 15 * time.Minute
)

type throttleRule struct {
	burst  float64
	refill time.Duration
}

var (
	loginThrottle    = throttleRule{burst: 5, refill: 12 * time.Second}
	registerThrottle = throttleRule{burst: 10, refill: 6 * time.Second}
)

type throttleEntry struct {
	tokens  float64
	updated time.Time
}

// throttle is a per-IP token bucket with an injected clock so its limits can
// be tested without sleeping.
type throttle struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]throttleEntry
}

func newThrottle(now func() time.Time) *throttle {
	return &throttle{now: now, entries: make(map[string]throttleEntry)}
}

func (t *throttle) reserve(key string, rule throttleRule) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	t.evict(now)
	e, ok := t.entries[key]
	if !ok {
		if len(t.entries) >= maxThrottleKeys {
			t.evictOldest()
		}
		e = throttleEntry{tokens: rule.burst, updated: now}
	}
	elapsed := now.Sub(e.updated)
	if elapsed > 0 {
		e.tokens = math.Min(rule.burst, e.tokens+float64(elapsed)/float64(rule.refill))
	}
	e.updated = now
	if e.tokens < 1 {
		t.entries[key] = e
		missing := 1 - e.tokens
		return false, time.Duration(math.Ceil(missing * float64(rule.refill)))
	}
	e.tokens--
	t.entries[key] = e
	return true, 0
}

func (t *throttle) refund(key string, rule throttleRule) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	if !ok {
		return
	}
	now := t.now()
	elapsed := now.Sub(e.updated)
	if elapsed > 0 {
		e.tokens = math.Min(rule.burst, e.tokens+float64(elapsed)/float64(rule.refill))
	}
	e.tokens = math.Min(rule.burst, e.tokens+1)
	e.updated = now
	t.entries[key] = e
}

func (t *throttle) evict(now time.Time) {
	for key, entry := range t.entries {
		if now.Sub(entry.updated) >= throttleIdle {
			delete(t.entries, key)
		}
	}
}

func (t *throttle) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range t.entries {
		if oldestKey == "" || entry.updated.Before(oldest) {
			oldestKey, oldest = key, entry.updated
		}
	}
	delete(t.entries, oldestKey)
}

// clientIP trusts X-Forwarded-For only from a loopback reverse proxy.
func clientIP(remoteAddr string, xForwardedFor string) string {
	peer := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		peer = host
	}
	peerIP := net.ParseIP(peer)
	if peerIP == nil || !peerIP.IsLoopback() {
		return peer
	}
	for _, candidate := range strings.Split(xForwardedFor, ",") {
		if ip := net.ParseIP(strings.TrimSpace(candidate)); ip != nil {
			return ip.String()
		}
	}
	return peerIP.String()
}
