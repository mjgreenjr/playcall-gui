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

## [0.4.2] - 2026-10-23

### Fixed
- NBA and WNBA: the line under a team's score shows its fouls while it has fewer than four, next to "Bonus" when it is in the bonus; a team at four or more that is not in the bonus shows neither. 0.4.1 hid the foul count whenever a team was in the bonus. College is unchanged.
- NBA: when ESPN's timeouts and fouls are missing or wrong, the timeout circles and the foul and bonus line are worked out from the plays instead of showing nothing or false values (CLE @ PHI on 2026-10-22: no counts at all, then 0 timeouts left and the bonus for both teams). The bonus in the last two minutes of a period, which comes earlier, is not covered in that case.

## [0.4.1] - 2026-10-22

### Fixed
- Basketball: once a team is in the bonus its line shows only "Bonus" ("1-and-1" or "Double bonus" in college), like a TV scoreboard, instead of the foul count and the bonus. The foul count shows until then.
- NBA and WNBA: a team's timeout circles in regulation always number 7. ESPN cuts its remaining count to 4 before the quarter starts, so a team that had taken only two showed 6 circles.
- NBA: a team's foul count under its score no longer includes offensive fouls, which the NBA does not count as team fouls (OKC @ SA: the arena board said 3, the app 4 after an offensive foul). ESPN counts them in its total but not in its own fouls-to-give. While a team is short of the bonus the count is now 4 minus ESPN's fouls-to-give, so a play ESPN retypes late no longer changes it. The WNBA and college are unchanged, since nothing shows how they count.
- The records after a final now update as soon as ESPN has them: the game page fetches the game every 20 seconds after the final, for up to 5 minutes, and stops when a record changes. Before, it checked once at 5 minutes, so the pregame records stayed on screen for that long even when ESPN had the new ones within seconds (PHI @ NY: 20 seconds).
- Basketball: the timeout circles under each team are drawn, like football's, instead of typed ● and ○ characters, so the empty ones no longer sit higher than the filled ones.

## [0.4.0] - 2026-10-18

### Fixed
- The game page checks the game once more 5 minutes after it turns final, so the teams' records catch up when ESPN updates them after the final status (BUF @ LV kept the pregame 4-1 for BUF; KENN @ MOST did the same). Polling still stops at the final, and a game opened already final is not rechecked.
- College basketball: a coach's challenge that the play text says "charged with a timeout" now takes one of the team's timeout circles. ESPN posts no timeout play for it in college games (the NBA does post one beside it, so the NBA's count was already right), so the circles showed one too many after each upheld challenge (5 of 20 games checked).
- Football: the header shows "Final" as soon as the game's End of Game row is posted, not 20 seconds or so later when ESPN's status catches up (PSU @ MICH sat on the 4th with no clock while ESPN already had the new records). The final event ("Game over") and the end of polling still wait for ESPN's status; a tied college game going to overtime is not affected.
- Football: the header shows "Halftime" with its countdown as soon as the first half's last play (the 2nd quarter's End Period, the NFL's End of Half) is posted, not 40 to 80 seconds later when ESPN's status catches up.
- Football: a play whose text says "1ST DOWN" no longer shows ESPN's down and distance after it when that says otherwise ("3rd & 1 ... 1ST DOWN · 4th & 1" after a 2-yard rush; also after a penalty that gave the first down). The header shows nothing for the situation until the next snap, which has the right one.
- College football: a timeout row is "Official timeout" or the team's by ESPN's own counter (`timeoutsUsed`) instead of by guesswork from the plays, so a row no longer contradicts the circles (a timeout right after a kickoff was charged, and a TV break filed under the offense was not, both in KENN @ MOST). Rows already in the feed when the app starts keep the old rules, since the counter's history is gone.
- Football: the header no longer sits on the start time or on "Halftime" (or "End of 1st") after a period's kickoff has been posted. ESPN posts the kickoff play before its status moves on (10 to 20 seconds at the start of the game, minutes at the half); a game whose newest play is in a later period is now shown as live in that period, with the clock from the play, and at the start of the game the score from the play and three timeouts each.
- Football: when ESPN corrects a play's text ("enforced at BUF 24" became "BUF 25", an 8-yard gain became 9) the play's row is rewritten in place, quietly (no toast, no new row, tagged "(updated)"). Before, the row kept the first wording while the header already used the corrected spot. Only plays that are not stale are rewritten (see the scoring-play entry below for what stale means).
- The game page no longer cuts the record under a team short ("1-2 (...") in football, hockey and the other sports: only baseball's fixed-width name column truncates.
- Baseball: a play ESPN adds late under a lower id than the play before it, such as "Vesia relieved Yamamoto" posted after the next batter's "pitches to" play, is now announced instead of being dropped. A "pitches to" play whose pitcher ESPN rewrites after a pitching change is corrected in place in the window (no toast).
- Baseball's game page: a play in the middle of an at-bat (a steal, a pickoff, a wild pitch) no longer resets the ball and strike circles to 0-0, and it moves the runners on the diamond to where the play leaves them. A pickoff that ends in an error ("Edman scored on pickoff error by pitcher Henderson, Betts picked off second") is no longer counted as an out in the window's outs or in the title's outs. Substitutions leave the diamond alone.

- Football and baseball: a play ESPN posts late, after plays that came later in the game (a blocked field goal that showed up after three plays of the next drive), now goes into the window's list at its place in the game, by the play's own time stamp, instead of on top. It is still announced once, with a toast if the window is hidden.

- Football: at the end of a quarter or half the header's down and distance no longer jumps to a wrong number (DEL @ MTSU showed "2nd & 10" after a 3-yard gain that left 2nd & 7), and the end-of-quarter row no longer carries one: ESPN's own figure on those rows differed from the play before in about 70 of 90 cases. The header keeps the situation after the last real play.

### Changed
- Football: a touchdown's row in the window and toasts no longer ends with ESPN's ", 1ST DOWN" (it adds it to a touchdown pass that also gained a first down); the CLI and the log keep ESPN's text.
- A scoring play whose text ESPN changes more than 5 minutes after it happened, or after the quarter, period or half-inning it was in has ended (an old home run's distance, a reworded error call that flips back and forth), is corrected in place quietly: no new alert and no toast, only the row's text changes. A real addition such as an extra point arrives within a couple of minutes. An extra point added to a touchdown within a couple of minutes is still announced as an update.
- In baseball, the batter's name under the team is blank between an at-bat's result and the announcement of the next batter (the pitcher stays), so it no longer names the batter who just walked or made an out.
- The game page's header keeps its middle (clock, count, bases) on the window's center instead of shifting with the names under the teams. In baseball the batter and pitcher names (now labeled "AB:" and "P:", not "At bat:" and "Pitcher:", with the first name as an initial: "P: Y. Yamamoto") get a fixed-width column under each team and a longer name is cut short with "..." (before, a long name widened the header and squeezed the middle).
- Baseball's game page shows the bases as a small diamond (second on top, squares turned on a corner, filled for each runner on, home plate at the bottom), and the balls, strikes and outs as rows of circles (B, S, O) beside it, filled as they pile up, the two centered together under the status line. They replace the text "1-2, 1 out, 2 on" there. Toasts, titles and the CLI keep the text.

### Added
- College football overtime shows one timeout circle per team, filled at the start of each overtime period and cleared when ESPN's counter says the team took one. Joining or restarting the GUI in the middle of an overtime period shows nothing for that period (the count at its start is unknown); the next period shows them again.
- College football: the first half's timeout totals are saved (a small `first_half.json` in the cache folder, per game, kept a week), so restarting the GUI in the second half keeps the circles. Only totals saved at halftime are used; a game the app did not see at the break still shows none.
- College football shows each team's timeouts left in the half as circles under its score, like the NFL's. It comes from ESPN's counter, which only counts charged timeouts (not the official ones ESPN files under a team), and needs the first half's total: if the app starts after halftime it shows none (unless no team has used a timeout), and overtime shows none.
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
