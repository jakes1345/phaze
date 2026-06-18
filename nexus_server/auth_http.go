package main

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *NexusServer) httpLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
		Device   string `json:"device_info"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Username == "" || body.Password == "" {
		http.Error(w, "username and password required", http.StatusBadRequest)
		return
	}

	ip := clientIP(r)
	if authTracker.isIPThrottled(ip) {
		http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
		return
	}
	if authTracker.isUserLocked(body.Username) {
		http.Error(w, "Account temporarily locked. Try again in 30 minutes.", http.StatusTooManyRequests)
		return
	}

	if !s.authenticateUser(body.Username, body.Password) {
		authTracker.recordFail(ip, body.Username)
		// Progressive delay mirrors the WS auth handler.
		authTracker.mu.Lock()
		delay := time.Duration(0)
		if f, ok := authTracker.ipFails[ip]; ok {
			delay = time.Duration(f.count) * 500 * time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
		authTracker.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	if banned, reason := s.userBanInfo(body.Username); banned {
		msg := "Account suspended"
		if reason != "" {
			msg += ": " + reason
		}
		http.Error(w, msg, http.StatusForbidden)
		return
	}

	// TOTP check (same logic as WS auth case).
	if authTracker.isTOTPThrottled(body.Username) {
		http.Error(w, "Too many 2FA attempts — wait a few minutes", http.StatusTooManyRequests)
		return
	}
	if !s.verifyTOTP(body.Username, body.TOTPCode) && !s.consumeBackupCode(body.Username, body.TOTPCode) {
		authTracker.recordTOTPFail(body.Username)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"status": "totp_required", "error": "2FA code required or invalid"})
		return
	}
	authTracker.recordTOTPSuccess(body.Username)
	authTracker.recordSuccess(ip, body.Username)
	metrics.authSuccess.Add(1)

	device := body.Device
	if device == "" {
		device = "web"
	}
	tok, err := s.issueSessionToken(body.Username, device)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	s.DB.Exec("UPDATE users SET last_ip = ?, last_login_at = CURRENT_TIMESTAMP WHERE username = ?", ip, body.Username)

	setSessionCookie(w, tok)
	w.Header().Set("Content-Type", "application/json")
	var displayName, mood string
	s.DB.QueryRow("SELECT COALESCE(display_name,''), COALESCE(mood,'') FROM users WHERE username=?", body.Username).Scan(&displayName, &mood)
	json.NewEncoder(w).Encode(map[string]string{
		"status":       "ok",
		"username":     body.Username,
		"display_name": displayName,
		"mood":         mood,
	})
}

func (s *NexusServer) httpLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	tok := tokenFromRequest(r)
	if tok != "" {
		s.revokeSession(tok)
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *NexusServer) httpMeHandler(w http.ResponseWriter, r *http.Request) {
	tok := tokenFromRequest(r)
	username := s.sessionUsername(tok)
	if username == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	var displayName, mood string
	s.DB.QueryRow("SELECT COALESCE(display_name,''), COALESCE(mood,'') FROM users WHERE username=?", username).Scan(&displayName, &mood)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"username":     username,
		"display_name": displayName,
		"mood":         mood,
	})
}
