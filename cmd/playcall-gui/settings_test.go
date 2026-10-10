package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"playcall/internal/core"
	"playcall/internal/espn"
)

// teamFetcher serves the teams fixture for the teams endpoint and the
// standings fixture for standings, like ESPN does.
type teamFetcher struct {
	t    *testing.T
	fail bool
}

func (f teamFetcher) Fetch(_ context.Context, url string) (espn.Data, error) {
	if f.fail {
		return nil, errors.New("down")
	}
	name := "teams_nfl.json"
	if strings.Contains(url, "standings") {
		name = "standings_cfb_all.json"
	}
	b, err := os.ReadFile("../../test/fixtures/" + name)
	if err != nil {
		f.t.Fatal(err)
	}
	return espn.Decode(b)
}

type settingsHarness struct {
	cfg     core.Config
	updates int
	backs   int
	view    *settingsView
}

func newHarness(t *testing.T, cfg core.Config, sport string, fail bool) *settingsHarness {
	test.NewApp()
	h := &settingsHarness{cfg: cfg}
	h.view = newSettings(settingsEnv{
		ctx:     context.Background(),
		fetcher: teamFetcher{t: t, fail: fail},
		cfg:     func() core.Config { return h.cfg },
		update:  func(fn func(*core.Config)) { h.updates++; fn(&h.cfg) },
		back:    func() { h.backs++ },
	}, sport)
	return h
}

func TestSettingsListsTeamsAndTogglesFavorites(t *testing.T) {
	h := newHarness(t, core.DefaultConfig(), "nfl", false)
	v := h.view
	if len(v.rows) < 30 {
		t.Fatalf("%d teams", len(v.rows))
	}
	if v.count.Text != "0 favorite(s) for NFL Football" {
		t.Errorf("count %q", v.count.Text)
	}
	v.setFavorite("DAL", true)
	v.setFavorite("PIT", true)
	v.setFavorite("DAL", true) // already a favorite: nothing changes
	if got := h.cfg.FavoriteTeams["nfl"]; len(got) != 2 || got[0] != "DAL" || got[1] != "PIT" {
		t.Errorf("favorites %v", got)
	}
	if v.count.Text != "2 favorite(s) for NFL Football" {
		t.Errorf("count %q", v.count.Text)
	}
	v.setFavorite("DAL", false)
	if got := h.cfg.FavoriteTeams["nfl"]; len(got) != 1 || got[0] != "PIT" {
		t.Errorf("after removing: %v", got)
	}
}

func TestSettingsSearch(t *testing.T) {
	h := newHarness(t, core.DefaultConfig(), "nfl", false)
	v := h.view
	all := len(v.rows)
	v.search.SetText("cowboy")
	if len(v.rows) != 1 || v.rows[0].Abbr != "DAL" {
		t.Errorf("search by name: %+v", v.rows)
	}
	v.search.SetText("DAL")
	if len(v.rows) != 1 {
		t.Errorf("search by abbreviation: %+v", v.rows)
	}
	v.search.SetText("")
	if len(v.rows) != all {
		t.Errorf("cleared search: %d rows, want %d", len(v.rows), all)
	}
}

func TestSettingsKeepsFavoritesESPNDoesNotList(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.SetFavoriteTeams("nfl", []string{"ZZZ", "DAL"})
	h := newHarness(t, cfg, "nfl", false)
	v := h.view
	if !v.rows[0].Extra || v.rows[0].Abbr != "ZZZ" {
		t.Fatalf("first row %+v", v.rows[0])
	}
	if !strings.Contains(v.rows[0].label(), "not in ESPN's team list") {
		t.Errorf("label %q", v.rows[0].label())
	}
	v.setFavorite("ZZZ", false)
	v.applyFilter()
	for _, r := range v.rows {
		if r.Abbr == "ZZZ" {
			t.Error("removed favorite still listed")
		}
	}
}

func TestSettingsWhenTeamsCannotBeLoaded(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.SetFavoriteTeams("nfl", []string{"DAL"})
	h := newHarness(t, cfg, "nfl", true)
	v := h.view
	if !strings.Contains(v.status.Text, "Could not load the team list") {
		t.Errorf("status %q", v.status.Text)
	}
	if len(v.rows) != 1 || v.rows[0].Abbr != "DAL" || !v.rows[0].Extra {
		t.Errorf("rows %+v", v.rows)
	}
	v.setFavorite("DAL", false) // can still be removed
	if len(h.cfg.FavoriteTeams["nfl"]) != 0 {
		t.Errorf("favorites %v", h.cfg.FavoriteTeams["nfl"])
	}
}

func TestSettingsSportSwitchLoadsThatSportsFavoritesAndGroup(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.SetFavoriteTeams("nfl", []string{"DAL"})
	cfg.SetFavoriteGroup("cfb", "Big Ten")
	h := newHarness(t, cfg, "nfl", false)
	v := h.view
	updates := h.updates
	v.sport.SetSelected(sportLabel("cfb"))
	if v.group.Selected != "Big Ten" {
		t.Errorf("cfb default group %q", v.group.Selected)
	}
	if v.count.Text != "0 favorite(s) for FBS College Football" {
		t.Errorf("count %q", v.count.Text)
	}
	if h.updates != updates {
		t.Errorf("switching sport saved %d times", h.updates-updates)
	}
	v.sport.SetSelected(sportLabel("nfl"))
	if v.group.Selected != anyGroup || v.count.Text != "1 favorite(s) for NFL Football" {
		t.Errorf("back on nfl: %q / %q", v.group.Selected, v.count.Text)
	}
}

func TestSettingsDefaultGroupIsSaved(t *testing.T) {
	h := newHarness(t, core.DefaultConfig(), "nfl", false)
	v := h.view
	v.group.SetSelected("AFC North")
	if h.cfg.FavoriteGroup("nfl") != "AFC North" {
		t.Errorf("group %q", h.cfg.FavoriteGroup("nfl"))
	}
	v.group.SetSelected(anyGroup)
	if h.cfg.FavoriteGroup("nfl") != "" {
		t.Errorf("group %q after All", h.cfg.FavoriteGroup("nfl"))
	}
}

func TestSettingsGeneral(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Interval = 7 // hand-edited: not one of the usual choices
	cfg.SetHighlightColor("orange")
	h := newHarness(t, cfg, "nfl", false)
	v := h.view
	if v.interval.Selected != "7 seconds" || v.color.Selected != "Orange" {
		t.Errorf("shows %q / %q", v.interval.Selected, v.color.Selected)
	}
	found := false
	for _, o := range v.interval.Options {
		if o == "7 seconds" {
			found = true
		}
	}
	if !found {
		t.Errorf("options %v", v.interval.Options)
	}
	v.interval.SetSelected("30 seconds")
	if core.ResolveInterval(nil, h.cfg) != 30 {
		t.Errorf("interval %v", h.cfg.Interval)
	}
	v.color.SetSelected("Green")
	if h.cfg.HighlightColor() != "green" {
		t.Errorf("color %q", h.cfg.HighlightColor())
	}
}

func TestSettingsDoesNotSaveJustByOpening(t *testing.T) {
	h := newHarness(t, core.DefaultConfig(), "nfl", false)
	if h.updates != 0 {
		t.Errorf("opening saved %d times", h.updates)
	}
}

func TestFavoritesRowsShowNameAndLogoNotTheAbbreviation(t *testing.T) {
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	cfg := core.DefaultConfig()
	cfg.SetFavoriteTeams("nfl", []string{"ZZZ"})
	h := newHarness(t, cfg, "nfl", false)
	v := h.view
	v.env.logos = newLogoCache(t.TempDir(), inline)
	v.rows = []teamRow{
		{Abbr: "ZZZ", Extra: true},
		{Abbr: "DAL", Name: "Dallas Cowboys", Logo: srv.URL + "/dal.png"},
		{Abbr: "HOU", Name: "Houston Texans"}, // ESPN gave no logo
		{Abbr: "KC"},                          // ...and no name
	}
	row := v.list.CreateItem().(*fyne.Container)
	parts := func() (*widget.Check, *canvas.Image) {
		return row.Objects[0].(*widget.Check), row.Objects[1].(*canvas.Image)
	}

	v.list.UpdateItem(1, row)
	check, logo := parts()
	if check.Text != "Dallas Cowboys" {
		t.Errorf("name row shows %q (the abbreviation should be gone)", check.Text)
	}
	if logo.File == "" {
		t.Error("no logo shown")
	}
	first := logo.File

	// The same row object reused for a team with no logo must not keep Dallas's.
	v.list.UpdateItem(2, row)
	check, logo = parts()
	if check.Text != "Houston Texans" || logo.File != "" {
		t.Errorf("recycled row: %q, logo %q", check.Text, logo.File)
	}

	// An unknown favorite keeps its abbreviation, since it has no name.
	v.list.UpdateItem(0, row)
	if check, logo = parts(); check.Text != "ZZZ  (not in ESPN's team list)" || logo.File != "" || !check.Checked {
		t.Errorf("unknown favorite: %q checked %v logo %q", check.Text, check.Checked, logo.File)
	}
	// A team with no name falls back to its abbreviation.
	v.list.UpdateItem(3, row)
	if check, _ = parts(); check.Text != "KC" {
		t.Errorf("nameless team: %q", check.Text)
	}

	// Back to Dallas: from disk, no new download.
	before := hits.Load()
	v.list.UpdateItem(1, row)
	if _, logo = parts(); logo.File != first || hits.Load() != before {
		t.Errorf("returned row: %q, %d new downloads", logo.File, hits.Load()-before)
	}
}

func TestFavoritesStillSearchableByAbbreviation(t *testing.T) {
	h := newHarness(t, core.DefaultConfig(), "nfl", false)
	v := h.view
	v.search.SetText("DAL")
	if len(v.rows) != 1 || v.rows[0].Abbr != "DAL" || v.rows[0].label() != "Dallas Cowboys" {
		t.Errorf("rows %+v", v.rows)
	}
}
