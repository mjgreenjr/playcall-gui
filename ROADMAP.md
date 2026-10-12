# Roadmap

Ideas for after the first public release. This is a list of things worth
doing, in rough order of how soon, not a promise or a schedule. Nothing here
is built unless it says so; the [changelog](CHANGELOG.md) lists what exists.

## Near term

- **A waybar module.** `playcall waybar` stays running and prints one JSON
  line each time the game header changes (text, tooltip, class), for a waybar
  `custom` module with `"return-type": "json"`. The bar shows a short score
  line, the hover tooltip is the pop-down with the full header (teams,
  records, count, runners, timeouts, series), and the class lets a stylesheet
  color it for pregame, live, a scoring play and final. The last-play part is
  just the status ("Final", "Game over"); a click opens the ESPN game page or
  the full window. To settle: which game it follows (the live game of a
  favorite team by default, `--team` to override), what it shows with no game,
  and how long "Final" stays.
- **A compact floating window.** The same header plus one status line in a
  small window, from the existing GUI code (a compact mode), with Hyprland
  window rules to float and pin it. Click or a key expands it to the full
  window. Fyne has no always-on-top and no layer-shell, so a true overlay would
  need another toolkit and a new dependency, which would be decided first.
- **Screenshots in the README:** a game in progress (a header with runners on,
  or a ranked football game), and possibly the settings screen.
- **A `.desktop` entry and icon** so the GUI starts from a launcher.

## New sports

ESPN has all three; none of them has been looked at yet, and each probably
has a different data shape from the team sports here, so each needs its own
research (and a fixture, goldens and the `add-sport` skill) before any
promise.

- **Golf.** A tournament is a leaderboard, not two teams: the header and the
  event rules would be different.
- **Tennis.** Matches with sets, games and points; probably a score line in
  place of a clock.
- **Soccer.** Many leagues, each its own path on ESPN; plays are commentary
  entries, and stoppage time and extra time make the clock unlike the other
  sports.

## Larger changes

- **Following several games at once.** This reverses the current "one game at
  a time" decision in `CLAUDE.md` (and its out-of-scope list), so those lines
  change first. It means one poller per game, `state.json` and `last_game`
  holding a list (with a migration and a changelog note), plays tagged with
  their game, a rule for which game gets the window and which the toasts, and
  a GUI with tabs or a strip of mini headers. The waybar module is a cheap way
  in: it could rotate through live games or list them in its tooltip.

## Loose ends

- Decide on the Go module path. `go install` needs `github.com/<owner>/<repo>`
  while the module is named `playcall`; renaming the module is a tag-worthy
  change, and the old Python repository's fate is part of the same decision.
- Check window visibility on Sway and other compositors (only Hyprland is
  checked directly; elsewhere it falls back to window focus).
- Basketball's timeout and foul indicators still use the ● ○ characters; the
  football ones are drawn circles.
- Confirm college football's halftime status on a Saturday, and the Top 25
  list on a game day.
