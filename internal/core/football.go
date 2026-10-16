package core

import (
	"encoding/json"
	"log/slog"
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
		body = footballTimeoutBody(f.Plays, p, text, f.Charged)
	}
	body = completeSpots(body, spotNames(f.Data))
	if isPeriodEnd(p) {
		body = text // its own down and distance is not reliable (see situation)
	}
	if footballHalftime(p) {
		// The end-of-half play stands in for the break: "Halftime" replaces
		// the clock, and its down and distance (the next possession) is dropped.
		clock, period, body = "Halftime", "", text
	}
	display := ""
	if espn.Truthy(p["scoringPlay"]) {
		if shown := touchdownFirstDown.ReplaceAllString(body, "$1"); shown != body {
			display = shown
		}
	}
	return Message{
		Title:   FormatTitle(f.Away.Abbr, f.Away.Score, clock, period, f.Home.Abbr, f.Home.Score),
		Display: display,
		Body:    body,
		Urgent:  footballUrgent(p, text),
		Home:    f.Home,
		Away:    f.Away,
		Clock:   clock,
		Period:  period,
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
func footballTimeoutBody(plays []espn.Data, p espn.Data, text string, charged map[string]bool) string {
	id := espn.PlayKey(p)
	for i, q := range plays {
		if espn.PlayKey(q) != id {
			continue
		}
		official := (i > 0 && (changesPossession(plays[i-1]) || espn.Truthy(plays[i-1]["scoringPlay"]))) || twoMinuteWarning(plays, i)
		if c, ok := charged[id]; ok {
			official = !c // ESPN's counter knows
		}
		if official {
			text = officialTimeout(text)
		}
		for j := i - 1; j >= 0; j-- {
			if !isTimeout(plays[j]) {
				return joinParts(downDistance(playEnd(plays[j])), endZone(text))
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

var endZoneSpot = regexp.MustCompile(`\b([A-Z][A-Za-z&]*)00\b`)

// endZone writes ESPN's goal-line spot ("to the BYU00 TOUCHDOWN") as
// "to the BYU end zone TOUCHDOWN". Only what is shown changes; the play's text
// is kept as ESPN sent it.
func endZone(text string) string {
	text = endZoneSpot.ReplaceAllString(text, "$1 end zone")
	return yardLine.ReplaceAllStringFunc(text, func(m string) string {
		sub := yardLine.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sub[2])
		return sub[1] + " " + strconv.Itoa(n)
	})
}

// yardLine matches ESPN's spot with no space ("ILL11", "MSU9").
var yardLine = regexp.MustCompile(`\b([A-Z][A-Za-z&]+)(\d{1,2})\b`)

// footballBody frames the play text with the down and distance before and
// after it ("1st & 10 · text · 2nd & 5"); a part ESPN has no value for is
// left out (kickoffs have no start).
func footballBody(p espn.Data, text string) string {
	var parts []string
	for _, s := range []string{
		downDistance(espn.Map(p["start"])),
		endZone(text),
		downDistance(playEnd(p)),
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

// footballOvertime relabels ESPN's "5th" as "OT" ("2OT", ...). College
// overtime has no clock (ESPN leaves 0:00 and every play says "clock 00:00"),
// so the clock is dropped there; the NFL's overtime clock is kept.
func footballOvertime(sport, clock, period string, data espn.Data) (string, string) {
	if sport != "nfl" && sport != "cfb" {
		return clock, period
	}
	n, ok := espn.Int64(espn.Status(data)["period"])
	if !ok || n < 5 {
		return clock, period
	}
	if n == 5 {
		period = "OT"
	} else {
		period = strconv.FormatInt(n-4, 10) + "OT"
	}
	if sport == "cfb" {
		clock = ""
	}
	return clock, period
}

// spotNames maps ESPN's six-character spot names to the full ones: it writes
// "Maryla22" for Maryland's 22 and "OhioSt19" for Ohio State's 19 (the name
// without spaces, cut at six characters).
func spotNames(data espn.Data) map[string]string {
	names := map[string]string{}
	home, away := espn.Teams(espn.Competition(data))
	for _, c := range []espn.Data{home, away} {
		name := espn.Str(espn.Map(c["team"])["location"])
		if compact := strings.ReplaceAll(name, " ", ""); len(compact) > 6 {
			names[compact[:6]] = name
		}
	}
	return names
}

var spotName = regexp.MustCompile(`\b([A-Z][A-Za-z&]{5}) (\d|end zone)`)

// completeSpots writes the full team name in a spot already split by endZone
// ("Maryla 22" as "Maryland 22"). Only names of the game's two teams are known.
func completeSpots(text string, names map[string]string) string {
	if len(names) == 0 {
		return text
	}
	return spotName.ReplaceAllStringFunc(text, func(m string) string {
		sub := spotName.FindStringSubmatch(m)
		if full, ok := names[sub[1]]; ok {
			return full + " " + sub[2]
		}
		return m
	})
}

// startedBeforeStatus puts a football game's status where its plays already
// are. ESPN posts a period's first play (the kickoff) 10 to 20 seconds, and
// at the half minutes, before its status block moves on, so the header sat on
// the start time or on "Halftime" meanwhile (KENN @ MOST on 2026-10-14: the
// 1st quarter's kickoff at 19:33:11 with the status "pre" until 19:33:32, and
// the 3rd quarter's kickoff row with the header still on Halftime).
//   - Status "pre" with plays in the 1st quarter: live, period 1, no timeouts
//     used and 0-0 unless the play has a score (a kickoff returned for a
//     touchdown).
//   - Status at a period break or halftime with a play from a later period
//     (not an End Period row): in progress in that period, the clock from the
//     play.
//
// The data is changed in place, once per poll, before anything reads it; the
// next poll's real status takes over.
func startedBeforeStatus(sport string, data espn.Data, plays []espn.Data) {
	if (sport != "nfl" && sport != "cfb") || len(plays) == 0 {
		return
	}
	status := espn.Status(data)
	typ := espn.Map(status["type"])
	if typ == nil {
		return
	}
	last := plays[len(plays)-1]
	period := espn.Map(last["period"])
	playPeriod, ok := espn.Int64(period["number"])
	if !ok {
		return
	}
	name := espn.Str(typ["name"])
	if halfOver(sport, last) && espn.Str(typ["state"]) == "in" && name != "STATUS_HALFTIME" {
		// The half's last play is in and the status is still in the 2nd
		// quarter (GASO @ ODU and ECU @ UAB on 2026-10-15: the End Period row
		// first, "Halftime" about 40 seconds later; DEL @ MTSU: 80).
		if shown, ok := espn.Int64(status["period"]); ok && shown > 2 {
			return
		}
		slog.Debug("Status behind the plays; treating the game as at halftime", "play", espn.PlayKey(last), "status", name)
		typ["name"] = "STATUS_HALFTIME"
		return
	}
	switch {
	case espn.Str(typ["state"]) == "pre" && playPeriod == 1:
	case (name == "STATUS_HALFTIME" || name == "STATUS_END_PERIOD") && !isPeriodEnd(last):
		if shown, ok := espn.Int64(status["period"]); !ok || playPeriod <= shown {
			return
		}
	default:
		return
	}
	slog.Debug("Status behind the plays; treating the game as live", "play", espn.PlayKey(last), "status", name, "period", playPeriod)
	typ["state"] = "in"
	typ["name"] = "STATUS_IN_PROGRESS"
	status["period"] = json.Number(strconv.FormatInt(playPeriod, 10))
	status["displayPeriod"] = espn.Str(period["displayValue"])
	status["displayClock"] = espn.Str(espn.Map(last["clock"])["displayValue"])
	// The play has the score (a kickoff returned for a touchdown is already
	// 6-0); without it the game is 0-0.
	home, away := espn.Teams(espn.Competition(data))
	for _, c := range []struct {
		team  espn.Data
		score any
	}{{away, last["awayScore"]}, {home, last["homeScore"]}} {
		if c.team == nil {
			continue
		}
		if _, ok := espn.Int64(c.team["timeoutsUsed"]); !ok {
			c.team["timeoutsUsed"] = json.Number("0")
		}
		if c.score != nil {
			c.team["score"] = c.score
		}
	}
}

// halfOver is true for the play that ends the first half: the NFL's End of
// Half, college's End Period of the 2nd quarter.
func halfOver(sport string, p espn.Data) bool {
	switch {
	case sport == "nfl":
		return strings.EqualFold(playType(p), "End of Half")
	case sport == "cfb":
		return strings.EqualFold(playType(p), "End Period") && espn.Str(espn.Map(p["period"])["number"]) == "2"
	}
	return false
}

// touchdownFirstDown matches the ", 1ST DOWN" ESPN adds to a touchdown pass
// that also gained a first down ("... KSU00 TOUCHDOWN, clock 09:20, 1ST DOWN
// #36 Y.Obeid kick attempt good"). Meaningless on a score, so the window and
// toasts leave it out; Body keeps ESPN's text.
var touchdownFirstDown = regexp.MustCompile(`(TOUCHDOWN(?:, clock \d+:\d+)?), 1ST DOWN`)

// playEnd is a play's `end` (down, distance, spot), or nil when its own text
// contradicts it: a play whose text says "1ST DOWN" but whose end is still
// "4th & 1" (ESPN does not update the end for a first down, and not after a
// penalty that gives one: 6 of the 71 non-scoring plays with it in two games on
// 2026-10-14 had the next snap start at 1st, e.g. a 2-yard rush on 3rd & 1 with
// "4th & 1" after it). There is
// no down and distance to show then, and the next snap's start fills it in.
func playEnd(p espn.Data) espn.Data {
	end := espn.Map(p["end"])
	text := espn.PlayText(p)
	// A reviewed play carries the first down of the play it replaced ("(Original
	// Play: ... 1ST DOWN)"): its end is the right one (2 of 2).
	if end != nil && strings.Contains(text, "1ST DOWN") && !strings.Contains(text, "Original Play") &&
		!strings.HasPrefix(espn.Str(end["shortDownDistanceText"]), "1st") {
		return nil
	}
	return end
}
