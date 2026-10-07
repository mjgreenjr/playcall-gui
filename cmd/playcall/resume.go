package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"playcall/internal/core"
	"playcall/internal/sports"
)

// resume re-enters the poll loop for the game in state.json. The sport and
// game come from the state; --sport is ignored. It exits 0 when it stops
// (game over, no state, or cancelled) so a supervisor never restarts it in a
// loop.
func (c *command) resume(ctx context.Context) int {
	store := core.NewStateStore(c.paths)
	state, err := store.Load()
	if err != nil {
		return fail(fmt.Errorf("Could not read state: %w", err))
	}
	if state == nil {
		slog.Warn("No state to resume; nothing to do")
		return exitOK
	}
	// The sport is read from the file, so check it before any request.
	if err := sports.Validate(state.Sport); err != nil {
		return fail(fmt.Errorf("Cannot resume game %s: %w", state.GameID, err))
	}
	c.opts.sport = state.Sport
	fetch, err := c.fetcher(state.GameID)
	if err != nil {
		return c.errorf("%v", err)
	}
	poller, err := core.NewPoller(state, c.sink(), fetch)
	if err != nil {
		return fail(fmt.Errorf("Cannot resume game %s: %w", state.GameID, err))
	}
	poller.Now = c.Now
	if c.opts.dryRun {
		poller.Trace = c.Stdout
	}

	if c.opts.once {
		// A standalone check: no log line, and state is never written or cleared.
		poller.Once = true
		if _, err := poller.Poll(ctx); err != nil {
			return c.errorf("%v", err)
		}
		return exitOK
	}

	cfg, err := core.LoadConfig(c.paths)
	if err != nil {
		return c.errorf("%v", err)
	}
	interval := core.ResolveInterval(c.opts.interval, cfg)
	poller.Store = store
	slog.Info(fmt.Sprintf("Service started: resuming game %s", state.GameID))
	loop := &core.Loop{
		Poll:       poller.Poll,
		GameID:     state.GameID,
		Interval:   time.Duration(interval) * time.Second,
		Sleep:      c.Sleep,
		OnFinished: func() { finishGame(store) },
	}
	// Run returns nil when the game ends or ctx is cancelled; a cancelled
	// context leaves state.json alone so the next resume carries on.
	loop.Run(ctx)
	return exitOK
}

// finishGame clears the state once the game is final.
//
// Slice 7b adds the service step before the clear: run
// `systemctl --user disable playcall.service` (only if the unit file exists)
// and, if disabling fails, log the error and return with the state kept, so a
// service never restarts with nothing to resume.
func finishGame(store *core.StateStore) {
	if err := store.Clear(); err != nil {
		slog.Error(fmt.Sprintf("Failed to clear state: %v", err))
	}
}
