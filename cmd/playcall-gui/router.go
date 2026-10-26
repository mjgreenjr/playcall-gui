package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"playcall/internal/core"
)

// windowSink is what the router needs from the window side (guiSink).
type windowSink interface {
	core.Sink
	core.StatusSink
}

// routerSink sends each event to the window when it is on screen and to a
// system toast when it is not, never both. The window's play list is the
// game's record, so toasted plays are still added to it (quietly) once the
// toast went out; that keeps the list complete when the user comes back.
type routerSink struct {
	win   windowSink
	toast core.Sink
	// Visible reports whether our window is on screen. An error means "can't
	// tell", which routes to the toast.
	Visible func(ctx context.Context) (bool, error)
	// noToast makes the "no notifier" warning appear once, not per play.
	noToast *sync.Once
	// ended remembers the last closing toast of each game, so the second of
	// the End of Game row and the final event is not toasted again.
	ended *endedToasts
}

// endedToasts holds, per game, the scores of the last ending event toasted.
type endedToasts struct {
	mu     sync.Mutex
	scores map[string][2]string
}

func scoresOf(e core.Event) [2]string { return [2]string{e.Away.Score, e.Home.Score} }

// repeats reports whether e closes a game whose closing toast already went
// out with the same scores, so a second toast would say nothing new. The
// first of the two is kept, whichever ESPN delivers first.
func (t *endedToasts) repeats(e core.Event) bool {
	if t == nil || !e.Ending {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, ok := t.scores[e.GameID]
	return ok && prev == scoresOf(e)
}

func (t *endedToasts) record(e core.Event) {
	if t == nil || !e.Ending {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scores == nil {
		t.scores = map[string][2]string{}
	}
	t.scores[e.GameID] = scoresOf(e)
}

func (r routerSink) Send(ctx context.Context, e core.Event) error {
	// A corrected foul or substitution only fixes the row already in the list;
	// a toast is not a record, so there is nothing to correct there. A
	// corrected scoring play is news and is toasted again.
	if e.Revision {
		return r.win.Send(ctx, e)
	}
	vis, err := r.Visible(ctx)
	if err != nil {
		slog.Debug("window visibility unknown, using toast", "err", err)
	}
	if vis {
		return r.win.Send(ctx, e)
	}
	if r.ended.repeats(e) {
		slog.Debug("Skipping a repeated closing toast", "game", e.GameID, "title", e.Title)
		return r.win.Send(ctx, e)
	}
	// A toast failure is returned as is: the poller retries on the next poll
	// and nothing is added to the list until then. The exception is a desktop
	// with no way to toast at all: retrying never helps, so the play goes to
	// the window instead of being lost.
	if err := r.toast.Send(ctx, e); err != nil {
		if !errors.Is(err, core.ErrNoNotifier) {
			return err
		}
		if r.noToast != nil {
			r.noToast.Do(func() { slog.Warn("No notification program works; plays go to the window only", "err", err) })
		}
	}
	r.ended.record(e)
	return r.win.Send(ctx, e)
}

func (r routerSink) Status(s core.GameStatus) { r.win.Status(s) }

var _ core.Sink = routerSink{}
var _ core.StatusSink = routerSink{}
