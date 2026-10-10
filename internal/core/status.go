package core

import (
	"strings"
	"time"

	"playcall/internal/espn"
)

// GameStatus is the game as of the last successful fetch: what a persistent
// header shows between plays. It is built from ESPN's game-level status, not
// from a play, so it does not inherit a play's snapshot of the clock. The
// clock is ESPN's displayClock as fetched; nothing ticks locally.
type GameStatus struct {
	GameID, Sport string
	Away, Home    TeamScore
	// State is ESPN's status state: "pre", "in" or "post" ("" if unknown).
	State string
	// Clock and Period are ESPN's displayClock and period text.
	Clock, Period string
	// Detail is ESPN's shortDetail ("Final", "End of 2nd", "10/9 - 9:00 PM EDT").
	Detail string
	// Halftime is true for the whole break, not just the end-of-half play.
	Halftime bool
	// Break is true between periods ("End of 3rd"), when ESPN's clock sits at
	// zero until the next one starts; Detail then says what to show.
	Break bool
	Final bool
	// Series is the postseason standing without parentheses, or "".
	Series string
	// Situation is football's down, distance and spot after the last play
	// ("1st & 10 at WASH 39"), "" when there is none (other sports, kickoffs,
	// between plays, not in play).
	Situation string
	// Possession is the side on offence: "away", "home", or "" when the sport
	// has no such notion (hockey, basketball: ESPN does not say), the game is
	// not in play (before the start, halftime, final) or ESPN does not say.
	Possession string
	// UpdatedAt is when ESPN last answered, so a header can dim or show
	// "updated Ns ago" while polls fail and the loop backs off.
	UpdatedAt time.Time
}

// StatusSink receives a GameStatus after every successful fetch, before that
// poll's plays are sent. It is called on the poll goroutine and must not
// block; a GUI should hand the value to its own goroutine.
type StatusSink interface {
	Status(GameStatus)
}

// StatusFunc adapts a function to a StatusSink.
type StatusFunc func(GameStatus)

// Status calls f.
func (f StatusFunc) Status(s GameStatus) { f(s) }

func (p *Poller) publishStatus(f *Feed) {
	if p.StatusSink == nil {
		return
	}
	st := espn.Map(espn.Status(f.Data)["type"])
	series := f.Series
	if f.Finished {
		series = seriesText(f.Data, p.State.GameID, true)
	}
	p.StatusSink.Status(GameStatus{
		GameID:     p.State.GameID,
		Sport:      p.Sport,
		Away:       f.Away,
		Home:       f.Home,
		State:      espn.Str(st["state"]),
		Clock:      f.Clock,
		Period:     f.Period,
		Detail:     espn.Str(st["shortDetail"]),
		Halftime:   espn.Str(st["name"]) == "STATUS_HALFTIME",
		Break:      espn.Str(st["name"]) == "STATUS_END_PERIOD",
		Final:      f.Finished,
		Series:     series,
		Possession: possession(p.Sport, f),
		Situation:  situation(p.Sport, f),
		UpdatedAt:  p.now(),
	})
}

// possession finds the side on offence from the game feed.
//
// Football: the team the last play ended with the ball (a punt or kickoff
// ends with the receiver), else the one that started it. Baseball: the team
// batting in the current half inning; between halves nobody is.
func possession(sport string, f *Feed) string {
	if !inPlay(f) {
		return ""
	}
	typ := espn.Map(espn.Status(f.Data)["type"])
	home, away := espn.Teams(espn.Competition(f.Data))
	if home == nil || away == nil {
		return ""
	}
	switch sport {
	case "nfl", "cfb":
		if len(f.Plays) == 0 {
			return ""
		}
		last := f.Plays[len(f.Plays)-1]
		id := espn.Str(espn.Map(espn.Map(last["end"])["team"])["id"])
		if id == "" {
			id = espn.Str(espn.Map(espn.Map(last["start"])["team"])["id"])
		}
		switch id {
		case "":
			return ""
		case espn.Str(away["id"]):
			return "away"
		case espn.Str(home["id"]):
			return "home"
		}
	case "mlb":
		detail := strings.ToLower(espn.Str(typ["shortDetail"]))
		switch {
		case strings.HasPrefix(detail, "top"):
			return "away"
		case strings.HasPrefix(detail, "bot"):
			return "home"
		}
	}
	return ""
}

// inPlay is true while the game is being played: not before the start, at
// halftime or after the end.
func inPlay(f *Feed) bool {
	typ := espn.Map(espn.Status(f.Data)["type"])
	return !f.Finished && espn.Str(typ["state"]) == "in" && espn.Str(typ["name"]) != "STATUS_HALFTIME"
}

// situation is the football down and distance after the last play.
func situation(sport string, f *Feed) string {
	if (sport != "nfl" && sport != "cfb") || !inPlay(f) || len(f.Plays) == 0 {
		return ""
	}
	last := f.Plays[len(f.Plays)-1]
	end := espn.Map(last["end"])
	if text := espn.Str(end["downDistanceText"]); text != "" {
		return text
	}
	if text := espn.Str(end["shortDownDistanceText"]); text != "" {
		return text
	}
	// A timeout changes nothing on the field and has no "after" of its own,
	// so the situation is the one it started from. Other plays without one
	// (a kickoff, a score) really have none, and the header shows nothing.
	if strings.Contains(strings.ToLower(espn.Str(espn.Map(last["type"])["text"])), "timeout") {
		start := espn.Map(last["start"])
		if text := espn.Str(start["downDistanceText"]); text != "" {
			return text
		}
		return espn.Str(start["shortDownDistanceText"])
	}
	return ""
}
