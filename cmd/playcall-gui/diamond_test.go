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
	st := core.GameStatus{Sport: "mlb", State: "in", Situation: "1-2, 1 out, 2 on", Bases: "13", Balls: 1, Strikes: 2, Outs: 1}
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
	if got := u.situation.Segments[0].(interface{ Textual() string }).Textual(); got != "" {
		t.Errorf("situation %q: the circles replace the count text", got)
	}
	if !u.counts.box.Visible() || u.counts.balls.filled != 1 || u.counts.strikes.filled != 2 || u.counts.outs.filled != 1 {
		t.Errorf("count circles: balls %d, strikes %d, outs %d (visible %v)", u.counts.balls.filled, u.counts.strikes.filled, u.counts.outs.filled, u.counts.box.Visible())
	}
	st.Bases = ""
	u.setStatus(st)
	if got := u.diamond.filled; got != [3]bool{} || !u.diamond.box.Visible() {
		t.Errorf("empty bases: %v, visible %v", got, u.diamond.box.Visible())
	}
	u.setStatus(core.GameStatus{Sport: "mlb", State: "pre"})
	if u.diamond.box.Visible() || u.counts.box.Visible() {
		t.Error("diamond or count shown with no game in play")
	}
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in", Situation: "1st & 10 at NO 25"})
	if u.diamond.box.Visible() || u.counts.box.Visible() {
		t.Error("diamond or count shown for football")
	}
}

func TestCountCirclesAreCappedAndRecolored(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	u.setStatus(core.GameStatus{Sport: "mlb", State: "in", Situation: "3 outs", Balls: 4, Strikes: 3, Outs: 3})
	if u.counts.balls.filled != 3 || u.counts.strikes.filled != 2 || u.counts.outs.filled != 2 {
		t.Errorf("a full count shows %d, %d, %d circles; at most 3, 2 and 2", u.counts.balls.filled, u.counts.strikes.filled, u.counts.outs.filled)
	}
	if u.counts.balls.pips[2].StrokeWidth != 0 {
		t.Error("a filled circle has an outline")
	}
	u.setStatus(core.GameStatus{Sport: "mlb", State: "in", Situation: "0 outs"})
	if u.counts.balls.pips[0].StrokeWidth == 0 || u.counts.balls.pips[0].FillColor != nil {
		t.Error("an empty circle is not an outline")
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
		centers[c.name] = (absX(u.diamond.box) + absX(u.counts.box) + u.counts.box.Size().Width) / 2 // the diamond and the count together
	}
	mid := w.Canvas().Size().Width / 2
	if centers["short"] != centers["long"] || centers["short"] < mid-1 || centers["short"] > mid+1 {
		t.Errorf("the bases and count's center is %v with short names and %v with long ones, window center %v", centers["short"], centers["long"], mid)
	}
}
