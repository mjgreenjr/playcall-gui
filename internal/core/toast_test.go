package core

import (
	"context"
	"errors"
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
		e := Event{Title: "Bills 17, Jets 14", Body: "Mahomes 45yd TD", Urgent: c.urgent, URL: url}
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
