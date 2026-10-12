package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"playcall/internal/core"
)

func TestDiamondShowsOccupiedBases(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	st := core.GameStatus{Sport: "mlb", State: "in", Situation: "1-2, 1 out, 2 on", Bases: "13"}
	u.setStatus(st)
	if !u.diamond.box.Visible() {
		t.Fatal("diamond hidden during an at-bat")
	}
	if got := u.diamond.filled; got != [3]bool{true, false, true} {
		t.Errorf("filled %v, want first and third", got)
	}
	if u.diamond.bases[0].StrokeWidth != 0 || u.diamond.bases[1].StrokeWidth == 0 {
		t.Error("occupied base not solid, or empty base not outlined")
	}
	if got := u.situation.Segments[0].(interface{ Textual() string }).Textual(); got != "1-2, 1 out" {
		t.Errorf("situation %q: the diamond replaces the runner count", got)
	}
	st.Bases = ""
	u.setStatus(st)
	if got := u.diamond.filled; got != [3]bool{} || !u.diamond.box.Visible() {
		t.Errorf("empty bases: %v, visible %v", got, u.diamond.box.Visible())
	}
	u.setStatus(core.GameStatus{Sport: "mlb", State: "pre"})
	if u.diamond.box.Visible() {
		t.Error("diamond shown with no game in play")
	}
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in", Situation: "1st & 10 at NO 25"})
	if u.diamond.box.Visible() {
		t.Error("diamond shown for football")
	}
}

func TestWithoutRunnerCount(t *testing.T) {
	for in, want := range map[string]string{
		"1-2, 1 out, 2 on": "1-2, 1 out",
		"0 outs, 0 on":     "0 outs",
		"3 outs":           "3 outs",
	} {
		if got := withoutRunnerCount(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
