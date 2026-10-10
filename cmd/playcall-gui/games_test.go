package main

import (
	"testing"

	"playcall/internal/espn"
)

func TestOrderForPicker(t *testing.T) {
	in := []espn.Game{
		{ID: "final1", State: "post"},
		{ID: "pre1", State: "pre"},
		{ID: "live1", State: "in"},
		{ID: "pre2", State: "pre"},
		{ID: "live2", State: "in"},
	}
	got := orderForPicker(in)
	want := []string{"live1", "live2", "pre1", "pre2", "final1"}
	for i, g := range got {
		if g.ID != want[i] {
			t.Fatalf("order %v, want %v", ids(got), want)
		}
	}
	if in[0].ID != "final1" {
		t.Error("input was reordered")
	}
}

func ids(gs []espn.Game) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.ID)
	}
	return out
}

func TestGameLines(t *testing.T) {
	live := espn.Game{Away: "TB ", Home: "DAL", State: "in", AwayScore: "7", HomeScore: "10", Status: "0:12 - 2nd"}
	if got := matchup(live); got != "TB @ DAL" {
		t.Errorf("matchup %q", got)
	}
	if got := gameDetail(live); got != "7-10  0:12 - 2nd" {
		t.Errorf("live detail %q", got)
	}
	pre := espn.Game{State: "pre", AwayScore: "0", HomeScore: "0", Status: "10/10 - 8:00 PM EDT"}
	if got := gameDetail(pre); got != "10/10 - 8:00 PM EDT" {
		t.Errorf("pre detail %q", got)
	}
}
