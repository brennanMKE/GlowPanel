package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Theme pairs the firmware's command name with the label and colours the UI
// uses for its big buttons. Colour and emoji live here rather than in the
// frontend so adding a firmware theme is a one-line change in one file.
type Theme struct {
	ID    string `json:"id"`    // exact MQTT command the firmware expects
	Label string `json:"label"` // what a child reads on the button
	Emoji string `json:"emoji"`
	Color string `json:"color"` // CSS background for the button
}

var themes = []Theme{
	{ID: "RAINBOW", Label: "Rainbow", Emoji: "🌈",
		Color: "linear-gradient(135deg,#ff4d4d,#ffb84d,#ffff4d,#4dff88,#4db8ff,#b84dff)"},
	{ID: "GREEN", Label: "Green", Emoji: "🌿",
		Color: "linear-gradient(135deg,#1e7a3c,#4ade80)"},
	{ID: "PINK_PONY", Label: "Pink Pony", Emoji: "🦄",
		Color: "linear-gradient(135deg,#d6157e,#ff8ad4)"},
	{ID: "OCEAN_WAVES", Label: "Ocean", Emoji: "🌊",
		Color: "linear-gradient(135deg,#0b5ea8,#38bdf8)"},
	{ID: "SUNSET", Label: "Sunset", Emoji: "🌅",
		Color: "linear-gradient(135deg,#c2410c,#fbbf24)"},
	{ID: "FOREST", Label: "Forest", Emoji: "🌲",
		Color: "linear-gradient(135deg,#14532d,#3f9142)"},
}

// The panel is event driven: the strips publish their state, the broker pushes
// it here, and this pushes a "status" event at the frontend. The two timers
// below are the only periodic work left, and neither of them changes anything
// on the lights.
const (
	// statusEmitInterval re-renders the UI so the "last seen" ages behind the
	// per-strip chips stay honest. Purely local - no MQTT traffic at all.
	statusEmitInterval = 60 * time.Second

	// statusQueryInterval is how often the panel nudges the strips to report in
	// unprompted. The strips already publish when they change, so this exists
	// only to pick up one that rebooted while nobody was looking.
	statusQueryInterval = 5 * time.Minute

	// emitCoalesce collects the burst of replies to a single STATUS request -
	// five strips answering at once - into one render.
	emitCoalesce = 250 * time.Millisecond

	// effectPollInterval is how often the panel asks for a report while an
	// effect is counting down. The device's clock is the one that matters -
	// it survives reboots the panel's timer does not - so the banner is driven
	// by what the strip says rather than by a local countdown. This ticker only
	// sends anything while an effect is actually running.
	effectPollInterval = 5 * time.Second

	// effectConfirmDelay is the pause before the STATUS that confirms an effect
	// command landed. A rejected SET_EFFECT produces no reply of any kind, so
	// the only confirmation available is seeing "Custom" come back. A round
	// trip is about 150ms on a healthy device.
	effectConfirmDelay = 400 * time.Millisecond
)

type App struct {
	ctx    context.Context
	cfg    *Config
	broker *Broker
	err    string // startup failure, surfaced to the UI instead of a blank window

	emit chan struct{} // depth 1: a pending render, coalescing further requests
	quit chan struct{}
}

func NewApp() *App {
	return &App{
		emit: make(chan struct{}, 1),
		quit: make(chan struct{}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := LoadConfig(defaultConfigPath())
	if err != nil {
		a.err = err.Error()
		log.Printf("config: %v", err)
	}
	a.cfg = cfg

	a.broker = NewBroker(cfg)
	a.broker.SetOnChange(a.notifyChanged)
	go a.run()

	if err := a.broker.Connect(); err != nil {
		// Not fatal: paho keeps retrying in the background, so the panel can
		// come up and recover on its own once the broker is reachable.
		if a.err == "" {
			a.err = fmt.Sprintf("MQTT: %v", err)
		}
		log.Printf("mqtt connect: %v", err)
	}
}

func (a *App) shutdown(context.Context) {
	close(a.quit)
	if a.broker != nil {
		a.broker.Disconnect()
	}
}

// notifyChanged is called from MQTT callbacks, so it never blocks: if a render
// is already pending it simply rides along with it.
func (a *App) notifyChanged() {
	select {
	case a.emit <- struct{}{}:
	default:
	}
}

// run owns every recurring thing the panel does. Between events it sits idle.
func (a *App) run() {
	heartbeat := time.NewTicker(statusEmitInterval)
	defer heartbeat.Stop()
	query := time.NewTicker(statusQueryInterval)
	defer query.Stop()
	effect := time.NewTicker(effectPollInterval)
	defer effect.Stop()

	for {
		select {
		case <-a.quit:
			return

		case <-a.emit:
			// Let the rest of the burst land before rendering.
			select {
			case <-time.After(emitCoalesce):
			case <-a.quit:
				return
			}
			select { // anything that arrived during the wait is covered by this render
			case <-a.emit:
			default:
			}
			a.emitStatus()

		case <-heartbeat.C:
			a.emitStatus()

		case <-query.C:
			// Read-only: asks the strips to talk, never tells them to change.
			a.broker.RequestStatus()

		case <-effect.C:
			// Same read-only query, just often enough to keep a countdown
			// honest - and only while there is one to keep.
			if a.broker.EffectActive() {
				a.broker.RequestStatusNow()
			}
		}
	}
}

func (a *App) emitStatus() {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "status", a.GetStatus())
}

// --- methods bound to the frontend -----------------------------------------

func (a *App) GetThemes() []Theme { return themes }

func (a *App) GetDevices() []string {
	if a.cfg == nil {
		return nil
	}
	return a.cfg.Devices
}

// Status is one poll's worth of everything the UI renders.
type Status struct {
	Connected bool          `json:"connected"`
	Error     string        `json:"error"`
	Devices   []DeviceState `json:"devices"`
	Percent   int           `json:"percent"` // representative level for the slider
	AnyOn     bool          `json:"anyOn"`

	// The running effect, from any strip on the broker rather than only the
	// configured ones - effects are broadcast, so the strip that answers may
	// not be in glow.conf. Empty EffectMode means nothing is running, and
	// EffectRemaining is -1 for an effect with no timeout.
	EffectMode      string `json:"effectMode"`
	EffectRemaining int    `json:"effectRemaining"`

	// SpeedMax is the top of the useful speed range for the shortest strip that
	// has reported, so the builder's slider does not offer a third that only
	// produces a flash. 255 until something reports.
	SpeedMax int `json:"speedMax"`
}

// GetStatus is what the frontend reads at startup and what the "status" event
// carries thereafter. Percent is taken from the first device that reported,
// which is right in the normal case where the whole room moves together;
// per-device levels are still shown in the list.
func (a *App) GetStatus() Status {
	st := Status{Error: a.err}
	if a.broker == nil {
		return st
	}
	st.Connected = a.broker.Connected()
	st.Devices = a.broker.Snapshot()
	st.EffectMode, st.EffectRemaining = a.broker.RunningEffect()
	st.SpeedMax = speedCeiling(a.broker.MinNumLeds())

	for _, d := range st.Devices {
		if d.LastSeenAgo >= 0 {
			if st.Percent == 0 {
				st.Percent = d.Percent
			}
			if d.Enabled {
				st.AnyOn = true
			}
		}
	}
	return st
}

// SetBrightness takes 0-100 from the slider and converts to the firmware's
// 0-225 scale. Sending 0 leaves the strips enabled but black; the Off button
// uses SetPower instead so the strips are genuinely disabled.
func (a *App) SetBrightness(percent int) string {
	if a.broker == nil {
		return "not ready"
	}
	raw := PercentToRaw(percent)
	if err := a.broker.Publish(fmt.Sprintf("SET_BRIGHTNESS:%d", raw), a.cfg.Retain); err != nil {
		return err.Error()
	}
	log.Printf("brightness %d%% -> %d", percent, raw)
	return ""
}

// SetTheme sends the theme name. Note the firmware re-enables a disabled strip
// when it receives a theme, so pressing a theme button also turns the lights on
// - which is the behaviour you want from a big friendly button.
func (a *App) SetTheme(id string) string {
	if a.broker == nil {
		return "not ready"
	}
	valid := false
	for _, t := range themes {
		if t.ID == id {
			valid = true
			break
		}
	}
	if !valid {
		return "unknown theme: " + id
	}
	// A theme arriving on the broadcast topic is ignored by a strip that is
	// running an effect - device state wins over a fleet-wide command, per
	// GlowKitchen issue #0020 - so the effect has to come down first. Pressing
	// a theme button is how anyone gets out of an effect without hunting for
	// the Stop button, and it has to keep working.
	if a.broker.EffectActive() {
		if err := a.broker.Publish("CLEAR_EFFECT", false); err != nil {
			return err.Error()
		}
		log.Print("theme press cleared the running effect")
	}

	// Routed through the configured retain flag rather than a hard-coded true,
	// which is what SetBrightness and SetPower already do. A retained command
	// is redelivered to every device on every reconnect, forever: a retained
	// FOREST sitting on the broker snapped a strip back to Forest seconds after
	// every effect was applied, and survived reboots of both the board and the
	// broker, because nothing was publishing it - MQTT was replaying it.
	if err := a.broker.Publish(id, a.cfg.Retain); err != nil {
		return err.Error()
	}
	log.Printf("theme -> %s", id)
	return ""
}

func (a *App) SetPower(on bool) string {
	if a.broker == nil {
		return "not ready"
	}
	cmd := "OFF"
	if on {
		cmd = "ON"
	}
	if err := a.broker.Publish(cmd, a.cfg.Retain); err != nil {
		return err.Error()
	}
	log.Printf("power -> %s", cmd)
	return ""
}

// --- effects ---------------------------------------------------------------

func (a *App) GetEffects() []Effect { return effects }

func (a *App) GetModes() []Mode { return modes }

func (a *App) GetPalette() []PaletteColor { return palette }

// SetCustomEffect publishes an effect composed in the builder rather than one
// of the presets. It takes the same path as SetEffect - same validation, same
// unretained broadcast - because the device is equally silent about rejecting
// either one.
//
// The 1-8 colour check is repeated here rather than trusted to the UI: nine
// colours are rejected outright by the firmware, not truncated, and a rejected
// payload produces no reply at all. A bug in the swatch row has to surface as
// an error string in the panel, because the alternative is lights that simply
// do not change and nothing anywhere saying why.
func (a *App) SetCustomEffect(mode string, colors []string, speed, intensity, seconds int) string {
	if a.broker == nil {
		return "not ready"
	}
	m, ok := findMode(strings.ToUpper(strings.TrimSpace(mode)))
	if !ok {
		return "unknown mode: " + mode
	}

	payload, err := buildEffectPayload(Effect{
		Label:     m.Label,
		Mode:      m.ID,
		Colors:    colors,
		Speed:     speed,
		Intensity: intensity,
	}, seconds)
	if err != nil {
		return err.Error()
	}
	if err := a.broker.Publish(payload, false); err != nil {
		return err.Error()
	}
	log.Printf("custom effect -> %s, %d colour(s), speed %d, intensity %d, %ds",
		m.ID, len(colors), speed, intensity, seconds)
	a.confirmEffect()
	return ""
}

func (a *App) GetDurations() []Duration { return durations }

func (a *App) GetDefaultDuration() int { return defaultDurationSeconds }

// SetEffect starts one preset for the given number of seconds, 0 meaning until
// something stops it.
//
// Effects are published unretained regardless of a.cfg.Retain, which the theme and
// brightness paths honour. An effect is an event, not a configuration: a
// retained SET_EFFECT is replayed to every device on every reconnect, and a
// retained one carrying a timeout restarts its countdown each time.
func (a *App) SetEffect(id string, seconds int) string {
	if a.broker == nil {
		return "not ready"
	}
	e, ok := findEffect(id)
	if !ok {
		return "unknown effect: " + id
	}

	// A preset that asks for a crossing time rather than a speed is sent to each
	// strip separately, with the speed solved for that strip's own length.
	//
	// It has to be. The firmware's speed is milliseconds per LED, so one
	// broadcast byte cannot give an 11-LED bench board and a 128-LED run the
	// same crossing time - solving for the long one makes the short one flash,
	// solving for the short one makes the long one crawl, and the compromise
	// between them satisfied neither. Adding the bench board to the broker was
	// enough to take Cylon on the workbench strip from a 1.1s sweep to 4.6s,
	// which is the bug this whole mechanism exists to fix, reintroduced by the
	// hardware bought to test the fix.
	//
	// Sending N messages instead of one is the price, and it is small: a
	// handful of devices, one publish each, only when a preset is pressed.
	if lengths := a.broker.StripLengths(); e.TraverseMs > 0 && len(lengths) > 0 {
		var failed []string
		for device, numLeds := range lengths {
			per := e
			per.Speed = speedForStrip(e, numLeds)
			payload, err := buildEffectPayload(per, seconds)
			if err != nil {
				return err.Error()
			}
			if err := a.broker.PublishTo(device, payload, false); err != nil {
				failed = append(failed, device)
				continue
			}
			log.Printf("effect -> %s to %s (%s, %d LEDs, speed %d, %ds)",
				e.ID, device, e.Mode, numLeds, per.Speed, seconds)
		}
		if len(failed) > 0 {
			return "could not reach: " + strings.Join(failed, ", ")
		}
		a.confirmEffect()
		return ""
	}

	// Nothing has reported a length, or the preset states a fixed speed: one
	// broadcast, which is also the only path that reaches a strip the panel has
	// never heard from.
	e.Speed = resolveSpeed(e, a.broker.MaxNumLeds(), a.broker.MinNumLeds())

	payload, err := buildEffectPayload(e, seconds)
	if err != nil {
		return err.Error()
	}
	if err := a.broker.Publish(payload, false); err != nil {
		return err.Error()
	}
	log.Printf("effect -> %s (%s, speed %d, %ds)", e.ID, e.Mode, e.Speed, seconds)
	a.confirmEffect()
	return ""
}

// ClearEffect stops whatever is running and hands the strips back to the theme
// they were showing. It is a no-op on the device when nothing is running, so
// the Stop button never needs to be conditionally disabled.
func (a *App) ClearEffect() string {
	if a.broker == nil {
		return "not ready"
	}
	if err := a.broker.Publish("CLEAR_EFFECT", false); err != nil {
		return err.Error()
	}
	log.Print("effect -> cleared")
	a.confirmEffect()
	return ""
}

// confirmEffect asks the strips to report shortly after an effect command, so
// the banner reflects what actually happened rather than what was asked for.
// The device says nothing at all when it rejects a payload, so seeing the mode
// come back is the only acknowledgement there is.
func (a *App) confirmEffect() {
	go func() {
		select {
		case <-time.After(effectConfirmDelay):
		case <-a.quit:
			return
		}
		a.broker.RequestStatusNow()
	}()
}

// RefreshStatus is called when the panel comes back into view. It asks the
// strips to report - a throttled, read-only query that cannot alter them - and
// pushes what is already known so the window is never stale while the replies
// arrive.
func (a *App) RefreshStatus() {
	if a.broker != nil {
		a.broker.RequestStatus()
	}
	a.emitStatus()
}
