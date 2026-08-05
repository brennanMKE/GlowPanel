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
    on: $("on"),
    off: $("off"),
    dot: $("dot"),
    connText: $("connText"),
    chips: $("chips"),
    error: $("error"),
};

// While the user is touching the slider we stop letting polled state write to
// it, otherwise the value fights the finger. Cleared shortly after they stop.
let holdUntil = 0;
const hold = (ms = 2500) => { holdUntil = Date.now() + ms; };
const holding = () => Date.now() < holdUntil;

let sendTimer = null;
let activeTheme = null;

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
    hold(1200);
    showError(await a.SetTheme(theme.id));
}

async function power(on) {
    const a = app();
    if (!a) return;
    hold(1200);
    showError(await a.SetPower(on));
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

function renderChips(devices) {
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

async function poll() {
    const a = app();
    if (!a) return;

    let s;
    try {
        s = await a.GetStatus();
    } catch (e) {
        return;
    }

    el.dot.classList.toggle("ok", s.connected);
    el.connText.textContent = s.connected ? "connected" : "reconnecting…";
    if (s.error) showError(s.error);

    renderChips(s.devices || []);

    if (!holding()) {
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

// --- wiring ----------------------------------------------------------------

el.bright.addEventListener("input", (e) => queueBrightness(Number(e.target.value)));
el.up.addEventListener("click", () => nudge(5));
el.down.addEventListener("click", () => nudge(-5));
el.on.addEventListener("click", () => power(true));
el.off.addEventListener("click", () => power(false));

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

    await poll();
    setInterval(poll, 2000);

    // Nudge the strips to re-report every 15s so a device that rebooted shows
    // up again without needing the panel restarted.
    setInterval(() => a.RefreshStatus(), 15000);
})();
