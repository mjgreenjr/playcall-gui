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
	hl       highlight // color of urgent plays

	model  Model
	status core.GameStatus

	away, home           *canvas.Text
	awayBadge, homeBadge *badge
	awayBall, homeBall   *canvas.Image
	logos                *logoCache
	statusText           *widget.RichText
	situation            *widget.RichText
	series               *widget.Label
	stale                *widget.Label

	rows   *fyne.Container
	scroll *container.Scroll
	newBtn *widget.Button
	unseen int

	content fyne.CanvasObject
}

func newUI(interval time.Duration, onBack func(), hl highlight, logos *logoCache) *ui {
	u := &ui{interval: interval, hl: hl, logos: logos}
	big := func() *canvas.Text {
		t := canvas.NewText("", theme.Color(theme.ColorNameForeground))
		t.TextSize = 30
		t.TextStyle = fyne.TextStyle{Bold: true}
		t.Alignment = fyne.TextAlignCenter
		return t
	}
	u.away, u.home = big(), big()
	u.awayBadge, u.homeBadge = newBadge(), newBadge()
	u.awayBall, u.homeBall = newBall(), newBall()
	u.statusText = widget.NewRichText()
	u.statusText.Wrapping = fyne.TextWrapWord
	u.situation = widget.NewRichText()
	u.situation.Wrapping = fyne.TextWrapWord
	u.setStatusLine("waiting for ESPN...", false)
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
		// One row: the ball (if away has it), logo, abbreviation and score of
		// each team with the clock and situation between; the home ball is the
		// last thing on the line.
		container.NewBorder(nil, nil,
			container.NewHBox(u.awayBall, u.awayBadge.logo, u.away),
			container.NewHBox(u.homeBadge.logo, u.home, u.homeBall),
			container.NewCenter(container.NewVBox(u.statusText, u.situation))),
		u.series, u.stale,
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
	u.setStatusLine(statusLine(s), false)
	u.setSituation(s.Situation)
	u.awayBadge.setLogo(u.logos, s.Away.Logo)
	u.homeBadge.setLogo(u.logos, s.Home.Logo)
	u.setBalls(s)
	if s.Series != "" {
		u.series.SetText("Series: " + s.Series)
		u.series.Show()
	} else {
		u.series.Hide()
	}
	u.refreshStale()
}

// setBalls puts the sport's ball at the outer edge of the team on offence.
func (u *ui) setBalls(s core.GameStatus) {
	res := ballFor(s.Sport)
	setBall(u.awayBall, res, s.Possession == "away")
	setBall(u.homeBall, res, s.Possession == "home")
}

// setError shows a problem where the status goes.
func (u *ui) setError(msg string) {
	u.setStatusLine(msg, true)
}

// setStatusLine shows the clock and period (or Halftime, Final, the start
// time), bold and centered between the scores.
func (u *ui) setStatusLine(text string, isError bool) {
	color := theme.ColorNameForeground
	if isError {
		color = theme.ColorNameError
	}
	u.statusText.Segments = []widget.RichTextSegment{&widget.TextSegment{
		Text: text,
		Style: widget.RichTextStyle{
			Alignment: fyne.TextAlignCenter,
			ColorName: color,
			TextStyle: fyne.TextStyle{Bold: true},
		},
	}}
	u.statusText.Refresh()
}

// setSituation shows football's down and distance under the clock, smaller.
func (u *ui) setSituation(text string) {
	u.situation.Segments = []widget.RichTextSegment{&widget.TextSegment{
		Text: text,
		Style: widget.RichTextStyle{
			Alignment: fyne.TextAlignCenter,
			SizeName:  theme.SizeNameCaptionText,
		},
	}}
	u.situation.Refresh()
}

func (u *ui) refreshStale() {
	if text := staleText(u.status.UpdatedAt, time.Now(), u.interval); text != "" {
		u.stale.SetText(text)
		u.stale.Show()
	} else {
		u.stale.Hide()
	}
}

// newRow renders one play: its text, bold for urgent plays and in the
// highlight color. The score and clock live in the header, not on every row.
func newRow(r Row, hl highlight) fyne.CanvasObject {
	text := r.Body
	if r.Updated {
		text += "  (updated)"
	}
	body := widget.NewRichText(&widget.TextSegment{Text: text, Style: widget.RichTextStyle{
		ColorName: rowColor(r, hl),
		TextStyle: fyne.TextStyle{Bold: r.Urgent || r.Final},
	}})
	body.Wrapping = fyne.TextWrapWord
	return container.NewVBox(body, widget.NewSeparator())
}

func (u *ui) addEvent(e core.Event) {
	atTop := u.scroll.Offset.Y <= 0
	added := u.model.Apply(e)
	if !added {
		for i, r := range u.model.Rows {
			if r.PlayID == e.PlayID {
				u.rows.Objects[i] = newRow(r, u.hl)
			}
		}
		u.rows.Refresh()
		return
	}
	row := newRow(u.model.Rows[0], u.hl)
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
