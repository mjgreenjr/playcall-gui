---
name: toasts-and-routing
description: The toast contract (omarchy-notification-send) and the adaptive routing between the GUI window and system toasts via Hyprland. Use before changing toasts, window visibility detection or notifier code.
---

## Toast contract (ToastSink, Linux, optional)
- Use `omarchy-notification-send`. `notify-send` is only the fallback `ToastSink` runs when `omarchy-notification-send` is not installed (`exec.ErrNotFound`; no `--exec`, so no click-to-open); when neither works `Send` returns `ErrNoNotifier`, which the GUI router treats as "no toasts here" (the play goes to the window, with one WARN in the log; Fyne's own `SendNotification` was tried and dropped: its success path was never tested, and without a daemon it added about two log lines per play). Any other failure of `omarchy-notification-send` is still returned and retried.
- `--exec` must be LAST, with the command as separate words (not a quoted string).
- Example: `omarchy-notification-send -u low -t 15000 "Bills 17, Jets 14" "Mahomes 45yd TD" --exec xdg-open https://www.espn.com/nfl/game/_/gameId/401872925`
- Urgency flag: `-u normal` or `-u low` from `Event.Urgent`. Duration `-t 15000` (`NOTIF_EXPIRE_MS`). Always include `--exec xdg-open <game page url>`.
- If the command fails, `Send` returns an error so the play is retried.
- `--test-notify` sends one sample toast (title "Test", body "playcall is working") and exits.

### Adaptive routing: UI vs toast (planned)
- When the Fyne GUI is running, each play event goes to the in-app UI if our window is visible, otherwise to `ToastSink` (system toast). Never both.
- Wayland does not tell clients which workspace they are on and Fyne has no workspace-visibility API, so detect visibility through Hyprland at event time (polling per play is cheap; do not use the Hyprland event socket):
  1. `hyprctl clients -j`: find our window by PID (`os.Getpid()` matches the client's `pid`; more robust than class/title), read `workspace.id`, `mapped`, `hidden`.
  2. `hyprctl monitors -j`: collect every monitor's `activeWorkspace.id` (each monitor has its own active workspace).
  3. Visible = our workspace id is in the active set AND `mapped` AND NOT `hidden`. A `special:` (scratchpad) workspace counts as hidden.
- Fallback: if `hyprctl` is unavailable or fails, a window with focus counts as visible and one without does not (`focusTracker`, from Fyne's foreground events; it starts unfocused). Sway and others could get their own check later. Keep this behind the OS-specific notifier interface with the rest of the Linux notification code.
