package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"playcall/internal/espn"
)

// event builds a scoreboard event; teams is "away home".
func event(id, date, away, home, status string) string {
	return fmt.Sprintf(`{"id":%q,"date":%q,"status":{"type":{"shortDetail":%q,"state":"pre"}},
		"competitions":[{"competitors":[
			{"homeAway":"home","team":{"abbreviation":%q},"score":"0"},
			{"homeAway":"away","team":{"abbreviation":%q},"score":"0"}]}]}`, id, date, status, home, away)
}

func scoreboard(events ...string) string {
	return `{"events":[` + strings.Join(events, ",") + `]}`
}

func listLines(t *testing.T, board string, cfg string, args ...string) []string {
	t.Helper()
	h := newHarness(t)
	h.src.data[espn.ScoreboardURL("nfl", "", "")] = board
	if cfg != "" {
		writeConfig(t, h, cfg)
	}
	if code := h.run(t, args...); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	return strings.Split(strings.TrimRight(h.stdout.String(), "\n"), "\n")[1:] // drop the header
}

func TestListWidthIsRuneCount(t *testing.T) {
	board := scoreboard(
		event("1", "2026-10-08T00:00Z", "TA&M", "LSU", "Final"),
		event("2", "2026-10-09T00:00Z", "ÉA", "UT", "Final"),
	)
	got := listLines(t, board, "", "list")
	want := []string{
		"1  TA&M @ LSU   Final  0-0",
		"2  ÉA   @ UT    Final  0-0", // É is one rune, so three pad spaces
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestListFavoritesTwoLetterAbbr(t *testing.T) {
	// GB is padded to "GB  " on this board; the filter must strip the padding.
	board := scoreboard(
		event("1", "2026-10-08T00:00Z", "CHI", "GB", "Final"),
		event("2", "2026-10-08T01:00Z", "BUF", "NYJ", "Final"),
		event("3", "2026-10-08T02:00Z", "MIA", "NE", "Final"),
	)
	cfg := `{"favorite_teams":{"nfl":["GB","NE"]},"interval":10}`
	got := listLines(t, board, cfg, "list")
	want := []string{"1  CHI @ GB   Final  0-0", "3  MIA @ NE   Final  0-0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	// --all ignores favorites, and the width is the same.
	if all := listLines(t, board, cfg, "list", "--all"); len(all) != 3 {
		t.Errorf("--all: %q", all)
	}
}

func TestListSortIsStable(t *testing.T) {
	board := scoreboard(
		event("3", "2026-10-09T00:00Z", "AAA", "BBB", "Final"),
		event("1", "2026-10-08T00:00Z", "CCC", "DDD", "Final"),
		event("2", "2026-10-09T00:00Z", "EEE", "FFF", "Final"),
		event("4", "2026-10-09T00:00Z", "GGG", "HHH", "Final"),
		event("0", "2026-10-08T00:00Z", "III", "JJJ", "Final"),
	)
	var ids []string
	for _, l := range listLines(t, board, "", "list") {
		ids = append(ids, strings.Fields(l)[0])
	}
	// By date; equal dates keep the feed order.
	if want := []string{"1", "0", "3", "2", "4"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order %v, want %v", ids, want)
	}
}

func TestTeamsSortAndPad(t *testing.T) {
	h := newHarness(t)
	h.src.data[espn.TeamsURL("nfl")] = `{"sports":[{"leagues":[{"teams":[
		{"team":{"abbreviation":"B","displayName":"Beta"}},
		{"team":{"abbreviation":"TA&M","displayName":"Alpha"}},
		{"team":{"abbreviation":"Z","displayName":"Beta"}},
		{"team":{"abbreviation":"B","displayName":"Beta"}}]}]}]}`
	if code := h.run(t, "teams"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "NFL\nTA&M  Alpha\nB     Beta\nZ     Beta\n"
	if got := h.stdout.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTestNotifyDryRun(t *testing.T) {
	h := newHarness(t)
	if code := h.run(t, "test-notify", "--dry-run"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "[DRY-RUN] Test\nplaycall is working\nExec: xdg-open https://www.espn.com/nfl/game/_/gameId/999999999\n"
	if got := h.stdout.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(h.runner.calls) != 0 {
		t.Errorf("ran %v", h.runner.calls)
	}
}

func TestTestNotifyToast(t *testing.T) {
	h := newHarness(t)
	if code := h.run(t, "test-notify"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := [][]string{{"omarchy-notification-send", "-u", "low", "-t", "15000", "Test", "playcall is working",
		"--exec", "xdg-open", "https://www.espn.com/nfl/game/_/gameId/999999999"}}
	if !reflect.DeepEqual(h.runner.calls, want) {
		t.Errorf("ran %q, want %q", h.runner.calls, want)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout %q", h.stdout.String())
	}
}

func TestTestNotifyFailureStillExitsZero(t *testing.T) {
	h := newHarness(t)
	h.runner.err = fmt.Errorf("executable file not found")
	if code := h.run(t, "test-notify"); code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if want := []string{"ERROR Failed to send notification: executable file not found"}; !reflect.DeepEqual(h.logs, want) {
		t.Errorf("log %q, want %q", h.logs, want)
	}
}

func TestFollowOnceSendsToasts(t *testing.T) {
	h := newHarness(t)
	code := h.run(t, "follow", "999", "--once", "--mock", fixtures+"playbyplay_multi_plays.json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	if len(h.runner.calls) != 1 || h.runner.calls[0][0] != "omarchy-notification-send" {
		t.Fatalf("ran %q", h.runner.calls)
	}
	args := h.runner.calls[0]
	if n := len(args); args[n-3] != "--exec" || args[n-2] != "xdg-open" {
		t.Errorf("--exec is not last: %q", args)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("toast run printed %q", h.stdout.String())
	}
}

func TestFollowOnceToastFailureLogs(t *testing.T) {
	h := newHarness(t)
	h.runner.err = fmt.Errorf("boom")
	h.run(t, "follow", "999", "--once", "--mock", fixtures+"playbyplay_multi_plays.json")
	// The poller then logs that the event was not delivered.
	if len(h.logs) == 0 || h.logs[0] != "ERROR Failed to send notification: boom" {
		t.Errorf("log %q", h.logs)
	}
}

func TestAddFavoriteAlreadyDoesNotSave(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, `{"favorite_teams":{"nfl":["BUF"]},"interval":10,"extra":{"a":1}}`)
	if code := h.run(t, "addfavorite", "buf"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := h.stdout.String(); got != "BUF is already a nfl favorite.\n" {
		t.Errorf("stdout %q", got)
	}
	if got := readConfig(t, h); !strings.Contains(got, `"extra":{"a":1}`) {
		t.Errorf("config was rewritten: %s", got)
	}
}

func TestAddFavoriteKeepsUnknownKeys(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, `{"favorite_teams":{"nfl":[]},"interval":10,"extra":{"a":1}}`)
	if code := h.run(t, "addfavorite", "buf"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := readConfig(t, h); !strings.Contains(got, `"extra"`) || !strings.Contains(got, `"BUF"`) {
		t.Errorf("config: %s", got)
	}
}
