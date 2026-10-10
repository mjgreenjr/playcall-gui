package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"playcall/internal/core"
	"playcall/internal/espn"
)

func TestHeaderShowsLogosAndPutsTheBallOnTheLogo(t *testing.T) {
	test.NewApp()
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	cache := newLogoCache(t.TempDir(), inline)
	u := newUI(10*time.Second, nil, highlights[0], cache)

	u.setStatus(core.GameStatus{
		Sport: "cfb", State: "in", Clock: "5:00", Period: "2nd", Possession: "home",
		Away: core.TeamScore{Abbr: "IOWA", Score: "13", Logo: srv.URL + "/iowa.png"},
		Home: core.TeamScore{Abbr: "WASH", Score: "0", Logo: srv.URL + "/wash.png"},
	})
	if u.awayBadge.logo.File == "" || u.homeBadge.logo.File == "" || u.awayBadge.logo.File == u.homeBadge.logo.File {
		t.Errorf("logo files %q / %q", u.awayBadge.logo.File, u.homeBadge.logo.File)
	}
	if u.homeBadge.ball.Translucency != 0 || u.awayBadge.ball.Translucency != 1 {
		t.Errorf("ball: home %v away %v (0 is visible)", u.homeBadge.ball.Translucency, u.awayBadge.ball.Translucency)
	}
	if u.homeBadge.ball.Resource != footballRes {
		t.Error("not a football")
	}

	// Possession flips: the ball moves to the other logo, nothing is downloaded again.
	u.setStatus(core.GameStatus{
		Sport: "cfb", State: "in", Possession: "away",
		Away: core.TeamScore{Abbr: "IOWA", Score: "13", Logo: srv.URL + "/iowa.png"},
		Home: core.TeamScore{Abbr: "WASH", Score: "0", Logo: srv.URL + "/wash.png"},
	})
	if u.awayBadge.ball.Translucency != 0 || u.homeBadge.ball.Translucency != 1 || hits.Load() != 2 {
		t.Errorf("after flip: away %v home %v, %d downloads", u.awayBadge.ball.Translucency, u.homeBadge.ball.Translucency, hits.Load())
	}

	// A sport with no ball never shows one.
	u.setStatus(core.GameStatus{Sport: "nhl", State: "in", Possession: "home",
		Away: core.TeamScore{Abbr: "A"}, Home: core.TeamScore{Abbr: "B"}})
	if u.homeBadge.ball.Translucency != 1 || u.awayBadge.logo.File != "" {
		t.Errorf("nhl: ball %v, logo %q", u.homeBadge.ball.Translucency, u.awayBadge.logo.File)
	}
}

func TestBadgeIgnoresALateLogoForAnotherTeam(t *testing.T) {
	test.NewApp()
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	// Hold the first download until the badge has moved on to another team.
	release := make(chan struct{})
	old := runAsync
	runAsync = func(f func()) { go func() { <-release; f() }() }
	defer func() { runAsync = old }()
	cache := newLogoCache(t.TempDir(), func(f func()) { f() })

	b := newBadge()
	b.setLogo(cache, srv.URL+"/old.png")
	b.setLogo(cache, "") // the game changed; no logo for the new team
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if b.logo.File != "" {
		t.Errorf("a late download for the old team landed: %q", b.logo.File)
	}
}

func TestPickerRowsShowLogosAndStayCorrectWhenRecycled(t *testing.T) {
	test.NewApp()
	var hits atomic.Int32
	srv := logoServer(t, &hits, http.StatusOK)
	p := newPicker(context.Background(), &recordingFetcher{}, nil, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.logos = newLogoCache(t.TempDir(), inline)
	p.games = []espn.Game{
		{ID: "1", Away: "TB ", Home: "DAL", State: "pre", Status: "10/11", AwayLogo: srv.URL + "/tb.png", HomeLogo: srv.URL + "/dal.png"},
		{ID: "2", Away: "NYJ", Home: "BUF", State: "pre", Status: "10/12", AwayLogo: srv.URL + "/nyj.png"}, // home has none
	}
	row := p.list.CreateItem()
	parts := func() (away, home *canvas.Image) {
		teams := row.(*fyne.Container).Objects[1].(*fyne.Container).Objects
		return teams[0].(*canvas.Image), teams[3].(*canvas.Image)
	}

	p.list.UpdateItem(0, row)
	away, home := parts()
	if away.File == "" || home.File == "" || away.File == home.File {
		t.Fatalf("row 0 logos %q / %q", away.File, home.File)
	}
	first := away.File

	// The same row object is reused for another game: it must not keep game 0's logos.
	p.list.UpdateItem(1, row)
	away, home = parts()
	if away.File == first || away.File == "" || home.File != "" {
		t.Errorf("row 1 logos %q / %q (game 0 was %q)", away.File, home.File, first)
	}

	// Back to game 0: served from disk, no extra downloads.
	before := hits.Load()
	p.list.UpdateItem(0, row)
	if away, _ = parts(); away.File != first || hits.Load() != before {
		t.Errorf("returned row: %q, %d new downloads", away.File, hits.Load()-before)
	}
}

func TestPickerWithoutACacheStillDrawsRows(t *testing.T) {
	test.NewApp()
	p := newPicker(context.Background(), &recordingFetcher{}, nil, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.games = []espn.Game{{ID: "1", Away: "TB", Home: "DAL", State: "pre", Status: "10/11", AwayLogo: "https://x/y.png"}}
	row := p.list.CreateItem()
	p.list.UpdateItem(0, row) // must not panic with p.logos == nil
}
