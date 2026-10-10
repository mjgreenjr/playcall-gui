package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"playcall/internal/core"
	"playcall/internal/espn"
)

// urlFetcher serves a scoreboard or a team list by URL.
type urlFetcher struct{ t *testing.T }

func (f urlFetcher) Fetch(_ context.Context, url string) (espn.Data, error) {
	name := "scoreboard_nfl_week.json"
	if strings.Contains(url, "/teams") {
		name = "teams_nfl.json"
	}
	b, err := os.ReadFile("../../test/fixtures/" + name)
	if err != nil {
		f.t.Fatal(err)
	}
	return espn.Decode(b)
}

func TestShellSettingsRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // the shell saves to the real config path
	s := &shell{root: context.Background(), fetcher: urlFetcher{t}}
	s.loadConfig()
	s.app = test.NewApp()
	s.win = s.app.NewWindow("playcall")

	s.showPicker("nfl")
	if s.pickerView == nil || s.pickerView.onSettings == nil {
		t.Fatal("picker has no settings button wiring")
	}
	if len(s.pickerView.games) != 15 {
		t.Fatalf("picker shows %d games", len(s.pickerView.games))
	}

	s.pickerView.onSettings()
	if s.settings == nil || s.win.Content() != s.settings.content {
		t.Fatal("settings not shown")
	}
	// Choose a favorite team, a default group and a color in the screen.
	s.settings.setFavorite("DAL", true)
	s.settings.group.SetSelected("NFC East")
	s.settings.color.SetSelected("Green")
	s.settings.interval.SetSelected("30 seconds")

	// Saved to the file ...
	paths, _ := core.DefaultPaths()
	saved, err := core.LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.FavoriteTeams["nfl"]; len(got) != 1 || got[0] != "DAL" ||
		saved.FavoriteGroup("nfl") != "NFC East" || saved.HighlightColor() != "green" || core.ResolveInterval(nil, saved) != 30 {
		t.Errorf("saved config: %+v", saved)
	}
	// ... and used by the app right away.
	if s.highlight.key != "green" || s.liveInterval().Seconds() != 30 {
		t.Errorf("app state: %v / %v", s.highlight, s.liveInterval())
	}

	// Back returns to the same picker, now filtering on the new favorite.
	s.settings.env.back()
	if s.win.Content() != s.pickerView.content {
		t.Fatal("did not return to the picker")
	}
	if s.pickerView.group.Selected != "NFC East" {
		t.Errorf("picker group %q", s.pickerView.group.Selected)
	}
	if got := s.pickerView.favorites["nfl"]; len(got) != 1 || got[0] != "DAL" {
		t.Errorf("picker favorites %v", got)
	}
	for _, g := range s.pickerView.games {
		if g.Away != "DAL" && g.Home != "DAL" {
			t.Errorf("non-favorite game shown: %s", matchup(g))
		}
	}
}

// rowText finds the RichText of a play row built by newRow.
func rowText(row fyne.CanvasObject) *widget.RichText {
	return row.(*fyne.Container).Objects[0].(*widget.RichText)
}

func segmentColor(row fyne.CanvasObject) fyne.ThemeColorName {
	return rowText(row).Segments[0].(*widget.TextSegment).Style.ColorName
}

func TestGameViewGearAndHighlightRecolor(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlightByKey("blue"), nil)
	opened := 0
	u.onSettings = func() { opened++ }
	// The gear is in the header, not only on the picker.
	var gear *widget.Button
	var find func(o fyne.CanvasObject)
	find = func(o fyne.CanvasObject) {
		switch w := o.(type) {
		case *widget.Button:
			if w.Icon == theme.SettingsIcon() {
				gear = w
			}
		case *fyne.Container:
			for _, c := range w.Objects {
				find(c)
			}
		}
	}
	find(u.content)
	if gear == nil {
		t.Fatal("no settings gear in the game view")
	}
	test.Tap(gear)
	if opened != 1 {
		t.Errorf("gear opened settings %d times", opened)
	}

	u.addEvent(core.Event{PlayID: "1", Kind: core.KindPlay, Body: "TOUCHDOWN", Urgent: true})
	u.addEvent(core.Event{PlayID: "2", Kind: core.KindPlay, Body: "rush for 2"})
	if segmentColor(u.rows.Objects[1]) != theme.ColorNamePrimary { // the TD, now second
		t.Fatalf("urgent play color %v", segmentColor(u.rows.Objects[1]))
	}
	u.setHighlight(highlightByKey("green"))
	if segmentColor(u.rows.Objects[1]) != theme.ColorNameSuccess {
		t.Errorf("after recolor: %v", segmentColor(u.rows.Objects[1]))
	}
	if segmentColor(u.rows.Objects[0]) != theme.ColorNameForeground {
		t.Errorf("a normal play changed color: %v", segmentColor(u.rows.Objects[0]))
	}
	// A play arriving afterwards uses the new color too.
	u.addEvent(core.Event{PlayID: "3", Kind: core.KindPlay, Body: "FIELD GOAL", Urgent: true})
	if segmentColor(u.rows.Objects[0]) != theme.ColorNameSuccess || len(u.rows.Objects) != 3 {
		t.Errorf("new row color %v, %d rows", segmentColor(u.rows.Objects[0]), len(u.rows.Objects))
	}
}

func TestShellSettingsFromAGameReturnsToTheGame(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := &shell{root: context.Background(), fetcher: urlFetcher{t}}
	s.loadConfig()
	s.app = test.NewApp()
	s.win = s.app.NewWindow("playcall")

	// A game on screen, as follow() leaves it (without its polling goroutine).
	view := newUI(10*time.Second, nil, s.highlight, nil)
	s.gameView = view
	view.onSettings = func() { s.settingsFromGame("nfl", view) }
	s.win.SetContent(view.content)
	view.addEvent(core.Event{PlayID: "1", Kind: core.KindPlay, Body: "TOUCHDOWN", Urgent: true})

	view.onSettings()
	if s.settings == nil || s.win.Content() != s.settings.content {
		t.Fatal("settings not shown over the game")
	}
	// The game is still there, and still receives plays while settings are open.
	view.addEvent(core.Event{PlayID: "2", Kind: core.KindPlay, Body: "rush for 2"})
	if len(view.model.Rows) != 2 {
		t.Errorf("game stopped updating: %d rows", len(view.model.Rows))
	}

	s.settings.color.SetSelected("Red")
	s.settings.env.back()
	if s.win.Content() != view.content {
		t.Fatal("Back did not return to the game")
	}
	if view.hl.key != "red" || segmentColor(view.rows.Objects[1]) != theme.ColorNameError {
		t.Errorf("game not recolored: %v / %v", view.hl, segmentColor(view.rows.Objects[1]))
	}
	paths, _ := core.DefaultPaths()
	if saved, err := core.LoadConfig(paths); err != nil || saved.HighlightColor() != "red" {
		t.Errorf("saved %q, %v", saved.HighlightColor(), err)
	}
}

// Following a game from the picker remembers it, and the next picker offers
// to continue it.
func TestShellContinueLastGame(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // follow() starts its polling, which stops at once
	s := &shell{root: ctx, fetcher: urlFetcher{t}}
	s.loadConfig()
	s.app = test.NewApp()
	s.win = s.app.NewWindow("playcall")

	s.showPicker("nfl")
	if s.pickerView.cont.Visible() {
		t.Fatal("a fresh config should have no Continue button")
	}
	s.followPicked(core.LastGame{Sport: "cfb", ID: "401856826", Label: "ISU @ BYU"})
	paths, _ := core.DefaultPaths()
	saved, err := core.LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if last, ok := saved.LastGame(); !ok || last.ID != "401856826" || last.Sport != "cfb" {
		t.Errorf("saved %+v, %v", last, ok)
	}

	// Back at the picker the game is offered, with its sport.
	s.showPicker("nfl")
	if !s.pickerView.cont.Visible() || s.pickerView.cont.Text != "Continue: ISU @ BYU (CFB)" {
		t.Errorf("button: visible=%v text=%q", s.pickerView.cont.Visible(), s.pickerView.cont.Text)
	}
	s.endGame()
}
