# playcall

Follow a live game play by play. playcall polls ESPN's free scoreboard and
play-by-play feeds and reports each play as it happens, in a small desktop
window, as a desktop notification, or as plain text on the command line.

Supported leagues: NFL, NBA, MLB, NHL, WNBA, college football (FBS) and men's
college basketball.

No API key is needed. playcall uses ESPN's public site API, which is
unofficial and undocumented, so it can change without notice.

## What you get

- **A GUI** (`playcall-gui`, built with [Fyne](https://fyne.io)): pick a game
  from today's scoreboard, then watch a header with both teams' logos, the
  score, clock and situation, over a list of plays, newest first. Urgent plays
  (scores, turnovers, penalties and so on) are highlighted in a color you
  choose. Settings cover favorite teams, a default conference per sport and
  the poll interval.
- **Possession markers** for football, baseball and basketball. ESPN does not
  report possession in basketball, so it is inferred from the plays.
- **Series standings** in the title during a playoff series, such as
  `(LAD 2-1)` or `(BOS WINS)`.
- **Toasts**: when the window is not on screen (another workspace, hidden or
  covered by the settings screen), plays go to a desktop notification instead,
  with the ball for the team that has it.
- **A CLI** (`playcall`) that prints the same text, with no dependencies
  beyond the Go standard library.

## Platform

Linux only for now. Parts of it assume an [Omarchy](https://omarchy.org)
setup:

| Feature | Needs |
| --- | --- |
| Toasts | `omarchy-notification-send` |
| Window-or-toast routing | Hyprland (`hyprctl`); without it every play is a toast |
| CLI autostart (`follow`, `stop`, `resume`) | a systemd user unit |

The OS-specific code is kept in its own files behind small interfaces so that
other platforms can be added later.

## Build

You need Go 1.24 or newer. The GUI also needs cgo and Fyne's system libraries
(a C compiler plus OpenGL and windowing headers; see
[Fyne's prerequisites](https://docs.fyne.io/started/)).

```sh
go build -o playcall ./cmd/playcall          # CLI, static, stdlib only
go build -o playcall-gui ./cmd/playcall-gui  # GUI, needs cgo
```

## Use

### GUI

```sh
./playcall-gui                        # game picker
./playcall-gui --sport nhl            # picker, starting on NHL
./playcall-gui --sport nba --game ID  # follow one game directly
```

`--mock fixture.json` replays a saved feed as if it were live (`--step` sets
how many plays appear per poll), which is handy for trying things out
without a game on:

```sh
./playcall-gui --sport nhl --mock test/fixtures/playbyplay_nhl_multi_plays.json
```

### CLI

```sh
./playcall list --sport mlb               # today's games (favorites only unless --all)
./playcall list --sport cfb --group "Big Ten"
./playcall teams --sport nfl
./playcall addfavorite BUF --sport nfl
./playcall follow BUF --sport nfl --dry-run --once   # print, don't notify
```

`follow` takes a game id or a team abbreviation. `--dry-run` prints to stdout
instead of sending toasts, and `--once` does a single poll and exits; together
they never touch your state file or the systemd unit.

### Files

| Path | Contents |
| --- | --- |
| `~/.config/playcall/config.json` | favorite teams per sport, poll interval (default 10 s, minimum 5 s), GUI settings |
| `~/.cache/playcall/state.json` | the game being followed (CLI) |
| `~/.cache/playcall/logos/` | team logos, downloaded once and cached |
| `~/.cache/playcall/playcall.log` | log |

## Layout

```
cmd/playcall        CLI: flag parsing and printing only
cmd/playcall-gui    Fyne GUI (the only place Fyne is imported)
internal/espn       HTTP client and JSON types for ESPN's endpoints
internal/core       per-sport rules, poll loop, state, config, sinks
internal/sports     sport and league tables
test/               fixtures and golden transcripts
```

The core never prints or exits. It turns plays into `Event` values and hands
them to a `Sink` (stdout, toast or window), so every output shows the same
text.

## Develop

```sh
.claude/hooks/verify.sh < /dev/null   # gofmt, go vet, go build, go test
```

Tests need neither the network nor a notification daemon. Golden files in
`test/golden/` pin the exact output for every fixture; see `CLAUDE.md` for the
rules around them and for the message contract per sport.

## Credits and data

Game data and logos come from ESPN and belong to ESPN and the leagues and
teams. This project is not affiliated with or endorsed by any of them.

## License

Not chosen yet.
