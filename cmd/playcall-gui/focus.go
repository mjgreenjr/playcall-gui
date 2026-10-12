package main

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
)

// focusTracker follows whether one of our windows has the keyboard focus, from
// Fyne's foreground events. It is the router's answer on a desktop where the
// window's workspace can't be asked (anything but Hyprland): a window in front
// of the user is visible, one that lost focus gets toasts. It starts false and
// turns true when the compositor first gives the window focus.
type focusTracker struct{ focused atomic.Bool }

// attach hooks the tracker to the app's lifecycle events.
func (f *focusTracker) attach(app fyne.App) {
	app.Lifecycle().SetOnEnteredForeground(func() { f.focused.Store(true) })
	app.Lifecycle().SetOnExitedForeground(func() { f.focused.Store(false) })
}

// has reports whether the window has focus.
func (f *focusTracker) has() bool { return f.focused.Load() }
