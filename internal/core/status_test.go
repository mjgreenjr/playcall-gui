package core

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
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
	// A timeout shows the situation after the play before it, not its own
	// start (stale after a punt); a score does not leave one.
	before := plays[len(plays)-2]
	before["type"] = map[string]any{"text": "Punt"}
	before["end"] = map[string]any{"downDistanceText": "1st & 10 at ISU 3"}
	last["type"] = map[string]any{"text": "Timeout"}
	last["start"] = map[string]any{"downDistanceText": "4th & 2 at ISU 43"}
	last["end"] = map[string]any{"team": map[string]any{"id": "1"}}
	if got := situation("cfb", feed(false)); got != "1st & 10 at ISU 3" {
		t.Errorf("timeout after a punt: %q", got)
	}
	before["type"] = map[string]any{"text": "Passing Touchdown"}
	before["end"] = map[string]any{}
	if got := situation("cfb", feed(false)); got != "" {
		t.Errorf("timeout after a touchdown: %q", got)
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
	if got.Away.Name != "Los Angeles Dodgers" || got.Home.Name != "Atlanta Braves" {
		t.Errorf("names %q / %q", got.Away.Name, got.Home.Name)
	}
	if got.Away.Logo == got.Home.Logo {
		t.Error("both sides have the same logo")
	}
}

// ESPN's status says "5th" for the first overtime; the header says "OT" like
// the plays do.
func TestStatusBasketballOvertimePeriod(t *testing.T) {
	cases := []struct {
		sport  string
		period int
		want   string
	}{
		{"nba", 4, "4th"}, {"nba", 5, "OT"}, {"wnba", 6, "2OT"},
		{"cbb", 2, "2nd"}, {"cbb", 3, "OT"},
	}
	for _, c := range cases {
		data := loadFixture(t, "playbyplay_nba_multi_plays.json")
		status := espn.Status(data)
		status["period"] = json.Number(strconv.Itoa(c.period))
		status["displayPeriod"] = "5th"
		var got GameStatus
		poller, err := NewPoller(&State{GameID: "999", Sport: c.sport}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
		if err != nil {
			t.Fatal(err)
		}
		poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got.Period != c.want {
			t.Errorf("%s period %d: got %q, want %q", c.sport, c.period, got.Period, c.want)
		}
	}
}

func TestBasketballCounts(t *testing.T) {
	// The NBA fixture is live, 4th quarter: both teams at 5 fouls and in the bonus.
	data := loadFixture(t, "playbyplay_nba_multi_plays.json")
	var got GameStatus
	poller, err := NewPoller(&State{GameID: "999", Sport: "nba"}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.AwayCounts == nil || got.HomeCounts == nil {
		t.Fatalf("no counts: %+v", got)
	}
	if *got.AwayCounts != (TeamCounts{Timeouts: 3, TimeoutSlots: 7, Fouls: 5, Bonus: "double"}) ||
		*got.HomeCounts != (TeamCounts{Timeouts: 3, TimeoutSlots: 7, Fouls: 5, Bonus: "double"}) {
		t.Errorf("away %+v home %+v", *got.AwayCounts, *got.HomeCounts)
	}

	// Not in play: nothing shown. The WNBA fixture is a final game.
	final := loadFixture(t, "playbyplay_wnba_multi_plays.json")
	poller, _ = NewPoller(&State{GameID: "998", Sport: "wnba"}, &recordSink{}, func(context.Context) (espn.Data, error) { return final, nil })
	poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.AwayCounts != nil || got.HomeCounts != nil {
		t.Errorf("final game has counts: %+v %+v", got.AwayCounts, got.HomeCounts)
	}
}

func TestTeamCountsBonusStates(t *testing.T) {
	c := func(state string) espn.Data {
		return espn.Data{"timeoutsRemaining": json.Number("2"), "fouls": map[string]any{"teamFoulsCurrent": json.Number("4"), "bonusState": state}}
	}
	for state, want := range map[string]string{"NONE": "", "": "", "DOUBLE": "double", "SINGLE": "single", "ONE_AND_ONE": "single"} {
		if got := teamCounts(c(state), nil); got.Bonus != want || got.Timeouts != 2 || got.Fouls != 4 {
			t.Errorf("%q: %+v", state, got)
		}
	}
	if teamCounts(espn.Data{}, nil) != nil || teamCounts(nil, nil) != nil {
		t.Error("no data should be nil")
	}
}

func TestPeriodTimeoutCap(t *testing.T) {
	comp := func(used string) espn.Data { return espn.Data{"timeoutsUsed": json.Number(used)} }
	for _, c := range []struct {
		name        string
		left, slots int
		used        string
		limit       int
		overtime    bool
		wantLeft    int
		wantSlots   int
	}{
		{"4th: six left, four allowed", 6, 7, "0", 4, false, 4, 7},
		{"4th: one used already", 6, 7, "1", 4, false, 3, 7},
		{"4th: fewer left than the cap", 3, 7, "0", 4, false, 3, 7},
		{"4th: used the quarter's four", 3, 7, "4", 4, false, 0, 7},
		{"OT: two allowed", 3, 9, "0", 2, true, 2, 2},
		{"OT: one used", 3, 9, "1", 2, true, 1, 2},
		{"OT: both used, ESPN still shows one", 1, 9, "2", 2, true, 0, 2},
	} {
		tc := &TeamCounts{Timeouts: c.left, TimeoutSlots: c.slots}
		capTimeouts(tc, comp(c.used), c.limit, c.overtime)
		if tc.Timeouts != c.wantLeft || tc.TimeoutSlots != c.wantSlots {
			t.Errorf("%s: got %+v, want %d of %d", c.name, *tc, c.wantLeft, c.wantSlots)
		}
	}
	capTimeouts(nil, nil, 4, false) // no counts: nothing to cap
}

func TestCollegeTimeouts(t *testing.T) {
	// A team's TimeoutSlots - Timeouts is what it has taken; ESPN's own
	// remaining count is replaced.
	for _, c := range []struct {
		name         string
		period       int
		left, slots  int
		wantL, wantS int
	}{
		{"1st half, none taken", 1, 4, 4, 4, 4},
		{"2nd half, two taken", 2, 2, 4, 2, 4},
		{"overtime adds one", 3, 4, 4, 5, 5},
		{"5OT, none taken", 7, 4, 4, 9, 9},
		{"5OT, three taken", 7, 1, 4, 6, 9},
		{"more taken than given", 2, 0, 6, 0, 4},
	} {
		tc := &TeamCounts{Timeouts: c.left, TimeoutSlots: c.slots}
		capCollegeTimeouts(tc, c.period)
		if tc.Timeouts != c.wantL || tc.TimeoutSlots != c.wantS {
			t.Errorf("%s: got %+v, want %d of %d", c.name, *tc, c.wantL, c.wantS)
		}
	}
	capCollegeTimeouts(nil, 3)
}

func TestStatusRecords(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	comp := espn.Competition(data)
	home, away := espn.Teams(comp)
	away["record"] = []any{
		map[string]any{"type": "total", "summary": "3-1"},
		map[string]any{"type": "vsconf", "summary": "1-0"},
	}
	home["record"] = []any{map[string]any{"type": "total", "summary": "2-2"}}
	comp["series"] = []any{map[string]any{"type": "playoff", "competitors": []any{
		map[string]any{"id": away["id"], "wins": json.Number("2")},
		map[string]any{"id": home["id"], "wins": json.Number("1")},
	}}}
	var got GameStatus
	poller, err := NewPoller(&State{GameID: "999", Sport: "nfl"}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := (TeamRecord{Overall: "3-1", Conf: "1-0", SeriesWins: "2"}); got.AwayRecord != want {
		t.Errorf("away %+v, want %+v", got.AwayRecord, want)
	}
	if want := (TeamRecord{Overall: "2-2", SeriesWins: "1"}); got.HomeRecord != want {
		t.Errorf("home %+v, want %+v", got.HomeRecord, want)
	}
}

func TestStatusPreseason(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	status := func() GameStatus {
		var got GameStatus
		poller, err := NewPoller(&State{GameID: "999", Sport: "nfl"}, &recordSink{}, func(context.Context) (espn.Data, error) { return data, nil })
		if err != nil {
			t.Fatal(err)
		}
		poller.StatusSink = StatusFunc(func(s GameStatus) { got = s })
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if status().Preseason {
		t.Error("the fixture is not a preseason game")
	}
	espn.Map(data["header"])["season"] = map[string]any{"type": json.Number("1")}
	if !status().Preseason {
		t.Error("season type 1 is the preseason")
	}
}

func TestFootballPeriodBreak(t *testing.T) {
	endPeriod := espn.Data{"type": espn.Data{"text": "End Period"}, "period": espn.Data{"number": json.Number("3")}}
	snap := espn.Data{"type": espn.Data{"text": "Rush"}, "period": espn.Data{"number": json.Number("3")}}
	feed := func(statusPeriod string, finished bool, plays ...espn.Data) *Feed {
		return &Feed{
			Data:     espn.Data{"status": espn.Data{"period": json.Number(statusPeriod)}},
			Plays:    plays,
			Finished: finished,
		}
	}
	cases := []struct {
		name  string
		sport string
		f     *Feed
		want  bool
	}{
		{"end of the 3rd, ESPN still on 3", "cfb", feed("3", false, snap, endPeriod), true},
		{"nfl too", "nfl", feed("3", false, snap, endPeriod), true},
		{"ESPN already on the 4th", "cfb", feed("4", false, snap, endPeriod), false},
		{"a snap after it", "cfb", feed("3", false, endPeriod, snap), false},
		{"final", "cfb", feed("3", true, snap, endPeriod), false},
		{"basketball has its own status", "nba", feed("3", false, snap, endPeriod), false},
		{"no plays", "cfb", feed("3", false), false},
	}
	for _, c := range cases {
		if got := footballPeriodBreak(c.sport, c.f); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMLBSituation(t *testing.T) {
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	plays := espn.ExtractPlays(data)
	got := situation("mlb", &Feed{Data: data, Plays: plays})
	if !regexp.MustCompile(`^(\d-\d, )?\d outs?(, \d on)?$`).MatchString(got) {
		t.Errorf("situation %q", got)
	}
	if got := situation("mlb", &Feed{Data: data, Plays: plays, Finished: true}); got != "" {
		t.Errorf("finished: %q", got)
	}
	if got := situation("mlb", &Feed{Data: data}); got != "" {
		t.Errorf("no plays: %q", got)
	}
}

func TestMLBMatchup(t *testing.T) {
	play := func(id, kind, half, text string) espn.Data {
		return espn.Data{"id": id, "text": text,
			"type":   map[string]any{"type": kind},
			"period": map[string]any{"type": half}}
	}
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	feed := func(plays ...espn.Data) *Feed { return &Feed{Data: data, Plays: plays} }
	top := play("101", "start-batterpitcher", "Top", "Gavin Williams pitches to Sam Antonacci")
	pitch := play("102", "pitch", "Top", "Pitch 1 : Ball 1")
	p, b, side := mlbMatchup("mlb", feed(top, pitch))
	if p != "Gavin Williams" || b != "Sam Antonacci" || side != "away" {
		t.Errorf("top: %q %q %q", p, b, side)
	}
	bot := play("201", "start-batterpitcher", "Bottom", "Sean Burke pitches to Patrick Bailey")
	if _, _, side := mlbMatchup("mlb", feed(top, bot)); side != "home" {
		t.Errorf("bottom: %q", side)
	}
	end := play("299", "end-inning", "End", "End of the 3rd inning")
	if p, b, side := mlbMatchup("mlb", feed(top, bot, end)); p != "" || b != "" || side != "" {
		t.Errorf("after the inning: %q %q %q", p, b, side)
	}
	if p, _, _ := mlbMatchup("nfl", feed(top)); p != "" {
		t.Error("another sport")
	}
}
