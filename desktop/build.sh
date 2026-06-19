#!/usr/bin/env bash
# Builds the Phaze desktop app.
# Usage: ./build.sh [--dev]
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WEB_DIR="$SCRIPT_DIR/../web"
DIST_DIR="$SCRIPT_DIR/dist"

if [[ "$1" == "--dev" ]]; then
  echo "→ Starting Vite dev server (keep this running, then run 'wails dev' in another terminal)"
  cd "$WEB_DIR" && VITE_BASE=/ npm run dev
  exit 0
fi

echo "→ Building web frontend..."
cd "$WEB_DIR"
npm ci --no-audit --no-fund
VITE_BASE=/ npm run build

echo "→ Copying dist to desktop/dist/..."
rm -rf "$DIST_DIR"
cp -r "$WEB_DIR/dist" "$DIST_DIR"

echo "→ Building Wails app..."
cd "$SCRIPT_DIR"
~/go/bin/wails build -tags webkit2_41

echo "✓ Build complete — binary at desktop/build/bin/Phaze"
