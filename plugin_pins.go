package main

// Pins put a widget of a plugin in the sidebar, above the sessions, as the
// built-in views once had a row of their own. A click on a pin shows the
// widget in the session that is selected: its pane if the session has one,
// else a new pane beside the others. The pins are the app's own and not
// kerod's, so they stay when a plugin pane closes, and a pin of a plugin
// that is not installed waits, hidden, for the plugin to come back.

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
	"kero/locales"
)

// pinRef is a widget of a plugin that the user pinned to the sidebar.
type pinRef struct {
	Plugin string `json:"plugin"`
	Widget string `json:"widget"`
}

// pinned tells whether a widget of a plugin is pinned.
func (a *App) pinned(plugin, widget string) bool {
	return slices.Contains(a.prefs.Pinned, pinRef{plugin, widget})
}

// pinWidget is the widget a pin names: the one given, or the plugin's first.
func (a *App) pinWidget(plugin, widget string) string {
	if widget == "" {
		return a.firstWidget(plugin)
	}
	return widget
}

// pinPlugin pins a widget of a plugin, the first one when widget is empty,
// and saves it. It tells whether the widget is pinned now: a plugin that is
// not installed, or a widget it has not, is not.
func (a *App) pinPlugin(plugin, widget string) bool {
	p, ok := a.pluginByID(plugin)
	if !ok {
		a.toast(locales.PluginUnknownDetail(plugin))
		return false
	}
	widget = a.pinWidget(plugin, widget)
	if widget == "" || p.Manifest.WidgetByName(widget) == nil {
		a.toast(locales.PluginNoWidgets(p.Manifest.Name))
		return false
	}
	if !a.pinned(plugin, widget) {
		a.prefs.Pinned = append(a.prefs.Pinned, pinRef{plugin, widget})
		a.savePrefs()
	}
	return true
}

// unpinPlugin takes a widget off the sidebar. A pin of a plugin that is not
// installed can be taken off too, so the widget is not looked up.
func (a *App) unpinPlugin(plugin, widget string) {
	widget = a.pinWidget(plugin, widget)
	pins := slices.DeleteFunc(slices.Clone(a.prefs.Pinned), func(r pinRef) bool {
		return r.Plugin == plugin && (widget == "" || r.Widget == widget)
	})
	if len(pins) == len(a.prefs.Pinned) {
		return
	}
	a.prefs.Pinned = pins
	a.savePrefs()
}

// togglePin pins a widget that is not pinned, and unpins one that is.
func (a *App) togglePin(plugin, widget string) {
	if a.pinned(plugin, widget) {
		a.unpinPlugin(plugin, widget)
		return
	}
	a.pinPlugin(plugin, widget)
}

// migratePins makes the pins the app starts with, once: the Linear row of
// the sidebar, if the user had it on, is the pin of the Linear plugin's
// board. It runs after the settings and the prefs are loaded, and saves
// nothing itself: the next save of the prefs keeps the pins and that they
// were made, and a launch that saved none makes the same pins again.
func (a *App) migratePins() {
	if a.prefs.PinsSeeded {
		return
	}
	a.prefs.PinsSeeded = true
	if a.set.LinearSidebar && !a.pinned("linear", "board") {
		a.prefs.Pinned = append(a.prefs.Pinned, pinRef{"linear", "board"})
	}
}

// pinRow is a pin as the sidebar shows it: the plugin's name and the icon
// of the widget.
type pinRow struct {
	ref  pinRef
	name string
	icon string
}

// pinRows are the pins of the plugins the app knows, in the order they were
// pinned.
func (a *App) pinRows() []pinRow {
	var rows []pinRow
	for _, r := range a.prefs.Pinned {
		p, ok := a.pluginByID(r.Plugin)
		if !ok {
			continue
		}
		w := p.Manifest.WidgetByName(r.Widget)
		if w == nil {
			continue
		}
		rows = append(rows, pinRow{ref: r, name: p.Manifest.Name, icon: w.Icon})
	}
	return rows
}

// pinPaneOf finds the pane of a pinned widget in a session.
func pinPaneOf(s *proto.Session, ref pinRef) *proto.Pane {
	for i := range s.Panes {
		p := &s.Panes[i]
		if p.View != proto.PanePlugins || p.Target == "" {
			continue
		}
		if t, err := protocol.DecodeTarget(p.Target); err == nil && t.Plugin == ref.Plugin && t.Widget == ref.Widget {
			return p
		}
	}
	return nil
}

// openPin shows a pinned widget in the selected session: its pane when the
// session has one, else a new pane beside the others.
func (a *App) openPin(ref pinRef) {
	s := a.session(a.selected)
	if s == nil {
		a.toast(locales.PluginCommandNoSession(a.pluginName(ref.Plugin)))
		return
	}
	if p := pinPaneOf(s, ref); p != nil {
		a.selectSession(s.ID)
		if s = a.session(s.ID); s != nil {
			a.setFocus(s, p.ID)
		}
		return
	}
	if a.plugins.openPane != nil {
		a.plugins.openPane(s, ref.Plugin, ref.Widget)
		return
	}
	a.openPluginPane(s, "", ref.Plugin, ref.Widget, false)
}

// pinNav is the sidebar's rows of the pinned widgets, drawn as the session
// rows are: an icon, a name, a pill when the focused pane is the widget.
func (a *App) pinNav(c *ui.Context, pal *palette) {
	if len(a.prefs.Pinned) == 0 {
		return
	}
	if !a.plugins.knownAt {
		a.discoverPlugins()
		return
	}
	rows := a.pinRows()
	if len(rows) == 0 {
		return
	}
	view := a.plugins.pinView
	if view == nil {
		view = newPluginView()
		a.plugins.pinView = view
	}
	var here pinRef
	if s := a.session(a.selected); s != nil && a.show == "session" {
		if p := s.PaneByID(a.focusOf(s)); p != nil && p.View == proto.PanePlugins && p.Target != "" {
			if t, err := protocol.DecodeTarget(p.Target); err == nil {
				here = pinRef{t.Plugin, t.Widget}
			}
		}
	}
	for _, r := range rows {
		a.pinItem(c, pal, view, r, r.ref == here)
	}
}

// pinItem is one pinned widget's row of the sidebar.
func (a *App) pinItem(c *ui.Context, pal *palette, view *pluginView, r pinRow, sel bool) {
	item := ui.ButtonBase(c.Key("pin-"+r.ref.Plugin+"-"+r.ref.Widget)).Height(30).Padding(0, 10).Gap(9).AlignItems(ui.Center).Radius(8).Label(r.name)
	bg := ui.Transparent
	switch {
	case sel:
		bg = pal.rowSelected
		chosen(item, pal)
	case item.Hovered():
		bg = pal.rowHover
	}
	item.Background(item.AnimateColor("bg", bg, hover))
	if item.Clicked() {
		a.openPin(r.ref)
	}
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.PluginUnpin()).Chosen() {
			a.unpinPlugin(r.ref.Plugin, r.ref.Widget)
		}
	})
	item.Children(func() {
		fg := pal.textMuted
		if sel {
			fg = pal.text
		}
		if r.icon != "" {
			if sym, scale := view.symbol(c, r.icon, 16, symbolMedium); sym != nil {
				drawSymbol(c, "symbol", sym, scale, fg)
			} else if svg, ok := pluginIcon(r.icon); ok {
				ui.Icon(c.Key("icon"), svg).Size(15, 15).TextColor(fg).Shrink(0)
			}
		}
		ui.Text(c, r.name).FontSize(12.5).FontWeight(600).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
	})
}

// pluginPinMenu adds the pin item to the menu of a plugin pane's header:
// Pin to Sidebar, or Unpin from it when the pane's widget is pinned.
func (a *App) pluginPinMenu(m *ui.Menu, p *proto.Pane) {
	if p.View != proto.PanePlugins || p.Target == "" {
		return
	}
	t, err := protocol.DecodeTarget(p.Target)
	if err != nil || t.Plugin == "" {
		return
	}
	widget := a.pinWidget(t.Plugin, t.Widget)
	if widget == "" {
		return
	}
	m.Separator()
	label := locales.PluginPin()
	if a.pinned(t.Plugin, widget) {
		label = locales.PluginUnpin()
	}
	if m.Item(label).Chosen() {
		a.togglePin(t.Plugin, widget)
	}
}

// pinPaletteItems are the palette's commands that pin and unpin the
// widgets of the plugins the app knows: Pin X to Sidebar, or Unpin it when
// it is pinned.
func (a *App) pinPaletteItems() []paletteItem {
	var items []paletteItem
	for _, k := range a.pluginWidgets() {
		plugin, widget, name := k.plugin, k.widget, k.title
		if p, ok := a.pluginByID(plugin); ok && len(p.Manifest.Widgets) > 1 {
			name = strings.TrimSpace(name + " " + k.detail)
		}
		title := locales.PalettePinPlugin(name)
		if a.pinned(plugin, widget) {
			title = locales.PaletteUnpinPlugin(name)
		}
		items = append(items, paletteItem{title: title, run: func() { a.togglePin(plugin, widget) }})
	}
	return items
}

// pinCommand runs the pin and unpin actions of `kero plugin pin|unpin
// <plugin> [widget]`, which arrive as the actions plugin.pin and
// plugin.unpin with the plugin and the widget as arguments.
func (a *App) pinCommand(args map[string]string, pin bool) {
	plugin, widget := args["plugin"], args["widget"]
	if plugin == "" {
		return
	}
	if pin {
		a.pinPlugin(plugin, widget)
		return
	}
	a.unpinPlugin(plugin, widget)
}

// pinSetting is the toggle of a plugin's widget in Settings › Plugins:
// whether the widget has a row in the sidebar.
func (a *App) pinSetting(c *ui.Context, pal *palette, m protocol.Manifest, w protocol.Widget) {
	label := locales.PluginPinSetting()
	if len(m.Widgets) > 1 {
		label = locales.PluginPinWidgetSetting(w.Title)
	}
	on := a.pinned(m.ID, w.Name)
	was := on
	settingRow(c, pal, label, locales.PluginPinSettingDetail(), func() {
		toggle(c.Key("plugin-pin-"+m.ID+"-"+w.Name), &on, pal)
	})
	if on != was {
		a.togglePin(m.ID, w.Name)
	}
}
