package core

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// FeedSink writes a game as plain lines, one per play and one per change of
// the header, for a terminal or `tail -f`. It is both a Sink (the plays) and a
// StatusSink (the header), so a Poller wired with it as both gives the whole
// picture the window shows:
//
//	21:40:54 HEADER BAL 21 (4-1) @ ATL 23 (2-3) · 0:40 4th · 1st & 10 at ATL 25 · Timeouts BAL 1, ATL 0
//	21:40:54 PLAY   BAL 21 · 0:40 4th · ATL 23 | T.Huntley pass short right to M.Andrews ...
//
// The header line is written when its text changes, not every poll. Each line
// is written whole, so a reader never sees half of one.
type FeedSink struct {
	W io.Writer
	// Now is the clock for the time stamps; nil means time.Now.
	Now func() time.Time

	mu         sync.Mutex
	lastHeader string
	err        error
}

// Err is the first write error, if any (a full disk, a closed pipe). The
// poll loop cannot stop on it from inside a Status call, so the caller checks.
func (f *FeedSink) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *FeedSink) line(kind, text string) error {
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	_, err := fmt.Fprintf(f.W, "%s %-6s %s\n", now().Format("15:04:05"), kind, text)
	if err != nil && f.err == nil {
		f.err = err
	}
	return err
}

// Note writes a line that is neither a play nor the header (the start of a
// game in a file that already holds others).
func (f *FeedSink) Note(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.line("NOTE", text)
}

// Send writes one play. A play that failed to write is returned as an error,
// so the poller retries it on its next poll.
func (f *FeedSink) Send(_ context.Context, e Event) error {
	kind := "PLAY"
	switch {
	case e.Kind == KindFinal:
		kind = "FINAL"
	case e.Revision:
		kind = "REVISE"
	case e.Kind == KindUpdate:
		kind = "UPDATE"
	}
	text := e.Title
	if body := strings.ReplaceAll(strings.TrimSpace(e.BodyText()), "\n", " "); body != "" {
		text += " | " + body
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.line(kind, text)
}

// Status writes the header when it differs from the last one written.
func (f *FeedSink) Status(s GameStatus) {
	h := HeaderLine(s)
	f.mu.Lock()
	defer f.mu.Unlock()
	if h == f.lastHeader {
		return
	}
	if f.line("HEADER", h) == nil {
		f.lastHeader = h
	}
}

// HeaderLine is the top of the game screen as one line: the teams with their
// scores and records, then what the game is doing, the situation, the series
// and the timeouts. It carries nothing that changes without the game changing
// (no "updated Ns ago", no halftime countdown), so an unchanged game gives an
// unchanged line.
func HeaderLine(s GameStatus) string {
	team := func(t TeamScore, r TeamRecord) string {
		out := strings.TrimSpace(t.Abbr + " " + t.Score)
		if r.Overall != "" && !s.Postseason {
			out += " (" + r.Overall + ")"
		}
		return out
	}
	parts := []string{team(s.Away, s.AwayRecord) + " @ " + team(s.Home, s.HomeRecord)}
	if st := headerStatus(s); st != "" {
		parts = append(parts, st)
	}
	if s.Situation != "" {
		parts = append(parts, s.Situation)
	}
	if s.Pitcher != "" || s.Batter != "" {
		parts = append(parts, "Pitcher: "+orDash(s.Pitcher)+", at bat: "+orDash(s.Batter))
	}
	switch s.Possession {
	case "away":
		parts = append(parts, s.Away.Abbr+" ball")
	case "home":
		parts = append(parts, s.Home.Abbr+" ball")
	}
	if s.Preseason {
		parts = append(parts, "Preseason")
	}
	if s.SeriesInfo != "" {
		parts = append(parts, s.SeriesInfo)
	}
	if s.Series != "" {
		parts = append(parts, "Series: "+s.Series)
	}
	if s.AwayCounts != nil && s.HomeCounts != nil {
		parts = append(parts, fmt.Sprintf("Timeouts %s %d, %s %d",
			s.Away.Abbr, s.AwayCounts.Timeouts, s.Home.Abbr, s.HomeCounts.Timeouts))
		if s.AwayCounts.Fouls+s.HomeCounts.Fouls > 0 || s.AwayCounts.Bonus != "" || s.HomeCounts.Bonus != "" {
			fouls := fmt.Sprintf("Fouls %s %d, %s %d", s.Away.Abbr, s.AwayCounts.Fouls, s.Home.Abbr, s.HomeCounts.Fouls)
			for _, b := range []struct {
				abbr string
				c    *TeamCounts
			}{{s.Away.Abbr, s.AwayCounts}, {s.Home.Abbr, s.HomeCounts}} {
				if b.c.Bonus != "" {
					fouls += ", " + b.abbr + " in the bonus"
				}
			}
			parts = append(parts, fouls)
		}
	}
	return strings.Join(parts, " · ")
}

// headerStatus is the clock and period, or what stands in for them.
func headerStatus(s GameStatus) string {
	switch {
	case s.Final:
		if s.Detail != "" {
			return s.Detail
		}
		return "Final"
	case s.Halftime:
		return "Halftime"
	case s.Break, s.State == "pre":
		return s.Detail // "End of 3rd"; the start time before the game
	case s.Sport == "mlb" && s.Detail != "":
		return s.Detail // "Top 3rd"; baseball has no clock
	}
	return strings.TrimSpace(s.Clock + " " + s.Period)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
