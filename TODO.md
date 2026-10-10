# TODO:

## Future ideas:

- Once the cbb season starts, check whether `list --sport cbb` needs `groups=50` sent by default (offseason, ESPN returned the same 43 games with or without it; cfb needs 80 or it only returns the top 25)
- Make it work outside Omarchy (Linux first; the OS-specific code is toast.go, visible_linux.go, cmd/playcall/resume.go and paths.go):
  - Toasts: fall back to `notify-send` when `omarchy-notification-send` is missing (CLAUDE.md's "never notify-send" is about Omarchy). No click-to-open with `notify-send`; that needs a notification library.
  - Routing: with no Hyprland, send everything to the window, or toast when the window is unfocused (Fyne focus events). Sway has the same data through `swaymsg -t get_tree`; GNOME and KDE hide workspaces from Wayland clients.
  - Paths: use `os.UserConfigDir` and `os.UserCacheDir` instead of the hardcoded `~/.config` and `~/.cache` (same on Linux; check the goldens and tests that assume the layout).
  - README: stop saying it needs Omarchy once these are in.
- Windows port (not started; can't be tested from this machine, and CLAUDE.md lists Windows as out of scope):
  - Build: Fyne needs a C compiler (MinGW/MSYS2); the CLI cross-compiles with `GOOS=windows`.
  - Toasts: Fyne's `app.SendNotification` (no click action) or a library such as `go-toast` (Windows only, has click actions).
  - Window visibility: no workspace API; use focus or minimized state.
  - Opening URLs: `rundll32 url.dll,FileProtocolHandler` or Fyne's `OpenURL`.
  - Autostart: a registry Run key or a Startup-folder shortcut, in place of systemd.
  - Paths: `os.UserConfigDir` and `os.UserCacheDir` (`%AppData%`, `%LocalAppData%`).
