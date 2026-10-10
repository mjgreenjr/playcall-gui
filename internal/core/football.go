package core

import (
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
	clock, period, body := f.Clock, f.Period, footballBody(p, text)
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
		for j := i - 1; j >= 0; j-- {
			if !isTimeout(plays[j]) {
				return joinParts(downDistance(espn.Map(plays[j]["end"])), text)
			}
		}
	}
	return footballBody(p, text)
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

// footballBody frames the play text with the down and distance before and
// after it ("1st & 10 · text · 2nd & 5"); a part ESPN has no value for is
// left out (kickoffs have no start).
func footballBody(p espn.Data, text string) string {
	var parts []string
	for _, s := range []string{
		downDistance(espn.Map(p["start"])),
		text,
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
