package main

import (
	"context"
	"fmt"
	"log/slog"
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
	// divisions adds the pro sports' division records to the rows; nil skips them.
	divisions *core.DivisionCache
	slots     logoSlots
	abbrWidth float32 // width of the abbreviation columns, so every row lines up
	recWidth  float32 // width of the record columns
	rankWidth float32 // width of the ranking columns; 0 when no team in the list is ranked

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
	// contSport and contID name that game, so a list that shows it final hides the button.
	contSport, contID string
	contLabel         string

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
	// setSport remembers the sport the user picked, for the next start.
	setSport func(sport string)
}

func (g groupPrefs) saveSport(sport string) {
	if g.setSport != nil {
		g.setSport(sport)
	}
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
		p.prefs.saveSport(p.currentSport()) // the next start opens on this sport
		p.setGroups()                       // the new sport's remembered group; favorites apply as usual
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
			small := func(width func() float32) fyne.CanvasObject {
				t := canvas.NewText("", theme.Color(theme.ColorNamePlaceHolder))
				t.TextSize = theme.TextSize() * recordTextScale
				// Centered up and down; the column's width is shared by all rows.
				return container.New(columnLayout{width: width},
					container.NewVBox(layout.NewSpacer(), t, layout.NewSpacer()))
			}
			record := func() fyne.CanvasObject { return small(func() float32 { return p.recWidth }) }
			rank := func() fyne.CanvasObject { return small(func() float32 { return p.rankWidth }) }
			// Each team: logo, ranking, abbreviation, record. The ranking comes
			// before the abbreviation, as on a broadcast.
			teams := container.NewHBox(rowLogo(), rank(), column(bold()), record(), widget.NewLabel("@"),
				rowLogo(), rank(), column(bold()), record())
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
			// An idle team (bye, no game) is one team on the left; hide the "@" and the home side.
			for _, o := range teams[4:] {
				if g.State == core.IdleState {
					o.Hide()
				} else {
					o.Show()
				}
			}
			// With no ranked team in the list the ranking columns are not there
			// at all (an empty column would still add its gap).
			for _, i := range []int{1, 6} {
				if p.rankWidth == 0 {
					teams[i].Hide()
				} else if i == 1 || g.State != core.IdleState {
					teams[i].Show()
				}
			}
			p.slots.show(p.logos, teams[0].(*canvas.Image), g.AwayLogo, isDark())
			setRecord(teams[1], g.AwayRank)
			teams[2].(*fyne.Container).Objects[0].(*widget.Label).SetText(strings.TrimSpace(g.Away))
			setRecord(teams[3], awayRecord(g))
			p.slots.show(p.logos, teams[5].(*canvas.Image), g.HomeLogo, isDark())
			setRecord(teams[6], g.HomeRank)
			teams[7].(*fyne.Container).Objects[0].(*widget.Label).SetText(strings.TrimSpace(g.Home))
			setRecord(teams[8], homeRecord(g))
			box.Objects[2].(*widget.Label).SetText(gameDetail(g, time.Now()))
		},
	)
	p.list.OnSelected = func(i widget.ListItemID) {
		p.list.UnselectAll()
		if i < len(p.games) && p.games[i].State != core.IdleState {
			g := p.games[i]
			slog.Debug("Picker pick", "sport", p.currentSport(), "game", g.ID, "matchup", matchup(g), "state", g.State)
			p.onPick(p.currentSport(), g)
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
	p.contLabel = label
	p.cont.Show()
}

// continueFor tells the picker which game the Continue button follows.
func (p *picker) continueFor(sport, id string) { p.contSport, p.contID = sport, id }

// hideFinishedContinue hides the Continue button when the list just loaded
// shows its game as over, and shows it again otherwise (a game the list does
// not have, say in another group, is not known to be over).
func (p *picker) hideFinishedContinue(games []espn.Game) {
	if p.contLabel == "" || p.contID == "" || p.currentSport() != p.contSport {
		return
	}
	for _, g := range games {
		if g.ID == p.contID {
			if g.State == "post" {
				slog.Debug("Continue hidden: the game is final", "game", p.contID)
				p.cont.Hide()
			} else {
				p.cont.Show()
			}
			return
		}
	}
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

// widestRank is how wide the ranking columns need to be for these games: the
// widest ranking in the list, 0 when none of the teams is ranked.
func widestRank(games []espn.Game) float32 {
	var w float32
	t := canvas.NewText("", theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.TextSize() * recordTextScale
	for _, g := range games {
		for _, r := range []string{g.AwayRank, g.HomeRank} {
			if r == "" {
				continue
			}
			t.Text = r
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
		if err == nil {
			// Teams in the group with no game on this scoreboard, after the games
			// (not for Top 25, which is no group of teams).
			if group != sports.RankedGroupID {
				if teams, terr := espn.FetchStandingTeams(p.ctx, p.fetcher, sport, group); terr == nil {
					games = append(games, core.IdleGames(sport, games, teams)...)
				}
			}
		}
		if err == nil && p.divisions != nil {
			core.AddDivisionRecords(games, p.divisions.Load(p.ctx, sport, time.Now()))
		}
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
			p.hideFinishedContinue(games)
			p.show(games)
		})
	})
}

// countGames is the number of real games in a list, not counting byes.
func countGames(games []espn.Game) int {
	n := 0
	for _, g := range games {
		if g.State != core.IdleState {
			n++
		}
	}
	return n
}

// show fills the list from the scoreboard answer. Favorite teams narrow it
// when any of them is in it (whatever group is chosen); if none is, the whole
// scoreboard shows and the status says so. "All games" skips the narrowing.
func (p *picker) show(games []espn.Game) {
	sport := p.currentSport()
	favorites := p.favorites[sport]
	shown, status := games, fmt.Sprintf("%d game(s)", countGames(games))
	if len(favorites) > 0 && !p.all.Checked {
		if fav := core.FavoriteGames(games, favorites); len(fav) > 0 {
			shown = fav
			status = fmt.Sprintf("%d favorite-team game(s) of %d. Tick \"All games\" for the rest.", countGames(fav), countGames(games))
		} else if len(games) > 0 {
			status = fmt.Sprintf("None of your favorite teams play here; showing all %d.", countGames(games))
		}
	}
	p.games = orderForPicker(shown)
	slog.Debug("Picker list", "sport", sport, "group", p.group.Selected, "all", p.all.Checked,
		"favorites", len(favorites), "games", countGames(games), "idle", idleTeams(games),
		"shown", countGames(shown), "shown_idle", idleTeams(shown))
	p.abbrWidth = widestAbbreviation(p.games)
	p.recWidth = widestRecord(p.games)
	p.rankWidth = widestRank(p.games)
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

// idleTeams lists the teams with no game in a list, for the log.
func idleTeams(games []espn.Game) string {
	var teams []string
	for _, g := range games {
		if g.State == core.IdleState {
			teams = append(teams, strings.TrimSpace(g.Away))
		}
	}
	return strings.Join(teams, " ")
}
