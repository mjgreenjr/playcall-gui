package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"playcall/internal/core"
)

// followFeed follows a game until it ends or the user stops it, printing each
// play and each change of the header as one line: to stdout, or appended to
// the --out file (made if missing, never truncated, so one file can hold
// several games). It sends no toasts and writes no state file; the log stays
// in its own file, so stdout is only the feed.
func (c *command) followFeed(ctx context.Context, target string) int {
	gameID := target
	if !isDigits(target) {
		g, err := core.FindGameForTeam(ctx, c.Source, c.opts.sport, strings.ToUpper(target), c.Now())
		if err != nil {
			return fail(err)
		}
		gameID = g.ID
		// stderr, so the feed on stdout holds nothing but the game.
		fmt.Fprintf(c.Stderr, "Found game %s: %s @ %s (%s)\n", g.ID, g.Away, g.Home, g.Status)
	}
	fetch, err := c.fetcher(gameID)
	if err != nil {
		return c.errorf("%v", err)
	}
	cfg, err := core.LoadConfig(c.paths)
	if err != nil {
		return c.errorf("%v", err)
	}
	interval := core.ResolveInterval(c.opts.interval, cfg)

	var w io.Writer = c.Stdout
	if c.opts.out != "" {
		f, err := os.OpenFile(c.opts.out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return c.errorf("cannot open %s: %v", c.opts.out, err)
		}
		defer f.Close()
		w = f
	}
	sink := &core.FeedSink{W: w, Now: c.Now}

	state := &core.State{GameID: gameID, Sport: c.opts.sport, StartedAt: float64(c.Now().UnixNano()) / 1e9}
	poller, err := core.NewPoller(state, sink, fetch)
	if err != nil {
		return c.errorf("%v", err)
	}
	poller.StatusSink = sink
	poller.Now = c.Now
	sink.Note(fmt.Sprintf("following game %s (%s)", gameID, c.opts.sport))

	loop := &core.Loop{
		Poll:     poller.Poll,
		GameID:   gameID,
		Interval: time.Duration(interval) * time.Second,
		Sleep:    c.Sleep,
	}
	// Run returns nil when the game ends or ctx is cancelled.
	loop.Run(ctx)
	if err := sink.Err(); err != nil {
		return c.errorf("writing the feed: %v", err)
	}
	return exitOK
}
