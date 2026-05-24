# Phaze TURN/STUN — self-hosted coturn

Replaces the public openrelay.metered.ca fallback with a dedicated TURN
relay you control. Better latency, no shared-bandwidth jitter, no rate
limits.

## Deploy

From this directory (`infra/coturn/`):

```bash
# 1. Create the app (does NOT deploy yet)
flyctl launch --copy-config --no-deploy --name phaze-turn

# 2. Generate a strong shared secret + set it on both apps
TURN_SECRET=$(openssl rand -hex 32)
flyctl secrets set TURN_SECRET="$TURN_SECRET" -a phaze-turn
flyctl secrets set PHAZE_TURN_SECRET="$TURN_SECRET" \
                   PHAZE_TURN_URL="turn:phaze-turn.fly.dev:3478" \
                   -a skype7-reborn

# 3. Allocate a dedicated IPv4 (TURN clients are cranky about Anycast)
flyctl ips allocate-v4 -a phaze-turn

# 4. Pin coturn to that IP so it advertises the right address
PUBLIC_IP=$(flyctl ips list -a phaze-turn | awk '/^v4/ {print $2; exit}')
flyctl secrets set PUBLIC_IP="$PUBLIC_IP" -a phaze-turn

# 5. Deploy
flyctl deploy -a phaze-turn

# 6. Redeploy the main app so it picks up the new TURN config
flyctl deploy --remote-only -a skype7-reborn
```

## Verify

After deploy, `https://phazechat.world/health` should show:

```
"turn_configured": true,
"turn_public_fallback": false,
```

Then run Trickle ICE on a real call:
https://webrtc.github.io/samples/src/content/peerconnection/trickle-ice/

Add server: `turn:phaze-turn.fly.dev:3478`, username/credential pair
the nexus generates via `generateMediaToken`. You should see `relay`
candidates returned within ~200ms.

## What this gives you

- 1:1 video/audio calls work behind every NAT (including symmetric)
- Group voice rooms in Spaces get reliable relays for laggy peers
- Livestreams use direct WebRTC, so don't depend on TURN; this is for
  the bidirectional call path only.

## When to scale up

If your relay ports saturate, edit fly.toml to add more UDP port
sections (or move to a port range and use `flyctl ips allocate-v4`
per machine). For >100 concurrent calls, swap coturn for LiveKit or
mediasoup with a real SFU — TURN is point-to-point and doesn't scale
the way an SFU does.
