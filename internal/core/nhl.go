package core

import (
	"fmt"
	"regexp"
	"sort"
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
		Title:   FormatTitle(away.Abbr, awayShown, clock, period, home.Abbr, homeShown),
		Body:    nhlBody(p, espn.PlayText(p)),
		Urgent:  espn.Truthy(p["scoringPlay"]) || espn.Truthy(espn.Map(p["type"])["penaltyMinutes"]),
		Home:    home,
		Away:    away,
		Clock:   clock,
		Period:  period,
		Display: icingDisplay(nhlIcingTeam(f, p)),
	}
}

func icingDisplay(team string) string {
	if team == "" {
		return ""
	}
	return "Icing (" + team + ")"
}

// nhlIcingTeam is the abbreviation of the team an icing is called against, or
// "". ESPN gives an icing no team, but it does give where the puck was shot
// from, which is in the icing team's own end (the faceoff after it is in that
// end too: 43 of 43 in five games). The team that defends that end is the one
// whose shots in the period come from the other end.
func nhlIcingTeam(f *Feed, p espn.Data) string {
	if espn.PlayText(p) != "Icing" {
		return ""
	}
	x, ok := espn.Float(espn.Map(p["coordinate"])["x"])
	if !ok || x == 0 {
		return ""
	}
	home, away := espn.Teams(espn.Competition(f.Data))
	if home == nil || away == nil {
		return ""
	}
	period, _ := espn.Int64(espn.Map(p["period"])["number"])
	sign := func(v float64) int {
		if v > 0 {
			return 1
		}
		return -1
	}
	for _, t := range []struct {
		c     espn.Data
		other espn.Data
		abbr  string
	}{{home, away, f.Home.Abbr}, {away, home, f.Away.Abbr}} {
		if att := nhlAttackSign(f.Plays, period, espn.Str(t.c["id"])); att != 0 {
			if att == -sign(x) {
				return t.abbr
			}
			if t.abbr == f.Home.Abbr {
				return f.Away.Abbr
			}
			return f.Home.Abbr
		}
	}
	return ""
}

// nhlAttackSign is +1 or -1 for the end a team shoots at in a period (the sign
// of the middle of its shots' x), 0 when unknown. A period with no shots from
// the team uses the one before it: the teams switch ends every period.
func nhlAttackSign(plays []espn.Data, period int64, team string) int {
	for n, flip := period, 1; n >= 1 && n <= 3; n, flip = n-1, -flip {
		var xs []float64
		for _, q := range plays {
			if m, _ := espn.Int64(espn.Map(q["period"])["number"]); m != n {
				continue
			}
			switch playType(q) {
			case "Shot", "Missed", "Goal":
			default:
				continue
			}
			if espn.Str(espn.Map(q["team"])["id"]) != team {
				continue
			}
			if x, ok := espn.Float(espn.Map(q["coordinate"])["x"]); ok {
				xs = append(xs, x)
			}
		}
		if len(xs) >= 2 {
			sort.Float64s(xs)
			if xs[len(xs)/2] > 0 {
				return flip
			}
			return -flip
		}
	}
	return 0
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
