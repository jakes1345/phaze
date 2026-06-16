package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
	s.DB.Exec("DELETE FROM totp_backup_codes WHERE username = ?", username)
}

// generateBackupCodes creates 8 one-time recovery codes, stores them hashed,
// and returns the plaintext codes (shown to the user once).
func (s *NexusServer) generateBackupCodes(username string) ([]string, error) {
	s.DB.Exec("DELETE FROM totp_backup_codes WHERE username = ?", username)
	codes := make([]string, 8)
	for i := range codes {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		plain := fmt.Sprintf("%x", b) // 10-char hex code
		codes[i] = plain[:5] + "-" + plain[5:] // display as "abcde-f0123"
		h := sha256.Sum256([]byte(plain))
		s.DB.Exec("INSERT INTO totp_backup_codes (username, code_hash) VALUES (?, ?)",
			username, hex.EncodeToString(h[:]))
	}
	return codes, nil
}

// consumeBackupCode checks if the given code matches an unused backup code and
// marks it consumed. Returns true if valid.
func (s *NexusServer) consumeBackupCode(username, code string) bool {
	// Normalise: strip dashes
	var sb strings.Builder
	for _, c := range code {
		if c != '-' {
			sb.WriteRune(c)
		}
	}
	plain := sb.String()
	h := sha256.Sum256([]byte(plain))
	hash := hex.EncodeToString(h[:])
	res, err := s.DB.Exec(
		"UPDATE totp_backup_codes SET used = 1 WHERE username = ? AND code_hash = ? AND used = 0",
		username, hash,
	)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
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
