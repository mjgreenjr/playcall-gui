package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"playcall/internal/espn"
)

// nhlRules are the NHL rules: play ids are not chronological, so list order
// and sent ids decide what is new; the title shows the time left (ESPN's
// clock counts up) and the side with the power play; catch-up limit 10.
type nhlRules struct{}

func (nhlRules) CatchupLimit() int { return 10 }
func (nhlRules) TrackSent() bool   { return true }

func (nhlRules) Prepare(f *Feed) PlayRules {
	home, away := espn.Teams(espn.Competition(f.Data))
	return nhlPlays{f: f, powerPlays: nhlPowerPlays(f.Plays, home, away)}
}

type nhlPlays struct {
	f          *Feed
	powerPlays map[string]string // play id -> "home", "away" or ""
}

// nhlSilentTypes are never announced: hits, and the period start/end markers
// that the opening faceoff and closing stoppage already cover. Unknown types
// are announced.
var nhlSilentTypes = map[string]bool{"Hit": true, "Period Start": true, "Period End": true}

// nhlOvertimeSeconds is regular-season overtime; playoff overtime is a full
// period.
const nhlOvertimeSeconds = 300

func (nhlPlays) Announced(p espn.Data) bool { return !nhlSilentTypes[playType(p)] }
func (nhlPlays) IDs(p espn.Data) []string   { return []string{espn.PlayKey(p)} }

func (r nhlPlays) Message(p espn.Data) Message {
	f := r.f
	// The play's own score and time left (ESPN's play clock counts up).
	away, home := f.Away, f.Home
	if v := p["awayScore"]; v != nil {
		away.Score = espn.ScoreText(v)
	}
	if v := p["homeScore"]; v != nil {
		home.Score = espn.ScoreText(v)
	}
	// The side with the power play gets "PP" after its score, until it ends.
	awayShown, homeShown := away.Score, home.Score
	switch r.powerPlays[espn.PlayKey(p)] {
	case "away":
		away.PowerPlay = true
		awayShown += " PP"
	case "home":
		home.PowerPlay = true
		homeShown += " PP"
	}
	clock := nhlClockLeft(f.Data, p)
	period := espn.Str(espn.Map(p["period"])["displayValue"])
	return Message{
		Title:  FormatTitle(away.Abbr, awayShown, clock, period, home.Abbr, homeShown),
		Body:   nhlBody(p, espn.PlayText(p)),
		Urgent: espn.Truthy(p["scoringPlay"]) || espn.Truthy(espn.Map(p["type"])["penaltyMinutes"]),
		Home:   home,
		Away:   away,
		Clock:  clock,
		Period: period,
	}
}

// intOr is Python's int(v or def): def when v is falsy or not an integer.
func intOr(v any, def int) int {
	if !espn.Truthy(v) {
		return def
	}
	n, ok := espn.Int64(v)
	if !ok {
		return def
	}
	return int(n)
}

// nhlPeriodSeconds is the length of period number: the regulation clock, or
// 5 minutes for regular-season overtime.
func nhlPeriodSeconds(data espn.Data, number int) int {
	regulation := espn.Map(espn.Map(data["format"])["regulation"])
	length := intOr(regulation["clock"], 1200)
	if number <= intOr(regulation["periods"], 3) {
		return length
	}
	season := espn.Map(espn.Map(data["header"])["season"])
	if espn.Str(season["type"]) == "3" {
		return length
	}
	return nhlOvertimeSeconds
}

var nhlClockRE = regexp.MustCompile(`^(\d+):(\d{2})$`)

// nhlClockLeft converts ESPN's count-up play clock into time left in the
// period ("1:57" -> "18:03"), or returns ESPN's value as is if it can't.
func nhlClockLeft(data, p espn.Data) string {
	shown := espn.Str(espn.Map(p["clock"])["displayValue"])
	m := nhlClockRE.FindStringSubmatch(shown)
	number, ok := espn.Int64(espn.Map(p["period"])["number"])
	if m == nil || !ok || number == 0 {
		return shown
	}
	mins, err1 := strconv.Atoi(m[1])
	secs, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return shown
	}
	left := nhlPeriodSeconds(data, int(number)) - mins*60 - secs
	if left < 0 {
		return shown
	}
	return fmt.Sprintf("%d:%02d", left/60, left%60)
}

// nhlBody is the play text; penalties add their length, e.g.
// "... Hooking against X · 2 min Minor".
func nhlBody(p espn.Data, text string) string {
	typ := espn.Map(p["type"])
	if minutes := typ["penaltyMinutes"]; espn.Truthy(minutes) {
		penalty := fmt.Sprintf("%s min %s", espn.ScoreText(minutes), espn.Str(typ["penaltyType"]))
		return text + " · " + strings.TrimSpace(penalty)
	}
	return text
}

// nhlPowerPlays walks the plays in order and returns, per play id, which side
// ("home", "away" or "") has the power play. ESPN's strength is relative to
// the play's team (Power Play: that team has the advantage; Shorthanded: the
// other team does), and plays with no team (stoppages) keep the last known
// side. Penalty plays are skipped: their label is set before the penalty takes
// effect. home and away may be nil. Team ids are compared as text, since ESPN
// sends them as strings or numbers.
func nhlPowerPlays(plays []espn.Data, home, away espn.Data) map[string]string {
	sides := map[string]string{}
	for _, c := range []struct {
		side string
		comp espn.Data
	}{{"home", home}, {"away", away}} {
		if c.comp != nil {
			sides[nhlTeamKey(espn.Map(c.comp["team"]))] = c.side
		}
	}
	result := map[string]string{}
	current := ""
	for _, p := range plays {
		label := espn.Str(espn.Map(p["strength"])["text"])
		if !espn.Truthy(espn.Map(p["type"])["penaltyMinutes"]) {
			side, known := sides[nhlTeamKey(espn.Map(p["team"]))]
			switch {
			case label == "Even Strength":
				current = ""
			case known && (label == "Power Play" || label == "Shorthanded"):
				other := "home"
				if side == "home" {
					other = "away"
				}
				if label == "Power Play" {
					current = side
				} else {
					current = other
				}
			}
		}
		result[espn.PlayKey(p)] = current
	}
	return result
}

// nhlTeamKey is a team's id as text; a team with no id shares one key, as
// None does in the Python.
func nhlTeamKey(team espn.Data) string {
	if v, ok := team["id"]; ok && v != nil {
		return espn.ScoreText(v)
	}
	return "None"
}
