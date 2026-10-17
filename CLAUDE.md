# playcall

Go app that follows live ESPN play-by-play (NFL, NBA, MLB, NHL, college football, men's college basketball, WNBA) and reports each play. The GUI is the main output; desktop toasts and the CLI stay as optional features. This repo is the only active implementation: the Python original (`reference/playcall.py`) is frozen. New sports, features and fixes go here only, and the Python is never updated to match.

Status: the core library and CLI (slices 1-7) are merged and match the Python on every golden. Current work is phase 2, a Fyne GUI in `cmd/playcall-gui` on top of the same core. Slice 7b (the systemd service: `stop`, systemctl) is deferred and likely skipped, since the GUI manages its own lifecycle; `follow` without `--once` instead follows in the terminal and prints a feed.

## Reference implementation (frozen)
`reference/playcall.py` is the original Python script. It was the source of truth while the port was built and is kept as a parity baseline for the seven existing sports and for `tools/compare_live.sh`. Where this file and the Python disagree, this file wins; the Python is only evidence of how the old behavior worked. Fix bugs in the Go, and do not edit the Python. Function names in this file (`mlb_contexts`, `nhl_power_plays`, `nba_bundles`, `nhl_clock_left`, ...) are the Python names; the Go uses idiomatic names (`mlbContexts`, ...).

`test/golden/` holds the Python's output for each fixture command, so `go test` can compare byte for byte. `tools/golden_cmds.py` generated the `cmd_*` command transcripts (list, teams, follow <team>, addfavorite, resume) and `tools/golden.py` the `replay_*` files by running the reference over the fixtures. The Go port never calls them. These goldens are a regression baseline: do not edit them by hand or regenerate them to make a test pass; if one looks wrong, say so and ask. Goldens for anything added after the port (new sports, new commands) have no Python behind them: write them from the Go build's output, review them by hand, and keep them separate from the Python-derived ones.

## Stack
- Go 1.24+. The core (`internal/...`) and the CLI are stdlib only (net/http, encoding/json, os, time, fmt, flag, log/slog, context). Fyne is allowed only in `cmd/playcall-gui`; ask before adding any other dependency.
- Target: Linux only for now. Do not spend time on Windows or macOS. Keep OS-specific code (notifications, autostart, opening URLs) behind small interfaces in their own files so another platform can be added later.
- The CLI is a static binary with no runtime dependencies. The GUI needs cgo (Fyne).

## Reference (load on demand)
The long reference sections live in skills, so they do not sit in every session. Invoke the one that matches the work before changing that area; when a change adds or corrects a finding, update the skill it lives in, not this file.
- `espn-api`: endpoints, URL parameters, groups and dates, the Go notes on ESPN's JSON.
- `message-contract`: title, body and urgency of every event; all football and college rules and ESPN quirks (timeouts and circles, down and distance, halftime, late plays, GUI header, rankings, final, series).
- `sport-rules`: MLB, NHL and NBA/WNBA/cbb rules.
- `architecture-and-state`: Event/Sink design, play tracking and state.json, config keys, polling, the old autostart design.
- `toasts-and-routing`: `omarchy-notification-send` and window-versus-toast routing.
- `golden-tests`: flags for testing, golden and replay transcripts, the fixture list.
- `add-sport`: how to add a sport.

## Testing (short)
`go test ./...` (through `/verify`) compares against `test/golden/`, which the Python reference generated: never edit or regenerate those by hand, and a diff is a bug in the Go unless the Python was wrong (say so and ask). Tests must not need the network or `omarchy-notification-send`. `--dry-run` never touches systemd, state or the service; `--once` never writes state; `--mock <fixture.json>` reads a local file. Details: the `golden-tests` skill.

## Commands
`./playcall` (built with `go build -o playcall ./cmd/playcall`). Flags (`--dry-run`, `--once`, `--interval`, `--mock`, `--sport`) work before or after the subcommand. Go's `flag` stops at the first positional argument, so use a `FlagSet` per subcommand and handle positionals yourself.

- `list [--all] [--sport S] [--group G]`: today's games. Output starts with a header line, `{SPORT}` (e.g. `MLB`) or `{SPORT} - {group name}` with `--group`. Then one line per game:
  ```
  {game_id}  {away_abbr} @ {home_abbr}  {status}  {away_score}-{home_score}
  ```
  - Abbreviations are padded with trailing spaces to the longest abbreviation on that sport's whole scoreboard (computed before the favorites filter).
  - Status is `shortDetail` from the status type. Scheduled start times (e.g. `10/9 - 9:00 PM EDT`) are padded so they line up: a single-digit month gets a leading space, a single-digit day a trailing space, a single-digit hour a leading 0 (`9/5` at 7:30 PM -> ` 9/5  - 07:30 PM EDT`). Only `list` pads; Final and in-progress clocks are left as is.
  - Not started: scores show `0-0`. Sorted by kickoff time, earliest first.
  - Filtered by the sport's `favorite_teams` unless `--all`; an empty list shows everything. The favorite match strips the display padding before comparing.
- `teams [--sport S] [--group G]`: `{abbr}  {team name}` for every team, sorted by name, abbreviations padded to the longest. Same header rule as `list`. With `--group` (and for cfb and cbb always) it reads the standings endpoint; cfb and cbb default to the first `GROUPS` entry.
- `follow {game_id|team} [--sport S] [--out FILE]`: digits-only is a game id; anything else is a team abbreviation (uppercased). For a team, look it up on the sport's default scoreboard (same date-retry as `list`), print "Found game {id}: {away} @ {home} ({status})" and follow it. If the team has several games a live one wins, then an upcoming one, then a finished one; ties go to the earliest start. With `--once` it is one poll (dry-run output, no state). Without `--once` it is the foreground feed (`followFeed`, `core.FeedSink`): it keeps polling until the game ends or Ctrl-C and prints one time-stamped line per play (`PLAY`, `UPDATE`, `REVISE`, `FINAL`) and per change of the header (`HEADER`, from `core.HeaderLine`, written only when its text changes), plus a `NOTE` line at the start, to stdout or appended to `--out FILE` (created if missing, never truncated; `--out` is a usage error with anything else). It sends no toasts, writes no state file, and keeps the log in its file only (stdout is just the feed); "Found game ..." for a team goes to stderr. No match exits 1 with an explanation that says whether the abbreviation is invalid (with close matches) or the team simply has no game on the current scoreboard.
- `addfavorite {team} [--sport S]`: add the team (uppercased) to that sport's `favorite_teams`; no-ops with a message if already a favorite.
- `stop`, `resume`: see Autostart in the `architecture-and-state` skill.

## Verification
After any change, run `/verify` (`.claude/commands/verify.md`). The sequence lives in `.claude/hooks/verify.sh`: gofmt, `go vet ./...`, `go build ./...` and `go test ./...`, which includes the golden comparisons for every fixture and command transcript. The Stop hook runs the same script and skips it when the Go code and `test/` hash the same as the last passing run (`.claude/.verified_hash`, gitignored).

Extra checks by hand:
- `go build -o playcall ./cmd/playcall`, then a fixture run when debugging a golden diff, e.g. `./playcall follow 999 --sport nhl --mock test/fixtures/playbyplay_nhl_multi_plays.json --dry-run --once`.
- On a machine with network access, `./playcall list --dry-run` as a live smoke test (skip in the cloud sandbox), or `tools/compare_live.sh SPORT GAME_ID SECONDS` to run the Python reference and the Go build side by side on a live game.

To add a new sport, use the `add-sport` skill (`.claude/skills/add-sport/`).

The `Makefile` builds and installs both programs (`make`, `make install [PREFIX=...]`, `make uninstall`, `make cli`, `make test`); the README's Install section documents it. It is not part of `/verify`.

`tools/cloud-tests/` holds instructions for a Claude Code cloud session to run things this desktop can't (a clean Linux with no Omarchy or Hyprland); start one with `claude --cloud` and tell it to follow the file. A cloud session writes its report under `tools/cloud-tests/results/` and opens a pull request with only that, from a `cloud-tests/...` branch (the exception to "no branches or pull requests"); it never changes code. The desktop session reviews the PR (`gh pr view`, `gh pr diff`) and tells the maintainer whether to merge; the maintainer merges and deletes the branch.

## Versioning and changelog
- Semantic Versioning, pre-1.0 (now 0.3.0): a minor bump for new features and for changes to what users see or store, a patch for fixes. The one source of truth is `Version` in `internal/version/version.go`; both binaries' `--version` and the GUI's settings screen read it, and a test fails if `CHANGELOG.md`'s newest `## [x.y.z]` heading differs from it.
- `CHANGELOG.md` (Keep a Changelog format) lists anything a user could notice, or that could break a config, state file, flag or exit code they depend on: new features and layout changes under Added/Changed, anything a user would complain about and then get fixed under Fixed. Put the entry under `## [Unreleased]` in the same commit as the change. Refactors, tests and docs are not listed.
- A change that renames or removes a config key, changes `state.json`'s layout, a flag or an exit code needs a migration or compatibility path and a note in the changelog; say so before making it. (Everything so far has been additive: new keys the CLI and the Python ignore.)
- Tell the maintainer which part of the number (minor or patch) the Unreleased entries call for. Do not bump `Version`, tag or publish unless asked: a release turns "Unreleased" into the new `## [x.y.z] - date` heading and changes `Version` in the same commit.

## Rules
- Core packages never print or exit; only `main` does. Never import Fyne from `internal/...` or `cmd/playcall`.
- Keep OS-specific code in separate files behind interfaces.
- Use targeted edits for fixes; keep `gofmt` clean.
- Behavior of the seven existing sports must keep matching the goldens. A golden diff is a bug in the Go unless the Python was wrong; say so and ask first.
- Commit directly on master. Do not create branches or pull requests unless the user asks for one. Commit when a piece of work is done and verified; push only when asked.

## Out of scope
(`ROADMAP.md` lists ideas for later, some of which, like several games at once, reverse an item below; change this list first if one is taken up.)

- Windows and macOS, following several games at once, shot-clock tracking.
