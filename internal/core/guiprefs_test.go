package core

import (
	"log/slog"
	"testing"
)

func TestHighlightColorRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HighlightColor() != "" {
		t.Fatalf("default: %q", cfg.HighlightColor())
	}
	cfg.SetHighlightColor("green")
	if cfg.HighlightColor() != "green" {
		t.Errorf("set: %q", cfg.HighlightColor())
	}
	cfg.SetHighlightColor("")
	if _, ok := cfg.Extra[highlightColorKey]; ok {
		t.Error("key kept after reset")
	}
}

func TestSaveHighlightColorKeepsOtherKeys(t *testing.T) {
	p := PathsFromHome(t.TempDir())
	cfg := DefaultConfig()
	cfg.FavoriteTeams["nfl"] = []string{"DAL"}
	cfg.SetFavoriteGroup("cfb", "Big Ten")
	if err := SaveConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveHighlightColor(p, "orange"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.HighlightColor() != "orange" || got.FavoriteGroup("cfb") != "Big Ten" || got.FavoriteTeams["nfl"][0] != "DAL" {
		t.Errorf("config after: %+v", got)
	}
}

func TestHighlightColorMalformedIsDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.setExtraString(highlightColorKey, "red")
	cfg.Extra[highlightColorKey] = []byte(`42`)
	if cfg.HighlightColor() != "" {
		t.Errorf("malformed: %q", cfg.HighlightColor())
	}
}

func TestLogLevel(t *testing.T) {
	var cfg Config
	if cfg.LogLevel() != "" || ParseLogLevel(cfg.LogLevel()) != slog.LevelInfo {
		t.Error("default is info")
	}
	cfg.SetLogLevel("debug")
	if cfg.LogLevel() != "debug" || ParseLogLevel(cfg.LogLevel()) != slog.LevelDebug {
		t.Errorf("debug: %q", cfg.LogLevel())
	}
	cfg.SetLogLevel("info")
	if _, ok := cfg.Extra["log_level"]; ok {
		t.Error("info should remove the key")
	}
}

func TestLastGame(t *testing.T) {
	cfg := DefaultConfig()
	if _, ok := cfg.LastGame(); ok {
		t.Fatal("a fresh config has no last game")
	}
	want := LastGame{Sport: "cfb", ID: "401856826", Label: "ISU @ BYU"}
	cfg.SetLastGame(want)
	if got, ok := cfg.LastGame(); !ok || got != want {
		t.Errorf("got %+v, %v", got, ok)
	}
	cfg.Extra["last_game"] = []byte(`{"sport":"","game_id":""}`)
	if _, ok := cfg.LastGame(); ok {
		t.Error("an empty entry is no game")
	}
	cfg.Extra["last_game"] = []byte(`"junk"`)
	if _, ok := cfg.LastGame(); ok {
		t.Error("junk is no game")
	}
}

func TestLastSportRoundTrip(t *testing.T) {
	p := PathsFromHome(t.TempDir())
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSport() != "" {
		t.Errorf("fresh config has %q", cfg.LastSport())
	}
	cfg.SetLastSport("cfb")
	if err := SaveConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	again, _ := LoadConfig(p)
	if again.LastSport() != "cfb" {
		t.Errorf("reloaded %q", again.LastSport())
	}
	again.SetLastSport("")
	if _, ok := again.Extra[lastSportKey]; ok {
		t.Error("an empty sport should drop the key")
	}
}
