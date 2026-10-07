package espn

import (
	"net/url"
	"strings"

	"playcall/internal/sports"
)

const (
	scoreboardFmt = "https://site.api.espn.com/apis/site/v2/sports/%s/scoreboard"
	summaryFmt    = "https://site.api.espn.com/apis/site/v2/sports/%s/summary?event=%s"
	teamsFmt      = "https://site.api.espn.com/apis/site/v2/sports/%s/teams?limit=500"
	standingsFmt  = "https://site.api.espn.com/apis/v2/sports/%s/standings?group=%s"
	gamePageFmt   = "https://www.espn.com/%s/game/_/gameId/%s"
)

func format(f string, args ...string) string {
	for _, a := range args {
		f = strings.Replace(f, "%s", a, 1)
	}
	return f
}

// ScoreboardURL builds the scoreboard URL for a sport. date is YYYYMMDD
// ("" for ESPN's default day; ESPN ignores `date=`, so the param is `dates=`).
// group overrides the sport's default group; sports with a default group
// (cfb: 80) always send `groups=`, others only when group is set.
func ScoreboardURL(sport, date, group string) string {
	u := format(scoreboardFmt, sports.Path(sport))
	var params []string
	if group == "" {
		group, _ = sports.DefaultGroup(sport)
	}
	if group != "" {
		params = append(params, "groups="+url.QueryEscape(group))
	}
	if date != "" {
		params = append(params, "dates="+url.QueryEscape(date))
	}
	if len(params) == 0 {
		return u
	}
	return u + "?" + strings.Join(params, "&")
}

// SummaryURL is the game summary (play-by-play) URL.
func SummaryURL(sport, gameID string) string {
	return format(summaryFmt, sports.Path(sport), gameID)
}

// TeamsURL lists a sport's teams.
func TeamsURL(sport string) string {
	return format(teamsFmt, sports.Path(sport))
}

// StandingsURL is the standings endpoint filtered by group id.
func StandingsURL(sport, groupID string) string {
	return format(standingsFmt, sports.Path(sport), groupID)
}

// GamePageURL is the espn.com game page. It uses the league segment of the
// sport's path (cfb -> college-football), not the bare sport key.
func GamePageURL(sport, gameID string) string {
	return format(gamePageFmt, sports.League(sport), gameID)
}
