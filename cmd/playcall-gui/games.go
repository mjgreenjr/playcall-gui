package main

import (
	"fmt"
	"sort"
	"strings"

	"playcall/internal/espn"
	"playcall/internal/sports"
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

// anyGroup is the picker's "no conference or division" choice.
const anyGroup = "All"

// groupChoices are the picker's group names for a sport: "All", then the
// conferences and divisions. A leading "All ..." entry (college sports) is
// left out, since "All" already means that.
func groupChoices(sport string) []string {
	list := sports.Groups(sport)
	if sports.HasAllGroup(sport) && len(list) > 0 {
		list = list[1:]
	}
	out := []string{anyGroup}
	for _, g := range list {
		out = append(out, g.Name)
	}
	return out
}

// groupID is the scoreboard id for a name from groupChoices ("" for All).
func groupID(sport, name string) string {
	if name == anyGroup {
		return ""
	}
	for _, g := range sports.Groups(sport) {
		if g.Name == name {
			return g.ID
		}
	}
	return ""
}

// sportLabels are the names the window shows for each sport key.
var sportLabels = map[string]string{
	"nfl":  "NFL Football",
	"nba":  "NBA Basketball",
	"mlb":  "MLB Baseball",
	"nhl":  "NHL Hockey",
	"cfb":  "FBS College Football",
	"cbb":  "Men's College Basketball",
	"wnba": "WNBA Basketball",
}

// sportLabel is the display name for a sport key (the key itself if unknown).
func sportLabel(sport string) string {
	if l, ok := sportLabels[sport]; ok {
		return l
	}
	return sport
}

// sportFromLabel is the sport key for a display name ("" if unknown).
func sportFromLabel(label string) string {
	for key, l := range sportLabels {
		if l == label {
			return key
		}
	}
	return ""
}
