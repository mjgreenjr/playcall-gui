package main

import (
	"context"
	"log/slog"

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
	// tell" (no hyprctl, not Hyprland), which routes to the toast.
	Visible func(ctx context.Context) (bool, error)
}

func (r routerSink) Send(ctx context.Context, e core.Event) error {
	vis, err := r.Visible(ctx)
	if err != nil {
		slog.Debug("window visibility unknown, using toast", "err", err)
	}
	if vis {
		return r.win.Send(ctx, e)
	}
	// A toast failure is returned as is: the poller retries on the next poll
	// and nothing is added to the list until then.
	if err := r.toast.Send(ctx, e); err != nil {
		return err
	}
	return r.win.Send(ctx, e)
}

func (r routerSink) Status(s core.GameStatus) { r.win.Status(s) }

var _ core.Sink = routerSink{}
var _ core.StatusSink = routerSink{}
