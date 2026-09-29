package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func fakeClockLimiter(limit int, window time.Duration) (*limiter, *time.Time) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter(limit, window)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterSlidingWindow(t *testing.T) {
	l, now := fakeClockLimiter(2, time.Minute)
	for i := range 2 {
		if ok, _ := allowAll(rule{l, "k"}); !ok {
			t.Fatalf("event %d refused within the limit", i+1)
		}
	}
	ok, wait := allowAll(rule{l, "k"})
	if ok || wait != time.Minute {
		t.Fatalf("third event: ok=%v wait=%v, want refused with a 1m wait", ok, wait)
	}
	if ok, _ := allowAll(rule{l, "other"}); !ok {
		t.Error("keys must be independent")
	}
	*now = now.Add(time.Minute + time.Second)
	if ok, _ := allowAll(rule{l, "k"}); !ok {
		t.Error("the window should have moved on")
	}
}

func TestAllowAllDoesNotChargeWhenRefused(t *testing.T) {
	perKey, _ := fakeClockLimiter(1, time.Hour)
	global, _ := fakeClockLimiter(1, time.Hour)

	if ok, _ := allowAll(rule{perKey, "a"}, rule{global, "all"}); !ok {
		t.Fatal("first request refused")
	}
	// The global cap refuses "b". That must not use up b's own allowance.
	if ok, _ := allowAll(rule{perKey, "b"}, rule{global, "all"}); ok {
		t.Fatal("global cap not enforced")
	}
	if ok, _ := allowAll(rule{perKey, "b"}); !ok {
		t.Error("a refused request used up another rule's allowance")
	}
}

func TestAllowAllSkipsEmptyKeys(t *testing.T) {
	l, _ := fakeClockLimiter(1, time.Hour)
	for range 3 {
		if ok, _ := allowAll(rule{l, ""}); !ok {
			t.Fatal("an empty key (unknown client IP) must not be limited")
		}
	}
}

func TestClientIP(t *testing.T) {
	for name, tc := range map[string]struct {
		header, value, want string
	}{
		"no trusted header configured": {"", "1.2.3.4", ""},
		"single value":                 {"X-Forwarded-For", "203.0.113.7", "203.0.113.7"},
		// A client can prepend anything; only the last hop was written by the
		// trusted proxy.
		"spoofed prefix": {"X-Forwarded-For", "6.6.6.6, 203.0.113.7", "203.0.113.7"},
		"not an IP":      {"X-Forwarded-For", "evil", ""},
		"IPv6":           {"CF-Connecting-IP", "2001:db8::1", "2001:db8::1"},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tc.value != "" {
				h := tc.header
				if h == "" {
					h = "X-Forwarded-For"
				}
				r.Header.Set(h, tc.value)
			}
			if got := clientIP(r, tc.header); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
