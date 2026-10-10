package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"playcall/internal/espn"
)

type recordSink struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordSink) Send(_ context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

// A GUI drives the same Poller and Loop as the CLI, with its own sinks and a
// context, and no state file.
func TestLoopWithCustomSinks(t *testing.T) {
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	events := &recordSink{}
	var statuses []GameStatus
	state := &State{GameID: "401908015", Sport: "mlb"}
	poller, err := NewPoller(state, events, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 21, 0, 0, 0, time.UTC)
	poller.Now = func() time.Time { return now }
	poller.StatusSink = StatusFunc(func(s GameStatus) { statuses = append(statuses, s) })

	finished := false
	loop := &Loop{
		Poll:       poller.Poll,
		GameID:     state.GameID,
		Interval:   5 * time.Second,
		Sleep:      func(context.Context, time.Duration) error { return nil },
		OnFinished: func() { finished = true },
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !finished {
		t.Fatal("loop did not finish a final game")
	}
	if len(events.events) < 2 || events.events[len(events.events)-1].Kind != KindFinal {
		t.Fatalf("events: %+v", events.events)
	}
	if len(statuses) == 0 {
		t.Fatal("no status published")
	}
	s := statuses[len(statuses)-1]
	if !s.Final || s.State != "post" || s.Away.Abbr != "LAD" || s.Home.Abbr != "ATL" ||
		s.Away.Score != "3" || s.Home.Score != "1" || s.Series != "LAD 2-1" || !s.UpdatedAt.Equal(now) {
		t.Errorf("status: %+v", s)
	}
}

func TestLoopStopsOnCancelWithStatus(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json") // live NFL game
	state := &State{GameID: "999", Sport: "nfl"}
	poller, err := NewPoller(state, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	polls := 0
	loop := &Loop{
		Poll:     func(c context.Context) (Outcome, error) { polls++; return poller.Poll(c) },
		GameID:   "999",
		Interval: 5 * time.Second,
		Sleep: func(c context.Context, _ time.Duration) error {
			if polls >= 3 {
				cancel()
			}
			return c.Err()
		},
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if polls != 3 {
		t.Errorf("polls = %d, want 3", polls)
	}
}

func TestStatusHalftime(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	status := espn.Status(data)
	typ := espn.Map(status["type"])
	typ["name"] = "STATUS_HALFTIME"
	typ["shortDetail"] = "Halftime"
	var got GameStatus
	poller, err := NewPoller(&State{GameID: "999", Sport: "nfl"}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !got.Halftime || got.Final || got.Detail != "Halftime" {
		t.Errorf("status: %+v", got)
	}
}
