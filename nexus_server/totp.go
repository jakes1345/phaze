package main

import (
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
)

// totpUsedCodes prevents TOTP code replay within the same 30-second window.
// Key: "username:code", value: expiry time. Swept every 2 minutes.
var totpUsedCodes sync.Map

func init() {
	go func() {
		for range time.Tick(2 * time.Minute) {
			now := time.Now()
			totpUsedCodes.Range(func(k, v any) bool {
				if v.(time.Time).Before(now) {
					totpUsedCodes.Delete(k)
				}
				return true
			})
		}
	}()
}

func (s *NexusServer) totpStatus(username string) (secret string, enabled bool) {
	var e int
	s.DB.QueryRow("SELECT totp_secret, totp_enabled FROM users WHERE username = ?", username).Scan(&secret, &e)
	return secret, e == 1
}

func (s *NexusServer) generateTOTPURI(username string) (uri, secret string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Phaze",
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}
	return key.URL(), key.Secret(), nil
}

func (s *NexusServer) enableTOTP(username, secret, code string) bool {
	if !totp.Validate(code, secret) {
		return false
	}
	_, err := s.DB.Exec("UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE username = ?", secret, username)
	return err == nil
}

func (s *NexusServer) disableTOTP(username string) {
	s.DB.Exec("UPDATE users SET totp_secret = '', totp_enabled = 0 WHERE username = ?", username)
}

func (s *NexusServer) verifyTOTP(username, code string) bool {
	secret, enabled := s.totpStatus(username)
	if !enabled || secret == "" {
		return true
	}
	if !totp.Validate(code, secret) {
		return false
	}
	// Replay guard: reject if this exact code was already accepted within its window.
	key := username + ":" + code
	expiry := time.Now().Add(60 * time.Second) // 2 windows of safety
	if _, loaded := totpUsedCodes.LoadOrStore(key, expiry); loaded {
		return false
	}
	return true
}
