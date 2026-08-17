// GlowPanel frontend.
//
// No framework and no build step: Wails injects the Go bindings on
// window.go.main.App, so plain JS can call them directly. That keeps npm off
// the Pi entirely, which matters on a 1GB machine.

const $ = (id) => document.getElementById(id);

const el = {
    bright: $("bright"),
    pct: $("pct"),
    up: $("up"),
    down: $("down"),
    themes: $("themes"),
    power: $("power"),
    powerLabel: $("powerLabel"),
    aboutBtn: $("aboutBtn"),
    aboutBack: $("aboutBack"),
    aboutRows: $("aboutRows"),
    aboutClose: $("aboutClose"),
    dot: $("dot"),
    connText: $("connText"),
    chips: $("chips"),
    error: $("error"),
};

// While the user is touching the slider we stop letting reported state write to
// it, otherwise the value fights the finger. Cleared shortly after they stop.
let holdUntil = 0;
const hold = (ms = 2500) => { holdUntil = Date.now() + ms; };
const holding = () => Date.now() < holdUntil;

let sendTimer = null;
let activeTheme = null;
let lightsOn = false;

function app() {
    return window.go && window.go.main && window.go.main.App;
}

// --- actions ---------------------------------------------------------------

function showError(msg) {
    if (!msg) { el.error.hidden = true; return; }
    el.error.textContent = msg;
    el.error.hidden = false;
}

// Debounced so dragging the slider does not publish on every step; 180ms is
// short enough to feel immediate but coalesces a full sweep into a few sends.
function queueBrightness(percent) {
    el.pct.textContent = percent + "%";
    hold();
    clearTimeout(sendTimer);
    sendTimer = setTimeout(async () => {
        const a = app();
        if (!a) return;
        showError(await a.SetBrightness(percent));
    }, 180);
}

function nudge(delta) {
    const next = Math.min(100, Math.max(0, Number(el.bright.value) + delta));
    el.bright.value = next;
    queueBrightness(next);
}

async function pickTheme(theme) {
    const a = app();
    if (!a) return;
    setActiveTheme(theme.id);
    // The firmware re-enables a disabled strip when it receives a theme, so the
    // switch belongs on the moment the button is pressed.
    setPowerUI(true);
    hold(1200);
    showError(await a.SetTheme(theme.id));
}

function setPowerUI(on) {
    lightsOn = on;
    el.power.classList.toggle("on", on);
    el.power.setAttribute("aria-checked", on ? "true" : "false");
    el.powerLabel.textContent = on ? "On" : "Off";
}

// The switch moves immediately rather than waiting for the strips to answer,
// so a press always feels like it did something. hold() keeps the reported
// state from flicking it back while the command is in flight.
async function togglePower() {
    const next = !lightsOn;
    setPowerUI(next);
    hold(1200);
    const a = app();
    if (!a) return;
    showError(await a.SetPower(next));
}

function setActiveTheme(id) {
    activeTheme = id;
    for (const b of el.themes.children) {
        b.classList.toggle("active", b.dataset.id === id);
    }
}

// --- rendering -------------------------------------------------------------

function buildThemes(list) {
    el.themes.innerHTML = "";
    for (const t of list) {
        const b = document.createElement("button");
        b.className = "theme";
        b.dataset.id = t.id;
        b.style.background = t.color;
        b.innerHTML = `<span class="em">${t.emoji}</span><span>${t.label}</span>`;
        b.addEventListener("click", () => pickTheme(t));
        el.themes.appendChild(b);
    }
}

// The firmware reports themes as display names ("Pink Pony Club", "Ocean
// Waves") while commands use IDs (PINK_PONY, OCEAN_WAVES). Normalise both to
// compare them.
function themeMatches(id, reported) {
    if (!id || !reported) return false;
    const norm = (s) => s.toUpperCase().replace(/[^A-Z]/g, "");
    const a = norm(id), b = norm(reported);
    return b.startsWith(a) || a.startsWith(b);
}

let lastChips = "";

function renderChips(devices) {
    // Chips arrive with every status push; rebuilding identical DOM on a Pi 3B
    // is wasted work, so skip when nothing about them changed.
    const sig = JSON.stringify(devices.map((d) => [d.name, d.percent, d.enabled, d.lastSeenAgo < 0]));
    if (sig === lastChips) return;
    lastChips = sig;

    el.chips.innerHTML = "";
    for (const d of devices) {
        const c = document.createElement("div");
        c.className = "chip";
        if (d.lastSeenAgo < 0) {
            c.classList.add("gone");
            c.innerHTML = `<b>${d.name}</b> — no reply`;
        } else {
            if (!d.enabled) c.classList.add("dark");
            const state = d.enabled ? `<span class="s">${d.percent}%</span>` : "off";
            c.innerHTML = `<b>${d.name}</b> ${state}`;
        }
        el.chips.appendChild(c);
    }
}

// applyStatus renders one status snapshot. It is called from the Go "status"
// event, not from a timer.
function applyStatus(s) {
    if (!s) return;

    el.dot.classList.toggle("ok", s.connected);
    el.connText.textContent = s.connected ? "connected" : "reconnecting…";
    if (s.error) showError(s.error);

    renderChips(s.devices || []);

    if (!holding()) {
        setPowerUI(!!s.anyOn);
        if (typeof s.percent === "number" && s.percent > 0) {
            el.bright.value = s.percent;
            el.pct.textContent = s.percent + "%";
        }
        const reported = (s.devices || []).find((d) => d.lastSeenAgo >= 0 && d.theme);
        if (reported) {
            const match = (window.__themes || []).find((t) => themeMatches(t.id, reported.theme));
            if (match && match.id !== activeTheme) setActiveTheme(match.id);
        }
    }
}

// --- about -----------------------------------------------------------------

async function showAbout() {
    const a = app();
    if (!a) return;

    let rows = [];
    try {
        rows = await a.GetAbout();
    } catch (e) {
        rows = [{ label: "Version", value: "unavailable" }];
    }

    el.aboutRows.innerHTML = "";
    for (const row of rows) {
        const dt = document.createElement("dt");
        dt.textContent = row.label;
        const dd = document.createElement("dd");
        dd.textContent = row.value;
        el.aboutRows.append(dt, dd);
    }

    el.aboutBack.hidden = false;
    el.aboutClose.focus();
}

function hideAbout() {
    el.aboutBack.hidden = true;
}

// Used at startup and as the fallback when the Wails event runtime is missing.
async function fetchStatus() {
    const a = app();
    if (!a) return;
    try {
        applyStatus(await a.GetStatus());
    } catch (e) {
        /* backend not ready; the next event or refresh covers it */
    }
}

// --- wiring ----------------------------------------------------------------

el.bright.addEventListener("input", (e) => queueBrightness(Number(e.target.value)));
el.up.addEventListener("click", () => nudge(5));
el.down.addEventListener("click", () => nudge(-5));
el.power.addEventListener("click", togglePower);

el.aboutBtn.addEventListener("click", showAbout);
el.aboutClose.addEventListener("click", hideAbout);
// Clicking the backdrop dismisses; clicking the panel itself must not.
el.aboutBack.addEventListener("click", (e) => { if (e.target === el.aboutBack) hideAbout(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape") hideAbout(); });

// Wails injects its bindings after the page loads, so wait for them rather
// than assuming they are present on first script execution.
(async function start() {
    for (let i = 0; i < 100 && !app(); i++) {
        await new Promise((r) => setTimeout(r, 50));
    }
    const a = app();
    if (!a) {
        showError("Could not reach the Go backend.");
        return;
    }

    window.__themes = await a.GetThemes();
    buildThemes(window.__themes);

    // No polling loop. The strips publish their state over MQTT, Go pushes a
    // "status" event when something actually changed, and this renders it. Go
    // also re-emits once a minute so the "last seen" ages do not go stale.
    if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn("status", applyStatus);
    } else {
        // No event runtime (a plain browser, say). Fall back to a slow poll,
        // still far quieter than the 2s loop this replaced.
        setInterval(fetchStatus, 30000);
    }

    await fetchStatus();

    // Ask the strips to report only when someone is actually looking at the
    // panel. RefreshStatus is a read-only query and Go throttles it, so these
    // two listeners firing together still cost one message.
    const wake = () => { if (!document.hidden) a.RefreshStatus(); };
    document.addEventListener("visibilitychange", wake);
    window.addEventListener("focus", wake);
})();
