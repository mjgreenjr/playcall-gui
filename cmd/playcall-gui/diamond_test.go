package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
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
	img := u.diamond.render(diamondW*4, diamondH*4)
	alpha := func(c pt) uint8 { _, _, _, a := img.At(int(c.x*4), int(c.y*4)).RGBA(); return uint8(a >> 8) }
	if alpha(baseCenters[0]) != 255 || alpha(baseCenters[2]) != 255 {
		t.Error("an occupied base is not solid")
	}
	if alpha(baseCenters[1]) != 0 {
		t.Error("an empty base is not hollow")
	}
	if alpha(pt{baseCenters[1].x, baseCenters[1].y - baseRadius + 1}) == 0 {
		t.Error("an empty base has no outline")
	}
	if alpha(pt{18, 25}) != 0 || alpha(pt{13.3, 22}) == 0 {
		t.Error("home plate is not an outline")
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

func TestMiddleStaysPutWhenTheNamesChange(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	w := test.NewWindow(u.content)
	st := core.GameStatus{Sport: "mlb", State: "in", Detail: "Top 6", Situation: "1-2, 1 out, 2 on", Bases: "13",
		Away: core.TeamScore{Abbr: "LAD", Score: "3"}, Home: core.TeamScore{Abbr: "ATL", Score: "1"}, BattingSide: "away"}
	centers := map[string]float32{}
	for _, c := range []struct{ name, bat, pitch string }{
		{"short", "Ohtani", "Sale"},
		{"long", "Jung Hoo Lee", "Yoshinobu Yamamoto"},
	} {
		st.Batter, st.Pitcher = c.bat, c.pitch
		u.setStatus(st)
		for i := 0; i < 4; i++ { // the test driver needs a few passes to settle
			w.Resize(fyne.NewSize(620+float32(i), 300))
			u.content.Refresh()
		}
		centers[c.name] = absX(u.diamond.box) + u.diamond.box.Size().Width/2
	}
	mid := w.Canvas().Size().Width / 2
	if centers["short"] != centers["long"] || centers["short"] < mid-1 || centers["short"] > mid+1 {
		t.Errorf("the diamond's center is %v with short names and %v with long ones, window center %v", centers["short"], centers["long"], mid)
	}
}
