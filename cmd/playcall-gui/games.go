package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
)

// stateRank puts live games first, then upcoming, then finished.
func stateRank(state string) int {
	switch state {
	case "in":
		return 0
	case "pre":
		return 1
	case core.IdleState:
		return 3
	}
	return 2
}

// orderForPicker sorts for choosing a game to watch: live, then upcoming by
// start, then finished. The sort is stable, so each group keeps start order.
func orderForPicker(games []espn.Game) []espn.Game {
	out := append([]espn.Game(nil), games...)
	sort.SliceStable(out, func(i, j int) bool { return stateRank(out[i].State) < stateRank(out[j].State) })
	return out
}

// matchup is "TB @ DAL".
func matchup(g espn.Game) string {
	return strings.TrimSpace(g.Away) + " @ " + strings.TrimSpace(g.Home)
}

// teamRecord is the small text after a team's abbreviation: the series
// record in a playoff series ("series 2-1", wins then the opponent's wins),
// otherwise the season record with the conference record after it ("3-1 (1-0)").
// Any part ESPN did not send is left out.
func teamRecord(overall, conf, seriesWins, opponentSeriesWins string) string {
	if seriesWins != "" && opponentSeriesWins != "" {
		return "series " + seriesWins + "-" + opponentSeriesWins
	}
	if overall != "" && conf != "" {
		return overall + " (" + conf + ")"
	}
	return overall
}

func awayRecord(g espn.Game) string {
	return teamRecord(g.AwayRecord, g.AwayConf, g.AwaySeries, g.HomeSeries)
}

func homeRecord(g espn.Game) string {
	return teamRecord(g.HomeRecord, g.HomeConf, g.HomeSeries, g.AwaySeries)
}

// statusRecords are the two teams' record texts for the game page header.
// division gives a team's division record where ESPN's game feed has no
// conference record (nil for none).
func statusRecords(s core.GameStatus, division func(abbr string) string) (away, home string) {
	conf := func(r core.TeamRecord, abbr string) string {
		if r.Conf == "" && division != nil {
			return division(abbr)
		}
		return r.Conf
	}
	return teamRecord(s.AwayRecord.Overall, conf(s.AwayRecord, s.Away.Abbr), s.AwayRecord.SeriesWins, s.HomeRecord.SeriesWins),
		teamRecord(s.HomeRecord.Overall, conf(s.HomeRecord, s.Home.Abbr), s.HomeRecord.SeriesWins, s.AwayRecord.SeriesWins)
}

// gameDetail is the right-hand side of a picker row: the score once the game
// has started, with ESPN's status ("5:12 - 1st", "Final", "10/9 - 9:00 PM EDT").
// A finished game that started on an earlier local day than now gets its date
// in front ("Fri 10/9  27-24  Final"), so last night's game is not mistaken for
// today's. A preseason game is tagged ("Preseason · 3-2  Final"). An upcoming game gets the weekday in front of ESPN's own date
// ("Sun 10/11 - 1:00 PM EDT").
func gameDetail(g espn.Game, now time.Time) string {
	detail := gameStatusText(g, now)
	if g.Season == "1" && g.State != core.IdleState {
		return "Preseason · " + detail
	}
	return detail
}

func gameStatusText(g espn.Game, now time.Time) string {
	if g.State == core.IdleState {
		return g.Status
	}
	if g.State == "pre" {
		// ESPN's status carries the date ("10/11 - 1:00 PM EDT"); add the weekday.
		start, err := time.Parse("2006-01-02T15:04Z", g.Date)
		if err != nil || g.Status == "" || g.Status[0] < '0' || g.Status[0] > '9' {
			return g.Status
		}
		return start.In(now.Location()).Weekday().String()[:3] + " " + g.Status
	}
	detail := fmt.Sprintf("%s-%s  %s", g.AwayScore, g.HomeScore, g.Status)
	if g.State != "post" {
		return detail
	}
	start, err := time.Parse("2006-01-02T15:04Z", g.Date)
	if err != nil {
		return detail
	}
	start = start.In(now.Location())
	if y, m, d := start.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return detail
	}
	return fmt.Sprintf("%s %d/%d  %s", start.Weekday().String()[:3], int(start.Month()), start.Day(), detail)
}

// anyGroup is the picker's "no conference or division" choice.
const anyGroup = "All"

// groupChoices are the picker's group names for a sport: "All", then the
// conferences and divisions. A leading "All ..." entry (college sports) is
// left out, since "All" already means that.
func groupChoices(sport string) []string {
	list := sports.Groups(sport)
	if sports.HasAllGroup(sport) && len(list) > 0 {
		list = list[1:]
	}
	out := []string{anyGroup}
	for _, g := range list {
		out = append(out, g.Name)
	}
	return out
}

// groupID is the scoreboard id for a name from groupChoices ("" for All).
func groupID(sport, name string) string {
	if name == anyGroup {
		return ""
	}
	for _, g := range sports.Groups(sport) {
		if g.Name == name {
			return g.ID
		}
	}
	return ""
}

// sportLabels are the names the window shows for each sport key.
var sportLabels = map[string]string{
	"nfl":  "NFL Football",
	"nba":  "NBA Basketball",
	"mlb":  "MLB Baseball",
	"nhl":  "NHL Hockey",
	"cfb":  "FBS College Football",
	"cbb":  "Men's College Basketball",
	"wnba": "WNBA Basketball",
}

// sportLabel is the display name for a sport key (the key itself if unknown).
func sportLabel(sport string) string {
	if l, ok := sportLabels[sport]; ok {
		return l
	}
	return sport
}

// sportFromLabel is the sport key for a display name ("" if unknown).
func sportFromLabel(label string) string {
	for key, l := range sportLabels {
		if l == label {
			return key
		}
	}
	return ""
}
