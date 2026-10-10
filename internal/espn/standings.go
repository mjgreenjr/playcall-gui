package espn

import (
	"context"
	"errors"

	"playcall/internal/sports"
)

// divisionStat is the standings stat type that holds a team's record against
// its own division, per sport. ESPN's scoreboard has no such record; the
// college sports get a conference record there instead.
var divisionStat = map[string]string{
	"nfl":  "vsdiv",
	"wnba": "vsconf", // no divisions: the conference record stands in
	"nba":  "vsdiv",
	"nhl":  "vsdiv",
	"mlb":  "intradivision",
}

// HasDivisionRecords is true for the sports whose division record (the
// conference record, for wnba) can be read from the standings.
func HasDivisionRecords(sport string) bool {
	_, ok := divisionStat[sport]
	return ok
}

// StandingsAllURL is the whole sport's standings, divisions included.
func StandingsAllURL(sport string) string {
	return "https://site.api.espn.com/apis/v2/sports/" + sports.Path(sport) + "/standings?level=3"
}

// ErrNoStandings means the standings could not be fetched or held no records.
var ErrNoStandings = errors.New("no division records in the standings")

// FetchDivisionRecords returns each team's record against its division, by
// abbreviation ("2-1", "2-1-0" in hockey).
func FetchDivisionRecords(ctx context.Context, f Fetcher, sport string) (map[string]string, error) {
	typ, ok := divisionStat[sport]
	if !ok {
		return nil, ErrNoStandings
	}
	data, err := f.Fetch(ctx, StandingsAllURL(sport))
	if err != nil || !Truthy(data) {
		return nil, ErrNoStandings
	}
	records := map[string]string{}
	var walk func(node Data)
	walk = func(node Data) {
		for _, e := range List(Map(node["standings"])["entries"]) {
			entry := Map(e)
			abbr := Str(Map(entry["team"])["abbreviation"])
			for _, s := range List(entry["stats"]) {
				stat := Map(s)
				if Str(stat["type"]) != typ {
					continue
				}
				rec := Str(stat["summary"])
				if rec == "" {
					rec = Str(stat["displayValue"])
				}
				if abbr != "" && rec != "" {
					records[abbr] = rec
				}
			}
		}
		for _, c := range List(node["children"]) {
			walk(Map(c))
		}
	}
	walk(data)
	if len(records) == 0 {
		return nil, ErrNoStandings
	}
	return records, nil
}

// StandingTeam is a team's line in the standings.
type StandingTeam struct {
	ID, Abbr string
	Logo     Logo
	// Overall is the season record ("5-0"); Conf is the record against its
	// conference (college) or division (pro). Either may be "".
	Overall, Conf string
}

// FetchStandingTeams lists the teams of a group (id; "" for the whole sport,
// which for cfb is the FBS group) with their records. An error means the
// standings could not be fetched.
func FetchStandingTeams(ctx context.Context, f Fetcher, sport, group string) ([]StandingTeam, error) {
	url := StandingsURL(sport, group)
	switch {
	case group == "" && HasDivisionRecords(sport):
		url = StandingsAllURL(sport)
	case group == "" && sports.HasAllGroup(sport):
		url = StandingsURL(sport, sports.Groups(sport)[0].ID)
	case group == "":
		url = StandingsAllURL(sport)
	}
	data, err := f.Fetch(ctx, url)
	if err != nil || !Truthy(data) {
		return nil, ErrNoStandings
	}
	confType, ok := divisionStat[sport]
	if !ok {
		confType = "vsconf"
	}
	var teams []StandingTeam
	seen := map[string]bool{}
	var walk func(node Data)
	walk = func(node Data) {
		for _, e := range List(Map(node["standings"])["entries"]) {
			entry := Map(e)
			team := Map(entry["team"])
			id := Str(team["id"])
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			st := StandingTeam{ID: id, Abbr: Str(team["abbreviation"]), Logo: TeamLogos(team)}
			for _, s := range List(entry["stats"]) {
				stat := Map(s)
				rec := Str(stat["summary"])
				if rec == "" {
					rec = Str(stat["displayValue"])
				}
				switch Str(stat["type"]) {
				case "total":
					st.Overall = rec
				case confType:
					st.Conf = rec
				}
			}
			teams = append(teams, st)
		}
		for _, c := range List(node["children"]) {
			walk(Map(c))
		}
	}
	walk(data)
	return teams, nil
}
