// Command playcall follows live ESPN games. This package only parses flags
// and prints; the game logic is in internal/core.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
	"playcall/internal/version"
)

func main() {
	d := deps{
		Source: httpSource{},
		Now:    time.Now,
		Paths:  core.DefaultPaths,
		Runner: core.ExecRunner{},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		SetupLog: func(p core.Paths, stdout io.Writer) (func() error, error) {
			return core.SetupLogging(p, stdout, slog.LevelInfo)
		},
	}
	// SIGINT and SIGTERM stop a running loop cleanly; the state is kept.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, d, os.Args[1:])
	stop()
	os.Exit(code)
}

// deps is everything run needs from the outside world, so the commands can be
// tested with no network, home directory, clock or notification program.
type deps struct {
	Source   source
	Now      func() time.Time
	Paths    func() (core.Paths, error)
	Runner   core.Runner
	Stdout   io.Writer
	Stderr   io.Writer
	SetupLog func(p core.Paths, stdout io.Writer) (closeLog func() error, err error)
	// Sleep waits between polls; nil means a real timer. Tests inject one.
	Sleep func(ctx context.Context, d time.Duration) error
}

// source reads ESPN data: from the network, or from a --mock file.
type source interface {
	espn.Fetcher
	ReadMock(path string) (espn.Data, error)
}

// httpSource is the real source.
type httpSource struct{ client espn.Client }

func (s httpSource) Fetch(ctx context.Context, url string) (espn.Data, error) {
	return s.client.Fetch(ctx, url)
}

func (httpSource) ReadMock(path string) (espn.Data, error) { return espn.ReadMock(path) }

// Exit codes, as in the Python (argparse errors exit 2).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

var commands = []string{"list", "teams", "follow", "stop", "resume", "addfavorite", "test-notify"}

// options are the parsed flags. The flags work before or after the
// subcommand, and the positional argument can sit between them.
type options struct {
	dryRun, once, all bool
	interval          *int
	mock, group       string
	out               string
	sport             string
	given             map[string]bool
}

const usageText = `usage: playcall [--dry-run] [--once] [--interval N] [--mock FILE] [--sport S] COMMAND ...
       playcall [--out FILE] [--interval N] [--sport S] follow GAME_ID|TEAM
       playcall --version

commands:
  list [--all] [--group G]   show today's games
  teams [--group G]          list team names and abbreviations
  follow GAME_ID|TEAM        follow a specific game: with --once one poll, otherwise
                             keep going and print the plays and header changes
                             (to stdout, or appended to --out FILE) until it ends
  stop                       stop following a game
  resume                     resume following from state
  addfavorite TEAM           add a favorite team for a sport
  test-notify                send a test notification
`

// parse splits args into options, the subcommand and its positional
// arguments. Go's flag package stops at the first positional, so parsing
// resumes after each one.
func parse(args []string) (opts options, positional []string, err error) {
	fs := flag.NewFlagSet("playcall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var interval int
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print notifications instead of sending")
	fs.BoolVar(&opts.once, "once", false, "perform a single poll cycle then exit")
	fs.IntVar(&interval, "interval", 0, "poll interval in seconds")
	fs.StringVar(&opts.mock, "mock", "", "mock JSON file for play-by-play")
	fs.StringVar(&opts.sport, "sport", sports.DefaultSport, "sport to follow")
	fs.StringVar(&opts.group, "group", "", "conference or division")
	fs.StringVar(&opts.out, "out", "", "file to append the followed game's feed to")
	fs.BoolVar(&opts.all, "all", false, "show all games")
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return opts, nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	opts.given = map[string]bool{}
	fs.Visit(func(f *flag.Flag) { opts.given[f.Name] = true })
	if opts.given["interval"] {
		opts.interval = &interval
	}
	return opts, positional, nil
}

func run(ctx context.Context, d deps, args []string) int {
	stdout, stderr := d.Stdout, d.Stderr
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(stdout, "playcall %s\n", version.Version)
		return exitOK
	}
	opts, positional, err := parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usageText)
		return exitOK
	}
	if err != nil {
		fmt.Fprint(stderr, usageText)
		fmt.Fprintf(stderr, "playcall: error: %v\n", err)
		return exitUsage
	}
	usageErr := func(format string, a ...any) int {
		fmt.Fprint(stderr, usageText)
		fmt.Fprintf(stderr, "playcall: error: "+format+"\n", a...)
		return exitUsage
	}
	if err := sports.Validate(opts.sport); err != nil {
		return usageErr("argument --sport: %v", err)
	}
	if len(positional) == 0 {
		return usageErr("a command is required")
	}
	cmd, rest := positional[0], positional[1:]
	known := false
	for _, c := range commands {
		known = known || c == cmd
	}
	if !known {
		return usageErr("invalid command %q (choose from %s)", cmd, strings.Join(commands, ", "))
	}
	for _, name := range []string{"group", "all"} {
		allowed := (cmd == "list") || (cmd == "teams" && name == "group")
		if opts.given[name] && !allowed {
			return usageErr("unrecognized argument --%s for %s", name, cmd)
		}
	}
	if opts.given["out"] && (cmd != "follow" || opts.once) {
		return usageErr("argument --out only works with follow, without --once")
	}
	want := 0
	if cmd == "follow" || cmd == "addfavorite" {
		want = 1
	}
	if len(rest) != want {
		return usageErr("%s takes %d positional argument(s), got %d", cmd, want, len(rest))
	}

	switch cmd {
	case "stop":
		return notImplemented(stderr, cmd)
	}

	// Like the Python, everything logs to the log file and stdout from here.
	// A group that does not exist is reported first, without touching either.
	var group *sports.Group
	if (cmd == "list" || cmd == "teams") && opts.given["group"] {
		g, err := sports.ResolveGroup(opts.sport, opts.group)
		if err != nil {
			fmt.Fprintf(stderr, "playcall: %v\n", err)
			return exitError
		}
		group = &g
	}
	paths, err := d.Paths()
	if err != nil {
		fmt.Fprintf(stderr, "playcall: %v\n", err)
		return exitError
	}
	// A continuous follow prints the feed on stdout, so the log stays in its file.
	logOut := stdout
	if cmd == "follow" && !opts.once {
		logOut = nil
	}
	closeLog, err := d.SetupLog(paths, logOut)
	if err != nil {
		fmt.Fprintf(stderr, "playcall: %v\n", err)
		return exitError
	}
	defer closeLog()

	c := &command{deps: d, opts: opts, paths: paths}
	switch cmd {
	case "list":
		return c.list(ctx, group)
	case "teams":
		return c.teams(ctx, group)
	case "follow":
		if !opts.once {
			return c.followFeed(ctx, rest[0])
		}
		return c.follow(ctx, rest[0])
	case "resume":
		return c.resume(ctx)
	case "addfavorite":
		return c.addFavorite(strings.ToUpper(rest[0]))
	}
	return c.testNotify(ctx)
}

func notImplemented(stderr io.Writer, what string) int {
	fmt.Fprintf(stderr, "playcall: %s: not implemented yet\n", what)
	return exitUsage
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// command is one invocation: the dependencies, the flags and the file paths.
type command struct {
	deps
	opts  options
	paths core.Paths
}

// fail logs err at ERROR, as the Python does before exiting 1.
func fail(err error) int {
	slog.Error(err.Error())
	return exitError
}

func (c *command) errorf(format string, a ...any) int {
	fmt.Fprintf(c.Stderr, "playcall: "+format+"\n", a...)
	return exitError
}

// sink is the dry-run printer or the toast sink.
func (c *command) sink() core.Sink {
	if c.opts.dryRun {
		return core.StdoutSink{W: c.Stdout}
	}
	return core.ToastSink{Runner: c.Runner}
}

// header is "{SPORT}" or "{SPORT} - {group name}".
func (c *command) header(group *sports.Group) string {
	h := strings.ToUpper(c.opts.sport)
	if group != nil {
		h += " - " + group.Name
	}
	return h
}

// padRight pads s with spaces to width runes.
func padRight(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func (c *command) list(ctx context.Context, group *sports.Group) int {
	groupID := ""
	if group != nil {
		groupID = group.ID
	}
	fmt.Fprintln(c.Stdout, c.header(group))
	// A group can legitimately have no games (e.g. one league in a playoff round).
	data, err := espn.FetchScoreboard(ctx, c.Source, c.opts.sport, groupID, group != nil, c.Now())
	if err != nil {
		return fail(err)
	}
	cfg, err := core.LoadConfig(c.paths)
	if err != nil {
		return c.errorf("%v", err)
	}
	favorites := map[string]bool{}
	for _, t := range cfg.FavoriteTeams[c.opts.sport] {
		favorites[t] = true
	}
	var games []espn.Game
	for _, ev := range espn.List(data["events"]) {
		if g, ok := espn.ParseScoreboardEvent(espn.Map(ev)); ok {
			games = append(games, g)
		}
	}
	// Pad abbreviations to the longest one on the whole scoreboard, so the
	// width is the same whether or not favorites filter the list down.
	width := 0
	for _, g := range games {
		width = max(width, utf8.RuneCountInString(g.Away), utf8.RuneCountInString(g.Home))
	}
	if !c.opts.all && len(favorites) > 0 {
		var kept []espn.Game
		for _, g := range games {
			if favorites[strings.TrimSpace(g.Away)] || favorites[strings.TrimSpace(g.Home)] {
				kept = append(kept, g)
			}
		}
		games = kept
	}
	sort.SliceStable(games, func(i, j int) bool { return games[i].Date < games[j].Date })
	if !espn.Truthy(data["events"]) {
		fmt.Fprintln(c.Stdout, "No games scheduled.")
	}
	for _, g := range games {
		fmt.Fprintf(c.Stdout, "%s  %s @ %s  %s  %s-%s\n", g.ID,
			padRight(g.Away, width), padRight(g.Home, width), espn.PadStatus(g.Status), g.AwayScore, g.HomeScore)
	}
	return exitOK
}

func (c *command) teams(ctx context.Context, group *sports.Group) int {
	groupID := ""
	if group != nil {
		groupID = group.ID
	}
	fmt.Fprintln(c.Stdout, c.header(group))
	teams := espn.SortTeams(espn.FetchTeams(ctx, c.Source, c.opts.sport, groupID))
	if len(teams) == 0 {
		return fail(errors.New("Could not fetch team data"))
	}
	width := 0
	for _, t := range teams {
		width = max(width, utf8.RuneCountInString(t.Abbr))
	}
	for _, t := range teams {
		fmt.Fprintf(c.Stdout, "%s  %s\n", padRight(t.Abbr, width), t.Name)
	}
	return exitOK
}

func (c *command) addFavorite(team string) int {
	cfg, err := core.LoadConfig(c.paths)
	if err != nil {
		return c.errorf("%v", err)
	}
	sport := c.opts.sport
	if cfg.FavoriteTeams == nil {
		cfg.FavoriteTeams = map[string][]string{}
	}
	for _, t := range cfg.FavoriteTeams[sport] {
		if t == team {
			fmt.Fprintf(c.Stdout, "%s is already a %s favorite.\n", team, sport)
			return exitOK
		}
	}
	cfg.FavoriteTeams[sport] = append(cfg.FavoriteTeams[sport], team)
	if err := core.SaveConfig(c.paths, cfg); err != nil {
		return c.errorf("%v", err)
	}
	slog.Info(fmt.Sprintf("Added %s to %s favorites", team, sport))
	fmt.Fprintf(c.Stdout, "Added %s to %s favorites.\n", team, sport)
	return exitOK
}

// testNotify sends one sample notification. Like the Python it exits 0 even
// if the send fails (ToastSink has logged the error).
func (c *command) testNotify(ctx context.Context) int {
	c.sink().Send(ctx, core.Event{
		Title: "Test",
		Body:  "playcall is working",
		URL:   espn.GamePageURL(sports.DefaultSport, "999999999"),
	})
	return exitOK
}

// follow runs one poll of a game (--once; following continuously needs the
// service code). A non-numeric target is a team abbreviation: its game is
// looked up on the scoreboard first.
func (c *command) follow(ctx context.Context, target string) int {
	gameID := target
	if !isDigits(target) {
		g, err := core.FindGameForTeam(ctx, c.Source, c.opts.sport, strings.ToUpper(target), c.Now())
		if err != nil {
			return fail(err)
		}
		gameID = g.ID
		fmt.Fprintf(c.Stdout, "Found game %s: %s @ %s (%s)\n", g.ID, g.Away, g.Home, g.Status)
	}

	fetch, err := c.fetcher(gameID)
	if err != nil {
		return c.errorf("%v", err)
	}
	state := &core.State{GameID: gameID, Sport: c.opts.sport, StartedAt: float64(c.Now().UnixNano()) / 1e9}
	poller, err := core.NewPoller(state, c.sink(), fetch)
	if err != nil {
		return c.errorf("%v", err)
	}
	poller.Once = true
	poller.Now = c.Now
	if c.opts.dryRun {
		poller.Trace = c.Stdout
	}
	// Like the Python, a failed fetch on a single poll is not an error exit.
	if _, err := poller.Poll(ctx); err != nil {
		return c.errorf("%v", err)
	}
	return exitOK
}

// fetcher reads a game's play-by-play: from the --mock file (the same feed
// on every poll) or from ESPN.
func (c *command) fetcher(gameID string) (func(context.Context) (espn.Data, error), error) {
	if c.opts.mock != "" {
		data, err := c.Source.ReadMock(c.opts.mock)
		if err != nil {
			return nil, err
		}
		return func(context.Context) (espn.Data, error) { return data, nil }, nil
	}
	url := espn.SummaryURL(c.opts.sport, gameID)
	return func(ctx context.Context) (espn.Data, error) { return c.Source.Fetch(ctx, url) }, nil
}
