package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type Theme struct {
	ID    string
	Label string
	Emoji string
}

var themes = []Theme{
	{ID: "RAINBOW", Label: "Rainbow", Emoji: "🌈"},
	{ID: "GREEN", Label: "Green", Emoji: "🌿"},
	{ID: "PINK_PONY", Label: "Pink Pony", Emoji: "🦄"},
	{ID: "OCEAN_WAVES", Label: "Ocean", Emoji: "🌊"},
	{ID: "SUNSET", Label: "Sunset", Emoji: "🌅"},
	{ID: "FOREST", Label: "Forest", Emoji: "🌲"},
}

const (
	statusEmitInterval  = 60 * time.Second
	statusQueryInterval = 5 * time.Minute
	emitCoalesce        = 250 * time.Millisecond
)

type App struct {
	cfg      *Config
	broker   *Broker
	stateMu  sync.RWMutex
	err      string
	onStatus func(Status)

	emit     chan struct{}
	quit     chan struct{}
	quitOnce sync.Once
}

func NewApp(onStatus func(Status)) *App {
	a := &App{
		onStatus: onStatus,
		emit:     make(chan struct{}, 1),
		quit:     make(chan struct{}),
	}
	cfg, err := LoadConfig(defaultConfigPath())
	if err != nil {
		a.err = err.Error()
		log.Printf("config: %v", err)
	}
	a.cfg = cfg

	a.broker = NewBroker(cfg)
	a.broker.SetOnChange(a.notifyChanged)
	return a
}

func (a *App) Start() {
	go a.run()

	if err := a.broker.Connect(); err != nil {
		a.stateMu.Lock()
		if a.err == "" {
			a.err = fmt.Sprintf("MQTT: %v", err)
		}
		a.stateMu.Unlock()
		log.Printf("mqtt connect: %v", err)
	}
	a.emitStatus()
}

func (a *App) Shutdown() {
	a.quitOnce.Do(func() { close(a.quit) })
	if a.broker != nil {
		a.broker.Disconnect()
	}
}

func (a *App) notifyChanged() {
	select {
	case a.emit <- struct{}{}:
	default:
	}
}

func (a *App) run() {
	heartbeat := time.NewTicker(statusEmitInterval)
	defer heartbeat.Stop()
	query := time.NewTicker(statusQueryInterval)
	defer query.Stop()

	for {
		select {
		case <-a.quit:
			return
		case <-a.emit:
			select {
			case <-time.After(emitCoalesce):
			case <-a.quit:
				return
			}
			select {
			case <-a.emit:
			default:
			}
			a.emitStatus()
		case <-heartbeat.C:
			a.emitStatus()
		case <-query.C:
			a.broker.RequestStatus()
		}
	}
}

func (a *App) emitStatus() {
	select {
	case <-a.quit:
		return
	default:
	}
	if a.onStatus != nil {
		a.onStatus(a.GetStatus())
	}
}

type Status struct {
	Connected  bool
	Error      string
	Devices    []DeviceState
	Percent    int
	HasPercent bool
	AnyOn      bool
}

func (a *App) GetStatus() Status {
	a.stateMu.RLock()
	errText := a.err
	a.stateMu.RUnlock()
	status := Status{Error: errText}
	if a.broker == nil {
		return status
	}
	status.Connected = a.broker.Connected()
	status.Devices = a.broker.Snapshot()
	foundPercent := false
	for _, device := range status.Devices {
		if device.LastSeenAgo >= 0 {
			if !foundPercent {
				status.Percent = device.Percent
				status.HasPercent = true
				foundPercent = true
			}
			if device.Enabled {
				status.AnyOn = true
			}
		}
	}
	return status
}

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

func (a *App) SetTheme(id string) string {
	if a.broker == nil {
		return "not ready"
	}
	valid := false
	for _, item := range themes {
		if item.ID == id {
			valid = true
			break
		}
	}
	if !valid {
		return "unknown theme: " + id
	}
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
	command := "OFF"
	if on {
		command = "ON"
	}
	if err := a.broker.Publish(command, a.cfg.Retain); err != nil {
		return err.Error()
	}
	log.Printf("power -> %s", command)
	return ""
}

func (a *App) RefreshStatus() {
	if a.broker != nil {
		a.broker.RequestStatus()
	}
	a.emitStatus()
}
