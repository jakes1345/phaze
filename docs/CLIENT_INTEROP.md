# Web, desktop, and mobile — working together

All Phaze clients are designed around **one relay** (Nexus) and **one account database** (`nexus_server` SQLite by default). If every client points at the **same Nexus URL** and speaks the **same `NexusMessage` wire types**, users see one roster, one friend graph, and one chat history model (server-side offline queue + client-side logs where applicable).

## What is already shared

| Capability | Nexus | Desktop (Go/Fyne) | Android (Kotlin) | Web (`web/`) |
|-------------|-------|-------------------|------------------|--------------|
| Same WS JSON (`NexusMessage`) | Yes | Yes | Yes | Yes |
| `auth` / `session_auth` / TOTP | Yes | Yes | Yes | Yes |
| Friends, presence, `msg` relay | Yes | Yes | Yes | Yes |
| Pairwise E2EE (NaCl box) + `key_request` | Yes | Yes | Yes | Yes |
| Group `convo_*` + `envelopes` | Yes | Yes | Yes | UI not in web yet |
| WebRTC signaling (`call_*`, `ice_*`) | Forwards | Pion | WebRTC | Not wired in web yet |
| QR code device linking (`link_create`, `link_check`, `link_approve`) | Yes | Yes | Yes (camera+gallery) | Yes |
| **E2EE Key Backup/Restore** (`key_backup_put`, `key_backup_get`, `key_backup_result`) | Yes | Yes | Yes | Yes |

## E2EE Key Backup Protocol

The key backup protocol is **identical across all three clients** (Go desktop, Kotlin Android, TypeScript web):

1. **Encrypt**: `PBKDF2-SHA256` (200 000 iterations, 16-byte salt) → 256-bit AES key → `AES-256-GCM` (12-byte IV) over a JSON payload `{"pub": "<b64>", "sec": "<b64>"}`.
2. **Wire (backup)**: `key_backup_put { type, sender, token: "<JSON blob string>" }` — Nexus stores the blob server-side per account.
3. **Wire (restore)**: `key_backup_get { type, sender }` → server responds `key_backup_result { token: "<JSON blob>" }`, client decrypts with PIN and persists the new keypair locally.
4. **Blob format**: `{ ciphertext: "<b64(IV+GCM_CT)>", salt: "<b64>", iterations: <int> }` — all clients read/write this exact shape.
5. **Error flow**: any server-side error returns `key_backup_result { error: "..." }`.

## How to run them "together" in practice

1. **Run one Nexus** (or use your deployed `wss://phazechat.world/ws`).
2. **Point every client at that gateway**
   - Desktop: built-in production URL default.
   - Android: same hardcoded production URL in `NexusClient`.
   - Web: `VITE_NEXUS_WS` in `.env.local` (see `web/.env.example`).
3. **CORS**: for browser access, set `Phaze_ALLOWED_ORIGINS` on Nexus to your web origin.
4. **Same username** everywhere = same account; session tokens issued at login work for `session_auth` on any surface.

## Known gaps (interop you should plan for)

1. **Voice/video web ↔ native** — Native registers **PCMU** (and VP8/…) in Pion; browsers usually prefer **Opus**. Until the web app implements `RTCPeerConnection` **and** you align codecs, **calls** are native↔native; **chat** is already cross-platform.
2. **Web registration** — Server supports `register` / `verify_email`; the web UI still assumes you can log in after registering on desktop. Adding the full wizard on web removes that friction.
3. **Group convos on web** — `convo_*` message types relay through Nexus correctly; the web client UI doesn't expose a group-chat creation flow yet.

## Single sentence

**Chat, social features, device linking (QR/code), and E2EE key backup already interoperate across web, desktop, and Android via Nexus; finish web WebRTC + codec alignment for calls and add web group-chat UI to reach full feature parity.**
