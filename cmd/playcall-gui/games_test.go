package main

import (
	"strings"
	"testing"
	"time"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
)

func TestOrderForPicker(t *testing.T) {
	in := []espn.Game{
		{ID: "final1", State: "post"},
		{ID: "pre1", State: "pre"},
		{ID: "live1", State: "in"},
		{ID: "pre2", State: "pre"},
		{ID: "live2", State: "in"},
	}
	got := orderForPicker(in)
	want := []string{"live1", "live2", "pre1", "pre2", "final1"}
	for i, g := range got {
		if g.ID != want[i] {
			t.Fatalf("order %v, want %v", ids(got), want)
		}
	}
	if in[0].ID != "final1" {
		t.Error("input was reordered")
	}
}

func ids(gs []espn.Game) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.ID)
	}
	return out
}

func TestGameLines(t *testing.T) {
	now := time.Date(2026, 10, 10, 11, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	live := espn.Game{Away: "TB ", Home: "DAL", State: "in", AwayScore: "7", HomeScore: "10", Status: "0:12 - 2nd"}
	if got := matchup(live); got != "TB @ DAL" {
		t.Errorf("matchup %q", got)
	}
	if got := gameDetail(live, now); got != "7-10  0:12 - 2nd" {
		t.Errorf("live detail %q", got)
	}
	pre := espn.Game{State: "pre", AwayScore: "0", HomeScore: "0", Status: "10/11 - 1:00 PM EDT", Date: "2026-10-11T17:00Z"}
	if got := gameDetail(pre, now); got != "Sun 10/11 - 1:00 PM EDT" {
		t.Errorf("pre detail %q", got)
	}
	pre.Status = "Postponed"
	if got := gameDetail(pre, now); got != "Postponed" {
		t.Errorf("pre without a date %q", got)
	}
	final := espn.Game{State: "post", AwayScore: "27", HomeScore: "24", Status: "Final", Date: "2026-10-10T01:00Z"}
	if got := gameDetail(final, now); got != "Fri 10/9  27-24  Final" {
		t.Errorf("earlier final %q", got)
	}
	final.Date = "2026-10-10T12:00Z"
	if got := gameDetail(final, now); got != "27-24  Final" {
		t.Errorf("same-day final %q", got)
	}
}

func TestGroupChoices(t *testing.T) {
	cfb := groupChoices("cfb")
	if cfb[0] != "All" || cfb[1] != "ACC" || len(cfb) != 9 {
		t.Errorf("cfb: %v", cfb)
	}
	for _, c := range cfb {
		if c == "All FBS" {
			t.Error("All FBS should be folded into All")
		}
	}
	if nfl := groupChoices("nfl"); nfl[0] != "All" || nfl[1] != "AFC" || len(nfl) != 11 {
		t.Errorf("nfl: %v", nfl)
	}
	if got := groupChoices("cbb"); got[1] != "America East" {
		t.Errorf("cbb: %v", got[:3])
	}
}

func TestGroupID(t *testing.T) {
	cases := []struct{ sport, name, want string }{
		{"cfb", "All", ""}, {"cfb", "SEC", "8"}, {"nfl", "NFC North", "10"}, {"nhl", "Metropolitan", "33"}, {"wnba", "West", "2"},
	}
	for _, c := range cases {
		if got := groupID(c.sport, c.name); got != c.want {
			t.Errorf("%s %s: got %q, want %q", c.sport, c.name, got, c.want)
		}
	}
}

func TestSportLabels(t *testing.T) {
	if sportLabel("cfb") != "FBS College Football" || sportLabel("nfl") != "NFL Football" {
		t.Errorf("labels: %q %q", sportLabel("cfb"), sportLabel("nfl"))
	}
	for _, key := range sports.Names {
		l := sportLabel(key)
		if l == key {
			t.Errorf("%s has no label", key)
		}
		if sportFromLabel(l) != key {
			t.Errorf("%s: label %q maps back to %q", key, l, sportFromLabel(l))
		}
	}
	if sportFromLabel("nonsense") != "" || sportLabel("zzz") != "zzz" {
		t.Error("unknown values should pass through empty")
	}
}

func TestTeamRecord(t *testing.T) {
	cases := []struct{ overall, conf, wins, other, want string }{
		{"3-1", "1-0", "", "", "3-1 (1-0)"},
		{"85-77", "", "", "", "85-77"},
		{"85-77", "", "2", "1", "series 2-1"},
		{"85-77", "", "2", "", "85-77"},
		{"", "", "", "", ""},
	}
	for _, c := range cases {
		if got := teamRecord(c.overall, c.conf, c.wins, c.other); got != c.want {
			t.Errorf("teamRecord(%q, %q, %q, %q) = %q, want %q", c.overall, c.conf, c.wins, c.other, got, c.want)
		}
	}
}

func TestStatusRecordsDivision(t *testing.T) {
	st := core.GameStatus{
		Away:       core.TeamScore{Abbr: "BUF"},
		Home:       core.TeamScore{Abbr: "NYJ"},
		AwayRecord: core.TeamRecord{Overall: "3-1"},
		HomeRecord: core.TeamRecord{Overall: "2-2", Conf: "1-0"},
	}
	away, home := statusRecords(st, func(abbr string) string { return map[string]string{"BUF": "0-1", "NYJ": "9-9"}[abbr] })
	if away != "3-1 (0-1)" || home != "2-2 (1-0)" {
		t.Errorf("records %q, %q", away, home)
	}
	if away, _ := statusRecords(st, nil); away != "3-1" {
		t.Errorf("without a division %q", away)
	}
}

func TestIdleRow(t *testing.T) {
	bye := espn.Game{Away: "BYU", State: core.IdleState, Status: "Bye"}
	if got := gameDetail(bye, time.Now()); got != "Bye" {
		t.Errorf("bye detail %q", got)
	}
	in := []espn.Game{{ID: "bye", State: core.IdleState}, {ID: "final", State: "post"}, {ID: "live", State: "in"}, {ID: "soon", State: "pre"}}
	if got := ids(orderForPicker(in)); strings.Join(got, ",") != "live,soon,final,bye" {
		t.Errorf("order %v", got)
	}
	if n := countGames(in); n != 3 {
		t.Errorf("countGames %d", n)
	}
}

func TestPreseasonTag(t *testing.T) {
	now := time.Date(2026, 10, 10, 11, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	g := espn.Game{State: "in", AwayScore: "7", HomeScore: "10", Status: "0:12 - 2nd", Season: "1"}
	if got := gameDetail(g, now); got != "Preseason · 7-10  0:12 - 2nd" {
		t.Errorf("preseason %q", got)
	}
	g.Season = "2"
	if got := gameDetail(g, now); got != "7-10  0:12 - 2nd" {
		t.Errorf("regular season %q", got)
	}
}

func TestStatusRecordsLeaveOutTheSeries(t *testing.T) {
	st := core.GameStatus{
		AwayRecord: core.TeamRecord{Overall: "84-78", SeriesWins: "2"},
		HomeRecord: core.TeamRecord{Overall: "85-77", SeriesWins: "2"},
	}
	if away, home := statusRecords(st, nil); away != "84-78" || home != "85-77" {
		t.Errorf("records %q, %q", away, home)
	}
}
