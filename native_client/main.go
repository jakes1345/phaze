package main

import (
	"bytes"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/liyue201/goqr"
	_ "image/jpeg"
	_ "image/png"

	"github.com/getsentry/sentry-go"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
	"github.com/zalando/go-keyring"
	_ "modernc.org/sqlite"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"phaze-native/internal/chat"
	"phaze-native/internal/crypto"
	"phaze-native/internal/remote"
	"phaze-native/internal/screencap"
	"phaze-native/internal/sentinel"
	"phaze-native/internal/ui"
	"phaze-native/internal/updater"

	"fyne.io/fyne/v2/driver/desktop"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"github.com/pion/webrtc/v3"
)

type Infrastructure struct {
	API      string
	Login    string
	Gateway  string
	Contacts string
	ASM      string
}

var PhazeInfra = Infrastructure{
	API:      "https://phazechat.world",
	Login:    "https://phazechat.world",
	Gateway:  "wss://phazechat.world/ws",
	Contacts: "https://phazechat.world",
	ASM:      "https://phazechat.world",
}

func humanSize(n int) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	if n < k*k {
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	if n < k*k*k {
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
}

// Version is stamped at build time via -ldflags "-X main.Version=$(VERSION)"
// (see Makefile, sourced from the repo-root VERSION file). Defaults to "dev"
// for ad-hoc `go build` runs.
var Version = "dev"

const (
	keyringService = "phaze-sovereign"
)

//go:embed assets/Icon.png
var embeddedIconPNG []byte

// NexusMessage matches the Nexus server protocol
type NexusMessage struct {
	Type           string           `json:"type"`
	Sender         string           `json:"sender"`
	Recipient      string           `json:"recipient"`
	Body           string           `json:"body"`
	Status         string           `json:"status"`
	Results        []string         `json:"results"`
	SDP            string           `json:"sdp"`
	Candidate      string           `json:"candidate"`
	Error          string           `json:"error"`
	Email          string           `json:"email,omitempty"`
	Mood           string           `json:"mood,omitempty"`
	DisplayName    string           `json:"display_name,omitempty"`
	Supporter      bool             `json:"supporter,omitempty"`
	Phone          string           `json:"phone,omitempty"`
	Location       string           `json:"location,omitempty"`
	Birthday       string           `json:"birthday,omitempty"`
	Language       string           `json:"language,omitempty"`
	ConvoID        string           `json:"convo_id,omitempty"`
	ConvoName      string           `json:"convo_name,omitempty"`
	Members        []string         `json:"members,omitempty"`
	Token          string           `json:"token,omitempty"`
	Endpoint       string           `json:"endpoint,omitempty"`
	PublicKey      []byte           `json:"public_key,omitempty"`
	KeyFingerprint string           `json:"key_fingerprint,omitempty"`
	TurnConfig     *chat.TurnConfig `json:"turn_config,omitempty"`
	TOTPCode       string           `json:"totp_code,omitempty"`
	TOTPURI        string           `json:"totp_uri,omitempty"`
	QRToken        string           `json:"qr_token,omitempty"`
	QRData         string           `json:"qr_data,omitempty"`
	DeviceInfo     string           `json:"device_info,omitempty"`

	// Per-member ciphertext for group messages (convo_msg). Server forwards
	// envelopes[recipient] as Body to each member without ever seeing plaintext.
	Envelopes map[string]string `json:"envelopes,omitempty"`

	// Extended message fields (edit/delete/react/voice/file)
	MsgID     string              `json:"msg_id,omitempty"`
	Reaction  string              `json:"reaction,omitempty"`
	Kind      string              `json:"kind,omitempty"`
	FileURL   string              `json:"file_url,omitempty"`
	FileName  string              `json:"file_name,omitempty"`
	Reactions map[string][]string `json:"reactions,omitempty"`

	// Spaces extensions
	Visibility string           `json:"visibility,omitempty"`
	Topic      string           `json:"topic,omitempty"`
	InviteCode string           `json:"invite_code,omitempty"`
	ServerID   string           `json:"server_id,omitempty"`
	ChannelID  string           `json:"channel_id,omitempty"`
	ServerName string           `json:"server_name,omitempty"`
	Servers    []ui.SpaceInfo   `json:"servers,omitempty"`
	Channels   []ui.ChannelInfo `json:"channels,omitempty"`
	Messages   []ui.ChannelMsg  `json:"messages,omitempty"`
	KeyBackup  *KeyBackupPayload `json:"key_backup,omitempty"`
}

type authResult struct {
	Status       string
	SessionToken string
	Error        string
}

const sessionKeyringService = "phaze-sovereign-session"

// PhazeApp holds all application state
type PhazeApp struct {
	App        fyne.App
	MainWindow fyne.Window

	// Windows
	ChatWindows map[string]fyne.Window
	CallWindows map[string]fyne.Window

	// Database
	DB     *sql.DB
	DBPath string

	// Settings
	CompactMode          bool
	ServerAddress        string
	SoundEnabled         bool
	NotificationsEnabled bool

	// Network
	Username     string
	SessionToken string
	Conn         *websocket.Conn
	ConnMu       sync.Mutex
	authChan     chan authResult

	// UI State
	ChatLogs         map[string]*fyne.Container
	ChatTypingLabels map[string]*widget.Label
	SearchResult     *widget.List
	ContactList      *widget.List
	Discovered       []string
	Friends          []ui.FriendInfo
	PendingInbound   []string
	AvatarPath       string
	TypingTimers     map[string]*time.Timer
	LastTypingSent   map[string]time.Time

	// Notifications
	UnreadCounts map[string]int
	Calls        *chat.CallManager
	Slicer       *ui.AeroSlicer

	Sidebar      fyne.CanvasObject
	HomeView     fyne.CanvasObject
	ContentStack *fyne.Container
	MainSplit    *container.Split
	LiveStreams   []ui.LiveStream
	P2P          *chat.P2PManager

	// Crypto Identity
	PubKey   *[32]byte
	PrivKey  *[32]byte
	PeerKeys map[string]*[32]byte // Cache for peer public keys

	// ConvoMembers[convoID] is the authoritative member list for a group,
	// populated from convo_info / convo_created messages. Used to fan out
	// per-member E2EE envelopes on outgoing group messages.
	ConvoMembers map[string][]string

	// Spaces State
	Spaces         []ui.SpaceInfo
	Channels       map[string][]ui.ChannelInfo
	ActiveSpace    string
	ActiveChannel  string
	ChannelHistory map[string][]ui.ChannelMsg

	// Extended State
	OpenWindows  map[string]fyne.Window
	SplitMode    bool
	LastActivity time.Time
	isAway       bool
	Status       string
	Mood         string

	// Forensic Infrastructure
	Infra    Infrastructure
	Sentinel *sentinel.Sentinel
}

func NewPhazeApp() *PhazeApp {
	// Unlock the Sovereign Forensic Vault
	if err := ui.UnlockVault(); err != nil {
		log.Printf("[Vault] FATAL: Could not unlock sovereign assets: %v", err)
		// We continue, but UI will be broken/unusable (Anti-Theft)
	}

	a := app.NewWithID("world.phazechat.app")

	// Set the window/taskbar icon. Prefer the binary-embedded PNG so the
	// icon shows even when assets.vault is absent (raw `go build` runs).
	// Fall back to the vault asset for backwards compatibility.
	if len(embeddedIconPNG) > 0 {
		a.SetIcon(fyne.NewStaticResource("Icon.png", embeddedIconPNG))
	} else {
		a.SetIcon(ui.GetAssetResource("assets/Icon.png"))
	}

	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".private_phaze")
	os.MkdirAll(dbDir, 0755)
	dbPath := filepath.Join(dbDir, "main.db")

	if err := decryptDBFile(dbPath); err != nil {
		log.Printf("[Vault] DB decrypt failed: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	// Local SQLite cache for accounts, contacts, and message history (legacy column names).
	tables := []string{
		`CREATE TABLE IF NOT EXISTS Accounts (
			id INTEGER PRIMARY KEY,
			skypename TEXT UNIQUE,
			fullname TEXT,
			emails TEXT,
			mood_text TEXT,
			avatar_image BLOB,
			public_key BLOB,
			private_key BLOB,
			last_used_timestamp INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS Contacts (
			id INTEGER PRIMARY KEY,
			skypename TEXT UNIQUE,
			displayname TEXT,
			avatar_image BLOB,
			mood_text TEXT,
			availability INTEGER,
			is_permanent INTEGER DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS Conversations (
			id INTEGER PRIMARY KEY,
			identity TEXT UNIQUE,
			displayname TEXT,
			creation_timestamp INTEGER,
			type INTEGER,
			last_message_id INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS Messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			convo_id INTEGER,
			chatname TEXT,
			author TEXT,
			from_dispname TEXT,
			body_xml TEXT,
			timestamp INTEGER,
			type INTEGER,
			sending_status INTEGER
		)`,
	}
	for _, sql := range tables {
		db.Exec(sql)
	}

	db.Exec(`CREATE TABLE IF NOT EXISTS Transfers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type INTEGER,
		partner_handle TEXT,
		partner_dispname TEXT,
		status INTEGER,
		filename TEXT,
		filepath TEXT,
		filesize INTEGER,
		bytestransferred INTEGER,
		convo_id INTEGER
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS Profile (
		key TEXT PRIMARY KEY,
		value TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS KeyPins (
		skypename TEXT PRIMARY KEY,
		fingerprint TEXT NOT NULL,
		public_key BLOB NOT NULL,
		first_seen INTEGER NOT NULL
	)`)

	s := &PhazeApp{
		App:              a,
		ChatWindows:      make(map[string]fyne.Window),
		CallWindows:      make(map[string]fyne.Window),
		DB:               db,
		DBPath:           dbPath,
		CompactMode:          false,
		SoundEnabled:         true,
		NotificationsEnabled: true,
		ChatLogs:         make(map[string]*fyne.Container),
		ChatTypingLabels: make(map[string]*widget.Label),
		TypingTimers:     make(map[string]*time.Timer),
		LastTypingSent:   make(map[string]time.Time),
		UnreadCounts:     make(map[string]int),
		Calls:            chat.NewCallManager(),
		Infra:            PhazeInfra,
		PeerKeys:         make(map[string]*[32]byte),
		ConvoMembers:     make(map[string][]string),
		Spaces:           make([]ui.SpaceInfo, 0),
		Channels:         make(map[string][]ui.ChannelInfo),
		ChannelHistory:   make(map[string][]ui.ChannelMsg),
	}

	s.Sentinel = sentinel.NewSentinel(func(issue string) {
		s.App.SendNotification(fyne.NewNotification("Sentinel System", "Repairing "+issue+"..."))
	})

	// Initialize Slicer with the master spritesheet
	slicer, err := ui.NewAeroSlicer("assets/ui_master_spritesheet.png")
	if err == nil {
		s.Slicer = slicer
	} else {
		log.Printf("Failed to load spritesheet: %v", err)
	}

	s.Calls.OnFile = func(peerName string, fileName string, totalSize int, data []byte) {
		home, _ := os.UserHomeDir()
		downloadPath := filepath.Join(home, "Downloads", fileName)
		if err := os.WriteFile(downloadPath, data, 0644); err != nil {
			log.Printf("save file: %v", err)
			return
		}

		// Persist the transfer record (Phaze-7-compatible schema)
		s.DB.Exec(`INSERT INTO Transfers (type, partner_handle, partner_dispname, status, filename, filepath, filesize, bytestransferred)
			VALUES (2, ?, ?, 8, ?, ?, ?, ?)`,
			peerName, peerName, fileName, downloadPath, totalSize, len(data))

		// Also add to message history so it survives restart
		ts := time.Now().Unix()
		label := "[File Received: " + fileName + "]"
		s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
			VALUES (?, ?, ?, ?, 68)`, peerName, peerName, label, ts)

		s.App.SendNotification(fyne.NewNotification(
			"File received from "+peerName,
			fileName+" ("+humanSize(totalSize)+") saved to Downloads",
		))
		if logContainer, ok := s.ChatLogs[peerName]; ok {
			logContainer.Add(ui.NewMessageBubble(peerName, label, false, s.Slicer))
			logContainer.Refresh()
		}
	}

	// Audio Init
	speaker.Init(44100, 44100/10)

	// Periodic Idle Check
	s.OpenWindows = make(map[string]fyne.Window)
	s.LastActivity = time.Now()
	go func() {
		for {
			time.Sleep(30 * time.Second)
			if time.Since(s.LastActivity) > 5*time.Minute && !s.isAway && s.Status == "Online" {
				s.isAway = true
				s.SendMessage(NexusMessage{Type: "status_update", Sender: s.Username, Body: "Away"})
			} else if time.Since(s.LastActivity) < 5*time.Minute && s.isAway {
				s.isAway = false
				s.SendMessage(NexusMessage{Type: "status_update", Sender: s.Username, Body: "Online"})
			}
		}
	}()

	s.setupTray()
	return s
}

func (s *PhazeApp) handleSearch(query string) {
	if query == "" {
		return
	}
	log.Printf("[Directory] Searching for: %s", query)
	s.SendMessage(NexusMessage{
		Type:   "search",
		Sender: s.Username,
		Body:   query,
	})
}

func (s *PhazeApp) requestPeerKey(peer string) {
	s.SendMessage(NexusMessage{
		Type:      "key_request",
		Sender:    s.Username,
		Recipient: peer,
	})
}

// encryptForPeer wraps a field with E2EE: prefix when peer key is known.
// Returns plaintext unchanged if no key — caller should require a key for
// privacy-sensitive flows.
func (s *PhazeApp) encryptForPeer(plain, recipient string) string {
	if plain == "" || s.PrivKey == nil || recipient == "" {
		return plain
	}
	peerPub, ok := s.PeerKeys[recipient]
	if !ok {
		return plain
	}
	enc, err := crypto.Encrypt([]byte(plain), peerPub, s.PrivKey)
	if err != nil {
		return plain
	}
	return "E2EE:" + hex.EncodeToString(enc)
}

// decryptFromPeer is the symmetric of encryptForPeer. Returns input
// unchanged if not E2EE-wrapped. Returns empty string if decryption fails.
func (s *PhazeApp) decryptFromPeer(field, sender string) string {
	if !strings.HasPrefix(field, "E2EE:") {
		return field
	}
	if s.PrivKey == nil || s.PeerKeys[sender] == nil {
		return ""
	}
	encrypted, err := hex.DecodeString(field[5:])
	if err != nil {
		return ""
	}
	plain, err := crypto.Decrypt(encrypted, s.PeerKeys[sender], s.PrivKey)
	if err != nil {
		return ""
	}
	return string(plain)
}

// acceptPeerKey applies TOFU (trust-on-first-use) pinning. First key seen
// for a peer is stored; subsequent keys must match or are rejected.
func (s *PhazeApp) acceptPeerKey(peer string, pk *[32]byte, fpHint string) {
	if peer == "" || pk == nil {
		return
	}
	fp := crypto.Fingerprint(pk)
	if fpHint != "" && fpHint != fp {
		log.Printf("[Sovereign] WARNING: %s sent fingerprint %s but key hashes to %s — discarding", peer, fpHint, fp)
		return
	}

	var pinnedFP string
	row := s.DB.QueryRow("SELECT fingerprint FROM KeyPins WHERE skypename = ?", peer)
	err := row.Scan(&pinnedFP)

	switch {
	case err != nil: // first time we've seen this peer
		_, ierr := s.DB.Exec(
			"INSERT INTO KeyPins (skypename, fingerprint, public_key, first_seen) VALUES (?, ?, ?, ?)",
			peer, fp, pk[:], time.Now().Unix(),
		)
		if ierr != nil {
			log.Printf("[Sovereign] pin insert failed for %s: %v", peer, ierr)
			return
		}
		s.PeerKeys[peer] = pk
		log.Printf("[Sovereign] Pinned new identity for %s (fp: %s)", peer, fp)
	case pinnedFP == fp:
		s.PeerKeys[peer] = pk
	default:
		log.Printf("[Sovereign] !! KEY MISMATCH for %s — pinned %s, got %s — REJECTING", peer, pinnedFP, fp)
		s.App.SendNotification(fyne.NewNotification(
			"Identity Mismatch",
			peer+"'s key changed. Possible MITM. Message rejected.",
		))
	}
}

func (s *PhazeApp) ShowProfileWindow(username string) {
	win := s.App.NewWindow("Profile: " + username)
	win.Resize(fyne.NewSize(350, 500))

	avatar := ui.NewAvatarWithStatus(120, "Online", "")

	// Fetch forensic details from local DB (reconstruction of contacts.skype.com data)
	var mood, phone, email, location, bday string
	s.DB.QueryRow("SELECT mood_text FROM Contacts WHERE skypename = ?", username).Scan(&mood)

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Phaze Name", Widget: widget.NewLabel(username)},
			{Text: "Mood", Widget: widget.NewLabel(mood)},
			{Text: "Mobile", Widget: widget.NewLabel(phone)},
			{Text: "Email", Widget: widget.NewLabel(email)},
			{Text: "Location", Widget: widget.NewLabel(location)},
			{Text: "Birthday", Widget: widget.NewLabel(bday)},
		},
	}

	win.SetContent(container.NewPadded(container.NewVBox(
		container.NewCenter(avatar),
		widget.NewSeparator(),
		form,
		layout.NewSpacer(),
		container.NewHBox(
			widget.NewButton("Send Message", func() { s.handleChatOpen(username); win.Close() }),
			widget.NewButton("Call", func() { s.StartCall(username); win.Close() }),
		),
	)))
	win.Show()
}

func (s *PhazeApp) ShowBuyCreditDialog() {
	dialog.ShowInformation("Phaze — call credit",
		"In-app purchase of PSTN credit is not implemented yet.\n\n"+
			"When your Nexus operator enables Telnyx, carrier calls are billed to that account — not through this client.\n\n"+
			"Phaze-to-Phaze voice uses WebRTC in a chat and does not use call credit.",
		s.MainWindow)
}

// ---------- Network ----------

func (s *PhazeApp) ConnectToServer(password string) error {
	res, err := s.connect(password, "", "")
	if err != nil {
		return err
	}
	if res.Status != "ok" {
		if res.Error != "" {
			return fmt.Errorf("%s", res.Error)
		}
		return fmt.Errorf("authentication failed")
	}
	return nil
}

func (s *PhazeApp) connect(password, totpCode, sessionToken string) (authResult, error) {
	s.ConnMu.Lock()
	defer s.ConnMu.Unlock()

	if s.Conn != nil {
		s.Conn.Close()
	}

	// Production-only fallbacks. Local dev hits the override path below by
	// setting ServerAddress via Settings; we don't probe ws://localhost in
	// the general flow because end users don't run a local Nexus and the
	// failed handshake delays first connect.
	targets := []string{
		s.Infra.Gateway,
		"wss://phazechat.world/ws",
	}
	if s.ServerAddress != "" && s.ServerAddress != s.Infra.Gateway {
		targets = append([]string{s.ServerAddress}, targets...)
	}

	var c *websocket.Conn
	var err error
	for _, addr := range targets {
		log.Printf("[Mesh] Attempting connection to %s...", addr)
		dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
		c, _, err = dialer.Dial(addr, nil)
		if err == nil {
			s.ServerAddress = addr
			log.Printf("[Mesh] Connected via %s", addr)
			break
		}
	}

	if err != nil {
		return authResult{}, fmt.Errorf("could not reach any Phaze Nexus: %w", err)
	}
	s.Conn = c

	s.authChan = make(chan authResult, 1)

	host, _ := os.Hostname()
	device := runtime.GOOS + "/" + host

	var authMsg NexusMessage
	if sessionToken != "" {
		authMsg = NexusMessage{Type: "session_auth", QRToken: sessionToken, DeviceInfo: device}
	} else {
		authMsg = NexusMessage{Type: "auth", Sender: s.Username, Body: password, TOTPCode: totpCode, DeviceInfo: device}
	}
	s.Conn.WriteJSON(authMsg)

	go s.ReadLoop()

	s.ConnMu.Unlock()
	defer s.ConnMu.Lock()

	select {
	case res := <-s.authChan:
		if res.Status != "ok" {
			return res, nil
		}
		if res.SessionToken != "" {
			s.SessionToken = res.SessionToken
			keyring.Set(sessionKeyringService, s.Username, res.SessionToken)
		}
		s.PlaySound("Login.wav")

		// Forensic Crypto Identity loading
		var pub, priv []byte
		err = s.DB.QueryRow("SELECT public_key, private_key FROM Accounts WHERE skypename = ?", s.Username).Scan(&pub, &priv)
		if err != nil || len(pub) == 0 {
			log.Println("[Sovereign] Generating new forensic key pair...")
			kp, _ := crypto.GenerateKeyPair()
			s.PubKey = kp.Public
			s.PrivKey = kp.Private
			s.DB.Exec("UPDATE Accounts SET public_key = ?, private_key = ? WHERE skypename = ?", kp.Public[:], kp.Private[:], s.Username)
		} else {
			var pK, sK [32]byte
			copy(pK[:], pub)
			copy(sK[:], priv)
			s.PubKey = &pK
			s.PrivKey = &sK
			log.Println("[Sovereign] Forensic keys loaded.")
		}

		// Sharing PubKey with Nexus and Peers
		go s.SendMessage(NexusMessage{
			Type:           "presence",
			Sender:         s.Username,
			Status:         "Online",
			PublicKey:      s.PubKey[:],
			KeyFingerprint: crypto.Fingerprint(s.PubKey),
		})

		// Announce on DHT
		if s.P2P == nil {
			p2p, err := chat.NewP2PManager(s.Username)
			if err == nil {
				s.P2P = p2p
				s.P2P.SetStreamHandler(func(data []byte) {
					var msg NexusMessage
					if err := json.Unmarshal(data, &msg); err == nil {
						s.HandleIncomingMessage(msg)
					}
				})
				s.P2P.Announce()
			} else {
				log.Printf("[P2P] Failed to start DHT manager: %v", err)
			}
		}

		// Activate Self-Healing Sentinel — prefer session-token reconnect, fall back to password
		pass, _ := keyring.Get(keyringService, s.Username)
		s.Sentinel.Watch(func() error {
			if tok, err := keyring.Get(sessionKeyringService, s.Username); err == nil && tok != "" {
				if r, err := s.connect("", "", tok); err == nil && r.Status == "ok" {
					return nil
				}
			}
			return s.ConnectToServer(pass)
		})

		// Register FCM push token (Android only; no-op on desktop).
		// Re-check every 30 minutes in case the token refreshes.
		go func() {
			for {
				if tok := readFCMToken(); tok != "" {
					s.SendMessage(NexusMessage{Type: "register_fcm_token", Body: tok})
				}
				time.Sleep(30 * time.Minute)
			}
		}()

		// Check for Sovereign Updates
		go s.CheckForUpdates()

		return res, nil
	case <-time.After(5 * time.Second):
		return authResult{}, fmt.Errorf("authentication timeout")
	}
}

func (s *PhazeApp) CheckForUpdates() {
	m, err := updater.Fetch(s.Infra.API)
	if err != nil {
		return // Silent fail for offline mesh
	}
	if m.Version == "" || !updater.IsNewer(m.Version, Version) {
		return
	}

	// SendNotification fires regardless of in-app install path — same
	// behaviour as before.
	s.App.SendNotification(fyne.NewNotification("Phaze update", "Version "+m.Version+" is available."))

	asset := m.PlatformAssetFor(updater.CurrentPlatformKey())
	openDownloadPage := func() {
		releaseURL := m.ReleaseURL
		if releaseURL == "" {
			releaseURL = m.URL
		}
		if releaseURL == "" {
			releaseURL = s.Infra.API + "/download"
		}
		if u, err := url.Parse(releaseURL); err == nil {
			s.App.OpenURL(u)
		}
	}

	// Fall back to the legacy "open browser" flow when the server didn't
	// publish a verifiable asset for our platform.
	if asset == nil || asset.SHA256 == "" {
		dialog.ShowConfirm("Phaze update",
			"Version "+m.Version+" is available. Open the download page?",
			func(ok bool) {
				if ok {
					openDownloadPage()
				}
			}, s.MainWindow)
		return
	}

	notes := m.ReleaseNotes
	if len(notes) > 600 {
		notes = notes[:600] + "…"
	}
	prompt := "Version " + m.Version + " is available (" + humanBytes(asset.Size) + ").\n\n"
	if notes != "" {
		prompt += notes + "\n\n"
	}
	prompt += "Phaze will download and verify it (SHA-256), then replace the running binary on next launch."

	dialog.ShowConfirm("Phaze update", prompt, func(ok bool) {
		if !ok {
			return
		}
		go s.performUpdate(*asset)
	}, s.MainWindow)
}

func (s *PhazeApp) performUpdate(asset updater.PlatformAsset) {
	progress := dialog.NewInformation("Phaze update", "Downloading "+asset.Name+"…", s.MainWindow)
	progress.Show()
	defer progress.Hide()

	path, err := updater.DownloadAndVerify(asset, updater.StagingDir())
	if err != nil {
		dialog.ShowError(fmt.Errorf("update download failed: %w", err), s.MainWindow)
		return
	}
	err = updater.Apply(path)
	if apkPath, needs := updater.AsNeedsUserAction(err); needs {
		// Android: hand the verified APK to the OS package installer.
		u, perr := url.Parse("file://" + apkPath)
		if perr == nil {
			s.App.OpenURL(u)
		}
		dialog.ShowInformation("Phaze update",
			"Update downloaded. Approve the install prompt from Android to apply it.",
			s.MainWindow)
		return
	}
	if err != nil {
		dialog.ShowError(fmt.Errorf("update install failed: %w", err), s.MainWindow)
		return
	}
	dialog.ShowInformation("Phaze update",
		"Update staged. Quit Phaze when ready — the new version will start automatically.",
		s.MainWindow)
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "size unknown"
	}
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.2f GiB", float64(n)/(k*k*k))
	}
}

func (s *PhazeApp) SendMessage(msg NexusMessage) {
	// Transparent E2EE for any field carrying privacy-sensitive content
	// to a known recipient. Body for chat; SDP/Candidate for call signaling.
	if s.PubKey != nil && msg.Recipient != "" {
		_, haveKey := s.PeerKeys[msg.Recipient]
		needsCrypto := msg.Type == "msg" ||
			msg.Type == "call_offer" ||
			msg.Type == "call_answer" ||
			msg.Type == "ice_candidate"
		if needsCrypto && !haveKey {
			log.Printf("[Sovereign] No key for %s, requesting...", msg.Recipient)
			go s.requestPeerKey(msg.Recipient)
		}
		if haveKey {
			if msg.Body != "" {
				msg.Body = s.encryptForPeer(msg.Body, msg.Recipient)
			}
			if msg.SDP != "" {
				msg.SDP = s.encryptForPeer(msg.SDP, msg.Recipient)
			}
			if msg.Candidate != "" {
				msg.Candidate = s.encryptForPeer(msg.Candidate, msg.Recipient)
			}
		}
	}

	s.ConnMu.Lock()
	conn := s.Conn
	s.ConnMu.Unlock()

	sent := false
	if conn != nil {
		if err := conn.WriteJSON(msg); err == nil {
			sent = true
			if msg.Type == "msg" {
				s.PlaySound("MessageOutgoing.wav")
			}
		}
	}

	if !sent {
		log.Printf("[P2P] Nexus down or send failed for recipient %s, trying DHT fallback", msg.Recipient)
		if s.P2P != nil {
			data, _ := json.Marshal(msg)
			go func() {
				if err := s.P2P.SendSignal(msg.Recipient, data); err != nil {
					log.Printf("[P2P] DHT fallback failed: %v", err)
				} else {
					log.Printf("[P2P] DHT fallback success for %s", msg.Recipient)
				}
			}()
		}
	}
}

// uploadFile uploads data as a multipart form to /api/v1/upload and returns
// the URL path returned by the server (e.g. "/uploads/abc123.wav").
func (s *PhazeApp) uploadFile(fileName string, data []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return "", fmt.Errorf("write form file: %w", err)
	}
	w.Close()

	req, err := http.NewRequest("POST", s.Infra.API+"/api/v1/upload", &buf)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+s.SessionToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	return result.URL, nil
}

func (s *PhazeApp) ReadLoop() {
	for {
		var msg NexusMessage
		err := s.Conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Connection lost:", err)
			// Attempt reconnect
			go func() {
				for i := 0; i < 10; i++ {
					time.Sleep(time.Duration(2<<uint(i)) * time.Second)
					if i > 5 {
						time.Sleep(30 * time.Second)
					}
					log.Printf("Reconnect attempt %d...", i+1)
					pass, _ := keyring.Get(keyringService, s.Username)
					if err := s.ConnectToServer(pass); err == nil {
						log.Println("Reconnected!")
						return
					}
				}
				log.Println("Failed to reconnect after 10 attempts")
			}()
			return
		}

		s.HandleIncomingMessage(msg)
	}
}

func (s *PhazeApp) HandleIncomingMessage(msg NexusMessage) {
	if s.Sentinel != nil {
		s.Sentinel.Heartbeat()
	}
	// Transparent decrypt of any E2EE-wrapped fields. No-op for cleartext.
	if msg.Sender != "" {
		if msg.Body != "" {
			msg.Body = s.decryptFromPeer(msg.Body, msg.Sender)
		}
		if msg.SDP != "" {
			msg.SDP = s.decryptFromPeer(msg.SDP, msg.Sender)
		}
		if msg.Candidate != "" {
			msg.Candidate = s.decryptFromPeer(msg.Candidate, msg.Sender)
		}
	}
	switch msg.Type {
	case "auth_result":
		res := authResult{Status: msg.Status, SessionToken: msg.QRToken, Error: msg.Error}
		if msg.Status == "ok" {
			log.Println("Authenticated with Nexus")
			s.PlaySound("Login.wav")
			s.Username = msg.Sender
			if msg.TurnConfig != nil {
				log.Println("[Sovereign] Captured Dynamic Media Token.")
				s.Calls.SetICEServers(msg.TurnConfig)
			}
			// Sync settings from server
			go s.SendMessage(NexusMessage{Type: "settings_get", Sender: msg.Sender})
			// Fetch Spaces
			go s.SendMessage(NexusMessage{Type: "server_list", Sender: msg.Sender})
		} else {
			log.Println("Auth failed:", msg.Error, "status:", msg.Status)
		}
		select {
		case s.authChan <- res:
		default:
		}

	case "totp_result":
		fyne.Do(func() {
			switch msg.Status {
			case "pending_confirm":
				s.showTOTPEnrollDialog(msg.TOTPURI)
			case "enabled":
				dialog.ShowInformation("Two-Factor Authentication", "2FA is now enabled on this account.", s.MainWindow)
			case "disabled":
				dialog.ShowInformation("Two-Factor Authentication", "2FA has been disabled.", s.MainWindow)
			default:
				if msg.Error != "" {
					dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
				}
			}
		})

	case "qr_login_result":
		if msg.Status == "approved" {
			fyne.Do(func() {
				dialog.ShowInformation("Phaze", "Sign-in approved on the other device.", s.MainWindow)
			})
		} else if msg.Error != "" {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
			})
		}

	case "link_result":
		fyne.Do(func() {
			if msg.Status == "ok" {
				dialog.ShowInformation("Link New Device", "One-time Link Code: "+msg.Token+"\n\nEnter this code on the device you want to sign in. It will expire in 5 minutes.", s.MainWindow)
			} else if msg.Status == "approved" {
				dialog.ShowInformation("Link New Device", "Device approved successfully.", s.MainWindow)
			} else if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("Link failed: %s", msg.Error), s.MainWindow)
			}
		})

	case "key_backup":
		if msg.Status == "ok" && msg.KeyBackup != nil {
			fyne.Do(func() {
				s.showRestoreBackupDialog(msg.KeyBackup)
			})
		}
	case "key_backup_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("Backup operation failed: %s", msg.Error), s.MainWindow)
			} else {
				msgText := "E2EE Backup operation successful."
				if msg.Status == "stored" {
					msgText = "E2EE key backup saved successfully."
				} else if msg.Status == "deleted" {
					msgText = "E2EE key backup deleted successfully."
				}
				dialog.ShowInformation("Backup", msgText, s.MainWindow)
			}
		})

	case "forgot_password_result":
		fyne.Do(func() {
			dialog.ShowInformation("Phaze", "If an account matches, a reset link has been emailed.", s.MainWindow)
		})

	case "reset_password_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
			} else {
				dialog.ShowInformation("Phaze", "Password reset. You can log in with your new password.", s.MainWindow)
			}
		})

	case "verify_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
			} else {
				dialog.ShowInformation("Phaze", "Email verified. You can now sign in.", s.MainWindow)
			}
		})

	case "update_result":
		if msg.Error != "" {
			fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow) })
		}

	case "phone_link_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
				return
			}
			switch msg.Status {
			case "code_sent":
				dialog.ShowInformation("Phaze", "Verification code sent to your phone.", s.MainWindow)
			case "verified":
				dialog.ShowInformation("Phaze", "Phone number linked.", s.MainWindow)
			}
		})

	case "pstn_status":
		if msg.Error != "" {
			fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow) })
		} else if msg.Status != "" {
			log.Printf("[pstn] %s", msg.Status)
		}

	case "block_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
			} else {
				dialog.ShowInformation("Phaze", msg.Recipient+" has been blocked.", s.MainWindow)
			}
		})

	case "blocks":
		log.Printf("[block] server reports %d blocks", len(msg.Results))

	case "report_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
			} else {
				dialog.ShowInformation("Phaze", "Report received. Our team will review it.", s.MainWindow)
			}
		})

	case "session_revoked":
		log.Printf("[session] revoked by server")

	case "convo_error":
		fyne.Do(func() {
			dialog.ShowError(fmt.Errorf("group chat: %s", msg.Error), s.MainWindow)
		})

	case "msg":
		s.PlaySound("MessageIncoming.wav")
		bodyText := msg.Body
		if bodyText == "" {
			bodyText = "[Encrypted Message - Handshaking...]"
		}

		ts := time.Now().Unix()
		s.DB.Exec("INSERT INTO Messages (chatname, author, body_xml, timestamp, type) VALUES (?, ?, ?, ?, 61)",
			msg.Sender, msg.Sender, bodyText, ts)

		// Always fire an OS notification for incoming DMs — the OS handles
		// suppression when the app is foregrounded on macOS/Windows.
		// (Linux generally surfaces them regardless; that's the platform
		// contract.) Suppress only when the user actively muted notifications.
		if s.NotificationsEnabled {
			s.App.SendNotification(fyne.NewNotification(
				"Phaze — "+msg.Sender,
				bodyText,
			))
		}

		if logContainer, ok := s.ChatLogs[msg.Sender]; ok {
			sender := msg.Sender
			msgID := msg.MsgID
			fyne.Do(func() {
				bubble := ui.NewMessageBubbleEx(sender, bodyText, false, s.Slicer, ui.MessageBubbleOpts{
					Kind:      msg.Kind,
					Reactions: msg.Reactions,
					OnReact: func(emoji string) {
						s.SendMessage(NexusMessage{
							Type:      "msg_react",
							Sender:    s.Username,
							Recipient: sender,
							MsgID:     msgID,
							Reaction:  emoji,
						})
					},
				})
				logContainer.Add(bubble)
				logContainer.Refresh()
			})
		} else {
			s.UnreadCounts[msg.Sender]++
		}

	case "typing":
		if lbl, ok := s.ChatTypingLabels[msg.Sender]; ok {
			lbl.SetText(msg.Sender + " is typing...")
			lbl.Show()
			if timer, tok := s.TypingTimers[msg.Sender]; tok {
				timer.Stop()
			}
			s.TypingTimers[msg.Sender] = time.AfterFunc(3*time.Second, func() {
				lbl.Hide()
			})
		}

	case "global_notice":
		// Admin broadcast to every connected client — show a popup.
		if msg.Body != "" {
			body := msg.Body
			fyne.Do(func() {
				s.PlaySound("MessageReceived.wav")
				dialog.ShowInformation("📢 Phaze Announcement", body, s.MainWindow)
			})
		}

	case "search_results":
		s.Discovered = msg.Results
		if s.SearchResult != nil {
			s.SearchResult.Refresh()
		}

	case "presence":
		s.updateFriendStatus(msg.Sender, msg.Status)
		s.setFriendSupporter(msg.Sender, msg.Supporter)
		if len(msg.PublicKey) == 32 {
			var pk [32]byte
			copy(pk[:], msg.PublicKey)
			s.acceptPeerKey(msg.Sender, &pk, msg.KeyFingerprint)
		}
		if msg.Status == "Online" {
			s.PlaySound("FriendOnline.wav")
		}

	case "key_request":
		if s.PubKey != nil && msg.Sender != "" {
			log.Printf("[Sovereign] %s requested our key, sending...", msg.Sender)
			go s.SendMessage(NexusMessage{
				Type:           "presence",
				Sender:         s.Username,
				Recipient:      msg.Sender,
				Status:         s.Status,
				PublicKey:      s.PubKey[:],
				KeyFingerprint: crypto.Fingerprint(s.PubKey),
			})
		}

	case "friend_status":
		s.updateFriendStatus(msg.Sender, msg.Status)
		if msg.Status == "Online" {
			s.PlaySound("FriendOnline.wav")
		}

	case "profile_update":
		s.updateFriendProfile(msg.Sender, msg.DisplayName, msg.Mood)

	case "friend_request":
		s.PlaySound("MessageReceived.wav")
		s.PendingInbound = append(s.PendingInbound, msg.Sender)
		s.App.SendNotification(fyne.NewNotification(
			"Friend Request",
			msg.Sender+" wants to add you as a contact",
		))
		if s.MainWindow != nil {
			s.showFriendRequestDialog(msg.Sender)
		}

	case "friend_accepted":
		s.PlaySound("MessageReceived.wav")
		s.DB.Exec("INSERT OR IGNORE INTO Contacts (skypename, availability) VALUES (?, 1)", msg.Sender)
		s.loadFriends()
		if s.ContactList != nil {
			s.ContactList.Refresh()
		}
		s.App.SendNotification(fyne.NewNotification(
			"Friend Added",
			msg.Sender+" accepted your friend request",
		))

	case "pending_requests":
		s.PendingInbound = msg.Results
		for _, requester := range msg.Results {
			s.showFriendRequestDialog(requester)
		}

	case "call_offer":
		s.PlaySound("CallIncoming.wav")

		iceConfig := webrtc.Configuration{
			ICEServers: s.Calls.ICEServers,
		}

		s.showIncomingCallDialog(msg.Sender, iceConfig, msg.SDP)

	case "call_answer":
		log.Printf("Call answered by %s", msg.Sender)

	case "call_reject", "call_end":
		s.PlaySound("CallHangup.wav")
		if win, ok := s.CallWindows[msg.Sender]; ok {
			win.Close()
			delete(s.CallWindows, msg.Sender)
		}

	case "call_error":
		s.App.SendNotification(fyne.NewNotification("Call Failed", msg.Error))

	case "ice_candidate":
		log.Printf("ICE candidate from %s", msg.Sender)

	case "msg_status":
		if msg.Body == "delivered_offline" {
			log.Printf("Message to %s stored for offline delivery", msg.Sender)
		}

	case "kicked":
		s.App.SendNotification(fyne.NewNotification("Phaze", msg.Body))
		log.Println("Kicked:", msg.Body)

	case "friend_removed":
		s.DB.Exec("DELETE FROM Contacts WHERE skypename = ?", msg.Sender)
		s.loadFriends()
		if s.ContactList != nil {
			s.ContactList.Refresh()
		}

	case "convo_info", "convo_created":
		s.DB.Exec(`INSERT OR REPLACE INTO Conversations (identity, displayname, type)
			VALUES (?, ?, 2)`, msg.ConvoID, msg.ConvoName)
		if len(msg.Members) > 0 {
			s.ConvoMembers[msg.ConvoID] = msg.Members
		}
		if msg.Type == "convo_created" {
			s.App.SendNotification(fyne.NewNotification(
				"New Group Chat",
				msg.Sender+" added you to "+msg.ConvoName,
			))
		}

	case "convo_msg":
		s.PlaySound("MessageReceived.wav")
		ts := time.Now().Unix()
		s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
			VALUES (?, ?, ?, ?, 61)`, msg.ConvoID, msg.Sender, msg.Body, ts)
		if logContainer, ok := s.ChatLogs[msg.ConvoID]; ok {
			sender := msg.Sender
			convoID := msg.ConvoID
			msgID := msg.MsgID
			bodyText2 := msg.Body
			fyne.Do(func() {
				bubble := ui.NewMessageBubbleEx(sender, bodyText2, false, s.Slicer, ui.MessageBubbleOpts{
					Kind:      msg.Kind,
					Reactions: msg.Reactions,
					OnReact: func(emoji string) {
						s.SendMessage(NexusMessage{
							Type:      "msg_react",
							Sender:    s.Username,
							Recipient: convoID,
							MsgID:     msgID,
							Reaction:  emoji,
						})
					},
				})
				logContainer.Add(bubble)
				logContainer.Refresh()
			})
		} else {
			s.UnreadCounts[msg.ConvoID]++
			s.App.SendNotification(fyne.NewNotification(
				msg.Sender+" in group",
				msg.Body,
			))
		}

	case "convo_left":
		if logContainer, ok := s.ChatLogs[msg.ConvoID]; ok {
			logContainer.Add(ui.NewMessageBubble("system", msg.Sender+" left the conversation", false, s.Slicer))
			logContainer.Refresh()
		}

	case "msg_react":
		// Apply reaction to the message bubble if the chat is open
		if logContainer, ok := s.ChatLogs[msg.Sender]; ok {
			fyne.Do(func() {
				logContainer.Add(ui.NewMessageBubble("", msg.Sender+" reacted "+msg.Reaction, false, s.Slicer))
				logContainer.Refresh()
			})
		} else if logContainer, ok := s.ChatLogs[msg.Recipient]; ok {
			fyne.Do(func() {
				logContainer.Add(ui.NewMessageBubble("", msg.Sender+" reacted "+msg.Reaction, false, s.Slicer))
				logContainer.Refresh()
			})
		}

	case "edit_msg":
		// Update in DB and show edited indicator in the chat log
		s.DB.Exec(`UPDATE Messages SET body_xml = ? WHERE chatname = ? AND body_xml LIKE ?`,
			msg.Body+"  (edited)", msg.Sender, "%")
		chatKey := msg.Sender
		if msg.ConvoID != "" {
			chatKey = msg.ConvoID
		}
		if logContainer, ok := s.ChatLogs[chatKey]; ok {
			fyne.Do(func() {
				logContainer.Add(ui.NewMessageBubble(msg.Sender, "(edited) "+msg.Body, false, s.Slicer))
				logContainer.Refresh()
			})
		}

	case "delete_msg":
		chatKey := msg.Sender
		if msg.ConvoID != "" {
			chatKey = msg.ConvoID
		}
		if logContainer, ok := s.ChatLogs[chatKey]; ok {
			fyne.Do(func() {
				logContainer.Add(ui.NewMessageBubble(msg.Sender, "(message deleted)", false, s.Slicer))
				logContainer.Refresh()
			})
		}

	case "read_receipt":
		log.Printf("%s read our message %s", msg.Sender, msg.Body)

	case "register_result":
		if msg.Error != "" {
			log.Println("Registration failed:", msg.Error)
		} else {
			log.Println("Registration successful")
		}

	case "purge_email_result":
		fyne.Do(func() {
			if msg.Error != "" {
				dialog.ShowError(fmt.Errorf("%s", msg.Error), s.MainWindow)
			} else {
				dialog.ShowInformation("Phaze", "Email removed from your account.", s.MainWindow)
			}
		})

	case "server_list_result":
		s.handleServerListResult(msg)
	case "server_info_result":
		s.handleServerInfoResult(msg)
	case "channel_history_result":
		s.handleChannelHistoryResult(msg)
	case "channel_msg_in":
		s.handleChannelMsgIn(msg)
	case "server_created", "server_joined":
		go s.SendMessage(NexusMessage{Type: "server_list", Sender: s.Username})

	case "settings_result":
		if msg.Body != "" {
			var prefs map[string]interface{}
			if err := json.Unmarshal([]byte(msg.Body), &prefs); err == nil {
				if v, ok := prefs["sound_enabled"].(bool); ok {
					s.SoundEnabled = v
				}
				if v, ok := prefs["notifications_enabled"].(bool); ok {
					s.NotificationsEnabled = v
				}
				if v, ok := prefs["compact_mode"].(bool); ok {
					s.CompactMode = v
				}
				log.Printf("[settings] synced from server")
			}
		}

	case "stream_list_result":
		s.LiveStreams = nil
		for i := 0; i+1 < len(msg.Results); i += 2 {
			s.LiveStreams = append(s.LiveStreams, ui.LiveStream{Host: msg.Results[i], Title: msg.Results[i+1]})
		}

	case "remote_input":
		if msg.Body != "" {
			evt, err := remote.ParseInputEvent([]byte(msg.Body))
			if err == nil {
				if err := remote.InjectInput(evt); err != nil {
					log.Printf("[remote] inject: %v", err)
				}
			}
		}

	default:
		log.Printf("[ws] unhandled message type %q from %s", msg.Type, msg.Sender)
	}
}

// ---------- Friend Management ----------

func (s *PhazeApp) loadFriends() {
	s.Friends = nil
	// Built-in Echo Service
	s.Friends = append(s.Friends, ui.FriendInfo{
		Username:    "Echo / Sound Test Service",
		DisplayName: "Echo / Sound Test Service",
		Status:      "Online",
		Mood:        "Call me to test your microphone.",
	})

	rows, err := s.DB.Query("SELECT skypename, displayname, mood_text, availability FROM Contacts ORDER BY availability DESC, skypename ASC")
	if err != nil {
		log.Printf("[DB] loadFriends: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f ui.FriendInfo
		var avail int
		rows.Scan(&f.Username, &f.DisplayName, &f.Mood, &avail)
		// Map availability to Skype 7 status strings
		switch avail {
		case 1:
			f.Status = "Online"
		case 2:
			f.Status = "Away"
		case 3:
			f.Status = "Do Not Disturb"
		default:
			f.Status = "Offline"
		}
		s.Friends = append(s.Friends, f)
	}
}

func (s *PhazeApp) updateFriendStatus(username, status string) {
	avail := 0
	switch status {
	case "Online":
		avail = 1
	case "Away":
		avail = 2
	case "Do Not Disturb":
		avail = 3
	}
	s.DB.Exec("UPDATE Contacts SET availability = ? WHERE skypename = ?", avail, username)
	for i, f := range s.Friends {
		if f.Username == username {
			s.Friends[i].Status = status
			if s.Sidebar != nil {
				s.Sidebar.Refresh()
			}
			break
		}
	}
}

// setFriendSupporter marks a friend as a Phaze supporter so the sidebar can
// render the 💜 badge. Only ever sets the flag on (presence carries it when true).
func (s *PhazeApp) setFriendSupporter(username string, supporter bool) {
	if !supporter {
		return
	}
	for i, f := range s.Friends {
		if f.Username == username {
			if !s.Friends[i].Supporter {
				s.Friends[i].Supporter = true
				if s.Sidebar != nil {
					s.Sidebar.Refresh()
				}
			}
			break
		}
	}
}

func (s *PhazeApp) updateFriendProfile(username, displayName, mood string) {
	s.DB.Exec("UPDATE Contacts SET displayname = ?, mood_text = ? WHERE skypename = ?", displayName, mood, username)
	for i, f := range s.Friends {
		if f.Username == username {
			s.Friends[i].DisplayName = displayName
			s.Friends[i].Mood = mood
			if s.Sidebar != nil {
				s.Sidebar.Refresh()
			}
			break
		}
	}
}

func (s *PhazeApp) showFriendRequestDialog(from string) {
	if s.MainWindow == nil {
		return
	}
	dialog.ShowConfirm("Friend Request",
		from+" wants to add you as a contact.\nAccept?",
		func(accept bool) {
			if accept {
				s.SendMessage(NexusMessage{Type: "friend_accept", Sender: from})
				s.DB.Exec("INSERT OR IGNORE INTO Contacts (skypename, availability) VALUES (?, 1)", from)
				s.loadFriends()
				if s.ContactList != nil {
					s.ContactList.Refresh()
				}
			} else {
				s.SendMessage(NexusMessage{Type: "friend_reject", Sender: from})
			}
			// Drop from pending list
			for i, p := range s.PendingInbound {
				if p == from {
					s.PendingInbound = append(s.PendingInbound[:i], s.PendingInbound[i+1:]...)
					break
				}
			}
		}, s.MainWindow)
}

func (s *PhazeApp) removeContact(name string) {
	dialog.ShowConfirm("Remove Contact",
		"Remove "+name+" from your contacts?",
		func(ok bool) {
			if !ok {
				return
			}
			s.SendMessage(NexusMessage{
				Type:      "friend_remove",
				Sender:    s.Username,
				Recipient: name,
			})
			s.DB.Exec("DELETE FROM Contacts WHERE skypename = ?", name)
			s.loadFriends()
			if s.ContactList != nil {
				s.ContactList.Refresh()
			}
		}, s.MainWindow)
}

// ---------- Sound ----------

func (s *PhazeApp) PlaySound(name string) {
	if !s.SoundEnabled {
		return
	}
	go func() {
		soundPath := ui.ResolveAsset(filepath.Join("assets", "sounds", name))
		var r io.Reader
		var file *os.File
		if f, err := os.Open(soundPath); err == nil {
			info, err := f.Stat()
			if err == nil && info.Size() > 0 {
				file = f
				r = f
			} else {
				f.Close()
			}
		}
		if r == nil {
			b, ok := ui.VaultSoundBytes(name)
			if !ok {
				return
			}
			r = bytes.NewReader(b)
		}
		if file != nil {
			defer file.Close()
		}

		streamer, _, err := wav.Decode(r)
		if err != nil {
			return
		}

		done := make(chan struct{})
		speaker.Play(beep.Seq(streamer, beep.Callback(func() {
			streamer.Close()
			close(done)
		})))
		<-done
	}()
}

// ---------- Calling ----------

func (s *PhazeApp) StartCall(name string) {
	if _, exists := s.CallWindows[name]; exists {
		return
	}

	iceConfig := webrtc.Configuration{
		ICEServers: s.Calls.ICEServers,
	}

	_, offerSDP, err := s.Calls.CreateOffer(name, iceConfig, func(c *webrtc.ICECandidate) {
		candidateBytes, _ := json.Marshal(c.ToJSON())
		s.SendMessage(NexusMessage{
			Type:      "ice_candidate",
			Sender:    s.Username,
			Recipient: name,
			Candidate: string(candidateBytes),
		})
	})
	if err != nil {
		log.Printf("CreateOffer failed: %v", err)
		return
	}

	if err := s.Calls.AddVideoTrack(name); err != nil {
		log.Printf("AddVideoTrack failed: %v (video disabled)", err)
	}

	s.SendMessage(NexusMessage{
		Type:      "call_offer",
		Sender:    s.Username,
		Recipient: name,
		SDP:       offerSDP,
	})

	s.PlaySound("CallOutgoing.wav")
	s.openCallWindow(name, "Calling...")
}

// AnswerCall is the *callee* path: PC already built via HandleOffer; just open window.
func (s *PhazeApp) AnswerCall(name string) {
	if _, exists := s.CallWindows[name]; exists {
		return
	}
	s.openCallWindow(name, "Connecting...")
}

func (s *PhazeApp) openCallWindow(name, initialStatus string) {
	callWin := s.App.NewWindow("Call: " + name)
	callWin.Resize(fyne.NewSize(300, 450))
	callWin.SetFixedSize(true)
	callWin.SetOnClosed(func() {
		delete(s.CallWindows, name)
		s.Calls.OnRemoteVideoFrameFor(name, nil)
		s.SendMessage(NexusMessage{
			Type:      "call_end",
			Sender:    s.Username,
			Recipient: name,
		})
	})
	s.CallWindows[name] = callWin

	localVideo := ui.NewVideoPreview(80, 60)
	remoteVideo := ui.NewVideoPreview(280, 210)

	// Pump locally captured frames to the peer at ~10fps via DataChannel JPEG.
	var lastSent time.Time
	localVideo.OnFrame = func(img image.Image) {
		if time.Since(lastSent) < 100*time.Millisecond {
			return
		}
		lastSent = time.Now()
		if err := s.Calls.WriteVideoFrame(name, img, 100); err != nil {
			log.Printf("[Video] send to %s: %v", name, err)
		}
	}
	localVideo.Start()

	// Display remote frames as they arrive.
	s.Calls.OnRemoteVideoFrameFor(name, func(img image.Image) {
		remoteVideo.Image.Image = img
		remoteVideo.Image.Refresh()
	})

	avatar := ui.NewAvatarWithStatus(128, "Online", s.getFriendAvatar(name))
	statusLabel := widget.NewLabel(initialStatus)
	callTimer := widget.NewLabel("00:00")
	callTimer.Hide()

	var callStart time.Time
	var timerTicker *time.Ticker

	hangupBtn := widget.NewButton("End Call", func() {
		if timerTicker != nil {
			timerTicker.Stop()
		}
		s.SendMessage(NexusMessage{
			Type:      "call_end",
			Sender:    s.Username,
			Recipient: name,
		})
		callWin.Close()
	})
	hangupBtn.Importance = widget.DangerImportance

	muted := false
	var muteBtn *widget.Button
	muteBtn = widget.NewButton("Mute", func() {
		muted = !muted
		if muted {
			muteBtn.SetText("Unmute")
			s.Calls.SetMuted(name, true)
		} else {
			muteBtn.SetText("Mute")
			s.Calls.SetMuted(name, false)
		}
	})

	videoEnabled := true
	var videoBtn *widget.Button
	videoBtn = widget.NewButton("Video On", func() {
		videoEnabled = !videoEnabled
		if videoEnabled {
			videoBtn.SetText("Video On")
			s.Calls.EnableVideo(name, true)
		} else {
			videoBtn.SetText("Video Off")
			s.Calls.EnableVideo(name, false)
		}
	})

	var screenCap *screencap.Capturer
	sharing := false
	var shareBtn *widget.Button
	shareBtn = widget.NewButton("Share Screen", func() {
		if sharing {
			sharing = false
			shareBtn.SetText("Share Screen")
			if screenCap != nil {
				screenCap.Stop()
				screenCap = nil
			}
			return
		}
		sc := screencap.New(10)
		if err := sc.Start(); err != nil {
			log.Printf("[screenshare] %v", err)
			return
		}
		screenCap = sc
		sharing = true
		shareBtn.SetText("Stop Sharing")
		go func() {
			for img := range sc.Frames() {
				if err := s.Calls.WriteVideoFrame(name, img, 60); err != nil {
					log.Printf("[screenshare] send: %v", err)
				}
			}
		}()
	})

	content := container.NewVBox(
		container.NewCenter(container.NewStack(remoteVideo.Container, avatar)),
		container.NewHBox(layout.NewSpacer(), localVideo.Container, layout.NewSpacer()),
		statusLabel,
		callTimer,
		layout.NewSpacer(),
		container.NewHBox(muteBtn, videoBtn, shareBtn, hangupBtn),
	)

	callWin.SetContent(container.NewPadded(content))
	callWin.Show()

	go func() {
		time.Sleep(1 * time.Second)
		statusLabel.SetText("Connected P2P")
		callTimer.Show()
		callStart = time.Now()
		timerTicker = time.NewTicker(1 * time.Second)
		for range timerTicker.C {
			elapsed := time.Since(callStart)
			mins := int(elapsed.Minutes())
			secs := int(elapsed.Seconds()) % 60
			callTimer.SetText(fmt.Sprintf("%02d:%02d", mins, secs))
		}
	}()
}

func (s *PhazeApp) showIncomingCallDialog(from string, config webrtc.Configuration, sdp string) {
	if s.MainWindow == nil {
		return
	}

	s.PlaySound("CallIncoming.wav")

	win := s.App.NewWindow("Incoming Call")
	win.Resize(fyne.NewSize(350, 500))
	win.SetFixedSize(true)

	overlay := ui.NewCallOverlay(from, s.getFriendAvatar(from), true)
	overlay.OnAnswer = func() {
		_, answerSDP, err := s.Calls.HandleOffer(from, config, sdp, func(c *webrtc.ICECandidate) {
			candidateBytes, _ := json.Marshal(c.ToJSON())
			s.SendMessage(NexusMessage{
				Type:      "ice_candidate",
				Sender:    s.Username,
				Recipient: from,
				Candidate: string(candidateBytes),
			})
		})
		if err != nil {
			log.Printf("HandleOffer failed: %v", err)
			return
		}
		s.SendMessage(NexusMessage{
			Type:      "call_answer",
			Sender:    s.Username,
			Recipient: from,
			SDP:       answerSDP,
		})
		s.AnswerCall(from)
		win.Close()
	}
	overlay.OnReject = func() {
		s.SendMessage(NexusMessage{
			Type:      "call_reject",
			Sender:    s.Username,
			Recipient: from,
		})
		win.Close()
	}

	win.SetContent(overlay.Render())
	win.Show()
}

// ---------- Chat Window ----------

func (s *PhazeApp) OpenChat(name string) fyne.CanvasObject {
	isGroup := false
	var convoName string
	s.DB.QueryRow("SELECT displayname FROM Conversations WHERE identity = ? AND type = 2", name).Scan(&convoName)
	if convoName != "" {
		isGroup = true
	}

	title := name
	if isGroup {
		title = convoName
	}

	// 1. Prepare chat logic
	historyContainer := container.NewVBox()
	s.ChatLogs[name] = historyContainer

	// Load history
	rows, err := s.DB.Query("SELECT author, body_xml FROM Messages WHERE chatname = ? ORDER BY timestamp ASC", name)
	if err == nil {
		for rows.Next() {
			var author, body string
			rows.Scan(&author, &body)
			isMe := author == s.Username
			historyContainer.Add(ui.NewMessageBubble(author, body, isMe, s.Slicer))
		}
		rows.Close()
	}

	scroll := container.NewVScroll(historyContainer)

	statusLabel := widget.NewLabelWithStyle("Phaze Encrypted Mesh", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	// Typing indicator
	typingLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	s.ChatTypingLabels[name] = typingLabel
	typingLabel.Hide()

	headerStatus := "Group"
	if !isGroup {
		headerStatus = "Unknown"
		for _, f := range s.Friends {
			if f.Username == name {
				headerStatus = f.Status
				break
			}
		}
	}

	// Build contacts list for @mention autocomplete
	var contactNames []string
	for _, f := range s.Friends {
		contactNames = append(contactNames, f.Username)
	}

	chatProps := ui.ChatViewProps{
		Name:     title,
		Status:   headerStatus,
		IsGroup:  isGroup,
		Slicer:   s.Slicer,
		Contacts: contactNames,
		OnCall:   func() { s.StartCall(name) },
		OnBlock: func() {
			dialog.ShowConfirm("Block User",
				"Block "+name+"? They won't be able to contact you.",
				func(ok bool) {
					if ok {
						s.SendMessage(NexusMessage{
							Type:      "block",
							Sender:    s.Username,
							Recipient: name,
						})
					}
				}, s.MainWindow)
		},
		OnReport: func() {
			reasonEntry := widget.NewMultiLineEntry()
			reasonEntry.SetPlaceHolder("Describe the issue...")
			reasonEntry.SetMinRowsVisible(4)
			d := dialog.NewCustomConfirm("Report Abuse", "Send Report", "Cancel",
				container.NewVBox(
					widget.NewLabel("What's the issue with "+name+"?"),
					reasonEntry,
				), func(ok bool) {
					if ok && strings.TrimSpace(reasonEntry.Text) != "" {
						s.SendMessage(NexusMessage{
							Type:      "report_abuse",
							Sender:    s.Username,
							Recipient: name,
							Body:      strings.TrimSpace(reasonEntry.Text),
						})
					}
				}, s.MainWindow)
			d.Resize(fyne.NewSize(400, 250))
			d.Show()
		},
		OnSend: func(text string) {
			if strings.TrimSpace(text) == "" {
				return
			}
			body := text
			if strings.HasPrefix(text, "/me ") {
				body = "* " + s.Username + " " + strings.TrimPrefix(text, "/me ")
			}
			if isGroup {
				members := s.ConvoMembers[name]
				envelopes := make(map[string]string, len(members))
				for _, m := range members {
					if m == "" || m == s.Username {
						continue
					}
					if _, haveKey := s.PeerKeys[m]; !haveKey {
						go s.requestPeerKey(m)
					}
					envelopes[m] = s.encryptForPeer(body, m)
				}
				s.SendMessage(NexusMessage{
					Type:      "convo_msg",
					Sender:    s.Username,
					ConvoID:   name,
					Envelopes: envelopes,
				})
				ts := time.Now().Unix()
				s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
					VALUES (?, ?, ?, ?, 61)`, name, s.Username, body, ts)
				historyContainer.Add(ui.NewMessageBubble(s.Username, body, true, s.Slicer))
			} else {
				msgID := fmt.Sprintf("%s-%d", s.Username, time.Now().UnixNano())
				s.SendMessage(NexusMessage{Type: "msg", Sender: s.Username, Recipient: name, Body: body, MsgID: msgID})
				msgIDCopy := msgID
				bodyCopy := body
				bubble := ui.NewMessageBubbleEx(s.Username, bodyCopy, true, s.Slicer, ui.MessageBubbleOpts{
					OnEdit: func() {
						entry := widget.NewEntry()
						entry.SetText(bodyCopy)
						dialog.ShowCustomConfirm("Edit Message", "Save", "Cancel", entry, func(ok bool) {
							if ok && entry.Text != "" {
								s.SendMessage(NexusMessage{
									Type:      "edit_msg",
									Sender:    s.Username,
									Recipient: name,
									MsgID:     msgIDCopy,
									Body:      entry.Text,
								})
							}
						}, s.MainWindow)
					},
					OnDelete: func() {
						dialog.ShowConfirm("Delete Message", "Delete this message?", func(ok bool) {
							if ok {
								s.SendMessage(NexusMessage{
									Type:      "delete_msg",
									Sender:    s.Username,
									Recipient: name,
									MsgID:     msgIDCopy,
								})
							}
						}, s.MainWindow)
					},
				})
				historyContainer.Add(bubble)
			}
			historyContainer.Refresh()
			scroll.ScrollToBottom()
		},
		OnSendFile: func() {
			fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err == nil && reader != nil {
					data, _ := io.ReadAll(reader)
					fileName := reader.URI().Name()
					go func() {
						fileURL, err := s.uploadFile(fileName, data)
						if err != nil {
							log.Printf("[upload] file upload failed: %v", err)
							// Fallback to WebRTC data channel for P2P
							if ferr := s.Calls.SendFile(name, fileName, data); ferr != nil {
								log.Printf("[WebRTC] SendFile fallback error: %v", ferr)
							}
							return
						}
						msgID := fmt.Sprintf("%s-%d", s.Username, time.Now().UnixNano())
						msg := NexusMessage{
							Type:      "msg",
							Sender:    s.Username,
							Recipient: name,
							MsgID:     msgID,
							Kind:      "file",
							FileURL:   fileURL,
							FileName:  fileName,
							Body:      "[File: " + fileName + "]",
						}
						s.SendMessage(msg)
						ts := time.Now().Unix()
						s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type) VALUES (?, ?, ?, ?, 68)`,
							name, s.Username, "[File: "+fileName+"]", ts)
						label := "[File: " + fileName + "]\n" + s.Infra.API + fileURL
						fyne.Do(func() {
							historyContainer.Add(ui.NewMessageBubble(s.Username, label, true, s.Slicer))
							scroll.ScrollToBottom()
						})
					}()
				}
			}, s.MainWindow)
			fd.Show()
		},
		OnVoiceRecord: func() {
			s.startVoiceRecording(name, historyContainer, scroll)
		},
		OnTyping: func() {
			if time.Since(s.LastTypingSent[name]) > 3*time.Second {
				s.SendMessage(NexusMessage{Type: "typing", Sender: s.Username, Recipient: name})
				s.LastTypingSent[name] = time.Now()
			}
		},
	}

	chatView := ui.NewChatView(chatProps)
	// Inject the real scroll into the ChatView placeholder
	// The ChatView container is a Border (header, bottom, nil, nil, center)
	// Objects: [0:header, 1:bottom, 2:center]
	chatView.Container.Objects[2] = container.NewBorder(nil, container.NewVBox(statusLabel, typingLabel), nil, nil, scroll)

	return chatView.Container
}

func (s *PhazeApp) OpenChatMobile(name string) fyne.CanvasObject {
	isGroup := false
	var convoName string
	s.DB.QueryRow("SELECT displayname FROM Conversations WHERE identity = ? AND type = 2", name).Scan(&convoName)
	if convoName != "" {
		isGroup = true
	}

	title := name
	if isGroup {
		title = convoName
	}

	historyContainer := container.NewVBox()
	s.ChatLogs[name] = historyContainer

	rows, err := s.DB.Query("SELECT author, body_xml FROM Messages WHERE chatname = ? ORDER BY timestamp ASC", name)
	if err == nil {
		for rows.Next() {
			var author, body string
			rows.Scan(&author, &body)
			isMe := author == s.Username
			historyContainer.Add(ui.NewMessageBubble(author, body, isMe, s.Slicer))
		}
		rows.Close()
	}

	scroll := container.NewVScroll(historyContainer)

	typingLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	s.ChatTypingLabels[name] = typingLabel
	typingLabel.Hide()

	headerStatus := "Group"
	if !isGroup {
		headerStatus = "Unknown"
		for _, f := range s.Friends {
			if f.Username == name {
				headerStatus = f.Status
				break
			}
		}
	}

	var contactNames []string
	for _, f := range s.Friends {
		contactNames = append(contactNames, f.Username)
	}

	chatProps := ui.ChatViewProps{
		Name:     title,
		Status:   headerStatus,
		IsGroup:  isGroup,
		Slicer:   s.Slicer,
		Contacts: contactNames,
		OnBack: func() {
			s.ContentStack.Objects = []fyne.CanvasObject{s.Sidebar}
			s.ContentStack.Refresh()
		},
		OnCall: func() { s.StartCall(name) },
		OnBlock: func() {
			dialog.ShowConfirm("Block User",
				"Block "+name+"?",
				func(ok bool) {
					if ok {
						s.SendMessage(NexusMessage{
							Type:      "block",
							Sender:    s.Username,
							Recipient: name,
						})
					}
				}, s.MainWindow)
		},
		OnReport: func() {
			reasonEntry := widget.NewMultiLineEntry()
			reasonEntry.SetPlaceHolder("Describe the issue...")
			d := dialog.NewCustomConfirm("Report", "Send", "Cancel",
				container.NewVBox(widget.NewLabel("Report "+name+"?"), reasonEntry),
				func(ok bool) {
					if ok && strings.TrimSpace(reasonEntry.Text) != "" {
						s.SendMessage(NexusMessage{
							Type:      "report_abuse",
							Sender:    s.Username,
							Recipient: name,
							Body:      strings.TrimSpace(reasonEntry.Text),
						})
					}
				}, s.MainWindow)
			d.Show()
		},
		OnSend: func(text string) {
			if strings.TrimSpace(text) == "" {
				return
			}
			body := text
			if strings.HasPrefix(text, "/me ") {
				body = "* " + s.Username + " " + strings.TrimPrefix(text, "/me ")
			}
			if isGroup {
				members := s.ConvoMembers[name]
				envelopes := make(map[string]string, len(members))
				for _, m := range members {
					if m == "" || m == s.Username {
						continue
					}
					if _, haveKey := s.PeerKeys[m]; !haveKey {
						go s.requestPeerKey(m)
					}
					envelopes[m] = s.encryptForPeer(body, m)
				}
				s.SendMessage(NexusMessage{
					Type:      "convo_msg",
					Sender:    s.Username,
					ConvoID:   name,
					Envelopes: envelopes,
				})
				ts := time.Now().Unix()
				s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
					VALUES (?, ?, ?, ?, 61)`, name, s.Username, body, ts)
				historyContainer.Add(ui.NewMessageBubble(s.Username, body, true, s.Slicer))
			} else {
				msgID := fmt.Sprintf("%s-%d", s.Username, time.Now().UnixNano())
				s.SendMessage(NexusMessage{Type: "msg", Sender: s.Username, Recipient: name, Body: body, MsgID: msgID})
				historyContainer.Add(ui.NewMessageBubble(s.Username, body, true, s.Slicer))
			}
			historyContainer.Refresh()
			scroll.ScrollToBottom()
		},
		OnSendFile: func() {
			fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err == nil && reader != nil {
					data, _ := io.ReadAll(reader)
					fileName := reader.URI().Name()
					go func() {
						fileURL, err := s.uploadFile(fileName, data)
						if err != nil {
							log.Printf("[upload] %v", err)
							return
						}
						msgID := fmt.Sprintf("%s-%d", s.Username, time.Now().UnixNano())
						s.SendMessage(NexusMessage{
							Type: "msg", Sender: s.Username, Recipient: name,
							MsgID: msgID, Kind: "file", FileURL: fileURL, FileName: fileName,
							Body: "[File: " + fileName + "]",
						})
						fyne.Do(func() {
							historyContainer.Add(ui.NewMessageBubble(s.Username, "[File: "+fileName+"]", true, s.Slicer))
							scroll.ScrollToBottom()
						})
					}()
				}
			}, s.MainWindow)
			fd.Show()
		},
		OnVoiceRecord: func() {
			s.startVoiceRecording(name, historyContainer, scroll)
		},
		OnTyping: func() {
			if time.Since(s.LastTypingSent[name]) > 3*time.Second {
				s.SendMessage(NexusMessage{Type: "typing", Sender: s.Username, Recipient: name})
				s.LastTypingSent[name] = time.Now()
			}
		},
	}

	chatView := ui.NewChatView(chatProps)
	chatView.Container.Objects[2] = container.NewBorder(nil, typingLabel, nil, nil, scroll)

	return chatView.Container
}

func (s *PhazeApp) startVoiceRecording(name string, historyContainer *fyne.Container, scroll *container.Scroll) {
	chat.StartVoiceRecord()

	rec := ui.NewVoiceRecorder(
		func() {
			go func() {
				filePath, err := chat.StopVoiceRecord()
				if err != nil {
					log.Printf("[voice] record error: %v", err)
					return
				}
				data, err := os.ReadFile(filePath)
				os.Remove(filePath)
				if err != nil {
					log.Printf("[voice] read wav: %v", err)
					return
				}
				fileURL, err := s.uploadFile("voice.wav", data)
				if err != nil {
					log.Printf("[voice] upload: %v", err)
					return
				}
				msgID := fmt.Sprintf("%s-%d", s.Username, time.Now().UnixNano())
				s.SendMessage(NexusMessage{
					Type: "msg", Sender: s.Username, Recipient: name,
					MsgID: msgID, Kind: "voice", FileURL: fileURL, FileName: "voice.wav",
					Body: "[Voice Message]",
				})
				ts := time.Now().Unix()
				s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type) VALUES (?, ?, ?, ?, 61)`,
					name, s.Username, "[Voice Message]", ts)
				fyne.Do(func() {
					historyContainer.Add(ui.NewMessageBubble(s.Username, "[Voice Message]", true, s.Slicer))
					scroll.ScrollToBottom()
				})
			}()
		},
		func() {
			go func() {
				chat.StopVoiceRecord()
			}()
		},
	)
	rec.Start()

	fyne.Do(func() {
		historyContainer.Add(rec.Container)
		historyContainer.Refresh()
		scroll.ScrollToBottom()
	})
}

func (s *PhazeApp) CreateHomeView() fyne.CanvasObject {
	var lastMood string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'mood'").Scan(&lastMood)
	return ui.NewPhazeHome(s.Username, lastMood, nil, s.Slicer, func(val string) {
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('mood', ?)", val)
		s.SendMessage(NexusMessage{
			Type:   "status_update",
			Sender: s.Username,
			Status: "Online",
			Body:   val,
		})
	})
}

func (s *PhazeApp) showNewGroupDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("E.g. The Mesh Lords")

	checks := make([]*widget.Check, len(s.Friends))
	items := []fyne.CanvasObject{widget.NewLabelWithStyle("Select members:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})}
	for i, f := range s.Friends {
		c := widget.NewCheck(f.Username, nil)
		checks[i] = c
		items = append(items, c)
	}

	form := container.NewVBox(
		widget.NewLabel("Group name:"),
		nameEntry,
		widget.NewSeparator(),
	)
	for _, o := range items {
		form.Add(o)
	}

	d := dialog.NewCustomConfirm("New Group Chat", "Create", "Cancel",
		container.NewVScroll(form),
		func(ok bool) {
			if !ok || strings.TrimSpace(nameEntry.Text) == "" {
				return
			}
			var members []string
			for i, c := range checks {
				if c.Checked {
					members = append(members, s.Friends[i].Username)
				}
			}
			if len(members) == 0 {
				dialog.ShowInformation("Phaze", "Select at least one contact", s.MainWindow)
				return
			}
			convoID := fmt.Sprintf("convo_%d_%s", time.Now().UnixNano(), s.Username)
			s.SendMessage(NexusMessage{
				Type:      "convo_create",
				Sender:    s.Username,
				ConvoID:   convoID,
				ConvoName: nameEntry.Text,
				Members:   members,
			})
		}, s.MainWindow)
	d.Resize(fyne.NewSize(420, 500))
	d.Show()
}

func (s *PhazeApp) showAddContactDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Enter Phaze name...")

	dialog.ShowForm("Add Contact", "Send Request", "Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Phaze Name", nameEntry),
		},
		func(ok bool) {
			if ok && nameEntry.Text != "" {
				recipient := nameEntry.Text

				// Attempt Nexus first
				s.SendMessage(NexusMessage{
					Type:      "friend_request",
					Sender:    s.Username,
					Recipient: recipient,
				})

				dialog.ShowInformation("Phaze", "Friend request sent to "+recipient, s.MainWindow)
			}
		}, s.MainWindow)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *PhazeApp) getFriendAvatar(name string) string {
	var avatar []byte
	s.DB.QueryRow("SELECT avatar_image FROM Contacts WHERE skypename = ?", name).Scan(&avatar)
	if len(avatar) == 0 {
		return ""
	}
	return string(avatar)
}

func (s *PhazeApp) getFriendStatus(name string) string {
	var status string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'status'").Scan(&status)
	if status == "" {
		return "Offline"
	}
	return status
}

func (s *PhazeApp) ShowMainWindow() {
	s.EnsureForensicKeys()
	s.MainWindow = s.App.NewWindow("Phaze™ - " + s.Username)

	s.loadFriends()

	if ui.IsMobile() {
		s.showMainWindowMobile()
	} else {
		s.showMainWindowDesktop()
	}
}

func (s *PhazeApp) showMainWindowDesktop() {
	s.MainWindow.Resize(fyne.NewSize(1000, 700))

	s.Sidebar = ui.NewPhazeSidebar(ui.SidebarProps{
		Username:        s.Username,
		Status:          "Online",
		AvatarPath:      s.AvatarPath,
		Slicer:          s.Slicer,
		OnChatOpen:      s.handleChatOpen,
		OnAddFriend:     s.showAddContactDialog,
		OnNewGroup:      s.showNewGroupDialog,
		RecentChats:     s.Friends,
		SpacesView:      s.buildSpacesView(),
		OnProfile:       s.ShowMyProfileWindow,
		CompactMode:     s.CompactMode,
		PSTNDialEnabled: os.Getenv("PHAZE_ENABLE_PSTN") == "true",
		OnDialCall: func(number string) {
			s.PlaySound("CallOutgoing.wav")
			s.SendMessage(NexusMessage{
				Type:   "pstn_call",
				Sender: s.Username,
				Body:   number,
			})
			dialog.ShowInformation("Phaze PSTN", "Calling "+number+"...", s.MainWindow)
		},
		OnStatusChange: func(status string) {
			s.SendMessage(NexusMessage{
				Type:   "status_update",
				Sender: s.Username,
				Body:   status,
			})
		},
		OnSearch:   s.handleSearch,
		OnSettings: s.showSettingsWindow,
	})
	s.HomeView = s.CreateHomeView()
	s.ContentStack = container.NewStack(s.HomeView)

	billingNote := widget.NewLabel("PSTN billing: in-app credit not wired — Telnyx on Nexus when enabled.")
	billingNote.Wrapping = fyne.TextWrapWord
	toolbar := container.NewHBox(
		widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
			s.ContentStack.Objects = []fyne.CanvasObject{s.HomeView}
			s.ContentStack.Refresh()
		}),
		widget.NewButtonWithIcon("Stories", theme.MediaPhotoIcon(), func() {
			storiesView := ui.NewStoriesView(s.Infra.API, s.SessionToken)
			s.ContentStack.Objects = []fyne.CanvasObject{storiesView}
			s.ContentStack.Refresh()
		}),
		widget.NewButtonWithIcon("Live", theme.MediaVideoIcon(), func() {
			s.SendMessage(NexusMessage{Type: "stream_list"})
			liveView := ui.NewLiveView(ui.LiveViewProps{
				Streams: s.LiveStreams,
				WebBase: s.Infra.API,
				OnRefresh: func() {
					s.SendMessage(NexusMessage{Type: "stream_list"})
				},
			})
			s.ContentStack.Objects = []fyne.CanvasObject{liveView}
			s.ContentStack.Refresh()
		}),
		layout.NewSpacer(),
		billingNote,
		widget.NewButton("Details", s.ShowBuyCreditDialog),
	)

	s.setupMenu(s.MainWindow)

	split := container.NewHSplit(s.Sidebar, container.NewBorder(toolbar, nil, nil, nil, s.ContentStack))
	split.Offset = 0.25
	s.MainSplit = split

	s.MainWindow.SetContent(split)
	s.MainWindow.Show()
}

func (s *PhazeApp) showMainWindowMobile() {
	s.MainWindow.SetFullScreen(true)

	s.ContentStack = container.NewStack()

	sidebarView := ui.NewPhazeSidebar(ui.SidebarProps{
		Username:   s.Username,
		Status:     "Online",
		AvatarPath: s.AvatarPath,
		Slicer:     s.Slicer,
		SpacesView: s.buildSpacesView(),
		OnChatOpen: func(name string) {
			s.mobileOpenChat(name)
		},
		OnAddFriend: s.showAddContactDialog,
		OnNewGroup:  s.showNewGroupDialog,
		RecentChats: s.Friends,
		OnProfile:   s.ShowMyProfileWindow,
		OnDialCall: func(number string) {
			s.PlaySound("CallOutgoing.wav")
			s.SendMessage(NexusMessage{
				Type:   "pstn_call",
				Sender: s.Username,
				Body:   number,
			})
		},
		OnStatusChange: func(status string) {
			s.SendMessage(NexusMessage{
				Type:   "status_update",
				Sender: s.Username,
				Body:   status,
			})
		},
		OnSearch:   s.handleSearch,
		OnSettings: s.showSettingsWindow,
	})
	s.Sidebar = sidebarView

	s.HomeView = s.CreateHomeView()

	chatsTab := sidebarView
	homeTab := s.HomeView

	storiesTab := widget.NewLabel("Tap to load stories")
	storiesTabWrap := container.NewStack(storiesTab)

	liveTab := widget.NewLabel("Tap to load live streams")
	liveTabWrap := container.NewStack(liveTab)

	tabContent := container.NewStack(chatsTab)

	bottomNav := container.NewGridWithColumns(4,
		widget.NewButtonWithIcon("Chats", theme.MailComposeIcon(), func() {
			tabContent.Objects = []fyne.CanvasObject{chatsTab}
			tabContent.Refresh()
		}),
		widget.NewButtonWithIcon("Home", theme.HomeIcon(), func() {
			tabContent.Objects = []fyne.CanvasObject{homeTab}
			tabContent.Refresh()
		}),
		widget.NewButtonWithIcon("Stories", theme.MediaPhotoIcon(), func() {
			storiesTabWrap.Objects = []fyne.CanvasObject{ui.NewStoriesView(s.Infra.API, s.SessionToken)}
			storiesTabWrap.Refresh()
			tabContent.Objects = []fyne.CanvasObject{storiesTabWrap}
			tabContent.Refresh()
		}),
		widget.NewButtonWithIcon("Live", theme.MediaVideoIcon(), func() {
			s.SendMessage(NexusMessage{Type: "stream_list"})
			liveTabWrap.Objects = []fyne.CanvasObject{ui.NewLiveView(ui.LiveViewProps{
				Streams: s.LiveStreams,
				WebBase: s.Infra.API,
				OnRefresh: func() {
					s.SendMessage(NexusMessage{Type: "stream_list"})
				},
			})}
			liveTabWrap.Refresh()
			tabContent.Objects = []fyne.CanvasObject{liveTabWrap}
			tabContent.Refresh()
		}),
	)

	s.ContentStack = tabContent

	root := container.NewBorder(nil, bottomNav, nil, nil, tabContent)
	s.MainWindow.SetContent(root)
	s.MainWindow.Show()
}

func (s *PhazeApp) mobileOpenChat(name string) {
	if name == "Echo / Sound Test Service" {
		s.showEchoCallDialog()
		return
	}

	view := s.OpenChatMobile(name)
	s.ContentStack.Objects = []fyne.CanvasObject{view}
	s.ContentStack.Refresh()
}

func (s *PhazeApp) rebuildSidebar() {
	chatOpen := s.handleChatOpen
	if ui.IsMobile() {
		chatOpen = func(name string) { s.mobileOpenChat(name) }
	}

	s.Sidebar = ui.NewPhazeSidebar(ui.SidebarProps{
		Username:        s.Username,
		Status:          "Online",
		AvatarPath:      s.AvatarPath,
		Slicer:          s.Slicer,
		OnChatOpen:      chatOpen,
		OnChatWindow:    s.handleChatWindowOpen,
		OnAddFriend:     s.showAddContactDialog,
		OnNewGroup:      s.showNewGroupDialog,
		RecentChats:     s.Friends,
		SpacesView:      s.buildSpacesView(),
		OnProfile:       s.ShowMyProfileWindow,
		CompactMode:     s.CompactMode,
		PSTNDialEnabled: os.Getenv("PHAZE_ENABLE_PSTN") == "true",
		OnDialCall: func(number string) {
			s.PlaySound("CallOutgoing.wav")
			s.SendMessage(NexusMessage{
				Type:   "pstn_call",
				Sender: s.Username,
				Body:   number,
			})
			dialog.ShowInformation("Phaze PSTN", "Calling "+number+"...", s.MainWindow)
		},
		OnStatusChange: func(status string) {
			s.SendMessage(NexusMessage{
				Type:   "status_update",
				Sender: s.Username,
				Body:   status,
			})
		},
		OnSearch:   s.handleSearch,
		OnSettings: s.showSettingsWindow,
	})

	if ui.IsMobile() {
		if s.ContentStack != nil {
			s.ContentStack.Objects = []fyne.CanvasObject{s.Sidebar}
			s.ContentStack.Refresh()
		}
	} else if s.MainSplit != nil {
		s.MainSplit.Leading = s.Sidebar
		s.MainSplit.Refresh()
	}
}

type RecentChat struct {
	Name    string
	LastMsg string
}

func (s *PhazeApp) loadRecentChats() []ui.FriendInfo {
	// Add the Phaze Echo Service if not present
	chats := []ui.FriendInfo{
		{Username: "Echo / Sound Test Service", Status: "Online", DisplayName: "Echo / Sound Test Service"},
	}

	rows, err := s.DB.Query(`
		SELECT chatname FROM Messages
		WHERE id IN (SELECT MAX(id) FROM Messages GROUP BY chatname)
		ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return chats
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		rows.Scan(&name)
		if name == "Echo / Sound Test Service" {
			continue
		}
		// Fetch full info
		found := false
		for _, f := range s.Friends {
			if f.Username == name {
				chats = append(chats, f)
				found = true
				break
			}
		}
		if !found {
			chats = append(chats, ui.FriendInfo{Username: name, Status: "Offline"})
		}
	}
	return chats
}

func (s *PhazeApp) setupMenu(win fyne.Window) {
	phazeMenu := fyne.NewMenu("Phaze™",
		fyne.NewMenuItem("Online Status", nil),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Privacy...", func() {}),
		fyne.NewMenuItem("Sign Out", func() { s.ShowLoginWindow() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Close", func() { win.Close() }),
	)

	contactsMenu := fyne.NewMenu("Contacts",
		fyne.NewMenuItem("Add Contact...", func() {}),
		fyne.NewMenuItem("Create New Group...", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show Outlook Contacts", func() {}),
	)

	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Contacts", func() {}),
		fyne.NewMenuItem("Recent", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Compact View", func() {
			s.CompactMode = !s.CompactMode
			s.rebuildSidebar()
		}),
		fyne.NewMenuItem("Split Window Mode", func() {
			s.SplitMode = !s.SplitMode
		}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Check for Updates", func() {}),
		fyne.NewMenuItem("Support Phaze 💜", func() {
			if u, err := url.Parse("https://buymeacoffee.com/phazeworld"); err == nil {
				s.App.OpenURL(u)
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About Phaze™", s.showAboutDialog),
	)

	toolsMenu := fyne.NewMenu("Tools",
		fyne.NewMenuItem("Options...", s.showSettingsWindow),
	)

	win.SetMainMenu(fyne.NewMainMenu(phazeMenu, contactsMenu, viewMenu, toolsMenu, helpMenu))
}

// ---------- Options Window ----------

func (s *PhazeApp) ShowOptionsWindow() {
	win := s.App.NewWindow("Phaze™ - Options")
	win.Resize(fyne.NewSize(700, 500))

	categories := []string{"General", "Privacy", "Notifications", "Audio & Video", "Advanced"}
	catList := widget.NewList(
		func() int { return len(categories) },
		func() fyne.CanvasObject { return widget.NewLabel("Category") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(categories[i])
		},
	)

	contentArea := container.NewStack(widget.NewLabel("Select a category on the left"))

	catList.OnSelected = func(id widget.ListItemID) {
		cat := categories[id]
		switch cat {
		case "General":
			compactCheck := widget.NewCheck("Launch in compact mode", func(val bool) {
				s.CompactMode = val
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('compact_mode', ?)", boolStr(val))
			})
			compactCheck.SetChecked(s.CompactMode)

			soundCheck := widget.NewCheck("Enable sounds", func(val bool) {
				s.SoundEnabled = val
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_sounds', ?)", boolStr(val))
			})
			soundCheck.SetChecked(s.SoundEnabled)

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					compactCheck,
					soundCheck,
				),
			}
		case "Audio & Video":
			preview := ui.NewVideoPreview(320, 240)
			preview.Start()

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Audio Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					widget.NewLabel("Microphone: Default (System)"),
					widget.NewLabel("Speakers: Default (System)"),
					widget.NewSeparator(),
					widget.NewLabelWithStyle("Video Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					container.NewCenter(preview.Container),
					widget.NewLabel("Camera: Integrated Webcam"),
				),
			}
		case "Privacy":
			callPolicy := widget.NewRadioGroup([]string{
				"Allow calls from anyone",
				"Allow calls from people in my Contacts only",
			}, func(val string) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('privacy_calls', ?)", val)
			})
			var cur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'privacy_calls'").Scan(&cur)
			if cur != "" {
				callPolicy.SetSelected(cur)
			}

			webStatus := widget.NewCheck("Allow my status to be shown on the web", func(v bool) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('privacy_web_status', ?)", boolStr(v))
			})
			var webCur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'privacy_web_status'").Scan(&webCur)
			webStatus.SetChecked(webCur == "1")

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Privacy Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					callPolicy,
					webStatus,
				),
			}
		case "Notifications":
			desk := widget.NewCheck("Show desktop notifications for messages", func(v bool) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_desktop', ?)", boolStr(v))
			})
			var dcur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_desktop'").Scan(&dcur)
			desk.SetChecked(dcur != "0")

			sounds := widget.NewCheck("Play sounds for incoming messages", func(v bool) {
				s.SoundEnabled = v
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_sounds', ?)", boolStr(v))
			})
			sounds.SetChecked(s.SoundEnabled)

			callNotify := widget.NewCheck("Show notification for incoming calls", func(v bool) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_calls', ?)", boolStr(v))
			})
			var ccur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_calls'").Scan(&ccur)
			callNotify.SetChecked(ccur != "0")

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Notification Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					desk, sounds, callNotify,
				),
			}
		case "Advanced":
			addrEntry := widget.NewEntry()
			addrEntry.SetText(s.ServerAddress)

			saveBtn := widget.NewButton("Apply", func() {
				s.ServerAddress = addrEntry.Text
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
				log.Println("Server updated:", s.ServerAddress)
			})

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Connection", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					container.NewBorder(nil, nil, widget.NewLabel("Server:"), saveBtn, addrEntry),
					widget.NewSeparator(),
					widget.NewLabel("Private Nexus Protocol v1.0"),
					widget.NewLabel("All messages are relayed through your Nexus server."),
					widget.NewLabel("For end-to-end encryption, both clients must support it."),
				),
			}
		}
		contentArea.Refresh()
	}

	split := container.NewHSplit(catList, container.NewPadded(contentArea))
	split.Offset = 0.3

	win.SetContent(split)
	win.Show()
}

// ---------- Login & Registration ----------

func (s *PhazeApp) ShowLoginWindow() {
	win := s.App.NewWindow("Phaze™ - Sign In")
	if ui.IsMobile() {
		win.SetFullScreen(true)
	} else {
		win.Resize(fyne.NewSize(400, 600))
		win.SetFixedSize(true)
	}

	logoSize := fyne.NewSize(200, 100)
	if ui.IsMobile() {
		logoSize = fyne.NewSize(140, 70)
	}
	logo := canvas.NewImageFromFile(ui.ResolveAsset("assets/phaze_logo.png"))
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(logoSize)

	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("Phaze Name")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Password")

	serverEntry := widget.NewEntry()
	serverEntry.SetText("wss://phazechat.world/ws")

	// Load saved credentials
	var savedUser, savedServer string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'username'").Scan(&savedUser)
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'server'").Scan(&savedServer)
	if savedUser != "" {
		usernameEntry.SetText(savedUser)
	}
	if savedServer != "" {
		serverEntry.SetText(savedServer)
	}

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	finishLogin := func(pass string) {
		if pass != "" {
			keyring.Set(keyringService, s.Username, pass)
		}
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('username', ?)", s.Username)
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
		s.PlaySound("Login.wav")
		s.ShowMainWindow()
		s.CheckForUpdates()
		win.Close()
	}

	var attemptLogin func(pass string)
	attemptLogin = func(pass string) {
		res, err := s.connect(pass, "", "")
		if err != nil {
			statusLabel.SetText("Error: " + err.Error())
			return
		}
		switch res.Status {
		case "ok":
			finishLogin(pass)
		case "totp_required":
			s.promptTOTP(win, func(code string) {
				r, err := s.connect(pass, code, "")
				if err != nil {
					statusLabel.SetText("Error: " + err.Error())
					return
				}
				if r.Status != "ok" {
					statusLabel.SetText("Invalid 2FA code")
					statusLabel.Show()
					attemptLogin(pass)
					return
				}
				finishLogin(pass)
			})
		default:
			msg := res.Error
			if msg == "" {
				msg = "authentication failed"
			}
			statusLabel.SetText("Error: " + msg)
			statusLabel.Show()
		}
	}

	// Defend against stale local-dev configs: if the saved server points at
	// localhost we silently upgrade to the production Nexus. Users who do
	// run a local server can re-enter it via Settings → Server.
	if strings.Contains(savedServer, "localhost") || strings.Contains(savedServer, "127.0.0.1") {
		log.Printf("[startup] dropping stale localhost server %q in favor of phazechat.world", savedServer)
		savedServer = "wss://phazechat.world/ws"
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", savedServer)
	}
	// Auto-login: prefer stored session token, then password
	if savedUser != "" && savedServer != "" {
		s.Username = savedUser
		s.ServerAddress = savedServer
		if tok, err := keyring.Get(sessionKeyringService, savedUser); err == nil && tok != "" {
			go func() {
				time.Sleep(500 * time.Millisecond)
				if r, err := s.connect("", "", tok); err == nil && r.Status == "ok" {
					s.ShowMainWindow()
					s.CheckForUpdates()
					win.Close()
				}
			}()
		} else if pass, err := keyring.Get(keyringService, savedUser); err == nil && pass != "" {
			passwordEntry.SetText(pass)
			go func() {
				time.Sleep(500 * time.Millisecond)
				attemptLogin(pass)
			}()
		}
	}

	loginBtn := widget.NewButton("Sign In", func() {
		if usernameEntry.Text == "" || passwordEntry.Text == "" {
			statusLabel.SetText("Please enter username and password")
			statusLabel.Show()
			return
		}
		s.Username = usernameEntry.Text
		statusLabel.SetText("Connecting to Phaze...")
		statusLabel.Show()
		attemptLogin(passwordEntry.Text)
	})
	loginBtn.Importance = widget.HighImportance

	var loginContent fyne.CanvasObject
	createBtn := widget.NewButton("Create Account", func() {
		s.showRegistrationWindow(win, serverEntry.Text, func() {
			win.SetTitle("Phaze™ - Sign In")
			win.SetContent(loginContent)
		})
	})

	forgotBtn := widget.NewButton("Forgot password?", func() {
		s.showForgotPasswordDialog(win)
	})
	forgotBtn.Importance = widget.LowImportance

	qrBtn := widget.NewButton("Sign in with QR", func() {
		s.showQRLoginDialog(win)
	})
	qrBtn.Importance = widget.LowImportance

	linkCodeBtn := widget.NewButton("Sign in with Link Code", func() {
		s.showLinkCodeLoginDialog(win)
	})
	linkCodeBtn.Importance = widget.LowImportance

	loginContent = container.NewCenter(
		container.NewVBox(
			container.NewCenter(logo),
			widget.NewLabelWithStyle("Phaze: Private & Safe", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewLabelWithStyle("Stay in phase.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
			widget.NewLabel("Sign in to your account"),
			container.NewPadded(usernameEntry),
			container.NewPadded(passwordEntry),
			statusLabel,
			container.NewPadded(loginBtn),
			container.NewCenter(createBtn),
			container.NewCenter(qrBtn),
			container.NewCenter(linkCodeBtn),
			container.NewCenter(forgotBtn),
			layout.NewSpacer(),
			widget.NewLabelWithStyle("Version "+Version, fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
		),
	)
	win.SetContent(loginContent)
	win.Show()
}

func (s *PhazeApp) showTOTPEnrollDialog(uri string) {
	uriLabel := widget.NewLabel(uri)
	uriLabel.Wrapping = fyne.TextWrapBreak
	codeEntry := widget.NewEntry()
	codeEntry.SetPlaceHolder("6-digit code from authenticator")

	var qrContainer fyne.CanvasObject
	if pngBytes, err := qrcode.Encode(uri, qrcode.Medium, 200); err == nil {
		img := canvas.NewImageFromReader(bytes.NewReader(pngBytes), "totp_qr.png")
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(200, 200))
		qrContainer = container.NewCenter(img)
	} else {
		qrContainer = widget.NewLabel("Could not generate QR code image locally.")
	}

	d := dialog.NewCustomConfirm("Enable 2FA", "Confirm", "Cancel",
		container.NewVBox(
			widget.NewLabel("Scan this QR code or add the URI manually to Google Authenticator / Authy:"),
			qrContainer,
			uriLabel,
			widget.NewLabel("Then enter the current 6-digit code to confirm:"),
			codeEntry,
		), func(ok bool) {
			if !ok || codeEntry.Text == "" {
				return
			}
			s.SendMessage(NexusMessage{Type: "confirm_totp", Sender: s.Username, TOTPCode: strings.TrimSpace(codeEntry.Text)})
		}, s.MainWindow)
	d.Resize(fyne.NewSize(500, 480))
	d.Show()
}

func (s *PhazeApp) promptTOTP(parent fyne.Window, onCode func(code string)) {
	codeEntry := widget.NewEntry()
	codeEntry.SetPlaceHolder("6-digit code")
	d := dialog.NewCustomConfirm("Two-Factor Authentication", "Verify", "Cancel",
		container.NewVBox(
			widget.NewLabel("Enter the code from your authenticator app:"),
			codeEntry,
		), func(ok bool) {
			if ok && codeEntry.Text != "" {
				onCode(strings.TrimSpace(codeEntry.Text))
			}
		}, parent)
	d.Show()
}

func (s *PhazeApp) showQRLoginDialog(parent fyne.Window) {
	addr := s.ServerAddress
	if addr == "" {
		addr = "wss://phazechat.world/ws"
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	c, _, err := dialer.Dial(addr, nil)
	if err != nil {
		dialog.ShowError(err, parent)
		return
	}

	c.WriteJSON(NexusMessage{Type: "qr_login_create"})
	var first NexusMessage
	if err := c.ReadJSON(&first); err != nil || first.Error != "" || first.QRToken == "" {
		c.Close()
		dialog.ShowError(fmt.Errorf("could not start QR login: %s", first.Error), parent)
		return
	}

	status := widget.NewLabel("Scan this QR code using a signed-in device (or approve via settings):")
	var qrContainer fyne.CanvasObject
	if pngBytes, err := qrcode.Encode(first.QRData, qrcode.Medium, 200); err == nil {
		img := canvas.NewImageFromReader(bytes.NewReader(pngBytes), "login_qr.png")
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(200, 200))
		qrContainer = container.NewCenter(img)
	} else {
		qrContainer = widget.NewLabel("Could not generate QR code image locally.")
	}

	linkLabel := widget.NewLabel(first.QRData)
	linkLabel.Wrapping = fyne.TextWrapBreak
	tokenLabel := widget.NewLabel("Token: " + first.QRToken)
	copyBtn := widget.NewButton("Copy link", func() {
		s.App.Clipboard().SetContent(first.QRData)
	})

	content := container.NewVBox(status, qrContainer, linkLabel, tokenLabel, copyBtn)
	stop := make(chan struct{})

	d := dialog.NewCustom("Sign in with QR", "Cancel", content, parent)
	d.SetOnClosed(func() {
		close(stop)
		c.Close()
	})
	d.Resize(fyne.NewSize(460, 480))
	d.Show()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := c.WriteJSON(NexusMessage{Type: "qr_login_check", QRToken: first.QRToken}); err != nil {
					return
				}
				var r NexusMessage
				if err := c.ReadJSON(&r); err != nil {
					return
				}
				if r.Type == "auth_result" && r.Status == "ok" {
					s.ConnMu.Lock()
					if s.Conn != nil {
						s.Conn.Close()
					}
					s.Conn = c
					s.Username = r.Sender
					s.ServerAddress = addr
					if r.QRToken != "" {
						s.SessionToken = r.QRToken
						keyring.Set(sessionKeyringService, s.Username, r.QRToken)
					}
					if r.TurnConfig != nil {
						s.Calls.SetICEServers(r.TurnConfig)
					}
					s.authChan = make(chan authResult, 1)
					s.ConnMu.Unlock()
					go s.ReadLoop()
					fyne.Do(func() {
						d.Hide()
						s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('username', ?)", s.Username)
						s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
						s.PlaySound("Login.wav")
						s.ShowMainWindow()
						s.CheckForUpdates()
						parent.Close()
					})
					return
				}
			}
		}
	}()
}

func (s *PhazeApp) showLinkCodeLoginDialog(parent fyne.Window) {
	addr := s.ServerAddress
	if addr == "" {
		addr = "wss://phazechat.world/ws"
	}

	tokenEntry := widget.NewEntry()
	tokenEntry.SetPlaceHolder("Enter Link Code / QR Token")

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	var d dialog.Dialog
	var stop chan struct{}
	var conn *websocket.Conn

	startPolling := func() {
		tok := strings.TrimSpace(tokenEntry.Text)
		if tok == "" {
			statusLabel.SetText("Please enter a code")
			statusLabel.Show()
			return
		}
		if idx := strings.Index(tok, "token="); idx >= 0 {
			tok = tok[idx+len("token="):]
			if endIdx := strings.Index(tok, "&"); endIdx >= 0 {
				tok = tok[:endIdx]
			}
		}

		statusLabel.SetText("Connecting to server...")
		statusLabel.Show()

		dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
		c, _, err := dialer.Dial(addr, nil)
		if err != nil {
			statusLabel.SetText("Error connecting: " + err.Error())
			return
		}
		conn = c

		statusLabel.SetText("Waiting for approval on another device...")
		stop = make(chan struct{})

		go func() {
			ticker := time.NewTicker(2500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					if err := c.WriteJSON(NexusMessage{Type: "link_check", Token: tok}); err != nil {
						fyne.Do(func() {
							statusLabel.SetText("Connection lost: " + err.Error())
						})
						return
					}
					var r NexusMessage
					if err := c.ReadJSON(&r); err != nil {
						fyne.Do(func() {
							statusLabel.SetText("Error reading response")
						})
						return
					}
					if r.Type == "link_check" && r.Status == "approved" {
						u := r.Sender
						sess := r.QRToken
						if u == "" || sess == "" {
							fyne.Do(func() {
								statusLabel.SetText("Invalid response: missing session details")
							})
							return
						}

						s.ConnMu.Lock()
						if s.Conn != nil {
							s.Conn.Close()
						}
						s.Conn = c
						s.Username = u
						s.ServerAddress = addr
						s.SessionToken = sess
						keyring.Set(sessionKeyringService, s.Username, sess)
						s.authChan = make(chan authResult, 1)
						s.ConnMu.Unlock()

						go s.ReadLoop()

						fyne.Do(func() {
							d.Hide()
							s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('username', ?)", s.Username)
							s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
							s.PlaySound("Login.wav")
							s.ShowMainWindow()
							s.CheckForUpdates()
							parent.Close()
						})
						// Request E2EE key backup from server
						go s.SendMessage(NexusMessage{Type: "key_backup_get", Sender: s.Username})
						return
					} else if r.Error != "" {
						fyne.Do(func() {
							statusLabel.SetText("Error: " + r.Error)
						})
						return
					}
				}
			}
		}()
	}

	scanQRFromFile := func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, parent)
				return
			}
			if reader == nil {
				return
			}
			defer reader.Close()

			img, _, err := image.Decode(reader)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to decode image: %w", err), parent)
				return
			}

			codes, err := goqr.Recognize(img)
			if err != nil || len(codes) == 0 {
				dialog.ShowError(fmt.Errorf("no QR code found in image"), parent)
				return
			}

			tok := string(codes[0].Payload)
			if idx := strings.Index(tok, "token="); idx >= 0 {
				tok = tok[idx+len("token="):]
				if endIdx := strings.Index(tok, "&"); endIdx >= 0 {
					tok = tok[:endIdx]
				}
			}

			fyne.Do(func() {
				tokenEntry.SetText(tok)
				statusLabel.SetText("✓ Scanned token: " + tok)
				statusLabel.Show()
			})
		}, parent)
		fd.Show()
	}

	content := container.NewVBox(
		widget.NewLabel("Enter the Link Code or QR Token shown on your other device:"),
		tokenEntry,
		statusLabel,
		container.NewHBox(
			widget.NewButton("Sign In with Code", startPolling),
			widget.NewButton("Scan QR from Image", scanQRFromFile),
		),
	)

	d = dialog.NewCustom("Sign in with Link Code", "Cancel", content, parent)
	d.SetOnClosed(func() {
		if stop != nil {
			close(stop)
		}
		if conn != nil {
			conn.Close()
		}
	})
	d.Resize(fyne.NewSize(400, 250))
	d.Show()
}

func (s *PhazeApp) showForgotPasswordDialog(parent fyne.Window) {
	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("Phaze Name")
	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("Email on file")
	d := dialog.NewCustomConfirm("Reset Password", "Send Reset Email", "Cancel",
		container.NewVBox(
			widget.NewLabel("We'll email a reset link to the address on file."),
			userEntry, emailEntry,
		), func(ok bool) {
			if !ok || userEntry.Text == "" || emailEntry.Text == "" {
				return
			}
			addr := s.ServerAddress
			if addr == "" {
				addr = "wss://phazechat.world/ws"
			}
			c, _, err := websocket.DefaultDialer.Dial(addr, nil)
			if err != nil {
				dialog.ShowError(err, parent)
				return
			}
			defer c.Close()
			c.WriteJSON(NexusMessage{
				Type:   "forgot_password",
				Sender: strings.TrimSpace(userEntry.Text),
				Email:  strings.TrimSpace(emailEntry.Text),
			})
			dialog.ShowInformation("Phaze", "If the account exists, a reset link has been emailed.", parent)
		}, parent)
	d.Show()
}

// showRegistrationWindow swaps the given window's content to a registration
// form. When the user clicks Back (or completes registration), the prior
// content is restored. Keeps the whole login/register/verify flow inside a
// single OS window instead of spawning new ones.
func (s *PhazeApp) showRegistrationWindow(win fyne.Window, serverAddr string, restore func()) {
	win.SetTitle("Phaze™ - Create Account")

	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("Choose a Phaze name")

	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("Email address")

	moodEntry := widget.NewEntry()
	moodEntry.SetPlaceHolder("Your mood (optional)")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Choose a password")

	confirmEntry := widget.NewPasswordEntry()
	confirmEntry.SetPlaceHolder("Confirm password")

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	registerBtn := widget.NewButton("Create Account", func() {
		if usernameEntry.Text == "" || emailEntry.Text == "" {
			statusLabel.SetText("Username and Email are required")
			statusLabel.Show()
			return
		}
		if !strings.Contains(emailEntry.Text, "@") {
			statusLabel.SetText("Invalid email address")
			statusLabel.Show()
			return
		}
		if len(passwordEntry.Text) < 8 {
			statusLabel.SetText("Password must be at least 8 characters")
			statusLabel.Show()
			return
		}
		if passwordEntry.Text != confirmEntry.Text {
			statusLabel.SetText("Passwords do not match")
			statusLabel.Show()
			return
		}

		// Always register against the production Nexus unless the user has
		// explicitly pointed the client at a different (e.g. self-hosted)
		// server via Settings. No localhost fallback — that only worked
		// when a Nexus happened to be running on the same machine.
		addr := s.ServerAddress
		if addr == "" || strings.Contains(addr, "localhost") {
			addr = "wss://phazechat.world/ws"
		}

		c, _, err := websocket.DefaultDialer.Dial(addr, nil)
		if err != nil {
			statusLabel.SetText("Cannot connect to " + addr)
			statusLabel.Show()
			return
		}

		c.WriteJSON(NexusMessage{
			Type:   "register",
			Sender: usernameEntry.Text,
			Body:   passwordEntry.Text,
			Email:  emailEntry.Text,
			Mood:   moodEntry.Text,
		})

		var result NexusMessage
		c.ReadJSON(&result)
		c.Close()

		if result.Status == "pending_verification" {
			s.showEmailVerificationDialog(usernameEntry.Text, win)
		} else if result.Error != "" {
			dialog.ShowError(fmt.Errorf("%s", result.Error), win)
		} else {
			dialog.ShowInformation("Registration Success", "Account created! You can now sign in.", win)
			restore()
		}
	})
	registerBtn.Importance = widget.HighImportance

	backBtn := widget.NewButton("← Back to Sign In", func() { restore() })
	backBtn.Importance = widget.LowImportance

	win.SetContent(container.NewCenter(
		container.NewVBox(
			widget.NewLabelWithStyle("Create Your Account", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			container.NewPadded(usernameEntry),
			container.NewPadded(emailEntry),
			container.NewPadded(moodEntry),
			container.NewPadded(passwordEntry),
			container.NewPadded(confirmEntry),
			statusLabel,
			container.NewPadded(registerBtn),
			container.NewCenter(backBtn),
		),
	))
}

// ---------- Main ----------

// initSentry wires error reporting when SENTRY_DSN is set. Same shape as
// the nexus side — no-op when DSN unset so we ship safely without it.
func initSentry() {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return
	}
	env := os.Getenv("SENTRY_ENVIRONMENT")
	if env == "" {
		env = "production"
	}
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      env,
		TracesSampleRate: 0.05,
		ServerName:       "phaze-native",
	})
}

func main() {
	initSentry()
	defer sentry.Flush(2 * time.Second)
	defer sentry.Recover()

	phaze := NewPhazeApp()
	phaze.App.Settings().SetTheme(&ui.Phaze7Theme{})

	// Load saved avatar + settings
	var savedAvatar string
	phaze.DB.QueryRow("SELECT value FROM Profile WHERE key = 'avatar'").Scan(&savedAvatar)
	if savedAvatar != "" {
		phaze.AvatarPath = savedAvatar
	}
	var soundVal string
	phaze.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_sounds'").Scan(&soundVal)
	if soundVal == "0" {
		phaze.SoundEnabled = false
	}
	var compactVal string
	phaze.DB.QueryRow("SELECT value FROM Profile WHERE key = 'compact_mode'").Scan(&compactVal)
	if compactVal == "1" {
		phaze.CompactMode = true
	}

	phaze.ShowLoginWindow()
	phaze.App.Lifecycle().SetOnStopped(func() {
		if phaze.DB != nil {
			phaze.DB.Close()
		}
		if phaze.DBPath != "" {
			if err := encryptDBFile(phaze.DBPath); err != nil {
				log.Printf("[Vault] DB encrypt on shutdown failed: %v", err)
			}
		}
	})
	phaze.App.Run()
}

func (s *PhazeApp) showEmailVerificationDialog(username string, parent fyne.Window) {
	codeEntry := widget.NewEntry()
	codeEntry.SetPlaceHolder("6-digit code")

	resendBtn := widget.NewButton("Resend code", func() {
		s.SendMessage(NexusMessage{
			Type:   "resend_verification",
			Sender: username,
		})
		dialog.ShowInformation("Phaze", "Verification code resent. Check your email.", parent)
	})
	resendBtn.Importance = widget.LowImportance

	d := dialog.NewCustomConfirm("Verify Email", "Verify", "Cancel", container.NewVBox(
		widget.NewLabel("We sent a verification link to your email. Click it, or enter the code below:"),
		codeEntry,
		resendBtn,
	), func(ok bool) {
		if ok {
			s.SendMessage(NexusMessage{
				Type:   "verify_email",
				Sender: username,
				Body:   codeEntry.Text,
			})
			dialog.ShowInformation("Phaze", "Activation code submitted. You can now try logging in.", parent)
			parent.Close()
		}
	}, parent)
	d.Show()
}

func (s *PhazeApp) handleChatOpen(name string) {
	if name == "Echo / Sound Test Service" {
		s.showEchoCallDialog()
		return
	}

	view := s.OpenChat(name)
	s.ContentStack.Objects = []fyne.CanvasObject{view}
	s.ContentStack.Refresh()
}

func (s *PhazeApp) handleChatWindowOpen(name string) {
	if win, ok := s.OpenWindows[name]; ok {
		win.RequestFocus()
		return
	}
	win := s.App.NewWindow("Chat: " + name)
	win.Resize(fyne.NewSize(600, 500))
	view := s.OpenChat(name)
	win.SetContent(view)
	win.Show()
	s.OpenWindows[name] = win
	win.SetOnClosed(func() {
		delete(s.OpenWindows, name)
	})
}

func (s *PhazeApp) showEchoCallDialog() {
	win := s.App.NewWindow("Phaze Echo Service")
	win.Resize(fyne.NewSize(350, 450))

	lbl := widget.NewLabelWithStyle("Welcome to the Phaze Echo Sound Test Service.", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	status := widget.NewLabel("Connected...")

	avatar := ui.NewAvatarWithStatus(120, "Online", "")

	content := container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(avatar),
		lbl,
		container.NewCenter(status),
		layout.NewSpacer(),
		widget.NewButtonWithIcon("End Call", theme.CancelIcon(), func() { win.Close() }),
	)

	win.SetContent(container.NewStack(canvas.NewRectangle(ui.PhazeShell), container.NewPadded(content)))
	win.Show()

	go func() {
		s.PlaySound("EchoGreeting.wav")
		time.Sleep(5 * time.Second)
		status.SetText("Recording: 10s remaining...")
		s.PlaySound("Beep.wav")
		time.Sleep(10 * time.Second)
		s.PlaySound("Beep.wav")
		status.SetText("Playing back your message...")
		time.Sleep(5 * time.Second)
		win.Close()
	}()
}

func (s *PhazeApp) ShowMyProfileWindow() {
	if win, ok := s.OpenWindows["profile_me"]; ok {
		win.RequestFocus()
		return
	}

	win := s.App.NewWindow("Profile: " + s.Username)
	win.Resize(fyne.NewSize(400, 500))
	s.OpenWindows["profile_me"] = win
	win.SetOnClosed(func() { delete(s.OpenWindows, "profile_me") })

	// Fetch current mood/displayname from DB
	var mood, dispName, email string
	s.DB.QueryRow("SELECT fullname, emails, mood_text FROM Accounts WHERE skypename = ?", s.Username).Scan(&dispName, &email, &mood)

	p2pAddr := "Offline"
	if s.P2P != nil {
		p2pAddr = s.P2P.Host.ID().String()
	}

	editor := ui.NewProfileEditor(ui.ProfileProps{
		Username:    s.Username,
		DisplayName: dispName,
		Mood:        mood,
		AvatarPath:  s.AvatarPath,
		Email:       email,
		P2PAddr:     p2pAddr,
		OnSave: func(newMood, newDisp string) {
			s.DB.Exec("UPDATE Accounts SET fullname = ?, mood_text = ? WHERE skypename = ?", newDisp, newMood, s.Username)
			s.Mood = newMood
			s.SendMessage(NexusMessage{
				Type:   "status_update",
				Sender: s.Username,
				Body:   newMood, // In Phaze, mood is sent via status_update Body
			})
			s.rebuildSidebar()
			win.Close()
		},
		OnAvatarClick: func() {
			// Placeholder for avatar picker
			dialog.ShowInformation("Avatar", "Avatar selection coming in v1.26", win)
		},
	})

	win.SetContent(editor)
	win.Show()
}

func (s *PhazeApp) showSettingsWindow() {
	if win, ok := s.OpenWindows["settings"]; ok {
		win.RequestFocus()
		return
	}

	win := s.App.NewWindow("Options")
	win.Resize(fyne.NewSize(500, 400))
	s.OpenWindows["settings"] = win
	win.SetOnClosed(func() { delete(s.OpenWindows, "settings") })

	settings := ui.NewSettingsDialog(ui.SettingsProps{
		ServerAddr:   s.ServerAddress,
		SoundEnabled: s.SoundEnabled,
		Sentinel:     s.Sentinel,
		OnLinkPhone: func(number string) {
			s.SendMessage(NexusMessage{
				Type:   "request_phone_link",
				Sender: s.Username,
				Phone:  number,
			})
		},
		OnVerifyPhone: func(number, code string) {
			s.SendMessage(NexusMessage{
				Type:   "verify_phone_link",
				Sender: s.Username,
				Phone:  number,
				Body:   code,
			})
		},
		OnPurgeEmail: func() {
			dialog.ShowConfirm("Remove Email",
				"Remove your email address? Password recovery via email will no longer work.",
				func(ok bool) {
					if ok {
						s.SendMessage(NexusMessage{
							Type:   "purge_email",
							Sender: s.Username,
						})
					}
				}, s.MainWindow)
		},
		OnSave: func(newServer string, sound bool) {
			s.ServerAddress = newServer
			s.SoundEnabled = sound
			s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server_addr', ?)", newServer)
			s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('sound_enabled', ?)", fmt.Sprintf("%v", sound))
			// Sync settings to server
			if settingsJSON, err := json.Marshal(map[string]interface{}{
				"sound_enabled":         sound,
				"notifications_enabled": s.NotificationsEnabled,
				"compact_mode":          s.CompactMode,
			}); err == nil {
				go s.SendMessage(NexusMessage{
					Type:   "settings_set",
					Sender: s.Username,
					Body:   string(settingsJSON),
				})
			}
			win.Close()
			dialog.ShowInformation("Settings", "Settings saved successfully.", s.MainWindow)
		},
		OnAudioChange: func(name string) {
			log.Printf("[Audio] Switched to device: %s", name)
		},
	})

	enable2FA := widget.NewButton("Enable 2FA", func() {
		s.SendMessage(NexusMessage{Type: "enable_totp", Sender: s.Username})
	})
	disable2FA := widget.NewButton("Disable 2FA", func() {
		dialog.ShowConfirm("Disable 2FA", "Are you sure? Your account will rely on password only.", func(ok bool) {
			if ok {
				s.SendMessage(NexusMessage{Type: "disable_totp", Sender: s.Username})
			}
		}, s.MainWindow)
	})
	approveQR := widget.NewButton("Approve QR sign-in", func() {
		tokenEntry := widget.NewEntry()
		tokenEntry.SetPlaceHolder("Paste phaze://login?token=... or just the token")
		dialog.ShowForm("Approve QR sign-in", "Approve", "Cancel",
			[]*widget.FormItem{widget.NewFormItem("Code", tokenEntry)},
			func(ok bool) {
				if !ok || tokenEntry.Text == "" {
					return
				}
				tok := strings.TrimSpace(tokenEntry.Text)
				if idx := strings.Index(tok, "token="); idx >= 0 {
					tok = tok[idx+len("token="):]
				}
				host, _ := os.Hostname()
				s.SendMessage(NexusMessage{
					Type:       "qr_login_approve",
					Sender:     s.Username,
					QRToken:    tok,
					DeviceInfo: runtime.GOOS + "/" + host,
				})
			}, s.MainWindow)
	})
	linkNewDevice := widget.NewButton("Link a new device", func() {
		s.SendMessage(NexusMessage{Type: "link_create", Sender: s.Username})
	})
	backupKeys := widget.NewButton("Back up E2EE Keys to Server", func() {
		pin1 := widget.NewPasswordEntry()
		pin1.SetPlaceHolder("Choose a recovery PIN (4+ chars)")
		pin2 := widget.NewPasswordEntry()
		pin2.SetPlaceHolder("Confirm PIN")
		dialog.ShowForm("Back up E2EE Keys", "Save Backup", "Cancel",
			[]*widget.FormItem{
				widget.NewFormItem("Recovery PIN", pin1),
				widget.NewFormItem("Confirm PIN", pin2),
			},
			func(ok bool) {
				if !ok {
					return
				}
				p1 := pin1.Text
				p2 := pin2.Text
				if len(p1) < 4 {
					dialog.ShowError(errors.New("PIN must be at least 4 characters"), s.MainWindow)
					return
				}
				if p1 != p2 {
					dialog.ShowError(errors.New("PINs do not match"), s.MainWindow)
					return
				}

				payload, err := EncryptKeypair(s.PubKey[:], s.PrivKey[:], p1)
				if err != nil {
					dialog.ShowError(err, s.MainWindow)
					return
				}

				s.SendMessage(NexusMessage{
					Type:   "key_backup_put",
					Sender: s.Username,
					KeyBackup: &KeyBackupPayload{
						Ciphertext: payload.Ciphertext,
						Salt:       payload.Salt,
						Iterations: payload.Iterations,
					},
				})
			}, s.MainWindow)
	})
	deleteBackup := widget.NewButton("Delete E2EE Backup from Server", func() {
		dialog.ShowConfirm("Delete Backup", "Are you sure you want to delete your key backup from the server? New devices won't be able to restore E2EE keys.", func(ok bool) {
			if ok {
				s.SendMessage(NexusMessage{Type: "key_backup_delete", Sender: s.Username})
			}
		}, s.MainWindow)
	})
	revokeSession := widget.NewButton("Sign out everywhere", func() {
		s.SendMessage(NexusMessage{Type: "revoke_session", Sender: s.Username, QRToken: s.SessionToken})
		keyring.Delete(sessionKeyringService, s.Username)
		s.SessionToken = ""
		dialog.ShowInformation("Phaze", "All sessions revoked. You'll need to sign in again next launch.", s.MainWindow)
	})

	security := container.NewVBox(
		widget.NewLabelWithStyle("Security", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		enable2FA,
		disable2FA,
		approveQR,
		linkNewDevice,
		backupKeys,
		deleteBackup,
		revokeSession,
	)

	win.SetContent(container.NewVBox(settings, widget.NewSeparator(), security))
	win.Show()
}

func (s *PhazeApp) EnsureForensicKeys() {
	if s.PubKey != nil && s.PrivKey != nil {
		return
	}
	s.DB.Exec("INSERT OR IGNORE INTO Accounts (skypename) VALUES (?)", s.Username)
	var pub, priv []byte
	err := s.DB.QueryRow("SELECT public_key, private_key FROM Accounts WHERE skypename = ?", s.Username).Scan(&pub, &priv)
	if err != nil || len(pub) == 0 {
		log.Println("[Sovereign] Generating new forensic key pair...")
		kp, _ := crypto.GenerateKeyPair()
		s.PubKey = kp.Public
		s.PrivKey = kp.Private
		s.DB.Exec("UPDATE Accounts SET public_key = ?, private_key = ? WHERE skypename = ?", kp.Public[:], kp.Private[:], s.Username)
	} else {
		var pK, sK [32]byte
		copy(pK[:], pub)
		copy(sK[:], priv)
		s.PubKey = &pK
		s.PrivKey = &sK
		log.Println("[Sovereign] Forensic keys loaded.")
	}

	// Share presence keys
	go s.SendMessage(NexusMessage{
		Type:           "presence",
		Sender:         s.Username,
		Status:         "Online",
		PublicKey:      s.PubKey[:],
		KeyFingerprint: crypto.Fingerprint(s.PubKey),
	})
}

func (s *PhazeApp) showRestoreBackupDialog(backup *KeyBackupPayload) {
	pinEntry := widget.NewPasswordEntry()
	pinEntry.SetPlaceHolder("Enter your 4+ character Recovery PIN")
	dialog.ShowForm("Restore E2EE Keys", "Restore", "Skip",
		[]*widget.FormItem{
			widget.NewFormItem("PIN", pinEntry),
		},
		func(ok bool) {
			if !ok {
				return
			}
			pub, priv, err := DecryptKeypair(backup.Ciphertext, backup.Salt, backup.Iterations, pinEntry.Text)
			if err != nil {
				dialog.ShowError(err, s.MainWindow)
				// Re-prompt on error
				s.showRestoreBackupDialog(backup)
				return
			}

			// Save to local db
			s.DB.Exec("INSERT OR IGNORE INTO Accounts (skypename) VALUES (?)", s.Username)
			s.DB.Exec("UPDATE Accounts SET public_key = ?, private_key = ? WHERE skypename = ?", pub, priv, s.Username)

			// Update in memory
			var pK, sK [32]byte
			copy(pK[:], pub)
			copy(sK[:], priv)
			s.PubKey = &pK
			s.PrivKey = &sK

			log.Println("[Sovereign] E2EE keys restored from backup successfully!")

			// Announce presence with new key
			go s.SendMessage(NexusMessage{
				Type:           "presence",
				Sender:         s.Username,
				Status:         "Online",
				PublicKey:      s.PubKey[:],
				KeyFingerprint: crypto.Fingerprint(s.PubKey),
			})

			dialog.ShowInformation("Restore", "E2EE keys restored and synchronized successfully.", s.MainWindow)
		}, s.MainWindow)
}

func (s *PhazeApp) showAboutDialog() {
	win := s.App.NewWindow("About Phaze™")
	win.Resize(fyne.NewSize(350, 400))
	win.SetFixedSize(true)

	logo := canvas.NewImageFromFile(ui.ResolveAsset("assets/phaze_logo.png"))
	logo.SetMinSize(fyne.NewSize(150, 75))
	logo.FillMode = canvas.ImageFillContain

	credits := widget.NewRichTextFromMarkdown(fmt.Sprintf(`
# Phaze

**Version:** %s

**Product:** Private chat and voice with Nexus relay and optional E2EE.

**Site:** [phazechat.world](https://phazechat.world)

---
*Phaze is independent software. Not affiliated with Microsoft Corporation or Skype.*
`, Version))
	credits.Wrapping = fyne.TextWrapWord

	win.SetContent(container.NewPadded(container.NewVBox(
		container.NewCenter(logo),
		widget.NewSeparator(),
		container.NewVScroll(credits),
	)))
	win.Show()
}

func (s *PhazeApp) setupTray() {
	if desk, ok := s.App.(desktop.App); ok {
		m := fyne.NewMenu("Phaze",
			fyne.NewMenuItem("Online", func() { s.updateStatus("Online") }),
			fyne.NewMenuItem("Away", func() { s.updateStatus("Away") }),
			fyne.NewMenuItem("Do Not Disturb", func() { s.updateStatus("Do Not Disturb") }),
			fyne.NewMenuItem("Invisible", func() { s.updateStatus("Invisible") }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Open Phaze", func() { s.MainWindow.Show() }),
			fyne.NewMenuItem("Quit", func() { s.App.Quit() }),
		)
		desk.SetSystemTrayMenu(m)
		// Default icon
		desk.SetSystemTrayIcon(theme.AccountIcon())
	}
}

func (s *PhazeApp) updateStatus(status string) {
	s.Status = status
	s.SendMessage(NexusMessage{Type: "status_update", Sender: s.Username, Body: status})

	// Forensic Tray Sync
	if _, ok := s.App.(desktop.App); ok {
		s.App.SendNotification(fyne.NewNotification("Phaze", "Status changed to "+status))
	}

	if s.Sidebar != nil {
		s.Sidebar.Refresh()
	}
}

func (s *PhazeApp) handleServerListResult(msg NexusMessage) {
	if msg.Status != "ok" {
		log.Println("[Spaces] Failed to load servers:", msg.Error)
		return
	}
	s.Spaces = msg.Servers
	log.Printf("[Spaces] Loaded %d servers", len(s.Spaces))
}

func (s *PhazeApp) handleServerInfoResult(msg NexusMessage) {
	if msg.Status != "ok" {
		log.Println("[Spaces] Failed to load server info:", msg.Error)
		return
	}
	s.Channels[msg.ServerID] = msg.Channels
	log.Printf("[Spaces] Loaded %d channels for server %s", len(msg.Channels), msg.ServerID)
}

func (s *PhazeApp) handleChannelHistoryResult(msg NexusMessage) {
	if msg.Status != "ok" {
		log.Println("[Spaces] Failed to load channel history:", msg.Error)
		return
	}
	s.ChannelHistory[msg.ChannelID] = msg.Messages
	log.Printf("[Spaces] Loaded %d messages for channel %s", len(msg.Messages), msg.ChannelID)
}

func (s *PhazeApp) handleChannelMsgIn(msg NexusMessage) {
	if len(msg.Messages) > 0 {
		chID := msg.ChannelID
		s.ChannelHistory[chID] = append(s.ChannelHistory[chID], msg.Messages...)
	}
}

func (s *PhazeApp) buildSpacesView() fyne.CanvasObject {
	return ui.NewSpacesView(ui.SpacesProps{
		Spaces:         s.Spaces,
		Channels:       s.Channels,
		ActiveSpace:    s.ActiveSpace,
		ActiveChannel:  s.ActiveChannel,
		OnSelectSpace: func(id string) {
			s.ActiveSpace = id
			s.ActiveChannel = ""
			s.rebuildSidebar()
		},
		OnSelectChannel: func(id string) {
			s.ActiveChannel = id
			// Note: would open channel chat view here
			s.rebuildSidebar()
		},
		OnJoinSpace: func(code string) {
			s.SendMessage(NexusMessage{Type: "server_join", Sender: s.Username, InviteCode: code})
		},
		OnCreateSpace: func(name, visibility string) {
			s.SendMessage(NexusMessage{Type: "server_create", Sender: s.Username, Body: name, Visibility: visibility})
		},
	})
}
