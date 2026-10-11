package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"testing"
	"time"

	"playcall/internal/core"
)

func TestModelNewestFirst(t *testing.T) {
	var m Model
	for _, id := range []string{"1", "2", "3"} {
		if !m.Apply(core.Event{PlayID: id, Kind: core.KindPlay}) {
			t.Fatalf("play %s not added", id)
		}
	}
	if got := []string{m.Rows[0].PlayID, m.Rows[1].PlayID, m.Rows[2].PlayID}; got[0] != "3" || got[2] != "1" {
		t.Errorf("order %v", got)
	}
}

func TestModelUpdateRewritesRow(t *testing.T) {
	var m Model
	m.Apply(core.Event{PlayID: "1", Kind: core.KindPlay, Body: "TD"})
	m.Apply(core.Event{PlayID: "2", Kind: core.KindPlay, Body: "run"})
	if m.Apply(core.Event{PlayID: "1", Kind: core.KindUpdate, Body: "TD, PAT good"}) {
		t.Fatal("update added a row")
	}
	if len(m.Rows) != 2 || m.Rows[1].Body != "TD, PAT good" || !m.Rows[1].Updated {
		t.Errorf("rows: %+v", m.Rows)
	}
}

func TestModelUpdateWithoutOriginalAdds(t *testing.T) {
	var m Model
	if !m.Apply(core.Event{PlayID: "9", Kind: core.KindUpdate}) || len(m.Rows) != 1 {
		t.Errorf("rows: %+v", m.Rows)
	}
}

func TestModelFinalIsARow(t *testing.T) {
	var m Model
	m.Apply(core.Event{PlayID: "1", Kind: core.KindPlay})
	m.Apply(core.Event{Kind: core.KindFinal, Title: "A 1 · Final · B 0"})
	if !m.Rows[0].Final || m.Rows[0].Title != "A 1 · Final · B 0" {
		t.Errorf("rows: %+v", m.Rows)
	}
}

func TestStatusLine(t *testing.T) {
	cases := []struct {
		s    core.GameStatus
		want string
	}{
		{core.GameStatus{State: "in", Clock: "0:48", Period: "1st"}, "0:48 1st"},
		{core.GameStatus{State: "in", Clock: "", Period: "Top 6"}, "Top 6"},
		{core.GameStatus{State: "in", Sport: "mlb", Clock: "0:00", Period: "3rd", Detail: "Top 3rd"}, "Top 3rd"},
		{core.GameStatus{State: "in", Sport: "mlb", Clock: "0:00", Period: "3rd"}, "0:00 3rd"},
		{core.GameStatus{State: "in", Halftime: true, Clock: "0:00", Period: "2nd"}, "Halftime"},
		{core.GameStatus{State: "in", Break: true, Clock: "0.0", Period: "3rd", Detail: "End of 3rd"}, "End of 3rd"},
		{core.GameStatus{State: "post", Final: true, Detail: "Final/OT"}, "Final/OT"},
		{core.GameStatus{State: "post", Final: true}, "Final"},
		{core.GameStatus{State: "pre", Detail: "10/10 - 8:00 PM EDT", Clock: "0:00"}, "10/10 - 8:00 PM EDT"},
	}
	for _, c := range cases {
		if got := statusLine(c.s); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.s, got, c.want)
		}
	}
}

func TestStaleText(t *testing.T) {
	now := time.Date(2026, 10, 9, 21, 0, 0, 0, time.UTC)
	iv := 10 * time.Second
	if got := staleText(time.Time{}, now, iv); got != "" {
		t.Errorf("never fetched: %q", got)
	}
	if got := staleText(now.Add(-14*time.Second), now, iv); got != "" {
		t.Errorf("fresh: %q", got)
	}
	if got := staleText(now.Add(-45*time.Second), now, iv); got != "updated 45s ago" {
		t.Errorf("stale: %q", got)
	}
	// A long interval (pregame sleep) widens the window.
	if got := staleText(now.Add(-45*time.Second), now, 30*time.Second); got != "" {
		t.Errorf("long interval: %q", got)
	}
}

func TestCountsText(t *testing.T) {
	for _, c := range []struct {
		sport string
		c     *core.TeamCounts
		want  string
	}{
		{"nba", nil, ""},
		{"nba", &core.TeamCounts{Timeouts: 3, TimeoutSlots: 7, Fouls: 2}, "TO ●●●○○○○ · Fouls 2"},
		{"wnba", &core.TeamCounts{Timeouts: 0, TimeoutSlots: 3, Fouls: 5, Bonus: "double"}, "TO ○○○ · Fouls 5 · Bonus"},
		{"nba", &core.TeamCounts{Timeouts: 2, TimeoutSlots: 3}, "TO ●●○ · Fouls 0"},
		{"nba", &core.TeamCounts{}, "TO 0 · Fouls 0"},
		{"cbb", &core.TeamCounts{Timeouts: 1, Fouls: 7, Bonus: "single"}, "TO ● · Fouls 7 · 1-and-1"},
		{"cbb", &core.TeamCounts{Timeouts: 1, Fouls: 10, Bonus: "double"}, "TO ● · Fouls 10 · Double bonus"},
		{"nfl", &core.TeamCounts{Timeouts: 2, TimeoutSlots: 3}, "●●○"},
		{"nfl", &core.TeamCounts{Timeouts: 0, TimeoutSlots: 2}, "○○"},
	} {
		if got := countsText(c.sport, c.c); got != c.want {
			t.Errorf("%s %+v: got %q, want %q", c.sport, c.c, got, c.want)
		}
	}
}

func TestFootballTimeoutsUnderScore(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in",
		Away:       core.TeamScore{Abbr: "DEN", Score: "0"},
		Home:       core.TeamScore{Abbr: "LAC", Score: "7"},
		AwayCounts: &core.TeamCounts{Timeouts: 3, TimeoutSlots: 3},
		HomeCounts: &core.TeamCounts{Timeouts: 1, TimeoutSlots: 3}})
	if u.awayTO.Text != "●●●" || u.homeTO.Text != "●○○" || !u.awayTO.Visible() || !u.homeTO.Visible() {
		t.Errorf("under the scores: %q (%v) %q (%v)", u.awayTO.Text, u.awayTO.Visible(), u.homeTO.Text, u.homeTO.Visible())
	}
	if u.awayCounts.Visible() || u.homeCounts.Visible() {
		t.Error("the basketball row is showing for football")
	}
}

func TestFootballScoreCenteredOverTimeouts(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	w := test.NewWindow(u.content)
	w.Resize(fyne.NewSize(600, 200))
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in", Clock: "1:00", Period: "1st",
		Away: core.TeamScore{Abbr: "DEN", Score: "0"}, Home: core.TeamScore{Abbr: "LAC", Score: "27"},
		AwayCounts: &core.TeamCounts{Timeouts: 3, TimeoutSlots: 3}, HomeCounts: &core.TeamCounts{Timeouts: 1, TimeoutSlots: 3}})
	w.Resize(fyne.NewSize(601, 200))
	// Both are centered in one column, so they line up only if the score's text
	// carries no padding of its own (a leading space pushed it off the dots).
	for _, c := range []struct {
		score *canvas.Text
		to    *widget.Label
		want  string
	}{{u.awayScore, u.awayTO, "0"}, {u.homeScore, u.homeTO, "27"}} {
		if c.score.Text != c.want || c.score.Alignment != fyne.TextAlignCenter || c.to.Alignment != fyne.TextAlignCenter {
			t.Errorf("score %q (align %v), timeouts align %v", c.score.Text, c.score.Alignment, c.to.Alignment)
		}
		if absX(c.score) != absX(c.to) || c.score.Size().Width != c.to.Size().Width {
			t.Errorf("score column %v+%v, timeouts column %v+%v", absX(c.score), c.score.Size().Width, absX(c.to), c.to.Size().Width)
		}
	}
}
