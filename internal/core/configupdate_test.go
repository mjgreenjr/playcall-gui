package core

import "testing"

func TestUpdateConfigKeepsOtherChanges(t *testing.T) {
	p := PathsFromHome(t.TempDir())
	cfg := DefaultConfig()
	cfg.SetFavoriteGroup("cfb", "SEC")
	if err := SaveConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	if err := UpdateConfig(p, func(c *Config) { c.SetFavoriteTeams("nfl", []string{"DAL", "PIT"}) }); err != nil {
		t.Fatal(err)
	}
	if err := UpdateConfig(p, func(c *Config) { c.SetInterval(30); c.SetHighlightColor("red") }); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.FavoriteGroup("cfb") != "SEC" || got.HighlightColor() != "red" ||
		len(got.FavoriteTeams["nfl"]) != 2 || ResolveInterval(nil, got) != 30 {
		t.Errorf("config: %+v", got)
	}
}

func TestSetFavoriteTeamsOnNilMap(t *testing.T) {
	var c Config
	c.SetFavoriteTeams("nhl", nil)
	if c.FavoriteTeams["nhl"] == nil || len(c.FavoriteTeams["nhl"]) != 0 {
		t.Errorf("teams: %#v", c.FavoriteTeams)
	}
}

func TestSetIntervalHasAMinimum(t *testing.T) {
	var c Config
	c.SetInterval(1)
	if ResolveInterval(nil, c) != MinInterval {
		t.Errorf("interval %v", c.Interval)
	}
	c.SetInterval(45)
	if ResolveInterval(nil, c) != 45 {
		t.Errorf("interval %v", c.Interval)
	}
}
