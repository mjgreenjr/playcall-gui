package core

import "testing"

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
