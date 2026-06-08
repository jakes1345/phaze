package main

import (
	"encoding/json"
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
		body := "Hey " + name + ",\n\n" +
			"Thanks so much for supporting Phaze! 💜\n\n" +
			"Once your contribution lands, your supporter badge will be added to your account. " +
			"If you donated under a different name, just reply to this email so we can match it up.\n\n" +
			"— The Phaze team"
		_ = s.sendEmail(to, "Thanks for supporting Phaze 💜", body)
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
