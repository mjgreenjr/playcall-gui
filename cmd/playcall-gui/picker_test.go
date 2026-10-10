package main

import (
	"context"
	"os"
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
