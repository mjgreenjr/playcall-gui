package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"playcall/internal/espn"
)

const nflState = `{"game_id": "999", "last_play_id": null, "sport": "nfl", "started_at": 1790000000.0}`

func stateFile(h *harness) string { return filepath.Join(h.home, ".cache", "playcall", "state.json") }

func writeState(t *testing.T, h *harness, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(stateFile(h)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile(h), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hasState(h *harness) bool {
	_, err := os.Stat(stateFile(h))
	return err == nil
}

// scripted serves one response per fetch, repeating the last. An empty
// response is a failed fetch. before runs ahead of each fetch.
type scripted struct {
	responses []string
	before    func(n int, ctx context.Context)
	n         int
	urls      []string
}

func (s *scripted) Fetch(ctx context.Context, url string) (espn.Data, error) {
	s.urls = append(s.urls, url)
	if s.before != nil {
		s.before(s.n, ctx)
	}
	r := s.responses[min(s.n, len(s.responses)-1)]
	s.n++
	if r == "" {
		return nil, errors.New("fetch failed")
	}
	return espn.Decode([]byte(r))
}

func (s *scripted) ReadMock(path string) (espn.Data, error) { return espn.ReadMock(path) }

// resumeRig is a harness whose source is a scripted feed.
func resumeRig(t *testing.T, state string, responses ...string) (*harness, *scripted) {
	t.Helper()
	h := newHarness(t)
	src := &scripted{responses: responses}
	h.override = src
	if state != "" {
		writeState(t, h, state)
	}
	return h, src
}

func TestResumeNoUsableState(t *testing.T) {
	cases := []struct {
		name, state string
		code        int
		log         string
	}{
		{"no file", "", 0, "WARN No state to resume; nothing to do"},
		{"invalid JSON", `{"game_id": `, 1, "ERROR"},
		{"unsupported sport", `{"game_id": "999", "sport": "curling"}`, 1, "ERROR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, src := resumeRig(t, c.state)
			if code := h.run(t, "resume", "--dry-run"); code != c.code {
				t.Errorf("exit %d, want %d", code, c.code)
			}
			if len(h.logs) != 1 || !strings.HasPrefix(h.logs[0], c.log) {
				t.Errorf("logs %q, want %q", h.logs, c.log)
			}
			if len(src.urls) != 0 || len(h.src.requests) != 0 {
				t.Errorf("made requests %v", src.urls)
			}
			if c.state != "" {
				if b, _ := os.ReadFile(stateFile(h)); string(b) != c.state {
					t.Errorf("state changed: %q", b)
				}
			}
			if h.stdout.Len() != 0 {
				t.Errorf("stdout %q", h.stdout.String())
			}
		})
	}
}

func TestResumeLoopContinuesAfterFailedFetch(t *testing.T) {
	h, src := resumeRig(t, nflState, "", "", readFixture(t, "playbyplay_final.json"))
	if code := h.run(t, "resume", "--dry-run"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	// interval 10 (default config): 10<<1, 10<<2, then the game ends.
	want := []time.Duration{20 * time.Second, 40 * time.Second}
	if !reflect.DeepEqual(h.sleeps, want) {
		t.Errorf("sleeps %v, want %v", h.sleeps, want)
	}
	if len(src.urls) != 3 {
		t.Errorf("fetched %d times, want 3", len(src.urls))
	}
	if hasState(h) {
		t.Error("state kept after the game finished")
	}
}

func TestResumeClearsStateOnlyWhenFinished(t *testing.T) {
	live := readFixture(t, "playbyplay_multi_plays.json")
	h, src := resumeRig(t, nflState, live, live, readFixture(t, "playbyplay_final.json"))
	src.before = func(n int, _ context.Context) {
		if n > 0 && !hasState(h) {
			t.Errorf("state cleared before poll %d, game still on", n+1)
		}
	}
	if code := h.run(t, "resume", "--dry-run", "--interval", "7"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := []time.Duration{7 * time.Second, 7 * time.Second}; !reflect.DeepEqual(h.sleeps, want) {
		t.Errorf("sleeps %v, want %v", h.sleeps, want)
	}
	if hasState(h) {
		t.Error("state kept after the game finished")
	}
	if last := h.logs[len(h.logs)-1]; last != "INFO State cleared" {
		t.Errorf("last log %q", last)
	}
}

func TestResumeFailingSinkKeepsStateAndPolling(t *testing.T) {
	h, src := resumeRig(t, nflState, readFixture(t, "playbyplay_final.json"))
	h.runner.err = errors.New("toast failed")
	h.sleep = func(ctx context.Context, _ time.Duration) error {
		if len(h.sleeps) == 3 {
			return errors.New("stop")
		}
		return nil
	}
	if code := h.run(t, "resume"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(src.urls) != 3 {
		t.Errorf("fetched %d times, want 3", len(src.urls))
	}
	if len(h.runner.calls) < 3 {
		t.Errorf("sink tried %d times, want a retry each poll", len(h.runner.calls))
	}
	// Plays skipped as older advance last_play_id on purpose; the failed
	// ones must not, and the state must not be cleared.
	b, err := os.ReadFile(stateFile(h))
	if err != nil {
		t.Fatalf("state gone: %v", err)
	}
	if strings.Contains(string(b), "401872964272") {
		t.Errorf("last_play_id advanced past a failed send: %s", b)
	}
}

func TestResumeCancelFromSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h, _ := resumeRig(t, nflState, readFixture(t, "playbyplay_multi_plays.json"))
	h.sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	if code := h.runCtx(t, ctx, "resume", "--dry-run"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !hasState(h) {
		t.Error("state cleared on cancel")
	}
	if len(h.sleeps) != 1 {
		t.Errorf("sleeps %v", h.sleeps)
	}
}

func TestResumeCancelDuringFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _ := resumeRig(t, nflState, "x")
	started := make(chan struct{})
	h.override = blockingSource{started: started}
	done := make(chan int)
	go func() { done <- h.runCtx(t, ctx, "resume", "--dry-run") }()
	<-started
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !hasState(h) {
		t.Error("state cleared on cancel")
	}
	if len(h.sleeps) != 0 {
		t.Errorf("slept after cancel: %v", h.sleeps)
	}
}

// blockingSource blocks every fetch until its context is cancelled.
type blockingSource struct{ started chan struct{} }

func (b blockingSource) Fetch(ctx context.Context, _ string) (espn.Data, error) {
	close(b.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingSource) ReadMock(path string) (espn.Data, error) { return espn.ReadMock(path) }

func TestResumeOnceNeverTouchesState(t *testing.T) {
	h, _ := resumeRig(t, nflState, readFixture(t, "playbyplay_final.json"))
	if code := h.run(t, "resume", "--dry-run", "--once"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if b, _ := os.ReadFile(stateFile(h)); string(b) != nflState {
		t.Errorf("state: %q", b)
	}
	if len(h.logs) != 0 || len(h.sleeps) != 0 {
		t.Errorf("logs %q, sleeps %v", h.logs, h.sleeps)
	}
	if !strings.Contains(h.stdout.String(), "Game over") {
		t.Errorf("final event missing: %q", h.stdout.String())
	}
}

func TestResumeIgnoresSportFlag(t *testing.T) {
	h, src := resumeRig(t, nflState, readFixture(t, "playbyplay_final.json"))
	if code := h.run(t, "resume", "--dry-run", "--once", "--sport", "nba"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := []string{espn.SummaryURL("nfl", "999")}; !reflect.DeepEqual(src.urls, want) {
		t.Errorf("urls %v, want %v", src.urls, want)
	}
	if !strings.Contains(h.stdout.String(), "/nfl/game/") {
		t.Errorf("stdout %q", h.stdout.String())
	}
}
