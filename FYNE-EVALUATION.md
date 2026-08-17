# Fyne vs Wails: porting GlowPanel off WebKit

A record of the `feature/fyne-port` experiment — what was built, what was
measured, and why the Wails build stayed the shipping one.

Evaluated 16 August 2026 on greenpi (Raspberry Pi 3B, Raspberry Pi OS 13, 905 MB
RAM, labwc) and on an arm64 Mac. Both builds were driven against the live MQTT
broker with all five strips reporting.

## The question

The Wails build renders through WebKitGTK. On a 1 GB Pi that means a browser
engine and three processes to draw six buttons and a slider. Fyne draws with Go
and OpenGL in one process. The port was worth trying for three reasons:

1. **Memory.** The Pi is the constrained machine.
2. **Dependencies.** WebKitGTK is a heavy build and runtime dependency, and
   Debian's 4.0 → 4.1 API move had already broken the build once.
3. **Process count.** One process is easier to reason about than three on a
   machine that runs unattended for weeks.

## How memory was measured

Three numbers appear below, and the difference between them is the whole story.

**RSS (Resident Set Size)** — pages of a process's address space currently in
RAM. Shared pages are counted *in full for every process that maps them*. Three
WebKit processes mapping the same library each report all of it, so summing RSS
across an app's processes multiply-counts memory that exists once. It is the
most quoted number and the most misleading one here.

**PSS (Proportional Set Size)** — the same accounting, except each shared page is
divided by the number of processes sharing it. Summing PSS across processes
gives a total that means something. PSS is Linux-only, read from
`/proc/<pid>/smaps_rollup`.

**MemAvailable delta** — the kernel's own estimate of memory available for new
work, sampled before and during a run. This is the number that decides whether
the Pi starts swapping, and the one to trust.

macOS has no PSS (no `/proc`). Its equivalent is **physical footprint**, the
number Activity Monitor shows: memory the process is *charged* for — dirty
anonymous pages, compressed pages, GPU/IOKit allocations — excluding clean
file-backed pages from shared system frameworks. Closer in spirit to PSS than to
RSS. It can exceed RSS, because compressed and GPU memory are not resident
pages.

Method: launch, idle 30 s, sample, kill. Both builds measured back to back in
the same session.

## Results: greenpi

| | processes | Σ RSS | Σ PSS | MemAvailable drop |
|---|---|---|---|---|
| **Fyne** | 1 | **142 MB** | **138 MB** | **69 MB** |
| **Wails** | 3 | 372 MB | 209 MB | 72 MB |

Wails, broken out:

| process | RSS | PSS |
|---|---|---|
| glowpanel | 151 MB | 85 MB |
| WebKitWebProcess | 161 MB | 98 MB |
| WebKitNetworkProcess | 60 MB | 26 MB |

The 372 MB total reproduces the figure already in the README (371.9 MB), so the
older measurement was sound.

**The headline is not the finding.** Summed RSS suggests Fyne uses 62 % less.
PSS narrows that to 34 %. The memory that actually stops being available differs
by **3 MB**. Most of what WebKit occupied was shared, file-backed library code
the kernel could already reclaim under pressure. The Fyne build's memory is
nearly all private and anonymous — its PSS is 138 against an RSS of 142, sharing
almost nothing with anyone.

## Results: macOS

Physical footprint, the number Activity Monitor reports:

| | | footprint |
|---|---|---|
| **Fyne** | single process | **154 MB** |
| **Wails** | app 27 + WebContent 24 + GPU 13 + Networking 5 | **69 MB** |

The comparison **inverts on the Mac**: Fyne costs about 85 MB more. macOS
supplies WKWebView as shared system frameworks, so the Wails app barely pays for
its renderer, while Fyne carries renderer, font atlas and Go runtime in-process.
By RSS the Wails app reads 104.7 MB; by footprint it is 27 MB. Most of its
resident memory is framework code it is not charged for.

## Other measurements

| | Fyne | Wails |
|---|---|---|
| UI code | 1,448 lines Go | 652 lines (HTML/CSS/JS + 54 line `main.go`) |
| Binary | 22.5 MB | 8.7 MB |
| Pi build deps | `gcc libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev` | `libwebkit2gtk-4.1-dev` |
| Pi runtime deps | none beyond the system graphics stack | `libwebkit2gtk-4.1-0`, `fonts-noto-color-emoji` |
| Emoji | six licensed PNGs embedded in the binary | system font |

Build dependencies are close to a wash: Fyne trades one large package for five.
The genuine dependency win is at **runtime** — deploying to another Pi becomes
copy-the-binary, with no WebKit runtime and no emoji font to install.

## What the port cost to look right

The first Fyne pass used stock widgets and looked it. Reaching something
comparable to the CSS build took four files of hand-drawn rendering: a theme
tile painting a 135° gradient with a top wash and edge highlight, a custom
slider, a power toggle, connection dot and device chips, on a custom theme.

Two things could not be reproduced, only substituted:

- **Emoji.** Fyne's bundled font has patchy emoji coverage and renders
  differently across platforms. The fix was embedding bitmaps — Microsoft Fluent
  Emoji 3D, 256 px, MIT. Adding a seventh theme now means sourcing an asset, not
  writing a line.
- **Depth.** Gradients, shadows and rounded edges that CSS expresses in a
  declaration are a per-pixel raster function here.

Even after that work, the WebKit build was judged to look better.

## Outstanding issue

Both platforms print:

```
*** This application has not been migrated to the fyne.Do threading model ***
*** The next major Fyne release will remove this safety! ***
```

Something still touches widgets off the main goroutine. Fyne 2.8 tolerates it;
the next major release will not. This must be fixed before any Fyne build ships.

## Conclusion

**Wails stays.** The case for the port rested on memory, and on the Pi the real
figure is 3 MB, not 230 MB. Against that: the Mac footprint more than doubles,
the UI code roughly doubles, the result looks worse after genuine iteration, and
emoji become assets to manage. For a panel in a community space, a UI anyone who
knows CSS can edit is worth more than a single-process architecture.

**What Fyne does win**, and would justify revisiting:

- one process, no browser engine to grow or leak over long uptimes
- no `WebviewGpuPolicyNever` workaround for WebKitGTK flicker under labwc
- immunity to Debian's WebKitGTK API churn
- copy-the-binary deployment

**Revisit if:** WebKitGTK breaks on a distro upgrade, the WebProcess grows in the
field over weeks of uptime, or a minimal-package kiosk build is wanted.

## Caveat worth testing

Both builds were sampled 30 seconds after launch, idle. The README already warns
that the WebProcess grows under sustained use, and greenpi runs for weeks. If
Wails drifts upward over days while Fyne stays flat, the 3 MB becomes something
real. Leaving both running for a day and re-measuring is the experiment that
would settle it.

## What came back to the Wails build

The About view. Two panels drawing the same controls made "which build is this?"
a genuinely hard question to answer by eye — it was mistaken for the other one
more than once during this evaluation. It reads the toolkit out of the linked
module graph rather than a constant, so a binary cannot claim a toolkit it was
not linked against.

The branch is kept as a working reference, not merged.
