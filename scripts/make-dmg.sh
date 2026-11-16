#!/bin/bash
# Builds a compressed, read-only DMG with the app and an Applications shortcut.
# Usage: scripts/make-dmg.sh path/to/Headroom.app path/to/Headroom.dmg
set -euo pipefail
APP="$1"; DMG="$2"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "Headroom" -srcfolder "$STAGE" -ov -format UDZO -fs HFS+ "$DMG" >/dev/null
codesign --force -s "${SIGN_IDENTITY:--}" "$DMG"
echo "Built $DMG ($(du -h "$DMG" | cut -f1))"
