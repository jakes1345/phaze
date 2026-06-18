package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

Always respond in plain text. Do not use markdown headers or excessive formatting in a tiny chat bubble. 2-4 short sentences max per reply, unless the user asks for steps.

If users ask how to support Phaze, mention they can donate at the "Buy Phaze a coffee" link in the footer. Don't volunteer this unless asked.`

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

// supportChatHandler proxies a chat request to whichever LLM provider has a
// key configured. Order is Gemini (free tier) → Anthropic. Returns 503 if
// neither is set.
func (s *NexusServer) supportChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	geminiKey := os.Getenv("GEMINI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if geminiKey == "" && anthropicKey == "" {
		http.Error(w, "support bot offline (no LLM key configured)", http.StatusServiceUnavailable)
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
	// Hard cap on conversation length: stops a malicious client from
	// shipping a huge transcript every turn and draining the API budget.
	if len(req.Messages) > 30 {
		req.Messages = req.Messages[len(req.Messages)-30:]
	}
	// Cap per-message size — protects against megabyte payloads.
	for i := range req.Messages {
		if len(req.Messages[i].Content) > 4000 {
			req.Messages[i].Content = req.Messages[i].Content[:4000]
		}
		// Anthropic rejects unknown roles. Coerce anything we don't know to "user".
		if req.Messages[i].Role != "user" && req.Messages[i].Role != "assistant" {
			req.Messages[i].Role = "user"
		}
	}
	// Optional caller identity; include in system prompt if known.
	system := supportSystemPrompt
	if u := s.sessionUsername(tokenFromRequest(r)); u != "" {
		system += "\n\nThe user you are talking to is signed in as: " + u
	}

	// Try Gemini first (free tier), fall back to Anthropic only if Gemini
	// returned an error or wasn't configured. Either provider proxies the
	// same request shape; whichever responds wins.
	var reply string
	var lastErr error
	if geminiKey != "" {
		reply, lastErr = callGemini(geminiKey, system, req.Messages)
	}
	if reply == "" && anthropicKey != "" {
		reply, lastErr = callAnthropic(anthropicKey, system, req.Messages)
	}
	if reply == "" {
		if lastErr != nil {
			log.Printf("[support] all providers failed: %v", lastErr)
			http.Error(w, "support bot unavailable: "+lastErr.Error(), http.StatusBadGateway)
		} else {
			http.Error(w, "support bot returned empty reply", http.StatusBadGateway)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"reply": reply})
}

// callGemini sends the conversation to Google's Gemini API (free tier
// 2.0 Flash). Translates our generic message shape into Gemini's
// "contents" + "system_instruction" structure.
func callGemini(apiKey, system string, msgs []supportMessage) (string, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	type sysInstr struct {
		Parts []part `json:"parts"`
	}
	type genCfg struct {
		MaxOutputTokens int     `json:"maxOutputTokens"`
		Temperature     float64 `json:"temperature"`
	}
	type geminiReq struct {
		Contents          []content `json:"contents"`
		SystemInstruction sysInstr  `json:"system_instruction"`
		GenerationConfig  genCfg    `json:"generationConfig"`
	}
	contents := make([]content, 0, len(msgs))
	for _, m := range msgs {
		// Gemini expects role "user" or "model" (not "assistant").
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, content{Role: role, Parts: []part{{Text: m.Content}}})
	}
	payload := geminiReq{
		Contents:          contents,
		SystemInstruction: sysInstr{Parts: []part{{Text: system}}},
		GenerationConfig:  genCfg{MaxOutputTokens: 600, Temperature: 0.4},
	}
	buf, _ := json.Marshal(payload)
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + apiKey
	req, err := http.NewRequest("POST", url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gemini %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var gr struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &gr); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, c := range gr.Candidates {
		for _, p := range c.Content.Parts {
			out.WriteString(p.Text)
		}
	}
	return out.String(), nil
}

// callAnthropic posts to the Anthropic Claude API. Used as the fallback
// when Gemini isn't configured or fails.
func callAnthropic(apiKey, system string, msgs []supportMessage) (string, error) {
	payload := anthropicMessageReq{
		Model:     "claude-haiku-4-5-20251001",
		System:    system,
		MaxTokens: 600,
		Messages:  msgs,
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	var ar anthropicMessageResp
	if err := json.Unmarshal(body, &ar); err != nil {
		return "", fmt.Errorf("decode anthropic: %w", err)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("anthropic %s: %s", ar.Error.Type, ar.Error.Message)
	}
	var out strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			out.WriteString(c.Text)
		}
	}
	return out.String(), nil
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
	if u := s.sessionUsername(tokenFromRequest(r)); u != "" {
		body.Username = u
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
// Both endpoints go through the IP rate limiter — the chat endpoint hits
// a paid LLM API on every call, so we cannot leave it bare.
func (s *NexusServer) initSupportRoutes() {
	http.HandleFunc("/api/v1/support/chat", rateLimit(s.supportChatHandler))
	http.HandleFunc("/api/v1/support/escalate", rateLimit(s.supportEscalateHandler))
}

