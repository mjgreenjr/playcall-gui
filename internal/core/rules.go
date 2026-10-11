package core

import (
	"errors"
	"fmt"
	"strings"

	"playcall/internal/espn"
	"playcall/internal/sports"
)

// ErrNotImplemented means the sport's rules are not written yet.
var ErrNotImplemented = errors.New("not implemented yet")

// Feed is one poll's view of the game, handed to Rules.Prepare. Header values
// are the game-level ones (the latest score, the status clock).
type Feed struct {
	Data     espn.Data
	Plays    []espn.Data // in ExtractPlays order
	Finished bool
	SentIDs  map[string]bool // ids announced or skipped (sent-id sports)

	Home, Away TeamScore
	Clock      string // top-level clock, else status displayClock
	Period     string // top-level period, else status displayPeriod
	Series     string // postseason series standing, e.g. "LAD 2-1"; "" if none
}

// Message is what a play becomes: the rendered text, and the structured parts
// of it for the Event.
type Message struct {
	Title, Body string
	Urgent      bool
	Home, Away  TeamScore
	Clock       string
	Period      string
	// Batter is baseball's batter on a pitch ("" otherwise); a toast adds it.
	Batter string
	// Display is the text for the window and toasts when it differs from Body
	// ("Icing (ANA)"); "" means Body. The CLI keeps the plain Body.
	Display string
}

// Rules holds a sport's per-poll constants and hooks.
type Rules interface {
	// CatchupLimit is how many of the newest unseen plays are announced after
	// a gap; the rest are skipped.
	CatchupLimit() int
	// TrackSent reports that "is new" means "id not in sent_ids" (NHL, NBA);
	// otherwise ids are compared as integers against last_play_id.
	TrackSent() bool
	// Prepare looks at the whole feed once per poll.
	Prepare(f *Feed) PlayRules
}

// PlayRules is Rules bound to one poll's feed.
type PlayRules interface {
	// Announced reports whether the play is sent. Plays that are not stay
	// unseen, silent on every poll.
	Announced(p espn.Data) bool
	// Message renders the play.
	Message(p espn.Data) Message
	// IDs are the ids marked sent once the play is (its own, plus any play
	// bundled with it). Used by sent-id sports.
	IDs(p espn.Data) []string
}

// RulesFor returns the rules for a sport, or an error (ErrNotImplemented
// for sports that are valid but not ported yet).
func RulesFor(sport string) (Rules, error) {
	if err := sports.Validate(sport); err != nil {
		return nil, err
	}
	switch sport {
	case "nfl", "cfb":
		return footballRules{}, nil
	case "nba", "wnba":
		return basketballRules{regulation: 4}, nil
	case "cbb":
		return basketballRules{regulation: 2}, nil
	case "nhl":
		return nhlRules{}, nil
	case "mlb":
		return mlbRules{}, nil
	}
	return nil, fmt.Errorf("sport %s: %w", sport, ErrNotImplemented)
}

// FormatTitle returns "ATL 14 · 0:48 1st · NO 0". The status is clock and
// period joined and trimmed; empty parts are left out.
func FormatTitle(awayAbbr, awayScore, clock, period, homeAbbr, homeScore string) string {
	if period != "" {
		period = " " + period
	}
	status := strings.TrimSpace(clock + period)
	var parts []string
	for _, p := range []string{awayAbbr + " " + awayScore, status, homeAbbr + " " + homeScore} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}
