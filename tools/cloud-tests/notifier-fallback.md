# Cloud test: notifications and window routing without Omarchy

Instructions for a Claude Code cloud session (`claude --cloud`) on a clean
Linux machine. The goal is to run the GUI where Omarchy and Hyprland do not
exist and report what happens. **Do not change any code.** Write your report
as a file and push only that (see "Report" at the end), so the author's own
session can fetch it.

## 0. Check the environment first

Run `which hyprctl omarchy-notification-send`. Both must print nothing. If
either exists, this is not a clean machine (it is probably the author's own
desktop): stop, report that, and do nothing else. Do not install packages
system-wide on a machine that is not a disposable container or VM, and never
open a window on someone's live desktop or send real toasts.

Also run `go version` (1.24 or newer is needed) and `git log -1 --oneline`.

## 1. Install

Debian or Ubuntu names (adapt for other systems):

```sh
sudo apt-get update
sudo apt-get install -y gcc libgl1-mesa-dev libgl1-mesa-dri xorg-dev xvfb \
    dbus-x11 dunst libnotify-bin openbox x11-apps imagemagick
```

## 2. Tests and builds

```sh
go vet ./... && go build ./... && go test ./...
go build -o playcall ./cmd/playcall && go build -o playcall-gui ./cmd/playcall-gui
```

Report any failure verbatim.

## 3. The GUI under a virtual display

What is under test: when a play arrives, the app toasts it if the window is
not visible and shows it in the window if it is. If no notification program
works (`core.ErrNoNotifier`) the play goes to the window and the log gets one
`No notification program works` warning. Without Hyprland the window counts
as visible when it has keyboard focus (`cmd/playcall-gui/focus.go`).

Each scenario uses a fresh home and a fresh D-Bus session:

```sh
export HOME=$(mktemp -d)
xvfb-run -a -s "-screen 0 1280x800x24" dbus-run-session -- bash -c '
  # scenario setup goes here (start dunst, openbox, change PATH, ...)
  ./playcall-gui --debug --sport nhl \
      --mock test/fixtures/playbyplay_nhl_multi_plays.json --step 2 &
  APP=$!
  sleep 15; import -window root /tmp/shot1.png
  sleep 20; import -window root /tmp/shot2.png
  kill $APP
'
```

Look at the screenshots (you can read image files): are play rows in the
window? The log is `$HOME/.cache/playcall/playcall.log`.

Scenarios (check with `which` that the programs are really absent or present;
to make `notify-send` absent, run the app with a PATH that does not contain
it, keeping `go`, `Xvfb`, `dbus-run-session` and `import` reachable):

| | notify-send | notification daemon | window manager |
| --- | --- | --- | --- |
| A | absent | none | none |
| B | present | none | none |
| C | present | `dunst &` started before the app | none |
| D | present | `dunst &` | `openbox &` started before the app |

For each, report:
1. Do play rows appear in the window (screenshots)?
2. The log lines matching `notification`, `Sent notification`,
   `No notification program`, `window manager can't say`, `hyprctl`,
   `Failed to send`, `focused`.
3. In C and D, what `dunstctl history` shows.
4. Any panic, crash, hang, or error that repeats every few seconds.

Expected, as a guide only (describe what you actually saw): A and B give one
`No notification program works` warning, with plays still in the window and no
retry spam. C gives `Sent notification with notify-send` lines, the same plays
in dunst's history and in the window. D is like C, and when the window has
focus the log line `window manager can't say ... focused=true` shows plays
going to the window without toasts. If the window never gets focus in D, say
so.

## 4. The terminal feed

```sh
./playcall follow 999 --sport wnba --mock test/fixtures/playbyplay_wnba_multi_plays.json
./playcall follow 999 --sport wnba --mock test/fixtures/playbyplay_wnba_multi_plays.json --out /tmp/feed.txt   # twice
timeout 8 ./playcall follow 999 --sport nfl --mock test/fixtures/playbyplay_multi_plays.json --out /tmp/f2.txt
```

Expected: NOTE, HEADER, PLAY and FINAL lines for the WNBA game with nothing
else on stdout; the second `--out` run appends (8 lines, not 4); the NFL mock
keeps running until `timeout` kills it (exit 124).

## 5. Report

Write the report to `tools/cloud-tests/results/notifier-fallback-YYYYMMDD.md`
(today's date): a table of scenario against result, then each anomaly with the
exact log lines or what the screenshots show, one sentence per suggested fix
at most. Put the screenshots you looked at next to it (PNG, keep them small).

Then, on a new branch `cloud-tests/notifier-fallback` made from the current
master, commit only the files under `tools/cloud-tests/results/` and push that
branch. Do not push to master, do not open a pull request, and do not touch
any other file. If you cannot push, say so and give the whole report in your
final message instead. Either way, finish with a short summary in your final
message.
