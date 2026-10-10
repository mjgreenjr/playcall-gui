package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

const (
	badgeLogo = 48 // header logo, in points
	badgeBall = 26 // possession ball at the outer edge of the header
)

// badge is a team's logo in the header. The slot keeps its size while the
// image loads (or never does), so the header does not jump around.
type badge struct {
	logo *canvas.Image
	url  string // the logo this badge is showing or waiting for
}

func newBadge() *badge {
	b := &badge{logo: &canvas.Image{FillMode: canvas.ImageFillContain}}
	b.logo.SetMinSize(fyne.NewSize(badgeLogo, badgeLogo))
	return b
}

// setLogo shows the logo at url once the cache has it ("" clears it).
func (b *badge) setLogo(cache *logoCache, url string) {
	if url == b.url {
		return
	}
	b.url = url
	b.logo.File = ""
	b.logo.Refresh()
	cache.Get(url, func(path string) {
		if b.url != url { // the team changed while this downloaded
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
