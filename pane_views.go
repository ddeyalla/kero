package main

import (
	"encoding/json"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"

	"kero/locales"
)

// A pane shows a terminal, or a view of the app's: its files, a web page,
// every session that needs the user, or a plugin's widget (the Linear tasks
// are the Linear plugin's). A view runs nothing in kerod, which only keeps
// where it is in the layout and what it shows (Target); the app draws it,
// and keeps what it holds while it shows: a folder unfolded, a page, a
// filter.

// paneViewSymbol is the SF Symbol of a view whose mark is one, "" for a
// view whose mark is an icon of icons.go.
func paneViewSymbol(view string) string {
	if view == proto.PaneAttention {
		return "bell.badge"
	}
	return ""
}

// paneViewIcon is the mark of what a pane shows, as an icon of icons.go:
// the terminal's for a view whose mark is an SF Symbol, which paneViewMark
// draws in its place.
func paneViewIcon(view string) *ui.SVG {
	switch view {
	case proto.PaneTasks:
		return trayIcon
	case proto.PaneFiles:
		return filesIcon
	case proto.PaneBrowser:
		return globeIcon
	case proto.PanePlugins:
		return projectIcon
	}
	return terminalIcon
}

// paneViewMark draws a view's mark: its symbol where it has one, its icon
// otherwise.
func paneViewMark(c *ui.Context, view string, size float32, tint ui.Color, dark bool) {
	if name := paneViewSymbol(view); name != "" {
		if symbolMark(c, name, size, tint, dark) {
			return
		}
	}
	controlIcon(c, paneViewIcon(view), size, tint, dark)
}

// paneViewName is a view's name, as its header shows it.
func paneViewName(view string) string {
	switch view {
	case proto.PaneTasks:
		return locales.PaneTasks()
	case proto.PaneFiles:
		return locales.PaneFiles()
	case proto.PaneBrowser:
		return locales.PaneBrowser()
	case proto.PanePlugins:
		return locales.PanePlugins()
	case proto.PaneAttention:
		return locales.PaneAttention()
	}
	return locales.PaneTerminal()
}

// viewPane builds a view's body, which takes the keyboard as a terminal
// does: a click, or the pane's focus, gives it.
func (a *App) viewPane(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane) {
	ref := a.paneRef(p.ID)
	// The body owns pane focus, but it is not the button that every control
	// inside it is: an explicit group keeps its plugin, file, browser and
	// attention children as separate accessible elements.
	body := ui.Column(c.Key("body")).Grow(1).MinHeight(0).MinWidth(0).Focusable().Role(ui.RoleGroup).Bind(ref)
	if body.Pressed() {
		ref.Focus(c)
	}
	if ref.Focused(c) && a.focusOf(s) != p.ID {
		a.setFocus(s, p.ID)
	}
	body.Children(func() {
		switch p.View {
		case proto.PaneFiles:
			a.filesPane(c, pal, s, p, body)
		case proto.PaneBrowser:
			a.browserPane(c, pal, s, p, body)
		case proto.PanePlugins:
			a.pluginPane(c, pal, s, p, body)
		case proto.PaneTasks:
			// A Tasks pane that an older kerod kept, not migrated: the
			// Linear plugin's board draws it.
			linear := *p
			linear.View, linear.Target = proto.PanePlugins, linearPluginTarget()
			a.pluginPane(c, pal, s, &linear, body)
		case proto.PaneAttention:
			a.attentionPane(c, pal, s, p, body)
		}
	})
}

// linearPluginTarget is the target of the Linear plugin's board, as a list
// of the session's project with finished issues hidden. It is the one
// kerod's migration gives a Tasks pane (internal/daemon/store.go).
func linearPluginTarget() string {
	t, err := protocol.EncodeTarget(protocol.Target{
		Plugin: "linear",
		Widget: "board",
		State:  json.RawMessage(`{"mode":"list","scope":"project","hideDone":true}`),
	})
	if err != nil {
		// The target is a constant that encodes; a failure is a bug.
		panic("linear plugin target: " + err.Error())
	}
	return t
}

// paneOf is the pane with the id, of any session, nil none.
func (a *App) paneOf(id string) *proto.Pane {
	for i := range a.st.Sessions {
		if p := a.st.Sessions[i].PaneByID(id); p != nil {
			return p
		}
	}
	return nil
}

// isViewPane tells a pane that is a view, which has no terminal to
// attach.
func (a *App) isViewPane(id string) bool {
	p := a.paneOf(id)
	return p != nil && p.IsView()
}

// viewSubtitle is what a view's header says beside its name: the Linear
// project of the tasks, the file or folder of the files, the page.
func (a *App) viewSubtitle(s *proto.Session, p *proto.Pane) string {
	switch p.View {
	case proto.PaneFiles:
		// The folder: the file read names itself in the pane.
		if st := a.files[p.ID]; st != nil {
			return proto.ShortDir(st.root)
		}
		return proto.ShortDir(p.Dir)
	case proto.PaneBrowser:
		return a.browserTitle(p)
	case proto.PanePlugins:
		return a.pluginSubtitle(p)
	}
	return ""
}
