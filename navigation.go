package main

// Navigation is where the sessions and their projects live: a sidebar on
// either side, or a row of tabs across the top or the bottom. The placement
// is one saved choice; a horizontal placement replaces the sidebar, and
// its tabs are whole sessions, each with the panes it already has.

import (
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
)

// navState is what horizontal navigation keeps between frames: how far its
// tabs are scrolled, and the session whose tab the next frame reveals.
type navState struct {
	scroll ui.ScrollState
	reveal string
}

// navPlacement is where navigation lives.
type navPlacement string

const (
	navLeft   navPlacement = "left"
	navRight  navPlacement = "right"
	navTop    navPlacement = "top"
	navBottom navPlacement = "bottom"
)

// navPlacements are the placements, in the order Settings offers them.
var navPlacements = []navPlacement{navLeft, navRight, navTop, navBottom}

// navPlacementAction is the action that chooses a placement.
func navPlacementAction(p navPlacement) string { return "nav.placement." + string(p) }

// validNavPlacement reads a placement, the left sidebar for anything that is
// not one of the four.
func validNavPlacement(s string) navPlacement {
	switch navPlacement(s) {
	case navRight:
		return navRight
	case navTop:
		return navTop
	case navBottom:
		return navBottom
	}
	return navLeft
}

// navPlacement is the placement the app shows now.
func (a *App) navPlacement() navPlacement { return validNavPlacement(a.prefs.NavPlacement) }

// navVertical tells that navigation is a sidebar.
func (a *App) navVertical() bool {
	p := a.navPlacement()
	return p == navLeft || p == navRight
}

// navHorizontal tells that navigation is a row of tabs.
func (a *App) navHorizontal() bool { return !a.navVertical() }

// setNavPlacement moves navigation, and saves the choice. It changes only
// the shell: the selected session, its panes, their terminals and the
// focused pane stay as they are.
func (a *App) setNavPlacement(p navPlacement) {
	p = validNavPlacement(string(p))
	if a.prefs.NavPlacement == string(p) {
		return
	}
	a.dismissNavUsage(false)
	a.prefs.NavPlacement = string(p)
	a.nav.reveal = ""
	a.savePrefs()
	// The View menu checks the placement in use: build it again with the
	// check moved.
	if a.win != nil {
		mygo.App.SetMenu(a.menu())
	}
	a.invalidateMain()
}

// navChrome is where the shell's animation puts things, for the placement.
// A sidebar on the left keeps the geometry it always had, its title bar in
// the traffic lights' line; a sidebar on the right leaves the lights over
// the panes, whose header makes room for them. Tabs across the top take the
// title bar; tabs across the bottom leave it to the panes.
func (a *App) navChrome(progress, width float32, bar ui.TitleBar) chrome {
	place := a.navPlacement()
	if place == navLeft {
		return chromeFor(progress, width, bar)
	}
	round := func(v float32) float32 { return float32(math.Round(float64(v))) }
	s := min(max(progress, 0), 1)
	top := round(barMid - headerH/2)
	if place == navTop {
		// With the tabs hidden the panes come up to the lights' line, as
		// they do with a hidden sidebar.
		top = round(cardGap - (cardGap-(barMid-headerH/2))*(1-s))
	}
	// With navigation hidden, the header holds the reveal and new-session
	// buttons after the traffic lights: the title makes room for them as the
	// surface goes, so it never draws over a button.
	headerPad := lead(bar) + 16 - cardGap + 60*(1-s)
	return chrome{
		shown:     s,
		sideW:     round(width * s),
		mainPad:   cardGap,
		headerPad: headerPad,
		topPad:    top,
	}
}

// selectRevealing selects a session that the user asked for by name: a
// palette row, an overflow row, a shortcut or another entry point. A
// project collapsed over it opens, and a tab scrolled out of sight comes
// into view. Explicitly collapsing a project calls none of this.
func (a *App) selectRevealing(id string) {
	s := a.session(id)
	if s == nil {
		return
	}
	if group := a.displayedGroup(s.Project); a.prefs.Collapsed[group] {
		delete(a.prefs.Collapsed, group)
		a.savePrefs()
	}
	a.requestReveal(id)
	a.selectSession(id)
}

// requestReveal asks navigation to bring a session's tab into view on the
// next frame, without changing which session is selected or which page
// shows.
func (a *App) requestReveal(id string) { a.nav.reveal = id }

// navRevealScrolls tells whether a session's tab should be scrolled into
// view in this frame.
func (a *App) navReveal(id string) bool {
	if a.nav.reveal != id {
		return false
	}
	a.nav.reveal = ""
	return true
}

// navShowTitle and navHideTitle name the reveal and hide controls for the
// surface that is there: a sidebar, or the tabs.
func (a *App) navShowTitle() string {
	if a.navHorizontal() {
		return locales.NavShowTabs()
	}
	return locales.SidebarShow()
}

func (a *App) navHideTitle() string {
	if a.navHorizontal() {
		return locales.NavHideTabs()
	}
	return locales.SidebarHide()
}

// navRevealButton shows navigation that the user hid: a sidebar, or the
// tabs' own mark rather than a sidebar's.
func (a *App) navRevealButton(c *ui.Context, pal *palette) {
	if a.navVertical() {
		iconButton(c, "show-sidebar", sidebarIcon, a.tip(a.navShowTitle(), "sidebar.toggle"), pal, a.toggleSidebar)
		return
	}
	name := "rectangle.topthird.inset.filled"
	if a.navPlacement() == navBottom {
		name = "rectangle.bottomthird.inset.filled"
	}
	b := navIconButton(c, "show-nav", name, sidebarIcon, a.navShowTitle(), a.tip(a.navShowTitle(), "sidebar.toggle"), pal)
	if b.Clicked() {
		a.toggleSidebar()
	}
}

// attentionNeeds counts the sessions of a group that wait on the user.
func attentionNeeds(g group) int {
	n := 0
	for _, s := range g.sessions {
		if s.Status == proto.Waiting {
			n++
		}
	}
	return n
}
