package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter allows at most limit events per key in any sliding window. State is
// in process memory, which is enough for a single backend instance; several
// instances would need a shared store such as Postgres.
type limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	hits  map[string][]time.Time
	calls int
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, now: time.Now, hits: make(map[string][]time.Time)}
}

// check reports whether one more event for key fits in the limit, and if not,
// how long until it would. It records nothing.
func (l *limiter) check(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := l.pruneLocked(key, now)
	if len(recent) >= l.limit {
		return false, recent[0].Add(l.window).Sub(now)
	}
	return true, 0
}

// record counts one event for key.
func (l *limiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.hits[key] = append(l.pruneLocked(key, now), now)

	// Occasionally drop keys that have gone quiet so the map can't grow
	// without bound.
	if l.calls++; l.calls%1000 == 0 {
		for k := range l.hits {
			if len(l.pruneLocked(k, now)) == 0 {
				delete(l.hits, k)
			}
		}
	}
}

func (l *limiter) pruneLocked(key string, now time.Time) []time.Time {
	hits := l.hits[key]
	cutoff := now.Add(-l.window)
	i := 0
	for i < len(hits) && !hits[i].After(cutoff) {
		i++
	}
	hits = hits[i:]
	l.hits[key] = hits
	return hits
}

// rule pairs a limiter with the key it applies to for one request.
type rule struct {
	l   *limiter
	key string
}

// allowAll checks every rule first and records the request against all of
// them only if none is exceeded, so a request refused by one rule (say, the
// global cap) doesn't use up the others (say, that phone's allowance). It
// reports the longest wait among exceeded rules. Rules with an empty key, such
// as per-IP when the client IP is unknown, are skipped.
func allowAll(rules ...rule) (bool, time.Duration) {
	var longest time.Duration
	for _, r := range rules {
		if r.key == "" {
			continue
		}
		if ok, wait := r.l.check(r.key); !ok && wait > longest {
			longest = wait
		}
	}
	if longest > 0 {
		return false, longest
	}
	for _, r := range rules {
		if r.key != "" {
			r.l.record(r.key)
		}
	}
	return true, 0
}

// clientIP returns the caller's address from a header set by a proxy we
// trust, or "" when there is none.
//
// The Next.js rewrite proxy forwards the browser's own X-Forwarded-For
// unchanged and adds nothing, so neither that header nor RemoteAddr (always
// the proxy) identifies the client. Only a header set by infrastructure in
// front of everything, such as a load balancer, can: with one that appends to
// X-Forwarded-For, the last entry is the one it wrote.
func clientIP(r *http.Request, trustedHeader string) string {
	if trustedHeader == "" {
		return ""
	}
	value := r.Header.Get(trustedHeader)
	if i := strings.LastIndex(value, ","); i >= 0 {
		value = value[i+1:]
	}
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}
