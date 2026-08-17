#!/bin/bash
#
# build-on-pi.sh - build GlowPanel on the Raspberry Pi itself.
#
# Building on the Pi avoids the CGO cross-compilation problem: Wails links
# against WebKitGTK, so a plain GOOS=linux GOARCH=arm64 build from macOS will
# not work without a full cross toolchain and arm64 webkit headers.
#
# Go compiles comfortably in the Pi's ~600MB of free RAM. There is no npm step
# because the frontend is plain HTML/CSS/JS with no bundler.
#
# Usage, from this directory on the Pi:
#   ./build-on-pi.sh
#
# Result: ./build/bin/glowpanel

set -euo pipefail

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Installing build dependencies"
sudo apt-get update -qq
# libwebkit2gtk-4.1-dev is the important one. Debian 13 ships only the 4.1 API;
# there is no 4.0 package, which is why the build needs -tags webkit2_41 below.
sudo apt-get install -y golang-go gcc pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev

export PATH="$PATH:$HOME/go/bin"

if ! command -v wails >/dev/null; then
    echo "==> Installing the Wails CLI (first run only, takes a few minutes)"
    go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
fi

echo "==> Resolving modules"
go mod tidy

echo "==> Building"
# Wails builds with -trimpath and strips the VCS information Go normally embeds,
# so the commit the About view reports has to be passed in by hand. Empty when
# this is not a git checkout, which drops the row rather than showing a blank.
REVISION="$(git rev-parse --short HEAD 2>/dev/null || true)"
if [[ -n "$REVISION" && -n "$(git status --porcelain 2>/dev/null)" ]]; then
    REVISION="$REVISION (modified)"
fi
[[ -n "$REVISION" ]] && echo "    revision: $REVISION"

# webkit2_41 selects the WebKitGTK 4.1 API. Without it the build fails looking
# for webkit2gtk-4.0, which does not exist on Debian 13.
wails build -tags webkit2_41 -platform linux/arm64 \
    -ldflags "-X 'main.gitRevision=$REVISION'"

echo
echo "Built: $(pwd)/build/bin/glowpanel"
echo "Run it from the Pi's desktop session, not over plain SSH:"
echo "  DISPLAY=:0 WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/run/user/1000 ./build/bin/glowpanel"
