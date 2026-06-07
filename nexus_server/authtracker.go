package main

import (
	"sync"
	"time"
)

// ---------- Auth brute-force protection ----------

// authFailTracker tracks per-IP and per-user consecutive failed auth attempts.
// When thresholds are breached, connections are throttled or blocked entirely.
type authFailTracker struct {
	mu        sync.Mutex
	ipFails   map[string]*authFail // keyed by IP
	userFails map[string]*authFail // keyed by username
}

type authFail struct {
	count   int
	firstAt time.Time
	lastAt  time.Time
}

var authTracker = &authFailTracker{
	ipFails:   make(map[string]*authFail),
	userFails: make(map[string]*authFail),
}

const (
	// Per-IP: after 5 fails in 15 min, impose progressive delay. At 50, auto-block.
	authIPFailThreshold  = 5
	authIPBlockThreshold = 50
	authIPWindow         = 15 * time.Minute
	// Per-user: lock account after 10 consecutive fails in 30 min.
	authUserLockThreshold = 10
	authUserWindow        = 30 * time.Minute
	// TOTP: max 5 attempts per 10 min.
	totpMaxAttempts = 5
	totpWindow      = 10 * time.Minute
)

func (t *authFailTracker) recordFail(ip, username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()

	// IP tracking
	if f, ok := t.ipFails[ip]; ok {
		if now.Sub(f.firstAt) > authIPWindow {
			*f = authFail{count: 1, firstAt: now, lastAt: now}
		} else {
			f.count++
			f.lastAt = now
		}
	} else {
		t.ipFails[ip] = &authFail{count: 1, firstAt: now, lastAt: now}
	}

	// User tracking
	if username != "" {
		if f, ok := t.userFails[username]; ok {
			if now.Sub(f.firstAt) > authUserWindow {
				*f = authFail{count: 1, firstAt: now, lastAt: now}
			} else {
				f.count++
				f.lastAt = now
			}
		} else {
			t.userFails[username] = &authFail{count: 1, firstAt: now, lastAt: now}
		}
	}
}

func (t *authFailTracker) recordSuccess(ip, username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.ipFails, ip)
	if username != "" {
		delete(t.userFails, username)
	}
}

// isIPThrottled returns true if this IP should be denied login attempts.
func (t *authFailTracker) isIPThrottled(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.ipFails[ip]
	if !ok {
		return false
	}
	if time.Since(f.firstAt) > authIPWindow {
		delete(t.ipFails, ip)
		return false
	}
	return f.count >= authIPFailThreshold
}

// shouldAutoBlock returns true if the IP has exceeded the hard block threshold.
func (t *authFailTracker) shouldAutoBlock(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.ipFails[ip]
	if !ok {
		return false
	}
	return f.count >= authIPBlockThreshold && time.Since(f.firstAt) <= authIPWindow
}

// isUserLocked returns true if too many consecutive auth failures for this user.
func (t *authFailTracker) isUserLocked(username string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.userFails[username]
	if !ok {
		return false
	}
	if time.Since(f.firstAt) > authUserWindow {
		delete(t.userFails, username)
		return false
	}
	return f.count >= authUserLockThreshold
}

// sweepAuthTracker cleans stale entries every 5 minutes.
func sweepAuthTracker() {
	tk := time.NewTicker(5 * time.Minute)
	defer tk.Stop()
	for {
		<-tk.C
		now := time.Now()
		authTracker.mu.Lock()
		for ip, f := range authTracker.ipFails {
			if now.Sub(f.lastAt) > authIPWindow {
				delete(authTracker.ipFails, ip)
			}
		}
		for u, f := range authTracker.userFails {
			if now.Sub(f.lastAt) > authUserWindow {
				delete(authTracker.userFails, u)
			}
		}
		authTracker.mu.Unlock()
	}
}
