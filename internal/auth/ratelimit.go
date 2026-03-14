package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type loginAttempt struct {
	count        int
	firstSeen    time.Time
	blockedUntil time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	max      int           // max attempts per window
	window   time.Duration // rolling window
	lockout  time.Duration // lockout after max attempts
}

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

	// still blocked?
	if now.Before(a.blockedUntil) {
		return false, a.blockedUntil.Sub(now)
	}

	// reset window if expired
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

func (r *RateLimiter) Reset(req *http.Request) {
	ip := clientIP(req)
	r.mu.Lock()
	delete(r.attempts, ip)
	r.mu.Unlock()
}

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
