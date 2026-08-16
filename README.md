# GlowPanel

A Wails desktop app for the [GlowKitchen](https://github.com/brennanMKE/GlowKitchen)
LED strips. Big brightness slider, big theme buttons — usable by a child without
instructions.

Does the same job as the `glow-*.sh` cron scripts, interactively.

Runs on Raspberry Pi 3B, Raspberry Pi OS 13 (trixie), arm64, under the labwc
Wayland session.

![Glow Panel showing six theme buttons, the brightness slider at 100%, the power switch in the header, and per-strip status chips](GlowPanel.png)

## What it does

- **Six theme buttons** with colour and emoji, sized for small hands
- **Brightness** 0–100% in steps of 5, converted to the firmware's 0–225 scale
- **On / Off** as a switch in the header, next to the connection status
- **Live status** per strip, pushed from `lights/+/state` as the strips report

## Design notes

**No npm.** The frontend is plain HTML/CSS/JS. Wails injects its Go bindings on
`window.go.main.App`, so no bundler is needed. A Vite build is the step most
likely to fail in the Pi's ~600 MB of free RAM, so it is simply not there.

**Event driven, not polled.** The strips publish to `lights/+/state` when they
change; the Go side pushes a `status` event at the frontend only when the cached
view actually differs, so an idle panel does no work. Two slow timers remain: a
local re-render every 60s to keep the "last seen" ages honest, and a `STATUS`
request every 5 minutes to pick up a strip that rebooted. Bringing the window
back into focus also asks for a report, throttled to one message per 15s.

**Background traffic never changes the lights.** The only thing GlowPanel
publishes unprompted is `STATUS` on `lights/all/cmd` — a read-only query, sent
with the retain flag off so nothing lingers on the broker for a device to replay
and act on when it reconnects. Themes, brightness and power go through
`Broker.Publish`, which is reached from a button press and nothing else.

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
| `build/appicon.png` | Source asset, installed into the icon theme |
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
