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
	// Fallback shows an event another way when no notification program
	// works (core.ErrNoNotifier); nil means the window alone has it.
	Fallback func(core.Event)
	// noToast makes the "no notifier" warning appear once, not per play.
	noToast *sync.Once
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
	// A toast failure is returned as is: the poller retries on the next poll
	// and nothing is added to the list until then. The exception is a desktop
	// with no way to toast at all: retrying never helps, so the play goes to
	// the window (and the Fallback, if there is one) instead of being lost.
	if err := r.toast.Send(ctx, e); err != nil {
		if !errors.Is(err, core.ErrNoNotifier) {
			return err
		}
		if r.noToast != nil {
			r.noToast.Do(func() { slog.Warn("No notification program works; plays go to the window only", "err", err) })
		}
		if r.Fallback != nil {
			r.Fallback(e)
		}
	}
	return r.win.Send(ctx, e)
}

func (r routerSink) Status(s core.GameStatus) { r.win.Status(s) }

var _ core.Sink = routerSink{}
var _ core.StatusSink = routerSink{}
