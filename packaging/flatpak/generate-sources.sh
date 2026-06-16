#!/usr/bin/env bash
# Generates the offline dependency source files required for a Flatpak build.
# Run this once after updating go.sum or package-lock.json, then commit the output.
#
# Requires:
#   pip install flatpak-builder-tools
#   (or: pip install flatpak-node-generator aiohttp)
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$SCRIPT_DIR/../.."

echo "→ Generating Go module sources..."
flatpak-go-get-generator \
  "$ROOT/desktop/go.sum" \
  -o "$SCRIPT_DIR/generated-go-sources.json"

echo "→ Generating Node module sources..."
flatpak-node-generator npm \
  "$ROOT/web/package-lock.json" \
  -o "$SCRIPT_DIR/generated-node-sources.json"

echo "✓ Done. Commit generated-go-sources.json and generated-node-sources.json."
