package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"playcall/internal/core"
	"playcall/internal/espn"
)

func init() {
	runAsync = func(f func()) { f() } // see runAsync
}

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
		map[string][]string{"nfl": {"DAL"}}, groupPrefs{}, "nfl", func(sport string, g espn.Game) { pickedSport, picked = sport, g })
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
		map[string][]string{"nfl": nil}, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.load()
	waitFor(t, "games", func() bool { return len(p.games) > 0 })
	if len(p.games) != 15 || p.status.Text != "15 game(s)" {
		t.Errorf("games %d, status %q", len(p.games), p.status.Text)
	}
}

func TestPickerEmptyScoreboard(t *testing.T) {
	test.NewApp()
	p := newPicker(context.Background(), fixtureFetcher{scoreboard(t, "scoreboard_empty.json")},
		nil, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.load()
	waitFor(t, "status", func() bool { return p.status.Text == "No NFL Football games on the scoreboard." })
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

// has reports whether any request so far contained substr. Loads overlap, so
// the most recent request is not always the one a test cares about.
func (r *recordingFetcher) has(substr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.urls {
		if strings.Contains(u, substr) {
			return true
		}
	}
	return false
}

func (r *recordingFetcher) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.urls) == 0 {
		return ""
	}
	return r.urls[len(r.urls)-1]
}

func TestPickerFavoritesNarrowWithinAGroup(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, map[string][]string{"nfl": {"DAL"}}, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.load()
	waitFor(t, "favorites", func() bool { return len(p.games) > 0 })
	favCount := len(p.games)
	if f.has("groups=") {
		t.Errorf("All sent a groups param: %v", f.urls)
	}

	// A group is chosen and a favorite plays in it: still only the favorites.
	p.group.SetSelected("AFC")
	waitFor(t, "group request", func() bool { return f.has("groups=8") })
	waitFor(t, "status", func() bool { return strings.Contains(p.status.Text, "favorite-team") })
	if len(p.games) != favCount || p.all.Disabled() || p.all.Checked {
		t.Errorf("games %d (want %d), all disabled %v checked %v", len(p.games), favCount, p.all.Disabled(), p.all.Checked)
	}

	// "All games" shows the rest of the group.
	p.all.SetChecked(true)
	if len(p.games) != 15 {
		t.Errorf("all games: %d", len(p.games))
	}
}

func TestPickerShowsTheWholeGroupWhenNoFavoritePlays(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, map[string][]string{"nfl": {"ZZZ"}}, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.group.SetSelected("AFC")
	waitFor(t, "whole group", func() bool { return len(p.games) == 15 })
	if !strings.Contains(p.status.Text, "None of your favorite teams play here; showing all 15") {
		t.Errorf("status %q", p.status.Text)
	}
}

func TestPickerSportChangeResetsGroups(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, nil, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.group.SetSelected("AFC North")
	p.sport.SetSelected(sportLabel("cfb"))
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
	waitFor(t, "cfb request", func() bool { return f.has("groups=80") })
}

// memoryPrefs is groupPrefs backed by a map, counting saves.
func memoryPrefs(initial map[string]string) (groupPrefs, map[string]string, *int) {
	m := map[string]string{}
	for k, v := range initial {
		m[k] = v
	}
	saves := 0
	return groupPrefs{
		get: func(sport string) string { return m[sport] },
		set: func(sport, group string) { saves++; m[sport] = group },
	}, m, &saves
}

func TestPickerStartsOnTheRememberedGroup(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	prefs, _, saves := memoryPrefs(map[string]string{"nfl": "AFC North", "cfb": "Big Ten"})
	p := newPicker(context.Background(), f, nil, prefs, "nfl", func(string, espn.Game) {})
	p.load()
	if p.group.Selected != "AFC North" {
		t.Fatalf("group %q", p.group.Selected)
	}
	waitFor(t, "group request", func() bool { return f.has("groups=12") })
	if *saves != 0 {
		t.Errorf("starting up saved %d times", *saves)
	}

	// Another sport starts on its own remembered group.
	p.sport.SetSelected(sportLabel("cfb"))
	if p.group.Selected != "Big Ten" {
		t.Errorf("cfb group %q", p.group.Selected)
	}
	waitFor(t, "cfb group request", func() bool { return f.has("groups=5") })
	// A sport with nothing remembered starts on All.
	p.sport.SetSelected(sportLabel("mlb"))
	if p.group.Selected != anyGroup {
		t.Errorf("mlb group %q", p.group.Selected)
	}
	if *saves != 0 {
		t.Errorf("changing sport saved %d times", *saves)
	}
}

func TestPickerRemembersWhatTheUserPicks(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	prefs, m, saves := memoryPrefs(nil)
	p := newPicker(context.Background(), f, nil, prefs, "nfl", func(string, espn.Game) {})
	p.group.SetSelected("NFC East")
	if m["nfl"] != "NFC East" || *saves != 1 {
		t.Errorf("saved %v (%d)", m, *saves)
	}
	p.group.SetSelected("AFC West") // another pick replaces it
	if m["nfl"] != "AFC West" {
		t.Errorf("saved %v", m)
	}
	p.group.SetSelected(anyGroup) // going back to All is remembered too
	if m["nfl"] != "" {
		t.Errorf("saved %v", m)
	}
}

func TestPickerIgnoresAForgottenGroup(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	prefs, _, _ := memoryPrefs(map[string]string{"nfl": "Not A Division"})
	p := newPicker(context.Background(), f, nil, prefs, "nfl", func(string, espn.Game) {})
	if p.group.Selected != anyGroup {
		t.Errorf("group %q", p.group.Selected)
	}
}

func TestPickerKeepsAllGamesWhenTheGroupChanges(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, map[string][]string{"nfl": {"DAL"}}, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.load()
	p.all.SetChecked(true)
	p.group.SetSelected("AFC")
	p.group.SetSelected("NFC East")
	if !p.all.Checked || len(p.games) != 15 {
		t.Errorf("all games checked %v, games %d", p.all.Checked, len(p.games))
	}
}

func TestPickerShowsSportNames(t *testing.T) {
	test.NewApp()
	f := &recordingFetcher{data: scoreboard(t, "scoreboard_nfl_week.json")}
	p := newPicker(context.Background(), f, nil, groupPrefs{}, "cfb", func(string, espn.Game) {})
	if p.sport.Selected != "FBS College Football" || p.currentSport() != "cfb" {
		t.Errorf("selected %q / %q", p.sport.Selected, p.currentSport())
	}
	p.load()
	if p.status.Text != "15 game(s)" {
		t.Errorf("status %q", p.status.Text)
	}
}

func TestPickerContinueButton(t *testing.T) {
	test.NewApp()
	p := newPicker(context.Background(), fixtureFetcher{scoreboard(t, "scoreboard_nfl_week.json")},
		nil, groupPrefs{}, "nfl", func(string, espn.Game) {})
	if p.cont.Visible() {
		t.Fatal("no last game: the button should be hidden")
	}
	tapped := false
	p.setContinue("ISU @ BYU (CFB)", func() { tapped = true })
	if !p.cont.Visible() || p.cont.Text != "Continue: ISU @ BYU (CFB)" {
		t.Errorf("button: visible=%v text=%q", p.cont.Visible(), p.cont.Text)
	}
	test.Tap(p.cont)
	if !tapped {
		t.Error("tapping did not continue")
	}
	p.setContinue("", nil)
	if p.cont.Visible() {
		t.Error("clearing the label should hide the button")
	}
}

func TestPickerIdleTeamsFollowTheFavoritesFilter(t *testing.T) {
	test.NewApp()
	games := []espn.Game{
		{ID: "1", Away: "TB", Home: "DAL", State: "pre"},
		{Away: "KC", State: core.IdleState},
		{Away: "CAR", State: core.IdleState},
	}
	awayNames := func(p *picker) string {
		var out []string
		for _, g := range p.games {
			out = append(out, strings.TrimSpace(g.Away))
		}
		return strings.Join(out, ",")
	}
	newP := func(favorites []string) *picker {
		return newPicker(context.Background(), fixtureFetcher{scoreboard(t, "scoreboard_empty.json")},
			map[string][]string{"nfl": favorites}, groupPrefs{}, "nfl", func(string, espn.Game) {})
	}

	p := newP([]string{"KC"})
	p.lastFetched = games // what ticking "All games" shows again
	p.show(games)
	if got := awayNames(p); got != "KC" {
		t.Errorf("favorite KC on a bye: %q, want just KC", got)
	}
	p.all.SetChecked(true)
	if got := awayNames(p); got != "TB,KC,CAR" {
		t.Errorf("All ticked: %q", got)
	}
	p = newP(nil)
	p.show(games)
	if got := awayNames(p); got != "TB,KC,CAR" {
		t.Errorf("no favorites: %q", got)
	}
	p = newP([]string{"DAL"})
	p.show(games)
	if got := awayNames(p); got != "TB" {
		t.Errorf("favorite plays: %q, want no byes", got)
	}
}
