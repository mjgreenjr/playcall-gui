package main

import (
	"context"
	"fmt"
	"image/color"
	"strings"
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
	// onSettings opens the settings screen; the gear is inert while nil.
	onSettings func()
	hl         highlight // color of urgent plays

	model  Model
	status core.GameStatus

	away, home             *canvas.Text // abbreviations
	awayScore, homeScore   *canvas.Text
	awayBadge, homeBadge   *badge
	awayBall, homeBall     *canvas.Image
	logos                  *logoCache
	statusText             *widget.RichText
	situation              *widget.RichText
	awayCounts, homeCounts *widget.Label
	// awayTO and homeTO are football's timeouts, under each team's score.
	awayTO, homeTO         *widget.Label
	awayRecord, homeRecord *widget.RichText
	// division is a team's division record for the header, from memory (it
	// runs on the UI goroutine); nil shows none.
	division func(abbr string) string
	series   *widget.Label
	stale    *widget.Label

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
	u.awayScore, u.homeScore = big(), big()
	u.awayBadge, u.homeBadge = newBadge(), newBadge()
	u.awayBall, u.homeBall = newBall(), newBall()
	u.statusText = widget.NewRichText()
	u.statusText.Wrapping = fyne.TextWrapWord
	u.situation = widget.NewRichText()
	u.situation.Wrapping = fyne.TextWrapWord
	u.setStatusLine("waiting for ESPN...", false)
	u.awayCounts = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{})
	u.homeCounts = widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{})
	for _, l := range []*widget.Label{u.awayCounts, u.homeCounts} {
		l.Hide() // basketball only
	}
	u.awayTO = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	u.homeTO = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	u.awayTO.Hide()
	u.homeTO.Hide()
	u.awayRecord, u.homeRecord = widget.NewRichText(), widget.NewRichText()
	u.setRecords("", "")
	u.series = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	u.stale = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	u.stale.Importance = widget.WarningImportance
	u.series.Hide() // shown only when there is something to say
	u.stale.Hide()

	// Top row: "Games" back to the picker (when there is one) and the settings gear.
	gear := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		if u.onSettings != nil {
			u.onSettings()
		}
	})
	back := container.NewHBox(layout.NewSpacer(), gear)
	if onBack != nil {
		b := widget.NewButtonWithIcon("Games", theme.NavigateBackIcon(), onBack)
		back = container.NewHBox(b, layout.NewSpacer(), gear)
	}
	header := container.NewVBox(
		back,
		// One row, mirrored like a broadcast bug: the ball (if away has it),
		// logo, abbreviation and score of the away team, the clock and
		// situation between, then the home team's score, abbreviation, logo
		// and ball. Football's timeouts sit under each score.
		// Each team's record sits centered under its abbreviation.
		container.NewBorder(nil, nil,
			container.NewHBox(ballSlot(u.awayBall), container.NewVBox(u.awayBadge.box), container.NewVBox(u.away, u.awayRecord), scoreGap(), container.NewVBox(u.awayScore, u.awayTO)),
			container.NewHBox(container.NewVBox(u.homeScore, u.homeTO), scoreGap(), container.NewVBox(u.home, u.homeRecord), container.NewVBox(u.homeBadge.box), ballSlot(u.homeBall)),
			container.New(scoreLineCenter{height: func() float32 { return u.awayScore.MinSize().Height }}, container.NewVBox(u.statusText, u.situation))),
		// Basketball's timeouts and fouls, under each team's score.
		container.NewBorder(nil, nil, u.awayCounts, u.homeCounts),
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

// scoreGap keeps a score off its abbreviation. It is a gap and not a space in
// the score's text, so the score stays centered over the timeouts under it.
func scoreGap() fyne.CanvasObject {
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(theme.Padding()*3, 1))
	return gap
}

// ballSlot puts the ball at the top of its column, vertically centered on a
// logo-high slot, so it lines up with the logo and the abbreviation.
func ballSlot(ball *canvas.Image) fyne.CanvasObject {
	slot := canvas.NewRectangle(color.Transparent)
	slot.SetMinSize(fyne.NewSize(badgeBall, badgeLogo))
	return container.NewVBox(container.NewStack(slot, container.NewCenter(ball)))
}

// themeChanged restyles what was colored when it was built: the scores'
// text, and the logos, which come in a variant for each kind of background.
func (u *ui) themeChanged() {
	for _, t := range []*canvas.Text{u.away, u.home, u.awayScore, u.homeScore} {
		t.Color = theme.Color(theme.ColorNameForeground)
		t.Refresh()
	}
	u.awayBadge.refresh(u.logos)
	u.homeBadge.refresh(u.logos)
}

// setHighlight changes the color of urgent plays, redrawing the rows already
// shown (they were colored when they were added). The scroll position stays.
func (u *ui) setHighlight(h highlight) {
	if h == u.hl {
		return
	}
	u.hl = h
	for i, r := range u.model.Rows {
		u.rows.Objects[i] = newRow(r, h)
	}
	u.rows.Refresh()
}

func (u *ui) clearUnseen() {
	u.unseen = 0
	u.newBtn.Hide()
}

func (u *ui) setStatus(s core.GameStatus) {
	u.status = s
	u.away.Text, u.awayScore.Text = s.Away.Abbr, s.Away.Score
	u.home.Text, u.homeScore.Text = s.Home.Abbr, s.Home.Score
	for _, t := range []*canvas.Text{u.away, u.home, u.awayScore, u.homeScore} {
		t.Refresh()
	}
	u.setStatusLine(statusLine(s), false)
	u.setSituation(s.Situation)
	u.awayBadge.setName(s.Away.Name)
	u.homeBadge.setName(s.Home.Name)
	u.awayBadge.setLogo(u.logos, s.Away.Logo)
	u.homeBadge.setLogo(u.logos, s.Home.Logo)
	u.setBalls(s)
	u.refreshRecords()
	if s.Sport == "nfl" {
		u.setCounts(u.awayTO, s.Sport, s.AwayCounts)
		u.setCounts(u.homeTO, s.Sport, s.HomeCounts)
	} else {
		u.setCounts(u.awayCounts, s.Sport, s.AwayCounts)
		u.setCounts(u.homeCounts, s.Sport, s.HomeCounts)
	}
	var tags []string
	if s.Preseason {
		tags = append(tags, "Preseason")
	}
	if s.Series != "" {
		tags = append(tags, "Series: "+s.Series)
	}
	if len(tags) > 0 {
		u.series.SetText(strings.Join(tags, " · "))
		u.series.Show()
	} else {
		u.series.Hide()
	}
	u.refreshStale()
}

// setCounts shows a team's timeouts and fouls, or hides the line.
func (u *ui) setCounts(l *widget.Label, sport string, c *core.TeamCounts) {
	if text := countsText(sport, c); text != "" {
		l.SetText(text)
		l.Show()
	} else {
		l.Hide()
	}
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

// refreshRecords redraws the records from the last status; the division
// records may have arrived since.
func (u *ui) refreshRecords() {
	away, home := statusRecords(u.status, u.division)
	awayRole, homeRole := atBatRoles(u.status)
	u.setRecords(joinLines(away, awayRole), joinLines(home, homeRole))
}

// setRecords shows each team's record under its side, in the situation's size.
func (u *ui) setRecords(away, home string) {
	for _, r := range []struct {
		w     *widget.RichText
		text  string
		align fyne.TextAlign
	}{{u.awayRecord, away, fyne.TextAlignCenter}, {u.homeRecord, home, fyne.TextAlignCenter}} {
		r.w.Segments = []widget.RichTextSegment{&widget.TextSegment{
			Text:  r.text,
			Style: widget.RichTextStyle{Alignment: r.align, SizeName: theme.SizeNameCaptionText},
		}}
		r.w.Refresh()
	}
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

// scoreLineCenter centers its one child across the width and, vertically,
// within the first height pixels: the score line, so the clock and situation
// sit beside the scores and not in the middle of the records' extra row.
type scoreLineCenter struct{ height func() float32 }

func (scoreLineCenter) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }

func (l scoreLineCenter) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	min := objs[0].MinSize()
	y := (l.height() - min.Height) / 2 // negative when taller than the scores: it grows both ways
	objs[0].Resize(fyne.NewSize(size.Width, min.Height))
	objs[0].Move(fyne.NewPos(0, y))
}
