# Phaze

Chat app built as a Skype replacement. DMs, group spaces, voice/video calls, screen share, end-to-end encryption. Free.

**Live at [phazechat.world](https://phazechat.world)**

---

## What's in the repo

| Directory | What it is |
|---|---|
| `nexus_server/` | Go WebSocket relay — handles auth, messaging, calls, file uploads, spaces |
| `web/` | React/TypeScript web app (Vite) — the main client most people use |
| `desktop/` | Wails desktop app (Go + the same web frontend) — Windows, macOS, Linux |
| `android/` | Android app — Kotlin + Jetpack Compose |

## Features

- End-to-end encrypted DMs (NaCl box)
- Voice and video calls with screen share (WebRTC)
- Group Spaces — text channels + voice rooms
- Stories (24h expiry)
- Push notifications (Android + web)
- TOTP 2FA
- Skype history import — upload your Skype export zip and messages appear in your DMs
- Cross-device sign-in via QR code or recovery PIN
- HttpOnly cookie sessions (tokens never in localStorage)

## Running locally

**Server:**
```bash
cd nexus_server
go build -o phaze-nexus .
./phaze-nexus
```

**Web client (dev):**
```bash
cd web
cp .env.example .env.local   # set VITE_NEXUS_WS=ws://localhost:8080/ws
npm install
npm run dev
```

**Desktop app:**
```bash
cd desktop
wails dev
```

**Android:**  
Open `android/` in Android Studio and run on a device or emulator.

## Self-hosting

See `docs/DEPLOY_SELF_HOSTED.md`. Short version: run `phaze-nexus` behind nginx/Caddy with TLS, point your domain at it. A `docker-compose.yml` is in `nexus_server/` if you prefer containers.

TURN server config is in `scripts/phaze_turnserver.conf`. Set `PHAZE_TURN_URL` and `PHAZE_TURN_SECRET` on the relay to use your own coturn instance — if you don't, it falls back to a free public relay which has bandwidth limits.

## Deployment

The hosted instance deploys automatically to Fly.io on push to master via GitHub Actions.

## Security

DMs are encrypted on the client before they're sent — the server never sees plaintext. If you find a vulnerability, open a private issue or email through the contact on phazechat.world.

See `SECURITY.md` for the full disclosure policy.

## Contributing

Bug reports are the most useful thing right now, especially from real devices. If something broke during registration, adding a contact, or making a call — open an issue with what happened.

---

*Not affiliated with Microsoft or Skype.*
