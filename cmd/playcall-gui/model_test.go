package main

import (
	"context"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
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

// ESPN posted a blocked field goal after three plays that followed it. It
// goes below those, above the plays before it.
func TestModelLatePlayGoesInGameOrder(t *testing.T) {
	var m Model
	at := func(sec int) time.Time { return time.Date(2026, 10, 12, 23, 40, sec, 0, time.UTC) }
	for i, id := range []string{"812", "869", "891", "913"} {
		m.Apply(core.Event{PlayID: id, Kind: core.KindPlay, At: at(10 * (i + 1))})
	}
	index, added := m.ApplyAt(core.Event{PlayID: "848", Kind: core.KindPlay, Late: true, At: at(15)})
	if !added || index != 3 {
		t.Fatalf("late play at %d (added %v), want 3", index, added)
	}
	got := ""
	for _, r := range m.Rows {
		got += r.PlayID + " "
	}
	if got != "913 891 869 848 812 " {
		t.Errorf("order: %s", got)
	}
	// Same stamp as a row: the late play goes above it, as an arrival would.
	if index, _ := m.ApplyAt(core.Event{PlayID: "850", Kind: core.KindPlay, Late: true, At: at(30)}); index != 1 {
		t.Errorf("tie went to %d, want 1", index)
	}
	// Older than everything: the bottom.
	if index, _ := m.ApplyAt(core.Event{PlayID: "700", Kind: core.KindPlay, Late: true, At: at(1)}); index != len(m.Rows)-1 {
		t.Errorf("oldest went to %d, want the bottom", index)
	}
}

// A late play with no stamp, or a play that is not late, goes on top, and a
// late one never lands above the final.
func TestModelLateWithoutStampOrAfterFinal(t *testing.T) {
	var m Model
	at := time.Date(2026, 10, 12, 23, 40, 0, 0, time.UTC)
	m.Apply(core.Event{PlayID: "1", Kind: core.KindPlay, At: at})
	if index, _ := m.ApplyAt(core.Event{PlayID: "2", Kind: core.KindPlay, Late: true}); index != 0 {
		t.Errorf("late without a stamp went to %d", index)
	}
	if index, _ := m.ApplyAt(core.Event{PlayID: "3", Kind: core.KindPlay, At: at.Add(-time.Hour)}); index != 0 {
		t.Errorf("a play that is not late went to %d", index)
	}
	m.Apply(core.Event{Kind: core.KindFinal})
	if index, _ := m.ApplyAt(core.Event{PlayID: "4", Kind: core.KindPlay, Late: true, At: at.Add(time.Second)}); index == 0 {
		t.Errorf("late play went above the final")
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
		if got := statusLine(c.s, time.Now()); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.s, got, c.want)
		}
	}
}

func TestHalftimeCountdown(t *testing.T) {
	now := time.Date(2026, 10, 12, 1, 50, 0, 0, time.UTC)
	s := core.GameStatus{State: "in", Halftime: true, HalftimeEnds: now.Add(9*time.Minute + 5*time.Second)}
	if got := statusLine(s, now); got != "Halftime · ~9:05" {
		t.Errorf("counting: %q", got)
	}
	if got := statusLine(s, now.Add(10*time.Minute)); got != "Halftime" {
		t.Errorf("past the estimate: %q", got)
	}
	s.HalftimeEnds = time.Time{}
	if got := statusLine(s, now); got != "Halftime" {
		t.Errorf("no estimate: %q", got)
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
		{"wnba", &core.TeamCounts{Timeouts: 0, TimeoutSlots: 3, Fouls: 5, Bonus: "double"}, "TO ○○○ · Bonus"},
		{"nba", &core.TeamCounts{Timeouts: 6, TimeoutSlots: 7, Fouls: 0, Bonus: "double"}, "TO ●●●●●●○ · Fouls 0 · Bonus"},
		{"nba", &core.TeamCounts{Timeouts: 6, TimeoutSlots: 7, Fouls: 4}, "TO ●●●●●●○"},
		{"nba", &core.TeamCounts{Timeouts: 6, TimeoutSlots: 7, Fouls: 3}, "TO ●●●●●●○ · Fouls 3"},
		{"nba", &core.TeamCounts{Timeouts: 2, TimeoutSlots: 3}, "TO ●●○ · Fouls 0"},
		{"nba", &core.TeamCounts{}, "TO 0 · Fouls 0"},
		{"cbb", &core.TeamCounts{Timeouts: 1, Fouls: 7, Bonus: "single"}, "TO ● · 1-and-1"},
		{"cbb", &core.TeamCounts{Timeouts: 1, Fouls: 10, Bonus: "double"}, "TO ● · Double bonus"},
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
	// Every circle has the same size; the ones used are outlined, not filled.
	for _, c := range []struct {
		d            *dotRow
		left, filled int
	}{{u.awayTO, 3, 3}, {u.homeTO, 1, 1}} {
		if !c.d.box.Visible() || len(c.d.dots) != 3 || c.d.left != c.left {
			t.Fatalf("row: shown %v, %d circles, %d left", c.d.box.Visible(), len(c.d.dots), c.d.left)
		}
		filled := 0
		for _, dot := range c.d.dots {
			if dot.FillColor != nil {
				filled++
			}
		}
		if filled != c.filled {
			t.Errorf("%d circles filled, want %d", filled, c.filled)
		}
	}
	if u.awayCounts.box.Visible() || u.homeCounts.box.Visible() {
		t.Error("the basketball row is showing for football")
	}
	// Overtime is two each, and a sport with no counts hides the row.
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in",
		AwayCounts: &core.TeamCounts{Timeouts: 2, TimeoutSlots: 2}, HomeCounts: &core.TeamCounts{Timeouts: 1, TimeoutSlots: 2}})
	if len(u.awayTO.dots) != 2 || len(u.homeTO.dots) != 2 || u.homeTO.left != 1 {
		t.Errorf("overtime: %d and %d circles", len(u.awayTO.dots), len(u.homeTO.dots))
	}
	u.setStatus(core.GameStatus{Sport: "cfb", State: "in"})
	if u.awayTO.box.Visible() || u.homeTO.box.Visible() {
		t.Error("college football shows timeouts with no counts (a restart in the second half)")
	}
	u.setStatus(core.GameStatus{Sport: "cfb", State: "in",
		AwayCounts: &core.TeamCounts{Timeouts: 1, TimeoutSlots: 3}, HomeCounts: &core.TeamCounts{Timeouts: 3, TimeoutSlots: 3}})
	if !u.awayTO.box.Visible() || len(u.awayTO.dots) != 3 || u.awayTO.left != 1 || u.homeTO.left != 3 {
		t.Errorf("college football: shown %v, %d circles, %d and %d left", u.awayTO.box.Visible(), len(u.awayTO.dots), u.awayTO.left, u.homeTO.left)
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
	// Both sit centered in one column, so they line up only if the score's text
	// carries no padding of its own (a leading space pushed it off the dots).
	for _, c := range []struct {
		score *canvas.Text
		to    *dotRow
		want  string
	}{{u.awayScore, u.awayTO, "0"}, {u.homeScore, u.homeTO, "27"}} {
		if c.score.Text != c.want || c.score.Alignment != fyne.TextAlignCenter {
			t.Errorf("score %q (align %v)", c.score.Text, c.score.Alignment)
		}
		if absX(c.score) != absX(c.to.box) || c.score.Size().Width != c.to.box.Size().Width {
			t.Errorf("score column %v+%v, timeouts column %v+%v", absX(c.score), c.score.Size().Width, absX(c.to.box), c.to.box.Size().Width)
		}
	}
}

func TestSeriesTagShowsGameAndLength(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	u.setStatus(core.GameStatus{Sport: "mlb", State: "pre", SeriesInfo: "NLCS - Game 1 · Best of 7"})
	if !u.series.Visible() || u.series.Text != "NLCS - Game 1 · Best of 7" {
		t.Errorf("0-0: %q (%v)", u.series.Text, u.series.Visible())
	}
	u.setStatus(core.GameStatus{Sport: "mlb", State: "in", SeriesInfo: "NLCS - Game 3 · Best of 7", Series: "LAD 2-0"})
	if u.series.Text != "NLCS - Game 3 · Best of 7 · Series: LAD 2-0" {
		t.Errorf("with a standing: %q", u.series.Text)
	}
}

func TestHeaderShowsRankingsOnlyForRankedTeams(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	u.setStatus(core.GameStatus{Sport: "cfb", State: "in",
		Away: core.TeamScore{Abbr: "OU"}, Home: core.TeamScore{Abbr: "TEX", Rank: "1"}})
	if u.awayRank.box.Visible() || u.awayRank.text.Text != "" {
		t.Error("an unranked team shows a ranking")
	}
	if !u.homeRank.box.Visible() || u.homeRank.text.Text != "1" {
		t.Errorf("home ranking %q visible=%v", u.homeRank.text.Text, u.homeRank.box.Visible())
	}
	// The next status can drop it again (a new game, or the sport changed).
	u.setStatus(core.GameStatus{Sport: "cfb", State: "in", Away: core.TeamScore{Abbr: "OU"}, Home: core.TeamScore{Abbr: "TEX"}})
	if u.homeRank.box.Visible() {
		t.Error("ranking stayed after the team lost it")
	}
}

func TestBasketballTimeoutsAreDrawn(t *testing.T) {
	test.NewApp()
	l := newCountsLine()
	l.set("wnba", &core.TeamCounts{Timeouts: 2, TimeoutSlots: 4, Fouls: 3, Bonus: "double"})
	if !l.box.Visible() || len(l.dots.dots) != 4 || l.dots.left != 2 {
		t.Fatalf("visible %v, %d circles, %d filled", l.box.Visible(), len(l.dots.dots), l.dots.left)
	}
	if l.to.Text != "TO" || l.rest.Text != "· Fouls 3 · Bonus" {
		t.Errorf("texts %q, %q", l.to.Text, l.rest.Text)
	}
	l.set("wnba", &core.TeamCounts{Fouls: 1})
	if l.to.Text != "TO 0" || l.dots.box.Visible() {
		t.Errorf("no timeouts: %q, dots visible %v", l.to.Text, l.dots.box.Visible())
	}
	l.set("wnba", nil)
	if l.box.Visible() {
		t.Error("nil counts should hide the line")
	}
}

func TestRecheckUntilStopsWhenDone(t *testing.T) {
	calls := 0
	recheckUntil(context.Background(), time.Millisecond, time.Second,
		func() { calls++ }, func() bool { return calls == 3 })
	if calls != 3 {
		t.Errorf("fetched %d times, want 3", calls)
	}
	calls = 0
	recheckUntil(context.Background(), time.Millisecond, 20*time.Millisecond,
		func() { calls++ }, func() bool { return false })
	if calls < 2 || calls > 20 {
		t.Errorf("never done: fetched %d times in 20 ms", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	recheckUntil(ctx, time.Millisecond, time.Second, func() { calls++ }, func() bool { return false })
	if calls != 0 {
		t.Errorf("cancelled context still fetched %d times", calls)
	}
}
