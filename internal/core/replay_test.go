package core

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"playcall/internal/espn"
)

// The replay tests port tools/golden.py: a fixture is replayed as a live game
// through the real poller, and the transcript must equal the Python's.

const (
	fixtureDir = "../../test/fixtures"
	goldenDir  = "../../test/golden"
)

func loadFixture(t *testing.T, name string) espn.Data {
	t.Helper()
	d, err := espn.ReadMock(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func deepCopy(t *testing.T, d espn.Data) espn.Data {
	t.Helper()
	c, err := espn.Copy(d)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// reveal is a copy of d with only the first k plays, and not final unless finalNow.
func reveal(t *testing.T, d espn.Data, k int, finalNow bool) espn.Data {
	t.Helper()
	c, err := espn.Reveal(d, k, finalNow)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type replayPoll struct {
	label string
	feed  espn.Data
}

func chunked(t *testing.T, d espn.Data, chunk int) []replayPoll {
	n := len(espn.ExtractPlays(d))
	var counts []int
	for k := chunk; k < n; k += chunk {
		counts = append(counts, k)
	}
	counts = append(counts, n)
	var polls []replayPoll
	for _, k := range counts {
		polls = append(polls, replayPoll{fmt.Sprintf("revealed %d/%d", k, n), reveal(t, d, k, k == n)})
	}
	return polls
}

// updatePolls: the full feed, then with the first scoring play's text
// extended, then unchanged. Nil when there is no scoring play.
func updatePolls(t *testing.T, d espn.Data) []replayPoll {
	n := len(espn.ExtractPlays(d))
	full := reveal(t, d, n, false)
	changed := deepCopy(t, full)
	for _, h := range espn.PlayHolders(changed) {
		for _, v := range h["plays"].([]any) {
			p := espn.Map(v)
			if espn.Truthy(p["scoringPlay"]) {
				p["text"] = espn.Str(p["text"]) + " (extra point is good)"
				return []replayPoll{
					{fmt.Sprintf("revealed %d/%d", n, n), full},
					{fmt.Sprintf("revealed %d/%d, scoring play %s text changed", n, n, espn.PlayKey(p)), changed},
					{fmt.Sprintf("revealed %d/%d, text unchanged", n, n), changed},
				}
			}
		}
	}
	return nil
}

func replay(t *testing.T, sport string, polls []replayPoll) string {
	var buf bytes.Buffer
	state := &State{GameID: "999", Sport: sport}
	var feed espn.Data
	poller, err := NewPoller(state, StdoutSink{W: &buf}, func(context.Context) (espn.Data, error) { return feed, nil })
	if err != nil {
		t.Fatal(err)
	}
	poller.Trace = &buf
	var out []string
	for i, p := range polls {
		feed = p.feed
		buf.Reset()
		res, err := poller.Poll(context.Background())
		if err != nil {
			t.Fatalf("poll %d: %v", i+1, err)
		}
		last := "None"
		if state.LastPlayID != nil {
			last = fmt.Sprint(int64(*state.LastPlayID))
		}
		out = append(out,
			fmt.Sprintf("== poll %d: %s ==", i+1, p.label),
			strings.TrimRight(buf.String(), "\n"),
			fmt.Sprintf("-> returned %s; last_play_id=%s sent_ids=%d scoring_texts=%d",
				res.Status, last, len(state.SentIDs), len(state.ScoringTexts)))
	}
	return strings.Join(out, "\n") + "\n"
}

func TestReplayGoldens(t *testing.T) {
	cases := []struct {
		name, sport, fixture string
		update               bool
	}{
		{"nfl_multi", "nfl", "playbyplay_multi_plays.json", true},
		{"nfl_final", "nfl", "playbyplay_final.json", false},
		{"cfb", "cfb", "playbyplay_cfb_multi_plays.json", true},
		{"nba", "nba", "playbyplay_nba_multi_plays.json", true},
		{"wnba", "wnba", "playbyplay_wnba_multi_plays.json", true},
		{"nhl", "nhl", "playbyplay_nhl_multi_plays.json", true},
		{"mlb", "mlb", "playbyplay_mlb_multi_plays.json", true},
	}
	for _, c := range cases {
		data := loadFixture(t, c.fixture)
		runs := map[string][]replayPoll{
			"c3":  chunked(t, data, 3),
			"c25": chunked(t, data, 25),
		}
		if c.update {
			up := updatePolls(t, data)
			if up == nil {
				t.Fatalf("%s: no scoring play for the update scenario", c.name)
			}
			runs["update"] = up
		}
		for suffix, polls := range runs {
			t.Run(c.name+"_"+suffix, func(t *testing.T) {
				want := readGolden(t, fmt.Sprintf("replay_%s_%s.txt", c.name, suffix))
				if got := replay(t, c.sport, polls); got != want {
					t.Errorf("transcript differs from golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
				}
			})
		}
	}
}
