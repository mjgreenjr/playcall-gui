package core

import (
	"time"

	"playcall/internal/espn"
)

// creditLife is how long a rise of ESPN's timeout counter waits for the
// timeout row that goes with it. The counter leads the row by about 10
// seconds; a rise nobody claims in this time (a charged timeout ESPN never
// posted a row for) is dropped, so it cannot label a later timeout.
const creditLife = 90 * time.Second

// collegeTimeoutVerdicts decides, from ESPN's counter, which college timeout
// rows are charged (a team's) and which are not (an official's), for the rows
// that arrive while the poller is running. A rise of `timeoutsUsed` is a credit;
// a new timeout row takes a credit if there is one (charged) and is an
// official's if there is none. The plays alone get this wrong for about 9% of
// timeouts (258 checked against the counter), and the counter was right in
// every live check (2026-10-13 and 14), so it overrides the rules. A row seen
// on the poller's first poll (a restart, a game joined late) has no verdict
// and keeps the rules' label. The verdicts are in the feed for the rules.
func (p *Poller) collegeTimeoutVerdicts(f *Feed) {
	if p.Sport != "cfb" {
		return
	}
	if p.timeoutVerdicts == nil {
		p.timeoutVerdicts = map[string]bool{}
		p.timeoutSeen = map[string]bool{}
	}
	f.Charged = p.timeoutVerdicts
	now := p.now()

	var used [2]int64
	counters := true
	home, away := espn.Teams(espn.Competition(f.Data))
	for i, c := range []espn.Data{away, home} {
		u, ok := espn.Int64(c["timeoutsUsed"])
		if !ok {
			counters = false
			break
		}
		used[i] = u
	}
	if counters {
		if p.usedSeeded {
			for i := range used {
				if rise := int(used[i] - p.lastUsed[i]); rise > 0 {
					for ; rise > 0; rise-- {
						p.credits = append(p.credits, now)
					}
				}
			}
		}
		p.lastUsed, p.usedSeeded = used, true
	}
	live := p.credits[:0]
	for _, at := range p.credits {
		if now.Sub(at) <= creditLife {
			live = append(live, at)
		}
	}
	p.credits = live

	first := !p.verdictsSeeded
	p.verdictsSeeded = true
	for _, pl := range f.Plays {
		id := espn.PlayKey(pl)
		if !isTimeout(pl) || p.timeoutSeen[id] {
			continue
		}
		p.timeoutSeen[id] = true
		if first || !counters {
			continue
		}
		charged := len(p.credits) > 0
		if charged {
			p.credits = p.credits[1:]
		}
		p.timeoutVerdicts[id] = charged
	}
}
