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

### Added
- `--version` on both binaries, and the version at the foot of the GUI's settings screen.
- NFL halftime shows an estimated countdown ("Halftime · ~9:40"): the end-of-half play's time plus the usual 14:15 break. It goes back to plain "Halftime" when the estimate runs out, and works when you start mid-break.

### Changed

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
