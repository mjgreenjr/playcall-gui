#!/bin/bash
INPUT=$(cat)

# Always run from the project root, whatever directory the session is in
cd "$(dirname "$0")/../.." || exit 3

# CRITICAL: prevent infinite loop
# If a previous Stop hook already forced continuation, allow the stop.
if [ "$(echo "$INPUT" | jq -r '.stop_hook_active')" = "true" ]; then
  exit 0
fi

# Nothing to verify until the Go module exists and has some Go code
[ -f go.mod ] || exit 0
[ -n "$(find . -name '*.go' -not -path './reference/*' -print -quit)" ] || exit 0

# Auto-fix formatting (the Python reference is not Go, so it is not touched)
find . -name '*.go' -not -path './reference/*' -print0 | xargs -0 -r gofmt -w

# Skip verification when the Go code and test data are identical to the last passing run
HASH_FILE=.claude/.verified_hash
CURRENT=$(find . -path ./.git -prune -o -type f \
  \( -name '*.go' -o -name go.mod -o -name go.sum -o -path './test/*' \) -print \
  | sort | xargs -r cat | sha256sum)
if [ "$CURRENT" = "$(cat "$HASH_FILE" 2>/dev/null)" ]; then
  exit 0
fi

FAIL=0
OUTPUT=""
OUTPUT+=$(go vet ./... 2>&1) || FAIL=1
OUTPUT+=$(go build ./... 2>&1) || FAIL=1
OUTPUT+=$(go test ./... 2>&1) || FAIL=1

if [ $FAIL -ne 0 ]; then
  echo "$OUTPUT" >&2
  echo "Verification failed. Fix the errors and try again." >&2
  exit 2
fi

echo "$CURRENT" > "$HASH_FILE"
exit 0
