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
