package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

// Self-hosted status page. Two pieces:
//   1. /status — public HTML page that shows the current health snapshot +
//      a 24h sparkline of recent health probes.
//   2. A background self-prober that snapshots /health every minute and
//      retains rolling 24h history in memory. Cheaper than UptimeRobot
//      for our scale and never blocks user-facing requests.
//
// Persistent uptime tracking (multi-week, post-restart) needs a DB row.
// v1 keeps it in-memory; reboots reset history. Good enough for the
// "is it up right now / has it been flapping today" question.

type healthSample struct {
	at time.Time
	ok bool
}

var (
	healthHistMu sync.Mutex
	healthHist   []healthSample // last ~1440 samples = 24h at 1/min
)

const healthHistMax = 1440

func recordHealth(ok bool) {
	healthHistMu.Lock()
	defer healthHistMu.Unlock()
	healthHist = append(healthHist, healthSample{at: time.Now().UTC(), ok: ok})
	if len(healthHist) > healthHistMax {
		healthHist = healthHist[len(healthHist)-healthHistMax:]
	}
}

func currentHealthSummary() (uptimePct float64, lastDown time.Time, total int) {
	healthHistMu.Lock()
	defer healthHistMu.Unlock()
	if len(healthHist) == 0 {
		return 100.0, time.Time{}, 0
	}
	okCount := 0
	for _, s := range healthHist {
		if s.ok {
			okCount++
		} else {
			lastDown = s.at
		}
	}
	uptimePct = float64(okCount) / float64(len(healthHist)) * 100
	total = len(healthHist)
	return
}

func healthHistorySnapshot() []healthSample {
	healthHistMu.Lock()
	defer healthHistMu.Unlock()
	out := make([]healthSample, len(healthHist))
	copy(out, healthHist)
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// startStatusProber starts a 1-minute self-prober that hits /health and
// records the result. Logs degraded transitions so Sentry / log analytics
// alerts can fire on them.
func (s *NexusServer) startStatusProber() {
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		var lastOK = true
		for {
			ok := s.probeSelf()
			recordHealth(ok)
			if ok != lastOK {
				if ok {
					log.Printf("[status] recovered: health passing")
				} else {
					log.Printf("[status] DEGRADED: health probe failed")
				}
				lastOK = ok
			}
			<-t.C
		}
	}()
}

func (s *NexusServer) probeSelf() bool {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil || resp == nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (s *NexusServer) statusPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	uptime, lastDown, total := currentHealthSummary()
	hist := healthHistorySnapshot()

	// Build sparkline bars. Each bar = 1 sample (1 min). Green = ok, red = down.
	var bars string
	for _, s := range hist {
		cls := "ok"
		if !s.ok {
			cls = "down"
		}
		bars += `<span class="bar ` + cls + `" title="` + s.at.Format(time.RFC3339) + `"></span>`
	}

	state := "All systems operational"
	stateCls := "ok"
	if total > 0 && uptime < 99.0 {
		state = "Recent incidents — see below"
		stateCls = "warn"
	}
	if total > 0 && uptime < 95.0 {
		state = "Service degraded"
		stateCls = "down"
	}

	lastDownStr := "No outages recorded in current window."
	if !lastDown.IsZero() {
		lastDownStr = "Last down: " + lastDown.Format("2006-01-02 15:04 UTC")
	}

	fmt.Fprintf(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Phaze · Status</title>
<style>
body{font-family:'Inter',system-ui,sans-serif;background:#0b0b0d;color:#fafafa;max-width:760px;margin:48px auto;padding:24px}
h1{margin:0 0 4px;font-size:1.4rem}
.hdr{display:flex;justify-content:space-between;align-items:center;margin-bottom:32px}
.badge{padding:6px 14px;border-radius:999px;font-weight:700;font-size:.8rem}
.badge.ok{background:rgba(34,197,94,.18);color:#86efac}
.badge.warn{background:rgba(234,179,8,.18);color:#fde047}
.badge.down{background:rgba(239,68,68,.18);color:#fca5a5}
.card{background:#16161a;border:1px solid #232328;border-radius:14px;padding:22px;margin-bottom:18px}
.card h2{margin:0 0 12px;font-size:1rem;color:#a1a1aa;text-transform:uppercase;letter-spacing:.08em}
.metric{font-size:2.2rem;font-weight:700}
.muted{color:#71717a;font-size:.9rem}
.sparkline{display:flex;gap:1px;margin-top:14px;height:28px;align-items:end}
.bar{flex:1;min-width:1px;background:#22c55e;border-radius:1px;height:100%%}
.bar.down{background:#ef4444}
.empty{padding:14px;color:#71717a;text-align:center;font-size:.9rem;background:rgba(255,255,255,.03);border-radius:8px}
footer{margin-top:32px;text-align:center;color:#52525b;font-size:.8rem}
footer a{color:#a677ff;text-decoration:none}
</style></head>
<body>
<div class="hdr">
  <div><h1>Phaze Status</h1><span class="muted">phazechat.world</span></div>
  <span class="badge %s">%s</span>
</div>

<div class="card">
  <h2>Uptime (last 24h)</h2>
  <div class="metric">%.2f%%</div>
  <div class="muted">%s</div>
  %s
</div>

<div class="card">
  <h2>Service health</h2>
  <p style="margin:0"><span class="muted">Endpoint:</span> <code>https://phazechat.world/health</code> — last probed every 60s by the server itself.</p>
  <p style="margin:8px 0 0"><span class="muted">Samples in window:</span> %d</p>
</div>

<footer>
  Backend: Fly.io (iad) · Backups: Tigris S3 / Litestream<br>
  <a href="/">← Back to Phaze</a>
</footer>
</body></html>`,
		stateCls, state, uptime, lastDownStr,
		func() string {
			if total == 0 {
				return `<div class="empty" style="margin-top:14px">No samples yet — try again in a minute.</div>`
			}
			return `<div class="sparkline">` + bars + `</div>`
		}(),
		total)
}

func (s *NexusServer) initStatusPage() {
	s.startStatusProber()
	http.HandleFunc("/status", rateLimit(s.statusPageHandler))
	http.HandleFunc("/status/", rateLimit(s.statusPageHandler))
}
