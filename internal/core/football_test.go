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

// A timeout after a punt carries the old fourth down in its own start; its
// body uses the situation after the punt.
func TestFootballTimeoutBodyUsesTheSituationBefore(t *testing.T) {
	punt := espn.Data{"id": "1", "text": "punt 40 yards", "type": map[string]any{"text": "Punt"},
		"start": map[string]any{"shortDownDistanceText": "4th & 2"},
		"end":   map[string]any{"shortDownDistanceText": "1st & 10"}}
	timeout := espn.Data{"id": "2", "text": "Timeout BYU", "type": map[string]any{"text": "Timeout"},
		"start": map[string]any{"shortDownDistanceText": "4th & 2"}}
	second := espn.Data{"id": "3", "text": "Timeout ISU", "type": map[string]any{"text": "Timeout"},
		"start": map[string]any{"shortDownDistanceText": "4th & 2"}}
	score := espn.Data{"id": "4", "text": "touchdown", "type": map[string]any{"text": "Passing Touchdown"},
		"start": map[string]any{"shortDownDistanceText": "2nd & 5"}}
	late := espn.Data{"id": "5", "text": "Timeout BYU", "type": map[string]any{"text": "Timeout"},
		"start": map[string]any{"shortDownDistanceText": "3rd & 1"}}
	r := footballPlays{&Feed{Plays: []espn.Data{punt, timeout, second, score, late}}}
	for play, want := range map[string]string{
		"2": "1st & 10 · Timeout BYU",
		"3": "1st & 10 · Timeout ISU", // two timeouts in a row
		"5": "Timeout BYU",            // after a score: nothing to show
	} {
		var p espn.Data
		for _, q := range r.f.Plays {
			if q["id"] == play {
				p = q
			}
		}
		if got := r.Message(p).Body; got != want {
			t.Errorf("play %s: got %q, want %q", play, got, want)
		}
	}
}
