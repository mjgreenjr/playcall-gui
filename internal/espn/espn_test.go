package espn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

const fixtures = "../../test/fixtures/"

func mock(t *testing.T, name string) Data {
	t.Helper()
	d, err := ReadMock(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestExtractPlays(t *testing.T) {
	cases := []struct {
		file        string
		count       int
		first, last int64
	}{
		{"playbyplay_cfb_multi_plays.json", 2, 401628456781, 401628456782},
		{"playbyplay_empty.json", 0, 0, 0},
		{"playbyplay_final.json", 10, 40187296441, 401872964272},
		{"playbyplay_mlb_multi_plays.json", 573, 4019080150000000059, 4019080151799990058},
		{"playbyplay_multi_plays.json", 8, 40187297940, 4018729791798},
		{"playbyplay_nba_multi_plays.json", 435, 4019018204, 401901820668},
		{"playbyplay_nhl_multi_plays.json", 269, 401892451000000520, 401892451000008390},
		{"playbyplay_wnba_multi_plays.json", 359, 4019182964, 401918296501},
	}
	for _, c := range cases {
		plays := ExtractPlays(mock(t, c.file))
		if len(plays) != c.count {
			t.Errorf("%s: %d plays, want %d", c.file, len(plays), c.count)
			continue
		}
		if c.count == 0 {
			continue
		}
		first, ok1 := PlayID(plays[0])
		last, ok2 := PlayID(plays[len(plays)-1])
		if !ok1 || !ok2 || first != c.first || last != c.last {
			t.Errorf("%s: first %d last %d, want %d %d", c.file, first, last, c.first, c.last)
		}
	}
}

func TestExtractPlaysLayoutsAndDedupe(t *testing.T) {
	d, err := Decode([]byte(`{
		"plays": [{"id": "1", "text": "a"}, {"id": 2}],
		"gamepackageJSON": {
			"playback": {"plays": [{"id": "1", "text": "a2"}]},
			"drives": {"previous": [{"plays": [{"id": "3"}]}], "current": {"plays": [{"id": "4"}]}}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	plays := ExtractPlays(d)
	var ids []string
	for _, p := range plays {
		ids = append(ids, playKey(p))
	}
	if got := len(ids); got != 4 || ids[0] != "1" || ids[1] != "2" || ids[2] != "3" || ids[3] != "4" {
		t.Fatalf("ids = %v", ids)
	}
	if PlayText(plays[0]) != "a2" {
		t.Errorf("later duplicate should win, got %q", PlayText(plays[0]))
	}
	// drives as a plain list
	d, _ = Decode([]byte(`{"drives": [{"plays": [{"id": "9"}]}]}`))
	if got := ExtractPlays(d); len(got) != 1 {
		t.Errorf("list drives: %d plays", len(got))
	}
}

func TestTeamsAndStatus(t *testing.T) {
	cases := []struct {
		file         string
		away, home   string
		awayS, homeS string
		final        bool
	}{
		{"playbyplay_cfb_multi_plays.json", "MICH", "OSU", "24", "31", false},
		{"playbyplay_final.json", "PIT", "CLE", "24", "27", true},
		{"playbyplay_multi_plays.json", "ATL", "NO", "24", "7", false},
		{"playbyplay_nba_multi_plays.json", "BKN", "CHA", "114", "80", false},
		{"playbyplay_nhl_multi_plays.json", "UTA", "NJ", "4", "2", false},
		{"playbyplay_wnba_multi_plays.json", "LV", "GS", "60", "71", true},
		{"playbyplay_mlb_multi_plays.json", "LAD", "ATL", "3", "1", true},
		{"playbyplay_empty.json", "PIT", "CLE", "0", "0", false},
	}
	for _, c := range cases {
		d := mock(t, c.file)
		home, away := Teams(Competition(d))
		if home == nil || away == nil {
			t.Errorf("%s: missing side", c.file)
			continue
		}
		if TeamAbbr(away) != c.away || TeamAbbr(home) != c.home ||
			ScoreText(away["score"]) != c.awayS || ScoreText(home["score"]) != c.homeS {
			t.Errorf("%s: %s %s @ %s %s", c.file, TeamAbbr(away), ScoreText(away["score"]), TeamAbbr(home), ScoreText(home["score"]))
		}
		if IsFinal(d) != c.final {
			t.Errorf("%s: IsFinal = %v", c.file, IsFinal(d))
		}
	}
}

func TestTeamsByHomeAway(t *testing.T) {
	comp, _ := Decode([]byte(`{"competitors": [
		{"homeAway": "away", "team": {"abbreviation": "AAA"}},
		{"homeAway": "home", "team": {}}]}`))
	home, away := Teams(comp)
	if TeamAbbr(away) != "AAA" || TeamAbbr(home) != "UNK" {
		t.Errorf("got %s %s", TeamAbbr(away), TeamAbbr(home))
	}
	one, _ := Decode([]byte(`{"competitors": [{"homeAway": "home"}]}`))
	if h, a := Teams(one); h == nil || a != nil {
		t.Error("missing side should be nil")
	}
	if h, a := Teams(Data{}); h != nil || a != nil {
		t.Error("no competitors")
	}
}

func TestStatusLayouts(t *testing.T) {
	d, _ := Decode([]byte(`{"gamepackageJSON": {"header": {"competitions": [{"status": {"type": {"name": "STATUS_FINAL"}}}]}}}`))
	if !IsFinal(d) {
		t.Error("nested header status not read")
	}
	d, _ = Decode([]byte(`{"status": {"type": {"shortDetail": "Final/OT"}}}`))
	if !IsFinal(d) {
		t.Error("top-level status not read")
	}
	if IsFinal(Data{}) {
		t.Error("empty data is not final")
	}
	if got := Competition(Data{}); got == nil {
		t.Error("Competition must not be nil")
	}
}

func TestScoreText(t *testing.T) {
	dec := func(s string) any {
		d, err := Decode([]byte(`{"v": ` + s + `}`))
		if err != nil {
			t.Fatal(err)
		}
		return d["v"]
	}
	cases := []struct {
		in, want string
	}{
		{`"24"`, "24"},
		{`24`, "24"},
		{`0`, "0"},
		{`null`, "0"},
		{`""`, ""},
		{`{"displayValue": "7", "value": 7}`, "7"},
		{`{"value": 7}`, "7"},
		{`{"displayValue": "", "value": 3}`, "3"},
		{`{"displayValue": "", "value": 0}`, "0"},
		{`{}`, "0"},
	}
	for _, c := range cases {
		if got := ScoreText(dec(c.in)); got != c.want {
			t.Errorf("ScoreText(%s) = %q, want %q", c.in, got, c.want)
		}
	}
	if ScoreText(nil) != "0" {
		t.Error("nil")
	}
}

func TestPlayText(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"text": "  Rush for 11 yards \n"}`, "Rush for 11 yards"},
		{`{"text": "", "display": {"play": {"text": " shown "}}}`, "shown"},
		{`{"description": " desc "}`, "desc"},
		{`{}`, ""},
	}
	for _, c := range cases {
		d, _ := Decode([]byte(c.in))
		if got := PlayText(d); got != c.want {
			t.Errorf("PlayText(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlayIDPrecision(t *testing.T) {
	d, _ := Decode([]byte(`{"id": 4019080151799990058}`))
	if id, ok := PlayID(d); !ok || id != 4019080151799990058 {
		t.Errorf("id = %d %v", id, ok)
	}
	d, _ = Decode([]byte(`{"id": "123"}`))
	if id, ok := PlayID(d); !ok || id != 123 {
		t.Errorf("string id = %d %v", id, ok)
	}
	if _, ok := PlayID(Data{"id": "x"}); ok {
		t.Error("non-numeric id")
	}
}

func TestURLs(t *testing.T) {
	cases := []struct{ got, want string }{
		{ScoreboardURL("nfl", "", ""), "https://site.api.espn.com/apis/site/v2/sports/football/nfl/scoreboard"},
		{ScoreboardURL("nba", "20261009", ""), "https://site.api.espn.com/apis/site/v2/sports/basketball/nba/scoreboard?dates=20261009"},
		{ScoreboardURL("mlb", "", "7"), "https://site.api.espn.com/apis/site/v2/sports/baseball/mlb/scoreboard?groups=7"},
		{ScoreboardURL("nhl", "20261009", "7"), "https://site.api.espn.com/apis/site/v2/sports/hockey/nhl/scoreboard?groups=7&dates=20261009"},
		{ScoreboardURL("cfb", "", ""), "https://site.api.espn.com/apis/site/v2/sports/football/college-football/scoreboard?groups=80"},
		{ScoreboardURL("cfb", "20261010", "5"), "https://site.api.espn.com/apis/site/v2/sports/football/college-football/scoreboard?groups=5&dates=20261010"},
		{ScoreboardURL("cbb", "", ""), "https://site.api.espn.com/apis/site/v2/sports/basketball/mens-college-basketball/scoreboard"},
		{ScoreboardURL("wnba", "", "1"), "https://site.api.espn.com/apis/site/v2/sports/basketball/wnba/scoreboard?groups=1"},
		{SummaryURL("nfl", "401"), "https://site.api.espn.com/apis/site/v2/sports/football/nfl/summary?event=401"},
		{SummaryURL("cbb", "9"), "https://site.api.espn.com/apis/site/v2/sports/basketball/mens-college-basketball/summary?event=9"},
		{TeamsURL("nhl"), "https://site.api.espn.com/apis/site/v2/sports/hockey/nhl/teams?limit=500"},
		{StandingsURL("cfb", "5"), "https://site.api.espn.com/apis/v2/sports/football/college-football/standings?group=5"},
		{GamePageURL("nfl", "1"), "https://www.espn.com/nfl/game/_/gameId/1"},
		{GamePageURL("cfb", "2"), "https://www.espn.com/college-football/game/_/gameId/2"},
		{GamePageURL("cbb", "3"), "https://www.espn.com/mens-college-basketball/game/_/gameId/3"},
		{GamePageURL("wnba", "4"), "https://www.espn.com/wnba/game/_/gameId/4"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got  %s\nwant %s", c.got, c.want)
		}
	}
	for _, s := range []string{"nfl", "nba", "mlb", "nhl", "cfb", "cbb", "wnba"} {
		if SummaryURL(s, "1") == "" || TeamsURL(s) == "" {
			t.Errorf("%s: empty URL", s)
		}
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"id": 4019080151799990058}`))
		case "/bad":
			w.Write([]byte(`<html>`))
		case "/array":
			w.Write([]byte(`[1]`))
		case "/500":
			http.Error(w, "boom", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var c Client
	ctx := context.Background()

	d, err := c.Fetch(ctx, srv.URL+"/ok")
	if err != nil || d == nil {
		t.Fatalf("ok: %v %v", d, err)
	}
	if id, _ := PlayID(d); id != 4019080151799990058 {
		t.Errorf("id = %d", id)
	}
	if d, err := c.Fetch(ctx, srv.URL+"/missing"); d != nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("404: %v %v", d, err)
	}
	for _, p := range []string{"/bad", "/array"} {
		if d, err := c.Fetch(ctx, srv.URL+p); d != nil || !errors.Is(err, ErrInvalidJSON) {
			t.Errorf("%s: %v %v", p, d, err)
		}
	}
	if d, err := c.Fetch(ctx, srv.URL+"/500"); d != nil || err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("500: %v %v", d, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if d, err := c.Fetch(cctx, srv.URL+"/ok"); d != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v %v", d, err)
	}
	srv.Close()
	if d, err := c.Fetch(ctx, srv.URL+"/ok"); d != nil || err == nil {
		t.Errorf("closed server: %v %v", d, err)
	}
}

func TestReadMockErrors(t *testing.T) {
	if _, err := ReadMock(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("missing file should error")
	}
}

func TestPadStatus(t *testing.T) {
	for in, want := range map[string]string{
		"9/5 - 7:30 PM EDT":    " 9/5  - 07:30 PM EDT",
		"10/9 - 9:00 PM EDT":   "10/9  - 09:00 PM EDT",
		"12/25 - 12:00 PM EST": "12/25 - 12:00 PM EST",
		"1/2 - 8:15 PM EST":    " 1/2  - 08:15 PM EST",
		"10/11 - 10:05 PM EDT": "10/11 - 10:05 PM EDT",
		"Final":                "Final",
		"Final/10":             "Final/10",
		"4:35 - 2nd":           "4:35 - 2nd",
		"Postponed":            "Postponed",
		"10/9 - TBD":           "10/9 - TBD",
	} {
		if got := PadStatus(in); got != want {
			t.Errorf("PadStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeamLogo(t *testing.T) {
	cases := []struct {
		name string
		team Data
		want string
	}{
		{"scoreboard dark", Data{"logo": "light.png", "logoDark": "dark.png"}, "dark.png"},
		{"scoreboard only light", Data{"logo": "light.png"}, "light.png"},
		{"game feed: the plain dark one, not the scoreboard variant", Data{"logos": []any{
			map[string]any{"href": "default.png", "rel": []any{"full", "default"}},
			map[string]any{"href": "sb-dark.png", "rel": []any{"full", "scoreboard", "dark"}},
			map[string]any{"href": "dark.png", "rel": []any{"full", "dark"}},
		}}, "dark.png"},
		{"game feed without a dark one", Data{"logos": []any{
			map[string]any{"href": "default.png", "rel": []any{"full", "default"}},
		}}, "default.png"},
		{"none", Data{"abbreviation": "X"}, ""},
	}
	for _, c := range cases {
		if got := TeamLogo(c.team); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

type staticFetcher struct{ data Data }

func (f staticFetcher) Fetch(context.Context, string) (Data, error) { return f.data, nil }

func TestFetchTeamsCarriesLogos(t *testing.T) {
	logos := func(abbr string) []any {
		return []any{
			map[string]any{"href": "https://x/" + abbr + ".png", "rel": []any{"full", "default"}},
			map[string]any{"href": "https://x/dark/" + abbr + ".png", "rel": []any{"full", "dark"}},
		}
	}
	teams := staticFetcher{Data{"sports": []any{map[string]any{"leagues": []any{map[string]any{"teams": []any{
		map[string]any{"team": map[string]any{"abbreviation": "DAL", "displayName": "Dallas Cowboys", "logos": logos("dal")}},
		map[string]any{"team": map[string]any{"abbreviation": "XX", "displayName": "No Logo"}},
	}}}}}}}
	got := FetchTeams(context.Background(), teams, "nfl", "")
	if len(got) != 2 || got[0].Logo != "https://x/dark/dal.png" || got[1].Logo != "" {
		t.Errorf("teams endpoint: %+v", got)
	}
	standings := staticFetcher{Data{"standings": map[string]any{"entries": []any{
		map[string]any{"team": map[string]any{"abbreviation": "IOWA", "displayName": "Iowa Hawkeyes", "logos": logos("iowa")}},
	}}}}
	got = FetchTeams(context.Background(), standings, "cfb", "")
	if len(got) != 1 || got[0].Logo != "https://x/dark/iowa.png" {
		t.Errorf("standings: %+v", got)
	}
}
