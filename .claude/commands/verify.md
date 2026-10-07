---
description: Run the verification sequence
allowed-tools: Bash(bash .claude/hooks/verify.sh *)
---

Run the verification sequence and report the results. No output below means it passed, or was skipped because the Go code and test data match the last passing run (delete `.claude/.verified_hash` to force a run).

```!
bash .claude/hooks/verify.sh <<< '{"stop_hook_active":false}'
```
