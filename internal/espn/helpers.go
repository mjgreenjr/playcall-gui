package espn

import (
	"strings"
)

// Competition returns the competition object (teams, scores, status) from
// either layout: summary has a top-level header, the CDN feed nests it under
// gamepackageJSON. It never returns nil.
func Competition(data Data) Data {
	header := Map(data["header"])
	if !Truthy(data["header"]) {
		header = Map(Map(data["gamepackageJSON"])["header"])
	}
	comps := List(data["competitions"])
	if len(comps) == 0 {
		comps = List(header["competitions"])
	}
	if len(comps) == 0 {
		return Data{}
	}
	if c := Map(comps[0]); c != nil {
		return c
	}
	return Data{}
}

// Teams returns the home and away competitors, matched by homeAway and never
// by list position. A missing side is nil.
func Teams(comp Data) (home, away Data) {
	for _, c := range List(comp["competitors"]) {
		m := Map(c)
		switch m["homeAway"] {
		case "home":
			if home == nil {
				home = m
			}
		case "away":
			if away == nil {
				away = m
			}
		}
	}
	return home, away
}

// TeamAbbr returns the competitor's team abbreviation, or "UNK".
func TeamAbbr(competitor Data) string {
	if v, ok := Map(competitor["team"])["abbreviation"]; ok {
		return Str(v)
	}
	return "UNK"
}

// Status returns the game status object from either layout. Never nil.
func Status(data Data) Data {
	if Truthy(data["status"]) {
		if s := Map(data["status"]); s != nil {
			return s
		}
	}
	if s := Map(Competition(data)["status"]); s != nil {
		return s
	}
	return Data{}
}

// IsFinal reports whether ESPN says the game is final.
func IsFinal(data Data) bool {
	t := Map(Status(data)["type"])
	short := strings.ToLower(Str(t["shortDetail"]))
	return strings.HasPrefix(short, "final") || Str(t["name"]) == "STATUS_FINAL"
}

// ScoreText renders a score that ESPN sends as a string, a number, or an
// object with displayValue/value. nil (and an object with neither) is "0".
func ScoreText(raw any) string {
	if m, ok := raw.(map[string]any); ok {
		for _, k := range []string{"displayValue", "value"} {
			if Truthy(m[k]) {
				return scalarText(m[k])
			}
		}
		return "0"
	}
	if raw == nil {
		return "0"
	}
	return scalarText(raw)
}

// scalarText is Python's str() for the scalar JSON values a score can be.
func scalarText(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "True"
		}
		return "False"
	}
	return Str(v)
}

// PlayText returns the play's description, stripped: text, then
// display.play.text, then description.
func PlayText(p Data) string {
	s := p["text"]
	if !Truthy(s) {
		s = Map(Map(p["display"])["play"])["text"]
	}
	if !Truthy(s) {
		s = p["description"]
	}
	return strings.TrimSpace(Str(s))
}

// PlayID returns the play's id as an int64. Ids are compared as integers,
// never as strings, because they are not all the same width.
func PlayID(p Data) (int64, bool) { return Int64(p["id"]) }

// playKey is the de-duplication key: the id's text (Python's str(p.get("id"))).
func playKey(p Data) string {
	if v, ok := p["id"]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
		return Str(v)
	}
	return "None"
}

// ExtractPlays collects plays from every known layout, de-duplicated by id:
// top-level plays, gamepackageJSON.playback.plays, then previous drives, then
// the current drive. The first occurrence fixes a play's position and the last
// occurrence supplies its content, as with the Python dict.
func ExtractPlays(data Data) []Data {
	gp := Map(data["gamepackageJSON"])
	var plays []any
	plays = append(plays, List(data["plays"])...)
	plays = append(plays, List(Map(gp["playback"])["plays"])...)

	drives := data["drives"]
	if !Truthy(drives) {
		drives = gp["drives"]
	}
	var driveList []any
	if m, ok := drives.(map[string]any); ok {
		driveList = append(driveList, List(m["previous"])...)
		if Truthy(m["current"]) {
			driveList = append(driveList, m["current"])
		}
	} else if l, ok := drives.([]any); ok {
		driveList = l
	}
	for _, d := range driveList {
		plays = append(plays, List(Map(d)["plays"])...)
	}

	index := map[string]int{}
	var out []Data
	for _, v := range plays {
		p := Map(v)
		if p == nil {
			continue
		}
		k := playKey(p)
		if i, ok := index[k]; ok {
			out[i] = p
			continue
		}
		index[k] = len(out)
		out = append(out, p)
	}
	return out
}

// PlayKey is the play's id as text ("None" when it has none), the key used
// for de-duplication, sent ids and scoring texts.
func PlayKey(p Data) string { return playKey(p) }

// Logo holds a team's logo URLs for each kind of background: Dark is meant
// for a dark background, Light for a light one. Either may be empty.
type Logo struct {
	Light, Dark string
}

// Pick returns the URL to use on a dark or light background and which variant
// it is ("dark" or "light"). If ESPN lacks the wanted variant the other one is
// used, labeled as what it really is. Both empty gives "", "".
func (l Logo) Pick(dark bool) (url, variant string) {
	first, second := l.Light, l.Dark
	firstName, secondName := "light", "dark"
	if dark {
		first, second = second, first
		firstName, secondName = secondName, firstName
	}
	if first != "" {
		return first, firstName
	}
	if second != "" {
		return second, secondName
	}
	return "", ""
}

// TeamLogos reads a team's logo URLs. The scoreboard gives `logo` and
// `logoDark`; the game feed, team list and standings give a `logos` list
// tagged with rels such as "default", "dark" and "scoreboard".
func TeamLogos(team Data) Logo {
	logo := Logo{Light: Str(team["logo"]), Dark: Str(team["logoDark"])}
	for _, l := range List(team["logos"]) {
		m := Map(l)
		href := Str(m["href"])
		if href == "" {
			continue
		}
		var dark, scoreboard bool
		for _, r := range List(m["rel"]) {
			switch Str(r) {
			case "dark":
				dark = true
			case "scoreboard":
				scoreboard = true
			}
		}
		switch {
		case scoreboard: // a differently cropped copy; the plain ones are used
		case dark && logo.Dark == "":
			logo.Dark = href
		case !dark && logo.Light == "":
			logo.Light = href
		}
	}
	return logo
}

// SeasonType is the season type ("1" preseason, "2" regular, "3" postseason)
// from a game feed's header, "" if it has none.
func SeasonType(data Data) string {
	header := Map(data["header"])
	if !Truthy(data["header"]) {
		header = Map(Map(data["gamepackageJSON"])["header"])
	}
	return Str(Map(header["season"])["type"])
}

// Rank is a college team's ranking, "1" to "25", or "" when it is unranked or
// ESPN gives none. The scoreboard sends it as `curatedRank.current` (99 means
// unranked), the game summary as `rank` (absent when unranked). Whether a sport
// has rankings at all is for the caller (sports.HasRankings).
func Rank(competitor Data) string {
	for _, v := range []any{Map(competitor["curatedRank"])["current"], competitor["rank"]} {
		if n, ok := Int64(v); ok && n >= 1 && n <= 25 {
			return Str(v)
		}
	}
	return ""
}
