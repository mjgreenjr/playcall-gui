package core

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"playcall/internal/espn"
)

func TestLogTimeouts(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	typ := func(s string) map[string]any { return map[string]any{"text": s} }
	td := espn.Data{"id": "1", "text": "TD pass", "type": typ("Passing Touchdown"), "scoringPlay": true}
	to := espn.Data{"id": "2", "text": "Timeout Nebraska, clock 11:51", "type": typ("Timeout"),
		"period": map[string]any{"number": json.Number("4")}, "clock": map[string]any{"displayValue": "11:51"}}
	home := espn.Data{"homeAway": "home", "team": map[string]any{"abbreviation": "NEB"}, "timeoutsUsed": json.Number("3")}
	away := espn.Data{"homeAway": "away", "team": map[string]any{"abbreviation": "IU"}, "timeoutsUsed": json.Number("3")}
	data := espn.Data{"header": espn.Data{"competitions": []any{espn.Data{"competitors": []any{home, away}}}}}
	feed := &Feed{Data: data, Plays: []espn.Data{td}, Period: "4th", Clock: "11:51"}
	p := &Poller{State: &State{GameID: "401858481"}}

	p.logTimeouts(feed)
	if got := buf.String(); strings.Count(got, "Timeout counters") != 1 || !strings.Contains(got, "home_used=3") || strings.Contains(got, "Timeout play") {
		t.Fatalf("first poll: %q", got)
	}

	// Nothing changed: nothing is logged.
	buf.Reset()
	p.logTimeouts(feed)
	if buf.Len() != 0 {
		t.Errorf("unchanged poll logged %q", buf.String())
	}

	// A timeout play after the touchdown and a counter that goes up.
	buf.Reset()
	home["timeoutsUsed"] = json.Number("4")
	feed.Plays = []espn.Data{td, to}
	p.logTimeouts(feed)
	got := buf.String()
	for _, want := range []string{"Timeout counters", "home_used=4", "last_type=Timeout", "Timeout play", "backfill=false",
		"prev_type=\"Passing Touchdown\"", "prev_scoring=true", "after_timeout=false"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %q", want, got)
		}
	}

	// The same timeout is not logged twice.
	buf.Reset()
	p.logTimeouts(feed)
	if buf.Len() != 0 {
		t.Errorf("repeat poll logged %q", buf.String())
	}

	// A run that starts with a timeout already in the feed marks it backfill.
	buf.Reset()
	q := &Poller{State: &State{GameID: "2"}}
	q.logTimeouts(feed)
	if !strings.Contains(buf.String(), "backfill=true") {
		t.Errorf("first poll should backfill: %q", buf.String())
	}

	// No counters, nothing to log about them.
	buf.Reset()
	delete(home, "timeoutsUsed")
	delete(away, "timeoutsUsed")
	r := &Poller{State: &State{GameID: "3"}}
	r.logTimeouts(&Feed{Data: data})
	if strings.Contains(buf.String(), "Timeout counters") {
		t.Errorf("logged counters that are not there: %q", buf.String())
	}
}
