---
name: architecture-and-state
description: Architecture of the core (Event, Sink, layout), play tracking and state.json, polling and backoff, and the old autostart design. Use before changing the poll loop, state handling, config keys, sinks or the GUI/core boundary.
---

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

## Play tracking and state
- Where files live: `playcall/` under `os.UserConfigDir` (config) and `os.UserCacheDir` (state, log, logos, standings), so `~/.config/playcall` and `~/.cache/playcall` unless `XDG_CONFIG_HOME` or `XDG_CACHE_HOME` say otherwise (`core.DefaultPaths`). A user who sets those but still has the old folders keeps using the old ones until the new ones exist (`keepLegacy`), so nothing is orphaned. The paths below are written for the default layout.
- State file: `~/.cache/playcall/state.json` (os.UserCacheDir equivalent on Linux), same JSON format as the Python version so either binary can read it: `game_id`, `sport`, `last_play_id`, `started_at`, `scoring_texts` (play id -> last seen text for scoring plays), and for NHL, NBA, WNBA and cbb `sent_ids` (every play announced or skipped).
- NFL, cfb and MLB compare ids as integers against `last_play_id`. NFL and cfb ids are not always in play order (a timeout can carry a higher id than the snap after it), and MLB has the same ("Vesia relieved Yamamoto" arrived after the batter's "pitches to" play, under a lower id), so a football or baseball play with an id below `last_play_id` that this run has not seen is a late play: it is sent once, and `last_play_id` does not move back (`Poller.known`, in memory; on the first poll of a run everything at or below `last_play_id` counts as seen, so a restart replays nothing). NHL and the NBA-rules sports use list order and `sent_ids`.
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

### Autostart (Linux, CLI only)
Not built: slice 7b is skipped, and `follow` without `--once` is now the foreground feed above, not the systemd enabler; `stop` still prints "not implemented yet" (exit 2). What follows is the old design, kept for `resume` and in case the service is revived under another command.
- (not built) `follow` (without `--once`) checks the systemd unit file `~/.config/systemd/user/playcall.service` exists (exits 1 without writing state if not), writes state.json, runs `systemctl --user enable playcall.service` then `restart playcall.service` (restart, not --now, so a running service switches games), prints "Following game {id}. Use playcall stop to stop.", and exits. Exits 1 if systemctl fails.
- `stop` runs `systemctl --user disable --now playcall.service`, clears state.json, prints "Stopped." Exits 1 and keeps state if systemctl fails. If the unit file is missing, it skips systemctl and still clears state.
- `resume` reads state.json and re-enters the polling loop. With no state it logs a warning and exits 0 (so systemd doesn't restart it in a loop). When the game is final it runs `systemctl --user disable playcall.service` (no --now) and clears state; if disabling fails, the state is kept.
- Unit file: `ExecStart=/full/path/to/playcall resume`, `Restart=on-failure`, `RestartSec=5`, `WantedBy=default.target`.
- This is the CLI's way of surviving reboots. The GUI app will manage its own lifecycle, so expect to revisit this in phase 2. Keep it behind an `Autostart` interface.
