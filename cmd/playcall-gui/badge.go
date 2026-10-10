package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

const (
	badgeLogo = 48 // header logo, in points
	badgeBall = 22 // possession ball on its corner
)

// badge is a team's logo with the possession ball on its lower right corner.
// The logo slot keeps its size while the image loads (or never does), so the
// header does not jump around.
type badge struct {
	box  fyne.CanvasObject
	logo *canvas.Image
	ball *canvas.Image
	url  string // the logo this badge is showing or waiting for
}

func newBadge() *badge {
	b := &badge{
		logo: &canvas.Image{FillMode: canvas.ImageFillContain},
		ball: canvas.NewImageFromResource(footballRes),
	}
	b.logo.SetMinSize(fyne.NewSize(badgeLogo, badgeLogo))
	b.ball.FillMode = canvas.ImageFillContain
	b.ball.SetMinSize(fyne.NewSize(badgeBall, badgeBall))
	b.ball.Translucency = 1 // invisible until this team has the ball
	corner := container.NewVBox(layout.NewSpacer(), container.NewHBox(layout.NewSpacer(), b.ball))
	b.box = container.NewStack(b.logo, corner)
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

// setBall shows the ball (of resource res) on the logo, or hides it.
func (b *badge) setBall(res fyne.Resource, has bool) {
	if res != nil {
		b.ball.Resource = res
	}
	if has && res != nil {
		b.ball.Translucency = 0
	} else {
		b.ball.Translucency = 1
	}
	b.ball.Refresh()
}
