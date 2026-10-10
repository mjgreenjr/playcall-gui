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
	"nfl": "vsdiv",
	"nba": "vsdiv",
	"nhl": "vsdiv",
	"mlb": "intradivision",
}

// HasDivisionRecords is true for the sports whose division record can be read
// from the standings (wnba has no divisions).
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
