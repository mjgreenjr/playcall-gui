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
	return Message{
		Title:  FormatTitle(f.Away.Abbr, f.Away.Score, f.Clock, f.Period, f.Home.Abbr, f.Home.Score),
		Body:   footballBody(p, text),
		Urgent: footballUrgent(p, text),
		Home:   f.Home,
		Away:   f.Away,
		Clock:  f.Clock,
		Period: f.Period,
	}
}

// footballBody frames the play text with the down and distance before and
// after it ("1st & 10 · text · 2nd & 5"); a part ESPN has no value for is
// left out (kickoffs have no start).
func footballBody(p espn.Data, text string) string {
	var parts []string
	for _, s := range []string{
		espn.Str(espn.Map(p["start"])["shortDownDistanceText"]),
		text,
		espn.Str(espn.Map(p["end"])["shortDownDistanceText"]),
	} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
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
