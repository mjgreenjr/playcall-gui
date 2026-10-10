package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"playcall/internal/espn"
)

type fixedFetcher struct {
	data espn.Data
	err  error
}

func (f fixedFetcher) Fetch(context.Context, string) (espn.Data, error) { return f.data, f.err }

func TestTodaysGamesSortedByStart(t *testing.T) {
	data := loadFixture(t, "scoreboard_nfl_week.json")
	games, err := TodaysGames(context.Background(), fixedFetcher{data: data}, "nfl", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 15 {
		t.Fatalf("%d games, want 15", len(games))
	}
	for i := 1; i < len(games); i++ {
		if games[i].Date < games[i-1].Date {
			t.Fatalf("not sorted at %d: %s before %s", i, games[i-1].Date, games[i].Date)
		}
	}
}

func TestTodaysGamesEmptyIsNotAnError(t *testing.T) {
	games, err := TodaysGames(context.Background(), fixedFetcher{data: loadFixture(t, "scoreboard_empty.json")}, "nfl", "", time.Now())
	if err != nil || len(games) != 0 {
		t.Errorf("games %v, err %v", games, err)
	}
}

func TestTodaysGamesFetchFailure(t *testing.T) {
	_, err := TodaysGames(context.Background(), fixedFetcher{err: errors.New("down")}, "nfl", "", time.Now())
	if !errors.Is(err, espn.ErrScoreboardFetch) {
		t.Errorf("err %v", err)
	}
}

func TestFavoriteGames(t *testing.T) {
	games := []espn.Game{{ID: "1", Away: "TB ", Home: "DAL"}, {ID: "2", Away: "NYJ", Home: "BUF"}}
	if got := FavoriteGames(games, []string{"DAL"}); len(got) != 1 || got[0].ID != "1" {
		t.Errorf("favorite DAL: %v", got)
	}
	if got := FavoriteGames(games, []string{"TB"}); len(got) != 1 || got[0].ID != "1" {
		t.Errorf("padded abbreviation: %v", got)
	}
	if got := FavoriteGames(games, nil); len(got) != 2 {
		t.Errorf("no favorites: %v", got)
	}
	if got := FavoriteGames(games, []string{"KC"}); len(got) != 0 {
		t.Errorf("no match: %v", got)
	}
}

type urlRecorder struct {
	data espn.Data
	urls []string
}

func (r *urlRecorder) Fetch(_ context.Context, url string) (espn.Data, error) {
	r.urls = append(r.urls, url)
	return r.data, nil
}

func TestTodaysGamesSendsTheGroup(t *testing.T) {
	r := &urlRecorder{data: loadFixture(t, "scoreboard_nfl_week.json")}
	if _, err := TodaysGames(context.Background(), r, "nfl", "8", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(r.urls) != 1 || !strings.Contains(r.urls[0], "groups=8") {
		t.Errorf("urls %v", r.urls)
	}
	r.urls = nil
	if _, err := TodaysGames(context.Background(), r, "cfb", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(r.urls) != 1 || !strings.Contains(r.urls[0], "groups=80") {
		t.Errorf("cfb default group not sent: %v", r.urls)
	}
}
