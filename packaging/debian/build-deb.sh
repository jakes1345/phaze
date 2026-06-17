#!/usr/bin/env bash
# Builds a .deb package for the Phaze desktop app.
# Usage: ./build-deb.sh <binary-path> <version> [icon-path]
#   binary-path  path to the compiled Phaze Linux binary
#   version      e.g. 1.5.1
#   icon-path    optional 256x256 PNG icon (defaults to ../../desktop/assets/icon.png)
set -e

BINARY="${1:?Usage: $0 <binary-path> <version> [icon-path]}"
VERSION="${2:?Usage: $0 <binary-path> <version> [icon-path]}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ICON="${3:-$SCRIPT_DIR/../../desktop/assets/icon.png}"
OUT_DIR="${SCRIPT_DIR}/out"
PKG="phaze_${VERSION}_amd64"
STAGING="${OUT_DIR}/${PKG}"

echo "→ Staging .deb at ${STAGING}"
rm -rf "${STAGING}"
mkdir -p \
    "${STAGING}/DEBIAN" \
    "${STAGING}/usr/bin" \
    "${STAGING}/usr/share/applications" \
    "${STAGING}/usr/share/icons/hicolor/256x256/apps"

# Binary
install -m 0755 "${BINARY}" "${STAGING}/usr/bin/phaze"

# Desktop entry
install -m 0644 "${SCRIPT_DIR}/phaze.desktop" "${STAGING}/usr/share/applications/phaze.desktop"

# Icon
if [ -f "${ICON}" ]; then
    install -m 0644 "${ICON}" "${STAGING}/usr/share/icons/hicolor/256x256/apps/phaze.png"
fi

# Control file (substitute version)
sed "s/VERSION_PLACEHOLDER/${VERSION}/" "${SCRIPT_DIR}/control" > "${STAGING}/DEBIAN/control"

# Post-install hook
install -m 0755 "${SCRIPT_DIR}/postinst" "${STAGING}/DEBIAN/postinst"

# Build
dpkg-deb --build --root-owner-group "${STAGING}" "${OUT_DIR}/${PKG}.deb"
echo "✓ Built ${OUT_DIR}/${PKG}.deb"
