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

The scoreboard addresses below were only checked for an HTTP 200; what they
return has not been read.

ESPN has all of these; none of them has been looked at yet, and each probably
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

- **CFL (Canadian football).** ESPN has a scoreboard for it (`football/cfl`
  answered when checked; the contents were not examined). It is likely close
  to the NFL and college football feeds, so most of that code could carry
  over, but the rules differ and the rule-dependent parts need their own
  handling: three downs instead of four, twelve players, a longer and wider
  field, a single point (the rouge) for a missed kick that goes into the end
  zone, a three-minute warning where the NFL has two, different timeouts and
  its own overtime. The timeout dots, the down-and-distance text and the
  halftime estimate all assume the NFL and need checking.
- **Rugby.** Two codes that ESPN files separately. Rugby league has a
  scoreboard (`rugby-league/3`, the NRL, answered when checked); rugby union is
  organised by competition ids that have not been looked up yet. Scoring
  events (tries, conversions, penalty kicks, drop goals; tries are worth
  different amounts in the two codes), cards and substitutions are the likely
  events, with an 80-minute clock in two halves.
- **Australian rules football (AFL).** ESPN has a scoreboard
  (`australian-football/afl` answered when checked). Four quarters; scores are
  written as goals, behinds and a total ("12.8.80", a goal is six points and a
  behind one), so the score display and the title format need a new shape.

- **Cricket.** ESPN files it by competition id, not by league name (one id
  answered when checked, labelled "World Cup"; the list of ids has not been
  collected). Three formats with very different lengths (Twenty20, one-day and
  Test matches), scores written as runs for wickets ("250/6"), and overs
  instead of a clock; a Test match runs for days. Ball-by-ball events are the
  natural plays. Almost everything about the display would be new.
- **Motor racing.** Like golf, a race is a field of drivers ordered on a
  leaderboard, not two teams, so it shares golf's problem of needing a
  different header and event model. ESPN answered for Formula 1, NASCAR (the
  Cup, O'Reilly and Truck series), IndyCar and the NHRA; it did not for
  MotoGP, WEC or IMSA under the names tried. Likely events: lead changes,
  cautions and safety cars, pit stops, crashes and the finish. Start with one
  series, probably NASCAR or Formula 1, and treat the others as variants.

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
