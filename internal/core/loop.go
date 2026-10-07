package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	// PregameLead is how long before the scheduled start a pregame sleep ends.
	PregameLead = 300 * time.Second
	// PregameMaxSleep is the longest single sleep while waiting for the start.
	PregameMaxSleep = 1800 * time.Second
	// MaxBackoff caps the wait after failed fetches.
	MaxBackoff = 300 * time.Second
)

// Loop polls one game until it finishes or its context is cancelled.
type Loop struct {
	// Poll runs one poll; usually Poller.Poll.
	Poll func(ctx context.Context) (Outcome, error)
	// GameID is for log messages.
	GameID string
	// Interval is the poll interval, already resolved with ResolveInterval.
	Interval time.Duration
	// Sleep waits for d or until ctx is done, returning ctx's error then. Nil
	// means a real timer; tests inject one that does not wait.
	Sleep func(ctx context.Context, d time.Duration) error
	// OnFinished is called once the game is final and everything is sent. The
	// CLI wires service shutdown and state clearing to it.
	OnFinished func()
}

// Run polls until the game finishes (OnFinished is called and Run returns
// nil) or ctx is cancelled (Run returns nil too: stopping is not an error).
func (l *Loop) Run(ctx context.Context) error {
	sleep := l.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	failures := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		out, err := l.pollSafely(ctx)
		if err != nil {
			// Keep going; the next poll retries from the saved state.
			slog.Error("Unexpected error polling game", "game", l.GameID, "err", err)
			out = Outcome{Status: FetchFailed}
		}
		if ctx.Err() != nil {
			return nil
		}
		if out.Status == Finished {
			slog.Info("Game has finished. Stopping polling.", "game", l.GameID)
			if l.OnFinished != nil {
				l.OnFinished()
			}
			return nil
		}
		var delay time.Duration
		if out.Status == FetchFailed {
			failures++
			delay = Backoff(l.Interval, failures)
			slog.Debug("Retrying after failed fetch", "delay", delay, "failures", failures)
		} else {
			failures = 0
			delay = l.Interval
			if out.UntilStart != nil {
				// Not started yet: sleep until shortly before the start, in capped chunks.
				delay = PregameDelay(l.Interval, *out.UntilStart)
				slog.Debug("Game not started; sleeping", "game", l.GameID, "delay", delay)
			}
		}
		if err := sleep(ctx, delay); err != nil {
			return nil
		}
	}
}

// pollSafely turns a panic in one poll into an error.
func (l *Loop) pollSafely(ctx context.Context) (out Outcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return l.Poll(ctx)
}

// Backoff is the wait after the given number of failed fetches in a row:
// interval * 2^failures, capped at MaxBackoff.
func Backoff(interval time.Duration, failures int) time.Duration {
	if failures >= 16 || interval <= 0 {
		return MaxBackoff
	}
	d := interval << failures
	if d > MaxBackoff || d <= 0 {
		return MaxBackoff
	}
	return d
}

// PregameDelay is the sleep before a game that starts in until: up to
// PregameLead before the start, at most PregameMaxSleep, never less than the
// interval.
func PregameDelay(interval, until time.Duration) time.Duration {
	wait := until - PregameLead
	if wait > PregameMaxSleep {
		wait = PregameMaxSleep
	}
	if wait < interval {
		return interval
	}
	return wait
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
