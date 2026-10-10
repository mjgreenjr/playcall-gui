package main

import (
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
		{core.GameStatus{State: "in", Halftime: true, Clock: "0:00", Period: "2nd"}, "Halftime"},
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
