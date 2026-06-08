package main

import "time"

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
