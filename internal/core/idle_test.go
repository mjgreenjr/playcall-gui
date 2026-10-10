package core

import (
	"context"
	"testing"

	"playcall/internal/espn"
)

type staticFetcher struct{ data string }

func (f staticFetcher) Fetch(context.Context, string) (espn.Data, error) {
	return espn.Decode([]byte(f.data))
}

func TestIdleGames(t *testing.T) {
	teams, err := espn.FetchStandingTeams(context.Background(), staticFetcher{`{"children":[{"standings":{"entries":[
		{"team":{"id":"1","abbreviation":"BYU"},"stats":[{"type":"total","summary":"5-0"},{"type":"vsconf","summary":"3-0"}]},
		{"team":{"id":"2","abbreviation":"ARIZ"},"stats":[{"type":"total","summary":"4-1"}]},
		{"team":{"id":"3","abbreviation":"ISU"},"stats":[]},
		{"team":{"id":"3","abbreviation":"ISU"},"stats":[]}]}}]}`}, "cfb", "4")
	if err != nil || len(teams) != 3 {
		t.Fatalf("teams %v, err %v", teams, err)
	}
	games := []espn.Game{{AwayID: "9", HomeID: "1", Season: "2"}}

	byes := IdleGames("cfb", games, teams)
	if len(byes) != 2 || byes[0].Away != "ARIZ" || byes[1].Away != "ISU" {
		t.Fatalf("byes: %+v", byes)
	}
	if byes[0].State != IdleState || byes[0].AwayRecord != "4-1" || byes[0].Status != "Bye" {
		t.Errorf("bye game: %+v", byes[0])
	}

	// BYU's conference record comes through when it has the bye.
	if b := IdleGames("cfb", []espn.Game{{AwayID: "9", HomeID: "8", Season: "2"}}, teams); len(b) != 3 || b[1].AwayConf != "3-0" {
		t.Errorf("all byes: %+v", b)
	}

	// No byes in the postseason, or when there are no games at all.
	if b := IdleGames("cfb", []espn.Game{{AwayID: "9", HomeID: "1", Season: "3"}}, teams); b != nil {
		t.Errorf("postseason: %+v", b)
	}
	if b := IdleGames("cfb", nil, teams); b != nil {
		t.Errorf("no games: %+v", b)
	}

	// The daily sports list their idle teams too, as "No game", in the
	// preseason as well; the weekly ones do not list a preseason bye.
	pre := []espn.Game{{AwayID: "9", HomeID: "1", Season: "1"}}
	if b := IdleGames("nba", pre, teams); len(b) != 2 || b[0].Status != "No game" || b[0].Season != "1" {
		t.Errorf("nba preseason: %+v", b)
	}
	if b := IdleGames("nfl", pre, teams); b != nil {
		t.Errorf("nfl preseason: %+v", b)
	}
	if b := IdleGames("mlb", []espn.Game{{AwayID: "9", HomeID: "1", Season: "3"}}, teams); b != nil {
		t.Errorf("mlb postseason: %+v", b)
	}
}
