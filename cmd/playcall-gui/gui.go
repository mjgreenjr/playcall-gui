package main

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"playcall/internal/core"
)

// ui is the window: a header that always shows the game's state, a list of
// plays newest first, and an "N new" button when plays arrive while the user
// has scrolled away from the top.
type ui struct {
	interval time.Duration

	model  Model
	status core.GameStatus

	away, home         *canvas.Text
	awayBall, homeBall *canvas.Image
	statusText         *widget.Label
	series             *widget.Label
	stale              *widget.Label

	rows   *fyne.Container
	scroll *container.Scroll
	newBtn *widget.Button
	unseen int

	content fyne.CanvasObject
}

func newUI(interval time.Duration, onBack func()) *ui {
	u := &ui{interval: interval}
	big := func() *canvas.Text {
		t := canvas.NewText("", theme.Color(theme.ColorNameForeground))
		t.TextSize = 30
		t.TextStyle = fyne.TextStyle{Bold: true}
		t.Alignment = fyne.TextAlignCenter
		return t
	}
	u.away, u.home = big(), big()
	ball := func() *canvas.Image {
		img := canvas.NewImageFromResource(footballRes)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(30, 30))
		img.Translucency = 1 // invisible but keeps its space, so the score does not shift
		return img
	}
	u.awayBall, u.homeBall = ball(), ball()
	u.statusText = widget.NewLabelWithStyle("waiting for ESPN...", fyne.TextAlignCenter, fyne.TextStyle{})
	u.series = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	u.stale = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	u.stale.Importance = widget.WarningImportance
	u.series.Hide() // shown only when there is something to say
	u.stale.Hide()

	var back fyne.CanvasObject = layout.NewSpacer()
	if onBack != nil {
		b := widget.NewButtonWithIcon("Games", theme.NavigateBackIcon(), onBack)
		back = container.NewHBox(b, layout.NewSpacer())
	}
	header := container.NewVBox(
		back,
		container.NewGridWithColumns(2,
			container.NewCenter(container.NewHBox(u.awayBall, u.away)),
			container.NewCenter(container.NewHBox(u.homeBall, u.home))),
		u.statusText, u.series, u.stale,
		widget.NewSeparator(),
	)

	u.rows = container.NewVBox()
	u.scroll = container.NewVScroll(u.rows)
	u.scroll.OnScrolled = func(p fyne.Position) {
		if p.Y <= 0 {
			u.clearUnseen()
		}
	}
	u.newBtn = widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		u.scroll.ScrollToTop()
		u.clearUnseen()
	})
	u.newBtn.Hide()

	u.content = container.NewBorder(container.NewVBox(header, u.newBtn), nil, nil, nil, u.scroll)
	return u
}

func (u *ui) clearUnseen() {
	u.unseen = 0
	u.newBtn.Hide()
}

func (u *ui) setStatus(s core.GameStatus) {
	u.status = s
	u.away.Text = fmt.Sprintf("%s  %s", s.Away.Abbr, s.Away.Score)
	u.home.Text = fmt.Sprintf("%s  %s", s.Home.Abbr, s.Home.Score)
	u.away.Refresh()
	u.home.Refresh()
	u.statusText.SetText(statusLine(s))
	u.setBalls(s)
	if s.Series != "" {
		u.series.SetText("Series: " + s.Series)
		u.series.Show()
	} else {
		u.series.Hide()
	}
	u.refreshStale()
}

// setBalls puts the sport's ball beside the team on offence.
func (u *ui) setBalls(s core.GameStatus) {
	res := ballFor(s.Sport)
	for side, img := range map[string]*canvas.Image{"away": u.awayBall, "home": u.homeBall} {
		if res != nil {
			img.Resource = res
		}
		if res != nil && s.Possession == side {
			img.Translucency = 0
		} else {
			img.Translucency = 1
		}
		img.Refresh()
	}
}

// setError shows a problem where the status goes.
func (u *ui) setError(msg string) {
	u.statusText.SetText(msg)
	u.statusText.Importance = widget.DangerImportance
	u.statusText.Refresh()
}

func (u *ui) refreshStale() {
	if text := staleText(u.status.UpdatedAt, time.Now(), u.interval); text != "" {
		u.stale.SetText(text)
		u.stale.Show()
	} else {
		u.stale.Hide()
	}
}

// newRow renders one play: its text, bold for urgent plays. The score and
// clock live in the header, not on every row.
func newRow(r Row) fyne.CanvasObject {
	text := r.Body
	if r.Updated {
		text += "  (updated)"
	}
	body := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: r.Urgent || r.Final})
	body.Wrapping = fyne.TextWrapWord
	if r.Urgent {
		body.Importance = widget.HighImportance
	}
	return container.NewVBox(body, widget.NewSeparator())
}

func (u *ui) addEvent(e core.Event) {
	atTop := u.scroll.Offset.Y <= 0
	added := u.model.Apply(e)
	if !added {
		for i, r := range u.model.Rows {
			if r.PlayID == e.PlayID {
				u.rows.Objects[i] = newRow(r)
			}
		}
		u.rows.Refresh()
		return
	}
	row := newRow(u.model.Rows[0])
	u.rows.Objects = append([]fyne.CanvasObject{row}, u.rows.Objects...)
	u.rows.Refresh()
	if atTop {
		return
	}
	// Keep what the user is reading where it is: push the offset down by the
	// height of the new row, and count it.
	row.Resize(fyne.NewSize(u.scroll.Size().Width, row.MinSize().Height))
	h := row.MinSize().Height + theme.Padding()
	u.scroll.Offset.Y += h
	u.scroll.Refresh()
	u.unseen++
	u.newBtn.SetText(fmt.Sprintf("%d new", u.unseen))
	u.newBtn.Show()
}

// guiSink feeds the poller's output to the window on the UI goroutine.
type guiSink struct{ ui *ui }

func (g guiSink) Send(_ context.Context, e core.Event) error {
	fyne.Do(func() { g.ui.addEvent(e) })
	return nil
}

func (g guiSink) Status(s core.GameStatus) {
	fyne.Do(func() { g.ui.setStatus(s) })
}

var _ core.Sink = guiSink{}
var _ core.StatusSink = guiSink{}
