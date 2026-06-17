package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
)

// supporters.go implements the opt-in supporter flow.
//
// Flow: a visitor fills the public support form (Phaze username + name +
// email). We store it as a pending row, send THEM a thank-you email, and
// hand back the Buy Me a Coffee URL so they can actually pay. Buy Me a
// Coffee never tells our server whether the payment went through, so the
// admin cross-checks the request against the BMC payment notification in
// their inbox and clicks "Grant badge" in the admin portal — which flips
// users.supporter. No payment data ever touches this server.

// SupporterRequest is one row of the opt-in queue, surfaced in the admin portal.
type SupporterRequest struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func bmcURL() string {
	if u := strings.TrimSpace(os.Getenv("BMC_URL")); u != "" {
		return u
	}
	return "https://buymeacoffee.com/phazeworld"
}

// supportRequestHandler is the PUBLIC endpoint behind the "Support Phaze"
// button. It records the opt-in, emails the donor a thank-you, and returns
// the BMC URL the client should redirect to. It is rate-limited but not
// admin-gated.
func (s *NexusServer) supportRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	if req.Name == "" || req.Email == "" {
		http.Error(w, "name and email are required", http.StatusBadRequest)
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	if len(req.Username) > 64 || len(req.Name) > 120 || len(req.Email) > 200 {
		http.Error(w, "field too long", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.Exec(
		`INSERT INTO supporter_requests (username, name, email, status) VALUES (?, ?, ?, 'pending')`,
		req.Username, req.Name, req.Email,
	); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// Thank-you to the donor (best effort — never block the redirect on email).
	go func(to, name string) {
		_ = s.sendEmail(to, "Thanks for supporting Phaze 💜", emailSupporterThankYou(name))
	}(req.Email, req.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"bmc_url": bmcURL(),
	})
}

// adminSupportersHandler lists supporter requests for the admin portal,
// newest first. Optional ?status= filter (default: pending).
func (s *NexusServer) adminSupportersHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	rows, err := s.DB.Query(
		`SELECT id, COALESCE(username, ''), COALESCE(name, ''), COALESCE(email, ''),
		        COALESCE(status, 'pending'), CAST(created_at AS TEXT)
		   FROM supporter_requests
		  WHERE COALESCE(status, 'pending') = ?
		  ORDER BY id DESC LIMIT 500`, status)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []SupporterRequest{}
	for rows.Next() {
		var sr SupporterRequest
		if err := rows.Scan(&sr.ID, &sr.Username, &sr.Name, &sr.Email, &sr.Status, &sr.CreatedAt); err != nil {
			continue
		}
		out = append(out, sr)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// adminSupporterActionHandler handles POST /api/v1/admin/supporters/{id}/(grant|dismiss).
// "grant" marks the request granted AND flips the matching user's supporter
// flag; "dismiss" just clears it from the queue.
func (s *NexusServer) adminSupporterActionHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/supporters/"), "/")
	if len(parts) != 2 {
		http.Error(w, "expected /api/v1/admin/supporters/{id}/(grant|dismiss)", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	action := parts[1]

	switch action {
	case "grant":
		var username string
		if err := s.DB.QueryRow(`SELECT COALESCE(username, '') FROM supporter_requests WHERE id = ?`, id).Scan(&username); err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if _, err := s.DB.Exec(`UPDATE supporter_requests SET status = 'granted' WHERE id = ?`, id); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if strings.TrimSpace(username) != "" {
			if _, err := s.DB.Exec(
				`UPDATE users SET supporter = 1, supporter_since = CURRENT_TIMESTAMP WHERE username = ?`, username,
			); err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
		}
	case "dismiss":
		if _, err := s.DB.Exec(`UPDATE supporter_requests SET status = 'dismissed' WHERE id = ?`, id); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// bmcWebhookHandler handles Buy Me a Coffee webhook events.
// BMC sends a POST for every new donation/membership. We match by email:
//   1. Check supporter_requests for a pending row with that email → grant it
//   2. Check users table for a verified account with that email → grant directly
//   3. No match → store in bmc_payments for manual resolution in the portal
//
// Set BMC_WEBHOOK_SECRET in Fly secrets to the token from your BMC dashboard.
// BMC signs requests with HMAC-SHA256 in the X-BMC-Signature header.
func (s *NexusServer) bmcWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Verify HMAC signature if secret is configured.
	// BMC sends the signature in X-BMC-Signature or X-Signature-Hmac-Sha256,
	// possibly prefixed with "sha256=". The dashboard shows the secret as a
	// hex string; BMC signs using the decoded raw bytes, not the ASCII hex.
	if secret := strings.TrimSpace(os.Getenv("BMC_WEBHOOK_SECRET")); secret != "" {
		sig := r.Header.Get("X-BMC-Signature")
		if sig == "" {
			sig = r.Header.Get("X-Signature-Hmac-Sha256")
		}
		sig = strings.TrimPrefix(sig, "sha256=")

		// Try hex-decoded key first (BMC displays secret as hex but signs with raw bytes).
		// Fall back to ASCII key in case they change this.
		valid := false
		for _, key := range bmcSigningKeys(secret) {
			m := hmac.New(sha256.New, key)
			m.Write(body)
			if hmac.Equal([]byte(sig), []byte(hex.EncodeToString(m.Sum(nil)))) {
				valid = true
				break
			}
		}
		if sig == "" {
			log.Printf("[bmc] no signature header — accepting (test mode?)")
		} else if !valid {
			log.Printf("[bmc] signature mismatch — rejecting")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// BMC payload shape (covers both donation and membership events).
	var payload struct {
		Type  string `json:"type"`
		Data  struct {
			SupporterName  string `json:"supporter_name"`
			SupporterEmail string `json:"supporter_email"`
			Amount         string `json:"amount"`
			Message        string `json:"message"`
		} `json:"data"`
		// Some BMC webhook versions are flat (no data wrapper).
		SupporterName  string `json:"supporter_name"`
		SupporterEmail string `json:"supporter_email"`
		Amount         string `json:"amount"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	// Normalise — handle both flat and nested payload shapes.
	name := payload.Data.SupporterName
	if name == "" {
		name = payload.SupporterName
	}
	email := strings.ToLower(strings.TrimSpace(payload.Data.SupporterEmail))
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(payload.SupporterEmail))
	}
	amount := payload.Data.Amount
	if amount == "" {
		amount = payload.Amount
	}
	message := payload.Data.Message
	if message == "" {
		message = payload.Message
	}

	if email == "" {
		log.Printf("[bmc] webhook received but no email in payload — type=%s", payload.Type)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("[bmc] payment from %s <%s> amount=%s", name, email, amount)

	matched := s.bmcGrantByEmail(email, name)

	// Store for audit trail regardless of match outcome.
	matchedUser := ""
	if matched != "" {
		matchedUser = matched
	}
	s.DB.Exec(
		`INSERT INTO bmc_payments (supporter_name, supporter_email, amount, message, matched_username)
		 VALUES (?,?,?,?,?)`, name, email, amount, message, matchedUser)

	w.WriteHeader(http.StatusOK)
}

// bmcGrantByEmail finds a Phaze account for the given BMC email and grants
// the supporter badge. Returns the matched username, or "" if no match.
func (s *NexusServer) bmcGrantByEmail(email, name string) string {
	// 1. Match against a pending supporter_requests row (user filled the form).
	var reqID int
	var reqUsername string
	err := s.DB.QueryRow(
		`SELECT id, COALESCE(username,'') FROM supporter_requests
		  WHERE LOWER(email)=? AND status='pending'
		  ORDER BY id DESC LIMIT 1`, email).Scan(&reqID, &reqUsername)
	if err == nil {
		s.DB.Exec(`UPDATE supporter_requests SET status='granted' WHERE id=?`, reqID)
		if reqUsername != "" {
			s.DB.Exec(`UPDATE users SET supporter=1, supporter_since=CURRENT_TIMESTAMP WHERE username=?`, reqUsername)
			log.Printf("[bmc] auto-granted badge to %s (via supporter_requests)", reqUsername)
			return reqUsername
		}
	}

	// 2. Match against a verified user account by email.
	var username string
	err = s.DB.QueryRow(
		`SELECT username FROM users WHERE LOWER(email)=? AND is_verified=1 LIMIT 1`, email).Scan(&username)
	if err == nil && username != "" {
		s.DB.Exec(`UPDATE users SET supporter=1, supporter_since=CURRENT_TIMESTAMP WHERE username=?`, username)
		s.DB.Exec(
			`INSERT OR IGNORE INTO supporter_requests (username, name, email, status)
			 VALUES (?,?,?,'granted')`, username, name, email)
		log.Printf("[bmc] auto-granted badge to %s (matched by email)", username)
		return username
	}

	log.Printf("[bmc] no Phaze account found for email %s — stored in bmc_payments for manual review", email)
	return ""
}

// bmcSigningKeys returns candidate HMAC keys to try when verifying a BMC
// webhook signature. BMC displays the secret as a hex string in their
// dashboard; they sign using the decoded raw bytes. We try both forms so
// the check works regardless of which encoding BMC uses internally.
func bmcSigningKeys(secret string) [][]byte {
	keys := [][]byte{[]byte(secret)}
	if decoded, err := hex.DecodeString(secret); err == nil {
		keys = append([][]byte{decoded}, keys...)
	}
	return keys
}
