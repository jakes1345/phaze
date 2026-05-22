# Phaze — full product audit

**Scope:** Phaze-owned product code (`nexus_server/`, `native_client/` excluding `third_party/`, `web/`, `docs/`, root CI and config). **Out of scope for “must fix”:** vendored trees under `native_client/third_party/**` (upstream TODOs/panics are not Phaze backlog).

**Goal:** No mystery about what is real vs simulated, what ships today, and what remains for a coherent multi-client product.

---

## 1. Repository map

| Area | Role | Production readiness |
|------|------|----------------------|
| `nexus_server/` | Go HTTP + WebSocket hub; auth; SQLite persistence; optional Resend/Twilio/Telnyx | Core chat/E2EE paths are real when env is set; mail/SMS/PSTN fall back to logged sim when unset |
| `native_client/` | Fyne desktop; WS client; local SQLite cache; WebRTC (pion) | Real against a running Nexus; billing UI is informational only |
| `web/` | Vite + React; WS + libsodium E2EE | Real login/chat against Nexus; no WebRTC; no register UI; limited reconnect |
| `docs/` | Protocol and interop specs | Authoritative for intended behavior; some items are roadmap not implemented |

---

## 2. Simulated vs production (Nexus)

When **credentials and feature flags** are missing, Nexus intentionally logs simulation-style prefixes instead of failing silently:

- **`[MAIL-SIM]`** — Resend not configured (`RESEND_API_KEY` / `RESEND_FROM` / `RESEND_BASE_URL` as applicable).
- **`[SMS-SIM]`** — Twilio not configured.
- **`[PSTN-SIM]`** — Telnyx not configured.

**Action:** Treat these as **explicit dev/test fallbacks**, not “fake product.” Document in operator runbooks which env vars turn each path live.

---

## 3. Native client — honesty checklist

| Item | Location | Status |
|------|----------|--------|
| Version string | `main.go` `Version` | User-visible build uses semantic style (`1.0.0`); bump for releases. |
| About / credits | `ShowAboutWindow` | Must describe Phaze + real links only; no fictional “engineering team” or third-party attribution that implies affiliation. |
| Toolbar “Phaze Credit” + Add Credit | `main.go` | **Not** a payment integration; opens informational dialog only until Stripe (or chosen provider) exists. |
| Buy credit dialog | `ShowBuyCreditDialog` | Radio amounts without checkout = **UX stub**; label clearly or remove until billing exists. |
| Chat header status | `showChat` `ChatViewProps.Status` | Must reflect `FriendInfo.Status` when available, not a hardcoded “Active now”. |
| Contacts sidebar tab | `internal/ui/sidebar.go` | “Global Mesh Directory” label only — **not** a directory service UI. |
| Local SQLite schema comment | `main.go` `initDB` | Must not reference third-party product names; describe Phaze local cache only. |

---

## 4. Web client — honesty checklist

| Item | Status |
|------|--------|
| Login + password | Real against Nexus `/api/login` and WS ticket flow. |
| Registration / email verify | **Not exposed** in UI; server may support flows — confirm `nexus_server` routes vs `web/` routes. |
| WebRTC / voice | **Not implemented** in `web/`; desktop uses pion; interop doc must stay source of truth until implemented. |
| Codec alignment (e.g. PCMU vs Opus) | **Gap** between desktop and web when WebRTC lands — tracked in engineering guideline / `WEBRTC_AND_PSTN.md`. |
| WS reconnect / backoff | **Partial or missing** — verify `web/` WS wrapper and list gaps. |

---

## 5. Branding and deploy naming drift

These confuse operators and users if left inconsistent:

| File | Issue |
|------|--------|
| `.goreleaser.yml` | Release notes header (binary IDs were already `phaze` / `phaze-nexus`). GitHub `release.github.name` must stay the **actual** repository slug until the repo is renamed on GitHub. |
| `nexus_server/docker-compose.yml` | Example self-host on your own VPS; set `Phaze_ALLOWED_ORIGINS` to your real HTTPS origins. See `docs/DEPLOY_SELF_HOSTED.md`. |
| `nexus_server/templates/*.html` | Footer links should describe **Phaze** while pointing at the real canonical git remote. |
| `native_client/.github/workflows/release.yml` | Artifact names should match **phaze**, not legacy codenames. |

---

## 6. Security and operations (high level)

- **Secrets:** Only via host environment, Docker/env files (never committed), or your CI secrets store; never commit keys.
- **TLS:** Browsers need **HTTPS/WSS** to your public hostname; typically the reverse proxy terminates TLS and forwards HTTP to Nexus on localhost — document threat model for local vs edge TLS.
- **E2EE:** Key upload, `key_request` relay, and pre-key behavior — cross-check `docs/WS_PROTOCOL.md` and server handlers for drift.
- **Rate limits / abuse:** Review Nexus middleware and registration endpoints if opened to the public internet.

---

## 7. Third_party noise

`native_client/third_party/mobile_x/**` and similar match thousands of `TODO`/`FIXME`/`panic`. **Do not** merge those into Phaze P0/P1; grep with path exclusions when triaging.

---

## 8. Prioritized backlog (suggested)

### P0 — Truth in UI and naming

- Toolbar credit + buy dialog: honest copy or hide until billing.
- About + version strings + schema comments.
- Goreleaser / templates / release workflow naming.

### P1 — Web ↔ desktop parity (product)

- Web: registration + verify if server supports.
- Web: WS reconnect with backoff + user-visible connection state.
- Web: WebRTC (or document “desktop-only voice” until done).

### P2 — Depth

- Contacts / directory: real search or server-backed list vs placeholder label.
- Group UX parity on web.
- Observability: structured logs, metrics, health checks for Nexus.

---

## 9. Verification commands (when touching Go)

```bash
cd nexus_server && go test ./...
cd native_client && go test ./...
```

Exclude `third_party` from expectations if packages do not build on the host; Phaze-owned packages should pass.

---

## 10. Sign-off process

Before claiming “no placeholders,” re-run:

1. `grep -r "TODO\|FIXME\|SIM\]\|placeholder" nexus_server web --include='*.go' --include='*.tsx' --include='*.ts'` (tune paths).
2. Manual smoke: Nexus up → native login → web login → DM E2EE roundtrip.
3. Update this doc’s date and a short changelog section when major gaps close.

*Last updated: audit pass aligned with Phaze naming and transparency goals.*

### Session note (follow-up)

- Native: removed fake credit amounts / fake “connecting to API” flow; toolbar documents PSTN billing reality; About uses `Version` + `phazechat.world`; chat header status uses `Friends` when not a group; SQLite comment de-mythologized; Contacts tab explains real UX limits.
- CI: Phaze artifact names; `.goreleaser` release header; templates link text clarified.
