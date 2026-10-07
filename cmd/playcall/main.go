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
	"strings"
	"time"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

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
	sport             string
	given             map[string]bool
}

const usageText = `usage: playcall [--dry-run] [--once] [--interval N] [--mock FILE] [--sport S] COMMAND ...

commands:
  list [--all] [--group G]   show today's games
  teams [--group G]          list team names and abbreviations
  follow GAME_ID|TEAM        follow a specific game
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

func run(args []string, stdout, stderr io.Writer) int {
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
	want := 0
	if cmd == "follow" || cmd == "addfavorite" {
		want = 1
	}
	if len(rest) != want {
		return usageErr("%s takes %d positional argument(s), got %d", cmd, want, len(rest))
	}

	switch cmd {
	case "follow":
		return follow(opts, rest[0], stdout, stderr)
	}
	return notImplemented(stderr, cmd)
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

// follow runs one poll of a game (--once). Following continuously needs the
// service code and team lookup, which are not written yet.
func follow(opts options, target string, stdout, stderr io.Writer) int {
	if !isDigits(target) {
		return notImplemented(stderr, "follow TEAM")
	}
	if !opts.once {
		return notImplemented(stderr, "follow without --once")
	}
	if !opts.dryRun {
		return notImplemented(stderr, "follow without --dry-run (toast sink)")
	}
	paths, err := core.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "playcall: %v\n", err)
		return exitError
	}
	closeLog, err := core.SetupLogging(paths, stdout, slog.LevelInfo)
	if err != nil {
		fmt.Fprintf(stderr, "playcall: %v\n", err)
		return exitError
	}
	defer closeLog()

	var fetch func(context.Context) (espn.Data, error)
	if opts.mock != "" {
		data, err := espn.ReadMock(opts.mock)
		if err != nil {
			fmt.Fprintf(stderr, "playcall: %v\n", err)
			return exitError
		}
		fetch = func(context.Context) (espn.Data, error) { return data, nil }
	} else {
		var client espn.Client
		url := espn.SummaryURL(opts.sport, target)
		fetch = func(ctx context.Context) (espn.Data, error) { return client.Fetch(ctx, url) }
	}
	state := &core.State{GameID: target, Sport: opts.sport, StartedAt: float64(time.Now().UnixNano()) / 1e9}
	poller, err := core.NewPoller(state, core.StdoutSink{W: stdout}, fetch)
	if err != nil {
		fmt.Fprintf(stderr, "playcall: %v\n", err)
		return exitError
	}
	poller.Once = true
	poller.Trace = stdout
	// Like the Python, a failed fetch on a single poll is not an error exit.
	if _, err := poller.Poll(context.Background()); err != nil {
		fmt.Fprintf(stderr, "playcall: %v\n", err)
		return exitError
	}
	return exitOK
}
