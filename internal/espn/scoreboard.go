package espn

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// Fetcher gets ESPN JSON by URL. *Client is one; tests and the CLI supply
// their own. An error means "no data".
type Fetcher interface {
	Fetch(ctx context.Context, url string) (Data, error)
}

// Errors from FetchScoreboard. Their text is the Python's log message.
var (
	ErrScoreboardFetch = errors.New("Could not fetch scoreboard data")
	ErrNoEvents        = errors.New("No events found in scoreboard data")
)

// Game is one scoreboard event with two teams.
type Game struct {
	ID        string
	Date      string // the event's `date`, "" if missing; ISO strings sort as text
	Away      string // abbreviations
	Home      string
	Status    string // status.type.shortDetail, "Unknown" if missing
	State     string // status.type.state: "pre", "in", "post"
	AwayScore string
	HomeScore string
	AwayLogo  Logo // ESPN's logo URLs; either variant may be empty
	HomeLogo  Logo
	// Season records as ESPN writes them ("3-1"); empty when it sends none.
	// Conf is the conference record, which only the college sports carry.
	AwayRecord, HomeRecord string
	AwayConf, HomeConf     string
	// Wins in the current playoff series ("2"), "" outside one. They count
	// completed games only.
	AwaySeries, HomeSeries string
}

// ParseScoreboardEvent reads a scoreboard event. ok is false when the event
// does not have both a home and an away team.
func ParseScoreboardEvent(event Data) (g Game, ok bool) {
	home, away := Teams(Competition(event))
	if home == nil || away == nil {
		return Game{}, false
	}
	t := Map(Status(event)["type"])
	status := "Unknown"
	if v, found := t["shortDetail"]; found {
		status = Str(v)
	}
	id := "None"
	if v, found := event["id"]; found && v != nil {
		id = Str(v)
	}
	return Game{
		ID:        id,
		Date:      Str(event["date"]),
		Away:      TeamAbbr(away),
		Home:      TeamAbbr(home),
		Status:    status,
		State:     Str(t["state"]),
		AwayScore: ScoreText(away["score"]),
		HomeScore: ScoreText(home["score"]),
		AwayLogo:  TeamLogos(Map(away["team"])),
		HomeLogo:  TeamLogos(Map(home["team"])),

		AwayRecord: record(away, "total"),
		HomeRecord: record(home, "total"),
		AwayConf:   record(away, "vsconf"),
		HomeConf:   record(home, "vsconf"),
		AwaySeries: seriesWins(Competition(event), away),
		HomeSeries: seriesWins(Competition(event), home),
	}, true
}

// record is a competitor's record summary of one type ("total", "vsconf"),
// "" if ESPN does not send it.
func record(competitor Data, typ string) string {
	for _, r := range List(competitor["records"]) {
		if m := Map(r); Str(m["type"]) == typ {
			return Str(m["summary"])
		}
	}
	return ""
}

// seriesWins is the competitor's wins in the competition's playoff series,
// matched by team id; "" if the game is not in a playoff series. The scoreboard
// sends `series` as one object, the summary as a list.
func seriesWins(comp Data, competitor Data) string {
	candidates := List(comp["series"])
	if m := Map(comp["series"]); m != nil {
		candidates = []any{m}
	}
	for _, c := range candidates {
		m := Map(c)
		if Str(m["type"]) != "playoff" {
			continue
		}
		for _, t := range List(m["competitors"]) {
			tm := Map(t)
			if Str(tm["id"]) == Str(competitor["id"]) && tm["wins"] != nil {
				return Str(tm["wins"])
			}
		}
	}
	return ""
}

// scheduledStatus matches a scheduled start, e.g. "10/9 - 9:00 PM EDT".
var scheduledStatus = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})( - )(\d{1,2})(:\d{2}.*)$`)

// PadStatus lines up scheduled start times in `list` output: a single-digit
// month gets a leading space, a single-digit day a trailing space and a
// single-digit hour a leading 0 ("9/5 - 7:30 PM" -> " 9/5  - 07:30 PM").
// Other statuses pass through unchanged.
func PadStatus(status string) string {
	m := scheduledStatus.FindStringSubmatch(status)
	if m == nil {
		return status
	}
	month, day, sep, hour, rest := m[1], m[2], m[3], m[4], m[5]
	if len(month) < 2 {
		month = " " + month
	}
	if len(day) < 2 {
		day += " "
	}
	if len(hour) < 2 {
		hour = "0" + hour
	}
	return month + "/" + day + sep + hour + rest
}

// FetchScoreboard fetches a sport's scoreboard (group is a group id, "" for
// none). A scoreboard with no events is retried with today's date (from now,
// local time). With a group ESPN does not skip ahead to the next game day like
// the unfiltered scoreboard does, so a still-empty result is retried on the
// day the unfiltered scoreboard shows. It returns ErrScoreboardFetch when the
// first fetch fails, and ErrNoEvents when data is missing or has no events
// (unless allowEmpty and some data came back).
func FetchScoreboard(ctx context.Context, f Fetcher, sport, group string, allowEmpty bool, now time.Time) (Data, error) {
	fetch := func(url string) Data {
		d, err := f.Fetch(ctx, url)
		if err != nil {
			return nil
		}
		return d
	}
	data := fetch(ScoreboardURL(sport, "", group))
	if data == nil {
		return nil, ErrScoreboardFetch
	}
	if !Truthy(data["events"]) {
		today := now.Format("20060102")
		slog.Debug("No events in default scoreboard; retrying", "date", today)
		data = fetch(ScoreboardURL(sport, today, group))
	}
	if group != "" && data != nil && !Truthy(data["events"]) {
		day := Str(Map(fetch(ScoreboardURL(sport, "", ""))["day"])["date"])
		if day != "" {
			slog.Debug("No events in group; retrying", "group", group, "date", day)
			data = fetch(ScoreboardURL(sport, strings.ReplaceAll(day, "-", ""), group))
		}
	}
	if data == nil || !(Truthy(data["events"]) || allowEmpty) {
		return nil, ErrNoEvents
	}
	return data, nil
}
