package main

import (
	"context"
	"testing"

	"playcall/internal/espn"
)

func TestMockFetcherRevealsGradually(t *testing.T) {
	fetch, err := mockFetcher("../../test/fixtures/playbyplay_nhl_multi_plays.json", 50)
	if err != nil {
		t.Fatal(err)
	}
	var counts []int
	var last espn.Data
	for i := 0; i < 100; i++ {
		last, err = fetch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		counts = append(counts, len(espn.ExtractPlays(last)))
		if counts[len(counts)-1] == counts[max(0, len(counts)-2)] && i > 0 {
			break
		}
	}
	if counts[0] != 50 || counts[1] != 100 {
		t.Errorf("counts %v", counts)
	}
	if n := len(espn.ExtractPlays(last)); n == 0 {
		t.Error("no plays at the end")
	}
}

func TestMockFetcherFinalOnlyAtTheEnd(t *testing.T) {
	fetch, err := mockFetcher("../../test/fixtures/playbyplay_mlb_multi_plays.json", 200)
	if err != nil {
		t.Fatal(err)
	}
	var finals []bool
	for i := 0; i < 4; i++ {
		d, _ := fetch(context.Background())
		finals = append(finals, espn.IsFinal(d))
	}
	if finals[0] || finals[1] || !finals[2] || !finals[3] {
		t.Errorf("finals %v", finals)
	}
}

func TestMockFetcherMissingFile(t *testing.T) {
	if _, err := mockFetcher("nope.json", 3); err == nil {
		t.Fatal("expected an error")
	}
}
