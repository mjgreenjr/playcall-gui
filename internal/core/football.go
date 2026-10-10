package core

import (
	"regexp"
	"strconv"
	"strings"

	"playcall/internal/espn"
)

// footballRules are the NFL and college football rules: ids compared as
// integers, catch-up limit 5, title from the game-level score and clock, body
// framed by the down and distance.
type footballRules struct{}

func (footballRules) CatchupLimit() int { return 5 }
func (footballRules) TrackSent() bool   { return false }

func (footballRules) Prepare(f *Feed) PlayRules { return footballPlays{f} }

type footballPlays struct{ f *Feed }

func (footballPlays) Announced(espn.Data) bool { return true }
func (footballPlays) IDs(p espn.Data) []string { return []string{espn.PlayKey(p)} }

func (r footballPlays) Message(p espn.Data) Message {
	text := espn.PlayText(p)
	f := r.f
	clock, period, body := f.Clock, f.Period, footballBody(p, dropPlaceholderClock(text, f.Clock))
	if isTimeout(p) {
		body = footballTimeoutBody(f.Plays, p, text)
	}
	if footballHalftime(p) {
		// The end-of-half play stands in for the break: "Halftime" replaces
		// the clock, and its down and distance (the next possession) is dropped.
		clock, period, body = "Halftime", "", text
	}
	return Message{
		Title:  FormatTitle(f.Away.Abbr, f.Away.Score, clock, period, f.Home.Abbr, f.Home.Score),
		Body:   body,
		Urgent: footballUrgent(p, text),
		Home:   f.Home,
		Away:   f.Away,
		Clock:  clock,
		Period: period,
	}
}

// footballHalftime reports whether p ends the first half. NFL has a dedicated
// "End of Half" type; college football uses the plain "End Period" type, so
// the second period is the half there.
func footballHalftime(p espn.Data) bool {
	switch playType(p) {
	case "End of Half":
		return true
	case "End Period":
		n, _ := espn.Int64(espn.Map(p["period"])["number"])
		return n == 2
	}
	return false
}

// footballTimeoutBody is a timeout's text framed by the situation after the
// play before it. ESPN's own "start" for a timeout can be stale (after a punt
// it still has the fourth down); with no earlier play to go by it is used as is.
func footballTimeoutBody(plays []espn.Data, p espn.Data, text string) string {
	id := espn.PlayKey(p)
	for i, q := range plays {
		if espn.PlayKey(q) != id {
			continue
		}
		if (i > 0 && (changesPossession(plays[i-1]) || espn.Truthy(plays[i-1]["scoringPlay"]))) || twoMinuteWarning(plays, i) {
			text = officialTimeout(text)
		}
		for j := i - 1; j >= 0; j-- {
			if !isTimeout(plays[j]) {
				return joinParts(downDistance(espn.Map(plays[j]["end"])), endZone(text))
			}
		}
	}
	return footballBody(p, text)
}

// changesPossession is a kickoff or punt, a turnover, or any play that ends
// with the other team holding the ball (an interception, a fumble lost, a
// turnover on downs).
func changesPossession(p espn.Data) bool {
	kind := strings.ToLower(playType(p))
	if strings.Contains(kind, "kickoff") || strings.Contains(kind, "punt") || espn.Truthy(p["isTurnover"]) {
		return true
	}
	start := espn.Str(espn.Map(espn.Map(p["start"])["team"])["id"])
	end := espn.Str(espn.Map(espn.Map(p["end"])["team"])["id"])
	return start != "" && end != "" && start != end
}

// twoMinuteWarning is the timeout that stops the clock at 2:00 of the 2nd or
// 4th quarter; ESPN files it under a team, like the others. It is logged at
// 2:00, or lower when the play before it ran through 2:00 (a snap at 2:03 that
// ends at 1:55). A team timeout late in the half is not mistaken for it: the
// play before must have started above 2:00.
func twoMinuteWarning(plays []espn.Data, i int) bool {
	p := plays[i]
	n, _ := espn.Int64(espn.Map(p["period"])["number"])
	if n != 2 && n != 4 {
		return false
	}
	at, ok := timeoutClock(espn.PlayText(p))
	if !ok || at > 120 {
		return false
	}
	if at == 120 {
		return true
	}
	if i == 0 || isTimeout(plays[i-1]) {
		return false
	}
	snap, ok := snapClock(espn.PlayText(plays[i-1]))
	return ok && snap > 120
}

var (
	timeoutClockRE = regexp.MustCompile(`, clock (\d+):(\d+)$`)
	snapClockRE    = regexp.MustCompile(`^\((\d+):(\d+)\)`)
)

func clockSeconds(m []string) (int, bool) {
	if m == nil {
		return 0, false
	}
	min, err1 := strconv.Atoi(m[1])
	sec, err2 := strconv.Atoi(m[2])
	return min*60 + sec, err1 == nil && err2 == nil
}

// timeoutClock is the seconds left in "Timeout BYU, clock 01:55".
func timeoutClock(text string) (int, bool) {
	return clockSeconds(timeoutClockRE.FindStringSubmatch(text))
}

// snapClock is the seconds left at the snap, from a play's "(02:03) ..." prefix.
func snapClock(text string) (int, bool) { return clockSeconds(snapClockRE.FindStringSubmatch(text)) }

var teamTimeoutText = regexp.MustCompile(`^Timeout [^,]+, clock `)

// officialTimeout rewrites "Timeout BYU, clock 08:54" as "Official timeout,
// clock 08:54". The two-minute warning, a change of possession (every kickoff
// and punt unless the return scores, and every turnover) and a score (the TV
// timeout before the kickoff) are each followed by an official's timeout, and
// ESPN files it under a team.
func officialTimeout(text string) string {
	return teamTimeoutText.ReplaceAllString(text, "Official timeout, clock ")
}

func joinParts(parts ...string) string {
	var kept []string
	for _, s := range parts {
		if s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, " · ")
}

var endZoneSpot = regexp.MustCompile(`\b([A-Z][A-Z&]*)00\b`)

// endZone writes ESPN's goal-line spot ("to the BYU00 TOUCHDOWN") as
// "to the BYU end zone TOUCHDOWN". Only what is shown changes; the play's text
// is kept as ESPN sent it.
func endZone(text string) string {
	return endZoneSpot.ReplaceAllString(text, "$1 end zone")
}

// footballBody frames the play text with the down and distance before and
// after it ("1st & 10 · text · 2nd & 5"); a part ESPN has no value for is
// left out (kickoffs have no start).
func footballBody(p espn.Data, text string) string {
	var parts []string
	for _, s := range []string{
		downDistance(espn.Map(p["start"])),
		endZone(text),
		downDistance(espn.Map(p["end"])),
	} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// downDistance is the "2nd & 5" of a play's start or end. Timeouts and some
// penalties come with no down, so ESPN's text is just " & Goal" (down 0); a
// fragment that starts with "&" says nothing alone and is left out.
func downDistance(side espn.Data) string {
	text := espn.Str(side["shortDownDistanceText"])
	if strings.HasPrefix(strings.TrimSpace(text), "&") {
		return ""
	}
	return text
}

var footballKeywords = []string{"touchdown", "field goal", "safety", "fumble", "interception"}

// footballUrgent: normal for scoring plays and turnovers; kickoffs and punts
// are always low; otherwise normal if the text has a keyword, which covers
// plain-text fixtures.
func footballUrgent(p espn.Data, text string) bool {
	if espn.Truthy(p["scoringPlay"]) || espn.Truthy(p["isTurnover"]) {
		return true
	}
	kind := strings.ToLower(espn.Str(espn.Map(p["type"])["text"]))
	if strings.Contains(kind, "kickoff") || strings.Contains(kind, "punt") {
		return false
	}
	lower := strings.ToLower(text)
	for _, k := range footballKeywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// placeholderClock is the ", clock 00:00" ESPN writes into a score's text
// before it knows the time (it fills in the real one a poll or two later).
var placeholderClock = regexp.MustCompile(`, clock 00:00`)

// dropPlaceholderClock takes the ", clock 00:00" out of what is shown unless
// the game clock really is at zero (the end of a quarter). The text ESPN sent
// is not changed, so the update that carries the real time is still detected.
func dropPlaceholderClock(text, gameClock string) string {
	if gameClock == "0:00" || gameClock == "00:00" {
		return text
	}
	return placeholderClock.ReplaceAllString(text, "")
}

// staleClockSeconds is how far ESPN's game clock may trail the newest play
// before it counts as stuck.
const staleClockSeconds = 45

var clockRE = regexp.MustCompile(`^(\d+):(\d{2})$`)

func parseClock(s string) (int, bool) {
	m := clockRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	return clockSeconds(m)
}

// footballClock is the game clock to show. ESPN's own can stay at the start of
// a quarter ("15:00 2nd") for minutes while the plays go on; it counts down,
// so when it is more than staleClockSeconds above the newest play's time in
// the same period, that play's time is the better answer. Otherwise ESPN's is
// kept, as it is outside live football play and at a period break.
func footballClock(sport, clock string, data espn.Data, plays []espn.Data, finished bool) string {
	if (sport != "nfl" && sport != "cfb") || finished || len(plays) == 0 {
		return clock
	}
	status := espn.Status(data)
	if espn.Str(espn.Map(status["type"])["state"]) != "in" {
		return clock
	}
	last := plays[len(plays)-1]
	switch strings.ToLower(playType(last)) {
	case "end period", "end of half", "end of game":
		return clock
	}
	period, ok := espn.Int64(status["period"])
	playPeriod, okPlay := espn.Int64(espn.Map(last["period"])["number"])
	if !ok || !okPlay || period != playPeriod {
		return clock
	}
	playClock := espn.Str(espn.Map(last["clock"])["displayValue"])
	shown, ok1 := parseClock(clock)
	played, ok2 := parseClock(playClock)
	if ok1 && ok2 && shown-played > staleClockSeconds {
		return playClock
	}
	return clock
}
