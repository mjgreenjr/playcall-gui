---
name: espn-api
description: ESPN endpoints, URL parameters, the cfb group rules, and the Go notes on ESPN's JSON (tolerant types, ids, timestamps). Use before touching internal/espn, the scoreboard or summary fetching, groups/dates handling, or when adding a field read from ESPN.
---

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
  - cbb, checked 2026-10-19 (nothing scheduled until 11/1): the unfiltered scoreboard and `groups=50` ("All D-I") both answer for ESPN's default day, 11/1, which had 2 games. A conference with no game that day (`groups=1`, 7, 8, 23) is not empty-retried by us because ESPN itself skips ahead and returns its next game days (11/2 and 11/3, 7 to 16 games), while ACC and Big East, which had one game on 11/1, showed only that. So "All" can show fewer games than the conferences; it is ESPN's day boundary, not a filter in our code. `dates=20261102` gave 181 games.
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
