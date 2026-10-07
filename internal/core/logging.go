package core

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// SetupLogging sends slog output to the log file under p and to stdout, and
// makes that logger the default. Records look like the Python's
// "2006-01-02 15:04:05 LEVEL message key=value". Use slog.LevelInfo for normal
// runs and slog.LevelDebug to see fetches, plays and sends. The returned
// function closes the log file.
func SetupLogging(p Paths, stdout io.Writer, level slog.Level) (func() error, error) {
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p.LogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := io.Writer(f)
	if stdout != nil {
		w = io.MultiWriter(f, stdout)
	}
	slog.SetDefault(slog.New(&lineHandler{w: &syncWriter{w: w}, level: level}))
	return f.Close, nil
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(b)
}

// lineHandler writes one "time LEVEL message attrs" line per record.
type lineHandler struct {
	w     io.Writer
	level slog.Level
	attrs []slog.Attr
	group string
}

func (h *lineHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *lineHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", r.Time.Format("2006-01-02 15:04:05"), r.Level, r.Message)
	write := func(a slog.Attr) {
		fmt.Fprintf(&b, " %s%s=%v", h.group, a.Key, a.Value)
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(func(a slog.Attr) bool { write(a); return true })
	b.WriteByte('\n')
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *lineHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), as...)
	return &c
}

func (h *lineHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.group += name + "."
	return &c
}
