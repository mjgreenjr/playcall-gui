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

func TestByeGames(t *testing.T) {
	teams, err := espn.FetchStandingTeams(context.Background(), staticFetcher{`{"children":[{"standings":{"entries":[
		{"team":{"id":"1","abbreviation":"BYU"},"stats":[{"type":"total","summary":"5-0"},{"type":"vsconf","summary":"3-0"}]},
		{"team":{"id":"2","abbreviation":"ARIZ"},"stats":[{"type":"total","summary":"4-1"}]},
		{"team":{"id":"3","abbreviation":"ISU"},"stats":[]},
		{"team":{"id":"3","abbreviation":"ISU"},"stats":[]}]}}]}`}, "cfb", "4")
	if err != nil || len(teams) != 3 {
		t.Fatalf("teams %v, err %v", teams, err)
	}
	games := []espn.Game{{AwayID: "9", HomeID: "1", Season: "2"}}

	byes := ByeGames(games, teams)
	if len(byes) != 2 || byes[0].Away != "ARIZ" || byes[1].Away != "ISU" {
		t.Fatalf("byes: %+v", byes)
	}
	if byes[0].State != ByeState || byes[0].AwayRecord != "4-1" || byes[0].Status != "Bye" {
		t.Errorf("bye game: %+v", byes[0])
	}

	// BYU's conference record comes through when it has the bye.
	if b := ByeGames([]espn.Game{{AwayID: "9", HomeID: "8", Season: "2"}}, teams); len(b) != 3 || b[1].AwayConf != "3-0" {
		t.Errorf("all byes: %+v", b)
	}

	// No byes in the postseason, or when there are no games at all.
	if b := ByeGames([]espn.Game{{AwayID: "9", HomeID: "1", Season: "3"}}, teams); b != nil {
		t.Errorf("postseason: %+v", b)
	}
	if b := ByeGames(nil, teams); b != nil {
		t.Errorf("no games: %+v", b)
	}
}
