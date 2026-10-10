package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
)

// picker lists a sport's games so one can be chosen instead of typing an id.
// It shows the favorite teams' games by default, with a box for all of them.
type picker struct {
	content fyne.CanvasObject
	prefs   groupPrefs
	// onSettings opens the settings screen; the gear button is inert while nil.
	onSettings func()
	logos      *logoCache // team logos for the rows; nil shows none
	slots      logoSlots
	abbrWidth  float32 // width of the abbreviation columns, so every row lines up
	recWidth   float32 // width of the record columns

	ctx       context.Context
	fetcher   espn.Fetcher
	favorites map[string][]string
	onPick    func(sport string, g espn.Game)

	sport  *widget.Select
	group  *widget.Select
	all    *widget.Check
	status *widget.Label
	list   *widget.List
	// cont is the "Continue" button for the last game; hidden when there is none.
	cont *widget.Button

	games       []espn.Game // what the list shows
	lastFetched []espn.Game // the scoreboard answer, before the favorites filter
	generation  int         // drops answers to a request that was replaced
	quiet       bool        // set while the code itself changes a control
}

// groupPrefs remembers the last conference or division chosen for each sport.
// Either function may be nil.
type groupPrefs struct {
	get func(sport string) string
	set func(sport, group string)
}

func (g groupPrefs) saved(sport string) string {
	if g.get == nil {
		return ""
	}
	return g.get(sport)
}

func (g groupPrefs) save(sport, group string) {
	if g.set != nil {
		g.set(sport, group)
	}
}

// runAsync starts the scoreboard fetch off the UI goroutine. Tests replace it
// with a synchronous call: Fyne's test driver runs fyne.Do on the calling
// goroutine, so a real background fetch would touch widgets concurrently with
// the test and crash the text shaper.
var runAsync = func(f func()) { go f() }

func newPicker(ctx context.Context, f espn.Fetcher, favorites map[string][]string, prefs groupPrefs, sport string, onPick func(string, espn.Game)) *picker {
	p := &picker{ctx: ctx, fetcher: f, favorites: favorites, prefs: prefs, onPick: onPick}

	names := make([]string, len(sports.Names))
	for i, n := range sports.Names {
		names[i] = sportLabel(n)
	}
	p.group = widget.NewSelect([]string{anyGroup}, func(name string) {
		if p.quiet {
			return
		}
		// A pick by the user is the new default for this sport.
		if name == anyGroup {
			name = ""
		}
		p.prefs.save(p.currentSport(), name)
		p.load() // "All games" stays as the user left it
	})
	p.sport = widget.NewSelect(names, func(string) {
		if p.quiet {
			return
		}
		p.setGroups() // the new sport's remembered group; favorites apply as usual
		p.load()
	})
	p.all = widget.NewCheck("All games", func(bool) {
		if !p.quiet {
			p.show(p.lastFetched)
		}
	})
	refresh := widget.NewButton("Refresh", p.load)
	gear := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		if p.onSettings != nil {
			p.onSettings()
		}
	})
	p.status = widget.NewLabel("")
	p.status.Wrapping = fyne.TextWrapWord

	p.list = widget.NewList(
		func() int { return len(p.games) },
		func() fyne.CanvasObject {
			bold := func() *widget.Label {
				return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			}
			column := func(l *widget.Label) fyne.CanvasObject {
				return container.New(columnLayout{width: func() float32 { return p.abbrWidth }}, l)
			}
			record := func() fyne.CanvasObject {
				t := canvas.NewText("", theme.Color(theme.ColorNamePlaceHolder))
				t.TextSize = theme.TextSize() * recordTextScale
				// Centered up and down; the column's width is shared by all rows.
				return container.New(columnLayout{width: func() float32 { return p.recWidth }},
					container.NewVBox(layout.NewSpacer(), t, layout.NewSpacer()))
			}
			teams := container.NewHBox(rowLogo(), column(bold()), record(), widget.NewLabel("@"), rowLogo(), column(bold()), record())
			return container.NewBorder(nil, nil, teams, widget.NewLabel(""), layout.NewSpacer())
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(p.games) {
				return
			}
			g := p.games[i]
			// Border puts its left and right objects after the (empty) center.
			box := o.(*fyne.Container)
			teams := box.Objects[1].(*fyne.Container).Objects
			p.slots.show(p.logos, teams[0].(*canvas.Image), g.AwayLogo, isDark())
			teams[1].(*fyne.Container).Objects[0].(*widget.Label).SetText(strings.TrimSpace(g.Away))
			setRecord(teams[2], awayRecord(g))
			p.slots.show(p.logos, teams[4].(*canvas.Image), g.HomeLogo, isDark())
			teams[5].(*fyne.Container).Objects[0].(*widget.Label).SetText(strings.TrimSpace(g.Home))
			setRecord(teams[6], homeRecord(g))
			box.Objects[2].(*widget.Label).SetText(gameDetail(g, time.Now()))
		},
	)
	p.list.OnSelected = func(i widget.ListItemID) {
		p.list.UnselectAll()
		if i < len(p.games) {
			p.onPick(p.currentSport(), p.games[i])
		}
	}

	p.cont = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
	p.cont.Hide()
	top := container.NewVBox(
		p.cont,
		container.NewBorder(nil, nil, widget.NewLabel("Sport"), container.NewHBox(refresh, gear), p.sport),
		container.NewBorder(nil, nil, widget.NewLabel("Group"), nil, p.group),
		p.all,
		p.status,
		widget.NewSeparator(),
	)
	p.content = container.NewBorder(top, nil, nil, nil, p.list)

	p.quiet = true
	p.sport.SetSelected(sportLabel(sport))
	p.quiet = false
	p.setGroups()
	p.resetAll()
	return p
}

// setContinue shows a button that follows the last game again, or hides it
// when label is "".
func (p *picker) setContinue(label string, follow func()) {
	if label == "" || follow == nil {
		p.cont.Hide()
		return
	}
	p.cont.SetText("Continue: " + label)
	p.cont.OnTapped = follow
	p.cont.Show()
}

// refresh picks up changes made elsewhere (favorites, the saved group) and
// reloads the scoreboard.
func (p *picker) refresh() {
	p.setGroups()
	p.load()
}

// columnLayout gives its one child at least the shared column width, so rows
// of different abbreviations still line up.
type columnLayout struct{ width func() float32 }

func (c columnLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	m := objs[0].MinSize()
	return fyne.NewSize(max(m.Width, c.width()), m.Height)
}

func (c columnLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(size)
}

// recordTextScale is the record's size relative to the normal text.
const recordTextScale = 0.8

// setRecord fills a record column made by the row template.
func setRecord(column fyne.CanvasObject, text string) {
	t := column.(*fyne.Container).Objects[0].(*fyne.Container).Objects[1].(*canvas.Text)
	t.Text = text
	t.Color = theme.Color(theme.ColorNamePlaceHolder)
	t.Refresh()
}

// widestRecord is how wide the record columns need to be for these games.
func widestRecord(games []espn.Game) float32 {
	var w float32
	t := canvas.NewText("", theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.TextSize() * recordTextScale
	for _, g := range games {
		for _, text := range []string{awayRecord(g), homeRecord(g)} {
			t.Text = text
			w = max(w, t.MinSize().Width)
		}
	}
	return w
}

// widestAbbreviation is how wide the abbreviation column needs to be for
// these games: the widest team abbreviation, in the row's bold label style.
func widestAbbreviation(games []espn.Game) float32 {
	var w float32
	label := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	for _, g := range games {
		for _, abbr := range []string{g.Away, g.Home} {
			label.SetText(strings.TrimSpace(abbr))
			w = max(w, label.MinSize().Width)
		}
	}
	return w
}

func (p *picker) currentSport() string { return sportFromLabel(p.sport.Selected) }

func (p *picker) currentGroup() string {
	return groupID(p.currentSport(), p.group.Selected)
}

// setGroups fills the group choices for the current sport and selects the
// one remembered for it ("All" if none, or if it is no longer a choice).
func (p *picker) setGroups() {
	p.quiet = true
	defer func() { p.quiet = false }()
	choices := groupChoices(p.currentSport())
	p.group.SetOptions(choices)
	pick := anyGroup
	if saved := p.prefs.saved(p.currentSport()); slices.Contains(choices, saved) {
		pick = saved
	}
	p.group.SetSelected(pick)
}

// resetAll puts "All games" back to unticked: favorites narrow the list again.
func (p *picker) resetAll() {
	p.quiet = true
	p.all.SetChecked(false)
	p.quiet = false
}

func (p *picker) load() {
	p.generation++
	gen, sport, group := p.generation, p.currentSport(), p.currentGroup()
	p.status.SetText("Loading " + sportLabel(sport) + " games...")
	runAsync(func() {
		games, err := core.TodaysGames(p.ctx, p.fetcher, sport, group, time.Now())
		fyne.Do(func() {
			if gen != p.generation {
				return
			}
			if err != nil {
				p.lastFetched = nil
				p.games = nil
				p.list.Refresh()
				p.status.SetText("Could not load games: " + err.Error())
				return
			}
			p.lastFetched = games
			p.show(games)
		})
	})
}

// show fills the list from the scoreboard answer. Favorite teams narrow it
// when any of them is in it (whatever group is chosen); if none is, the whole
// scoreboard shows and the status says so. "All games" skips the narrowing.
func (p *picker) show(games []espn.Game) {
	sport := p.currentSport()
	favorites := p.favorites[sport]
	shown, status := games, fmt.Sprintf("%d game(s)", len(games))
	if len(favorites) > 0 && !p.all.Checked {
		if fav := core.FavoriteGames(games, favorites); len(fav) > 0 {
			shown = fav
			status = fmt.Sprintf("%d favorite-team game(s) of %d. Tick \"All games\" for the rest.", len(fav), len(games))
		} else if len(games) > 0 {
			status = fmt.Sprintf("None of your favorite teams play here; showing all %d.", len(games))
		}
	}
	p.games = orderForPicker(shown)
	p.abbrWidth = widestAbbreviation(p.games)
	p.recWidth = widestRecord(p.games)
	p.list.Refresh()
	switch {
	case len(games) == 0 && p.group.Selected != anyGroup:
		p.status.SetText("No " + sportLabel(sport) + " " + p.group.Selected + " games on the scoreboard.")
	case len(games) == 0:
		p.status.SetText("No " + sportLabel(sport) + " games on the scoreboard.")
	default:
		p.status.SetText(status)
	}
}
