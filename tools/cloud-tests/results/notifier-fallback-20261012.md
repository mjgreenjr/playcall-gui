# Cloud test results: notifier fallback and routing (2026-10-12)

Commit tested: `e6ee0f1`. No code was changed.

## Environment

- Step 0 passed: `which hyprctl omarchy-notification-send` printed nothing (a disposable cloud container, Ubuntu 24.04, root).
- `go version go1.24.7 linux/amd64`.
- Packages: the list in the instructions plus the extras below. Programs: dunst 1.9.2, openbox, Xvfb, `notify-send` from libnotify-bin.
- The ESPN standings fetch returned `Forbidden` (the sandbox proxy), so every run logs two harmless WARN lines about standings. The mock plays were not affected.

## Steps 2 and 4

- `go vet ./... && go build ./... && go test ./...`: all pass. Both binaries build.
- **The install list in the instructions is incomplete.** The first build failed in `github.com/go-gl/glfw` with `wayland-client-core.h: No such file or directory`. GLFW compiles its X11 and Wayland backends together, so the build needs the Wayland headers even though the test runs on X11 only. Fix: add `libwayland-dev libxkbcommon-dev wayland-protocols` to the `apt-get install` line.
- Terminal feed: all three commands match the expected output.
  - The WNBA run printed `NOTE`, `HEADER`, `PLAY`, `FINAL`, exit 0, nothing on stderr.
  - `--out` run twice: 4 lines, then 8 (appended, not truncated), no stdout.
  - The NFL mock with `--out` ran until `timeout` killed it (exit 124) and wrote `NOTE`, `HEADER`, `PLAY`.
  - Only `playcall.log` was created under the cache dir (no state file).

## Step 3: results

Each scenario is a fresh `$HOME`, a fresh D-Bus session and its own Xvfb. The first four (A–D) leave the window focused. A second set (Af–Df) repeats them with an `xclock` window given focus with `xdotool` about 8 seconds in, because without that the toast path never runs (see anomaly 1). Af–Df show plays in the window while it has focus and toasts after it loses it. The 49 or so toasts per run match "unfocused" plays.

| | notify-send | daemon | WM | window focused? | Plays in window (screenshot) | Notification log lines | dunst history | Crash/hang |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| A | absent | none | none | always (61 of 61 `focused=true`) | yes | none | n/a | none |
| B | present | none | none | always (61 of 61) | yes | none | n/a | none |
| C | present | dunst | none | always (61 of 61) | yes | none | 0 entries | none |
| D | present | dunst | openbox | always (59 of 59) | yes | none | 0 entries | none |
| Af | absent | none | none | lost at ~8 s (51 `false`, 15 `true`) | yes (only the focused ones) | 1 WARN `No notification program works`; then 52 DEBUG `no notification program available` | n/a | none |
| Bf | present | none | none | lost at ~8 s (49 `false`, 15 `true`) | yes | same one WARN (`notify-send: exit status 1`); 50 DEBUG | n/a | none |
| Cf | present | dunst | none | lost at ~8 s (49 `false`, 15 `true`) | yes (focused ones only) | 49 `Sent notification with notify-send: <title>`; no WARN | 19 entries | none |
| Df | present | dunst | openbox | lost at ~8 s (49 `false`, 13 `true`) | yes (focused ones only) | 49 `Sent notification with notify-send: <title>`; no WARN | 19 entries | none |

Matching the guide:

- A and B (focused): one warning was **not** logged, because no toast was ever attempted while the window had focus. This differs from the guide, which expected the warning in A and B. With focus stolen (Af, Bf) the guide's expectation holds: exactly one `No notification program works` warning and the plays still reach the window.
- C and D: `Sent notification with notify-send` lines appear once the window loses focus. In dunst's history the toasts are 19 entries because many plays share the same title text (the title is score and clock only, so two plays at 15:04 look identical). Rows in the window and in dunst do not overlap, which matches "never both".
- D: under openbox the window did get focus (59 of 59 `window manager can't say ... focused=true`), as the guide predicted.

Screenshots (640 px wide): `notifier-fallback-20261012-shot2_A.png` (focused, window only), `..._Af.png` and `..._Bf.png` (focus on xclock, no notifier; plays still in the window), `..._Cf.png` (plays in the window, toasts went to dunst), `..._Df.png` (openbox, window plus dunst toasts on the right).

## Anomalies and notes

1. **The guide's scenarios A–D never reach the toast path.** The window gets focus on its own under Xvfb, even with no window manager, so every play goes to the window and the notifier is never called. As written, those scenarios cannot show the fallback. Fix: have the guide give another window focus (for example `xclock &` then `xdotool search --class xclock windowfocus`; add `xdotool`).
2. **Missing build packages**: see Steps 2 and 4 above.
3. **dbus starts dunst by itself.** `/usr/share/dbus-1/services/org.knopwob.dunst.service` makes any `notify-send` start dunst on demand, so a "no daemon" scenario is not really that once dunst is installed. I moved that file aside for the final runs (and put it back afterwards). The guide should say to do the same, or to install dunst only for C and D.
4. **dunst prints `CRITICAL: Cannot connect to DBus` (`dbus_cb_name_lost`) at the end of every run.** This is from the D-Bus session shutting down when the script ends, not a failure; dunst answered `GetServerInformation` and recorded the toasts while the session ran.
5. **With no notifier, each play logs noise.** In Af and Bf, after the one WARN, every unfocused play adds a DEBUG `no notification program available (...)` line, and Fyne's own `SendNotification` fallback logs two INFO lines per play (`Fyne error: Failed to send message to bus` and `Cause: The name org.freedesktop.Notifications was not provided by any .service files`). That is about 50 plays: 100 extra INFO lines in under a minute, not a loop, and nothing repeats while idle. Suggested fix: once `ErrNoNotifier` has been seen, skip the Fyne fallback too, or log its error once.
6. `window manager can't say if the window is visible; using focus err=exec: "hyprctl": ...` is logged at DEBUG on every play (about 60 per run). The guide expects this; it is only noisy with `--debug`.
7. No panic, hang or crash in any scenario, and no error repeats while idle.

## Not done

- Two real-time checks were not repeated: the toast click-to-open path (`--exec xdg-open`) does not exist for the `notify-send` fallback by design, and nothing here exercises it.
- A focused window in scenario C/D was not tried with dunst's history to confirm zero toasts while focused beyond the counts above (history was 0 in C and D before focus was stolen).
