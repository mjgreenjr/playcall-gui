package core

import (
	"fmt"

	"playcall/internal/espn"
)

// seriesText is the postseason series standing for a game, without
// parentheses: "LAD 2-1" (leader first), "Tied 2-2", or "BOS WINS" once a
// team has the wins to clinch. It is empty outside a playoff series, and at 0-0.
//
// The data is the header competition's `series` entry with type "playoff"
// (the "current" and "season" entries are ignored). Its competitors carry the
// team id and `wins` only, so they are matched to the game's competitors by id.
// ESPN counts only completed games, so an in-progress game is not in the wins.
//
// With final set, gameID's own result is included: ESPN may not have updated
// the wins yet when the game turns final, so if this game's place in the
// series' event list shows it is not counted, the winner gets the win.
func seriesText(data espn.Data, gameID string, final bool) string {
	comp := espn.Competition(data)
	var series espn.Data
	for _, s := range espn.List(comp["series"]) {
		if m := espn.Map(s); espn.Str(m["type"]) == "playoff" {
			series = m
			break
		}
	}
	if series == nil {
		return ""
	}
	wins := map[string]int64{}
	var total int64
	for _, c := range espn.List(series["competitors"]) {
		m := espn.Map(c)
		w, _ := espn.Int64(m["wins"])
		wins[espn.Str(m["id"])] = w
		total += w
	}
	if final {
		for i, e := range espn.List(series["events"]) {
			if espn.Str(espn.Map(e)["id"]) != gameID {
				continue
			}
			if total < int64(i+1) {
				if id := winnerID(comp); id != "" {
					if _, ok := wins[id]; ok {
						wins[id]++
					}
				}
			}
			break
		}
	}

	home, away := espn.Teams(comp)
	if home == nil || away == nil {
		return ""
	}
	type side struct {
		abbr string
		wins int64
	}
	sides := []side{}
	for _, t := range []espn.Data{away, home} {
		w, ok := wins[espn.Str(t["id"])]
		if !ok {
			return ""
		}
		sides = append(sides, side{espn.TeamAbbr(t), w})
	}
	if sides[1].wins > sides[0].wins {
		sides[0], sides[1] = sides[1], sides[0]
	}
	lead, trail := sides[0], sides[1]

	// Best-of-N: the first to N/2+1 wins the series.
	if n, _ := espn.Int64(series["totalCompetitions"]); n > 0 && lead.wins >= n/2+1 {
		return lead.abbr + " WINS"
	}
	switch {
	case lead.wins == trail.wins && lead.wins == 0:
		return ""
	case lead.wins == trail.wins:
		return fmt.Sprintf("Tied %d-%d", lead.wins, trail.wins)
	}
	return fmt.Sprintf("%s %d-%d", lead.abbr, lead.wins, trail.wins)
}

// winnerID is the id of the competitor flagged as the winner, or "".
func winnerID(comp espn.Data) string {
	for _, c := range espn.List(comp["competitors"]) {
		if m := espn.Map(c); espn.Truthy(m["winner"]) {
			return espn.Str(m["id"])
		}
	}
	return ""
}

// withSeries appends the series standing to a title: "... · ATL 1 (LAD 2-1)".
func withSeries(title, series string) string {
	if series == "" {
		return title
	}
	return title + " (" + series + ")"
}
