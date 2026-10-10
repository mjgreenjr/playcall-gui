package core

import (
	"encoding/json"
	"log/slog"
)

// extraString reads a top-level string key this version does not model, from
// Config.Extra. A missing or non-string value is "".
func (c Config) extraString(key string) string {
	raw, ok := c.Extra[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// setExtraString writes (or, for "", removes) a top-level string key.
func (c *Config) setExtraString(key, value string) {
	if value == "" {
		delete(c.Extra, key)
		return
	}
	if c.Extra == nil {
		c.Extra = map[string]json.RawMessage{}
	}
	b, _ := json.Marshal(value)
	c.Extra[key] = b
}

const highlightColorKey = "highlight_color"

// HighlightColor is the GUI's color for scoring plays and turnovers, a name
// the GUI understands ("blue", "green", ...); "" means the default.
func (c Config) HighlightColor() string { return c.extraString(highlightColorKey) }

// SetHighlightColor remembers the color name; "" goes back to the default.
func (c *Config) SetHighlightColor(name string) { c.setExtraString(highlightColorKey, name) }

// SaveHighlightColor writes the color into the config file, re-reading it
// first so other changes made meanwhile are kept.
func SaveHighlightColor(p Paths, name string) error {
	cfg, err := LoadConfig(p)
	if err != nil {
		return err
	}
	cfg.SetHighlightColor(name)
	return SaveConfig(p, cfg)
}

const logLevelKey = "log_level"

// LogLevel is the GUI's log level name, "info" or "debug"; "" means the
// default, info.
func (c Config) LogLevel() string { return c.extraString(logLevelKey) }

// SetLogLevel remembers the level name; "" and "info" go back to the default.
func (c *Config) SetLogLevel(name string) {
	if name == "info" {
		name = ""
	}
	c.setExtraString(logLevelKey, name)
}

// ParseLogLevel turns a saved level name into a slog level; anything but
// "debug" is info.
func ParseLogLevel(name string) slog.Level {
	if name == "debug" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
