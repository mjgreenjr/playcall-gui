package core

import (
	"sort"

	"playcall/internal/espn"
)

// ByeState is the State of a Game that stands for a team with no game this
// week (see ByeGames).
const ByeState = "bye"

// HasByes is true for the sports whose scoreboard is a week: a team missing
// from it has a bye. The daily sports have no such thing.
func HasByes(sport string) bool { return sport == "nfl" || sport == "cfb" }

// ByeGames returns one single-team Game (State ByeState, the team in Away)
// for each of the group's teams that is not in the week's games, by
// abbreviation. There are none outside the regular season, when a team with
// no game is out of the running rather than resting, or when the scoreboard
// has no games at all.
func ByeGames(games []espn.Game, teams []espn.StandingTeam) []espn.Game {
	if len(games) == 0 {
		return nil
	}
	playing := map[string]bool{}
	for _, g := range games {
		if g.Season != "2" {
			return nil
		}
		playing[g.AwayID], playing[g.HomeID] = true, true
	}
	var byes []espn.Game
	for _, t := range teams {
		if playing[t.ID] {
			continue
		}
		byes = append(byes, espn.Game{
			Away: t.Abbr, AwayLogo: t.Logo, AwayRecord: t.Overall, AwayConf: t.Conf,
			State: ByeState, Status: "Bye", Season: "2",
		})
	}
	sort.SliceStable(byes, func(i, j int) bool { return byes[i].Away < byes[j].Away })
	return byes
}
