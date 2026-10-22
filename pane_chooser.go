package main

import (
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// chooserState is the New Pane chooser's: what the new pane shows, picked
// from a short list, by its letter, or with the arrows and Return.
type chooserState struct {
	open  bool
	at    string // the pane the new one opens beside, "" the focused one
	index int
	// plugin asks for a widget of a plugin: the plugin rows come first.
	plugin bool
}

// paneKind is what a pane can show, with the action that opens it and
// the letter that picks it in the chooser.
type paneKind struct {
	view   string
	action string
	key    ui.Key
	letter string

	// A widget of a plugin is a kind of pane too: its plugin and widget
	// name, and what the chooser says of it.
	plugin string
	widget string
	title  string
	detail string
}

var paneKinds = []paneKind{
	{view: "", action: "pane.new.terminal", key: ui.KeyT, letter: "T"},
	{view: proto.PaneTasks, action: "pane.new.tasks", key: ui.KeyL, letter: "L"},
	{view: proto.PaneFiles, action: "pane.new.files", key: ui.KeyE, letter: "E"},
	{view: proto.PaneBrowser, action: "pane.new.browser", key: ui.KeyB, letter: "B"},
	{view: proto.PaneAttention, action: "pane.new.attention", key: ui.KeyA, letter: "A"},
}

// chooserKinds is every kind the New Pane chooser offers: the app's own
// views, then a row for each widget of each plugin known.
func (a *App) chooserKinds() []paneKind {
	return append(append([]paneKind{}, paneKinds...), a.pluginWidgets()...)
}

// name is a kind's name in the chooser.
func (k paneKind) name() string {
	if k.plugin != "" {
		return k.title
	}
	return paneViewName(k.view)
}

// description is what a kind of pane is for, in the chooser.
func (k paneKind) description() string {
	if k.plugin != "" {
		return k.detail
	}
	return paneKindDetail(k.view)
}

// icon is a kind's mark in the chooser.
func (k paneKind) icon() *ui.SVG {
	if k.plugin != "" {
		return projectIcon
	}
	return paneViewIcon(k.view)
}

// paneKindDetail says what a kind of pane is for.
func paneKindDetail(view string) string {
	switch view {
	case proto.PaneTasks:
		return locales.PaneTasksDetail()
	case proto.PaneFiles:
		return locales.PaneFilesDetail()
	case proto.PaneBrowser:
		return locales.PaneBrowserDetail()
	case proto.PaneAttention:
		return locales.PaneAttentionDetail()
	}
	return locales.PaneTerminalDetail()
}

func (a *App) openChooser(at string) {
	if a.session(a.selected) == nil {
		return
	}
	a.chooser = chooserState{open: true, at: at}
}

// openView opens a pane showing view beside the pane at, the focused one
// when "": at its right, or below it. The Tasks view is the Linear plugin's
// board, which a second asking focuses.
func (a *App) openView(s *proto.Session, at, view string, below bool) {
	if s == nil {
		return
	}
	if view == proto.PaneTasks {
		// Tasks is the Linear plugin's board: a pane of it that the session
		// has takes the focus, an unmigrated Tasks pane of an older kerod
		// too. Else the pane is made as a plugin pane, with the board's
		// target, which an older kerod also takes (it has no Tasks pane
		// to make for the new app, and refuses a second one).
		p := pinPaneOf(s, pinRef{"linear", "board"})
		for i := range s.Panes {
			if p == nil && s.Panes[i].View == proto.PaneTasks {
				p = &s.Panes[i]
			}
		}
		if p != nil {
			a.setFocus(s, p.ID)
			a.paneRef(p.ID).Focus()
			return
		}
	}
	direction := "right"
	if below {
		direction = "down"
	}
	view, target := newPaneView(view)
	a.splitTarget(s, at, direction, view, target)
}

// newPaneView is the view and target a new pane of the kind is made with.
// Tasks is made as the Linear plugin's pane, never as a Tasks view: a kerod
// that has not migrated would make another blank pane of it, or refuse it.
func newPaneView(view string) (string, string) {
	if view == proto.PaneTasks {
		return proto.PanePlugins, linearPluginTarget()
	}
	return view, ""
}

// showInPane turns a pane into one showing view, a terminal when "". What
// a terminal runs ends, which asks first, as closing it would.
func (a *App) showInPane(s *proto.Session, pane, view string, ask bool) {
	p := s.PaneByID(pane)
	if a.cl == nil || p == nil || p.View == view {
		return
	}
	if ask && a.set.ConfirmClose && !p.IsView() && p.Status != proto.Exited && p.Process != "" {
		sid := s.ID
		a.confirm = &confirmation{
			open:    true,
			title:   locales.ConfirmShowTitle(paneViewName(view)),
			detail:  locales.ConfirmShowDetail(p.Process),
			action:  locales.ConfirmShowAction(),
			destroy: true,
			run: func() {
				if s := a.session(sid); s != nil {
					a.showInPane(s, pane, view, false)
				}
			},
		}
		return
	}
	cl := a.cl
	params := proto.CreateParams{Split: pane, View: view, Cols: p.Cols, Rows: p.Rows}
	go func() {
		var ns proto.Session
		err := cl.Call("pane.replace", params, &ns)
		a.update(func() {
			if err != nil {
				a.toast(viewCallError(err))
				return
			}
			a.dropTerm(pane)
			a.focusNew(ns)
		})
	}()
}

// chooserView is the New Pane chooser, over the window while open.
func (a *App) chooserView(c *ui.Context, pal *palette) {
	if !a.chooser.open {
		return
	}
	s := a.session(a.selected)
	if s == nil {
		a.chooser.open = false
		return
	}
	kinds := a.chooserKinds()
	n := len(kinds)
	if a.chooser.plugin {
		a.chooser.plugin, a.chooser.index = false, len(paneKinds)
	}
	a.chooser.index = (a.chooser.index%n + n) % n
	chosen := -1
	// Shift held, the pane opens below rather than beside, which the
	// title says as it is held.
	below := c.Modifiers()&ui.Shift != 0
	ui.DialogBase(c.Key("chooser"), &a.chooser.open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.overlay).Justify(ui.Start).Padding(120, 0, 0, 0)
		floating(panel.Width(380).Padding(6).Gap(2).Radius(14).Background(pal.popover), pal).Label(locales.ActionNewPane())
		title := locales.PaneNewTitle()
		if below {
			title = locales.PaneNewBelowTitle()
		}
		ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).Padding(8, 10, 6, 10)
		if c.Shortcut(0, ui.KeyDown) || c.Shortcut(ui.Ctrl, ui.KeyN) {
			a.chooser.index = (a.chooser.index + 1) % n
		}
		if c.Shortcut(0, ui.KeyUp) || c.Shortcut(ui.Ctrl, ui.KeyP) {
			a.chooser.index = (a.chooser.index + n - 1) % n
		}
		if c.Shortcut(0, ui.KeyEnter) {
			chosen = a.chooser.index
		}
		if c.Shortcut(ui.Shift, ui.KeyEnter) {
			chosen, below = a.chooser.index, true
		}
		for i, k := range kinds {
			if k.key != 0 && c.Shortcut(0, k.key) {
				chosen = i
			}
			if k.key != 0 && c.Shortcut(ui.Shift, k.key) {
				chosen, below = i, true
			}
			row := ui.Row(c.Key(i)).Height(48).Padding(0, 10).Gap(12).AlignItems(ui.Center).Radius(9)
			if row.Hovered() && row.Pressed() {
				a.chooser.index = i
			}
			if i == a.chooser.index {
				row.Background(pal.accent.Alpha(0.12))
			} else if row.Hovered() {
				row.Background(pal.rowHover)
			}
			if row.Clicked() {
				chosen = i
				below = below || row.ClickModifiers()&ui.Shift != 0
			}
			row.Children(func() {
				ui.Box(c).Size(30, 30).Radius(8).Background(pal.kbd).Center().Shrink(0).Children(func() {
					if k.plugin != "" {
						ui.Icon(c, k.icon()).Size(16, 16).TextColor(pal.textMuted)
					} else {
						paneViewMark(c, k.view, 16, pal.textMuted, pal.dark)
					}
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, k.name()).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine()
					ui.Text(c, k.description()).FontSize(12).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
				})
				if keys := a.keys(k.action); keys != "" {
					ui.Text(c, keys).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Shrink(0)
				}
				if k.letter != "" {
					kbd(c, pal, k.letter)
				}
			})
		}
		ui.Row(c).Padding(8, 10, 4, 10).Gap(5).AlignItems(ui.Center).Children(func() {
			kbd(c, pal, "⇧")
			ui.Text(c, locales.PaneNewBelowHint()).FontSize(11.5).TextColor(pal.textFaint).SingleLine()
		})
	})
	if chosen >= 0 {
		at := a.chooser.at
		a.chooser.open = false
		k := kinds[chosen]
		if k.plugin != "" {
			a.openPluginPane(s, at, k.plugin, k.widget, below)
			return
		}
		a.openView(s, at, k.view, below)
		return
	}
	if !a.chooser.open {
		a.focusTerminal()
	}
}
