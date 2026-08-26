// Package middleware provides HTTP middleware for the SEO Analyser server.
package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ipEntry tracks login attempts for one IP address.
type ipEntry struct {
	attempts  []time.Time // timestamps of recent failed attempts
	blockedUntil time.Time
}

// RateLimiter is a per-IP sliding-window rate limiter.
// It blocks an IP for lockDuration after maxAttempts failed attempts
// within the window duration.
type RateLimiter struct {
	mu           sync.Mutex
	entries      map[string]*ipEntry
	maxAttempts  int
	window       time.Duration
	lockDuration time.Duration
}

// NewLoginRateLimiter returns a RateLimiter configured for brute-force
// protection on login endpoints:
//   - 5 failed attempts within 15 minutes → blocked for 15 minutes
func NewLoginRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		entries:      make(map[string]*ipEntry),
		maxAttempts:  5,
		window:       15 * time.Minute,
		lockDuration: 15 * time.Minute,
	}
	// Background goroutine to prune stale entries every 10 minutes.
	go func() {
		for range time.Tick(10 * time.Minute) {
			rl.prune()
		}
	}()
	return rl
}

// Middleware wraps a handler and enforces the rate limit.
// On a 401 response from the inner handler, the attempt is counted.
func (rl *RateLimiter) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r)

		// Check if IP is currently locked out
		if blocked, retryAfter := rl.isBlocked(ip); blocked {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too many failed login attempts — please try again later"}`))
			return
		}

		// Wrap ResponseWriter to intercept the status code
		rw := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next(rw, r)

		// Count failed auth attempts (401 = wrong credentials)
		if rw.statusCode == http.StatusUnauthorized {
			rl.record(ip)
		}
	}
}

// isBlocked returns (true, retryAfterSeconds) if the IP is locked out.
func (rl *RateLimiter) isBlocked(ip string) (bool, string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, ok := rl.entries[ip]
	if !ok {
		return false, ""
	}
	if time.Now().Before(e.blockedUntil) {
		secs := int(time.Until(e.blockedUntil).Seconds()) + 1
		return true, strconv.Itoa(secs)
	}
	return false, ""
}

// record adds a failed attempt for the IP and locks it out if the threshold is exceeded.
func (rl *RateLimiter) record(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	e, ok := rl.entries[ip]
	if !ok {
		e = &ipEntry{}
		rl.entries[ip] = e
	}

	// Slide the window: drop attempts older than rl.window
	cutoff := now.Add(-rl.window)
	fresh := e.attempts[:0]
	for _, t := range e.attempts {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	fresh = append(fresh, now)
	e.attempts = fresh

	if len(e.attempts) >= rl.maxAttempts {
		e.blockedUntil = now.Add(rl.lockDuration)
		e.attempts = nil // reset counter after locking
	}
}

// prune removes entries that have no recent activity and are not blocked.
func (rl *RateLimiter) prune() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for ip, e := range rl.entries {
		if now.After(e.blockedUntil) && len(e.attempts) == 0 {
			delete(rl.entries, ip)
		}
	}
}

// responseRecorder captures the HTTP status code written by the handler.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.written {
		rr.statusCode = code
		rr.written = true
		rr.ResponseWriter.WriteHeader(code)
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	if !rr.written {
		rr.WriteHeader(http.StatusOK)
	}
	return rr.ResponseWriter.Write(b)
}

// realIP extracts the client IP from common proxy headers or RemoteAddr.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// X-Forwarded-For can be "client, proxy1, proxy2" — take first
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}
	// Fall back to RemoteAddr, strip port
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
