package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

const (
	pipSize  = 9
	pipLabel = 10 // text size of the B, S and O
	pipRowH  = 11 // distance from one row of circles to the next
	pipDrop  = 1  // how far the circles sit below their row's center
)

// pipRow is one line of baseball's count: a letter and a few circles, filled
// for each ball, strike or out so far.
type pipRow struct {
	box    *fyne.Container
	label  *canvas.Text
	pips   []*canvas.Circle
	filled int
}

func newPipRow(letter string, slots int) *pipRow {
	r := &pipRow{label: canvas.NewText(letter, theme.Color(theme.ColorNameForeground))}
	r.label.TextSize = pipLabel
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	circles := container.New(newPipLayout())
	for range slots {
		c := canvas.NewCircle(color.Transparent)
		r.pips = append(r.pips, c)
		circles.Add(c)
	}
	// The letters differ in width; a fixed slot lines the circles up in columns.
	slot := canvas.NewRectangle(color.Transparent)
	slot.SetMinSize(fyne.NewSize(pipLabel, pipSize))
	r.box = container.NewHBox(container.NewStack(slot, container.NewCenter(r.label)), circles)
	return r
}

// set fills the first n circles (no more than there are).
func (r *pipRow) set(n int) {
	r.filled = min(max(n, 0), len(r.pips))
	r.recolor()
}

func (r *pipRow) recolor() {
	fg := theme.Color(theme.ColorNameForeground)
	r.label.Color = fg
	r.label.Refresh()
	for i, c := range r.pips {
		if i < r.filled {
			c.FillColor, c.StrokeWidth = fg, 0
		} else {
			c.FillColor, c.StrokeColor, c.StrokeWidth = nil, fg, 1.25
		}
		c.Refresh()
	}
}

// pipLayout puts its circles in a row, pipSize square, with a small gap.
type pipLayout struct{}

func newPipLayout() fyne.Layout { return pipLayout{} }

func (pipLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(float32(len(objs))*pipSize+float32(max(len(objs)-1, 0))*3, pipSize)
}

func (pipLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	y := (size.Height-pipSize)/2 + pipDrop // centered on the row, then a hair lower to sit level with the letter
	for i, o := range objs {
		o.Resize(fyne.NewSize(pipSize, pipSize))
		o.Move(fyne.NewPos(float32(i)*(pipSize+3), y))
	}
}

// countBox is baseball's balls, strikes and outs, three rows of circles.
type countBox struct {
	box                  *fyne.Container
	balls, strikes, outs *pipRow
}

func newCountBox() *countBox {
	c := &countBox{balls: newPipRow("B", 3), strikes: newPipRow("S", 2), outs: newPipRow("O", 2)}
	c.box = container.New(layoutNoGap(), c.balls.box, c.strikes.box, c.outs.box)
	c.box.Hide()
	return c
}

func layoutNoGap() fyne.Layout { return noGapColumn{} }

// noGapColumn stacks rows one under another, each pipRowH tall (the letters
// are taller than that and overhang a pixel, which is why it is tight), each
// at its left.
type noGapColumn struct{}

func (noGapColumn) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w float32
	for _, o := range objs {
		w = max(w, o.MinSize().Width)
	}
	return fyne.NewSize(w, float32(len(objs))*pipRowH)
}

func (noGapColumn) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for i, o := range objs {
		o.Resize(fyne.NewSize(o.MinSize().Width, pipRowH))
		o.Move(fyne.NewPos(0, float32(i)*pipRowH))
	}
}

// set shows the count, or hides the rows when show is false.
func (c *countBox) set(show bool, balls, strikes, outs int) {
	if !show {
		c.box.Hide()
		return
	}
	c.balls.set(balls)
	c.strikes.set(strikes)
	c.outs.set(outs)
	c.box.Show()
	c.box.Refresh()
}

func (c *countBox) recolor() {
	for _, r := range []*pipRow{c.balls, c.strikes, c.outs} {
		r.recolor()
	}
}
