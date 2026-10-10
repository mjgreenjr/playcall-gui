package core

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"playcall/internal/espn"
)

// bbFeed builds a flat summary from play JSON objects.
func bbFeed(t *testing.T, final bool, plays ...string) espn.Data {
	t.Helper()
	status := `{"type":{"state":"in","shortDetail":"3:07 - 2nd","name":"STATUS_IN_PROGRESS"}}`
	if final {
		status = `{"type":{"state":"post","shortDetail":"Final","name":"STATUS_FINAL"}}`
	}
	js := fmt.Sprintf(`{"header":{"competitions":[{"competitors":[
		{"homeAway":"home","score":"70","team":{"abbreviation":"HOM"}},
		{"homeAway":"away","score":"65","team":{"abbreviation":"AWY"}}]}]},
		"status":%s,"plays":[%s]}`, status, strings.Join(plays, ","))
	d, err := espn.Decode([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func play(id, typ, text string, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"id":%q,"type":{"text":%q},"text":%q,"period":{"number":2},"clock":{"displayValue":"5:00"}%s}`, id, typ, text, extra)
}

func miss(id, text string) string {
	return play(id, "Jumper", text, `"shootingPlay":true,"scoringPlay":false`)
}
func rebound(id, text string) string { return play(id, "Defensive Rebound", text, "") }
func other(id, text string) string   { return play(id, "Substitution", text, "") }

func TestBasketballRulesFor(t *testing.T) {
	for _, s := range []string{"nba", "wnba", "cbb"} {
		r, err := RulesFor(s)
		if err != nil {
			t.Fatal(err)
		}
		if !r.TrackSent() || r.CatchupLimit() != 10 {
			t.Errorf("%s: TrackSent %v, limit %d", s, r.TrackSent(), r.CatchupLimit())
		}
	}
	if r, _ := RulesFor("cbb"); r.(basketballRules).regulation != 2 {
		t.Error("cbb regulation is not 2")
	}
	if r, _ := RulesFor("wnba"); r.(basketballRules).regulation != 4 {
		t.Error("wnba regulation is not 4")
	}
}

func TestNBAPeriodNames(t *testing.T) {
	mk := func(js string) espn.Data {
		d, err := espn.Decode([]byte(js))
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	cases := []struct {
		reg  int
		js   string
		want string
	}{
		{4, `{"period":{"number":1}}`, "1st"},
		{4, `{"period":{"number":2}}`, "2nd"},
		{4, `{"period":{"number":3}}`, "3rd"},
		{4, `{"period":{"number":4}}`, "4th"},
		{4, `{"period":{"number":5}}`, "OT"},
		{4, `{"period":{"number":6}}`, "2OT"},
		{4, `{"period":{"number":8}}`, "4OT"},
		{2, `{"period":{"number":1}}`, "1st"},
		{2, `{"period":{"number":2}}`, "2nd"},
		{2, `{"period":{"number":3}}`, "OT"},
		{2, `{"period":{"number":4}}`, "2OT"},
		{4, `{"period":{"displayValue":"Halftime"}}`, "Halftime"},
		{4, `{"period":{"number":0,"displayValue":"x"}}`, "x"},
		{4, `{}`, ""},
	}
	for _, c := range cases {
		if got := nbaPeriod(mk(c.js), c.reg); got != c.want {
			t.Errorf("reg %d %s: %q, want %q", c.reg, c.js, got, c.want)
		}
	}
}

func TestNBAUrgency(t *testing.T) {
	cases := []struct {
		js   string
		want bool
	}{
		{`{"type":{"text":"Three Point Jump Shot"},"scoringPlay":true,"scoreValue":3}`, true},
		{`{"type":{"text":"Jump Shot"},"scoringPlay":true,"scoreValue":2}`, false},
		{`{"type":{"text":"Free Throw"},"scoringPlay":true,"scoreValue":1}`, false},
		{`{"type":{"text":"Three Point Jump Shot"},"shootingPlay":true,"scoringPlay":false,"scoreValue":3}`, false},
		{`{"type":{"text":"Personal Foul"}}`, true},
		{`{"type":{"text":"Shooting Foul"}}`, true},
		{`{"type":{"text":"Offensive Foul Turnover"}}`, false},
		{`{"type":{"text":"Substitution"}}`, false},
		{`{}`, false},
	}
	for _, c := range cases {
		d, _ := espn.Decode([]byte(c.js))
		if got := nbaUrgent(d); got != c.want {
			t.Errorf("%s: %v, want %v", c.js, got, c.want)
		}
	}
}

func TestBasketballMessage(t *testing.T) {
	r := newRig(t, "nba")
	r.feed = bbFeed(t, false,
		play("1", "Substitution", "Smith enters\nthe game for  Jones", `"awayScore":10,"homeScore":0`),
		play("2", "Jumper", "no scores here", ""))
	r.poll(t)
	// First poll announces only the newest play: the game-level scores and no per-play ones.
	if len(r.sink.events) != 1 {
		t.Fatal(r.sink.events)
	}
	if got := r.sink.events[0].Title; got != "AWY 65 · 5:00 2nd · HOM 70" {
		t.Errorf("fallback title %q", got)
	}
	r = newRig(t, "nba")
	r.feed = bbFeed(t, false, play("1", "Substitution", "Smith enters\nthe game for  Jones", `"awayScore":10,"homeScore":0`))
	r.poll(t)
	e := r.sink.events[0]
	if e.Title != "AWY 10 · 5:00 2nd · HOM 0" || e.Body != "Smith enters the game for Jones" || e.Urgent {
		t.Errorf("%+v", e)
	}
	if e.Clock != "5:00" || e.Period != "2nd" || e.Away.Score != "10" || e.Home.Score != "0" {
		t.Errorf("structured fields %+v", e)
	}
}

func TestBundleMissThenRebound(t *testing.T) {
	r := newRig(t, "nba")
	r.feed = bbFeed(t, false, other("1", "sub"), miss("2", "Smith misses\na jumper"), rebound("3", "Jones rebound"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"Smith misses a jumper · Jones rebound"})
	e := r.sink.events[0]
	if e.PlayID != "3" || e.Urgent {
		t.Errorf("%+v", e)
	}
	// Both ids are marked sent together; the earlier play was skipped (also sent).
	eq(t, r.poller.State.SentIDs, []string{"1", "2", "3"})
}

func TestBundledToastIsLowEvenForFoulRebound(t *testing.T) {
	r := newRig(t, "nba")
	r.feed = bbFeed(t, false, miss("1", "miss"), play("2", "Offensive Rebound Foul", "rebound foul", ""))
	r.poll(t)
	// "Rebound" in the type makes it a rebound, so it bundles; a foul-typed rebound is still low.
	if len(r.sink.events) != 1 || r.sink.events[0].Urgent {
		t.Errorf("bundled toast should be low: %+v", r.sink.events)
	}
}

func TestMissThenNonReboundGoesAlone(t *testing.T) {
	r := newRig(t, "nba")
	lp := PlayID(0)
	r.poller.State.LastPlayID = &lp
	r.feed = bbFeed(t, false, miss("1", "miss"), other("2", "sub"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"miss", "sub"})
}

func TestNewestMissHeldUntilNextPollOrFinal(t *testing.T) {
	r := newRig(t, "nba")
	r.feed = bbFeed(t, false, other("1", "sub"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"sub"})

	r.feed = bbFeed(t, false, other("1", "sub"), miss("2", "miss"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"sub"}) // held: nothing new
	if got := r.poller.State.SentIDs; !reflect.DeepEqual(got, []string{"1"}) {
		t.Errorf("held miss was marked sent: %v", got)
	}

	r.feed = bbFeed(t, false, other("1", "sub"), miss("2", "miss"), rebound("3", "board"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"sub", "miss · board"})

	// Released when the game is final.
	r.feed = bbFeed(t, true, other("1", "sub"), miss("2", "miss"), rebound("3", "board"), miss("4", "last miss"))
	if r.poll(t) != Finished {
		t.Error("game should be finished")
	}
	eq(t, r.sink.bodies(), []string{"sub", "miss · board", "last miss", "Game over"})
}

func TestReboundWhoseMissWasAlreadySent(t *testing.T) {
	r := newRig(t, "nba")
	lp := PlayID(1)
	r.poller.State.LastPlayID = &lp
	r.poller.State.SentIDs = []string{"1", "2"}
	r.feed = bbFeed(t, false, other("1", "sub"), miss("2", "miss"), rebound("3", "board"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"board"})
	eq(t, r.poller.State.SentIDs, []string{"1", "2", "3"})
}

func TestSilentTypeNeverAnnouncedAndNeverMarkedSent(t *testing.T) {
	r := newRig(t, "nba")
	lp := PlayID(1)
	r.poller.State.LastPlayID = &lp
	r.poller.State.SentIDs = []string{"1"}
	r.feed = bbFeed(t, false, other("1", "sub"), play("2", "Offensive Foul Turnover", "dup", ""), other("3", "next"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"next"})
	eq(t, r.poller.State.SentIDs, []string{"1", "3"})
}

func TestIDsOutOfOrderUseListOrder(t *testing.T) {
	r := newRig(t, "nba")
	lp := PlayID(1)
	r.poller.State.LastPlayID = &lp
	r.poller.State.SentIDs = []string{"500"}
	// The second play has a smaller id than the last sent play: still new.
	r.feed = bbFeed(t, false, other("500", "old"), other("40", "late insert"), other("900", "after"))
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"late insert", "after"})
	// Sorted as strings, like Python's sorted().
	eq(t, r.poller.State.SentIDs, []string{"40", "500", "900"})
	if *r.poller.State.LastPlayID != 900 {
		t.Errorf("last_play_id %d", *r.poller.State.LastPlayID)
	}
}

func TestSentIDsSortedAsStrings(t *testing.T) {
	r := newRig(t, "nba")
	lp := PlayID(1)
	r.poller.State.LastPlayID = &lp
	r.feed = bbFeed(t, false, other("9", "a"), other("10", "b"), other("100", "c"))
	r.poll(t)
	eq(t, r.poller.State.SentIDs, []string{"10", "100", "9"})
}

func TestCatchupLimitSkipsAndMarksSent(t *testing.T) {
	for _, sport := range []string{"nba", "wnba", "cbb"} {
		r := newRig(t, sport)
		lp := PlayID(0)
		r.poller.State.LastPlayID = &lp
		var plays []string
		for i := 1; i <= 13; i++ {
			plays = append(plays, other(fmt.Sprint(i), fmt.Sprint("p", i)))
		}
		r.feed = bbFeed(t, false, plays...)
		r.poll(t)
		if len(r.sink.events) != 10 || r.sink.events[0].Body != "p4" {
			t.Errorf("%s: %d events, first %q", sport, len(r.sink.events), r.sink.bodies())
		}
		if len(r.poller.State.SentIDs) != 13 {
			t.Errorf("%s: sent ids %v", sport, r.poller.State.SentIDs)
		}
		// Nothing left on the next poll.
		n := len(r.sink.events)
		r.poll(t)
		if len(r.sink.events) != n {
			t.Errorf("%s: re-sent plays", sport)
		}
	}
}

func TestSkippedBundleMarksBothIDs(t *testing.T) {
	r := newRig(t, "nba")
	lp := PlayID(0)
	r.poller.State.LastPlayID = &lp
	plays := []string{miss("1", "miss"), rebound("2", "board")}
	for i := 3; i <= 12; i++ {
		plays = append(plays, other(fmt.Sprint(i), "x"))
	}
	r.feed = bbFeed(t, false, plays...)
	r.poll(t)
	if len(r.sink.events) != 10 {
		t.Fatalf("%d events", len(r.sink.events))
	}
	if got := r.poller.State.SentIDs; len(got) != 12 {
		t.Errorf("sent ids %v", got)
	}
}

func TestFailedSendLeavesBundleUnsentAndRetries(t *testing.T) {
	r := newRig(t, "nba")
	r.sink.failures = map[int]bool{0: true}
	r.feed = bbFeed(t, false, other("1", "sub"), miss("2", "miss"), rebound("3", "board"))
	r.poll(t)
	if len(r.sink.events) != 0 || len(r.poller.State.SentIDs) != 1 {
		// "1" was skipped (first poll announces only the newest) and is marked sent.
		t.Fatalf("events %v, sent %v", r.sink.bodies(), r.poller.State.SentIDs)
	}
	eq(t, r.poller.State.SentIDs, []string{"1"})
	r.poll(t)
	eq(t, r.sink.bodies(), []string{"miss · board"})
	eq(t, r.poller.State.SentIDs, []string{"1", "2", "3"})
}

func TestSentIDsSavedInStateJSON(t *testing.T) {
	st := &State{GameID: "1", Sport: "nba", SentIDs: []string{"10", "9"}}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"sent_ids":["10","9"]`) {
		t.Errorf("%s", b)
	}
}

func TestOnceDoesNotWriteBasketballState(t *testing.T) {
	r := newRig(t, "nba")
	paths := PathsFromHome(t.TempDir())
	r.poller.Once = true
	r.poller.Store = NewStateStore(paths)
	r.feed = bbFeed(t, false, other("1", "sub"))
	r.poll(t)
	if _, err := os.Stat(paths.StateFile()); err == nil {
		t.Error("--once wrote state.json")
	}
	r2 := newRig(t, "nba")
	r2.poller.Store = NewStateStore(paths)
	r2.feed = r.feed
	r2.poll(t)
	if _, err := os.Stat(paths.StateFile()); err != nil {
		t.Errorf("normal poll did not write state: %v", err)
	}
}

func TestTeamTurnoverNamesTheTeam(t *testing.T) {
	data := espn.Data{"header": map[string]any{"competitions": []any{map[string]any{"competitors": []any{
		map[string]any{"homeAway": "home", "id": "129689", "team": map[string]any{"abbreviation": "GS"}},
		map[string]any{"homeAway": "away", "id": "17", "team": map[string]any{"abbreviation": "LV"}},
	}}}}}
	rules := basketballPlays{f: &Feed{Data: data}, regulation: 4}
	play := func(kind, text, team string, participants []any) espn.Data {
		return espn.Data{
			"id": "1", "text": text, "type": map[string]any{"text": kind},
			"team": map[string]any{"id": team}, "participants": participants,
		}
	}
	player := []any{map[string]any{"athlete": map[string]any{"id": "9"}}}

	cases := []struct {
		name string
		p    espn.Data
		want string
	}{
		{"shot clock, no player", play("Shot Clock Turnover", "shot clock turnover ", "129689", nil), "shot clock turnover (GS)"},
		{"other team", play("Shot Clock Turnover", "shot clock turnover", "17", nil), "shot clock turnover (LV)"},
		{"empty participants list", play("Shot Clock Turnover", "shot clock turnover", "17", []any{}), "shot clock turnover (LV)"},
		{"a player is named: untouched", play("Traveling", "Isobel Borlase traveling turnover", "17", player), "Isobel Borlase traveling turnover"},
		{"a steal is named: untouched", play("Lost Ball Turnover", "A lost ball turnover (B steals)", "17", player), "A lost ball turnover (B steals)"},
		{"unknown team id: untouched", play("Shot Clock Turnover", "shot clock turnover", "999", nil), "shot clock turnover"},
		{"no team id: untouched", play("Shot Clock Turnover", "shot clock turnover", "", nil), "shot clock turnover"},
		{"not a turnover: untouched", play("Official Timeout", "Official timeout", "17", nil), "Official timeout"},
		{"type with a newline", play("Team\nTurnover", "team turnover", "17", nil), "team turnover (LV)"},
	}
	for _, c := range cases {
		if got := rules.Message(c.p).Body; got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
