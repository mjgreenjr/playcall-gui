package main

import (
	"fmt"
	"sort"
	"strings"

	"playcall/internal/espn"
)

// stateRank puts live games first, then upcoming, then finished.
func stateRank(state string) int {
	switch state {
	case "in":
		return 0
	case "pre":
		return 1
	}
	return 2
}

// orderForPicker sorts for choosing a game to watch: live, then upcoming by
// start, then finished. The sort is stable, so each group keeps start order.
func orderForPicker(games []espn.Game) []espn.Game {
	out := append([]espn.Game(nil), games...)
	sort.SliceStable(out, func(i, j int) bool { return stateRank(out[i].State) < stateRank(out[j].State) })
	return out
}

// matchup is "TB @ DAL".
func matchup(g espn.Game) string {
	return strings.TrimSpace(g.Away) + " @ " + strings.TrimSpace(g.Home)
}

// gameDetail is the right-hand side of a picker row: the score once the game
// has started, with ESPN's status ("5:12 - 1st", "Final", "10/9 - 9:00 PM EDT").
func gameDetail(g espn.Game) string {
	if g.State == "pre" {
		return g.Status
	}
	return fmt.Sprintf("%s-%s  %s", g.AwayScore, g.HomeScore, g.Status)
}
