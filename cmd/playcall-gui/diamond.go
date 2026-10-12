package main

import (
	"image"
	"image/color"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Baseball's bases, drawn like a broadcast's: a square turned on its corner
// for each base, laid out as a diamond (second on top, third left, first
// right), filled while a runner is on it, with home plate, an outline, at the
// bottom. Fyne has no polygons, so one raster draws the lot (anti-aliased by
// sampling), which also follows the theme and leaves no image files to cache;
// all eight combinations come from the same drawing.
const (
	diamondW, diamondH = 36, 33 // logical size
	baseRadius         = 6.5    // a base's corner to its center
	baseInset          = 0.66   // the hollow of an empty base, as a share of its size
	plateInset         = 0.68   // the same for home plate
)

type pt struct{ x, y float64 }

// baseCenters are first, second and third base.
var baseCenters = [3]pt{{29.5, 16}, {18, 7.5}, {6.5, 16}}

// plateShape is home plate, flat edge up: as wide as it is tall, the sides
// reaching half way down and then closing to the point.
var plateShape = []pt{{12.5, 21.5}, {23.5, 21.5}, {23.5, 26.5}, {18, 32}, {12.5, 26.5}}

type diamond struct {
	box    *fyne.Container
	raster *canvas.Raster
	filled [3]bool
}

func newDiamond() *diamond {
	d := &diamond{}
	d.raster = canvas.NewRaster(d.render)
	d.raster.SetMinSize(fyne.NewSize(diamondW, diamondH))
	d.box = container.NewCenter(d.raster)
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
	d.box.Show()
	d.raster.Refresh()
}

func (d *diamond) recolor() { d.raster.Refresh() }

func baseShape(c pt) []pt {
	return []pt{{c.x, c.y - baseRadius}, {c.x + baseRadius, c.y}, {c.x, c.y + baseRadius}, {c.x - baseRadius, c.y}}
}

// scaled shrinks a shape toward its center, to cut the hollow out of it.
func scaled(shape []pt, k float64) []pt {
	var cx, cy float64
	for _, p := range shape {
		cx, cy = cx+p.x, cy+p.y
	}
	cx, cy = cx/float64(len(shape)), cy/float64(len(shape))
	out := make([]pt, len(shape))
	for i, p := range shape {
		out[i] = pt{cx + (p.x-cx)*k, cy + (p.y-cy)*k}
	}
	return out
}

// inside reports whether p is within a convex shape.
func inside(shape []pt, p pt) bool {
	sign := 0.0
	for i, a := range shape {
		b := shape[(i+1)%len(shape)]
		cross := (b.x-a.x)*(p.y-a.y) - (b.y-a.y)*(p.x-a.x)
		switch {
		case cross == 0:
		case sign == 0:
			sign = cross
		case (cross > 0) != (sign > 0):
			return false
		}
	}
	return true
}

// render draws the diamond at w by h pixels in the theme's text color.
func (d *diamond) render(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r, g, b, _ := theme.Color(theme.ColorNameForeground).RGBA()
	sx, sy := float64(diamondW)/float64(w), float64(diamondH)/float64(h)
	type part struct {
		shape, hollow []pt
	}
	var parts []part
	for i, c := range baseCenters {
		p := part{shape: baseShape(c)}
		if !d.filled[i] {
			p.hollow = scaled(p.shape, baseInset)
		}
		parts = append(parts, p)
	}
	parts = append(parts, part{shape: plateShape, hollow: scaled(plateShape, plateInset)})
	const n = 4 // samples per pixel side
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			hit := 0
			for j := 0; j < n; j++ {
				for i := 0; i < n; i++ {
					p := pt{(float64(x) + (float64(i)+0.5)/n) * sx, (float64(y) + (float64(j)+0.5)/n) * sy}
					for _, q := range parts {
						if inside(q.shape, p) && (q.hollow == nil || !inside(q.hollow, p)) {
							hit++
							break
						}
					}
				}
			}
			if hit > 0 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(255 * hit / (n * n))})
			}
		}
	}
	return img
}

var onSuffix = regexp.MustCompile(`, \d+ on$`)

// withoutRunnerCount drops ", 2 on" from a baseball situation: the diamond
// says it instead.
func withoutRunnerCount(situation string) string {
	return onSuffix.ReplaceAllString(situation, "")
}
