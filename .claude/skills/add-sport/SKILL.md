---
name: add-sport
description: Add a new ESPN sport or league (a new --sport value) to playcall. Use when asked to support another sport, league or competition.
---

# Adding a sport to playcall

This repo is the only implementation; the Python in `reference/` is frozen and is not updated. Commit on master when done and verified.

## 1. Inspect the real feed first
- Find the ESPN path (`{sport}/{league}`) and fetch a scoreboard and a summary for a real game with `curl`.
- Check: drives shape or flat top-level `plays`? Are play ids in game order? Does `sequenceNumber` match list position? Does the clock count up or down? How many periods?
- Prefer a game with a full feed. A final game works for the fixture (`--once` announces the newest play, then the final event).

## 2. Decide the notification rules WITH THE USER
The user settles per-sport rules one decision at a time (see the MLB, NHL and NBA sections of CLAUDE.md). Propose title, body, urgency, silent play types and catch-up limit, and confirm before implementing. Reuse an existing rules type when the feed matches:
- Flat basketball-style feed: `basketballRules` (set `regulation`).
- Drives shape: `footballRules`.
- Otherwise a new rules type in its own file (like `nhl.go`, `mlb.go`): walk all plays, decide per play whether it is announced and what its title, body and urgency are.
- Ids out of order or plays inserted late: `TrackSent` (list order and `sent_ids`). Busy feeds: a catch-up limit of 10.

## 3. Go touchpoints
- `internal/sports/sports.go`: `Names`, `paths`, `groups` (every sport has an entry; ids from ESPN's standings `level=3`; cfb/cbb start with an "All ..." entry) and default groups. The "sport must be one of ..." message is built from `Names`; `cmd/playcall/main_test.go` asserts that text and `internal/sports/sports_test.go` checks the `Names` list, so update both.
- `internal/core/rules.go` `RulesFor`: return the rules for the new key.
- The game page URL uses the league half of the sport path; check it.
- Config defaults pick the new key up from `sports.Names`.
- Core packages never print, exit or import Fyne.

## 4. Fixture and tests
- Save a real capture as `test/fixtures/playbyplay_{sport}_multi_plays.json` (trim only if large; note the capture date in CLAUDE.md).
- Add its command to the golden table in `cmd/playcall/main_test.go` and a row in `internal/core/replay_test.go`. There is no Python behind a new sport: build, run `./playcall follow 999 --sport {sport} --mock <fixture> --dry-run --once`, review the output by hand with the user, then save it as the golden. Keep it clearly separate from the Python-derived goldens.
- Unit tests for new pure logic (as in `nhl_test.go`, `mlb_test.go`).
- Run `/verify`, then `list` and `teams` (with and without `--group`) against the live API, and watch a live game with `--dry-run --once` (`tools/compare_live.sh` needs the Python, which has no rules for a new sport).

## 5. Docs
Update CLAUDE.md: the sport list and path mapping under API Endpoints, a rules section if the sport has its own, `--sport` mentions in Commands and Testing, and the fixture list. Say whether the sport was checked against a live feed. Do not touch `reference/`.
