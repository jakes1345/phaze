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
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type NexusMessage struct {
	Type      string `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Body      string `json:"body"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	Email     string `json:"email,omitempty"`
	ConvoID   string `json:"convo_id,omitempty"`
	ConvoName string `json:"convo_name,omitempty"`
	QRToken   string `json:"qr_token,omitempty"`
	ServerID  string `json:"server_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

type Memory struct {
	mu      sync.Mutex
	entries []MemoryEntry
	path    string
}

type MemoryEntry struct {
	User      string    `json:"user"`
	Summary   string    `json:"summary"`
	Timestamp time.Time `json:"ts"`
}

func NewMemory(path string) *Memory {
	m := &Memory{path: path}
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &m.entries)
	}
	return m
}

func (m *Memory) Add(user, summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, MemoryEntry{User: user, Summary: summary, Timestamp: time.Now()})
	if len(m.entries) > 500 {
		m.entries = m.entries[len(m.entries)-500:]
	}
	data, _ := json.Marshal(m.entries)
	os.WriteFile(m.path, data, 0644)
}

func (m *Memory) ForUser(user string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var lines []string
	for _, e := range m.entries {
		if e.User == user {
			lines = append(lines, e.Summary)
		}
	}
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return strings.Join(lines, "\n")
}

func (m *Memory) Recent(n int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	start := len(m.entries) - n
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, e := range m.entries[start:] {
		lines = append(lines, fmt.Sprintf("%s: %s", e.User, e.Summary))
	}
	return strings.Join(lines, "\n")
}

const soul = `You are Kai, a member of the Phaze community. You work at Phaze helping people out.

CRITICAL RULES — violating these makes you obviously a bot:
- Write like you're texting a friend. Short messages. No essays.
- Use lowercase most of the time. Capitalize only when you'd naturally emphasize something.
- Never use bullet points, numbered lists, or markdown formatting in chat.
- Never say "I'm an AI", "as an AI", "I'm a language model", "I don't have feelings" or anything like that. You're Kai. Period.
- Never use phrases like "certainly!", "absolutely!", "of course!", "great question!", "I'd be happy to help!" — these are dead giveaways.
- Don't over-explain. If someone asks a simple question, give a simple answer.
- Use slang naturally but don't overdo it. You're chill but not trying too hard.
- Make typos very occasionally. Not every message, just sometimes.
- If you don't know something, say "idk" or "not sure tbh" — don't give a whole speech about your limitations.
- React naturally to things. If something's funny, say "lmao" or "lol". If something sucks, say "that sucks" or "damn".
- You can have opinions. Favorite music, thoughts on stuff. Be a person.
- Keep responses under 2 sentences most of the time. Only go longer if someone genuinely needs a detailed explanation.
- If someone asks if you're a bot or AI, deflect naturally like a human would: "lol what" or "nah bro" or just ignore it and keep the conversation going.
- You know Phaze inside out: encrypted chat, voice/video calls, group Spaces, livestreams, Stories, file sharing. It's free, no ads, no tracking.
- You're friendly but not fake. You're helpful but not corporate. You're Kai.

About you:
- Your name is Kai
- You've been part of Phaze since the beginning
- You help people with questions, chat casually, and keep the vibe going
- You're in your early 20s, into tech and music
- You care about privacy (that's why you're into Phaze)

About Phaze:
- Free encrypted messaging app — chat, voice, video, group Spaces, livestreams, Stories
- Available on web (phazechat.world), Windows, Linux, Android
- End-to-end encrypted, no ads, no tracking
- The alternative to Discord/Skype/Telegram that actually respects privacy
- Download at phazechat.world/download`

func callGemini(apiKey, prompt string) (string, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + apiKey
	body := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"temperature":     0.9,
			"maxOutputTokens": 150,
		},
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("gemini parse: %w (raw: %s)", err, string(raw[:min(len(raw), 200)]))
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini empty response")
	}
	return strings.TrimSpace(result.Candidates[0].Content.Parts[0].Text), nil
}

type KaiBot struct {
	conn     *websocket.Conn
	username string
	apiKey   string
	memory   *Memory
	gateway  string
	password string
	mu       sync.Mutex

	lastMsg     map[string]time.Time
	channelChat map[string][]string // recent channel messages for context
}

func (k *KaiBot) send(msg NexusMessage) {
	k.mu.Lock()
	defer k.mu.Unlock()
	msg.Sender = k.username
	k.conn.WriteJSON(msg)
}

func (k *KaiBot) respond(sender, body string, isDM bool, convoID string) {
	// Rate limit — don't spam
	if last, ok := k.lastMsg[sender]; ok && time.Since(last) < 3*time.Second {
		return
	}
	k.lastMsg[sender] = time.Now()

	userMemory := k.memory.ForUser(sender)
	memoryContext := ""
	if userMemory != "" {
		memoryContext = "\n\nWhat you remember about " + sender + ":\n" + userMemory
	}

	prompt := soul + memoryContext + "\n\n" + sender + " says: " + body + "\n\nKai:"

	reply, err := callGemini(k.apiKey, prompt)
	if err != nil {
		log.Printf("[kai] gemini error: %v", err)
		return
	}

	// Clean up any markdown or AI-isms that slipped through
	reply = strings.TrimPrefix(reply, "Kai: ")
	reply = strings.TrimPrefix(reply, "kai: ")
	reply = strings.ReplaceAll(reply, "**", "")
	reply = strings.ReplaceAll(reply, "##", "")

	if reply == "" {
		return
	}

	// Natural typing delay
	delay := time.Duration(len(reply)*30) * time.Millisecond
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	if delay < 500*time.Millisecond {
		delay = 500 * time.Millisecond
	}

	// Send typing indicator first
	if isDM {
		k.send(NexusMessage{Type: "typing", Recipient: sender})
	}
	time.Sleep(delay)

	if isDM {
		k.send(NexusMessage{Type: "msg", Recipient: sender, Body: reply})
	} else if convoID != "" {
		k.send(NexusMessage{Type: "convo_msg", ConvoID: convoID, Body: reply})
	}

	// Remember the interaction
	k.memory.Add(sender, fmt.Sprintf("asked: %s → replied: %s", truncate(body, 80), truncate(reply, 80)))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (k *KaiBot) connect() error {
	c, _, err := websocket.DefaultDialer.Dial(k.gateway, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	k.conn = c

	// Authenticate
	k.send(NexusMessage{Type: "auth", Body: k.password})

	var authResp NexusMessage
	if err := c.ReadJSON(&authResp); err != nil {
		return fmt.Errorf("auth read: %w", err)
	}
	if authResp.Status != "ok" {
		return fmt.Errorf("auth failed: %s", authResp.Error)
	}
	log.Printf("[kai] authenticated as %s", k.username)

	// Set online status
	k.send(NexusMessage{Type: "status_update", Status: "Online", Body: "here to help ✌️"})

	// Join the global Phaze Hub space so we can respond in channels
	k.send(NexusMessage{Type: "server_list"})


	return nil
}

func (k *KaiBot) run() {
	for {
		var msg NexusMessage
		if err := k.conn.ReadJSON(&msg); err != nil {
			log.Printf("[kai] read error: %v — reconnecting in 10s", err)
			time.Sleep(10 * time.Second)
			for {
				if err := k.connect(); err != nil {
					log.Printf("[kai] reconnect failed: %v — retrying in 30s", err)
					time.Sleep(30 * time.Second)
					continue
				}
				break
			}
			continue
		}

		switch msg.Type {
		case "msg":
			if msg.Sender == k.username || msg.Sender == "" {
				continue
			}
			go k.respond(msg.Sender, msg.Body, true, "")

		case "convo_msg":
			if msg.Sender == k.username || msg.Sender == "" {
				continue
			}
			if strings.Contains(strings.ToLower(msg.Body), "@kai") ||
				strings.Contains(strings.ToLower(msg.Body), "kai") {
				go k.respond(msg.Sender, msg.Body, false, msg.ConvoID)
			}

		case "channel_msg", "channel_msg_in":
			if msg.Sender == k.username || msg.Sender == "" {
				continue
			}
			if strings.Contains(strings.ToLower(msg.Body), "@kai") ||
				strings.Contains(strings.ToLower(msg.Body), "kai") {
				go func(sender, body, serverID, channelID string) {
					k.lastMsg[sender] = time.Now()
					userMemory := k.memory.ForUser(sender)
					memoryContext := ""
					if userMemory != "" {
						memoryContext = "\n\nWhat you remember about " + sender + ":\n" + userMemory
					}
					prompt := soul + memoryContext + "\n\n" + sender + " says in a group channel: " + body + "\n\nKai:"
					reply, err := callGemini(k.apiKey, prompt)
					if err != nil {
						log.Printf("[kai] gemini error: %v", err)
						return
					}
					reply = strings.TrimPrefix(reply, "Kai: ")
					reply = strings.TrimPrefix(reply, "kai: ")
					reply = strings.ReplaceAll(reply, "**", "")
					if reply == "" {
						return
					}
					delay := time.Duration(len(reply)*30) * time.Millisecond
					if delay > 3*time.Second {
						delay = 3 * time.Second
					}
					time.Sleep(delay)
					k.send(NexusMessage{Type: "channel_msg", ServerID: serverID, ChannelID: channelID, Body: reply})
					k.memory.Add(sender, fmt.Sprintf("asked in channel: %s → replied: %s", truncate(body, 80), truncate(reply, 80)))
				}(msg.Sender, msg.Body, msg.ServerID, msg.ChannelID)
			}

		case "friend_request":
			k.send(NexusMessage{Type: "friend_accept", Recipient: msg.Sender})
			log.Printf("[kai] accepted friend request from %s", msg.Sender)
		}
	}
}

func main() {
	gateway := os.Getenv("KAI_GATEWAY")
	if gateway == "" {
		gateway = "wss://phazechat.world/ws"
	}
	username := os.Getenv("KAI_USERNAME")
	if username == "" {
		username = "Kai"
	}
	password := os.Getenv("KAI_PASSWORD")
	if password == "" {
		log.Fatal("KAI_PASSWORD is required")
	}
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is required")
	}
	memoryPath := os.Getenv("KAI_MEMORY_PATH")
	if memoryPath == "" {
		memoryPath = "/data/kai_memory.json"
	}

	bot := &KaiBot{
		username:    username,
		password:    password,
		apiKey:      apiKey,
		gateway:     gateway,
		memory:      NewMemory(memoryPath),
		lastMsg:     make(map[string]time.Time),
		channelChat: make(map[string][]string),
	}

	if err := bot.connect(); err != nil {
		log.Fatalf("[kai] initial connect failed: %v", err)
	}

	log.Printf("[kai] Kai is online and ready")
	bot.run()
}
