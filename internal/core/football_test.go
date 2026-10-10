package core

import (
	"encoding/json"
	"strconv"
	"testing"

	"playcall/internal/espn"
)

func TestFootballHalftime(t *testing.T) {
	feed := &Feed{
		Away:   TeamScore{Abbr: "TB", Score: "7"},
		Home:   TeamScore{Abbr: "DAL", Score: "10"},
		Clock:  "0:00",
		Period: "2nd",
	}
	end := func(kind string, period int, text string) espn.Data {
		return espn.Data{
			"text":   text,
			"type":   map[string]any{"text": kind},
			"period": map[string]any{"number": json.Number(strconv.Itoa(period))},
			"end":    map[string]any{"shortDownDistanceText": "2nd & 10"},
		}
	}
	cases := []struct {
		name        string
		play        espn.Data
		title, body string
	}{
		{"nfl half", end("End of Half", 2, "END QUARTER 2"), "TB 7 · Halftime · DAL 10", "END QUARTER 2"},
		{"cfb half", end("End Period", 2, "End of 2nd quarter."), "TB 7 · Halftime · DAL 10", "End of 2nd quarter."},
		{"end of q1", end("End Period", 1, "END QUARTER 1"), "TB 7 · 0:00 2nd · DAL 10", "END QUARTER 1 · 2nd & 10"},
		{"end of q3", end("End Period", 3, "END QUARTER 3"), "TB 7 · 0:00 2nd · DAL 10", "END QUARTER 3 · 2nd & 10"},
	}
	for _, c := range cases {
		m := footballPlays{feed}.Message(c.play)
		if m.Title != c.title || m.Body != c.body {
			t.Errorf("%s: got %q / %q, want %q / %q", c.name, m.Title, m.Body, c.title, c.body)
		}
	}
	if m := (footballPlays{feed}).Message(end("End of Half", 2, "x")); m.Clock != "Halftime" || m.Period != "" {
		t.Errorf("event fields: clock %q period %q", m.Clock, m.Period)
	}
}

// ESPN sends " & Goal" (down 0) on timeouts and some penalties; a fragment with
// no down is left out of the body.
func TestFootballBodyDropsDownlessDistance(t *testing.T) {
	play := func(start, end string) espn.Data {
		p := espn.Data{}
		if start != "" {
			p["start"] = map[string]any{"shortDownDistanceText": start}
		}
		if end != "" {
			p["end"] = map[string]any{"shortDownDistanceText": end}
		}
		return p
	}
	for _, c := range []struct {
		name       string
		start, end string
		want       string
	}{
		{"normal", "1st & 10", "2nd & 5", "1st & 10 · Rush · 2nd & 5"},
		{"goal to go", "1st & Goal", "2nd & Goal", "1st & Goal · Rush · 2nd & Goal"},
		{"timeout, no down", " & Goal", "", "Rush"},
		{"no down at either end", " & Goal", "& 3", "Rush"},
		{"only the start lacks a down", " & Goal", "2nd & 2", "Rush · 2nd & 2"},
		{"no start", "", "2nd & 5", "Rush · 2nd & 5"},
	} {
		if got := footballBody(play(c.start, c.end), "Rush"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
