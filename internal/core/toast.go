package core

import (
	"context"
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
	return append([]string{
		"-u", urgency,
		"-t", strconv.Itoa(NotifExpireMS),
		e.Title, e.Body,
		"--exec",
	}, strings.Fields("xdg-open "+e.URL)...)
}

// Send runs NotifCmd. A failure is logged at ERROR and returned, so the poller
// retries the play on the next poll.
func (s ToastSink) Send(ctx context.Context, e Event) error {
	ctx, cancel := context.WithTimeout(ctx, NotifTimeout)
	defer cancel()
	if err := s.Runner.Run(ctx, NotifCmd, s.Args(e)...); err != nil {
		slog.Error(fmt.Sprintf("Failed to send notification: %v", err))
		return err
	}
	slog.Debug("Sent notification: " + e.Title)
	return nil
}
