package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Config mirrors the settings in ~/.config/glowkitchen/glow.conf, the same file
// the glow-*.sh cron scripts read. Sharing it means one broker address and one
// copy of the password rather than a second set that can drift.
type Config struct {
	Broker   string
	Port     string
	User     string
	Password string
	Devices  []string

	NormalBrightness  int
	EveningBrightness int
	DimBrightness     int
	Retain            bool
}

// MaxBrightness is the firmware ceiling from GlowKitchen src/main.cpp:
//
//	const int MAX_BRIGHTNESS = 225;
//
// The UI works in 0-100 percent and converts against this.
const MaxBrightness = 225

func defaultConfigPath() string {
	if p := os.Getenv("GLOW_CONF"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "glow.conf"
	}
	return filepath.Join(home, ".config", "glowkitchen", "glow.conf")
}

var (
	scalarRe = regexp.MustCompile(`^([A-Z_]+)=(.*)$`)
	arrayRe  = regexp.MustCompile(`^([A-Z_]+)=\((.*)\)$`)
)

// LoadConfig parses the shell-syntax glow.conf, then lets environment variables
// override whatever it found. A missing file is not an error as long as the
// environment supplies a password — that is how this runs on a Mac, which has
// no glow.conf because it uses the zsh scripts in GlowKitchen/scripts instead.
//
// Caveat worth knowing on macOS: an app bundle launched from Finder does not
// inherit your shell environment, so $MQTT_PASSWORD only reaches the app when
// it is started from a terminal. For double-click launching, write a real
// ~/.config/glowkitchen/glow.conf.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		// Placeholder only. The real broker address comes from glow.conf or
		// the GLOW_BROKER environment variable; it is deliberately not in the
		// repository.
		Broker:            "127.0.0.1",
		Port:              "1883",
		User:              "mqtt",
		Devices:           []string{"tv", "desk", "kitchen", "workbench", "recycling"},
		NormalBrightness:  225,
		EveningBrightness: 112,
		DimBrightness:     40,
		Retain:            true,
	}

	haveFile := true
	if err := parseConfigFile(path, cfg); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("could not read %s: %w", path, err)
		}
		haveFile = false
	}

	applyEnvOverrides(cfg)

	if cfg.Password == "" {
		if !haveFile {
			return cfg, fmt.Errorf("no config at %s, and MQTT_PASSWORD is not set", path)
		}
		return cfg, fmt.Errorf("MQTT_PASSWORD is empty in %s", path)
	}
	return cfg, nil
}

// applyEnvOverrides lets the environment win over the config file, which makes
// one-off runs easy: MQTT_PASSWORD=... ./GlowPanel
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MQTT_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("MQTT_USER"); v != "" {
		cfg.User = v
	}
	if v := os.Getenv("GLOW_BROKER"); v != "" {
		cfg.Broker = v
	}
	if v := os.Getenv("GLOW_PORT"); v != "" {
		cfg.Port = v
	}
	// Accepts either "tv desk kitchen" or "tv,desk,kitchen".
	if v := os.Getenv("GLOW_DEVICES"); v != "" {
		cfg.Devices = splitWords(strings.ReplaceAll(v, ",", " "))
	}
}

// parseConfigFile handles the small subset of shell syntax glow.conf uses:
// KEY="value", KEY=value, and KEY=(a b c). Anything else is ignored rather than
// treated as an error, so the scripts can grow new settings without breaking
// this app.
func parseConfigFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if m := arrayRe.FindStringSubmatch(line); m != nil {
			if m[1] == "DEVICES" {
				cfg.Devices = splitWords(m[2])
			}
			continue
		}

		m := scalarRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := m[1], unquote(m[2])

		switch key {
		case "BROKER":
			cfg.Broker = val
		case "PORT":
			cfg.Port = val
		case "MQTT_USER":
			cfg.User = val
		case "MQTT_PASSWORD":
			cfg.Password = val
		case "NORMAL_BRIGHTNESS":
			cfg.NormalBrightness = atoiOr(val, cfg.NormalBrightness)
		case "EVENING_BRIGHTNESS":
			cfg.EveningBrightness = atoiOr(val, cfg.EveningBrightness)
		case "DIM_BRIGHTNESS":
			cfg.DimBrightness = atoiOr(val, cfg.DimBrightness)
		case "RETAIN":
			cfg.Retain = val == "true"
		}
	}

	return scanner.Err()
}

// unquote strips surrounding quotes and any trailing inline comment. The config
// file is written by install.sh, so the shapes here are predictable.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func splitWords(s string) []string {
	out := []string{}
	for _, w := range strings.Fields(s) {
		if w = unquote(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return fallback
}

// PercentToRaw converts the UI's 0-100 percent to a firmware 0-225 value.
func PercentToRaw(percent int) int {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return (percent*MaxBrightness + 50) / 100
}

// RawToPercent converts a firmware value back to percent, snapped to the
// nearest multiple of 5 so it lines up with the slider's step.
func RawToPercent(raw int) int {
	if raw <= 0 {
		return 0
	}
	if raw > MaxBrightness {
		raw = MaxBrightness
	}
	p := (raw*100 + MaxBrightness/2) / MaxBrightness
	return ((p + 2) / 5) * 5
}
