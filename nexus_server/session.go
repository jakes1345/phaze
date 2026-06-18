package main

import (
	"net/http"
	"strings"
	"time"
)

const sessionCookieName = "phaze_session"

// tokenFromRequest extracts the session token from either the HttpOnly cookie
// (web) or the Authorization: Bearer header (native / Android). Cookie wins
// when both are present so the web never has a Bearer token in JS memory.
func tokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	h := r.Header.Get("Authorization")
	if tok, ok := strings.CutPrefix(h, "Bearer "); ok {
		return tok
	}
	return ""
}

// setSessionCookie writes the HttpOnly session cookie to a response.
// HttpOnly: JS cannot read it → XSS can't steal the token.
// Secure: only sent over HTTPS.
// SameSite=Strict: cross-site requests never include the cookie → blocks CSRF.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   30 * 24 * 3600,
	})
}

// clearSessionCookie expires the session cookie immediately.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (s *NexusServer) issueSessionToken(username, device string) (string, error) {
	tok, err := randHex(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO session_tokens (token, username, device_info, expires_at) VALUES (?, ?, ?, ?)",
		tok, username, device, expires,
	)
	return tok, err
}

func (s *NexusServer) issueAdminSessionToken(username string) (string, error) {
	tok, err := randHex(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(4 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO session_tokens (token, username, device_info, expires_at) VALUES (?, ?, ?, ?)",
		tok, username, "admin-portal", expires,
	)
	return tok, err
}

func (s *NexusServer) sessionUsername(token string) string {
	if token == "" {
		return ""
	}
	var u string
	var expires time.Time
	var revoked int
	err := s.DB.QueryRow(
		"SELECT username, expires_at, revoked FROM session_tokens WHERE token = ?",
		token,
	).Scan(&u, &expires, &revoked)
	if err != nil || revoked != 0 || time.Now().After(expires) {
		return ""
	}
	return u
}

func (s *NexusServer) revokeSession(token string) {
	s.DB.Exec("UPDATE session_tokens SET revoked = 1 WHERE token = ?", token)
}
