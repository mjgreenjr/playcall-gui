// Command playcall-gui follows one live ESPN game in a window: a header with
// the game's state, and the plays as they happen, newest first. Without
// --game or --mock it opens on a list of today's games to pick from.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
)

func main() {
	os.Exit(run())
}

func run() int {
	sport := flag.String("sport", sports.DefaultSport, "sport: nfl, nba, mlb, nhl, cfb, cbb or wnba")
	game := flag.String("game", "", "ESPN game id; without it (and --mock) a game picker opens")
	mock := flag.String("mock", "", "replay a fixture file as if it were live, instead of ESPN")
	step := flag.Int("step", 3, "with --mock: plays revealed per poll")
	interval := flag.Duration("interval", 0, "poll interval (default: the config's; 1s with --mock; never below 5s live)")
	flag.Parse()

	if err := sports.Validate(*sport); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &shell{root: root, fetcher: &espn.Client{}, interval: *interval}
	s.loadConfig()

	var start func()
	switch {
	case *mock != "":
		fetch, err := mockFetcher(*mock, *step)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		id := *game
		if id == "" {
			id = "999"
		}
		every := *interval
		if every == 0 {
			every = time.Second
		}
		start = func() { s.follow(*sport, id, fetch, every, false) }
	case *game != "":
		start = func() { s.follow(*sport, *game, liveFetcher(*sport, *game), s.liveInterval(), false) }
	default:
		start = func() { s.showPicker(*sport) }
	}

	s.app = app.New() // before any widget: they read the theme from it
	if paths, err := core.DefaultPaths(); err == nil {
		s.logos = newLogoCache(filepath.Join(paths.CacheDir, "logos"), func(f func()) { fyne.Do(f) })
	}
	s.win = s.app.NewWindow("playcall")
	s.win.Resize(fyne.NewSize(480, 700))
	s.win.SetOnClosed(cancel)
	s.app.Lifecycle().SetOnStarted(func() { fyne.Do(start) })
	s.win.ShowAndRun()
	return 0
}

// shell owns the window and switches it between the picker and one game.
type shell struct {
	app  fyne.App
	win  fyne.Window
	root context.Context

	fetcher  espn.Fetcher
	cfg      core.Config
	interval time.Duration // --interval, 0 if not given

	stopGame context.CancelFunc // ends the current game's polling

	highlight  highlight // color of scoring plays and turnovers
	pickerView *picker   // the picker last shown, kept while settings are open
	settings   *settingsView
	logos      *logoCache // team logos on disk; nil if the cache folder is unknown
}

func (s *shell) loadConfig() {
	s.cfg = core.DefaultConfig()
	paths, err := core.DefaultPaths()
	if err == nil {
		var cfg core.Config
		if cfg, err = core.LoadConfig(paths); err == nil {
			s.cfg = cfg
		}
	}
	if err != nil {
		slog.Warn("Using the default config", "err", err)
	}
	if s.cfg.FavoriteTeams == nil {
		s.cfg.FavoriteTeams = map[string][]string{} // shared with the picker, so edits show up there
	}
	s.highlight = highlightByKey(s.cfg.HighlightColor())
}

// update applies a change to the config in memory and saves it to the file.
// The file is read again first, so a change made elsewhere (the CLI's
// addfavorite, a hand edit) is kept.
func (s *shell) update(fn func(*core.Config)) {
	fn(&s.cfg)
	s.highlight = highlightByKey(s.cfg.HighlightColor())
	paths, err := core.DefaultPaths()
	if err == nil {
		err = core.UpdateConfig(paths, fn)
	}
	if err != nil {
		slog.Warn("Could not save the config", "err", err)
	}
}

// liveInterval is --interval if given, else the config's, never below the minimum.
func (s *shell) liveInterval() time.Duration {
	var flagSecs *int
	if s.interval > 0 {
		n := int(s.interval / time.Second)
		flagSecs = &n
	}
	return time.Duration(core.ResolveInterval(flagSecs, s.cfg)) * time.Second
}

func (s *shell) showPicker(sport string) {
	s.endGame()
	s.win.SetTitle("playcall")
	p := newPicker(s.root, s.fetcher, s.cfg.FavoriteTeams, s.groupPrefs(), sport, func(sport string, g espn.Game) {
		s.follow(sport, g.ID, liveFetcher(sport, g.ID), s.liveInterval(), true)
		s.win.SetTitle(fmt.Sprintf("playcall - %s: %s", sportLabel(sport), matchup(g)))
	})
	p.onSettings = s.showSettings
	p.logos = s.logos
	s.pickerView = p
	s.win.SetContent(p.content)
	p.load()
}

// showSettings opens the settings screen over the picker. Back returns to the
// same picker, which reloads in case the favorites or default group changed.
func (s *shell) showSettings() {
	sport := s.pickerView.currentSport()
	v := newSettings(settingsEnv{
		ctx:     s.root,
		fetcher: s.fetcher,
		cfg:     func() core.Config { return s.cfg },
		update:  s.update,
		back: func() {
			s.win.SetContent(s.pickerView.content)
			s.pickerView.refresh()
		},
	}, sport)
	s.settings = v
	s.win.SetContent(v.content)
}

// groupPrefs reads and writes each sport's remembered group in config.json.
func (s *shell) groupPrefs() groupPrefs {
	return groupPrefs{
		get: func(sport string) string { return s.cfg.FavoriteGroup(sport) },
		set: func(sport, group string) {
			s.update(func(c *core.Config) { c.SetFavoriteGroup(sport, group) })
		},
	}
}

func (s *shell) endGame() {
	if s.stopGame != nil {
		s.stopGame()
		s.stopGame = nil
	}
}

// follow polls one game into a fresh game view. back adds a button that
// returns to the picker.
func (s *shell) follow(sport, gameID string, fetch fetchFunc, every time.Duration, back bool) {
	s.endGame()
	ctx, cancel := context.WithCancel(s.root)
	s.stopGame = cancel

	var onBack func()
	if back {
		onBack = func() { s.showPicker(sport) }
	}
	view := newUI(every, onBack, s.highlight, s.logos)
	sink := guiSink{view}
	state := &core.State{GameID: gameID, Sport: sport, StartedAt: float64(time.Now().UnixNano()) / 1e9}
	poller, err := core.NewPoller(state, sink, fetch)
	if err != nil {
		cancel()
		view.setError(err.Error())
		s.win.SetContent(view.content)
		return
	}
	poller.StatusSink = sink

	s.win.SetTitle(fmt.Sprintf("playcall - %s %s", sportLabel(sport), gameID))
	s.win.SetContent(view.content)

	go func() {
		loop := &core.Loop{Poll: poller.Poll, GameID: gameID, Interval: every}
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
				fyne.Do(view.refreshStale)
			}
		}
	}()
}
