package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"playcall/internal/espn"
)

type standingsFetcher struct {
	calls int
	fail  bool
	rec   string
}

func (f *standingsFetcher) Fetch(ctx context.Context, url string) (espn.Data, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("down")
	}
	return espn.Decode([]byte(`{"children":[{"children":[{"standings":{"entries":[
		{"team":{"abbreviation":"BUF"},"stats":[{"type":"total","summary":"3-1"},{"type":"vsdiv","summary":"` + f.rec + `"}]},
		{"team":{"abbreviation":"NYJ"},"stats":[{"type":"vsdiv","displayValue":"1-0"}]}]}}]}]}`))
}

func TestDivisionCacheOncePerDay(t *testing.T) {
	f := &standingsFetcher{rec: "0-1"}
	dir := t.TempDir()
	c := NewDivisionCache(dir, f)
	day1 := time.Date(2026, 10, 10, 9, 0, 0, 0, time.Local)
	ctx := context.Background()

	got := c.Load(ctx, "nfl", day1)
	if got["BUF"] != "0-1" || got["NYJ"] != "1-0" {
		t.Fatalf("records: %v", got)
	}
	c.Load(ctx, "nfl", day1.Add(5*time.Hour))
	if f.calls != 1 {
		t.Errorf("fetched %d times on one day, want 1", f.calls)
	}
	if c.Cached("nfl", "BUF") != "0-1" {
		t.Errorf("Cached: %q", c.Cached("nfl", "BUF"))
	}

	// A new process the same day reads the file instead of fetching.
	c2 := NewDivisionCache(dir, f)
	if c2.Load(ctx, "nfl", day1)["BUF"] != "0-1" || f.calls != 1 {
		t.Errorf("file not used: calls %d", f.calls)
	}

	// The end of a game forces a fetch; the next day does too.
	f.rec = "1-1"
	if c.Refresh(ctx, "nfl", day1)["BUF"] != "1-1" || f.calls != 2 {
		t.Errorf("Refresh: calls %d", f.calls)
	}
	f.rec = "2-1"
	if c.Load(ctx, "nfl", day1.AddDate(0, 0, 1))["BUF"] != "2-1" || f.calls != 3 {
		t.Errorf("next day: calls %d", f.calls)
	}

	// A failed fetch keeps what was saved.
	f.fail = true
	if got := c.Refresh(ctx, "nfl", day1.AddDate(0, 0, 2)); got["BUF"] != "2-1" {
		t.Errorf("after a failed fetch: %v", got)
	}
}

func TestDivisionCacheSportWithoutDivisions(t *testing.T) {
	f := &standingsFetcher{}
	c := NewDivisionCache(t.TempDir(), f)
	for _, sport := range []string{"cfb", "cbb"} {
		if got := c.Load(context.Background(), sport, time.Now()); got != nil || f.calls != 0 {
			t.Errorf("%s: %v, %d fetches", sport, got, f.calls)
		}
	}
}

func TestAddDivisionRecords(t *testing.T) {
	games := []espn.Game{
		{Away: "BUF ", Home: "NYJ"},
		{Away: "IU", Home: "NEB", AwayConf: "2-0"}, // ESPN's own conference record stays
	}
	AddDivisionRecords(games, map[string]string{"BUF": "0-1", "NYJ": "1-0", "IU": "9-9"})
	if games[0].AwayConf != "0-1" || games[0].HomeConf != "1-0" {
		t.Errorf("game 0: %+v", games[0])
	}
	if games[1].AwayConf != "2-0" {
		t.Errorf("game 1 overwritten: %+v", games[1])
	}
}
