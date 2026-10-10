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
	"strings"
	"sync/atomic"
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
	debug := flag.Bool("debug", false, "log at DEBUG level this run, whatever the settings say")
	flag.Parse()

	if err := sports.Validate(*sport); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	if paths, err := core.DefaultPaths(); err == nil {
		if closeLog, err := core.SetupLogging(paths, os.Stderr, level); err == nil {
			defer closeLog()
		}
	}

	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &shell{root: root, fetcher: &espn.Client{}, interval: *interval, forceDebug: *debug}
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
	changes := make(chan fyne.Settings, 1)
	s.app.Settings().AddChangeListener(changes)
	go func() {
		for {
			select {
			case <-root.Done():
				return
			case <-changes:
				fyne.Do(s.themeChanged)
			}
		}
	}()
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
	// forceDebug is --debug: log at DEBUG whatever the config says.
	forceDebug bool

	stopGame context.CancelFunc // ends the current game's polling

	highlight  highlight // color of scoring plays and turnovers
	pickerView *picker   // the picker last shown, kept while settings are open
	settings   *settingsView
	gameView   *ui // the game being followed, nil in the picker
	// gameShown is true while the game view is the screen on display (not
	// the settings opened over it). Read by the poll goroutine.
	gameShown atomic.Bool
	logos     *logoCache // team logos on disk; nil if the cache folder is unknown
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
	s.applyLogLevel()
}

// applyLogLevel sets the log level from the config; --debug wins.
func (s *shell) applyLogLevel() {
	if s.forceDebug {
		core.SetLogLevel(slog.LevelDebug)
		return
	}
	core.SetLogLevel(core.ParseLogLevel(s.cfg.LogLevel()))
}

// update applies a change to the config in memory and saves it to the file.
// The file is read again first, so a change made elsewhere (the CLI's
// addfavorite, a hand edit) is kept.
func (s *shell) update(fn func(*core.Config)) {
	fn(&s.cfg)
	s.highlight = highlightByKey(s.cfg.HighlightColor())
	s.applyLogLevel()
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
		s.followPicked(core.LastGame{Sport: sport, ID: g.ID, Label: matchup(g)})
	})
	if last, ok := s.cfg.LastGame(); ok {
		p.setContinue(fmt.Sprintf("%s (%s)", last.Label, strings.ToUpper(last.Sport)), func() { s.followPicked(last) })
	}
	p.onSettings = s.settingsFromPicker
	p.logos = s.logos
	s.pickerView = p
	s.win.SetContent(p.content)
	p.load()
}

// followPicked follows a game chosen in the picker (or the Continue button)
// and remembers it for the next launch.
func (s *shell) followPicked(g core.LastGame) {
	s.update(func(c *core.Config) { c.SetLastGame(g) })
	s.follow(g.Sport, g.ID, liveFetcher(g.Sport, g.ID), s.liveInterval(), true)
	s.win.SetTitle(fmt.Sprintf("playcall - %s: %s", sportLabel(g.Sport), g.Label))
}

// openSettings shows the settings screen in place of whatever is on screen;
// back puts that screen back. The sport is the one the favorites page opens on.
func (s *shell) openSettings(sport string, back func()) {
	v := newSettings(settingsEnv{
		ctx:     s.root,
		fetcher: s.fetcher,
		cfg:     func() core.Config { return s.cfg },
		update:  s.update,
		logos:   s.logos,
		back:    back,
	}, sport)
	s.settings = v
	s.win.SetContent(v.content)
}

// settingsFromPicker: Back returns to the same picker, which reloads in case
// the favorites or default group changed.
func (s *shell) settingsFromPicker() {
	s.openSettings(s.pickerView.currentSport(), func() {
		s.win.SetContent(s.pickerView.content)
		s.pickerView.refresh()
	})
}

// settingsFromGame: the game keeps polling while settings are open, so Back
// returns to a current view, with its rows recolored if the highlight changed.
func (s *shell) settingsFromGame(sport string, view *ui) {
	s.gameShown.Store(false)
	s.openSettings(sport, func() {
		view.setHighlight(s.highlight)
		s.win.SetContent(view.content)
		s.gameShown.Store(true)
	})
}

// gameVisible is the router's test: the game screen is up and the window is
// on a visible workspace. With settings open the game can't be seen, so its
// plays go to a toast.
func (s *shell) gameVisible(ctx context.Context) (bool, error) {
	if !s.gameShown.Load() {
		return false, nil
	}
	return windowVisible(ctx)
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

// themeChanged runs when the user switches between light and dark: the logos
// and any colors set at build time follow the new theme.
func (s *shell) themeChanged() {
	if s.pickerView != nil {
		s.pickerView.list.Refresh()
	}
	if s.settings != nil {
		s.settings.list.Refresh()
	}
	if s.gameView != nil {
		s.gameView.themeChanged()
	}
}

func (s *shell) endGame() {
	s.gameShown.Store(false)
	s.gameView = nil
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
	s.gameView = view
	view.onSettings = func() { s.settingsFromGame(sport, view) }
	win := guiSink{view}
	sink := routerSink{win: win, toast: core.ToastSink{Runner: core.ExecRunner{}}, Visible: s.gameVisible}
	state := &core.State{GameID: gameID, Sport: sport, StartedAt: float64(time.Now().UnixNano()) / 1e9}
	poller, err := core.NewPoller(state, sink, fetch)
	if err != nil {
		cancel()
		view.setError(err.Error())
		s.win.SetContent(view.content)
		return
	}
	poller.StatusSink = win

	s.win.SetTitle(fmt.Sprintf("playcall - %s %s", sportLabel(sport), gameID))
	s.win.SetContent(view.content)
	s.gameShown.Store(true)

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
