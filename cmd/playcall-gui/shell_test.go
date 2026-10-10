package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

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
