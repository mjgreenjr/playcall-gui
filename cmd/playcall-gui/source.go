package main

import (
	"context"
	"fmt"
	"sync"

	"playcall/internal/espn"
)

type fetchFunc = func(context.Context) (espn.Data, error)

// liveFetcher reads a game's play-by-play from ESPN.
func liveFetcher(sport, gameID string) fetchFunc {
	var client espn.Client
	url := espn.SummaryURL(sport, gameID)
	return func(ctx context.Context) (espn.Data, error) { return client.Fetch(ctx, url) }
}

// mockFetcher replays a finished feed from a file as if it were live: each
// fetch reveals step more plays, and the game turns final once all are out.
func mockFetcher(path string, step int) (fetchFunc, error) {
	data, err := espn.ReadMock(path)
	if err != nil {
		return nil, err
	}
	total := len(espn.ExtractPlays(data))
	if total == 0 {
		return nil, fmt.Errorf("%s has no plays to replay", path)
	}
	if step < 1 {
		step = 1
	}
	var mu sync.Mutex
	shown := 0
	return func(context.Context) (espn.Data, error) {
		mu.Lock()
		shown = min(shown+step, total)
		k := shown
		mu.Unlock()
		return espn.Reveal(data, k, k == total)
	}, nil
}
