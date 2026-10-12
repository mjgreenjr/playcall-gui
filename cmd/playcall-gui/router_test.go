package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"playcall/internal/core"
)

type countSink struct {
	n   int
	err error
}

func (c *countSink) Send(context.Context, core.Event) error { c.n++; return c.err }
func (c *countSink) Status(core.GameStatus)                 {}

func TestHyprVisible(t *testing.T) {
	clients := `[{"pid":1,"mapped":true,"hidden":false,"workspace":{"id":2,"name":"2"}},
{"pid":7,"mapped":true,"hidden":false,"workspace":{"id":3,"name":"3"}}]`
	monitors := `[{"activeWorkspace":{"id":1}},{"activeWorkspace":{"id":3}}]`
	run := func(c, m string) hyprOutput {
		return func(_ context.Context, args ...string) ([]byte, error) {
			if args[0] == "clients" {
				return []byte(c), nil
			}
			return []byte(m), nil
		}
	}
	ctx := context.Background()
	if v, err := hyprVisible(ctx, run(clients, monitors), 7); err != nil || !v {
		t.Errorf("active workspace: got %v, %v", v, err)
	}
	if v, err := hyprVisible(ctx, run(clients, monitors), 1); err != nil || v {
		t.Errorf("inactive workspace: got %v, %v", v, err)
	}
	if _, err := hyprVisible(ctx, run(clients, monitors), 99); err == nil {
		t.Error("unknown pid should be an error")
	}
	for name, c := range map[string]string{
		"unmapped": `[{"pid":7,"mapped":false,"hidden":false,"workspace":{"id":3,"name":"3"}}]`,
		"hidden":   `[{"pid":7,"mapped":true,"hidden":true,"workspace":{"id":3,"name":"3"}}]`,
		"special":  `[{"pid":7,"mapped":true,"hidden":false,"workspace":{"id":-98,"name":"special:scratch"}}]`,
	} {
		if v, err := hyprVisible(ctx, run(c, `[{"activeWorkspace":{"id":3}},{"activeWorkspace":{"id":-98}}]`), 7); err != nil || v {
			t.Errorf("%s: got %v, %v", name, v, err)
		}
	}
	bad := func(context.Context, ...string) ([]byte, error) { return nil, errors.New("no hyprctl") }
	if _, err := hyprVisible(ctx, bad, 7); err == nil {
		t.Error("missing hyprctl should be an error")
	}
}

func TestRouterSink(t *testing.T) {
	ev := core.Event{PlayID: "1"}
	cases := []struct {
		name      string
		vis       bool
		visErr    error
		toastErr  error
		wantToast int
		wantErr   bool
	}{
		{"visible", true, nil, nil, 0, false},
		{"hidden", false, nil, nil, 1, false},
		{"unknown", false, errors.New("x"), nil, 1, false},
		{"toast fails", false, nil, errors.New("boom"), 1, true},
	}
	// A revision never makes a toast, wherever the window is; a scoring update does.
	upd := routerSink{win: &countSink{}, toast: &countSink{}, Visible: func(context.Context) (bool, error) { return false, nil }}
	if err := upd.Send(context.Background(), core.Event{Kind: core.KindUpdate, Revision: true}); err != nil ||
		upd.toast.(*countSink).n != 0 || upd.win.(*countSink).n != 1 {
		t.Errorf("update: toasts=%d window=%d err=%v", upd.toast.(*countSink).n, upd.win.(*countSink).n, err)
	}
	scoring := routerSink{win: &countSink{}, toast: &countSink{}, Visible: func(context.Context) (bool, error) { return false, nil }}
	if err := scoring.Send(context.Background(), core.Event{Kind: core.KindUpdate}); err != nil || scoring.toast.(*countSink).n != 1 {
		t.Errorf("scoring update: toasts=%d err=%v", scoring.toast.(*countSink).n, err)
	}
	for _, c := range cases {
		toast := &countSink{err: c.toastErr}
		win := &countSink{}
		r := routerSink{win: win, toast: toast,
			Visible: func(context.Context) (bool, error) { return c.vis, c.visErr }}
		err := r.Send(context.Background(), ev)
		if (err != nil) != c.wantErr || toast.n != c.wantToast {
			t.Errorf("%s: err=%v toasts=%d", c.name, err, toast.n)
		}
		// The list gets the play unless the toast failed (retried next poll).
		if want := map[bool]int{true: 0, false: 1}[c.wantErr]; win.n != want {
			t.Errorf("%s: window got %d, want %d", c.name, win.n, want)
		}
	}
}

func TestGameVisibleNeedsGameScreen(t *testing.T) {
	s := &shell{}
	if v, err := s.gameVisible(context.Background()); v || err != nil {
		t.Errorf("game not shown: got %v, %v", v, err)
	}
}

func TestRouterWithNoNotifierUsesTheWindow(t *testing.T) {
	win, toast := &countSink{}, &countSink{err: fmt.Errorf("%w (nothing installed)", core.ErrNoNotifier)}
	var fallback int
	r := routerSink{win: win, toast: toast, noToast: new(sync.Once),
		Visible:  func(context.Context) (bool, error) { return false, nil },
		Fallback: func(core.Event) { fallback++ }}
	for i := 0; i < 3; i++ {
		if err := r.Send(context.Background(), core.Event{PlayID: "1"}); err != nil {
			t.Fatalf("a desktop with no way to toast must not fail the play: %v", err)
		}
	}
	if win.n != 3 || fallback != 3 {
		t.Errorf("window got %d, fallback %d; want 3 each", win.n, fallback)
	}
	// Without a Fallback the window alone has the play.
	r.Fallback = nil
	if err := r.Send(context.Background(), core.Event{PlayID: "2"}); err != nil || win.n != 4 {
		t.Errorf("no fallback: err=%v window=%d", err, win.n)
	}
}

func TestGameVisibleFallsBackToFocus(t *testing.T) {
	s := &shell{}
	s.gameShown.Store(true)
	s.visible = func(context.Context) (bool, error) { return false, errors.New("no hyprctl") }
	if v, err := s.gameVisible(context.Background()); v || err != nil {
		t.Errorf("unfocused, window manager unknown: got %v, %v", v, err)
	}
	s.focus.focused.Store(true)
	if v, err := s.gameVisible(context.Background()); !v || err != nil {
		t.Errorf("focused, window manager unknown: got %v, %v", v, err)
	}
	// When the window manager does answer, focus does not matter.
	s.visible = func(context.Context) (bool, error) { return false, nil }
	if v, _ := s.gameVisible(context.Background()); v {
		t.Error("hyprland says hidden; focus must not override it")
	}
	s.visible = func(context.Context) (bool, error) { return true, nil }
	s.focus.focused.Store(false)
	if v, _ := s.gameVisible(context.Background()); !v {
		t.Error("hyprland says visible; no focus must not hide it")
	}
}
