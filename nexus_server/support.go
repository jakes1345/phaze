package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Support chat proxy: forwards user messages to Anthropic's Claude API
// (cheap Haiku model) with a Phaze-specific system prompt. Stateless on the
// server — clients send the full conversation each turn. Optional auth: signed
// in users get their username injected into the system prompt; anonymous
// landing-page visitors can still chat.

const supportSystemPrompt = `You are Phaze Helper, the in-app support bot for Phaze (https://phazechat.world) — a real-time chat platform with DMs, group spaces, voice/video calls, livestreams, and end-to-end encryption.

Your job:
- Answer questions about how to use Phaze.
- Help users with sign-up, friend requests, voice calls, livestreams, file sharing, settings.
- Be concise, friendly, and direct — no fluff.

Key features you can explain:
- DMs: 1:1 messages, E2E encrypted, with reactions, edits, deletes, pins, file uploads up to 25 MB, audio messages.
- Spaces: Discord-style group servers with text and voice channels. Create from the Spaces tab or join with an invite code.
- Phaze Hub: a global space every user is automatically in (#general, #lobby, #announcements).
- Calls: 1:1 audio + video calls from any DM chat header (☎ / 📹 icons). Screen share during video calls.
- Voice channels: in a Space, click a 🎙 channel → "Join voice" (audio) or "Join with video" (group video). Mesh peer-to-peer.
- Live: broadcast your camera to anyone who clicks your stream card. Found under the 🔴 Live tab.
- Recovery PIN: encrypts your E2E keypair so you can sign in on new devices via Settings → Backup & Devices.
- Link code: alternative cross-device sign-in via a one-time code from another logged-in device.

Common issues:
- "Can't make a call" → make sure a friend is selected; the call buttons appear in the DM header.
- "Camera not working on Android" → grant Camera + Microphone permissions in Android Settings.
- "Different channels in different browsers" → each user only sees Spaces they belong to. Either sign in as the same user, or join the same Space with an invite code.
- "Verification email didn't arrive" → check spam, then ask the user to message support; an admin can fetch the code.

When to escalate to a human:
- Account recovery beyond Recovery PIN.
- Billing (Phaze is free, but later).
- Abuse reports — direct them to use the in-app Report button.
- Anything you cannot answer confidently.

If the user clicks "Talk to a human", a live agent will be notified and may DM them — but only if one is online. Otherwise tell them to leave a description and an agent will follow up via DM.

Always respond in plain text. Do not use markdown headers or excessive formatting in a tiny chat bubble. 2-4 short sentences max per reply, unless the user asks for steps.`

type supportMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

type supportRequest struct {
	Messages []supportMessage `json:"messages"`
}

type anthropicMessageReq struct {
	Model     string           `json:"model"`
	System    string           `json:"system"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []supportMessage `json:"messages"`
}

type anthropicMessageResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// supportChatHandler proxies a chat request to the Anthropic API and returns
// the assistant's reply.
func (s *NexusServer) supportChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		http.Error(w, "support bot offline (ANTHROPIC_API_KEY not configured)", http.StatusServiceUnavailable)
		return
	}
	var req supportRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		http.Error(w, "no messages", http.StatusBadRequest)
		return
	}
	// Optional caller identity (Bearer token); include in system prompt if known.
	system := supportSystemPrompt
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if u := s.sessionUsername(tok); u != "" {
			system += "\n\nThe user you are talking to is signed in as: " + u
		}
	}

	payload := anthropicMessageReq{
		Model:     "claude-haiku-4-5-20251001",
		System:    system,
		MaxTokens: 600,
		Messages:  req.Messages,
	}
	buf, _ := json.Marshal(payload)
	httpReq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		http.Error(w, "request build error", http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[support] anthropic call: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	var ar anthropicMessageResp
	if err := json.Unmarshal(body, &ar); err != nil {
		log.Printf("[support] decode anthropic resp: %v body=%q", err, string(body))
		http.Error(w, "upstream decode error", http.StatusBadGateway)
		return
	}
	if ar.Error != nil {
		log.Printf("[support] anthropic error: %s — %s", ar.Error.Type, ar.Error.Message)
		http.Error(w, "anthropic: "+ar.Error.Message, http.StatusBadGateway)
		return
	}
	var reply strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			reply.WriteString(c.Text)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"reply": reply.String()})
}

// supportEscalateHandler is the "Talk to a human" entrypoint. Counts how many
// helper-or-higher users are currently connected and (if any) sends each a
// system DM telling them a user wants help.
func (s *NexusServer) supportEscalateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	// Caller identity: prefer authenticated session, fall back to provided name.
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if u := s.sessionUsername(tok); u != "" {
			body.Username = u
		}
	}
	if body.Username == "" {
		body.Username = "Anonymous visitor"
	}
	note := strings.TrimSpace(body.Note)
	if note == "" {
		note = "(no description)"
	}

	// Find online staff (helper or higher).
	s.Mu.RLock()
	staffOnline := []*Client{}
	for _, c := range s.Clients {
		if roleRank(s.userRole(c.Username)) >= roleRank("helper") {
			staffOnline = append(staffOnline, c)
		}
	}
	s.Mu.RUnlock()

	for _, c := range staffOnline {
		c.Send(NexusMessage{
			Type:   "support_escalation",
			Sender: body.Username,
			Body:   note,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"agents_online":  len(staffOnline),
		"escalation_for": body.Username,
	})
	log.Printf("[support] %s escalated to human; %d staff online", body.Username, len(staffOnline))
}

// init wires routes. Called from main.go after the rest of the routes.
func (s *NexusServer) initSupportRoutes() {
	http.HandleFunc("/api/v1/support/chat", s.supportChatHandler)
	http.HandleFunc("/api/v1/support/escalate", s.supportEscalateHandler)
}

