package main

import (
	"context"
	"fmt"
	"log"
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

type App struct {
	ctx    context.Context
	cfg    *Config
	broker *Broker
	err    string // startup failure, surfaced to the UI instead of a blank window
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := LoadConfig(defaultConfigPath())
	if err != nil {
		a.err = err.Error()
		log.Printf("config: %v", err)
	}
	a.cfg = cfg

	a.broker = NewBroker(cfg)
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
	if a.broker != nil {
		a.broker.Disconnect()
	}
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
}

// GetStatus is polled by the frontend. Percent is taken from the first device
// that reported, which is right in the normal case where the whole room moves
// together; per-device levels are still shown in the list.
func (a *App) GetStatus() Status {
	st := Status{Error: a.err}
	if a.broker == nil {
		return st
	}
	st.Connected = a.broker.Connected()
	st.Devices = a.broker.Snapshot()

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
	if err := a.broker.Publish(id, true); err != nil {
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

func (a *App) RefreshStatus() {
	if a.broker != nil {
		a.broker.RequestStatus()
	}
}
