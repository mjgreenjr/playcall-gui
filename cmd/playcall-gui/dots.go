package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"playcall/internal/core"
)

// dotSize is the diameter of a timeout circle.
const dotSize = 13

// dotRow is a team's timeouts as a row of circles, filled for each left and
// outlined for each used, like the bars on a broadcast. They are drawn, not
// typed as ● and ○: the font draws the hollow one smaller and higher than the
// filled one, so a mixed row looked uneven.
type dotRow struct {
	box         *fyne.Container
	dots        []*canvas.Circle
	left, slots int
	shown       bool
}

func newDotRow() *dotRow {
	d := &dotRow{box: container.NewCenter(container.NewHBox())}
	d.box.Hide()
	return d
}

// set shows the counts, or hides the row when there is nothing to draw.
func (d *dotRow) set(c *core.TeamCounts) {
	if c == nil || c.TimeoutSlots <= 0 {
		d.shown = false
		d.box.Hide()
		return
	}
	d.left, d.slots, d.shown = min(max(c.Timeouts, 0), c.TimeoutSlots), c.TimeoutSlots, true
	row := d.box.Objects[0].(*fyne.Container)
	if len(d.dots) != d.slots {
		d.dots = d.dots[:0]
		row.Objects = row.Objects[:0]
		for range d.slots {
			dot := canvas.NewCircle(theme.Color(theme.ColorNameForeground))
			size := canvas.NewRectangle(color.Transparent) // a circle has no minimum size of its own
			size.SetMinSize(fyne.NewSize(dotSize, dotSize))
			d.dots = append(d.dots, dot)
			row.Add(container.NewStack(size, dot))
		}
	}
	d.recolor()
	d.box.Show()
	d.box.Refresh()
}

// recolor paints the circles for the current theme and counts.
func (d *dotRow) recolor() {
	fg := theme.Color(theme.ColorNameForeground)
	for i, dot := range d.dots {
		if i < d.left {
			dot.FillColor, dot.StrokeWidth = fg, 0
		} else {
			dot.FillColor, dot.StrokeColor, dot.StrokeWidth = nil, fg, 1.5
		}
		dot.Refresh()
	}
}
