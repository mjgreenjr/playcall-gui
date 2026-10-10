package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// hyprTimeout bounds one hyprctl call; the router asks once per play.
const hyprTimeout = 3 * time.Second

// hyprOutput runs `hyprctl args... -j` and returns its JSON.
type hyprOutput func(ctx context.Context, args ...string) ([]byte, error)

func execHyprctl(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, hyprTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "hyprctl", args...).Output()
}

// hyprVisible reports whether our window is on screen under Hyprland: it is
// mapped, not hidden, and on a workspace that is active on some monitor. A
// special (scratchpad) workspace counts as hidden. Any failure (hyprctl
// missing, bad JSON, no window of ours) is an error, which the router treats
// as "not visible".
func hyprVisible(ctx context.Context, run hyprOutput, pid int) (bool, error) {
	cl, err := run(ctx, "clients", "-j")
	if err != nil {
		return false, err
	}
	var clients []struct {
		PID       int  `json:"pid"`
		Mapped    bool `json:"mapped"`
		Hidden    bool `json:"hidden"`
		Workspace struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(cl, &clients); err != nil {
		return false, fmt.Errorf("hyprctl clients: %w", err)
	}
	mon, err := run(ctx, "monitors", "-j")
	if err != nil {
		return false, err
	}
	var monitors []struct {
		Active struct {
			ID int `json:"id"`
		} `json:"activeWorkspace"`
	}
	if err := json.Unmarshal(mon, &monitors); err != nil {
		return false, fmt.Errorf("hyprctl monitors: %w", err)
	}
	active := map[int]bool{}
	for _, m := range monitors {
		active[m.Active.ID] = true
	}
	for _, c := range clients {
		if c.PID != pid {
			continue
		}
		if !c.Mapped || c.Hidden || c.Workspace.ID < 0 || strings.HasPrefix(c.Workspace.Name, "special:") {
			return false, nil
		}
		return active[c.Workspace.ID], nil
	}
	return false, fmt.Errorf("no hyprland window for pid %d", pid)
}

// windowVisible is the router's Visible func on Linux.
func windowVisible(ctx context.Context) (bool, error) {
	return hyprVisible(ctx, execHyprctl, os.Getpid())
}
