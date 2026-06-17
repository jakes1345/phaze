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
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer(
		"&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&#39;", "'",
	).Replace(s)
	return strings.TrimSpace(s)
}

func skypeBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// skypeImportHandler: POST /api/v1/import/skype
// Accepts multipart form with a "file" field (Skype export ZIP).
func (s *NexusServer) skypeImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	username := s.sessionUsername(skypeBearer(r))
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
				s.DB.Exec(`INSERT OR IGNORE INTO skype_import_messages
					(username, conversation_id, conversation_display, sender_display, body, sent_at)
					VALUES (?, ?, ?, ?, ?, ?)`,
					username, convo.ID, convo.DisplayName, msg.DisplayName, body, msg.OriginalArrivalTime)
				result.MessagesImported++
			}

			// Collect unique conversation partners
			if seen[convo.ID] || convo.DisplayName == "" {
				continue
			}
			seen[convo.ID] = true

			var phazeUser string
			s.DB.QueryRow(`SELECT username FROM users WHERE LOWER(username)=LOWER(?)`, convo.DisplayName).Scan(&phazeUser)
			s.DB.Exec(`INSERT OR IGNORE INTO skype_import_contacts (username, skype_id, display_name, phaze_username)
				VALUES (?, ?, ?, NULLIF(?,'''))`, username, convo.ID, convo.DisplayName, phazeUser)

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
	username := s.sessionUsername(skypeBearer(r))
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

// skypeInviteHandler: POST /api/v1/import/skype/invite
// Body: {"display_names": ["Alice", "Bob"]}
func (s *NexusServer) skypeInviteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	username := s.sessionUsername(skypeBearer(r))
	if username == "" {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}

	var body struct {
		DisplayNames []string `json:"display_names"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.DisplayNames) == 0 {
		http.Error(w, "display_names required", http.StatusBadRequest)
		return
	}

	sent := 0
	for _, name := range body.DisplayNames {
		if sent >= 20 {
			break
		}
		var email string
		s.DB.QueryRow(`SELECT COALESCE(email,'') FROM skype_import_contacts
			WHERE username=? AND display_name=?`, username, name).Scan(&email)
		if email == "" {
			continue
		}
		go s.sendEmailLogged(email,
			username+" invited you to Phaze",
			emailInvite(username, "https://phazechat.world"),
		)
		s.DB.Exec(`UPDATE skype_import_contacts SET invite_sent=1 WHERE username=? AND display_name=?`, username, name)
		sent++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"sent": sent})
}
