package core

import (
	"regexp"
	"strings"

	"playcall/internal/espn"
)

// ESPN does not say who has the ball in basketball (the scoreboard's
// `situation` holds only the last play), so it is worked out from the plays,
// in order. The rules, applied to each play by the team that made it:
//
//   - a shot or free throw: the shooter's team has the ball ...
//   - ... until it goes in: after a made field goal, or the last free throw of
//     a trip, the other team has it
//   - a rebound, offensive or defensive: the rebounder's team
//   - a turnover (a steal is part of one): the other team. A bare "offensive
//     foul" play is ignored: ESPN often files it under the team that was fouled,
//     and the turnover play that follows it says who lost the ball
//   - a foul that sends the other team to the line (shooting, away from play,
//     and the plain personal and take fouls, which keep the ball with the
//     team fouled): the team fouled. This is what puts the ball back with a
//     player who scored and was fouled (the "and-one") for the free throw
//   - a jump ball: the team the play says gains possession; a jump ball
//     violation: the other team
//   - the end of a period or game: nobody is known to, until the next shot,
//     rebound or turnover shows who has it
//
// Fouls, timeouts, substitutions and the like change nothing. Technical free
// throws do not either, since the team that had the ball keeps it.
//
// It is an inference: a play ESPN adds late or mislabels can throw it off
// until the next shot, rebound or turnover puts it right.

var freeThrowOf = regexp.MustCompile(`(\d+) of (\d+)`)

// basketballPossessionTrace is the team id with the ball after each play, ""
// where it is not known. teamA and teamB are the two teams' ids.
func basketballPossessionTrace(plays []espn.Data, teamA, teamB string) []string {
	other := func(id string) string {
		switch id {
		case teamA:
			return teamB
		case teamB:
			return teamA
		}
		return ""
	}
	out := make([]string, len(plays))
	poss := ""
	retain := false // the fouled team keeps the ball after its free throw
	for i, p := range plays {
		team := espn.Str(espn.Map(p["team"])["id"])
		typ := strings.ToLower(strings.Join(strings.Fields(playType(p)), " "))
		text := strings.ToLower(espn.PlayText(p))
		switch {
		case strings.Contains(typ, "end period") || strings.Contains(typ, "end game"):
			poss = ""
		case typ == "jumpball":
			if team != "" && strings.Contains(text, "gains possession") {
				poss = team
			}
		case typ == "jump ball": // a violation
			poss = other(team)
		case strings.Contains(typ, "turnover") || strings.Contains(text, "turnover"):
			poss = other(team)
		case strings.Contains(typ, "free throw"):
			if strings.Contains(text, "technical") || strings.Contains(typ, "technical") {
				break
			}
			poss = team
			if m := freeThrowOf.FindStringSubmatch(typ); m != nil && m[1] == m[2] {
				if espn.Truthy(p["scoringPlay"]) && !retain {
					poss = other(team) // the last free throw went in
				}
				retain = false
			}
		case strings.Contains(typ, "away from play foul") || strings.Contains(typ, "flagrant"):
			poss = other(team)
			retain = true
		case strings.Contains(typ, "shooting foul") || typ == "personal foul" || strings.Contains(typ, "take foul"):
			poss = other(team)
		case strings.Contains(typ, "rebound"):
			if team != "" {
				poss = team
			}
		case espn.Truthy(p["shootingPlay"]):
			poss = team
			if espn.Truthy(p["scoringPlay"]) {
				poss = other(team)
			}
		}
		out[i] = poss
	}
	return out
}

// basketballPossession is the team id with the ball after the last play.
func basketballPossession(plays []espn.Data, teamA, teamB string) string {
	trace := basketballPossessionTrace(plays, teamA, teamB)
	if len(trace) == 0 {
		return ""
	}
	return trace[len(trace)-1]
}
