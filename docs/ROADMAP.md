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

## ☎️ Calling — bugs & gaps

### 1:1 Calls (web + Android — both exist)
- [ ] **No ring timeout / missed call** — if callee never answers, the call rings forever on both sides. No `missed_call` message sent to callee. Add a 60s ring timer on the caller side that sends `call_end` and logs the miss. Both web (`App.tsx:startCall`) and Android (`PhazeViewModel`) need this.
- [ ] **No "busy" signal** — if callee is already in a call, caller just gets silence. Server should detect the existing call state and route a `call_busy` back to the caller.
- [ ] **No call duration timer** — once active the UI shows "Connected" but no elapsed time. Add a seconds counter that starts on `call_answer`. (Web: `callState.status === 'active'`, Android: `CallScreen.kt`)
- [ ] **Android: no speakerphone / earpiece toggle** — `CallManager.kt` has zero `AudioManager` code. During a call audio goes to earpiece and can't be switched to speaker. Add `AudioManager.MODE_IN_COMMUNICATION` + `setSpeakerphoneOn()` + a toggle button in `CallScreen.kt`.
- [ ] **Android: no audio routing at call start** — `CallManager.startLocalMedia()` creates an `AudioSource` with blank `MediaConstraints()` — no echo cancellation, noise suppression, or auto-gain explicitly requested. Chrome does this automatically; Android WebRTC does not. Pass `offerToReceiveAudio: true` and set constraints: `{ googEchoCancellation: true, googNoiseSuppression: true, googAutoGainControl: true }`.
- [ ] **Opus codec not forced** — web and Android both use whatever the browser/WebRTC picks by default. Force Opus 48kHz for voice calls via `RTCRtpSender.setParameters` / SDP munging (`setCodecPreferences` where available). Phaze is voice-first; this is the single biggest audio quality lever.
- [ ] **No call recording** — Skype had it. Out of scope for now but document it.
- [ ] **No PSTN calling** — `initiateTwilioCall()` is called but Twilio is never wired. Either remove the button completely or add a clear "coming soon" state instead of the silent fallback. `ws_handlers.go:136`

### Group Voice Rooms (web only)
- [ ] **Android: 0% implemented** — `PhazeViewModel.kt` has no `voice_join`/`voice_signal`/`voice_peers` handling. This is the biggest Android gap. Port `VoiceRoom.tsx` logic to `CallManager` + new `VoiceRoomScreen.kt`.
- [ ] **No speaking indicator** — when a peer talks there's no visual "speaking" ring or audio level bar. Add a `AnalyserNode` (web) / `AudioTrack.volume` poll (Android) to drive a glow on the peer tile.
- [ ] **Screen share renegotiation flaky** — `toggleScreenShare` in `VoiceRoom.tsx` calls `initiateOffer` only for peers where `me < u` (alphabetical tiebreak). Peers whose username sorts after yours never get a renegotiation offer. Fix: always initiate from the person who changed their track.
- [ ] **No peer volume control** — can't lower a specific peer. Add per-`audio` element gain via `GainNode`.
- [ ] **No deafen** — can only mute self, can't mute all incoming audio at once.
- [ ] **No noise suppression in voice rooms** — `getUserMedia` is called with just `{ audio: true }`. Pass `{ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } }`. Same fix as 1:1.
- [ ] **Viewer cap warning** — `LivePage.tsx` comments note the mesh breaks at ~10-15 viewers. No warning shown to broadcaster. Show a badge when viewer count exceeds 10.

## 🖥️ Remote Control (TeamViewer-like)

The feature exists and works end-to-end. Known gaps:

- [ ] **Viewer sends mouse/keyboard over WS relay AND data channel** — `sendInputEvent` sends via both `inputChannel.send()` AND `send({ type: 'remote_input' })`. The server just relays this; the host receives it twice. Remove the WS relay path — data channel is the right transport for input events.
- [ ] **Host receives cursor position but doesn't act on it** — `setCursorPos({ x, y })` in the hosting UI just shows a percentage readout. The host needs to actually move their OS cursor using `robot.js` or similar (desktop native only) or display an overlay cursor on the shared video (web). Currently it's a ghost value.
- [ ] **Keyboard input only relayed, never executed** — key events are sent via data channel but on the host side the handler only handles `mousemove`. Key events arrive (`type`, `key`, `code`) but do nothing. Either execute them (native client) or document this limitation.
- [ ] **No clipboard sync** — can't paste text between viewer and host. Add `clipboard_text` data channel message type using `navigator.clipboard`.
- [ ] **No session auth on remote_lookup** — anyone who guesses a 6-digit code can connect. A 6-digit numeric code = 1M combos — brute-forceable in seconds if uncapped. Add per-IP rate limiting on `remote_lookup` (server-side, `ws_handlers.go`).
- [ ] **Single viewer only** — a second viewer connecting kicks the first silently. Enforce this explicitly and show an error to the second viewer.
- [ ] **No timeout on idle sessions** — a hosting session with no viewer connected stays open forever. Add a 5-minute idle timeout if no peer has connected.
- [ ] **`useEffect` missing dependency array** — `RemoteControl.tsx` subscribe effect runs on every render (no `[]`). This re-subscribes on every keystroke, leaking handlers. Add `[subscribe, mode, peer, handleIncomingFile, tearDown, send]` or restructure.

## 💬 Messaging — gaps

- [ ] **No message replies / threading** — `NexusMessage` has no `reply_to` field. Users can't quote-reply. This is a core Skype feature. Add `reply_to_id` + `reply_preview` to `NexusMessage`, persist in `dms` table, surface as a collapsed quote bubble in `App.tsx` and `ChatScreen.kt`.
- [ ] **Read receipts: server wired, no client sends or shows them** — server routes `read_receipt` correctly (`ws_handlers.go:1215`) but web never sends one when you open a chat, and there's no ✓✓ indicator in the message bubble. Wire it: send `read_receipt` on chat open, show ✓ (sent) / ✓✓ (read) in `App.tsx`.
- [ ] **No message forwarding** — can't forward a DM to another contact.
- [ ] **No message search** — no way to search your own DM history. Add full-text search on `dms` table (SQLite FTS5) + a `/api/v1/search` endpoint.
- [ ] **Emoji reactions on DMs work but no reaction summary tooltip** — clicking an emoji reaction toggles it but hovering doesn't show who reacted. Add a tooltip/popover listing usernames.
- [ ] **Pinned DM messages (client pin strip)** — web has a pin strip UI (`pinnedIds`, `pinsOpen`) but pinning a DM just stores the IDs in `localStorage`. If you clear storage or switch device, pins are gone. Persist via `user_settings` key `dm_pins_<peer>`.
- [ ] **No @mention notifications in DMs** — `@username` autocomplete works in the text box but tapping a completed mention doesn't highlight or notify the peer beyond the normal message.
- [ ] **`msg_edit` has no length cap** — already in security list but also a UX bug: edited messages can be 1 MB strings.
- [ ] **No link previews** — URLs in chat are plain text. Add `og:` metadata fetching server-side (`/api/v1/link-preview?url=`) with a strict allowlist and a result card in the bubble.

## 📱 Android feature gaps (vs web)

- [ ] **Group voice rooms** — none. Biggest gap. (Detailed above.)
- [ ] **Screen share in 1:1 calls** — `CallManager.startScreenShare()` + `PhazeViewModel.startScreenShare()` are implemented! But `CallScreen.kt` has no UI button for it. Wire the button.
- [ ] **Remote Control** — not on Android at all. At minimum, viewer mode (watch a shared screen, send touch events). `RemoteControl.tsx` logic to port.
- [ ] **Livestreaming** — `LivePage.tsx` and server fully implemented. Android has nothing.
- [ ] **Stories** — `StoriesScreen.kt` exists and `loadStories()` fires via HTTP. But creating a story requires picking media and POSTing to `/api/v1/stories` — the "post story" button is absent in `StoriesScreen.kt`.
- [ ] **Invite via email / invite link** — web has an invite flow. Android has no UI for it.
- [ ] **@mention autocomplete in Spaces channels** — `SpacesScreen.kt` has a basic message input but no `@` trigger.
- [ ] **Message reactions in Spaces** — `SpacesScreen.kt` has no reaction UI (server + web both support it).
- [ ] **No push notification tap-to-open** — `PhazeFCMService.kt` receives notifications but tapping one just opens the app to the default screen, not the relevant chat/call.
- [ ] **Typing indicator** — Android `ChatScreen.kt` displays "typing…" status (line 94) but never sends a `typing` message when the user types. Wire `TextFieldValue` onChange → debounced `send("typing")`.
- [ ] **Read receipts** — same as web: server ready, Android sends/shows nothing.
- [ ] **No call history / missed call log** — `PhazeViewModel` tracks current call state but doesn't persist call events to a log.
- [ ] **No speakerphone toggle** — covered in Calling section above.
- [ ] **No Bluetooth audio routing** — `CallManager` ignores Bluetooth headset events. Add `BluetoothHeadset` profile + `AudioManager.startBluetoothSco()`.

## 🏗️ Server engineering (from the code review)
- [ ] Convert the 90-case `msg.Type` switch in `ws_handlers.go` to a `map[string]handlerFunc` registry. **Write protocol tests first.**
- [ ] Continue `main.go` decomposition (now ~4k lines). See `server_decomposition` memory for the safe procedure + goimports gotcha.
- [ ] Move inline `Fprintf` HTML (password-reset page etc.) into `templates/` — string-built HTML is an XSS footgun; `html/template` auto-escapes.
- [ ] Error-hygiene sweep: audit `_ =` ignored errors on DB writes / `client.Send` (a swallowed send = a silently dropped message).
- [ ] **No server-kick / server-ban for Spaces** — owners can't kick a member from their server. No `server_kick` handler. Add it with a server role check.
- [ ] **No ownership transfer for Spaces** — owners must delete a server to leave (`ws_handlers.go:1515`). Add `server_transfer_owner` so they can pass ownership without nuking the space.
- [ ] **No DM history search endpoint** — `/api/v1/search` is missing. Add FTS5 on `dms.body` with auth gate.
- [ ] **Link preview API** — no `/api/v1/link-preview`. Add server-side OG scraper (Go `net/http` fetch of URL HEAD → parse `og:title`/`og:image`) to avoid exposing user IPs to external servers.
- [ ] **`channel_pin` auth too loose** — any server member can pin messages, not just admins/owners. Check `s.roleAtLeast` on `channel_pin`. `ws_handlers.go:1781`

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

---

## 🔥 Deep audit 2026-06-13 — FIXED NOW

- [x] **`friend_reject` wrong arg order** (`msg.Sender` → should be `msg.Recipient` like `friend_accept`). Silent bug: rejecting a friend request would silently no-op. `ws_handlers.go:1089`
- [x] **Reserved bot username "PhazeBot" not blocked** — anyone could register as `phazebot`/`phaze_bot` and send spoofed bot messages. Added `reservedUsernames` map in `registerUser`. `main.go`
- [x] **TOTP code replay** — same 6-digit code could be used multiple times within its 30s window. Added `totpUsedCodes` sync.Map with 60s TTL. `totp.go`
- [x] **`resend_verification` email bombing** — unauthenticated callers could spam any address. Added `resendLimiter` (1 resend/5min per username). `ws_handlers.go` + `netutil.go`
- [x] **`conversationMembers()` ignored scan errors** — silently dropped rows if DB malformed. `main.go:1306`
- [x] **Key backup PIN minimum too weak (4 chars)** — raised to 6 chars; 4-char PIN crackable in ~40h offline. `keyBackup.ts`

## 🔒 Security — remaining (from deep audit)

- [ ] **No Content-Security-Policy header** — admin portal uses inline `<script>` blocks so CSP was intentionally deferred. Fix: move admin portal JS to `nexus_server/public/admin.js` so CSP `script-src 'self'` can be set. `middleware.go:74`
- [ ] **Admin portal: no audit log** — bans, role changes, deletions leave no trace. Add `admin_actions` table (who, what, when, target). `admin_portal.go`
- [ ] **TOTP backup codes missing** — if user loses TOTP device, account is permanently locked out. Implement 8-code one-time backup set on TOTP enable.
- [ ] **TOFU key warning on first message** — first-time peer key is silently accepted (MITM window). Consider out-of-band fingerprint confirm (QR code scan between users). `App.tsx:705`
- [ ] **E2EE secret key in `localStorage`** — vulnerable to XSS exfil. Long-term: move to `IndexedDB` behind a session-encrypted wrapper. `App.tsx:28`
- [ ] **Password reset endpoint: no rate limit per email** — callers can spam reset emails. Add `forgotPasswordLimiter` same pattern as `resendLimiter`.
- [ ] **A release keystore committed to repo** (`android/app/phaze-release.keystore`) — rotate + move to secrets vault. (Known — captured here as reminder.)
- [ ] **`msg_edit` no length cap** — authenticated users can edit a message to arbitrarily large content. Add 10k char cap in `editDM()`. `main.go`
- [ ] **Channel messages: no per-user rate limit** — group chat has no throttle beyond global WS rate. Add per-user per-channel rate limit (e.g., 5 msg/s).
- [ ] **QR code token format not validated client-side** — backend validates but client should check `^[a-f0-9]{64}$` before sending. `App.tsx:472`

## 🏗️ Features — incomplete/missing (from deep audit)

- [ ] **Spaces: no channel moderation** — no admin-delete-message, no per-channel post permissions, no @mention notifications, no pinned messages handler (schema exists, no backend).
- [ ] **PSTN bridge stub** — `initiateTwilioCall()` is called but Twilio credentials/impl never wired in live. Either ship it or gate the button behind a feature flag. `ws_handlers.go:136`
- [ ] **File upload: no per-user rate limit** — 25 MB limit per file but no upload frequency cap. Add e.g. 100 MB/hr per user.
- [ ] **`acceptCall()` silent promise rejection** — `void pcRef.current.setRemoteDescription(...)` swallows errors. Add `.catch(e => setCallErr(e.message))`. `App.tsx`
- [ ] **Unknown WS message types: no default case log** — `msg.type` switch has no `default: console.warn(...)`. Hard to debug mystery server messages.

## ⚡ Performance / bundle

- [ ] **Web bundle 540 kB minified** — exceeds Vite's 500 kB chunk warning. Code-split: lazy-load `VoiceRoom`, `RemoteControl`, `Spaces`, `Stories` via `React.lazy`. `vite.config.ts`
- [ ] **`App.tsx` 2,571 lines** — split into `useCall`, `useChat`, `useSpaces`, `useAuth` hooks + route-level components. (Also in 🖥️ Platforms above.)

## 🧹 Code quality / tech debt (deep audit additions)

- [ ] Many `_ = s.DB.Exec(...)` throughout `main.go` — errors silently swallowed. Sweep and log.
- [ ] Many `c.Send(...)` call sites ignore the returned error — dead peer goes undetected. Log failures and close conn.
- [ ] `stories.go` cascade delete on story expiry already handles `story_views` orphans ✅ (confirmed working).
- [ ] `friend_reject` now uses `msg.Recipient` ✅ — but `friend_remove` should be audited for same pattern (currently uses `msg.Recipient` correctly).
