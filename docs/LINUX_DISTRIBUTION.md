# Linux distribution — Phaze desktop

Phaze ships a native Linux desktop app (Wails/WebKit2GTK) via three channels:

## APT repository (Ubuntu/Debian — recommended)

Add the repo once and Update Manager handles future upgrades automatically.

```bash
# 1. Signing key
curl -fsSL https://apt.phazechat.world/apt/phaze.gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/phaze.gpg

# 2. Repository
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/phaze.gpg] \
  https://apt.phazechat.world/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/phaze.list

# 3. Install
sudo apt update && sudo apt install phaze
```

Requires Ubuntu 22.04+ or Debian 12+, amd64.

### DNS / GitHub Pages setup (one-time)

| What | Value |
|------|-------|
| DNS CNAME | `apt` → `jakes1345.github.io` |
| GitHub Pages custom domain | `apt.phazechat.world` |
| APT files | `docs/apt/` on master, auto-deployed by pages.yml |

### Releasing a new version

`apt-repo.yml` fires automatically on every GitHub Release and requires
one secret: **`APT_SIGNING_KEY`** (GPG private key, armored).

**One-time GPG key generation:**
```bash
gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: Phaze Releases
Name-Email: support@phazechat.world
Expire-Date: 0
%no-protection
EOF
gpg --armor --export-secret-keys support@phazechat.world
```

Paste the output as **`APT_SIGNING_KEY`** in GitHub → Settings → Secrets.

## Flatpak / Flathub

Manifest is at `packaging/flatpak/world.phazechat.Phaze.yml`.
Flathub submission is a separate PR against [flathub/flathub](https://github.com/flathub/flathub).

## Raw binary

Download `Phaze-linux-amd64` from [GitHub Releases](https://github.com/jakes1345/skype7-reborn/releases),
`chmod +x`, run directly.
