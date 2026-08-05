#!/bin/bash
#
# install-desktop.sh - make GlowPanel a real desktop application.
#
# A Linux desktop app is not a bundle. It is three separate things:
#
#   1. the executable, somewhere on PATH        -> /usr/local/bin/glowpanel
#   2. an icon, in the icon theme               -> hicolor/256x256/apps/glowpanel.png
#   3. a .desktop file describing the launcher  -> /usr/share/applications/
#
# The .desktop file is what puts it in the applications menu and lets it be
# double-clicked. Run this on the Pi after building.
#
# Usage:
#   ./install-desktop.sh            Install to the menu
#   ./install-desktop.sh --desktop  ...and drop a launcher on the desktop too

set -euo pipefail

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$SRC_DIR/build/bin/glowpanel"
ICON_SRC="$SRC_DIR/build/appicon.png"

[[ -x "$BIN" ]] || { echo "Not built yet: $BIN"; echo "Run ./build-on-pi.sh first."; exit 1; }

echo "==> Installing binary"
sudo install -Dm755 "$BIN" /usr/local/bin/glowpanel

echo "==> Installing icon"
sudo install -Dm644 "$ICON_SRC" \
    /usr/share/icons/hicolor/256x256/apps/glowpanel.png

echo "==> Installing desktop entry"
sudo install -Dm644 "$SRC_DIR/glowpanel.desktop" \
    /usr/share/applications/glowpanel.desktop

echo "==> Refreshing caches"
# Without these the menu may not notice the new entry until the next login.
sudo update-desktop-database /usr/share/applications 2>/dev/null || true
sudo gtk-update-icon-cache -f /usr/share/icons/hicolor 2>/dev/null || true

if [[ "${1:-}" == "--desktop" ]]; then
    echo "==> Adding a desktop launcher"
    DESK="$(xdg-user-dir DESKTOP 2>/dev/null || echo "$HOME/Desktop")"
    mkdir -p "$DESK"
    install -Dm644 /usr/share/applications/glowpanel.desktop "$DESK/glowpanel.desktop"
    # Mode 644, deliberately NOT executable. That is GNOME/Nautilus semantics,
    # where a launcher must be executable and "trusted". pcmanfm, which draws
    # the desktop on Raspberry Pi OS, is the opposite: an executable file makes
    # libfm show its "Execute / Execute in Terminal / Open" dialog instead of
    # just launching. Every stock launcher in /usr/share/applications is 644.
fi

echo
echo "Done. GlowPanel should now appear under Menu > Accessories."
echo "Launch from a terminal with:  glowpanel"
