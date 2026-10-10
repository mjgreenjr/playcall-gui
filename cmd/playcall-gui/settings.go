package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"playcall/internal/core"
	"playcall/internal/espn"
	"playcall/internal/sports"
)

// settingsEnv is what the settings screen needs from the app.
type settingsEnv struct {
	ctx     context.Context
	fetcher espn.Fetcher
	// cfg returns the current config.
	cfg func() core.Config
	// update applies a change to the app's config and saves it to the file.
	update func(func(*core.Config))
	// back leaves the screen.
	back func()
}

// settingsView is the settings screen: General (poll interval, highlight
// color) and Favorites (per sport: default group and favorite teams). Every
// change is saved as soon as it is made.
type settingsView struct {
	content fyne.CanvasObject
	env     settingsEnv

	interval *widget.Select
	color    *widget.Select
	preview  *widget.RichText

	sport      *widget.Select
	group      *widget.Select
	search     *widget.Entry
	status     *widget.Label
	count      *widget.Label
	list       *widget.List
	teams      []espn.Team // the sport's teams as ESPN lists them
	rows       []teamRow   // what the list shows, after the search filter
	loadedFor  string      // sport the teams belong to
	generation int
	quiet      bool
}

// teamRow is one line of the favorites list.
type teamRow struct {
	Abbr, Name string
	Extra      bool // a favorite that ESPN's list for this sport does not have
}

// intervalChoices are the poll intervals offered, in seconds.
var intervalChoices = []int{5, 10, 15, 20, 30, 60}

func intervalLabel(seconds int) string { return fmt.Sprintf("%d seconds", seconds) }

func newSettings(env settingsEnv, sport string) *settingsView {
	v := &settingsView{env: env}

	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), env.back)
	title := widget.NewLabelWithStyle("Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	top := container.NewVBox(container.NewBorder(nil, nil, back, layout.NewSpacer(), title), widget.NewSeparator())

	tabs := container.NewAppTabs(
		container.NewTabItem("General", v.generalTab()),
		container.NewTabItem("Favorites", v.favoritesTab(sport)),
	)
	v.content = container.NewBorder(top, nil, nil, nil, tabs)
	return v
}

// ---- General

func (v *settingsView) generalTab() fyne.CanvasObject {
	cfg := v.env.cfg()

	current := core.ResolveInterval(nil, cfg)
	choices := slices.Clone(intervalChoices)
	if !slices.Contains(choices, current) { // a hand-edited value stays visible
		choices = append(choices, current)
		slices.Sort(choices)
	}
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = intervalLabel(c)
	}
	v.interval = widget.NewSelect(labels, func(label string) {
		if v.quiet {
			return
		}
		secs, err := strconv.Atoi(strings.Fields(label)[0])
		if err == nil {
			v.env.update(func(c *core.Config) { c.SetInterval(secs) })
		}
	})
	v.quiet = true // choosing what is already saved is not a change
	v.interval.SetSelected(intervalLabel(current))
	v.quiet = false

	hl := highlightByKey(cfg.HighlightColor())
	v.preview = widget.NewRichText()
	v.showPreview(hl)
	v.color = widget.NewSelect(highlightLabels(), func(label string) {
		if v.quiet {
			return
		}
		h := highlightByLabel(label)
		v.showPreview(h)
		v.env.update(func(c *core.Config) { c.SetHighlightColor(h.key) })
	})
	v.quiet = true
	v.color.SetSelected(hl.label)
	v.quiet = false

	note := widget.NewLabel("How often a followed game is refreshed. Applies to the next game you open. Never below 5 seconds.")
	note.Wrapping = fyne.TextWrapWord
	note.Importance = widget.LowImportance
	return container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Poll interval", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		v.interval, note,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Highlight color", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Marks scoring plays and turnovers in the play list."),
		v.color, v.preview,
	))
}

// showPreview draws a highlighted play and a normal one in the chosen color.
func (v *settingsView) showPreview(h highlight) {
	v.preview.Segments = []widget.RichTextSegment{
		&widget.TextSegment{Text: "Touchdown: a scoring play looks like this.", Style: widget.RichTextStyle{
			ColorName: h.color, TextStyle: fyne.TextStyle{Bold: true},
		}},
		&widget.TextSegment{Text: "Incomplete pass: a normal play looks like this.", Style: widget.RichTextStyle{
			ColorName: theme.ColorNameForeground,
		}},
	}
	v.preview.Refresh()
}

// ---- Favorites

func (v *settingsView) favoritesTab(sport string) fyne.CanvasObject {
	names := make([]string, len(sports.Names))
	for i, n := range sports.Names {
		names[i] = sportLabel(n)
	}
	v.group = widget.NewSelect([]string{anyGroup}, func(name string) {
		if v.quiet {
			return
		}
		if name == anyGroup {
			name = ""
		}
		sp := v.currentSport()
		v.env.update(func(c *core.Config) { c.SetFavoriteGroup(sp, name) })
	})
	v.sport = widget.NewSelect(names, func(string) {
		if v.quiet {
			return
		}
		v.sportChanged()
	})
	v.search = widget.NewEntry()
	v.search.SetPlaceHolder("Search teams")
	v.search.OnChanged = func(string) { v.applyFilter() }
	v.status = widget.NewLabel("")
	v.status.Wrapping = fyne.TextWrapWord
	v.count = widget.NewLabel("")

	v.list = widget.NewList(
		func() int { return len(v.rows) },
		func() fyne.CanvasObject { return widget.NewCheck("", nil) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(v.rows) {
				return
			}
			row := v.rows[i]
			check := o.(*widget.Check)
			check.OnChanged = nil // setting the state below is not a user's choice
			check.SetText(row.label())
			check.SetChecked(v.isFavorite(row.Abbr))
			check.OnChanged = func(on bool) { v.setFavorite(row.Abbr, on) }
		},
	)

	v.quiet = true
	v.sport.SetSelected(sportLabel(sport))
	v.quiet = false
	v.fillGroup()
	v.loadTeams()

	head := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Sport"), nil, v.sport),
		container.NewBorder(nil, nil, widget.NewLabel("Default group"), nil, v.group),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Favorite teams", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		v.search, v.count, v.status,
	)
	return container.NewBorder(head, nil, nil, nil, v.list)
}

func (r teamRow) label() string {
	if r.Extra {
		return r.Abbr + "  (not in ESPN's team list)"
	}
	return r.Abbr + "  " + r.Name
}

func (v *settingsView) currentSport() string { return sportFromLabel(v.sport.Selected) }

func (v *settingsView) sportChanged() {
	v.search.SetText("")
	v.fillGroup()
	v.loadTeams()
}

// fillGroup lists the sport's groups and selects the saved default.
func (v *settingsView) fillGroup() {
	v.quiet = true
	defer func() { v.quiet = false }()
	sp := v.currentSport()
	choices := groupChoices(sp)
	v.group.SetOptions(choices)
	pick := anyGroup
	if saved := v.env.cfg().FavoriteGroup(sp); slices.Contains(choices, saved) {
		pick = saved
	}
	v.group.SetSelected(pick)
}

func (v *settingsView) favorites() []string { return v.env.cfg().FavoriteTeams[v.currentSport()] }

func (v *settingsView) isFavorite(abbr string) bool { return slices.Contains(v.favorites(), abbr) }

// setFavorite adds or removes a team from the current sport's favorites.
func (v *settingsView) setFavorite(abbr string, on bool) {
	sp := v.currentSport()
	v.env.update(func(c *core.Config) {
		list := slices.Clone(c.FavoriteTeams[sp])
		has := slices.Contains(list, abbr)
		switch {
		case on && !has:
			list = append(list, abbr)
		case !on && has:
			list = slices.DeleteFunc(list, func(t string) bool { return t == abbr })
		default:
			return
		}
		c.SetFavoriteTeams(sp, list)
	})
	v.updateCount()
}

func (v *settingsView) updateCount() {
	v.count.SetText(fmt.Sprintf("%d favorite(s) for %s", len(v.favorites()), sportLabel(v.currentSport())))
}

// loadTeams fetches the current sport's teams, then fills the list.
func (v *settingsView) loadTeams() {
	v.generation++
	gen, sp := v.generation, v.currentSport()
	v.teams, v.rows = nil, nil
	v.list.Refresh()
	v.status.SetText("Loading teams...")
	v.updateCount()
	runAsync(func() {
		teams := espn.SortTeams(espn.FetchTeams(v.env.ctx, v.env.fetcher, sp, ""))
		fyne.Do(func() {
			if gen != v.generation {
				return
			}
			v.teams, v.loadedFor = teams, sp
			if len(teams) == 0 {
				v.status.SetText("Could not load the team list. Your current favorites are shown so you can still remove them.")
			} else {
				v.status.SetText("")
			}
			v.applyFilter()
		})
	})
}

// applyFilter rebuilds the rows: favorites ESPN's list lacks first, then the
// teams matching the search.
func (v *settingsView) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(v.search.Text))
	known := map[string]bool{}
	for _, t := range v.teams {
		known[t.Abbr] = true
	}
	var rows []teamRow
	for _, abbr := range v.favorites() {
		if !known[abbr] && strings.Contains(strings.ToLower(abbr), query) {
			rows = append(rows, teamRow{Abbr: abbr, Extra: true})
		}
	}
	for _, t := range v.teams {
		if query == "" || strings.Contains(strings.ToLower(t.Abbr), query) || strings.Contains(strings.ToLower(t.Name), query) {
			rows = append(rows, teamRow{Abbr: t.Abbr, Name: t.Name})
		}
	}
	v.rows = rows
	v.list.Refresh()
	v.updateCount()
}
