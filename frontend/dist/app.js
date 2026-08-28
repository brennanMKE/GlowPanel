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
    tabCustom: $("tabCustom"),
    builder: $("builder"),
    modes: $("modes"),
    modeNote: $("modeNote"),
    meansNote: $("meansNote"),
    swatches: $("swatches"),
    swatchCount: $("swatchCount"),
    swLeft: $("swLeft"),
    swRight: $("swRight"),
    swRemove: $("swRemove"),
    swAdd: $("swAdd"),
    palette: $("palette"),
    preview: $("preview"),
    speed: $("speed"),
    intensity: $("intensity"),
    intensityLabel: $("intensityLabel"),
    apply: $("apply"),
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

// Which view the picker panel is showing: "looks", "effects" or "custom".
let tab = "looks";

// The effect being composed on the Custom tab. Colours are always drawn from
// the palette, so their vividness can be looked up rather than recomputed.
const custom = {
    mode: "SPARKLE",
    colors: ["#FF0000"],
    selected: 0,
    speed: 128,
    intensity: 180,
};

// The top of the useful speed range for the shortest strip that has reported.
// The renderers were tuned for ~240 LEDs, so on a 10-LED board the top of a raw
// 0-255 slider is all flash and no movement.
let speedMax = 255;

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

const TAB_TITLES = { looks: "Pick a Look", effects: "Effects", custom: "Custom" };

function setTab(next) {
    tab = next;
    el.themes.hidden = next !== "looks";
    el.effects.hidden = next !== "effects";
    el.builder.hidden = next !== "custom";
    el.pickerTitle.textContent = TAB_TITLES[next];

    for (const [name, btn] of [["looks", el.tabLooks], ["effects", el.tabEffects],
                               ["custom", el.tabCustom]]) {
        btn.classList.toggle("on", name === next);
        btn.setAttribute("aria-selected", name === next ? "true" : "false");
    }

    // The builder needs the whole middle of the page, so the brightness panel
    // stands down while it is up. It comes back on the other two tabs.
    document.body.classList.toggle("composing", next === "custom");
    // The speed ceiling may have arrived from a status push while the builder
    // was hidden, so redraw rather than trusting whatever it last rendered.
    if (next === "custom") renderBuilder();
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
    // Both effect tabs need the duration row; only the builder needs Apply,
    // since a preset button is its own apply.
    const composing = tab === "custom";
    const onControlsTab = tab === "effects" || composing;

    el.effectBar.hidden = !running && !onControlsTab;
    el.durations.hidden = !onControlsTab;
    el.running.hidden = !running;
    el.apply.hidden = !composing;
    el.stop.hidden = !running && !onControlsTab;

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

// --- the builder -----------------------------------------------------------

function currentMode() {
    return (window.__modes || []).find((m) => m.id === custom.mode) || null;
}

// Colours only ever come from the palette, so vividness is a lookup rather than
// a second implementation of the rule the Go side already applies.
function isVivid(hex) {
    const p = (window.__palette || []).find((c) => c.hex.toLowerCase() === hex.toLowerCase());
    return p ? p.vivid : false;
}

const allVivid = () => custom.colors.every(isVivid);

function buildModes(list) {
    el.modes.innerHTML = "";
    for (const m of list) {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "mode";
        b.dataset.id = m.id;
        b.textContent = m.label;
        b.addEventListener("click", () => pickMode(m.id));
        el.modes.appendChild(b);
    }
}

function buildPalette(list) {
    el.palette.innerHTML = "";
    for (const c of list) {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "pcolor";
        b.dataset.hex = c.hex;
        b.style.background = c.hex;
        b.title = c.vivid ? c.name : `${c.name} — not available in Blend, Flicker or Loop`;
        b.setAttribute("aria-label", c.name);
        b.addEventListener("click", () => setSwatch(c.hex));
        el.palette.appendChild(b);
    }
}

function pickMode(id) {
    custom.mode = id;
    renderBuilder();
}

function setSwatch(hex) {
    custom.colors[custom.selected] = hex;
    // A pastel can make the selected mode unavailable, which renderBuilder
    // resolves rather than leaving a disabled mode selected.
    renderBuilder();
}

function selectSwatch(i) {
    custom.selected = i;
    renderBuilder();
}

function addSwatch() {
    // Nine colours are rejected outright by the firmware, not truncated, and
    // the rejection is silent - so the ceiling is enforced here rather than
    // discovered as lights that do not change.
    if (custom.colors.length >= 8) return;
    custom.colors.push(custom.colors[custom.colors.length - 1]);
    custom.selected = custom.colors.length - 1;
    renderBuilder();
}

function removeSwatch() {
    if (custom.colors.length <= 1) return;
    custom.colors.splice(custom.selected, 1);
    custom.selected = Math.min(custom.selected, custom.colors.length - 1);
    renderBuilder();
}

// Order is the animation in six of the nine modes, so moving a swatch is real
// work rather than tidying.
function moveSwatch(delta) {
    const from = custom.selected;
    const to = from + delta;
    if (to < 0 || to >= custom.colors.length) return;
    const [c] = custom.colors.splice(from, 1);
    custom.colors.splice(to, 0, c);
    custom.selected = to;
    renderBuilder();
}

function renderModes() {
    const vivid = allVivid();
    let blocked = null;

    for (const b of el.modes.children) {
        const m = (window.__modes || []).find((x) => x.id === b.dataset.id);
        if (!m) continue;
        // The three modes that force saturation and value to full would render
        // a pastel as something the user did not pick.
        const off = !vivid && !m.keepsColors;
        b.disabled = off;
        if (off && m.id === custom.mode) blocked = m;
        b.classList.toggle("on", m.id === custom.mode);
    }

    if (blocked) {
        // Leaving a disabled mode selected would mean an Apply button that
        // sends something the picker is telling the user not to send.
        custom.mode = "SPARKLE";
        for (const b of el.modes.children) {
            b.classList.toggle("on", b.dataset.id === custom.mode);
        }
        el.modeNote.textContent =
            `${blocked.label} renders every colour fully saturated, so it is off while ` +
            `the palette has a pastel in it. Switched to Sparkle.`;
        el.modeNote.hidden = false;
        return;
    }

    if (!vivid) {
        el.modeNote.textContent =
            "Blend, Flicker and Loop force every colour to full saturation, so they are " +
            "off while the palette has a pastel in it.";
        el.modeNote.hidden = false;
    } else {
        el.modeNote.hidden = true;
    }
}

function renderSwatches() {
    el.swatches.innerHTML = "";
    custom.colors.forEach((hex, i) => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "swatch" + (i === custom.selected ? " on" : "");
        b.style.background = hex;
        b.setAttribute("aria-label", `Colour ${i + 1} of ${custom.colors.length}`);
        b.addEventListener("click", () => selectSwatch(i));
        el.swatches.appendChild(b);
    });

    el.swatchCount.textContent = `${custom.colors.length}/8`;
    el.swAdd.disabled = custom.colors.length >= 8;
    el.swRemove.disabled = custom.colors.length <= 1;
    el.swLeft.disabled = custom.selected === 0;
    el.swRight.disabled = custom.selected >= custom.colors.length - 1;

    for (const b of el.palette.children) {
        b.classList.toggle("on", b.dataset.hex === custom.colors[custom.selected]);
    }
}

function hexToRgb(hex) {
    const h = hex.replace("#", "");
    return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

const rgbToCss = (c) => `rgb(${c[0]},${c[1]},${c[2]})`;

// A gradient across the palette, for the modes that blend between hues rather
// than showing colours one at a time.
function gradientAt(t) {
    const n = custom.colors.length;
    if (n === 1) return hexToRgb(custom.colors[0]);
    const pos = t * (n - 1);
    const i = Math.min(Math.floor(pos), n - 2);
    const f = pos - i;
    const a = hexToRgb(custom.colors[i]), b = hexToRgb(custom.colors[i + 1]);
    return [0, 1, 2].map((k) => Math.round(a[k] + (b[k] - a[k]) * f));
}

// One frame of what the strip would do. It is an approximation twice over: a
// still image of something moving, and — per the hue warping in hsv2rgb_rainbow
// — brighter and cooler than the strip actually renders.
const PREVIEW_LEDS = 10;

function renderPreview() {
    const m = currentMode();
    const n = custom.colors.length;

    el.preview.innerHTML = "";
    for (let i = 0; i < PREVIEW_LEDS; i++) {
        const led = document.createElement("div");
        led.className = "led";

        if (m && m.showsWholePalette) {
            // SPARKLE lays the palette out by LED position, so this one is a
            // fair likeness rather than a single frame of a moving thing.
            led.style.background = custom.colors[i % n];
        } else if (m && !m.keepsColors) {
            led.style.background = rgbToCss(gradientAt(i / (PREVIEW_LEDS - 1)));
        } else if (m && m.id === "WIPE") {
            // Two at once: what has been filled, and what it is replacing.
            led.style.background = i < 6 ? custom.colors[0] : custom.colors[1 % n];
        } else {
            led.style.background = custom.colors[0];
        }
        el.preview.appendChild(led);
    }
}

function renderSliders() {
    const m = currentMode();

    el.speed.max = speedMax;
    if (custom.speed > speedMax) custom.speed = speedMax;
    el.speed.value = custom.speed;

    // Two modes hard-code their own value and ignore the field entirely.
    const uses = !m || m.usesIntensity;
    el.intensity.disabled = !uses;
    el.intensity.value = custom.intensity;
    el.intensityLabel.style.opacity = uses ? "" : "0.4";

    if (m && uses && m.means) {
        el.meansNote.textContent = `Intensity here is ${m.means}.`;
        el.meansNote.hidden = false;
    } else if (m && !uses) {
        el.meansNote.textContent = `${m.label} sets its own intensity and ignores the slider.`;
        el.meansNote.hidden = false;
    } else {
        el.meansNote.hidden = true;
    }
}

function renderBuilder() {
    renderModes();
    renderSwatches();
    renderPreview();
    renderSliders();
}

async function applyCustom() {
    const a = app();
    if (!a) return;
    setPowerUI(true);
    hold(1200);
    showError(await a.SetCustomEffect(
        custom.mode, custom.colors, custom.speed, custom.intensity, durationSeconds));
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

    // The shortest strip that has reported sets the top of the speed slider.
    if (typeof s.speedMax === "number" && s.speedMax > 0 && s.speedMax !== speedMax) {
        speedMax = s.speedMax;
        if (!el.builder.hidden) renderSliders();
    }

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
el.tabCustom.addEventListener("click", () => setTab("custom"));
el.stop.addEventListener("click", stopEffect);
el.apply.addEventListener("click", applyCustom);

el.swAdd.addEventListener("click", addSwatch);
el.swRemove.addEventListener("click", removeSwatch);
el.swLeft.addEventListener("click", () => moveSwatch(-1));
el.swRight.addEventListener("click", () => moveSwatch(1));

// The sliders only redraw the preview; nothing reaches the strips until Apply.
el.speed.addEventListener("input", (e) => { custom.speed = Number(e.target.value); });
el.intensity.addEventListener("input", (e) => {
    custom.intensity = Number(e.target.value);
    renderPreview();
});

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

    window.__modes = await a.GetModes();
    window.__palette = await a.GetPalette();
    buildModes(window.__modes);
    buildPalette(window.__palette);
    renderBuilder();

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
