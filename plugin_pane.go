package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/host"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/proto"
	"kero/locales"
)

// pluginState is what the app keeps about the plugins it hosts: the one
// process host, the plugins it knows, and the pane of each plugin the app
// draws.
type pluginState struct {
	host  *host.Host
	known []registry.Plugin

	// The plugins are found off the UI goroutine, once, when the window
	// opens. Drawing, the palette and the chooser read the cache only.
	knownAt     bool
	discovering bool
	// discover reads the installed plugins. The app leaves it nil, which
	// means registry.Known; a test sets it to watch the read happen off
	// the UI goroutine.
	discover func(dir string) []registry.Plugin

	panes map[string]*pluginPaneState

	// settings and hasSecret are what the settings page shows, read once
	// off the UI goroutine: a secret is shown as set, never as its value.
	settings   map[string]map[string]string
	hasSecret  map[string]map[string]bool
	loading    bool
	pageLoaded bool

	theme    host.Theme
	themeSet bool

	logs    map[string]string
	crashed map[string]string
	notice  string
}

// pluginPaneState is one pane of a plugin: the view that draws its tree,
// and what the app has told the plugin about it.
type pluginPaneState struct {
	plugin string
	widget string
	state  json.RawMessage
	target string

	view  *pluginView
	tree  *protocol.Tree
	err   string
	saved json.RawMessage

	opened  bool
	size    protocol.Size
	visible bool
	shown   bool
}

// knownPlugins lists the plugins the app knows: its own, and the ones the
// user installed under proto.Dir()/plugins. It reads the cache that
// discoverPlugins filled, and never its disk: a slow config folder must not
// stop a frame.
func (a *App) knownPlugins() []registry.Plugin {
	return a.plugins.known
}

// discoverPlugins finds the installed plugins off the UI goroutine, and
// caches them when it is done. The first read of proto.Dir()/plugins is a
// folder walk and a JSON file per plugin, which can wait on a slow volume.
// Until it finishes, the app draws no plugin rows; it invalidates the
// window when they are known.
func (a *App) discoverPlugins() {
	if a.plugins.knownAt || a.plugins.discovering {
		return
	}
	a.plugins.discovering = true
	discover := a.plugins.discover
	if discover == nil {
		discover = registry.Known
	}
	dir := filepath.Join(proto.Dir(), "plugins")
	go func() {
		plugins := discover(dir)
		a.update(func() { a.applyPlugins(plugins) })
	}()
}

// applyPlugins caches the plugins the discovery found. It runs on the UI
// goroutine, through win.Update.
func (a *App) applyPlugins(plugins []registry.Plugin) {
	a.plugins.known = plugins
	a.plugins.knownAt = true
	a.plugins.discovering = false
	if a.win != nil {
		a.win.Invalidate()
	}
}

// pluginByID finds one of the plugins the app knows.
func (a *App) pluginByID(id string) (registry.Plugin, bool) {
	return registry.Find(a.knownPlugins(), id)
}

// pluginName is a plugin's name, or its id when it is not installed.
func (a *App) pluginName(id string) string {
	if p, ok := a.pluginByID(id); ok {
		return p.Manifest.Name
	}
	return id
}

// pluginWidgetTitle is what a plugin calls one of its widgets.
func (a *App) pluginWidgetTitle(plugin, widget string) string {
	if p, ok := a.pluginByID(plugin); ok {
		if w := p.Manifest.WidgetByName(widget); w != nil && w.Title != "" {
			return w.Title
		}
	}
	return widget
}

// pluginHost is the process host, made the first time a plugin pane needs
// it. A plugin runs in a process of its own: its I/O is never the UI
// goroutine's.
func (a *App) pluginHost() *host.Host {
	if a.plugins.host != nil {
		return a.plugins.host
	}
	a.plugins.host = host.New(host.Config{
		Dir:     proto.Dir(),
		Plugins: a.knownPlugins(),
		Callbacks: host.Callbacks{
			Ready: func(plugin string) {
				a.update(func() {
					for _, st := range a.plugins.panes {
						if st.plugin == plugin {
							st.opened = false
						}
					}
					a.win.Invalidate()
				})
			},
			Render: func(plugin, pane string, tree *protocol.Tree) {
				a.update(func() {
					st := a.plugins.panes[pane]
					if st == nil || st.plugin != plugin {
						return
					}
					st.tree = tree
					delete(a.plugins.crashed, plugin)
					a.win.Invalidate()
				})
			},
			State: func(plugin, pane string, state json.RawMessage) {
				a.update(func() { a.pluginStateSaved(plugin, pane, state) })
			},
			Notify: func(plugin, title, body string) {
				a.update(func() { a.showNotification(plugin, title, body) })
			},
			OpenURL: func(plugin, url string) {
				a.update(func() { openPath(url) })
			},
			Crash: func(plugin, message, logPath string) {
				a.update(func() {
					if a.plugins.crashed == nil {
						a.plugins.crashed = map[string]string{}
					}
					if a.plugins.logs == nil {
						a.plugins.logs = map[string]string{}
					}
					a.plugins.crashed[plugin] = message
					a.plugins.logs[plugin] = logPath
					for _, st := range a.plugins.panes {
						if st.plugin == plugin {
							st.err = message
							st.opened = false
							st.tree = nil
						}
					}
					a.win.Invalidate()
				})
			},
		},
	})
	return a.plugins.host
}

// pluginPaneState is the app's state of one plugin pane, made on its first
// draw and brought up to date with the pane's target.
func (a *App) pluginPaneState(p *proto.Pane) *pluginPaneState {
	if a.plugins.panes == nil {
		a.plugins.panes = map[string]*pluginPaneState{}
	}
	st := a.plugins.panes[p.ID]
	if st == nil {
		st = &pluginPaneState{view: newPluginView()}
		a.plugins.panes[p.ID] = st
	}
	if p.Target != st.target {
		st.target = p.Target
		st.saved = nil
		var target protocol.Target
		if p.Target != "" {
			if t, err := protocol.DecodeTarget(p.Target); err == nil {
				target = t
			}
		}
		st.plugin, st.widget = target.Plugin, target.Widget
		// A target may name only the plugin: its first widget then.
		if st.plugin != "" && st.widget == "" {
			st.widget = a.firstWidget(st.plugin)
		}
		st.state = target.State
		if len(target.State) > 0 {
			st.saved = target.State
		}
	}
	return st
}

// pluginStateSaved writes state a plugin saved for a pane into the pane's
// target, with the daemon call the other views use. Then the pane comes
// back after a restart.
func (a *App) pluginStateSaved(plugin, pane string, state json.RawMessage) {
	st := a.plugins.panes[pane]
	if st == nil || st.plugin != plugin {
		return
	}
	st.state = state
	if len(state) == 0 || string(state) == string(st.saved) {
		return
	}
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: plugin, Widget: st.widget, State: state})
	if err != nil {
		a.toast(err.Error())
		return
	}
	st.saved = state
	a.call("pane.target", proto.TargetParams{Pane: pane, Target: target}, nil)
}

// pluginPane draws one pane of a plugin: its tree, its start, or the error
// it stopped with.
func (a *App) pluginPane(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane, body ui.Element) {
	st := a.pluginPaneState(p)
	st.shown = true
	if st.err == "" && st.plugin == "" {
		paneNothing(c, pal, projectIcon, locales.PluginUnknownTitle(), locales.PluginUnknownDetail(p.ID))
		return
	}
	if st.err != "" {
		a.pluginCrash(c, pal, st)
		return
	}
	// The list of plugins is read off the UI goroutine when the window
	// opens. A pane that comes back before it is done waits one frame
	// instead of stopping the window.
	if !a.plugins.knownAt {
		a.discoverPlugins()
		paneNothing(c, pal, projectIcon, locales.PluginStarting(a.pluginName(st.plugin)), locales.PluginStartingDetail())
		return
	}
	plugin, ok := a.pluginByID(st.plugin)
	if !ok {
		paneNothing(c, pal, projectIcon, locales.PluginUnknownTitle(), locales.PluginUnknownDetail(st.plugin))
		return
	}
	// A target may name only its plugin; its widget is the first one,
	// once the manifest is known.
	if st.widget == "" {
		st.widget = a.firstWidget(st.plugin)
	}
	if st.widget == "" {
		paneNothing(c, pal, projectIcon, locales.PluginUnknownTitle(), locales.PluginNoWidgets(plugin.Manifest.Name))
		return
	}

	// The plugin hears how the app looks, once for every plugin.
	theme := host.Theme{Dark: pal.dark, Accent: a.set.Accent}
	if !a.plugins.themeSet || a.plugins.theme != theme {
		a.plugins.theme, a.plugins.themeSet = theme, true
		a.pluginHost().SetTheme(theme)
	}

	size := protocol.Size{Width: max(int(a.cardSize[p.ID].W), 1), Height: max(int(a.cardSize[p.ID].H), 1)}
	h := a.pluginHost()
	if !st.opened {
		if err := h.Open(plugin.Manifest.ID, host.Pane{ID: p.ID, Widget: st.widget, State: st.state, Size: size}); err != nil {
			a.toast(err.Error())
		} else {
			st.opened = true
		}
	} else if st.size != size {
		h.Resize(p.ID, size)
	}
	st.size = size
	if !st.visible {
		h.Visible(p.ID, true)
		st.visible = true
	}

	if st.tree == nil {
		paneNothing(c, pal, projectIcon, locales.PluginStarting(plugin.Manifest.Name), locales.PluginStartingDetail())
		return
	}
	if st.view == nil {
		st.view = newPluginView()
	}
	st.view.draw(c, pal, st.tree, func(ev pluginEvent) {
		a.pluginHost().Event(p.ID, ev.Node, ev.Kind, ev.Value)
	})
}

// pluginCrash draws why a plugin stopped, with the Restart button. Nothing
// restarts it on its own: a crash loop would be a plugin that takes the
// app down with it.
func (a *App) pluginCrash(c *ui.Context, pal *palette, st *pluginPaneState) {
	ui.Column(c.Key("plugin-crash")).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Icon(c, projectIcon).Size(30, 30).TextColor(pal.danger)
		ui.Text(c, locales.PluginCrashTitle(a.pluginName(st.plugin))).FontSize(13.5).FontWeight(600).TextColor(pal.text).TextAlign(ui.Center)
		ui.Text(c, st.err).FontSize(12.5).TextColor(pal.textMuted).TextAlign(ui.Center).MaxWidth(420)
		if path := a.plugins.logs[st.plugin]; path != "" {
			ui.Text(c, locales.PluginCrashLog(path)).FontSize(11.5).TextColor(pal.textFaint).TextAlign(ui.Center).MaxWidth(420)
		}
		ui.Row(c).Gap(8).Children(func() {
			if button(c, "plugin-restart", locales.PluginRestart(), "", false, pal) {
				a.restartPlugin(st.plugin)
			}
		})
	})
}

// restartPlugin starts a crashed plugin again, and opens its panes on the
// new process.
func (a *App) restartPlugin(plugin string) {
	if err := a.pluginHost().Restart(plugin); err != nil {
		a.toast(err.Error())
		return
	}
	delete(a.plugins.crashed, plugin)
	for _, st := range a.plugins.panes {
		if st.plugin == plugin {
			st.err = ""
			st.opened = false
			st.tree = nil
		}
	}
	a.win.Invalidate()
}

// beginPluginFrame marks every plugin pane as not drawn, so that the end
// of the frame can tell which ones are hidden.
func (a *App) beginPluginFrame() {
	for _, st := range a.plugins.panes {
		st.shown = false
	}
}

// sweepPluginPanes closes the panes that are gone, and tells a plugin
// which of its panes are still on screen.
func (a *App) sweepPluginPanes(alive map[string]bool) {
	if len(a.plugins.panes) == 0 {
		return
	}
	h := a.plugins.host
	for id, st := range a.plugins.panes {
		if !alive[id] {
			if h != nil {
				h.Close(id)
			}
			delete(a.plugins.panes, id)
			continue
		}
		if st.visible && !st.shown && h != nil {
			h.Visible(id, false)
			st.visible = false
		}
	}
}

// stopPlugins ends every plugin process when the app quits.
func (a *App) stopPlugins() {
	if a.plugins.host != nil {
		a.plugins.host.Stop()
	}
}

// viewCallError turns a daemon error into one the user can act on. An old
// daemon has no plugin view; saying that is not a message a user can use,
// so the app asks for the restart that adds the view instead.
func viewCallError(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), `no view "plugin"`) {
		return locales.PluginNeedsRestart()
	}
	return err.Error()
}

// openPluginPane splits a pane with a widget of a plugin. An empty plugin
// takes the first widget of the first plugin the app knows.
func (a *App) openPluginPane(s *proto.Session, at, plugin, widget string, below bool) {
	if s == nil {
		return
	}
	if plugin == "" {
		plugin, widget = a.firstPluginWidget()
	}
	if plugin == "" {
		a.toast(locales.PluginNoPlugins())
		return
	}
	if widget == "" {
		widget = a.firstWidget(plugin)
	}
	if widget == "" {
		a.toast(locales.PluginNoWidgets(a.pluginName(plugin)))
		return
	}
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: plugin, Widget: widget})
	if err != nil {
		a.toast(err.Error())
		return
	}
	direction := "right"
	if below {
		direction = "down"
	}
	a.splitTarget(s, at, direction, proto.PanePlugins, target)
}

// pluginWidgets is every widget of every plugin the app knows, in the
// order the New Pane chooser and the palette show them.
func (a *App) pluginWidgets() []paneKind {
	var kinds []paneKind
	for _, p := range a.knownPlugins() {
		for _, w := range p.Manifest.Widgets {
			kinds = append(kinds, paneKind{view: proto.PanePlugins, action: "pane.new.plugin", plugin: p.Manifest.ID, widget: w.Name, title: p.Manifest.Name, detail: w.Title})
		}
	}
	return kinds
}

// firstPluginWidget is the first widget of the first plugin, for the
// shortcut that opens one without asking.
func (a *App) firstPluginWidget() (plugin, widget string) {
	kinds := a.pluginWidgets()
	if len(kinds) == 0 {
		return "", ""
	}
	return kinds[0].plugin, kinds[0].widget
}

func (a *App) firstWidget(plugin string) string {
	if p, ok := a.pluginByID(plugin); ok && len(p.Manifest.Widgets) > 0 {
		return p.Manifest.Widgets[0].Name
	}
	return ""
}

// newPluginPane is the shortcut's action: one plugin widget opens itself,
// and several ask in the New Pane chooser.
func (a *App) newPluginPane() {
	s := a.session(a.selected)
	if s == nil {
		return
	}
	a.discoverPlugins()
	kinds := a.pluginWidgets()
	if len(kinds) == 0 {
		if !a.plugins.knownAt {
			// The list is still being read; the chooser fills itself
			// with the plugin rows when it lands.
			a.chooser = chooserState{open: true, index: len(paneKinds), plugin: true}
			return
		}
		a.toast(locales.PluginNoPlugins())
		return
	}
	if len(kinds) == 1 {
		a.openPluginPane(s, "", kinds[0].plugin, kinds[0].widget, false)
		return
	}
	a.chooser = chooserState{open: true, index: len(paneKinds), plugin: true}
}

// --- Settings > Plugins ---

// loadPluginSettings reads the settings and the set secrets of every
// plugin once, off the UI goroutine.
func (a *App) loadPluginSettings() {
	if a.plugins.pageLoaded || a.plugins.loading {
		return
	}
	a.plugins.loading = true
	plugins := a.knownPlugins()
	h := a.pluginHost()
	go func() {
		settings := map[string]map[string]string{}
		hasSecret := map[string]map[string]bool{}
		ids := []string{}
		for _, p := range plugins {
			ids = append(ids, p.Manifest.ID)
		}
		if _, ok := registry.Find(plugins, "spotify"); !ok {
			ids = append(ids, "spotify")
		}
		for _, id := range ids {
			values := h.Store().Settings(id)
			settings[id] = values
			secrets := map[string]bool{}
			for _, p := range plugins {
				if p.Manifest.ID != id {
					continue
				}
				for _, setting := range p.Manifest.Settings {
					if setting.Secret && h.Store().HasSecret(id, setting.Key) {
						secrets[setting.Key] = true
					}
				}
			}
			hasSecret[id] = secrets
		}
		a.update(func() {
			a.plugins.settings = settings
			a.plugins.hasSecret = hasSecret
			a.plugins.loading = false
			a.plugins.pageLoaded = true
			a.win.Invalidate()
		})
	}()
}

// pluginField is the text field of one plugin setting, kept between
// frames.
func (a *App) pluginField(key, value string) *string {
	if a.ss.pluginDraft == nil {
		a.ss.pluginDraft = map[string]*string{}
	}
	draft := a.ss.pluginDraft[key]
	if draft == nil {
		draft = new(string)
		*draft = value
		a.ss.pluginDraft[key] = draft
	}
	return draft
}

func (a *App) pluginsPage(c *ui.Context, pal *palette) {
	// Reading the plugins is off the UI goroutine; until it is done the
	// page draws nothing rather than a list that is not there.
	if !a.plugins.knownAt {
		a.discoverPlugins()
		return
	}
	a.loadPluginSettings()
	plugins := a.knownPlugins()
	settingsGroup(c, pal, locales.PluginSettingsGroup(), locales.PluginSettingsGroupDetail(), func() {
		if len(plugins) == 0 {
			settingRow(c, pal, locales.PluginNone(), locales.PluginNoneDetail(), func() {})
		}
		for _, p := range plugins {
			a.pluginCard(c, pal, p)
		}
	})
	if _, ok := registry.Find(plugins, "spotify"); !ok {
		settingsGroup(c, pal, locales.PluginSpotifyGroup(), locales.PluginSpotifyGroupDetail(), func() {
			key := "spotify.clientId"
			draft := a.pluginField(key, a.plugins.settings["spotify"]["clientId"])
			settingRow(c, pal, locales.PluginSpotifyClientID(), locales.PluginSpotifyClientIDDetail(), func() {
				in := textField(c, "plugin-spotify-client", draft, locales.PluginSpotifyClientIDPlaceholder(), 260, pal)
				in.OnSubmit(func() { a.savePluginSetting("spotify", "clientId", *draft) })
			})
			settingRow(c, pal, locales.PluginSpotifyDashboard(), "", func() {
				if button(c, "plugin-spotify-dashboard", locales.PluginSpotifyOpen(), "", false, pal) {
					openPath("https://developer.spotify.com/dashboard")
				}
			})
		})
	}
	if a.plugins.notice != "" {
		ui.Text(c, a.plugins.notice).FontSize(12).TextColor(pal.textFaint).Padding(4, 0)
	}
}

// pluginCard is one plugin in Settings: what it is, what it may do, and
// its settings fields.
func (a *App) pluginCard(c *ui.Context, pal *palette, p registry.Plugin) {
	m := p.Manifest
	kind := locales.PluginInstalled()
	if p.Builtin {
		kind = locales.PluginBuiltin()
	}
	permissions := locales.PluginNoPermissions()
	if len(m.Permissions) > 0 {
		permissions = strings.Join(m.Permissions, ", ")
	}
	ui.Column(c.Key("plugin-"+m.ID)).Padding(10, 0).Gap(3).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, projectIcon).Size(15, 15).TextColor(pal.textMuted)
			ui.Text(c, m.Name).FontSize(13.5).FontWeight(600).TextColor(pal.text)
			ui.Text(c, "v"+m.Version).FontSize(11.5).TextColor(pal.textFaint)
			ui.Text(c, kind).FontSize(11.5).TextColor(pal.textFaint)
		})
		if m.Description != "" {
			ui.Text(c, m.Description).FontSize(12).TextColor(pal.textMuted).MaxWidth(520)
		}
		ui.Text(c, locales.PluginPermissionsLine(permissions)).FontSize(11.5).TextColor(pal.textFaint)
		for _, setting := range m.Settings {
			a.pluginSettingRow(c, pal, p, setting)
		}
	})
}

func (a *App) pluginSettingRow(c *ui.Context, pal *palette, p registry.Plugin, s protocol.Setting) {
	key := p.Manifest.ID + "." + s.Key
	label := s.Label
	if label == "" {
		label = s.Key
	}
	settingRow(c, pal, label, s.Help, func() {
		if s.Secret {
			placeholder := locales.PluginSecretEmpty()
			if a.plugins.hasSecret[p.Manifest.ID][s.Key] {
				placeholder = locales.PluginSecretSet()
			}
			in := pluginSecretField(c, "plugin-secret-"+key, a.pluginField(key, ""), placeholder, pal)
			in.OnSubmit(func() { a.savePluginSecret(p.Manifest.ID, s.Key, *a.pluginField(key, "")) })
			return
		}
		in := textField(c, "plugin-field-"+key, a.pluginField(key, a.plugins.settings[p.Manifest.ID][s.Key]), s.Help, 260, pal)
		in.OnSubmit(func() { a.savePluginSetting(p.Manifest.ID, s.Key, *a.pluginField(key, "")) })
	})
}

// pluginSecretField is a masked field for a secret. The value is written
// to the plugin's secrets file, and never sent in settings.
func pluginSecretField(c *ui.Context, key string, value *string, placeholder string, pal *palette) ui.Element {
	var in ui.Element
	frame := ui.Row(c.Key(key)).Cursor(ui.CursorText).Width(260)
	frame.Children(func() {
		in = ui.TextInputBase(c.Key("in"), value).Grow(1).MinWidth(0).FontSize(13).Password().Placeholder(placeholder).Label(placeholder)
	})
	field(c, frame, in.Focused(), pal)
	return in
}

func (a *App) savePluginSetting(plugin, key, value string) {
	h := a.pluginHost()
	value = strings.TrimSpace(value)
	go func() {
		err := h.Store().SetSettings(plugin, map[string]string{key: value})
		if err == nil {
			h.SendSettings(plugin)
		}
		a.update(func() {
			if err != nil {
				a.plugins.notice = err.Error()
			} else {
				if a.plugins.settings == nil {
					a.plugins.settings = map[string]map[string]string{}
				}
				if a.plugins.settings[plugin] == nil {
					a.plugins.settings[plugin] = map[string]string{}
				}
				a.plugins.settings[plugin][key] = value
				a.plugins.notice = locales.PluginSettingsSaved()
			}
			a.win.Invalidate()
		})
	}()
}

func (a *App) savePluginSecret(plugin, key, value string) {
	h := a.pluginHost()
	value = strings.TrimSpace(value)
	go func() {
		err := h.Store().SetSecret(plugin, key, value)
		a.update(func() {
			if err != nil {
				a.plugins.notice = err.Error()
			} else {
				if a.plugins.hasSecret == nil {
					a.plugins.hasSecret = map[string]map[string]bool{}
				}
				if a.plugins.hasSecret[plugin] == nil {
					a.plugins.hasSecret[plugin] = map[string]bool{}
				}
				if value == "" {
					delete(a.plugins.hasSecret[plugin], key)
				} else {
					a.plugins.hasSecret[plugin][key] = true
				}
				*a.pluginField(plugin+"."+key, "") = ""
				a.plugins.notice = locales.PluginSecretSaved()
			}
			a.win.Invalidate()
		})
	}()
}

// pluginSubtitle is the pane header's line for a plugin pane.
func (a *App) pluginSubtitle(p *proto.Pane) string {
	var target protocol.Target
	if p.Target != "" {
		if t, err := protocol.DecodeTarget(p.Target); err == nil {
			target = t
		}
	}
	if target.Plugin == "" {
		return ""
	}
	name := a.pluginName(target.Plugin)
	title := a.pluginWidgetTitle(target.Plugin, target.Widget)
	if title == "" || title == target.Widget {
		return fmt.Sprintf("%s · %s", name, target.Widget)
	}
	return fmt.Sprintf("%s · %s", name, title)
}
