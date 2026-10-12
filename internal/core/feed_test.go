package core

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func feedAt() *FeedSink {
	var b bytes.Buffer
	return &FeedSink{W: &b, Now: func() time.Time { return time.Date(2026, 10, 12, 21, 40, 54, 0, time.UTC) }}
}

func TestFeedSinkLines(t *testing.T) {
	f := feedAt()
	ctx := context.Background()
	f.Send(ctx, Event{Kind: KindPlay, Title: "BAL 21 · 0:40 4th · ATL 23", Body: "Pass\nshort right", Display: ""})
	f.Send(ctx, Event{Kind: KindUpdate, Title: "BAL 30 · 2:57 OT · ATL 23", Body: "TOUCHDOWN. Extra point is GOOD."})
	f.Send(ctx, Event{Kind: KindPlay, Revision: true, Title: "t", Body: "fixed", Display: "fixed (BAL)"})
	f.Send(ctx, Event{Kind: KindFinal, Title: "BAL 30 · Final/OT · ATL 23", Body: "Game over"})
	want := `21:40:54 PLAY   BAL 21 · 0:40 4th · ATL 23 | Pass short right
21:40:54 UPDATE BAL 30 · 2:57 OT · ATL 23 | TOUCHDOWN. Extra point is GOOD.
21:40:54 REVISE t | fixed (BAL)
21:40:54 FINAL  BAL 30 · Final/OT · ATL 23 | Game over
`
	if got := f.W.(*bytes.Buffer).String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFeedSinkWritesTheHeaderOnlyWhenItChanges(t *testing.T) {
	f := feedAt()
	s := GameStatus{
		Sport: "nfl", State: "in", Clock: "0:40", Period: "4th",
		Away: TeamScore{Abbr: "BAL", Score: "21"}, Home: TeamScore{Abbr: "ATL", Score: "23"},
		AwayRecord: TeamRecord{Overall: "4-1"}, HomeRecord: TeamRecord{Overall: "2-3"},
		Situation: "1st & 10 at ATL 25", Possession: "away",
		AwayCounts: &TeamCounts{Timeouts: 1}, HomeCounts: &TeamCounts{Timeouts: 0},
	}
	f.Status(s)
	s.UpdatedAt = s.UpdatedAt.Add(time.Minute) // a later fetch of the same game
	f.Status(s)
	s.Clock = "0:33"
	f.Status(s)
	got := f.W.(*bytes.Buffer).String()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 header lines, got %d:\n%s", len(lines), got)
	}
	want := "21:40:54 HEADER BAL 21 (4-1) @ ATL 23 (2-3) · 0:40 4th · 1st & 10 at ATL 25 · BAL ball · Timeouts BAL 1, ATL 0"
	if lines[0] != want {
		t.Errorf("header\n got %q\nwant %q", lines[0], want)
	}
}

func TestHeaderLineByState(t *testing.T) {
	base := GameStatus{Away: TeamScore{Abbr: "LAD", Score: "1"}, Home: TeamScore{Abbr: "MIL", Score: "2"}}
	for _, c := range []struct {
		name string
		mod  func(*GameStatus)
		want string
	}{
		{"final in a series", func(s *GameStatus) {
			s.Final, s.Detail, s.Postseason = true, "Final", true
			s.SeriesInfo, s.Series = "NLCS - Game 1 · Best of 7", "MIL 1-0"
			s.AwayRecord = TeamRecord{Overall: "100-62"}
		}, "LAD 1 @ MIL 2 · Final · NLCS - Game 1 · Best of 7 · Series: MIL 1-0"},
		{"halftime", func(s *GameStatus) { s.Halftime, s.Clock, s.Period = true, "0:00", "2nd" }, "LAD 1 @ MIL 2 · Halftime"},
		{"before the start", func(s *GameStatus) { s.State, s.Detail = "pre", "10/12 - 8:15 PM EDT" }, "LAD 1 @ MIL 2 · 10/12 - 8:15 PM EDT"},
		{"baseball", func(s *GameStatus) {
			s.Sport, s.Detail, s.Pitcher, s.Batter = "mlb", "Top 3rd", "Sean Burke", ""
		}, "LAD 1 @ MIL 2 · Top 3rd · Pitcher: Sean Burke, at bat: -"},
		{"basketball", func(s *GameStatus) {
			s.Clock, s.Period = "5:12", "2nd"
			s.AwayCounts = &TeamCounts{Timeouts: 5, Fouls: 3, Bonus: "double"}
			s.HomeCounts = &TeamCounts{Timeouts: 4, Fouls: 1}
		}, "LAD 1 @ MIL 2 · 5:12 2nd · Timeouts LAD 5, MIL 4 · Fouls LAD 3, MIL 1, LAD in the bonus"},
	} {
		s := base
		c.mod(&s)
		if got := HeaderLine(s); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestFeedSinkReportsWriteFailures(t *testing.T) {
	f := &FeedSink{W: failingWriter{}}
	if err := f.Send(context.Background(), Event{Title: "x"}); err == nil {
		t.Error("a failed write must come back as an error, so the play is retried")
	}
	f.Status(GameStatus{})
	if f.Err() == nil {
		t.Error("Err should keep the first failure")
	}
	// The header that failed is written again once the writer works.
	var b bytes.Buffer
	f.W = &b
	f.Status(GameStatus{})
	if !strings.Contains(b.String(), "HEADER") {
		t.Errorf("header not retried: %q", b.String())
	}
}
