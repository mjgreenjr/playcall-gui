package core

import (
	"strings"
	"time"

	"playcall/internal/espn"
)

// GameStatus is the game as of the last successful fetch: what a persistent
// header shows between plays. It is built from ESPN's game-level status, not
// from a play, so it does not inherit a play's snapshot of the clock. The
// clock is ESPN's displayClock as fetched; nothing ticks locally.
type GameStatus struct {
	GameID, Sport string
	Away, Home    TeamScore
	// State is ESPN's status state: "pre", "in" or "post" ("" if unknown).
	State string
	// Clock and Period are ESPN's displayClock and period text.
	Clock, Period string
	// Detail is ESPN's shortDetail ("Final", "End of 2nd", "10/9 - 9:00 PM EDT").
	Detail string
	// Halftime is true for the whole break, not just the end-of-half play.
	Halftime bool
	// Break is true between periods ("End of 3rd"), when ESPN's clock sits at
	// zero until the next one starts; Detail then says what to show.
	Break bool
	Final bool
	// Series is the postseason standing without parentheses, or "".
	Series string
	// Situation is football's down, distance and spot after the last play
	// ("1st & 10 at WASH 39"), "" when there is none (other sports, kickoffs,
	// between plays, not in play).
	Situation string
	// AwayRecord and HomeRecord are the teams' season and series records.
	AwayRecord, HomeRecord TeamRecord
	// Possession is the side on offence: "away", "home", or "" when the sport
	// has no such notion (hockey, basketball: ESPN does not say), the game is
	// not in play (before the start, halftime, final) or ESPN does not say.
	Possession string
	// AwayCounts and HomeCounts are basketball's timeouts and foul trouble;
	// nil for other sports, before the start and after the end.
	AwayCounts, HomeCounts *TeamCounts
	// UpdatedAt is when ESPN last answered, so a header can dim or show
	// "updated Ns ago" while polls fail and the loop backs off.
	UpdatedAt time.Time
}

// TeamRecord is what ESPN says about a team's season, each part "" when it
// does not send it: Overall ("3-1"), Conf (the conference record, college
// sports only) and SeriesWins (wins in the current playoff series, "" outside
// one; ESPN counts completed games only).
type TeamRecord struct {
	Overall, Conf, SeriesWins string
}

func teamRecord(f *Feed, c espn.Data) TeamRecord {
	if c == nil {
		return TeamRecord{}
	}
	return TeamRecord{
		Overall:    espn.Record(c, "total"),
		Conf:       espn.Record(c, "vsconf"),
		SeriesWins: espn.SeriesWins(espn.Competition(f.Data), c),
	}
}

// TeamCounts is what a basketball scoreboard shows under a team's score.
type TeamCounts struct {
	// Timeouts is how many the team has left in the game. TimeoutSlots is how
	// many it had to start with: those left plus the timeouts it has taken, so
	// a row of that many dots, Timeouts of them filled, shows the rest. (ESPN's
	// own timeoutsUsed is for the current period only.)
	Timeouts, TimeoutSlots int
	// Fouls is the team's fouls this period.
	Fouls int
	// Bonus is "" when the team is not shooting free throws on the opponent's
	// fouls, else "single" (one-and-one) or "double" (two shots). ESPN calls
	// the NBA's and WNBA's bonus "DOUBLE" as well.
	Bonus string
}

// basketballCounts reads each team's timeouts and fouls from the game's
// competitors. ESPN's foulsToGive and bonusState are about the team they sit
// on: bonusState says that team is in the bonus.
func basketballCounts(sport string, f *Feed) (away, home *TeamCounts) {
	if sport != "nba" && sport != "wnba" && sport != "cbb" {
		return nil, nil
	}
	if f.Finished || espn.Str(espn.Map(espn.Status(f.Data)["type"])["state"]) != "in" {
		return nil, nil
	}
	h, a := espn.Teams(espn.Competition(f.Data))
	away, home = teamCounts(a, f.Plays), teamCounts(h, f.Plays)
	// NBA and WNBA: 7 a game, but no more than 4 in the 4th quarter and 2 in
	// each overtime. ESPN's timeoutsRemaining is not capped (a team that took
	// both of its overtime timeouts still shows one left), so do it here.
	period, _ := espn.Int64(espn.Status(f.Data)["period"])
	if sport == "cbb" {
		capCollegeTimeouts(away, int(period))
		capCollegeTimeouts(home, int(period))
	} else if period >= 4 {
		limit, overtime := fourthQuarterTimeouts, false
		if period > 4 {
			limit, overtime = overtimeTimeouts, true
		}
		capTimeouts(away, a, limit, overtime)
		capTimeouts(home, h, limit, overtime)
	}
	return away, home
}

// Most timeouts a pro team may use in the 4th quarter and in each overtime.
const (
	fourthQuarterTimeouts = 4
	overtimeTimeouts      = 2
)

// capTimeouts lowers the timeouts a team can still use to what the period's
// limit allows, given what it used so far in the period (ESPN's timeoutsUsed
// is per period). In the 4th quarter the team's starting total stays as it is;
// in overtime the row is just the period's allowance, since what was left from
// regulation does not matter.
func capTimeouts(c *TeamCounts, competitor espn.Data, limit int, overtime bool) {
	if c == nil {
		return
	}
	used, _ := espn.Int64(competitor["timeoutsUsed"])
	c.Timeouts = min(c.Timeouts, max(limit-int(used), 0))
	if overtime {
		c.TimeoutSlots = limit
	}
}

// Men's college basketball: 4 timeouts a game and one more for each overtime.
// What is not used carries over, so after 5 overtimes a team that never called
// one has 9. The count is worked out here from the timeouts taken rather than
// trusting ESPN's timeoutsRemaining, which may not include the extra ones.
const (
	collegeTimeouts = 4
	collegeHalves   = 2
)

func capCollegeTimeouts(c *TeamCounts, period int) {
	if c == nil {
		return
	}
	taken := c.TimeoutSlots - c.Timeouts
	c.TimeoutSlots = collegeTimeouts + max(period-collegeHalves, 0)
	c.Timeouts = max(c.TimeoutSlots-taken, 0)
}

// timeoutsTaken counts the timeout plays that belong to a team. A timeout
// called by nobody (the "Official timeout") has no team and is not counted.
func timeoutsTaken(plays []espn.Data, teamID string) int {
	n := 0
	for _, p := range plays {
		if strings.Contains(strings.ToLower(playType(p)), "timeout") && espn.Str(espn.Map(p["team"])["id"]) == teamID {
			n++
		}
	}
	return n
}

// teamCounts is nil when the competitor has neither timeouts nor fouls.
func teamCounts(c espn.Data, plays []espn.Data) *TeamCounts {
	if c == nil {
		return nil
	}
	timeouts, okT := espn.Int64(c["timeoutsRemaining"])
	fouls := espn.Map(c["fouls"])
	current, okF := espn.Int64(fouls["teamFoulsCurrent"])
	if !okT && !okF {
		return nil
	}
	bonus := ""
	switch strings.ToLower(espn.Str(fouls["bonusState"])) {
	case "", "none":
	case "double":
		bonus = "double"
	default:
		bonus = "single"
	}
	return &TeamCounts{Timeouts: int(timeouts), TimeoutSlots: int(timeouts) + timeoutsTaken(plays, espn.Str(c["id"])), Fouls: int(current), Bonus: bonus}
}

// StatusSink receives a GameStatus after every successful fetch, before that
// poll's plays are sent. It is called on the poll goroutine and must not
// block; a GUI should hand the value to its own goroutine.
type StatusSink interface {
	Status(GameStatus)
}

// StatusFunc adapts a function to a StatusSink.
type StatusFunc func(GameStatus)

// Status calls f.
func (f StatusFunc) Status(s GameStatus) { f(s) }

func (p *Poller) publishStatus(f *Feed) {
	if p.StatusSink == nil {
		return
	}
	st := espn.Map(espn.Status(f.Data)["type"])
	period := f.Period
	if br, ok := p.Rules.(basketballRules); ok {
		// ESPN's status says "5th" in overtime; the plays say "OT".
		if n := espn.Status(f.Data)["period"]; espn.Truthy(n) {
			period = nbaPeriod(espn.Data{"period": espn.Data{"number": n, "displayValue": f.Period}}, br.regulation)
		}
	}
	awayCounts, homeCounts := basketballCounts(p.Sport, f)
	homeComp, awayComp := espn.Teams(espn.Competition(f.Data))
	series := f.Series
	if f.Finished {
		series = seriesText(f.Data, p.State.GameID, true)
	}
	p.StatusSink.Status(GameStatus{
		GameID:     p.State.GameID,
		Sport:      p.Sport,
		Away:       f.Away,
		Home:       f.Home,
		State:      espn.Str(st["state"]),
		Clock:      f.Clock,
		Period:     period,
		AwayCounts: awayCounts,
		HomeCounts: homeCounts,
		Detail:     espn.Str(st["shortDetail"]),
		Halftime:   espn.Str(st["name"]) == "STATUS_HALFTIME",
		Break:      espn.Str(st["name"]) == "STATUS_END_PERIOD",
		Final:      f.Finished,
		Series:     series,
		AwayRecord: teamRecord(f, awayComp),
		HomeRecord: teamRecord(f, homeComp),
		Possession: possession(p.Sport, f),
		Situation:  situation(p.Sport, f),
		UpdatedAt:  p.now(),
	})
}

// possession finds the side on offence from the game feed.
//
// Basketball: worked out from the plays (ESPN does not say). Football: the team the last play ended with the ball (a punt or kickoff
// ends with the receiver), else the one that started it. Baseball: the team
// batting in the current half inning; between halves nobody is.
func possession(sport string, f *Feed) string {
	if !inPlay(f) {
		return ""
	}
	typ := espn.Map(espn.Status(f.Data)["type"])
	home, away := espn.Teams(espn.Competition(f.Data))
	if home == nil || away == nil {
		return ""
	}
	switch sport {
	case "nfl", "cfb":
		if len(f.Plays) == 0 {
			return ""
		}
		last := f.Plays[len(f.Plays)-1]
		id := espn.Str(espn.Map(espn.Map(last["end"])["team"])["id"])
		if id == "" {
			id = espn.Str(espn.Map(espn.Map(last["start"])["team"])["id"])
		}
		switch id {
		case "":
			return ""
		case espn.Str(away["id"]):
			return "away"
		case espn.Str(home["id"]):
			return "home"
		}
	case "nba", "wnba", "cbb":
		// Inferred from the plays; see basketballPossession.
		if len(f.Plays) == 0 || espn.Str(typ["name"]) == "STATUS_END_PERIOD" {
			return ""
		}
		switch basketballPossession(f.Plays, espn.Str(away["id"]), espn.Str(home["id"])) {
		case "":
			return ""
		case espn.Str(away["id"]):
			return "away"
		case espn.Str(home["id"]):
			return "home"
		}
	case "mlb":
		detail := strings.ToLower(espn.Str(typ["shortDetail"]))
		switch {
		case strings.HasPrefix(detail, "top"):
			return "away"
		case strings.HasPrefix(detail, "bot"):
			return "home"
		}
	}
	return ""
}

// inPlay is true while the game is being played: not before the start, at
// halftime or after the end.
func inPlay(f *Feed) bool {
	typ := espn.Map(espn.Status(f.Data)["type"])
	return !f.Finished && espn.Str(typ["state"]) == "in" && espn.Str(typ["name"]) != "STATUS_HALFTIME"
}

// situation is the football down and distance after the last play.
func situation(sport string, f *Feed) string {
	if (sport != "nfl" && sport != "cfb") || !inPlay(f) || len(f.Plays) == 0 {
		return ""
	}
	last := f.Plays[len(f.Plays)-1]
	if isTimeout(last) {
		// A timeout changes nothing on the field and has no "after" of its
		// own, and its "start" can be stale (after a punt it still shows the
		// fourth down). So it is the situation after the last real play. If
		// that play has none (a score, a kickoff), there is none to show.
		for i := len(f.Plays) - 2; i >= 0; i-- {
			if !isTimeout(f.Plays[i]) {
				return downDistanceFull(espn.Map(f.Plays[i]["end"]))
			}
		}
		return downDistanceFull(espn.Map(last["start"]))
	}
	return downDistanceFull(espn.Map(last["end"]))
}

func isTimeout(p espn.Data) bool {
	return strings.Contains(strings.ToLower(playType(p)), "timeout")
}

// downDistanceFull is "1st & 10 at NO 25", or the short form, from a play's
// start or end.
func downDistanceFull(side espn.Data) string {
	if text := espn.Str(side["downDistanceText"]); text != "" {
		return text
	}
	return espn.Str(side["shortDownDistanceText"])
}
