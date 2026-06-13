package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

func (s *NexusServer) handleConnections(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.isIPBlocked(ip) {
		http.Error(w, "blocked", http.StatusForbidden)
		return
	}
	s.wsConnCountMu.Lock()
	if s.wsConnCount == nil {
		s.wsConnCount = make(map[string]int)
	}
	count := s.wsConnCount[ip]
	if count >= 10 {
		s.wsConnCountMu.Unlock()
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	s.wsConnCount[ip] = count + 1
	s.wsConnCountMu.Unlock()
	defer func() {
		s.wsConnCountMu.Lock()
		if s.wsConnCount != nil {
			s.wsConnCount[ip]--
			if s.wsConnCount[ip] <= 0 {
				delete(s.wsConnCount, ip)
			}
		}
		s.wsConnCountMu.Unlock()
	}()
	log.Printf("Incoming connection from %s: %s %s", ip, r.Method, r.URL.String())
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		metrics.wsConnectionsFail.Add(1)
		return
	}
	metrics.wsConnections.Add(1)
	defer ws.Close()

	// C3: cap inbound message size to 1 MB. The NexusMessage protocol is
	// small (typically < 50 KB even with envelopes); anything larger is
	// either a bug or an adversary trying to OOM the server.
	ws.SetReadLimit(1 << 20) // 1 MB

	// client owns the write mutex for this connection. Pre-auth writes use
	// it even before the client is registered in s.Clients.
	// msgLimiter: 20 msg/s sustained, burst 40. Accommodates typing
	// indicators + rapid sends without permitting a tight spam loop.
	client := &Client{
		Conn:       ws,
		IP:         clientIP(r),
		msgLimiter: rate.NewLimiter(rate.Limit(20), 40),
	}

	var username string

	for {
		var msg NexusMessage
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Read error: %v", err)
			if username != "" {
				// Compare-and-swap delete: only remove the map entry if it
				// still points at *this* connection. A concurrent login from
				// the same user kicks the previous session via Conn.Close(),
				// which wakes *this* read-loop with an error — without the
				// guard below we'd delete the freshly-installed new session
				// and mark the user offline even though they're online on
				// another device.
				s.Mu.Lock()
				current, ok := s.Clients[username]
				weWereReplaced := ok && current != client
				if ok && current == client {
					delete(s.Clients, username)
				}
				s.Mu.Unlock()
				if !weWereReplaced {
					s.broadcastPresence(username, "Offline")
					s.voiceRoomEvictUser(username)
					s.streamEvictUser(username)
					log.Printf("User %s disconnected", username)
				} else {
					log.Printf("User %s old session closed (replaced by newer login)", username)
				}
			}
			return
		}

		// Per-connection rate limit. Drop silently on overflow: an authed
		// spammer shouldn't learn they've tripped the limiter.
		if !client.msgLimiter.Allow() {
			log.Printf("[ratelimit] dropping %q from %s", msg.Type, username)
			continue
		}

		metrics.wsMessagesIn.Add(1)

		switch msg.Type {
		case "pstn_call":
			metrics.pstnAttempts.Add(1)
			if !pstnBridgeEnabled() {
				metrics.pstnRejected.Add(1)
				client.Send(NexusMessage{
					Type:  "pstn_status",
					Error: "PSTN bridge is disabled on this relay. Use in-app voice/video (WebRTC) with Phaze contacts — no phone network or Twilio required.",
				})
				continue
			}
			number := msg.Body
			// SECURITY CHECK: Verify this number belongs to this sender
			var verified int
			err := s.DB.QueryRow("SELECT phone_verified FROM users WHERE username = ? AND phone_number = ?", username, number).Scan(&verified)
			if err != nil || verified == 0 {
				client.Send(NexusMessage{Type: "pstn_status", Error: "Caller identity not verified. Please link your phone in Settings."})
				continue
			}

			log.Printf("[PSTN-SECURE] User %s initiating call to %s", msg.Sender, number)
			err = s.initiateTwilioCall(number)
			if err != nil {
				client.Send(NexusMessage{Type: "pstn_status", Error: "Telephony error: " + err.Error()})
			} else {
				client.Send(NexusMessage{Type: "pstn_status", Status: "Connecting via Sovereign Bridge..."})
			}

		case "register":
			if authTracker.isIPThrottled(client.IP) {
				metrics.authFailure.Add(1)
				if authTracker.shouldAutoBlock(client.IP) {
					log.Printf("[security] auto-blocking IP %s after %d registration/auth failures", client.IP, authIPBlockThreshold)
					s.blockIP(client.IP)
				}
				client.Send(NexusMessage{Type: "register_result", Error: "Too many registration/login attempts. Try again later."})
				time.Sleep(2 * time.Second)
				continue
			}
			code, err := s.registerUser(msg.Sender, msg.Email, msg.Mood, msg.Body)
			if err != nil {
				authTracker.recordFail(client.IP, "")
				client.Send(NexusMessage{Type: "register_result", Error: err.Error()})
			} else {
				s.DB.Exec("UPDATE users SET signup_ip = ?, last_ip = ? WHERE username = ?", client.IP, client.IP, msg.Sender)
				log.Printf("New user registered: %s (%s) from %s", msg.Sender, msg.Email, client.IP)
				verifyLink := "https://phazechat.world/verify-email?u=" + url.QueryEscape(msg.Sender) + "&code=" + url.QueryEscape(code)
				go s.sendEmailLogged(msg.Email, "Activate your Phaze Identity",
					"<h1>Welcome to Phaze</h1>"+
						"<p>Click the button below to verify your account:</p>"+
						"<p style=\"margin:24px 0\"><a href=\""+verifyLink+"\" style=\"background:#863bff;color:#fff;padding:14px 32px;border-radius:8px;text-decoration:none;font-weight:600;font-size:16px\">Verify My Account</a></p>"+
						"<p style=\"color:#888\">Or enter this code manually in the app: <b>"+code+"</b></p>")
				client.Send(NexusMessage{Type: "register_result", Status: "pending_verification"})
			}

		case "verify_email":
			if s.verifyUser(msg.Sender, msg.Body) {
				s.autoJoinGlobalSpace(msg.Sender)
				client.Send(NexusMessage{Type: "verify_result", Status: "ok"})
			} else {
				client.Send(NexusMessage{Type: "verify_result", Error: "Invalid verification code"})
			}

		case "status_update":
			s.Mu.Lock()
			if client, ok := s.Clients[username]; ok {
				client.Status = msg.Body
				log.Printf("User %s changed status to %s", username, msg.Body)
			}
			s.Mu.Unlock()
			s.broadcastPresence(username, msg.Body)

		case "request_phone_link":
			number := msg.Body
			// H4: use crypto/rand instead of time-based code (was predictable).
			code, _ := randDigits(6)
			_, err := s.DB.Exec("UPDATE users SET phone_verification_code = ?, phone_number = ? WHERE username = ?", code, number, username)
			if err != nil {
				client.Send(NexusMessage{Type: "phone_link_result", Error: "Update failed"})
			} else {
				log.Printf("[SMS] Sending verification to %s", number)
				go s.sendSMS(number, "Your Phaze verification code is: "+code)
				client.Send(NexusMessage{Type: "phone_link_result", Status: "code_sent"})
			}

		case "verify_phone_link":
			var dbCode string
			err := s.DB.QueryRow("SELECT phone_verification_code FROM users WHERE username = ?", username).Scan(&dbCode)
			// H6: constant-time compare for phone verification codes.
			if err == nil && dbCode != "" && msg.Body != "" && subtle.ConstantTimeCompare([]byte(dbCode), []byte(msg.Body)) == 1 {
				s.DB.Exec("UPDATE users SET phone_verified = 1, phone_verification_code = NULL WHERE username = ?", username)
				client.Send(NexusMessage{Type: "phone_link_result", Status: "verified"})
			} else {
				s.DB.Exec("UPDATE users SET phone_verification_code = NULL WHERE username = ?", username)
				client.Send(NexusMessage{Type: "phone_link_result", Error: "Invalid code. Security lockout: please request a new code."})
			}

		case "update_profile":
			// C5: Use server-side username, NOT client-supplied msg.Sender.
			// Without this, any authed user could overwrite another user's profile.
			if username == "" {
				continue
			}
			_, err := s.DB.Exec("UPDATE users SET mood = ?, display_name = ? WHERE username = ?",
				msg.Mood, msg.DisplayName, username)
			if err != nil {
				client.Send(NexusMessage{Type: "update_result", Error: "Update failed"})
			} else {
				log.Printf("Profile updated for %s: %s | %s", username, msg.DisplayName, msg.Mood)
				client.Send(NexusMessage{Type: "update_result", Status: "ok"})
				s.broadcastProfileUpdate(username, msg.DisplayName, msg.Mood)
			}

		case "auth":
			if msg.Body == "" {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "Password required"})
				continue
			}
			// Brute-force protection: check IP and user lockouts BEFORE doing
			// expensive bcrypt work. This stops credential-stuffing bots cold.
			if authTracker.isIPThrottled(client.IP) {
				metrics.authFailure.Add(1)
				if authTracker.shouldAutoBlock(client.IP) {
					log.Printf("[security] auto-blocking IP %s after %d auth failures", client.IP, authIPBlockThreshold)
					s.blockIP(client.IP)
				}
				client.Send(NexusMessage{Type: "auth_result", Error: "Too many failed attempts. Try again later."})
				// Intentional delay to waste attacker time
				time.Sleep(2 * time.Second)
				continue
			}
			if authTracker.isUserLocked(msg.Sender) {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "Account temporarily locked due to too many failed attempts. Try again in 30 minutes."})
				time.Sleep(2 * time.Second)
				continue
			}
			if !s.authenticateUser(msg.Sender, msg.Body) {
				metrics.authFailure.Add(1)
				authTracker.recordFail(client.IP, msg.Sender)
				// Progressive delay: 500ms per failure count (max 5s).
				// Slows brute-force without hurting legit users.
				authTracker.mu.Lock()
				delay := time.Duration(0)
				if f, ok := authTracker.ipFails[client.IP]; ok {
					delay = time.Duration(f.count) * 500 * time.Millisecond
					if delay > 5*time.Second {
						delay = 5 * time.Second
					}
				}
				authTracker.mu.Unlock()
				if delay > 0 {
					time.Sleep(delay)
				}
				client.Send(NexusMessage{Type: "auth_result", Error: "Invalid username or password"})
				continue
			}
			if banned, reason := s.userBanInfo(msg.Sender); banned {
				metrics.authFailure.Add(1)
				body := "Account suspended"
				if reason != "" {
					body += ": " + reason
				}
				client.Send(NexusMessage{Type: "auth_result", Error: body, Status: "banned"})
				continue
			}
			// Brute-force protection for the 6-digit TOTP code (only relevant
			// once the password already checked out).
			if authTracker.isTOTPThrottled(msg.Sender) {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "Too many 2FA attempts — wait a few minutes", Status: "totp_required"})
				continue
			}
			if !s.verifyTOTP(msg.Sender, msg.TOTPCode) {
				metrics.authFailure.Add(1)
				authTracker.recordTOTPFail(msg.Sender)
				client.Send(NexusMessage{Type: "auth_result", Error: "2FA code required or invalid", Status: "totp_required"})
				continue
			}
			authTracker.recordTOTPSuccess(msg.Sender)
			metrics.authSuccess.Add(1)
			authTracker.recordSuccess(client.IP, msg.Sender) // clear fail counters
			username = msg.Sender
			s.DB.Exec("UPDATE users SET last_ip = ?, last_login_at = CURRENT_TIMESTAMP WHERE username = ?", client.IP, username)
			s.autoJoinGlobalSpace(username)
			sessTok, _ := s.issueSessionToken(username, msg.DeviceInfo)
			s.Mu.Lock()
			if existing, ok := s.Clients[username]; ok {
				existing.Send(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			client.Username = username
			client.Status = "Online"
			s.Clients[username] = client
			s.Mu.Unlock()
			log.Printf("User %s authenticated", username)

			client.Send(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				QRToken:    sessTok,
				TurnConfig: s.generateMediaToken(username),
			})

			// Broadcast online presence to friends
			s.broadcastPresence(username, "Online")

			// Deliver any offline messages
			s.deliverOfflineMessages(username)

			// Send pending friend requests
			pending := s.getPendingRequests(username)
			if len(pending) > 0 {
				client.Send(NexusMessage{Type: "pending_requests", Results: pending})
			}

			// Send conversations this user belongs to
			for _, cm := range s.userConversations(username) {
				cm.Type = "convo_info"
				client.Send(cm)
			}

			// Send friends list with online status
			friends := s.getFriends(username)
			for _, f := range friends {
				status := "Offline"
				s.Mu.RLock()
				if c, ok := s.Clients[f]; ok {
					status = c.Status
				}
				s.Mu.RUnlock()
				client.Send(NexusMessage{Type: "friend_status", Sender: f, Status: status})
			}

		case "session_auth":
			if authTracker.isIPThrottled(client.IP) {
				metrics.authFailure.Add(1)
				if authTracker.shouldAutoBlock(client.IP) {
					log.Printf("[security] auto-blocking IP %s after %d auth failures", client.IP, authIPBlockThreshold)
					s.blockIP(client.IP)
				}
				client.Send(NexusMessage{Type: "auth_result", Error: "Too many failed attempts. Try again later."})
				time.Sleep(2 * time.Second)
				continue
			}
			u := s.sessionUsername(msg.QRToken)
			if u == "" {
				metrics.authFailure.Add(1)
				authTracker.recordFail(client.IP, "")
				authTracker.mu.Lock()
				delay := time.Duration(0)
				if f, ok := authTracker.ipFails[client.IP]; ok {
					delay = time.Duration(f.count) * 500 * time.Millisecond
					if delay > 5*time.Second {
						delay = 5 * time.Second
					}
				}
				authTracker.mu.Unlock()
				if delay > 0 {
					time.Sleep(delay)
				}
				client.Send(NexusMessage{Type: "auth_result", Error: "Session expired, please log in"})
				continue
			}
			authTracker.recordSuccess(client.IP, u)
			if banned, reason := s.userBanInfo(u); banned {
				body := "Account suspended"
				if reason != "" {
					body += ": " + reason
				}
				s.revokeSession(msg.QRToken)
				client.Send(NexusMessage{Type: "auth_result", Error: body, Status: "banned"})
				continue
			}
			username = u
			s.DB.Exec("UPDATE users SET last_ip = ?, last_login_at = CURRENT_TIMESTAMP WHERE username = ?", client.IP, username)
			s.autoJoinGlobalSpace(username)
			s.Mu.Lock()
			if existing, ok := s.Clients[username]; ok {
				existing.Send(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			client.Username = username
			client.Status = "Online"
			s.Clients[username] = client
			s.Mu.Unlock()
			log.Printf("User %s resumed via session token from %s", username, client.IP)
			client.Send(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				QRToken:    msg.QRToken,
				TurnConfig: s.generateMediaToken(username),
			})
			s.broadcastPresence(username, "Online")
			s.deliverOfflineMessages(username)

			// Send friends list with online status (same as auth path).
			for _, f := range s.getFriends(username) {
				status := "Offline"
				s.Mu.RLock()
				if c, ok := s.Clients[f]; ok {
					status = c.Status
				}
				s.Mu.RUnlock()
				client.Send(NexusMessage{Type: "friend_status", Sender: f, Status: status})
			}
			// Send pending friend requests.
			if pending := s.getPendingRequests(username); len(pending) > 0 {
				client.Send(NexusMessage{Type: "pending_requests", Results: pending})
			}
			// Send conversations.
			for _, cm := range s.userConversations(username) {
				cm.Type = "convo_info"
				client.Send(cm)
			}

		case "revoke_session":
			if username == "" || msg.QRToken == "" {
				continue
			}
			s.revokeSession(msg.QRToken)
			client.Send(NexusMessage{Type: "session_revoked", Status: "ok"})

		case "delete_account":
			// GDPR Article 17 — right to erasure. Requires the user to be
			// authenticated AND to confirm their password in msg.Body so an
			// attacker with a stolen session token can't nuke the account.
			if username == "" {
				client.Send(NexusMessage{Type: "delete_account_result", Error: "Not authenticated"})
				continue
			}
			if msg.Body == "" || !s.authenticateUser(username, msg.Body) {
				client.Send(NexusMessage{Type: "delete_account_result", Error: "Password confirmation required"})
				continue
			}
			if err := s.deleteAccount(username); err != nil {
				log.Printf("[delete_account] %s: %v", username, err)
				client.Send(NexusMessage{Type: "delete_account_result", Error: "Internal error — try again"})
				continue
			}
			log.Printf("[delete_account] erased account %s", username)
			// Notify friends so their rosters update.
			s.broadcastPresence(username, "Offline")
			client.Send(NexusMessage{Type: "delete_account_result", Status: "ok"})
			// Drop the connection and the in-memory client entry.
			s.Mu.Lock()
			if cur, ok := s.Clients[username]; ok && cur == client {
				delete(s.Clients, username)
			}
			s.Mu.Unlock()
			ws.Close()
			return

		case "resend_verification":
			// H3: resend_verification previously used msg.Sender (client-supplied),
			// allowing unauthenticated spam. Now: if authed, use server-side username;
			// if not, use msg.Sender but only for unverified accounts (pre-auth flow).
			targetUser := msg.Sender
			if username != "" {
				targetUser = username
			}
			if targetUser == "" {
				client.Send(NexusMessage{Type: "register_result", Error: "Username required"})
				continue
			}
			var email string
			var verified int
			err := s.DB.QueryRow("SELECT email, is_verified FROM users WHERE username = ?", targetUser).Scan(&email, &verified)
			if err != nil || email == "" {
				// Don't reveal whether the user exists.
				client.Send(NexusMessage{Type: "register_result", Status: "code_resent"})
				continue
			}
			if verified == 1 {
				// Already verified — nothing to resend.
				client.Send(NexusMessage{Type: "register_result", Status: "code_resent"})
				continue
			}
			code, err := randDigits(6)
			if err != nil {
				client.Send(NexusMessage{Type: "register_result", Error: "Internal error"})
				continue
			}
			if _, err := s.DB.Exec("UPDATE users SET verification_code = ? WHERE username = ?", code, targetUser); err != nil {
				client.Send(NexusMessage{Type: "register_result", Error: "Database error"})
				continue
			}
			go s.sendEmailLogged(email, "Your Phaze activation code",
				"<h1>New code</h1><p>Your activation code is: <b>"+code+"</b></p>")
			client.Send(NexusMessage{Type: "register_result", Status: "code_resent"})

		case "enable_totp":
			if username == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "Not authenticated"})
				continue
			}
			uri, secret, err := s.generateTOTPURI(username)
			if err != nil {
				client.Send(NexusMessage{Type: "totp_result", Error: "Could not generate secret"})
				continue
			}
			// Stash secret pending verification; set enabled=0 so auth still allows login until confirmed.
			s.DB.Exec("UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE username = ?", secret, username)
			client.Send(NexusMessage{Type: "totp_result", Status: "pending_confirm", TOTPURI: uri})

		case "confirm_totp":
			if username == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "Not authenticated"})
				continue
			}
			if authTracker.isTOTPThrottled(username) {
				client.Send(NexusMessage{Type: "totp_result", Error: "Too many attempts — wait a few minutes"})
				continue
			}
			secret, _ := s.totpStatus(username)
			if secret == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "No pending TOTP enrollment"})
				continue
			}
			if !s.enableTOTP(username, secret, msg.TOTPCode) {
				authTracker.recordTOTPFail(username)
				client.Send(NexusMessage{Type: "totp_result", Error: "Invalid code"})
				continue
			}
			authTracker.recordTOTPSuccess(username)
			client.Send(NexusMessage{Type: "totp_result", Status: "enabled"})

		case "disable_totp":
			if username == "" {
				continue
			}
			if !s.authenticateUser(username, msg.Body) {
				client.Send(NexusMessage{Type: "totp_result", Error: "Password required"})
				continue
			}
			s.disableTOTP(username)
			client.Send(NexusMessage{Type: "totp_result", Status: "disabled"})

		case "forgot_password":
			// Accept email in msg.Email; always ack "sent" to avoid user enumeration.
			// Rate-limit per email address: 1 reset per 5 minutes.
			if msg.Email != "" && !forgotPasswordLimiter.allow(msg.Email) {
				client.Send(NexusMessage{Type: "forgot_password_result", Status: "sent"})
				continue
			}
			go func(addr string) {
				tok, user, err := s.createPasswordReset(addr)
				if err != nil {
					log.Printf("[reset] no user for %s", addr)
					return
				}
				link := "https://phazechat.world/reset?token=" + tok
				s.sendEmailLogged(addr, "Reset your Phaze password",
					"<h1>Reset password</h1><p>Hello "+user+",</p><p>Click to reset (valid 1 hour): <a href=\""+link+"\">"+link+"</a></p>")
			}(msg.Email)
			client.Send(NexusMessage{Type: "forgot_password_result", Status: "sent"})

		case "reset_password":
			if err := s.consumePasswordReset(msg.QRToken, msg.Body); err != nil {
				client.Send(NexusMessage{Type: "reset_password_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "reset_password_result", Status: "ok"})

		case "change_password":
			// msg.Body = "oldpass:newpass" — split on first colon only
			if username == "" {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Not authenticated"})
				continue
			}
			idx := strings.Index(msg.Body, ":")
			if idx < 1 || idx == len(msg.Body)-1 {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Malformed request"})
				continue
			}
			oldPw, newPw := msg.Body[:idx], msg.Body[idx+1:]
			if !s.authenticateUser(username, oldPw) {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Current password incorrect"})
				continue
			}
			if len(newPw) < 8 {
				client.Send(NexusMessage{Type: "change_password_result", Error: "New password must be at least 8 characters"})
				continue
			}
			// C2: pre-hash with SHA-256 to match registerUser/authenticateUser.
			pwHash := sha256.Sum256([]byte(newPw))
			hash, err := bcrypt.GenerateFromPassword(pwHash[:], bcrypt.DefaultCost)
			if err != nil {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Internal error"})
				continue
			}
			if _, err := s.DB.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username); err != nil {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Database error"})
				continue
			}
			log.Printf("[security] %s changed password", username)
			client.Send(NexusMessage{Type: "change_password_result", Status: "ok"})

		case "qr_login_create":
			tok, err := s.createQRLogin()
			if err != nil {
				client.Send(NexusMessage{Type: "qr_login_result", Error: "Could not create QR token"})
				continue
			}
			client.Send(NexusMessage{
				Type:    "qr_login_result",
				Status:  "pending",
				QRToken: tok,
				QRData:  "phaze://login?token=" + tok,
			})

		case "qr_login_approve":
			if username == "" {
				client.Send(NexusMessage{Type: "qr_login_result", Error: "Not authenticated"})
				continue
			}
			if err := s.approveQRLogin(msg.QRToken, username, msg.DeviceInfo); err != nil {
				client.Send(NexusMessage{Type: "qr_login_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "qr_login_result", Status: "approved"})

		case "qr_login_check":
			if authTracker.isIPThrottled(client.IP) {
				metrics.authFailure.Add(1)
				if authTracker.shouldAutoBlock(client.IP) {
					log.Printf("[security] auto-blocking IP %s after %d auth failures", client.IP, authIPBlockThreshold)
					s.blockIP(client.IP)
				}
				client.Send(NexusMessage{Type: "qr_login_result", Error: "Too many failed attempts. Try again later."})
				time.Sleep(2 * time.Second)
				continue
			}
			u, sess, approved, exists := s.checkQRLogin(msg.QRToken)
			if !approved {
				if !exists {
					metrics.authFailure.Add(1)
					authTracker.recordFail(client.IP, "")
				}
				client.Send(NexusMessage{Type: "qr_login_result", Status: "pending", QRToken: msg.QRToken})
				continue
			}
			authTracker.recordSuccess(client.IP, u)
			// Promote this socket onto the approved session.
			username = u
			s.Mu.Lock()
			if existing, ok := s.Clients[username]; ok {
				existing.Send(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			client.Username = username
			client.Status = "Online"
			s.Clients[username] = client
			s.Mu.Unlock()
			log.Printf("User %s logged in via QR", username)
			client.Send(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				QRToken:    sess,
				TurnConfig: s.generateMediaToken(username),
			})
			s.broadcastPresence(username, "Online")
			s.deliverOfflineMessages(username)

		case "msg":
			if username == "" {
				continue
			}
			// Authoritative sender = authenticated session, not client claim
			msg.Sender = username
			if msg.Recipient == "PhazeBot" {
				s.handleBotMessage(client, msg)
				continue
			}
			// Trust & safety: drop if either party has blocked the other.
			// Sender sees a benign delivered_offline status — no oracle leak.
			if s.isBlocked(msg.Recipient, msg.Sender) || s.isBlocked(msg.Sender, msg.Recipient) {
				log.Printf("[block] dropped %s -> %s", msg.Sender, msg.Recipient)
				client.Send(NexusMessage{
					Type: "msg_status", Body: "delivered_offline", Sender: msg.Recipient,
				})
				continue
			}
			log.Printf("Message from %s to %s", msg.Sender, msg.Recipient)
			// Durable cross-device history: store the ciphertext exactly as
			// the sender produced it. Idempotent on msg_id so retries are
			// safe. Both ends fetch this via "dm_history" on chat open.
			s.persistDM(msg.MsgID, msg.Sender, msg.Recipient, msg.Body)
			s.Mu.RLock()
			recipientClient, online := s.Clients[msg.Recipient]
			s.Mu.RUnlock()

			if online {
				recipientClient.Send(msg)
			} else {
				s.storeOfflineMessage(msg.Sender, msg.Recipient, msg.Body, "msg")
				client.Send(NexusMessage{
					Type:   "msg_status",
					Body:   "delivered_offline",
					Sender: msg.Recipient,
				})
			}

		case "block":
			if username == "" {
				continue
			}
			if err := s.blockUser(username, msg.Recipient); err != nil {
				client.Send(NexusMessage{Type: "block_result", Error: err.Error()})
			} else {
				log.Printf("[block] %s blocked %s", username, msg.Recipient)
				client.Send(NexusMessage{Type: "block_result", Status: "blocked", Recipient: msg.Recipient})
			}

		case "unblock":
			if username == "" {
				continue
			}
			log.Printf("[block] %s unblocking %s", username, msg.Recipient)
			if err := s.unblockUser(username, msg.Recipient); err != nil {
				client.Send(NexusMessage{Type: "block_result", Error: err.Error()})
			} else {
				client.Send(NexusMessage{Type: "block_result", Status: "unblocked", Recipient: msg.Recipient})
			}

		case "list_blocks":
			if username == "" {
				continue
			}
			client.Send(NexusMessage{Type: "blocks", Results: s.listBlocks(username)})

		case "register_fcm_token":
			if username == "" {
				continue
			}
			if msg.Body == "" {
				continue
			}
			s.DB.Exec("UPDATE users SET fcm_token = ? WHERE username = ?", msg.Body, username)
			log.Printf("[FCM] registered token for %s", username)

		case "subscribe_push":
			if username == "" {
				continue
			}
			// msg.Body = JSON {"endpoint":"…","p256dh":"…","auth":"…"}
			var sub struct {
				Endpoint string `json:"endpoint"`
				P256DH   string `json:"p256dh"`
				Auth     string `json:"auth"`
			}
			if err := json.Unmarshal([]byte(msg.Body), &sub); err != nil || sub.Endpoint == "" {
				client.Send(NexusMessage{Type: "push_result", Error: "Invalid subscription"})
				continue
			}
			s.DB.Exec(`INSERT INTO push_subscriptions (username, endpoint, p256dh, auth)
				VALUES (?, ?, ?, ?) ON CONFLICT(endpoint) DO UPDATE SET username=excluded.username`,
				username, sub.Endpoint, sub.P256DH, sub.Auth)
			client.Send(NexusMessage{Type: "push_result", Status: "subscribed"})

		case "unsubscribe_push":
			if username == "" {
				continue
			}
			s.DB.Exec("DELETE FROM push_subscriptions WHERE username = ? AND endpoint = ?", username, msg.Body)
			client.Send(NexusMessage{Type: "push_result", Status: "unsubscribed"})

		case "list_sessions":
			if username == "" {
				continue
			}
			sessions := s.listSessions(username)
			data, _ := json.Marshal(sessions)
			client.Send(NexusMessage{Type: "sessions_list", Body: string(data)})

		case "revoke_session_by_token":
			if username == "" {
				continue
			}
			// Only revoke sessions belonging to this user
			s.DB.Exec("UPDATE session_tokens SET revoked = 1 WHERE token = ? AND username = ?", msg.Body, username)
			client.Send(NexusMessage{Type: "session_revoked", Status: "ok"})

		case "invite_email":
			if username == "" {
				continue
			}
			if !validEmail(msg.Email) {
				client.Send(NexusMessage{Type: "invite_result", Error: "Invalid email"})
				continue
			}
			code, err := randHex(16)
			if err != nil {
				client.Send(NexusMessage{Type: "invite_result", Error: "Internal error"})
				continue
			}
			s.DB.Exec("INSERT INTO invite_codes (code, inviter, email) VALUES (?, ?, ?)", code, username, msg.Email)
			link := "https://phazechat.world/web?invite=" + code
			go s.sendEmailLogged(msg.Email, username+" invited you to Phaze",
				"<h1>You've been invited to Phaze!</h1><p><b>"+username+"</b> wants to chat with you on Phaze — free encrypted messaging, calls, and more.</p>"+
					"<p><a href=\""+link+"\" style=\"background:#863bff;color:#fff;padding:12px 24px;border-radius:8px;text-decoration:none;font-weight:bold;display:inline-block;\">Join Phaze</a></p>"+
					"<p style=\"font-size:12px;color:#666\">Or paste this link: "+link+"</p>")
			client.Send(NexusMessage{Type: "invite_result", Status: "sent"})

		case "report_abuse":
			if username == "" {
				continue
			}
			// msg.Recipient = subject (offending user), msg.Status = reason tag, msg.Body = freeform
			if err := s.recordAbuseReport(username, msg.Recipient, msg.Status, msg.Body); err != nil {
				client.Send(NexusMessage{Type: "report_result", Error: err.Error()})
			} else {
				log.Printf("[abuse] report from %s about %s reason=%s", username, msg.Recipient, msg.Status)
				client.Send(NexusMessage{Type: "report_result", Status: "received"})
			}

		case "typing":
			if username == "" {
				continue
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(NexusMessage{
					Type:   "typing",
					Sender: username,
				})
			}
			s.Mu.RUnlock()

		// Edit / delete / react are best-effort live relays for DMs. The body
		// (for msg_edit) and emoji (for msg_react) are still E2EE-encrypted
		// client-side; the server only forwards the envelope. If the
		// recipient is offline the signal is dropped — clients reconcile
		// from their own local history when they reconnect.
		case "msg_edit", "msg_delete", "msg_react":
			if username == "" || msg.Recipient == "" || msg.MsgID == "" {
				continue
			}
			if s.isBlocked(msg.Recipient, username) || s.isBlocked(username, msg.Recipient) {
				continue
			}
			msg.Sender = username
			// Mirror the action into durable history so both sides see it
			// after a refresh / new-device login.
			switch msg.Type {
			case "msg_edit":
				if len(msg.Body) > 10000 {
					client.Send(NexusMessage{Type: "msg_edit_result", Error: "message too long"})
					continue
				}
				s.editDM(msg.MsgID, username, msg.Body)
			case "msg_delete":
				s.deleteDM(msg.MsgID, username)
			case "msg_react":
				if msg.Reaction != "" {
					s.toggleReaction(msg.MsgID, username, msg.Reaction)
				}
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			}
			s.Mu.RUnlock()

		case "dm_history":
			if username == "" || msg.Recipient == "" {
				continue
			}
			limit := 100
			rows := s.fetchDMHistory(username, msg.Recipient, msg.HistoryFrom, limit)
			client.Send(NexusMessage{
				Type:      "dm_history",
				Recipient: msg.Recipient,
				DMHistory: rows,
			})

		// Key backup put/get/delete: server is dumb storage for an opaque
		// PIN-encrypted blob containing the user's NaCl keypair. The plain
		// keys never touch the server.
		case "key_backup_put":
			if username == "" {
				continue
			}
			// Accept the backup blob from either the structured KeyBackup field
			// (Go desktop / web) or the Token string field (Android / new clients).
			var b *KeyBackupPayload
			if msg.KeyBackup != nil {
				b = msg.KeyBackup
			} else if msg.Token != "" {
				b = &KeyBackupPayload{}
				if err := json.Unmarshal([]byte(msg.Token), b); err != nil {
					client.Send(NexusMessage{Type: "key_backup_result", Error: "invalid backup token: " + err.Error()})
					continue
				}
			}
			if b == nil || b.Ciphertext == "" || b.Salt == "" || b.Iterations < 10000 {
				client.Send(NexusMessage{Type: "key_backup_result", Error: "invalid backup payload"})
				continue
			}
			if _, err := s.DB.Exec(
				`INSERT INTO key_backups (username, ciphertext, salt, iterations) VALUES (?, ?, ?, ?)
				 ON CONFLICT(username) DO UPDATE SET ciphertext = excluded.ciphertext, salt = excluded.salt,
				 iterations = excluded.iterations, created_at = CURRENT_TIMESTAMP`,
				username, b.Ciphertext, b.Salt, b.Iterations); err != nil {
				client.Send(NexusMessage{Type: "key_backup_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "key_backup_result", Status: "stored"})

		case "key_backup_get":
			if username == "" {
				continue
			}
			var ct, salt, created string
			var iters int
			err := s.DB.QueryRow(
				`SELECT ciphertext, salt, iterations, created_at FROM key_backups WHERE username = ?`,
				username,
			).Scan(&ct, &salt, &iters, &created)
			if err != nil {
				client.Send(NexusMessage{Type: "key_backup_result", Status: "not_found", Error: "no backup found"})
				continue
			}
			// Build the JSON blob string so Token-based clients (Android) can parse directly.
			blobBytes, _ := json.Marshal(KeyBackupPayload{Ciphertext: ct, Salt: salt, Iterations: iters})
			client.Send(NexusMessage{
				Type:   "key_backup_result",
				Status: "ok",
				KeyBackup: &KeyBackupPayload{
					Ciphertext: ct, Salt: salt, Iterations: iters, CreatedAt: created,
				},
				Token: string(blobBytes),
			})

		case "key_backup_delete":
			if username == "" {
				continue
			}
			s.DB.Exec(`DELETE FROM key_backups WHERE username = ?`, username)
			client.Send(NexusMessage{Type: "key_backup_result", Status: "deleted"})

		// Device link: create / approve / poll a short-lived link code. The
		// new device shows the code; the existing logged-in device "approves"
		// to authorize a session for the new one. Reuses the qr_login_* DB
		// table — qr_login was already the same pattern for QR sign-in.
		case "link_create":
			tok, err := s.createQRLogin()
			if err != nil {
				client.Send(NexusMessage{Type: "link_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "link_result", Status: "ok", Token: tok})

		case "link_approve":
			if username == "" || msg.Token == "" {
				continue
			}
			device := msg.DeviceInfo
			if device == "" {
				device = "linked-device"
			}
			if err := s.approveQRLogin(msg.Token, username, device); err != nil {
				client.Send(NexusMessage{Type: "link_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "link_result", Status: "approved"})

		case "link_check":
			if msg.Token == "" {
				continue
			}
			if authTracker.isIPThrottled(client.IP) {
				metrics.authFailure.Add(1)
				if authTracker.shouldAutoBlock(client.IP) {
					log.Printf("[security] auto-blocking IP %s after %d auth failures", client.IP, authIPBlockThreshold)
					s.blockIP(client.IP)
				}
				client.Send(NexusMessage{Type: "link_check", Status: "pending", Error: "Too many attempts"})
				time.Sleep(2 * time.Second)
				continue
			}
			u, sess, approved, exists := s.checkQRLogin(msg.Token)
			if !approved {
				if !exists {
					metrics.authFailure.Add(1)
					authTracker.recordFail(client.IP, "")
				}
				client.Send(NexusMessage{Type: "link_check", Status: "pending"})
				continue
			}
			authTracker.recordSuccess(client.IP, u)
			if approved {
				client.Send(NexusMessage{Type: "link_check", Status: "approved", Sender: u, QRToken: sess})
			} else {
				client.Send(NexusMessage{Type: "link_check", Status: "pending"})
			}

		case "presence":
			if username == "" {
				continue
			}
			// Directed key handoff (native_client replies to key_request with a
			// presence carrying public_key + recipient = requester).
			if msg.Recipient != "" && len(msg.PublicKey) == 32 && s.areFriends(username, msg.Recipient) {
				msg.Sender = username
				s.Mu.RLock()
				if peer, ok := s.Clients[msg.Recipient]; ok {
					if err := peer.Send(msg); err != nil {
						log.Printf("[presence] key forward to %s: %v", msg.Recipient, err)
					}
				}
				s.Mu.RUnlock()
			}
			log.Printf("User %s is now %s", username, msg.Status)
			s.Mu.Lock()
			if client, ok := s.Clients[username]; ok {
				client.Status = msg.Status
			}
			s.Mu.Unlock()
			s.broadcastPresence(username, msg.Status)

		case "search":
			if username == "" {
				continue
			}
			log.Printf("User %s searching for: %s", username, msg.Body)
			results := s.searchUsers(msg.Body, username)
			client.Send(NexusMessage{
				Type:    "search_results",
				Results: results,
			})

		case "friend_request":
			if username == "" {
				continue
			}
			err := s.sendFriendRequest(username, msg.Recipient)
			if err != nil {
				log.Printf("Friend request error: %v", err)
				continue
			}
			log.Printf("Friend request: %s -> %s", username, msg.Recipient)
			// Notify recipient if online
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(NexusMessage{
					Type:   "friend_request",
					Sender: username,
				})
			}
			s.Mu.RUnlock()

		case "friend_accept":
			if username == "" {
				continue
			}
			requester := msg.Recipient
			if requester == "" {
				requester = msg.Body
			}
			err := s.acceptFriendRequest(requester, username)
			if err != nil {
				log.Printf("Friend accept error: %v", err)
				continue
			}
			log.Printf("Friend accepted: %s accepted %s", username, requester)
			s.Mu.RLock()
			if requesterClient, ok := s.Clients[requester]; ok {
				requesterClient.Send(NexusMessage{
					Type:   "friend_accepted",
					Sender: username,
				})
			}
			s.Mu.RUnlock()

		case "friend_reject":
			if username == "" {
				continue
			}
			_ = s.rejectFriendRequest(msg.Sender, username)
			log.Printf("Friend reject: %s rejected %s", username, msg.Sender)

		case "friend_remove":
			if username == "" {
				continue
			}
			_ = s.removeFriend(username, msg.Recipient)
			log.Printf("Friend removed: %s <-> %s", username, msg.Recipient)
			s.Mu.RLock()
			if peer, ok := s.Clients[msg.Recipient]; ok {
				peer.Send(NexusMessage{Type: "friend_removed", Sender: username})
			}
			s.Mu.RUnlock()

		case "convo_create":
			if username == "" {
				continue
			}
			// Only accept members who are already accepted friends of the
			// creator. Without this, any authed user can spam strangers into
			// unsolicited group chats. Self is always allowed.
			friendSet := map[string]bool{username: true}
			for _, f := range s.getFriends(username) {
				friendSet[f] = true
			}
			eligible := msg.Members[:0:0]
			for _, m := range msg.Members {
				if friendSet[m] {
					eligible = append(eligible, m)
				}
			}
			if len(eligible) == 0 {
				client.Send(NexusMessage{Type: "convo_error", Error: "No eligible members — add friends first"})
				continue
			}
			if err := s.createConversation(msg.ConvoID, msg.ConvoName, username, eligible); err != nil {
				client.Send(NexusMessage{Type: "convo_error", Error: err.Error()})
				continue
			}
			members := s.conversationMembers(msg.ConvoID)
			notice := NexusMessage{
				Type:      "convo_created",
				ConvoID:   msg.ConvoID,
				ConvoName: msg.ConvoName,
				Members:   members,
				Sender:    username,
			}
			s.Mu.RLock()
			for _, m := range members {
				if c, ok := s.Clients[m]; ok {
					c.Send(notice)
				}
			}
			s.Mu.RUnlock()
			log.Printf("Conversation %s (%s) created by %s with %d members", msg.ConvoID, msg.ConvoName, username, len(members))

		case "convo_msg":
			if username == "" || msg.ConvoID == "" {
				continue
			}
			metrics.convoMessages.Add(1)
			members := s.conversationMembers(msg.ConvoID)
			var convoName string
			_ = s.DB.QueryRow(`SELECT name FROM conversations WHERE id = ?`, msg.ConvoID).Scan(&convoName)
			pushTitle := username
			if convoName != "" {
				pushTitle = username + " in " + convoName
			}
			pushBody := "Sent a message"

			s.Mu.RLock()
			for _, m := range members {
				if m == username {
					continue
				}
				// Prefer per-member envelope so the server never sees plaintext.
				// Fall back to msg.Body for older clients still using the legacy
				// plaintext fan-out path.
				body := msg.Body
				if msg.Envelopes != nil {
					if env, ok := msg.Envelopes[m]; ok {
						body = env
					}
				}
				fanout := NexusMessage{
					Type:    "convo_msg",
					Sender:  username,
					Body:    body,
					ConvoID: msg.ConvoID,
				}
				if c, ok := s.Clients[m]; ok {
					c.Send(fanout)
				} else {
					s.DB.Exec(`INSERT INTO offline_messages (sender, recipient, body, msg_type, convo)
						VALUES (?, ?, ?, 'convo_msg', ?)`, username, m, body, msg.ConvoID)
					go s.sendWebPush(m, pushTitle, pushBody)
					go s.sendFCMPush(m, pushTitle, pushBody)
				}
			}
			s.Mu.RUnlock()

		case "convo_leave":
			if username == "" {
				continue
			}
			_ = s.leaveConversation(msg.ConvoID, username)
			members := s.conversationMembers(msg.ConvoID)
			s.Mu.RLock()
			for _, m := range members {
				if c, ok := s.Clients[m]; ok {
					c.Send(NexusMessage{
						Type: "convo_left", Sender: username, ConvoID: msg.ConvoID,
					})
				}
			}
			s.Mu.RUnlock()

		case "read_receipt":
			if username == "" {
				continue
			}
			// Only friends can send each other read receipts. Without this
			// gate, any authed user could spam fake "I read your message"
			// notifications to strangers — low-impact but a free side-channel.
			// Group read receipts aren't modeled on the wire (no ConvoID
			// field in receipts) so we skip them for now.
			if !s.areFriends(username, msg.Recipient) {
				continue
			}
			s.Mu.RLock()
			if peer, ok := s.Clients[msg.Recipient]; ok {
				peer.Send(NexusMessage{
					Type: "read_receipt", Sender: username, Body: msg.Body,
				})
			}
			s.Mu.RUnlock()

		// Pairwise public-key handoff for NaCl box E2EE. Desktop clients send
		// this when they need a peer's key; the recipient answers with a
		// "presence" message carrying public_key (see native_client).
		case "key_request":
			if username == "" || msg.Recipient == "" {
				continue
			}
			if !s.areFriends(username, msg.Recipient) {
				continue
			}
			metrics.keyRequests.Add(1)
			msg.Sender = username
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			}
			s.Mu.RUnlock()

		case "call_offer", "call_answer", "ice_candidate":
			if username == "" {
				continue
			}
			log.Printf("Signal [%s] from %s to %s", msg.Type, username, msg.Recipient)
			s.Mu.Lock()
			if msg.Type == "call_offer" {
				if recipientClient, ok := s.Clients[msg.Recipient]; ok && recipientClient.InCall {
					s.Mu.Unlock()
					client.Send(NexusMessage{Type: "call_busy", Sender: msg.Recipient})
					continue
				}
			} else if msg.Type == "call_answer" {
				// Mark both parties as in-call.
				if c, ok := s.Clients[username]; ok {
					c.InCall = true
				}
				if c, ok := s.Clients[msg.Recipient]; ok {
					c.InCall = true
				}
			}
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			} else {
				s.Mu.Unlock()
				client.Send(NexusMessage{
					Type:  "call_error",
					Body:  "User is offline",
					Error: msg.Recipient + " is not available",
				})
				continue
			}
			s.Mu.Unlock()

		case "call_reject", "call_end":
			if username == "" {
				continue
			}
			s.Mu.Lock()
			// Clear in-call state for both parties.
			if c, ok := s.Clients[username]; ok {
				c.InCall = false
			}
			if c, ok := s.Clients[msg.Recipient]; ok {
				c.InCall = false
				c.Send(msg)
			}
			s.Mu.Unlock()

		// ---------- Group Call Invite ----------
		case "call_invite":
			if username == "" || msg.Recipient == "" || msg.ChannelID == "" {
				continue
			}
			msg.Sender = username
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			}
			s.Mu.RUnlock()

		// ---------- Remote Control (TeamViewer-style) ----------
		case "remote_register":
			if username == "" {
				continue
			}
			code := msg.Body
			if len(code) != 6 {
				client.Send(NexusMessage{Type: "remote_error", Error: "Invalid code"})
				continue
			}
			s.RemoteCodesMu.Lock()
			s.RemoteCodes[code] = remoteCodeEntry{Username: username, CreatedAt: time.Now()}
			s.RemoteCodesMu.Unlock()
			log.Printf("[remote] %s registered remote control code", username)
			client.Send(NexusMessage{Type: "remote_registered", Body: code})

		case "remote_unregister":
			if username == "" {
				continue
			}
			s.RemoteCodesMu.Lock()
			for k, v := range s.RemoteCodes {
				if v.Username == username {
					log.Printf("[remote] %s unregistered remote control code", username)
					delete(s.RemoteCodes, k)
				}
			}
			s.RemoteCodesMu.Unlock()

		case "remote_lookup":
			if username == "" {
				continue
			}
			// Rate-limit per IP: max 10 lookups per minute to prevent code brute-forcing.
			if !remoteLookupLimiter.allow(client.IP) {
				client.Send(NexusMessage{Type: "remote_lookup_result", Status: "error", Error: "Too many lookup attempts — try again shortly"})
				continue
			}
			code := msg.Body
			s.RemoteCodesMu.RLock()
			entry, ok := s.RemoteCodes[code]
			s.RemoteCodesMu.RUnlock()
			// L2: also reject expired codes
			if ok && time.Since(entry.CreatedAt) > remoteCodeTTL {
				ok = false
			}
			log.Printf("[remote] %s looked up a remote control code -> found=%v host=%s (map has %d entries)", username, ok, entry.Username, len(s.RemoteCodes))
			if ok {
				client.Send(NexusMessage{Type: "remote_lookup_result", Status: "ok", Body: entry.Username})
			} else {
				client.Send(NexusMessage{Type: "remote_lookup_result", Status: "error", Error: "Invalid code — make sure the host is still sharing"})
			}

		case "remote_offer", "remote_answer", "remote_ice", "remote_input", "remote_end":
			if username == "" {
				continue
			}
			msg.Sender = username
			if msg.Type == "remote_end" {
				s.RemoteCodesMu.Lock()
				for k, v := range s.RemoteCodes {
					if v.Username == username || v.Username == msg.Recipient {
						delete(s.RemoteCodes, k)
					}
				}
				s.RemoteCodesMu.Unlock()
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			} else if msg.Type == "remote_offer" {
				client.Send(NexusMessage{Type: "remote_error", Error: msg.Recipient + " is not online"})
			}
			s.Mu.RUnlock()

		// ---------- Servers + Channels ----------

		case "server_create":
			if username == "" {
				continue
			}
			name := strings.TrimSpace(msg.ServerName)
			if !validServerName.MatchString(name) {
				client.Send(NexusMessage{Type: "server_result", Error: "Server name: 2-64 chars, letters/digits/space/-_.'"})
				continue
			}
			visibility := strings.ToLower(strings.TrimSpace(msg.Visibility))
			if visibility != "public" && visibility != "private" {
				visibility = "private"
			}
			id, err := randHex(16)
			if err != nil {
				client.Send(NexusMessage{Type: "server_result", Error: "rand failure"})
				continue
			}
			invite, err := randHex(8)
			if err != nil {
				client.Send(NexusMessage{Type: "server_result", Error: "rand failure"})
				continue
			}
			tx, err := s.DB.Begin()
			if err != nil {
				client.Send(NexusMessage{Type: "server_result", Error: "db error"})
				continue
			}
			func() {
				defer tx.Rollback()
				if _, err := tx.Exec(
					`INSERT INTO servers (id, name, description, owner, visibility, invite_code) VALUES (?,?,?,?,?,?)`,
					id, name, strings.TrimSpace(msg.Topic), username, visibility, invite); err != nil {
					client.Send(NexusMessage{Type: "server_result", Error: "create server: " + err.Error()})
					return
				}
				if _, err := tx.Exec(
					`INSERT INTO server_members (server_id, username, role) VALUES (?, ?, 'owner')`,
					id, username); err != nil {
					client.Send(NexusMessage{Type: "server_result", Error: "add owner: " + err.Error()})
					return
				}
				// Bootstrap channels every server has from day one.
				for i, ch := range []string{"general", "random"} {
					cid, err := randHex(12)
					if err != nil {
						client.Send(NexusMessage{Type: "server_result", Error: "rand failure"})
						return
					}
					if _, err := tx.Exec(
						`INSERT INTO channels (id, server_id, name, kind, position) VALUES (?,?,?, 'text', ?)`,
						cid, id, ch, i); err != nil {
						client.Send(NexusMessage{Type: "server_result", Error: "channel: " + err.Error()})
						return
					}
				}
				if err := tx.Commit(); err != nil {
					client.Send(NexusMessage{Type: "server_result", Error: "commit: " + err.Error()})
					return
				}
				channels, _ := s.listServerChannels(id)
				client.Send(NexusMessage{
					Type:       "server_result",
					Status:     "ok",
					ServerID:   id,
					ServerName: name,
					InviteCode: invite,
					Role:       "owner",
					Visibility: visibility,
					Channels:   channels,
				})
				log.Printf("[server] %s created server %q (%s)", username, name, id)
				if visibility == "public" {
					if bot := os.Getenv("KAI_USERNAME"); bot != "" {
						s.DB.Exec(`INSERT OR IGNORE INTO server_members (server_id, username, role) VALUES (?, ?, 'member')`, id, bot)
					}
				}
			}()

		case "server_list":
			if username == "" {
				continue
			}
			servers, err := s.listUserServers(username)
			if err != nil {
				client.Send(NexusMessage{Type: "server_list_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "server_list_result", Status: "ok", Servers: servers})

		case "server_discover":
			if username == "" {
				continue
			}
			servers, err := s.listPublicServers(username)
			if err != nil {
				client.Send(NexusMessage{Type: "server_discover_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "server_discover_result", Status: "ok", Servers: servers})

		case "server_join":
			if username == "" {
				continue
			}
			code := strings.TrimSpace(msg.InviteCode)
			var serverID, serverName string
			if code != "" {
				// Join a private/invite-only server by its share code.
				if err := s.DB.QueryRow(
					`SELECT id, name FROM servers WHERE invite_code = ?`, code).Scan(&serverID, &serverName); err != nil {
					client.Send(NexusMessage{Type: "server_join_result", Error: "invite invalid"})
					continue
				}
			} else if msg.ServerID != "" {
				// Join a PUBLIC server straight from the discovery directory —
				// no invite code needed, but only if it's actually public.
				if err := s.DB.QueryRow(
					`SELECT id, name FROM servers WHERE id = ? AND visibility = 'public'`,
					msg.ServerID).Scan(&serverID, &serverName); err != nil {
					client.Send(NexusMessage{Type: "server_join_result", Error: "server not found or not public"})
					continue
				}
			} else {
				client.Send(NexusMessage{Type: "server_join_result", Error: "invite_code or public server_id required"})
				continue
			}
			if _, err := s.DB.Exec(
				`INSERT OR IGNORE INTO server_members (server_id, username, role) VALUES (?, ?, 'member')`,
				serverID, username); err != nil {
				client.Send(NexusMessage{Type: "server_join_result", Error: "db: " + err.Error()})
				continue
			}
			channels, _ := s.listServerChannels(serverID)
			client.Send(NexusMessage{
				Type:       "server_join_result",
				Status:     "ok",
				ServerID:   serverID,
				ServerName: serverName,
				Channels:   channels,
			})
			log.Printf("[server] %s joined %s (%s)", username, serverName, serverID)

		case "server_leave":
			if username == "" || msg.ServerID == "" {
				continue
			}
			if msg.ServerID == globalSpaceID {
				client.Send(NexusMessage{Type: "server_leave_result", Error: "the Phaze Hub is for everyone — can't leave"})
				continue
			}
			// Owners must transfer ownership before leaving; for v1, owner
			// can't leave their own server.
			role := s.userServerRole(msg.ServerID, username)
			if role == "owner" {
				client.Send(NexusMessage{Type: "server_leave_result", Error: "owners can't leave; delete the server or transfer ownership first"})
				continue
			}
			if _, err := s.DB.Exec(
				`DELETE FROM server_members WHERE server_id = ? AND username = ?`,
				msg.ServerID, username); err != nil {
				client.Send(NexusMessage{Type: "server_leave_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "server_leave_result", Status: "ok", ServerID: msg.ServerID})

		case "server_info":
			if username == "" || msg.ServerID == "" {
				continue
			}
			if !s.userIsServerMember(msg.ServerID, username) {
				client.Send(NexusMessage{Type: "server_info_result", Error: "not a member"})
				continue
			}
			channels, _ := s.listServerChannels(msg.ServerID)
			// Members list.
			memberRows, _ := s.DB.Query(`SELECT username FROM server_members WHERE server_id = ?`, msg.ServerID)
			var members []string
			if memberRows != nil {
				for memberRows.Next() {
					var u string
					if memberRows.Scan(&u) == nil {
						members = append(members, u)
					}
				}
				memberRows.Close()
			}
			client.Send(NexusMessage{
				Type:     "server_info_result",
				Status:   "ok",
				ServerID: msg.ServerID,
				Channels: channels,
				Members:  members,
			})

		case "channel_create":
			if username == "" || msg.ServerID == "" {
				continue
			}
			role := s.userServerRole(msg.ServerID, username)
			if role != "owner" && role != "admin" {
				client.Send(NexusMessage{Type: "channel_result", Error: "admin only"})
				continue
			}
			name := strings.ToLower(strings.TrimSpace(msg.ChannelName))
			if !validChannelName.MatchString(name) {
				client.Send(NexusMessage{Type: "channel_result", Error: "channel name: lowercase a-z 0-9 - _ , 2-32 chars"})
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(msg.Kind))
			if kind != "text" && kind != "voice" {
				kind = "text"
			}
			cid, err := randHex(12)
			if err != nil {
				client.Send(NexusMessage{Type: "channel_result", Error: "rand failure"})
				continue
			}
			var maxPos int
			s.DB.QueryRow(`SELECT COALESCE(MAX(position), 0) FROM channels WHERE server_id = ?`, msg.ServerID).Scan(&maxPos)
			if _, err := s.DB.Exec(
				`INSERT INTO channels (id, server_id, name, topic, kind, position) VALUES (?,?,?,?,?,?)`,
				cid, msg.ServerID, name, strings.TrimSpace(msg.Topic), kind, maxPos+1); err != nil {
				client.Send(NexusMessage{Type: "channel_result", Error: "db: " + err.Error()})
				continue
			}
			channels, _ := s.listServerChannels(msg.ServerID)
			// Push update to everyone in the server.
			s.broadcastChannelMsg(msg.ServerID, NexusMessage{
				Type:     "server_channels_updated",
				ServerID: msg.ServerID,
				Channels: channels,
			})
			client.Send(NexusMessage{Type: "channel_result", Status: "ok", ServerID: msg.ServerID, ChannelID: cid})

		case "channel_msg":
			if username == "" || msg.ChannelID == "" {
				continue
			}
			// Resolve server, check membership.
			var serverID string
			if err := s.DB.QueryRow(`SELECT server_id FROM channels WHERE id = ?`, msg.ChannelID).Scan(&serverID); err != nil {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "no such channel"})
				continue
			}
			if !s.userIsServerMember(serverID, username) {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "not a member"})
				continue
			}
			body := strings.TrimSpace(msg.Body)
			if body == "" || len(body) > 8000 {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "body 1-8000 chars"})
				continue
			}
			res, err := s.DB.Exec(
				`INSERT INTO channel_messages (channel_id, sender, body) VALUES (?,?,?)`,
				msg.ChannelID, username, body)
			if err != nil {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "db: " + err.Error()})
				continue
			}
			id, _ := res.LastInsertId()
			out := NexusMessage{
				Type:      "channel_msg_in",
				ServerID:  serverID,
				ChannelID: msg.ChannelID,
				Sender:    username,
				Body:      body,
				Messages: []ChannelMsg{{
					ID: id, ChannelID: msg.ChannelID, Sender: username, Body: body,
					CreatedAt: time.Now().UTC().Format(time.RFC3339),
				}},
			}
			s.broadcastChannelMsg(serverID, out)

		case "channel_history":
			if username == "" || msg.ChannelID == "" {
				continue
			}
			var serverID string
			if err := s.DB.QueryRow(`SELECT server_id FROM channels WHERE id = ?`, msg.ChannelID).Scan(&serverID); err != nil {
				client.Send(NexusMessage{Type: "channel_history_result", Error: "no such channel"})
				continue
			}
			if !s.userIsServerMember(serverID, username) {
				client.Send(NexusMessage{Type: "channel_history_result", Error: "not a member"})
				continue
			}
			history, err := s.channelHistory(msg.ChannelID, msg.HistoryFrom, 50)
			if err != nil {
				client.Send(NexusMessage{Type: "channel_history_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{
				Type:      "channel_history_result",
				Status:    "ok",
				ServerID:  serverID,
				ChannelID: msg.ChannelID,
				Messages:  history,
			})

		case "channel_react":
			if username == "" || msg.ChannelID == "" || msg.MsgID == "" || msg.Reaction == "" {
				continue
			}
			// Look up the message's server for permission + fan-out scope.
			var serverID string
			if err := s.DB.QueryRow(`SELECT server_id FROM channels WHERE id = ?`, msg.ChannelID).Scan(&serverID); err != nil {
				continue
			}
			if !s.userIsServerMember(serverID, username) {
				continue
			}
			id64, parseErr := strconv.ParseInt(msg.MsgID, 10, 64)
			if parseErr != nil {
				continue
			}
			// Toggle: if exists, remove; else insert. Same UX as DMs.
			var existed int
			s.DB.QueryRow(`SELECT 1 FROM channel_reactions WHERE msg_id=? AND emoji=? AND username=?`,
				id64, msg.Reaction, username).Scan(&existed)
			if existed == 1 {
				s.DB.Exec(`DELETE FROM channel_reactions WHERE msg_id=? AND emoji=? AND username=?`,
					id64, msg.Reaction, username)
			} else {
				s.DB.Exec(`INSERT OR IGNORE INTO channel_reactions (msg_id, emoji, username) VALUES (?,?,?)`,
					id64, msg.Reaction, username)
			}
			// Re-fetch the full reaction map for that message and fan out.
			fresh := s.channelReactionsBulk([]int64{id64})[id64]
			s.broadcastChannelMsg(serverID, NexusMessage{
				Type:      "channel_react_in",
				ServerID:  serverID,
				ChannelID: msg.ChannelID,
				MsgID:     msg.MsgID,
				Messages: []ChannelMsg{{
					ID: id64, ChannelID: msg.ChannelID, Reactions: fresh,
				}},
			})

		case "channel_edit":
			if username == "" || msg.MsgID == "" || msg.Body == "" {
				continue
			}
			id64, perr := strconv.ParseInt(msg.MsgID, 10, 64)
			if perr != nil {
				continue
			}
			// Verify ownership.
			var sender, channelID string
			if err := s.DB.QueryRow(`SELECT sender, channel_id FROM channel_messages WHERE id=?`, id64).Scan(&sender, &channelID); err != nil {
				continue
			}
			if sender != username {
				continue
			}
			if _, err := s.DB.Exec(`UPDATE channel_messages SET body=?, edited=1 WHERE id=?`, msg.Body, id64); err != nil {
				continue
			}
			var serverID string
			s.DB.QueryRow(`SELECT server_id FROM channels WHERE id=?`, channelID).Scan(&serverID)
			s.broadcastChannelMsg(serverID, NexusMessage{
				Type:      "channel_edit_in",
				ServerID:  serverID,
				ChannelID: channelID,
				MsgID:     msg.MsgID,
				Body:      msg.Body,
				Messages: []ChannelMsg{{
					ID: id64, ChannelID: channelID, Sender: username, Body: msg.Body, Edited: true,
				}},
			})

		case "channel_delete":
			if username == "" || msg.MsgID == "" {
				continue
			}
			id64, perr := strconv.ParseInt(msg.MsgID, 10, 64)
			if perr != nil {
				continue
			}
			var sender, channelID string
			if err := s.DB.QueryRow(`SELECT sender, channel_id FROM channel_messages WHERE id=?`, id64).Scan(&sender, &channelID); err != nil {
				continue
			}
			var serverID string
			s.DB.QueryRow(`SELECT server_id FROM channels WHERE id=?`, channelID).Scan(&serverID)
			// Own message, server admin/owner, or global staff (helper+) can delete.
			serverRole := s.userServerRole(serverID, username)
			if sender != username && serverRole != "owner" && serverRole != "admin" && !s.roleAtLeast(username, "helper") {
				continue
			}
			if _, err := s.DB.Exec(`UPDATE channel_messages SET deleted=1, body='' WHERE id=?`, id64); err != nil {
				continue
			}
			s.broadcastChannelMsg(serverID, NexusMessage{
				Type:      "channel_delete_in",
				ServerID:  serverID,
				ChannelID: channelID,
				MsgID:     msg.MsgID,
				Messages: []ChannelMsg{{
					ID: id64, ChannelID: channelID, Sender: sender, Deleted: true,
				}},
			})

		case "channel_pin":
			if username == "" || msg.MsgID == "" {
				continue
			}
			id64, perr := strconv.ParseInt(msg.MsgID, 10, 64)
			if perr != nil {
				continue
			}
			var channelID string
			if err := s.DB.QueryRow(`SELECT channel_id FROM channel_messages WHERE id=?`, id64).Scan(&channelID); err != nil {
				continue
			}
			var serverID string
			s.DB.QueryRow(`SELECT server_id FROM channels WHERE id=?`, channelID).Scan(&serverID)
			if !s.userIsServerMember(serverID, username) {
				continue
			}
			// Only server admins and owners can pin messages.
			pinRole := s.userServerRole(serverID, username)
			if pinRole != "owner" && pinRole != "admin" {
				client.Send(NexusMessage{Type: "channel_pin_result", Error: "admin only"})
				continue
			}
			// Toggle pin.
			var cur int
			s.DB.QueryRow(`SELECT COALESCE(pinned,0) FROM channel_messages WHERE id=?`, id64).Scan(&cur)
			next := 1 - cur
			if _, err := s.DB.Exec(`UPDATE channel_messages SET pinned=? WHERE id=?`, next, id64); err != nil {
				continue
			}
			s.broadcastChannelMsg(serverID, NexusMessage{
				Type:      "channel_pin_in",
				ServerID:  serverID,
				ChannelID: channelID,
				MsgID:     msg.MsgID,
				Messages: []ChannelMsg{{
					ID: id64, ChannelID: channelID, Pinned: next == 1,
				}},
			})

		case "purge_email":
			// User-initiated: drop the stored email address from this account.
			// Loses email-based recovery; relies entirely on Recovery PIN +
			// session tokens going forward. Idempotent.
			if username == "" {
				continue
			}
			if _, err := s.DB.Exec(`UPDATE users SET email = '' WHERE username = ?`, username); err != nil {
				client.Send(NexusMessage{Type: "purge_email_result", Error: "db: " + err.Error()})
				continue
			}
			log.Printf("[privacy] %s purged email", username)
			client.Send(NexusMessage{Type: "purge_email_result", Status: "ok"})

		case "settings_get":
			// Return all per-user settings for the authenticated user.
			if username == "" {
				continue
			}
			rows, err := s.DB.Query(`SELECT key, value FROM user_settings WHERE username = ?`, username)
			if err != nil {
				client.Send(NexusMessage{Type: "settings_result", Error: "db: " + err.Error()})
				continue
			}
			settings := map[string]string{}
			for rows.Next() {
				var k, v string
				if err := rows.Scan(&k, &v); err == nil {
					settings[k] = v
				}
			}
			rows.Close()
			// Reuse Envelopes (already a string-keyed map) for transport.
			client.Send(NexusMessage{
				Type:      "settings_result",
				Status:    "ok",
				Envelopes: settings,
			})

		case "settings_set":
			// Body is JSON: {"key":"...","value":"..."}. Value is opaque; client
			// owns the schema.
			if username == "" || msg.Body == "" {
				continue
			}
			var kv struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal([]byte(msg.Body), &kv); err != nil || kv.Key == "" {
				continue
			}
			if len(kv.Key) > 64 || len(kv.Value) > 64*1024 {
				continue
			}
			s.DB.Exec(
				`INSERT INTO user_settings (username, key, value, updated_at)
				 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
				 ON CONFLICT(username, key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
				username, kv.Key, kv.Value)

		case "voice_join":
			if username == "" || msg.ChannelID == "" {
				continue
			}
			// Ad-hoc call rooms (prefixed "call_") skip Spaces channel checks.
			if strings.HasPrefix(msg.ChannelID, "call_") {
				s.voiceRoomJoin(msg.ChannelID, username)
				s.voiceRoomBroadcastPeers(msg.ChannelID)
				continue
			}
			// Verify the channel exists and the user belongs to its server.
			var serverID, kind string
			if err := s.DB.QueryRow(`SELECT server_id, kind FROM channels WHERE id = ?`, msg.ChannelID).Scan(&serverID, &kind); err != nil {
				client.Send(NexusMessage{Type: "voice_peers", Error: "channel not found"})
				continue
			}
			if kind != "voice" {
				client.Send(NexusMessage{Type: "voice_peers", Error: "not a voice channel"})
				continue
			}
			if !s.isServerMember(serverID, username) {
				client.Send(NexusMessage{Type: "voice_peers", Error: "not a member"})
				continue
			}
			s.voiceRoomJoin(msg.ChannelID, username)
			s.voiceRoomBroadcastPeers(msg.ChannelID)

		case "voice_leave":
			if username == "" || msg.ChannelID == "" {
				continue
			}
			s.voiceRoomLeave(msg.ChannelID, username)
			s.voiceRoomBroadcastPeers(msg.ChannelID)

		case "voice_signal":
			// Relay WebRTC signaling (offer/answer/ice) between peers in a voice room.
			if username == "" || msg.Recipient == "" || msg.ChannelID == "" {
				continue
			}
			if !s.voiceRoomHas(msg.ChannelID, username) || !s.voiceRoomHas(msg.ChannelID, msg.Recipient) {
				continue
			}
			s.Mu.RLock()
			peer, ok := s.Clients[msg.Recipient]
			s.Mu.RUnlock()
			if ok {
				peer.Send(NexusMessage{
					Type:      "voice_signal",
					Sender:    username,
					Recipient: msg.Recipient,
					ChannelID: msg.ChannelID,
					Body:      msg.Body, // "offer" | "answer" | "ice"
					SDP:       msg.SDP,
					Candidate: msg.Candidate,
				})
			}

		case "stream_start":
			if username == "" {
				continue
			}
			title := strings.TrimSpace(msg.Body)
			if title == "" {
				title = username + "'s stream"
			}
			s.streamStart(username, title)
			s.streamBroadcastList()

		case "stream_stop":
			if username == "" {
				continue
			}
			s.streamStop(username)
			s.streamBroadcastList()

		case "stream_list":
			client.Send(NexusMessage{
				Type:    "stream_list_result",
				Status:  "ok",
				Results: s.streamList(),
			})

		case "stream_join":
			// Viewer wants to watch broadcaster Recipient. Server adds them as a
			// viewer and notifies the broadcaster to initiate an offer.
			if username == "" || msg.Recipient == "" {
				continue
			}
			if !s.streamHas(msg.Recipient) {
				client.Send(NexusMessage{Type: "stream_join_result", Error: "not live"})
				continue
			}
			s.streamAddViewer(msg.Recipient, username)
			s.Mu.RLock()
			host, ok := s.Clients[msg.Recipient]
			s.Mu.RUnlock()
			if ok {
				host.Send(NexusMessage{Type: "stream_viewer_join", Sender: username, Recipient: msg.Recipient})
			}
			client.Send(NexusMessage{Type: "stream_join_result", Status: "ok", Recipient: msg.Recipient})

		case "stream_leave":
			if username == "" || msg.Recipient == "" {
				continue
			}
			s.streamRemoveViewer(msg.Recipient, username)
			s.Mu.RLock()
			host, ok := s.Clients[msg.Recipient]
			s.Mu.RUnlock()
			if ok {
				host.Send(NexusMessage{Type: "stream_viewer_leave", Sender: username, Recipient: msg.Recipient})
			}

		case "stream_signal":
			// Relay WebRTC signaling between broadcaster and a specific viewer.
			if username == "" || msg.Recipient == "" {
				continue
			}
			s.Mu.RLock()
			peer, ok := s.Clients[msg.Recipient]
			s.Mu.RUnlock()
			if ok {
				peer.Send(NexusMessage{
					Type:      "stream_signal",
					Sender:    username,
					Recipient: msg.Recipient,
					Body:      msg.Body, // "offer" | "answer" | "ice"
					SDP:       msg.SDP,
					Candidate: msg.Candidate,
				})
			}

		default:
			log.Printf("Unknown message type: %s from %s", msg.Type, username)
		}
	}
}
