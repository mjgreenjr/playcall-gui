package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"playcall/internal/difflib"
	"playcall/internal/espn"
)

// MissingTeamError says why no game was found for a team. Its text is the
// Python's log message.
type MissingTeamError struct{ Msg string }

func (e *MissingTeamError) Error() string { return e.Msg }

// FindGameForTeam returns the game on the sport's current scoreboard that has
// the team (abbr, uppercase). With several (e.g. a doubleheader) the live game
// wins, then an upcoming one, then a finished one; ties go to the earliest
// start, then to the first in the feed. Scoreboard errors are returned as
// espn.FetchScoreboard gives them; a team with no game is a *MissingTeamError.
func FindGameForTeam(ctx context.Context, f espn.Fetcher, sport, abbr string, now time.Time) (espn.Game, error) {
	data, err := espn.FetchScoreboard(ctx, f, sport, "", false, now)
	if err != nil {
		return espn.Game{}, err
	}
	rank := func(g espn.Game) int {
		switch g.State {
		case "in":
			return 0
		case "pre":
			return 1
		}
		return 2
	}
	var best espn.Game
	found := false
	for _, ev := range espn.List(data["events"]) {
		g, ok := espn.ParseScoreboardEvent(espn.Map(ev))
		if !ok || (abbr != strings.ToUpper(g.Away) && abbr != strings.ToUpper(g.Home)) {
			continue
		}
		if !found || rank(g) < rank(best) || (rank(g) == rank(best) && g.Date < best.Date) {
			best, found = g, true
		}
	}
	if !found {
		return espn.Game{}, explainMissingTeam(ctx, f, sport, abbr)
	}
	return best, nil
}

// explainMissingTeam fetches the sport's teams to tell an unknown abbreviation
// (with close matches) from a real team that is not on the scoreboard.
func explainMissingTeam(ctx context.Context, f espn.Fetcher, sport, abbr string) error {
	var abbrs []string
	seen := map[string]bool{}
	for _, t := range espn.FetchTeams(ctx, f, sport, "") {
		if u := strings.ToUpper(t.Abbr); !seen[u] {
			seen[u] = true
			abbrs = append(abbrs, u)
		}
	}
	switch {
	case len(abbrs) == 0:
		return &MissingTeamError{fmt.Sprintf("No %s game found for %s on the scoreboard", sport, abbr)}
	case seen[abbr]:
		return &MissingTeamError{fmt.Sprintf("%s has no %s game on the current scoreboard", abbr, sport)}
	}
	hint := ""
	if near := difflib.GetCloseMatches(abbr, abbrs, 3, 0.6); len(near) > 0 {
		hint = "; did you mean " + strings.Join(near, ", ") + "?"
	}
	return &MissingTeamError{fmt.Sprintf("%s is not a valid %s team%s (see 'playcall teams --sport %s')", abbr, sport, hint, sport)}
}

// IsMissingTeam reports whether err is a *MissingTeamError.
func IsMissingTeam(err error) bool {
	var m *MissingTeamError
	return errors.As(err, &m)
}
