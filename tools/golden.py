#!/usr/bin/env python3
"""Generate replay golden files by running the real poll_once() over each fixture.

A fixture is replayed as a live game: poll i sees only the first min(i * chunk, N) plays
(N = number of plays extract_plays() finds, counted in its order). State is carried
between polls in memory, so catch-up limits, skipped plays, NBA miss hold-back,
sent_ids and scoring-text updates all go through the real code. A final game reports
"In Progress" until the last poll, so the final toast only appears at the end.

Usage: tools/golden.py [outdir]   (default test/golden)
Writes replay_{fixture}_c{chunk}.txt for chunk sizes 3 and 25, and
replay_{fixture}_update.txt for fixtures that have a scoring play.
"""
import contextlib
import copy
import importlib.machinery
import importlib.util
import io
import logging
import os
import sys
import tempfile
import types

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
# The script writes config, state and logs under HOME; keep that out of the real one
os.environ["HOME"] = tempfile.mkdtemp(prefix="playcall-golden-")

loader = importlib.machinery.SourceFileLoader("playcall_mod", os.path.join(ROOT, "reference", "playcall.py"))
spec = importlib.util.spec_from_loader("playcall_mod", loader)
pc = importlib.util.module_from_spec(spec)
loader.exec_module(pc)
logging.disable(logging.CRITICAL)
pc.save_state = lambda state: None
pc.clear_state = lambda: None

FIXTURES = [
    ("nfl_multi", "nfl", "playbyplay_multi_plays.json"),
    ("nfl_final", "nfl", "playbyplay_final.json"),
    ("nba", "nba", "playbyplay_nba_multi_plays.json"),
    ("cfb", "cfb", "playbyplay_cfb_multi_plays.json"),
    ("wnba", "wnba", "playbyplay_wnba_multi_plays.json"),
    ("mlb", "mlb", "playbyplay_mlb_multi_plays.json"),
    ("nhl", "nhl", "playbyplay_nhl_multi_plays.json"),
]


def play_lists(data):
    """The play lists inside `data`, in the order extract_plays() reads them."""
    gp = data.get("gamepackageJSON", {})
    lists = [data.get("plays", []), gp.get("playback", {}).get("plays", [])]
    drives = data.get("drives") or gp.get("drives") or {}
    if isinstance(drives, dict):
        drive_list = list(drives.get("previous", []))
        if drives.get("current"):
            drive_list.append(drives["current"])
    else:
        drive_list = drives
    lists.extend(d.get("plays", []) for d in drive_list)
    return lists


def reveal(data, k, final_now):
    """Copy of `data` with only the first k plays, and not final unless final_now."""
    d = copy.deepcopy(data)
    remaining = k
    for lst in play_lists(d):
        keep = lst[:remaining]
        remaining -= len(keep)
        lst[:] = keep
    if not final_now and pc.is_final(d):
        status = pc.get_status(d)
        status["type"] = dict(
            status.get("type", {}),
            name="STATUS_IN_PROGRESS",
            shortDetail="In Progress",
            state="in",
        )
    return d


def replay(sport, polls):
    """polls: list of feeds (dicts). Returns the transcript text."""
    args = types.SimpleNamespace(dry_run=True, once=False, mock=None)
    state = {"game_id": "999", "sport": sport}
    out = []
    for i, (label, feed) in enumerate(polls, 1):
        pc.fetch = lambda url, mock_file=None, feed=feed: feed
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            result = pc.poll_once(state, args)
        out.append(f"== poll {i}: {label} ==")
        out.append(buf.getvalue().rstrip("\n"))
        out.append(
            f"-> returned {result}; last_play_id={state.get('last_play_id')} "
            f"sent_ids={len(state.get('sent_ids', []))} "
            f"scoring_texts={len(state.get('scoring_texts', {}))}"
        )
    return "\n".join(out) + "\n"


def chunked(data, chunk):
    n = len(pc.extract_plays(data))
    counts = list(range(chunk, n, chunk)) + [n]
    return [
        (f"revealed {k}/{n}", reveal(data, k, final_now=(k == n))) for k in counts
    ]


def update_polls(data):
    """Full feed once, then the same feed with the first scoring play's text extended
    (a PAT result added), then unchanged again. None if there is no scoring play."""
    n = len(pc.extract_plays(data))
    full = reveal(data, n, final_now=False)
    changed = copy.deepcopy(full)
    for lst in play_lists(changed):
        for p in lst:
            if p.get("scoringPlay"):
                p["text"] = p.get("text", "") + " (extra point is good)"
                return [
                    (f"revealed {n}/{n}", full),
                    (f"revealed {n}/{n}, scoring play {p['id']} text changed", changed),
                    (f"revealed {n}/{n}, text unchanged", changed),
                ]
    return None


def main():
    import json

    outdir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "test", "golden")
    os.makedirs(outdir, exist_ok=True)
    for name, sport, fixture in FIXTURES:
        with open(os.path.join(ROOT, "test", "fixtures", fixture), encoding="utf-8") as f:
            data = json.load(f)
        runs = {f"c{c}": chunked(data, c) for c in (3, 25)}
        update = update_polls(data)
        if update:
            runs["update"] = update
        for suffix, polls in runs.items():
            path = os.path.join(outdir, f"replay_{name}_{suffix}.txt")
            with open(path, "w", encoding="utf-8") as f:
                f.write(replay(sport, polls))
            print(path, len(polls), "polls")


if __name__ == "__main__":
    main()
