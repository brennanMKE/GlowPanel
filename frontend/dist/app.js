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
    effects: $("effects"),
    pickerTitle: $("pickerTitle"),
    tabLooks: $("tabLooks"),
    tabEffects: $("tabEffects"),
    effectBar: $("effectBar"),
    durations: $("durations"),
    running: $("running"),
    runningText: $("runningText"),
    stop: $("stop"),
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
let activeEffect = null;
let lightsOn = false;

// Which grid the picker panel is showing: "looks" or "effects".
let tab = "looks";

// How long the next effect should run, in seconds. 0 is "until stopped".
let durationSeconds = 300;

// The device owns the countdown - the panel polls STATUS for it rather than
// running its own timer, because the strip's clock survives reboots the
// panel's does not. These two only smooth the seconds between polls: a
// deadline taken from the last report, and the ticker that redraws against it.
let effectDeadline = null; // ms epoch, or null when nothing is counting down
let effectMode = "";       // the mode the strips last reported, "" for none
let countdownTimer = null;

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

async function pickEffect(effect) {
    const a = app();
    if (!a) return;
    setActiveEffect(effect.id);
    // Effects light the strips whether or not they were on, the same way a
    // theme does, so the switch belongs on the moment the button is pressed.
    setPowerUI(true);
    hold(1200);
    showError(await a.SetEffect(effect.id, durationSeconds));
}

async function stopEffect() {
    const a = app();
    if (!a) return;
    // Optimistic: clear the banner now rather than waiting for the strips to
    // confirm, so the press always looks like it did something. The next
    // status report is authoritative either way.
    setActiveEffect(null);
    renderEffectBar("", 0);
    showError(await a.ClearEffect());
}

function pickDuration(seconds) {
    durationSeconds = seconds;
    for (const b of el.durations.querySelectorAll(".dur-btn")) {
        b.classList.toggle("on", Number(b.dataset.seconds) === seconds);
    }
}

function setTab(next) {
    tab = next;
    const effects = next === "effects";
    el.themes.hidden = effects;
    el.effects.hidden = !effects;
    el.pickerTitle.textContent = effects ? "Effects" : "Pick a Look";
    el.tabLooks.classList.toggle("on", !effects);
    el.tabEffects.classList.toggle("on", effects);
    el.tabLooks.setAttribute("aria-selected", effects ? "false" : "true");
    el.tabEffects.setAttribute("aria-selected", effects ? "true" : "false");
    renderEffectBar(effectMode, remainingSeconds());
}

function setActiveEffect(id) {
    activeEffect = id;
    for (const b of el.effects.children) {
        b.classList.toggle("active", b.dataset.id === id);
    }
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

function buildEffects(list) {
    el.effects.innerHTML = "";
    for (const e of list) {
        const b = document.createElement("button");
        b.className = "theme effect";
        b.dataset.id = e.id;
        b.style.background = e.color;
        b.innerHTML = `<span class="em">${e.emoji}</span><span>${e.label}</span>`;
        b.addEventListener("click", () => pickEffect(e));
        el.effects.appendChild(b);
    }
}

function buildDurations(list) {
    for (const d of list) {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "dur-btn";
        b.dataset.seconds = d.seconds;
        b.textContent = d.label;
        b.addEventListener("click", () => pickDuration(d.seconds));
        el.durations.appendChild(b);
    }
}

function mmss(seconds) {
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${m}:${String(s).padStart(2, "0")}`;
}

// Title case for the firmware's mode names: SPARKLE reads better as Sparkle on
// a panel a child is looking at.
function prettyMode(mode) {
    if (!mode) return "";
    return mode.charAt(0) + mode.slice(1).toLowerCase();
}

function remainingSeconds() {
    if (effectDeadline === null) return -1; // running, no timeout
    return Math.max(0, Math.round((effectDeadline - Date.now()) / 1000));
}

// renderEffectBar shows the duration chips on the Effects tab, and the running
// banner wherever the user is - an effect started from the Effects tab is still
// stoppable after switching back to Looks.
function renderEffectBar(mode, remaining) {
    const running = !!mode;
    const onEffectsTab = tab === "effects";

    el.effectBar.hidden = !running && !onEffectsTab;
    el.durations.hidden = !onEffectsTab;
    el.running.hidden = !running;
    el.stop.hidden = !running && !onEffectsTab;

    if (!running) {
        stopCountdown();
        return;
    }
    el.runningText.textContent = remaining < 0
        ? prettyMode(mode)
        : `${prettyMode(mode)} · ${mmss(remaining)} left`;
}

// The countdown ticks locally between the 5s status polls purely so the number
// moves once a second. Every poll snaps it back to what the device says.
function startCountdown() {
    if (countdownTimer !== null) return;
    countdownTimer = setInterval(() => {
        if (!effectMode || effectDeadline === null) return;
        renderEffectBar(effectMode, remainingSeconds());
    }, 1000);
}

function stopCountdown() {
    if (countdownTimer === null) return;
    clearInterval(countdownTimer);
    countdownTimer = null;
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
    applyEffect(s.effectMode || "", typeof s.effectRemaining === "number" ? s.effectRemaining : -1);

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

// applyEffect takes one report of what the strips are running and reconciles
// the banner, the deadline and the highlighted button with it. Absence of a
// mode means no effect, not an error.
function applyEffect(mode, remaining) {
    effectMode = mode;

    if (!mode) {
        effectDeadline = null;
        setActiveEffect(null);
        renderEffectBar("", 0);
        return;
    }

    // -1 is an effect with no timeout at all, which is never a countdown.
    effectDeadline = remaining < 0 ? null : Date.now() + remaining * 1000;

    const match = (window.__effects || []).find((e) => e.mode === mode);
    if (match && match.id !== activeEffect) setActiveEffect(match.id);

    renderEffectBar(mode, remaining);
    if (effectDeadline !== null) startCountdown(); else stopCountdown();
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

el.tabLooks.addEventListener("click", () => setTab("looks"));
el.tabEffects.addEventListener("click", () => setTab("effects"));
el.stop.addEventListener("click", stopEffect);

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

    window.__effects = await a.GetEffects();
    buildEffects(window.__effects);
    buildDurations(await a.GetDurations());
    pickDuration(await a.GetDefaultDuration());

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
