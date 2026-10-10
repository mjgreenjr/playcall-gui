package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"playcall/internal/core"
	"playcall/internal/espn"
)

func TestHeaderShowsLogosAndTheBallAtTheOuterEdge(t *testing.T) {
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
	if u.homeBall.Translucency != 0 || u.awayBall.Translucency != 1 {
		t.Errorf("ball: home %v away %v (0 is visible)", u.homeBall.Translucency, u.awayBall.Translucency)
	}
	if u.homeBall.Resource != footballRes {
		t.Error("not a football")
	}

	// Possession flips: the ball moves to the other logo, nothing is downloaded again.
	u.setStatus(core.GameStatus{
		Sport: "cfb", State: "in", Possession: "away",
		Away: core.TeamScore{Abbr: "IOWA", Score: "13", Logo: srv.URL + "/iowa.png"},
		Home: core.TeamScore{Abbr: "WASH", Score: "0", Logo: srv.URL + "/wash.png"},
	})
	if u.awayBall.Translucency != 0 || u.homeBall.Translucency != 1 || hits.Load() != 2 {
		t.Errorf("after flip: away %v home %v, %d downloads", u.awayBall.Translucency, u.homeBall.Translucency, hits.Load())
	}

	// A sport with no ball never shows one.
	u.setStatus(core.GameStatus{Sport: "nhl", State: "in", Possession: "home",
		Away: core.TeamScore{Abbr: "A"}, Home: core.TeamScore{Abbr: "B"}})
	if u.homeBall.Translucency != 1 || u.awayBadge.logo.File != "" {
		t.Errorf("nhl: ball %v, logo %q", u.homeBall.Translucency, u.awayBadge.logo.File)
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

// absX is an object's x position on its window's canvas.
func absX(o fyne.CanvasObject) float32 {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(o).X
}

func TestHeaderOrderBallLogoNameOnBothSides(t *testing.T) {
	test.NewApp()
	u := newUI(10*time.Second, nil, highlights[0], nil)
	w := test.NewWindow(u.content)
	w.Resize(fyne.NewSize(600, 300))
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in", Clock: "1:00", Period: "1st", Possession: "away",
		Away: core.TeamScore{Abbr: "IOWA", Score: "13"}, Home: core.TeamScore{Abbr: "WASH", Score: "0"}})

	// Away: ball, then logo, then name, all left of centre.
	if !(absX(u.awayBall) < absX(u.awayBadge.logo) && absX(u.awayBadge.logo) < absX(u.away)) {
		t.Errorf("away order: ball %v logo %v name %v", absX(u.awayBall), absX(u.awayBadge.logo), absX(u.away))
	}
	// Home: logo, then name, then ball as the last thing on the line.
	if !(absX(u.homeBadge.logo) < absX(u.home) && absX(u.home) < absX(u.homeBall)) {
		t.Errorf("home order: logo %v name %v ball %v", absX(u.homeBadge.logo), absX(u.home), absX(u.homeBall))
	}
	if !(absX(u.away) < absX(u.statusText) && absX(u.statusText) < absX(u.homeBadge.logo)) {
		t.Errorf("clock is not between the teams: %v %v %v", absX(u.away), absX(u.statusText), absX(u.homeBadge.logo))
	}
	if u.awayBall.Translucency != 0 || u.homeBall.Translucency != 1 {
		t.Errorf("ball: away %v home %v", u.awayBall.Translucency, u.homeBall.Translucency)
	}
	// The ball's slot is reserved when it is hidden, so nothing moves when possession flips.
	before := absX(u.home)
	u.setStatus(core.GameStatus{Sport: "nfl", State: "in", Possession: "home",
		Away: core.TeamScore{Abbr: "IOWA", Score: "13"}, Home: core.TeamScore{Abbr: "WASH", Score: "0"}})
	if absX(u.home) != before {
		t.Errorf("home name moved from %v to %v when the ball arrived", before, absX(u.home))
	}
}

func TestPickerRowsAlignInColumns(t *testing.T) {
	test.NewApp()
	p := newPicker(context.Background(), &recordingFetcher{}, nil, groupPrefs{}, "nfl", func(string, espn.Game) {})
	p.games = []espn.Game{
		{ID: "1", Away: "TB ", Home: "DAL", State: "pre", Status: "10/11"},
		{ID: "2", Away: "UCONN", Home: "SJSU", State: "pre", Status: "10/11"},
		{ID: "3", Away: "A", Home: "WASH", State: "pre", Status: "10/12"},
	}
	p.abbrWidth = widestAbbreviation(p.games)
	if p.abbrWidth <= 0 {
		t.Fatal("no column width")
	}
	var rows []fyne.CanvasObject
	for i := range p.games {
		row := p.list.CreateItem()
		p.list.UpdateItem(i, row)
		rows = append(rows, row)
	}
	w := test.NewWindow(container.NewVBox(rows...))
	w.Resize(fyne.NewSize(700, 300))

	logo := func(row fyne.CanvasObject, i int) float32 {
		return absX(row.(*fyne.Container).Objects[1].(*fyne.Container).Objects[i])
	}
	for i := 1; i < len(rows); i++ {
		if logo(rows[i], 0) != logo(rows[0], 0) || logo(rows[i], 3) != logo(rows[0], 3) {
			t.Errorf("row %d logos at %v / %v, row 0 at %v / %v", i, logo(rows[i], 0), logo(rows[i], 3), logo(rows[0], 0), logo(rows[0], 3))
		}
	}
	if logo(rows[0], 3) <= logo(rows[0], 0) {
		t.Errorf("home logo %v is not right of away logo %v", logo(rows[0], 3), logo(rows[0], 0))
	}
}

func TestWidestAbbreviation(t *testing.T) {
	test.NewApp()
	short := widestAbbreviation([]espn.Game{{Away: "TB", Home: "KC"}})
	long := widestAbbreviation([]espn.Game{{Away: "TB", Home: "UCONN "}})
	if short <= 0 || long <= short {
		t.Errorf("short %v long %v", short, long)
	}
	if widestAbbreviation(nil) != 0 {
		t.Error("no games should need no width")
	}
}
