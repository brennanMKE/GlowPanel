#!/bin/bash
#
# build-on-pi.sh - build GlowPanel on the Raspberry Pi itself.
#
# Building on the Pi avoids the CGO cross-compilation problem: Fyne links to
# the platform graphics stack, so a plain GOOS=linux GOARCH=arm64 build from
# macOS still needs a full cross toolchain and arm64 graphics headers.
#
# Go compiles comfortably in the Pi's ~600MB of free RAM. There is no browser
# engine or frontend build step.
#
# Usage, from this directory on the Pi:
#   ./build-on-pi.sh
#
# Result: ./build/bin/glowpanel

set -euo pipefail

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Installing build dependencies"
sudo apt-get update -qq
sudo apt-get install -y golang-go gcc libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev

echo "==> Resolving modules"
go mod tidy

echo "==> Testing"
go test ./...

echo "==> Building"
mkdir -p build/bin
go build -trimpath -ldflags="-s -w" -o build/bin/glowpanel .

echo
echo "Built: $(pwd)/build/bin/glowpanel"
echo "Run it from the Pi's desktop session, not over plain SSH:"
echo "  DISPLAY=:0 WAYLAND_DISPLAY=wayland-0 XDG_RUNTIME_DIR=/run/user/1000 ./build/bin/glowpanel"
