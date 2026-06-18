package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Skype data export format (from go.skype.com/export)
type skypeExport struct {
	UserID        string              `json:"userId"`
	Conversations []skypeConversation `json:"conversations"`
}

type skypeConversation struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"displayName"`
	Messages    []skypeMessage `json:"MessageList"`
}

type skypeMessage struct {
	OriginalArrivalTime string `json:"originalarrivaltime"`
	MessageType         string `json:"messagetype"`
	Content             string `json:"content"`
	DisplayName         string `json:"displayName"`
	From                string `json:"from"` // e.g. "8:username" — skype ID of sender
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer(
		"&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&#39;", "'",
	).Replace(s)
	return strings.TrimSpace(s)
}

// skypeImportHandler: POST /api/v1/import/skype
// Accepts multipart form with a "file" field (Skype export ZIP).
func (s *NexusServer) skypeImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	username := s.sessionUsername(tokenFromRequest(r))
	if username == "" {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, 100<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		http.Error(w, "invalid zip", http.StatusBadRequest)
		return
	}

	type contactOut struct {
		DisplayName   string `json:"display_name"`
		PhazeUsername string `json:"phaze_username,omitempty"`
		OnPhaze       bool   `json:"on_phaze"`
	}
	var result struct {
		MessagesImported int          `json:"messages_imported"`
		Contacts         []contactOut `json:"contacts"`
	}

	seen := map[string]bool{}
	for _, zf := range zr.File {
		if !strings.HasSuffix(zf.Name, ".json") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(rc, 50<<20))
		rc.Close()

		var export skypeExport
		if json.Unmarshal(raw, &export) != nil || len(export.Conversations) == 0 {
			continue
		}

		for _, convo := range export.Conversations {
			// Import text messages
			for _, msg := range convo.Messages {
				if msg.MessageType != "RichText" && msg.MessageType != "Text" {
					continue
				}
				body := stripHTML(msg.Content)
				if body == "" || len(body) > 4000 {
					continue
				}
				res, err := s.DB.Exec(`INSERT OR IGNORE INTO skype_import_messages
					(username, conversation_id, conversation_display, sender_display, body, sent_at)
					VALUES (?, ?, ?, ?, ?, ?)`,
					username, convo.ID, convo.DisplayName, msg.DisplayName, body, msg.OriginalArrivalTime)
				if err == nil {
					if n, _ := res.RowsAffected(); n > 0 {
						result.MessagesImported++
					}
				}
			}

			// Collect unique 1:1 conversation partners only.
			// Group conversations have IDs like "19:xxx@thread.skype" — skip them.
			if seen[convo.ID] || convo.DisplayName == "" {
				continue
			}
			if strings.Contains(convo.ID, "@thread") {
				continue
			}
			seen[convo.ID] = true

			// Try to match by display_name first, then by username as fallback.
			var phazeUser string
			s.DB.QueryRow(`SELECT username FROM users WHERE LOWER(display_name)=LOWER(?) LIMIT 1`, convo.DisplayName).Scan(&phazeUser)
			if phazeUser == "" {
				s.DB.QueryRow(`SELECT username FROM users WHERE LOWER(username)=LOWER(?) LIMIT 1`, convo.DisplayName).Scan(&phazeUser)
			}

			var phazeUserNullable *string
			if phazeUser != "" {
				phazeUserNullable = &phazeUser
			}
			s.DB.Exec(`INSERT OR IGNORE INTO skype_import_contacts (username, skype_id, display_name, phaze_username)
				VALUES (?, ?, ?, ?)`, username, convo.ID, convo.DisplayName, phazeUserNullable)

			result.Contacts = append(result.Contacts, contactOut{
				DisplayName:   convo.DisplayName,
				PhazeUsername: phazeUser,
				OnPhaze:       phazeUser != "",
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// skypeContactsHandler: GET /api/v1/import/skype/contacts
func (s *NexusServer) skypeContactsHandler(w http.ResponseWriter, r *http.Request) {
	username := s.sessionUsername(tokenFromRequest(r))
	if username == "" {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	rows, err := s.DB.Query(`SELECT display_name, COALESCE(phaze_username,''), COALESCE(invite_sent,0)
		FROM skype_import_contacts WHERE username=? ORDER BY display_name`, username)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type row struct {
		DisplayName   string `json:"display_name"`
		PhazeUsername string `json:"phaze_username,omitempty"`
		InviteSent    bool   `json:"invite_sent"`
		OnPhaze       bool   `json:"on_phaze"`
	}
	out := []row{}
	for rows.Next() {
		var rr row
		var pu string
		rows.Scan(&rr.DisplayName, &pu, &rr.InviteSent)
		if pu != "" {
			rr.PhazeUsername = pu
			rr.OnPhaze = true
		}
		out = append(out, rr)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// skypeMessagesHandler: GET /api/v1/import/skype/messages?contact=DisplayName
// Returns up to 200 imported Skype messages for a given conversation partner.
func (s *NexusServer) skypeMessagesHandler(w http.ResponseWriter, r *http.Request) {
	username := s.sessionUsername(tokenFromRequest(r))
	if username == "" {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	contact := r.URL.Query().Get("contact")
	if contact == "" {
		http.Error(w, "contact required", http.StatusBadRequest)
		return
	}
	rows, err := s.DB.Query(`SELECT sender_display, body, sent_at
		FROM skype_import_messages
		WHERE username=? AND conversation_display=?
		ORDER BY sent_at ASC LIMIT 200`, username, contact)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type msg struct {
		Sender string `json:"sender"`
		Body   string `json:"body"`
		SentAt string `json:"sent_at"`
	}
	out := []msg{}
	for rows.Next() {
		var m msg
		rows.Scan(&m.Sender, &m.Body, &m.SentAt)
		out = append(out, m)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// skypeInviteHandler: GET /api/v1/import/skype/invite-link
// Returns a shareable invite link with the user's referral code.
// Skype exports contain no email addresses so email-based invites are not possible;
// instead the user copies/shares this link themselves.
func (s *NexusServer) skypeInviteHandler(w http.ResponseWriter, r *http.Request) {
	username := s.sessionUsername(tokenFromRequest(r))
	if username == "" {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	link := "https://phazechat.world/?ref=" + username
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"invite_link": link})
}
