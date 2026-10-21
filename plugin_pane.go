package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
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
	// The other hooks a test sets, as discover: command starts a plugin
	// (nil: the registry's argv), post applies what a plugin or a read did
	// on the UI goroutine (nil: win.Update), and saveTarget writes a pane's
	// target (nil: the daemon's pane.target).
	command    func(registry.Plugin) ([]string, error)
	post       func(func())
	saveTarget func(pane, target string)

	panes map[string]*pluginPaneState
	// pending are the commands of each plugin that wait for a pane of it
	// to open: the palette or `kero do` ran them while it had none.
	pending map[string][]string

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
		a.pluginPost(func() { a.applyPlugins(plugins) })
	}()
}

// pluginPost runs fn on the UI goroutine: what a plugin's process or a read
// off the UI goroutine found.
func (a *App) pluginPost(fn func()) {
	if a.plugins.post != nil {
		a.plugins.post(fn)
		return
	}
	a.update(fn)
}

// applyPlugins caches the plugins the discovery found. It runs on the UI
// goroutine, through win.Update.
func (a *App) applyPlugins(plugins []registry.Plugin) {
	a.plugins.known = plugins
	a.plugins.knownAt = true
	a.plugins.discovering = false
	a.invalidateMain()
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
		Command: a.plugins.command,
		Callbacks: host.Callbacks{
			Ready: func(plugin string) {
				a.pluginPost(func() { a.pluginReady(plugin) })
			},
			Render: func(plugin, pane string, tree *protocol.Tree) {
				a.pluginPost(func() { a.pluginRendered(plugin, pane, tree) })
			},
			State: func(plugin, pane string, state json.RawMessage) {
				a.pluginPost(func() { a.pluginStateSaved(plugin, pane, state) })
			},
			Notify: func(plugin, title, body string) {
				a.pluginPost(func() { a.showPluginNotification(plugin, title, body) })
			},
			OpenURL: func(plugin, url string) {
				a.pluginPost(func() { openPath(url) })
			},
			Crash: func(plugin, message, logPath string) {
				a.pluginPost(func() { a.pluginCrashed(plugin, message, logPath) })
			},
		},
	})
	return a.plugins.host
}

// pluginReady is a plugin that answered the handshake, after a start or a
// restart: its panes open again on the new process.
func (a *App) pluginReady(plugin string) {
	for _, st := range a.plugins.panes {
		if st.plugin == plugin {
			st.opened = false
		}
	}
	a.invalidateMain()
}

// pluginRendered is a tree a plugin drew for one of its panes.
func (a *App) pluginRendered(plugin, pane string, tree *protocol.Tree) {
	st := a.plugins.panes[pane]
	if st == nil || st.plugin != plugin {
		return
	}
	st.tree = tree
	delete(a.plugins.crashed, plugin)
	a.invalidateMain()
}

// pluginCrashed shows why a plugin stopped in each of its panes. The panes
// are neither open nor shown on a process now, so a restart tells the new
// one both again.
func (a *App) pluginCrashed(plugin, message, logPath string) {
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
			st.visible = false
			st.tree = nil
		}
	}
	a.invalidateMain()
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
	if a.plugins.saveTarget != nil {
		a.plugins.saveTarget(pane, target)
		return
	}
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

	size := a.pluginPaneSize(p.ID)
	h := a.pluginHost()
	if !st.opened {
		a.openOnHost(p.ID, st, size)
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

// pluginPaneSize is the size the app gives a plugin pane: its card's, at
// least one point.
func (a *App) pluginPaneSize(pane string) protocol.Size {
	return protocol.Size{Width: max(int(a.cardSize[pane].W), 1), Height: max(int(a.cardSize[pane].H), 1)}
}

// openOnHost opens a pane on its plugin's process, which starts when it is
// not running, and hands it the commands that waited for a pane of the
// plugin. The host keeps their order: the open goes first.
func (a *App) openOnHost(pane string, st *pluginPaneState, size protocol.Size) {
	h := a.pluginHost()
	if err := h.Open(st.plugin, host.Pane{ID: pane, Widget: st.widget, State: st.state, Size: size}); err != nil {
		a.toast(err.Error())
		return
	}
	st.opened = true
	for _, id := range a.plugins.pending[st.plugin] {
		h.Command(pane, id)
	}
	delete(a.plugins.pending, st.plugin)
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
			// The new process has not heard that the pane shows: the next
			// frame tells it, after the open.
			st.visible = false
			st.tree = nil
		}
	}
	a.invalidateMain()
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

// --- Commands ---

// pluginCommands is every command of every plugin the app knows, in the
// order the palette shows them.
func (a *App) pluginCommands() []pluginCommand {
	var cmds []pluginCommand
	for _, p := range a.knownPlugins() {
		for _, c := range p.Manifest.Commands {
			cmds = append(cmds, pluginCommand{plugin: p.Manifest.ID, name: p.Manifest.Name, id: c.ID, title: c.Title})
		}
	}
	return cmds
}

// pluginCommand is one command of a plugin, as the palette offers it.
type pluginCommand struct {
	plugin, name, id, title string
}

// pluginPaneOf finds a pane of a plugin: the focused pane of the session
// shown, another pane of it, then a pane of another session. It is nil
// when no session has one.
func (a *App) pluginPaneOf(plugin string) (*proto.Session, *proto.Pane) {
	is := func(p *proto.Pane) bool {
		if p.View != proto.PanePlugins || p.Target == "" {
			return false
		}
		t, err := protocol.DecodeTarget(p.Target)
		return err == nil && t.Plugin == plugin
	}
	if s := a.session(a.selected); s != nil {
		if p := s.PaneByID(a.focusOf(s)); p != nil && is(p) {
			return s, p
		}
		for i := range s.Panes {
			if is(&s.Panes[i]) {
				return s, &s.Panes[i]
			}
		}
	}
	for i := range a.st.Sessions {
		s := &a.st.Sessions[i]
		for j := range s.Panes {
			if is(&s.Panes[j]) {
				return s, &s.Panes[j]
			}
		}
	}
	return nil, nil
}

// runPluginCommand runs one of a plugin's commands, from the palette or
// `kero do`. The plugin hears it on a pane of its own (see pluginPaneOf),
// which opens on its process if it has not yet: a pane of a session not
// shown since the app started. With no pane of the plugin anywhere, the
// command opens one in the session shown, and waits for it to open.
func (a *App) runPluginCommand(plugin, command string) {
	p, ok := a.pluginByID(plugin)
	if !ok {
		a.toast(locales.PluginUnknownDetail(plugin))
		return
	}
	known := false
	for _, c := range p.Manifest.Commands {
		known = known || c.ID == command
	}
	if !known {
		a.toast(locales.PluginNoCommand(p.Manifest.Name, command))
		return
	}
	// A plugin that crashed waits for its Restart button: a command does
	// not start it behind the user's back.
	if _, crashed := a.plugins.crashed[plugin]; crashed {
		a.toast(locales.PluginCrashTitle(p.Manifest.Name))
		return
	}
	_, pane := a.pluginPaneOf(plugin)
	if pane == nil {
		s := a.session(a.selected)
		if s == nil {
			a.toast(locales.PluginCommandNoSession(p.Manifest.Name))
			return
		}
		if a.plugins.pending == nil {
			a.plugins.pending = map[string][]string{}
		}
		a.plugins.pending[plugin] = append(a.plugins.pending[plugin], command)
		a.openPluginPane(s, "", plugin, "", false)
		return
	}
	st := a.pluginPaneState(pane)
	if st.widget == "" {
		st.widget = a.firstWidget(plugin)
	}
	if !st.opened {
		a.openOnHost(pane.ID, st, a.pluginPaneSize(pane.ID))
		if !st.opened {
			return
		}
	}
	if err := a.pluginHost().Command(pane.ID, command); err != nil {
		a.toast(locales.PluginCommandFailed())
	}
}

// showPluginNotification shows a notification a plugin asked for. A click
// on it shows a pane of the plugin, found when the user clicks: the pane
// may have moved, or closed, since.
func (a *App) showPluginNotification(plugin, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	if title == "" {
		title = a.pluginName(plugin)
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: "plugin/" + plugin, Group: "plugin/" + plugin, Title: title, Body: body, Silent: !a.set.NotifySound})
	n.OnClick(func() {
		a.update(func() {
			a.focusPluginPane(plugin)
			a.win.Show()
			a.win.Focus()
		})
		n.Close()
	})
	go n.Show()
}

// focusPluginPane shows a pane of a plugin, as a click on its notification
// does. A plugin with no pane leaves the selection as it is.
func (a *App) focusPluginPane(plugin string) {
	s, p := a.pluginPaneOf(plugin)
	if s == nil {
		return
	}
	a.selectSession(s.ID)
	if s = a.session(s.ID); s != nil {
		a.setFocus(s, p.ID)
	}
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
		for _, p := range plugins {
			id := p.Manifest.ID
			settings[id] = h.Store().Settings(id)
			secrets := map[string]bool{}
			for _, setting := range p.Manifest.Settings {
				if setting.Secret && h.Store().HasSecret(id, setting.Key) {
					secrets[setting.Key] = true
				}
			}
			hasSecret[id] = secrets
		}
		a.pluginPost(func() {
			a.plugins.settings = settings
			a.plugins.hasSecret = hasSecret
			a.plugins.loading = false
			a.plugins.pageLoaded = true
			a.invalidateMain()
		})
	}()
}

// pluginField is the text field of one plugin setting, kept between
// frames. It starts with the saved value, so it is made only once the page
// has read them: a field made before would start empty, and Return on it
// would save the empty value over the saved one.
func (a *App) pluginField(key, value string) *string {
	if !a.plugins.pageLoaded {
		return new(string)
	}
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
	// Every field comes from a manifest's settings: the page knows no
	// plugin by name.
	settingsGroup(c, pal, locales.PluginSettingsGroup(), locales.PluginSettingsGroupDetail(), func() {
		if len(plugins) == 0 {
			settingRow(c, pal, locales.PluginNone(), locales.PluginNoneDetail(), func() {})
		}
		for _, p := range plugins {
			a.pluginCard(c, pal, p)
		}
	})
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
		// The fields wait for the saved values, read off the UI goroutine.
		if !a.plugins.pageLoaded {
			return
		}
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
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if s.Secret {
				placeholder := locales.PluginSecretEmpty()
				if a.plugins.hasSecret[p.Manifest.ID][s.Key] {
					placeholder = locales.PluginSecretSet()
				}
				in := pluginSecretField(c, "plugin-secret-"+key, a.pluginField(key, ""), placeholder, pal)
				in.OnSubmit(func() { a.savePluginSecret(p.Manifest.ID, s.Key, *a.pluginField(key, "")) })
			} else {
				in := textField(c, "plugin-field-"+key, a.pluginField(key, a.plugins.settings[p.Manifest.ID][s.Key]), locales.PluginSettingEmpty(), 260, pal)
				in.OnSubmit(func() { a.savePluginSetting(p.Manifest.ID, s.Key, *a.pluginField(key, "")) })
			}
			// A field's link is where its value comes from, as a
			// developer dashboard: a web page, never a file.
			if s.Link != "" && webOnly(s.Link) {
				if button(c, "plugin-link-"+key, locales.PluginSettingOpenLink(), "", false, pal) {
					openPath(s.Link)
				}
			}
		})
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
		a.pluginPost(func() {
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
			a.invalidateMain()
		})
	}()
}

func (a *App) savePluginSecret(plugin, key, value string) {
	h := a.pluginHost()
	value = strings.TrimSpace(value)
	go func() {
		err := h.Store().SetSecret(plugin, key, value)
		a.pluginPost(func() {
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
			a.invalidateMain()
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
