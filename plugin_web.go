package main

import (
	"github.com/egoist/mygo"
)

// A plugin with the web permission runs a page of its own in a web view
// with no window: an engine, such as the one that plays Spotify's audio. The
// host checked the address, http on a loopback address, and the web view
// belongs to the plugin's process, not to a pane: it plays on while the pane
// is hidden, and ends with the process, or at the plugin's web.close.

// pluginWeb is one of those web views. A test replaces how the app makes
// it.
type pluginWeb struct {
	load    func(url string)
	destroy func()
}

// pluginWebKey names a plugin's web view.
func pluginWebKey(plugin, id string) string { return plugin + "/" + id }

// pluginWebOpen runs a plugin's page, or loads the page again when the id
// runs one. It runs on the UI goroutine.
func (a *App) pluginWebOpen(plugin, id, url string) {
	key := pluginWebKey(plugin, id)
	if w := a.plugins.webs[key]; w != nil {
		w.load(url)
		return
	}
	create := a.plugins.newWeb
	if create == nil {
		create = a.newPluginWeb
	}
	w, err := create(url)
	if err != nil {
		a.plugins.notice = err.Error()
		return
	}
	if a.plugins.webs == nil {
		a.plugins.webs = map[string]*pluginWeb{}
	}
	a.plugins.webs[key] = w
}

// pluginWebClose ends a plugin's web view, if it runs. It runs on the UI
// goroutine.
func (a *App) pluginWebClose(plugin, id string) {
	key := pluginWebKey(plugin, id)
	if w := a.plugins.webs[key]; w != nil {
		w.destroy()
		delete(a.plugins.webs, key)
	}
}

// newPluginWeb makes the web view of a plugin's page. Like the Browser
// pane's, its user agent names Safari and it gets nothing the system
// guards; it also opens no window of its own.
func (a *App) newPluginWeb(url string) (*pluginWeb, error) {
	// The page is the plugin's own, on a loopback address, and plays with
	// no window to click: a player's sound must not wait for a gesture.
	// No one sees the page either, and WebKit would throttle and then
	// suspend it, which stops its timers and requests while the music is
	// paused: it runs as a page that is shown.
	wv, err := a.win.NewWebView(mygo.WebViewOptions{Page: mygo.PageOptions{UserAgentSuffix: safariSuffix, AllowAutoplay: true, RunWhenHidden: true}})
	if err != nil {
		return nil, err
	}
	page := wv.Page()
	page.SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
	page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
	page.OnWillNavigate(func(e *mygo.NavigateEvent) {
		if !webOnly(e.URL) {
			e.PreventDefault()
		}
	})
	if err := page.LoadURL(url); err != nil {
		wv.Destroy()
		return nil, err
	}
	return &pluginWeb{
		load:    func(url string) { _ = page.LoadURL(url) },
		destroy: wv.Destroy,
	}, nil
}

// pluginWebCallbacks are the host's calls for a plugin's web view.
func (a *App) pluginWebCallbacks() (open func(plugin, id, url string), end func(plugin, id string)) {
	return func(plugin, id, url string) {
			a.pluginPost(func() { a.pluginWebOpen(plugin, id, url) })
		}, func(plugin, id string) {
			a.pluginPost(func() { a.pluginWebClose(plugin, id) })
		}
}
