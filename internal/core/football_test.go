package core

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"playcall/internal/espn"
)

func TestFootballHalftime(t *testing.T) {
	feed := &Feed{
		Away:   TeamScore{Abbr: "TB", Score: "7"},
		Home:   TeamScore{Abbr: "DAL", Score: "10"},
		Clock:  "0:00",
		Period: "2nd",
	}
	end := func(kind string, period int, text string) espn.Data {
		return espn.Data{
			"text":   text,
			"type":   map[string]any{"text": kind},
			"period": map[string]any{"number": json.Number(strconv.Itoa(period))},
			"end":    map[string]any{"shortDownDistanceText": "2nd & 10"},
		}
	}
	cases := []struct {
		name        string
		play        espn.Data
		title, body string
	}{
		{"nfl half", end("End of Half", 2, "END QUARTER 2"), "TB 7 · Halftime · DAL 10", "END QUARTER 2"},
		{"cfb half", end("End Period", 2, "End of 2nd quarter."), "TB 7 · Halftime · DAL 10", "End of 2nd quarter."},
		{"end of q1", end("End Period", 1, "END QUARTER 1"), "TB 7 · 0:00 2nd · DAL 10", "END QUARTER 1 · 2nd & 10"},
		{"end of q3", end("End Period", 3, "END QUARTER 3"), "TB 7 · 0:00 2nd · DAL 10", "END QUARTER 3 · 2nd & 10"},
	}
	for _, c := range cases {
		m := footballPlays{feed}.Message(c.play)
		if m.Title != c.title || m.Body != c.body {
			t.Errorf("%s: got %q / %q, want %q / %q", c.name, m.Title, m.Body, c.title, c.body)
		}
	}
	if m := (footballPlays{feed}).Message(end("End of Half", 2, "x")); m.Clock != "Halftime" || m.Period != "" {
		t.Errorf("event fields: clock %q period %q", m.Clock, m.Period)
	}
}

// ESPN sends " & Goal" (down 0) on timeouts and some penalties; a fragment with
// no down is left out of the body.
func TestFootballBodyDropsDownlessDistance(t *testing.T) {
	play := func(start, end string) espn.Data {
		p := espn.Data{}
		if start != "" {
			p["start"] = map[string]any{"shortDownDistanceText": start}
		}
		if end != "" {
			p["end"] = map[string]any{"shortDownDistanceText": end}
		}
		return p
	}
	for _, c := range []struct {
		name       string
		start, end string
		want       string
	}{
		{"normal", "1st & 10", "2nd & 5", "1st & 10 · Rush · 2nd & 5"},
		{"goal to go", "1st & Goal", "2nd & Goal", "1st & Goal · Rush · 2nd & Goal"},
		{"timeout, no down", " & Goal", "", "Rush"},
		{"no down at either end", " & Goal", "& 3", "Rush"},
		{"only the start lacks a down", " & Goal", "2nd & 2", "Rush · 2nd & 2"},
		{"no start", "", "2nd & 5", "Rush · 2nd & 5"},
	} {
		if got := footballBody(play(c.start, c.end), "Rush"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A timeout after a punt carries the old fourth down in its own start; its
// body uses the situation after the punt.
func TestFootballTimeoutBodyUsesTheSituationBefore(t *testing.T) {
	punt := espn.Data{"id": "1", "text": "punt 40 yards", "type": map[string]any{"text": "Punt"},
		"start": map[string]any{"shortDownDistanceText": "4th & 2"},
		"end":   map[string]any{"shortDownDistanceText": "1st & 10"}}
	timeout := espn.Data{"id": "2", "text": "Timeout BYU", "type": map[string]any{"text": "Timeout"},
		"start": map[string]any{"shortDownDistanceText": "4th & 2"}}
	second := espn.Data{"id": "3", "text": "Timeout ISU", "type": map[string]any{"text": "Timeout"},
		"start": map[string]any{"shortDownDistanceText": "4th & 2"}}
	score := espn.Data{"id": "4", "text": "touchdown", "type": map[string]any{"text": "Passing Touchdown"},
		"start": map[string]any{"shortDownDistanceText": "2nd & 5"}}
	late := espn.Data{"id": "5", "text": "Timeout BYU", "type": map[string]any{"text": "Timeout"},
		"start": map[string]any{"shortDownDistanceText": "3rd & 1"}}
	r := footballPlays{&Feed{Plays: []espn.Data{punt, timeout, second, score, late}}}
	for play, want := range map[string]string{
		"2": "1st & 10 · Timeout BYU",
		"3": "1st & 10 · Timeout ISU", // two timeouts in a row
		"5": "Timeout BYU",            // after a score: nothing to show
	} {
		var p espn.Data
		for _, q := range r.f.Plays {
			if q["id"] == play {
				p = q
			}
		}
		if got := r.Message(p).Body; got != want {
			t.Errorf("play %s: got %q, want %q", play, got, want)
		}
	}
}

// The timeout right after a punt or kickoff is the official's, whatever team
// ESPN files it under; a timeout after anything else is left alone.
func TestFootballOfficialTimeoutAfterAKick(t *testing.T) {
	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	punt := espn.Data{"id": "1", "text": "punt", "type": typ("Punt"), "end": map[string]any{"shortDownDistanceText": "1st & 10"}}
	t1 := espn.Data{"id": "2", "text": "Timeout BYU, clock 08:54", "type": typ("Timeout")}
	t2 := espn.Data{"id": "3", "text": "Timeout Iowa State, clock 08:50", "type": typ("Timeout")}
	rush := espn.Data{"id": "4", "text": "rush", "type": typ("Rush"), "end": map[string]any{"shortDownDistanceText": "2nd & 5"}}
	t3 := espn.Data{"id": "5", "text": "Timeout BYU, clock 05:00", "type": typ("Timeout")}
	kick := espn.Data{"id": "6", "text": "kickoff", "type": typ("Kickoff")}
	t4 := espn.Data{"id": "7", "text": "Timeout Iowa State, clock 04:00", "type": typ("Timeout")}
	r := footballPlays{&Feed{Plays: []espn.Data{punt, t1, t2, rush, t3, kick, t4}}}
	for _, c := range []struct {
		p    espn.Data
		want string
	}{
		{t1, "1st & 10 · Official timeout, clock 08:54"},
		{t2, "1st & 10 · Timeout Iowa State, clock 08:50"}, // second in a row: ESPN's label
		{t3, "2nd & 5 · Timeout BYU, clock 05:00"},
		{t4, "Official timeout, clock 04:00"}, // after a kickoff
	} {
		if got := r.Message(c.p).Body; got != c.want {
			t.Errorf("play %v: got %q, want %q", c.p["id"], got, c.want)
		}
	}
}

// Any change of possession is followed by an official's timeout, not only a kick.
func TestFootballOfficialTimeoutAfterATurnover(t *testing.T) {
	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	team := func(id string) map[string]any { return map[string]any{"team": map[string]any{"id": id}} }
	pick := espn.Data{"id": "1", "text": "pass intercepted", "type": typ("Pass Interception Return"),
		"start": team("252"), "end": map[string]any{"team": map[string]any{"id": "66"}, "shortDownDistanceText": "1st & 10"}}
	downs := espn.Data{"id": "3", "text": "pass incomplete", "type": typ("Pass Incompletion"),
		"start": team("66"), "end": map[string]any{"team": map[string]any{"id": "252"}, "shortDownDistanceText": "1st & 10"}}
	flagged := espn.Data{"id": "5", "text": "fumble", "type": typ("Fumble Recovery (Opponent)"), "isTurnover": true}
	same := espn.Data{"id": "7", "text": "rush", "type": typ("Rush"),
		"start": team("252"), "end": map[string]any{"team": map[string]any{"id": "252"}, "shortDownDistanceText": "2nd & 5"}}
	mk := func(id string) espn.Data {
		return espn.Data{"id": id, "text": "Timeout BYU, clock 08:00", "type": typ("Timeout")}
	}
	plays := []espn.Data{pick, mk("2"), downs, mk("4"), flagged, mk("6"), same, mk("8")}
	r := footballPlays{&Feed{Plays: plays}}
	for _, c := range []struct{ id, want string }{
		{"2", "Official timeout"}, {"4", "Official timeout"}, {"6", "Official timeout"}, {"8", "Timeout BYU"},
	} {
		var p espn.Data
		for _, q := range plays {
			if q["id"] == c.id {
				p = q
			}
		}
		if got := r.Message(p).Body; !strings.Contains(got, c.want) {
			t.Errorf("timeout %s: %q does not contain %q", c.id, got, c.want)
		}
	}
}

// A touchdown or field goal is followed by the official's timeout before the
// kickoff (31 of them in one Saturday's feeds, each filed under a team), so
// the timeout right after a score is relabeled.
func TestFootballOfficialTimeoutAfterAScore(t *testing.T) {
	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	td := espn.Data{"id": "1", "text": "pass for a touchdown", "type": typ("Passing Touchdown"), "scoringPlay": true}
	fg := espn.Data{"id": "3", "text": "field goal", "type": typ("Field Goal Good"), "scoringPlay": true}
	rush := espn.Data{"id": "5", "text": "rush", "type": typ("Rush"), "scoringPlay": false}
	mk := func(id string) espn.Data {
		return espn.Data{"id": id, "text": "Timeout Nebraska, clock 11:51", "type": typ("Timeout")}
	}
	plays := []espn.Data{td, mk("2"), fg, mk("4"), rush, mk("6")}
	r := footballPlays{&Feed{Plays: plays}}
	for _, c := range []struct{ id, want string }{
		{"2", "Official timeout, clock 11:51"},
		{"4", "Official timeout, clock 11:51"},
		{"6", "Timeout Nebraska, clock 11:51"}, // after a plain play: ESPN's label
	} {
		var p espn.Data
		for _, q := range plays {
			if q["id"] == c.id {
				p = q
			}
		}
		if got := r.Message(p).Body; got != c.want {
			t.Errorf("timeout %s: got %q, want %q", c.id, got, c.want)
		}
	}
}

// The timeout that stops the clock at 2:00 of the 2nd or 4th quarter is the
// two-minute warning, even when the play before it ran through 2:00.
func TestFootballTwoMinuteWarningIsOfficial(t *testing.T) {
	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	period := func(n int) map[string]any { return map[string]any{"number": json.Number(strconv.Itoa(n))} }
	snap := func(id, clock string, n int) espn.Data {
		return espn.Data{"id": id, "text": "(" + clock + ") rush for 4 yards", "type": typ("Rush"), "period": period(n)}
	}
	timeout := func(id string, n int, clock string) espn.Data {
		return espn.Data{"id": id, "text": "Timeout BYU, clock " + clock, "type": typ("Timeout"), "period": period(n)}
	}
	for _, c := range []struct {
		name  string
		plays []espn.Data
		want  string
	}{
		{"exactly 2:00", []espn.Data{snap("0", "02:10", 2), timeout("1", 2, "02:00")}, "Official timeout"},
		{"4th quarter", []espn.Data{snap("0", "02:10", 4), timeout("1", 4, "02:00")}, "Official timeout"},
		{"play ran through 2:00", []espn.Data{snap("0", "02:03", 2), timeout("1", 2, "01:55")}, "Official timeout"},
		{"snap at 2:00 sharp, ends below", []espn.Data{snap("0", "02:00", 4), timeout("1", 4, "01:54")}, "Timeout BYU"},
		{"already inside two minutes", []espn.Data{snap("0", "01:58", 4), timeout("1", 4, "01:50")}, "Timeout BYU"},
		{"before the warning", []espn.Data{snap("0", "02:30", 4), timeout("1", 4, "02:10")}, "Timeout BYU"},
		{"3rd quarter", []espn.Data{snap("0", "02:10", 3), timeout("1", 3, "02:00")}, "Timeout BYU"},
		{"second timeout in a row", []espn.Data{snap("0", "02:03", 2), timeout("1", 2, "01:55"), timeout("2", 2, "01:50")}, "Timeout BYU"},
	} {
		r := footballPlays{&Feed{Plays: c.plays}}
		last := c.plays[len(c.plays)-1]
		if got := r.Message(last).Body; !strings.Contains(got, c.want) {
			t.Errorf("%s: %q does not contain %q", c.name, got, c.want)
		}
	}
}

func TestEndZone(t *testing.T) {
	for in, want := range map[string]string{
		"(13:40) #1 J.Raynor pass complete to #13 J.Polite caught at BYU01, for 17 yards to the BYU00 TOUCHDOWN, clock 13:35": "(13:40) #1 J.Raynor pass complete to #13 J.Polite caught at BYU 1, for 17 yards to the BYU end zone TOUCHDOWN, clock 13:35",
		"M.James caught at IOWA00, for 3 yards to the IOWA00 TOUCHDOWN":                                                       "M.James caught at IOWA end zone, for 3 yards to the IOWA end zone TOUCHDOWN",
		"punt 40 yards to the ISU00, Touchback":                                                                               "punt 40 yards to the ISU end zone, Touchback",
		"rush for 5 yards to the TA&M00 TOUCHDOWN":                                                                            "rush for 5 yards to the TA&M end zone TOUCHDOWN",
		"Timeout BYU, clock 02:00":                                                                                            "Timeout BYU, clock 02:00",    // a clock, not a spot
		"rush to the ISU10 (tackle)":                                                                                          "rush to the ISU 10 (tackle)", // not the goal line
		"penalty from ISU01 to ISU06":                                                                                         "penalty from ISU 1 to ISU 6",
	} {
		if got := endZone(in); got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
	body := footballBody(espn.Data{"end": map[string]any{"shortDownDistanceText": "1st & 10"}}, "rush to the BYU00 TOUCHDOWN")
	if body != "rush to the BYU end zone TOUCHDOWN · 1st & 10" {
		t.Errorf("body %q", body)
	}
}

// ESPN writes ", clock 00:00" into a score before it knows the time. It is
// left out of what is shown unless the game clock really is at zero.
func TestFootballDropsThePlaceholderClock(t *testing.T) {
	text := "Shotgun #4 K.Houser rush right for 2 yards gain to the MSU00 TOUCHDOWN, clock 00:00"
	for _, c := range []struct{ clock, want string }{
		{"8:57", "Shotgun #4 K.Houser rush right for 2 yards gain to the MSU end zone TOUCHDOWN"},
		{"15:00", "Shotgun #4 K.Houser rush right for 2 yards gain to the MSU end zone TOUCHDOWN"},
		{"0:00", "Shotgun #4 K.Houser rush right for 2 yards gain to the MSU end zone TOUCHDOWN, clock 00:00"}, // the quarter really ended
	} {
		td := espn.Data{"id": "1", "text": text, "type": map[string]any{"text": "Rushing Touchdown"}, "scoringPlay": true}
		r := footballPlays{&Feed{Plays: []espn.Data{td}, Clock: c.clock, Period: "2nd"}}
		if got := r.Message(td).Body; got != c.want {
			t.Errorf("game clock %s: got %q, want %q", c.clock, got, c.want)
		}
	}
	// The real time ESPN fills in later is kept.
	if got := dropPlaceholderClock("TOUCHDOWN, clock 08:57", "8:57"); got != "TOUCHDOWN, clock 08:57" {
		t.Errorf("real clock dropped: %q", got)
	}
	// The raw text is what update detection compares, and is not touched.
	if got := espn.PlayText(espn.Data{"text": text}); !strings.HasSuffix(got, "clock 00:00") {
		t.Errorf("play text changed: %q", got)
	}
}

// ESPN's game clock sometimes sits at the start of a quarter while plays go
// on. When it is well above the newest play's time, that time is shown.
func TestFootballClockFallsBackWhenESPNsIsStuck(t *testing.T) {
	play := func(typ, period, clock string) espn.Data {
		return espn.Data{"type": map[string]any{"text": typ}, "period": map[string]any{"number": json.Number(period)},
			"clock": map[string]any{"displayValue": clock}}
	}
	data := func(state, period string) espn.Data {
		return espn.Data{"status": espn.Data{"period": json.Number(period), "type": espn.Data{"state": state}}}
	}
	rush := func(period, clock string) []espn.Data { return []espn.Data{play("Rush", period, clock)} }
	for _, c := range []struct {
		name     string
		sport    string
		clock    string
		data     espn.Data
		plays    []espn.Data
		finished bool
		want     string
	}{
		{"stuck at the start of the quarter", "cfb", "15:00", data("in", "2"), rush("2", "12:12"), false, "12:12"},
		{"nfl too", "nfl", "15:00", data("in", "2"), rush("2", "12:12"), false, "12:12"},
		{"not noticeably behind", "cfb", "12:40", data("in", "2"), rush("2", "12:12"), false, "12:40"},
		{"exactly at the limit", "cfb", "12:57", data("in", "2"), rush("2", "12:12"), false, "12:57"},
		{"ESPN is ahead of the play", "cfb", "11:50", data("in", "2"), rush("2", "12:12"), false, "11:50"},
		{"the play is from the last quarter", "cfb", "15:00", data("in", "3"), rush("2", "0:30"), false, "15:00"},
		{"after the End Period play", "cfb", "15:00", data("in", "2"), []espn.Data{play("End Period", "2", "0:00")}, false, "15:00"},
		{"not in play", "cfb", "15:00", data("pre", "2"), rush("2", "12:12"), false, "15:00"},
		{"final", "cfb", "15:00", data("in", "2"), rush("2", "12:12"), true, "15:00"},
		{"no plays", "cfb", "15:00", data("in", "2"), nil, false, "15:00"},
		{"another sport", "nba", "15:00", data("in", "2"), rush("2", "12:12"), false, "15:00"},
		{"a clock that is not m:ss", "cfb", "", data("in", "2"), rush("2", "12:12"), false, ""},
	} {
		if got := footballClock(c.sport, c.clock, c.data, c.plays, c.finished); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFootballOvertimeLabel(t *testing.T) {
	data := func(period int) espn.Data {
		return espn.Data{"status": map[string]any{"period": json.Number(strconv.Itoa(period))}}
	}
	cases := []struct {
		sport, clock, period string
		n                    int
		wantClock, wantPer   string
	}{
		{"cfb", "0:00", "5th", 5, "", "OT"},
		{"cfb", "0:00", "6th", 6, "", "2OT"},
		{"nfl", "7:12", "5th", 5, "7:12", "OT"},
		{"cfb", "3:00", "4th", 4, "3:00", "4th"},
		{"nba", "0:00", "5th", 5, "0:00", "5th"},
	}
	for _, c := range cases {
		clock, period := footballOvertime(c.sport, c.clock, c.period, data(c.n))
		if clock != c.wantClock || period != c.wantPer {
			t.Errorf("%s period %d: got %q %q, want %q %q", c.sport, c.n, clock, period, c.wantClock, c.wantPer)
		}
	}
	if got := dropPlaceholderClock("TOUCHDOWN, clock 00:00, 1ST DOWN", ""); got != "TOUCHDOWN, 1ST DOWN" {
		t.Errorf("overtime text kept the placeholder: %q", got)
	}
}

func TestFootballYardLineSpaced(t *testing.T) {
	cases := map[string]string{
		"rush for 10 yards gain to the ILL15 (#16 T.Heckel)":   "rush for 10 yards gain to the ILL 15 (#16 T.Heckel)",
		"caught at MSU13, for 25 yards to the MSU00 TOUCHDOWN": "caught at MSU 13, for 25 yards to the MSU end zone TOUCHDOWN",
		"pass to the OhioSt19 (tackle)":                        "pass to the OhioSt 19 (tackle)",
		"rush to the Mizzou00 TOUCHDOWN":                       "rush to the Mizzou end zone TOUCHDOWN",
		"1ST DOWN #37 E.Moczulski kick":                        "1ST DOWN #37 E.Moczulski kick",
		"from ILL31 to ILL26. NO PLAY":                         "from ILL 31 to ILL 26. NO PLAY",
	}
	for in, want := range cases {
		if got := endZone(in); got != want {
			t.Errorf("endZone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompleteSpots(t *testing.T) {
	team := func(loc string) map[string]any {
		return map[string]any{"team": map[string]any{"location": loc}}
	}
	data := espn.Data{"competitions": []any{map[string]any{"competitors": []any{
		map[string]any{"homeAway": "home", "team": team("Maryland")["team"]},
		map[string]any{"homeAway": "away", "team": team("Ohio State")["team"]},
	}}}}
	names := spotNames(data)
	for in, want := range map[string]string{
		"punt to the Maryla 22 return":   "punt to the Maryland 22 return",
		"pass to the OhioSt 19 (tackle)": "pass to the Ohio State 19 (tackle)",
		"rush to the Maryla end zone":    "rush to the Maryland end zone",
		"pass to the Mizzou 37 (tackle)": "pass to the Mizzou 37 (tackle)",
		"Maryla is not a spot":           "Maryla is not a spot",
	} {
		if got := completeSpots(in, names); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestDropReplacedPlay(t *testing.T) {
	play := func(id, clock, kind, text string) espn.Data {
		return espn.Data{
			"id": id, "text": text,
			"type":   map[string]any{"text": kind},
			"period": map[string]any{"number": json.Number("1")},
			"clock":  map[string]any{"displayValue": clock},
		}
	}
	last := PlayID(191)
	p := &Poller{State: &State{GameID: "g", LastPlayID: &last}, Rules: footballRules{}, known: map[string]bool{}}
	kick := play("162", "1:39", "Kickoff", "(01:39) #80 S.Starzyk kickoff 65 yards to the UK00 fair catch")
	pass := play("165", "1:29", "Pass Reception", "(01:39) pass complete")
	if got := p.dropReplaced([]espn.Data{kick, pass}, nil); len(got) != 0 {
		t.Fatalf("first poll: %v", got)
	}
	// ESPN drops 162 and posts the corrected kickoff as 194.
	fixed := play("194", "1:39", "Kickoff", "(01:39) #80 S.Starzyk kickoff 65 yards to the UK00, Touchback")
	if got := p.dropReplaced([]espn.Data{fixed, pass}, []espn.Data{fixed}); len(got) != 0 {
		t.Errorf("a replaced play was kept: %v", got)
	}
	if *p.State.LastPlayID != 194 || !p.known["194"] {
		t.Errorf("last_play_id %d, known %v", *p.State.LastPlayID, p.known)
	}
	// The original is used up: the same look again is a real new play.
	again := play("200", "1:39", "Kickoff", "(01:39) #80 S.Starzyk kickoff 65 yards to the UK00, Touchback")
	if got := p.dropReplaced([]espn.Data{fixed, pass, again}, []espn.Data{again}); len(got) != 1 {
		t.Errorf("a play whose twin is still there was dropped: %v", got)
	}
}

func TestDropAfterPeriodEnd(t *testing.T) {
	play := func(id string, period int, kind string) espn.Data {
		return espn.Data{
			"id": id, "text": "play " + id,
			"type":   map[string]any{"text": kind},
			"period": map[string]any{"number": json.Number(strconv.Itoa(period))},
		}
	}
	last := PlayID(191)
	p := &Poller{State: &State{GameID: "g", LastPlayID: &last}, Rules: footballRules{}, known: map[string]bool{}}
	end := play("191", 1, "End Period")
	late := play("194", 1, "Kickoff")
	// The end arrives in the same poll as the plays before it: all are sent.
	before := play("190", 1, "Rush")
	if got := p.dropAfterPeriodEnd([]espn.Data{before, end}, []espn.Data{before, end}); len(got) != 2 {
		t.Fatalf("same poll: %d kept", len(got))
	}
	// Later a 1st-quarter play shows up: not announced; a 2nd-quarter one is.
	next := play("195", 2, "Rush")
	got := p.dropAfterPeriodEnd([]espn.Data{before, end, late, next}, []espn.Data{late, next})
	if len(got) != 1 || espn.PlayKey(got[0]) != "195" {
		t.Errorf("kept %v", got)
	}
	if *p.State.LastPlayID != 194 || !p.known["194"] {
		t.Errorf("last_play_id %d, known %v", *p.State.LastPlayID, p.known)
	}
}
