package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	msAuthURL  = "https://login.microsoftonline.com/consumers/v2.0/oauth2/authorize"
	msTokenURL = "https://login.microsoftonline.com/consumers/v2.0/oauth2/token"
	msGraphURL = "https://graph.microsoft.com/v1.0/me?$select=id,displayName,mail,userPrincipalName"
)

type msPending struct {
	token     string
	username  string
	expiresAt time.Time
}

var msPendingMap sync.Map // state → *msPending

func msRedirectURI() string {
	if u := os.Getenv("MICROSOFT_REDIRECT_URI"); u != "" {
		return u
	}
	return "https://phazechat.world/api/v1/auth/microsoft/callback"
}

// msAuthInitHandler: GET /api/v1/auth/microsoft → {url, state}
// The native app opens `url` in a browser, then polls /poll?state= for the token.
func (s *NexusServer) msAuthInitHandler(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("MICROSOFT_CLIENT_ID")
	if clientID == "" {
		http.Error(w, "Microsoft auth not configured", http.StatusNotImplemented)
		return
	}
	state, err := randHex(16)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	msPendingMap.Store(state, &msPending{expiresAt: time.Now().Add(10 * time.Minute)})

	authURL := msAuthURL + "?" + url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {msRedirectURI()},
		"scope":         {"openid profile email User.Read"},
		"state":         {state},
		"response_mode": {"query"},
	}.Encode()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": authURL, "state": state})
}

// msAuthCallbackHandler: GET /api/v1/auth/microsoft/callback
// Microsoft redirects here after the user grants permission.
func (s *NexusServer) msAuthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "auth error: "+e, http.StatusBadRequest)
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	clientID := os.Getenv("MICROSOFT_CLIENT_ID")
	clientSecret := os.Getenv("MICROSOFT_CLIENT_SECRET")

	resp, err := http.PostForm(msTokenURL, url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {msRedirectURI()},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		log.Printf("[ms-auth] token exchange: %v", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tokenResp)

	req, _ := http.NewRequest("GET", msGraphURL, nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	gr, err := http.DefaultClient.Do(req)
	if err != nil || gr.StatusCode != 200 {
		http.Error(w, "profile fetch failed", http.StatusInternalServerError)
		return
	}
	defer gr.Body.Close()
	var msUser struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	json.NewDecoder(gr.Body).Decode(&msUser)

	email := msUser.Mail
	if email == "" {
		email = msUser.UserPrincipalName
	}

	var username string
	s.DB.QueryRow(`SELECT username FROM users WHERE microsoft_oid=?`, msUser.ID).Scan(&username)
	if username == "" && email != "" {
		s.DB.QueryRow(`SELECT username FROM users WHERE email=?`, email).Scan(&username)
		if username != "" {
			s.DB.Exec(`UPDATE users SET microsoft_oid=? WHERE username=?`, msUser.ID, username)
		}
	}
	if username == "" {
		base := sanitizeMSUsername(msUser.DisplayName)
		username = s.uniquifyUsername(base)
		if _, err = s.DB.Exec(
			`INSERT INTO users (username, email, microsoft_oid, is_verified, password_hash, salt) VALUES (?,?,?,1,'','')`,
			username, email, msUser.ID,
		); err != nil {
			log.Printf("[ms-auth] create user: %v", err)
			http.Error(w, "account creation failed", http.StatusInternalServerError)
			return
		}
		log.Printf("[ms-auth] new account %s for MS %s", username, msUser.ID)
	}

	tok, err := s.issueSessionToken(username, "microsoft-oauth")
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	if p, ok := msPendingMap.Load(state); ok {
		pa := p.(*msPending)
		pa.token = tok
		pa.username = username
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Phaze</title>
<style>*{margin:0;padding:0;box-sizing:border-box}body{background:#0a0a0a;color:#fff;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh}.box{text-align:center;padding:2rem}.logo{color:#a677ff;font-size:2rem;font-weight:800;margin-bottom:1rem}p{color:#888;margin:.5rem 0}strong{color:#fff}</style>
</head><body><div class="box"><div class="logo">Phaze</div>
<p>Signed in as <strong>%s</strong></p>
<p>You can close this window and return to the app.</p>
</div></body></html>`, username)
}

// msAuthPollHandler: GET /api/v1/auth/microsoft/poll?state=xxx
// Native app polls this until it gets status=ok with the session token.
func (s *NexusServer) msAuthPollHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	p, ok := msPendingMap.Load(state)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	pa := p.(*msPending)
	if time.Now().After(pa.expiresAt) {
		msPendingMap.Delete(state)
		http.Error(w, "expired", http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if pa.token == "" {
		json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
		return
	}
	msPendingMap.Delete(state)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"token":    pa.token,
		"username": pa.username,
	})
}

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9_]`)

func sanitizeMSUsername(displayName string) string {
	s := strings.ToLower(strings.ReplaceAll(displayName, " ", "_"))
	s = nonAlphanumRe.ReplaceAllString(s, "")
	if len(s) < 3 {
		s = "user"
	}
	if len(s) > 20 {
		s = s[:20]
	}
	return s
}

// uniquifyUsername appends a number suffix until the username is available.
func (s *NexusServer) uniquifyUsername(base string) string {
	candidate := base
	for i := 2; ; i++ {
		var dummy string
		if err := s.DB.QueryRow(`SELECT username FROM users WHERE username=?`, candidate).Scan(&dummy); err != nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", base, i)
		if len(candidate) > 24 {
			candidate = fmt.Sprintf("%s%d", base[:18], i)
		}
	}
}
