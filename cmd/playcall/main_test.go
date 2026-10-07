package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"playcall/internal/core"
	"playcall/internal/espn"
)

// fakeSource serves ESPN data from a URL -> JSON table and records requests.
// Unknown URLs fail like a failed fetch. Mock files are read from disk.
type fakeSource struct {
	data     map[string]string // url -> JSON body
	requests []string          // "GET url" and "mock path", in order
}

func (f *fakeSource) Fetch(_ context.Context, url string) (espn.Data, error) {
	f.requests = append(f.requests, "GET "+url)
	body, ok := f.data[url]
	if !ok {
		return nil, errors.New("no data")
	}
	return espn.Decode([]byte(body))
}

// ReadMock records the path as the transcripts write it (relative to the repo root).
func (f *fakeSource) ReadMock(path string) (espn.Data, error) {
	f.requests = append(f.requests, "mock "+strings.TrimPrefix(path, "../../"))
	return espn.ReadMock(path)
}

// fakeRunner records the programs it was asked to run.
type fakeRunner struct {
	calls [][]string // name followed by args
	err   error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.err
}

// logRecords is a slog handler that keeps "LEVEL message" for INFO and above.
type logRecords struct{ lines *[]string }

func (h logRecords) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }
func (h logRecords) Handle(_ context.Context, r slog.Record) error {
	*h.lines = append(*h.lines, r.Level.String()+" "+r.Message)
	return nil
}
func (h logRecords) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h logRecords) WithGroup(string) slog.Handler      { return h }

// fixedNow is the frozen clock: 2026-10-07 12:00 local time.
func fixedNow() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local) }

// harness is one CLI run with every dependency faked: a temp home, a frozen
// clock, a fake source and runner, and captured logs.
type harness struct {
	home           string
	src            *fakeSource
	runner         *fakeRunner
	stdout, stderr bytes.Buffer
	logs           []string
	override       source                                           // replaces src when set
	sleep          func(ctx context.Context, d time.Duration) error // nil: return at once
	sleeps         []time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{home: t.TempDir(), src: &fakeSource{data: map[string]string{}}, runner: &fakeRunner{}}
	return h
}

func (h *harness) run(t *testing.T, args ...string) int {
	t.Helper()
	return h.runCtx(t, context.Background(), args...)
}

// runCtx is run with a context the test can cancel. Sleeping never waits:
// each sleep is recorded and then calls h.sleep, if set.
func (h *harness) runCtx(t *testing.T, ctx context.Context, args ...string) int {
	t.Helper()
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	var src source = h.src
	if h.override != nil {
		src = h.override
	}
	return run(ctx, deps{
		Source: src,
		Now:    fixedNow,
		Paths:  func() (core.Paths, error) { return core.PathsFromHome(h.home), nil },
		Runner: h.runner,
		Stdout: &h.stdout,
		Stderr: &h.stderr,
		Sleep: func(ctx context.Context, d time.Duration) error {
			h.sleeps = append(h.sleeps, d)
			if h.sleep != nil {
				return h.sleep(ctx, d)
			}
			return ctx.Err()
		},
		SetupLog: func(core.Paths, io.Writer) (func() error, error) {
			slog.SetDefault(slog.New(logRecords{&h.logs}))
			return func() error { return nil }, nil
		},
	}, args)
}

// runCLI runs the command with a temp home and no network.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	h := newHarness(t)
	code = h.run(t, args...)
	return h.stdout.String(), h.stderr.String(), code
}

const (
	fixtures = "../../test/fixtures/"
	golden   = "../../test/golden/"
)

func TestFollowGoldens(t *testing.T) {
	cases := []struct {
		golden string
		args   []string
	}{
		{"nfl_multi.txt", []string{"follow", "999", "--mock", fixtures + "playbyplay_multi_plays.json", "--dry-run", "--once"}},
		{"nfl_final.txt", []string{"follow", "999", "--mock", fixtures + "playbyplay_final.json", "--dry-run", "--once"}},
		{"cfb.txt", []string{"follow", "999", "--sport", "cfb", "--mock", fixtures + "playbyplay_cfb_multi_plays.json", "--dry-run", "--once"}},
		{"nba.txt", []string{"follow", "999", "--sport", "nba", "--mock", fixtures + "playbyplay_nba_multi_plays.json", "--dry-run", "--once"}},
		{"wnba.txt", []string{"follow", "999", "--sport", "wnba", "--mock", fixtures + "playbyplay_wnba_multi_plays.json", "--dry-run", "--once"}},
		{"nhl.txt", []string{"follow", "999", "--sport", "nhl", "--mock", fixtures + "playbyplay_nhl_multi_plays.json", "--dry-run", "--once"}},
		{"mlb.txt", []string{"follow", "999", "--sport", "mlb", "--mock", fixtures + "playbyplay_mlb_multi_plays.json", "--dry-run", "--once"}},
		// Flags before the subcommand, between it and the game id, and after it.
		{"cfb.txt", []string{"--dry-run", "--sport=cfb", "follow", "--once", "999", "--mock", fixtures + "playbyplay_cfb_multi_plays.json"}},
		{"nfl_multi.txt", []string{"--once", "--mock", fixtures + "playbyplay_multi_plays.json", "follow", "999", "--dry-run"}},
	}
	for _, c := range cases {
		want, err := os.ReadFile(golden + c.golden)
		if err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runCLI(t, c.args...)
		if code != 0 || stderr != "" {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, stderr)
		}
		// Nothing but the poll output: no INFO log lines on stdout.
		if stdout != string(want) {
			t.Errorf("%v: stdout differs from %s\n--- got ---\n%s\n--- want ---\n%s", c.args, c.golden, stdout, want)
		}
	}
}

func TestFollowOnceTouchesNothing(t *testing.T) {
	h := newHarness(t)
	code := h.run(t, "follow", "999", "--mock", fixtures+"playbyplay_multi_plays.json", "--dry-run", "--once")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(h.home, ".cache", "playcall", "state.json")); err == nil {
		t.Error("--once wrote state.json")
	}
	if _, err := os.Stat(filepath.Join(h.home, ".config", "playcall", "config.json")); err == nil {
		t.Error("--once created config.json")
	}
	if len(h.runner.calls) != 0 {
		t.Errorf("--dry-run ran %v", h.runner.calls)
	}
}

func TestServiceCommandsNotImplemented(t *testing.T) {
	for _, args := range [][]string{
		{"stop"},
		{"follow", "999"}, {"follow", "BUF"}, {"follow", "BUF", "--dry-run"}, // no --once
	} {
		h := newHarness(t)
		code := h.run(t, args...)
		if code != 2 || h.stdout.Len() != 0 || !strings.Contains(h.stderr.String(), "not implemented yet") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, h.stdout.String(), h.stderr.String())
		}
		if len(h.src.requests) != 0 || len(h.runner.calls) != 0 {
			t.Errorf("%v: made requests %v or ran %v", args, h.src.requests, h.runner.calls)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"follow"},
		{"follow", "1", "2"},
		{"stop", "extra"},
		{"follow", "999", "--sport", "curling"},
		{"follow", "999", "--interval", "fast"},
		{"follow", "999", "--nope"},
		{"follow", "999", "--group", "x"},
		{"teams", "--all"},
	} {
		stdout, stderr, code := runCLI(t, args...)
		if code != 2 || stdout != "" || stderr == "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	if _, stderr, _ := runCLI(t, "follow", "999", "--sport", "curling"); !strings.Contains(stderr, "nfl, nba, mlb, nhl, cfb, cbb, wnba") {
		t.Errorf("sport error does not list sports: %q", stderr)
	}
}

func TestHelpExitsZero(t *testing.T) {
	stdout, _, code := runCLI(t, "--help")
	if code != 0 || !strings.Contains(stdout, "usage: playcall") {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
}

func TestMissingMockFile(t *testing.T) {
	_, stderr, code := runCLI(t, "follow", "999", "--once", "--dry-run", "--mock", "/nonexistent/mock.json")
	if code != 1 || stderr == "" {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func writeConfig(t *testing.T, h *harness, body string) {
	t.Helper()
	dir := filepath.Join(h.home, ".config", "playcall")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readConfig returns config.json with whitespace removed.
func readConfig(t *testing.T, h *harness) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.home, ".config", "playcall", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(b)), "")
}
