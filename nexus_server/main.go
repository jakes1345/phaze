package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"bytes"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/getsentry/sentry-go"
	"github.com/gorilla/websocket"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
	"google.golang.org/api/option"
	_ "modernc.org/sqlite"
)

// ---------- CORS / Rate limiting ----------

var allowedOrigins = func() map[string]bool {
	raw := os.Getenv("Phaze_ALLOWED_ORIGINS")
	if raw == "" {
		// H1: warn loudly in production when CORS is wide-open.
		log.Println("[WARNING] Phaze_ALLOWED_ORIGINS is not set — CORS allows ALL origins. Set this in production!")
		return nil // nil = allow all (dev mode)
	}
	m := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		m[strings.TrimSpace(o)] = true
	}
	return m
}()

func originAllowed(r *http.Request) bool {
	if allowedOrigins == nil {
		return true
	}
	return allowedOrigins[r.Header.Get("Origin")]
}

// pstnBridgeEnabled gates Twilio outbound PSTN. Default off so relays run
// WebRTC-only (Phaze-to-Phaze) with no carrier or Twilio call charges.
func pstnBridgeEnabled() bool {
	return strings.EqualFold(os.Getenv("PHAZE_ENABLE_PSTN"), "true")
}

type limiterEntry struct {
	lim  *rate.Limiter
	last time.Time
}

type ipLimiter struct {
	mu       sync.Mutex
	entries  map[string]*limiterEntry
	r        rate.Limit
	burst    int
	idleTTL  time.Duration
	maxSize  int
	lastGC   time.Time
	gcEvery  time.Duration
}

func newIPLimiter(r rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{
		entries: map[string]*limiterEntry{},
		r:       r,
		burst:   burst,
		idleTTL: 10 * time.Minute,
		maxSize: 50000,
		gcEvery: 1 * time.Minute,
	}
}

// gcLocked evicts idle limiters. Caller must hold l.mu.
func (l *ipLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.gcEvery && len(l.entries) < l.maxSize {
		return
	}
	cutoff := now.Add(-l.idleTTL)
	for ip, e := range l.entries {
		if e.last.Before(cutoff) {
			delete(l.entries, ip)
		}
	}
	// If still over cap, drop oldest opportunistically.
	if len(l.entries) > l.maxSize {
		for ip := range l.entries {
			delete(l.entries, ip)
			if len(l.entries) <= l.maxSize*9/10 {
				break
			}
		}
	}
	l.lastGC = now
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)
	e, ok := l.entries[ip]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.r, l.burst)}
		l.entries[ip] = e
	}
	e.last = now
	return e.lim.Allow()
}

// trustedProxyHeader is set when the server runs behind a known reverse proxy
// (Fly.io edge, Cloudflare, nginx). Empty = ignore X-Forwarded-For entirely so
// attackers cannot spoof a source IP to bypass per-IP rate limits.
var trustedProxyHeader = strings.TrimSpace(os.Getenv("PHAZE_TRUST_PROXY_HEADER"))

func clientIP(r *http.Request) string {
	if trustedProxyHeader != "" {
		if v := strings.TrimSpace(r.Header.Get(trustedProxyHeader)); v != "" {
			// Take leftmost entry (original client). Edge proxies append on the right.
			if i := strings.Index(v, ","); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			if v != "" {
				return v
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var globalLimiter = newIPLimiter(rate.Limit(10), 30)      // 10 req/s, burst 30 per IP
var adminLimiter = newIPLimiter(rate.Limit(0.05), 3)     // 3 attempts per minute per IP

// ---------- Auth brute-force protection ----------

// authFailTracker tracks per-IP and per-user consecutive failed auth attempts.
// When thresholds are breached, connections are throttled or blocked entirely.
type authFailTracker struct {
	mu         sync.Mutex
	ipFails    map[string]*authFail // keyed by IP
	userFails  map[string]*authFail // keyed by username
}

type authFail struct {
	count   int
	firstAt time.Time
	lastAt  time.Time
}

var authTracker = &authFailTracker{
	ipFails:   make(map[string]*authFail),
	userFails: make(map[string]*authFail),
}

const (
	// Per-IP: after 5 fails in 15 min, impose progressive delay. At 50, auto-block.
	authIPFailThreshold    = 5
	authIPBlockThreshold   = 50
	authIPWindow           = 15 * time.Minute
	// Per-user: lock account after 10 consecutive fails in 30 min.
	authUserLockThreshold  = 10
	authUserWindow         = 30 * time.Minute
	// TOTP: max 5 attempts per 10 min.
	totpMaxAttempts        = 5
	totpWindow             = 10 * time.Minute
)

func (t *authFailTracker) recordFail(ip, username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()

	// IP tracking
	if f, ok := t.ipFails[ip]; ok {
		if now.Sub(f.firstAt) > authIPWindow {
			*f = authFail{count: 1, firstAt: now, lastAt: now}
		} else {
			f.count++
			f.lastAt = now
		}
	} else {
		t.ipFails[ip] = &authFail{count: 1, firstAt: now, lastAt: now}
	}

	// User tracking
	if username != "" {
		if f, ok := t.userFails[username]; ok {
			if now.Sub(f.firstAt) > authUserWindow {
				*f = authFail{count: 1, firstAt: now, lastAt: now}
			} else {
				f.count++
				f.lastAt = now
			}
		} else {
			t.userFails[username] = &authFail{count: 1, firstAt: now, lastAt: now}
		}
	}
}

func (t *authFailTracker) recordSuccess(ip, username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.ipFails, ip)
	if username != "" {
		delete(t.userFails, username)
	}
}

// isIPThrottled returns true if this IP should be denied login attempts.
func (t *authFailTracker) isIPThrottled(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.ipFails[ip]
	if !ok {
		return false
	}
	if time.Since(f.firstAt) > authIPWindow {
		delete(t.ipFails, ip)
		return false
	}
	return f.count >= authIPFailThreshold
}

// shouldAutoBlock returns true if the IP has exceeded the hard block threshold.
func (t *authFailTracker) shouldAutoBlock(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.ipFails[ip]
	if !ok {
		return false
	}
	return f.count >= authIPBlockThreshold && time.Since(f.firstAt) <= authIPWindow
}

// isUserLocked returns true if too many consecutive auth failures for this user.
func (t *authFailTracker) isUserLocked(username string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.userFails[username]
	if !ok {
		return false
	}
	if time.Since(f.firstAt) > authUserWindow {
		delete(t.userFails, username)
		return false
	}
	return f.count >= authUserLockThreshold
}

// sweepAuthTracker cleans stale entries every 5 minutes.
func sweepAuthTracker() {
	tk := time.NewTicker(5 * time.Minute)
	defer tk.Stop()
	for {
		<-tk.C
		now := time.Now()
		authTracker.mu.Lock()
		for ip, f := range authTracker.ipFails {
			if now.Sub(f.lastAt) > authIPWindow {
				delete(authTracker.ipFails, ip)
			}
		}
		for u, f := range authTracker.userFails {
			if now.Sub(f.lastAt) > authUserWindow {
				delete(authTracker.userFails, u)
			}
		}
		authTracker.mu.Unlock()
	}
}

// adminIPGate restricts admin endpoints to allowed IPs when PHAZE_ADMIN_IPS
// is set (comma-separated CIDRs or IPs). If unset, all IPs are allowed.
func adminIPGate(next http.HandlerFunc) http.HandlerFunc {
	raw := strings.TrimSpace(os.Getenv("PHAZE_ADMIN_IPS"))
	if raw == "" {
		return next
	}
	allowed := strings.Split(raw, ",")
	for i := range allowed {
		allowed[i] = strings.TrimSpace(allowed[i])
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		ok := false
		for _, a := range allowed {
			if a == ip || a == "0.0.0.0" {
				ok = true
				break
			}
			if strings.Contains(a, "/") {
				_, cidr, err := net.ParseCIDR(a)
				if err == nil && cidr.Contains(net.ParseIP(ip)) {
					ok = true
					break
				}
			}
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		next(w, r)
	}
}

// adminLoginLimit is a tighter rate limiter specifically for admin login —
// 3 attempts per minute per IP to prevent brute-force.
func adminLoginLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeSecurityHeaders(w)
		if !adminLimiter.allow(clientIP(r)) {
			http.Error(w, "too many login attempts — try again in a minute", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Defense-in-depth headers on every response that goes through
		// the limiter. Cheap, applies broadly, and stops a few classes
		// of attack (MIME sniffing, clickjacking, leaky referrers).
		writeSecurityHeaders(w)
		if !globalLimiter.allow(clientIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// writeSecurityHeaders sets headers that protect every HTTP response.
// Intentionally conservative: only headers that won't break the React SPA
// or the existing API. CSP is omitted because the /admin portal uses
// inline scripts; if you tighten that later, add it here.
func writeSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
}

// NexusMessage is the wire protocol for Phaze™
type NexusMessage struct {
	Type        string      `json:"type"`
	Sender      string      `json:"sender"`
	Recipient   string      `json:"recipient"`
	Body        string      `json:"body"`
	Status      string      `json:"status"`
	Results     []string    `json:"results"`
	SDP         string      `json:"sdp"`
	Candidate   string      `json:"candidate"`
	Token       string      `json:"token"`
	Error       string      `json:"error"`
	Email       string      `json:"email,omitempty"`
	Mood        string      `json:"mood,omitempty"`
	DisplayName string      `json:"display_name,omitempty"`
	Supporter   bool        `json:"supporter,omitempty"`
	ConvoID     string      `json:"convo_id,omitempty"`
	ConvoName   string      `json:"convo_name,omitempty"`
	Members     []string    `json:"members,omitempty"`
	TurnConfig  *TurnConfig `json:"turn_config,omitempty"`
	TOTPCode    string      `json:"totp_code,omitempty"`
	TOTPURI     string      `json:"totp_uri,omitempty"`
	QRToken     string      `json:"qr_token,omitempty"`
	QRData      string      `json:"qr_data,omitempty"`
	DeviceInfo  string      `json:"device_info,omitempty"`

	// MsgID is a stable client-generated ID for a chat message. It lets edits,
	// deletes, and reactions target a specific previously-sent message without
	// needing the server to assign IDs (DMs are E2EE so the server can't see
	// content anyway). Forwarded verbatim to the recipient.
	MsgID    string `json:"msg_id,omitempty"`
	Reaction string `json:"reaction,omitempty"`

	// Envelopes[recipient] = ciphertext body encrypted to that member's key.
	// Used for group E2EE: the client fans out per-member ciphertext so the
	// server never sees plaintext. Only set on "convo_msg".
	Envelopes map[string]string `json:"envelopes,omitempty"`

	// PublicKey / KeyFingerprint forwarded so pairwise TOFU still works when
	// a client is about to send a group envelope to a member it hasn't keyed.
	PublicKey      []byte `json:"public_key,omitempty"`
	KeyFingerprint string `json:"key_fingerprint,omitempty"`

	// --- Servers + Channels (Discord-style "Spaces") ---
	ServerID    string             `json:"server_id,omitempty"`
	ChannelID   string             `json:"channel_id,omitempty"`
	ServerName  string             `json:"server_name,omitempty"`
	ChannelName string             `json:"channel_name,omitempty"`
	Topic       string             `json:"topic,omitempty"`
	Kind        string             `json:"kind,omitempty"`  // "text" | "voice"
	Role        string             `json:"role,omitempty"`  // member | admin | owner
	Visibility  string             `json:"visibility,omitempty"` // public | private
	InviteCode  string             `json:"invite_code,omitempty"`
	Servers     []ServerSummary    `json:"servers,omitempty"`
	Channels    []ChannelInfo      `json:"channels,omitempty"`
	Messages    []ChannelMsg       `json:"messages,omitempty"`
	HistoryFrom int64              `json:"history_from,omitempty"` // id cursor (return messages with id < this)

	// DMHistory is the response payload for "dm_history" requests:
	// durable cross-device history of DMs between the requester and Recipient.
	DMHistory []DMMessage `json:"dm_history,omitempty"`

	// KeyBackup carries the PIN-encrypted E2EE keypair blob between client
	// and server. Used by key_backup_put / key_backup_get cases.
	KeyBackup *KeyBackupPayload `json:"key_backup,omitempty"`
}

// ServerSummary is the slim view a client gets for the server-list pane.
type ServerSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Owner       string `json:"owner"`
	Visibility  string `json:"visibility"`
	Role        string `json:"role"`
	InviteCode  string `json:"invite_code,omitempty"`
}

// ChannelInfo is one channel inside a server.
type ChannelInfo struct {
	ID       string `json:"id"`
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	Topic    string `json:"topic,omitempty"`
	Kind     string `json:"kind"`
	Position int    `json:"position"`
}

// ChannelMsg is one row of channel chat history (plaintext server-side).
type ChannelMsg struct {
	ID        int64               `json:"id"`
	ChannelID string              `json:"channel_id"`
	Sender    string              `json:"sender"`
	Body      string              `json:"body"`
	CreatedAt string              `json:"created_at"`
	Edited    bool                `json:"edited,omitempty"`
	Deleted   bool                `json:"deleted,omitempty"`
	Pinned    bool                `json:"pinned,omitempty"`
	Reactions map[string][]string `json:"reactions,omitempty"` // emoji -> users
}

// KeyBackupPayload is the opaque envelope clients use to back up their
// E2EE NaCl keypair. The server never sees plaintext: ciphertext is the
// AES-GCM encryption of the keypair under a key derived from the user's
// recovery PIN via PBKDF2(Salt, Iterations). CreatedAt is informational.
type KeyBackupPayload struct {
	Ciphertext string `json:"ciphertext"`
	Salt       string `json:"salt"`
	Iterations int    `json:"iterations"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// DMMessage is one row from dm_messages serialized back to clients during
// history fetch. The body is still E2EE-encrypted — the client decrypts.
type DMMessage struct {
	MsgID     string              `json:"msg_id"`
	Sender    string              `json:"sender"`
	Recipient string              `json:"recipient"`
	Body      string              `json:"body"`
	Edited    bool                `json:"edited,omitempty"`
	Deleted   bool                `json:"deleted,omitempty"`
	CreatedAt string              `json:"created_at"`
	Reactions map[string][]string `json:"reactions,omitempty"`
}

type TurnConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Client struct {
	Conn     *websocket.Conn
	Username string
	Status   string
	IP       string
	// writeMu serializes all writes to Conn. gorilla/websocket is explicit
	// that concurrent writes are undefined behavior, and this server fans
	// messages out to other clients' conns from unrelated read-loops.
	writeMu sync.Mutex
	// msgLimiter throttles inbound WS messages per-connection. The HTTP
	// rateLimit middleware only covers the upgrade handshake — once WS is
	// established, a single authed client could otherwise flood the read
	// loop unbounded.
	msgLimiter *rate.Limiter
}

// Send locks the per-connection write mutex and emits a JSON message.
// Every outbound WebSocket write goes through here — do not call
// WriteJSON on the underlying connection directly.
func (c *Client) Send(m NexusMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	conn := c.Conn
	// M4: prevent slow-client goroutine leaks — if the peer stops reading,
	// the write times out after 10s instead of blocking forever.
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(m)
}

type NexusServer struct {
	DB         *sql.DB
	Clients    map[string]*Client
	Mu         sync.RWMutex
	fcmClient  *messaging.Client

	// VoiceRooms tracks who is currently in each voice channel.
	// channel_id -> set of usernames.
	VoiceRooms   map[string]map[string]struct{}
	VoiceRoomsMu sync.RWMutex
	// Streams maps broadcaster username -> active livestream metadata.
	// Guarded by VoiceRoomsMu (shared mutex — both are room-style state).
	Streams map[string]*liveStream
	// RemoteCodes maps 6-digit session codes to host info for remote control.
	// L2: entries include creation time for TTL enforcement.
	RemoteCodes   map[string]remoteCodeEntry
	RemoteCodesMu sync.RWMutex
	// Per-IP WebSocket connection counter for DDoS mitigation.
	wsConnCount   map[string]int
	wsConnCountMu sync.Mutex
	// IP blocklist — blocked IPs cannot connect.
	blockedIPs   map[string]bool
	blockedIPsMu sync.RWMutex
}

func (s *NexusServer) isIPBlocked(ip string) bool {
	s.blockedIPsMu.RLock()
	defer s.blockedIPsMu.RUnlock()
	if s.blockedIPs == nil {
		return false
	}
	return s.blockedIPs[ip]
}

func (s *NexusServer) blockIP(ip string) {
	s.blockedIPsMu.Lock()
	if s.blockedIPs == nil {
		s.blockedIPs = make(map[string]bool)
	}
	s.blockedIPs[ip] = true
	s.blockedIPsMu.Unlock()
	// M3: persist to DB so the block survives restarts.
	s.DB.Exec(`INSERT OR IGNORE INTO blocked_ips (ip) VALUES (?)`, ip)
}

func (s *NexusServer) unblockIP(ip string) {
	s.blockedIPsMu.Lock()
	if s.blockedIPs == nil {
		s.blockedIPs = make(map[string]bool)
	}
	delete(s.blockedIPs, ip)
	s.blockedIPsMu.Unlock()
	s.DB.Exec(`DELETE FROM blocked_ips WHERE ip = ?`, ip)
}

// loadBlockedIPs loads persisted IP blocks from the database on boot.
func (s *NexusServer) loadBlockedIPs() {
	s.blockedIPsMu.Lock()
	if s.blockedIPs == nil {
		s.blockedIPs = make(map[string]bool)
	}
	s.blockedIPsMu.Unlock()
	rows, err := s.DB.Query(`SELECT ip FROM blocked_ips`)
	if err != nil {
		log.Printf("[ipblock] load: %v", err)
		return
	}
	defer rows.Close()
	s.blockedIPsMu.Lock()
	for rows.Next() {
		var ip string
		if rows.Scan(&ip) == nil {
			s.blockedIPs[ip] = true
		}
	}
	count := len(s.blockedIPs)
	s.blockedIPsMu.Unlock()
	if count > 0 {
		log.Printf("[ipblock] loaded %d blocked IPs from database", count)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: originAllowed,
}

// remoteCodeEntry holds a remote-control pairing code with an expiry.
type remoteCodeEntry struct {
	Username  string
	CreatedAt time.Time
}

const remoteCodeTTL = 10 * time.Minute

// sweepRemoteCodes removes expired remote control codes every 2 minutes.
func (s *NexusServer) sweepRemoteCodes() {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		<-t.C
		now := time.Now()
		s.RemoteCodesMu.Lock()
		for k, v := range s.RemoteCodes {
			if now.Sub(v.CreatedAt) > remoteCodeTTL {
				delete(s.RemoteCodes, k)
			}
		}
		s.RemoteCodesMu.Unlock()
	}
}

// ---------- Sovereign Media Configuration ----------

var (
	TurnSecret    = os.Getenv("PHAZE_TURN_SECRET")
	TurnURL       = os.Getenv("PHAZE_TURN_URL")
	TurnShortTerm = os.Getenv("PHAZE_TURN_SHORT_TERM") == "true"
)

// openRelayTURN is a free public TURN/STUN cluster from metered.ca. We use
// it as the automatic fallback when no self-hosted TURN is configured, so
// voice/video calls work for users behind symmetric NAT out of the box.
// For production traffic, set PHAZE_TURN_URL + PHAZE_TURN_SECRET to point
// at a self-hosted coturn instance — open relay has bandwidth limits.
var openRelayTURN = &TurnConfig{
	URL:      "turn:openrelay.metered.ca:80",
	Username: "openrelayproject",
	Password: "openrelayproject",
}

func (s *NexusServer) generateMediaToken(username string) *TurnConfig {
	if TurnSecret == "" || TurnURL == "" {
		log.Printf("[TURN] No self-hosted TURN configured — falling back to openrelay.metered.ca public TURN for %s", username)
		return openRelayTURN
	}

	var expiresIn time.Duration
	if TurnShortTerm {
		expiresIn = 10 * time.Minute
	} else {
		expiresIn = 24 * time.Hour
	}

	timestamp := time.Now().Add(expiresIn).Unix()
	user := fmt.Sprintf("%d:%s", timestamp, username)

	mac := hmac.New(sha1.New, []byte(TurnSecret))
	mac.Write([]byte(user))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	log.Printf("[TURN] Generated token for %s (expires in %v)", username, expiresIn)

	return &TurnConfig{
		URL:      TurnURL,
		Username: user,
		Password: password,
	}
}

func (s *NexusServer) initDB() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			email TEXT,
			mood TEXT,
			display_name TEXT,
			password_hash TEXT NOT NULL,
			salt TEXT NOT NULL,
			is_verified INTEGER DEFAULT 0,
			verification_code TEXT,
			phone_number TEXT,
			phone_verified INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS friends (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_a TEXT NOT NULL,
			user_b TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_a, user_b)
		)`,
		`CREATE TABLE IF NOT EXISTS offline_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			body TEXT NOT NULL,
			msg_type TEXT NOT NULL DEFAULT 'msg',
			convo TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS conversation_members (
			convo_id TEXT NOT NULL,
			username TEXT NOT NULL,
			PRIMARY KEY (convo_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS blocks (
			blocker TEXT NOT NULL,
			blocked TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (blocker, blocked)
		)`,
		`CREATE TABLE IF NOT EXISTS abuse_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reporter TEXT NOT NULL,
			subject TEXT NOT NULL,
			reason TEXT NOT NULL,
			body TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS session_tokens (
			token TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			device_info TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			revoked INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS password_resets (
			token TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			used INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS qr_login_tokens (
			token TEXT PRIMARY KEY,
			username TEXT DEFAULT '',
			session_token TEXT DEFAULT '',
			device_info TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			approved INTEGER DEFAULT 0
		)`,
		// --- Servers ("Spaces") + Channels ---
		// Persistent communities. Channel-level chat history is server-side
		// plaintext (unlike 1:1 / convo E2EE) so search, moderation, and join
		// history work. Private servers + E2EE-channels are a future feature.
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			icon TEXT DEFAULT '',
			owner TEXT NOT NULL,
			visibility TEXT NOT NULL DEFAULT 'private',
			invite_code TEXT UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			topic TEXT DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'text',
			position INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_members (
			server_id TEXT NOT NULL,
			username TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'member',
			joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (server_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS channel_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id TEXT NOT NULL,
			sender TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channels_server ON channels(server_id, position)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_messages ON channel_messages(channel_id, id)`,
		`CREATE TABLE IF NOT EXISTS channel_reactions (
			msg_id INTEGER NOT NULL,
			emoji TEXT NOT NULL,
			username TEXT NOT NULL,
			PRIMARY KEY (msg_id, emoji, username)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_reactions ON channel_reactions(msg_id)`,
		`CREATE TABLE IF NOT EXISTS stories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			author TEXT NOT NULL,
			media_url TEXT NOT NULL,
			media_kind TEXT NOT NULL,
			caption TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_stories_author ON stories(author, expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_stories_expires ON stories(expires_at)`,
		`CREATE TABLE IF NOT EXISTS story_views (
			story_id INTEGER NOT NULL,
			viewer TEXT NOT NULL,
			viewed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (story_id, viewer)
		)`,
		// Per-user key/value preferences (muted peers, onboarding flag, theme,
		// notification settings, anything that was previously local-only).
		// Value is JSON-encoded so callers can store strings, lists, objects.
		`CREATE TABLE IF NOT EXISTS user_settings (
			username TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (username, key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_members_user ON server_members(username)`,
		`CREATE INDEX IF NOT EXISTS idx_servers_invite ON servers(invite_code)`,
		`CREATE TABLE IF NOT EXISTS push_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			endpoint TEXT NOT NULL UNIQUE,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_username ON push_subscriptions(username)`,
		`CREATE TABLE IF NOT EXISTS invite_codes (
			code TEXT PRIMARY KEY,
			inviter TEXT NOT NULL,
			email TEXT NOT NULL,
			used INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			used_at DATETIME
		)`,
		// dm_messages: durable cross-device history of direct messages.
		// body holds the E2EE ciphertext exactly as the sender produced it —
		// the server cannot decrypt. msg_id is the client-generated stable
		// ID so edits/deletes/reactions can target a row from either side.
		`CREATE TABLE IF NOT EXISTS dm_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			msg_id TEXT NOT NULL UNIQUE,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			body TEXT NOT NULL,
			edited INTEGER DEFAULT 0,
			deleted INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dm_pair       ON dm_messages(sender, recipient, id)`,
		`CREATE INDEX IF NOT EXISTS idx_dm_pair_rev   ON dm_messages(recipient, sender, id)`,
		`CREATE TABLE IF NOT EXISTS dm_reactions (
			msg_id TEXT NOT NULL,
			username TEXT NOT NULL,
			emoji TEXT NOT NULL,
			PRIMARY KEY (msg_id, username, emoji)
		)`,
		// key_backups: PIN-encrypted NaCl keypair. The blob is opaque to the
		// server — encrypted client-side with a key derived from a user-chosen
		// recovery PIN (PBKDF2/Argon2). Lets a public user restore their E2EE
		// identity on a new browser / device / after a localStorage wipe.
		`CREATE TABLE IF NOT EXISTS key_backups (
			username TEXT PRIMARY KEY,
			ciphertext TEXT NOT NULL,
			salt TEXT NOT NULL,
			iterations INTEGER NOT NULL DEFAULT 200000,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// M3: persisted IP blocklist — survives server restarts.
		`CREATE TABLE IF NOT EXISTS blocked_ips (
			ip TEXT PRIMARY KEY,
			blocked_by TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, q := range tables {
		if _, err := s.DB.Exec(q); err != nil {
			log.Fatalf("DB init error: %v", err)
		}
	}
	// Idempotent column migrations for existing deployments.
	migrations := []string{
		`ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN totp_enabled INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0`,
		// Role hierarchy. Values (low → high): "user","helper","moderator","admin","super_admin".
		// Kept alongside is_admin for backwards-compatibility; is_admin is set to 1
		// when role is admin or super_admin and used by older code paths.
		`ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user'`,
		// Channel-message lifecycle: edits, deletes, pins. Added late so old
		// rows have NULL/0 defaults that the read path treats as "unedited".
		`ALTER TABLE channel_messages ADD COLUMN edited INTEGER DEFAULT 0`,
		`ALTER TABLE channel_messages ADD COLUMN deleted INTEGER DEFAULT 0`,
		`ALTER TABLE channel_messages ADD COLUMN pinned INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN is_banned INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN ban_reason TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN banned_at DATETIME`,
		`ALTER TABLE abuse_reports ADD COLUMN status TEXT DEFAULT 'pending'`,
		`ALTER TABLE abuse_reports ADD COLUMN resolved_by TEXT DEFAULT ''`,
		`ALTER TABLE abuse_reports ADD COLUMN resolved_at DATETIME`,
		`CREATE INDEX IF NOT EXISTS idx_abuse_reports_status ON abuse_reports(status)`,
		`CREATE INDEX IF NOT EXISTS idx_users_banned ON users(is_banned)`,
		`ALTER TABLE users ADD COLUMN fcm_token TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_ip TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN last_login_at DATETIME`,
		`ALTER TABLE users ADD COLUMN signup_ip TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN phone_verification_code TEXT`,
		// Supporters: a `supporter` flag (with the date it was granted) plus a
		// queue of opt-in requests captured by the public support form. The
		// admin matches a request against the actual Buy Me a Coffee payment
		// notification, then grants the badge — see supporters.go.
		`ALTER TABLE users ADD COLUMN supporter INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN supporter_since DATETIME`,
		`CREATE TABLE IF NOT EXISTS supporter_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT,
			name TEXT,
			email TEXT,
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_supporter_requests_status ON supporter_requests(status)`,
	}
	for _, q := range migrations {
		if _, err := s.DB.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column") && !strings.Contains(err.Error(), "already exists") {
			log.Printf("DB migration skipped (%v)", err)
		}
	}

	// Promote any usernames listed in PHAZE_ADMIN_USERS (comma-separated) to
	// admin on every boot. Lets you bootstrap the first admin without a DB
	// shell, and keeps admin status in sync if you rotate the env var.
	if raw := strings.TrimSpace(os.Getenv("PHAZE_ADMIN_USERS")); raw != "" {
		for _, u := range strings.Split(raw, ",") {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			if _, err := s.DB.Exec(`UPDATE users SET is_admin = 1, role = 'super_admin' WHERE username = ?`, u); err != nil {
				log.Printf("[admin] promote %s: %v", u, err)
			}
		}
	}
	// Backfill role from legacy is_admin flag for users created before the
	// role column existed: existing admins get the lowest-privilege admin
	// tier (real admin, not super_admin). Set super_admin only via the
	// PHAZE_ADMIN_USERS env var above.
	if _, err := s.DB.Exec(`UPDATE users SET role = 'admin' WHERE is_admin = 1 AND (role = '' OR role = 'user')`); err != nil {
		log.Printf("[role] backfill: %v", err)
	}
}

// ---------- Roles ----------

// Role hierarchy from lowest to highest. roleRank(r) returns the numeric
// position; higher rank means more power. Use roleAtLeast(actor, target)
// to gate endpoints.
var roleRanks = map[string]int{
	"user":        0,
	"helper":      1,
	"moderator":   2,
	"admin":       3,
	"super_admin": 4,
}

func roleRank(role string) int {
	if r, ok := roleRanks[role]; ok {
		return r
	}
	return 0
}

func (s *NexusServer) userRole(username string) string {
	var r string
	if err := s.DB.QueryRow(`SELECT COALESCE(role, 'user') FROM users WHERE username = ?`, username).Scan(&r); err != nil {
		return "user"
	}
	if r == "" {
		return "user"
	}
	return r
}

// roleAtLeast returns true when the user's role rank >= required rank.
func (s *NexusServer) roleAtLeast(username, minRole string) bool {
	return roleRank(s.userRole(username)) >= roleRank(minRole)
}

// ---------- Account Management (bcrypt) ----------

func (s *NexusServer) registerUser(username, email, mood, password string) (string, error) {
	if !validUsername(username) {
		return "", errBadUsername
	}
	if email == "" {
		return "", fmt.Errorf("email is required")
	}
	if !validEmail(email) {
		return "", errBadEmail
	}
	if len(password) < 8 {
		return "", errShortPassword
	}
	// C2: pre-hash with SHA-256 to avoid bcrypt's silent 72-byte truncation.
	pwHash := sha256.Sum256([]byte(password))
	hash, err := bcrypt.GenerateFromPassword(pwHash[:], bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	code, err := randDigits(6)
	if err != nil {
		return "", err
	}

	_, err = s.DB.Exec("INSERT INTO users (username, email, mood, password_hash, salt, verification_code) VALUES (?, ?, ?, ?, '', ?)",
		username, email, mood, string(hash), code)
	return code, err
}

// ---------- Auth MVP helpers (OTP, email, username, session, TOTP, reset) ----------

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

func validUsername(u string) bool { return usernameRegex.MatchString(u) }

func validEmail(e string) bool {
	if e == "" {
		return false
	}
	_, err := mail.ParseAddress(e)
	return err == nil
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randDigits(n int) (string, error) {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + v.Int64())
	}
	return string(out), nil
}

func (s *NexusServer) issueSessionToken(username, device string) (string, error) {
	tok, err := randHex(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO session_tokens (token, username, device_info, expires_at) VALUES (?, ?, ?, ?)",
		tok, username, device, expires,
	)
	return tok, err
}

func (s *NexusServer) issueAdminSessionToken(username string) (string, error) {
	tok, err := randHex(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(4 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO session_tokens (token, username, device_info, expires_at) VALUES (?, ?, ?, ?)",
		tok, username, "admin-portal", expires,
	)
	return tok, err
}

func (s *NexusServer) sessionUsername(token string) string {
	if token == "" {
		return ""
	}
	var u string
	var expires time.Time
	var revoked int
	err := s.DB.QueryRow(
		"SELECT username, expires_at, revoked FROM session_tokens WHERE token = ?",
		token,
	).Scan(&u, &expires, &revoked)
	if err != nil || revoked != 0 || time.Now().After(expires) {
		return ""
	}
	return u
}

func (s *NexusServer) revokeSession(token string) {
	s.DB.Exec("UPDATE session_tokens SET revoked = 1 WHERE token = ?", token)
}

// deleteAccount performs a GDPR Article 17 ("right to erasure") cascade for a
// single user. Runs in a single transaction so partial failure leaves the
// account intact. Reports MADE BY the user are removed; reports ABOUT the
// user are retained (the subject column is just text, so the username string
// remains in the safety log — this is the legitimate-interests carve-out).
func (s *NexusServer) deleteAccount(username string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM friends WHERE user_a = ? OR user_b = ?`, []any{username, username}},
		{`DELETE FROM offline_messages WHERE sender = ? OR recipient = ?`, []any{username, username}},
		{`DELETE FROM conversation_members WHERE username = ?`, []any{username}},
		{`DELETE FROM conversations WHERE created_by = ?`, []any{username}},
		{`DELETE FROM blocks WHERE blocker = ? OR blocked = ?`, []any{username, username}},
		{`DELETE FROM abuse_reports WHERE reporter = ?`, []any{username}},
		{`DELETE FROM session_tokens WHERE username = ?`, []any{username}},
		{`DELETE FROM password_resets WHERE username = ?`, []any{username}},
		{`DELETE FROM qr_login_tokens WHERE username = ?`, []any{username}},
		// Drop the user last — every other row referencing the username is
		// already gone, so a foreign-key constraint (if added later) would still
		// pass.
		{`DELETE FROM users WHERE username = ?`, []any{username}},
	}
	for _, q := range statements {
		if _, err := tx.Exec(q.sql, q.args...); err != nil {
			return fmt.Errorf("deleteAccount %q: %w", q.sql, err)
		}
	}
	return tx.Commit()
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
}

func (s *NexusServer) verifyTOTP(username, code string) bool {
	secret, enabled := s.totpStatus(username)
	if !enabled || secret == "" {
		return true
	}
	return totp.Validate(code, secret)
}

func (s *NexusServer) createPasswordReset(email string) (string, string, error) {
	var username string
	err := s.DB.QueryRow("SELECT username FROM users WHERE email = ?", email).Scan(&username)
	if err != nil {
		return "", "", err
	}
	tok, err := randHex(24)
	if err != nil {
		return "", "", err
	}
	expires := time.Now().Add(1 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO password_resets (token, username, expires_at) VALUES (?, ?, ?)",
		tok, username, expires,
	)
	return tok, username, err
}

func (s *NexusServer) consumePasswordReset(token, newPassword string) error {
	if len(newPassword) < 8 {
		return errShortPassword
	}
	var username string
	var expires time.Time
	var used int
	err := s.DB.QueryRow(
		"SELECT username, expires_at, used FROM password_resets WHERE token = ?",
		token,
	).Scan(&username, &expires, &used)
	if err != nil {
		return err
	}
	if used != 0 || time.Now().After(expires) {
		return errResetInvalid
	}
	// C2: pre-hash with SHA-256 to avoid bcrypt's silent 72-byte truncation.
	pwHash := sha256.Sum256([]byte(newPassword))
	hash, err := bcrypt.GenerateFromPassword(pwHash[:], bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec("UPDATE password_resets SET used = 1 WHERE token = ?", token); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *NexusServer) createQRLogin() (string, error) {
	tok, err := randHex(16)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(5 * time.Minute)
	_, err = s.DB.Exec(
		"INSERT INTO qr_login_tokens (token, expires_at) VALUES (?, ?)",
		tok, expires,
	)
	return tok, err
}

func (s *NexusServer) approveQRLogin(token, username, device string) error {
	sess, err := s.issueSessionToken(username, device)
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(
		"UPDATE qr_login_tokens SET username = ?, session_token = ?, device_info = ?, approved = 1 WHERE token = ? AND approved = 0 AND expires_at > CURRENT_TIMESTAMP",
		username, sess, device, token,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errQRInvalid
	}
	return nil
}

func (s *NexusServer) checkQRLogin(token string) (string, string, bool, bool) {
	var username, sess string
	var approved int
	var expires time.Time
	err := s.DB.QueryRow(
		"SELECT username, session_token, approved, expires_at FROM qr_login_tokens WHERE token = ?",
		token,
	).Scan(&username, &sess, &approved, &expires)
	if err == sql.ErrNoRows {
		return "", "", false, false
	}
	if err != nil || time.Now().After(expires) {
		return "", "", false, true
	}
	return username, sess, approved == 1, true
}

func (s *NexusServer) verifyUser(username, code string) bool {
	var dbCode string
	err := s.DB.QueryRow("SELECT verification_code FROM users WHERE username = ?", username).Scan(&dbCode)
	// H6: constant-time comparison to prevent timing side-channels on OTP codes.
	if err != nil || dbCode == "" || code == "" || subtle.ConstantTimeCompare([]byte(dbCode), []byte(code)) != 1 {
		return false
	}
	_, err = s.DB.Exec("UPDATE users SET is_verified = 1, verification_code = NULL WHERE username = ?", username)
	return err == nil
}

func (s *NexusServer) sendEmail(to, subject, body string) error {
	// Preference order: Resend (no IP allowlist) → Brevo → SMTP. Each one
	// is opt-in via its env var; users can run with whichever they have.
	if apiKey := os.Getenv("RESEND_API_KEY"); apiKey != "" {
		return sendEmailResend(apiKey, to, subject, body)
	}
	if apiKey := os.Getenv("BREVO_API_KEY"); apiKey != "" {
		return sendEmailBrevo(apiKey, to, subject, body)
	}
	return sendEmailSMTP(to, subject, body)
}

// sendEmailResend posts to Resend's HTTP API. No IP allowlist, no SMTP
// fragility — just an API key. Free tier covers 100 emails/day, plenty
// for verification flows. https://resend.com/docs/api-reference/emails/send-email
func sendEmailResend(apiKey, to, subject, body string) error {
	from := os.Getenv("RESEND_SENDER")
	if from == "" {
		// Resend requires the sender to be from a verified domain. Default
		// to onboarding@resend.dev (Resend's built-in sandbox sender) so
		// the first run works without any DNS setup; users override with
		// RESEND_SENDER="Phaze <noreply@phazechat.world>" once their
		// custom domain is verified in the Resend dashboard.
		from = "Phaze <onboarding@resend.dev>"
	}
	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    body,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("resend api %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

// sendEmailBrevo posts to Brevo's transactional API. Preferred over SMTP — auth
// is a single header, no port 587, and bounces/opens come back via webhooks.
func sendEmailBrevo(apiKey, to, subject, body string) error {
	fromEmail := os.Getenv("BREVO_SENDER_EMAIL")
	if fromEmail == "" {
		fromEmail = os.Getenv("SMTP_FROM")
	}
	if fromEmail == "" {
		fromEmail = "noreply@phazechat.world"
	}
	fromName := os.Getenv("BREVO_SENDER_NAME")
	if fromName == "" {
		fromName = "Phaze"
	}

	payload := map[string]any{
		"sender":      map[string]string{"name": fromName, "email": fromEmail},
		"to":          []map[string]string{{"email": to}},
		"subject":     subject,
		"htmlContent": body,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("brevo api %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func sendEmailSMTP(to, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")

	if host == "" || user == "" || pass == "" {
		log.Printf("[MAIL-SIM] To: %s | Subject: %s | Body: %s", to, subject, body)
		return nil
	}

	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = user
	}
	auth := smtp.PlainAuth("", user, pass, host)
	// C4: sanitize header values — strip CR/LF to prevent SMTP header injection.
	sanitizeHeader := func(s string) string {
		return strings.NewReplacer("\r", "", "\n", "").Replace(s)
	}
	msg := []byte("From: Phaze <" + sanitizeHeader(from) + ">\r\n" +
		"To: " + sanitizeHeader(to) + "\r\n" +
		"Subject: " + sanitizeHeader(subject) + "\r\n" +
		"MIME-version: 1.0\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		body + "\r\n")

	addr := fmt.Sprintf("%s:%s", host, port)
	if port == "" {
		addr = host + ":587"
	}

	return smtp.SendMail(addr, auth, user, []string{to}, msg)
}

// sendEmailLogged wraps sendEmail for 'go'-launched calls so SMTP failures
// land in logs instead of vanishing silently. Use this any time email
// delivery is not on the caller's synchronous response path.
func (s *NexusServer) sendEmailLogged(to, subject, body string) {
	if err := s.sendEmail(to, subject, body); err != nil {
		log.Printf("[mail] send to %s subject %q failed: %v", to, subject, err)
	}
}

func (s *NexusServer) authenticateUser(username, password string) bool {
	var hash string
	var isVerified bool
	err := s.DB.QueryRow("SELECT password_hash, is_verified FROM users WHERE username = ?", username).Scan(&hash, &isVerified)
	if err != nil {
		return false
	}
	if !isVerified {
		return false
	}
	// C2 dual-path migration:
	// 1. Try new scheme: bcrypt(SHA256(password))
	// 2. Fall back to legacy: bcrypt(password)
	// On successful legacy match, silently re-hash with the new scheme
	// so future logins use the hardened path. After all users have logged
	// in once post-deploy, the fallback can be removed.
	pwHash := sha256.Sum256([]byte(password))
	if bcrypt.CompareHashAndPassword([]byte(hash), pwHash[:]) == nil {
		return true // new scheme match
	}
	// Legacy fallback: raw password was hashed directly
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
		// Silently upgrade to new scheme
		newHash, err := bcrypt.GenerateFromPassword(pwHash[:], bcrypt.DefaultCost)
		if err == nil {
			s.DB.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(newHash), username)
			log.Printf("[security] migrated %s password hash to SHA256+bcrypt", username)
		}
		return true
	}
	return false
}

// userBanInfo returns (banned, reason). Reason is empty for non-banned users.
func (s *NexusServer) userBanInfo(username string) (bool, string) {
	var banned int
	var reason string
	err := s.DB.QueryRow(`SELECT is_banned, COALESCE(ban_reason, '') FROM users WHERE username = ?`, username).Scan(&banned, &reason)
	if err != nil {
		return false, ""
	}
	return banned == 1, reason
}

// userIsAdmin reports whether the user has the admin flag set.
func (s *NexusServer) userIsAdmin(username string) bool {
	var n int
	err := s.DB.QueryRow(`SELECT is_admin FROM users WHERE username = ?`, username).Scan(&n)
	if err != nil {
		return false
	}
	return n == 1
}

// ---------- Servers + Channels (Discord-style "Spaces") ----------

var validServerName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _\-\.']{1,63}$`)
var validChannelName = regexp.MustCompile(`^[a-z0-9][a-z0-9\-_]{1,31}$`)

// userIsServerMember reports whether the user is in the given server.
func (s *NexusServer) userIsServerMember(server, user string) bool {
	var n int
	s.DB.QueryRow(`SELECT 1 FROM server_members WHERE server_id = ? AND username = ?`, server, user).Scan(&n)
	return n == 1
}

// userServerRole returns the user's role in the server, or "" if not a member.
func (s *NexusServer) userServerRole(server, user string) string {
	var r string
	if err := s.DB.QueryRow(`SELECT role FROM server_members WHERE server_id = ? AND username = ?`, server, user).Scan(&r); err != nil {
		return ""
	}
	return r
}

// listUserServers returns the server-list pane data for a user.
func (s *NexusServer) listUserServers(username string) ([]ServerSummary, error) {
	rows, err := s.DB.Query(
		`SELECT s.id, s.name, COALESCE(s.description,''), COALESCE(s.icon,''),
		         s.owner, s.visibility, m.role, COALESCE(s.invite_code,'')
		   FROM servers s JOIN server_members m ON s.id = m.server_id
		  WHERE m.username = ?
		  ORDER BY m.joined_at ASC`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSummary{}
	for rows.Next() {
		var ss ServerSummary
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.Description, &ss.Icon, &ss.Owner, &ss.Visibility, &ss.Role, &ss.InviteCode); err != nil {
			continue
		}
		out = append(out, ss)
	}
	return out, nil
}

// listServerChannels returns every channel in a server.
func (s *NexusServer) listServerChannels(serverID string) ([]ChannelInfo, error) {
	rows, err := s.DB.Query(
		`SELECT id, server_id, name, COALESCE(topic,''), kind, position
		   FROM channels WHERE server_id = ?
		  ORDER BY position ASC, name ASC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelInfo{}
	for rows.Next() {
		var c ChannelInfo
		if err := rows.Scan(&c.ID, &c.ServerID, &c.Name, &c.Topic, &c.Kind, &c.Position); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// channelHistory returns the most recent `limit` messages in a channel,
// optionally before a cursor id. Returned in chronological order (oldest first).
func (s *NexusServer) channelHistory(channelID string, beforeID int64, limit int) ([]ChannelMsg, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = s.DB.Query(
			`SELECT id, channel_id, sender, body, CAST(created_at AS TEXT),
			        COALESCE(edited,0), COALESCE(deleted,0), COALESCE(pinned,0)
			   FROM channel_messages
			  WHERE channel_id = ? AND id < ?
			  ORDER BY id DESC LIMIT ?`, channelID, beforeID, limit)
	} else {
		rows, err = s.DB.Query(
			`SELECT id, channel_id, sender, body, CAST(created_at AS TEXT),
			        COALESCE(edited,0), COALESCE(deleted,0), COALESCE(pinned,0)
			   FROM channel_messages
			  WHERE channel_id = ?
			  ORDER BY id DESC LIMIT ?`, channelID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelMsg
	ids := []int64{}
	for rows.Next() {
		var m ChannelMsg
		var ed, del, pin int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Body, &m.CreatedAt, &ed, &del, &pin); err != nil {
			continue
		}
		m.Edited = ed == 1
		m.Deleted = del == 1
		m.Pinned = pin == 1
		if m.Deleted {
			m.Body = ""
		}
		out = append(out, m)
		ids = append(ids, m.ID)
	}
	// Bulk-load reactions for these messages.
	if len(ids) > 0 {
		reactions := s.channelReactionsBulk(ids)
		for i := range out {
			if r, ok := reactions[out[i].ID]; ok {
				out[i].Reactions = r
			}
		}
	}
	// Reverse to chronological.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// channelReactionsBulk loads the reaction map for many message ids in one
// query. Returns msg_id -> emoji -> []users.
func (s *NexusServer) channelReactionsBulk(ids []int64) map[int64]map[string][]string {
	out := map[int64]map[string][]string{}
	if len(ids) == 0 {
		return out
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.Query(`SELECT msg_id, emoji, username FROM channel_reactions WHERE msg_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var emoji, user string
		if err := rows.Scan(&id, &emoji, &user); err != nil {
			continue
		}
		if out[id] == nil {
			out[id] = map[string][]string{}
		}
		out[id][emoji] = append(out[id][emoji], user)
	}
	return out
}

// broadcastChannelMsg fan-outs a new channel message to all currently
// connected members of that server. Plaintext on the wire; clients filter
// by the channel they have open.
func (s *NexusServer) broadcastChannelMsg(serverID string, payload NexusMessage) {
	rows, err := s.DB.Query(`SELECT username FROM server_members WHERE server_id = ?`, serverID)
	if err != nil {
		return
	}
	defer rows.Close()
	var recipients []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil {
			recipients = append(recipients, u)
		}
	}

	var serverName string
	_ = s.DB.QueryRow(`SELECT name FROM servers WHERE id = ?`, serverID).Scan(&serverName)
	if serverName == "" {
		serverName = "Group"
	}
	var channelName string
	if payload.ChannelID != "" {
		_ = s.DB.QueryRow(`SELECT name FROM channels WHERE id = ?`, payload.ChannelID).Scan(&channelName)
	}

	pushTitle := payload.Sender + " in " + serverName
	if channelName != "" {
		pushTitle = payload.Sender + " in " + serverName + " #" + channelName
	}

	s.Mu.RLock()
	defer s.Mu.RUnlock()
	for _, u := range recipients {
		if c, ok := s.Clients[u]; ok {
			c.Send(payload)
		} else if u != payload.Sender {
			go s.sendWebPush(u, pushTitle, payload.Body)
			go s.sendFCMPush(u, pushTitle, payload.Body)
		}
	}
}

var errShortPassword = &strErr{"password must be at least 8 characters"}
var errBadUsername = &strErr{"invalid username (3-32 chars, a-z A-Z 0-9 . _ -)"}
var errBadEmail = &strErr{"invalid email address"}
var errResetInvalid = &strErr{"password reset token invalid or expired"}
var errQRInvalid = &strErr{"qr login token invalid or expired"}

type strErr struct{ msg string }

func (e *strErr) Error() string { return e.msg }

// ---------- Friend Management ----------

func (s *NexusServer) getFriends(username string) []string {
	rows, err := s.DB.Query(`
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END as friend
		FROM friends
		WHERE (user_a = ? OR user_b = ?) AND status = 'accepted'`,
		username, username, username)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var friends []string
	for rows.Next() {
		var f string
		rows.Scan(&f)
		friends = append(friends, f)
	}
	return friends
}

func (s *NexusServer) sendFriendRequest(from, to string) error {
	// Check if already exists in either direction
	var count int
	s.DB.QueryRow("SELECT COUNT(*) FROM friends WHERE (user_a=? AND user_b=?) OR (user_a=? AND user_b=?)",
		from, to, to, from).Scan(&count)
	if count > 0 {
		return nil // Already exists
	}
	_, err := s.DB.Exec("INSERT INTO friends (user_a, user_b, status) VALUES (?, ?, 'pending')", from, to)
	return err
}

func (s *NexusServer) acceptFriendRequest(from, to string) error {
	_, err := s.DB.Exec("UPDATE friends SET status = 'accepted' WHERE user_a = ? AND user_b = ? AND status = 'pending'",
		from, to)
	return err
}

func (s *NexusServer) rejectFriendRequest(from, to string) error {
	_, err := s.DB.Exec("DELETE FROM friends WHERE user_a = ? AND user_b = ? AND status = 'pending'",
		from, to)
	return err
}

// areFriends reports whether two usernames have an accepted friendship
// record in either direction.
func (s *NexusServer) areFriends(a, b string) bool {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM friends
		WHERE status = 'accepted' AND ((user_a = ? AND user_b = ?) OR (user_a = ? AND user_b = ?))`,
		a, b, b, a).Scan(&n)
	return n > 0
}

func (s *NexusServer) removeFriend(a, b string) error {
	_, err := s.DB.Exec(`DELETE FROM friends
		WHERE (user_a = ? AND user_b = ?) OR (user_a = ? AND user_b = ?)`,
		a, b, b, a)
	return err
}

// ---------- Group Chat ----------

func (s *NexusServer) createConversation(id, name, creator string, members []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO conversations (id, name, created_by) VALUES (?, ?, ?)", id, name, creator); err != nil {
		tx.Rollback()
		return err
	}
	seen := map[string]bool{creator: false}
	all := append([]string{creator}, members...)
	for _, m := range all {
		if seen[m] {
			continue
		}
		seen[m] = true
		if _, err := tx.Exec("INSERT OR IGNORE INTO conversation_members (convo_id, username) VALUES (?, ?)", id, m); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *NexusServer) conversationMembers(id string) []string {
	rows, err := s.DB.Query("SELECT username FROM conversation_members WHERE convo_id = ?", id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		out = append(out, u)
	}
	return out
}

func (s *NexusServer) userConversations(username string) []NexusMessage {
	rows, err := s.DB.Query(`SELECT c.id, c.name
		FROM conversations c
		JOIN conversation_members m ON m.convo_id = c.id
		WHERE m.username = ?`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []NexusMessage
	for rows.Next() {
		var m NexusMessage
		rows.Scan(&m.ConvoID, &m.ConvoName)
		m.Members = s.conversationMembers(m.ConvoID)
		out = append(out, m)
	}
	return out
}

func (s *NexusServer) leaveConversation(convoID, username string) error {
	_, err := s.DB.Exec("DELETE FROM conversation_members WHERE convo_id = ? AND username = ?", convoID, username)
	return err
}

func (s *NexusServer) getPendingRequests(username string) []string {
	rows, err := s.DB.Query("SELECT user_a FROM friends WHERE user_b = ? AND status = 'pending'", username)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var pending []string
	for rows.Next() {
		var f string
		rows.Scan(&f)
		pending = append(pending, f)
	}
	return pending
}

// ---------- Offline Messages ----------

// persistDM stores a delivered (or queued) direct message in dm_messages.
// Idempotent on msg_id so retries and offline-redelivery don't duplicate.
func (s *NexusServer) persistDM(msgID, sender, recipient, body string) {
	if msgID == "" || sender == "" || recipient == "" {
		return
	}
	if _, err := s.DB.Exec(
		`INSERT OR IGNORE INTO dm_messages (msg_id, sender, recipient, body) VALUES (?, ?, ?, ?)`,
		msgID, sender, recipient, body); err != nil {
		log.Printf("[dm-persist] %s->%s: %v", sender, recipient, err)
	}
}

func (s *NexusServer) editDM(msgID, sender, newBody string) {
	if _, err := s.DB.Exec(
		`UPDATE dm_messages SET body = ?, edited = 1 WHERE msg_id = ? AND sender = ? AND deleted = 0`,
		newBody, msgID, sender); err != nil {
		log.Printf("[dm-edit] %s/%s: %v", sender, msgID, err)
	}
}

func (s *NexusServer) deleteDM(msgID, sender string) {
	if _, err := s.DB.Exec(
		`UPDATE dm_messages SET body = '', deleted = 1 WHERE msg_id = ? AND sender = ?`,
		msgID, sender); err != nil {
		log.Printf("[dm-delete] %s/%s: %v", sender, msgID, err)
	}
}

// toggleReaction inserts or removes a (msg_id, user, emoji) row. Returns
// true if the reaction was added, false if it was removed. The caller
// forwards the same toggle event to the peer so both sides converge.
func (s *NexusServer) toggleReaction(msgID, user, emoji string) bool {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM dm_reactions WHERE msg_id = ? AND username = ? AND emoji = ?`,
		msgID, user, emoji).Scan(&n)
	if n > 0 {
		s.DB.Exec(`DELETE FROM dm_reactions WHERE msg_id = ? AND username = ? AND emoji = ?`, msgID, user, emoji)
		return false
	}
	s.DB.Exec(`INSERT OR IGNORE INTO dm_reactions (msg_id, username, emoji) VALUES (?, ?, ?)`, msgID, user, emoji)
	return true
}

// fetchDMHistory returns up to limit messages between the two parties in
// chronological order (oldest first). If beforeID > 0, only rows with
// id < beforeID are returned — for paginating older history.
func (s *NexusServer) fetchDMHistory(a, b string, beforeID int64, limit int) []DMMessage {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT id, msg_id, sender, recipient, body, edited, deleted, created_at
	      FROM dm_messages
	      WHERE ((sender = ? AND recipient = ?) OR (sender = ? AND recipient = ?))`
	args := []any{a, b, b, a}
	if beforeID > 0 {
		q += ` AND id < ?`
		args = append(args, beforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.Query(q, args...)
	if err != nil {
		log.Printf("[dm-history] query: %v", err)
		return nil
	}
	defer rows.Close()

	var out []DMMessage
	var ids []string
	for rows.Next() {
		var id int64
		var ed, del int
		var m DMMessage
		if err := rows.Scan(&id, &m.MsgID, &m.Sender, &m.Recipient, &m.Body, &ed, &del, &m.CreatedAt); err != nil {
			continue
		}
		m.Edited = ed == 1
		m.Deleted = del == 1
		if m.Deleted {
			m.Body = ""
		}
		out = append(out, m)
		ids = append(ids, m.MsgID)
	}
	// Reverse to chronological order (oldest first).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}

	if len(ids) > 0 {
		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1]
		ra := make([]any, 0, len(ids))
		for _, id := range ids {
			ra = append(ra, id)
		}
		rxRows, err := s.DB.Query(
			`SELECT msg_id, username, emoji FROM dm_reactions WHERE msg_id IN (`+placeholders+`)`, ra...)
		if err == nil {
			defer rxRows.Close()
			byID := map[string]map[string][]string{}
			for rxRows.Next() {
				var mid, user, emoji string
				if rxRows.Scan(&mid, &user, &emoji) != nil {
					continue
				}
				if byID[mid] == nil {
					byID[mid] = map[string][]string{}
				}
				byID[mid][emoji] = append(byID[mid][emoji], user)
			}
			for i := range out {
				if r, ok := byID[out[i].MsgID]; ok {
					out[i].Reactions = r
				}
			}
		}
	}
	return out
}

func (s *NexusServer) storeOfflineMessage(sender, recipient, body, msgType string) {
	if _, err := s.DB.Exec("INSERT INTO offline_messages (sender, recipient, body, msg_type) VALUES (?, ?, ?, ?)",
		sender, recipient, body, msgType); err != nil {
		log.Printf("[offline] store %s->%s (%s) failed: %v", sender, recipient, msgType, err)
	}
	go s.sendWebPush(recipient, sender, body)
	go s.sendFCMPush(recipient, sender, body)
}

func (s *NexusServer) sendWebPush(recipient, sender, preview string) {
	privKey := os.Getenv("VAPID_PRIVATE_KEY")
	pubKey := os.Getenv("VAPID_PUBLIC_KEY")
	if privKey == "" || pubKey == "" {
		return
	}
	rows, err := s.DB.Query("SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE username = ?", recipient)
	if err != nil {
		return
	}
	defer rows.Close()
	msg := preview
	if len(msg) > 80 {
		msg = msg[:80] + "…"
	}
	payload, _ := json.Marshal(map[string]string{
		"title": sender,
		"body":  msg,
	})
	for rows.Next() {
		var endpoint, p256dh, auth string
		if err := rows.Scan(&endpoint, &p256dh, &auth); err != nil {
			continue
		}
		sub := &webpush.Subscription{
			Endpoint: endpoint,
			Keys: webpush.Keys{
				P256dh: p256dh,
				Auth:   auth,
			},
		}
		resp, err := webpush.SendNotification(payload, sub, &webpush.Options{
			VAPIDPublicKey:  pubKey,
			VAPIDPrivateKey: privKey,
			Subscriber:      "noreply@phazechat.world",
			TTL:             86400,
		})
		if err != nil {
			log.Printf("[push] send to %s: %v", recipient, err)
		} else {
			resp.Body.Close()
		}
	}
}

func (s *NexusServer) listSessions(username string) []map[string]string {
	rows, err := s.DB.Query(`SELECT token, device_info, created_at FROM session_tokens
		WHERE username = ? AND revoked = 0 AND expires_at > datetime('now') ORDER BY created_at DESC`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var tok, dev, created string
		rows.Scan(&tok, &dev, &created)
		out = append(out, map[string]string{
			"token":      tok[:8] + "…",
			"full_token": tok,
			"device":     dev,
			"created_at": created,
		})
	}
	return out
}

func (s *NexusServer) exportUserData(username string) map[string]interface{} {
	out := map[string]interface{}{"username": username}
	// Profile
	var email, mood, displayName string
	s.DB.QueryRow("SELECT email, mood, display_name FROM users WHERE username = ?", username).Scan(&email, &mood, &displayName)
	out["email"] = email
	out["mood"] = mood
	out["display_name"] = displayName
	// Friends
	rows, _ := s.DB.Query("SELECT user_b, status FROM friends WHERE user_a = ? AND status = 'accepted'", username)
	var friends []string
	if rows != nil {
		for rows.Next() {
			var u, st string
			rows.Scan(&u, &st)
			friends = append(friends, u)
		}
		rows.Close()
	}
	out["friends"] = friends
	// Messages (offline store only — live messages are E2EE so server never sees plaintext)
	msgRows, _ := s.DB.Query("SELECT sender, recipient, body, created_at FROM offline_messages WHERE sender = ? OR recipient = ? ORDER BY created_at ASC", username, username)
	var msgs []map[string]string
	if msgRows != nil {
		for msgRows.Next() {
			var sender, recipient, body, createdAt string
			msgRows.Scan(&sender, &recipient, &body, &createdAt)
			msgs = append(msgs, map[string]string{"from": sender, "to": recipient, "body": body, "at": createdAt})
		}
		msgRows.Close()
	}
	out["messages"] = msgs
	out["exported_at"] = time.Now().UTC().Format(time.RFC3339)
	out["note"] = "Live E2EE messages are not stored server-side. This export contains only metadata and queued offline messages."
	return out
}

func (s *NexusServer) deliverOfflineMessages(username string) {
	s.Mu.RLock()
	client, online := s.Clients[username]
	s.Mu.RUnlock()
	if !online {
		return
	}

	rows, err := s.DB.Query("SELECT id, sender, body, msg_type, created_at FROM offline_messages WHERE recipient = ? ORDER BY created_at ASC", username)
	if err != nil {
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		var sender, body, msgType, createdAt string
		rows.Scan(&id, &sender, &body, &msgType, &createdAt)
		client.Send(NexusMessage{
			Type:   msgType,
			Sender: sender,
			Body:   body,
			Status: "offline:" + createdAt,
		})
		ids = append(ids, id)
	}

	// Delete delivered messages
	for _, id := range ids {
		s.DB.Exec("DELETE FROM offline_messages WHERE id = ?", id)
	}
	if len(ids) > 0 {
		log.Printf("Delivered %d offline messages to %s", len(ids), username)
	}
}

// ---------- Presence ----------

func (s *NexusServer) broadcastPresence(username, status string) {
	friends := s.getFriends(username)
	supporter := s.isSupporter(username)
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	for _, friend := range friends {
		if client, ok := s.Clients[friend]; ok {
			client.Send(NexusMessage{
				Type:      "presence",
				Sender:    username,
				Status:    status,
				Supporter: supporter,
			})
		}
	}
}

// isSupporter reports whether the user holds the supporter badge.
func (s *NexusServer) isSupporter(username string) bool {
	var n int
	err := s.DB.QueryRow("SELECT COALESCE(supporter, 0) FROM users WHERE username = ?", username).Scan(&n)
	return err == nil && n == 1
}

// ---------- Trust & Safety: blocks + abuse reports ----------

// isBlocked reports whether `blocker` has blocked `blocked`. Either direction
// being blocked should suppress message delivery (checked at the call site).
func (s *NexusServer) isBlocked(blocker, blocked string) bool {
	var n int
	err := s.DB.QueryRow(
		"SELECT 1 FROM blocks WHERE blocker = ? AND blocked = ? LIMIT 1",
		blocker, blocked).Scan(&n)
	return err == nil
}

func (s *NexusServer) blockUser(blocker, blocked string) error {
	if blocker == "" || blocked == "" || blocker == blocked {
		return fmt.Errorf("invalid block")
	}
	_, err := s.DB.Exec(
		"INSERT OR IGNORE INTO blocks (blocker, blocked) VALUES (?, ?)",
		blocker, blocked)
	return err
}

func (s *NexusServer) unblockUser(blocker, blocked string) error {
	_, err := s.DB.Exec(
		"DELETE FROM blocks WHERE blocker = ? AND blocked = ?",
		blocker, blocked)
	return err
}

func (s *NexusServer) listBlocks(blocker string) []string {
	rows, err := s.DB.Query(
		"SELECT blocked FROM blocks WHERE blocker = ? ORDER BY created_at DESC",
		blocker)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			out = append(out, u)
		}
	}
	return out
}

func (s *NexusServer) recordAbuseReport(reporter, subject, reason, body string) error {
	if reporter == "" || subject == "" || reason == "" {
		return fmt.Errorf("missing fields")
	}
	_, err := s.DB.Exec(
		"INSERT INTO abuse_reports (reporter, subject, reason, body) VALUES (?, ?, ?, ?)",
		reporter, subject, reason, body)
	return err
}

// ---------- Search ----------

func (s *NexusServer) searchUsers(query, excludeUser string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	// Search registered users by username substring
	rows, err := s.DB.Query("SELECT username FROM users WHERE LOWER(username) LIKE ? AND username != ? LIMIT 20",
		"%"+query+"%", excludeUser)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		results = append(results, name)
	}
	return results
}

// ---------- WebSocket Handler ----------

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
			if !s.verifyTOTP(msg.Sender, msg.TOTPCode) {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "2FA code required or invalid", Status: "totp_required"})
				continue
			}
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
			secret, _ := s.totpStatus(username)
			if secret == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "No pending TOTP enrollment"})
				continue
			}
			if !s.enableTOTP(username, secret, msg.TOTPCode) {
				client.Send(NexusMessage{Type: "totp_result", Error: "Invalid code"})
				continue
			}
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
			log.Printf("Signal [%s] from %s to %s", msg.Type, msg.Sender, msg.Recipient)
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			} else {
				client.Send(NexusMessage{
					Type:  "call_error",
					Body:  "User is offline",
					Error: msg.Recipient + " is not available",
				})
			}
			s.Mu.RUnlock()

		case "call_reject", "call_end":
			if username == "" {
				continue
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			}
			s.Mu.RUnlock()

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

		case "server_join":
			if username == "" {
				continue
			}
			code := strings.TrimSpace(msg.InviteCode)
			if code == "" {
				client.Send(NexusMessage{Type: "server_join_result", Error: "invite_code required"})
				continue
			}
			var serverID, serverName string
			err := s.DB.QueryRow(
				`SELECT id, name FROM servers WHERE invite_code = ?`, code).Scan(&serverID, &serverName)
			if err != nil {
				client.Send(NexusMessage{Type: "server_join_result", Error: "invite invalid"})
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
			// Owner or any staff (helper+) can delete.
			if sender != username && !s.roleAtLeast(username, "helper") {
				continue
			}
			if _, err := s.DB.Exec(`UPDATE channel_messages SET deleted=1, body='' WHERE id=?`, id64); err != nil {
				continue
			}
			var serverID string
			s.DB.QueryRow(`SELECT server_id FROM channels WHERE id=?`, channelID).Scan(&serverID)
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

// ---------- Livestreams ----------

type liveStream struct {
	Title   string
	Viewers map[string]struct{}
}

func (s *NexusServer) streamStart(host, title string) {
	s.VoiceRoomsMu.Lock()
	defer s.VoiceRoomsMu.Unlock()
	if s.Streams == nil {
		s.Streams = make(map[string]*liveStream)
	}
	s.Streams[host] = &liveStream{Title: title, Viewers: make(map[string]struct{})}
}

func (s *NexusServer) streamStop(host string) {
	s.VoiceRoomsMu.Lock()
	st := s.Streams[host]
	delete(s.Streams, host)
	s.VoiceRoomsMu.Unlock()
	if st == nil {
		return
	}
	// Tell every active viewer the stream ended.
	for v := range st.Viewers {
		s.Mu.RLock()
		c, ok := s.Clients[v]
		s.Mu.RUnlock()
		if ok {
			c.Send(NexusMessage{Type: "stream_ended", Sender: host})
		}
	}
}

func (s *NexusServer) streamHas(host string) bool {
	s.VoiceRoomsMu.RLock()
	defer s.VoiceRoomsMu.RUnlock()
	_, ok := s.Streams[host]
	return ok
}

func (s *NexusServer) streamAddViewer(host, viewer string) {
	s.VoiceRoomsMu.Lock()
	if st, ok := s.Streams[host]; ok {
		st.Viewers[viewer] = struct{}{}
	}
	s.VoiceRoomsMu.Unlock()
}

func (s *NexusServer) streamRemoveViewer(host, viewer string) {
	s.VoiceRoomsMu.Lock()
	if st, ok := s.Streams[host]; ok {
		delete(st.Viewers, viewer)
	}
	s.VoiceRoomsMu.Unlock()
}

// streamList returns alternating host|title pairs for the wire (kept as a flat
// string slice to fit the existing NexusMessage.Results field).
func (s *NexusServer) streamList() []string {
	s.VoiceRoomsMu.RLock()
	defer s.VoiceRoomsMu.RUnlock()
	out := make([]string, 0, len(s.Streams)*2)
	for host, st := range s.Streams {
		out = append(out, host, st.Title)
	}
	return out
}

// streamBroadcastList pushes the live list to every connected client so a
// "Live now" banner can appear in real time.
func (s *NexusServer) streamBroadcastList() {
	list := s.streamList()
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	for _, c := range s.Clients {
		c.Send(NexusMessage{Type: "stream_list_result", Status: "ok", Results: list})
	}
}

// streamEvictUser called on disconnect: end the user's own stream and remove
// them as a viewer from anyone else's stream.
func (s *NexusServer) streamEvictUser(username string) {
	s.VoiceRoomsMu.Lock()
	notifyHosts := make([]string, 0)
	endedAsHost := false
	if _, isHost := s.Streams[username]; isHost {
		delete(s.Streams, username)
		endedAsHost = true
	}
	for host, st := range s.Streams {
		if _, in := st.Viewers[username]; in {
			delete(st.Viewers, username)
			notifyHosts = append(notifyHosts, host)
		}
	}
	s.VoiceRoomsMu.Unlock()
	for _, h := range notifyHosts {
		s.Mu.RLock()
		c, ok := s.Clients[h]
		s.Mu.RUnlock()
		if ok {
			c.Send(NexusMessage{Type: "stream_viewer_leave", Sender: username, Recipient: h})
		}
	}
	if endedAsHost {
		s.streamBroadcastList()
	}
}

// ---------- Global hub ----------

// globalSpaceID is the fixed identifier for the Phaze-wide "Hub" space that
// every user is automatically a member of. Lets new signups land in a chat
// where the whole community is reachable without needing an invite code.
const globalSpaceID = "global"

// ensureGlobalSpace creates the hub space + its default channels if they don't
// already exist. Safe to call on every boot — idempotent INSERT OR IGNORE.
func (s *NexusServer) ensureGlobalSpace() {
	if _, err := s.DB.Exec(
		`INSERT OR IGNORE INTO servers (id, name, description, owner, visibility, invite_code)
		 VALUES (?, ?, ?, ?, 'public', ?)`,
		globalSpaceID, "Phaze Hub", "The Phaze-wide chat. Everyone is welcome.", "phaze", "phaze",
	); err != nil {
		log.Printf("[hub] ensure space: %v", err)
		return
	}
	defaults := []struct {
		id, name, topic string
		pos             int
	}{
		{"global-general", "general", "Say hi.", 0},
		{"global-lobby", "lobby", "Casual chat.", 1},
		{"global-announcements", "announcements", "Phaze news.", 2},
	}
	for _, c := range defaults {
		if _, err := s.DB.Exec(
			`INSERT OR IGNORE INTO channels (id, server_id, name, topic, kind, position) VALUES (?, ?, ?, ?, 'text', ?)`,
			c.id, globalSpaceID, c.name, c.topic, c.pos,
		); err != nil {
			log.Printf("[hub] ensure channel %s: %v", c.name, err)
		}
	}
	// Backfill membership for every existing verified user so they don't
	// have to sign out/in to see the Hub on first deploy.
	if _, err := s.DB.Exec(
		`INSERT OR IGNORE INTO server_members (server_id, username, role)
		 SELECT ?, username, 'member' FROM users WHERE is_verified = 1`,
		globalSpaceID,
	); err != nil {
		log.Printf("[hub] backfill memberships: %v", err)
	}
}

// autoJoinGlobalSpace makes the user a member of the global hub. Idempotent.
// Called on successful register and on every auth so existing users from
// before the hub existed get auto-enrolled the next time they sign in.
func (s *NexusServer) autoJoinGlobalSpace(username string) {
	if username == "" {
		return
	}
	if _, err := s.DB.Exec(
		`INSERT OR IGNORE INTO server_members (server_id, username, role) VALUES (?, ?, 'member')`,
		globalSpaceID, username,
	); err != nil {
		log.Printf("[hub] auto-join %s: %v", username, err)
	}
}

// autoJoinPublicSpaces adds a bot user to every public space. Called on boot.
func (s *NexusServer) autoJoinPublicSpaces(botUsername string) {
	rows, err := s.DB.Query(`SELECT id FROM servers WHERE visibility = 'public'`)
	if err != nil {
		log.Printf("[bot] public spaces query: %v", err)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		rows.Scan(&id)
		s.DB.Exec(`INSERT OR IGNORE INTO server_members (server_id, username, role) VALUES (?, ?, 'member')`, id, botUsername)
		count++
	}
	log.Printf("[bot] %s joined %d public spaces", botUsername, count)
}

// ---------- Voice rooms ----------

func (s *NexusServer) voiceRoomJoin(channelID, username string) {
	s.VoiceRoomsMu.Lock()
	room, ok := s.VoiceRooms[channelID]
	if !ok {
		room = make(map[string]struct{})
		s.VoiceRooms[channelID] = room
	}
	room[username] = struct{}{}
	s.VoiceRoomsMu.Unlock()
}

func (s *NexusServer) voiceRoomLeave(channelID, username string) {
	s.VoiceRoomsMu.Lock()
	if room, ok := s.VoiceRooms[channelID]; ok {
		delete(room, username)
		if len(room) == 0 {
			delete(s.VoiceRooms, channelID)
		}
	}
	s.VoiceRoomsMu.Unlock()
}

func (s *NexusServer) voiceRoomHas(channelID, username string) bool {
	s.VoiceRoomsMu.RLock()
	defer s.VoiceRoomsMu.RUnlock()
	if room, ok := s.VoiceRooms[channelID]; ok {
		_, in := room[username]
		return in
	}
	return false
}

// voiceRoomPeers returns the current member list of a voice channel.
func (s *NexusServer) voiceRoomPeers(channelID string) []string {
	s.VoiceRoomsMu.RLock()
	defer s.VoiceRoomsMu.RUnlock()
	room := s.VoiceRooms[channelID]
	peers := make([]string, 0, len(room))
	for u := range room {
		peers = append(peers, u)
	}
	return peers
}

// voiceRoomBroadcastPeers pushes the current peer list to every connected
// member of the voice room. Each member gets the same list (including
// themselves) so the client can diff and (dis)connect peer connections.
func (s *NexusServer) voiceRoomBroadcastPeers(channelID string) {
	peers := s.voiceRoomPeers(channelID)
	for _, u := range peers {
		s.Mu.RLock()
		c, ok := s.Clients[u]
		s.Mu.RUnlock()
		if ok {
			c.Send(NexusMessage{
				Type:      "voice_peers",
				Status:    "ok",
				ChannelID: channelID,
				Results:   peers,
			})
		}
	}
}

// voiceRoomEvictUser removes a user from all voice rooms (called on disconnect)
// and broadcasts updated peer lists.
func (s *NexusServer) voiceRoomEvictUser(username string) {
	s.VoiceRoomsMu.Lock()
	affected := make([]string, 0)
	for cid, room := range s.VoiceRooms {
		if _, in := room[username]; in {
			delete(room, username)
			affected = append(affected, cid)
			if len(room) == 0 {
				delete(s.VoiceRooms, cid)
			}
		}
	}
	s.VoiceRoomsMu.Unlock()
	for _, cid := range affected {
		s.voiceRoomBroadcastPeers(cid)
	}
}

// isServerMember reports whether a user is a member of a given server.
func (s *NexusServer) isServerMember(serverID, username string) bool {
	var n int
	err := s.DB.QueryRow(`SELECT 1 FROM server_members WHERE server_id = ? AND username = ?`, serverID, username).Scan(&n)
	return err == nil
}

// ---------- Health Check ----------

func (s *NexusServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbOK := s.DB.PingContext(ctx) == nil

	v := os.Getenv("Phaze_LATEST_VERSION")
	if v == "" {
		v = "1.0.0-Phaze"
	}

	turnOK := TurnSecret != "" && TurnURL != ""
	turnFallback := !turnOK // we still serve openrelay public TURN as fallback
	resendOK := os.Getenv("RESEND_API_KEY") != ""
	brevoOK := os.Getenv("BREVO_API_KEY") != ""
	smtpOK := resendOK || brevoOK || (os.Getenv("SMTP_HOST") != "" && os.Getenv("SMTP_USER") != "")
	pushOK := os.Getenv("VAPID_PUBLIC_KEY") != "" && os.Getenv("VAPID_PRIVATE_KEY") != ""
	fcmOK := s.fcmClient != nil
	sentryOK := os.Getenv("SENTRY_DSN") != ""
	litestreamOK := os.Getenv("LITESTREAM_BUCKET") != "" || os.Getenv("BUCKET_NAME") != ""

	s.Mu.RLock()
	clients := len(s.Clients)
	s.Mu.RUnlock()

	status := "ok"
	code := http.StatusOK
	if !dbOK {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                status,
		"server":                "phaze-nexus",
		"version":               v,
		"database_ok":           dbOK,
		"turn_configured":       turnOK,
		"turn_public_fallback":  turnFallback, // openrelay.metered.ca in use
		"smtp_configured":       smtpOK,
		"brevo_configured":      brevoOK,
		"resend_configured":     resendOK,
		"webpush_configured":    pushOK,
		"fcm_configured":        fcmOK,
		"sentry_configured":     sentryOK,
		"litestream_configured": litestreamOK,
		"connected_clients":     clients,
	})
}

// ---------- Metrics (Prometheus text format) ----------

var metricsStart = time.Now()

type nexusMetrics struct {
	wsConnections     atomic.Uint64
	wsConnectionsFail atomic.Uint64
	wsMessagesIn      atomic.Uint64
	authSuccess       atomic.Uint64
	authFailure       atomic.Uint64
	keyRequests       atomic.Uint64
	convoMessages     atomic.Uint64
	pstnAttempts      atomic.Uint64
	pstnRejected      atomic.Uint64
}

var metrics = &nexusMetrics{}

func (s *NexusServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if tok := strings.TrimSpace(os.Getenv("PHAZE_METRICS_TOKEN")); tok != "" {
		got := r.Header.Get("Authorization")
		if got != "Bearer "+tok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	s.Mu.RLock()
	activeClients := len(s.Clients)
	s.Mu.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	var pending int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM offline_messages`).Scan(&pending)

	var users int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP nexus_uptime_seconds Seconds since server start\n")
	fmt.Fprintf(w, "# TYPE nexus_uptime_seconds counter\n")
	fmt.Fprintf(w, "nexus_uptime_seconds %.0f\n", time.Since(metricsStart).Seconds())
	fmt.Fprintf(w, "# HELP nexus_active_clients Connected authenticated WebSocket clients\n")
	fmt.Fprintf(w, "# TYPE nexus_active_clients gauge\n")
	fmt.Fprintf(w, "nexus_active_clients %d\n", activeClients)
	fmt.Fprintf(w, "# HELP nexus_registered_users Total user accounts\n")
	fmt.Fprintf(w, "# TYPE nexus_registered_users gauge\n")
	fmt.Fprintf(w, "nexus_registered_users %d\n", users)
	fmt.Fprintf(w, "# HELP nexus_offline_messages_pending Queued messages awaiting recipient login\n")
	fmt.Fprintf(w, "# TYPE nexus_offline_messages_pending gauge\n")
	fmt.Fprintf(w, "nexus_offline_messages_pending %d\n", pending)
	fmt.Fprintf(w, "# HELP nexus_ws_connections_total WebSocket upgrade attempts\n")
	fmt.Fprintf(w, "# TYPE nexus_ws_connections_total counter\n")
	fmt.Fprintf(w, "nexus_ws_connections_total %d\n", metrics.wsConnections.Load())
	fmt.Fprintf(w, "# HELP nexus_ws_connections_failed_total WebSocket upgrades that failed\n")
	fmt.Fprintf(w, "# TYPE nexus_ws_connections_failed_total counter\n")
	fmt.Fprintf(w, "nexus_ws_connections_failed_total %d\n", metrics.wsConnectionsFail.Load())
	fmt.Fprintf(w, "# HELP nexus_ws_messages_in_total Inbound WebSocket messages\n")
	fmt.Fprintf(w, "# TYPE nexus_ws_messages_in_total counter\n")
	fmt.Fprintf(w, "nexus_ws_messages_in_total %d\n", metrics.wsMessagesIn.Load())
	fmt.Fprintf(w, "# HELP nexus_auth_total Auth attempts by result\n")
	fmt.Fprintf(w, "# TYPE nexus_auth_total counter\n")
	fmt.Fprintf(w, "nexus_auth_total{result=\"ok\"} %d\n", metrics.authSuccess.Load())
	fmt.Fprintf(w, "nexus_auth_total{result=\"fail\"} %d\n", metrics.authFailure.Load())
	fmt.Fprintf(w, "# HELP nexus_key_requests_total Pairwise key_request relays\n")
	fmt.Fprintf(w, "# TYPE nexus_key_requests_total counter\n")
	fmt.Fprintf(w, "nexus_key_requests_total %d\n", metrics.keyRequests.Load())
	fmt.Fprintf(w, "# HELP nexus_convo_messages_total Group envelope messages relayed\n")
	fmt.Fprintf(w, "# TYPE nexus_convo_messages_total counter\n")
	fmt.Fprintf(w, "nexus_convo_messages_total %d\n", metrics.convoMessages.Load())
	fmt.Fprintf(w, "# HELP nexus_pstn_total PSTN attempts and rejections\n")
	fmt.Fprintf(w, "# TYPE nexus_pstn_total counter\n")
	fmt.Fprintf(w, "nexus_pstn_total{result=\"attempt\"} %d\n", metrics.pstnAttempts.Load())
	fmt.Fprintf(w, "nexus_pstn_total{result=\"rejected_disabled\"} %d\n", metrics.pstnRejected.Load())
	fmt.Fprintf(w, "# HELP go_memstats_alloc_bytes Currently allocated heap bytes\n")
	fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_alloc_bytes %d\n", memStats.Alloc)
	fmt.Fprintf(w, "# HELP go_goroutines Currently running goroutines\n")
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())
}

const rootHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Phaze — Stay in phase.</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
	:root { --skype-blue: #00AFF0; --skype-dark: #0078D4; }
	body { margin: 0; font-family: 'Segoe UI', Tahoma, sans-serif; background: #fff; color: #333; }
	.hero { background: linear-gradient(135deg, var(--skype-blue), var(--skype-dark)); color: #fff; text-align: center; padding: 100px 20px; }
	.hero h1 { font-size: 3rem; font-weight: 300; margin: 0 0 20px; }
	.hero h1 strong { font-weight: 700; }
	.hero p { font-size: 1.25rem; opacity: 0.9; max-width: 600px; margin: 0 auto 40px; }
	.btn { display: inline-block; background: #fff; color: var(--skype-blue); text-decoration: none; padding: 16px 40px; border-radius: 4px; font-weight: 700; font-size: 1.1rem; box-shadow: 0 4px 15px rgba(0,0,0,0.1); transition: 0.2s; }
	.btn:hover { transform: translateY(-2px); box-shadow: 0 6px 20px rgba(0,0,0,0.2); }
	.navbar { background: #fff; border-bottom: 1px solid #eee; padding: 15px 40px; display: flex; align-items: center; justify-content: space-between; }
	.nav-brand { font-weight: 700; font-size: 1.5rem; color: var(--skype-blue); display: flex; align-items: center; gap: 10px; }
	.nav-links { display: flex; gap: 30px; list-style: none; margin: 0; padding: 0; }
	.nav-links a { text-decoration: none; color: #333; font-size: 0.9rem; font-weight: 600; }
	.nav-links a:hover { color: var(--skype-blue); }
	.container { max-width: 1140px; margin: 0 auto; }
</style></head><body>
	<nav class="navbar">
		<div class="nav-brand"><svg width="32" height="32" viewBox="0 0 32 32"><circle cx="16" cy="16" r="15" fill="#00AFF0"/><text x="16" y="22" text-anchor="middle" fill="white" font-size="16" font-weight="700" font-family="sans-serif">P</text></svg> Phaze</div>
		<ul class="nav-links">
			<li><a href="/features">Features</a></li>
			<li><a href="/download">Download</a></li>
			<li><a href="/rates">Rates</a></li>
		</ul>
	</nav>
	<div class="stats">
		<div class="stat-item">
			<span class="stat-val" id="node-count">...</span>
			<span class="stat-label">Online Now</span>
		</div>
		<div class="stat-item">
			<span class="stat-val" id="member-count">...</span>
			<span class="stat-label">Sovereign Members</span>
		</div>
		<div class="stat-item">
			<span class="stat-val">E2EE</span>
			<span class="stat-label">Encrypted</span>
		</div>
	</div>
	<section class="hero">
		<div class="container">
			<h1>Stay in touch with the people who <strong>matter most</strong></h1>
			<p>Sovereign chat Phaze keeps the world talking. Free video calls, voice calls and instant messaging on any device. calls. End-to-end encrypted. Yours, not theirs.</p>
			<a href="/download" class="btn">Download Phaze</a>
		</div>
	</section>
</body></html>`

func (s *NexusServer) landingHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Try to serve the high-fidelity template first
	_, err := os.Stat("templates/landing.html")
	if err == nil {
		http.ServeFile(w, r, "templates/landing.html")
		return
	}
	// Fallback to beautiful inline HTML
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(rootHTML))
}

func (s *NexusServer) downloadHandler(w http.ResponseWriter, r *http.Request) {
	// Custom handling for binaries to ensure correct MIME types
	if strings.HasSuffix(r.URL.Path, ".apk") {
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	}
	http.ServeFile(w, r, "templates/download.html")
}

func (s *NexusServer) fileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	// M6: use filepath.Base to prevent path traversal ("../../../etc/passwd").
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/downloads/"))
	if name == "" || name == "." || name == "/" {
		http.NotFound(w, r)
		return
	}

	// Force octet-stream + attachment for every binary so mobile browsers
	// (Samsung Internet, Chrome Android) save to Downloads instead of
	// handing off to the package installer or a file viewer.
	if strings.HasSuffix(name, ".apk") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"Phaze.apk\"")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasSuffix(name, ".exe") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"Phaze.exe\"")
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasSuffix(name, ".linux") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"Phaze.linux\"")
		w.Header().Set("Cache-Control", "no-store")
	}

	http.ServeFile(w, r, filepath.Join("public", "downloads", name))
}

func (s *NexusServer) featuresHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/features.html")
}

func (s *NexusServer) ratesHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/rates.html")
}

func (s *NexusServer) aboutHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/about.html")
}

func (s *NexusServer) supportHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/support.html")
}

func (s *NexusServer) privacyHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/privacy.html")
}

func (s *NexusServer) termsHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/terms.html")
}

func (s *NexusServer) legalHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/legal.html")
}

func (s *NexusServer) resetHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		token = r.FormValue("token")
		pw := r.FormValue("password")
		if err := s.consumePasswordReset(token, pw); err != nil {
			fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>Reset failed</title><body style="font-family:system-ui;max-width:520px;margin:80px auto;padding:20px"><h1>Reset failed</h1><p>%s</p><p><a href="/">Back to Phaze</a></p>`, err.Error())
			return
		}
		fmt.Fprint(w, `<!doctype html><meta charset=utf-8><title>Password reset</title><body style="font-family:system-ui;max-width:520px;margin:80px auto;padding:20px"><h1>Password updated</h1><p>You can now log in with your new password.</p><p><a href="/">Back to Phaze</a></p>`)
		return
	}
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset=utf-8><title>Reset Phaze password</title><style>body{font-family:system-ui;max-width:520px;margin:80px auto;padding:20px}input{width:100%%;padding:10px;margin:8px 0;font-size:1rem}button{background:#00AFF0;color:#fff;border:0;padding:12px 24px;border-radius:6px;font-size:1rem;cursor:pointer}</style></head><body><h1>Reset your Phaze password</h1><form method="POST"><input type="hidden" name="token" value="%s"><label>New password (min. 8 chars)<input type="password" name="password" minlength="8" required></label><button type="submit">Set new password</button></form></body></html>`, token)
}

// ---------- Admin moderation API ----------

// adminFromRequest authenticates an admin caller. Expects phaze_admin_token cookie
// or Authorization: Bearer <session_token>. Returns "" + status code on failure
// (already written by the helper). The session_token is the standard one issued
// by /auth — there is no separate "admin token".
func (s *NexusServer) adminFromRequest(w http.ResponseWriter, r *http.Request) string {
	var tok string
	if cookie, err := r.Cookie("phaze_admin_token"); err == nil {
		tok = cookie.Value
	}
	if tok == "" {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if tok == "" {
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return ""
	}
	u := s.sessionUsername(tok)
	if u == "" {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return ""
	}
	if !s.userIsAdmin(u) {
		http.Error(w, "admin access required", http.StatusForbidden)
		return ""
	}
	return u
}

// modFromRequest authenticates a caller and requires at least the given
// role (one of "helper", "moderator", "admin", "super_admin"). Returns
// the username + role, or "" on rejection (already responded).
func (s *NexusServer) modFromRequest(w http.ResponseWriter, r *http.Request, minRole string) (string, string) {
	var tok string
	if cookie, err := r.Cookie("phaze_admin_token"); err == nil {
		tok = cookie.Value
	}
	if tok == "" {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if tok == "" {
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return "", ""
	}
	u := s.sessionUsername(tok)
	if u == "" {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return "", ""
	}
	role := s.userRole(u)
	if roleRank(role) < roleRank(minRole) {
		http.Error(w, "insufficient role (need at least "+minRole+")", http.StatusForbidden)
		return "", ""
	}
	return u, role
}

// AdminReport is one row of the abuse_reports table for the listing endpoint.
type AdminReport struct {
	ID         int64  `json:"id"`
	Reporter   string `json:"reporter"`
	Subject    string `json:"subject"`
	Reason     string `json:"reason"`
	Body       string `json:"body"`
	Status     string `json:"status"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	ResolvedAt string `json:"resolved_at,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// adminPendingVerificationsHandler returns unverified accounts + their
// verification codes so support can rescue users when SMTP is broken.
// Admin auth required. Returns recent unverified rows (last 24h).
func (s *NexusServer) adminPendingVerificationsHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	rows, err := s.DB.Query(`SELECT username, email, verification_code, created_at
		FROM users WHERE is_verified = 0 AND created_at > datetime('now','-24 hours')
		ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var u, e, c, t string
		if err := rows.Scan(&u, &e, &c, &t); err == nil {
			out = append(out, map[string]string{"username": u, "email": e, "code": c, "created_at": t})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *NexusServer) adminReportsHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	rows, err := s.DB.Query(
		`SELECT id, reporter, subject, reason, COALESCE(body, ''), COALESCE(status, 'pending'),
		         COALESCE(resolved_by, ''), COALESCE(CAST(resolved_at AS TEXT), ''), CAST(created_at AS TEXT)
		   FROM abuse_reports
		  WHERE COALESCE(status, 'pending') = ?
		  ORDER BY id DESC LIMIT 500`, status)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []AdminReport{}
	for rows.Next() {
		var rep AdminReport
		if err := rows.Scan(&rep.ID, &rep.Reporter, &rep.Subject, &rep.Reason, &rep.Body,
			&rep.Status, &rep.ResolvedBy, &rep.ResolvedAt, &rep.CreatedAt); err != nil {
			continue
		}
		out = append(out, rep)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *NexusServer) adminResolveReportHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/v1/admin/reports/{id}/resolve
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/reports/"), "/")
	if len(parts) < 2 || parts[1] != "resolve" {
		http.Error(w, "expected /api/v1/admin/reports/{id}/resolve", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "bad report id", http.StatusBadRequest)
		return
	}
	res, err := s.DB.Exec(
		`UPDATE abuse_reports SET status = 'resolved', resolved_by = ?, resolved_at = CURRENT_TIMESTAMP WHERE id = ?`,
		admin, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}
	log.Printf("[admin] %s resolved report %d", admin, id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id})
}

func (s *NexusServer) adminBanHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/v1/admin/users/{username}/(ban|unban|role)
	tail := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/")
	parts := strings.Split(tail, "/")
	if len(parts) < 2 {
		http.Error(w, "expected /api/v1/admin/users/{username}/(ban|unban|role)", http.StatusBadRequest)
		return
	}
	target := parts[0]
	action := parts[1]
	if target == "" || !validUsername(target) {
		http.Error(w, "bad username", http.StatusBadRequest)
		return
	}
	if target == admin {
		http.Error(w, "cannot ban yourself", http.StatusBadRequest)
		return
	}

	// Single body decode shared by ban (reason) and role (role).
	var reqBody struct {
		Reason string `json:"reason"`
		Role   string `json:"role"`
	}
	if r.ContentLength > 0 && r.ContentLength < 4096 {
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&reqBody)
	}
	reason := strings.TrimSpace(reqBody.Reason)

	switch action {
	case "ban":
		res, err := s.DB.Exec(
			`UPDATE users SET is_banned = 1, ban_reason = ?, banned_at = CURRENT_TIMESTAMP WHERE username = ?`,
			reason, target)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		// Revoke all live sessions so the user is logged out everywhere.
		s.DB.Exec(`UPDATE session_tokens SET revoked = 1 WHERE username = ?`, target)
		// Kick connected session, if any.
		s.Mu.Lock()
		if c, ok := s.Clients[target]; ok {
			body := "Account suspended"
			if reason != "" {
				body += ": " + reason
			}
			c.Send(NexusMessage{Type: "kicked", Body: body})
			c.Conn.Close()
			delete(s.Clients, target)
		}
		s.Mu.Unlock()
		log.Printf("[admin] %s banned %s (reason=%q)", admin, target, reason)
	case "unban":
		res, err := s.DB.Exec(
			`UPDATE users SET is_banned = 0, ban_reason = '', banned_at = NULL WHERE username = ?`, target)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("[admin] %s unbanned %s", admin, target)
	case "role":
		// Role hierarchy enforcement: actor must outrank target both before
		// and after. Stops a regular admin from minting fellow admins.
		if _, ok := roleRanks[reqBody.Role]; !ok {
			http.Error(w, "unknown role (use user|helper|moderator|admin|super_admin)", http.StatusBadRequest)
			return
		}
		actorRank := roleRank(s.userRole(admin))
		if actorRank < roleRank("admin") {
			http.Error(w, "only admins can change roles", http.StatusForbidden)
			return
		}
		targetCurrentRank := roleRank(s.userRole(target))
		desiredRank := roleRank(reqBody.Role)
		if targetCurrentRank >= actorRank {
			http.Error(w, "cannot modify a peer or higher-ranked user", http.StatusForbidden)
			return
		}
		if desiredRank >= actorRank {
			http.Error(w, "cannot set a role >= your own", http.StatusForbidden)
			return
		}
		isAdminInt := 0
		if desiredRank >= roleRank("admin") {
			isAdminInt = 1
		}
		if _, err := s.DB.Exec(`UPDATE users SET role = ?, is_admin = ? WHERE username = ?`, reqBody.Role, isAdminInt, target); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		log.Printf("[role] %s set %s role to %s", admin, target, reqBody.Role)
	case "delete":
		if err := s.deleteAccount(target); err != nil {
			http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.Mu.Lock()
		if c, ok := s.Clients[target]; ok {
			c.Send(NexusMessage{Type: "kicked", Body: "Account deleted by admin"})
			c.Conn.Close()
			delete(s.Clients, target)
		}
		s.Mu.Unlock()
		log.Printf("[admin] %s deleted account %s", admin, target)
	default:
		http.Error(w, "expected /ban, /unban, /role, or /delete", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": target, "action": action})
}

// adminMeHandler returns the authenticated admin's username and role.
func (s *NexusServer) adminMeHandler(w http.ResponseWriter, r *http.Request) {
	u := s.adminFromRequest(w, r)
	if u == "" {
		return
	}
	role := s.userRole(u)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"username": u, "role": role})
}

// adminLogoutHandler clears the phaze_admin_token cookie.
func (s *NexusServer) adminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "phaze_admin_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// adminLoginHandler authenticates an admin via username + password and
// returns a session token usable as Bearer on every other admin endpoint.
// Also sets the phaze_admin_token HttpOnly, Secure cookie.
// Body JSON: { "username": "...", "password": "..." }
func (s *NexusServer) adminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if !s.authenticateUser(body.Username, body.Password) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	role := s.userRole(body.Username)
	if roleRank(role) < roleRank("helper") {
		http.Error(w, "not a staff account", http.StatusForbidden)
		return
	}
	tok, err := s.issueAdminSessionToken(body.Username)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "phaze_admin_token",
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": tok, "username": body.Username, "role": role})
}

// adminUsersHandler returns a listing of all users with key metadata, used
// by the admin portal to browse, ban, and promote users. Available to any
// staff role (helper+). Supports limit, offset pagination and search.
func (s *NexusServer) adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	if u, _ := s.modFromRequest(w, r, "helper"); u == "" {
		return
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	search := r.URL.Query().Get("search")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		if l > 1000 {
			l = 1000
		}
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	var total int
	var rows *sql.Rows
	var err error

	if search != "" {
		likePattern := "%" + search + "%"
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username LIKE ? OR email LIKE ? OR last_ip LIKE ? OR signup_ip LIKE ?`, likePattern, likePattern, likePattern, likePattern).Scan(&total); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		rows, err = s.DB.Query(`SELECT username, COALESCE(email,''), is_verified, is_banned, is_admin, COALESCE(role,'user'), COALESCE(ban_reason,''), COALESCE(CAST(created_at AS TEXT),''), COALESCE(last_ip,''), COALESCE(signup_ip,''), COALESCE(CAST(last_login_at AS TEXT),'')
			FROM users WHERE username LIKE ? OR email LIKE ? OR last_ip LIKE ? OR signup_ip LIKE ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, likePattern, likePattern, likePattern, likePattern, limit, offset)
	} else {
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		rows, err = s.DB.Query(`SELECT username, COALESCE(email,''), is_verified, is_banned, is_admin, COALESCE(role,'user'), COALESCE(ban_reason,''), COALESCE(CAST(created_at AS TEXT),''), COALESCE(last_ip,''), COALESCE(signup_ip,''), COALESCE(CAST(last_login_at AS TEXT),'')
			FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	}

	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type adminUser struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Verified    bool   `json:"verified"`
		Banned      bool   `json:"banned"`
		IsAdmin     bool   `json:"is_admin"`
		Role        string `json:"role"`
		BanReason   string `json:"ban_reason"`
		CreatedAt   string `json:"created_at"`
		LastIP      string `json:"last_ip"`
		SignupIP    string `json:"signup_ip"`
		LastLoginAt string `json:"last_login_at"`
		Online      bool   `json:"online"`
	}

	s.Mu.RLock()
	onlineMap := make(map[string]bool, len(s.Clients))
	for u := range s.Clients {
		onlineMap[u] = true
	}
	s.Mu.RUnlock()

	users := []adminUser{}
	for rows.Next() {
		var u adminUser
		var v, b, a int
		if err := rows.Scan(&u.Username, &u.Email, &v, &b, &a, &u.Role, &u.BanReason, &u.CreatedAt, &u.LastIP, &u.SignupIP, &u.LastLoginAt); err != nil {
			continue
		}
		u.Verified = v == 1
		u.Banned = b == 1
		u.IsAdmin = a == 1
		u.Online = onlineMap[u.Username]
		users = append(users, u)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"users":  users,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}


// adminBroadcastHandler posts a message to the global Phaze Hub
// #announcements channel as the authenticated admin user.
func (s *NexusServer) adminBroadcastHandler(w http.ResponseWriter, r *http.Request) {
	u := s.adminFromRequest(w, r)
	if u == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Message) == "" {
		http.Error(w, "bad json or empty message", http.StatusBadRequest)
		return
	}
	res, err := s.DB.Exec(
		`INSERT INTO channel_messages (channel_id, sender, body) VALUES (?, ?, ?)`,
		"global-announcements", u, strings.TrimSpace(body.Message))
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	created := time.Now().UTC().Format(time.RFC3339)
	// Push to every connected client subscribed to the global channel.
	s.Mu.RLock()
	for _, c := range s.Clients {
		c.Send(NexusMessage{
			Type:      "channel_msg_in",
			ChannelID: "global-announcements",
			ServerID:  globalSpaceID,
			Messages: []ChannelMsg{{
				ID: id, ChannelID: "global-announcements", Sender: u, Body: strings.TrimSpace(body.Message), CreatedAt: created,
			}},
		})
	}
	s.Mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id, "ok": true})
}

func (s *NexusServer) adminIPBlockHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method == http.MethodGet {
		s.blockedIPsMu.RLock()
		ips := make([]string, 0, len(s.blockedIPs))
		for ip := range s.blockedIPs {
			ips = append(ips, ip)
		}
		s.blockedIPsMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ips)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		IP     string `json:"ip"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IP == "" {
		http.Error(w, "ip required", http.StatusBadRequest)
		return
	}
	if body.Action == "unblock" {
		s.unblockIP(body.IP)
		log.Printf("[admin] %s unblocked IP %s", admin, body.IP)
	} else {
		s.blockIP(body.IP)
		log.Printf("[admin] %s blocked IP %s", admin, body.IP)
		s.Mu.RLock()
		for _, c := range s.Clients {
			if c.IP == body.IP {
				c.Send(NexusMessage{Type: "kicked", Body: "Your IP has been blocked"})
				c.Conn.Close()
			}
		}
		s.Mu.RUnlock()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (s *NexusServer) adminGlobalNoticeHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	log.Printf("[admin] %s sent global notice: %s", admin, body.Message)
	s.Mu.RLock()
	for _, c := range s.Clients {
		c.Send(NexusMessage{Type: "global_notice", Body: body.Message, Sender: admin})
	}
	s.Mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (s *NexusServer) adminVerifyUserHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}
	if _, err := s.DB.Exec("UPDATE users SET is_verified = 1 WHERE username = ?", body.Username); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	log.Printf("[admin] %s verified user %s", admin, body.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (s *NexusServer) adminStatsHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	s.Mu.RLock()
	online := len(s.Clients)
	s.Mu.RUnlock()
	var totalUsers, verified, banned, totalMessages, totalDMs int
	s.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&totalUsers)
	s.DB.QueryRow("SELECT COUNT(*) FROM users WHERE is_verified=1").Scan(&verified)
	s.DB.QueryRow("SELECT COUNT(*) FROM users WHERE is_banned=1").Scan(&banned)
	s.DB.QueryRow("SELECT COUNT(*) FROM channel_messages").Scan(&totalMessages)
	s.DB.QueryRow("SELECT COUNT(*) FROM dm_messages").Scan(&totalDMs)
	var dbSize int64
	s.DB.QueryRow("SELECT page_count * page_size FROM pragma_page_count, pragma_page_size").Scan(&dbSize)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"online":         online,
		"total_users":    totalUsers,
		"verified":       verified,
		"banned":         banned,
		"total_messages": totalMessages,
		"total_dms":      totalDMs,
		"db_size_mb":     float64(dbSize) / 1024 / 1024,
	})
}

func (s *NexusServer) adminLogsHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	rows, err := s.DB.Query(`SELECT username, COALESCE(last_ip,''), COALESCE(CAST(last_login_at AS TEXT),''), COALESCE(CAST(created_at AS TEXT),'')
		FROM users WHERE last_login_at IS NOT NULL ORDER BY last_login_at DESC LIMIT 50`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type logEntry struct {
		Username string `json:"username"`
		IP       string `json:"ip"`
		LoginAt  string `json:"login_at"`
		JoinedAt string `json:"joined_at"`
	}
	out := []logEntry{}
	for rows.Next() {
		var e logEntry
		rows.Scan(&e.Username, &e.IP, &e.LoginAt, &e.JoinedAt)
		out = append(out, e)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *NexusServer) adminGeoHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		http.Error(w, "ip required", http.StatusBadRequest)
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://ip-api.com/json/" + url.QueryEscape(ip) + "?fields=country,city,isp")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{})
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// adminPortalHandler serves the single-page HTML admin dashboard.
func (s *NexusServer) adminPortalHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(adminPortalHTML))
}

func (s *NexusServer) adminBannedUsersHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	rows, err := s.DB.Query(
		`SELECT username, COALESCE(ban_reason, ''), COALESCE(CAST(banned_at AS TEXT), '')
		   FROM users WHERE is_banned = 1 ORDER BY banned_at DESC LIMIT 500`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type bannedUser struct {
		Username string `json:"username"`
		Reason   string `json:"reason"`
		BannedAt string `json:"banned_at"`
	}
	out := []bannedUser{}
	for rows.Next() {
		var u bannedUser
		if err := rows.Scan(&u.Username, &u.Reason, &u.BannedAt); err != nil {
			continue
		}
		out = append(out, u)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ---------- Update manifest (GitHub Releases) ----------

// PlatformAsset is one downloadable artifact for the auto-update flow.
// SHA256 is the canonical integrity check; the client MUST verify before
// running the new binary. Empty SHA256 means checksums.txt was unavailable
// at refresh time — clients should refuse to auto-install in that case.
type PlatformAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
}

type UpdateManifest struct {
	Version      string                   `json:"version"`
	ReleaseURL   string                   `json:"release_url"`
	ReleaseNotes string                   `json:"release_notes,omitempty"`
	PublishedAt  string                   `json:"published_at,omitempty"`
	Platforms    map[string]PlatformAsset `json:"platforms"`
	RefreshedAt  int64                    `json:"refreshed_at"`
	Source       string                   `json:"source"` // "github" or "env"
}

type updateCache struct {
	mu       sync.RWMutex
	manifest UpdateManifest
	expires  time.Time
}

var updates = &updateCache{}

const updateTTL = 5 * time.Minute

// ghRelease is the subset of the GitHub Releases JSON we care about.
type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// classifyAsset returns the platform key ("windows" / "linux" / "android")
// for an asset name, or "" if it doesn't match any of our targets. Match is
// intentionally broad so we accept either "Phaze.exe"/"Phaze.apk"/"Phaze.linux"
// (current naming) or the goreleaser pattern "Phaze_<os>_<arch>.<ext>".
func classifyAsset(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, ".exe"),
		strings.Contains(low, "windows"):
		return "windows"
	case strings.HasSuffix(low, ".apk"):
		return "android"
	case strings.HasSuffix(low, ".linux"),
		strings.Contains(low, "linux"):
		return "linux"
	}
	return ""
}

// fetchChecksums downloads checksums.txt (goreleaser format: "<sha256>  <name>")
// and returns a map of filename -> sha256 hex. Returns nil on error so the
// caller can decide whether to publish an unverified manifest.
func fetchChecksums(url string, client *http.Client) map[string]string {
	if url == "" {
		return nil
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sum, name := fields[0], fields[1]
		if len(sum) != 64 {
			continue
		}
		// Strip a leading "*" goreleaser sometimes prefixes for binary mode.
		name = strings.TrimPrefix(name, "*")
		out[name] = strings.ToLower(sum)
	}
	return out
}

// refreshUpdateManifest fetches the latest release from GitHub and rebuilds
// the cached manifest. Falls back to env-var-only manifest when the API
// roundtrip fails (so the endpoint never disappears, just becomes thin).
func refreshUpdateManifest(repo string) UpdateManifest {
	envVersion := strings.TrimSpace(os.Getenv("Phaze_LATEST_VERSION"))
	envURL := strings.TrimSpace(os.Getenv("Phaze_UPDATE_URL"))
	if envURL == "" {
		envURL = "https://github.com/" + repo + "/releases/latest"
	}

	manifest := UpdateManifest{
		Version:     envVersion,
		ReleaseURL:  envURL,
		Platforms:   map[string]PlatformAsset{},
		RefreshedAt: time.Now().Unix(),
		Source:      "env",
	}

	if repo == "" {
		return manifest
	}

	client := &http.Client{Timeout: 8 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[update] github fetch: %v", err)
		return manifest
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[update] github status %d", resp.StatusCode)
		return manifest
	}

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		log.Printf("[update] github decode: %v", err)
		return manifest
	}

	// Find checksums.txt asset (goreleaser default name) before classifying
	// per-platform so we can attach SHA-256 to each.
	var checksumsURL string
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "checksums.txt") {
			checksumsURL = a.BrowserDownloadURL
			break
		}
	}
	sums := fetchChecksums(checksumsURL, client)

	platforms := map[string]PlatformAsset{}
	for _, a := range rel.Assets {
		plat := classifyAsset(a.Name)
		if plat == "" {
			continue
		}
		if _, taken := platforms[plat]; taken {
			continue // first match wins per platform
		}
		platforms[plat] = PlatformAsset{
			URL:    a.BrowserDownloadURL,
			SHA256: sums[a.Name],
			Size:   a.Size,
			Name:   a.Name,
		}
	}

	version := strings.TrimPrefix(rel.TagName, "v")
	if version == "" {
		version = envVersion
	}

	return UpdateManifest{
		Version:      version,
		ReleaseURL:   rel.HTMLURL,
		ReleaseNotes: rel.Body,
		PublishedAt:  rel.PublishedAt,
		Platforms:    platforms,
		RefreshedAt:  time.Now().Unix(),
		Source:       "github",
	}
}

func (s *NexusServer) versionHandler(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(os.Getenv("PHAZE_RELEASE_REPO"))
	if repo == "" {
		repo = "jakes1345/skype7-reborn"
	}

	updates.mu.RLock()
	fresh := time.Now().Before(updates.expires) && updates.manifest.RefreshedAt > 0
	manifest := updates.manifest
	updates.mu.RUnlock()

	if !fresh {
		manifest = refreshUpdateManifest(repo)
		updates.mu.Lock()
		updates.manifest = manifest
		updates.expires = time.Now().Add(updateTTL)
		updates.mu.Unlock()
	}

	// Backwards-compat shim: older clients only look at `version` and `url`.
	// New clients use `platforms` + `release_notes`.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	out := struct {
		UpdateManifest
		// Legacy fields:
		URL string `json:"url"`
	}{UpdateManifest: manifest, URL: manifest.ReleaseURL}
	json.NewEncoder(w).Encode(out)
}

func (s *NexusServer) statsHandler(w http.ResponseWriter, r *http.Request) {
	s.Mu.RLock()
	active := len(s.Clients)
	s.Mu.RUnlock()

	var total int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&total)
	if err != nil {
		total = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_nodes":  active,
		"total_members": total,
		"timestamp":     time.Now().Unix(),
		"status":        "online",
	})
}

func (s *NexusServer) initFCM() {
	keyJSON := os.Getenv("FCM_SERVICE_ACCOUNT_JSON")
	if keyJSON == "" {
		log.Println("[FCM] FCM_SERVICE_ACCOUNT_JSON not set — FCM push disabled")
		return
	}
	app, err := firebase.NewApp(context.Background(), nil, option.WithCredentialsJSON([]byte(keyJSON)))
	if err != nil {
		log.Printf("[FCM] init error: %v", err)
		return
	}
	client, err := app.Messaging(context.Background())
	if err != nil {
		log.Printf("[FCM] messaging client error: %v", err)
		return
	}
	s.fcmClient = client
	log.Println("[FCM] Firebase Cloud Messaging initialized")
}

func (s *NexusServer) sendFCMPush(recipient, sender, preview string) {
	if s.fcmClient == nil {
		return
	}
	rows, err := s.DB.Query("SELECT fcm_token FROM users WHERE username = ? AND fcm_token != ''", recipient)
	if err != nil {
		return
	}
	defer rows.Close()
	body := preview
	if len(body) > 100 {
		body = body[:100] + "…"
	}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil || token == "" {
			continue
		}
		_, err := s.fcmClient.Send(context.Background(), &messaging.Message{
			Token: token,
			Notification: &messaging.Notification{
				Title: sender,
				Body:  body,
			},
			Android: &messaging.AndroidConfig{
				Priority: "high",
				Notification: &messaging.AndroidNotification{
					Sound:       "default",
					ClickAction: "world.phazechat.app.OPEN",
				},
			},
		})
		if err != nil {
			log.Printf("[FCM] send to %s: %v", recipient, err)
			// Token invalid — clear it
			if messaging.IsRegistrationTokenNotRegistered(err) {
				s.DB.Exec("UPDATE users SET fcm_token = '' WHERE username = ? AND fcm_token = ?", recipient, token)
			}
		}
	}
}

func (s *NexusServer) vapidKeyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{"publicKey": os.Getenv("VAPID_PUBLIC_KEY")})
}

func (s *NexusServer) exportHandler(w http.ResponseWriter, r *http.Request) {
	// H5: prefer Authorization header; fall back to query param for backwards compat.
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		http.Error(w, "token required", http.StatusUnauthorized)
		return
	}
	username := s.sessionUsername(tok)
	if username == "" {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}
	data := s.exportUserData(username)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="phaze-data-export.json"`)
	json.NewEncoder(w).Encode(data)
}

// ---------- Main ----------

// resolveWorkingDir sets the process working directory so relative paths
// templates/, public/, and the default SQLite file resolve correctly when
// the binary is launched from outside nexus_server/ (for example ../bin/phaze-nexus).
func resolveWorkingDir() {
	if d := strings.TrimSpace(os.Getenv("PHAZE_ASSET_ROOT")); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			log.Printf("[nexus] PHAZE_ASSET_ROOT: %v", err)
			return
		}
		if err := os.Chdir(abs); err != nil {
			log.Printf("[nexus] PHAZE_ASSET_ROOT chdir %q: %v", abs, err)
		} else {
			log.Printf("[nexus] working directory: %s (PHAZE_ASSET_ROOT)", abs)
		}
		return
	}
	if _, err := os.Stat("templates/landing.html"); err == nil {
		if wd, err := os.Getwd(); err == nil {
			log.Printf("[nexus] working directory: %s", wd)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[nexus] could not resolve executable: %v", err)
		return
	}
	exeDir := filepath.Clean(filepath.Dir(exe))
	candidates := []string{
		exeDir,
		filepath.Join(exeDir, "..", "nexus_server"),
		filepath.Join(exeDir, "..", "..", "nexus_server"),
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "templates", "landing.html")); err != nil {
			continue
		}
		if err := os.Chdir(abs); err != nil {
			log.Printf("[nexus] chdir %q: %v", abs, err)
			continue
		}
		log.Printf("[nexus] working directory: %s (auto-detected)", abs)
		return
	}
	if wd, err := os.Getwd(); err == nil {
		log.Printf("[nexus] working directory: %s (templates/ not found; some pages use built-in HTML fallback)", wd)
	}
}

// initSentry wires error tracking when SENTRY_DSN is set. No-op when unset,
// so the server still ships safely before an account exists. Captures all
// unhandled panics + explicit sentry.CaptureException(...) calls.
func initSentry(component string) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		log.Printf("[sentry] SENTRY_DSN not set — error reporting disabled")
		return
	}
	env := os.Getenv("SENTRY_ENVIRONMENT")
	if env == "" {
		env = "production"
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      env,
		Release:          os.Getenv("FLY_MACHINE_VERSION"),
		TracesSampleRate: 0.05,
		ServerName:       component,
	})
	if err != nil {
		log.Printf("[sentry] init failed: %v", err)
		return
	}
	log.Printf("[sentry] error reporting enabled (env=%s)", env)
}

func main() {
	resolveWorkingDir()
	initSentry("nexus")
	defer sentry.Flush(2 * time.Second)
	defer sentry.Recover()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "nexus.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	// Enable WAL mode for better concurrent access
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	server := &NexusServer{
		DB:          db,
		Clients:     make(map[string]*Client),
		VoiceRooms:  make(map[string]map[string]struct{}),
		Streams:     make(map[string]*liveStream),
		RemoteCodes: make(map[string]remoteCodeEntry),
		wsConnCount: make(map[string]int),
		blockedIPs:  make(map[string]bool),
	}
	server.initDB()
	server.initFCM()
	server.loadBlockedIPs() // M3: restore IP blocks from DB
	server.ensureGlobalSpace()
	go server.sweepRemoteCodes() // L2: expire stale remote codes
	if bot := os.Getenv("KAI_USERNAME"); bot != "" {
		server.autoJoinPublicSpaces(bot)
	}

	http.HandleFunc("/ws", rateLimit(server.handleConnections))
	http.HandleFunc("/api/v1/version", rateLimit(server.versionHandler))
	http.HandleFunc("/api/v1/profile/", rateLimit(server.profileHandler))
	http.HandleFunc("/api/v1/avatars/", rateLimit(server.avatarHandler))
	http.HandleFunc("/api/v1/upload", rateLimit(server.uploadHandler))
	http.HandleFunc("/uploads/", server.uploadsServeHandler)
	http.HandleFunc("/twiml/outbound", rateLimit(server.twimlHandler))

	fs := http.FileServer(http.Dir("public"))
	http.Handle("/public/", http.StripPrefix("/public/", fs))
	http.HandleFunc("/downloads/", server.fileDownloadHandler)

	// Web client (React/Vite SPA). The Docker build stage compiles
	// web/dist into /app/web; in dev a symlink (or PHAZE_WEB_DIR env)
	// points elsewhere. SPA fallback to index.html so client-side routes
	// keep working on hard refresh; static assets get long-lived caching
	// via Vite's content-hashed filenames.
	webDir := strings.TrimSpace(os.Getenv("PHAZE_WEB_DIR"))
	if webDir == "" {
		webDir = "web"
	}
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err == nil {
		webFS := http.FileServer(http.Dir(webDir))
		http.HandleFunc("/web/", func(w http.ResponseWriter, r *http.Request) {
			// Explicit Permissions-Policy so getUserMedia/getDisplayMedia work
			// even when the SPA is reached via embedding proxies or CDN paths
			// that strip defaults. Top-level HTTPS already allows these, but
			// being explicit avoids edge-case "permission denied" silently.
			w.Header().Set("Permissions-Policy", "camera=(self), microphone=(self), display-capture=(self), autoplay=(self)")
			// M1: Content-Security-Policy for the React SPA.
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"connect-src 'self' wss://phazechat.world wss://*.phazechat.world https://api.github.com; "+
					"img-src 'self' data: blob:; "+
					"media-src 'self' blob:; "+
					"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
					"font-src 'self' https://fonts.gstatic.com; "+
					"script-src 'self'; "+
					"frame-ancestors 'none'")
			rel := strings.TrimPrefix(r.URL.Path, "/web/")
			candidate := filepath.Join(webDir, filepath.FromSlash(rel))
			if rel != "" {
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					if strings.HasPrefix(rel, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					http.StripPrefix("/web/", webFS).ServeHTTP(w, r)
					return
				}
			}
			// SPA fallback — anything unknown serves index.html.
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
		})
		// Bare /web → redirect to /web/ so relative asset URLs resolve correctly.
		http.HandleFunc("/web", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/web/", http.StatusFound)
		})
		log.Printf("[web] serving SPA from %s at /web/", webDir)
	} else {
		log.Printf("[web] no SPA at %s — /web/ will 404 until web client is built", webDir)
	}

	http.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fmt.Fprint(w, "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /ws\nDisallow: /reset\nSitemap: https://phazechat.world/sitemap.xml\n")
	})
	http.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		const base = "https://phazechat.world"
		today := time.Now().UTC().Format("2006-01-02")
		paths := []string{"/", "/download", "/features", "/rates", "/about", "/support", "/privacy", "/terms", "/legal", "/status", "/web/"}
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)
		fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		for _, p := range paths {
			fmt.Fprintf(w, `<url><loc>%s%s</loc><lastmod>%s</lastmod><changefreq>weekly</changefreq></url>`, base, p, today)
		}
		fmt.Fprint(w, `</urlset>`)
	})

	// Search engine verification endpoints.
	// Google: flyctl secrets set PHAZE_GSC_VERIFICATION=google123abc.html
	// Bing:   flyctl secrets set PHAZE_BING_VERIFICATION=<xml-code>
	// Yandex: flyctl secrets set PHAZE_YANDEX_VERIFICATION=<code>
	http.HandleFunc("/google", func(w http.ResponseWriter, r *http.Request) {
		v := os.Getenv("PHAZE_GSC_VERIFICATION")
		if v == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "google-site-verification: "+v)
	})
	http.HandleFunc("/BingSiteAuth.xml", func(w http.ResponseWriter, r *http.Request) {
		v := os.Getenv("PHAZE_BING_VERIFICATION")
		if v == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0"?><users><user>%s</user></users>`, v)
	})
	http.HandleFunc("/yandex_", func(w http.ResponseWriter, r *http.Request) {
		v := os.Getenv("PHAZE_YANDEX_VERIFICATION")
		if v == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><meta name="yandex-verification" content="%s" /></head></html>`, v)
	})

	// IndexNow: instant indexing for Bing, Yandex, DuckDuckGo, Seznam, etc.
	// Key file must be hosted at /<key>.txt for verification.
	indexNowKey := os.Getenv("INDEXNOW_KEY")
	if indexNowKey == "" {
		indexNowKey = "c7b5c55a52c28ecfa0b858dbce7c2a3e"
	}
	http.HandleFunc("/"+indexNowKey+".txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, indexNowKey)
	})

	http.HandleFunc("/verify-email", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("u")
		code := r.URL.Query().Get("code")
		if u == "" || code == "" {
			http.Error(w, "Missing parameters", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if server.verifyUser(u, code) {
			server.autoJoinGlobalSpace(u)
			fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8"><title>Verified!</title>
<style>body{font-family:Inter,system-ui,sans-serif;background:#0b0b0d;color:#fafafa;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0}
.card{background:#16161a;border:1px solid #232328;border-radius:16px;padding:48px;text-align:center;max-width:420px}
h1{color:#86efac;margin:0 0 12px}p{color:#a1a1aa;margin:0 0 24px}
a{display:inline-block;background:#863bff;color:#fff;padding:12px 32px;border-radius:8px;text-decoration:none;font-weight:600}</style></head>
<body><div class="card"><h1>You're verified!</h1><p>Your Phaze account is ready. Sign in and start chatting.</p><a href="/web/">Open Phaze</a></div></body></html>`)
		} else {
			fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8"><title>Verification Failed</title>
<style>body{font-family:Inter,system-ui,sans-serif;background:#0b0b0d;color:#fafafa;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0}
.card{background:#16161a;border:1px solid #232328;border-radius:16px;padding:48px;text-align:center;max-width:420px}
h1{color:#fca5a5;margin:0 0 12px}p{color:#a1a1aa}</style></head>
<body><div class="card"><h1>Verification failed</h1><p>The link may have expired or already been used. Try signing in and requesting a new code.</p></div></body></html>`)
		}
	})

	http.HandleFunc("/", server.landingHandler)
	http.HandleFunc("/download", server.downloadHandler)
	http.HandleFunc("/features", server.featuresHandler)
	http.HandleFunc("/rates", server.ratesHandler)
	http.HandleFunc("/about", server.aboutHandler)
	http.HandleFunc("/support", server.supportHandler)
	http.HandleFunc("/privacy", server.privacyHandler)
	http.HandleFunc("/terms", server.termsHandler)
	http.HandleFunc("/legal", server.legalHandler)
	http.HandleFunc("/reset", server.resetHandler)
	http.HandleFunc("/version", server.versionHandler)
	http.HandleFunc("/health", server.healthHandler)
	http.HandleFunc("/metrics", server.metricsHandler)
	server.initSupportRoutes()
	server.initStoriesRoutes()
	server.startDataRetentionSweepers()
	server.initStatusPage()

	// Runtime config the SPA can fetch on boot. Anything user-visible
	// that should be tweakable without a rebuild goes here.
	http.HandleFunc("/api/v1/config", rateLimit(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		bmc := os.Getenv("BMC_URL")
		if bmc == "" {
			bmc = "https://buymeacoffee.com/phazeworld"
		}
		json.NewEncoder(w).Encode(map[string]string{
			"bmc_url":     bmc,
			"support_email": os.Getenv("PHAZE_SUPPORT_EMAIL"),
			"version":     "1.0.0-Phaze",
		})
	}))
	// Admin portal — hidden path, not /admin. Set PHAZE_ADMIN_PATH env var
	// to customize (default: /nexus-cmd). Bots scanning /admin get nothing.
	adminPath := strings.TrimSpace(os.Getenv("PHAZE_ADMIN_PATH"))
	if adminPath == "" {
		adminPath = "/nexus-cmd"
	}
	adminPath = "/" + strings.TrimLeft(adminPath, "/")
	log.Printf("[admin] portal mounted at %s", adminPath)
	adminGate := adminIPGate(rateLimit(server.adminPortalHandler))
	http.HandleFunc(adminPath, adminGate)
	http.HandleFunc(adminPath+"/", adminGate)
	http.HandleFunc("/api/v1/admin/login", adminIPGate(adminLoginLimit(server.adminLoginHandler)))
	http.HandleFunc("/api/v1/admin/me", adminIPGate(rateLimit(server.adminMeHandler)))
	http.HandleFunc("/api/v1/admin/logout", adminIPGate(rateLimit(server.adminLogoutHandler)))
	http.HandleFunc("/api/v1/admin/users", adminIPGate(rateLimit(server.adminUsersHandler)))
	http.HandleFunc("/api/v1/admin/broadcast", adminIPGate(rateLimit(server.adminBroadcastHandler)))
	http.HandleFunc("/api/v1/admin/ip-block", adminIPGate(rateLimit(server.adminIPBlockHandler)))
	http.HandleFunc("/api/v1/admin/global-notice", adminIPGate(rateLimit(server.adminGlobalNoticeHandler)))
	http.HandleFunc("/api/v1/admin/verify-user", adminIPGate(rateLimit(server.adminVerifyUserHandler)))
	http.HandleFunc("/api/v1/admin/stats", adminIPGate(rateLimit(server.adminStatsHandler)))
	http.HandleFunc("/api/v1/admin/logs", adminIPGate(rateLimit(server.adminLogsHandler)))
	http.HandleFunc("/api/v1/admin/geo", adminIPGate(rateLimit(server.adminGeoHandler)))
	// C1: All admin endpoints must go through adminIPGate to enforce PHAZE_ADMIN_IPS.
	// These 5 were previously missing the IP gate, allowing access from any IP with a valid token.
	http.HandleFunc("/api/v1/admin/pending-verifications", adminIPGate(rateLimit(server.adminPendingVerificationsHandler)))
	http.HandleFunc("/api/v1/admin/reports", adminIPGate(rateLimit(server.adminReportsHandler)))
	http.HandleFunc("/api/v1/admin/reports/", adminIPGate(rateLimit(server.adminResolveReportHandler))) // /{id}/resolve
	http.HandleFunc("/api/v1/admin/users/", adminIPGate(rateLimit(server.adminBanHandler)))             // /{username}/(ban|unban|role)
	http.HandleFunc("/api/v1/admin/banned", adminIPGate(rateLimit(server.adminBannedUsersHandler)))
	http.HandleFunc("/api/v1/admin/supporters", adminIPGate(rateLimit(server.adminSupportersHandler)))
	http.HandleFunc("/api/v1/admin/supporters/", adminIPGate(rateLimit(server.adminSupporterActionHandler))) // /{id}/(grant|dismiss)

	// Public: opt-in supporter form behind the "Support Phaze" button.
	http.HandleFunc("/api/v1/support/request", rateLimit(server.supportRequestHandler))

	http.HandleFunc("/api/v1/stats", rateLimit(server.statsHandler))
	http.HandleFunc("/api/v1/export", rateLimit(server.exportHandler))
	http.HandleFunc("/api/v1/vapid-key", server.vapidKeyHandler)

	bindAddr := os.Getenv("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	log.Printf("Phaze Nexus Server v1.0.0 starting on %s:%s...", bindAddr, port)
	log.Printf("  WebSocket endpoint: ws://%s:%s/ws", bindAddr, port)
	log.Printf("  Health check: http://%s:%s/health", bindAddr, port)

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			server.Mu.RLock()
			count := len(server.Clients)
			server.Mu.RUnlock()
			log.Printf("Connected clients: %d", count)
		}
	}()

	err = http.ListenAndServe(bindAddr+":"+port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func (s *NexusServer) sendSMS(to, body string) error {
	sid := os.Getenv("TWILIO_SID")
	token := os.Getenv("TWILIO_TOKEN")
	from := os.Getenv("TWILIO_FROM")

	if sid == "" || token == "" || from == "" {
		log.Printf("[SMS-SIM] To: %s | Body: %s", to, body)
		return nil
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid)
	v := url.Values{}
	v.Set("To", to)
	v.Set("From", from)
	v.Set("Body", body)

	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(v.Encode()))
	req.SetBasicAuth(sid, token)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *NexusServer) broadcastProfileUpdate(username, displayName, mood string) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	msg := NexusMessage{
		Type:        "profile_update",
		Sender:      username,
		DisplayName: displayName,
		Mood:        mood,
	}
	for _, client := range s.Clients {
		if client.Username != username {
			client.Send(msg)
		}
	}
}

func (s *NexusServer) profileHandler(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/v1/profile/")
	if username == "" {
		http.Error(w, "Username required", 400)
		return
	}
	var displayName, mood string
	var supporter int
	err := s.DB.QueryRow("SELECT COALESCE(display_name, ''), COALESCE(mood, ''), COALESCE(supporter, 0) FROM users WHERE username = ?", username).Scan(&displayName, &mood, &supporter)
	if err != nil {
		http.Error(w, "User not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"username":     username,
		"display_name": displayName,
		"mood":         mood,
		"supporter":    supporter == 1,
	})
}

func (s *NexusServer) avatarHandler(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/v1/avatars/")
	if username == "" {
		http.Error(w, "Username required", 400)
		return
	}
	// Block path traversal: reject anything that isn't a plain valid
	// username. validUsername already enforces [a-zA-Z0-9_] and length —
	// the same regex used at registration — so callers can't sneak ".." or
	// "/" into the avatar path.
	if !validUsername(username) {
		http.ServeFile(w, r, "assets/default_avatar.png")
		return
	}
	path := "avatars/" + username + ".png"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.ServeFile(w, r, "assets/default_avatar.png")
		return
	}
	http.ServeFile(w, r, path)
}

// uploadHandler accepts authenticated multipart uploads for in-chat
// file/image attachments. Auth is the same session token issued by /auth.
// Files are stored under public/uploads/ with a random opaque name so the
// URL itself doesn't leak the original filename. The returned URL is sent
// (E2EE-encrypted on the client) as the message body — the server never
// learns which conversation references which upload.
const maxUploadBytes = 25 * 1024 * 1024 // 25 MB

// uploadDir returns the directory where chat attachments are stored. In
// production (Fly) set PHAZE_UPLOAD_DIR=/data/uploads so files persist on
// the mounted volume; otherwise we fall back to public/uploads relative to
// the working directory for local dev.
func uploadDir() string {
	if d := os.Getenv("PHAZE_UPLOAD_DIR"); d != "" {
		return d
	}
	return "public/uploads"
}

func (s *NexusServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	authH := r.Header.Get("Authorization")
	if !strings.HasPrefix(authH, "Bearer ") {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	tok := strings.TrimSpace(strings.TrimPrefix(authH, "Bearer "))
	uploader := s.sessionUsername(tok)
	if uploader == "" {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if banned, _ := s.userBanInfo(uploader); banned {
		http.Error(w, "account suspended", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)
	if err := r.ParseMultipartForm(maxUploadBytes + 1<<20); err != nil {
		http.Error(w, "file too large or malformed", http.StatusRequestEntityTooLarge)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if hdr.Size > maxUploadBytes {
		http.Error(w, "file exceeds 25MB", http.StatusRequestEntityTooLarge)
		return
	}

	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	// Allow only a known-safe set of extensions; reject obvious executable types.
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic",
		".mp4", ".mov", ".webm", ".m4a", ".mp3", ".ogg", ".wav",
		".pdf", ".txt", ".md", ".log", ".csv", ".json",
		".zip", ".7z", ".tar", ".gz":
		// ok
	default:
		http.Error(w, "file type not allowed", http.StatusUnsupportedMediaType)
		return
	}

	// Magic-byte validation for image types: reject extension-spoofed executables.
	// Read the first 512 bytes for MIME sniffing without consuming the reader.
	headBuf := make([]byte, 512)
	n, _ := io.ReadFull(file, headBuf)
	file.Seek(0, io.SeekStart) // rewind for the actual copy
	detected := http.DetectContentType(headBuf[:n])
	switch {
	case strings.HasPrefix(ext, ".png") || strings.HasPrefix(ext, ".jpg") ||
		strings.HasPrefix(ext, ".jpeg") || strings.HasPrefix(ext, ".gif") ||
		strings.HasPrefix(ext, ".webp") || strings.HasPrefix(ext, ".bmp"):
		// Image extensions must have image/* MIME
		if !strings.HasPrefix(detected, "image/") {
			log.Printf("[upload] rejected %s: ext=%s detected=%s uploader=%s", hdr.Filename, ext, detected, uploader)
			http.Error(w, "file content does not match extension", http.StatusUnsupportedMediaType)
			return
		}
	case ext == ".pdf":
		if detected != "application/pdf" {
			log.Printf("[upload] rejected %s: ext=%s detected=%s uploader=%s", hdr.Filename, ext, detected, uploader)
			http.Error(w, "file content does not match extension", http.StatusUnsupportedMediaType)
			return
		}
	}

	if err := os.MkdirAll(uploadDir(), 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	rnd, err := randHex(16)
	if err != nil {
		http.Error(w, "rng error", http.StatusInternalServerError)
		return
	}
	storedName := rnd + ext
	outPath := filepath.Join(uploadDir(), storedName)
	out, err := os.Create(outPath)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(file, maxUploadBytes)); err != nil {
		os.Remove(outPath)
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}

	url := "/uploads/" + storedName
	log.Printf("[upload] %s saved %s (%d bytes)", uploader, storedName, hdr.Size)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"url":  url,
		"name": filepath.Base(hdr.Filename),
		"size": hdr.Size,
		"mime": hdr.Header.Get("Content-Type"),
	})
}

func (s *NexusServer) uploadsServeHandler(w http.ResponseWriter, r *http.Request) {
	// Strip path traversal: only allow the bare filename.
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/uploads/"))
	if name == "" || name == "." || name == "/" {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(uploadDir(), name)
	if _, err := os.Stat(full); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, full)
}

func (s *NexusServer) handleBotMessage(client *Client, msg NexusMessage) {
	reply := NexusMessage{
		Type:      "msg",
		Sender:    "PhazeBot",
		Recipient: msg.Sender,
		Body:      "I am the Phaze Mesh Assistant. Try: /mesh, /version, /pstn (PSTN status), /webrtc",
	}

	cmd := strings.ToLower(strings.TrimSpace(msg.Body))
	switch {
	case cmd == "/mesh":
		s.Mu.RLock()
		count := len(s.Clients)
		s.Mu.RUnlock()
		reply.Body = fmt.Sprintf("The Phaze Mesh currently has %d active sovereign peers.", count)
	case cmd == "/webrtc":
		reply.Body = "Phaze voice/video uses WebRTC (Pion on desktop, browser APIs on web). Signaling goes over Nexus; media is peer-to-peer when possible, with TURN from your relay when NAT blocks direct paths."
	case cmd == "/version":
		reply.Body = "Nexus Server v1.0.0-Phaze | Build: Enterprise-Mesh"
	case cmd == "/pstn":
		if pstnBridgeEnabled() {
			reply.Body = "PSTN bridge is ON for this relay (Twilio). Link your phone in Settings for verified outbound. Otherwise use WebRTC calls in chat."
		} else {
			reply.Body = "PSTN bridge is OFF. Voice/video is WebRTC between Phaze users only — no carrier charges."
		}
	}
	client.Send(reply)
}

func (s *NexusServer) twimlHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Dial><Conference>Phaze_MESH_BRIDGE</Conference></Dial></Response>`))
}

func (s *NexusServer) initiateTwilioCall(to string) error {
	sid := os.Getenv("TWILIO_SID")
	token := os.Getenv("TWILIO_TOKEN")
	from := os.Getenv("TWILIO_FROM")
	appURL := os.Getenv("Phaze_APP_URL")

	if sid == "" || token == "" || from == "" || appURL == "" {
		log.Printf("[PSTN-SIM] Initiating call to %s via TwiML Hub", to)
		return nil
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls.json", sid)
	v := url.Values{}
	v.Set("To", to)
	v.Set("From", from)
	v.Set("Url", appURL+"/twiml/outbound")

	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(v.Encode()))
	req.SetBasicAuth(sid, token)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
