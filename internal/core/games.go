package core

import (
	"context"
	"sort"
	"strings"
	"time"

	"playcall/internal/espn"
)

// TodaysGames is the sport's current scoreboard, earliest start first (games
// with the same start keep ESPN's order). No games is an empty list, not an
// error; an error means the scoreboard could not be fetched.
func TodaysGames(ctx context.Context, f espn.Fetcher, sport string, now time.Time) ([]espn.Game, error) {
	data, err := espn.FetchScoreboard(ctx, f, sport, "", true, now)
	if err != nil {
		return nil, err
	}
	var games []espn.Game
	for _, ev := range espn.List(data["events"]) {
		if g, ok := espn.ParseScoreboardEvent(espn.Map(ev)); ok {
			games = append(games, g)
		}
	}
	sort.SliceStable(games, func(i, j int) bool { return games[i].Date < games[j].Date })
	return games, nil
}

// FavoriteGames keeps the games with a favorite team in them. No favorites
// keeps everything, like `list`.
func FavoriteGames(games []espn.Game, favorites []string) []espn.Game {
	if len(favorites) == 0 {
		return games
	}
	fav := map[string]bool{}
	for _, t := range favorites {
		fav[t] = true
	}
	var kept []espn.Game
	for _, g := range games {
		if fav[strings.TrimSpace(g.Away)] || fav[strings.TrimSpace(g.Home)] {
			kept = append(kept, g)
		}
	}
	return kept
}
