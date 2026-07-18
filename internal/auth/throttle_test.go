package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestThrottle_BurstsRefillsIsolatesAndEvicts(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limits := newThrottle(func() time.Time { return now })

	for range 5 {
		if ok, _ := limits.reserve("192.0.2.1", loginThrottle); !ok {
			t.Fatal("login burst rejected too early")
		}
	}
	if ok, retry := limits.reserve("192.0.2.1", loginThrottle); ok || retry != 12*time.Second {
		t.Fatalf("sixth login = ok:%v retry:%v, want rejection after 12s", ok, retry)
	}
	if ok, _ := limits.reserve("192.0.2.2", loginThrottle); !ok {
		t.Fatal("different IP should have its own bucket")
	}
	now = now.Add(12 * time.Second)
	if ok, _ := limits.reserve("192.0.2.1", loginThrottle); !ok {
		t.Fatal("login token did not refill")
	}

	for range 10 {
		if ok, _ := limits.reserve("198.51.100.1", registerThrottle); !ok {
			t.Fatal("registration burst rejected too early")
		}
	}
	if ok, retry := limits.reserve("198.51.100.1", registerThrottle); ok || retry != 6*time.Second {
		t.Fatalf("eleventh registration = ok:%v retry:%v, want rejection after 6s", ok, retry)
	}

	for i := range maxThrottleKeys + 1 {
		limits.reserve(fmt.Sprintf("203.0.113.%d", i), loginThrottle)
	}
	if len(limits.entries) > maxThrottleKeys {
		t.Fatalf("entries = %d, cap = %d", len(limits.entries), maxThrottleKeys)
	}
	now = now.Add(throttleIdle)
	limits.reserve("203.0.113.new", loginThrottle)
	if len(limits.entries) != 1 {
		t.Fatalf("idle entries = %d, want 1", len(limits.entries))
	}
}

func TestClientIP_TrustsForwardedForOnlyFromLoopback(t *testing.T) {
	tests := []struct {
		name, remote, forwarded, want string
	}{
		{"remote peer", "203.0.113.9:1234", "198.51.100.7", "203.0.113.9"},
		{"loopback forwarded", "127.0.0.1:8080", "invalid, 198.51.100.7, 192.0.2.4", "198.51.100.7"},
		{"loopback no valid forwarded", "[::1]:8080", "invalid,", "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientIP(tt.remote, tt.forwarded); got != tt.want {
				t.Fatalf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
