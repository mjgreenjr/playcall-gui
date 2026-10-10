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
}

// Model is the play list, newest first. It has no Fyne types and is only
// touched from the UI goroutine.
type Model struct {
	Rows []Row
}

// Apply adds an event, or rewrites the row of a re-sent scoring play. It
// returns true when a new row was added at the top.
func (m *Model) Apply(e core.Event) (added bool) {
	row := Row{
		PlayID:  e.PlayID,
		Title:   e.Title,
		Body:    e.Body,
		Urgent:  e.Urgent,
		Updated: e.Kind == core.KindUpdate,
		Final:   e.Kind == core.KindFinal,
	}
	if e.Kind == core.KindUpdate {
		for i := range m.Rows {
			if m.Rows[i].PlayID == e.PlayID && !m.Rows[i].Final {
				m.Rows[i] = row
				return false
			}
		}
	}
	m.Rows = append([]Row{row}, m.Rows...)
	return true
}

// statusLine is the header's second line: what ESPN says the game is doing.
func statusLine(s core.GameStatus) string {
	switch {
	case s.Final:
		if s.Detail != "" {
			return s.Detail
		}
		return "Final"
	case s.Halftime:
		return "Halftime"
	case s.Break:
		return s.Detail // "End of 3rd", not a clock stuck at 0.0
	case s.State == "pre":
		return s.Detail
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

// countsText is basketball's line under a team's score: timeouts left, the
// team's fouls this period and the bonus when it is in it. "" when there is
// nothing to show.
func countsText(sport string, c *core.TeamCounts) string {
	if c == nil {
		return ""
	}
	parts := []string{"TO " + timeoutDots(c), fmt.Sprintf("Fouls %d", c.Fouls)}
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
