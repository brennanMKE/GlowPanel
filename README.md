# GlowPanel

A Wails desktop app for the [GlowKitchen](https://github.com/brennanMKE/GlowKitchen)
LED strips. Big brightness slider, big theme buttons — usable by a child without
instructions.

Does the same job as the `glow-*.sh` cron scripts, interactively.

Runs on Raspberry Pi 3B, Raspberry Pi OS 13 (trixie), arm64, under the labwc
Wayland session — and on macOS as a native `.app`.

![GlowPanel showing the Looks tab: six theme buttons, a Looks/Effects toggle, a running-effect banner reading "Scan · 3:58 left" with a Stop button, the brightness slider at 75%, On/Off and connection status in the header, and per-strip status chips](GlowPanel.png)

## What it does

- **Six theme buttons** with colour and emoji, sized for small hands
- **Nine effect presets** behind a Looks/Effects toggle, with a duration and a
  Stop button
- **Brightness** 0–100% in steps of 5, converted to the firmware's 0–225 scale
- **On / Off** as a switch in the header, next to the connection status
- **Live status** per strip, pushed from `lights/+/state` as the strips report

## Effects

![GlowPanel showing the Effects tab: nine preset buttons with Cylon selected, a Run For duration row with 5 min selected, and a banner reading "Scan · 4:26 left" beside a Stop button](GlowPanelEffects.png)

A theme is a look the strips hold. An effect is something they *do* — it runs,
it can time out, and when it stops the strips go back to whatever theme was
showing. The firmware exposes them through one command, `SET_EFFECT:` followed
by a JSON body, and `CLEAR_EFFECT` to stop.

The nine presets are the tuned defaults from GlowKitchen's
`scripts/demo_effect.sh`, copied into `effects.go` rather than read from it — a
Pi that only has the binary has no copy of that script. Each carries its mode,
palette, speed and intensity; the panel adds the timeout from the "Run for" row.

**Effects share the theme panel rather than taking a second screen.** The
brightness slider and the power switch stay reachable whichever grid is showing,
and there is no navigation for a child to get lost in. The running banner and
the Stop button also appear on the Looks tab while something is running, so
stopping an effect never requires finding the tab it was started from.

**Effects are never published retained**, whatever `RETAIN` says in `glow.conf`.
An effect is an event, not a configuration: a retained `SET_EFFECT` is replayed
to every device on every reconnect, and one carrying a timeout restarts its
countdown each time. This is not hypothetical — a retained `FOREST` sitting on
the broker used to snap a strip back to Forest seconds after every effect, and
survived reboots of the board and the broker both, because nothing was
publishing it. MQTT was replaying it. `SetTheme` used to hard-code the same
mistake and now follows the configured flag like brightness and power do.

**Everything is validated before it is sent.** The firmware answers a malformed
payload by changing nothing and publishing nothing — no error topic, no NACK,
the reason goes to its serial log alone. A rejection on the wire is
indistinguishable from a message that never arrived, so `buildEffectPayload`
checks the mode, the colour format, the 0–255 ranges and the 512-byte device
buffer first. `effects_test.go` runs every shipped preset through it.

**The countdown comes from the device, not from a local timer.** While an effect
is running the strip's `STATUS` reply gains a `custom` object with the mode and
the seconds remaining, and the panel polls for it every 5 seconds — the strip's
clock survives reboots the panel's would not. The displayed seconds tick locally
between polls purely so the number moves; every reply snaps it back. A `custom`
object is absent whenever the theme is not `Custom`, which means no effect
rather than an error.

**Strobe is capped at 60 seconds** even when "Until stopped" is selected. The
other eight presets take the duration as given.

See `docs/glowpanel_effects_integration.md` in the GlowKitchen repo for the
command reference and the firmware-side details.

## Design notes

**No npm.** The frontend is plain HTML/CSS/JS. Wails injects its Go bindings on
`window.go.main.App`, so no bundler is needed. A Vite build is the step most
likely to fail in the Pi's ~600 MB of free RAM, so it is simply not there.

**Event driven, not polled.** The strips publish to `lights/+/state` when they
change; the Go side pushes a `status` event at the frontend only when the cached
view actually differs, so an idle panel does no work. Three timers remain: a
local re-render every 60s to keep the "last seen" ages honest, a `STATUS`
request every 5 minutes to pick up a strip that rebooted, and — only while an
effect is counting down — a `STATUS` request every 5s to keep the banner's
remaining seconds honest. Bringing the window back into focus also asks for a
report, throttled to one message per 15s.

**Background traffic never changes the lights.** The only thing GlowPanel
publishes unprompted is `STATUS` — a read-only query, sent with the retain flag
off so nothing lingers on the broker for a device to replay and act on when it
reconnects. Themes, brightness, power and effects go through `Broker.Publish`,
which is reached from a button press and nothing else.

**One message, not one per device.** Everything goes to `lights/all/cmd`, which
every strip subscribes to. `Broker.Publish` used to loop over the names in
`glow.conf` and send an identical copy to each — five times the traffic, and
silent towards any strip the config did not happen to list, such as one being
staged onto new firmware. `DEVICES` now only decides which strips get a status
chip in the footer.

One wrinkle comes with it: a strip running an effect ignores a *theme* arriving
on the broadcast topic, because device state wins over a fleet-wide command
(GlowKitchen issue #0020). Pressing a theme button is how anyone gets out of an
effect without hunting for Stop, so `SetTheme` sends `CLEAR_EFFECT` first when
something is running.

**Shares `glow.conf`.** Config comes from `~/.config/glowkitchen/glow.conf` —
the same file the GlowKitchen `install.sh` already wrote for the cron scripts.
One broker address, one copy of the password, no drift. `config.go` parses the
small subset of shell syntax that file uses, including `DEVICES=(a b c)`.

**Fixed header, scrolling middle, fixed footer.** The window used to cut off the
bottom of the page at its default size. Now the header (title, power switch,
connection status) and the footer (status chips) are pinned, and only the middle
scrolls — so nothing can become unreachable however short the window gets. It
fits without scrolling at the default 900×660 and down to about 480px tall,
where a media query shrinks the theme buttons rather than adding a scrollbar.

Themes come first because they are what anyone walking up to the panel wants,
and pressing one also turns the strips on. Power went from a pair of 76px
buttons filling a panel at the bottom — the part that got cut off — to one
switch in the header.

**GPU disabled deliberately.** `WebviewGpuPolicyNever` in `main.go`: the Pi 3B's
VideoCore IV gives WebKitGTK nothing useful, and requesting acceleration causes
flicker and occasional blank surfaces under labwc. It falls back to software
rendering and logs a GL warning on startup, which is expected.

## Building

Build **on the Pi**. Wails links against WebKitGTK through CGO, so a
`GOOS=linux GOARCH=arm64` build from macOS needs a full cross toolchain plus
arm64 webkit headers — not worth the trouble when Go compiles fine on the Pi.

```bash
scp -r glowpanel greenpi:~/
ssh greenpi 'cd ~/glowpanel && ./build-on-pi.sh && ./install-desktop.sh --desktop'
```

`build-on-pi.sh` installs `golang-go`, `libwebkit2gtk-4.1-dev` and friends,
fetches the Wails CLI, and builds. First run pulls roughly 300 MB and takes
several minutes on an A53; later builds are fast.

The one non-obvious flag:

```bash
wails build -tags webkit2_41
```

Debian 13 ships **only** the WebKitGTK 4.1 API — there is no
`libwebkit2gtk-4.0-37` package at all. Wails v2 defaults to 4.0, so without this
tag the build fails on a missing dependency.

## Building for macOS

Unlike the Pi build, this one is straightforward — macOS uses the system
WKWebView, so there is no CGO/webkit dependency to wrestle with.

```bash
brew install go
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails build -platform darwin/arm64 -ldflags "-X 'main.gitRevision=$(git rev-parse --short HEAD)'"
mv build/bin/glowpanel.app "build/bin/Glow Panel.app"   # nicer name in Finder
```

The `-ldflags` stamp is what puts a commit in the About view. Wails builds with
`-trimpath` and strips the VCS information Go would otherwise embed, so without
it the dialog can only report a version that rarely changes. Omitting the flag
is harmless — the Revision row is simply left out.

Produces a real bundle at `build/bin/Glow Panel.app` — 8.7 MB, arm64, with
`build/appicon.png` converted to `iconfile.icns`. Drag it to `/Applications`.

Bundle metadata lives in `build/darwin/Info.plist`; the bundle identifier is
`com.brennanmke.glowpanel`. `CFBundleName` and `CFBundleDisplayName` both come
from `productName` in `wails.json`, so the menu bar reads **Glow Panel** while
the binary, the repo and the bundle ID stay `glowpanel`.

### Signing

Wails ad-hoc signs the bundle, so `spctl` reports **rejected** and it has no
Team ID. That is fine for a locally built app: Gatekeeper only blocks bundles
carrying a `com.apple.quarantine` attribute, which downloads get and local
builds do not.

It matters the moment you send it to anyone else — over AirDrop, a download, or
a DMG — at which point it needs Developer ID signing and notarization or the
recipient gets "unidentified developer".

### Configuration on macOS

The Mac has no `glow.conf` by default. The app falls back to environment
variables (`MQTT_PASSWORD`, `MQTT_USER`, `GLOW_BROKER`, `GLOW_PORT`,
`GLOW_DEVICES`), which covers launching from a terminal.

**A Finder-launched app bundle does not inherit your shell environment**, so for
double-click launching you need a real config file:

```bash
mkdir -p ~/.config/glowkitchen && chmod 700 ~/.config/glowkitchen
cat > ~/.config/glowkitchen/glow.conf <<'EOF'
BROKER="your.broker.address"
PORT="1883"
MQTT_USER="mqtt"
MQTT_PASSWORD="your-password"
DEVICES=(tv desk kitchen workbench recycling)
EOF
chmod 600 ~/.config/glowkitchen/glow.conf
```

Reaching the broker from outside the space relies on a Tailscale subnet router
advertising the network the broker sits on. That is what makes the Mac app
usable remotely.

### macOS footprint

~105 MB resident, against ~372 MB summed RSS on the Pi. WKWebView is provided by
the system and shared, so there is no bundled engine and no separate WebKit
processes counted against the app.

## Desktop integration

There is no app bundle on Linux. `install-desktop.sh` places the three separate
pieces that together make a double-clickable app:

| Piece | Path |
|---|---|
| Executable | `/usr/local/bin/glowpanel` |
| Icon | `/usr/share/icons/hicolor/256x256/apps/glowpanel.png` |
| Launcher | `/usr/share/applications/glowpanel.desktop` |
| Desktop copy | `~/Desktop/glowpanel.desktop` (with `--desktop`) |

### The launcher must NOT be executable

This is the opposite of GNOME/Nautilus, where a desktop launcher must be
executable and carry `metadata::trusted`. Raspberry Pi OS draws the desktop with
**pcmanfm**, and libfm treats an executable file as a script — double-clicking
it opens an *"Execute / Execute in Terminal / Open"* dialog instead of launching
the app. Every stock launcher in `/usr/share/applications` is mode 644, and so
is this one.

Two further details if the dialog ever comes back:

- **pcmanfm caches file info.** A `chmod` on a launcher is not picked up by a
  running desktop; restart it or log out.
- **`quick_exec=1`** in `~/.config/libfm/libfm.conf` suppresses the dialog for
  all executables. Both Pis have it set. pcmanfm rewrites that file from memory
  on a clean exit, so edit it while pcmanfm is stopped or the change is lost.

### Emoji font

Raspberry Pi OS ships no emoji font, so the theme buttons render as tofu boxes
(▯) until one is installed:

```bash
sudo apt install fonts-noto-color-emoji
```

Fontconfig is read at process start, so relaunch the app afterwards.

## Deploying to a second Pi

No rebuild needed for identical hardware and OS. The binary is dynamically
linked, so the target needs the WebKit **runtime** — not the ~300 MB dev
toolchain:

```bash
ssh rainbowpi 'sudo apt install -y libwebkit2gtk-4.1-0 fonts-noto-color-emoji'
scp greenpi:~/glowpanel/build/bin/glowpanel rainbowpi:~/glowpanel/build/bin/
scp install-desktop.sh glowpanel.desktop rainbowpi:~/glowpanel/
ssh rainbowpi 'cd ~/glowpanel && ./install-desktop.sh --desktop'
ssh rainbowpi 'ldd /usr/local/bin/glowpanel | grep "not found" || echo ok'
```

Build on the **older** machine and run on the newer one if their package
versions have drifted. glibc and soname compatibility work forward, not
backward.

## Running

Launched from the menu or the desktop icon it inherits the session environment.
Over SSH there is none, so it has to be supplied:

```bash
ssh greenpi 'XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-0 NO_AT_BRIDGE=1 glowpanel'
```

To start it with the desktop, **append** to `~/.config/labwc/autostart` — that
file also carries the `swayidle` line for screen blanking, so do not replace it:

```
/usr/bin/lwrespawn /home/pi/glowpanel/build/bin/glowpanel &
```

## Measured footprint

On a Pi 3B, idle, immediately after launch:

```
WebKitWebProcess      160.8 MB
glowpanel             151.4 MB
WebKitNetworkProcess   59.8 MB
TOTAL (RSS)           371.9 MB

available: 665 MB -> 598 MB   (-67 MB)
```

Those figures disagree because summed RSS double-counts pages shared between
the three processes. The ~67 MB drop in available memory is the genuinely
unavailable portion; most of the remainder is file-backed library code the
kernel can reclaim under pressure. Budget somewhere between the two, and expect
the WebProcess to grow under sustained use.

The cron scripts keep working with the panel closed, so on a memory-tight Pi it
is reasonable to launch it on demand rather than autostart it.

## Behaviour worth knowing

Pressing a theme button also **turns the lights on**. The firmware re-enables a
disabled strip when it receives a theme — right for a big friendly button, but
it means the panel can override the 02:00 scheduled off. The next scheduled step
puts things back.

Brightness publishes with `RETAIN` from `glow.conf` (true by default), matching
the cron scripts, so a strip that reboots picks up the level it last received.

## Repository layout

| File | Purpose |
|---|---|
| `main.go` | Wails setup, window options, Linux GPU policy |
| `app.go` | Methods bound to the frontend; theme table |
| `mqtt.go` | paho client, publish, `lights/+/state` subscription and cache |
| `config.go` | `glow.conf` parser, percent ↔ 0–225 conversion |
| `frontend/dist/` | `index.html`, `style.css`, `app.js` — no build step |
| `build/appicon.png` | Source asset; icon theme on Linux, `.icns` on macOS |
| `build/darwin/` | macOS bundle metadata (`Info.plist`) |
| `build-on-pi.sh` | Dependencies, Wails CLI, build |
| `install-desktop.sh` | Binary, icon, `.desktop` entry |
| `glowpanel.desktop` | Launcher definition |

This Mac checkout is the source of truth. `~/glowpanel` on greenpi is the build
directory — same source plus `build/bin/`, the Go module cache, and the Wails
CLI. `~/glowpanel` on rainbowpi holds only a staged binary and the install
scripts.

Edit here, `scp` to greenpi, rebuild, reinstall, copy the binary onward.
Anything under `frontend/dist/` needs a rebuild too, since `go:embed` compiles
it into the binary.
