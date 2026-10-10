package main

import (
	"context"
	"fmt"
	"strings"
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

func newPicker(ctx context.Context, f espn.Fetcher, favorites map[string][]string, sport string, onPick func(string, espn.Game)) *picker {
	p := &picker{ctx: ctx, fetcher: f, favorites: favorites, onPick: onPick}

	names := make([]string, len(sports.Names))
	for i, n := range sports.Names {
		names[i] = strings.ToUpper(n)
	}
	p.group = widget.NewSelect([]string{anyGroup}, func(string) {
		if !p.quiet {
			p.resetAll()
			p.load()
		}
	})
	p.sport = widget.NewSelect(names, func(string) {
		if p.quiet {
			return
		}
		p.setGroups()
		p.resetAll()
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
	p.sport.SetSelected(strings.ToUpper(sport))
	p.quiet = false
	p.setGroups()
	p.resetAll()
	return p
}

func (p *picker) currentSport() string { return strings.ToLower(p.sport.Selected) }

func (p *picker) currentGroup() string {
	return groupID(p.currentSport(), p.group.Selected)
}

// setGroups fills the group choices for the current sport, back at "Any".
func (p *picker) setGroups() {
	p.quiet = true
	p.group.SetOptions(groupChoices(p.currentSport()))
	p.group.SetSelected(anyGroup)
	p.quiet = false
}

// resetAll sets "All games" for sports with no favorites, where filtering
// would show nothing to choose from. A chosen group is itself the filter, so
// it overrides favorites: the box is ticked and locked.
func (p *picker) resetAll() {
	p.quiet = true
	defer func() { p.quiet = false }()
	if p.currentGroup() != "" {
		p.all.SetChecked(true)
		p.all.Disable()
		return
	}
	p.all.Enable()
	p.all.SetChecked(len(p.favorites[p.currentSport()]) == 0)
}

func (p *picker) load() {
	p.generation++
	gen, sport, group := p.generation, p.currentSport(), p.currentGroup()
	p.status.SetText("Loading " + strings.ToUpper(sport) + " games...")
	go func() {
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
	}()
}

// show fills the list from the scoreboard answer, applying the favorites
// filter unless "All games" is ticked.
func (p *picker) show(games []espn.Game) {
	sport := p.currentSport()
	shown := games
	if !p.all.Checked {
		shown = core.FavoriteGames(games, p.favorites[sport])
	}
	p.games = orderForPicker(shown)
	p.list.Refresh()
	switch {
	case len(games) == 0 && p.group.Selected != anyGroup:
		p.status.SetText("No " + strings.ToUpper(sport) + " " + p.group.Selected + " games on the scoreboard.")
	case len(games) == 0:
		p.status.SetText("No " + strings.ToUpper(sport) + " games on the scoreboard.")
	case len(p.games) == 0:
		p.status.SetText(fmt.Sprintf("None of your favorite teams play today. Tick \"All games\" to see %d.", len(games)))
	default:
		p.status.SetText(fmt.Sprintf("%d game(s)", len(p.games)))
	}
}
