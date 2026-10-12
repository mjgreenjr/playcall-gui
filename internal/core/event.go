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
	Rank      string    // college ranking "1" to "25"; "" if unranked or not a college sport
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
	// Revision marks an update to a non-scoring play (a foul, a substitution,
	// a penalty): it corrects a row already shown, and a sink with no list
	// to correct (a toast) should skip it. Scoring updates are not revisions.
	Revision bool
	Series   string // postseason series standing without parentheses, or ""
	// Possession is the side on offence when the event was sent: "away",
	// "home", or "" when unknown or the sport has no such notion.
	Possession string
	// Batter is baseball's batter on a pitch, which a toast adds to the text
	// (the play itself, "Ball 2", does not name anyone); "" otherwise.
	Batter string
	// Display is Body with each player's team added after the name, or an
	// icing's team; "" when there is nothing to add. Body itself stays ESPN's
	// text so the CLI output is unchanged.
	Display string
}

// BodyText is what the window and toasts show: Display, or else Body.
func (e Event) BodyText() string {
	if e.Display != "" {
		return e.Display
	}
	return e.Body
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

// possessionMarker is the ball that shows who has it in a toast, by sport; a
// plain dot for any sport without one.
func possessionMarker(sport string) string {
	switch sport {
	case "nfl", "cfb":
		return "🏈"
	case "nba", "wnba", "cbb":
		return "🏀"
	case "mlb":
		return "⚾"
	}
	return "•"
}

// MarkedTitle is Title with the sport's ball added, for sinks that have
// no room for a ball icon: before the away abbreviation ("• ATL 14 · ..."),
// or after the home score ("... · NO 0 •"), like the GUI header. Title
// itself stays unmarked so the CLI output does not change.
func (e Event) MarkedTitle() string {
	mark := possessionMarker(e.Sport)
	switch e.Possession {
	case "away":
		return mark + " " + e.Title
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
		return e.Title[:i] + " " + mark + e.Title[i:]
	}
	return e.Title
}
