package espn

import (
	"context"
	"sort"

	"playcall/internal/sports"
)

// Team is a team's abbreviation, display name and logo (a URL for a dark
// background, "" if ESPN has none).
type Team struct {
	Abbr, Name, Logo string
}

// FetchTeams returns the sport's teams, or nil if they cannot be fetched. A
// group (id) reads that group's standings; with no group, cfb and cbb default
// to their first "All ..." group and the other sports use the teams endpoint.
func FetchTeams(ctx context.Context, f Fetcher, sport, group string) []Team {
	if group == "" && sports.HasAllGroup(sport) {
		group = sports.Groups(sport)[0].ID
	}
	if group != "" {
		data, err := f.Fetch(ctx, StandingsURL(sport, group))
		if err != nil || !Truthy(data) {
			return nil
		}
		return standingsTeams(data)
	}
	data, err := f.Fetch(ctx, TeamsURL(sport))
	if err != nil || !Truthy(data) {
		return nil
	}
	var teams []Team
	if all := List(data["sports"]); len(all) > 0 {
		for _, league := range List(Map(all[0])["leagues"]) {
			for _, t := range List(Map(league)["teams"]) {
				teams = append(teams, teamOf(Map(Map(t)["team"])))
			}
		}
	}
	return teams
}

func teamOf(t Data) Team {
	return Team{Abbr: Str(t["abbreviation"]), Name: Str(t["displayName"]), Logo: TeamLogo(t)}
}

// standingsTeams collects the teams of a standings tree; conferences nest.
func standingsTeams(node Data) []Team {
	var teams []Team
	for _, e := range List(Map(node["standings"])["entries"]) {
		teams = append(teams, teamOf(Map(Map(e)["team"])))
	}
	for _, c := range List(node["children"]) {
		teams = append(teams, standingsTeams(Map(c))...)
	}
	return teams
}

// SortTeams removes duplicates and sorts by name, then by abbreviation (the
// Python leaves ties in set order; this keeps the output deterministic).
func SortTeams(teams []Team) []Team {
	seen := map[Team]bool{}
	var out []Team
	for _, t := range teams {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Abbr < out[j].Abbr
	})
	return out
}
