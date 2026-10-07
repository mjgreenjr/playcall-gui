package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"playcall/internal/espn"
)

// fakeSink records events; failures[i] makes the i-th Send call (0-based) fail.
type fakeSink struct {
	events   []Event
	calls    int
	failures map[int]bool
}

func (s *fakeSink) Send(_ context.Context, e Event) error {
	n := s.calls
	s.calls++
	if s.failures[n] {
		return errors.New("boom")
	}
	s.events = append(s.events, e)
	return nil
}

func (s *fakeSink) bodies() []string {
	var out []string
	for _, e := range s.events {
		out = append(out, e.Body)
	}
	return out
}

// mkFeed builds a flat-layout summary. Each play is "id|text|scoring" ("s" or "").
func mkFeed(t *testing.T, final bool, plays ...string) espn.Data {
	t.Helper()
	status := `{"type":{"state":"in","shortDetail":"3:07 - 2nd","name":"STATUS_IN_PROGRESS"},"displayClock":"3:07","displayPeriod":"2nd"}`
	if final {
		status = `{"type":{"state":"post","shortDetail":"Final","name":"STATUS_FINAL"},"displayClock":"0:00","displayPeriod":"4th"}`
	}
	var ps []string
	for _, p := range plays {
		f := strings.Split(p, "|")
		scoring := "false"
		if len(f) > 2 && f[2] == "s" {
			scoring = "true"
		}
		ps = append(ps, fmt.Sprintf(`{"id":%q,"text":%q,"scoringPlay":%s}`, f[0], f[1], scoring))
	}
	js := fmt.Sprintf(`{"header":{"competitions":[{"date":"2026-10-09T01:00Z","competitors":[
		{"homeAway":"home","score":"7","team":{"abbreviation":"HOM"}},
		{"homeAway":"away","score":"3","team":{"abbreviation":"AWY"}}]}]},
		"status":%s,"plays":[%s]}`, status, strings.Join(ps, ","))
	d, err := espn.Decode([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type rig struct {
	poller *Poller
	sink   *fakeSink
	feed   espn.Data
	trace  bytes.Buffer
}

func newRig(t *testing.T, sport string) *rig {
	t.Helper()
	r := &rig{sink: &fakeSink{}}
	p, err := NewPoller(&State{GameID: "999", Sport: sport}, r.sink, func(context.Context) (espn.Data, error) {
		if r.feed == nil {
			return nil, errors.New("no data")
		}
		return r.feed, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r.poller = p
	return r
}

func (r *rig) poll(t *testing.T) Status {
	t.Helper()
	out, err := r.poller.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out.Status
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestUnseenPlaysComparesIDsAsIntegers(t *testing.T) {
	mk := func(ids ...string) []espn.Data {
		var out []espn.Data
		for _, id := range ids {
			out = append(out, espn.Data{"id": id})
		}
		return out
	}
	// As strings "99" > "1000" > "401872964272"; as integers it is the reverse.
	plays := mk("401872964272", "99", "1000")
	got, err := unseenPlays(plays, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range got {
		ids = append(ids, espn.PlayKey(p))
	}
	eq(t, ids, []string{"99", "1000", "401872964272"})

	last := PlayID(1000)
	got, _ = unseenPlays(plays, &last)
	if len(got) != 1 || espn.PlayKey(got[0]) != "401872964272" {
		t.Errorf("after 1000: %v", got)
	}
	if _, err := unseenPlays(mk("abc"), nil); err == nil {
		t.Error("non-integer id should be an error")
	}
}

func TestFirstPollAnnouncesOnlyNewest(t *testing.T) {
	r := newRig(t, "nfl")
	r.feed = mkFeed(t, false, "10|one", "11|two", "12|three")
	if r.poll(t) != InProgress {
		t.Fatal("want InProgress")
	}
	eq(t, r.sink.bodies(), []string{"three"})
	if got := *r.poller.State.LastPlayID; got != 12 {
		t.Errorf("last play id = %d", got)
	}
	// Nothing new on the next poll.
	r.poll(t)
	if len(r.sink.events) != 1 {
		t.Errorf("resent: %v", r.sink.bodies())
	}
}

func TestCatchupSkipsOlderPlays(t *testing.T) {
	r := newRig(t, "nfl")
	last := PlayID(1)
	r.poller.State.LastPlayID = &last
	var plays []string
	for i := 2; i <= 9; i++ { // 8 unseen plays, limit 5
		plays = append(plays, fmt.Sprintf("%d|p%d", i, i))
	}
	r.feed = mkFeed(t, false, plays...)
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"p5", "p6", "p7", "p8", "p9"})
	if got := *r.poller.State.LastPlayID; got != 9 {
		t.Errorf("last play id = %d", got)
	}
}

func TestSkippedPlaysAdvanceLastPlayID(t *testing.T) {
	r := newRig(t, "nfl")
	last := PlayID(1)
	r.poller.State.LastPlayID = &last
	r.feed = mkFeed(t, false, "2|a", "3|b", "4|c", "5|d", "6|e", "7|f", "8|g")
	r.sink.failures = map[int]bool{0: true} // the first announced play fails
	r.poll(t)
	// Skipped 2 and 3; 4 failed, so last_play_id stays at the last skipped play.
	if got := *r.poller.State.LastPlayID; got != 3 {
		t.Errorf("last play id = %d, want 3", got)
	}
}

func TestFailedSendStopsBatchAndIsRetried(t *testing.T) {
	r := newRig(t, "nfl")
	last := PlayID(1)
	r.poller.State.LastPlayID = &last
	r.feed = mkFeed(t, false, "2|a", "3|b", "4|c")
	r.sink.failures = map[int]bool{1: true} // "b" fails
	if r.poll(t) != InProgress {
		t.Fatal("want InProgress")
	}
	eq(t, r.sink.bodies(), []string{"a"})
	if got := *r.poller.State.LastPlayID; got != 2 {
		t.Errorf("last play id = %d, want 2", got)
	}
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"a", "b", "c"})
	if got := *r.poller.State.LastPlayID; got != 4 {
		t.Errorf("last play id = %d, want 4", got)
	}
}

func TestUpdateSentBeforeNewPlayAndDoesNotAdvance(t *testing.T) {
	r := newRig(t, "nfl")
	r.feed = mkFeed(t, false, "10|touchdown|s")
	r.poll(t)
	if got := r.poller.State.ScoringTexts["10"]; got != "touchdown" {
		t.Fatalf("baseline = %q", got)
	}
	r.sink.events = nil
	r.feed = mkFeed(t, false, "10|touchdown (pat good)|s", "11|kickoff")
	r.trace.Reset()
	r.poller.Trace = &r.trace
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"touchdown (pat good)", "kickoff"})
	if r.sink.events[0].Kind != KindUpdate || r.sink.events[1].Kind != KindPlay {
		t.Errorf("kinds = %s, %s", r.sink.events[0].Kind, r.sink.events[1].Kind)
	}
	if !strings.Contains(r.trace.String(), "1 new play(s)") {
		t.Errorf("trace = %q", r.trace.String())
	}
	if got := r.poller.State.ScoringTexts["10"]; got != "touchdown (pat good)" {
		t.Errorf("baseline after update = %q", got)
	}
	// Only the update goes out when there is no new play, and it does not move last_play_id.
	r.sink.events = nil
	r.feed = mkFeed(t, false, "10|touchdown (pat good) again|s", "11|kickoff")
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"touchdown (pat good) again"})
	if got := *r.poller.State.LastPlayID; got != 11 {
		t.Errorf("last play id = %d", got)
	}
}

func TestScoringBaselineForPlaysNotSent(t *testing.T) {
	r := newRig(t, "nfl")
	last := PlayID(1)
	r.poller.State.LastPlayID = &last
	// 7 unseen plays, limit 5: play 2 is skipped but its text becomes the baseline.
	r.feed = mkFeed(t, false, "2|early td|s", "3|a", "4|b", "5|c", "6|d", "7|e", "8|f|s")
	r.poll(t)
	seen := r.poller.State.ScoringTexts
	if seen["2"] != "early td" || seen["8"] != "f" {
		t.Errorf("scoring texts = %v", seen)
	}
}

func TestFinalSentAfterPlaysAndRetried(t *testing.T) {
	r := newRig(t, "nfl")
	r.feed = mkFeed(t, true, "10|last play")
	r.sink.failures = map[int]bool{1: true} // the final event fails once
	if r.poll(t) != InProgress {
		t.Fatal("failed final must not finish the game")
	}
	eq(t, r.sink.bodies(), []string{"last play"})
	if r.poll(t) != Finished {
		t.Fatal("retry should finish the game")
	}
	eq(t, r.sink.bodies(), []string{"last play", "Game over"})
	f := r.sink.events[1]
	if f.Kind != KindFinal || !f.Urgent || f.Title != "AWY 3 · Final · HOM 7" {
		t.Errorf("final = %+v", f)
	}
}

func TestFinalNotSentWhilePlayFails(t *testing.T) {
	r := newRig(t, "nfl")
	r.feed = mkFeed(t, true, "10|last play")
	r.sink.failures = map[int]bool{0: true}
	if r.poll(t) != InProgress || r.sink.calls != 1 {
		t.Errorf("calls = %d", r.sink.calls)
	}
}

func TestThreeStateResult(t *testing.T) {
	r := newRig(t, "nfl")
	if got := r.poll(t); got != FetchFailed || got.String() != "None" {
		t.Errorf("fetch failure = %v", got)
	}
	r.feed = mkFeed(t, false, "1|a")
	if got := r.poll(t); got != InProgress || got.String() != "False" {
		t.Errorf("in progress = %v", got)
	}
	r.feed = mkFeed(t, true, "1|a")
	if got := r.poll(t); got != Finished || got.String() != "True" {
		t.Errorf("finished = %v", got)
	}
}

func TestEmptyFixtureSendsNothing(t *testing.T) {
	r := newRig(t, "nfl")
	r.feed = loadFixture(t, "playbyplay_empty.json")
	if got := r.poll(t); got != InProgress {
		t.Errorf("status = %v", got)
	}
	if r.sink.calls != 0 {
		t.Errorf("sent %d events", r.sink.calls)
	}
}

func TestNoPlaysFinalGame(t *testing.T) {
	r := newRig(t, "nfl")
	r.feed = mkFeed(t, true)
	if got := r.poll(t); got != Finished {
		t.Errorf("status = %v", got)
	}
	eq(t, r.sink.bodies(), []string{"Game over"})
}

func TestPregameSeconds(t *testing.T) {
	r := newRig(t, "nfl")
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	r.poller.Now = func() time.Time { return now }
	r.feed = mkFeed(t, false)
	espn.Status(r.feed)["type"] = espn.Data{"state": "pre", "shortDetail": "10/9 - 9:00 PM EDT"}
	out, err := r.poller.Poll(context.Background())
	if err != nil || out.Status != InProgress || out.UntilStart == nil || *out.UntilStart != time.Hour {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	// A game that is not "pre" has no pregame wait.
	r.feed = mkFeed(t, false)
	if out, _ = r.poller.Poll(context.Background()); out.UntilStart != nil {
		t.Errorf("until start = %v", *out.UntilStart)
	}
}

func TestOnceNeverWritesState(t *testing.T) {
	for _, once := range []bool{true, false} {
		p := PathsFromHome(t.TempDir())
		r := newRig(t, "nfl")
		r.poller.Store = NewStateStore(p)
		r.poller.Once = once
		r.feed = mkFeed(t, false, "1|a")
		r.poll(t)
		_, err := os.Stat(p.StateFile())
		if once && err == nil {
			t.Error("--once wrote state.json")
		}
		if !once && err != nil {
			t.Errorf("state.json not written: %v", err)
		}
	}
}

func TestEventFields(t *testing.T) {
	r := newRig(t, "cfb")
	r.feed = mkFeed(t, false, "10|Rush for 3 yards|s")
	r.poll(t)
	e := r.sink.events[0]
	if e.GameID != "999" || e.PlayID != "10" || e.Sport != "cfb" || e.PlayText != "Rush for 3 yards" ||
		e.URL != "https://www.espn.com/college-football/game/_/gameId/999" ||
		e.Away != (TeamScore{Abbr: "AWY", Score: "3"}) || e.Home != (TeamScore{Abbr: "HOM", Score: "7"}) ||
		e.Clock != "3:07" || e.Period != "2nd" || !e.Urgent || e.Title != "AWY 3 · 3:07 2nd · HOM 7" {
		t.Errorf("event = %+v", e)
	}
}

func TestStdoutSinkFormat(t *testing.T) {
	var buf bytes.Buffer
	err := StdoutSink{W: &buf}.Send(context.Background(), Event{Title: "T", Body: "B", URL: "http://x"})
	if err != nil || buf.String() != "[DRY-RUN] T\nB\nExec: xdg-open http://x\n" {
		t.Errorf("%q %v", buf.String(), err)
	}
}

func TestFootballUrgency(t *testing.T) {
	cases := []struct {
		p    espn.Data
		text string
		want bool
	}{
		{espn.Data{"scoringPlay": true}, "x", true},
		{espn.Data{"isTurnover": true}, "x", true},
		{espn.Data{"type": espn.Data{"text": "Kickoff"}}, "touchdown on the kickoff", false},
		{espn.Data{"type": espn.Data{"text": "Punt"}}, "fumble", false},
		{espn.Data{}, "Field Goal is NO GOOD", true},
		{espn.Data{}, "rush for 2 yards", false},
		{espn.Data{"scoringPlay": true, "type": espn.Data{"text": "Kickoff"}}, "x", true},
	}
	for _, c := range cases {
		if got := footballUrgent(c.p, c.text); got != c.want {
			t.Errorf("%v %q = %v", c.p, c.text, got)
		}
	}
}

func TestFormatTitle(t *testing.T) {
	if got := FormatTitle("ATL", "14", "0:48", "1st", "NO", "0"); got != "ATL 14 · 0:48 1st · NO 0" {
		t.Error(got)
	}
	if got := FormatTitle("ATL", "14", "", "", "NO", "0"); got != "ATL 14 · NO 0" {
		t.Error(got)
	}
	if got := FormatTitle("ATL", "14", "", "2nd", "NO", "0"); got != "ATL 14 · 2nd · NO 0" {
		t.Error(got)
	}
}

func TestUnimplementedSport(t *testing.T) {
	if _, err := RulesFor("curling"); err == nil || errors.Is(err, ErrNotImplemented) {
		t.Errorf("invalid sport: %v", err)
	}
	for _, s := range []string{"nfl", "cfb", "nba", "wnba", "cbb", "nhl", "mlb"} {
		if _, err := RulesFor(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestBackoff(t *testing.T) {
	iv := 10 * time.Second
	want := []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, MaxBackoff, MaxBackoff}
	for i, w := range want {
		if got := Backoff(iv, i+1); got != w {
			t.Errorf("failures %d: %v, want %v", i+1, got, w)
		}
	}
	if Backoff(iv, 1000) != MaxBackoff {
		t.Error("huge failure count must stay capped")
	}
}

func TestPregameDelay(t *testing.T) {
	iv := 10 * time.Second
	cases := []struct{ until, want time.Duration }{
		{3 * time.Hour, PregameMaxSleep},     // far away: one max chunk
		{40 * time.Minute, PregameMaxSleep},  // capped at one chunk
		{20 * time.Minute, 15 * time.Minute}, // wake 5 min before the start
		{PregameLead + 5*time.Second, iv},    // less than the interval: poll normally
		{-time.Minute, iv},                   // already past
		{PregameLead + 20*time.Second, 20 * time.Second},
	}
	for _, c := range cases {
		if got := PregameDelay(iv, c.until); got != c.want {
			t.Errorf("until %v: %v, want %v", c.until, got, c.want)
		}
	}
}

// loopRig runs a Loop over scripted outcomes, recording sleeps instead of waiting.
type loopRig struct {
	script   []Outcome
	errs     map[int]error
	polls    int
	sleeps   []time.Duration
	finished int
	cancelAt int // cancel the context after this many sleeps (0 = never)
	cancel   context.CancelFunc
}

func (r *loopRig) run(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	defer cancel()
	l := &Loop{
		GameID:   "999",
		Interval: 10 * time.Second,
		Poll: func(context.Context) (Outcome, error) {
			i := r.polls
			r.polls++
			if err := r.errs[i]; err != nil {
				return Outcome{}, err
			}
			return r.script[i], nil
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			r.sleeps = append(r.sleeps, d)
			if r.cancelAt > 0 && len(r.sleeps) >= r.cancelAt {
				r.cancel()
			}
			return ctx.Err()
		},
		OnFinished: func() { r.finished++ },
	}
	if err := l.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLoopBackoffAndReset(t *testing.T) {
	s := time.Second
	r := &loopRig{script: []Outcome{
		{Status: FetchFailed}, {Status: FetchFailed}, {Status: FetchFailed},
		{Status: InProgress},
		{Status: FetchFailed},
		{Status: Finished},
	}}
	r.run(t)
	want := []time.Duration{20 * s, 40 * s, 80 * s, 10 * s, 20 * s}
	if fmt.Sprint(r.sleeps) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps, want)
	}
	if r.finished != 1 || r.polls != 6 {
		t.Errorf("finished %d, polls %d", r.finished, r.polls)
	}
}

func TestLoopBackoffCap(t *testing.T) {
	var script []Outcome
	for i := 0; i < 8; i++ {
		script = append(script, Outcome{Status: FetchFailed})
	}
	script = append(script, Outcome{Status: Finished})
	r := &loopRig{script: script}
	r.run(t)
	if last := r.sleeps[len(r.sleeps)-1]; last != MaxBackoff {
		t.Errorf("last sleep = %v", last)
	}
	for _, d := range r.sleeps {
		if d > MaxBackoff {
			t.Errorf("sleep %v over the cap", d)
		}
	}
}

func TestLoopPregameSleep(t *testing.T) {
	d := func(x time.Duration) *time.Duration { return &x }
	r := &loopRig{script: []Outcome{
		{Status: InProgress, UntilStart: d(3 * time.Hour)},
		{Status: InProgress, UntilStart: d(90 * time.Minute)},
		{Status: InProgress, UntilStart: d(PregameLead + 5*time.Second)},
		{Status: InProgress},
		{Status: Finished},
	}}
	r.run(t)
	want := []time.Duration{PregameMaxSleep, PregameMaxSleep, 10 * time.Second, 10 * time.Second}
	if fmt.Sprint(r.sleeps) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps, want)
	}
}

func TestLoopUnexpectedErrorContinues(t *testing.T) {
	r := &loopRig{
		script: []Outcome{{}, {Status: Finished}},
		errs:   map[int]error{0: errors.New("bad play id")},
	}
	r.run(t)
	if r.polls != 2 || r.finished != 1 || fmt.Sprint(r.sleeps) != "[20s]" {
		t.Errorf("polls %d finished %d sleeps %v", r.polls, r.finished, r.sleeps)
	}
}

func TestLoopSurvivesPanic(t *testing.T) {
	n := 0
	l := &Loop{
		Interval: 10 * time.Second,
		Poll: func(context.Context) (Outcome, error) {
			n++
			if n == 1 {
				panic("oops")
			}
			return Outcome{Status: Finished}, nil
		},
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	if err := l.Run(context.Background()); err != nil || n != 2 {
		t.Errorf("n = %d, err = %v", n, err)
	}
}

func TestLoopStopsOnCancel(t *testing.T) {
	var script []Outcome
	for i := 0; i < 10; i++ {
		script = append(script, Outcome{Status: InProgress})
	}
	r := &loopRig{script: script, cancelAt: 3}
	r.run(t)
	if r.polls != 3 || r.finished != 0 {
		t.Errorf("polls %d finished %d", r.polls, r.finished)
	}
	// An already cancelled context never polls.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := &Loop{Poll: func(context.Context) (Outcome, error) { t.Error("polled"); return Outcome{}, nil }}
	if err := l.Run(ctx); err != nil {
		t.Error(err)
	}
}

func TestSleepContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepContext(ctx, time.Hour); err == nil || time.Since(start) > time.Second {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}
}
