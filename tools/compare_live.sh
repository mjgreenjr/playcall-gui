#!/bin/bash
# Run the Python reference and the Go build side by side on one game, both in dry-run
# mode with their own scratch home directories (nothing touches your real config, state
# or systemd service, and no toasts are sent), then compare the notifications.
#
# Usage: tools/compare_live.sh SPORT GAME_ID [MINUTES] [-- extra playcall args]
#   MINUTES  how long to watch (default 240, counted from when you start the script, so
#            for a game that has not started use enough to cover the wait plus the game,
#            e.g. 720). Both stop on their own when the game ends. Ctrl-C stops early
#            and still prints the comparison.
#   Example: tools/compare_live.sh mlb 401908016
#   Offline check: tools/compare_live.sh nfl 999 1 -- --mock test/fixtures/playbyplay_final.json
#
# Output goes to a new directory under /tmp (printed at the start): python.out, go.out,
# python.msgs, go.msgs (the notifications only) and the diff.
set -u

if [ $# -lt 2 ]; then
  sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
fi
SPORT=$1
GAME=$2
MINUTES=${3:-240}
shift 3 2>/dev/null || shift $#
[ "${1:-}" = "--" ] && shift
EXTRA=("$@")

ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=$(mktemp -d /tmp/playcall-compare-XXXXXX)
echo "output directory: $OUT"

(cd "$ROOT" && go build -o "$OUT/playcall-go" ./cmd/playcall) || { echo "go build failed" >&2; exit 1; }

STATE="{\"game_id\": \"$GAME\", \"sport\": \"$SPORT\", \"last_play_id\": null, \"started_at\": $(date +%s)}"
for side in python go; do
  mkdir -p "$OUT/home-$side/.cache/playcall"
  echo "$STATE" > "$OUT/home-$side/.cache/playcall/state.json"
done

cd "$ROOT"
HOME="$OUT/home-python" python3 -B reference/playcall.py resume --dry-run --interval 5 "${EXTRA[@]}" > "$OUT/python.out" 2>&1 &
PY=$!
HOME="$OUT/home-go" "$OUT/playcall-go" resume --dry-run --interval 5 "${EXTRA[@]}" > "$OUT/go.out" 2>&1 &
GO=$!
echo "started python (pid $PY) and go (pid $GO) on $SPORT game $GAME; watching up to $MINUTES minutes"

# Ctrl-C or SIGTERM ends the watch early, stops both children and still compares.
# (Background jobs in a script ignore SIGINT, so they must be stopped explicitly.)
STOP=0
trap 'STOP=1' INT TERM
END=$((SECONDS + MINUTES * 60))
while [ $STOP -eq 0 ] && [ $SECONDS -lt $END ] && { kill -0 $PY 2>/dev/null || kill -0 $GO 2>/dev/null; }; do
  sleep 1
done
trap - INT TERM
for pid in $PY $GO; do
  if kill -0 $pid 2>/dev/null; then
    kill -TERM $pid
  fi
done
wait $PY; PYCODE=$?
wait $GO; GOCODE=$?
echo "exit codes: python $PYCODE, go $GOCODE (143/0 is expected when stopped by the timeout)"

# Reduce each output to the notifications: title, body and exec lines. The per-poll
# "N new play(s)" trace lines and the log lines are left out.
python3 -B - "$OUT" <<'EOF'
import re, sys, difflib, os

out = sys.argv[1]
TRACE = re.compile(r"^\[DRY-RUN\] \d+ new play\(s\) for game ")
LOG = re.compile(r"^(?:\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:[,.]\d+)? )?(DEBUG|INFO|WARNING|WARN|ERROR) ")


def messages(path):
    msgs, current, logs = [], None, []
    for line in open(path, encoding="utf-8", errors="replace").read().splitlines():
        if TRACE.match(line):
            current = None
            continue
        if line.startswith("[DRY-RUN] "):
            current = [line[len("[DRY-RUN] "):]]
            msgs.append(current)
            continue
        if LOG.match(line):
            current = None
            level = LOG.match(line).group(1)
            if level in ("WARNING", "WARN", "ERROR"):
                logs.append(LOG.sub(lambda m: m.group(1) + " ", line, count=1))
            continue
        if line.startswith("  ") and current is None:
            continue  # indented trace line under a "N new play(s)" header
        if current is not None:
            current.append(line)
    return ["\n".join(m) for m in msgs], logs


results = {}
for side in ("python", "go"):
    msgs, logs = messages(os.path.join(out, side + ".out"))
    results[side] = msgs
    with open(os.path.join(out, side + ".msgs"), "w", encoding="utf-8") as f:
        f.write("\n--\n".join(msgs) + "\n")
    print(f"{side}: {len(msgs)} notifications, {len(logs)} warnings/errors")
    for l in logs[:10]:
        print("   ", l)

py, go = results["python"], results["go"]
if py == go:
    print("IDENTICAL: both produced the same notifications in the same order")
    sys.exit(0)

print("DIFFERENT. Unified diff of the notifications (python -> go):")
diff = difflib.unified_diff(
    "\n--\n".join(py).splitlines(), "\n--\n".join(go).splitlines(), "python", "go", lineterm="", n=2
)
text = "\n".join(diff)
with open(os.path.join(out, "diff.txt"), "w", encoding="utf-8") as f:
    f.write(text + "\n")
print(text[:6000])
if len(text) > 6000:
    print(f"... (truncated; full diff in {out}/diff.txt)")
print()
print("Reminder: a start at slightly different moments can change which play is announced first;")
print("if the only difference is at the very start, rerun both together before suspecting a bug.")
sys.exit(1)
EOF
