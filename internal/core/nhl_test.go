package core

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"playcall/internal/espn"
)

func nhlData(t *testing.T, js string) espn.Data {
	t.Helper()
	d, err := espn.Decode([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// nhlPlay builds a play from its JSON fields.
func nhlPlay(t *testing.T, id string, fields string) espn.Data {
	t.Helper()
	if fields != "" {
		fields = "," + fields
	}
	return nhlData(t, fmt.Sprintf(`{"id":%q%s}`, id, fields))
}

func clockPlay(t *testing.T, clock string, period string) espn.Data {
	t.Helper()
	return nhlData(t, fmt.Sprintf(`{"clock":{"displayValue":%q},"period":%s}`, clock, period))
}

func TestNHLRulesFor(t *testing.T) {
	r, err := RulesFor("nhl")
	if err != nil {
		t.Fatal(err)
	}
	if !r.TrackSent() || r.CatchupLimit() != 10 {
		t.Errorf("TrackSent %v, limit %d", r.TrackSent(), r.CatchupLimit())
	}
}

func TestNHLClockLeft(t *testing.T) {
	regSeason := nhlData(t, `{"format":{"regulation":{"clock":1200,"periods":3}},"header":{"season":{"type":2}}}`)
	playoffs := nhlData(t, `{"format":{"regulation":{"clock":1200,"periods":3}},"header":{"season":{"type":3}}}`)
	empty := nhlData(t, `{}`)
	cases := []struct {
		name   string
		data   espn.Data
		clock  string
		period string
		want   string
	}{
		{"period 1", regSeason, "1:57", `{"number":1}`, "18:03"},
		{"period 2", regSeason, "10:30", `{"number":2}`, "9:30"},
		{"start", regSeason, "0:00", `{"number":1}`, "20:00"},
		{"end", regSeason, "20:00", `{"number":3}`, "0:00"},
		{"regulation period 3", regSeason, "5:00", `{"number":3}`, "15:00"},
		{"regular-season overtime", regSeason, "1:30", `{"number":4}`, "3:30"},
		{"playoff overtime", playoffs, "1:30", `{"number":4}`, "18:30"},
		{"playoff overtime 2", playoffs, "1:30", `{"number":5}`, "18:30"},
		{"no period number", regSeason, "1:57", `{}`, "1:57"},
		{"zero period number", regSeason, "1:57", `{"number":0}`, "1:57"},
		{"not m:ss", regSeason, "31.4", `{"number":1}`, "31.4"},
		{"one-digit seconds", regSeason, "1:5", `{"number":1}`, "1:5"},
		{"empty clock", regSeason, "", `{"number":1}`, ""},
		{"below zero", regSeason, "21:00", `{"number":1}`, "21:00"},
		{"overtime below zero", regSeason, "6:00", `{"number":4}`, "6:00"},
		{"missing format uses defaults", empty, "1:57", `{"number":1}`, "18:03"},
		{"missing format, overtime", empty, "1:00", `{"number":4}`, "4:00"},
		{"zero format values use defaults", nhlData(t, `{"format":{"regulation":{"clock":0,"periods":0}}}`), "1:57", `{"number":3}`, "18:03"},
	}
	for _, c := range cases {
		if got := nhlClockLeft(c.data, clockPlay(t, c.clock, c.period)); got != c.want {
			t.Errorf("%s: nhlClockLeft(%q) = %q, want %q", c.name, c.clock, got, c.want)
		}
	}
}

func competitor(t *testing.T, side, id string) espn.Data {
	return nhlData(t, fmt.Sprintf(`{"homeAway":%q,"team":{"id":%s}}`, side, id))
}

func TestNHLPowerPlays(t *testing.T) {
	home, away := competitor(t, "home", `"1"`), competitor(t, "away", `2`) // string and number ids
	pl := func(id, team, strength string, extra string) espn.Data {
		f := ""
		if team != "" {
			f += fmt.Sprintf(`"team":{"id":%s},`, team)
		}
		if strength != "" {
			f += fmt.Sprintf(`"strength":{"text":%q},`, strength)
		}
		f += `"type":{"text":"x"` + extra + `}`
		return nhlPlay(t, id, f)
	}
	penalty := `,"penaltyMinutes":2`
	plays := []espn.Data{
		pl("1", "1", "Even Strength", ""),
		pl("2", "1", "Power Play", ""),       // home has it
		pl("3", "", "", ""),                  // no team: keeps home
		pl("4", "2", "Shorthanded", penalty), // penalty play: unchanged
		pl("5", "2", "Shorthanded", ""),      // away shorthanded: home
		pl("6", "2", "Power Play", ""),       // away has it
		pl("7", "1", "Shorthanded", ""),      // home shorthanded: away
		pl("8", "9", "Power Play", ""),       // unknown team: unchanged
		pl("9", "1", "Even Strength", ""),    // clears
		pl("10", "1", "Shorthanded", ""),     // away
		pl("11", "2", "Even Strength", ""),   // goal-ending play is itself marked by the label...
		pl("12", "2", "", ""),                // ...and the next play carries none
	}
	got := nhlPowerPlays(plays, home, away)
	want := map[string]string{
		"1": "", "2": "home", "3": "home", "4": "home", "5": "home", "6": "away",
		"7": "away", "8": "away", "9": "", "10": "away", "11": "", "12": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// A goal that ends the power play is still marked; the next play is not. ESPN
// labels the goal with the strength before it, and the following play Even.
func TestNHLPowerPlayGoalEnding(t *testing.T) {
	home, away := competitor(t, "home", `"1"`), competitor(t, "away", `"2"`)
	plays := []espn.Data{
		nhlPlay(t, "1", `"team":{"id":"1"},"strength":{"text":"Power Play"}`),
		nhlPlay(t, "2", `"team":{"id":"1"},"strength":{"text":"Power Play"},"scoringPlay":true`),
		nhlPlay(t, "3", `"team":{"id":"1"},"strength":{"text":"Even Strength"}`),
	}
	got := nhlPowerPlays(plays, home, away)
	if got["2"] != "home" || got["3"] != "" {
		t.Errorf("got %v", got)
	}
}

func TestNHLPowerPlaysMissingCompetitor(t *testing.T) {
	home := competitor(t, "home", `"1"`)
	plays := []espn.Data{
		nhlPlay(t, "1", `"team":{"id":"1"},"strength":{"text":"Power Play"}`),
		nhlPlay(t, "2", `"team":{"id":"2"},"strength":{"text":"Power Play"}`), // away missing: unknown team
		nhlPlay(t, "3", `"team":{"id":"1"},"strength":{"text":"Shorthanded"}`),
	}
	got := nhlPowerPlays(plays, home, nil)
	if got["1"] != "home" || got["2"] != "home" || got["3"] != "away" {
		t.Errorf("got %v", got)
	}
	got = nhlPowerPlays(plays, nil, nil)
	for k, v := range got {
		if v != "" {
			t.Errorf("play %s: %q with no competitors", k, v)
		}
	}
	// A competitor with no team id must not panic.
	nhlPowerPlays(plays, nhlData(t, `{"homeAway":"home"}`), nhlData(t, `{"homeAway":"away"}`))
}

func TestNHLBody(t *testing.T) {
	cases := []struct{ typ, want string }{
		{`{"text":"Penalty","penaltyMinutes":2,"penaltyType":"Minor"}`, "Timo Meier High-sticking against Jack McBain · 2 min Minor"},
		{`{"text":"Penalty","penaltyMinutes":"5","penaltyType":"Major"}`, "Timo Meier High-sticking against Jack McBain · 5 min Major"},
		{`{"text":"Penalty","penaltyMinutes":2}`, "Timo Meier High-sticking against Jack McBain · 2 min"},
		{`{"text":"Penalty","penaltyMinutes":0,"penaltyType":"Minor"}`, "Timo Meier High-sticking against Jack McBain"},
		{`{"text":"Shot"}`, "Timo Meier High-sticking against Jack McBain"},
	}
	for _, c := range cases {
		p := nhlData(t, `{"id":"1","text":"  Timo Meier High-sticking against Jack McBain ","type":`+c.typ+`}`)
		if got := nhlBody(p, espn.PlayText(p)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.typ, got, c.want)
		}
	}
}

// nhlFeed is a summary with UTA away (id 2) and NJ home (id 1).
func nhlFeed(t *testing.T, plays ...string) *Feed {
	t.Helper()
	d := nhlData(t, `{"header":{"competitions":[{"competitors":[
		{"homeAway":"home","score":"2","team":{"id":"1","abbreviation":"NJ"}},
		{"homeAway":"away","score":"4","team":{"id":2,"abbreviation":"UTA"}}]}]},
		"status":{"type":{"state":"in","shortDetail":"3rd","name":"STATUS_IN_PROGRESS"}},
		"plays":[`+strings.Join(plays, ",")+`]}`)
	f := &Feed{Data: d, Plays: espn.ExtractPlays(d)}
	f.Away = TeamScore{Abbr: "UTA", Score: "4"}
	f.Home = TeamScore{Abbr: "NJ", Score: "2"}
	return f
}

func TestNHLMessageTitleAndUrgency(t *testing.T) {
	f := nhlFeed(t,
		`{"id":"10","type":{"text":"Penalty","penaltyMinutes":2,"penaltyType":"Minor"},"text":"Hold","team":{"id":"1"},"strength":{"text":"Even Strength"},"awayScore":0,"homeScore":0,"period":{"number":1,"displayValue":"1st"},"clock":{"displayValue":"1:57"}}`,
		`{"id":"11","type":{"text":"Shot"},"text":"Shot on goal","team":{"id":"2"},"strength":{"text":"Power Play"},"awayScore":1,"homeScore":0,"period":{"number":1,"displayValue":"1st"},"clock":{"displayValue":"2:00"}}`,
		`{"id":"12","type":{"text":"Goal"},"text":"Goal!","scoringPlay":true,"team":{"id":"1"},"strength":{"text":"Shorthanded"},"awayScore":1,"homeScore":1,"period":{"number":2,"displayValue":"2nd"},"clock":{"displayValue":"0:00"}}`,
		`{"id":"13","type":{"text":"Takeaway"},"text":"Takeaway","period":{"displayValue":"3rd"}}`)
	r := nhlRules{}.Prepare(f)
	msgs := make([]Message, len(f.Plays))
	for i, p := range f.Plays {
		msgs[i] = r.Message(p)
	}
	wantTitles := []string{
		"UTA 0 · 18:03 1st · NJ 0",
		"UTA 1 PP · 18:00 1st · NJ 0",
		"UTA 1 PP · 20:00 2nd · NJ 1",
		"UTA 4 PP · 3rd · NJ 2", // no score, clock or number: game-level score; PP carried
	}
	wantUrgent := []bool{true, false, true, false}
	for i, m := range msgs {
		if m.Title != wantTitles[i] || m.Urgent != wantUrgent[i] {
			t.Errorf("play %d: %q urgent %v; want %q urgent %v", i, m.Title, m.Urgent, wantTitles[i], wantUrgent[i])
		}
		if strings.Contains(m.Body, "PP") {
			t.Errorf("play %d: body mentions PP: %q", i, m.Body)
		}
	}
	if !msgs[1].Away.PowerPlay || msgs[1].Home.PowerPlay || msgs[1].Away.Score != "1" {
		t.Errorf("structured PP: %+v", msgs[1])
	}
	if msgs[1].Clock != "18:00" || msgs[1].Period != "1st" {
		t.Errorf("clock/period: %+v", msgs[1])
	}
	if msgs[0].Body != "Hold · 2 min Minor" {
		t.Errorf("penalty body %q", msgs[0].Body)
	}
	// Home side power play.
	f2 := nhlFeed(t, `{"id":"1","type":{"text":"Shot"},"text":"x","team":{"id":"1"},"strength":{"text":"Power Play"},"awayScore":0,"homeScore":0}`)
	m := nhlRules{}.Prepare(f2).Message(f2.Plays[0])
	if m.Title != "UTA 0 · NJ 0 PP" || !m.Home.PowerPlay {
		t.Errorf("home PP: %+v", m)
	}
}

func TestNHLAnnounced(t *testing.T) {
	f := nhlFeed(t)
	r := nhlRules{}.Prepare(f)
	for typ, want := range map[string]bool{
		"Hit": false, "Period Start": false, "Period End": false,
		"Face Off": true, "Stoppage": true, "Shot": true, "Goal": true,
		"Penalty": true, "Giveaway": true, "Something New": true, "": true,
	} {
		p := nhlData(t, fmt.Sprintf(`{"id":"1","type":{"text":%q}}`, typ))
		if got := r.Announced(p); got != want {
			t.Errorf("%q: announced %v, want %v", typ, got, want)
		}
	}
	if got := r.IDs(nhlData(t, `{"id":"7"}`)); !reflect.DeepEqual(got, []string{"7"}) {
		t.Errorf("IDs %v", got)
	}
}

// Play ids are not chronological: list order decides what is new and the
// order of sending.
func TestNHLOutOfOrderIDs(t *testing.T) {
	js := func(id, text string) string {
		return fmt.Sprintf(`{"id":%q,"type":{"text":"Shot"},"text":%q}`, id, text)
	}
	feed := nhlFeed(t, js("500", "first"), js("100", "second"), js("900", "third"), js("300", "fourth"))
	var sent []string
	state := &State{GameID: "1", Sport: "nhl", SentIDs: []string{"500", "100"}}
	state.LastPlayID = new(PlayID)
	*state.LastPlayID = 100
	sink := &fakeSink{}
	poller, err := NewPoller(state, sink, func(context.Context) (espn.Data, error) { return feed.Data, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := poller.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range sink.events {
		sent = append(sent, e.PlayID)
	}
	if !reflect.DeepEqual(sent, []string{"900", "300"}) {
		t.Errorf("sent %v, want [900 300]", sent)
	}
}
