package main

// ipcheck.go — VPN / proxy / datacenter detection at registration time.
//
// Primary: proxycheck.io free tier (returns real proxy/VPN flags, 1000 req/day free).
//          Set PROXYCHECK_KEY to a free API key for 1000 req/day → 1M req/day.
// Secondary: ip-api.com (ISP/org keyword fallback + pro proxy/hosting fields
//          when IPAPI_KEY is set).
// Enabled only when PHAZE_BLOCK_VPN=1 is set in environment.
// Fails open: if both checks time out or error, registration proceeds.

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var ipCheckClient = &http.Client{Timeout: 3 * time.Second}

// ipCheckCache avoids hitting external APIs multiple times for the same IP
// within a single server lifetime (e.g. retry storms).
var ipCheckCache sync.Map // map[string]bool  true = datacenter/VPN

// isVPNOrDatacenter returns true if ip looks like a VPN, proxy, or
// datacenter. Always returns false if PHAZE_BLOCK_VPN is not set to "1",
// or if ip is private/loopback.
func isVPNOrDatacenter(ip string) bool {
	if os.Getenv("PHAZE_BLOCK_VPN") != "1" {
		return false
	}
	if isPrivateIP(ip) {
		return false
	}
	if v, ok := ipCheckCache.Load(ip); ok {
		return v.(bool)
	}

	// proxycheck.io is the primary: free tier returns real proxy/VPN flags.
	result := checkProxycheck(ip)
	// Fall back to ip-api if proxycheck didn't flag it.
	if !result {
		result = checkIPAPI(ip)
	}

	ipCheckCache.Store(ip, result)
	return result
}

// checkProxycheck queries proxycheck.io. Free tier: 1000/day unkeyed,
// more with a free PROXYCHECK_KEY. Returns true if flagged as proxy/VPN.
func checkProxycheck(ip string) bool {
	key := strings.TrimSpace(os.Getenv("PROXYCHECK_KEY"))
	url := "https://proxycheck.io/v2/" + ip + "?vpn=1&asn=1"
	if key != "" {
		url += "&key=" + key
	}
	resp, err := ipCheckClient.Get(url)
	if err != nil {
		log.Printf("[ipcheck/proxycheck] lookup failed for %s: %v — skipping", ip, err)
		return false
	}
	defer resp.Body.Close()

	// Response: {"status":"ok", "<ip>": {"proxy":"yes","type":"VPN",...}}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return false
	}
	statusRaw, ok := raw["status"]
	if !ok {
		return false
	}
	var status string
	if err := json.Unmarshal(statusRaw, &status); err != nil || (status != "ok" && status != "warning") {
		return false
	}
	ipRaw, ok := raw[ip]
	if !ok {
		return false
	}
	var entry struct {
		Proxy string `json:"proxy"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(ipRaw, &entry); err != nil {
		return false
	}
	if entry.Proxy == "yes" {
		log.Printf("[ipcheck/proxycheck] %s flagged: type=%q", ip, entry.Type)
		return true
	}
	return false
}

type ipAPIResponse struct {
	Status  string `json:"status"`
	Proxy   bool   `json:"proxy"`   // pro field
	Hosting bool   `json:"hosting"` // pro field
	ISP     string `json:"isp"`
	Org     string `json:"org"`
	AS      string `json:"as"`
}

// checkIPAPI queries ip-api.com. Uses the pro endpoint when IPAPI_KEY is set
// (enables real proxy/hosting boolean fields). Otherwise falls back to
// ISP/org/AS keyword matching.
func checkIPAPI(ip string) bool {
	key := strings.TrimSpace(os.Getenv("IPAPI_KEY"))
	var url string
	if key != "" {
		url = "https://pro.ip-api.com/json/" + ip + "?fields=status,proxy,hosting,isp,org,as&key=" + key
	} else {
		url = "http://ip-api.com/json/" + ip + "?fields=status,isp,org,as"
	}

	resp, err := ipCheckClient.Get(url)
	if err != nil {
		log.Printf("[ipcheck/ipapi] lookup failed for %s: %v — allowing", ip, err)
		return false
	}
	defer resp.Body.Close()

	var r ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.Status != "success" {
		return false
	}

	// Pro fields: use directly when available.
	if r.Proxy || r.Hosting {
		log.Printf("[ipcheck/ipapi] %s flagged: proxy=%v hosting=%v isp=%q", ip, r.Proxy, r.Hosting, r.ISP)
		return true
	}

	// Free-tier fallback: pattern-match ISP/org/AS against known
	// datacenter and VPN provider strings.
	combined := strings.ToLower(r.ISP + " " + r.Org + " " + r.AS)
	for _, kw := range datacenterKeywords {
		if strings.Contains(combined, kw) {
			log.Printf("[ipcheck/ipapi] %s flagged by keyword %q: isp=%q org=%q", ip, kw, r.ISP, r.Org)
			return true
		}
	}
	return false
}

// datacenterKeywords matches ISP/org names that indicate a hosting provider
// or known VPN service. Lowercase, checked with strings.Contains.
var datacenterKeywords = []string{
	// Cloud / hosting
	"amazon", "aws", "digitalocean", "linode", "akamai", "vultr", "hetzner",
	"ovh", "scaleway", "contabo", "leaseweb", "choopa", "psychz",
	"cogent", "zayo", "hurricane electric",
	"microsoft azure", "google cloud", "google llc",
	"alibaba", "tencent cloud", "ibm cloud",
	// VPN / proxy providers
	"nordvpn", "expressvpn", "privateinternet", "pia ", "mullvad",
	"protonvpn", "cyberghost", "surfshark", "ipvanish", "purevpn",
	"hidemyass", "tunnelbear", "windscribe", "vpn unlimited",
	"perfect privacy", "ivacy", "torguard", "anonine",
	"lightspeed networks", // the specific one that slipped through
	// Generic VPN / proxy indicators
	"vpn", "proxy", "tor exit", "anonymizer", "datacenter",
	"data center", "hosting", "colocation", "colo ",
}

// isPrivateIP returns true for loopback, link-local, and RFC-1918 addresses.
func isPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true // unparseable — treat as private to avoid blocking
	}
	for _, cidr := range privateRanges {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}

var privateRanges = func() []*net.IPNet {
	ranges := []string{
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC-1918
		"172.16.0.0/12",  // RFC-1918
		"192.168.0.0/16", // RFC-1918
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 ULA
		"169.254.0.0/16", // link-local
		"100.64.0.0/10",  // Fly.io internal / CGNAT
	}
	var nets []*net.IPNet
	for _, r := range ranges {
		_, n, _ := net.ParseCIDR(r)
		if n != nil {
			nets = append(nets, n)
		}
	}
	return nets
}()
