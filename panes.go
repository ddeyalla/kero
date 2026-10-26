package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

const paneGap = 8 // between panes, where the divider is

// The smallest a pane gets: a terminal of some 36 columns and a few rows
// under its header. A divider stops there, a split that would make one
// smaller is refused, and a window too small takes room from the others
// first.
const (
	minPaneW = 280
	minPaneH = 150
)

// layoutMotion is how the panes glide to the room they share, as when it
// is made even: not as a divider is dragged, which they follow.
var layoutMotion = slide

// minExtent is the least room a layout needs across (row) or down.
func minExtent(l *proto.Layout, row bool) float32 {
	if l == nil {
		return 0
	}
	if l.Leaf() {
		if row {
			return minPaneW
		}
		return minPaneH
	}
	a, b := minExtent(l.A, row), minExtent(l.B, row)
	if l.Row == row {
		return a + b + paneGap
	}
	return max(a, b)
}

// paneRef returns the handle a pane's terminal binds, which its focus
// follows.
func (a *App) paneRef(id string) *ui.Handle {
	if a.refs == nil {
		a.refs = map[string]*ui.Handle{}
	}
	h := a.refs[id]
	if h == nil {
		h = &ui.Handle{}
		a.refs[id] = h
	}
	return h
}

// focusOf returns the pane of a session the user is in.
func (a *App) focusOf(s *proto.Session) string {
	if id := a.focus[s.ID]; id != "" && s.PaneByID(id) != nil {
		return id
	}
	if p := s.FocusPane(); p != nil {
		return p.ID
	}
	return ""
}

// setFocus moves the user into a pane, and tells kerod, which remembers it.
// A pane already exiting is not a focus target.
func (a *App) setFocus(s *proto.Session, pane string) {
	if a.closing[pane] {
		return
	}
	if a.focus == nil {
		a.focus = map[string]string{}
	}
	if a.focus[s.ID] == pane && s.Focus == pane {
		return
	}
	a.focus[s.ID] = pane
	s.Focus = pane
	if s.Zoomed != "" {
		s.Zoomed = pane
	}
	a.ensureTerm(pane)
	a.touch(pane)
	a.call("focus", proto.FocusParams{Session: s.ID, Pane: pane}, nil)
}

// focusTerminal gives the keyboard to the pane the user is in.
func (a *App) focusTerminal() {
	if a.introShowing() {
		// The first run's reveal holds the keyboard; it gives it back.
		return
	}
	if s := a.session(a.selected); s != nil {
		if id := a.focusOf(s); id != "" {
			a.paneRef(id).Focus()
		}
	}
}

// panes shows a session's panes in their layout: the zoomed one alone.
func (a *App) panes(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32, s *proto.Session) {
	multi := len(s.Panes) > 1
	if z := s.PaneByID(s.Zoomed); z != nil || s.Layout == nil || !multi {
		p := z
		if p == nil {
			p = s.FocusPane()
		}
		if p != nil {
			a.paneCard(c, pal, bar, shown, s, p, true, true, true, multi)
		}
		return
	}
	a.layoutNode(c, pal, bar, shown, s, s.Layout, "", true, true, true)
}

func (a *App) layoutNode(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32, s *proto.Session, l *proto.Layout, path string, top, left, right bool) {
	if l.Leaf() {
		if p := s.PaneByID(l.Pane); p != nil {
			a.paneCard(c, pal, bar, shown, s, p, top, left, right, true)
		}
		return
	}
	ratio := float32(l.Ratio)
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	key := "split-" + s.ID + path
	var box ui.Element
	if l.Row {
		box = ui.Row(c.Key(key))
	} else {
		box = ui.Column(c.Key(key))
	}
	box.Grow(1).Basis(0).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Bind(a.paneRef(key))
	// A pane opening grows from its edge, and one closing gives its room
	// back, before it goes.
	leaving := false
	switch {
	case a.closing[l.A.Pane] && l.A.Leaf():
		ratio, leaving = 0, true
	case a.closing[l.B.Pane] && l.B.Leaf():
		ratio, leaving = 1, true
	case a.growing[l.A.Pane] && l.A.Leaf():
		box.Jump("ratio", 0)
		delete(a.growing, l.A.Pane)
	case a.growing[l.B.Pane] && l.B.Leaf():
		box.Jump("ratio", 1)
		delete(a.growing, l.B.Pane)
	}
	// A drag moves the panes with the pointer; anything else, as making
	// them even or the layout kerod sends, glides.
	target := ratio
	if a.layoutDirty == s.ID {
		ratio = box.Jump("ratio", ratio)
	} else {
		ratio = box.AnimateTo("ratio", ratio, layoutMotion)
	}
	// While they glide, and as one goes, the panes may be smaller than
	// they can stay.
	gliding := leaving || math.Abs(float64(ratio-target)) > 0.001
	if gliding {
		a.sizeHold.Store(time.Now().Add(80 * time.Millisecond).UnixNano())
	}
	side := func(e ui.Element, n *proto.Layout) ui.Element {
		switch {
		case gliding:
			return e.MinWidth(0).MinHeight(0)
		case l.Row:
			return e.MinWidth(minExtent(n, true)).MinHeight(0)
		}
		return e.MinWidth(0).MinHeight(minExtent(n, false))
	}
	box.Children(func() {
		side(ui.Column(c.Key("a")).Grow(ratio).Basis(0), l.A).AlignItems(ui.Stretch).Children(func() {
			a.layoutNode(c, pal, bar, shown, s, l.A, path+"a", top, left, right && !l.Row)
		})
		a.divider(c, pal, s, l, key)
		side(ui.Column(c.Key("b")).Grow(1-ratio).Basis(0), l.B).AlignItems(ui.Stretch).Children(func() {
			a.layoutNode(c, pal, bar, shown, s, l.B, path+"b", top && l.Row, left && !l.Row, right)
		})
	})
}

// divider is the gap between two panes, which a drag moves.
func (a *App) divider(c *ui.Context, pal *palette, s *proto.Session, l *proto.Layout, key string) {
	d := ui.Box(c.Key("div")).Shrink(0)
	if l.Row {
		d.Width(paneGap).Cursor(ui.CursorResizeColumn)
	} else {
		d.Height(paneGap).Cursor(ui.CursorResizeRow)
	}
	if d.Hovered() || d.Pressed() {
		// A short grip, so the gap shows it moves.
		d.Draw(func(p *ui.Painter, r ui.Rect) {
			if l.Row {
				p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, pal.textFaint.Alpha(0.7), 1.5)
			} else {
				p.Fill(ui.Rect{X: r.X + r.W/2 - 18, Y: r.Y + r.H/2 - 1.5, W: 36, H: 3}, pal.textFaint.Alpha(0.7), 1.5)
			}
		})
	}
	if dx, dy, ok := d.Dragged(); ok {
		b := a.paneRef(key).Bounds(c)
		size, delta := b.W, dx
		if !l.Row {
			size, delta = b.H, dy
		}
		if size > 0 {
			r := l.Ratio
			if r <= 0 || r >= 1 {
				r = 0.5
			}
			// Neither side smaller than its panes can be.
			lo := float64((minExtent(l.A, l.Row) + paneGap/2) / size)
			hi := 1 - float64((minExtent(l.B, l.Row)+paneGap/2)/size)
			if lo >= hi {
				lo, hi = 0.5, 0.5
			}
			l.Ratio = math.Min(math.Max(r+float64(delta/size), lo), hi)
			a.layoutDirty = s.ID
		}
	} else if a.layoutDirty == s.ID && !d.Pressed() {
		a.layoutDirty = ""
		a.call("layout", proto.LayoutParams{Session: s.ID, Layout: s.Layout.Clone()}, nil)
	}
	// A double click makes every pane of the session as big as the others.
	if d.DoubleClicked() {
		a.evenPanes(s)
	}
}

// paneCard is a pane: a card with a header and the terminal. top, left and
// right tell that it touches the window's edges, where the header drags
// the window, keeps clear of the traffic lights on macOS, and holds the
// window controls elsewhere (windowControls).
func (a *App) paneCard(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32, s *proto.Session, p *proto.Pane, top, left, right, multi bool) {
	focused := p.ID == a.focusOf(s)
	bg := pal.card
	card := ui.Column(c.Key("pane-" + p.ID)).Grow(1).Basis(0).MinWidth(0).MinHeight(0).Radius(14).Clip().Bind(a.paneRef("card-" + p.ID))
	// A closing pane is only a picture of its exit: focus already moved to
	// its neighbour, and neither it nor a child may take input back.
	card.Disabled(a.closing[p.ID])
	if a.cardSize == nil {
		a.cardSize = map[string]ui.Rect{}
	}
	a.cardSize[p.ID] = a.paneRef("card-" + p.ID).Bounds(c)
	raised(card.Background(card.AnimateColor("bg", bg, hover)), pal)
	hovered := card.Hovered()
	// The card takes presses: one on its padding, beside the terminal,
	// focuses the pane, rather than going on to what drags the window.
	if card.Pressed() && !focused {
		a.setFocus(s, p.ID)
		a.paneRef(p.ID).Focus()
	}

	// Another pane dragged over this one: where it would go.
	zone := zoneNone
	if multi {
		if d, over := ui.DragOver[paneDrag](card); over && d.session == s.ID && d.pane != p.ID {
			if x, y, in := card.PointerPosition(); in {
				b := a.paneRef("card-" + p.ID).Bounds(c)
				zone = zoneAt(x/max(b.W, 1), y/max(b.H, 1))
			}
		}
		if d, ok := ui.Drop[paneDrag](card); ok && d.session == s.ID && d.pane != p.ID {
			sid, target := s.ID, p.ID
			drop := a.lastZone
			a.defer_(func() { a.movePane(sid, d.pane, target, drop) })
		}
	}
	if zone != zoneNone {
		a.lastZone = zone
	}

	dragging := false
	card.Children(func() {
		dragging = a.paneHeader(c, pal, bar, shown, s, p, top, left, right, multi, focused, hovered)
		if p.IsView() {
			a.viewPane(c, pal, s, p)
		} else {
			a.paneTerminal(c, pal, s, p)
		}
		// The focus is the only thing that changes when the user moves
		// between panes: the card and its text stay as they are, and a
		// light edge marks the pane in, when there is another pane to
		// tell it from: not a pane alone, nor one zoomed. Every pane keeps
		// the edge's room, so the border never moves what is inside it.
		// It lies inside the card's own border, the dark's lit edge, and
		// follows its inner curve.
		edge := ui.Box(c.Key("focus-" + p.ID)).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14 - b2f(pal.dark)).PassThrough()
		outline := ui.Transparent
		if focused && multi && s.Zoomed == "" {
			outline = pal.focusEdge
		}
		edge.Border(1, edge.AnimateColor("edge", outline, focus))
		if zone != zoneNone {
			a.dropPreview(c, pal, p.ID, zone)
		}
	})

	// The pane dragged sinks back where it was; one just moved springs in
	// at its new place. A pane opening starts a hair small and fades in,
	// and one closing fades out the same way: the first frame shows it
	// small, and the next animates it to its place.
	pose := ui.Pose{}
	poseMotion := springDrag
	switch {
	case dragging:
		pose = ui.Pose{Scale: 0.985, Fade: 0.45}
	case a.opening[p.ID] == 1:
		pose = ui.Pose{Scale: 0.985, Fade: 1}
		a.opening[p.ID] = 2
		poseMotion = focus
		c.Invalidate()
	case a.opening[p.ID] == 2:
		delete(a.opening, p.ID)
		poseMotion = focus
	case a.closing[p.ID]:
		pose = ui.Pose{Scale: 0.985, Fade: 1}
		poseMotion = focus
	case a.arrived[p.ID]:
		pose = ui.Pose{Scale: 0.94, Fade: 0.7}
		delete(a.arrived, p.ID)
		c.Invalidate()
	}
	card.AnimatePose(pose, poseMotion)
}

// paneDrag is a pane dragged by its title to another place.
type paneDrag struct{ session, pane string }

// dropZone is where a pane dropped on another goes: beside it, on a side,
// or in its place, which the other takes (swap).
type dropZone int

const (
	zoneNone dropZone = iota
	zoneLeft
	zoneRight
	zoneUp
	zoneDown
	zoneSwap
)

// zoneAt returns the zone under the pointer, at u, v of a pane (0 to 1):
// its middle swaps, else the nearest edge.
func zoneAt(u, v float32) dropZone {
	if u > 0.3 && u < 0.7 && v > 0.3 && v < 0.7 {
		return zoneSwap
	}
	best, zone := u, zoneLeft
	for _, e := range []struct {
		d float32
		z dropZone
	}{{1 - u, zoneRight}, {v, zoneUp}, {1 - v, zoneDown}} {
		if e.d < best {
			best, zone = e.d, e.z
		}
	}
	return zone
}

func (z dropZone) side() string {
	return [...]string{"", "left", "right", "up", "down", ""}[z]
}

// dropPreview shows, over the pane under the pointer, the room the
// dragged pane would take, gliding from zone to zone, with what dropping
// does.
func (a *App) dropPreview(c *ui.Context, pal *palette, target string, zone dropZone) {
	key := target
	a.previewShown = true
	if a.previewKey != key {
		a.previewKey, a.previewSince = key, c.Now()
	}
	// left, top, width, height in percent of the pane
	r := [...][4]float32{
		zoneLeft:  {0, 0, 50, 100},
		zoneRight: {50, 0, 50, 100},
		zoneUp:    {0, 0, 100, 50},
		zoneDown:  {0, 50, 100, 50},
		zoneSwap:  {0, 0, 100, 100},
	}[zone]
	label, glyph := map[dropZone]string{
		zoneLeft: locales.PaneSplitLeft(), zoneRight: locales.ActionSplitRight(), zoneUp: locales.PaneSplitUp(), zoneDown: locales.ActionSplitDown(), zoneSwap: locales.PaneSwap(),
	}[zone], splitRightIcon
	if zone == zoneUp || zone == zoneDown {
		glyph = splitDownIcon
	}
	if zone == zoneSwap {
		glyph = swapIcon
	}
	fadeIn := min(float32(c.Now().Sub(a.previewSince))/float32(140*time.Millisecond), 1)
	if fadeIn < 1 {
		c.AnimationFrame()
	}
	spring := springUI
	o := ui.Column(c.Key("drop")).Absolute().PassThrough().Padding(8).Opacity(fadeIn)
	o.LeftPercent(o.AnimateTo("l", r[0], spring)).TopPercent(o.AnimateTo("t", r[1], spring)).
		WidthPercent(o.AnimateTo("w", r[2], spring)).HeightPercent(o.AnimateTo("h", r[3], spring))
	o.Children(func() {
		room := ui.Column(c).Grow(1).Radius(10).AlignItems(ui.Center).
			Background(pal.accent.Alpha(0.10)).InsetShadow(0, 0, 28, 0, pal.accent.Alpha(0.22))
		room.Children(func() {
			// The label sits high in the room, clear of the title the
			// pointer carries through its middle.
			ui.Box(c).HeightPercent(22).Shrink(0)
			pill := ui.Row(c).Height(30).Padding(0, 12).Gap(7).Radius(15).AlignItems(ui.Center).
				Background(pal.accent).TextColor(ui.Hex("#ffffff"))
			floating(pill, pal).Children(func() {
				controlIcon(c, glyph, 14, ui.Hex("#ffffff"), pal.dark)
				ui.Text(c, label).FontSize(12.5).FontWeight(600).SingleLine()
			})
			pill.AnimatePose(ui.Pose{Scale: 0.9 + 0.1*fadeIn}, springUI)
		})
	})
}

// movePane puts a pane dropped on another beside it, or in its place.
func (a *App) movePane(sid, pane, target string, zone dropZone) {
	s := a.session(sid)
	if s == nil || s.Layout == nil || zone == zoneNone {
		return
	}
	if zone == zoneSwap {
		s.Layout = s.Layout.Swap(pane, target)
		a.arrived[target] = true
	} else {
		s.Layout = s.Layout.Move(pane, target, zone.side())
	}
	a.arrived[pane] = true
	s.Zoomed = ""
	a.setFocus(s, pane)
	a.paneRef(pane).Focus()
	a.call("layout", proto.LayoutParams{Session: sid, Layout: s.Layout.Clone()}, nil)
}

// paneHeader builds a pane's header, and reports whether its title is
// being dragged.
func (a *App) paneHeader(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32, s *proto.Session, p *proto.Pane, top, left, right, multi, focused, hovered bool) (dragging bool) {
	pad := float32(16)
	if left {
		// With the sidebar hidden, the header starts after the traffic
		// lights and the sidebar's buttons, which come here.
		pad = a.chrome.headerPad
	}
	h := ui.Row(c.Key("hdr")).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 10, 0, pad).Gap(8)
	if h.Pressed() && !focused {
		a.setFocus(s, p.ID)
		a.paneRef(p.ID).Focus()
	}
	h.ContextMenu(func(m *ui.Menu) { a.paneMenu(m, s, p) })
	h.Children(func() {
		if top && left && shown < 0.5 {
			a.headerButtons(c, bar, shown).Children(func() {
				a.navRevealButton(c, pal)
				iconButton(c, "new-session-2", plusIcon, a.tip(locales.ActionNewSession(), "session.new"), pal, func() { a.newSession("") })
			})
		}
		// The title is the pane's handle: dragged, it takes the pane to
		// another place in the session.
		grip := ui.Row(c.Key("grip")).Gap(8).AlignItems(ui.Center).Shrink(1).MinWidth(0).Height(28).Padding(0, 8).Margin(0, 0, 0, -8).Radius(8)
		if multi {
			grip.Drag(paneDrag{s.ID, p.ID}).Tooltip(locales.PaneDragTip())
			dragging = grip.Dragging()
			switch {
			case dragging:
				// What follows the pointer is a copy of the grip: a chip.
				glassChip(c, grip, pal)
			case grip.Hovered():
				grip.Background(pal.rowHover).Cursor(ui.CursorGrab)
			}
		}
		grip.Children(func() {
			switch {
			case p.IsView():
				paneViewMark(c, p.View, 14, pal.textMuted, pal.dark)
			case multi && (p.Status != proto.Idle || p.Unread):
				statusIconP(c, pal, p.Status, p.Unread, p.Progress, 0, 13)
			case multi:
				controlIcon(c, programIcon(p), 14, pal.textFaint, pal.dark)
			}
			title, size := p.PaneName(), float32(13.5)
			if !multi {
				title, size = s.DisplayName(a.set.SessionName), 14
			}
			if p.IsView() {
				title, size = paneViewName(p.View), 13.5
			}
			name := ui.Text(c, title).FontSize(size).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(30)
			if p.IsView() {
				// A view's name is short, and says what the pane is: what
				// it shows beside gives way first.
				name.Shrink(0)
			}
			if multi && !focused && !dragging {
				name.TextColor(pal.textMuted)
			}
			if !multi && name.DoubleClicked() {
				a.startRename(s.ID)
			}
			// The home directory names itself "~", which the title may
			// already say. A view says what it shows.
			if p.IsView() {
				if sub := a.viewSubtitle(s, p); sub != "" {
					ui.Text(c, sub).FontSize(13).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Shrink(2).MinWidth(0)
				}
			} else if dir := proto.ShortDir(p.WorkDir()); dir != title {
				ui.Text(c, dir).FontSize(13).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Shrink(2).MinWidth(0)
			}
		})
		// The header's empty stretch moves the window, as a title bar
		// does, and zooms it on a double click: the header around it
		// takes clicks, which focus the pane.
		gap := ui.Box(c.Key("drag")).Grow(1).AlignSelf(ui.Stretch)
		if top {
			gap.DragWindow()
		}
		if !p.IsView() {
			a.paneChips(c, pal, p, !multi)
		}
		show := (hovered || focused && multi) && !dragging
		ctl := ui.Row(c.Key("controls")).Gap(0).AlignItems(ui.Center)
		ctl.Opacity(ctl.Animate("shown", b2f(show), hover.Duration)).Children(func() {
			if !show {
				return
			}
			iconButton(c, "new-pane", plusIcon, a.tip(locales.PaneNewButton(), "pane.new"), pal, func() { a.openChooser(p.ID) })
			iconButton(c, "split-r", splitRightIcon, a.tip(locales.ActionSplitRight(), "pane.split.right"), pal, func() { a.splitPane(s, p.ID, "right") })
			iconButton(c, "split-d", splitDownIcon, a.tip(locales.ActionSplitDown(), "pane.split.down"), pal, func() { a.splitPane(s, p.ID, "down") })
			if multi {
				sid, pid := s.ID, p.ID
				zoom := func() { a.defer_(func() { a.zoomPane(a.session(sid), pid) }) }
				if s.Zoomed == p.ID {
					iconButton(c, "zoom", unzoomIcon, a.tip(locales.PaneShowAll(), "pane.zoom"), pal, zoom)
				} else {
					iconButton(c, "zoom", zoomIcon, a.tip(locales.ActionZoomPane(), "pane.zoom"), pal, zoom)
				}
				iconButton(c, "close", closeIcon, a.tip(locales.PaneClose(), "pane.close"), pal, func() { a.defer_(func() { a.closePane(a.session(sid), pid, true) }) })
			}
		})
		if top && right {
			windowControls(c, pal, a.win)
		}
	})
	return dragging
}

// paneMenu is a pane header's menu: another pane beside it, or another
// view in it.
func (a *App) paneMenu(m *ui.Menu, s *proto.Session, p *proto.Pane) {
	sid, pid := s.ID, p.ID
	later := func(fn func(s *proto.Session)) {
		a.defer_(func() {
			if s := a.session(sid); s != nil {
				fn(s)
			}
		})
	}
	if a.menuShortcut(m.Item(locales.ActionNewPane()), "pane.new").Chosen() {
		a.openChooser(pid)
	}
	if a.menuShortcut(m.Item(locales.ActionSplitRight()), "pane.split.right").Chosen() {
		later(func(s *proto.Session) { a.splitPane(s, pid, "right") })
	}
	if a.menuShortcut(m.Item(locales.ActionSplitDown()), "pane.split.down").Chosen() {
		later(func(s *proto.Session) { a.splitPane(s, pid, "down") })
	}
	m.Submenu(locales.PaneShow(), func(m *ui.Menu) {
		for _, k := range paneKinds {
			view := k.view
			if m.Item(paneViewName(view)).Checked(p.View == view).Disabled(p.View == view).Chosen() {
				later(func(s *proto.Session) { a.showInPane(s, pid, view, true) })
			}
		}
	})
	a.pluginPinMenu(m, p)
	if len(s.Panes) > 1 {
		m.Separator()
		if a.menuShortcut(m.Item(locales.PaneClose()), "pane.close").Chosen() {
			later(func(s *proto.Session) { a.closePane(s, pid, true) })
		}
	}
}

// paneChips says what the pane is doing. With several panes the title
// already leads with the status icon, so the chips go without (icon false).
func (a *App) paneChips(c *ui.Context, pal *palette, p *proto.Pane, icon bool) {
	switch p.Status {
	case proto.Waiting:
		msg := p.Message
		if msg == "" {
			msg = map[string]string{"permission": locales.PaneNeedsPermission(), "question": locales.PaneHasQuestion(), "auth": locales.PaneNeedsSignIn()}[p.Kind]
		}
		if msg == "" {
			msg = locales.StatusNeedsYou()
		}
		chip(c, pal, pal.attnSoft, pal.attention, func() {
			if icon {
				statusIcon(c, pal, p.Status, p.Unread, 11)
			}
			ui.Text(c, msg).FontSize(12).FontWeight(500).SingleLine().Ellipsis("…").MaxWidth(320)
		})
	case proto.Working:
		chip(c, pal, pal.chip, pal.textMuted, func() {
			if icon {
				statusIconP(c, pal, p.Status, false, p.Progress, 0, 11)
			}
			label := ago(p.Since, c.Now())
			agoTick(c) // with the sidebar hidden too
			if p.Progress >= 0 {
				label = itoa(p.Progress) + "%"
			}
			if p.Message != "" {
				label = p.Message + " · " + label
			}
			ui.Text(c, label).FontSize(12).FontFeatures("tnum").SingleLine().Ellipsis("…").MaxWidth(320)
		})
	case proto.Failed:
		chip(c, pal, pal.danger.Alpha(0.1), pal.danger, func() {
			if icon {
				statusIcon(c, pal, p.Status, true, 11)
			}
			msg := p.Message
			if msg == "" {
				msg = locales.StatusFailed()
			}
			ui.Text(c, msg).FontSize(12).FontWeight(500).SingleLine().Ellipsis("…").MaxWidth(320)
		})
	case proto.Exited:
		chip(c, pal, pal.danger.Alpha(0.1), pal.danger, func() {
			ui.Text(c, locales.PaneExited(itoa(p.ExitCode))).FontSize(12).FontWeight(500).SingleLine()
		})
		if button(c, "restart-"+p.ID, locales.CommonRestart(), "", false, pal) {
			a.restartSession(p.ID)
		}
	case proto.Idle:
		if p.Message != "" {
			chip(c, pal, pal.chip, pal.textMuted, func() {
				if icon {
					statusIcon(c, pal, p.Status, p.Unread, 11)
				}
				ui.Text(c, p.Message).FontSize(12).SingleLine().Ellipsis("…").MaxWidth(320)
			})
		}
	}
	if p.Process != "" && p.Status != proto.Exited {
		chip(c, pal, pal.chip, pal.textMuted, func() {
			ui.Text(c, p.Process).FontSize(12).SingleLine()
		}).Tooltip(locales.PaneForegroundTip())
	}
}

func (a *App) paneTerminal(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane) {
	e := a.terms[p.ID]
	if e == nil {
		a.ensureTerm(p.ID)
		e = a.terms[p.ID]
	}
	// The terminal fills the card under the header: its own padding
	// takes the colors of the cells beside it, so that a full-screen
	// program's background reaches the card's edges.
	ui.Box(c.Key("body")).Grow(1).MinHeight(0).Children(func() {
		switch {
		case e != nil && e.term != nil:
			e.shown = true
			ref := a.paneRef(p.ID)
			termView := terminal.View(c.Key("term"), e.term).Fill().Bind(ref)
			// Our drop goes first: it takes images too, which the
			// terminal's own file drop does not.
			if drop, ok := ui.DropData(termView, transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.PNG}, Operations: transfer.Copy}); ok {
				a.pasteAttachment(p.ID, drop.Data)
				ref.Focus(c)
			}
			if link, isPath, at := e.term.HoveredLink(); isPath {
				a.imagePeek(p.ID, link, at, ref.Bounds(c))
			}
			if ref.Focused(c) && a.focusOf(s) != p.ID {
				a.setFocus(s, p.ID)
			}
			if a.find.pane == p.ID {
				a.findBar(c, pal, p, e.term)
			}
		case e != nil && e.err != "":
			a.message(c, pal, locales.PaneCantShow(), e.err, locales.CommonTryAgain(), func() {
				a.dropTerm(p.ID)
				a.ensureTerm(p.ID)
			})
		default:
			// Attaching takes a moment for a long scrollback: the loading
			// ring only once it is a noticeable one.
			if e != nil && time.Since(e.started) > 250*time.Millisecond {
				working(c, pal, locales.PaneAttaching())
			} else {
				c.After(260 * time.Millisecond)
			}
		}
	})
}

// --- pane actions ---

func (a *App) splitPane(s *proto.Session, at, direction string) {
	a.splitView(s, at, direction, "")
}

// splitView splits the pane at, the focused one when "", with a pane
// showing view: a terminal when "".
func (a *App) splitView(s *proto.Session, at, direction, view string) {
	a.splitTarget(s, at, direction, view, "")
}

// splitTarget splits the pane at with a pane showing view at target. The
// target is the view's own: a plugin pane's plugin, widget and state.
func (a *App) splitTarget(s *proto.Session, at, direction, view, target string) {
	if s == nil {
		return
	}
	if at == "" {
		at = a.focusOf(s)
	}
	// Two panes where one is must each be a pane still.
	if b, ok := a.cardSize[at]; ok && b.W > 0 && s.Zoomed == "" {
		across := direction == "right" || direction == "left"
		if across && b.W < 2*minPaneW+paneGap || !across && b.H < 2*minPaneH+paneGap {
			a.toast(locales.PaneNoRoom())
			return
		}
	}
	if a.cl == nil {
		return
	}
	p := proto.CreateParams{Split: at, Direction: direction, View: view, Target: target}
	if pane := s.PaneByID(at); pane != nil {
		p.Cols, p.Rows = pane.Cols, pane.Rows
		if direction == "down" || direction == "up" {
			p.Rows = max(p.Rows/2, 4)
		} else {
			p.Cols = max(p.Cols/2, 10)
		}
	}
	cl := a.cl
	go func() {
		var ns proto.Session
		err := cl.Call("split", p, &ns)
		a.update(func() {
			if err != nil {
				a.toast(viewCallError(err))
				return
			}
			a.focusNew(ns)
		})
	}()
}

// focusNew puts a session kerod answered with a new pane into the state,
// and focuses the pane.
func (a *App) focusNew(ns proto.Session) {
	// The pane new to the layout fades in; one beside the others grows
	// from its edge, where one in another's place does not.
	if old := a.session(ns.ID); old != nil && old.PaneByID(ns.Focus) == nil {
		a.opening[ns.Focus] = 1
		if len(ns.Panes) > len(old.Panes) {
			if a.growing == nil {
				a.growing = map[string]bool{}
			}
			a.growing[ns.Focus] = true
		}
	}
	a.replaceSession(ns)
	if cur := a.session(ns.ID); cur != nil {
		if a.focus == nil {
			a.focus = map[string]string{}
		}
		a.focus[ns.ID] = ns.Focus
		a.ensureTerm(ns.Focus)
		a.touch(ns.Focus)
		a.paneRef(ns.Focus).Focus()
	}
}

// replaceSession puts a session as kerod answered into the state, before
// the state that has it comes.
func (a *App) replaceSession(ns proto.Session) {
	for i := range a.st.Sessions {
		if a.st.Sessions[i].ID == ns.ID {
			a.st.Sessions[i] = ns
			return
		}
	}
	a.addPending(ns)
}

func (a *App) closePane(s *proto.Session, pane string, ask bool) {
	if s == nil {
		return
	}
	if len(s.Panes) <= 1 {
		a.closeSession(s.ID, ask)
		return
	}
	p := s.PaneByID(pane)
	if p == nil {
		return
	}
	if ask && a.set.ConfirmClose && p.Status != proto.Exited && p.Process != "" {
		sid := s.ID
		a.confirm = &confirmation{
			open:    true,
			title:   locales.ConfirmClosePaneTitle(),
			detail:  locales.ConfirmClosePaneDetail(p.Process),
			action:  locales.PaneClose(),
			destroy: true,
			run: func() {
				if s := a.session(sid); s != nil {
					a.closePane(s, pane, false)
				}
			},
		}
		return
	}
	// A pane beside others gives its room back first, as they glide into
	// it.
	if a.win != nil && s.Layout != nil && s.Zoomed == "" && !a.closing[pane] {
		if a.closing == nil {
			a.closing = map[string]bool{}
		}
		a.closing[pane] = true
		// The keyboard goes to the neighbor at once: not to what ends.
		if a.focusOf(s) == pane {
			for _, dir := range []string{"left", "up", "right", "down"} {
				if n := s.Layout.NeighborExcluding(pane, dir, func(id string) bool { return a.closing[id] }); n != "" {
					a.setFocus(s, n)
					a.paneRef(n).Focus()
					break
				}
			}
		}
		sid := s.ID
		time.AfterFunc(layoutMotion.Duration, func() {
			a.update(func() {
				delete(a.closing, pane)
				if s := a.session(sid); s != nil {
					a.removePane(s, pane)
				}
			})
		})
		return
	}
	if a.closing[pane] {
		return
	}
	a.removePane(s, pane)
}

// removePane takes a pane out of its session, and ends it.
func (a *App) removePane(s *proto.Session, pane string) {
	if s.PaneByID(pane) == nil {
		return
	}
	// The neighbor taking the room takes the focus, as kerod does. Closing
	// panes are not focus targets, so a farther live pane may take it.
	if a.focusOf(s) == pane && s.Layout != nil {
		for _, dir := range []string{"left", "up", "right", "down"} {
			if n := s.Layout.NeighborExcluding(pane, dir, func(id string) bool { return a.closing[id] }); n != "" {
				a.focus[s.ID] = n
				break
			}
		}
	}
	s.Layout = s.Layout.Remove(pane)
	if s.Layout.Leaf() {
		s.Layout = nil
	}
	if s.Zoomed == pane {
		s.Zoomed = ""
	}
	for i := range s.Panes {
		if s.Panes[i].ID == pane {
			s.Panes = append(s.Panes[:i], s.Panes[i+1:]...)
			break
		}
	}
	a.dropTerm(pane)
	a.call("pane.close", proto.IDParams{ID: pane}, nil)
	a.focusTerminal()
}

// evenPanes gives the panes of a session even room, gliding there.
func (a *App) evenPanes(s *proto.Session) {
	if s == nil || s.Layout == nil || s.Layout.Leaf() {
		return
	}
	s.Layout = s.Layout.Equalized()
	a.call("layout", proto.LayoutParams{Session: s.ID, Layout: s.Layout.Clone()}, nil)
}

func (a *App) zoomPane(s *proto.Session, pane string) {
	if s == nil || len(s.Panes) < 2 {
		return
	}
	if pane == "" {
		pane = a.focusOf(s)
	}
	if s.Zoomed == pane {
		s.Zoomed = ""
	} else {
		s.Zoomed = pane
		a.setFocus(s, pane)
	}
	a.call("zoom", proto.FocusParams{Session: s.ID, Pane: pane}, nil)
	a.paneRef(pane).Focus()
}

// focusDirection moves into the live pane next to the focused one. A closing
// pane is skipped as a candidate in the original layout, so it cannot
// dead-end the search before a live pane farther along.
func (a *App) focusDirection(dir string) {
	s := a.session(a.selected)
	if s == nil || s.Layout == nil || s.Zoomed != "" {
		return
	}
	n := s.Layout.NeighborExcluding(a.focusOf(s), dir, func(id string) bool { return a.closing[id] })
	if n == "" {
		return
	}
	a.setFocus(s, n)
	a.paneRef(n).Focus()
}

// cyclePane moves to the next or previous pane, in the layout's order,
// skipping panes that are already exiting.
func (a *App) cyclePane(d int) {
	s := a.session(a.selected)
	if s == nil || s.Layout == nil || len(s.Panes) < 2 {
		return
	}
	order := s.Layout.Panes()
	i := 0
	for j, id := range order {
		if id == a.focusOf(s) {
			i = j
			break
		}
	}
	for step := 1; step <= len(order); step++ {
		n := order[((i+d*step)%len(order)+len(order))%len(order)]
		if !a.closing[n] {
			a.setFocus(s, n)
			a.paneRef(n).Focus()
			return
		}
	}
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// defer_ runs fn once the view is built: for actions that change the
// panes or sessions being built.
func (a *App) defer_(fn func()) { a.later = append(a.later, fn) }

func chip(c *ui.Context, pal *palette, bg, fg ui.Color, fn func()) ui.Element {
	return ui.Row(c).Height(22).Padding(0, 9).Gap(6).Radius(11).AlignItems(ui.Center).Background(bg).TextColor(fg).Children(fn)
}

// closeFocused closes the pane the user is in, or the session with its
// last pane.
func (a *App) closeFocused() {
	if s := a.session(a.selected); s != nil {
		a.closePane(s, a.focusOf(s), true)
	}
}
