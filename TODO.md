# TODO:

## Future ideas:

- Once the cbb season starts, check whether `list --sport cbb` needs `groups=50` sent by default (offseason, ESPN returned the same 43 games with or without it; cfb needs 80 or it only returns the top 25)
- Make it work outside Omarchy (Linux first; the OS-specific code is toast.go, visible_linux.go, cmd/playcall/resume.go and paths.go):
  - Done in 0.3.0 work: the `notify-send` fallback and focus-based routing without Hyprland. Left: Sway's own check through `swaymsg -t get_tree` (GNOME and KDE hide workspaces from Wayland clients).
  - README: stop saying it needs Omarchy once these are in.
- Windows port (not started; can't be tested from this machine, and CLAUDE.md lists Windows as out of scope):
  - Build: Fyne needs a C compiler (MinGW/MSYS2); the CLI cross-compiles with `GOOS=windows`.
  - Toasts: Fyne's `app.SendNotification` (no click action) or a library such as `go-toast` (Windows only, has click actions).
  - Window visibility: no workspace API; use focus or minimized state.
  - Opening URLs: `rundll32 url.dll,FileProtocolHandler` or Fyne's `OpenURL`.
  - Autostart: a registry Run key or a Startup-folder shortcut, in place of systemd.
  - Paths: `os.UserConfigDir` and `os.UserCacheDir` (`%AppData%`, `%LocalAppData%`).
- League logos: show the league's logo (NFL, NBA, MLB, ...) in the picker's sport selector and/or the game header. ESPN's scoreboard has a `leagues[].logos` entry per league; check which variants it gives (light and dark) and cache them like the team logos.
