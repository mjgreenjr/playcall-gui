package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"playcall/internal/espn"
)

// mlbRules are the MLB rules: ids compared as integers, catch-up limit 5.
// ESPN sends every pitch plus bookkeeping plays, so Prepare walks the whole
// feed once (mlbContexts) and describes each play; plays marked not announced
// stay unseen and silent.
type mlbRules struct{}

func (mlbRules) CatchupLimit() int { return 5 }
func (mlbRules) TrackSent() bool   { return false }

func (mlbRules) Prepare(f *Feed) PlayRules {
	return mlbPlays{f: f, ctxs: mlbContexts(f.Plays)}
}

type mlbPlays struct {
	f    *Feed
	ctxs map[string]mlbContext
}

func (r mlbPlays) Announced(p espn.Data) bool { return r.ctxs[espn.PlayKey(p)].announce }
func (mlbPlays) IDs(p espn.Data) []string     { return []string{espn.PlayKey(p)} }

func (r mlbPlays) Message(p espn.Data) Message {
	ctx := r.ctxs[espn.PlayKey(p)]
	away, home := r.f.Away, r.f.Home
	if ctx.awayScore != nil {
		away.Score = espn.ScoreText(ctx.awayScore)
	}
	if ctx.homeScore != nil {
		home.Score = espn.ScoreText(ctx.homeScore)
	}
	return Message{
		Title:  FormatTitle(away.Abbr, away.Score, ctx.status, "", home.Abbr, home.Score),
		Body:   ctx.body,
		Urgent: ctx.urgent,
		Home:   home,
		Away:   away,
		Clock:  ctx.status,
		Batter: ctx.batter,
	}
}

// mlbContext describes one play. The scores are the play's own (nil when ESPN
// sent none).
type mlbContext struct {
	announce             bool
	status, body, batter string
	urgent               bool
	awayScore, homeScore any
	// bases is the occupied bases the status's runner count comes from, as
	// "1", "13", "123" ("" when empty).
	bases string
}

// \w and \d are Unicode in the Python, so accented names must match.
var (
	mlbPitchPrefix = regexp.MustCompile(`^Pitch \p{Nd}+[\s\p{Z}]*:[\s\p{Z}]*`)
	mlbFielderSub  = regexp.MustCompile(`^[\p{L}\p{N}_.' -]+ (at|in) (pitcher|catcher|first base|second base|third base|shortstop|left field|center field|right field|designated hitter)\.?$`)
	mlbNormalEvent = regexp.MustCompile(`(?i)ran for|relieved|caught stealing|picked off|thrown out`)
	mlbOutEvent    = regexp.MustCompile(`(?i)caught stealing|picked off|thrown out`)
	mlbPitchEnd    = regexp.MustCompile(`Ball In Play|Strike 3|Ball 4`)
)

// mlbRunners counts the runners on base on this play.
func mlbRunners(p espn.Data) int {
	n := 0
	for _, k := range []string{"onFirst", "onSecond", "onThird"} {
		if espn.Truthy(p[k]) {
			n++
		}
	}
	return n
}

// mlbBases names the occupied bases on this play: "1", "13", "123", or "".
func mlbBases(p espn.Data) string {
	out := ""
	for i, k := range []string{"onFirst", "onSecond", "onThird"} {
		if espn.Truthy(p[k]) {
			out += string(rune('1' + i))
		}
	}
	return out
}

// mlbStatus is e.g. "Top 6 · 1-2, 1 out, 2 on". The count and runners are
// left out when nil; a 0-0 count is still shown.
func mlbStatus(label string, outs int, on *int, count *[2]int) string {
	var parts []string
	if count != nil {
		parts = append(parts, fmt.Sprintf("%d-%d", count[0], count[1]))
	}
	if outs == 1 {
		parts = append(parts, "1 out")
	} else {
		parts = append(parts, fmt.Sprintf("%d outs", outs))
	}
	if on != nil {
		parts = append(parts, fmt.Sprintf("%d on", *on))
	}
	return label + " · " + strings.Join(parts, ", ")
}

// lastChars is the last n characters of s (all of it when shorter).
func lastChars(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[len(r)-n:]
	}
	return string(r)
}

// mlbContexts walks the plays in id order and describes each one, keyed by
// play id. ESPN's `outs` is the count AFTER the at-bat (even on its first
// pitch), so outs are tracked here: pitches use the outs from the previous
// at-bat and the at-bat's result sets them. Runners on pitches are the
// runners before the pitch; on a result they are the runners after it.
func mlbContexts(plays []espn.Data) map[string]mlbContext {
	sorted := append([]espn.Data(nil), plays...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, _ := espn.PlayID(sorted[i])
		b, _ := espn.PlayID(sorted[j])
		return a < b
	})
	ctxs := map[string]mlbContext{}
	outs, on := 0, 0
	bases := ""
	batter := "" // the one named by the latest Start Batter/Pitcher
	for _, p := range sorted {
		typ := espn.Map(p["type"])
		kind := espn.Str(typ["type"])
		text := espn.PlayText(p)
		period := espn.Map(p["period"])
		number := "None"
		if v := period["number"]; v != nil {
			number = espn.ScoreText(v)
		}
		half := "Bot"
		if espn.Str(period["type"]) == "Top" {
			half = "Top"
		}
		label := half + " " + number
		ctx := mlbContext{
			announce:  true,
			body:      text,
			awayScore: p["awayScore"],
			homeScore: p["homeScore"],
		}
		switch {
		case kind == "start-inning" || kind == "end-batterpitcher":
			ctx.announce = false
			if kind == "start-inning" {
				outs, on, bases = 0, 0, ""
			}
		case kind == "end-inning":
			outs, on, bases = 0, 0, ""
			// ESPN's "Mid"/"End" is a half-inning break: show the half that comes next.
			isMid := espn.Str(period["type"]) == "Mid"
			n := intOr(period["number"], 0)
			next := fmt.Sprintf("Top %d", n+1)
			if isMid {
				next = fmt.Sprintf("Bot %d", n)
			}
			lead := intOr(p["homeScore"], 0) - intOr(p["awayScore"], 0)
			gameOver := n >= 9 && ((isMid && lead > 0) || (!isMid && lead != 0))
			ctx.announce = !gameOver
			ctx.status = mlbStatus(next, 0, nil, nil)
			ctx.body = "Side retired"
		case kind == "start-batterpitcher":
			if _, name, found := strings.Cut(text, " pitches to "); found {
				batter = strings.TrimSpace(name)
			}
			on, bases = mlbRunners(p), mlbBases(p)
			ctx.status = mlbStatus(label, outs, &on, &[2]int{0, 0})
		case strings.HasPrefix(text, "Pitch "):
			on, bases = mlbRunners(p), mlbBases(p)
			ctx.body = mlbPitchPrefix.ReplaceAllString(text, "")
			result := espn.Map(p["resultCount"])
			count := [2]int{intOr(result["balls"], 0), intOr(result["strikes"], 0)}
			// The at-bat ends on this pitch; its Play Result carries the outcome.
			if mlbPitchEnd.MatchString(text) {
				ctx.announce = false
			}
			ctx.status = mlbStatus(label, outs, &on, &count)
			ctx.batter = batter
		case espn.Str(typ["text"]) == "Stolen Base":
			// Duplicated by the Play Result with the same text.
			ctx.announce = false
		case kind == "play-result":
			if mlbFielderSub.MatchString(text) {
				ctx.announce = false
			} else if lastChars(espn.PlayKey(p), 6) >= "990000" {
				// At-bat results have ids ending 99xxxx; other results
				// (substitutions, steals) happen mid at-bat and their `outs`
				// is not reliable.
				outsAfter := outs
				if v, ok := p["outs"]; ok {
					outsAfter = intOr(v, 0)
				}
				onAfter, basesAfter := mlbRunners(p), mlbBases(p)
				if outsAfter >= 3 {
					basesAfter = ""
				}
				bases = basesAfter
				if outsAfter >= 3 {
					ctx.status = mlbStatus(label, 3, nil, nil)
				} else {
					ctx.status = mlbStatus(label, outsAfter, &onAfter, nil)
				}
				ctx.urgent = espn.Truthy(p["scoringPlay"]) || outsAfter > outs || onAfter > on
				outs, on = outsAfter, onAfter
			} else {
				if mlbOutEvent.MatchString(text) {
					outs++
				}
				ctx.status = mlbStatus(label, outs, &on, nil)
				ctx.urgent = espn.Truthy(p["scoringPlay"]) || mlbNormalEvent.MatchString(text)
			}
		default:
			ctx.status = mlbStatus(label, outs, &on, nil)
			if text == "" {
				ctx.announce = false
			}
		}
		ctx.bases = bases
		ctxs[espn.PlayKey(p)] = ctx
	}
	return ctxs
}
