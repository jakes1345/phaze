package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ---------- CORS / Rate limiting ----------

var allowedOrigins = func() map[string]bool {
	raw := os.Getenv("Phaze_ALLOWED_ORIGINS")
	if raw == "" {
		// H1: warn loudly in production when CORS is wide-open.
		log.Println("[WARNING] Phaze_ALLOWED_ORIGINS is not set — CORS allows ALL origins. Set this in production!")
		return nil // nil = allow all (dev mode)
	}
	m := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		m[strings.TrimSpace(o)] = true
	}
	return m
}()

func originAllowed(r *http.Request) bool {
	if allowedOrigins == nil {
		return true
	}
	return allowedOrigins[r.Header.Get("Origin")]
}

// pstnBridgeEnabled gates Twilio outbound PSTN. Default off so relays run
// WebRTC-only (Phaze-to-Phaze) with no carrier or Twilio call charges.
func pstnBridgeEnabled() bool {
	return strings.EqualFold(os.Getenv("PHAZE_ENABLE_PSTN"), "true")
}

type limiterEntry struct {
	lim  *rate.Limiter
	last time.Time
}

type ipLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	r       rate.Limit
	burst   int
	idleTTL time.Duration
	maxSize int
	lastGC  time.Time
	gcEvery time.Duration
}

func newIPLimiter(r rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{
		entries: map[string]*limiterEntry{},
		r:       r,
		burst:   burst,
		idleTTL: 10 * time.Minute,
		maxSize: 50000,
		gcEvery: 1 * time.Minute,
	}
}

// gcLocked evicts idle limiters. Caller must hold l.mu.
func (l *ipLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.gcEvery && len(l.entries) < l.maxSize {
		return
	}
	cutoff := now.Add(-l.idleTTL)
	for ip, e := range l.entries {
		if e.last.Before(cutoff) {
			delete(l.entries, ip)
		}
	}
	// If still over cap, drop oldest opportunistically.
	if len(l.entries) > l.maxSize {
		for ip := range l.entries {
			delete(l.entries, ip)
			if len(l.entries) <= l.maxSize*9/10 {
				break
			}
		}
	}
	l.lastGC = now
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)
	e, ok := l.entries[ip]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.r, l.burst)}
		l.entries[ip] = e
	}
	e.last = now
	return e.lim.Allow()
}

// trustedProxyHeader is set when the server runs behind a known reverse proxy
// (Fly.io edge, Cloudflare, nginx). Empty = ignore X-Forwarded-For entirely so
// attackers cannot spoof a source IP to bypass per-IP rate limits.
var trustedProxyHeader = strings.TrimSpace(os.Getenv("PHAZE_TRUST_PROXY_HEADER"))

func clientIP(r *http.Request) string {
	if trustedProxyHeader != "" {
		if v := strings.TrimSpace(r.Header.Get(trustedProxyHeader)); v != "" {
			// Take leftmost entry (original client). Edge proxies append on the right.
			if i := strings.Index(v, ","); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			if v != "" {
				return v
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var globalLimiter = newIPLimiter(rate.Limit(10), 30) // 10 req/s, burst 30 per IP
var adminLimiter = newIPLimiter(rate.Limit(0.05), 3) // 3 attempts per minute per IP
// resendLimiter prevents email-bombing via resend_verification: 1 resend per 5 minutes per username.
var resendLimiter = newIPLimiter(rate.Limit(1.0/300), 1)
// forgotPasswordLimiter prevents password-reset email flooding: 1 reset per 5 minutes per email.
var forgotPasswordLimiter = newIPLimiter(rate.Limit(1.0/300), 1)
// remoteLookupLimiter prevents brute-forcing remote control codes: 10 lookups per minute per IP.
var remoteLookupLimiter = newIPLimiter(rate.Limit(10.0/60), 10)
