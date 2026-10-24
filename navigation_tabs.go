package main

// The horizontal navigation's tabs: projects from the left, their sessions
// between them, and the utility icons at the trailing edge. The tabs are
// whole sessions; they never wrap, and they stop shrinking at a readable
// width, where the row scrolls and the overflow menu lists the sessions by
// project.

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
)

const (
	navIconW    = 28
	navIconGap  = 2
	navTabGap   = 2
	navGroupGap = 6
	navGroupPad = 2
	// navTabMin is the narrowest a session tab gets before the row scrolls,
	// and navTabMax the widest it grows. They are fixed design values, not
	// user settings.
	navTabMin = 80
	// navTabCompact is the preferred width at or below which a title stays
	// whole; only longer tabs give up room when the row is short of space.
	navTabCompact = 120
	navTabMax     = 220
	// navProjectMax keeps a long project name from taking the tabs' room.
	navProjectMax = 180
	// navAttentionW is the attention count's fixed width, so that the tab
	// widths do not depend on how the count reads.
	navAttentionW = 46
	navRowH       = 30
)

// navBar is the row of tabs, over the panes or under them. It takes the
// title bar's line at the top, where the traffic lights are, and fades away
// when the user hides navigation with Cmd+B.
func (a *App) navBar(c *ui.Context, pal *palette, bar ui.TitleBar) {
	top := a.navPlacement() == navTop
	pad := float32(cardGap)
	if top {
		pad = lead(bar)
	}
	row := ui.Row(c.Key("nav-bar")).Shrink(0).AlignItems(ui.Center).ClipY().
		Padding(0, 8, 0, pad).DragWindow()
	h := row.AnimateTo("h", b2f(!a.prefs.SidebarHidden)*topBar, slide)
	row.Height(h)
	if h < 1 {
		return
	}
	row.Children(func() { a.navBarContent(c, pal, bar, top) })
}

// navBarContent places the scrolling tabs and the fixed utilities. Project
// header and utility widths are taken out of the window first, then each
// session tab takes its content's width within the shared room.
func (a *App) navBarContent(c *ui.Context, pal *palette, bar ui.TitleBar, top bool) {
	if !a.plugins.knownAt {
		a.discoverPlugins()
	}
	winW, _ := c.Size()
	avail := winW - lead(bar) - 8
	if !top {
		avail = winW - 2*cardGap
	}
	groups := a.groups()
	waiting := a.navWaiting()
	badConn := a.conn != connReady || a.daemonStale()

	headerW := make(map[string]float32, len(groups))
	var headers, gaps float32
	for i, g := range groups {
		w := a.navProjectWidth(c, g)
		headerW[g.project.ID] = w
		headers += w
		children := 1
		if !a.prefs.Collapsed[g.project.ID] {
			children += len(g.sessions)
		}
		gaps += float32(children-1) * navTabGap
		if i > 0 {
			gaps += navGroupGap
		}
	}
	groupPads := float32(len(groups)) * 2 * navGroupPad

	preferredW := a.navPreferredSessionWidths(c, pal, groups)
	var floorTotal float32
	for _, preferred := range preferredW {
		floorTotal += navSessionFloor(preferred)
	}

	pins := a.pinRows()
	utilsNoPins := navUtilitiesWidth(waiting > 0, badConn, 0)
	pinsW := navUtilitiesWidth(false, false, len(pins)) - navUtilitiesWidth(false, false, 0)
	// Optional plugin shortcuts must fit beside the same compact-title floors
	// the tab allocator protects; otherwise they move, labelled, into overflow.
	showPins := false
	if len(pins) > 0 {
		availNoPins := avail - utilsNoPins - gaps - headers - groupPads
		showPins = availNoPins-floorTotal >= pinsW
	}
	shownPins := 0
	if showPins {
		shownPins = len(pins)
	}
	utilsW := navUtilitiesWidth(waiting > 0, badConn, shownPins)

	room := avail - utilsW - gaps - headers - groupPads
	tabW := navFitSessionWidths(preferredW, room)

	scroll := ui.ScrollHorizontal(c.Key("nav-tabs")).TrackScroll(&a.nav.scroll).Grow(1).MinWidth(0).Height(navRowH)
	scroll.Children(func() {
		ui.Row(c.Key("nav-tab-row")).Gap(navGroupGap).AlignItems(ui.Center).FillHeight().Children(func() {
			for _, g := range groups {
				a.navProjectGroup(c, pal, g, headerW[g.project.ID], tabW)
			}
		})
	})

	utils := ui.Row(c.Key("nav-utils")).Shrink(0).Gap(navIconGap).AlignItems(ui.Center)
	utils.Children(func() {
		a.navUtilities(c, pal, pins, showPins, waiting, badConn)
	})
}

// navPreferredSessionWidths measures the tabs once for both pin eligibility
// and allocation, so those decisions share the same titles and compact floors.
func (a *App) navPreferredSessionWidths(c *ui.Context, pal *palette, groups []group) map[string]float32 {
	widths := make(map[string]float32)
	for _, g := range groups {
		if a.prefs.Collapsed[g.project.ID] {
			continue
		}
		for _, s := range g.sessions {
			widths[s.ID] = a.navSessionWidth(c, pal, s)
		}
	}
	return widths
}

// navSessionFloor is the width a tab keeps even when the row is short of room.
func navSessionFloor(preferred float32) float32 {
	if preferred <= navTabCompact {
		return preferred
	}
	return navTabMin
}

// navFitSessionWidths fits measured tabs to the room. Compact titles stay
// whole; longer tabs share the remaining room, and the row scrolls when even
// their readable floors do not fit.
func navFitSessionWidths(preferredW map[string]float32, room float32) map[string]float32 {
	widths := make(map[string]float32, len(preferredW))
	floors := make(map[string]float32, len(preferredW))
	var desiredTotal, floorTotal, flexibleTotal float32
	for id, desired := range preferredW {
		floor := navSessionFloor(desired)
		widths[id] = desired
		floors[id] = floor
		desiredTotal += desired
		floorTotal += floor
		flexibleTotal += desired - floor
	}
	if desiredTotal <= room {
		return widths
	}
	if floorTotal >= room {
		return floors
	}
	scale := (room - floorTotal) / flexibleTotal
	for id, desired := range widths {
		widths[id] = floors[id] + (desired-floors[id])*scale
	}
	return widths
}

// navSessionWidth is a tab's preferred width: its full title, status, actual
// chrome and the close button's reserved space, capped at the usual maximum.
func (a *App) navSessionWidth(c *ui.Context, pal *palette, s *proto.Session) float32 {
	title := s.DisplayName(a.set.SessionName)
	weight := 400
	if s.Unread {
		weight = 650
	}
	titleW, _ := c.MeasureText(0, ui.Span{Text: title, Size: 13, Weight: weight})
	fixed := float32(16 /* padding */ + 13 /* status */ + 16 /* close */ + 12 /* gaps */)
	if pal.dark {
		fixed += 2 // the border navSession draws only in the dark
	}
	if a.prefs.SessionPins[s.ID] && symbolImage("pin.fill", 10, pal.dark) != nil {
		fixed += 10 + 6
	}
	return min(max(float32(math.Ceil(float64(titleW+fixed))), navTabMin), navTabMax)
}

// navChosenFill is the selected tab's and folded project header's fill. The
// dark palette's row fill is translucent; inside the tabs' scrolling layer
// an opaque raised surface reads better, so the dark uses the control fill.
func navChosenFill(pal *palette) ui.Color {
	if pal.dark {
		return pal.control
	}
	return pal.rowSelected
}

// navWaiting is how many sessions wait on the user.
func (a *App) navWaiting() int {
	n := 0
	for i := range a.st.Sessions {
		if a.st.Sessions[i].Status == proto.Waiting {
			n++
		}
	}
	return n
}

// navUtilitiesWidth is the room the trailing controls take, with n plugin
// shortcuts among them.
func navUtilitiesWidth(waiting, badConn bool, n int) float32 {
	count := 5 + n // new session, search, usage, settings, overflow
	var w float32
	if waiting {
		count++
	}
	if badConn {
		count++
	}
	w = float32(count-1)*navIconGap + float32(count)*navIconW
	if waiting {
		w += navAttentionW - navIconW
	}
	return w
}

// navUtilities draws the trailing controls: new session and search, the
// plugin shortcuts when they fit, usage, attention, the connection, settings
// and the overflow menu.
func (a *App) navUtilities(c *ui.Context, pal *palette, pins []pinRow, showPins bool, waiting int, badConn bool) {
	b := navIconButton(c, "nav-new", "plus", plusIcon, locales.ActionNewSession(), a.tip(locales.ActionNewSession(), "session.new"), pal)
	if b.Clicked() {
		a.newSession("")
	}
	b = navIconButton(c, "nav-search", "magnifyingglass", searchIcon, locales.ActionGoToSession(), a.tip(locales.ActionGoToSession(), "palette.open"), pal)
	if b.Clicked() {
		a.openPalette()
	}
	if showPins {
		for _, r := range pins {
			a.navPin(c, pal, r)
		}
	}
	a.usageNavButton(c, pal)
	if waiting > 0 {
		a.navAttention(c, pal, waiting)
	}
	if badConn {
		a.navConnection(c, pal)
	}
	b = navIconButton(c, "nav-settings", "gearshape", gearIcon, locales.SettingsTitle(), a.tip(locales.SettingsTitle(), "settings.open"), pal)
	if b.Clicked() {
		a.openSettings("")
	}
	overflow := navIconButton(c, "nav-overflow", "ellipsis", nil, locales.NavOverflow(), locales.NavOverflowTip(), pal)
	menuPins := pins
	if showPins {
		// Visible shortcuts stay in the row; the overflow represents the
		// controls that had to move out of it.
		menuPins = nil
	}
	overflow.Menu(func(m *ui.Menu) { a.navOverflowMenu(m, menuPins) })
}

// navProjectWidth is the room a project header takes: its name, its session
// count, the chevron, and, collapsed, how many sessions need the user.
func (a *App) navProjectWidth(c *ui.Context, g group) float32 {
	nameW, _ := c.MeasureText(0, ui.Span{Text: g.project.Name, Size: 12.5, Weight: 600})
	nameW = min(nameW, navProjectMax)
	countW, _ := c.MeasureText(0, ui.Span{Text: fmt.Sprint(len(g.sessions)), Size: 12, Features: "tnum"})
	w := nameW + countW + 11 /* chevron */ + 18 /* padding */ + 2*6 /* gaps */
	if a.prefs.Collapsed[g.project.ID] {
		if needs := attentionNeeds(g); needs > 0 {
			needW, _ := c.MeasureText(0, ui.Span{Text: locales.SidebarNeedsYou(needs), Size: 12})
			w += 6 + min(needW, 96)
		}
	}
	return max(72, min(w, 260))
}

// navProjectGroup keeps a project's header and its session tabs in one glass
// capsule. The space between capsules separates projects; the quieter space
// and selected tab's raised fill separate the sessions inside one.
func (a *App) navProjectGroup(c *ui.Context, pal *palette, g group, headerW float32, tabW map[string]float32) {
	id := g.project.ID
	frame := ui.Row(c.Key("nav-group-" + id)).Shrink(0).Gap(navTabGap).Padding(navGroupPad).
		AlignItems(ui.Center).FillHeight()
	frame.InsetShadow(0, 0, 0, 0.5, pal.hairline)
	glassToolbar(c, frame, pal).Radius(concentric(7, navGroupPad)).Children(func() {
		a.navProject(c, pal, g, headerW)
		if a.prefs.Collapsed[id] {
			return
		}
		for _, s := range g.sessions {
			a.navSession(c, pal, s, tabW[s.ID])
		}
	})
}

// navProject is a project's header in the tabs: its name and session count,
// folded or unfolded. Clicking it toggles its sessions and never changes
// the selection; a collapsed active project stays marked, and its session
// keeps the panes in view.
func (a *App) navProject(c *ui.Context, pal *palette, g group, width float32) {
	id := g.project.ID
	collapsed := a.prefs.Collapsed[id]
	needs := attentionNeeds(g)
	active := false
	for _, s := range g.sessions {
		active = active || s.ID == a.selected
	}
	renaming := id != "" && a.renamingProject == id
	hdr := ui.Row(c.Key("nav-project-"+id)).Width(width).Height(26).Shrink(0).Padding(0, 9).Gap(6).
		AlignItems(ui.Center).Radius(7).Focusable().Label(g.project.Name)
	bg := ui.Transparent
	switch {
	case collapsed && active:
		bg = navChosenFill(pal)
		chosen(hdr, pal)
	case hdr.Hovered():
		bg = pal.rowHover
	}
	hdr.Background(hdr.AnimateColor("bg", bg, hover))
	if !renaming {
		a.navProjectDrag(c, pal, hdr, id)
	}
	pointerTooltip(c, hdr, "nav-tip-project-"+id, proto.ShortDir(g.project.Dir))
	hdr.ContextMenu(func(m *ui.Menu) { a.projectMenu(m, g) })
	hdr.Children(func() {
		if renaming {
			in := ui.TextInputBase(c.Key("rename"), &a.renameBuf).Grow(1).MinWidth(0).FontSize(12.5).FontWeight(600).AutoFocus()
			if in.Shortcut(0, ui.KeyEscape) {
				a.renamingProject = ""
				a.focusTerminal()
			} else if in.Submitted() || (!in.Focused() && !in.Pressed() && in.Valid() && a.renameStarted(c)) {
				a.commitProjectRename()
			}
			return
		}
		nameColor := pal.textMuted
		if collapsed && active {
			nameColor = pal.text
		}
		ui.Text(c, g.project.Name).FontSize(12.5).FontWeight(600).TextColor(nameColor).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		ui.Textf(c, "%d", len(g.sessions)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").Shrink(0)
		if collapsed && needs > 0 {
			ui.Text(c, locales.SidebarNeedsYou(needs)).FontSize(12).FontWeight(500).
				TextColor(pal.attention).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		chev := controlIcon(c, chevronIcon, 11, pal.textFaint, pal.dark)
		turn := chev.AnimateTo("turn", 90*(1-b2f(collapsed)), row)
		chev.Rotate(turn).Opacity(chev.Animate("shown", b2f(hdr.Hovered() || collapsed), hover.Duration))
	})
	if !renaming && (hdr.Clicked() ||
		hdr.Focused() && (c.Shortcut(0, ui.KeyEnter) || c.Shortcut(0, ui.KeySpace))) {
		a.toggleProject(id)
	}
}

// navSession is one session's tab: its status, its title shortened, its pin,
// and a close button while the pointer is over it or it has the keyboard.
// Its title stays whole in the tooltip and for assistive technology.
func (a *App) navSession(c *ui.Context, pal *palette, s *proto.Session, width float32) {
	sel := s.ID == a.selected
	title := s.DisplayName(a.set.SessionName)
	tab := ui.Row(c.Key("nav-session-"+s.ID)).Width(width).Height(26).Shrink(0).Padding(0, 8).Gap(6).
		AlignItems(ui.Center).Radius(7).Focusable().Label(locales.NavSessionTab(title)).Tooltip(title)
	if pal.dark {
		tab.Border(1, ui.Transparent)
	}
	bg := ui.Transparent
	switch {
	case sel:
		bg = navChosenFill(pal)
		chosen(tab, pal)
	case tab.Hovered():
		bg = pal.rowHover
	}
	tab.Background(tab.AnimateColor("bg", bg, hover))
	over := a.navSessionDrag(c, pal, tab, s)
	a.navProjectDrop(c, pal, tab, s.Project)
	tab.ContextMenu(func(m *ui.Menu) { a.sessionMenu(m, s) })

	closing := false
	tab.Children(func() {
		content := ui.Row(c.Key("tab-content")).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center)
		if over {
			content.Opacity(0.15)
		}
		content.Children(func() {
			statusIconP(c, pal, s.Status, s.Unread, s.Progress, s.Since, 13)
			if a.prefs.SessionPins[s.ID] {
				symbolMark(c, "pin.fill", 10, pal.textMuted, pal.dark)
			}
			if a.renaming == s.ID {
				in := ui.TextInputBase(c.Key("rename"), &a.renameBuf).Grow(1).MinWidth(0).FontSize(13).AutoFocus()
				if in.Shortcut(0, ui.KeyEscape) {
					a.renaming = ""
					a.focusTerminal()
				} else if in.Submitted() || (!in.Focused() && !in.Pressed() && in.Valid() && a.renameStarted(c)) {
					a.commitRename()
				}
				return
			}
			name := ui.Text(c, title).FontSize(13).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).TextColor(pal.text)
			if s.Unread {
				name.FontWeight(650)
			}
			if inactive(s.Status, s.Unread, s.Since, c.Now()) {
				name.TextColor(pal.textMuted)
			}
		})
		if tab.Hovered() || tab.FocusWithin() {
			x := ui.ButtonBase(c.Key("close")).Size(16, 16).Radius(8).Center().Shrink(0).
				Label(locales.ActionCloseSession()).Tooltip(locales.ActionCloseSession())
			if x.Hovered() {
				x.Background(pal.rowHover)
			}
			x.Children(func() { controlIcon(c, closeIcon, 9, pal.textMuted, pal.dark) })
			if x.Clicked() {
				closing = true
				id := s.ID
				a.defer_(func() { a.closeSession(id, true) })
			}
		} else {
			// The close button keeps its room, so the title does not move
			// as the pointer crosses the tab.
			ui.Box(c).Size(16, 16).Shrink(0)
		}
	})
	if tab.Clicked() && !closing && !sel {
		a.selectRevealing(s.ID)
	}
	if tab.DoubleClicked() {
		a.startRename(s.ID)
	}
	if tab.Focused() && (c.Shortcut(0, ui.KeyEnter) || c.Shortcut(0, ui.KeySpace)) && !closing {
		a.selectRevealing(s.ID)
	}
	if a.navReveal(s.ID) || tab.FocusVisible() {
		tab.ScrollIntoView()
	}
}

// navSessionDrag moves a session tab within its project and pin section, as
// the sidebar's row does. The insertion rule is a vertical line, the
// direction the tabs run.
func (a *App) navSessionDrag(c *ui.Context, pal *palette, tab ui.Element, s *proto.Session) (over bool) {
	if a.renaming == s.ID || a.conn != connReady {
		return false
	}
	tab.Drag(sessionDrag{s.ID}).Cursor(ui.CursorGrab)
	sidebarDragSource(c, tab, pal)
	if d, hovering := ui.DragOver[sessionDrag](tab); hovering {
		after := navDropAfter(tab)
		if _, ok := a.sessionMove(d.id, s.ID, after); ok {
			navInsertion(tab, pal, after)
			over = true
		}
	}
	if d, dropped := ui.Drop[sessionDrag](tab); dropped {
		id, target, after := d.id, s.ID, navDropAfter(tab)
		a.defer_(func() { a.reorderSession(id, target, after) })
	}
	return over
}

// navProjectDrag reorders whole projects by their headers, as the sidebar's
// group drag does. Session membership and order stay with each project.
func (a *App) navProjectDrag(c *ui.Context, pal *palette, hdr ui.Element, id string) {
	if id == "" || a.conn != connReady {
		return
	}
	hdr.Drag(projectDrag{id}).Cursor(ui.CursorGrab)
	sidebarDragSource(c, hdr, pal)
	if d, over := ui.DragOver[projectDrag](hdr); over {
		after := navDropAfter(hdr)
		if _, ok := a.projectMove(d.id, id, after); ok {
			navInsertion(hdr, pal, after)
		}
	}
	if d, dropped := ui.Drop[projectDrag](hdr); dropped {
		source, after := d.id, navDropAfter(hdr)
		a.defer_(func() { a.reorderProject(source, id, after) })
	}
}

// navProjectDrop lets a project dragged by its header land on a session tab
// of another project, as it lands on another group in the sidebar.
func (a *App) navProjectDrop(c *ui.Context, pal *palette, tab ui.Element, project string) {
	if project == "" || a.conn != connReady {
		return
	}
	if d, over := ui.DragOver[projectDrag](tab); over {
		after := navDropAfter(tab)
		if _, ok := a.projectMove(d.id, project, after); ok {
			navInsertion(tab, pal, after)
		}
	}
	if d, dropped := ui.Drop[projectDrag](tab); dropped {
		source, after := d.id, navDropAfter(tab)
		a.defer_(func() { a.reorderProject(source, project, after) })
	}
}

// navDropAfter tells where a dragged tab or project goes in a row: after the
// target when the pointer is past its middle.
func navDropAfter(target ui.Element) bool {
	x, _, _ := target.PointerPosition()
	return x > target.Bounds().W/2
}

// navInsertion marks where a dragged tab or project goes: a neutral vertical
// rule, clear of the accent that marks the session shown.
func navInsertion(target ui.Element, pal *palette, after bool) {
	target.DrawOver(func(p *ui.Painter, r ui.Rect) {
		const thickness = 2
		x := r.X
		if after {
			x += r.W - thickness
		}
		p.Fill(ui.Rect{X: x, Y: r.Y + 4, W: thickness, H: max(0, r.H-8)}, pal.textMuted.Alpha(0.8), thickness/2)
	})
}

// navPin is a pinned plugin's icon in the tabs: a click focuses its pane or
// opens one, as the sidebar's pin row does.
func (a *App) navPin(c *ui.Context, pal *palette, r pinRow) {
	b := ui.ButtonBase(c.Key("nav-pin-"+r.ref.Plugin+"-"+r.ref.Widget)).Size(navIconW, 26).Radius(6).
		Center().Shrink(0).Label(r.name).Tooltip(r.name)
	if b.Pressed() {
		b.Background(pal.rowHover.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(pal.rowHover)
	}
	tint := pal.textMuted
	if b.Hovered() {
		tint = pal.text
	}
	b.Children(func() {
		if r.icon == "" {
			controlIcon(c, projectIcon, 15, tint, pal.dark)
			return
		}
		view := a.plugins.pinView
		if view == nil {
			view = newPluginView()
			a.plugins.pinView = view
		}
		if sym, scale := view.symbol(c, r.icon, 16, symbolMedium); sym != nil {
			drawSymbol(c, "symbol", sym, scale, tint)
		} else if svg, ok := pluginIcon(r.icon); ok {
			controlIcon(c.Key("icon"), svg, 15, tint, pal.dark)
		}
	})
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.PluginUnpin()).Chosen() {
			a.unpinPlugin(r.ref.Plugin, r.ref.Widget)
		}
	})
	if b.Clicked() {
		a.openPin(r.ref)
	}
}

// navAttention is the count of sessions that need the user. It is a fixed
// width so that the tab widths never depend on how many wait.
func (a *App) navAttention(c *ui.Context, pal *palette, n int) {
	words := locales.FooterNeedsYou(n)
	b := ui.ButtonBase(c.Key("nav-attention")).Width(navAttentionW).Height(26).Padding(0, 7).Gap(4).
		Radius(13).Shrink(0).AlignItems(ui.Center).Label(words).Tooltip(words + " · " + locales.FooterGoToIt())
	if b.Hovered() {
		b.Background(pal.rowHover)
	}
	b.Children(func() {
		statusIcon(c, pal, proto.Waiting, false, 12)
		ui.Textf(c, "%d", n).FontSize(12).FontWeight(500).TextColor(pal.attention).FontFeatures("tnum")
	})
	if b.Clicked() {
		a.jumpToAttention()
	}
}

// navConnection marks a service that is not ready: connecting, or gone. It
// stays in the trailing cluster in every layout.
func (a *App) navConnection(c *ui.Context, pal *palette) {
	if a.daemonStale() {
		label := locales.CommonRestartService()
		b := navIconButton(c, "nav-connection", "arrow.clockwise", updateIcon, label, locales.UpdateRestartTip(), pal)
		if b.Clicked() {
			a.offerRestart()
		}
		return
	}
	label := locales.StatusDisconnected()
	if a.conn == connConnecting {
		label = locales.StatusConnecting()
	}
	b := navIconButton(c, "nav-connection", "exclamationmark.triangle.fill", nil, label, label, pal)
	if b.Clicked() && a.conn == connFailed {
		a.retries = 0
		a.conn = connConnecting
		a.connect()
	}
}

// navOverflowMenu lists the sessions by project, the projects' own menu, and
// the plugin shortcuts that did not fit. A collapsed project is represented
// by its header, so its sessions are not listed here.
func (a *App) navOverflowMenu(m *ui.Menu, pins []pinRow) {
	if m.Item(locales.ActionOpenProject()).Chosen() {
		a.defer_(a.openProject)
	}
	if m.Item(locales.NavHideTabs()).Chosen() {
		a.defer_(a.toggleSidebar)
	}
	for _, g := range a.groups() {
		if a.prefs.Collapsed[g.project.ID] {
			continue
		}
		name := g.project.Name
		if g.project.ID == "" {
			name = locales.SidebarOtherProject()
		}
		if len(g.sessions) > 0 {
			m.Submenu(name, func(sub *ui.Menu) {
				for _, s := range g.sessions {
					if sub.Item(s.DisplayName(a.set.SessionName)).Checked(s.ID == a.selected).Chosen() {
						id := s.ID
						a.defer_(func() { a.selectRevealing(id) })
					}
				}
			})
		}
	}
	if len(pins) > 0 {
		m.Separator()
		m.Submenu(locales.PagePlugins(), func(sub *ui.Menu) {
			for _, r := range pins {
				ref := r.ref
				if sub.Item(r.name).Chosen() {
					a.defer_(func() { a.openPin(ref) })
				}
			}
		})
	}
}

// navIconButton is a quiet square button with an icon, as the sidebar's are,
// for the tab row's trailing controls. It returns the element, so that a
// caller can make it the usage popover's anchor or open a menu from it.
func navIconButton(c *ui.Context, key, symbol string, fallback *ui.SVG, label, tip string, pal *palette) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(navIconW, 26).Radius(6).Center().Shrink(0).
		TextColor(pal.textMuted).Label(label).Tooltip(tip)
	if b.Pressed() {
		b.Background(pal.rowHover.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(pal.rowHover)
		b.TextColor(pal.text)
	}
	tint := pal.textMuted
	if b.Hovered() {
		tint = pal.text
	}
	b.Children(func() {
		if symbol != "" && symbolMark(c, symbol, 16, tint, pal.dark) {
			return
		}
		if fallback != nil {
			controlIcon(c, fallback, 16, tint, pal.dark)
		}
	})
	return b
}
