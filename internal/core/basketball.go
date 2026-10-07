package core

import (
	"fmt"
	"strings"

	"playcall/internal/espn"
)

// basketballRules are the NBA, WNBA and college basketball rules: play ids are
// not in game order, so list order and sent ids decide what is new; a missed
// shot goes out together with the rebound after it; catch-up limit 10.
//
// cbb is assumed to match the NBA (two halves instead of four periods) and has
// not been verified against a live feed.
type basketballRules struct {
	regulation int // regulation periods: 4, or 2 for cbb
}

func (basketballRules) CatchupLimit() int { return 10 }
func (basketballRules) TrackSent() bool   { return true }

func (r basketballRules) Prepare(f *Feed) PlayRules {
	bundles, held := nbaBundles(f.Plays, f.SentIDs, f.Finished)
	absorbed := map[string]bool{}
	for _, miss := range bundles {
		absorbed[espn.PlayKey(miss)] = true
	}
	return basketballPlays{f: f, regulation: r.regulation, bundles: bundles, absorbed: absorbed, held: held}
}

type basketballPlays struct {
	f          *Feed
	regulation int
	bundles    map[string]espn.Data // rebound id -> the missed shot sent with it
	absorbed   map[string]bool      // ids of missed shots bundled into a rebound
	held       map[string]bool      // misses held back until the next poll
}

// nbaSilentTypes are never announced: the turnover only repeats the offensive
// foul logged at the same second.
var nbaSilentTypes = map[string]bool{"Offensive Foul Turnover": true}

func (r basketballPlays) Announced(p espn.Data) bool {
	k := espn.PlayKey(p)
	return !nbaSilentTypes[playType(p)] && !r.absorbed[k] && !r.held[k]
}

func (r basketballPlays) IDs(p espn.Data) []string {
	ids := []string{espn.PlayKey(p)}
	if miss, ok := r.bundles[espn.PlayKey(p)]; ok {
		ids = append(ids, espn.PlayKey(miss))
	}
	return ids
}

func (r basketballPlays) Message(p espn.Data) Message {
	f := r.f
	// The play's own score and clock (counts down, e.g. "31.4").
	away, home := f.Away, f.Home
	if v := p["awayScore"]; v != nil {
		away.Score = espn.ScoreText(v)
	}
	if v := p["homeScore"]; v != nil {
		home.Score = espn.ScoreText(v)
	}
	clock := espn.Str(espn.Map(p["clock"])["displayValue"])
	period := nbaPeriod(p, r.regulation)
	miss, bundled := r.bundles[espn.PlayKey(p)]
	urgent := nbaUrgent(p)
	if bundled {
		urgent = false
	}
	return Message{
		Title:  FormatTitle(away.Abbr, away.Score, clock, period, home.Abbr, home.Score),
		Body:   nbaBody(p, miss, bundled),
		Urgent: urgent,
		Home:   home,
		Away:   away,
		Clock:  clock,
		Period: period,
	}
}

func playType(p espn.Data) string { return espn.Str(espn.Map(p["type"])["text"]) }

// nbaIsMiss: a shot (or free throw) that did not go in.
func nbaIsMiss(p espn.Data) bool {
	return espn.Truthy(p["shootingPlay"]) && !espn.Truthy(p["scoringPlay"])
}

func nbaIsRebound(p espn.Data) bool { return strings.Contains(playType(p), "Rebound") }

// nbaBundles pairs each unsent missed shot with the rebound right after it, so
// both go out as one toast. It returns {rebound id: miss play} and the ids of
// plays to hold back this poll. A miss that is still the newest play is held
// back, because the rebound almost always follows in the next update; once the
// game is over nothing is held back.
func nbaBundles(plays []espn.Data, sent map[string]bool, finished bool) (map[string]espn.Data, map[string]bool) {
	bundles, held := map[string]espn.Data{}, map[string]bool{}
	for i, p := range plays {
		if !nbaIsMiss(p) || sent[espn.PlayKey(p)] {
			continue
		}
		if i+1 >= len(plays) {
			if !finished {
				held[espn.PlayKey(p)] = true
			}
			continue
		}
		next := plays[i+1]
		if nbaIsRebound(next) && !sent[espn.PlayKey(next)] {
			bundles[espn.PlayKey(next)] = p
		}
	}
	return bundles, held
}

var nbaOrdinals = []string{"", "1st", "2nd", "3rd", "4th"}

// nbaPeriod is "1st" ... "4th" ("1st", "2nd" for cbb), then "OT", "2OT", ...
// A play with no period number falls back to ESPN's period text.
func nbaPeriod(p espn.Data, regulation int) string {
	period := espn.Map(p["period"])
	if !espn.Truthy(period["number"]) {
		return espn.Str(period["displayValue"])
	}
	n, ok := espn.Int64(period["number"])
	if !ok || n < 1 {
		return espn.Str(period["displayValue"])
	}
	reg := int64(regulation)
	switch {
	case n <= reg:
		return nbaOrdinals[n]
	case n == reg+1:
		return "OT"
	}
	return fmt.Sprintf("%dOT", n-reg)
}

// nbaBody is ESPN's text on one line (some contain a newline); a bundled miss
// comes first.
func nbaBody(p, miss espn.Data, bundled bool) string {
	text := strings.Join(strings.Fields(espn.PlayText(p)), " ")
	if bundled {
		return strings.Join(strings.Fields(espn.PlayText(miss)), " ") + " · " + text
	}
	return text
}

// nbaUrgent: normal for made threes and fouls, low for everything else.
func nbaUrgent(p espn.Data) bool {
	kind := strings.ToLower(playType(p))
	if espn.Truthy(p["scoringPlay"]) && espn.Str(p["scoreValue"]) == "3" {
		return true
	}
	return strings.Contains(kind, "foul") && !strings.Contains(kind, "turnover")
}
