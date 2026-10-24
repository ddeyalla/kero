package main

import (
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/actions"
	"kero/internal/proto"

	"kero/locales"
)

const (
	// barMid is the line the traffic lights center on, and the sidebar's
	// buttons with them. topBar is the sidebar's title bar, centered on
	// it: 40 tall, as macOS's compact title bar and toolbar
	// (setCompactTitleBar), which AppKit centers the lights in. headerH is
	// a header of a card, which starts cardGap down: as tall as a pane's
	// title, 28, and its buttons need, apart from the lights' line.
	barMid  = 20
	topBar  = 2 * barMid
	headerH = 36
	cardGap = 8 // around the card
	rowH    = 34
)

// A project's header in the sidebar: projectHeaderH tall, projectGap below
// the project above it, and nothing between it and its first row, which
// has its own padding: the header reads as the heading of its rows.
const (
	projectHeaderH = 24
	projectGap     = 20
)

// chrome is where the sidebar's animation puts things, in whole pixels so
// that edges stay crisp. Each is rounded once, from one progress quantized
// to the sidebar's pixels, so that they move together: rounded apart,
// they would step at different frames, and a title, their sum, would
// shake by a pixel as the spring settles.
type chrome struct {
	shown     float32 // the sidebar's progress, 0 hidden to 1 shown
	sideW     float32 // the sidebar's width as shown
	mainPad   float32 // the left padding of the cards' column
	headerPad float32 // the left padding of the header at the left edge
	topPad    float32 // the top padding of the cards' column
}

// buttonsLeft places the sidebar's buttons that the header holds while
// the sidebar hides, in the header's coordinates, so that they stay where
// they are in the window, after the traffic lights, as the card slides.
func (ch chrome) buttonsLeft(bar ui.TitleBar) float32 {
	return lead(bar) - (ch.sideW + ch.mainPad)
}

// lead is where the sidebar's buttons start in the window, with the sidebar
// shown or not: after the traffic lights on macOS; elsewhere inside the
// card's edge, which they come over as the sidebar hides.
func lead(bar ui.TitleBar) float32 {
	if ownControls {
		return bar.Left + 2*cardGap
	}
	return bar.Left + cardGap
}

// headerButtons places the sidebar's buttons that a header holds while the
// sidebar hides: where they are in the window, after the traffic lights
// and centered on them as in the sidebar, as the card slides.
func (a *App) headerButtons(c *ui.Context, bar ui.TitleBar, shown float32) ui.Element {
	const h = 26 // iconButton's
	return ui.Row(c).Absolute().Left(a.chrome.buttonsLeft(bar)).Top(barMid - a.chrome.topPad - h/2).Height(h).AlignItems(ui.Center).Gap(2).Opacity(1 - 2*shown)
}

func chromeFor(progress, width float32, bar ui.TitleBar) chrome {
	round := func(v float32) float32 { return float32(math.Round(float64(v))) }
	w := round(width * min(max(progress, 0), 1))
	s := w / width
	pad := round(cardGap * (1 - s))
	// Where the header's title starts in the window, rounded once: after
	// the traffic lights and the sidebar's buttons with the sidebar
	// hidden, 16 into the card with it shown.
	hidden, shown := lead(bar)+16+60, width+16
	x := round(hidden + (shown-hidden)*s)
	// The cards start cardGap down with the sidebar shown, which has the
	// title bar. With it hidden, the first header has it on macOS: it
	// comes up to center on the traffic lights' line, and its title with
	// them, rather than 6 below. Elsewhere the window controls are the
	// header's own, and it stays.
	top := float32(cardGap)
	if !ownControls {
		top = round(cardGap - (cardGap-(barMid-headerH/2))*(1-s))
	}
	return chrome{shown: s, sideW: w, mainPad: pad, headerPad: x - w - pad, topPad: top}
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func (a *App) view(c *ui.Context) {
	for _, e := range a.terms {
		e.shown = false
	}
	pal := a.paletteFor(c)
	c.SetTheme(borderless(c, pal))
	// Every plugin pane this frame draws marks itself: the rest are
	// hidden, and hear it.
	a.beginPluginFrame()
	a.navUsage.drawn, a.navUsage.triggerOver = false, false

	if a.pendingToast != "" {
		c.Toast(a.pendingToast)
		a.pendingToast = ""
	}
	a.shortcuts(c)
	a.introStart(c)

	bar := a.titleBar(c)
	row := ui.Row(c).Fill().AlignItems(ui.Stretch)
	backdrop(c, pal, row, a.set)
	if p, ok := a.introShellP(c); ok {
		row.Opacity(p).Translate(0, a.introLift(c, p, 8))
	}
	// A curve, not a spring: see slide.
	progress := row.AnimateTo("sidebar", b2f(!a.prefs.SidebarHidden), slide)
	a.chrome = a.navChrome(progress, a.prefs.SidebarWidth, bar)
	shown := a.chrome.shown
	row.Children(func() {
		if a.navVertical() {
			a.verticalShell(c, pal, bar, shown)
			return
		}
		a.horizontalShell(c, pal, bar, shown)
	})
	// A popover whose icon is not on screen, as with navigation hidden,
	// goes with it.
	if a.navUsage.open && !a.navUsage.drawn {
		a.dismissNavUsage(false)
	}
	a.paletteView(c, pal)
	a.chooserView(c, pal)
	a.sweepViews()
	a.usageModal(c, pal)
	a.settingsModal(c, pal)
	a.confirmView(c, pal)
	a.introView(c, pal)
	// A preview that showed no more fades in anew next time.
	if !a.previewShown {
		a.previewKey = ""
	}
	a.previewShown = false
	// An image previewed no more closes, and lets go of its bitmap.
	if !a.peek.shown {
		a.closePeek()
		a.peek = peek{}
	}
	a.peek.shown = false
	if later := a.later; len(later) > 0 {
		a.later = nil
		for _, fn := range later {
			fn()
		}
		c.Invalidate()
	}
	// Apply the completed composition once, without toggling terminals
	// that remain visible from one frame to the next.
	for _, e := range a.terms {
		if e.term != nil {
			e.term.SetVisible(e.shown)
		}
	}
}

// verticalShell shows a sidebar beside the session's panes. The sidebar is
// on the left or the right of the panes, and the panes' column keeps the
// gaps that move the window.
func (a *App) verticalShell(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	place := a.navPlacement()
	w := a.chrome.sideW
	side := func() {
		box := ui.Row(c).Width(w).Shrink(0).AlignItems(ui.Stretch).ClipX()
		box.Children(func() {
			ui.Column(c).Width(a.prefs.SidebarWidth).Shrink(0).Margin(0, 0, 0, w-a.prefs.SidebarWidth).
				Opacity(0.4 + 0.6*shown).Children(func() { a.sidebar(c, pal, bar) })
		})
	}
	main := func() {
		col := ui.Column(c).Grow(1).MinWidth(0).Padding(a.chrome.topPad, cardGap, cardGap, a.chrome.mainPad).DragWindow()
		col.Children(func() {
			if shown > 0.01 {
				a.resizeHandle(c, pal, place == navRight)
			}
			a.card(c, pal, bar, shown)
		})
	}
	if place == navLeft {
		side()
		main()
		return
	}
	main()
	side()
}

// horizontalShell shows the session's panes under or over the navigation
// tabs. Horizontal navigation replaces the sidebar: the panes take the
// window's width, and the tabs take the title bar's line or the bottom's.
func (a *App) horizontalShell(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	top := a.navPlacement() == navTop
	main := ui.Column(c).Grow(1).MinWidth(0).DragWindow()
	main.Children(func() {
		if top {
			a.navBar(c, pal, bar)
		}
		body := ui.Column(c).Grow(1).MinHeight(0).
			Padding(a.chrome.topPad, cardGap, cardGap, a.chrome.mainPad)
		body.Children(func() { a.card(c, pal, bar, shown) })
		if !top {
			a.navBar(c, pal, bar)
		}
	})
}

// resizeHandle is the sidebar's edge, which a drag moves.
func (a *App) resizeHandle(c *ui.Context, pal *palette, atRight bool) {
	h := ui.Box(c.Key("resize")).Absolute().Top(0).Bottom(0).Width(9).Cursor(ui.CursorResizeColumn)
	if atRight {
		h.Right(-4)
	} else {
		h.Left(-4)
	}
	if dx, _, ok := h.Dragged(); ok {
		if a.sideDrag == 0 {
			a.sideDrag = a.prefs.SidebarWidth
		}
		// The edge is the sidebar's right on the left, and its left on the
		// right: dragging toward the window's own edge makes it wider.
		if atRight {
			a.sideDrag -= dx
		} else {
			a.sideDrag += dx
		}
		a.prefs.SidebarWidth = min(max(a.sideDrag, minSidebar), maxSidebar)
	} else if a.sideDrag != 0 && !h.Pressed() {
		a.sideDrag = 0
		a.savePrefs()
	}
	if h.DoubleClicked() {
		a.prefs.SidebarWidth = defaultSidebar
		a.savePrefs()
	}
}

// iconButton is a quiet square button with an icon, as in a toolbar.
func iconButton(c *ui.Context, key string, svg *ui.SVG, label string, pal *palette, fn func()) {
	b := ui.ButtonBase(c.Key(key)).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(label).Tooltip(label)
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
	b.Children(func() { controlIcon(c, svg, 16, tint, pal.dark) })
	if b.Clicked() {
		fn()
	}
}

// tipsAt are where pointerTooltip's tips show, by key: where the pointer
// rested as each came, which they keep while they show.
var tipsAt = map[string]ui.Point{}

// pointerTooltip shows s as el's tooltip, as Element.Tooltip does, but
// where macOS shows its tooltips: below the pointer where it came to rest,
// clear of the arrow, rather than centered above el, which a long row
// puts far from where the user looks.
func pointerTooltip(c *ui.Context, el ui.Element, key, s string) {
	el.Description(s)
	const below = 18 // the arrow's height, from its tip
	px, py, over := el.PointerPosition()
	at, showing := tipsAt[key]
	if !showing {
		b := el.Bounds()
		at = ui.Point{X: b.X + px, Y: b.Y + py + below}
	}
	var spot ui.Element
	if over {
		ui.Overlay(c, func() {
			spot = ui.Box(c.Key(key+"-at")).Absolute().Left(at.X).Top(at.Y).Size(0, 0).PassThrough()
		})
	}
	shown := false
	ui.TooltipBase(c, el, func(tip ui.Element) {
		shown = true
		th := c.Theme()
		fill, text := th.Inverse, th.InverseText
		if fill == ui.Transparent {
			fill = th.Text
		}
		if text == ui.Transparent {
			text = th.Background
		}
		tip.MaxWidth(th.Space(80)).Padding(th.Space(1.25), th.Space(2)).Radius(th.Space(1.25)).
			Background(fill).TextColor(text).FontSize(th.FontSize-1).Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
		tip.Children(func() { ui.Text(c, s) })
		if over {
			tip.AttachTo(spot, ui.AnchorTopLeft, ui.AnchorTopLeft)
		}
	})
	if shown {
		tipsAt[key] = at
	} else {
		delete(tipsAt, key)
	}
}

func kbd(c *ui.Context, pal *palette, keys string) {
	ui.Text(c, keys).FontSize(11).TextColor(pal.textMuted).Padding(1, 5).Radius(4).
		Background(pal.kbd).SingleLine()
}

// hoverIntent keeps hover chrome for a moment after the pointer leaves it:
// moving diagonally off a project's header should not flicker its chevron
// and add button away. The first frame a pointer is not over something
// shows nothing; only a leave after a hover is delayed.
func (a *App) hoverIntent(c *ui.Context, key string, over bool) bool {
	const leave = 80 * time.Millisecond
	if over {
		a.hoverSeen[key] = true
		delete(a.hoverUntil, key)
		return true
	}
	now := c.Now()
	if until, ok := a.hoverUntil[key]; ok {
		if now.Before(until) {
			c.After(until.Sub(now))
			return true
		}
		delete(a.hoverUntil, key)
		a.hoverSeen[key] = false
		return false
	}
	if !a.hoverSeen[key] {
		return false
	}
	a.hoverSeen[key] = false
	a.hoverUntil[key] = now.Add(leave)
	c.After(leave)
	return true
}

// --- the sidebar ---

func (a *App) sidebar(c *ui.Context, pal *palette, bar ui.TitleBar) {
	left := a.navPlacement() == navLeft
	pad := float32(12)
	if left {
		// After the traffic lights, which sit over the sidebar's title bar.
		pad = lead(bar)
	}
	ui.Row(c).Height(topBar).Shrink(0).AlignItems(ui.Center).Padding(0, 12, 0, pad).Gap(2).DragWindow().Children(func() {
		// On the left on macOS the buttons are at the right of the lights,
		// pushed there by a spacer; elsewhere the window controls are at
		// the right, and the buttons at the left, where they stay as the
		// sidebar hides (chrome.buttonsLeft). A sidebar on the right has
		// no lights over it, and its buttons lead it.
		if left && runtime.GOOS == "darwin" {
			ui.Spacer(c)
		}
		iconButton(c, "hide-sidebar", sidebarIcon, a.tip(a.navHideTitle(), "sidebar.toggle"), pal, a.toggleSidebar)
		iconButton(c, "new-session", plusIcon, a.tip(locales.ActionNewSession(), "session.new"), pal, func() { a.newSession("") })
		if !left {
			ui.Spacer(c)
		}
	})
	// The first row starts 30 below the lights' line, as it did under the
	// taller bar, and the rows' left edge lines up with the close light's,
	// 12 in, as the sidebar's footer does.
	scrollUnderBar(c, "sessions", &a.sidebarScroll, pal.sidebar, func(list ui.Element) {
		list.Padding(10, 12, 16, 12).Children(func() {
			if !a.loaded {
				return
			}
			a.pinNav(c, pal)
			n := 0 // the rows shown, for ⌘1–9 (Ctrl+1–9 elsewhere)
			for gi, g := range a.groups() {
				a.groupView(c, pal, g, gi, &n)
			}
			if len(a.st.Sessions) == 0 && a.conn == connReady {
				ui.Text(c, locales.SidebarNoSessions()).FontSize(12.5).TextColor(pal.textFaint).Padding(8, 10)
			}
		})
	})
	a.sidebarFooter(c, pal)
	// Times ago move on, a minute at a time as they read, while the
	// sidebar shows them (agoTick).
	if !a.prefs.SidebarHidden {
		agoTick(c)
	}
}

func (a *App) groupView(c *ui.Context, pal *palette, g group, gi int, n *int) {
	group := ui.Column(c.Key("group-" + g.project.ID)).Shrink(0).Transition(headerTransition)
	a.projectGroupDrag(pal, group, g.project.ID)
	group.Children(func() { a.projectGroupRows(c, pal, g, gi, n) })
}

func (a *App) projectGroupRows(c *ui.Context, pal *palette, g group, gi int, n *int) {
	id := g.project.ID
	collapsed := a.prefs.Collapsed[id]
	needs := 0
	for _, s := range g.sessions {
		if s.Status == proto.Waiting {
			needs++
		}
	}
	// A header has room above it and sits close to its rows, so that a
	// project reads as a heading over what it owns: projectGap between
	// projects, and as much under the pinned rows above the first. The
	// first, with nothing above it, starts where the rows always did.
	top := float32(projectGap)
	if gi == 0 && len(a.prefs.Pinned) == 0 {
		top = 2
	}
	hdr := ui.Row(c.Key("project-"+id)).Height(projectHeaderH).Margin(top, 0, 0, 0).Padding(0, 6, 0, 12).Gap(6).AlignItems(ui.Center).Radius(6).
		Transition(headerTransition)
	hovered := a.hoverIntent(c, "project-"+id, hdr.Hovered())
	renaming := id != "" && a.renamingProject == id
	if !renaming && id != "" && a.conn == connReady {
		hdr.Drag(projectDrag{id}).Cursor(ui.CursorGrab)
		sidebarDragSource(c, hdr, pal)
		if hdr.Dragging() {
			hovered = false
		}
	}
	if hdr.Clicked() && !renaming {
		a.toggleProject(id)
	}
	pointerTooltip(c, hdr, "tip-project-"+id, proto.ShortDir(g.project.Dir))
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
		ui.Text(c, g.project.Name).FontSize(12.5).FontWeight(600).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		ui.Textf(c, "%d", len(g.sessions)).FontSize(12.5).TextColor(pal.textFaint).FontFeatures("tnum").Shrink(0)
		chev := controlIcon(c, chevronIcon, 11, pal.textFaint, pal.dark)
		turn := chev.AnimateTo("turn", 90*(1-b2f(collapsed)), row)
		chev.Rotate(turn).Opacity(chev.Animate("shown", b2f(hovered || collapsed), hover.Duration))
		ui.Spacer(c)
		if needs > 0 {
			ui.Text(c, locales.SidebarNeedsYou(needs)).FontSize(12.5).FontWeight(500).TextColor(pal.attention).SingleLine().Shrink(0)
		}
		// The add button's place is kept while it is hidden, so that the
		// "needs you" before it stays put as the header is hovered.
		if !hovered {
			ui.Box(c).Size(20, 20).Shrink(0)
		} else {
			b := ui.ButtonBase(c.Key("add")).Size(20, 20).Radius(5).Center().TextColor(pal.textMuted).Label(locales.NewSessionIn(g.project.Name)).Tooltip(locales.ActionNewSession())
			tint := pal.textMuted
			if b.Hovered() {
				b.Background(pal.rowHover).TextColor(pal.text)
				tint = pal.text
			}
			b.Children(func() { controlIcon(c, plusIcon, 13, tint, pal.dark) })
			if b.Clicked() {
				a.newSession(id)
			}
		}
	})
	if collapsed {
		// The session shown stays in view.
		for _, s := range g.sessions {
			if s.ID == a.selected {
				a.sessionRow(c, pal, s, n)
			}
		}
		return
	}
	for _, s := range g.sessions {
		a.sessionRow(c, pal, s, n)
	}
}

// projectMenu is a project header's menu, shared by the sidebar and the
// horizontal tabs: a session in it, folding it, its order and its name, the
// Linear board it feeds, and closing it.
func (a *App) projectMenu(m *ui.Menu, g group) {
	id := g.project.ID
	collapsed := a.prefs.Collapsed[id]
	if m.Item(locales.ActionNewSession()).Chosen() {
		a.newSession(id)
	}
	label := locales.SidebarCollapse()
	if collapsed {
		label = locales.SidebarExpand()
	}
	if m.Item(label).Chosen() {
		a.toggleProject(id)
	}
	a.projectOrderMenu(m, &g.project)
	if m.Item(locales.SessionRename()).Disabled(id == "").Chosen() {
		a.startProjectRename(id)
	}
	if lin := a.st.Linear; id != "" && lin != nil && len(lin.Projects) > 0 {
		m.Submenu(locales.ProjectLinear(), func(m *ui.Menu) { a.projectLinearMenu(m, g.project) })
	}
	m.Separator()
	if m.Item(locales.SidebarCloseProjectMenu()).Disabled(id == "").Chosen() {
		name, count := g.project.Name, len(g.sessions)
		a.confirm = &confirmation{
			open:    true,
			title:   locales.ConfirmCloseTitle(name),
			detail:  locales.ConfirmCloseProjectDetail(count),
			action:  locales.SidebarCloseProject(),
			destroy: true,
			run:     func() { a.call("project.close", proto.IDParams{ID: id}, nil) },
		}
	}
}

func (a *App) sessionRow(c *ui.Context, pal *palette, s *proto.Session, n *int) {
	*n++
	index := *n
	selected := s.ID == a.selected
	row := ui.Row(c.Key(s.ID)).Height(rowH).Padding(0, 10, 0, 12).Gap(10).AlignItems(ui.Center).Radius(8).Margin(0, 0, 1, 0).
		Transition(rowTransition)
	if p, ok := a.introRowP(c, *n); ok {
		row.Opacity(p).Translate(0, a.introLift(c, p, 6))
	}
	hovered := row.Hovered()
	if pal.dark {
		// Room for the chosen row's edge in every row, so that choosing
		// one moves no text.
		row.Border(1, ui.Transparent)
	}
	bg := ui.Transparent
	switch {
	case selected:
		bg = pal.rowSelected
		chosen(row, pal)
	case hovered:
		bg = pal.rowHover
	}
	row.Background(row.AnimateColor("bg", bg, hover))
	dropOver := a.sessionDrag(c, pal, row, s)
	if row.Clicked() && !selected {
		a.selectSession(s.ID)
	}
	if row.DoubleClicked() {
		a.startRename(s.ID)
	}
	tip := proto.ShortDir(s.WorkDir())
	if s.Process != "" {
		tip = s.Process + " · " + tip
	}
	if s.Message != "" {
		tip = s.Message + "\n" + tip
	}
	pointerTooltip(c, row, "tip-"+s.ID, tip)
	row.ContextMenu(func(m *ui.Menu) { a.sessionMenu(m, s) })
	row.Children(func() {
		content := ui.Row(c.Key("session-content")).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center)
		if dropOver {
			content.Opacity(0.15)
		}
		content.Children(func() {
			if a.renaming == s.ID {
				in := ui.TextInputBase(c.Key("rename"), &a.renameBuf).Grow(1).MinWidth(0).FontSize(13.5).AutoFocus()
				if in.Submitted() || in.Shortcut(0, ui.KeyEscape) {
					if in.Shortcut(0, ui.KeyEscape) {
						a.renaming = ""
						a.focusTerminal()
					} else {
						a.commitRename()
					}
				} else if !in.Focused() && !in.Pressed() && in.Valid() && a.renameStarted(c) {
					a.commitRename()
				}
				return
			}
			// The title's mark (title_marks.go) keeps its place at the head;
			// the title gives way to the time and the status glyph.
			ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
				if a.prefs.SessionPins[s.ID] {
					symbolMark(c, "pin.fill", 11, pal.textMuted, pal.dark)
				}
				titleMarkView(c, pal, titleMark(s, a.st.Linear))
				name := ui.Text(c, s.DisplayName(a.set.SessionName)).FontSize(13.5).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).TextColor(pal.text)
				if s.Unread {
					name.FontWeight(650)
				}
				if inactive(s.Status, s.Unread, s.Since, c.Now()) {
					name.TextColor(pal.textMuted)
				}
			})
			cmd := c.Modifiers() == ui.Cmd
			if cmd && index <= 9 {
				ui.Text(c, actions.Glyphs(fmt.Sprintf("CmdOrCtrl+%d", index))).FontSize(12).TextColor(pal.textMuted).FontFeatures("tnum").SingleLine()
			} else {
				ui.Text(c, ago(s.Since, c.Now())).FontSize(12.5).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine()
			}
			statusIconP(c, pal, s.Status, s.Unread, s.Progress, s.Since, 15)
		})
	})
}

// renameStarted keeps a rename from ending in the frame it began in,
// before its input took the focus.
func (a *App) renameStarted(c *ui.Context) bool {
	if a.renameFrames < 3 {
		a.renameFrames++
		c.Invalidate()
		return false
	}
	return true
}

func (a *App) sessionMenu(m *ui.Menu, s *proto.Session) {
	id := s.ID
	a.sessionOrderMenu(m, s)
	m.Separator()
	if a.menuShortcut(m.Item(locales.SessionRename()), "session.rename").Chosen() {
		a.startRename(id)
	}
	if m.Item(a.silenceTitle(id)).Chosen() {
		a.defer_(func() { a.toggleSilence(id) })
	}
	if s.Status == proto.Exited {
		if m.Item(locales.CommonRestart()).Chosen() {
			a.restartSession(id)
		}
	}
	if s.Unread {
		if m.Item(locales.SessionMarkRead()).Chosen() {
			a.call("view", proto.ViewParams{ID: id}, func(error) { a.viewSent = nil; a.reportView() })
		}
	}
	m.Submenu(locales.SessionMoveToProject(), func(m *ui.Menu) {
		for _, p := range a.st.Projects {
			if m.Item(p.Name).Checked(p.ID == s.Project).Disabled(p.ID == s.Project).Chosen() {
				pid := p.ID
				a.defer_(func() { a.moveSession(id, pid) })
			}
		}
	})
	m.Separator()
	if m.Item(locales.SessionCopyId()).Chosen() {
		a.copyText = id
	}
	if m.Item(locales.SessionCopyDirectory()).Disabled(s.WorkDir() == "").Chosen() {
		a.copyText = s.WorkDir()
	}
	if m.Item(locales.SessionRevealIn(fileManager())).Disabled(s.WorkDir() == "").Chosen() {
		a.openLink(id, s.WorkDir())
	}
	m.Separator()
	if a.menuShortcut(m.Item(locales.ActionCloseSession()), "session.close").Chosen() {
		a.defer_(func() { a.closeSession(id, true) })
	}
}

// sidebarFooterNarrow is where the sidebar footer stops showing every
// control and moves the lower-priority ones into its overflow menu.
const sidebarFooterNarrow = 240

// sidebarHidden is what the footer's overflow menu holds, by kind.
type sidebarHidden struct {
	project, search        bool
	waiting, working, done int
}

// sidebarFooter is the sidebar's foot: the controls for projects, search,
// Usage and Settings; then, at the right, the connection and what across
// the sessions wants a look, each a button going to the next session of it.
// The widths are reserved before the status chips are placed; a control that
// does not fit moves, labelled, into the Sidebar Actions menu. This keeps
// Usage, Settings, attention and the connection reachable at the minimum
// sidebar width, in either sidebar.
func (a *App) sidebarFooter(c *ui.Context, pal *palette) {
	width := a.prefs.SidebarWidth
	pad := float32(12)
	if width < sidebarFooterNarrow {
		pad = 6
	}
	const gap, icon = float32(2), float32(28)
	content := width - 2*pad
	waiting, working, done := a.statusCounts()
	badConn := a.conn != connReady || a.daemonStale()
	showProject := width >= sidebarFooterNarrow
	showSearch := showProject

	// The widths include the row's gap before each item. usage, settings and
	// the spacer between them and the status cluster are always there.
	used := 2*icon + 2*gap
	if badConn {
		used += gap + icon
	}
	total := used
	if showProject {
		total += gap + icon
	}
	if showSearch {
		total += gap + icon
	}
	chipW := func(n int) float32 { return gap + sidebarChipWidth(n) }
	if waiting > 0 {
		total += chipW(waiting)
	}
	if working > 0 {
		total += chipW(working)
	}
	if done > 0 {
		total += chipW(done)
	}
	needOverflow := !showProject || total > content
	if needOverflow {
		used += gap + icon
	}
	projectShown, searchShown := showProject, showSearch
	waitingShown, workingShown, doneShown := waiting > 0, working > 0, done > 0
	if needOverflow {
		projectShown, searchShown = false, false
		waitingShown, workingShown, doneShown = false, false, false
		// Attention is required; project, search, working and done are
		// optional and move to the overflow first.
		if waiting > 0 && used+chipW(waiting) <= content {
			waitingShown, used = true, used+chipW(waiting)
		}
		if showProject && used+gap+icon <= content {
			projectShown, used = true, used+gap+icon
		}
		if showSearch && used+gap+icon <= content {
			searchShown, used = true, used+gap+icon
		}
		if working > 0 && used+chipW(working) <= content {
			workingShown, used = true, used+chipW(working)
		}
		if done > 0 && used+chipW(done) <= content {
			doneShown, used = true, used+chipW(done)
		}
	}
	hidden := sidebarHidden{
		project: !projectShown,
		search:  !searchShown,
	}
	if !waitingShown {
		hidden.waiting = waiting
	}
	if !workingShown {
		hidden.working = working
	}
	if !doneShown {
		hidden.done = done
	}

	workingJump := func() {
		a.jumpTo(func(s *proto.Session) bool { return s.Status == proto.Working })
	}
	doneJump := func() {
		a.jumpTo(func(s *proto.Session) bool { return s.Status == proto.Idle && s.Unread })
	}

	ui.Row(c).Height(36).Shrink(0).Padding(0, pad, 4, pad).Gap(gap).AlignItems(ui.Center).DragWindow().Children(func() {
		if projectShown {
			iconButton(c, "open-project", folderIcon, a.tip(locales.ActionOpenProject(), "project.open"), pal, a.openProject)
		}
		if searchShown {
			iconButton(c, "palette", searchIcon, a.tip(locales.ActionGoToSession(), "palette.open"), pal, a.openPalette)
		}
		a.usageNavButton(c, pal)
		iconButton(c, "settings", gearIcon, a.tip(locales.SettingsTitle(), "settings.open"), pal, func() { a.openSettings("") })
		ui.Spacer(c)
		if badConn {
			a.navConnection(c, pal)
		}
		if waitingShown {
			a.footerChip(c, pal, proto.Waiting, false, waiting, pal.attention, locales.FooterNeedsYou,
				a.tip(locales.FooterGoToIt(), "session.attention"), a.jumpToAttention)
		}
		if workingShown {
			a.footerChip(c, pal, proto.Working, false, working, pal.textMuted, locales.FooterWorking,
				locales.FooterGoToIt(), workingJump)
		}
		if doneShown {
			a.footerChip(c, pal, proto.Idle, true, done, pal.textMuted, locales.FooterDone,
				locales.FooterGoToIt(), doneJump)
		}
		if needOverflow {
			a.sidebarOverflow(c, pal, hidden)
		}
	})
}

// statusCounts is how many sessions are waiting, working, or done unseen.
func (a *App) statusCounts() (waiting, working, done int) {
	for i := range a.st.Sessions {
		s := &a.st.Sessions[i]
		switch {
		case s.Status == proto.Waiting:
			waiting++
		case s.Status == proto.Working:
			working++
		case s.Status == proto.Idle && s.Unread:
			done++
		}
	}
	return waiting, working, done
}

// sidebarChipWidth is the room a footer count takes. The count is one or two
// digits in practice; three digits get a wider chip rather than clipping.
func sidebarChipWidth(n int) float32 {
	switch {
	case n >= 100:
		return 38
	case n >= 10:
		return 34
	}
	return 30
}

// footerChip counts n sessions as their rows' status icon and the count, in
// fg. A click goes to the next of them, by jump. one and many word its label
// and tooltip, as "%d session needs you".
func (a *App) footerChip(c *ui.Context, pal *palette, st proto.Status, unread bool, n int, fg ui.Color, say func(int) string, hint string, jump func()) {
	if n == 0 {
		return
	}
	words := say(n)
	b := ui.ButtonBase(c.Key("chip-" + string(st))).Width(sidebarChipWidth(n)).Height(24).Gap(2).Radius(12).Center().
		Label(words).Tooltip(words + " · " + hint)
	if b.Hovered() {
		b.Background(pal.rowHover)
	}
	b.Children(func() {
		statusIconP(c, pal, st, unread, -1, 0, 11)
		ui.Textf(c, "%d", n).FontSize(11.5).FontWeight(500).TextColor(fg).FontFeatures("tnum")
	})
	if b.Clicked() && jump != nil {
		jump()
	}
}

// sidebarOverflow is the Sidebar Actions menu: the controls the footer could
// not show, labelled by what they do. It replaces neither Usage nor Settings;
// those stay in the footer at every width.
func (a *App) sidebarOverflow(c *ui.Context, pal *palette, hidden sidebarHidden) {
	b := navIconButton(c, "sidebar-overflow", "ellipsis", nil, locales.SidebarMore(), locales.SidebarMoreTip(), pal)
	b.Menu(func(m *ui.Menu) {
		actions := hidden.project || hidden.search
		if hidden.project {
			if m.Item(locales.ActionOpenProject()).Chosen() {
				a.defer_(a.openProject)
			}
		}
		if hidden.search {
			if m.Item(locales.ActionGoToSession()).Chosen() {
				a.defer_(a.openPalette)
			}
		}
		if hidden.waiting > 0 || hidden.working > 0 || hidden.done > 0 {
			if actions {
				m.Separator()
			}
			if hidden.waiting > 0 {
				if m.Item(locales.FooterNeedsYou(hidden.waiting)).Chosen() {
					a.defer_(a.jumpToAttention)
				}
			}
			if hidden.working > 0 {
				if m.Item(locales.FooterWorking(hidden.working)).Chosen() {
					a.defer_(func() {
						a.jumpTo(func(s *proto.Session) bool { return s.Status == proto.Working })
					})
				}
			}
			if hidden.done > 0 {
				if m.Item(locales.FooterDone(hidden.done)).Chosen() {
					a.defer_(func() {
						a.jumpTo(func(s *proto.Session) bool { return s.Status == proto.Idle && s.Unread })
					})
				}
			}
		}
	})
}

// ago is how long ago a time in Unix milliseconds was, shortly, as
// proto.Ago writes it, in the language the app shows.
func ago(ms int64, now time.Time) string {
	if ms == 0 {
		return ""
	}
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return locales.AgoNow()
	case d < time.Hour:
		return locales.AgoMinutes(int(d / time.Minute))
	case d < 24*time.Hour:
		return locales.AgoHours(int(d / time.Hour))
	case d < 7*24*time.Hour:
		return locales.AgoDays(int(d / (24 * time.Hour)))
	}
	return locales.AgoWeeks(int(d / (7 * 24 * time.Hour)))
}

// agoTick builds the view again in a minute, for the times ago a view
// shows to move on. They read in whole minutes at the finest, so one
// rebuild a minute keeps each within a minute of its time, and every
// view that asks in the same frame shares it. A view calls it only while
// it shows them: an idle window does not paint for times nobody sees.
func agoTick(c *ui.Context) { c.After(time.Minute) }

// inactiveAfter is how long a session rests, read and done, before the
// sidebar shows it inactive.
const inactiveAfter = 24 * time.Hour

// inactive tells a session at rest: done and read, its last change of
// status, since, inactiveAfter or more before now. One that works, waits
// for the user, failed or exited is never at rest, nor one unread.
func inactive(st proto.Status, unread bool, since int64, now time.Time) bool {
	return st == proto.Idle && !unread && since != 0 && now.Sub(time.UnixMilli(since)) >= inactiveAfter
}

// statusIcon draws a session's status, as the sidebar shows it: a ring
// turning while it works, an amber ring when it needs the user, a filled
// check when it finished unseen, a faint check once seen, a red disc when
// a program failed and a crossed ring when its shell exited with an error.
func statusIcon(c *ui.Context, pal *palette, st proto.Status, unread bool, size float32) ui.Element {
	return statusIconP(c, pal, st, unread, -1, 0, size)
}

// statusIconP is statusIcon with a program's progress (OSC 7501), 0–100,
// which a working ring fills instead of turning, -1 for none; and since,
// when the status last changed, in Unix milliseconds, which draws a half
// moon for a session inactive since, 0 for a mark that never shows it.
func statusIconP(c *ui.Context, pal *palette, st proto.Status, unread bool, progress int, since int64, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0)
	asleep := inactive(st, unread, since, c.Now())
	switch {
	case st == proto.Working:
		box.Label(locales.StatusWorking())
	case st == proto.Waiting:
		box.Label(locales.StatusNeedsYou())
	case st == proto.Exited:
		box.Label(locales.StatusExited())
	case st == proto.Failed:
		box.Label(locales.StatusFailed())
	case asleep:
		box.Label(locales.StatusInactive())
	case st == proto.Idle:
		box.Label(locales.StatusDone())
	}
	// Working, before a program reports how far it has got, turns the
	// loading ring, the faint track and the arc going round it, in steps
	// (turningRing), and stands still where the desktop asks for less
	// motion. Nothing else moves.
	turns := st == proto.Working && progress < 0
	var start time.Time
	if turns {
		start = ringStart(c, box)
	}
	still := c.Preferences().ReduceMotion
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if turns {
			turningRing(p, r, pal.text, start, still)
			return
		}
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		// The loading ring draws its track at 10 of 24 across, and its
		// stroke at 2.5: a session that starts reporting progress keeps
		// the ring the size it was.
		side := min(r.W, r.H)
		rad, w := side*10/24, side*2.5/24
		switch {
		case st == proto.Working:
			var track ui.Path
			track.Circle(cx, cy, rad)
			p.StrokePath(&track, w, pal.text.Alpha(0.2))
			// From the top, clockwise: the circle starts at 3 o'clock.
			p.Transform(ui.Rotation(-90), cx, cy, func() {
				var arc ui.Path
				arc.Circle(cx, cy, rad)
				p.StrokePath(arc.Trim(0, max(0.02, float32(progress)/100)), w, pal.text)
			})
		case st == proto.Waiting:
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, w*1.4, pal.attention)
			var dot ui.Path
			dot.Circle(cx, cy, rad*0.42)
			p.FillPath(&dot, pal.attention)
		case st == proto.Failed:
			var disc ui.Path
			disc.Circle(cx, cy, rad+0.5)
			col := pal.danger
			if !unread {
				col = pal.danger.Alpha(0.75)
			}
			p.FillPath(&disc, col)
			p.Line(cx, cy-rad*0.45, cx, cy+rad*0.08, 1.7, pal.onDone)
			var dot ui.Path
			dot.Circle(cx, cy+rad*0.42, 1)
			p.FillPath(&dot, pal.onDone)
		case st == proto.Exited:
			// The shell ended with an error: a crossed ring, red until
			// the user has seen it, faint after, while the pane keeps its
			// output and offers a restart.
			col := pal.textFaint
			if unread {
				col = pal.danger
			}
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, 1.5, col)
			k := rad * 0.45
			p.Line(cx-k, cy-k, cx+k, cy+k, 1.5, col)
			p.Line(cx+k, cy-k, cx-k, cy+k, 1.5, col)
		case asleep:
			// A moon, filled: it reads over the sidebar's material as
			// it reads over the card.
			moon := moonPath(cx, cy, rad)
			p.FillPath(moon, pal.textFaint)
		case unread:
			var disc ui.Path
			disc.Circle(cx, cy, rad+0.5)
			p.FillPath(&disc, pal.done)
			check := (&ui.Path{}).MoveTo(cx-rad*0.42, cy+0.2).LineTo(cx-rad*0.08, cy+rad*0.36).LineTo(cx+rad*0.46, cy-rad*0.34)
			p.StrokePath(check, 1.6, pal.onDone)
		default:
			check := (&ui.Path{}).MoveTo(cx-rad*0.62, cy+0.4).LineTo(cx-rad*0.12, cy+rad*0.5).LineTo(cx+rad*0.66, cy-rad*0.46)
			p.StrokePath(check, 1.5, pal.textFaint)
		}
	})
	return box
}

// moonPath is a half moon at (cx, cy) of radius r: the rim of the circle,
// and the bite a smaller one takes from it, closed as one outline, so it
// fills in a solid shape rather than cutting one out of the sidebar.
func moonPath(cx, cy, r float32) *ui.Path {
	const bite, reach = 0.86, 0.52 // of the radius, as the design's
	d, br := r*reach, r*bite
	// Where the bite's circle crosses the rim's.
	x := (d*d + r*r - br*br) / (2 * d)
	y := float32(math.Sqrt(float64(max(0, r*r-x*x))))
	deg := func(dy, dx float32) float32 {
		return float32(math.Atan2(float64(dy), float64(dx)) * 180 / math.Pi)
	}
	up, low := deg(-y, x), deg(y, x)
	upBite, lowBite := deg(-y, x-d), deg(y, x-d)
	p := &ui.Path{}
	p.MoveTo(cx+x, cy-y)
	// The rim, round the outside from the upper tip to the lower one, and
	// the bite back, its arc running the other way round.
	arcPath(p, cx, cy, r, up, low-360)
	arcPath(p, cx+d, cy, br, lowBite, upBite+360)
	return p.Close()
}

// arcPath adds to p the arc of the circle at (cx, cy) of radius r, from
// the angle from to to, in degrees clockwise on the screen, in cubic
// curves of a quarter turn at most, from where the pen is.
func arcPath(p *ui.Path, cx, cy, r, from, to float32) {
	steps := int(math.Ceil(math.Abs(float64(to-from)) / 90))
	step := (to - from) / float32(steps)
	// The tangent's length that makes a cubic follow its arc.
	k := r * 4 / 3 * float32(math.Tan(float64(step)*math.Pi/720))
	for range steps {
		sin, cos := math.Sincos(float64(from) * math.Pi / 180)
		sin2, cos2 := math.Sincos(float64(from+step) * math.Pi / 180)
		x, y := cx+r*float32(cos), cy+r*float32(sin)
		nx, ny := cx+r*float32(cos2), cy+r*float32(sin2)
		p.CubeTo(x-k*float32(sin), y+k*float32(cos), nx+k*float32(sin2), ny-k*float32(cos2), nx, ny)
		from += step
	}
}

// --- the card ---

func (a *App) card(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	s := a.session(a.selected)
	if s != nil && a.conn != connVersion && a.loaded {
		a.panes(c, pal, bar, shown, s)
		return
	}
	// No session: one card, for what the app has to say.
	card := raised(ui.Column(c).Grow(1).MinHeight(0).Radius(14).Background(pal.card), pal).Clip()
	card.Children(func() {
		left := a.chrome.headerPad
		ui.Row(c).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 10, 0, left).DragWindow().Children(func() {
			if shown < 0.5 {
				a.headerButtons(c, bar, shown).Children(func() {
					a.navRevealButton(c, pal)
					iconButton(c, "new-session-2", plusIcon, a.tip(locales.ActionNewSession(), "session.new"), pal, func() { a.newSession("") })
				})
			}
			ui.Text(c, "Kero").FontSize(14).FontWeight(600).TextColor(pal.text)
			ui.Spacer(c)
			windowControls(c, pal, a.win)
		})
		ui.Column(c).Grow(1).MinHeight(0).Children(func() {
			switch {
			case a.conn == connVersion:
				a.message(c, pal, locales.UpdatedTitle(), locales.UpdatedDetail(), locales.CommonRestartService(), a.restartDaemon)
			case a.conn == connFailed && a.cl == nil:
				a.message(c, pal, locales.ConnCantReach(), a.connErr, locales.CommonTryAgain(), func() { a.retries = 0; a.conn = connConnecting; a.connect() })
			case !a.loaded:
				working(c, pal, locales.StatusConnectingLabel())
			default:
				a.emptyState(c, pal)
			}
		})
	})
}

func (a *App) message(c *ui.Context, pal *palette, title, detail, action string, fn func()) {
	ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(17).FontWeight(600).TextColor(pal.text)
		ui.Text(c, detail).FontSize(13).TextColor(pal.textMuted).MaxWidth(420).TextAlign(ui.Center).LineHeight(1.4)
		if action != "" {
			ui.Box(c).Height(8)
			if button(c, "msg-action", action, "", true, pal) {
				fn()
			}
		}
	})
}

func (a *App) emptyState(c *ui.Context, pal *palette) {
	ui.Column(c).Fill().Center().Gap(0).Padding(24).Children(func() {
		ui.Box(c).Size(52, 52).Radius(14).Center().Background(pal.chip).TextColor(pal.textMuted).Margin(0, 0, 18, 0).Children(func() {
			controlIcon(c, terminalIcon, 26, pal.textMuted, pal.dark)
		})
		ui.Text(c, locales.EmptyTitle()).FontSize(19).FontWeight(650).TextColor(pal.text)
		ui.Text(c, locales.EmptyDetail()).
			FontSize(13.5).TextColor(pal.textMuted).MaxWidth(380).TextAlign(ui.Center).LineHeight(1.45).Margin(8, 0, 22, 0)
		ui.Row(c).Gap(10).Children(func() {
			if button(c, "empty-new", locales.ActionNewSession(), a.keys("session.new"), true, pal) {
				a.newSession("")
			}
			if button(c, "empty-open", locales.ActionOpenProject(), a.keys("project.open"), false, pal) {
				a.openProject()
			}
		})
		ui.Column(c).Margin(40, 0, 0, 0).Padding(14, 16).Gap(6).Radius(10).Background(pal.chip).MaxWidth(420).Children(func() {
			ui.Text(c, locales.EmptyForAgents()).FontSize(11.5).FontWeight(600).TextColor(pal.textMuted)
			ui.Text(c, "kero new -n review -- claude\nkero prompt review \"run the tests and fix them\"\nkero wait review && kero read review").
				Font("monospace").FontSize(12).TextColor(pal.text).LineHeight(1.5).Selectable()
		})
	})
}
