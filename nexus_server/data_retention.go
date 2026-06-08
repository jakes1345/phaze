package main

import (
	"log"
	"os"
	"strconv"
	"time"
)

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func itoa(n int) string { return strconv.Itoa(n) }

// Background sweepers that enforce data-minimization policy:
//   - Unverified accounts: deleted after 7 days
//   - Channel messages: deleted after 30 days (configurable)
//   - Stories: already handled by storiesExpireSweeper (24h)

// startDataRetentionSweepers fires the per-domain janitors on independent
// tickers. Called once from main after DB init.
func (s *NexusServer) startDataRetentionSweepers() {
	go s.sweepUnverifiedAccounts()
	go s.sweepOldChannelMessages()
	go s.sweepExpiredSessions()
	go s.sweepExpiredResetTokens()
	go s.sweepExpiredQRLoginTokens()
	go sweepAuthTracker()
}

// sweepExpiredSessions purges expired and revoked session tokens every 12h.
// L1: without this, the session_tokens table grows unbounded.
func (s *NexusServer) sweepExpiredSessions() {
	t := time.NewTicker(12 * time.Hour)
	defer t.Stop()
	for {
		<-t.C
		res, err := s.DB.Exec(
			`DELETE FROM session_tokens WHERE revoked = 1 OR expires_at < CURRENT_TIMESTAMP`)
		if err != nil {
			log.Printf("[retention] session sweep: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[retention] purged %d expired/revoked sessions", n)
		}
	}
}

// sweepExpiredResetTokens purges used/expired password reset tokens every 6h.
func (s *NexusServer) sweepExpiredResetTokens() {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		<-t.C
		res, err := s.DB.Exec(
			`DELETE FROM password_resets WHERE used = 1 OR expires_at < CURRENT_TIMESTAMP`)
		if err != nil {
			log.Printf("[retention] reset token sweep: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[retention] purged %d expired/used reset tokens", n)
		}
	}
}

// sweepUnverifiedAccounts deletes user rows that registered with an email,
// never confirmed the code, and have been sitting around > 7 days.
// Verified-with-email and anonymous-no-email accounts are untouched.
func (s *NexusServer) sweepUnverifiedAccounts() {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		<-t.C
		res, err := s.DB.Exec(
			`DELETE FROM users
			  WHERE is_verified = 0
			    AND email != ''
			    AND created_at < datetime('now','-7 days')`)
		if err != nil {
			log.Printf("[retention] unverified sweep: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[retention] purged %d unverified accounts", n)
		}
	}
}

// sweepOldChannelMessages removes Spaces channel messages older than the
// retention window. Reduces breach exposure for plaintext channel content.
// Honors PHAZE_CHANNEL_RETENTION_DAYS env (default 30, set 0 to disable).
func (s *NexusServer) sweepOldChannelMessages() {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		<-t.C
		days := 30
		if v := envInt("PHAZE_CHANNEL_RETENTION_DAYS", 30); v > 0 {
			days = v
		} else if v == 0 {
			continue // disabled
		}
		res, err := s.DB.Exec(
			`DELETE FROM channel_messages
			  WHERE created_at < datetime('now', ?)`,
			"-"+itoa(days)+" days")
		if err != nil {
			log.Printf("[retention] channel sweep: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			// Cascade: drop reactions for messages that no longer exist.
			s.DB.Exec(`DELETE FROM channel_reactions WHERE msg_id NOT IN (SELECT id FROM channel_messages)`)
			log.Printf("[retention] purged %d old channel messages (>%dd)", n, days)
		}
	}
}

// sweepExpiredQRLoginTokens purges expired/approved QR login tokens every 6h.
func (s *NexusServer) sweepExpiredQRLoginTokens() {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		<-t.C
		res, err := s.DB.Exec(
			`DELETE FROM qr_login_tokens WHERE approved = 1 OR expires_at < CURRENT_TIMESTAMP`)
		if err != nil {
			log.Printf("[retention] qr token sweep: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[retention] purged %d expired/approved QR login tokens", n)
		}
	}
}
