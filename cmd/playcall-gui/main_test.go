package main

import (
	"fmt"
	"os"
	"testing"
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
