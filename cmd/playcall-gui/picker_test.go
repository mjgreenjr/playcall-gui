package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"playcall/internal/espn"
)

type fixtureFetcher struct{ data espn.Data }

func (f fixtureFetcher) Fetch(context.Context, string) (espn.Data, error) { return f.data, nil }

func scoreboard(t *testing.T, name string) espn.Data {
	t.Helper()
	b, err := os.ReadFile("../../test/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := espn.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// waitFor polls until cond holds; the picker loads on a goroutine.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPickerShowsGamesAndPicks(t *testing.T) {
	test.NewApp()
	var pickedSport string
	var picked espn.Game
	p := newPicker(context.Background(), fixtureFetcher{scoreboard(t, "scoreboard_nfl_week.json")},
		map[string][]string{"nfl": {"DAL"}}, "nfl", func(sport string, g espn.Game) { pickedSport, picked = sport, g })
	p.load()

	// Favorites on: only games with DAL.
	waitFor(t, "favorite games", func() bool { return len(p.games) > 0 })
	for _, g := range p.games {
		if g.Away != "DAL" && g.Home != "DAL" {
			t.Errorf("non-favorite game shown: %s", matchup(g))
		}
	}
	favCount := len(p.games)

	// "All games" shows the whole scoreboard.
	p.all.SetChecked(true)
	if len(p.games) != 15 || len(p.games) <= favCount {
		t.Errorf("all games: %d (favorites %d)", len(p.games), favCount)
	}

	p.list.Select(2)
	if pickedSport != "nfl" || picked.ID != p.games[2].ID {
		t.Errorf("picked %q %+v, want game %s", pickedSport, picked, p.games[2].ID)
	}
}

func TestPickerNoFavoritesShowsAll(t *testing.T) {
	test.NewApp()
	p := newPicker(context.Background(), fixtureFetcher{scoreboard(t, "scoreboard_nfl_week.json")},
		map[string][]string{"nfl": nil}, "nfl", func(string, espn.Game) {})
	p.load()
	waitFor(t, "games", func() bool { return len(p.games) > 0 })
	if !p.all.Checked || len(p.games) != 15 {
		t.Errorf("all checked %v, games %d", p.all.Checked, len(p.games))
	}
}

func TestPickerEmptyScoreboard(t *testing.T) {
	test.NewApp()
	p := newPicker(context.Background(), fixtureFetcher{scoreboard(t, "scoreboard_empty.json")},
		nil, "nfl", func(string, espn.Game) {})
	p.load()
	waitFor(t, "status", func() bool { return p.status.Text == "No NFL games on the scoreboard." })
}

type recordingFetcher struct {
	data espn.Data
	mu   sync.Mutex
	urls []string
}

func (r *recordingFetcher) Fetch(_ context.Context, url string) (espn.Data, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls = append(r.urls, url)
	return r.data, nil
}

func (r *recordingFetcher) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.urls) == 0 {
		return ""
	}
	return r.urls[len(r.urls)-1]
}

func TestPickerGroupFilter(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, map[string][]string{"nfl": {"DAL"}}, "nfl", func(string, espn.Game) {})
	p.load()
	waitFor(t, "favorites", func() bool { return len(p.games) > 0 })
	favCount := len(p.games)
	if f.last() == "" || strings.Contains(f.last(), "groups=") {
		t.Errorf("Any group sent a groups param: %s", f.last())
	}

	// A group is the filter: favorites no longer apply, and the box locks.
	p.group.SetSelected("AFC")
	waitFor(t, "group request", func() bool { return strings.Contains(f.last(), "groups=8") })
	waitFor(t, "group games", func() bool { return len(p.games) > favCount })
	if !p.all.Checked || !p.all.Disabled() {
		t.Errorf("all games: checked %v disabled %v", p.all.Checked, p.all.Disabled())
	}

	// Back to Any: favorites apply again.
	p.group.SetSelected(anyGroup)
	waitFor(t, "favorites again", func() bool { return len(p.games) == favCount })
	if p.all.Checked || p.all.Disabled() {
		t.Errorf("all games after Any: checked %v disabled %v", p.all.Checked, p.all.Disabled())
	}
}

func TestPickerSportChangeResetsGroups(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, nil, "nfl", func(string, espn.Game) {})
	p.group.SetSelected("AFC North")
	p.sport.SetSelected("CFB")
	if p.group.Selected != anyGroup {
		t.Errorf("group %q after sport change", p.group.Selected)
	}
	found := false
	for _, o := range p.group.Options {
		if o == "Big Ten" {
			found = true
		}
		if o == "AFC North" {
			t.Error("old sport's group still offered")
		}
	}
	if !found {
		t.Errorf("cfb groups missing: %v", p.group.Options)
	}
	waitFor(t, "cfb request", func() bool { return strings.Contains(f.last(), "groups=80") })
}
