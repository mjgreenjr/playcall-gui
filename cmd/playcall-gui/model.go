package main

import (
	"fmt"
	"strings"
	"time"

	"playcall/internal/core"
)

// Row is one line of the play list.
type Row struct {
	PlayID  string
	Title   string
	Body    string
	Urgent  bool
	Updated bool // a scoring play whose text changed after it was shown
	Final   bool
	At      time.Time // the play's own wallclock stamp; zero when ESPN gave none
}

// Model is the play list, newest first. It has no Fyne types and is only
// touched from the UI goroutine.
type Model struct {
	Rows []Row
}

// Apply adds an event, or rewrites the row of a re-sent scoring play. It
// returns true when a new row was added.
func (m *Model) Apply(e core.Event) (added bool) {
	_, added = m.ApplyAt(e)
	return added
}

// ApplyAt is Apply that also says where the new row went (0 is the top).
// Rows are in arrival order, newest first, except a late play (one ESPN
// posted after plays that came later in the game), which goes below the rows
// that happened after it, by its own wallclock stamp. A late play with no
// stamp, or older than every stamped row, takes the top or the bottom as
// arrival or age says; rows with no stamp (the final) are stepped over.
func (m *Model) ApplyAt(e core.Event) (index int, added bool) {
	row := Row{
		PlayID:  e.PlayID,
		Title:   e.Title,
		Body:    e.BodyText(),
		Urgent:  e.Urgent,
		Updated: e.Kind == core.KindUpdate,
		Final:   e.Kind == core.KindFinal,
		At:      e.At,
	}
	if e.Kind == core.KindUpdate {
		for i := range m.Rows {
			if m.Rows[i].PlayID == e.PlayID && !m.Rows[i].Final {
				m.Rows[i] = row
				return i, false
			}
		}
	}
	at := 0
	if e.Late && !e.At.IsZero() {
		at = len(m.Rows)
		for i, r := range m.Rows {
			if !r.At.IsZero() && !r.At.After(e.At) {
				at = i
				break
			}
		}
	}
	m.Rows = append(m.Rows, Row{})
	copy(m.Rows[at+1:], m.Rows[at:])
	m.Rows[at] = row
	return at, true
}

// statusLine is the header's second line: what ESPN says the game is doing.
func statusLine(s core.GameStatus, now time.Time) string {
	switch {
	case s.Final:
		if s.Detail != "" {
			return s.Detail
		}
		return "Final"
	case s.Halftime:
		if left := s.HalftimeEnds.Sub(now); !s.HalftimeEnds.IsZero() && left > 0 {
			// An estimate (the usual break), so it carries a tilde and stops at zero.
			return fmt.Sprintf("Halftime · ~%d:%02d", int(left/time.Minute), int(left%time.Minute/time.Second))
		}
		return "Halftime"
	case s.Break:
		return s.Detail // "End of 3rd", not a clock stuck at 0.0
	case s.State == "pre":
		return s.Detail
	case s.Sport == "mlb" && s.Detail != "":
		return s.Detail // "Top 3rd", "Mid 3rd"; baseball has no clock, ESPN's says 0:00
	}
	return strings.TrimSpace(s.Clock + " " + s.Period)
}

// staleText is empty while the last fetch is recent, then says how old it
// is, so a header that stopped updating (failed polls, backoff) shows it.
func staleText(updated, now time.Time, interval time.Duration) string {
	if updated.IsZero() {
		return ""
	}
	age := now.Sub(updated)
	if age < max(3*interval, 15*time.Second) {
		return ""
	}
	return fmt.Sprintf("updated %s ago", age.Round(time.Second))
}

// timeoutDots is one circle per timeout the team started with: filled for
// each left, empty for each used, like the boxes on a broadcast. "0" when
// there is nothing to draw.
func timeoutDots(c *core.TeamCounts) string {
	left := max(c.Timeouts, 0)
	slots := max(c.TimeoutSlots, left)
	if slots == 0 {
		return "0"
	}
	return strings.Repeat("●", left) + strings.Repeat("○", slots-left)
}

// countsText is the line under a team's score: timeouts left and, in
// basketball, the team's fouls this period and the bonus when it is in it. "" when there is
// nothing to show.
func countsText(sport string, c *core.TeamCounts) string {
	if c == nil {
		return ""
	}
	return "TO " + timeoutDots(c) + " · " + foulsText(sport, c)
}

// foulsText is the fouls and bonus part of the counts line.
func foulsText(sport string, c *core.TeamCounts) string {
	parts := []string{fmt.Sprintf("Fouls %d", c.Fouls)}
	switch {
	case c.Bonus == "":
	case sport == "cbb" && c.Bonus == "double":
		parts = append(parts, "Double bonus")
	case sport == "cbb":
		parts = append(parts, "1-and-1")
	default:
		parts = append(parts, "Bonus")
	}
	return strings.Join(parts, " · ")
}
