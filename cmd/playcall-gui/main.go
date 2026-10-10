// Command playcall-gui follows one live ESPN game in a window: a header with
// the game's state, and the plays as they happen, newest first.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"playcall/internal/core"
	"playcall/internal/sports"
)

func main() {
	os.Exit(run())
}

func run() int {
	sport := flag.String("sport", sports.DefaultSport, "sport: nfl, nba, mlb, nhl, cfb, cbb or wnba")
	game := flag.String("game", "", "ESPN game id (use `playcall list` to find one)")
	mock := flag.String("mock", "", "replay a fixture file as if it were live, instead of ESPN")
	step := flag.Int("step", 3, "with --mock: plays revealed per poll")
	interval := flag.Duration("interval", 0, "poll interval (default 10s; 1s with --mock; never below 5s live)")
	flag.Parse()

	if err := sports.Validate(*sport); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var fetch fetchFunc
	every := *interval
	if *mock != "" {
		var err error
		if fetch, err = mockFetcher(*mock, *step); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if *game == "" {
			*game = "999"
		}
		if every == 0 {
			every = time.Second
		}
	} else {
		if *game == "" {
			fmt.Fprintln(os.Stderr, "need --game ID (or --mock FILE)")
			return 2
		}
		fetch = liveFetcher(*sport, *game)
		if every == 0 {
			every = 10 * time.Second
		}
		every = max(every, 5*time.Second)
	}

	a := app.New() // before any widget: they read the theme from it
	win := newUI(every)
	sink := guiSink{win}
	state := &core.State{GameID: *game, Sport: *sport, StartedAt: float64(time.Now().UnixNano()) / 1e9}
	poller, err := core.NewPoller(state, sink, fetch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	poller.StatusSink = sink

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := a.NewWindow(fmt.Sprintf("playcall - %s %s", *sport, *game))
	w.SetContent(win.content)
	w.Resize(fyne.NewSize(480, 700))
	w.SetOnClosed(cancel)

	a.Lifecycle().SetOnStarted(func() {
		go func() {
			loop := &core.Loop{Poll: poller.Poll, GameID: *game, Interval: every}
			if err := loop.Run(ctx); err != nil {
				slog.Error("poll loop ended", "err", err)
			}
		}()
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					fyne.Do(win.refreshStale)
				}
			}
		}()
	})
	w.ShowAndRun()
	return 0
}
