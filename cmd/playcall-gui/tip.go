package main

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// tipDelay is how long the pointer rests on a logo before its tip appears,
// so passing over it does not flash the tip. Tests change it.
var tipDelay = 400 * time.Millisecond

// hoverTip wraps an object and shows a small text popup under it while the
// pointer rests on it. Fyne has no tooltips of its own.
type hoverTip struct {
	widget.BaseWidget
	content fyne.CanvasObject

	mu      sync.Mutex
	text    string
	timer   *time.Timer
	hovered bool
	popup   *widget.PopUp
}

func newHoverTip(content fyne.CanvasObject) *hoverTip {
	h := &hoverTip{content: content}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hoverTip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.content)
}

// SetText sets the tip; "" turns it off. A tip showing now is closed.
func (h *hoverTip) SetText(text string) {
	h.mu.Lock()
	changed := h.text != text
	h.text = text
	h.mu.Unlock()
	if changed {
		h.hide()
	}
}

// MouseIn starts the delay before the tip shows.
func (h *hoverTip) MouseIn(*desktop.MouseEvent) {
	h.mu.Lock()
	h.hovered = true
	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(tipDelay, func() { fyne.Do(h.show) })
	h.mu.Unlock()
}

func (h *hoverTip) MouseMoved(*desktop.MouseEvent) {}

// MouseOut cancels a pending tip and closes a shown one.
func (h *hoverTip) MouseOut() {
	h.mu.Lock()
	h.hovered = false
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	h.mu.Unlock()
	h.hide()
}

// show opens the popup under the logo, kept inside the window.
func (h *hoverTip) show() {
	h.mu.Lock()
	text, hovered := h.text, h.hovered
	h.mu.Unlock()
	if text == "" || !hovered || h.popup != nil {
		return
	}
	app := fyne.CurrentApp()
	canvas := app.Driver().CanvasForObject(h)
	if canvas == nil {
		return
	}
	label := widget.NewLabel(text)
	popup := widget.NewPopUp(label, canvas)
	pos := app.Driver().AbsolutePositionForObject(h).Add(fyne.NewPos(0, h.Size().Height))
	if over := pos.X + popup.MinSize().Width - canvas.Size().Width; over > 0 {
		pos.X -= over // the home logo is at the right edge
	}
	popup.ShowAtPosition(pos)
	h.popup = popup
}

func (h *hoverTip) hide() {
	if h.popup != nil {
		h.popup.Hide()
		h.popup = nil
	}
}

// Shown is the text of the tip on screen, "" if none (for tests).
func (h *hoverTip) shown() string {
	if h.popup == nil {
		return ""
	}
	return h.text
}
