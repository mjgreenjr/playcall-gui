package core

import (
	"context"
	"fmt"
	"io"
	"strings"

	"playcall/internal/espn"
)

// Event kinds.
const (
	KindPlay   = "play"   // a new play
	KindUpdate = "update" // a scoring play re-sent with new text
	KindFinal  = "final"  // the end-of-game event
)

// TeamScore is one side of the game as shown in an event.
type TeamScore struct {
	Abbr      string
	Name      string // full team name, e.g. "Washington Huskies"; "" if unknown
	Score     string
	PowerPlay bool      // NHL: this side has the man advantage
	Logo      espn.Logo // logo URLs for light and dark backgrounds
}

// Event is one announcement. Title and Body are rendered once in the core, so
// every Sink shows the same text; the structured fields are for sinks (the
// GUI) that do not want to parse them.
type Event struct {
	GameID string
	PlayID string
	Kind   string
	Title  string
	Body   string
	Urgent bool // normal urgency; false = low
	URL    string

	Away, Home TeamScore
	Clock      string
	Period     string
	PlayText   string
	Sport      string
	Series     string // postseason series standing without parentheses, or ""
	// Possession is the side on offence when the event was sent: "away",
	// "home", or "" when unknown or the sport has no such notion.
	Possession string
}

// Sink receives events. An error means "not delivered": the poller keeps the
// play unsent and retries it on the next poll.
type Sink interface {
	Send(ctx context.Context, e Event) error
}

// StdoutSink prints events the way the Python --dry-run does.
type StdoutSink struct {
	W io.Writer
}

// Send writes "[DRY-RUN] title\nbody\nExec: xdg-open url".
func (s StdoutSink) Send(_ context.Context, e Event) error {
	_, err := fmt.Fprintf(s.W, "[DRY-RUN] %s\n%s\nExec: xdg-open %s\n", e.Title, e.Body, e.URL)
	return err
}

// PossessionMarker is the dot that shows who has the ball in a toast.
const PossessionMarker = "•"

// MarkedTitle is Title with the possession marker added, for sinks that have
// no room for a ball icon: before the away abbreviation ("• ATL 14 · ..."),
// or after the home score ("... · NO 0 •"), like the GUI header. Title
// itself stays unmarked so the CLI output does not change.
func (e Event) MarkedTitle() string {
	switch e.Possession {
	case "away":
		return PossessionMarker + " " + e.Title
	case "home":
		i := strings.LastIndex(e.Title, " · ")
		if i < 0 {
			return e.Title
		}
		i += len(" · ")
		score := e.Home.Abbr + " " + e.Home.Score
		if !strings.HasPrefix(e.Title[i:], score) {
			return e.Title
		}
		i += len(score)
		if strings.HasPrefix(e.Title[i:], " PP") {
			i += len(" PP")
		}
		return e.Title[:i] + " " + PossessionMarker + e.Title[i:]
	}
	return e.Title
}
