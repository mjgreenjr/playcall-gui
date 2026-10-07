package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI runs the command with HOME pointing at a temp dir, so nothing
// touches the real home directory.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return out.String(), errb.String(), code
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errb bytes.Buffer
	code := run([]string{"follow", "999", "--mock", fixtures + "playbyplay_multi_plays.json", "--dry-run", "--once"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "playcall", "state.json")); err == nil {
		t.Error("--once wrote state.json")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "playcall", "config.json")); err == nil {
		t.Error("--once created config.json")
	}
}

func TestStubsAreNotImplemented(t *testing.T) {
	for _, args := range [][]string{
		{"list"}, {"teams"}, {"stop"}, {"resume"}, {"test-notify"}, {"addfavorite", "buf"},
		{"list", "--all", "--group", "AFC East"},
		{"follow", "BUF"}, // team lookup
		{"follow", "999"}, // no --once
		{"follow", "999", "--once", "--mock", fixtures + "playbyplay_multi_plays.json"}, // no --dry-run: toast sink
		{"follow", "999", "--once", "--dry-run", "--sport", "nhl", "--mock", fixtures + "playbyplay_nhl_multi_plays.json"},
		{"--sport", "mlb", "follow", "999", "--once", "--dry-run"},
	} {
		stdout, stderr, code := runCLI(t, args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "not implemented yet") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
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
