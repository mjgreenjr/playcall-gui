# playcall

Go port of the playcall core logic, built to become a GUI application. The GUI is the main output; desktop toasts stay as an optional feature.

- Phase 1 (this repo's current task, done by the cloud agent): Go core library + CLI. Behavior must match the Python original exactly.
- Phase 2 (done later by the owner, locally): a Fyne GUI in `cmd/playcall-gui`, on top of the same core. Do NOT add Fyne or any other dependency, do not create `cmd/playcall-gui`, and do not write GUI code. The only preparation is the `Event`/`Sink` boundary below.

## Reference implementation
`reference/playcall.py` is the original Python script and the source of truth for behavior, constants and data tables (`SPORTS`, `GROUPS` with ids, `DEFAULT_GROUPS`, `NHL_SILENT_TYPES`, `NBA_SILENT_TYPES`, `REGULATION_PERIODS`, `CATCHUP_LIMITS`, `PREGAME_LEAD`, `PREGAME_MAX_SLEEP`, `NOTIF_EXPIRE_MS`). Where this file and the Python disagree, ask; do not guess. Function names in this file (`mlb_contexts`, `nhl_power_plays`, `nba_bundles`, `nhl_clock_left`, ...) are Python names; use idiomatic Go names.

`test/golden/` holds the Python output for each fixture command, so `go test` can compare byte for byte. `tools/golden_cmds.py` generated the `cmd_*` command transcripts (list, teams, follow <team>, addfavorite) the same way. `tools/golden.py` generated the `replay_*` files by running `reference/playcall.py` over the fixtures. It is a reference tool: the Go port never calls it, and agents must not regenerate or edit the goldens to make a test pass. If a golden looks wrong, say so and ask.

## Stack
- Go 1.24+, stdlib only in phase 1 (net/http, encoding/json, os, time, fmt, flag, log/slog, context)
- Target: Linux only for now. Do not spend time on Windows or macOS. Keep OS-specific code (notifications, autostart, opening URLs) behind small interfaces in their own files so another platform can be added later.
- Static binary, no runtime dependencies

## Architecture
The GUI is the main consumer, so the core must be reusable and independent of any output.

Layout: `cmd/playcall` (CLI `main`), `internal/...` (core). `cmd/playcall-gui` will be added in phase 2 so Fyne's cgo requirement never touches the core or the CLI build.

- `internal/espn`: HTTP client and JSON types for scoreboard, summary, teams and standings. Includes `extract_plays` (drives layout and flat layout).
- `internal/core` (or similar): per-sport rules that turn plays into events, the poll loop, state, and config.
- `main` package: flag parsing and printing only. No game logic.
- The core never calls `os.Exit` or prints. It returns errors, logs through `slog`, takes a `context.Context` for cancellation, and reports through a `Sink`.

### Events and sinks
The poll loop produces `Event` values and hands each one to a `Sink`:

```go
type Event struct {
    GameID  string
    PlayID  string
    Kind    string // "play", "update" (re-sent scoring play), "final"
    Title   string // rendered title, per the contract below
    Body    string // rendered body
    Urgent  bool   // normal urgency; false = low
    URL     string // ESPN game page
    // Structured fields for the GUI, so it does not have to parse Title/Body:
    Away, Home           TeamScore // abbr, score, power-play flag
    Clock, Period        string
    PlayText             string
    Sport                string
}

type Sink interface {
    Send(ctx context.Context, e Event) error // an error means "retry on the next poll"
}
```

Sinks:
- `StdoutSink` (`--dry-run`): prints the toast text exactly as the Python dry-run does.
- `ToastSink` (Linux): runs `omarchy-notification-send` (see Toast contract). Optional; this is a feature, not the main output.
- `GUISink` (phase 2, Fyne, built locally by the owner): not part of this task. The `Event` struct above is the interface it will consume, probably over a channel. Do not shape Event around toasts only, and never import Fyne from the core.

`last_play_id` / `sent_ids` advance only when `Send` succeeds (see Play tracking). Keep Title/Body rendering in the core, shared by all sinks, so every sink shows the same text.

## API Endpoints (free, no key)
- URLs are parameterized by `--sport` (nfl, nba, mlb, nhl, cfb, cbb, wnba; default nfl), mapped to ESPN's `{sport}/{league}` path:
  - nfl -> football/nfl
  - nba -> basketball/nba
  - mlb -> baseball/mlb
  - nhl -> hockey/nhl
  - cfb -> football/college-football
  - cbb -> basketball/mens-college-basketball
  - wnba -> basketball/wnba
- Scoreboard: https://site.api.espn.com/apis/site/v2/sports/{sport}/{league}/scoreboard
  - cfb always sends `groups=` (default 80, all FBS; without it ESPN returns only the top 25). `list --group` picks a conference from the per-sport `GROUPS` table (name or id; name matching ignores case, spaces, hyphens and underscores). Every sport has a `GROUPS` entry. cfb/cbb start with an "All ..." entry (the `teams` default); the pro sports have conferences and divisions under short names (nfl: AFC, NFC, AFC East...; nba and nhl: East, West, Atlantic...; mlb: AL, NL, AL East...; wnba: East, West, no divisions). Ids are in the reference script.
  - The `groups=` filter matches games where either team is in the group, so cross-group games show in both lists. A group with no games prints the header and "No games scheduled." (exit 0).
  - Unfiltered, ESPN's default scoreboard jumps to the next game day when today has none; with a group it does not, so `fetchScoreboard` retries an empty group listing on the unfiltered scoreboard's `day.date`, keeping the groups param and adding `dates=YYYYMMDD` (ESPN ignores `date=`). NFL and cfb have no `day`; their default already covers the week.
  - Pro sports and cbb have no default group; `groups=` is sent only with `--group`.
- Play-by-play (game summary): https://site.api.espn.com/apis/site/v2/sports/{sport}/{league}/summary?event={GAME_ID}
  - NFL and cfb plays live under `drives.current.plays` and `drives.previous[].plays`; the other sports expose a flat top-level `plays` list. `extractPlays` handles every known layout. Header/competitions/status are at top level either way.
  - Chosen over the CDN playbyplay feed, which batches plays.
- Teams: `apis/site/v2/sports/{sport}/{league}/teams?limit=500`. Standings (for `teams --group`, and cfb/cbb `teams` always): `apis/v2/sports/{sport}/{league}/standings?group=ID` (conferences may nest).
- Game page URL: https://www.espn.com/{league}/game/_/gameId/{GAME_ID}, where `{league}` is the part after the slash in the sport path (cfb -> college-football, cbb -> mens-college-basketball).

### Go notes on ESPN JSON
- Types are inconsistent (ids, scores and clocks are strings in some places and numbers in others). Use tolerant types. Play ids are compared as integers (`int64`), never as strings.
- Teams are matched by `homeAway`, never by list position.
- Timestamps look like `2026-10-09T01:00Z` (no seconds); use layout `"2006-01-02T15:04Z"`.
- A scoreboard event's start time is its `date` field (ISO strings sort correctly as text). There is no `startTime` field, even though an earlier version of the Python looked for one. `list` sorts by `date`, stable, so games with the same start keep ESPN's order.
- Pad and align by rune count, not `len()` (bytes). Output uses `·`; text can contain accents.

## Message contract (title / body / urgency)
These rules define the text of every Event, whatever the sink. Default (NFL, cfb):
- Title: "{Away} {away_score} · {displayClock} {period} · {Home} {home_score}" (e.g. "ATL 14 · 0:48 1st · NO 0"; the status section is left out if there is no clock)
- Body: "{start D&D} · {play text} · {end D&D}" (e.g. "1st & 10 · Rush for 11 yards · 2nd & 5"), where D&D is `shortDownDistanceText` from the play's `start` and `end`. The play text is ESPN's, stripped of surrounding whitespace. The start part is left out when there is no `start` (kickoffs); the end part when there is no `end`. The scoring-text baseline still uses the bare play text.
- Urgency: normal for scoring plays (`scoringPlay`) or turnovers (`isTurnover`), low otherwise.
  - Kickoffs and punts are low, even if their text contains a keyword ("Kickoff" and "Punt" come from `type.text`). A missed field goal is normal.
  - Plain-text keywords (touchdown, field goal, safety, fumble, interception) are a fallback for plays without the flags above. NFL terms; other sports rely on `scoringPlay`/`isTurnover`.
- Updates: if a scoring play's text changes after it was sent (e.g. the PAT result is added to the touchdown play under the same id), it is re-sent as an update (`Kind: "update"`) with the new text. Updates are sent before new plays.
- Final: once the game is final and all plays are sent, one more event goes out before polling stops: title "{Away} {away_score} · {shortDetail} · {Home} {home_score}" (e.g. "PIT 24 · Final · CLE 27", "Final/10"), body "Game over", normal urgency (`Kind: "final"`). If `Send` fails, the next poll retries it. Applies to every sport.
- MLB, NHL and NBA-rules sports override the above (next sections).

### MLB
ESPN sends every pitch plus bookkeeping plays, so `mlbContexts` walks all plays in id order and describes each one. Plays marked not announced are left unseen (silent on every poll). The catch-up limit applies to announced plays only.
- Title: "{Away} {away_score} · {status} · {Home} {home_score}", with the play's own scores (not the latest). Status is "{Top|Bot} {inning} · {balls}-{strikes}, {outs} out(s), {n} on", e.g. "LAD 3 · Top 6 · 1-2, 1 out, 2 on · ATL 1".
  - Plays before the at-bat ended (Start Batter, pitches) show the count; Play Results leave it out and show the outs and runners after the play. A third out shows just "Top 6 · 3 outs".
- ESPN's `outs` is the count AFTER the at-bat, even on its first pitch, so outs are tracked: pitches use the previous at-bat's outs, the at-bat's Play Result sets them, and a mid at-bat out (caught stealing, picked off, thrown out) adds one. Runners on pitches are the runners before the pitch; on a result, after it.
- At-bat results are the Play Results whose id ends in 99xxxx. Other Play Results (substitutions, steals) happen mid at-bat and their `outs` is not reliable.
- Body: ESPN's text. Pitches drop the "Pitch N :" prefix (e.g. "Strike 2 Foul"). Start Batter keeps ESPN's text ("{pitcher} pitches to {batter}").
- Announced: pitches, Start Batter, Play Results, and the half-inning transition.
- Not announced: pitches that end the at-bat ("Ball In Play", "Strike 3", "Ball 4"; the Play Result carries it), Start Inning, End Batter/Pitcher, the Stolen Base play (duplicated by its Play Result), and fielder substitutions ("Dubón at shortstop.").
- Half-inning transition: the End Inning play is sent as title "{Away} {n} · {next half} · 0 outs · {Home} {n}", body "Side retired"; the next half is Bot N after "Mid N" and Top N+1 after "End N". Left out when the game is over (bottom of the 9th or later, or top of the 9th or later with the home team ahead); the final event covers that.
- Urgency: normal for scoring plays, outs (the out count rose), runners reaching (the runner count rose), runner substitutions ("ran for"), pitching changes ("relieved"), and runners put out mid at-bat. Everything else is low.

### NHL
Flat `plays` list of discrete events: Face Off, Stoppage, Shot, Blocked, Missed, Goal, Takeaway, Giveaway, Hit, penalties, Period Start/End.
- Announced: every play except the silent types (Hit, Period Start, Period End; the faceoff and stoppage around a period boundary are still sent). Unknown play types are announced.
- Ordering and "is new": NHL play ids are NOT chronological, and `sequenceNumber` is only the play's position in the list (ESPN inserts plays late, in the middle, shifting later numbers). The list is in game order, so take plays in list order; a play is new when its id is not in `sent_ids`, never by comparing to `last_play_id`. Skipped plays are added to `sent_ids` too. `last_play_id` is still written (last id sent) for the log.
- Title: "{Away} {n} · {time left} {period} · {Home} {n}" with the play's own scores. ESPN's clock counts UP from 0:00, so `nhlClockLeft` converts it to time left in the period (regulation length is `format.regulation.clock`, 1200; regular-season overtime is 5:00, playoff overtime a full period). If it can't convert, show ESPN's clock as is.
- Body: ESPN's text; penalties append their length, e.g. "Timo Meier High-sticking against Jack McBain · 2 min Minor" (`type.penaltyMinutes`, `type.penaltyType`).
- Urgency: normal for goals (`scoringPlay`) and penalties, low otherwise.
- Catch-up limit 10.
- Power play: the side with the man advantage gets "PP" after its score in the title ("UTA 1 PP · 18:03 1st · NJ 0"), dropped once the power play ends. `nhlPowerPlays` walks the plays in order. ESPN's `strength` label is relative to the play's `team` (Power Play: that team has the advantage; Shorthanded: the other team does; Even Strength: none). Plays with no team (stoppages) and penalty plays keep the last known side. A goal that ends the power play is still marked PP; the next play is not. The body never mentions it.
- Not tracked: a live penalty clock or 5-on-3 vs 5-on-4.

### NBA, WNBA and cbb (shared rules)
Flat `plays` list: made shots, misses, free throws, rebounds, substitutions, fouls, turnovers, timeouts, violations, Jumpball, End Period. The clock counts DOWN (12:00 to 0.0, sub-minute values like "31.4").
- Ordering: play ids are not in game order (the id is the game id followed by `sequenceNumber`, both out of order). List order is chronological; "is new" means the id is not in `sent_ids`.
- Announced: every play except the silent types ("Offensive Foul Turnover", which repeats the Offensive Foul at the same second). Substitutions and timeouts are kept.
- Miss bundling (`nbaBundles`): a missed shot or free throw (`shootingPlay` and not `scoringPlay`) goes out in one event with the rebound right after it: body "{miss text} · {rebound text}", title and urgency from the rebound. Both ids are marked sent together. A miss that is still the newest play is held back until the next poll shows what follows it, unless the game is over; a miss followed by anything other than a rebound goes out alone.
- Title: "{Away} {n} · {clock} {period} · {Home} {n}" with the play's own scores and clock; period is 1st-4th, then OT, 2OT, ... cbb plays two halves: 1st, 2nd, then OT, 2OT.
- Body: ESPN's text on one line (some contain a newline).
- Urgency: normal for made threes (`scoringPlay` with `scoreValue` 3) and fouls (type contains "foul", not "turnover"); low otherwise, including 2-point baskets, free throws and bundled misses.
- Catch-up limit 10. cbb is assumed to match and has not been tested against a live feed. No shot-clock tracking (ruled out).

## Play tracking and state
- State file: `~/.cache/playcall/state.json` (os.UserCacheDir equivalent on Linux), same JSON format as the Python version so either binary can read it: `game_id`, `sport`, `last_play_id`, `started_at`, `scoring_texts` (play id -> last seen text for scoring plays), and for NHL, NBA, WNBA and cbb `sent_ids` (every play announced or skipped).
- NFL, cfb and MLB compare ids as integers against `last_play_id`. NHL and the NBA-rules sports use list order and `sent_ids`.
- First poll for a game announces only the most recent play, then follows new plays.
- After a gap (e.g. a reboot mid-game), only the newest 5 unseen plays are announced (10 for NHL and basketball); older ones are skipped.
- `last_play_id` advances only after a play's `Send` succeeds, so a failed send is retried on the next poll. Skipped plays advance it on purpose.
- Config: `~/.config/playcall/config.json`: `favorite_teams` (map of sport to list of abbreviations, a key for every sport), `interval` in seconds (default 10, minimum 5; anything lower is raised to 5, from config or `--interval`). Created with defaults on first run.
- Log: `~/.cache/playcall/playcall.log` via `log/slog`. State changes (saved/cleared, config created) and service start/stop are INFO. Fetches, new and updated plays, sent events, systemctl calls and skipped plays are DEBUG. Failures are WARN or ERROR.
- One game at a time. `follow` replaces the current game. Following several games at once was ruled out.

## Polling
- After a failed fetch, wait twice as long before the next try (starting from the interval, capped at 300 seconds); reset to the interval after a success. Unexpected errors in one poll are logged and the loop continues.
- A game that hasn't started (no plays, status `pre`) is followed too: sleep until `PREGAME_LEAD` (5 min) before the scheduled start, in chunks of at most `PREGAME_MAX_SLEEP` (30 min), instead of polling every interval.
- The loop stops cleanly when its context is cancelled.

## Toast contract (ToastSink, Linux, optional)
- Use `omarchy-notification-send`, never `notify-send`.
- `--exec` must be LAST, with the command as separate words (not a quoted string).
- Example: `omarchy-notification-send -u low -t 15000 "Bills 17, Jets 14" "Mahomes 45yd TD" --exec xdg-open https://www.espn.com/nfl/game/_/gameId/401872925`
- Urgency flag: `-u normal` or `-u low` from `Event.Urgent`. Duration `-t 15000` (`NOTIF_EXPIRE_MS`). Always include `--exec xdg-open <game page url>`.
- If the command fails, `Send` returns an error so the play is retried.
- `--test-notify` sends one sample toast (title "Test", body "playcall is working") and exits.

## Commands
`./playcall` (built with `go build -o playcall .`). Flags (`--dry-run`, `--once`, `--interval`, `--mock`, `--sport`) work before or after the subcommand. Go's `flag` stops at the first positional argument, so use a `FlagSet` per subcommand and handle positionals yourself.

- `list [--all] [--sport S] [--group G]`: today's games. Output starts with a header line, `{SPORT}` (e.g. `MLB`) or `{SPORT} - {group name}` with `--group`. Then one line per game:
  ```
  {game_id}  {away_abbr} @ {home_abbr}  {status}  {away_score}-{home_score}
  ```
  - Abbreviations are padded with trailing spaces to the longest abbreviation on that sport's whole scoreboard (computed before the favorites filter).
  - Status is `shortDetail` from the status type. Scheduled start times (e.g. `10/9 - 9:00 PM EDT`) are padded so they line up: a single-digit month gets a leading space, a single-digit day a trailing space, a single-digit hour a leading 0 (`9/5` at 7:30 PM -> ` 9/5  - 07:30 PM EDT`). Only `list` pads; Final and in-progress clocks are left as is.
  - Not started: scores show `0-0`. Sorted by kickoff time, earliest first.
  - Filtered by the sport's `favorite_teams` unless `--all`; an empty list shows everything. The favorite match strips the display padding before comparing.
- `teams [--sport S] [--group G]`: `{abbr}  {team name}` for every team, sorted by name, abbreviations padded to the longest. Same header rule as `list`. With `--group` (and for cfb and cbb always) it reads the standings endpoint; cfb and cbb default to the first `GROUPS` entry.
- `follow {game_id|team} [--sport S]`: digits-only is a game id; anything else is a team abbreviation (uppercased). For a team, look it up on the sport's default scoreboard (same date-retry as `list`), print "Found game {id}: {away} @ {home} ({status})" and follow it. If the team has several games a live one wins, then an upcoming one, then a finished one; ties go to the earliest start. No match exits 1 with an explanation that says whether the abbreviation is invalid (with close matches) or the team simply has no game on the current scoreboard.
- `addfavorite {team} [--sport S]`: add the team (uppercased) to that sport's `favorite_teams`; no-ops with a message if already a favorite.
- `stop`, `resume`: see Autostart.

### Autostart (Linux, CLI only)
- `follow` (without `--once`) checks the systemd unit file `~/.config/systemd/user/playcall.service` exists (exits 1 without writing state if not), writes state.json, runs `systemctl --user enable playcall.service` then `restart playcall.service` (restart, not --now, so a running service switches games), prints "Following game {id}. Use playcall stop to stop.", and exits. Exits 1 if systemctl fails.
- `stop` runs `systemctl --user disable --now playcall.service`, clears state.json, prints "Stopped." Exits 1 and keeps state if systemctl fails. If the unit file is missing, it skips systemctl and still clears state.
- `resume` reads state.json and re-enters the polling loop. With no state it logs a warning and exits 0 (so systemd doesn't restart it in a loop). When the game is final it runs `systemctl --user disable playcall.service` (no --now) and clears state; if disabling fails, the state is kept.
- Unit file: `ExecStart=/full/path/to/playcall resume`, `Restart=on-failure`, `RestartSec=5`, `WantedBy=default.target`.
- This is the CLI's way of surviving reboots. The GUI app will manage its own lifecycle, so expect to revisit this in phase 2. Keep it behind an `Autostart` interface.

## Testing
- `--dry-run`: use `StdoutSink` instead of `ToastSink`. It must never touch systemd, the state file or the real service. (The Python version's `follow --dry-run` touched the real service; do not copy that.)
- `--once`: one poll cycle then exit. Standalone check: never writes state.json, never changes the service, for both `follow` and `resume`.
- `--mock <fixture.json>`: read play-by-play from a local file instead of ESPN.
- `--sport` works with `--mock`.
- Unit tests for the pure logic: MLB contexts (outs and runners), NHL power plays and clock conversion, NBA bundling, id ordering, state handling.
- Scoreboard, teams and standings code is tested against an `httptest` server serving the scoreboard fixtures. Tests must not need the network or `omarchy-notification-send`.
- Golden tests: `go test` compares against `test/golden/`, which was generated by the Python version. Do not edit these files by hand.
  - `{name}.txt` (e.g. `nba.txt`): the output of the matching fixture command below with `--dry-run --once`.
  - `replay_{name}_c3.txt`, `replay_{name}_c25.txt`, `replay_{name}_update.txt`: transcripts from `tools/golden.py`, which replays a fixture as a live game through the real poll code. Poll i sees only the first `min(i*chunk, N)` plays, where N is the number of unique plays and plays are counted in `extractPlays` order (top-level `plays`, then playback plays, then previous drives, then the current drive). `c3` and `c25` use chunk sizes 3 and 25 (25 exceeds the catch-up limit, so it exercises skipping). State carries over between polls in memory and nothing is written to disk. A final game reports "In Progress" until its last poll, so the final event appears only at the end. `update` polls the full feed, then again with the first scoring play's text extended by " (extra point is good)", then once more unchanged.
  - Each poll in a transcript starts with `== poll i: ... ==`, then the dry-run output, then `-> returned {true if the game finished}; last_play_id=... sent_ids={count} scoring_texts={count}`. The Go replay test must build the same truncated feeds and produce this exact text (the `-> returned` line may be formatted differently as long as the values match).
  - These cover the hard parts: MLB outs and runners, NHL power play and clock conversion, NBA miss bundling and hold-back, catch-up skipping, and scoring-text updates.
  - `cmd_{scenario}.txt`: transcripts from `tools/golden_cmds.py` for `list`, `teams`, `follow <team>` and `addfavorite`. Each one runs a command with the network replaced by a table of URL -> fixture file, the clock frozen at 2026-10-07 12:00 local time, and a scratch home directory. A transcript has these sections: `$ playcall ...` (the command, for reading) and `args:` (the same arguments as a JSON array, the form the test must use, since quoting is lost in the first line), `config:` (the config file the run started with), `-- requests --` (every URL requested, in order, and the fixture that answered it, or `(no data)`; `mock <path>` for a `--mock` read), `-- stdout --`, `-- stderr --`, `-- log (INFO and above) --` (`LEVEL message` lines), optionally `-- config after (parsed, sorted) --`, and `-- exit N --`. The Go tests must serve the same fixtures for the same URLs (an `httptest` server or a fake client), inject the same "now" (the date-retry URL uses `dates=20261007`), and compare stdout, the request list, the log messages (not the timestamp format) and the exit code. Compare the config after as parsed JSON, not as text.
  - These cover the ordering (by `date`), the padding, the favorites filter, the group and date retries, the error messages (including the "did you mean" matches), the doubleheader choice (live, then upcoming earliest, then finished), and config creation and updates. Not covered: argparse's own usage and error text, which Go does not need to copy.
- `test/fixtures/` (copied from the Python repo):
  - scoreboard_in_progress.json: one live NFL event
  - scoreboard_in_progress_full.json: complete, unmodified NFL scoreboard response
  - scoreboard_final.json: one final NFL event
  - scoreboard_nba_in_progress.json: one live NBA event
  - playbyplay_multi_plays.json: NFL live game, a few plays (drives-shaped)
  - playbyplay_final.json: NFL final game, trimmed
  - playbyplay_empty.json: pre-game shape, no drives
  - playbyplay_nba_multi_plays.json: NBA live game, real full feed (ids out of order in 5 places, 80 of 84 misses followed by a rebound). With `--once` the first poll announces only the newest play.
  - playbyplay_wnba_multi_plays.json: WNBA final game (LV @ GS), real full feed. With `--once`: newest play, then the final event.
  - playbyplay_cfb_multi_plays.json: cfb live game, trimmed (drives-shaped)
  - playbyplay_mlb_multi_plays.json: MLB final game (LAD @ ATL), real full feed. With `--once`: the last announced play, then the final event.
  - playbyplay_nhl_multi_plays.json: NHL live game (UTA @ NJ, 3rd period), real full feed (ids out of order). With `--once`: the newest announced play.
  - Captured from ESPN on 2026-10-07 and trimmed to the fields the code reads (structure and values unchanged): scoreboard_nfl_week.json (15 games, several days, scheduled times), scoreboard_cfb_fbs.json (58 games, 4-letter abbreviations, feed not in date order), scoreboard_mlb_day.json and scoreboard_nba_day.json (finished games, with `day.date`), teams_nfl.json (the teams endpoint), standings_cfb_all.json (nested conferences), standings_nfl_afc.json and standings_nfl_afc_east.json (standings for a conference and a division).
  - Synthetic, written by `tools/golden_cmds.py`: scoreboard_syn_padding.json (scheduled-time and other statuses, derived from the NFL week), scoreboard_syn_doubleheader.json and scoreboard_syn_doubleheader_nolive.json (one team in several games), scoreboard_syn_noteams.json (an event with one team), scoreboard_empty.json (`{"events": []}`).

## Verification
After any change, run:
1. `gofmt -l .` (must print nothing) and `go vet ./...`
2. `go test ./...` (includes the golden comparisons)
3. `go build -o playcall .`
4. The fixture runs, each with `--dry-run --once` (these are covered by the golden tests, run them by hand when debugging):
   - `./playcall follow 999 --mock test/fixtures/playbyplay_multi_plays.json`
   - `./playcall follow 999 --mock test/fixtures/playbyplay_final.json`
   - `./playcall follow 999 --sport nba --mock test/fixtures/playbyplay_nba_multi_plays.json`
   - `./playcall follow 999 --sport cfb --mock test/fixtures/playbyplay_cfb_multi_plays.json`
   - `./playcall follow 999 --sport wnba --mock test/fixtures/playbyplay_wnba_multi_plays.json`
   - `./playcall follow 999 --sport mlb --mock test/fixtures/playbyplay_mlb_multi_plays.json`
   - `./playcall follow 999 --sport nhl --mock test/fixtures/playbyplay_nhl_multi_plays.json`
5. On a machine with network access, `./playcall list --dry-run` as a live smoke test (skip in the cloud sandbox).

## Rules
- Go stdlib only in phase 1; ask before adding any dependency.
- Keep behavior identical to the Python reference. Golden diffs are bugs unless the Python was wrong, in which case say so and ask first.
- Core packages never print or exit; only `main` does.
- Keep OS-specific code in separate files behind interfaces.
- Use targeted edits for fixes; keep `gofmt` clean.
- Never commit or push to master. Work on a branch for the task and open a pull request. One slice per PR.
- Each PR description lists which golden tests now pass and anything that diverges from the Python reference.

## Out of scope
- The GUI itself (phase 2, Fyne, done locally by the owner), Windows and macOS, following several games at once, shot-clock tracking.
