package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// rankBox is a college team's ranking in the game header: a small number just
// before the abbreviation, centered on its line, as on a broadcast. It takes no
// room (and leaves no gap) while the team is unranked.
type rankBox struct {
	box  *fyne.Container
	text *canvas.Text
}

func newRankBox() *rankBox {
	t := canvas.NewText("", theme.Color(theme.ColorNameForeground))
	t.TextSize = 14
	t.TextStyle = fyne.TextStyle{Bold: true}
	r := &rankBox{text: t, box: container.NewCenter(t)}
	r.box.Hide()
	return r
}

// set shows the ranking, or hides the box for "".
func (r *rankBox) set(rank string) {
	r.text.Text = rank
	r.text.Refresh()
	if rank == "" {
		r.box.Hide()
	} else {
		r.box.Show()
	}
}

func (r *rankBox) recolor() {
	r.text.Color = theme.Color(theme.ColorNameForeground)
	r.text.Refresh()
}
