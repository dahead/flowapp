package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// loginAttempt tracks failed login attempts for a single IP address.
type loginAttempt struct {
	count        int
	firstSeen    time.Time
	blockedUntil time.Time
}

// RateLimiter enforces a per-IP limit on login attempts to prevent brute-force attacks.
// After max attempts within the rolling window, the IP is locked out for a fixed duration.
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	max      int           // maximum attempts allowed within the window
	window   time.Duration // rolling time window for counting attempts
	lockout  time.Duration // how long to block an IP after exceeding max attempts
}

// NewRateLimiter creates a RateLimiter with sensible defaults:
// 5 attempts per 15 minutes, 30-minute lockout on breach.
// A background goroutine periodically cleans up stale entries.
func NewRateLimiter() *RateLimiter {
	r := &RateLimiter{
		attempts: map[string]*loginAttempt{},
		max:      5,
		window:   15 * time.Minute,
		lockout:  30 * time.Minute,
	}
	go r.cleanup()
	return r
}

// Allow checks whether the requesting IP is permitted to attempt a login.
// Returns (true, 0) if allowed, or (false, remaining wait duration) if blocked.
func (r *RateLimiter) Allow(req *http.Request) (bool, time.Duration) {
	ip := clientIP(req)
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	a, ok := r.attempts[ip]
	if !ok {
		a = &loginAttempt{}
		r.attempts[ip] = a
	}

	// still within an active lockout period
	if now.Before(a.blockedUntil) {
		return false, a.blockedUntil.Sub(now)
	}

	// reset the attempt counter if the rolling window has expired
	if now.Sub(a.firstSeen) > r.window {
		a.count = 0
		a.firstSeen = now
	}

	a.count++
	if a.count == 1 {
		a.firstSeen = now
	}
	if a.count > r.max {
		a.blockedUntil = now.Add(r.lockout)
		return false, r.lockout
	}
	return true, 0
}

// Reset clears the attempt record for the requesting IP.
// Call this after a successful login to lift any partial counts.
func (r *RateLimiter) Reset(req *http.Request) {
	ip := clientIP(req)
	r.mu.Lock()
	delete(r.attempts, ip)
	r.mu.Unlock()
}

// cleanup runs in the background and removes stale attempt records every 10 minutes.
func (r *RateLimiter) cleanup() {
	for range time.Tick(10 * time.Minute) {
		r.mu.Lock()
		cutoff := time.Now().Add(-r.window)
		for ip, a := range r.attempts {
			if a.firstSeen.Before(cutoff) && a.blockedUntil.Before(time.Now()) {
				delete(r.attempts, ip)
			}
		}
		r.mu.Unlock()
	}
}

// clientIP extracts the real client IP from the request,
// honouring the X-Forwarded-For header when present (e.g. behind a reverse proxy).
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if host, _, err := net.SplitHostPort(fwd); err == nil {
			return host
		}
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
