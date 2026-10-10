package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFavoriteGroupRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.FavoriteGroup("cfb"); got != "" {
		t.Fatalf("default: %q", got)
	}
	cfg.SetFavoriteGroup("cfb", "Big Ten")
	cfg.SetFavoriteGroup("nhl", "Metropolitan")
	if cfg.FavoriteGroup("cfb") != "Big Ten" || cfg.FavoriteGroup("nhl") != "Metropolitan" || cfg.FavoriteGroup("nfl") != "" {
		t.Errorf("groups: %+v", cfg.Extra)
	}
	cfg.SetFavoriteGroup("cfb", "SEC") // picking another conference replaces it
	if cfg.FavoriteGroup("cfb") != "SEC" {
		t.Errorf("replace: %q", cfg.FavoriteGroup("cfb"))
	}
	cfg.SetFavoriteGroup("cfb", "")
	cfg.SetFavoriteGroup("nhl", "")
	if _, ok := cfg.Extra[favoriteGroupsKey]; ok {
		t.Errorf("empty key kept: %s", cfg.Extra[favoriteGroupsKey])
	}
}

func TestFavoriteGroupMalformedIsReplaced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Extra = map[string]json.RawMessage{favoriteGroupsKey: json.RawMessage(`"oops"`)}
	if cfg.FavoriteGroup("cfb") != "" {
		t.Error("malformed value read")
	}
	cfg.SetFavoriteGroup("cfb", "ACC")
	if cfg.FavoriteGroup("cfb") != "ACC" {
		t.Errorf("not replaced: %s", cfg.Extra[favoriteGroupsKey])
	}
}

func TestSaveFavoriteGroupKeepsTheRestOfTheFile(t *testing.T) {
	p := PathsFromHome(t.TempDir())
	cfg := DefaultConfig()
	cfg.FavoriteTeams["nfl"] = []string{"DAL"}
	cfg.Interval = json.Number("7")
	cfg.Extra = map[string]json.RawMessage{"python_only": json.RawMessage(`{"x":1}`)}
	if err := SaveConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveFavoriteGroup(p, "cfb", "Big Ten"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	var kept map[string]int
	if err := json.Unmarshal(got.Extra["python_only"], &kept); err != nil || kept["x"] != 1 {
		t.Errorf("unknown key lost: %s (%v)", got.Extra["python_only"], err)
	}
	if got.FavoriteGroup("cfb") != "Big Ten" || got.FavoriteTeams["nfl"][0] != "DAL" {
		t.Errorf("config after: %+v", got)
	}
	if iv, _ := got.Interval.(json.Number); iv.String() != "7" {
		t.Errorf("interval %v", got.Interval)
	}
}

func TestFreshConfigHasNoFavoriteGroupKey(t *testing.T) {
	p := PathsFromHome(t.TempDir())
	if _, err := LoadConfig(p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "favorite_groups") {
		t.Errorf("fresh config: %s", b)
	}
}
