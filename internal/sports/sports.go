// Package sports holds the per-sport data tables (ESPN paths, scoreboard
// groups) and group lookup. It mirrors SPORTS, GROUPS, DEFAULT_GROUPS and
// ALL_GROUP_SPORTS in reference/playcall.py.
package sports

import (
	"fmt"
	"strings"
)

// DefaultSport is the --sport default.
const DefaultSport = "nfl"

// Names lists the supported sport keys in the Python's SPORTS order.
var Names = []string{"nfl", "nba", "mlb", "nhl", "cfb", "cbb", "wnba"}

// paths maps a sport key to ESPN's {sport}/{league} path segment.
var paths = map[string]string{
	"nfl":  "football/nfl",
	"nba":  "basketball/nba",
	"mlb":  "baseball/mlb",
	"nhl":  "hockey/nhl",
	"cfb":  "football/college-football",
	"cbb":  "basketball/mens-college-basketball",
	"wnba": "basketball/wnba",
}

// Group is one scoreboard group (conference or division).
type Group struct {
	Name string
	ID   string
}

// groups keeps the Python's insertion order: it drives the "valid groups"
// message and the default group for `teams` (the first entry for cfb/cbb).
var groups = map[string][]Group{
	"cfb": {
		{"All FBS", "80"}, {"ACC", "1"}, {"Big 12", "4"}, {"Big Ten", "5"},
		{"SEC", "8"}, {"CUSA", "12"}, {"MAC", "15"}, {"Mountain West", "17"},
		{"Sun Belt", "37"},
	},
	"cbb": {
		{"All D-I", "50"}, {"America East", "1"}, {"ACC", "2"}, {"A-10", "3"},
		{"Big East", "4"}, {"Big Sky", "5"}, {"Big South", "6"}, {"Big Ten", "7"},
		{"Big 12", "8"}, {"Big West", "9"}, {"CAA", "10"}, {"CUSA", "11"},
		{"Ivy", "12"}, {"MAAC", "13"}, {"MAC", "14"}, {"MEAC", "16"},
		{"MVC", "18"}, {"NEC", "19"}, {"OVC", "20"}, {"Pac-12", "21"},
		{"Patriot", "22"}, {"SEC", "23"}, {"SoCon", "24"}, {"Southland", "25"},
		{"SWAC", "26"}, {"Sun Belt", "27"}, {"WCC", "29"}, {"UAC", "30"},
		{"Independents", "43"}, {"Mountain West", "44"}, {"Horizon", "45"},
		{"ASUN", "46"}, {"Summit", "49"}, {"American", "62"},
	},
	"nfl": {
		{"AFC", "8"}, {"NFC", "7"}, {"AFC East", "4"}, {"AFC North", "12"},
		{"AFC South", "13"}, {"AFC West", "6"}, {"NFC East", "1"},
		{"NFC North", "10"}, {"NFC South", "11"}, {"NFC West", "3"},
	},
	"nba": {
		{"East", "5"}, {"West", "6"}, {"Atlantic", "1"}, {"Central", "2"},
		{"Southeast", "9"}, {"Northwest", "11"}, {"Pacific", "4"},
		{"Southwest", "10"},
	},
	"mlb": {
		{"AL", "7"}, {"NL", "8"}, {"AL East", "1"}, {"AL Central", "2"},
		{"AL West", "3"}, {"NL East", "4"}, {"NL Central", "5"}, {"NL West", "6"},
	},
	"nhl": {
		{"East", "7"}, {"West", "8"}, {"Atlantic", "32"}, {"Metropolitan", "33"},
		{"Central", "31"}, {"Pacific", "30"},
	},
	"wnba": {{"East", "1"}, {"West", "2"}},
}

// defaultGroups are sent to the scoreboard even without --group (cfb: 80 is
// all FBS; without it ESPN returns only the top 25).
var defaultGroups = map[string]string{"cfb": "80"}

// RankedGroupID is not an ESPN group id but a request for ESPN's own default
// college football scoreboard, which is sent without `groups=` and holds only
// the games with a ranked team (checked: 16 of the week's 62 games, every one
// with a team in the top 25). It is the GUI's "Top 25" choice (cfb only for
// now) and is left out of the `groups` table, so the CLI's group lists, the
// standings lookups and the goldens do not change. It only works on the default
// request: with a `dates=` parameter ESPN answers with every game.
const RankedGroupID = "ranked"

// RankedGroupName is how the picker shows RankedGroupID.
const RankedGroupName = "Top 25"

// allGroupSports start their groups with an "All ..." entry, the default for `teams`.
var allGroupSports = map[string]bool{"cfb": true, "cbb": true}

// Valid reports whether sport is a supported sport key.
func Valid(sport string) bool {
	_, ok := paths[sport]
	return ok
}

// Validate returns an error listing the valid sports when sport is unknown.
func Validate(sport string) error {
	if Valid(sport) {
		return nil
	}
	return fmt.Errorf("sport must be one of %s, got %q", strings.Join(Names, ", "), sport)
}

// Path returns ESPN's "{sport}/{league}" segment, e.g. "football/college-football".
// It is empty for an unknown sport.
func Path(sport string) string { return paths[sport] }

// League returns the part of Path after the slash, the segment espn.com's
// game pages use (cfb -> "college-football"). It is empty for an unknown sport.
func League(sport string) string {
	_, league, _ := strings.Cut(paths[sport], "/")
	return league
}

// Groups returns the sport's groups in table order. The slice is a copy.
func Groups(sport string) []Group {
	return append([]Group(nil), groups[sport]...)
}

// DefaultGroup returns the group id always sent to the scoreboard, if any.
func DefaultGroup(sport string) (string, bool) {
	id, ok := defaultGroups[sport]
	return id, ok
}

// HasAllGroup reports whether the sport's first group is an "All ..." entry
// (cfb, cbb), which `teams` uses as its default.
func HasAllGroup(sport string) bool { return allGroupSports[sport] }

// GroupKey normalizes a group name for matching: "Big Ten", "big-ten" and
// "bigten" agree. Like the Python it also drops underscores.
func GroupKey(name string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(name))
}

// ResolveGroup finds a group by id or by name (ignoring case, spaces, hyphens
// and underscores). An unknown value is an error listing the valid groups.
func ResolveGroup(sport, value string) (Group, error) {
	list := groups[sport]
	if len(list) == 0 {
		return Group{}, fmt.Errorf("--group is not supported for %s", sport)
	}
	for _, g := range list {
		if value == g.ID || GroupKey(value) == GroupKey(g.Name) {
			return g, nil
		}
	}
	valid := make([]string, len(list))
	for i, g := range list {
		valid[i] = fmt.Sprintf("%s (%s)", g.Name, g.ID)
	}
	return Group{}, fmt.Errorf("unknown %s group '%s'; valid groups: %s", sport, value, strings.Join(valid, ", "))
}
