package main

import (
	"fmt"
	"os"
	"testing"

	"playcall/internal/core"
)

// TestMain keeps every test away from the real config and cache: whatever a
// test forgets to redirect lands in a throwaway home, never the user's own.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "playcall-gui-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", home+"/.config")
	os.Setenv("XDG_CACHE_HOME", home+"/.cache")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestResumeGame(t *testing.T) {
	var cfg core.Config
	if _, ok := resumeGame(cfg); ok {
		t.Error("resumed with nothing remembered")
	}
	cfg.SetLastGame(core.LastGame{Sport: "nba", ID: "401", Label: "SA @ UTAH"})
	if g, ok := resumeGame(cfg); !ok || g.ID != "401" || g.Sport != "nba" {
		t.Errorf("got %+v, %v", g, ok)
	}
	cfg.SetLastGame(core.LastGame{Sport: "curling", ID: "1"})
	if _, ok := resumeGame(cfg); ok {
		t.Error("resumed a sport that is not supported")
	}
}
