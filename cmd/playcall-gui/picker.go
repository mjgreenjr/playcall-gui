package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
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

	ctx       context.Context
	fetcher   espn.Fetcher
	favorites map[string][]string
	onPick    func(sport string, g espn.Game)

	sport  *widget.Select
	group  *widget.Select
	all    *widget.Check
	status *widget.Label
	list   *widget.List

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
	p.status = widget.NewLabel("")
	p.status.Wrapping = fyne.TextWrapWord

	p.list = widget.NewList(
		func() int { return len(p.games) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				widget.NewLabel(""), layout.NewSpacer())
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(p.games) {
				return
			}
			g := p.games[i]
			box := o.(*fyne.Container)
			// Border puts its left and right objects after the (empty) center.
			box.Objects[1].(*widget.Label).SetText(matchup(g))
			box.Objects[2].(*widget.Label).SetText(gameDetail(g))
		},
	)
	p.list.OnSelected = func(i widget.ListItemID) {
		p.list.UnselectAll()
		if i < len(p.games) {
			p.onPick(p.currentSport(), p.games[i])
		}
	}

	top := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Sport"), refresh, p.sport),
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
