# Phaze Roadmap / TODO

## 🎉 LAUNCH FINALE (2026-06-09) — shipped to master + deploying
- [x] Public group discovery — server + web + **Android**
- [x] Theme packs — Dark / Light / **Skype 7** (web + Android)
- [x] Snowflakes seasonal overlay (web + Android)
- [x] Recovery backup — **verified working live** (set PIN → new device → restore = exact keys)
- [x] All audit fixes (Android voice crash, Android 2FA, web block, TOTP throttle, camera feature)
- [x] Single launch AAB v1.4.0 (versionCode 17) + Fly deploy
- [ ] **Upload AAB to Google Play Console** (you — within 2 days)


**This is the single front door for "what's left."** Living backlog, prioritized. Last updated 2026-06-08.

### 📑 The only two lists that matter
- **`docs/ROADMAP.md`** (this file) — the working backlog. Day-to-day, look here.
- **`docs/PRE_BETA_CHECKLIST.md`** — a one-time ops gate to run through *right before* opening a public beta (CORS, secrets, rate limits, runbook).

If a TODO doesn't fit the beta checklist, it belongs here — not in a new file.

## 🚀 Ready to ship (verified; needs deploy)
- [ ] **`fly deploy`** master → phazechat.world (Fly app `skype7-reborn`). Ships server + web SPA + download page. Manual; no auto-deploy on merge. (Merged & waiting: screen-share fix, group discovery.)
- [ ] Replace stale Fyne `Phaze.apk` in `nexus_server/public/downloads/` with the new Kotlin APK (also clears the git large-file push warning).
- [ ] Web push: generate VAPID keys (`npx web-push generate-vapid-keys`) + set `VAPID_PUBLIC_KEY`/`VAPID_PRIVATE_KEY` env on Fly.
- [ ] Android release: upload v1.3.0 AAB to Google Play Console; add CI signing secrets (`ANDROID_KEYSTORE_PASS`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASS`).

## 📱 Cross-client parity (Android is behind web)
Android only does 1:1 calls and lacks several web features. Each needs the build→install→device-test loop.
- [ ] **Group voice rooms** (multi-party calls) — web has mesh `VoiceRoom.tsx`; Android has none. Biggest gap.
- [ ] **Group discovery UI** on Android (server + web done; Android consumes `server_discover`).
- [ ] **Live streaming** on Android (web-only today).
- [ ] **Remote control** on Android (web-only today).
- [ ] **Invite via email** on Android (web-only today).
- [ ] Confirm audio routing + speakerphone on real Android hardware.

## 🆚 Competitor parity (vs Velocity Chat — see memory)
- [x] Public group discovery directory (server + web). ✅ shipped to repo 2026-06-08.
- [ ] Group-discovery UI on Android.
- [ ] Group-first social polish: categories/curation (Official / Partners / featured), like their "Group World".

## 🏗️ Server engineering (from the code review)
- [ ] Convert the 90-case `msg.Type` switch in `ws_handlers.go` to a `map[string]handlerFunc` registry. **Write protocol tests first.**
- [ ] Continue `main.go` decomposition (now ~4k lines). See `server_decomposition` memory for the safe procedure + goimports gotcha.
- [ ] Move inline `Fprintf` HTML (password-reset page etc.) into `templates/` — string-built HTML is an XSS footgun; `html/template` auto-escapes.
- [ ] Error-hygiene sweep: audit `_ =` ignored errors on DB writes / `client.Send` (a swallowed send = a silently dropped message).

## 🧪 Testing
- [ ] Android: **0 unit tests** — add `E2EE.kt` round-trips + `NexusMessage` parsing.
- [ ] Web: only 1 test — add message/store reducer tests.
- [ ] Protocol tests for the WS handlers (prerequisite for the handler-map refactor).

## 🖥️ Platforms
- [ ] Web: split `App.tsx` (2,545 lines) into hooks (`useCall`/`useChat`/`useAuth`) + route components.
- [ ] macOS/iOS: out of reach (no Apple hardware). Web PWA is the Apple story for now.

## 🔒 Security (from beta checklist + handoff)
- [ ] Run the operational gate in `docs/PRE_BETA_CHECKLIST.md` before any public beta wave.
- [ ] SQL-injection audit (baseline good — parameterized everywhere), admin role lockdown, VPN detection, input sanitization.
- [ ] A release keystore is committed in the repo — consider rotating + moving to secrets long-term.

## 🐞 From the full audit (2026-06-08) — remaining
- [ ] **Read receipts**: server relays `read_receipt` but no client sends or displays them. Build it (send on read + "Seen" indicator) or remove the dead server path.
- [ ] **Recovery backup repro**: key-backup protocol + crypto verified correct on both clients (PBKDF2-SHA256/AES-GCM match; restore-apply paths consistent). If it's still failing, capture a live repro (which client, same-device vs new-device) — the bug isn't visible statically.
- [ ] Replace deprecated `option.WithCredentialsJSON` (Firebase init) — low-risk-but-flagged.
- [x] ~~Android voice-note crash <API31~~ · ~~Android 2FA broken~~ · ~~web profile-block ignored~~ · ~~TOTP brute-force unthrottled~~ · ~~camera uses-feature~~ — fixed in `fix/audit-defects`.
