#!/usr/bin/env python3
import argparse
import difflib
import json
import logging
import os
import re
import subprocess
import sys
import time
import datetime
import urllib.request
import urllib.error
import urllib.parse

# Constants
DEFAULT_INTERVAL = 10
MIN_INTERVAL = 5  # seconds; shorter intervals are raised to this
PREGAME_LEAD = 300  # seconds; before kickoff, wake this early to catch the first plays
PREGAME_MAX_SLEEP = 1800  # seconds; longest single sleep while waiting for kickoff
MAX_BACKOFF = 300  # seconds; cap for retries after failed fetches
CATCHUP_LIMIT = 5  # after a gap, announce only this many of the newest plays
# per-sport override: NHL and NBA feeds are busy and arrive in bursts
CATCHUP_LIMITS = {"nhl": 10, "nba": 10, "wnba": 10, "cbb": 10}
# Sports whose play ids and sequence numbers are not in game order, and where ESPN inserts
# plays late, in the middle of the list. The list itself is in game order, so plays are
# taken in list order, and "is new" means "id not sent yet" instead of "after the last id"
TRACK_SENT = {"nhl", "nba", "wnba", "cbb"}
# Basketball sports that share the NBA rules (cbb is assumed to match, untested)
NBA_RULES = {"nba", "wnba", "cbb"}
# Regulation periods: college basketball plays two halves, the rest four
REGULATION_PERIODS = {"cbb": 2}
# --sport value -> ESPN's {sport}/{league} path segment
SPORTS = {
    "nfl": "football/nfl",
    "nba": "basketball/nba",
    "mlb": "baseball/mlb",
    "nhl": "hockey/nhl",
    "cfb": "football/college-football",
    "cbb": "basketball/mens-college-basketball",
    "wnba": "basketball/wnba",
}
# Per-sport scoreboard groups (conferences): name -> ESPN `groups` id. The default
# group is sent even without --group (cfb: 80 is all FBS games, not just the top 25).
# A sport with no entry here does not support --group. cbb has no default: ESPN already
# returns all of D-I without `groups` (50), so it is sent only with --group.
GROUPS = {
    "cfb": {
        "All FBS": "80",
        "ACC": "1",
        "Big 12": "4",
        "Big Ten": "5",
        "SEC": "8",
        "CUSA": "12",
        "MAC": "15",
        "Mountain West": "17",
        "Sun Belt": "37",
    },
    "cbb": {
        "All D-I": "50",
        "America East": "1",
        "ACC": "2",
        "A-10": "3",
        "Big East": "4",
        "Big Sky": "5",
        "Big South": "6",
        "Big Ten": "7",
        "Big 12": "8",
        "Big West": "9",
        "CAA": "10",
        "CUSA": "11",
        "Ivy": "12",
        "MAAC": "13",
        "MAC": "14",
        "MEAC": "16",
        "MVC": "18",
        "NEC": "19",
        "OVC": "20",
        "Pac-12": "21",
        "Patriot": "22",
        "SEC": "23",
        "SoCon": "24",
        "Southland": "25",
        "SWAC": "26",
        "Sun Belt": "27",
        "WCC": "29",
        "UAC": "30",
        "Independents": "43",
        "Mountain West": "44",
        "Horizon": "45",
        "ASUN": "46",
        "Summit": "49",
        "American": "62",
    },
    # Pro leagues: conferences and divisions, short names. ESPN ids from the standings
    # endpoint (level=3); the scoreboard filter matches games with a team in the group.
    # No "All" entry, and no default: groups= is only sent with --group.
    "nfl": {
        "AFC": "8",
        "NFC": "7",
        "AFC East": "4",
        "AFC North": "12",
        "AFC South": "13",
        "AFC West": "6",
        "NFC East": "1",
        "NFC North": "10",
        "NFC South": "11",
        "NFC West": "3",
    },
    "nba": {
        "East": "5",
        "West": "6",
        "Atlantic": "1",
        "Central": "2",
        "Southeast": "9",
        "Northwest": "11",
        "Pacific": "4",
        "Southwest": "10",
    },
    "mlb": {
        "AL": "7",
        "NL": "8",
        "AL East": "1",
        "AL Central": "2",
        "AL West": "3",
        "NL East": "4",
        "NL Central": "5",
        "NL West": "6",
    },
    "nhl": {
        "East": "7",
        "West": "8",
        "Atlantic": "32",
        "Metropolitan": "33",
        "Central": "31",
        "Pacific": "30",
    },
    # No divisions.
    "wnba": {"East": "1", "West": "2"},
}
DEFAULT_GROUPS = {"cfb": "80"}
# Sports whose GROUPS start with an "All ..." entry, used as the default for `teams`
ALL_GROUP_SPORTS = ("cfb", "cbb")
DEFAULT_SPORT = "nfl"
ESPN_SCOREBOARD_URL = "https://site.api.espn.com/apis/site/v2/sports/{}/scoreboard"
# Game summary: includes play-by-play drives/plays and runs closer to live than the CDN playbyplay feed
ESPN_PLAYBYPLAY_URL = "https://site.api.espn.com/apis/site/v2/sports/{}/summary?event={}"  # format with SPORTS path, game id
ESPN_TEAMS_URL = "https://site.api.espn.com/apis/site/v2/sports/{}/teams?limit=500"
# Standings are the only ESPN endpoint that filters teams by group (the teams endpoint ignores it)
ESPN_STANDINGS_URL = "https://site.api.espn.com/apis/v2/sports/{}/standings?group={}"
GAME_PAGE_URL = "https://www.espn.com/{}/game/_/gameId/{}"  # format with sport, game id
NOTIF_CMD = "omarchy-notification-send"
NOTIF_EXPIRE_MS = 15000  # how long each toast stays on screen

# Paths
HOME_DIR = os.path.expanduser("~")
CONFIG_DIR = os.path.join(HOME_DIR, ".config", "playcall")
CONFIG_FILE = os.path.join(CONFIG_DIR, "config.json")
CACHE_DIR = os.path.join(HOME_DIR, ".cache", "playcall")
STATE_FILE = os.path.join(CACHE_DIR, "state.json")
LOG_FILE = os.path.join(CACHE_DIR, "playcall.log")
SERVICE_FILE = os.path.join(HOME_DIR, ".config", "systemd", "user", "playcall.service")

# Create the cache dir before logging opens the log file in it
os.makedirs(CACHE_DIR, exist_ok=True)

# Setup logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    handlers=[
        logging.FileHandler(LOG_FILE),
        logging.StreamHandler(sys.stdout),
    ],
)


def ensure_directories():
    """Ensure config and cache directories exist."""
    os.makedirs(CONFIG_DIR, exist_ok=True)
    os.makedirs(CACHE_DIR, exist_ok=True)


def load_config():
    """Load or create default config."""
    ensure_directories()
    if not os.path.exists(CONFIG_FILE):
        default = {
            "favorite_teams": {sport: [] for sport in SPORTS},
            "interval": DEFAULT_INTERVAL,
        }
        save_config(default)
        logging.info("Created default config at %s", CONFIG_FILE)
        return default
    with open(CONFIG_FILE, "r", encoding="utf-8") as f:
        return json.load(f)


def save_config(config):
    """Persist config to disk."""
    with open(CONFIG_FILE, "w", encoding="utf-8") as f:
        json.dump(config, f, indent=2)


_saved_state = None  # text of the last state written, so unchanged polls skip the write


def load_state():
    """Load polling state from file."""
    if not os.path.exists(STATE_FILE):
        return None
    with open(STATE_FILE, "r", encoding="utf-8") as f:
        return json.load(f)


def save_state(state):
    """Persist state if it changed since the last save."""
    global _saved_state
    text = json.dumps(state, indent=2, sort_keys=True)
    if text == _saved_state:
        return
    with open(STATE_FILE, "w", encoding="utf-8") as f:
        f.write(text)
    _saved_state = text
    logging.info(
        "State saved: game %s, last play %s",
        state.get("game_id"),
        state.get("last_play_id"),
    )


def clear_state():
    """Remove state file."""
    global _saved_state
    if os.path.exists(STATE_FILE):
        os.remove(STATE_FILE)
        logging.info("State cleared")
    _saved_state = None


def scoreboard_url(sport, date=None, group=None):
    params = {}
    if group or sport in DEFAULT_GROUPS:
        params["groups"] = group or DEFAULT_GROUPS[sport]
    if date:
        params["dates"] = date  # YYYYMMDD; ESPN ignores "date"
    url = ESPN_SCOREBOARD_URL.format(SPORTS[sport])
    return f"{url}?{urllib.parse.urlencode(params)}" if params else url


def playbyplay_url(sport, game_id):
    return ESPN_PLAYBYPLAY_URL.format(SPORTS[sport], game_id)


def game_page_url(sport, game_id):
    # espn.com's web path uses the league half of the SPORTS value (e.g. cfb ->
    # college-football), not the bare --sport flag.
    league = SPORTS[sport].split("/", 1)[1]
    return GAME_PAGE_URL.format(league, game_id)


def fetch(url, mock_file=None):
    """Fetch URL or read from mock file."""
    if mock_file:
        logging.debug("Reading mock file %s", mock_file)
        with open(mock_file, "r", encoding="utf-8") as f:
            return json.load(f)
    try:
        with urllib.request.urlopen(url, timeout=10) as resp:
            data = json.load(resp)
        logging.debug("Fetched %s", url)
        return data
    except json.JSONDecodeError:
        return None
    except urllib.error.HTTPError as e:
        if e.code == 404:
            # Game not found; treat as no plays
            return None
        logging.warning("HTTP error fetching %s: %s", url, e)
        return None
    except Exception as e:
        logging.warning("Network error fetching %s: %s", url, e)
        return None


def send_notification(title, body, exec_cmd, urgency="low", dry_run=False):
    """Send desktop notification via omarchy-notification-send. Returns True if sent."""
    if dry_run:
        print(f"[DRY-RUN] {title}\n{body}\nExec: {exec_cmd}")
        return True
    # Build command: omarchy-notification-send -u low -t 15000 "Title" "Body" --exec xdg-open url
    cmd = [
        NOTIF_CMD,
        "-u",
        urgency,
        "-t",
        str(NOTIF_EXPIRE_MS),
        title,
        body,
        "--exec",
    ] + exec_cmd.split()
    try:
        subprocess.run(cmd, check=True, timeout=30)
        logging.debug("Sent notification: %s", title)
        return True
    except (OSError, subprocess.SubprocessError) as e:
        # OSError covers a missing omarchy-notification-send binary
        logging.error("Failed to send notification: %s", e)
        return False


def format_title(away_abbr, away_score, clock, period, home_abbr, home_score):
    """Return title string: "ATL 14 · 0:48 1st · NO 0"."""
    status = f"{clock}{period}".strip()
    parts = [f"{away_abbr} {away_score}", status, f"{home_abbr} {home_score}"]
    return " · ".join(p for p in parts if p)


def parse_scoreboard_event(event):
    """Return (game_id, away_abbr, home_abbr, status, away_score, home_score),
    or None if the event has no two teams."""
    home, away = get_teams(get_competition(event))
    if home is None or away is None:
        return None
    status = get_status(event).get("type", {}).get("shortDetail", "Unknown")
    return (
        event.get("id"),
        team_abbr(away),
        team_abbr(home),
        status,
        score_text(away.get("score")),
        score_text(home.get("score")),
    )


def group_key(name):
    """Normalize a group name for matching: 'Big Ten' / 'big-ten' / 'bigten' all agree."""
    return name.lower().replace(" ", "").replace("-", "").replace("_", "")


def resolve_group(sport, value):
    """Return (name, id) for a --group value (name or id) on this sport, or exit 2."""
    groups = GROUPS.get(sport)
    if not groups:
        sys.exit(f"playcall: --group is not supported for {sport}")
    for name, gid in groups.items():
        if value == gid or group_key(value) == group_key(name):
            return name, gid
    valid = ", ".join(f"{n} ({i})" for n, i in groups.items())
    sys.exit(f"playcall: unknown {sport} group {value!r}; valid groups: {valid}")


# Scheduled-game status, e.g. "10/9 - 9:00 PM EDT" (ESPN shortDetail)
SCHEDULED_STATUS_RE = re.compile(r"^(\d{1,2})/(\d{1,2})( - )(\d{1,2})(:\d{2}.*)$")


def pad_status(status):
    """Line up scheduled start times in `list` output: pad a single-digit month with
    a leading space, a single-digit day with a trailing space and a single-digit hour
    with a leading 0 ("9/5 - 7:30 PM" -> " 9/5  - 07:30 PM").
    Other statuses (Final, "4:35 2nd") pass through unchanged."""
    m = SCHEDULED_STATUS_RE.match(status)
    if not m:
        return status
    month, day, sep, hour, rest = m.groups()
    return f"{month:>2}/{day:<2}{sep}{hour:0>2}{rest}"


def fetch_scoreboard(sport, group_id=None, allow_empty=False):
    """Fetch the scoreboard, retrying with today's date if it has no events. With a
    group, ESPN doesn't skip ahead to the next game day like the unfiltered
    scoreboard does, so a still-empty result is retried on the day the unfiltered
    scoreboard shows. Exits 1 if it can't be fetched or has no events (unless
    allow_empty)."""
    data = fetch(scoreboard_url(sport, group=group_id))
    if data is None:
        logging.error("Could not fetch scoreboard data")
        sys.exit(1)
    if not data.get("events"):
        # Attempt to fetch with today's date
        today_str = datetime.datetime.now().strftime("%Y%m%d")
        logging.debug("No events in default scoreboard; retrying for %s", today_str)
        data = fetch(scoreboard_url(sport, today_str, group_id))
    if group_id is not None and data is not None and not data.get("events"):
        day = (fetch(scoreboard_url(sport)) or {}).get("day", {}).get("date", "")
        if day:
            logging.debug("No events in group %s; retrying for %s", group_id, day)
            data = fetch(scoreboard_url(sport, day.replace("-", ""), group_id))
    if data is None or not (data.get("events") or allow_empty):
        logging.error("No events found in scoreboard data")
        sys.exit(1)
    return data


def explain_missing_team(sport, abbr):
    """Exit 1 with why no game was found: an unknown abbreviation (with close
    matches) or a real team that isn't on the current scoreboard."""
    teams = fetch_teams(sport)
    abbrs = {a.upper(): a for a, _ in teams}
    if not abbrs:
        logging.error("No %s game found for %s on the scoreboard", sport, abbr)
    elif abbr in abbrs:
        logging.error("%s has no %s game on the current scoreboard", abbr, sport)
    else:
        close = difflib.get_close_matches(abbr, list(abbrs), n=3)
        hint = f"; did you mean {', '.join(close)}?" if close else ""
        logging.error(
            "%s is not a valid %s team%s (see 'playcall teams --sport %s')",
            abbr,
            sport,
            hint,
            sport,
        )
    sys.exit(1)


def find_game_for_team(sport, abbr):
    """Return (game_id, description) for the game on the current scoreboard that has
    this team (by abbreviation). With several (e.g. a doubleheader) the live game
    wins, then an upcoming one, then a finished one; ties go to the earliest start.
    Exits 1 if the team isn't playing."""
    rank = {"in": 0, "pre": 1}
    matches = []
    for event in fetch_scoreboard(sport)["events"]:
        parsed = parse_scoreboard_event(event)
        if parsed is None or abbr not in (parsed[1].upper(), parsed[2].upper()):
            continue
        state = get_status(event).get("type", {}).get("state")
        matches.append((rank.get(state, 2), event.get("date", ""), parsed))
    if not matches:
        explain_missing_team(sport, abbr)
    _, _, (game_id, away, home, status, _, _) = min(matches, key=lambda m: m[:2])
    return game_id, f"{away} @ {home} ({status})"


def list_games(args):
    group_id = None
    header = args.sport.upper()
    if args.group is not None:
        group_name, group_id = resolve_group(args.sport, args.group)
        header += f" - {group_name}"
    print(header)
    # A group can legitimately have no games (e.g. one league in a playoff round)
    data = fetch_scoreboard(args.sport, group_id, allow_empty=group_id is not None)
    config = load_config()
    favorites = set(config.get("favorite_teams", {}).get(args.sport, []))
    parsed_games = []
    for g in data.get("events", []):
        parsed = parse_scoreboard_event(g)
        if parsed is None:
            continue
        parsed_games.append((g.get("date", ""), *parsed))
    # Pad abbreviations to the longest one on the whole scoreboard, so the
    # width is the same whether or not favorites filter the list down
    abbr_width = max(
        (len(abbr) for _, _, away, home, *_ in parsed_games for abbr in (away, home)),
        default=0,
    )
    games = []
    for (
        kickoff,
        game_id,
        away_abbr,
        home_abbr,
        status,
        away_score,
        home_score,
    ) in parsed_games:
        away_abbr = f"{away_abbr:<{abbr_width}}"
        home_abbr = f"{home_abbr:<{abbr_width}}"
        status = pad_status(status)
        games.append(
            (kickoff, game_id, away_abbr, home_abbr, status, away_score, home_score)
        )
    if not args.all and favorites:
        games = [
            g for g in games if g[2].strip() in favorites or g[3].strip() in favorites
        ]
    games.sort(key=lambda x: x[0])
    if not data.get("events"):
        print("No games scheduled.")
    for _, game_id, away_abbr, home_abbr, status, away_score, home_score in games:
        print(
            f"{game_id}  {away_abbr} @ {home_abbr}  {status}  {away_score}-{home_score}"
        )


def standings_teams(node):
    """Collect (abbr, name) for every team in a standings tree (conferences may nest)."""
    teams = [
        (e["team"].get("abbreviation", ""), e["team"].get("displayName", ""))
        for e in node.get("standings", {}).get("entries", [])
    ]
    for child in node.get("children", []):
        teams += standings_teams(child)
    return teams


def fetch_teams(sport, group=None):
    """Return [(abbr, name)] for the sport, or [] if it can't be fetched. A group (id)
    reads that group's standings; cfb and cbb default to their first, "All ...", entry."""
    if group is None and sport in ALL_GROUP_SPORTS:
        group = next(iter(GROUPS[sport].values()))
    if group is not None:
        group_id = group
        data = fetch(ESPN_STANDINGS_URL.format(SPORTS[sport], group_id))
        return standings_teams(data) if data else []
    data = fetch(ESPN_TEAMS_URL.format(SPORTS[sport]))
    teams = []
    if data:
        for league in data["sports"][0]["leagues"]:
            teams += [
                (t["team"]["abbreviation"], t["team"]["displayName"])
                for t in league["teams"]
            ]
    return teams


def list_teams(args):
    """Print every team's abbreviation and name for the sport, sorted by name."""
    group_id = None
    header = args.sport.upper()
    if args.group is not None:
        # Exits if --group is unsupported for the sport; default is the "All ..." group
        group_name, group_id = resolve_group(args.sport, args.group)
        header += f" - {group_name}"
    print(header)
    teams = fetch_teams(args.sport, group_id)
    if not teams:
        logging.error("Could not fetch team data")
        sys.exit(1)
    teams = sorted(set(teams), key=lambda t: t[1])
    width = max(len(abbr) for abbr, _ in teams)
    for abbr, name in teams:
        print(f"{abbr:<{width}}  {name}")


def systemctl(*args):
    """Run systemctl --user with args. Raises CalledProcessError on failure."""
    logging.debug("Running systemctl --user %s", " ".join(args))
    subprocess.run(["systemctl", "--user", *args], check=True)


def follow_game(args):
    game_id = args.game
    if not game_id.isdigit():
        game_id, description = find_game_for_team(args.sport, game_id)
        print(f"Found game {game_id}: {description}")
    state = {
        "game_id": game_id,
        "sport": args.sport,
        "last_play_id": None,
        "started_at": time.time(),
    }
    if args.once:
        # Single poll only: no state file write and no service change
        poll_once(state, args)
        return
    if not os.path.exists(SERVICE_FILE):
        logging.error("Systemd service file not found at %s", SERVICE_FILE)
        sys.exit(1)
    save_state(state)
    try:
        systemctl("enable", "playcall.service")
        # restart (not --now) so an already-running service picks up the new game
        systemctl("restart", "playcall.service")
    except subprocess.CalledProcessError as e:
        logging.error("Failed to enable service: %s", e)
        sys.exit(1)
    logging.info("Service enabled and restarted for game %s", game_id)
    print(f"Following game {game_id}. Use playcall stop to stop.")


def stop_service(args):
    if os.path.exists(SERVICE_FILE):
        try:
            systemctl("disable", "--now", "playcall.service")
        except subprocess.CalledProcessError as e:
            logging.error("Failed to disable service: %s", e)
            sys.exit(1)
        logging.info("Service disabled and stopped")
    else:
        logging.warning(
            "Systemd service file not found at %s; nothing to disable", SERVICE_FILE
        )
    clear_state()
    print("Stopped.")


def add_favorite(args):
    config = load_config()
    favorites = config.setdefault("favorite_teams", {}).setdefault(args.sport, [])
    if args.team in favorites:
        print(f"{args.team} is already a {args.sport} favorite.")
        return
    favorites.append(args.team)
    save_config(config)
    logging.info("Added %s to %s favorites", args.team, args.sport)
    print(f"Added {args.team} to {args.sport} favorites.")


def test_notify(args):
    title = "Test"
    body = "playcall is working"
    exec_cmd = f"xdg-open {game_page_url(DEFAULT_SPORT, '999999999')}"
    send_notification(title, body, exec_cmd, dry_run=args.dry_run)


def get_competition(data):
    """Return the competition dict (teams, scores, status) from either layout."""
    # Summary endpoint: header at top level; CDN playbyplay: under gamepackageJSON
    header = data.get("header") or data.get("gamepackageJSON", {}).get("header", {})
    comps = data.get("competitions") or header.get("competitions") or [{}]
    return comps[0]


def get_teams(comp):
    """Return (home, away) competitors matched by homeAway; a missing side is None."""
    competitors = comp.get("competitors", [])
    home = next((c for c in competitors if c.get("homeAway") == "home"), None)
    away = next((c for c in competitors if c.get("homeAway") == "away"), None)
    return home, away


def team_abbr(competitor):
    return competitor.get("team", {}).get("abbreviation", "UNK")


def get_status(data):
    """Return the game status dict from either layout."""
    return data.get("status") or get_competition(data).get("status", {})


def is_final(data):
    """True when ESPN reports the game as final."""
    type_ = get_status(data).get("type", {})
    short = (type_.get("shortDetail") or "").lower()
    return short.startswith("final") or type_.get("name") == "STATUS_FINAL"


def score_text(raw):
    """ESPN gives scores as a plain string/number or as a dict with displayValue."""
    if isinstance(raw, dict):
        return str(raw.get("displayValue") or raw.get("value") or 0)
    return str(raw if raw is not None else 0)


def extract_plays(data):
    """Collect plays from every known play-by-play layout, de-duplicated by id."""
    gamepackage = data.get("gamepackageJSON", {})
    plays = list(data.get("plays", []))
    plays.extend(gamepackage.get("playback", {}).get("plays", []))
    # Live layout: drives is {"current": {...}, "previous": [...]}
    drives = data.get("drives") or gamepackage.get("drives") or {}
    if isinstance(drives, dict):
        drive_list = list(drives.get("previous", []))
        if drives.get("current"):
            drive_list.append(drives["current"])
    else:
        drive_list = drives
    for d in drive_list:
        plays.extend(d.get("plays", []))
    unique = {}
    for p in plays:
        unique[str(p.get("id"))] = p
    return list(unique.values())


def get_play_text(p):
    """Return the play's description text from any known layout."""
    return (
        p.get("text")
        or p.get("display", {}).get("play", {}).get("text", "")
        or p.get("description", "")
    ).strip()


def build_body(p, play_text):
    """Play text framed by the pre-play and post-play down and distance, e.g.
    "1st & 10 · text · 2nd & 5". A part is left out when ESPN has no value for it
    (kickoffs have no start)."""
    parts = [
        p.get("start", {}).get("shortDownDistanceText"),
        play_text,
        p.get("end", {}).get("shortDownDistanceText"),
    ]
    return " · ".join(part for part in parts if part)


def catchup_limit(sport):
    return CATCHUP_LIMITS.get(sport, CATCHUP_LIMIT)


def unseen_plays(sport, last_id, plays, sent_ids):
    """Return plays not announced yet, oldest first. TRACK_SENT sports: those whose id
    is not in `sent_ids`, in list order. Others: those after last_id, ids compared as
    integers (they are not all the same width)."""
    if sport in TRACK_SENT:
        return [p for p in plays if str(p["id"]) not in sent_ids]
    last = None if last_id is None else int(last_id)
    return sorted(
        (p for p in plays if last is None or int(p["id"]) > last),
        key=lambda p: int(p["id"]),
    )


def play_ids(p, bundles):
    """Ids to mark as sent once play p is announced: its own, plus the missed shot that
    went out together with it (NBA)."""
    ids = {str(p["id"])}
    if str(p["id"]) in bundles:
        ids.add(str(bundles[str(p["id"])]["id"]))
    return ids


def find_scoring_updates(seen, plays, pending_ids):
    """Return scoring plays whose text differs from the text last sent for them
    (e.g. a PAT result added to the touchdown play under the same id). Plays in
    pending_ids are being sent as new plays this poll, so they are not updates."""
    return [
        p
        for p in plays
        if p.get("scoringPlay")
        and str(p["id"]) in seen
        and seen[str(p["id"])] != get_play_text(p)
        and str(p["id"]) not in pending_ids
    ]


def play_urgency(p, play_text):
    """normal for scoring plays and turnovers; kickoffs and punts are always low;
    otherwise normal if the text has a keyword, which covers plain-text fixtures."""
    if p.get("scoringPlay") or p.get("isTurnover"):
        return "normal"
    play_type = (p.get("type", {}).get("text") or "").lower()
    if "kickoff" in play_type or "punt" in play_type:
        return "low"
    keywords = ["touchdown", "field goal", "safety", "fumble", "interception"]
    return "normal" if any(k in play_text.lower() for k in keywords) else "low"


# MLB: ESPN sends every pitch and many bookkeeping plays, so MLB plays are filtered and
# described by mlb_contexts() instead of the generic title/body/urgency path.
MLB_PITCH_PREFIX = re.compile(r"^Pitch \d+\s*:\s*")
MLB_FIELDER_SUB = re.compile(
    r"^[\w.' -]+ (at|in) (pitcher|catcher|first base|second base|third base|shortstop"
    r"|left field|center field|right field|designated hitter)\.?$"
)
MLB_NORMAL_EVENT = re.compile(
    r"ran for|relieved|caught stealing|picked off|thrown out", re.IGNORECASE
)
MLB_OUT_EVENT = re.compile(r"caught stealing|picked off|thrown out", re.IGNORECASE)


def mlb_runners(p):
    """Number of runners on base on this play."""
    return sum(1 for k in ("onFirst", "onSecond", "onThird") if p.get(k))


def mlb_status(label, outs, on=None, count=None):
    """e.g. "Top 6 · 1-2, 1 out, 2 on". Count and runners are left out when None."""
    parts = []
    if count:
        parts.append(f"{count[0]}-{count[1]}")
    parts.append(f"{outs} out" if outs == 1 else f"{outs} outs")
    if on is not None:
        parts.append(f"{on} on")
    return f"{label} · " + ", ".join(parts)


def mlb_contexts(plays):
    """Walk the plays in id order and describe each one: {play_id: {announce, status,
    body, urgency, away_score, home_score}}. ESPN's `outs` is the count AFTER the at-bat
    (even on its first pitch), so outs are tracked here instead: pitches use the outs
    from the previous at-bat, and the at-bat's result sets them. Runners on pitches are
    the runners before the pitch; on a result they are the runners after it."""
    contexts = {}
    outs = on = 0
    for p in sorted(plays, key=lambda p: int(p["id"])):
        kind = p.get("type", {}).get("type", "")
        text = get_play_text(p)
        period = p.get("period", {})
        number = period.get("number")
        half = "Top" if period.get("type") == "Top" else "Bot"
        label = f"{half} {number}"
        away_score, home_score = p.get("awayScore"), p.get("homeScore")
        ctx = {
            "announce": True,
            "status": "",
            "body": text,
            "urgency": "low",
            "away_score": away_score,
            "home_score": home_score,
        }
        contexts[str(p["id"])] = ctx
        if kind == "start-inning" or kind == "end-batterpitcher":
            ctx["announce"] = False
            if kind == "start-inning":
                outs = on = 0
        elif kind == "end-inning":
            outs = on = 0
            # ESPN's "Mid"/"End" is a half-inning break: show the half that comes next
            is_mid = period.get("type") == "Mid"
            next_label = f"Bot {number}" if is_mid else f"Top {(number or 0) + 1}"
            lead = (home_score or 0) - (away_score or 0)
            game_over = (number or 0) >= 9 and (lead > 0 if is_mid else lead != 0)
            ctx["announce"] = not game_over
            ctx["status"] = mlb_status(next_label, 0)
            ctx["body"] = "Side retired"
        elif kind == "start-batterpitcher":
            on = mlb_runners(p)
            ctx["status"] = mlb_status(label, outs, on, (0, 0))
        elif text.startswith("Pitch "):
            on = mlb_runners(p)
            ctx["body"] = MLB_PITCH_PREFIX.sub("", text)
            result = p.get("resultCount") or {}
            count = (result.get("balls", 0), result.get("strikes", 0))
            # The at-bat ends on this pitch; its Play Result carries the outcome
            if re.search(r"Ball In Play|Strike 3|Ball 4", text):
                ctx["announce"] = False
            ctx["status"] = mlb_status(label, outs, on, count)
        elif p.get("type", {}).get("text") == "Stolen Base":
            # Duplicated by the Play Result with the same text
            ctx["announce"] = False
        elif kind == "play-result":
            if MLB_FIELDER_SUB.search(text):
                ctx["announce"] = False
            # At-bat results have ids ending 99xxxx; other results (substitutions,
            # steals) happen mid at-bat and their `outs` is not reliable.
            elif str(p["id"])[-6:] >= "990000":
                outs_after, on_after = p.get("outs", outs), mlb_runners(p)
                if outs_after >= 3:
                    ctx["status"] = mlb_status(label, 3)
                else:
                    ctx["status"] = mlb_status(label, outs_after, on_after)
                if p.get("scoringPlay") or outs_after > outs or on_after > on:
                    ctx["urgency"] = "normal"
                outs, on = outs_after, on_after
            else:
                if MLB_OUT_EVENT.search(text):
                    outs += 1
                ctx["status"] = mlb_status(label, outs, on)
                if p.get("scoringPlay") or MLB_NORMAL_EVENT.search(text):
                    ctx["urgency"] = "normal"
        else:
            ctx["status"] = mlb_status(label, outs, on)
            if not text:
                ctx["announce"] = False
    return contexts


# NHL: every play is announced except these (hits, and the period start/end markers, which
# the opening faceoff and closing stoppage already cover). Unknown play types are announced.
NHL_SILENT_TYPES = {"Hit", "Period Start", "Period End"}
NHL_OVERTIME_SECONDS = 300  # regular-season overtime; playoff overtime is a full period


def nhl_announced(p):
    return p.get("type", {}).get("text") not in NHL_SILENT_TYPES


def nhl_period_seconds(data, number):
    """Length of period `number`: the regulation clock, or 5 minutes for regular-season
    overtime."""
    regulation = data.get("format", {}).get("regulation", {})
    length = int(regulation.get("clock") or 1200)
    if number <= int(regulation.get("periods") or 3):
        return length
    playoffs = data.get("header", {}).get("season", {}).get("type") == 3
    return length if playoffs else NHL_OVERTIME_SECONDS


def nhl_clock_left(data, p):
    """ESPN's NHL play clock counts up from 0:00. Return it as time left in the period
    ("1:57" -> "18:03"), or ESPN's value as is if it can't be converted."""
    shown = p.get("clock", {}).get("displayValue", "")
    m = re.fullmatch(r"(\d+):(\d{2})", shown)
    number = p.get("period", {}).get("number")
    if not m or not number:
        return shown
    left = nhl_period_seconds(data, number) - int(m[1]) * 60 - int(m[2])
    return f"{left // 60}:{left % 60:02d}" if left >= 0 else shown


def nhl_body(p, play_text):
    """Play text; penalties add their length, e.g. "... Hooking against X · 2 min Minor"."""
    type_ = p.get("type", {})
    if type_.get("penaltyMinutes"):
        penalty = f"{type_['penaltyMinutes']} min {type_.get('penaltyType', '')}"
        return f"{play_text} · {penalty.strip()}"
    return play_text


def nhl_power_plays(plays, home, away):
    """Walk the plays in order and return {play mark: "home"/"away"/None}: which side
    has the power play on each play. ESPN's `strength` is relative to the play's team
    (Power Play: that team has the advantage; Shorthanded: the other team does), and
    plays with no team (stoppages) carry the previous label, so they keep the last known
    side. Penalty plays are skipped: their label is set before the penalty takes effect."""
    sides = {}
    for side, competitor in (("home", home), ("away", away)):
        if competitor is not None:
            sides[competitor.get("team", {}).get("id")] = side
    result = {}
    current = None
    for p in plays:
        label = p.get("strength", {}).get("text")
        team = p.get("team", {}).get("id")
        if not p.get("type", {}).get("penaltyMinutes"):
            if label == "Even Strength":
                current = None
            elif team in sides and label in ("Power Play", "Shorthanded"):
                other = "away" if sides[team] == "home" else "home"
                current = sides[team] if label == "Power Play" else other
        result[str(p["id"])] = current
    return result


def nhl_urgency(p):
    """normal for goals and penalties, low for everything else."""
    if p.get("scoringPlay") or p.get("type", {}).get("penaltyMinutes"):
        return "normal"
    return "low"


# NBA: every play is announced, except a turnover that only repeats the offensive foul
# logged at the same second. A missed shot goes out together with the rebound after it.
NBA_SILENT_TYPES = {"Offensive Foul Turnover"}
NBA_ORDINALS = {1: "1st", 2: "2nd", 3: "3rd", 4: "4th"}


def nba_announced(p):
    return p.get("type", {}).get("text") not in NBA_SILENT_TYPES


def nba_is_miss(p):
    """A shot (or free throw) that did not go in."""
    return bool(p.get("shootingPlay")) and not p.get("scoringPlay")


def nba_is_rebound(p):
    return "Rebound" in p.get("type", {}).get("text", "")


def nba_bundles(plays, sent_ids, finished):
    """Pair each missed shot with the rebound right after it, so both go out as one
    toast. Returns ({rebound id: miss play}, {ids of plays to hold back this poll}).
    A miss that is still the newest play is held back, because the rebound almost always
    follows in the next update; once the game is over nothing is held back."""
    bundles, held = {}, set()
    for i, p in enumerate(plays):
        if not nba_is_miss(p) or str(p["id"]) in sent_ids:
            continue
        following = plays[i + 1] if i + 1 < len(plays) else None
        if following is None:
            if not finished:
                held.add(str(p["id"]))
        elif nba_is_rebound(following) and str(following["id"]) not in sent_ids:
            bundles[str(following["id"])] = p
    return bundles, held


def nba_period(p, sport="nba"):
    """ "1st" ... "4th" ("1st", "2nd" for cbb), then "OT", "2OT", ..."""
    number = p.get("period", {}).get("number")
    if not number:
        return p.get("period", {}).get("displayValue", "")
    regulation = REGULATION_PERIODS.get(sport, 4)
    if number <= regulation:
        return NBA_ORDINALS[number]
    return "OT" if number == regulation + 1 else f"{number - regulation}OT"


def nba_body(p, bundled):
    """ESPN's text on one line (some contain a newline); a bundled miss comes first."""
    text = " ".join(get_play_text(p).split())
    if bundled is not None:
        return f"{' '.join(get_play_text(bundled).split())} · {text}"
    return text


def nba_urgency(p):
    """normal for made threes and fouls, low for everything else."""
    type_ = (p.get("type", {}).get("text") or "").lower()
    if p.get("scoringPlay") and p.get("scoreValue") == 3:
        return "normal"
    if "foul" in type_ and "turnover" not in type_:
        return "normal"
    return "low"


def send_final(data, state, args):
    """Send the end-of-game toast ("ATL 24 · Final · NO 21"). Returns True if sent."""
    home, away = get_teams(get_competition(data))
    if home is None or away is None:
        return True
    detail = get_status(data).get("type", {}).get("shortDetail") or "Final"
    title = format_title(
        team_abbr(away),
        score_text(away.get("score")),
        detail,
        "",
        team_abbr(home),
        score_text(home.get("score")),
    )
    logging.debug("Final toast for game %s: %s", state["game_id"], title)
    return send_notification(
        title,
        "Game over",
        f"xdg-open {game_page_url(state['sport'], state['game_id'])}",
        urgency="normal",
        dry_run=args.dry_run,
    )


# Seconds until a not-yet-started game begins, set by poll_once for poll_loop to sleep on
_pregame_seconds = None


def seconds_until_start(data):
    """Seconds until a scheduled ("pre") game starts, or None if it isn't scheduled
    or has no usable start time."""
    comp = get_competition(data)
    if get_status(data).get("type", {}).get("state") != "pre":
        return None
    try:
        start = datetime.datetime.strptime(comp["date"], "%Y-%m-%dT%H:%MZ")
    except (KeyError, ValueError):
        return None
    start = start.replace(tzinfo=datetime.timezone.utc)
    return (start - datetime.datetime.now(datetime.timezone.utc)).total_seconds()


def poll_once(state, args):
    """Run one poll. Returns None if the fetch failed, otherwise True once the game
    is final and everything has been sent."""
    game_id = state["game_id"]
    sport = state["sport"]
    data = fetch(playbyplay_url(sport, game_id), mock_file=args.mock)
    if data is None:
        logging.warning("Failed to fetch data for game %s", game_id)
        return None
    plays = extract_plays(data)
    finished = is_final(data)
    global _pregame_seconds
    _pregame_seconds = None
    if not plays:
        _pregame_seconds = seconds_until_start(data)
        logging.debug("No plays for game %s", game_id)
        return finished and send_final(data, state, args)
    last_id = state.get("last_play_id")
    # Ids already announced or skipped, for TRACK_SENT sports (kept in state.json)
    sent_ids = set(state.get("sent_ids", []))
    unseen = unseen_plays(sport, last_id, plays, sent_ids)
    mlb = sport == "mlb"
    nhl = sport == "nhl"
    nba = sport in NBA_RULES
    contexts = mlb_contexts(plays) if mlb else {}
    bundles = {}  # NBA: {rebound id: the missed shot announced together with it}
    # Plays that are never announced are left unseen; they stay silent every poll
    if mlb:
        unseen = [p for p in unseen if contexts[str(p["id"])]["announce"]]
    elif nhl:
        unseen = [p for p in unseen if nhl_announced(p)]
    elif nba:
        bundles, held = nba_bundles(plays, sent_ids, finished)
        absorbed = {str(miss["id"]) for miss in bundles.values()}
        unseen = [
            p
            for p in unseen
            if nba_announced(p) and str(p["id"]) not in absorbed | held
        ]
    # First poll announces only the latest play; after a gap, the newest catch-up limit
    limit = 1 if last_id is None else catchup_limit(sport)
    skipped, to_announce = unseen[:-limit], unseen[-limit:]
    if skipped:
        logging.debug("Skipping %d older play(s) for game %s", len(skipped), game_id)
        state["last_play_id"] = skipped[-1]["id"]
        if sport in TRACK_SENT:
            for p in skipped:
                sent_ids.update(play_ids(p, bundles))
            state["sent_ids"] = sorted(sent_ids)
    seen = state.setdefault("scoring_texts", {})
    pending_ids = {str(p["id"]) for p in to_announce}
    updates = find_scoring_updates(seen, plays, pending_ids)
    if mlb:
        updates = [p for p in updates if contexts[str(p["id"])]["announce"]]
    to_send_ids = pending_ids | {str(p["id"]) for p in updates}
    # Scoring plays not being sent this poll get their current text as the baseline
    for p in plays:
        if p.get("scoringPlay") and str(p["id"]) not in to_send_ids:
            seen[str(p["id"])] = get_play_text(p)
    home, away = get_teams(get_competition(data))
    if home is None or away is None:
        away_abbr = home_abbr = "UNK"
        away_score = home_score = "0"
    else:
        away_abbr, home_abbr = team_abbr(away), team_abbr(home)
        away_score = score_text(away.get("score"))
        home_score = score_text(home.get("score"))
    power_plays = nhl_power_plays(plays, home, away) if nhl else {}
    status = get_status(data)
    clock = data.get("clock") or status.get("displayClock", "")
    period = data.get("period") or status.get("displayPeriod", "")
    if period:
        period = f" {period}"
    if args.dry_run:
        print(f"[DRY-RUN] {len(to_announce)} new play(s) for game {game_id}")
        for p in to_announce:
            print(f"  {p.get('id')}: {get_play_text(p)}")
    all_sent = True
    # Re-send scoring plays that changed (they happened earlier), then new plays
    sends = [(p, True) for p in updates] + [(p, False) for p in to_announce]
    for p, is_update in sends:
        play_text = get_play_text(p)
        logging.debug(
            "%s play %s: %s", "Updated" if is_update else "New", p["id"], play_text
        )
        if mlb:
            ctx = contexts[str(p["id"])]
            title = format_title(
                away_abbr,
                away_score if ctx["away_score"] is None else ctx["away_score"],
                ctx["status"],
                "",
                home_abbr,
                home_score if ctx["home_score"] is None else ctx["home_score"],
            )
            body, urgency = ctx["body"], ctx["urgency"]
        elif nhl:
            # The play's own score and time left (ESPN's play clock counts up)
            period_name = p.get("period", {}).get("displayValue", "")
            scores = {
                "away": away_score if p.get("awayScore") is None else p["awayScore"],
                "home": home_score if p.get("homeScore") is None else p["homeScore"],
            }
            # The side with the power play gets "PP" after its score, until it ends
            power_play = power_plays.get(str(p["id"]))
            if power_play:
                scores[power_play] = f"{scores[power_play]} PP"
            title = format_title(
                away_abbr,
                scores["away"],
                f"{nhl_clock_left(data, p)} {period_name}".strip(),
                "",
                home_abbr,
                scores["home"],
            )
            body, urgency = nhl_body(p, play_text), nhl_urgency(p)
        elif nba:
            # The play's own score and clock (counts down, e.g. "31.4")
            title = format_title(
                away_abbr,
                away_score if p.get("awayScore") is None else p["awayScore"],
                f"{p.get('clock', {}).get('displayValue', '')} {nba_period(p, sport)}".strip(),
                "",
                home_abbr,
                home_score if p.get("homeScore") is None else p["homeScore"],
            )
            bundled = bundles.get(str(p["id"]))
            body = nba_body(p, bundled)
            urgency = "low" if bundled else nba_urgency(p)
        else:
            title = format_title(
                away_abbr, away_score, clock, period, home_abbr, home_score
            )
            body, urgency = build_body(p, play_text), play_urgency(p, play_text)
        exec_cmd = f"xdg-open {game_page_url(sport, game_id)}"
        sent = send_notification(
            title, body, exec_cmd, urgency=urgency, dry_run=args.dry_run
        )
        if not sent:
            # Stop here; the play stays unsent and the next poll retries it
            all_sent = False
            break
        if p.get("scoringPlay"):
            seen[str(p["id"])] = play_text
        if not is_update:
            state["last_play_id"] = p["id"]
            if sport in TRACK_SENT:
                sent_ids.update(play_ids(p, bundles))
                state["sent_ids"] = sorted(sent_ids)
        if not args.once:
            save_state(state)
    # --once is a standalone check: never persist progress
    if not args.once:
        save_state(state)
    return finished and all_sent and send_final(data, state, args)


def resolve_interval(args, config):
    """--interval beats the config file. Values below MIN_INTERVAL are raised to it."""
    value = args.interval
    if value is None:
        value = config.get("interval", DEFAULT_INTERVAL)
    try:
        value = int(value)
    except (TypeError, ValueError):
        logging.warning("Invalid interval %r; using %s", value, DEFAULT_INTERVAL)
        value = DEFAULT_INTERVAL
    if value < MIN_INTERVAL:
        logging.warning(
            "Interval %s is below the minimum; using %s", value, MIN_INTERVAL
        )
        value = MIN_INTERVAL
    return value


def finish_game(state):
    """Disable autostart and clear state after the game is final. State is kept if
    the unit can't be disabled, so the service never restarts with nothing to resume."""
    if os.path.exists(SERVICE_FILE):
        try:
            systemctl("disable", "playcall.service")
        except subprocess.CalledProcessError as e:
            logging.error("Failed to disable service: %s", e)
            return
        logging.info("Service disabled after game %s finished", state["game_id"])
    clear_state()


def poll_loop(state, args):
    interval = resolve_interval(args, load_config())
    failures = 0
    while True:
        try:
            finished = poll_once(state, args)
        except Exception:
            # Keep the service alive; the next poll retries from the saved state
            logging.exception("Unexpected error polling game %s", state["game_id"])
            finished = None
        if finished:
            logging.info("Game %s has finished. Stopping polling.", state["game_id"])
            finish_game(state)
            break
        if finished is None:
            failures += 1
            delay = min(interval * 2**failures, MAX_BACKOFF)
            logging.debug("Retrying in %s s after %d failed fetch(es)", delay, failures)
        else:
            failures = 0
            delay = interval
            if _pregame_seconds is not None:
                # Not started yet: sleep until shortly before kickoff, in capped chunks
                wait = min(_pregame_seconds - PREGAME_LEAD, PREGAME_MAX_SLEEP)
                delay = max(interval, wait)
                logging.debug(
                    "Game %s not started; sleeping %d s", state["game_id"], delay
                )
        time.sleep(delay)


def game_target_arg(value):
    """argparse type: digits are a game id; anything else is a team abbreviation."""
    return value if value.isdigit() else team_abbr_arg(value)


def sport_arg(value):
    """argparse type: --sport must be one of the supported SPORTS keys."""
    if value not in SPORTS:
        raise argparse.ArgumentTypeError(
            f"sport must be one of {', '.join(SPORTS)}, got {value!r}"
        )
    return value


def team_abbr_arg(value):
    """argparse type: team abbreviations are normalized to uppercase, e.g. buf -> BUF."""
    return value.upper()


def build_parser():
    # Shared flags go on every subparser too, so they work before or after the command.
    # SUPPRESS keeps a subparser from overwriting a value given before the command.
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument(
        "--dry-run",
        action="store_true",
        default=argparse.SUPPRESS,
        help="Print notifications instead of sending",
    )
    common.add_argument(
        "--once",
        action="store_true",
        default=argparse.SUPPRESS,
        help="Perform single poll cycle then exit",
    )
    common.add_argument(
        "--interval",
        type=int,
        default=argparse.SUPPRESS,
        help=f"Poll interval in seconds (minimum {MIN_INTERVAL})",
    )
    common.add_argument(
        "--mock",
        default=argparse.SUPPRESS,
        help="Path to mock JSON file for play-by-play",
    )
    common.add_argument(
        "--sport",
        type=sport_arg,
        default=argparse.SUPPRESS,
        help=f"Sport to follow: {', '.join(SPORTS)} (default {DEFAULT_SPORT})",
    )
    parser = argparse.ArgumentParser(
        prog="playcall",
        description="Live sports notification tool",
        epilog="Use 'playcall list --help' for list options, including --group "
        "(conference or division filter).",
        parents=[common],
    )
    sub = parser.add_subparsers(dest="command", required=True)
    group_lines = "\n\n".join(
        f"{sport}:\n" + "\n".join(f"  {n:<14} {i}" for n, i in groups.items())
        for sport, groups in GROUPS.items()
    )
    list_parser = sub.add_parser(
        "list",
        parents=[common],
        help="Show today's games (--group picks a conference or division)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="--group picks a conference or division. Valid groups per sport (name or id, "
        "name matching ignores case, spaces and hyphens):\n"
        f"{group_lines}\n"
        "Without --group, all games are listed (cfb: all FBS, 80). The output "
        "starts with a header such as 'CFB - Big Ten', or just 'NFL' without it.\n\n"
        "example: playcall list --sport nfl --group 'AFC East'",
    )
    list_parser.add_argument("--all", action="store_true", help="Show all games")
    list_parser.add_argument(
        "--group",
        help="Conference or division to list, by name or id (default all games). "
        "Valid groups are listed below.",
    )
    teams_parser = sub.add_parser(
        "teams",
        parents=[common],
        help="List team names and abbreviations for a sport",
        epilog="example: playcall teams --sport cbb --group ACC",
    )
    teams_parser.add_argument(
        "--group",
        help="Conference or division to list, by name or id (default all teams). "
        "Valid groups are shown by 'playcall list --help'.",
    )
    follow = sub.add_parser("follow", parents=[common], help="Follow a specific game")
    follow.add_argument(
        "game",
        type=game_target_arg,
        help="Game ID (digits), or a team abbreviation in a current game for --sport, e.g. BUF",
    )
    sub.add_parser("stop", parents=[common], help="Stop following a game")
    sub.add_parser("resume", parents=[common], help="Resume following from state")
    sub.add_parser("test-notify", parents=[common], help="Send a test notification")
    addfavorite = sub.add_parser(
        "addfavorite", parents=[common], help="Add a favorite team for a sport"
    )
    addfavorite.add_argument(
        "team",
        type=team_abbr_arg,
        help="Team abbreviation, e.g. BUF (quote it if it has special characters, e.g. 'TA&M')",
    )
    return parser


FLAG_DEFAULTS = {
    "dry_run": False,
    "once": False,
    "interval": None,
    "mock": None,
    "sport": DEFAULT_SPORT,
    "group": None,
}


def parse_args(argv=None):
    """Parse the command line. Flags that weren't given get their FLAG_DEFAULTS."""
    args = build_parser().parse_args(argv)
    for name, value in FLAG_DEFAULTS.items():
        if not hasattr(args, name):
            setattr(args, name, value)
    return args


def main():
    args = parse_args()

    if args.command == "list":
        list_games(args)
    elif args.command == "teams":
        list_teams(args)
    elif args.command == "follow":
        follow_game(args)
    elif args.command == "stop":
        stop_service(args)
    elif args.command == "resume":
        state = load_state()
        if not state:
            # Exit 0: a failure here would make systemd restart the unit in a loop
            logging.warning("No state to resume; nothing to do")
            return
        if args.once:
            poll_once(state, args)
        else:
            logging.info("Service started: resuming game %s", state["game_id"])
            poll_loop(state, args)
    elif args.command == "test-notify":
        test_notify(args)
    elif args.command == "addfavorite":
        add_favorite(args)
    else:
        build_parser().print_help()


if __name__ == "__main__":
    main()
