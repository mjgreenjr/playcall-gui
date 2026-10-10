package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"

	"playcall/internal/core"
)

// noTimer keeps the delay timer from firing during a test: Fyne's test driver
// runs fyne.Do on the timer's goroutine, which would race with the test. The
// tests call show() themselves.
func noTimer(t *testing.T) {
	t.Helper()
	old := tipDelay
	tipDelay = time.Hour
	t.Cleanup(func() { tipDelay = old })
}

func tipInWindow(t *testing.T, width float32, atRight bool) *hoverTip {
	t.Helper()
	test.NewApp()
	noTimer(t)
	img := &canvas.Image{}
	img.SetMinSize(fyne.NewSize(48, 48))
	h := newHoverTip(img)
	var row fyne.CanvasObject = container.NewHBox(h, layout.NewSpacer())
	if atRight {
		row = container.NewHBox(layout.NewSpacer(), h)
	}
	w := test.NewWindow(container.NewVBox(row))
	w.Resize(fyne.NewSize(width, 200))
	return h
}

func TestHoverTipShowsTheNameWhileHoveredAndHidesAfter(t *testing.T) {
	h := tipInWindow(t, 400, false)
	h.SetText("Washington Huskies")

	h.show() // not hovered yet: nothing
	if h.shown() != "" {
		t.Fatal("tip shown without hover")
	}
	h.MouseIn(nil)
	h.show()
	if h.shown() != "Washington Huskies" {
		t.Fatalf("tip %q", h.shown())
	}
	h.MouseOut()
	if h.shown() != "" {
		t.Error("tip still showing after the pointer left")
	}
	h.show() // a late timer after leaving must not bring it back
	if h.shown() != "" {
		t.Error("tip came back after the pointer left")
	}
}

func TestHoverTipWithNoTextShowsNothing(t *testing.T) {
	h := tipInWindow(t, 400, false)
	h.MouseIn(nil)
	h.show()
	if h.shown() != "" {
		t.Errorf("tip %q for a team with no name", h.shown())
	}
	h.MouseOut()
}

func TestHoverTipClosesWhenTheNameChanges(t *testing.T) {
	h := tipInWindow(t, 400, false)
	h.SetText("Iowa Hawkeyes")
	h.MouseIn(nil)
	h.show()
	h.SetText("Washington Huskies") // the header moved on to another game
	if h.shown() != "" {
		t.Errorf("old tip left open: %q", h.shown())
	}
	h.MouseOut()
}

func TestHoverTipStaysInsideTheWindowAtTheRightEdge(t *testing.T) {
	h := tipInWindow(t, 300, true)
	h.SetText("Los Angeles Dodgers and a very long name besides")
	h.MouseIn(nil)
	h.show()
	if h.popup == nil {
		t.Fatal("no tip")
	}
	right := h.popup.Position().X + h.popup.Size().Width
	if right > 300.5 {
		t.Errorf("tip spills out of the window: right edge %v of 300", right)
	}
	h.MouseOut()
}

func TestHeaderLogoTipsAreTheTeamNames(t *testing.T) {
	test.NewApp()
	noTimer(t)
	u := newUI(10*time.Second, nil, highlights[0], nil)
	u.setStatus(core.GameStatus{Sport: "cfb", State: "in",
		Away: core.TeamScore{Abbr: "IOWA", Name: "Iowa Hawkeyes"}, Home: core.TeamScore{Abbr: "WASH", Name: "Washington Huskies"}})
	if u.awayBadge.box.text != "Iowa Hawkeyes" || u.homeBadge.box.text != "Washington Huskies" {
		t.Errorf("tips %q / %q", u.awayBadge.box.text, u.homeBadge.box.text)
	}
	u.setStatus(core.GameStatus{Sport: "cfb", State: "in", Away: core.TeamScore{Abbr: "A"}, Home: core.TeamScore{Abbr: "B"}})
	if u.awayBadge.box.text != "" {
		t.Errorf("stale tip %q", u.awayBadge.box.text)
	}
}
