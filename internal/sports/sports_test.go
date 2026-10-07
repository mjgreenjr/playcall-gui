package sports

import (
	"strings"
	"testing"
)

func TestPathsAndLeague(t *testing.T) {
	want := map[string][2]string{
		"nfl":  {"football/nfl", "nfl"},
		"nba":  {"basketball/nba", "nba"},
		"mlb":  {"baseball/mlb", "mlb"},
		"nhl":  {"hockey/nhl", "nhl"},
		"cfb":  {"football/college-football", "college-football"},
		"cbb":  {"basketball/mens-college-basketball", "mens-college-basketball"},
		"wnba": {"basketball/wnba", "wnba"},
	}
	if len(Names) != len(want) {
		t.Fatalf("Names has %d entries", len(Names))
	}
	for _, s := range Names {
		if Path(s) != want[s][0] || League(s) != want[s][1] {
			t.Errorf("%s: got %q %q", s, Path(s), League(s))
		}
		if len(Groups(s)) == 0 {
			t.Errorf("%s has no groups", s)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("wnba"); err != nil {
		t.Fatal(err)
	}
	err := Validate("xfl")
	if err == nil || !strings.Contains(err.Error(), "nfl, nba, mlb, nhl, cfb, cbb, wnba") {
		t.Fatalf("got %v", err)
	}
}

func TestDefaults(t *testing.T) {
	if id, ok := DefaultGroup("cfb"); !ok || id != "80" {
		t.Errorf("cfb default = %q %v", id, ok)
	}
	for _, s := range []string{"nfl", "nba", "mlb", "nhl", "cbb", "wnba"} {
		if _, ok := DefaultGroup(s); ok {
			t.Errorf("%s should have no default group", s)
		}
	}
	if !HasAllGroup("cfb") || !HasAllGroup("cbb") || HasAllGroup("nfl") {
		t.Error("HasAllGroup wrong")
	}
	if Groups("cfb")[0].ID != "80" || Groups("cbb")[0].ID != "50" {
		t.Error("first group should be the All entry")
	}
	if n := len(Groups("cbb")); n != 34 {
		t.Errorf("cbb has %d groups", n)
	}
}

func TestResolveGroup(t *testing.T) {
	cases := []struct {
		sport, in, name, id string
	}{
		{"cfb", "Big Ten", "Big Ten", "5"},
		{"cfb", "big-ten", "Big Ten", "5"},
		{"cfb", "BIGTEN", "Big Ten", "5"},
		{"cfb", "5", "Big Ten", "5"},
		{"cfb", "mountain-west", "Mountain West", "17"},
		{"cbb", "a10", "A-10", "3"},
		{"cbb", "pac12", "Pac-12", "21"},
		{"nfl", "afc east", "AFC East", "4"},
		{"nfl", "afc", "AFC", "8"},
		{"nhl", "metropolitan", "Metropolitan", "33"},
		{"mlb", "nl_central", "NL Central", "5"},
		{"wnba", "west", "West", "2"},
	}
	for _, c := range cases {
		g, err := ResolveGroup(c.sport, c.in)
		if err != nil || g.Name != c.name || g.ID != c.id {
			t.Errorf("%s %q: got %+v, %v", c.sport, c.in, g, err)
		}
	}
}

func TestResolveGroupUnknown(t *testing.T) {
	_, err := ResolveGroup("nfl", "nope")
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{"unknown nfl group 'nope'", "AFC (8)", "NFC West (3)"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q missing %q", err, s)
		}
	}
	if _, err := ResolveGroup("xfl", "x"); err == nil {
		t.Error("unsupported sport should error")
	}
}
