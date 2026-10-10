package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"playcall/internal/espn"
)

const (
	badgeLogo = 48 // header logo, in points
	badgeBall = 26 // possession ball at the outer edge of the header
)

// badge is a team's logo in the header. The slot keeps its size while the
// image loads (or never does), so the header does not jump around.
type badge struct {
	box   *hoverTip // what goes in the header: the logo, with the team's name as its tip
	logo  *canvas.Image
	src   espn.Logo // the team's logos, both variants
	shown string    // the URL this badge is showing or waiting for
}

func newBadge() *badge {
	b := &badge{logo: &canvas.Image{FillMode: canvas.ImageFillContain}}
	b.logo.SetMinSize(fyne.NewSize(badgeLogo, badgeLogo))
	b.box = newHoverTip(b.logo)
	return b
}

// setName sets the team's full name, shown when the pointer rests on the logo.
func (b *badge) setName(name string) { b.box.SetText(name) }

// setLogo sets the team's logos and shows the one for the current theme once
// the cache has it.
func (b *badge) setLogo(cache *logoCache, src espn.Logo) {
	b.src = src
	b.refresh(cache)
}

// refresh shows the variant that suits the current theme; call it when the
// theme changes.
func (b *badge) refresh(cache *logoCache) {
	url, variant := b.src.Pick(isDark())
	if url == b.shown {
		return
	}
	b.shown = url
	b.logo.File = ""
	b.logo.Refresh()
	cache.Get(url, variant, func(path string) {
		if b.shown != url { // the team or the theme changed while this downloaded
			return
		}
		b.logo.File = path
		b.logo.Refresh()
	})
}

// newBall is a possession marker: invisible, but holding its space, until
// its team has the ball.
func newBall() *canvas.Image {
	img := canvas.NewImageFromResource(footballRes)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(badgeBall, badgeBall))
	img.Translucency = 1
	return img
}

// setBall shows the ball (of resource res) or hides it.
func setBall(img *canvas.Image, res fyne.Resource, has bool) {
	if res != nil {
		img.Resource = res
	}
	if has && res != nil {
		img.Translucency = 0
	} else {
		img.Translucency = 1
	}
	img.Refresh()
}
