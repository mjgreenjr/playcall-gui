package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

func TestLogTextChanges(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	p := &Poller{State: &State{GameID: "g"}}
	play := func(text string) []espn.Data { return []espn.Data{{"id": "7", "text": text}} }
	p.logTextChanges(play("foul"))
	p.logTextChanges(play("foul"))
	if buf.Len() != 0 {
		t.Fatalf("unchanged text logged: %s", buf.String())
	}
	p.logTextChanges(play("Smith personal foul"))
	out := buf.String()
	for _, want := range []string{"Play text changed", "old=foul", `new="Smith personal foul"`, "play=7"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
}

func TestRevisablePlay(t *testing.T) {
	typ := func(s string) espn.Data { return espn.Data{"type": map[string]any{"text": s}} }
	for _, c := range []struct {
		sport string
		play  espn.Data
		want  bool
	}{
		{"nba", typ("Substitution"), true},
		{"wnba", typ("Personal Foul"), true},
		{"nba", typ("Offensive Foul Turnover"), false},
		{"nba", typ("Pullup Jump Shot"), false},
		{"nhl", espn.Data{"type": map[string]any{"text": "Penalty", "penaltyMinutes": "2"}}, true},
		{"nhl", typ("Shot"), true},
		{"nhl", typ("Missed"), true},
		{"nhl", typ("Blocked"), true},
		{"nhl", typ("Takeaway"), false},
		{"mlb", typ("Substitution"), false},
		{"mlb", typ("Start Batter/Pitcher"), true},
		{"mlb", typ("Ball"), false},
		{"nfl", typ("Rush"), true},
		{"cfb", typ("Pass Reception"), true},
	} {
		if got := revisablePlay(c.sport, c.play); got != c.want {
			t.Errorf("%s %v: got %v", c.sport, c.play["type"], got)
		}
	}
}

// A foul that ESPN first posts as a bare "foul" and fills in later is re-sent
// as an update once; a shot whose description changes is not.
func TestRevisedFoulIsResent(t *testing.T) {
	data := loadFixture(t, "playbyplay_nba_multi_plays.json")
	plays := espn.ExtractPlays(data)
	foul := -1
	for i, p := range plays {
		if strings.Contains(strings.ToLower(playType(p)), "foul") && !strings.Contains(strings.ToLower(playType(p)), "turnover") {
			foul = i
			break
		}
	}
	if foul < 0 {
		t.Fatal("no foul in the fixture")
	}
	first := reveal(t, data, foul+1, false)
	edited := deepCopy(t, first)
	var id string
	for _, h := range espn.PlayHolders(edited) {
		for _, v := range h["plays"].([]any) {
			p := espn.Map(v)
			if espn.PlayKey(p) == espn.PlayKey(plays[foul]) {
				id = espn.PlayKey(p)
				p["text"] = "Edited name personal foul"
			}
		}
	}
	feed := first
	sink := &recordSink{}
	poller, err := NewPoller(&State{GameID: "999", Sport: "nba"}, sink, func(context.Context) (espn.Data, error) { return feed, nil })
	if err != nil {
		t.Fatal(err)
	}
	poll := func() {
		t.Helper()
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	if len(sink.events) != 1 || sink.events[0].Kind != KindPlay {
		t.Fatalf("first poll: %+v", sink.events)
	}
	poll() // nothing changed
	if len(sink.events) != 1 {
		t.Fatalf("unchanged poll sent %d events", len(sink.events))
	}
	feed = edited
	poll()
	if len(sink.events) != 2 || sink.events[1].Kind != KindUpdate || !sink.events[1].Revision || sink.events[1].PlayID != id ||
		!strings.Contains(sink.events[1].Body, "Edited name personal foul") {
		t.Fatalf("after the edit: %+v", sink.events)
	}
	poll() // the edit is sent once
	if len(sink.events) != 2 {
		t.Errorf("edit re-sent: %d events", len(sink.events))
	}
}

// ESPN's football ids are not always in play order: a timeout can carry a
// higher id than the snap after it. A play that shows up with an id below
// last_play_id is still new, and is sent once without moving last_play_id back.
func TestFootballLatePlayWithALowerIDIsSent(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	plays := espn.ExtractPlays(data)
	maxID := int64(0)
	for _, p := range plays {
		if id, _ := espn.PlayID(p); id > maxID {
			maxID = id
		}
	}
	late := deepCopy(t, data)
	holders := espn.PlayHolders(late)
	clone := deepCopy(t, espn.Data(plays[0]))
	clone["id"] = fmt.Sprint(maxID - 1)
	clone["text"] = "The late snap"
	holders[len(holders)-1]["plays"] = append(holders[len(holders)-1]["plays"].([]any), map[string]any(clone))

	feed := data
	sink := &recordSink{}
	state := &State{GameID: "999", Sport: "nfl"}
	poller, err := NewPoller(state, sink, func(context.Context) (espn.Data, error) { return feed, nil })
	if err != nil {
		t.Fatal(err)
	}
	poll := func() {
		t.Helper()
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	poll() // the first poll announces the newest play
	sent := len(sink.events)
	lastBefore := *state.LastPlayID
	feed = late
	poll()
	if len(sink.events) != sent+1 || !strings.Contains(sink.events[sent].Body, "The late snap") {
		t.Fatalf("late play not sent: %+v", sink.events[sent:])
	}
	if *state.LastPlayID != lastBefore {
		t.Errorf("last_play_id moved from %d to %d", lastBefore, *state.LastPlayID)
	}
	poll()
	poll()
	if len(sink.events) != sent+1 {
		t.Errorf("late play sent again: %d events", len(sink.events)-sent)
	}
}

// After a restart nothing at or below last_play_id is treated as late.
func TestFootballRestartDoesNotReplayOldPlays(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	plays := espn.ExtractPlays(data)
	maxID := int64(0)
	for _, p := range plays {
		if id, _ := espn.PlayID(p); id > maxID {
			maxID = id
		}
	}
	last := PlayID(maxID)
	sink := &recordSink{}
	poller, err := NewPoller(&State{GameID: "999", Sport: "nfl", LastPlayID: &last}, sink, func(context.Context) (espn.Data, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.events) != 0 {
		t.Errorf("resumed game replayed %d plays", len(sink.events))
	}
}

// Seen in NLCS game 2: ESPN posted "Vesia relieved Yamamoto" after the next
// batter's "pitches to" play, under a lower id, so it was never announced.
func TestMLBLatePlayWithALowerIDIsSent(t *testing.T) {
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	var late espn.Data
	at := 0
	for i, p := range espn.ExtractPlays(data) {
		if strings.Contains(espn.PlayText(p), "Iglesias relieved Suter") {
			late, at = p, i
		}
	}
	if late == nil {
		t.Fatal("no relief play in the fixture")
	}
	data = reveal(t, data, at+20, false) // some plays after the relief
	lateKey := espn.PlayKey(late)
	without := deepCopy(t, data)
	for _, h := range espn.PlayHolders(without) {
		var kept []any
		for _, v := range h["plays"].([]any) {
			if espn.PlayKey(espn.Map(v)) != lateKey {
				kept = append(kept, v)
			}
		}
		h["plays"] = kept
	}
	feed := without
	sink := &recordSink{}
	state := &State{GameID: "999", Sport: "mlb"}
	poller, err := NewPoller(state, sink, func(context.Context) (espn.Data, error) { return feed, nil })
	if err != nil {
		t.Fatal(err)
	}
	poll := func() {
		t.Helper()
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	poll()
	sent := len(sink.events)
	lastBefore := *state.LastPlayID
	feed = data
	poll()
	if len(sink.events) != sent+1 || !strings.Contains(sink.events[sent].Body, "relieved") {
		t.Fatalf("late relief not sent: %+v", sink.events[sent:])
	}
	if *state.LastPlayID != lastBefore {
		t.Errorf("last_play_id moved from %d to %d", lastBefore, *state.LastPlayID)
	}
	poll()
	poll()
	if len(sink.events) != sent+1 {
		t.Errorf("late play sent again: %d events", len(sink.events)-sent)
	}
}

// A "pitches to" play whose pitcher ESPN rewrites after a pitching change is
// corrected in place.
func TestMLBRevisedStartBatterIsResent(t *testing.T) {
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	all := espn.ExtractPlays(data)
	n := 0
	for i, p := range all {
		if i > 40 && playType(p) == "Start Batter/Pitcher" {
			n = i + 1
			break
		}
	}
	first := reveal(t, data, n, false)
	edited := deepCopy(t, first)
	var key string
	for _, h := range espn.PlayHolders(edited) {
		vs := h["plays"].([]any)
		p := espn.Map(vs[len(vs)-1])
		key = espn.PlayKey(p)
		p["text"] = "Alex Vesia pitches to Garrett Mitchell"
	}
	feed := first
	sink := &recordSink{}
	poller, err := NewPoller(&State{GameID: "999", Sport: "mlb"}, sink, func(context.Context) (espn.Data, error) { return feed, nil })
	if err != nil {
		t.Fatal(err)
	}
	poll := func() {
		t.Helper()
		if _, err := poller.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	if len(sink.events) != 1 {
		t.Fatalf("first poll: %+v", sink.events)
	}
	feed = edited
	poll()
	if len(sink.events) != 2 || !sink.events[1].Revision || sink.events[1].PlayID != key || !strings.Contains(sink.events[1].Body, "Alex Vesia") {
		t.Fatalf("after the edit: %+v", sink.events[1:])
	}
	poll()
	if len(sink.events) != 2 {
		t.Errorf("edit re-sent: %d events", len(sink.events))
	}
}

func editPlayText(t *testing.T, d espn.Data, key, text string) espn.Data {
	t.Helper()
	c := deepCopy(t, d)
	n := 0
	for _, h := range espn.PlayHolders(c) {
		for _, v := range h["plays"].([]any) {
			if p := espn.Map(v); espn.PlayKey(p) == key {
				p["text"] = text
				n++
			}
		}
	}
	if n == 0 {
		t.Fatalf("play %s not found", key)
	}
	return c
}

// ESPN rewords old scoring plays long after the fact (the same error call
// flipped back and forth an hour later): that is a quiet correction. An extra
// point added to a touchdown a minute later is still an update.
func TestOldScoringPlayChangesAreQuietRevisions(t *testing.T) {
	data := loadFixture(t, "playbyplay_mlb_multi_plays.json")
	typ := espn.Map(espn.Status(data)["type"])
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	all := espn.ExtractPlays(data)
	var scoring []int
	for i, p := range all {
		if espn.Truthy(p["scoringPlay"]) && i > 100 {
			scoring = append(scoring, i)
		}
	}
	if len(scoring) < 2 {
		t.Fatal("need two scoring plays")
	}
	old := all[scoring[0]]
	for _, c := range []struct {
		name       string
		reveal     int
		play       espn.Data
		wantRevise bool
	}{
		{"hours old", len(all), old, true},
		{"just happened", scoring[0] + 3, old, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			first := reveal(t, data, c.reveal, false)
			edited := editPlayText(t, first, espn.PlayKey(c.play), espn.PlayText(c.play)+" (changed)")
			feed := first
			sink := &recordSink{}
			poller, err := NewPoller(&State{GameID: "999", Sport: "mlb"}, sink, func(context.Context) (espn.Data, error) { return feed, nil })
			if err != nil {
				t.Fatal(err)
			}
			poll := func() {
				t.Helper()
				if _, err := poller.Poll(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			poll()
			sent := len(sink.events)
			feed = edited
			poll()
			if len(sink.events) != sent+1 {
				t.Fatalf("after the edit: %d new events", len(sink.events)-sent)
			}
			ev := sink.events[sent]
			if ev.Kind != KindUpdate || ev.Revision != c.wantRevise {
				t.Errorf("kind %q, revision %v; want an update with revision %v", ev.Kind, ev.Revision, c.wantRevise)
			}
		})
	}
}

func TestStaleScoringPlay(t *testing.T) {
	at := func(s string) espn.Data { return espn.Data{"wallclock": s} }
	plays := []espn.Data{at("2026-10-12T22:00:00Z"), at("2026-10-12T22:30:00Z")}
	if !stalePlay(plays, plays[0]) {
		t.Error("30 minutes older should be stale")
	}
	if !stalePlay(plays, at("2026-10-12T22:24:00Z")) {
		t.Error("6 minutes older should be stale")
	}
	if stalePlay(plays, at("2026-10-12T22:26:00Z")) {
		t.Error("4 minutes older, same period, is not stale")
	}
	if stalePlay(plays, espn.Data{}) || stalePlay([]espn.Data{{}}, plays[0]) {
		t.Error("no timestamp is never stale")
	}
}

// ESPN corrects a football play's text for a few minutes ("enforced at BUF 24"
// became "BUF 25"). A recent play's row is rewritten once and quietly; an old
// play's correction is ignored.
func TestFootballRecentPlayTextCorrectionIsAQuietRevision(t *testing.T) {
	data := loadFixture(t, "playbyplay_multi_plays.json")
	run := func(t *testing.T, editedIndex int) []Event {
		t.Helper()
		all := espn.ExtractPlays(data)
		first := reveal(t, data, 3, false) // plays 0-2, the last at 00:18
		next := reveal(t, data, 5, false)  // play 3 is a touchdown, play 4 an hour later
		edited := editPlayText(t, next, espn.PlayKey(all[editedIndex]), espn.PlayText(all[editedIndex])+" (corrected)")
		feed := first
		sink := &recordSink{}
		poller, err := NewPoller(&State{GameID: "999", Sport: "nfl"}, sink, func(context.Context) (espn.Data, error) { return feed, nil })
		if err != nil {
			t.Fatal(err)
		}
		poll := func() {
			t.Helper()
			if _, err := poller.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		poll() // announces play 2 (the newest)
		feed = next
		poll() // announces plays 3 and 4
		sent := len(sink.events)
		feed = edited
		poll()
		poll() // the correction goes out once
		return sink.events[sent:]
	}
	t.Run("the newest play", func(t *testing.T) {
		got := run(t, 4)
		if len(got) != 1 || got[0].Kind != KindUpdate || !got[0].Revision || !strings.Contains(got[0].Body, "(corrected)") {
			t.Fatalf("events after the correction: %+v", got)
		}
	})
	t.Run("a play an hour older", func(t *testing.T) {
		if got := run(t, 2); len(got) != 0 {
			t.Fatalf("an old play's correction was sent: %+v", got)
		}
	})
}

// Older than a minute and the game has moved to a later period or half-inning:
// stale even inside the 5 minutes. Right after a period ends it is not.
func TestStalePlayWhenThePeriodMovedOn(t *testing.T) {
	play := func(wc string, period int, half string) espn.Data {
		return espn.Data{"wallclock": wc, "period": map[string]any{"number": json.Number(fmt.Sprint(period)), "type": half}}
	}
	td := play("2026-10-12T22:00:00Z", 1, "")
	if !stalePlay([]espn.Data{td, play("2026-10-12T22:03:00Z", 2, "")}, td) {
		t.Error("3 minutes later in the next quarter should be stale")
	}
	if stalePlay([]espn.Data{td, play("2026-10-12T22:00:30Z", 2, "")}, td) {
		t.Error("30 seconds later in the next quarter is not stale: the extra point may still be coming")
	}
	if stalePlay([]espn.Data{td, play("2026-10-12T22:03:00Z", 1, "")}, td) {
		t.Error("3 minutes later in the same quarter is not stale")
	}
	top := play("2026-10-12T22:00:00Z", 5, "Top")
	if !stalePlay([]espn.Data{top, play("2026-10-12T22:04:00Z", 5, "Bottom")}, top) {
		t.Error("the bottom of the inning should make the top stale")
	}
}
