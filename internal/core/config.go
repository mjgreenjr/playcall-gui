package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"playcall/internal/sports"
)

const (
	// DefaultInterval is the poll interval in seconds.
	DefaultInterval = 10
	// MinInterval is the shortest allowed poll interval in seconds.
	MinInterval = 5
)

// Config is config.json.
type Config struct {
	// FavoriteTeams maps a sport key to team abbreviations.
	FavoriteTeams map[string][]string `json:"favorite_teams"`
	// Interval is the poll interval in seconds. It is left loosely typed
	// (json.Number, string, ...) because hand-edited files may hold anything;
	// ResolveInterval validates it.
	Interval any `json:"interval"`
	// Extra holds every other top-level key, verbatim, so keys this version
	// does not know (the Python's, the GUI's later ones) survive a load and
	// save. Written after the known keys, sorted by name.
	Extra map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON reads the known keys and keeps the rest in Extra.
func (c *Config) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*c = Config{}
	if v, ok := raw["favorite_teams"]; ok {
		if err := json.Unmarshal(v, &c.FavoriteTeams); err != nil {
			return fmt.Errorf("favorite_teams: %w", err)
		}
		delete(raw, "favorite_teams")
	}
	if v, ok := raw["interval"]; ok {
		dec := json.NewDecoder(bytes.NewReader(v))
		dec.UseNumber()
		if err := dec.Decode(&c.Interval); err != nil {
			return fmt.Errorf("interval: %w", err)
		}
		delete(raw, "interval")
	}
	if len(raw) > 0 {
		c.Extra = raw
	}
	return nil
}

// MarshalJSON writes favorite_teams, interval, then the extra keys.
func (c Config) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	field := func(key string, v any) error {
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		buf.Write(k)
		buf.WriteByte(':')
		b, err := json.Marshal(v)
		buf.Write(b)
		return err
	}
	if err := field("favorite_teams", c.FavoriteTeams); err != nil {
		return nil, err
	}
	if err := field("interval", c.Interval); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(c.Extra))
	for k := range c.Extra {
		if k != "favorite_teams" && k != "interval" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := field(k, c.Extra[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// DefaultConfig has an empty favorites list for every sport and interval 10.
func DefaultConfig() Config {
	fav := make(map[string][]string, len(sports.Names))
	for _, s := range sports.Names {
		fav[s] = []string{}
	}
	return Config{FavoriteTeams: fav, Interval: DefaultInterval}
}

// LoadConfig reads the config file, creating it with defaults on first run.
func LoadConfig(p Paths) (Config, error) {
	if err := p.EnsureDirs(); err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p.ConfigFile())
	if errors.Is(err, os.ErrNotExist) {
		cfg := DefaultConfig()
		if err := SaveConfig(p, cfg); err != nil {
			return Config{}, err
		}
		slog.Info("Created default config", "path", p.ConfigFile())
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", p.ConfigFile(), err)
	}
	return cfg, nil
}

// SaveConfig writes the config file.
func SaveConfig(p Paths, cfg Config) error {
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	b, err := marshalIndent(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(p.ConfigFile(), b, 0o644)
}

// ResolveInterval picks the poll interval in seconds: the --interval flag
// (nil when not given) beats the config; a missing or invalid value falls back
// to DefaultInterval; anything below MinInterval is raised to it.
func ResolveInterval(flag *int, cfg Config) int {
	var raw any = cfg.Interval
	if flag != nil {
		raw = *flag
	}
	if raw == nil {
		raw = DefaultInterval
	}
	v, ok := toInt(raw)
	if !ok {
		slog.Warn("Invalid interval; using default", "interval", raw, "default", DefaultInterval)
		v = DefaultInterval
	}
	if v < MinInterval {
		slog.Warn("Interval is below the minimum; raising it", "interval", v, "minimum", MinInterval)
		v = MinInterval
	}
	return v
}

// toInt converts like Python's int(): integers, floats (truncated), and
// strings of digits.
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case float64:
		return floatToInt(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n), true
		}
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return floatToInt(f)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		return n, err == nil
	}
	return 0, false
}

func floatToInt(f float64) (int, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1<<31 {
		return 0, false
	}
	return int(f), true
}
