package core

import (
	"encoding/json"
	"strconv"
	"strings"
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

// The timeout right after a punt or kickoff is the official's, whatever team
// ESPN files it under; a timeout after anything else is left alone.
func TestFootballOfficialTimeoutAfterAKick(t *testing.T) {
	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	punt := espn.Data{"id": "1", "text": "punt", "type": typ("Punt"), "end": map[string]any{"shortDownDistanceText": "1st & 10"}}
	t1 := espn.Data{"id": "2", "text": "Timeout BYU, clock 08:54", "type": typ("Timeout")}
	t2 := espn.Data{"id": "3", "text": "Timeout Iowa State, clock 08:50", "type": typ("Timeout")}
	rush := espn.Data{"id": "4", "text": "rush", "type": typ("Rush"), "end": map[string]any{"shortDownDistanceText": "2nd & 5"}}
	t3 := espn.Data{"id": "5", "text": "Timeout BYU, clock 05:00", "type": typ("Timeout")}
	kick := espn.Data{"id": "6", "text": "kickoff", "type": typ("Kickoff")}
	t4 := espn.Data{"id": "7", "text": "Timeout Iowa State, clock 04:00", "type": typ("Timeout")}
	r := footballPlays{&Feed{Plays: []espn.Data{punt, t1, t2, rush, t3, kick, t4}}}
	for _, c := range []struct {
		p    espn.Data
		want string
	}{
		{t1, "1st & 10 · Official timeout, clock 08:54"},
		{t2, "1st & 10 · Timeout Iowa State, clock 08:50"}, // second in a row: ESPN's label
		{t3, "2nd & 5 · Timeout BYU, clock 05:00"},
		{t4, "Official timeout, clock 04:00"}, // after a kickoff
	} {
		if got := r.Message(c.p).Body; got != c.want {
			t.Errorf("play %v: got %q, want %q", c.p["id"], got, c.want)
		}
	}
}

// Any change of possession is followed by an official's timeout, not only a kick.
func TestFootballOfficialTimeoutAfterATurnover(t *testing.T) {
	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	team := func(id string) map[string]any { return map[string]any{"team": map[string]any{"id": id}} }
	pick := espn.Data{"id": "1", "text": "pass intercepted", "type": typ("Pass Interception Return"),
		"start": team("252"), "end": map[string]any{"team": map[string]any{"id": "66"}, "shortDownDistanceText": "1st & 10"}}
	downs := espn.Data{"id": "3", "text": "pass incomplete", "type": typ("Pass Incompletion"),
		"start": team("66"), "end": map[string]any{"team": map[string]any{"id": "252"}, "shortDownDistanceText": "1st & 10"}}
	flagged := espn.Data{"id": "5", "text": "fumble", "type": typ("Fumble Recovery (Opponent)"), "isTurnover": true}
	same := espn.Data{"id": "7", "text": "rush", "type": typ("Rush"),
		"start": team("252"), "end": map[string]any{"team": map[string]any{"id": "252"}, "shortDownDistanceText": "2nd & 5"}}
	mk := func(id string) espn.Data {
		return espn.Data{"id": id, "text": "Timeout BYU, clock 08:00", "type": typ("Timeout")}
	}
	plays := []espn.Data{pick, mk("2"), downs, mk("4"), flagged, mk("6"), same, mk("8")}
	r := footballPlays{&Feed{Plays: plays}}
	for _, c := range []struct{ id, want string }{
		{"2", "Official timeout"}, {"4", "Official timeout"}, {"6", "Official timeout"}, {"8", "Timeout BYU"},
	} {
		var p espn.Data
		for _, q := range plays {
			if q["id"] == c.id {
				p = q
			}
		}
		if got := r.Message(p).Body; !strings.Contains(got, c.want) {
			t.Errorf("timeout %s: %q does not contain %q", c.id, got, c.want)
		}
	}
}
