package main

import (
	"image/color"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Baseball's bases, drawn like a broadcast's: a circle for each base laid out
// as a diamond (second on top, third left, first right), filled while a
// runner is on it. Drawn rather than kept as images, so it follows the theme
// and has no files to cache; all eight combinations come from the same three
// circles.
const (
	diamondW, diamondH = 48, 34
	baseSize           = 14
)

// baseCenters are the circles' centers: first, second, third.
var baseCenters = [3]fyne.Position{{X: 40, Y: 25}, {X: 24, Y: 9}, {X: 8, Y: 25}}

type diamond struct {
	box    *fyne.Container
	bases  [3]*canvas.Circle
	filled [3]bool
}

func newDiamond() *diamond {
	d := &diamond{}
	size := canvas.NewRectangle(color.Transparent)
	size.SetMinSize(fyne.NewSize(diamondW, diamondH))
	objs := []fyne.CanvasObject{size}
	for i, c := range baseCenters {
		b := canvas.NewCircle(color.Transparent)
		b.Resize(fyne.NewSize(baseSize, baseSize))
		b.Move(fyne.NewPos(c.X-baseSize/2, c.Y-baseSize/2))
		d.bases[i] = b
		objs = append(objs, b)
	}
	d.box = container.NewCenter(container.NewWithoutLayout(objs...))
	d.box.Hide()
	return d
}

// set shows the occupied bases ("13" is first and third), or hides the
// diamond when show is false (no baseball situation to describe).
func (d *diamond) set(show bool, bases string) {
	if !show {
		d.box.Hide()
		return
	}
	for i := range d.filled {
		d.filled[i] = strings.ContainsRune(bases, rune('1'+i))
	}
	d.recolor()
	d.box.Show()
	d.box.Refresh()
}

func (d *diamond) recolor() {
	fg := theme.Color(theme.ColorNameForeground)
	for i, b := range d.bases {
		if d.filled[i] {
			b.FillColor, b.StrokeWidth = fg, 0
		} else {
			b.FillColor, b.StrokeColor, b.StrokeWidth = nil, fg, 1.5
		}
		b.Refresh()
	}
}

var onSuffix = regexp.MustCompile(`, \d+ on$`)

// withoutRunnerCount drops ", 2 on" from a baseball situation: the diamond
// says it instead.
func withoutRunnerCount(situation string) string {
	return onSuffix.ReplaceAllString(situation, "")
}
