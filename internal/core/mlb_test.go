package core

import (
	"fmt"
	"strings"
	"testing"

	"playcall/internal/espn"
)

// mp builds an MLB play: id, type.type, text, and extra JSON fields.
func mp(t *testing.T, id, typ, text, extra string) espn.Data {
	t.Helper()
	if extra != "" {
		extra = "," + extra
	}
	return nhlData(t, fmt.Sprintf(`{"id":%q,"type":{"type":%q},"text":%q%s}`, id, typ, text, extra))
}

const top6 = `"period":{"type":"Top","number":6}`

// ctxOf runs mlbContexts over plays and returns the context of one id.
func ctxsOf(plays ...espn.Data) map[string]mlbContext { return mlbContexts(plays) }

func TestMLBRulesFor(t *testing.T) {
	r, err := RulesFor("mlb")
	if err != nil {
		t.Fatal(err)
	}
	if r.TrackSent() || r.CatchupLimit() != 5 {
		t.Errorf("TrackSent %v, limit %d", r.TrackSent(), r.CatchupLimit())
	}
}

func TestMLBStatus(t *testing.T) {
	two, zero := 2, 0
	if got := mlbStatus("Top 6", 1, &two, &[2]int{1, 2}); got != "Top 6 · 1-2, 1 out, 2 on" {
		t.Error(got)
	}
	if got := mlbStatus("Top 6", 0, &zero, &[2]int{0, 0}); got != "Top 6 · 0-0, 0 outs, 0 on" {
		t.Error(got)
	}
	if got := mlbStatus("Top 6", 3, nil, nil); got != "Top 6 · 3 outs" {
		t.Error(got)
	}
}

func TestMLBPitchesAndOutsTracking(t *testing.T) {
	c := ctxsOf(
		mp(t, "100", "start-batterpitcher", "A pitches to B", top6+`,"onFirst":{"x":1}`),
		// ESPN's outs (1) is the count AFTER the at-bat; the pitch shows the tracked 0.
		mp(t, "101", "pitch", "Pitch 1 : Strike 2 Foul", top6+`,"outs":1,"onFirst":{"x":1},"resultCount":{"balls":1,"strikes":2}`),
		mp(t, "102", "pitch", "Pitch 2 : Ball In Play", top6+`,"outs":1,"resultCount":{"balls":1,"strikes":2}`),
		mp(t, "1990001", "play-result", "B flied out", top6+`,"outs":1,"onFirst":{"x":1}`),
		mp(t, "2000000", "start-batterpitcher", "A pitches to C", top6+`,"outs":1,"onFirst":{"x":1},"onThird":{"x":1}`),
		mp(t, "2000001", "pitch", "Pitch 1 : Ball", top6+`,"onFirst":{"x":1}`),
	)
	if got := c["100"]; got.status != "Top 6 · 0-0, 0 outs, 1 on" || !got.announce || got.body != "A pitches to B" {
		t.Errorf("start batter: %+v", got)
	}
	if got := c["101"]; got.status != "Top 6 · 1-2, 0 outs, 1 on" || got.body != "Strike 2 Foul" || got.urgent {
		t.Errorf("pitch: %+v", got)
	}
	if got := c["102"]; got.announce {
		t.Errorf("ball in play announced: %+v", got)
	}
	// 1 out rose, runners 1 -> 1: normal because the out rose.
	if got := c["1990001"]; got.status != "Top 6 · 1 out, 1 on" || !got.urgent {
		t.Errorf("result: %+v", got)
	}
	if got := c["2000000"]; got.status != "Top 6 · 0-0, 1 out, 2 on" {
		t.Errorf("next at-bat: %+v", got)
	}
	if got := c["2000001"]; got.status != "Top 6 · 0-0, 1 out, 1 on" {
		t.Errorf("pitch, no resultCount: %+v", got)
	}
}

func TestMLBPitchesEndingAtBatNotAnnounced(t *testing.T) {
	for _, text := range []string{"Pitch 3 : Strike 3 Swinging", "Pitch 4 : Ball 4", "Pitch 1 : Ball In Play"} {
		if mlbContexts([]espn.Data{mp(t, "1", "pitch", text, top6)})["1"].announce {
			t.Errorf("%q announced", text)
		}
	}
}

func TestMLBSilentPlays(t *testing.T) {
	c := ctxsOf(
		mp(t, "1", "start-inning", "Top of the 6th", top6),
		mp(t, "2", "end-batterpitcher", "End batter", top6),
		nhlData(t, `{"id":"3","type":{"type":"play-result","text":"Stolen Base"},"text":"A steals second","period":{"type":"Top","number":6}}`),
		mp(t, "4", "other", "", top6),
		mp(t, "5", "other", "Mound visit", top6),
	)
	for _, id := range []string{"1", "2", "3", "4"} {
		if c[id].announce {
			t.Errorf("play %s announced", id)
		}
	}
	if got := c["5"]; !got.announce || got.status != "Top 6 · 0 outs, 0 on" {
		t.Errorf("other: %+v", got)
	}
}

func TestMLBStartInningResetsOuts(t *testing.T) {
	c := ctxsOf(
		mp(t, "1990001", "play-result", "Out", `"period":{"type":"Top","number":5},"outs":2`),
		mp(t, "200", "start-inning", "Bottom 5", `"period":{"type":"Bottom","number":5}`),
		mp(t, "201", "start-batterpitcher", "X pitches to Y", `"period":{"type":"Bottom","number":5},"outs":2`),
	)
	if got := c["201"].status; got != "Bot 5 · 0-0, 0 outs, 0 on" {
		t.Error(got)
	}
}

func TestMLBFielderSubstitution(t *testing.T) {
	for text, announced := range map[string]bool{
		"Dubón at shortstop.":                false, // accented name: Python's \w is Unicode
		"J.D. Martinez at designated hitter": false,
		"Ozzie Albies in second base":        false,
		"Dubón hit for Smith.":               true, // pinch hitters stay announced
	} {
		got := mlbContexts([]espn.Data{mp(t, "5", "play-result", text, top6)})["5"]
		if got.announce != announced {
			t.Errorf("%q: announce = %v", text, got.announce)
		}
		if got.announce && got.urgent {
			t.Errorf("%q should be low urgency", text)
		}
	}
	// The substitution check applies to play-results only.
	if !mlbContexts([]espn.Data{mp(t, "6", "other", "Dubón at shortstop.", top6)})["6"].announce {
		t.Error("non play-result substitution text was dropped")
	}
}

func TestMLBMidAtBatEvents(t *testing.T) {
	c := ctxsOf(
		mp(t, "100", "start-batterpitcher", "A pitches to B", top6+`,"onFirst":1`),
		mp(t, "101", "play-result", "Smith caught stealing second.", top6+`,"outs":0`),
		mp(t, "102", "play-result", "Jones ran for Smith.", top6),
		mp(t, "103", "play-result", "Lee relieved Kim.", top6),
		mp(t, "104", "play-result", "Brown hit for Green.", top6),
		mp(t, "105", "play-result", "Runner scores.", top6+`,"scoringPlay":true`),
	)
	// ESPN's own `outs` is ignored mid at-bat; caught stealing adds one.
	if got := c["101"]; got.status != "Top 6 · 1 out, 1 on" || !got.urgent {
		t.Errorf("caught stealing: %+v", got)
	}
	if got := c["102"]; !got.urgent || got.status != "Top 6 · 1 out, 1 on" {
		t.Errorf("ran for: %+v", got)
	}
	if !c["103"].urgent {
		t.Error("relieved not urgent")
	}
	if c["104"].urgent {
		t.Error("pinch hitter urgent")
	}
	if !c["105"].urgent {
		t.Error("scoring not urgent")
	}
}

func TestMLBAtBatResultBoundary(t *testing.T) {
	// Id ending 989999 is mid at-bat (outs tracked, not ESPN's); 990000 is a result.
	c := ctxsOf(
		mp(t, "400989999", "play-result", "Sub something.", top6+`,"outs":2`),
		mp(t, "400990000", "play-result", "Smith grounded out.", top6+`,"outs":2,"onSecond":1`),
		mp(t, "12345", "play-result", "Short id.", top6+`,"outs":2`),
	)
	if got := c["400989999"].status; got != "Top 6 · 0 outs, 0 on" {
		t.Errorf("989999: %s", got)
	}
	if got := c["400990000"]; got.status != "Top 6 · 2 outs, 1 on" || !got.urgent {
		t.Errorf("990000: %+v", got)
	}
	// "12345" (5 chars) sorts before and compares as a string: "12345" < "990000".
	if got := c["12345"].status; got != "Top 6 · 0 outs, 0 on" {
		t.Errorf("short id: %s", got)
	}
}

func TestMLBResultMissingOutsAndThirdOut(t *testing.T) {
	c := ctxsOf(
		mp(t, "1990001", "play-result", "Single.", top6+`,"onFirst":1`),
		mp(t, "1990002", "play-result", "Walk.", top6+`,"outs":1,"onFirst":1,"onSecond":1`),
		mp(t, "1990003", "play-result", "Lineout.", top6+`,"outs":3,"onFirst":1`),
	)
	// Missing outs default to the tracked outs; a runner reaching is urgent.
	if got := c["1990001"]; got.status != "Top 6 · 0 outs, 1 on" || !got.urgent {
		t.Errorf("single: %+v", got)
	}
	if got := c["1990002"]; got.status != "Top 6 · 1 out, 2 on" || !got.urgent {
		t.Errorf("walk: %+v", got)
	}
	if got := c["1990003"]; got.status != "Top 6 · 3 outs" || !got.urgent {
		t.Errorf("third out: %+v", got)
	}
	// Nothing changes: low.
	c = ctxsOf(
		mp(t, "1990001", "play-result", "Single.", top6+`,"outs":0,"onFirst":1`),
		mp(t, "1990002", "play-result", "Fielder's choice, same count.", top6+`,"outs":0,"onFirst":1`),
	)
	if c["1990002"].urgent {
		t.Error("no change was urgent")
	}
}

func TestMLBEndInning(t *testing.T) {
	end := func(id, ptype string, n int, away, home string) mlbContext {
		extra := fmt.Sprintf(`"period":{"type":%q,"number":%d}`, ptype, n)
		if away != "" {
			extra += `,"awayScore":` + away
		}
		if home != "" {
			extra += `,"homeScore":` + home
		}
		return mlbContexts([]espn.Data{mp(t, id, "end-inning", "Middle of the inning", extra)})[id]
	}
	if got := end("1", "Mid", 6, "3", "1"); !got.announce || got.status != "Bot 6 · 0 outs" || got.body != "Side retired" {
		t.Errorf("mid 6: %+v", got)
	}
	if got := end("1", "End", 6, "3", "1"); !got.announce || got.status != "Top 7 · 0 outs" {
		t.Errorf("end 6: %+v", got)
	}
	cases := []struct {
		name         string
		ptype        string
		n            int
		away, home   string
		wantAnnounce bool
	}{
		{"mid 9 home ahead", "Mid", 9, "2", "3", false},
		{"mid 9 tie", "Mid", 9, "3", "3", true},
		{"mid 9 away ahead", "Mid", 9, "4", "3", true},
		{"end 9 away ahead", "End", 9, "4", "3", false},
		{"end 9 tie goes to extras", "End", 9, "3", "3", true},
		{"end 9 home ahead", "End", 9, "2", "3", false},
		{"end 10 tie", "End", 10, "3", "3", true},
		{"end 11 home ahead", "End", 11, "3", "4", false},
		{"end 8 away ahead", "End", 8, "4", "3", true},
		{"end 9 missing scores = tie", "End", 9, "", "", true},
		{"end 9 missing away, home 1", "End", 9, "", "1", false},
		{"mid 9 home 0 away missing", "Mid", 9, "", "0", true},
	}
	for _, c := range cases {
		if got := end("1", c.ptype, c.n, c.away, c.home); got.announce != c.wantAnnounce {
			t.Errorf("%s: announce = %v", c.name, got.announce)
		}
	}
}

func TestMLBIDOrderIsNumericNotListOrder(t *testing.T) {
	// Listed out of order and with different widths: outs are tracked in id order.
	plays := []espn.Data{
		mp(t, "1000", "start-batterpitcher", "late", top6),
		mp(t, "999990", "play-result", "Out.", top6+`,"outs":1`),
	}
	if got := mlbContexts(plays)["1000"].status; got != "Top 6 · 0-0, 0 outs, 0 on" {
		// 1000 < 999990 numerically, so the out has not happened yet.
		t.Error(got)
	}
	plays = []espn.Data{plays[1], mp(t, "999991", "start-batterpitcher", "next", top6)}
	if got := mlbContexts(plays)["999991"].status; got != "Top 6 · 0-0, 1 out, 0 on" {
		t.Error(got)
	}
}

func TestMLBMessage(t *testing.T) {
	f := &Feed{Away: TeamScore{Abbr: "LAD", Score: "9"}, Home: TeamScore{Abbr: "ATL", Score: "8"}}
	f.Plays = []espn.Data{
		mp(t, "1", "start-batterpitcher", "X pitches to Y", top6+`,"awayScore":0,"homeScore":1`),
		mp(t, "2", "start-batterpitcher", "X pitches to Z", top6),
	}
	r := mlbRules{}.Prepare(f)
	m := r.Message(f.Plays[0])
	if m.Title != "LAD 0 · Top 6 · 0-0, 0 outs, 0 on · ATL 1" {
		t.Error(m.Title) // a score of 0 is present, not missing
	}
	if m.Away.Score != "0" || m.Home.Score != "1" || m.Body != "X pitches to Y" {
		t.Errorf("%+v", m)
	}
	m = r.Message(f.Plays[1])
	if !strings.HasPrefix(m.Title, "LAD 9 · ") || !strings.HasSuffix(m.Title, " · ATL 8") {
		t.Error(m.Title) // missing score falls back to the game score
	}
	if ids := r.IDs(f.Plays[0]); len(ids) != 1 || ids[0] != "1" {
		t.Error(ids)
	}
}

func TestMLBBases(t *testing.T) {
	c := ctxsOf(
		mp(t, "100", "start-batterpitcher", "A pitches to B", top6+`,"onFirst":{"x":1},"onThird":{"x":1}`),
		mp(t, "101", "pitch", "Pitch 1 : Ball", top6+`,"onFirst":{"x":1},"onThird":{"x":1}`),
		mp(t, "102", "play-result", "Sub something.", top6),
		mp(t, "1990001", "play-result", "Walk.", top6+`,"outs":0,"onFirst":1,"onSecond":1,"onThird":1`),
		mp(t, "1990002", "play-result", "Lineout.", top6+`,"outs":3,"onFirst":1`),
	)
	for id, want := range map[string]string{"100": "13", "101": "13", "102": "13", "1990001": "123", "1990002": ""} {
		if got := c[id].bases; got != want {
			t.Errorf("play %s: bases %q, want %q", id, got, want)
		}
	}
}

func TestMLBCountNumbers(t *testing.T) {
	c := ctxsOf(
		mp(t, "100", "start-batterpitcher", "A pitches to B", top6+`,"outs":1`),
		mp(t, "101", "pitch", "Pitch 1 : Strike 2 Foul", top6+`,"outs":1,"resultCount":{"balls":3,"strikes":2}`),
		mp(t, "1990001", "play-result", "B flied out", top6+`,"outs":2`),
		mp(t, "1990002", "play-result", "C flied out", top6+`,"outs":3`),
	)
	for id, want := range map[string][3]int{
		"100":     {0, 0, 0}, // outs are tracked: ESPN's 1 is the count after the at-bat
		"101":     {3, 2, 0},
		"1990001": {0, 0, 2}, // a result carries no count
		"1990002": {0, 0, 3},
	} {
		if got := c[id]; [3]int{got.balls, got.strikes, got.outs} != want {
			t.Errorf("play %s: balls %d strikes %d outs %d, want %v", id, got.balls, got.strikes, got.outs, want)
		}
	}
}
