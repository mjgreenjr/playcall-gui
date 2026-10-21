#!/usr/bin/env python3
"""Watch live ESPN games and log what matters for the timeout and edited-play questions.

One JSON object per line in events.jsonl:
  game_start     a live game began being tracked
  text_change    a play's text changed between polls (type, old, new, scoring)
  timeout_play   a new timeout play (period, clock, team label, flagged team, previous play type)
  timeouts_used  ESPN's timeoutsUsed/timeoutsRemaining changed for a team (with the timeout plays seen so far, by team)
  kick_followup  once per game at the end: every kickoff/punt and the type of the play after it
  status_change  the status (state, name, period), the play count or the newest play changed: what ESPN's status said when each play arrived
  competitor_fields  (basketball) everything ESPN sends per competitor at the first poll
  team_stats     (basketball) boxscore fouls, technicals, flagrants, turnovers when they change
  possession     (basketball) ESPN's per-competitor possession flag when it changes
  (timeout_play and timeouts_used also cover coach's challenge plays)
  game_end       final timeoutsUsed and counts of timeout plays per team and period
Run: capture.py OUTDIR HOURS
"""
import json, os, sys, time, urllib.request, datetime

OUT = sys.argv[1]
HOURS = float(sys.argv[2])
os.makedirs(OUT, exist_ok=True)
EVENTS = os.path.join(OUT, "events.jsonl")
LOG = os.path.join(OUT, "capture.log")

# (sport path, scoreboard params, priority: lower is tracked first)
LEAGUES = [
    ("basketball/nba", "", 0, "nba"),
    ("football/college-football", "?groups=80", 1, "cfb"),
]
MAX_GAMES = 8
POLL = 20          # seconds between polls of one game
DISCOVER = 30      # seconds between scoreboard checks
PREGAME = 15 * 60  # follow a game from this long before its start, to see its status flip


def get(url):
    req = urllib.request.Request(url, headers={"User-Agent": "playcall-capture/1"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def log(msg):
    with open(LOG, "a") as f:
        f.write(f"{datetime.datetime.now():%Y-%m-%d %H:%M:%S} {msg}\n")


def emit(**kw):
    kw["t"] = f"{datetime.datetime.now():%Y-%m-%d %H:%M:%S}"
    with open(EVENTS, "a") as f:
        f.write(json.dumps(kw) + "\n")


def is_timeout(p):
    t = ((p.get("type") or {}).get("text", "")).lower()
    return "timeout" in t or "challenge" in t


def all_plays(d):
    """Every play once, in feed order (drives layout and flat layout)."""
    out, seen = [], set()
    def add(ps):
        for p in ps or []:
            if p.get("id") not in seen:
                seen.add(p.get("id"))
                out.append(p)
    add(d.get("plays"))
    dr = d.get("drives") or {}
    for x in dr.get("previous") or []:
        add((x or {}).get("plays"))
    add((dr.get("current") or {}).get("plays"))
    return out


def team_ids(d):
    comp = (d.get("header", {}).get("competitions") or [{}])[0]
    return {c["id"]: c.get("team", {}).get("abbreviation", c["id"]) for c in comp.get("competitors", [])}, comp


class Game:
    def __init__(self, sport, key, gid, label):
        self.sport, self.key, self.gid, self.label = sport, key, gid, label
        self.texts, self.timeouts_seen, self.used = {}, set(), {}
        self.first = True
        self.stats, self.poss = None, None
        self.seen_status = None

    def poll(self):
        d = get(f"https://site.api.espn.com/apis/site/v2/sports/{self.sport}/summary?event={self.gid}")
        plays = all_plays(d)
        teams, comp = team_ids(d)
        status = comp.get("status", {}).get("type", {})
        done = status.get("state") == "post"
        # what the status said as the plays arrived (pre/in/halftime lag)
        cs = comp.get("status", {})
        last = plays[-1] if plays else {}
        sig = (status.get("state"), status.get("name"), cs.get("period"), len(plays))
        if sig != self.seen_status:
            emit(kind="status_change", game=self.gid, label=self.label, sport=self.key,
                 state=status.get("state"), name=status.get("name"), period=cs.get("period"),
                 displayClock=cs.get("displayClock"), plays=len(plays),
                 last_id=last.get("id"), last_type=(last.get("type") or {}).get("text"),
                 last_period=(last.get("period") or {}).get("number"), last_clock=(last.get("clock") or {}).get("displayValue"),
                 last_wallclock=last.get("wallclock"), score={c["team"]["abbreviation"]: c.get("score") for c in comp.get("competitors", [])})
            self.seen_status = sig
        # edited plays
        for p in plays:
            pid, text = p.get("id"), p.get("text", "")
            old = self.texts.get(pid)
            if old is not None and old != text:
                emit(kind="text_change", game=self.gid, label=self.label, sport=self.key, id=pid,
                     type=(p.get("type") or {}).get("text"), scoring=bool(p.get("scoringPlay")),
                     period=(p.get("period") or {}).get("number"), old=old, new=text)
            self.texts[pid] = text
        # new timeout plays
        new_timeouts = []
        for i, p in enumerate(plays):
            if is_timeout(p) and p["id"] not in self.timeouts_seen:
                self.timeouts_seen.add(p["id"])
                prev = plays[i - 1] if i else {}
                flagged = [teams.get(t.get("id"), t.get("id")) for t in p.get("teamParticipants", []) if t.get("timeout")]
                rec = dict(id=p["id"], period=(p.get("period") or {}).get("number"),
                           clock=(p.get("clock") or {}).get("displayValue"), text=p.get("text"),
                           team=(p.get("team") or {}).get("id"), flagged=flagged,
                           prev_type=(prev.get("type") or {}).get("text"), prev_text=(prev.get("text") or "")[:80],
                           start_dd=(p.get("start") or {}).get("shortDownDistanceText"))
                new_timeouts.append(rec)
                if not self.first:
                    emit(kind="timeout_play", game=self.gid, label=self.label, sport=self.key, **rec)
        # ESPN's counters
        used = {}
        for c in comp.get("competitors", []):
            used[c["team"]["abbreviation"]] = (c.get("timeoutsUsed"), c.get("timeoutsRemaining"))
        if used != self.used:
            by_team = {}
            for p in plays:
                if is_timeout(p):
                    k = (teams.get((p.get("team") or {}).get("id")) or
                         ",".join(teams.get(t.get("id"), "?") for t in p.get("teamParticipants", []) if t.get("timeout")) or "-")
                    by_team.setdefault(k, {}).setdefault(str((p.get("period") or {}).get("number")), 0)
                    by_team[k][str((p.get("period") or {}).get("number"))] += 1
            emit(kind="timeouts_used", game=self.gid, label=self.label, sport=self.key,
                 period=comp.get("status", {}).get("period"), clock=comp.get("status", {}).get("displayClock"),
                 espn=used, timeout_plays_by_team_period=by_team)
            self.used = used
        if self.key == "nba":
            self.basketball(d, comp, teams, plays, last)
        if self.first:
            emit(kind="game_start", game=self.gid, label=self.label, sport=self.key, plays_so_far=len(plays),
                 detail=status.get("shortDetail"))
            self.first = False
        if done:
            kicks = []
            for i, p in enumerate(plays[:-1]):
                t = (p.get("type") or {}).get("text", "")
                if "kickoff" in t.lower() or "punt" in t.lower():
                    kicks.append(dict(id=p["id"], period=(p.get("period") or {}).get("number"),
                                      clock=(p.get("clock") or {}).get("displayValue"), type=t,
                                      next=(plays[i + 1].get("type") or {}).get("text")))
            if kicks:
                emit(kind="kick_followup", game=self.gid, label=self.label, sport=self.key, kicks=kicks)
            emit(kind="game_end", game=self.gid, label=self.label, sport=self.key, espn=self.used, detail=status.get("shortDetail"))
        return done


def _basketball(self, d, comp, teams, plays, last):
    st = comp.get("status", {})
    where = dict(period=st.get("period"), clock=st.get("displayClock"),
                 score={c["team"]["abbreviation"]: c.get("score") for c in comp.get("competitors", [])},
                 last_type=(last.get("type") or {}).get("text"), last_text=(last.get("text") or "")[:80])
    if self.first:
        emit(kind="competitor_fields", game=self.gid, label=self.label, sport=self.key,
             fields={c["team"]["abbreviation"]: {k: v for k, v in c.items() if k not in ("team", "leaders", "statistics", "linescores", "records")}
                     for c in comp.get("competitors", [])})
    stats = {}
    for t in (d.get("boxscore") or {}).get("teams", []):
        stats[t["team"]["abbreviation"]] = {s["name"]: s.get("displayValue") for s in t.get("statistics", [])
                                            if s.get("name") in ("fouls", "technicalFouls", "totalTechnicalFouls", "flagrantFouls", "turnovers", "totalTurnovers")}
    if stats != self.stats:
        emit(kind="team_stats", game=self.gid, label=self.label, sport=self.key, stats=stats, **where)
        self.stats = stats
    poss = {c["team"]["abbreviation"]: c.get("possession") for c in comp.get("competitors", [])}
    if poss != self.poss:
        emit(kind="possession", game=self.gid, label=self.label, sport=self.key, possession=poss, **where)
        self.poss = poss


Game.basketball = _basketball


def discover(tracked):
    found = []
    for path, params, prio, key in LEAGUES:
        try:
            sb = get(f"https://site.api.espn.com/apis/site/v2/sports/{path}/scoreboard{params}")
        except Exception as e:
            log(f"scoreboard {key}: {e}")
            continue
        for ev in sb.get("events", []):
            st = ev.get("status", {}).get("type", {})
            soon = False
            if st.get("state") == "pre":
                try:
                    start = datetime.datetime.strptime(ev["date"], "%Y-%m-%dT%H:%MZ").replace(tzinfo=datetime.timezone.utc)
                    soon = 0 <= (start - datetime.datetime.now(datetime.timezone.utc)).total_seconds() <= PREGAME
                except Exception:
                    soon = False
            if (st.get("state") == "in" or soon) and ev["id"] not in tracked:
                comp = ev["competitions"][0]["competitors"]
                label = " @ ".join(c["team"]["abbreviation"] for c in sorted(comp, key=lambda c: c["homeAway"] == "home"))
                found.append((prio, Game(path, key, ev["id"], f"{key} {label}")))
    return [g for _, g in sorted(found, key=lambda x: x[0])]


def main():
    deadline = time.time() + HOURS * 3600
    games, done_ids = {}, set()
    last_discover = 0
    log(f"started, {HOURS}h")
    while time.time() < deadline:
        now = time.time()
        if now - last_discover > DISCOVER:
            last_discover = now
            for g in discover(set(games) | done_ids):
                if len(games) < MAX_GAMES:
                    games[g.gid] = g
                    log(f"tracking {g.label} ({g.gid})")
        for gid in list(games):
            try:
                if games[gid].poll():
                    log(f"finished {games[gid].label}")
                    done_ids.add(gid)
                    del games[gid]
            except Exception as e:
                log(f"poll {games[gid].label}: {e}")
        time.sleep(POLL)
    log("deadline reached")


if __name__ == "__main__":
    main()
