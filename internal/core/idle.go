package core

import (
	"sort"

	"playcall/internal/espn"
)

// IdleState is the State of a Game that stands for one team with no game on
// the scoreboard (see IdleGames).
const IdleState = "idle"

// IdleLabel is what an idle team's row says: a bye on the weekly scoreboards
// (NFL, cfb), where a team skipping a week is resting, and "No game" on the
// daily ones.
func IdleLabel(sport string) string {
	if sport == "nfl" || sport == "cfb" {
		return "Bye"
	}
	return "No game"
}

// IdleGames returns one single-team Game (State IdleState, the team in Away,
// Status IdleLabel) for each of the group's teams that is not in the
// scoreboard's games, by abbreviation, so every team's standing shows. The
// weekly sports list them in the regular season only (a team with no game in
// the playoffs is out, not resting); the daily ones also in the preseason.
// There are none when the scoreboard has no games at all.
func IdleGames(sport string, games []espn.Game, teams []espn.StandingTeam) []espn.Game {
	if len(games) == 0 {
		return nil
	}
	weekly := sport == "nfl" || sport == "cfb"
	playing := map[string]bool{}
	for _, g := range games {
		if g.Season != "2" && (weekly || g.Season != "1") {
			return nil
		}
		playing[g.AwayID], playing[g.HomeID] = true, true
	}
	var idle []espn.Game
	for _, t := range teams {
		if playing[t.ID] {
			continue
		}
		idle = append(idle, espn.Game{
			Away: t.Abbr, AwayLogo: t.Logo, AwayRecord: t.Overall, AwayConf: t.Conf,
			State: IdleState, Status: IdleLabel(sport), Season: games[0].Season,
		})
	}
	sort.SliceStable(idle, func(i, j int) bool { return idle[i].Away < idle[j].Away })
	return idle
}
