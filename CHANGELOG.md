# Changelog

Notable changes to playcall, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the version numbers
[Semantic Versioning](https://semver.org/) (pre-1.0: a minor bump for new
features and changes to what users see or store, a patch for fixes).

Anything a user could notice, or that could break a file or script they have,
is listed under "Unreleased" in the commit that makes the change. A release is
cut only on request: "Unreleased" becomes the next version and `Version` in
`internal/version/version.go` changes with it.

## [Unreleased]

### Changed
- The game page's header keeps its middle (clock, count, bases) on the window's center instead of shifting with the names under the teams. In baseball the batter and pitcher names (now labeled "AB:" and "P:", not "At bat:" and "Pitcher:") get a fixed-width column under each team and a longer name is cut short with "..." (before, a long name widened the header and squeezed the middle). The window opens 20 px wider (560) to fit it.
- Baseball's game page shows the bases as a small diamond of three circles (second on top), filled for each runner on, with home plate at the bottom (the bases are squares turned on a corner), in place of "2 on" under the count and outs. Toasts, titles and the CLI keep the "2 on" text.

### Added
- A `Makefile`: `make` builds both programs, `make install` copies them to `~/.local/bin` (or `PREFIX`), `make uninstall`, `make cli`, `make test`. The README has an Install section.

## [0.3.0] - 2026-10-12

### Added
- College teams' rankings (1 to 25) show as a small number before the team's abbreviation, in the picker rows and in the game header, for college football and basketball; unranked teams show nothing. The window opens a little wider (540 instead of 480) so a ranked game's clock still fits on one line.
- College football's group list in the picker has a "Top 25" choice, after All: the games with a ranked team, from ESPN's own default scoreboard (no teams-without-a-game rows for it).
- The picker opens on the sport you last used, with that sport's remembered group (it already remembered a group per sport). A first run still starts on NFL and All; `--sport` still wins for that run.
- Config, state, log and cache files follow `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` when they are set (the same `~/.config/playcall` and `~/.cache/playcall` as before when they are not). If they are set and the old folders still hold your files, the old ones keep being used until the new folders exist, so nothing is lost.
- Toasts work without Omarchy: when `omarchy-notification-send` is not installed, `notify-send` is used (no click-to-open), and when neither works the GUI shows the play in the window only (one warning in the log). Before, a missing notifier kept every play out of the window.
- Without Hyprland the GUI can't ask where its window is, so a window with focus counts as visible (plays go to the list) and one without gets toasts.

## [0.2.0] - 2026-10-12

### Added
- `playcall follow GAME` without `--once` keeps following in the terminal and prints each play and each header change as a time-stamped line, to stdout or appended to a file with `--out FILE`. It sends no toasts and writes no state file. (It used to exit with "not implemented".)
- `--version` on both binaries, and the version at the foot of the GUI's settings screen.
- Football halftime shows an estimated countdown ("Halftime · ~9:40"): the end-of-half play's time plus the usual break (14:15 NFL, 20:45 college). It goes back to plain "Halftime" when the estimate runs out, and works when you start mid-break.

### Changed

- The GUI no longer echoes its log to the terminal; it goes to `~/.cache/playcall/playcall.log` only (stderr still gets startup errors).
- The game picker shows a playoff series as one statement on the right of the row ("Series: LAD 2-1", "Series: Tied 2-2", "Series: LAD WINS" once it is won; nothing at 0-0) instead of a "series 2-1" record under each team.

### Fixed
- The settings screen scrolls when the window is too short to show it all, so the bottom (and the version) can still be reached.
- NFL timeout circles are drawn shapes, so the filled and outlined ones are the same size and sit on one line.

## [0.1.0] - 2026-10-11

First numbered version. A summary of what exists, not a history.

### Added
- CLI (`playcall`): `list`, `teams`, `follow`, `addfavorite`, `stop`, `resume`
  and `test-notify` for NFL, NBA, MLB, NHL, college football, men's college
  basketball and WNBA, with desktop toasts, `--dry-run`, `--once` and `--mock`.
- GUI (`playcall-gui`): game picker with records and a Continue button, live
  header (score, clock, situation, timeouts, series), play list, settings
  (poll interval, highlight color, log level, favorites), team logos, and
  routing of each play to the window or a toast depending on whether the
  window is visible.
- Config and state files shared with the original Python version (extra keys
  added by the GUI are ignored by the CLI).
- NFL timeouts under each score (3 a half, 2 in overtime) and playoff games
  tagged with their game number and series length.
