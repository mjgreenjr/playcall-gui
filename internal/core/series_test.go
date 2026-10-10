package core

import (
	"encoding/json"
	"strings"
	"testing"

	"playcall/internal/espn"
)

// seriesFeed builds a header with a playoff series. Competitor ids are the
// team ids; awayWins/homeWins are ESPN's wins (completed games only).
func seriesFeed(t *testing.T, awayWins, homeWins, best int, gameIndex int, winner string) espn.Data {
	t.Helper()
	events := ""
	for i := 1; i <= best; i++ {
		if i > 1 {
			events += ","
		}
		id := "e" + string(rune('0'+i))
		if i == gameIndex {
			id = "game"
		}
		events += `{"id":"` + id + `"}`
	}
	w := func(side string) string {
		if winner == side {
			return "true"
		}
		return "false"
	}
	src := `{"header":{"competitions":[{
	  "competitors":[
	    {"homeAway":"home","id":"15","winner":` + w("home") + `,"team":{"abbreviation":"ATL"}},
	    {"homeAway":"away","id":"19","winner":` + w("away") + `,"team":{"abbreviation":"LAD"}}],
	  "series":[
	    {"type":"current","competitors":[{"id":"15","wins":9},{"id":"19","wins":9}]},
	    {"type":"playoff","totalCompetitions":` + itoa(best) + `,
	     "competitors":[{"id":"15","wins":` + itoa(homeWins) + `},{"id":"19","wins":` + itoa(awayWins) + `}],
	     "events":[` + events + `]}]}]}}`
	var d espn.Data
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	if err := dec.Decode(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestSeriesText(t *testing.T) {
	cases := []struct {
		name             string
		away, home, best int
		index            int
		winner           string
		final            bool
		want             string
	}{
		{"away leads", 2, 1, 5, 4, "", false, "LAD 2-1"},
		{"home leads", 1, 2, 5, 4, "", false, "ATL 2-1"},
		{"tied", 2, 2, 7, 5, "", false, "Tied 2-2"},
		{"0-0 is left out", 0, 0, 5, 1, "", false, ""},
		{"final already counted", 2, 1, 5, 3, "away", true, "LAD 2-1"},
		{"final not yet counted", 1, 1, 5, 3, "away", true, "LAD 2-1"},
		{"final ties it up", 1, 0, 5, 2, "home", true, "Tied 1-1"},
		{"final evens at 2-2", 2, 1, 5, 4, "home", true, "Tied 2-2"},
		{"final clinch away", 2, 2, 5, 5, "away", true, "LAD WINS"},
		{"final clinch counted", 3, 2, 5, 5, "away", true, "LAD WINS"},
	}
	for _, c := range cases {
		d := seriesFeed(t, c.away, c.home, c.best, c.index, c.winner)
		if got := seriesText(d, "game", c.final); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := seriesText(espn.Data{}, "game", true); got != "" {
		t.Errorf("no header: got %q", got)
	}
}

func TestWithSeries(t *testing.T) {
	if got := withSeries("LAD 3 · Final · ATL 1", "LAD 2-1"); got != "LAD 3 · Final · ATL 1 (LAD 2-1)" {
		t.Errorf("got %q", got)
	}
	if got := withSeries("LAD 3 · Final · ATL 1", ""); got != "LAD 3 · Final · ATL 1" {
		t.Errorf("got %q", got)
	}
}
