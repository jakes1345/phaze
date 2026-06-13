package main

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// adminIPGate restricts admin endpoints to allowed IPs when PHAZE_ADMIN_IPS
// is set (comma-separated CIDRs or IPs). If unset, all IPs are allowed.
func adminIPGate(next http.HandlerFunc) http.HandlerFunc {
	raw := strings.TrimSpace(os.Getenv("PHAZE_ADMIN_IPS"))
	if raw == "" {
		return next
	}
	allowed := strings.Split(raw, ",")
	for i := range allowed {
		allowed[i] = strings.TrimSpace(allowed[i])
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		ok := false
		for _, a := range allowed {
			if a == ip || a == "0.0.0.0" {
				ok = true
				break
			}
			if strings.Contains(a, "/") {
				_, cidr, err := net.ParseCIDR(a)
				if err == nil && cidr.Contains(net.ParseIP(ip)) {
					ok = true
					break
				}
			}
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		next(w, r)
	}
}

// adminLoginLimit is a tighter rate limiter specifically for admin login —
// 3 attempts per minute per IP to prevent brute-force.
func adminLoginLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeSecurityHeaders(w)
		if !adminLimiter.allow(clientIP(r)) {
			http.Error(w, "too many login attempts — try again in a minute", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Defense-in-depth headers on every response that goes through
		// the limiter. Cheap, applies broadly, and stops a few classes
		// of attack (MIME sniffing, clickjacking, leaky referrers).
		writeSecurityHeaders(w)
		// Strict CSP for API/WS endpoints — they never serve HTML or
		// load external resources, so default-src 'none' is safe.
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if !globalLimiter.allow(clientIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// writeSecurityHeaders sets headers that protect every HTTP response.
// Intentionally conservative: only headers that won't break the React SPA
// or the existing API. CSP is omitted because the /admin portal uses
// inline scripts; if you tighten that later, add it here.
func writeSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
}
