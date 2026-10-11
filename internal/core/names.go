package core

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"playcall/internal/espn"
)

// nameSports are the sports whose play text names players in full ("Nikita
// Zadorov hit Noel Acciari"), so a team can be added after each name. Football
// text has numbers and initials ("#3 K.Minchey"); baseball results use surnames.
var nameSports = map[string]bool{"nhl": true, "nba": true, "wnba": true, "cbb": true, "mlb": true}

// rosterTeams maps each player's full name to the team's abbreviation, from the
// box score. A name that two teams share is left out.
func rosterTeams(data espn.Data) map[string]string {
	roster := map[string]string{}
	clash := map[string]bool{}
	for _, t := range espn.List(espn.Map(data["boxscore"])["players"]) {
		team := espn.Str(espn.Map(espn.Map(t)["team"])["abbreviation"])
		if team == "" {
			continue
		}
		for _, g := range espn.List(espn.Map(t)["statistics"]) {
			for _, a := range espn.List(espn.Map(g)["athletes"]) {
				name := strings.TrimSpace(espn.Str(espn.Map(espn.Map(a)["athlete"])["displayName"]))
				if name == "" || !strings.Contains(name, " ") {
					continue
				}
				if old, ok := roster[name]; ok && old != team {
					clash[name] = true
				}
				roster[name] = team
			}
		}
	}
	for name := range clash {
		delete(roster, name)
	}
	return roster
}

// annotateNames adds " (TEAM)" after each known full name in text.
func annotateNames(text string, roster map[string]string) string {
	if len(roster) == 0 {
		return text
	}
	names := make([]string, 0, len(roster))
	for n := range roster {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	var out strings.Builder
	for i := 0; i < len(text); {
		matched := ""
		if startsName(text, i) {
			for _, n := range names {
				if strings.HasPrefix(text[i:], n) && endsName(text, i+len(n)) {
					matched = n
					break
				}
			}
		}
		if matched == "" {
			_, size := utf8.DecodeRuneInString(text[i:])
			out.WriteString(text[i : i+size])
			i += size
			continue
		}
		out.WriteString(matched)
		i += len(matched)
		if !strings.HasPrefix(text[i:], " (") {
			out.WriteString(" (" + roster[matched] + ")")
		}
	}
	return out.String()
}

// startsName and endsName check a name is not inside a longer word.
func startsName(text string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func endsName(text string, i int) bool {
	if i >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[i:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
