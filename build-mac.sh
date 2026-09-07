#!/bin/bash
#
# build-mac.sh - build the macOS app bundle as "Glow Panel.app".
#
# This exists because the bundle has to be called "Glow Panel.app" and the
# Linux binary has to be called "glowpanel", and Wails takes both from the same
# setting: wails.json's outputfilename. It is "glowpanel" because that is what
# Linux needs - install-desktop.sh installs build/bin/glowpanel to
# /usr/local/bin/glowpanel, and glowpanel.desktop's Exec= line names that path.
# Changing the setting to get a nicely-named .app would break the Pi deploy
# silently, at install time rather than at build time.
#
# `wails build -o "Glow Panel"` does NOT do it. Wails v2.13 accepts the flag,
# prints it back as "Output File", and still writes build/bin/glowpanel.app,
# because on darwin the bundle directory is named from outputfilename.
#
# So the bundle is renamed afterwards, which is all that was ever needed: macOS
# finds the binary through CFBundleExecutable (still "glowpanel") and shows the
# name from the bundle's own filename, while CFBundleName and
# CFBundleDisplayName already say "Glow Panel" via info.productName. Renaming
# the directory does not disturb the signature, which covers the contents.
#
# Usage:
#   ./build-mac.sh            Build "build/bin/Glow Panel.app"
#   ./build-mac.sh --run      ...and launch it

set -euo pipefail

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BUILT="build/bin/glowpanel.app"
APP="build/bin/Glow Panel.app"

echo "==> Building"
wails build

[[ -d "$BUILT" ]] || { echo "Wails did not produce $BUILT"; exit 1; }

rm -rf "$APP"
mv "$BUILT" "$APP"

VERSION=$(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$APP/Contents/Info.plist")
echo "==> Built $APP ($VERSION)"

# The rename is only safe if the signature survived it, so say so rather than
# assuming. An ad-hoc signature covers the contents, not the directory name.
codesign --verify --deep "$APP" 2>/dev/null \
    && echo "==> Signature intact" \
    || echo "==> WARNING: signature did not verify after the rename"

if [[ "${1:-}" == "--run" ]]; then
    open "$APP"
fi
