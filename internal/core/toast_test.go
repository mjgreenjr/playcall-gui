package core

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

type recordRunner struct {
	name     string
	args     []string
	deadline time.Time
	err      error
}

func (r *recordRunner) Run(ctx context.Context, name string, args ...string) error {
	r.name, r.args = name, args
	r.deadline, _ = ctx.Deadline()
	return r.err
}

func TestToastSinkArgs(t *testing.T) {
	url := "https://www.espn.com/nfl/game/_/gameId/401872925"
	for _, c := range []struct {
		urgent  bool
		urgency string
	}{{true, "normal"}, {false, "low"}} {
		r := &recordRunner{}
		e := Event{Sport: "nfl", Title: "Bills 17, Jets 14", Body: "Mahomes 45yd TD", Urgent: c.urgent, URL: url}
		if err := (ToastSink{Runner: r}).Send(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		want := []string{"-u", c.urgency, "-t", "15000", "Bills 17, Jets 14", "Mahomes 45yd TD", "--exec", "xdg-open", url}
		if r.name != "omarchy-notification-send" || !reflect.DeepEqual(r.args, want) {
			t.Errorf("urgent=%v: ran %s %q, want %q", c.urgent, r.name, r.args, want)
		}
		if got := time.Until(r.deadline); got <= 0 || got > 30*time.Second {
			t.Errorf("timeout is %v, want at most 30s", got)
		}
	}
}

func TestToastSinkFailureIsReturned(t *testing.T) {
	boom := errors.New("not found")
	err := ToastSink{Runner: &recordRunner{err: boom}}.Send(context.Background(), Event{Title: "t", URL: "u"})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestExecRunnerMissingProgram(t *testing.T) {
	if err := (ExecRunner{}).Run(context.Background(), "playcall-no-such-program"); err == nil {
		t.Error("running a missing program succeeded")
	}
}

func TestMarkedTitle(t *testing.T) {
	e := Event{Sport: "nfl", Title: "ATL 14 · 0:48 1st · NO 0", Home: TeamScore{Abbr: "NO", Score: "0"}}
	cases := []struct{ poss, title, want string }{
		{"", e.Title, e.Title},
		{"away", e.Title, "🏈 ATL 14 · 0:48 1st · NO 0"},
		{"home", e.Title, "ATL 14 · 0:48 1st · NO 0 🏈"},
		{"home", "ATL 14 · 0:48 1st · NO 0 (ATL 2-1)", "ATL 14 · 0:48 1st · NO 0 🏈 (ATL 2-1)"},
		{"home", "ATL 14 · 0:48 1st · NO 3 PP", "ATL 14 · 0:48 1st · NO 3 PP 🏈"},
		{"home", "garbled", "garbled"},
	}
	for _, c := range cases {
		ev := e
		ev.Possession, ev.Title = c.poss, c.title
		if c.title == "ATL 14 · 0:48 1st · NO 3 PP" {
			ev.Home.Score = "3"
		}
		if got := ev.MarkedTitle(); got != c.want {
			t.Errorf("%q %q: got %q, want %q", c.poss, c.title, got, c.want)
		}
	}
}

func TestMarkedTitleBalls(t *testing.T) {
	for sport, ball := range map[string]string{"cfb": "🏈", "wnba": "🏀", "mlb": "⚾", "nhl": "•"} {
		e := Event{Sport: sport, Possession: "away", Title: "A 1 · x · B 2"}
		if got := e.MarkedTitle(); got != ball+" A 1 · x · B 2" {
			t.Errorf("%s: %q", sport, got)
		}
	}
}

// scriptRunner fails the programs named in errs and records every run.
type scriptRunner struct {
	errs map[string]error
	ran  []string
	args map[string][]string
}

func (r *scriptRunner) Run(_ context.Context, name string, args ...string) error {
	r.ran = append(r.ran, name)
	if r.args == nil {
		r.args = map[string][]string{}
	}
	r.args[name] = args
	return r.errs[name]
}

func TestToastFallsBackToNotifySend(t *testing.T) {
	missing := fmt.Errorf("exec: %q: %w", NotifCmd, exec.ErrNotFound)
	r := &scriptRunner{errs: map[string]error{NotifCmd: missing}}
	e := Event{Title: "BAL 21 · 0:40 4th · ATL 23", Body: "Pass complete", Urgent: true, URL: "https://x/g"}
	if err := (ToastSink{Runner: r}).Send(context.Background(), e); err != nil {
		t.Fatalf("the fallback should deliver: %v", err)
	}
	if !reflect.DeepEqual(r.ran, []string{NotifCmd, FallbackNotifCmd}) {
		t.Errorf("ran %v", r.ran)
	}
	want := []string{"-u", "normal", "-t", "15000", "-a", "playcall", e.Title, "Pass complete"}
	if got := r.args[FallbackNotifCmd]; !reflect.DeepEqual(got, want) {
		t.Errorf("notify-send args %q, want %q", got, want)
	}
}

func TestToastWithNoNotifierAtAll(t *testing.T) {
	missing := fmt.Errorf("exec: %w", exec.ErrNotFound)
	for name, errs := range map[string]map[string]error{
		"neither installed":     {NotifCmd: missing, FallbackNotifCmd: missing},
		"notify-send can't run": {NotifCmd: missing, FallbackNotifCmd: errors.New("exit status 1")},
	} {
		r := &scriptRunner{errs: errs}
		err := ToastSink{Runner: r}.Send(context.Background(), Event{Title: "t", URL: "u"})
		if !errors.Is(err, ErrNoNotifier) {
			t.Errorf("%s: err = %v, want ErrNoNotifier", name, err)
		}
	}
}

func TestToastOmarchyFailureDoesNotFallBack(t *testing.T) {
	// The program exists but failed: retry it next poll; notify-send is only
	// for a desktop that does not have it.
	boom := errors.New("exit status 1")
	r := &scriptRunner{errs: map[string]error{NotifCmd: boom}}
	err := ToastSink{Runner: r}.Send(context.Background(), Event{Title: "t", URL: "u"})
	if !errors.Is(err, boom) || errors.Is(err, ErrNoNotifier) || len(r.ran) != 1 {
		t.Errorf("err = %v, ran %v", err, r.ran)
	}
}

func TestExecRunnerMissingProgramIsNotFound(t *testing.T) {
	err := (ExecRunner{}).Run(context.Background(), "playcall-no-such-program")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v; the fallback relies on errors.Is(err, exec.ErrNotFound)", err)
	}
}
