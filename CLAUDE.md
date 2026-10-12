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

## Architecture
The GUI is the main consumer, so the core must be reusable and independent of any output.

Layout: `cmd/playcall` (CLI `main`), `internal/...` (core). `cmd/playcall-gui` (phase 2) keeps Fyne's cgo requirement out of the core and the CLI build.

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
- `GUISink` (phase 2, Fyne, in `cmd/playcall-gui`): consumes `Event`, probably over a channel. Do not shape Event around toasts only, and never import Fyne from the core.

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
  - cfb always sends `groups=` (default 80, all FBS; without it ESPN returns only the top 25). The GUI picker's "Top 25" for cfb (`sports.RankedGroupID`, not in the `GROUPS` table, so the CLI, standings and goldens are unchanged) is the opposite: a request with no `groups=` at all, which gives ESPN's default list, the games with a ranked team (checked 2026-10-12: 16 of 62 in the week, all with a ranked team). It is never retried with `dates=` (ESPN answers with every game then), and has no standings lookup, so no bye rows. `list --group` picks a conference from the per-sport `GROUPS` table (name or id; name matching ignores case, spaces, hyphens and underscores). Every sport has a `GROUPS` entry. cfb/cbb start with an "All ..." entry (the `teams` default); the pro sports have conferences and divisions under short names (nfl: AFC, NFC, AFC East...; nba and nhl: East, West, Atlantic...; mlb: AL, NL, AL East...; wnba: East, West, no divisions). Ids are in the reference script.
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
- Body: "{start D&D} · {play text} · {end D&D}" (e.g. "1st & 10 · Rush for 11 yards · 2nd & 5"), where D&D is `shortDownDistanceText` from the play's `start` and `end`. The play text is ESPN's, stripped of surrounding whitespace. The start part is left out when there is no `start` (kickoffs); the end part when there is no `end`. The scoring-text baseline still uses the bare play text. ESPN writes a goal-line spot as the team plus 00 ("to the BYU00 TOUCHDOWN"); the body shows it as "BYU end zone", and other spots with a space and no leading zero ("ILL11" as "ILL 11", "BYU01" as "BYU 1"), and a name ESPN cut at six characters is completed from the game's teams ("Maryla22" as "Maryland 22", "OhioSt19" as "Ohio State 19"); display only, the play text is kept as sent. ESPN sends " & Goal" (down 0) on timeouts and some penalties; a D&D that starts with "&" has no down and is left out of the body. A timeout's own `start` can be stale (after a punt it still shows the fourth down), so a timeout's body, and the GUI header's situation while the last play is a timeout, use the end of the last non-timeout play before it (none after a score or kickoff). The timeout that comes right after a change of possession (a kickoff or punt, a turnover, a turnover on downs) is the official's (each ends with one unless the play scores), and so is the timeout right after a scoring play (the TV timeout before the kickoff: in one Saturday's college feeds all 31 timeouts after a touchdown or field goal were followed by the kickoff, never a snap, though ESPN files each under a team), and so is the timeout at 2:00 of the 2nd or 4th quarter (the two-minute warning), which ESPN logs below 2:00 when the play before it ran through the mark (that play's snap, from its "(02:03)" prefix, was above 2:00), so "Timeout BYU, clock 08:54" there is shown as "Official timeout, clock 08:54"; ESPN files it under a team.
- Urgency: normal for scoring plays (`scoringPlay`) or turnovers (`isTurnover`), low otherwise.
  - Kickoffs and punts are low, even if their text contains a keyword ("Kickoff" and "Punt" come from `type.text`). A missed field goal is normal.
  - Plain-text keywords (touchdown, field goal, safety, fumble, interception) are a fallback for plays without the flags above. NFL terms; other sports rely on `scoringPlay`/`isTurnover`.
- Updates: if a scoring play's text changes after it was sent (e.g. the PAT result is added to the touchdown play under the same id), it is re-sent as an update (`Kind: "update"`) with the new text. Updates are sent before new plays. ESPN also posts some plays with partial text (a bare "foul", "enters the game for X") and fills it in later, so basketball fouls and substitutions and NHL penalties and shots (a bare "Missed Shot" later becomes "Joel Farabee saved by Ville Husso") are re-sent the same way when their text changes after they were sent (`revisablePlay`; tracked in memory only, not in state.json). Other edits (a shot's description, a removed assist on a non-scoring play) are logged at DEBUG ("Play text changed") but not re-sent. In the GUI router, these corrections (`Event.Revision`) replace the row in the window and never make a toast; a re-sent scoring play does.
- Late football plays: a play from a period whose end an earlier poll already showed (an End Period, End of Half or End of Game play in the feed) is not announced, since nothing from the 1st quarter can follow the end of the 1st (`dropAfterPeriodEnd`; plays that arrive in the same poll as the end are sent). It is marked handled and logged at DEBUG.
- Replaced football plays: ESPN sometimes corrects a play (a kickoff "fair catch" became "Touchback") by dropping its id and posting the corrected play under a new one. A new play with the same period, clock, type and first 30 characters of text as an earlier one whose id has left the feed is that play again, so it is marked handled and not announced (`dropReplaced`; in memory, so a restart forgets it).
- Football clock quirks (ESPN's data, shown as follows): ESPN writes ", clock 00:00" into a score's text until it knows the time (a poll or two), so the body leaves that out unless the game clock really is 0:00 (the raw text is untouched, so the update with the real time is still detected). ESPN's game clock can also stay at the start of a quarter ("15:00 2nd") for minutes while plays go on (seen in cfb; its scoreboard and summary agreed); when it is more than 45 seconds above the newest play's clock in the same period, that play's clock is shown instead (`footballClock`, NFL and cfb, live games only, not at a period break).
- GUI header layout: one row, mirrored like a broadcast score bug (`headerRow` keeps the middle on the window's center whatever the side widths; in baseball each side's record column is a fixed `roleWidth` and the batter and pitcher names, shortened to an initial and surname, are cut with an ellipsis if still too long). Away: ball (if it has the ball), logo, abbreviation (record under it), score. Home: score, abbreviation (record under it), logo, ball. Football's timeouts sit under each score as circles (drawn, not the ● ○ characters, whose glyphs differ in size and height), filled for each left, centered on it (a gap between score and abbreviation, not a space in the score's text, so the two stay aligned); basketball's timeouts and fouls keep their own row under the header.
- Halftime estimate (NFL and college): `GameStatus.HalftimeEnds` is the first half's last play's `wallclock` (NFL: End of Half; college: the 2nd quarter's End Period) plus the usual break (`nflHalftime` 14:15, from seven games at 13:57 to 15:37 and a London one at 19:45; `cfbHalftime` 20:45, median of 45 games with 40 between 19:36 and 21:36, the rest delays; college's halftime status name is assumed to be ESPN's STATUS_HALFTIME and not yet seen live); the header shows "Halftime · ~9:40", redrawn each second, and plain "Halftime" once it runs out or when ESPN gives no stamp.
- Rankings (college football and basketball only, `sports.HasRankings`): ESPN's `curatedRank.current` on the scoreboard (99 = unranked) and `rank` in a game summary, kept when 1 to 25 (`espn.Rank`; `Game.AwayRank`/`HomeRank`, `TeamScore.Rank`). The GUI shows the number small, just before the abbreviation, on both teams (picker row: a ranking column sized to the widest, gone when nobody in the list is ranked; header: left of the abbreviation, hidden when unranked); toasts, titles and the CLI do not show it. A ranked game needs about 540 px of header width for the clock to stay on one line, hence the window's default width of 540 (baseball fits in it, with first names shortened to an initial).
- GUI header extras: under the clock, football shows the down and distance and baseball the bases as a diamond of squares on a corner with a home plate outline (`GameStatus.Bases`, "13" is first and third, filled when a runner is on) and the balls, strikes and outs as rows of circles beside it (`GameStatus.Balls/Strikes/Outs`: 3, 2 and 2 circles, no more; a result or inning break shows 0-0), centered together; the text "1-2, 1 out, 2 on" from the newest play that has one stays in titles and the CLI feed, and the window shows no count text; the baseball status line is ESPN's "Top 3rd" (it has no clock). Baseball has no ball marker (neither in the window nor in toasts): the half inning says who bats, and a ball would read as the pitching side. A toast for a pitch adds the batter after the text ("Ball 2 · Sam Antonacci", `Event.Batter`; the CLI output and the window rows do not change). Baseball also shows "P: ..." under the fielding team and "AB: ..." under the batting one (first name as an initial, "P: Y. Yamamoto", to save width), from the newest Start Batter/Pitcher play ("X pitches to Y"; the batter is blank after an at-bat's result until the next batter is announced, and everything is blank after an End Inning). In the postseason (season type 3) the game page shows no season records under the teams, and the picker shows only the series standing, if there is one, as one statement in the row's detail ("Series: LAD 2-1", "Series: Tied 2-2", "Series: LAD WINS" once won, from the scoreboard's `totalCompetitions`; nothing at 0-0), not a record under each team.
- Display text (window and toasts only, `Event.Display`; `Body` and the CLI stay ESPN's text, so the goldens hold): NHL, NBA, WNBA, cbb and MLB play text gets the team after each full player name ("Nikita Zadorov (PHI) hit Noel Acciari (BOS)"), from the box score's rosters (a name two teams share is left alone; football and baseball results use initials or surnames, so they are untouched). An NHL icing, which ESPN gives no team, reads "Icing (BOS)": the puck's shot-from `coordinate` is in the icing team's own end (the next faceoff is there too, 43 of 43), and the team that defends that end is the one whose shots in the period (or the one before, ends switch each period) come from the other end.
- Final: once the game is final and all plays are sent, one more event goes out before polling stops: title "{Away} {away_score} · {shortDetail} · {Home} {home_score}" (e.g. "PIT 24 · Final · CLE 27", "Final/10"), body "Game over", normal urgency (`Kind: "final"`). If `Send` fails, the next poll retries it. Applies to every sport.
- Postseason series: when the game header has a `series` entry with `type: "playoff"`, every title (plays, updates and the final event) ends with the standing in parentheses, leader first: `(LAD 2-1)`, `(Tied 2-2)`, `(BOS WINS)` once a team clinches (best-of from `totalCompetitions`). Nothing is added at 0-0 or outside a playoff series. Wins come from ESPN's `competitors[].wins`, matched to the game's teams by id; ESPN counts completed games only, so the final event adds this game's winner if the series' event list shows it is not counted yet. `Event.Series` carries the text without the parentheses. Its goldens (`mlb`, `wnba` and their replays) were written from the Go build, not the Python. The GUI's game page also tags the game in a playoff series with its place and length (`GameStatus.SeriesInfo`, "NLCS - Game 1 · Best of 7": ESPN's header `gameNote` when it has one, else the game's position in the series' event list), shown even at 0-0, before the standing "Series: LAD 2-0"; titles and the CLI are unchanged.
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
- Body: ESPN's text on one line (some contain a newline). A turnover with no player in it ("shot clock turnover": ESPN sends only the team's id for these) gets the team's abbreviation appended: "shot clock turnover (GS)".
- Urgency: normal for made threes (`scoringPlay` with `scoreValue` 3) and fouls (type contains "foul", not "turnover"); low otherwise, including 2-point baskets, free throws and bundled misses.
- Possession (the GUI's ball marker) is not in ESPN's data for basketball, so `basketballPossession` infers it from the plays: made shots and last free throws hand the ball over, rebounds and turnovers say who has it, a shooting foul sends the fouled team to the line, jump balls name the team, and period ends leave it unknown until the next shot. Checked against 40 complete NBA and WNBA games, the team it said had the ball was the next one to shoot or turn it over 99.7% of the time (answering on 98.9% of those plays). Hockey has no marker: ESPN gives nothing to infer it from.
- Timeouts and fouls (GUI header, under each score): `GameStatus.AwayCounts`/`HomeCounts` hold, per team, `timeoutsRemaining` (ESPN's `timeoutsUsed` is for the current period only, so it is not used; the row has one circle per timeout the team started with, i.e. those left plus the team's timeout plays counted from the feed, filled for each left and empty for each used). NBA and WNBA teams get 7 a game but at most 4 in the 4th quarter, so there the number filled is capped at 4 minus the timeouts used that quarter (`timeoutsUsed`), and in overtime they get 2 each period, so the row is two circles (2 minus the timeouts used that period); college teams get 4 a game plus one per overtime, unused ones carrying over (5OT with none used is 9), worked out from the timeouts taken rather than ESPN's remaining count (not checked against a live college feed), `fouls.teamFoulsCurrent` (fouls this period) and `fouls.bonusState` (NONE, else the team is in the bonus; DOUBLE shows as "Bonus", and for cbb as "Double bonus" with other states as "1-and-1"). Shown only while the game is in play. ESPN also gives `foulsToGive` (not used). NFL shows timeouts only ("●●○", no fouls): `footballCounts` takes 3 minus ESPN's `timeoutsUsed`, which counts the current half (it is 0 again by the end of the 2nd quarter, so halftime shows 3 each) and runs a play or two ahead of the numbered timeout plays ("Timeout #2 by PHI at 01:06."; checked over 9 games, they agree). Overtime is 2 each (ESPN's counter resets for it; seen in NYG @ WSH). College football shows nothing: its `timeoutsUsed` is a running total (up to 6, 7 with overtime), and its timeout plays include official ones filed under a team, so a count from the plays was right for the first half only about 80% of the time.
- Catch-up limit 10. cbb is assumed to match and has not been tested against a live feed. No shot-clock tracking (ruled out).

## Play tracking and state
- Where files live: `playcall/` under `os.UserConfigDir` (config) and `os.UserCacheDir` (state, log, logos, standings), so `~/.config/playcall` and `~/.cache/playcall` unless `XDG_CONFIG_HOME` or `XDG_CACHE_HOME` say otherwise (`core.DefaultPaths`). A user who sets those but still has the old folders keeps using the old ones until the new ones exist (`keepLegacy`), so nothing is orphaned. The paths below are written for the default layout.
- State file: `~/.cache/playcall/state.json` (os.UserCacheDir equivalent on Linux), same JSON format as the Python version so either binary can read it: `game_id`, `sport`, `last_play_id`, `started_at`, `scoring_texts` (play id -> last seen text for scoring plays), and for NHL, NBA, WNBA and cbb `sent_ids` (every play announced or skipped).
- NFL, cfb and MLB compare ids as integers against `last_play_id`. NFL and cfb ids are not always in play order (a timeout can carry a higher id than the snap after it), so a football play with an id below `last_play_id` that this run has not seen is a late play: it is sent once, and `last_play_id` does not move back (`Poller.known`, in memory; on the first poll of a run everything at or below `last_play_id` counts as seen, so a restart replays nothing). NHL and the NBA-rules sports use list order and `sent_ids`.
- First poll for a game announces only the most recent play, then follows new plays.
- After a gap (e.g. a reboot mid-game), only the newest 5 unseen plays are announced (10 for NHL and basketball); older ones are skipped.
- `last_play_id` advances only after a play's `Send` succeeds, so a failed send is retried on the next poll. Skipped plays advance it on purpose.
- Config: `~/.config/playcall/config.json`: `favorite_teams` (map of sport to list of abbreviations, a key for every sport), `interval` in seconds (default 10, minimum 5; anything lower is raised to 5, from config or `--interval`). Created with defaults on first run.
- GUI settings live in the same file and are kept as extra keys, so the CLI ignores them and a fresh config does not contain them: `favorite_groups` (sport -> remembered conference or division name, e.g. `{"cfb": "Big Ten"}`) and `highlight_color` (`blue`, `green`, `orange`, `red` or `none`), `last_sport` (the sport the picker last showed, so the next start opens on it with that sport's `favorite_groups` entry; a first run, with the key absent, opens on NFL; `--sport` overrides it for one run and then becomes the remembered sport; set when the user changes the picker's sport, or when the picker is shown for a different sport, e.g. coming back from a game), `last_game` (`{"sport", "game_id", "label"}`, set when a game is picked or continued; the picker's "Continue: ISU @ BYU (CFB)" button follows it again, and is hidden while the loaded list shows that game as final), and `log_level` (`debug`; absent means info; applied at once, and `--debug` forces debug for one run). The GUI's settings screen also edits `favorite_teams` and `interval`, saving each change right away by re-reading the file first.
- Team logos (GUI only): ESPN gives each team two logos, one for dark backgrounds and one for light (`logoDark`/`logo` on the scoreboard, the `dark` and default entries of `logos` elsewhere; `espn.TeamLogos`, `espn.Logo.Pick`). The GUI uses the one that matches the current theme, swaps them when the theme changes, and falls back to the other variant if ESPN lacks one. Each is downloaded once, as a 96px copy through ESPN's image service, into `~/.cache/playcall/logos/dark/` or `logos/light/` (file name: sha256 of the URL, first 16 hex digits), in the background; a failed download is not retried until the next run, and the abbreviation shows alone meanwhile. Files from before the variants were labeled (flat in `logos/`) are moved into `dark/` on start. The core and CLI never fetch images.
- Log: `~/.cache/playcall/playcall.log` via `log/slog`. State changes (saved/cleared, config created) and service start/stop are INFO. Fetches, new and updated plays, sent events, systemctl calls and skipped plays are DEBUG. Failures are WARN or ERROR.
- One game at a time. `follow` replaces the current game. Following several games at once was ruled out.

## Polling
- After a failed fetch, wait twice as long before the next try (starting from the interval, capped at 300 seconds); reset to the interval after a success. Unexpected errors in one poll are logged and the loop continues.
- A game that hasn't started (no plays, status `pre`) is followed too: sleep until `PREGAME_LEAD` (5 min) before the scheduled start, in chunks of at most `PREGAME_MAX_SLEEP` (30 min), instead of polling every interval.
- The loop stops cleanly when its context is cancelled.

## Toast contract (ToastSink, Linux, optional)
- Use `omarchy-notification-send`. `notify-send` is only the fallback `ToastSink` runs when `omarchy-notification-send` is not installed (`exec.ErrNotFound`; no `--exec`, so no click-to-open); when neither works `Send` returns `ErrNoNotifier`, which the GUI router treats as "no toasts here" (the play goes to the window, with one WARN in the log; Fyne's own `SendNotification` was tried and dropped: its success path was never tested, and without a daemon it added about two log lines per play). Any other failure of `omarchy-notification-send` is still returned and retried.
- `--exec` must be LAST, with the command as separate words (not a quoted string).
- Example: `omarchy-notification-send -u low -t 15000 "Bills 17, Jets 14" "Mahomes 45yd TD" --exec xdg-open https://www.espn.com/nfl/game/_/gameId/401872925`
- Urgency flag: `-u normal` or `-u low` from `Event.Urgent`. Duration `-t 15000` (`NOTIF_EXPIRE_MS`). Always include `--exec xdg-open <game page url>`.
- If the command fails, `Send` returns an error so the play is retried.
- `--test-notify` sends one sample toast (title "Test", body "playcall is working") and exits.

### Adaptive routing: UI vs toast (planned)
- When the Fyne GUI is running, each play event goes to the in-app UI if our window is visible, otherwise to `ToastSink` (system toast). Never both.
- Wayland does not tell clients which workspace they are on and Fyne has no workspace-visibility API, so detect visibility through Hyprland at event time (polling per play is cheap; do not use the Hyprland event socket):
  1. `hyprctl clients -j`: find our window by PID (`os.Getpid()` matches the client's `pid`; more robust than class/title), read `workspace.id`, `mapped`, `hidden`.
  2. `hyprctl monitors -j`: collect every monitor's `activeWorkspace.id` (each monitor has its own active workspace).
  3. Visible = our workspace id is in the active set AND `mapped` AND NOT `hidden`. A `special:` (scratchpad) workspace counts as hidden.
- Fallback: if `hyprctl` is unavailable or fails, a window with focus counts as visible and one without does not (`focusTracker`, from Fyne's foreground events; it starts unfocused). Sway and others could get their own check later. Keep this behind the OS-specific notifier interface with the rest of the Linux notification code.

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
- `stop`, `resume`: see Autostart.

### Autostart (Linux, CLI only)
Not built: slice 7b is skipped, and `follow` without `--once` is now the foreground feed above, not the systemd enabler; `stop` still prints "not implemented yet" (exit 2). What follows is the old design, kept for `resume` and in case the service is revived under another command.
- (not built) `follow` (without `--once`) checks the systemd unit file `~/.config/systemd/user/playcall.service` exists (exits 1 without writing state if not), writes state.json, runs `systemctl --user enable playcall.service` then `restart playcall.service` (restart, not --now, so a running service switches games), prints "Following game {id}. Use playcall stop to stop.", and exits. Exits 1 if systemctl fails.
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
  - `cmd_{scenario}.txt`: transcripts from `tools/golden_cmds.py` for `list`, `teams`, `follow <team>` and `addfavorite`. Each one runs a command with the network replaced by a table of URL -> fixture file, the clock frozen at 2026-10-07 12:00 local time, and a scratch home directory. A transcript has these sections: `$ playcall ...` (the command, for reading) and `args:` (the same arguments as a JSON array, the form the test must use, since quoting is lost in the first line), `config:` (the config file the run started with), `state:` (for the `resume` scenarios only: the state.json the run started with, or `(none)`; write it to the cache directory before running), `-- requests --` (every URL requested, in order, and the fixture that answered it, or `(no data)`; `mock <path>` for a `--mock` read), `-- stdout --`, `-- stderr --`, `-- log (INFO and above) --` (`LEVEL message` lines), optionally `-- config after (parsed, sorted) --`, for the `resume` scenarios `-- state after (parsed, sorted) --` (the state.json left behind, compared as parsed JSON; `(none)` means the file must not exist), and `-- exit N --`. The Go tests must serve the same fixtures for the same URLs (an `httptest` server or a fake client), inject the same "now" (the date-retry URL uses `dates=20261007`), and compare stdout, the request list, the log messages (not the timestamp format) and the exit code. Compare the config after as parsed JSON, not as text.
  - The `cmd_resume_*` transcripts cover `resume`: a game that finishes on the first poll (nfl and a TRACK_SENT sport), a resume after a gap, `--once` leaving the state file untouched, and no state at all. They use `--mock`, so no network.
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
