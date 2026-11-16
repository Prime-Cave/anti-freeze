#!/bin/bash
# Builds a compressed, read-only DMG with the app and an Applications shortcut.
# Usage: scripts/make-dmg.sh path/to/Anti-Freeze.app path/to/Anti-Freeze.dmg
set -euo pipefail
APP="$1"; DMG="$2"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "Anti-Freeze" -srcfolder "$STAGE" -ov -format UDZO -fs HFS+ "$DMG" >/dev/null
# A real identity gets a secure timestamp, which notarization requires.
if [ "${SIGN_IDENTITY:--}" = "-" ]; then
  codesign --force -s - "$DMG"
else
  codesign --force --timestamp -s "$SIGN_IDENTITY" "$DMG"
fi
echo "Built $DMG ($(du -h "$DMG" | cut -f1))"
