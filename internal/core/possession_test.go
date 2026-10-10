package core

import (
	"fmt"
	"strings"
	"testing"

	"playcall/internal/espn"
)

// bp builds a basketball play: type, text, the team that made it, and whether
// it scored.
func bp(kind, text, team string, scoring bool) espn.Data {
	shooting := strings.Contains(kind, "Shot") || strings.Contains(kind, "Layup") || strings.HasPrefix(kind, "Free Throw")
	p := espn.Data{
		"id": "1", "text": text, "type": map[string]any{"text": kind},
		"shootingPlay": shooting, "scoringPlay": scoring,
	}
	if team != "" {
		p["team"] = map[string]any{"id": team}
	}
	return p
}

func TestBasketballPossessionRules(t *testing.T) {
	const A, B = "A", "B"
	type step struct {
		play espn.Data
		want string // possession after this play
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{"a made shot gives the ball to the other team", []step{
			{bp("Jump Shot", "A misses", A, false), A},
			{bp("Defensive Rebound", "B defensive rebound", B, false), B},
			{bp("Layup Shot", "B makes layup", B, true), A},
		}},
		{"an offensive rebound keeps it", []step{
			{bp("Jump Shot", "A misses", A, false), A},
			{bp("Offensive Rebound", "A offensive rebound", A, false), A},
		}},
		{"a turnover, steals included, gives it to the other team", []step{
			{bp("Jump Shot", "A misses", A, false), A},
			{bp("Lost Ball Turnover", "A lost ball turnover (B steals)", A, false), B},
			{bp("Shot Clock Turnover", "shot clock turnover", B, false), A},
			{bp("Traveling", "A traveling turnover", A, false), B},
		}},
		{"free throws: the shooter until the last one goes in", []step{
			{bp("Free Throw - 1 of 2", "A makes free throw 1 of 2", A, true), A},
			{bp("Free Throw - 2 of 2", "A makes free throw 2 of 2", A, true), B},
		}},
		{"a missed last free throw waits for the rebound", []step{
			{bp("Free Throw - 2 of 2", "A misses free throw 2 of 2", A, false), A},
			{bp("Defensive Rebound", "B defensive rebound", B, false), B},
		}},
		{"an and-one: the scorer shoots, then the other team has it", []step{
			{bp("Layup Shot", "A makes layup", A, true), B},
			{bp("Shooting Foul", "B shooting foul", B, false), A},
			{bp("Free Throw - 1 of 1", "A makes free throw 1 of 1", A, true), B},
		}},
		{"an away-from-play foul: the fouled team shoots and keeps the ball", []step{
			{bp("Jump Shot", "B misses", B, false), B},
			{bp("Defensive Rebound", "A defensive rebound", A, false), A},
			{bp("Away from Play Foul", "B away from play foul", B, false), A},
			{bp("Free Throw - 1 of 1", "A makes free throw 1 of 1", A, true), A},
		}},
		{"technical free throws change nothing", []step{
			{bp("Jump Shot", "A misses", A, false), A},
			{bp("Free Throw - Technical", "B makes technical free throw", B, true), A},
		}},
		{"a jump ball goes to the team the play names", []step{
			{bp("Jumpball", "A vs. B (B gains possession)", B, false), B},
		}},
		{"a jump ball violation gives it to the other team", []step{
			{bp("Jump Ball", "A jump ball violation", A, false), B},
		}},
		{"the end of a period leaves nobody until the next shot shows it", []step{
			{bp("Jump Shot", "A makes", A, true), B},
			{bp("End Period", "End of the 1st Quarter", "", false), ""},
			{bp("Substitution", "A enters the game", A, false), ""},
			{bp("Jump Shot", "B misses", B, false), B},
		}},
		{"fouls, timeouts and substitutions change nothing", []step{
			{bp("Jump Shot", "A misses", A, false), A},
			{bp("Full Timeout", "B Full timeout", B, false), A},
			{bp("Substitution", "B enters the game", B, false), A},
			{bp("Loose Ball Foul", "B loose ball foul", B, false), A},
		}},
		{"a bare offensive foul is ignored; its turnover play decides", []step{
			{bp("Jump Shot", "A misses", A, false), A},
			{bp("Offensive Foul", "A offensive foul", B, false), A},
			{bp("Offensive Foul Turnover", "A offensive foul turnover", A, false), B},
		}},
	}
	for _, c := range cases {
		var plays []espn.Data
		for _, s := range c.steps {
			plays = append(plays, s.play)
		}
		trace := basketballPossessionTrace(plays, A, B)
		for i, s := range c.steps {
			if trace[i] != s.want {
				t.Errorf("%s: after play %d (%s) got %q, want %q", c.name, i+1, espn.PlayText(s.play), trace[i], s.want)
			}
		}
	}
	if got := basketballPossession(nil, A, B); got != "" {
		t.Errorf("no plays: %q", got)
	}
}

// On real full games the inferred ball holder should be the team that makes
// the next shot, free throw or turnover.
func TestBasketballPossessionAgreesWithRealGames(t *testing.T) {
	for _, fixture := range []string{"playbyplay_nba_multi_plays.json", "playbyplay_wnba_multi_plays.json"} {
		d := loadFixture(t, fixture)
		home, away := espn.Teams(espn.Competition(d))
		a, h := espn.Str(away["id"]), espn.Str(home["id"])
		plays := espn.ExtractPlays(d)
		trace := basketballPossessionTrace(plays, a, h)
		var agree, disagree, unknown int
		for i := 1; i < len(plays); i++ {
			p := plays[i]
			team := espn.Str(espn.Map(p["team"])["id"])
			typ := strings.ToLower(strings.Join(strings.Fields(playType(p)), " "))
			text := strings.ToLower(espn.PlayText(p))
			acts := espn.Truthy(p["shootingPlay"]) && !strings.Contains(text, "technical") ||
				strings.Contains(typ, "turnover") || strings.Contains(text, "turnover")
			if !acts || team == "" {
				continue
			}
			switch before := trace[i-1]; {
			case before == "":
				unknown++
			case before == team:
				agree++
			default:
				disagree++
			}
		}
		known := agree + disagree
		t.Log(fmt.Sprintf("%s: %d agree, %d disagree, %d unknown", fixture, agree, disagree, unknown))
		if known < 100 {
			t.Fatalf("%s: only %d plays to check", fixture, known)
		}
		if got := float64(agree) / float64(known); got < 0.97 {
			t.Errorf("%s: the inferred ball holder matched the next actor %.1f%% of the time, want at least 97%%", fixture, 100*got)
		}
		if got := float64(known) / float64(known+unknown); got < 0.95 {
			t.Errorf("%s: an answer on only %.1f%% of plays", fixture, 100*got)
		}
	}
}

func TestPossessionInStatusForBasketball(t *testing.T) {
	data := espn.Data{"header": map[string]any{"competitions": []any{map[string]any{
		"competitors": []any{
			map[string]any{"homeAway": "home", "id": "H", "team": map[string]any{"abbreviation": "HOM"}},
			map[string]any{"homeAway": "away", "id": "V", "team": map[string]any{"abbreviation": "VIS"}},
		},
		"status": map[string]any{"type": map[string]any{"state": "in", "name": "STATUS_IN_PROGRESS"}},
	}}}}
	typ := espn.Map(espn.Status(data)["type"])
	miss := bp("Jump Shot", "V misses", "V", false)
	feed := func(finished bool, plays ...espn.Data) *Feed {
		return &Feed{Data: data, Plays: plays, Finished: finished}
	}
	if got := possession("nba", feed(false, miss)); got != "away" {
		t.Errorf("visitor shot: %q", got)
	}
	if got := possession("wnba", feed(false, miss, bp("Defensive Rebound", "H rebound", "H", false))); got != "home" {
		t.Errorf("home rebound: %q", got)
	}
	if got := possession("cbb", feed(false, miss)); got != "away" {
		t.Errorf("cbb: %q", got)
	}
	if got := possession("nhl", feed(false, miss)); got != "" {
		t.Errorf("hockey has no marker: %q", got)
	}
	if got := possession("nba", feed(false)); got != "" {
		t.Errorf("no plays: %q", got)
	}
	if got := possession("nba", feed(true, miss)); got != "" {
		t.Errorf("final: %q", got)
	}
	typ["name"] = "STATUS_END_PERIOD"
	if got := possession("nba", feed(false, miss)); got != "" {
		t.Errorf("between periods: %q", got)
	}
	typ["name"] = "STATUS_HALFTIME"
	if got := possession("nba", feed(false, miss)); got != "" {
		t.Errorf("halftime: %q", got)
	}
}
