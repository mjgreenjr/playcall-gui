#!/usr/bin/env python3
"""Generate golden transcripts for list, teams, follow <team> and addfavorite.

Each scenario runs the real playcall main() with fetch() replaced by a table
{url: fixture file}, the clock frozen and HOME pointing at a scratch directory. The
transcript records the command, the config, every URL requested in order (and what
answered it), stdout, stderr, the log records at INFO and above, and the exit code.

Also writes the synthetic scoreboard fixtures the scenarios use
(test/fixtures/scoreboard_syn_*.json, scoreboard_empty*.json), derived from the real
captures scoreboard_nfl_week.json and scoreboard_cfb_fbs.json.

Usage: tools/golden_cmds.py [outdir]   (default test/golden)
Writes cmd_{scenario}.txt.
"""
import contextlib
import copy
import datetime
import importlib.machinery
import importlib.util
import io
import json
import logging
import os
import sys
import tempfile
import types

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FIX = os.path.join(ROOT, "test", "fixtures")
os.environ["HOME"] = tempfile.mkdtemp(prefix="playcall-golden-")
os.chdir(ROOT)  # commands see fixtures as test/fixtures/..., so transcripts hold no absolute paths

loader = importlib.machinery.SourceFileLoader("playcall_mod", os.path.join(ROOT, "reference", "playcall.py"))
spec = importlib.util.spec_from_loader("playcall_mod", loader)
pc = importlib.util.module_from_spec(spec)
loader.exec_module(pc)

# The frozen "now" (local time), used for the date-retry URL: dates=20261007
NOW = datetime.datetime(2026, 10, 7, 12, 0, 0)


class FixedDT(datetime.datetime):
    @classmethod
    def now(cls, tz=None):
        return NOW if tz is None else NOW.replace(tzinfo=tz)


pc.datetime = types.SimpleNamespace(datetime=FixedDT, timezone=datetime.timezone)

# ---- log capture ---------------------------------------------------------------------

records = []


class Capture(logging.Handler):
    def emit(self, record):
        records.append(f"{record.levelname} {record.getMessage()}")


root = logging.getLogger()
for h in list(root.handlers):
    root.removeHandler(h)
root.addHandler(Capture(level=logging.INFO))
root.setLevel(logging.INFO)

# ---- fake fetch ----------------------------------------------------------------------

requests = []
table = {}


def fake_fetch(url, mock_file=None):
    if mock_file:
        requests.append(f"mock {mock_file}")
        with open(mock_file, encoding="utf-8") as f:
            return json.load(f)
    name = table.get(url)
    if name is None:
        requests.append(f"GET {url} -> (no data)")
        return None
    requests.append(f"GET {url} -> {name}")
    with open(os.path.join(FIX, name), encoding="utf-8") as f:
        return json.load(f)


pc.fetch = fake_fetch


def load(name):
    with open(os.path.join(FIX, name), encoding="utf-8") as f:
        return json.load(f)


def save(name, data):
    with open(os.path.join(FIX, name), "w", encoding="utf-8") as f:
        json.dump(data, f, indent=1, ensure_ascii=False)
        f.write("\n")


# ---- synthetic fixtures --------------------------------------------------------------


def make_synthetic():
    week = load("scoreboard_nfl_week.json")

    # Scheduled-time padding and a few non-time statuses
    padding = copy.deepcopy(week)
    details = [
        "9/5 - 7:30 PM EDT",
        "10/9 - 9:00 PM EDT",
        "12/25 - 12:00 PM EST",
        "1/2 - 8:15 PM EST",
        "Postponed",
        "Delayed",
        "Halftime",
    ]
    for ev, detail in zip(padding["events"], details):
        ev["status"]["type"]["shortDetail"] = detail
    save("scoreboard_syn_padding.json", padding)

    # One team in several games: live, upcoming (late and early), finished
    def clone(ev, game_id, state, detail, date):
        c = copy.deepcopy(ev)
        c["id"] = game_id
        c["date"] = date
        c["status"]["type"]["state"] = state
        c["status"]["type"]["shortDetail"] = detail
        return c

    base = week["events"][0]
    games = [
        clone(base, "900001", "post", "Final", "2026-10-04T17:00Z"),
        clone(base, "900002", "pre", "10/11 - 8:20 PM EDT", "2026-10-12T00:20Z"),
        clone(base, "900003", "in", "5:12 - 1st", "2026-10-09T00:15Z"),
        clone(base, "900004", "pre", "10/11 - 1:00 PM EDT", "2026-10-11T17:00Z"),
    ]
    dh = copy.deepcopy(week)
    dh["events"] = games
    save("scoreboard_syn_doubleheader.json", dh)
    nolive = copy.deepcopy(week)
    nolive["events"] = [games[0], games[1], games[3]]
    save("scoreboard_syn_doubleheader_nolive.json", nolive)

    # An event with only one team is skipped
    noteams = copy.deepcopy(week)
    broken = copy.deepcopy(week["events"][1])
    broken["competitions"][0]["competitors"] = broken["competitions"][0]["competitors"][:1]
    noteams["events"] = [week["events"][0], broken]
    save("scoreboard_syn_noteams.json", noteams)

    save("scoreboard_empty.json", {"events": []})


make_synthetic()

# ---- running a scenario --------------------------------------------------------------

SCOREBOARD = "https://site.api.espn.com/apis/site/v2/sports/{}/scoreboard"
TEAMS = "https://site.api.espn.com/apis/site/v2/sports/{}/teams?limit=500"
STANDINGS = "https://site.api.espn.com/apis/v2/sports/{}/standings?group={}"
PATH = pc.SPORTS


def sb(sport, **params):
    url = SCOREBOARD.format(PATH[sport])
    if params:
        url += "?" + "&".join(f"{k}={v}" for k, v in params.items())
    return url


def default_config(favorites=None):
    return {
        "favorite_teams": {sport: list((favorites or {}).get(sport, [])) for sport in pc.SPORTS},
        "interval": 10,
    }


def run(argv, responses, config=None):
    table.clear()
    table.update(responses)
    requests.clear()
    records.clear()
    os.makedirs(pc.CONFIG_DIR, exist_ok=True)
    with open(pc.CONFIG_FILE, "w", encoding="utf-8") as f:
        json.dump(config or default_config(), f, indent=2)
    out, err = io.StringIO(), io.StringIO()
    code, message = 0, None
    old_argv = sys.argv
    sys.argv = ["playcall", *argv]
    try:
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            try:
                pc.main()
            except SystemExit as e:
                if isinstance(e.code, int) or e.code is None:
                    code = e.code or 0
                else:
                    code, message = 1, str(e.code)
    finally:
        sys.argv = old_argv
    if message:
        err.write(message + "\n")
    return out.getvalue(), err.getvalue(), code


def transcript(name, argv, responses, config=None, show_config=False):
    out, err, code = run(argv, responses, config)
    lines = [f"$ playcall {' '.join(argv)}"]
    lines.append("config: " + json.dumps(config or default_config(), sort_keys=True))
    lines.append("-- requests --")
    lines += requests or ["(none)"]
    lines.append("-- stdout --")
    lines.append(out.rstrip("\n") if out else "(empty)")
    lines.append("-- stderr --")
    lines.append(err.rstrip("\n") if err else "(empty)")
    lines.append("-- log (INFO and above) --")
    lines += records or ["(none)"]
    if show_config:
        with open(pc.CONFIG_FILE, encoding="utf-8") as f:
            lines.append("-- config after (parsed, sorted) --")
            lines.append(json.dumps(json.load(f), sort_keys=True))
    lines.append(f"-- exit {code} --")
    return name, "\n".join(lines) + "\n"


def abbrs(fixture):
    """(away, home) abbreviation pairs of a scoreboard fixture, in feed order."""
    pairs = []
    for ev in load(fixture)["events"]:
        home, away = pc.get_teams(pc.get_competition(ev))
        if home is not None and away is not None:
            pairs.append((pc.team_abbr(away), pc.team_abbr(home)))
    return pairs


def scenarios():
    week = "scoreboard_nfl_week.json"
    cfb = "scoreboard_cfb_fbs.json"
    nfl_sb = sb("nfl")
    week_pairs = abbrs(week)
    cfb_pairs = abbrs(cfb)
    two_letter = next((a for p in week_pairs for a in p if len(a) == 2), week_pairs[0][0])
    fav_week = [two_letter, week_pairs[3][1]]
    fav_cfb = [cfb_pairs[0][1], cfb_pairs[5][0]]
    teams_nfl = [t["team"]["abbreviation"] for t in load("teams_nfl.json")["sports"][0]["leagues"][0]["teams"]]
    playing = {a for p in week_pairs for a in p}
    idle = next(a for a in teams_nfl if a not in playing)
    found = week_pairs[0][0]
    mock = "test/fixtures/playbyplay_multi_plays.json"

    # ---- list ----
    yield transcript("list_nfl_all", ["list", "--all"], {nfl_sb: week})
    yield transcript("list_nfl_no_favorites", ["list"], {nfl_sb: week})
    yield transcript(
        "list_nfl_favorites", ["list"], {nfl_sb: week}, default_config({"nfl": fav_week})
    )
    yield transcript(
        "list_nfl_favorites_all", ["list", "--all"], {nfl_sb: week}, default_config({"nfl": fav_week})
    )
    yield transcript("list_nfl_padding", ["list"], {nfl_sb: "scoreboard_syn_padding.json"})
    yield transcript("list_nfl_one_team_event", ["list"], {nfl_sb: "scoreboard_syn_noteams.json"})
    yield transcript("list_cfb_default", ["list", "--sport", "cfb"], {sb("cfb", groups=80): cfb})
    yield transcript(
        "list_cfb_favorites",
        ["list", "--sport", "cfb"],
        {sb("cfb", groups=80): cfb},
        default_config({"cfb": fav_cfb}),
    )
    yield transcript(
        "list_cfb_group", ["list", "--sport", "cfb", "--group", "Big Ten"], {sb("cfb", groups=5): cfb}
    )
    yield transcript(
        "list_nfl_group_name", ["list", "--group", "afc east"], {sb("nfl", groups=4): week}
    )
    yield transcript("list_nfl_group_id", ["list", "--group", "8"], {sb("nfl", groups=8): week})
    yield transcript("list_mlb", ["list", "--sport", "mlb"], {sb("mlb"): "scoreboard_mlb_day.json"})
    yield transcript("list_nba", ["list", "--sport", "nba"], {sb("nba"): "scoreboard_nba_day.json"})
    yield transcript(
        "list_retry_today",
        ["list", "--sport", "nba"],
        {sb("nba"): "scoreboard_empty.json", sb("nba", dates="20261007"): "scoreboard_nba_day.json"},
    )
    yield transcript(
        "list_group_retry_next_day",
        ["list", "--sport", "mlb", "--group", "AL"],
        {
            sb("mlb", groups=7): "scoreboard_empty.json",
            sb("mlb", groups=7, dates="20261007"): "scoreboard_empty.json",
            sb("mlb"): "scoreboard_mlb_day.json",
            sb("mlb", groups=7, dates="20261006"): "scoreboard_mlb_day.json",
        },
    )
    yield transcript(
        "list_group_no_games",
        ["list", "--sport", "mlb", "--group", "NL West"],
        {
            sb("mlb", groups=6): "scoreboard_empty.json",
            sb("mlb", groups=6, dates="20261007"): "scoreboard_empty.json",
            sb("mlb"): "scoreboard_mlb_day.json",
            sb("mlb", groups=6, dates="20261006"): "scoreboard_empty.json",
        },
    )
    yield transcript("list_group_unknown", ["list", "--group", "Nope"], {})
    yield transcript("list_fetch_failed", ["list"], {})
    yield transcript("list_all_empty", ["list"], {nfl_sb: "scoreboard_empty.json"})

    # ---- teams ----
    yield transcript("teams_nfl", ["teams"], {TEAMS.format(PATH["nfl"]): "teams_nfl.json"})
    yield transcript(
        "teams_nfl_division",
        ["teams", "--group", "AFC East"],
        {STANDINGS.format(PATH["nfl"], 4): "standings_nfl_afc_east.json"},
    )
    yield transcript(
        "teams_nfl_conference",
        ["teams", "--group", "AFC"],
        {STANDINGS.format(PATH["nfl"], 8): "standings_nfl_afc.json"},
    )
    yield transcript(
        "teams_cfb_default",
        ["teams", "--sport", "cfb"],
        {STANDINGS.format(PATH["cfb"], 80): "standings_cfb_all.json"},
    )
    yield transcript("teams_fetch_failed", ["teams"], {})
    yield transcript("teams_group_unknown", ["teams", "--group", "Nope"], {})

    # ---- follow <team> (lookup, then one dry-run poll of the mock feed) ----
    follow = ["--dry-run", "--once", "--mock", mock]
    yield transcript("follow_team_found", ["follow", found, *follow], {nfl_sb: week})
    yield transcript("follow_team_lowercase", ["follow", found.lower(), *follow], {nfl_sb: week})
    yield transcript(
        "follow_team_doubleheader_live",
        ["follow", week_pairs[0][0], *follow],
        {nfl_sb: "scoreboard_syn_doubleheader.json"},
    )
    yield transcript(
        "follow_team_doubleheader_upcoming",
        ["follow", week_pairs[0][0], *follow],
        {nfl_sb: "scoreboard_syn_doubleheader_nolive.json"},
    )
    teams_url = TEAMS.format(PATH["nfl"])
    yield transcript(
        "follow_team_no_game", ["follow", idle, *follow], {nfl_sb: week, teams_url: "teams_nfl.json"}
    )
    yield transcript(
        "follow_team_invalid_close",
        ["follow", "BUFF", *follow],
        {nfl_sb: week, teams_url: "teams_nfl.json"},
    )
    yield transcript(
        "follow_team_invalid_far",
        ["follow", "ZZZZ", *follow],
        {nfl_sb: week, teams_url: "teams_nfl.json"},
    )
    yield transcript(
        "follow_team_teams_unavailable", ["follow", idle, *follow], {nfl_sb: week}
    )
    yield transcript("follow_team_scoreboard_failed", ["follow", found, *follow], {})
    yield transcript(
        "follow_team_cfb",
        ["follow", cfb_pairs[0][1], "--sport", "cfb", *follow],
        {sb("cfb", groups=80): cfb},
    )

    # ---- addfavorite ----
    yield transcript("addfavorite_new", ["addfavorite", "buf"], {}, show_config=True)
    yield transcript(
        "addfavorite_existing",
        ["addfavorite", "BUF"],
        {},
        default_config({"nfl": ["BUF"]}),
        show_config=True,
    )
    yield transcript(
        "addfavorite_special_chars",
        ["addfavorite", "ta&m", "--sport", "cfb"],
        {},
        default_config({"cfb": ["MICH"]}),
        show_config=True,
    )
    yield transcript(
        "addfavorite_missing_sport_key",
        ["addfavorite", "lal", "--sport", "nba"],
        {},
        {"favorite_teams": {"nfl": ["BUF"]}, "interval": 10},
        show_config=True,
    )


def main():
    outdir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "test", "golden")
    os.makedirs(outdir, exist_ok=True)
    for name, text in scenarios():
        path = os.path.join(outdir, f"cmd_{name}.txt")
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        print(path)


if __name__ == "__main__":
    main()
