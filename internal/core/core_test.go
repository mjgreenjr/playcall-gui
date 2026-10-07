package core

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	return PathsFromHome(t.TempDir())
}

func TestPaths(t *testing.T) {
	p := PathsFromHome("/h")
	if p.ConfigFile() != "/h/.config/playcall/config.json" ||
		p.StateFile() != "/h/.cache/playcall/state.json" ||
		p.LogFile() != "/h/.cache/playcall/playcall.log" {
		t.Errorf("%+v", p)
	}
}

func TestConfigFirstRun(t *testing.T) {
	p := testPaths(t)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.FavoriteTeams) != 7 {
		t.Errorf("favorite_teams has %d sports", len(cfg.FavoriteTeams))
	}
	for s, l := range cfg.FavoriteTeams {
		if l == nil || len(l) != 0 {
			t.Errorf("%s: %v", s, l)
		}
	}
	b, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		t.Fatal("config not created:", err)
	}
	if !strings.Contains(string(b), `"interval": 10`) || !strings.Contains(string(b), `"nfl": []`) {
		t.Errorf("file:\n%s", b)
	}
	if got := ResolveInterval(nil, cfg); got != 10 {
		t.Errorf("interval = %d", got)
	}
}

func TestConfigExisting(t *testing.T) {
	p := testPaths(t)
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p.ConfigFile(), []byte(`{"favorite_teams": {"nfl": ["BUF"], "nhl": ["NJ", "UTA"]}, "interval": 30}`), 0o644)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.FavoriteTeams["nhl"]; len(got) != 2 || got[1] != "UTA" {
		t.Errorf("favorites = %v", cfg.FavoriteTeams)
	}
	if ResolveInterval(nil, cfg) != 30 {
		t.Error("interval not read")
	}
	// save and reload round trip
	cfg.FavoriteTeams["nfl"] = append(cfg.FavoriteTeams["nfl"], "NYJ")
	if err := SaveConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := LoadConfig(p)
	if err != nil || len(again.FavoriteTeams["nfl"]) != 2 || ResolveInterval(nil, again) != 30 {
		t.Errorf("reload: %+v %v", again, err)
	}
	os.WriteFile(p.ConfigFile(), []byte(`{bad`), 0o644)
	if _, err := LoadConfig(p); err == nil {
		t.Error("corrupt config should error")
	}
}

func TestResolveInterval(t *testing.T) {
	ip := func(n int) *int { return &n }
	cfgOf := func(raw any) Config { return Config{Interval: raw} }
	cases := []struct {
		name string
		flag *int
		cfg  Config
		want int
	}{
		{"default", nil, cfgOf(10), 10},
		{"missing", nil, cfgOf(nil), 10},
		{"config value", nil, cfgOf(20), 20},
		{"flag beats config", ip(7), cfgOf(20), 7},
		{"flag raised", ip(1), cfgOf(20), 5},
		{"config raised", nil, cfgOf(2), 5},
		{"negative raised", nil, cfgOf(-3), 5},
		{"exactly minimum", nil, cfgOf(5), 5},
		{"string number", nil, cfgOf("15"), 15},
		{"float truncated", nil, cfgOf(12.9), 12},
		{"invalid string", nil, cfgOf("fast"), 10},
		{"invalid type", nil, cfgOf([]any{1}), 10},
		{"zero flag raised", ip(0), cfgOf(20), 5},
	}
	for _, c := range cases {
		if got := ResolveInterval(c.flag, c.cfg); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func i64(n int64) *PlayID { id := PlayID(n); return &id }

func TestStateRoundTrip(t *testing.T) {
	p := testPaths(t)
	store := NewStateStore(p)
	if st, err := store.Load(); st != nil || err != nil {
		t.Fatalf("no file: %v %v", st, err)
	}
	in := &State{
		GameID: "999", Sport: "nhl", LastPlayID: i64(401892451000000590), StartedAt: 1760000000.5,
		ScoringTexts: map[string]string{"2": "Goal b", "1": "Goal a"},
		SentIDs:      []string{"10", "9"},
	}
	if err := store.Save(in); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.StateFile())
	want := `{
  "game_id": "999",
  "last_play_id": "401892451000000590",
  "scoring_texts": {
    "1": "Goal a",
    "2": "Goal b"
  },
  "sent_ids": [
    "10",
    "9"
  ],
  "sport": "nhl",
  "started_at": 1760000000.5
}`
	if string(b) != want {
		t.Errorf("file:\n%s\nwant:\n%s", b, want)
	}
	out, err := NewStateStore(p).Load()
	if err != nil || out == nil {
		t.Fatal(out, err)
	}
	if out.GameID != "999" || *out.LastPlayID != 401892451000000590 || out.ScoringTexts["1"] != "Goal a" || len(out.SentIDs) != 2 || out.StartedAt != 1760000000.5 {
		t.Errorf("%+v", out)
	}
}

func TestStateInitialAndOmitted(t *testing.T) {
	p := testPaths(t)
	store := NewStateStore(p)
	if err := store.Save(&State{GameID: "1", Sport: "nfl", StartedAt: 5}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.StateFile())
	want := "{\n  \"game_id\": \"1\",\n  \"last_play_id\": null,\n  \"sport\": \"nfl\",\n  \"started_at\": 5\n}"
	if string(b) != want {
		t.Errorf("file:\n%s", b)
	}
	// empty (non-nil) collections are kept, as Python writes {} after a first poll
	store.Save(&State{GameID: "1", Sport: "nfl", StartedAt: 5, ScoringTexts: map[string]string{}, SentIDs: []string{}})
	b, _ = os.ReadFile(p.StateFile())
	if !strings.Contains(string(b), `"scoring_texts": {}`) || !strings.Contains(string(b), `"sent_ids": []`) {
		t.Errorf("file:\n%s", b)
	}
}

func TestStateUnchangedSaveSkipped(t *testing.T) {
	p := testPaths(t)
	store := NewStateStore(p)
	st := &State{GameID: "1", Sport: "nfl", StartedAt: 1}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	// a skipped save must not recreate a removed file
	os.Remove(p.StateFile())
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.StateFile()); err == nil {
		t.Error("unchanged state was written again")
	}
	st.LastPlayID = i64(7)
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.StateFile()); err != nil {
		t.Error("changed state was not written")
	}
}

func TestStateClear(t *testing.T) {
	p := testPaths(t)
	store := NewStateStore(p)
	if err := store.Clear(); err != nil {
		t.Fatal("clearing nothing:", err)
	}
	st := &State{GameID: "1", Sport: "nfl", StartedAt: 1}
	store.Save(st)
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.StateFile()); err == nil {
		t.Error("state file still exists")
	}
	// after a clear the same state is written again
	store.Save(st)
	if _, err := os.Stat(p.StateFile()); err != nil {
		t.Error("save after clear skipped")
	}
}

// pythonState is what reference/playcall.py writes (json.dump indent=2,
// sort_keys=True), with ESPN's string ids.
const pythonState = `{
  "game_id": "401892451",
  "last_play_id": "401892451000008390",
  "scoring_texts": {
    "401892451000000700": "Nico Hischier Goal (Wrist Shot)"
  },
  "sent_ids": [
    "401892451000000520",
    "401892451000000550"
  ],
  "sport": "nhl",
  "started_at": 1760001234.567
}`

func TestLoadPythonState(t *testing.T) {
	p := testPaths(t)
	p.EnsureDirs()
	os.WriteFile(p.StateFile(), []byte(pythonState), 0o644)
	store := NewStateStore(p)
	st, err := store.Load()
	if err != nil || st == nil {
		t.Fatal(st, err)
	}
	if st.GameID != "401892451" || st.Sport != "nhl" || st.LastPlayID == nil || *st.LastPlayID != 401892451000008390 {
		t.Errorf("%+v", st)
	}
	if len(st.SentIDs) != 2 || st.SentIDs[1] != "401892451000000550" || st.ScoringTexts["401892451000000700"] == "" {
		t.Errorf("%+v", st)
	}
	// writing it back reproduces the Python bytes
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.StateFile())
	if string(b) != pythonState {
		t.Errorf("rewrite differs:\n%s", b)
	}
}

func TestLoadStateVariants(t *testing.T) {
	p := testPaths(t)
	p.EnsureDirs()
	store := NewStateStore(p)
	// numeric last_play_id, null last_play_id, no optional keys
	os.WriteFile(p.StateFile(), []byte(`{"game_id": "1", "sport": "nfl", "last_play_id": 4018729791798, "started_at": 1}`), 0o644)
	st, err := store.Load()
	if err != nil || *st.LastPlayID != 4018729791798 || st.SentIDs != nil || st.ScoringTexts != nil {
		t.Errorf("%+v %v", st, err)
	}
	os.WriteFile(p.StateFile(), []byte(`{"game_id": "1", "sport": "nfl", "last_play_id": null, "started_at": 1}`), 0o644)
	if st, err := store.Load(); err != nil || st.LastPlayID != nil {
		t.Errorf("%+v %v", st, err)
	}
	for _, bad := range []string{`{`, `{"last_play_id": "abc"}`} {
		os.WriteFile(p.StateFile(), []byte(bad), 0o644)
		if _, err := store.Load(); err == nil {
			t.Errorf("%s should error", bad)
		}
	}
}

func TestLogging(t *testing.T) {
	p := testPaths(t)
	var out bytes.Buffer
	prev := slog.Default()
	defer slog.SetDefault(prev)
	closeLog, err := SetupLogging(p, &out, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	slog.Debug("hidden")
	slog.Info("State saved", "game", "999")
	slog.Warn("Network error", "err", "boom")
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	file, _ := os.ReadFile(filepath.Join(p.CacheDir, "playcall.log"))
	for _, got := range []string{string(file), out.String()} {
		if strings.Contains(got, "hidden") || !strings.Contains(got, " INFO State saved game=999\n") || !strings.Contains(got, " WARN Network error err=boom\n") {
			t.Errorf("log output:\n%s", got)
		}
	}
	if string(file) != out.String() {
		t.Error("file and stdout differ")
	}
}
