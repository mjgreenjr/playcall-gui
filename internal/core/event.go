package core

import (
	"context"
	"fmt"
	"io"
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
	Score     string
	PowerPlay bool   // NHL: this side has the man advantage
	Logo      string // logo URL for a dark background, "" if none
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
