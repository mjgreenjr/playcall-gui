# Cloud test results, run 2: notifier fallback and routing (2026-10-12)

Commit tested: `e752e5b` (origin/master, with the revised instructions and the
change that drops Fyne's own notification). No code was changed. Same container
as run 1 (packages already installed).

## Setup notes

- Step 0 passed: `which hyprctl omarchy-notification-send` printed nothing; `go1.24.7 linux/amd64`.
- The revised install list already had the Wayland and `xdotool` packages; nothing new was needed.
- `go vet ./... && go build ./... && go test ./...` pass; both binaries build.
- I moved `org.knopwob.dunst.service` aside for A, B and E and put it back for C and D, as the file says. It is back in place now.
- Local `master` in this container had diverged from `origin/master` (3 commits ahead, 53 behind), so `git pull` refused. I did not touch it and branched from `origin/master` instead.
- Standings fetch is blocked by the sandbox (`Forbidden`), so two harmless standings WARN lines appear in each run.
- I set `XDG_RUNTIME_DIR` to a private directory per scenario (the harness has none).
- Each scenario ran two phases: window focused for ~8 s, then `xclock` focused with `xdotool` (~25 s). Counts below are split at the switch time written to a file.

## Results

"Plays" is the number of `Play kind=...` debug lines the poller logged in that phase.

| | Phase | Plays | Window rows | `focused=` lines | Notification lines | dunst history | Problems |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A (no notify-send, no daemon, no WM) | focused | 13 | yes | 13 `true` | none | n/a | none |
| | unfocused | 48 | yes | 46 `false` (+2 `true` at the switch second) | 1 WARN `No notification program works`; 46 DEBUG `no notification program available` | n/a | none |
| B (notify-send, no daemon, no WM) | focused | 15 | yes | 15 `true` | none | n/a | none |
| | unfocused | 46 | yes | 44 `false` (+2 `true`) | 1 WARN (`notify-send: exit status 1`); 44 DEBUG | n/a | none |
| C (notify-send, dunst, no WM) | focused | 17 | yes | 17 `true` | none | | none |
| | unfocused | 44 | yes | 44 `false` | 44 `Sent notification with notify-send: <title>`; no WARN | 19 entries | none |
| D (as C plus openbox) | focused | 17 | yes | 17 `true` | none | | none |
| | unfocused | 44 | yes | 44 `false` | 44 `Sent notification with notify-send: <title>`; no WARN | 19 entries | none |
| E (no notify-send, dunst, no WM) | focused | 15 | yes | 15 `true` | none | | none |
| | unfocused | 46 | yes | 46 `false` | 1 WARN `No notification program works` (`notify-send: exec: ... not found`); 46 DEBUG | 0 entries | none |

Across all scenarios: zero `Fyne error` lines, zero `Failed to send` lines, no panic, no crash, no hang, and no error that repeats on a timer. The extra lines in A, B and E are one DEBUG per play, not a loop.

## What matches the guide

- Focused phase: every play goes to the window, log says `focused=true`, nothing is toasted.
- A, B and E: one `No notification program works` warning, plays still in the window, no `Fyne error` lines, no retry spam. E's dunst history is empty because `notify-send` is absent.
- C and D: `Sent notification with notify-send` for every unfocused play. D's window kept focus under openbox until `xclock` took it (`playcall - NHL Hockey 999` title bar visible in `..._run2-shot1_D.png`).
- Window rows: the screenshots for C, D and E show the same list, and rows that were toasted (for example `Brenden Dillon (NJ) Tripping against John Marino (UTA) · 2 min Minor` and `Goalie Stopped`) are in both dunst's history and the window, as the guide says. I could not count rows exactly because the list is scrolled.

## Differences and notes

1. **dunst's history shows 19 entries, not 44.** I believe dunst's default history length (20) caps it: 44 toasts were sent, so older ones are dropped. In run 1 I wrongly put this down to repeated titles. I did not check `dunstrc` (no config file exists under `/etc/dunst`), so this is an inference.
2. **A and B show two `focused=true` lines after the recorded switch time.** The switch time is written one second before `xclock` actually holds focus; this is timing, not routing.
3. **The "no daemon" B case needs the dunst service file moved aside**, as the guide now says; with it in place `notify-send` would start dunst.
4. **Local `master` in the cloud container had diverged**, which broke the `git pull` step; it is an environment issue, not a repo one. A `git fetch` and a branch from `origin/master` was enough.

## Step 4

All three commands match the guide:
- WNBA run: `NOTE`, `HEADER`, `PLAY`, `FINAL`, exit 0, nothing on stderr.
- `--out` twice: 4 lines, then 8 (appended), no stdout.
- NFL mock with `--out`: runs until `timeout` (exit 124); wrote `NOTE`, `HEADER`, `PLAY`. Only `playcall.log` was created under the cache dir.

## Screenshots

`notifier-fallback-20261012-run2-shot2_A.png` (unfocused, no notifier: plays still in the window, no toasts), `..._shot2_C.png` (window plus dunst toasts), `..._shot2_D.png` (openbox: window, xclock on top, dunst toasts), `..._shot2_E.png` (notify-send absent, dunst running: window only, nothing toasted), `..._shot1_D.png` (focused phase under openbox).
