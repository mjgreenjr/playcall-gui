package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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
	// HalftimeEnds is when the break is expected to end (football: the
	// end-of-half play's wall-clock time plus the usual break); zero if unknown.
	HalftimeEnds time.Time
	// Break is true between periods ("End of 3rd"), when ESPN's clock sits at
	// zero until the next one starts; Detail then says what to show.
	Break bool
	Final bool
	// ScoreBehind is true when ESPN's own scores lagged the plays and the
	// newest play's were used; a recheck should wait for ESPN's.
	ScoreBehind bool
	// Preseason is true for exhibition games (ESPN's season type 1).
	Preseason bool
	// Postseason is true for playoff and bowl games (season type 3).
	Postseason bool
	// Series is the postseason standing without parentheses, or "".
	// SeriesInfo is which game of a playoff series this is and its length
	// ("NLCS - Game 1 · Best of 7"), or "".
	Series, SeriesInfo string
	// Situation is football's down, distance and spot after the last play
	// ("1st & 10 at WASH 39"), "" when there is none (other sports, kickoffs,
	// between plays, not in play).
	Situation string
	// Bases is baseball's occupied bases ("1", "13", "123", "" for none),
	// meaningful while Situation is not empty.
	Bases string
	// Balls, Strikes and Outs are baseball's count and outs, meaningful while
	// Situation is not empty.
	Balls, Strikes, Outs int
	// Pitcher and Batter are baseball's current at-bat, and BattingSide the
	// side ("away" or "home") the batter is on; "" between half-innings.
	Pitcher, Batter, BattingSide string
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
	period, _ := espn.Int64(espn.Status(f.Data)["period"])
	if sport == "nba" && !espnCountsUsable(a, h, f.Plays) {
		a, h = countsFromPlays(a, h, f.Plays, period), countsFromPlays(h, a, f.Plays, period)
	}
	if sport == "nba" {
		a, h = withRetainedTimeouts(a, f.Plays, period), withRetainedTimeouts(h, f.Plays, period)
	}
	away, home = teamCounts(a, f.Plays), teamCounts(h, f.Plays)
	if sport == "nba" {
		nbaFouls(away, a, f.Plays, period)
		nbaFouls(home, h, f.Plays, period)
	}
	// NBA and WNBA: 7 a game, but no more than 4 in the 4th quarter and 2 in
	// each overtime. ESPN's timeoutsRemaining is not capped in overtime (a team
	// that took both of its overtime timeouts still shows one left), so do it
	// here; in regulation it is cut to 4 early, which the row's 7 allows for.
	if sport == "cbb" {
		capCollegeTimeouts(away, int(period))
		capCollegeTimeouts(home, int(period))
	} else if period <= 4 {
		raiseTimeoutSlots(away)
		raiseTimeoutSlots(home)
	}
	if sport != "cbb" && period >= 4 {
		limit, overtime := fourthQuarterTimeouts, false
		if period > 4 {
			limit, overtime = overtimeTimeouts, true
		}
		capTimeouts(away, a, limit, overtime)
		capTimeouts(home, h, limit, overtime)
	}
	return away, home
}

// espnCountsUsable is whether ESPN's timeouts and fouls can be trusted. CLE @
// PHI on 2026-10-22 had no counts at all in the feed for the first minutes,
// then 0 timeouts left for both teams (also after PHI's first timeout, with 1
// used), the bonus for both and fouls that did not match the plays. A team
// with none left must have taken all 7 by the plays, so a 0 that the plays
// do not back up is no data.
func espnCountsUsable(away, home espn.Data, plays []espn.Data) bool {
	if away == nil || home == nil {
		return true
	}
	for _, c := range []espn.Data{away, home} {
		left, ok := espn.Int64(c["timeoutsRemaining"])
		if !ok || left == 0 && timeoutsTaken(plays, espn.Str(c["id"])) < proTimeouts {
			return false
		}
	}
	return true
}

// withRetainedTimeouts gives a team that kept a timeout on a challenge its
// timeout counts from the plays. ESPN's timeoutsRemaining mostly does not give
// the timeout back (2026-10-20 to 25, 43 games: a team with an earlier
// retained challenge was 1 low in 34 of 40 readings, against 227 of 280 right
// without one), so the row would show a circle too few for the rest of the
// game, and the 4th quarter's cap would count the kept timeout as used. The
// competitor is copied; ESPN's data is left as it was.
func withRetainedTimeouts(c espn.Data, plays []espn.Data, period int64) espn.Data {
	id := espn.Str(c["id"])
	overtime := period > 4
	// Overtime starts over with 2 (BKN @ BOS on 2026-10-27: 0 and 0 used,
	// 2 left for both after a regulation that used all 7), so only a kept
	// timeout in this period matters there, and what is left is out of 2.
	scope := int64(0)
	if overtime {
		scope = period
	}
	if c == nil || !hasRetainedTimeout(plays, id, scope) {
		return c
	}
	out := make(espn.Data, len(c)+2)
	for k, v := range c {
		out[k] = v
	}
	left := proTimeouts - netTimeouts(plays, id, 0)
	if overtime {
		left = overtimeTimeouts - netTimeouts(plays, id, period)
	}
	out["timeoutsRemaining"] = json.Number(strconv.Itoa(max(left, 0)))
	out["timeoutsUsed"] = json.Number(strconv.Itoa(netTimeouts(plays, id, period)))
	return out
}

// hasRetainedTimeout is whether a challenge of the team kept its timeout, next
// to the timeout play it refunds, in the given period (0 for any).
func hasRetainedTimeout(plays []espn.Data, teamID string, period int64) bool {
	for i, p := range plays {
		if espn.Str(espn.Map(p["team"])["id"]) != teamID || !retainedChallenge(p) || !timeoutNextTo(plays, i) {
			continue
		}
		if n, _ := espn.Int64(espn.Map(p["period"])["number"]); period == 0 || n == period {
			return true
		}
	}
	return false
}

// countsFromPlays builds the fields teamCounts reads from the plays alone:
// 7 timeouts less the ones taken (a challenge that kept its timeout is given
// back), the timeouts taken this period, the team's fouls this period, and the
// bonus when the other team has four. The last two minutes of a period, when
// the NBA gives the bonus earlier, are not modelled. teamFoulsCurrent includes
// the offensive fouls because nbaFouls takes them off again.
func countsFromPlays(c, other espn.Data, plays []espn.Data, period int64) espn.Data {
	if c == nil {
		return nil
	}
	num := func(n int) json.Number { return json.Number(strconv.Itoa(n)) }
	id := espn.Str(c["id"])
	fouls, offensive := periodFouls(plays, id, period)
	otherFouls, _ := periodFouls(plays, espn.Str(other["id"]), period)
	bonus := "NONE"
	if otherFouls >= nbaTeamFouls {
		bonus = "DOUBLE"
	}
	return espn.Data{
		"id":                id,
		"timeoutsRemaining": num(max(proTimeouts-netTimeouts(plays, id, 0), 0)),
		"timeoutsUsed":      num(netTimeouts(plays, id, period)),
		"fouls": espn.Data{
			"teamFoulsCurrent": num(fouls + offensive),
			"foulsToGive":      num(max(nbaTeamFouls-fouls, 0)),
			"bonusState":       bonus,
		},
	}
}

// periodFouls counts a team's team fouls in a period, and separately its
// offensive fouls, which the NBA does not count against the team. Technical
// fouls are not team fouls either.
func periodFouls(plays []espn.Data, teamID string, period int64) (fouls, offensive int) {
	for _, p := range plays {
		if espn.Str(espn.Map(p["team"])["id"]) != teamID {
			continue
		}
		if n, _ := espn.Int64(espn.Map(p["period"])["number"]); n != period {
			continue
		}
		t := strings.ToLower(playType(p))
		switch {
		case !strings.Contains(t, "foul"), strings.Contains(t, "technical"), strings.Contains(t, "turnover"):
		case strings.Contains(t, "offensive"):
			offensive++
		default:
			fouls++
		}
	}
	return fouls, offensive
}

// netTimeouts is timeoutsTaken for one period (0 for the whole game) less the
// challenges whose text says the team keeps its timeout. ESPN's own counter
// gave those back in 7 of 8 cases in the first two NBA nights.
func netTimeouts(plays []espn.Data, teamID string, period int64) int {
	n := 0
	for i, p := range plays {
		if espn.Str(espn.Map(p["team"])["id"]) != teamID {
			continue
		}
		if pe, _ := espn.Int64(espn.Map(p["period"])["number"]); period > 0 && pe != period {
			continue
		}
		switch {
		case isTimeout(p):
			n++
		case chargedChallenge(p) && !timeoutNextTo(plays, i):
			n++
		case retainedChallenge(p) && timeoutNextTo(plays, i):
			n--
		}
	}
	return max(n, 0)
}

func retainedChallenge(p espn.Data) bool {
	return strings.Contains(strings.ToLower(playType(p)), "challenge") &&
		strings.Contains(strings.ToLower(espn.Str(p["text"])), "retain their timeout")
}

// nbaTeamFouls is how many team fouls a team can commit before the other side
// shoots free throws; ESPN's foulsToGive counts down from it.
const nbaTeamFouls = 4

// nbaFouls sets an NBA team's fouls this period. While the team is short of
// the bonus ESPN's foulsToGive is the count to trust: it is 4 minus the team
// fouls and leaves out offensive fouls, and it holds up when ESPN retypes or
// reorders plays after posting them. At 0 the team is in the bonus and the
// number no longer says how far past 4 it is, so the count comes from
// teamFoulsCurrent less the period's offensive fouls (dropOffensiveFouls).
func nbaFouls(c *TeamCounts, competitor espn.Data, plays []espn.Data, period int64) {
	if c == nil {
		return
	}
	if toGive, ok := espn.Int64(espn.Map(competitor["fouls"])["foulsToGive"]); ok && toGive >= 1 && toGive <= nbaTeamFouls {
		c.Fouls = nbaTeamFouls - int(toGive)
		return
	}
	dropOffensiveFouls(c, competitor, plays, period)
}

// dropOffensiveFouls takes a team's offensive fouls this period off its foul
// count. The NBA does not count them as team fouls, so they do not move a team
// toward the bonus or show on the arena's board, but ESPN's teamFoulsCurrent
// does (OKC @ SA on 2026-10-20: the board said 3 and ESPN 4 after an offensive
// foul, while ESPN's own foulsToGive, 1, agreed with the board). Only the NBA
// is known to work this way; the WNBA and college are left as ESPN sends them.
func dropOffensiveFouls(c *TeamCounts, competitor espn.Data, plays []espn.Data, period int64) {
	if c == nil {
		return
	}
	id := espn.Str(competitor["id"])
	for _, p := range plays {
		if espn.Str(espn.Map(p["team"])["id"]) != id {
			continue
		}
		if n, _ := espn.Int64(espn.Map(p["period"])["number"]); n != period {
			continue
		}
		if t := playType(p); t == "Offensive Foul" || t == "Offensive Charge" {
			c.Fouls = max(c.Fouls-1, 0)
		}
	}
}

// NFL teams get 3 timeouts in each half and 2 in overtime. ESPN's
// timeoutsUsed counts the current half only (it drops to 0 as the 2nd quarter
// ends, so halftime shows 3 each) and runs a play or two ahead of the numbered
// timeout plays ("Timeout #2 by PHI at 01:06."), which agreed with it in
// every game checked. College's counter is a running total and its timeout
// plays include official ones, so only the NFL is shown.
const (
	nflHalfTimeouts     = 3
	nflOvertimeTimeouts = 2
)

// footballCounts reads each NFL team's timeouts left in the half from the
// competitors; nil when the game is not in play or ESPN sends no counter.
// Fouls are not part of it (Fouls stays 0 and the GUI leaves them out).
func footballCounts(f *Feed) (away, home *TeamCounts) {
	if f.Finished || espn.Str(espn.Map(espn.Status(f.Data)["type"])["state"]) != "in" {
		return nil, nil
	}
	period, _ := espn.Int64(espn.Status(f.Data)["period"])
	slots := nflHalfTimeouts
	if period > 4 {
		slots = nflOvertimeTimeouts
	}
	h, a := espn.Teams(espn.Competition(f.Data))
	count := func(c espn.Data) *TeamCounts {
		used, ok := espn.Int64(c["timeoutsUsed"])
		if !ok {
			return nil
		}
		return &TeamCounts{Timeouts: min(max(slots-int(used), 0), slots), TimeoutSlots: slots}
	}
	return count(a), count(h)
}

// collegeHalfTimeouts is a college team's timeouts in each half; unused ones
// do not carry over to the second half.
const collegeHalfTimeouts = 3

// collegeCounts is each college team's timeouts left in the half. ESPN's
// timeoutsUsed counts charged timeouts for the whole game, never resetting
// (the plays cannot be used instead: ESPN files official timeouts under a
// team too), so the second half's number needs the first half's total, taken
// while the first half is in play or at the break and kept here. A run that
// starts in the second half does not have it: it shows nothing, unless no
// team has used a timeout (so the first half's total was 0). Overtime has one
// timeout per team in each period (see collegeOvertime). Nil also when the
// game is not in play or ESPN sends no counter.
func (p *Poller) collegeCounts(f *Feed) (away, home *TeamCounts) {
	st := espn.Status(f.Data)
	typ := espn.Map(st["type"])
	if f.Finished || espn.Str(typ["state"]) != "in" {
		return nil, nil
	}
	period, ok := espn.Int64(st["period"])
	if !ok || period < 1 {
		return nil, nil
	}
	h, a := espn.Teams(espn.Competition(f.Data))
	if h == nil || a == nil {
		return nil, nil
	}
	sides := [2]string{"away", "home"}
	var used [2]int64
	for i, c := range []espn.Data{a, h} {
		u, ok := espn.Int64(c["timeoutsUsed"])
		if !ok {
			return nil, nil
		}
		used[i] = u
	}
	if period > 4 {
		return p.collegeOvertime(period, used)
	}
	p.collegeWatched = true
	if period <= 2 {
		if p.collegeFirstHalf == nil {
			p.collegeFirstHalf = map[string]int64{}
		}
		for i, side := range sides {
			p.collegeFirstHalf[side] = used[i]
		}
		p.saveFirstHalf(used, espn.Str(typ["name"]) == "STATUS_HALFTIME")
	} else if p.collegeFirstHalf == nil && p.FirstHalfStore != nil {
		// A restart in the second half: the totals saved at the break, if the
		// last run saw it (one saved while the half was in play can be short).
		if h, ok := p.FirstHalfStore.Load(p.State.GameID); ok && h.Final {
			p.collegeFirstHalf = map[string]int64{"away": h.Away, "home": h.Home}
		}
	}
	if period >= 3 && p.collegeFirstHalf == nil && used[0] == 0 && used[1] == 0 {
		// Nobody has used one, so the first half's total was 0 for both; keep
		// that for when the first one is taken.
		p.collegeFirstHalf = map[string]int64{"away": 0, "home": 0}
	}
	var left [2]int64
	for i, side := range sides {
		switch {
		case espn.Str(typ["name"]) == "STATUS_HALFTIME":
			left[i] = collegeHalfTimeouts // the second half's, as the NFL's
		case period <= 2:
			left[i] = collegeHalfTimeouts - used[i]
		default:
			base, known := p.collegeFirstHalf[side]
			if !known {
				return nil, nil
			}
			left[i] = collegeHalfTimeouts - (used[i] - base)
		}
		left[i] = min(max(left[i], 0), collegeHalfTimeouts)
	}
	return &TeamCounts{Timeouts: int(left[0]), TimeoutSlots: collegeHalfTimeouts},
		&TeamCounts{Timeouts: int(left[1]), TimeoutSlots: collegeHalfTimeouts}
}

// collegeOvertime is each college team's timeouts in an overtime period: one
// each, gone when ESPN's running counter rises (FSU @ MIA on 2026-10-17: MIA 4
// to 5 and FSU 1 to 2 in the second overtime, none in the first), and a new
// one for each further period. The period's base is the counter at the first
// poll of the period, so it needs a poll that came before it; joined or
// restarted inside an overtime, the base is unknown and nothing is shown for
// that period.
func (p *Poller) collegeOvertime(period int64, used [2]int64) (away, home *TeamCounts) {
	if p.collegeOTPeriod != period {
		p.collegeOTPeriod = period
		p.collegeOTKnown = p.collegeWatched
		p.collegeOTBase = used
	}
	p.collegeWatched = true
	if !p.collegeOTKnown {
		return nil, nil
	}
	left := func(i int) *TeamCounts {
		return &TeamCounts{Timeouts: int(min(max(1-(used[i]-p.collegeOTBase[i]), 0), 1)), TimeoutSlots: 1}
	}
	return left(0), left(1)
}

// saveFirstHalf writes the first-half totals when they or their finality
// changed.
func (p *Poller) saveFirstHalf(used [2]int64, final bool) {
	if p.Once || p.FirstHalfStore == nil {
		return
	}
	h := FirstHalf{Away: used[0], Home: used[1], Final: final}
	if h.Away == p.halfSaved.Away && h.Home == p.halfSaved.Home && h.Final == p.halfSaved.Final && p.halfSaved.At != 0 {
		return
	}
	if err := p.FirstHalfStore.Save(p.State.GameID, h); err != nil {
		slog.Warn("Could not save the first-half timeouts", "err", err)
		return
	}
	h.At = 1
	p.halfSaved = h
}

// Most timeouts a pro team may use in the 4th quarter and in each overtime.
const (
	proTimeouts           = 7
	fourthQuarterTimeouts = 4
	overtimeTimeouts      = 2
)

// raiseTimeoutSlots makes a pro team's regulation row its full 7. ESPN's
// timeoutsRemaining is already cut to 4 by the end of the 3rd (PHX @ POR and
// SAC @ LAC on 2026-10-21: LAC had taken two and showed 4 left, not 5), so
// remaining plus timeouts taken comes up short.
func raiseTimeoutSlots(c *TeamCounts) {
	if c != nil {
		c.TimeoutSlots = max(c.TimeoutSlots, proTimeouts)
	}
}

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
// called by nobody (the "Official timeout", college's "Official TV Timeout")
// has no team and is not counted. A coach's challenge that ESPN's text says
// charged a timeout counts too, unless the team's own timeout play sits right
// next to it at the same clock: the NBA posts one (6 of 6 in the 2026
// preseason), college basketball does not (5 of 5 in last season's games).
func timeoutsTaken(plays []espn.Data, teamID string) int {
	n := 0
	for i, p := range plays {
		if espn.Str(espn.Map(p["team"])["id"]) != teamID {
			continue
		}
		switch {
		case isTimeout(p):
			n++
		case chargedChallenge(p) && !timeoutNextTo(plays, i):
			n++
		}
	}
	return n
}

func chargedChallenge(p espn.Data) bool {
	return strings.Contains(strings.ToLower(playType(p)), "challenge") &&
		strings.Contains(strings.ToLower(espn.Str(p["text"])), "charged with a timeout")
}

// timeoutNextTo is whether a timeout play of the same team, period and clock is
// within two plays of plays[i].
func timeoutNextTo(plays []espn.Data, i int) bool {
	at := func(p espn.Data) (string, string, string) {
		return espn.Str(espn.Map(p["team"])["id"]), espn.Str(espn.Map(p["clock"])["displayValue"]),
			fmt.Sprint(espn.Map(p["period"])["number"])
	}
	team, clock, period := at(plays[i])
	for j := max(i-2, 0); j <= min(i+2, len(plays)-1); j++ {
		if t, c, pe := at(plays[j]); j != i && isTimeout(plays[j]) && t == team && c == clock && pe == period {
			return true
		}
	}
	return false
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
	over := footballGameOver(p.Sport, f)
	if over {
		g := *f
		g.Finished = true // the header, counts and situation read it as final
		f = &g
	}
	period := f.Period
	if br, ok := p.Rules.(basketballRules); ok {
		// ESPN's status says "5th" in overtime; the plays say "OT".
		if n := espn.Status(f.Data)["period"]; espn.Truthy(n) {
			period = nbaPeriod(espn.Data{"period": espn.Data{"number": n, "displayValue": f.Period}}, br.regulation)
		}
	}
	detail := espn.Str(st["shortDetail"])
	if over {
		detail = "Final"
	}
	if footballPeriodBreak(p.Sport, f) && espn.Str(st["name"]) != "STATUS_END_PERIOD" {
		detail = "End of " + period // ESPN's own says "15:00 - 3rd"
	}
	awayCounts, homeCounts := basketballCounts(p.Sport, f)
	switch p.Sport {
	case "nfl":
		awayCounts, homeCounts = footballCounts(f)
	case "cfb":
		awayCounts, homeCounts = p.collegeCounts(f)
	}
	homeComp, awayComp := espn.Teams(espn.Competition(f.Data))
	series := f.Series
	if f.Finished {
		series = seriesText(f.Data, p.State.GameID, true)
	}
	pitcher, batter, side := mlbMatchup(p.Sport, f)
	var sit mlbNow
	if p.Sport == "mlb" {
		sit = mlbSituation(f)
	}
	p.StatusSink.Status(GameStatus{
		Pitcher:      pitcher,
		Batter:       batter,
		BattingSide:  side,
		GameID:       p.State.GameID,
		Sport:        p.Sport,
		Away:         f.Away,
		Home:         f.Home,
		State:        espn.Str(st["state"]),
		Clock:        f.Clock,
		Period:       period,
		AwayCounts:   awayCounts,
		HomeCounts:   homeCounts,
		Detail:       detail,
		Halftime:     espn.Str(st["name"]) == "STATUS_HALFTIME",
		HalftimeEnds: halftimeEnds(p.Sport, f),
		Break:        espn.Str(st["name"]) == "STATUS_END_PERIOD" || footballPeriodBreak(p.Sport, f),
		Final:        f.Finished,
		ScoreBehind:  f.ScoreBehind,
		Series:       series,
		SeriesInfo:   seriesInfo(f.Data, p.State.GameID),
		Preseason:    espn.SeasonType(f.Data) == "1",
		Postseason:   espn.SeasonType(f.Data) == "3",
		AwayRecord:   teamRecord(f, awayComp),
		HomeRecord:   teamRecord(f, homeComp),
		Possession:   possession(p.Sport, f),
		Situation:    situation(p.Sport, f),
		Bases:        sit.bases,
		Balls:        sit.balls,
		Strikes:      sit.strikes,
		Outs:         sit.outs,
		UpdatedAt:    p.now(),
	})
}

// The usual halftime break, counted from the first half's last play stamp to
// the kickoff play's. NFL: eight games, 13:57 to 15:37 with most near 14:15
// (BUF @ LAR on 2026-10-12 took 13:59; the one in London took 19:45). College: 45 finished games, median 20:40,
// 40 of them between 19:36 and 21:36 (the rest were delays); DEL @ MTSU on 2026-10-13 took 21:03.
const (
	nflHalftime = 14*time.Minute + 15*time.Second
	cfbHalftime = 20*time.Minute + 45*time.Second
)

// halftimeEnds estimates when the football halftime break ends: the first
// half's last play's `wallclock` plus the usual break. The NFL ends the half
// with an End of Half play, college with the 2nd quarter's End Period. Zero
// outside halftime or when ESPN gives no stamp. It comes from the feed, so a
// late start has it too.
func halftimeEnds(sport string, f *Feed) time.Time {
	if (sport != "nfl" && sport != "cfb") || espn.Str(espn.Map(espn.Status(f.Data)["type"])["name"]) != "STATUS_HALFTIME" {
		return time.Time{}
	}
	for i := len(f.Plays) - 1; i >= 0; i-- {
		p := f.Plays[i]
		typ := playType(p)
		var length time.Duration
		switch {
		case sport == "nfl" && strings.EqualFold(typ, "End of Half"):
			length = nflHalftime
		case sport == "cfb" && strings.EqualFold(typ, "End Period") && espn.Str(espn.Map(p["period"])["number"]) == "2":
			length = cfbHalftime
		default:
			continue
		}
		t, err := time.Parse(time.RFC3339, espn.Str(p["wallclock"]))
		if err != nil {
			return time.Time{}
		}
		return t.Add(length)
	}
	return time.Time{}
}

// footballPeriodBreak is true between quarters in football. ESPN does not use
// its end-of-period status there: it resets the clock to 15:00 while the
// period is still the old one, so the header would say "15:00 3rd". The "End
// Period" play for the current period is the sign of the break. It ends with
// the next snap, or when ESPN moves on to the next period.
func footballPeriodBreak(sport string, f *Feed) bool {
	if (sport != "nfl" && sport != "cfb") || len(f.Plays) == 0 || f.Finished {
		return false
	}
	last := f.Plays[len(f.Plays)-1]
	if !strings.EqualFold(playType(last), "End Period") {
		return false
	}
	period, ok := espn.Int64(espn.Status(f.Data)["period"])
	playPeriod, okPlay := espn.Int64(espn.Map(last["period"])["number"])
	return ok && okPlay && period == playPeriod
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
	}
	return ""
}

// inPlay is true while the game is being played: not before the start, at
// halftime or after the end.
func inPlay(f *Feed) bool {
	typ := espn.Map(espn.Status(f.Data)["type"])
	return !f.Finished && espn.Str(typ["state"]) == "in" && espn.Str(typ["name"]) != "STATUS_HALFTIME"
}

// situation is the football down and distance after the last play, or the
// baseball count, outs and runners ("1-2, 1 out, 2 on").
func situation(sport string, f *Feed) string {
	if sport == "mlb" {
		return mlbSituation(f).text
	}
	if (sport != "nfl" && sport != "cfb") || !inPlay(f) || len(f.Plays) == 0 {
		return ""
	}
	last := f.Plays[len(f.Plays)-1]
	if isTimeout(last) || isPeriodEnd(last) {
		// A timeout changes nothing on the field and has no "after" of its
		// own, and its "start" can be stale (after a punt it still shows the
		// fourth down). So it is the situation after the last real play. If
		// that play has none (a score, a kickoff), there is none to show. The
		// End Period row is the same: its own "end" can be the down and
		// distance from before the play ("2nd & 10" after a gain that left
		// 2nd & 7).
		for i := len(f.Plays) - 2; i >= 0; i-- {
			if !isTimeout(f.Plays[i]) && !isPeriodEnd(f.Plays[i]) {
				return downDistanceFull(playEnd(f.Plays[i]))
			}
		}
		if isTimeout(last) {
			return downDistanceFull(espn.Map(last["start"]))
		}
		return ""
	}
	return downDistanceFull(playEnd(last))
}

// isPeriodEnd is a football play that marks the end of a quarter, a half or
// the game.
func isPeriodEnd(p espn.Data) bool {
	switch playType(p) {
	case "End Period", "End of Half", "End of Regulation", "End of Game":
		return true
	}
	return false
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

// mlbNow is the at-bat the newest play describes: its status text without the
// half-inning, and the numbers behind it.
type mlbNow struct {
	text, bases          string
	balls, strikes, outs int
}

// mlbSituation is what the newest play that has one says about the at-bat, the
// way a play's title gives it, without the half-inning.
func mlbSituation(f *Feed) mlbNow {
	if !inPlay(f) || len(f.Plays) == 0 {
		return mlbNow{}
	}
	ctxs := mlbContexts(f.Plays)
	best, now := int64(-1), mlbNow{}
	for _, p := range f.Plays {
		id, ok := espn.PlayID(p)
		ctx := ctxs[espn.PlayKey(p)]
		_, status, found := strings.Cut(ctx.status, " · ")
		if ok && found && id > best {
			best = id
			now = mlbNow{status, ctx.bases, ctx.balls, ctx.strikes, ctx.outs}
		}
	}
	return now
}

// mlbMatchup is the pitcher, the batter and the batting side ("away" in the top
// of an inning) from the newest Start Batter/Pitcher play ("Sean Burke pitches
// to Travis Bazzana"). Once that at-bat has a result the batter is blank until
// the next one is announced (the pitcher and side stay), and once an inning has
// ended all three are.
func mlbMatchup(sport string, f *Feed) (pitcher, batter, side string) {
	if sport != "mlb" || !inPlay(f) {
		return "", "", ""
	}
	var start espn.Data
	startID, endID, resultID := int64(-1), int64(-1), int64(-1)
	for _, p := range f.Plays {
		id, ok := espn.PlayID(p)
		if !ok {
			continue
		}
		switch kind := espn.Str(espn.Map(p["type"])["type"]); {
		case kind == "start-batterpitcher" && id > startID:
			startID, start = id, p
		case kind == "end-inning" && id > endID:
			endID = id
		case kind == "play-result" && lastChars(espn.PlayKey(p), 6) >= "990000" && id > resultID: // an at-bat's result
			resultID = id
		}
	}
	if start == nil || endID > startID {
		return "", "", ""
	}
	pitcher, batter, found := strings.Cut(espn.PlayText(start), " pitches to ")
	if !found {
		return "", "", ""
	}
	side = "home"
	if espn.Str(espn.Map(start["period"])["type"]) == "Top" {
		side = "away"
	}
	if resultID > startID {
		batter = ""
	}
	return strings.TrimSpace(pitcher), strings.TrimSpace(batter), side
}
