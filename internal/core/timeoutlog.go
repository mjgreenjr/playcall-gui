package core

import (
	"log/slog"

	"playcall/internal/espn"
)

// logTimeouts writes ESPN's timeout counters to the DEBUG log, to work out
// what ESPN charges to a team: "Timeout counters" whenever timeoutsUsed or
// timeoutsRemaining changes for either team (with the newest play, to tie the
// change to it), and "Timeout play" the first time a timeout play is seen,
// with the counters of that poll and the play before it. On the first poll of
// a run the timeout plays already in the feed are marked backfill: their
// counters are today's, not those of the time.
func (p *Poller) logTimeouts(f *Feed) {
	home, away := espn.Teams(espn.Competition(f.Data))
	if home == nil || away == nil {
		return
	}
	counters := []any{
		"away", espn.TeamAbbr(away), "away_used", espn.Str(away["timeoutsUsed"]), "away_left", espn.Str(away["timeoutsRemaining"]),
		"home", espn.TeamAbbr(home), "home_used", espn.Str(home["timeoutsUsed"]), "home_left", espn.Str(home["timeoutsRemaining"]),
	}
	key := ""
	for _, c := range []espn.Data{away, home} {
		key += espn.Str(c["timeoutsUsed"]) + "/" + espn.Str(c["timeoutsRemaining"]) + ";"
	}
	game := p.State.GameID
	if key != "/;/;" && key != p.lastTimeouts { // "/;/;" is no counters at all
		args := append([]any{"game", game, "period", f.Period, "clock", f.Clock}, counters...)
		if n := len(f.Plays); n > 0 {
			last := f.Plays[n-1]
			args = append(args, "last_play", espn.PlayKey(last), "last_type", playType(last), "last_text", espn.PlayText(last))
		}
		slog.Debug("Timeout counters", args...)
		p.lastTimeouts = key
	}

	if p.timeoutPlays == nil {
		p.timeoutPlays = map[string]bool{}
	}
	backfill := !p.timeoutsSeeded
	for i, pl := range f.Plays {
		id := espn.PlayKey(pl)
		if !isTimeout(pl) || p.timeoutPlays[id] {
			continue
		}
		p.timeoutPlays[id] = true
		prev := espn.Data{}
		for j := i - 1; j >= 0; j-- {
			if !isTimeout(f.Plays[j]) {
				prev = f.Plays[j]
				break
			}
		}
		args := append([]any{"game", game, "play", id, "text", espn.PlayText(pl), "backfill", backfill,
			"period", espn.Map(pl["period"])["number"], "clock", espn.Map(pl["clock"])["displayValue"],
			"prev_type", playType(prev), "prev_scoring", espn.Truthy(prev["scoringPlay"]), "after_timeout", i > 0 && isTimeout(f.Plays[i-1])}, counters...)
		slog.Debug("Timeout play", args...)
	}
	p.timeoutsSeeded = true
}
