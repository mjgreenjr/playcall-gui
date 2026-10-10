package main

import (
	"context"
	"errors"
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
	// An update never makes a toast, wherever the window is.
	upd := routerSink{win: &countSink{}, toast: &countSink{}, Visible: func(context.Context) (bool, error) { return false, nil }}
	if err := upd.Send(context.Background(), core.Event{Kind: core.KindUpdate}); err != nil ||
		upd.toast.(*countSink).n != 0 || upd.win.(*countSink).n != 1 {
		t.Errorf("update: toasts=%d window=%d err=%v", upd.toast.(*countSink).n, upd.win.(*countSink).n, err)
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
