package core

import (
	"context"
	"strings"
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

func TestPossession(t *testing.T) {
	// football: the fixture's drives-shaped feed, last play's end team.
	data := loadFixture(t, "playbyplay_multi_plays.json")
	status := espn.Status(data)
	typ := espn.Map(status["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	feed := func(d espn.Data, finished bool) *Feed {
		return &Feed{Data: d, Plays: espn.ExtractPlays(d), Finished: finished}
	}
	home, away := espn.Teams(espn.Competition(data))
	last := espn.ExtractPlays(data)
	lastPlay := last[len(last)-1]

	setTeam := func(id string) {
		lastPlay["end"] = map[string]any{"team": map[string]any{"id": id}}
	}
	setTeam(espn.Str(away["id"]))
	if got := possession("nfl", feed(data, false)); got != "away" {
		t.Errorf("away ball: %q", got)
	}
	setTeam(espn.Str(home["id"]))
	if got := possession("cfb", feed(data, false)); got != "home" {
		t.Errorf("home ball: %q", got)
	}
	delete(lastPlay, "end")
	lastPlay["start"] = map[string]any{"team": map[string]any{"id": espn.Str(away["id"])}}
	if got := possession("nfl", feed(data, false)); got != "away" {
		t.Errorf("falls back to start team: %q", got)
	}
	setTeam("12345")
	if got := possession("nfl", feed(data, false)); got != "" {
		t.Errorf("unknown team: %q", got)
	}
	setTeam(espn.Str(home["id"]))
	if got := possession("nba", feed(data, false)); got != "" {
		t.Errorf("basketball has no marker: %q", got)
	}
	if got := possession("nfl", feed(data, true)); got != "" {
		t.Errorf("final: %q", got)
	}
	typ["name"] = "STATUS_HALFTIME"
	if got := possession("nfl", feed(data, false)); got != "" {
		t.Errorf("halftime: %q", got)
	}
	typ["name"] = "STATUS_IN_PROGRESS"
	typ["state"] = "pre"
	if got := possession("nfl", feed(data, false)); got != "" {
		t.Errorf("pregame: %q", got)
	}

	// baseball: batting team from the half inning.
	typ["state"] = "in"
	for detail, want := range map[string]string{
		"Top 6th": "away", "Bot 7th": "home", "Bottom 7th": "home", "Mid 4th": "", "End 9th": "", "": "",
	} {
		typ["shortDetail"] = detail
		if got := possession("mlb", feed(data, false)); got != want {
			t.Errorf("mlb %q: got %q, want %q", detail, got, want)
		}
	}
}

func TestSituation(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	plays := espn.ExtractPlays(data)
	last := plays[len(plays)-1]
	feed := func(finished bool) *Feed { return &Feed{Data: data, Plays: plays, Finished: finished} }

	last["end"] = map[string]any{"downDistanceText": "1st & 10 at NO 25", "shortDownDistanceText": "1st & 10"}
	if got := situation("nfl", feed(false)); got != "1st & 10 at NO 25" {
		t.Errorf("full text: %q", got)
	}
	last["end"] = map[string]any{"shortDownDistanceText": "2nd & Goal"}
	if got := situation("cfb", feed(false)); got != "2nd & Goal" {
		t.Errorf("short fallback: %q", got)
	}
	last["end"] = map[string]any{"team": map[string]any{"id": "1"}}
	if got := situation("nfl", feed(false)); got != "" {
		t.Errorf("no text: %q", got)
	}
	// A timeout keeps the situation it started from; a score does not.
	last["type"] = map[string]any{"text": "Timeout"}
	last["start"] = map[string]any{"downDistanceText": "3rd & 5 at IOWA 13"}
	last["end"] = map[string]any{"team": map[string]any{"id": "1"}}
	if got := situation("cfb", feed(false)); got != "3rd & 5 at IOWA 13" {
		t.Errorf("timeout: %q", got)
	}
	last["type"] = map[string]any{"text": "Passing Touchdown"}
	if got := situation("cfb", feed(false)); got != "" {
		t.Errorf("touchdown should show no situation: %q", got)
	}

	last["end"] = map[string]any{"downDistanceText": "1st & 10 at NO 25"}
	if got := situation("nba", feed(false)); got != "" {
		t.Errorf("basketball: %q", got)
	}
	if got := situation("nfl", feed(true)); got != "" {
		t.Errorf("final: %q", got)
	}
	typ["name"] = "STATUS_HALFTIME"
	if got := situation("nfl", feed(false)); got != "" {
		t.Errorf("halftime: %q", got)
	}
}

func TestStatusPeriodBreak(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["name"] = "STATUS_END_PERIOD"
	typ["state"] = "in"
	typ["shortDetail"] = "End of 3rd"
	var got GameStatus
	poller, err := NewPoller(&State{GameID: "999", Sport: "nba"}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !got.Break || got.Halftime || got.Final || got.Detail != "End of 3rd" {
		t.Errorf("status: %+v", got)
	}
}

func TestStatusCarriesTeamLogos(t *testing.T) {
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	var got GameStatus
	poller, err := NewPoller(&State{GameID: "401908015", Sport: "mlb"}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	for side, logo := range map[string]espn.Logo{"away": got.Away.Logo, "home": got.Home.Logo} {
		if !strings.HasPrefix(logo.Dark, "https://") || !strings.Contains(logo.Dark, "dark") ||
			!strings.HasPrefix(logo.Light, "https://") || strings.Contains(logo.Light, "dark") {
			t.Errorf("%s logo %+v", side, logo)
		}
	}
	if got.Away.Logo == got.Home.Logo {
		t.Error("both sides have the same logo")
	}
}
