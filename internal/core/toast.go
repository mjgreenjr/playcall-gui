package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	// NotifCmd is the notification program. Never notify-send.
	NotifCmd = "omarchy-notification-send"
	// FallbackNotifCmd is used only when NotifCmd is not installed (any desktop
	// but Omarchy). It cannot open the game page when the toast is clicked.
	FallbackNotifCmd = "notify-send"
	// NotifExpireMS is how long a toast stays up, in milliseconds.
	NotifExpireMS = 15000
	// NotifTimeout bounds one run of NotifCmd.
	NotifTimeout = 30 * time.Second
)

// Runner runs an external program and waits for it. A non-zero exit, a
// missing program and a cancelled ctx are all errors.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs programs with os/exec.
type ExecRunner struct{}

// Run runs name with args.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// ErrNoNotifier means no notification program is available or working:
// omarchy-notification-send is not installed and notify-send is missing or
// failed. It is not worth retrying; the GUI shows such plays in the window.
var ErrNoNotifier = errors.New("no notification program available")

// ToastSink shows events as desktop notifications through omarchy-notification-send.
type ToastSink struct {
	Runner Runner
}

// Args builds the arguments for NotifCmd. `--exec` is last, followed by the
// command as separate words: "xdg-open URL".
func (ToastSink) Args(e Event) []string {
	urgency := "low"
	if e.Urgent {
		urgency = "normal"
	}
	body := e.BodyText()
	if e.Batter != "" {
		body += " · " + e.Batter
	}
	return append([]string{
		"-u", urgency,
		"-t", strconv.Itoa(NotifExpireMS),
		e.MarkedTitle(), body,
		"--exec",
	}, strings.Fields("xdg-open "+e.URL)...)
}

// fallbackArgs builds the arguments for FallbackNotifCmd: the same toast
// without the click action.
func (s ToastSink) fallbackArgs(e Event) []string {
	urgency := "low"
	if e.Urgent {
		urgency = "normal"
	}
	body := e.BodyText()
	if e.Batter != "" {
		body += " · " + e.Batter
	}
	return []string{"-u", urgency, "-t", strconv.Itoa(NotifExpireMS), "-a", "playcall", e.MarkedTitle(), body}
}

// Send runs NotifCmd, or FallbackNotifCmd when NotifCmd is not installed. A
// failure of NotifCmd is logged at ERROR and returned, so the poller retries
// the play on the next poll. When neither program works the error wraps
// ErrNoNotifier (logged at DEBUG: a caller that can show the play another way
// says so once).
func (s ToastSink) Send(ctx context.Context, e Event) error {
	ctx, cancel := context.WithTimeout(ctx, NotifTimeout)
	defer cancel()
	err := s.Runner.Run(ctx, NotifCmd, s.Args(e)...)
	if errors.Is(err, exec.ErrNotFound) {
		if err = s.Runner.Run(ctx, FallbackNotifCmd, s.fallbackArgs(e)...); err != nil {
			err = fmt.Errorf("%w (%s is not installed; %s: %v)", ErrNoNotifier, NotifCmd, FallbackNotifCmd, err)
			slog.Debug(err.Error())
			return err
		}
		slog.Debug("Sent notification with " + FallbackNotifCmd + ": " + e.Title)
		return nil
	}
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to send notification: %v", err))
		return err
	}
	slog.Debug("Sent notification: " + e.Title)
	return nil
}
