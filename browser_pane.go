package main

import (
	"bufio"
	"bytes"
	"cmp"
	"math"
	"net"
	"net/url"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// The Browser pane is a web page, in the system's web view (WebKit on
// macOS), with its address, back, forward and reload: most often the
// project's dev server. Its pages are not the app's: they get none of
// what the system would ask the user for, go nowhere but the web, and
// call no Go (the app binds none). The page shown is the pane's Target,
// which kerod keeps; the web view keeps its page while the pane is in
// another session, and goes with the pane.

// browserState is a Browser pane's.
type browserState struct {
	wv       *mygo.WebView
	offs     []func()
	address  string // what the address field holds
	editing  bool   // the field has the keyboard: the page leaves it alone
	url      string // the page shown
	title    string
	loading  bool
	canBack  bool
	canFwd   bool
	err      string
	sent     string // the Target kerod has
	sentAt   time.Time
	servers  []devServer
	serverAt time.Time
	finding  bool
	// focusSeen is whether the session's focused pane had the keyboard
	// last frame.
	focusSeen bool
	zoom      float64 // the page's, 0 for its own size
	locate    bool    // the address takes the keyboard, all of it selected
}

// browserZooms are the page's sizes, as Safari's.
var browserZooms = []float64{0.5, 0.75, 0.85, 1, 1.15, 1.25, 1.5, 1.75, 2, 2.5, 3}

// zoomPage makes the page a size bigger (1), smaller (-1), or its own
// (0).
func (st *browserState) zoomPage(step int) {
	cur := cmp.Or(st.zoom, 1)
	next := 1.0
	switch {
	case step > 0:
		next = browserZooms[len(browserZooms)-1]
		for _, z := range browserZooms {
			if z > cur+0.001 {
				next = z
				break
			}
		}
	case step < 0:
		next = browserZooms[0]
		for _, z := range slices.Backward(browserZooms) {
			if z < cur-0.001 {
				next = z
				break
			}
		}
	}
	st.zoom = next
	if st.wv != nil {
		st.wv.Page().SetZoomFactor(next)
	}
}

// focusedBrowser is the Browser pane the user is in, nil when in none.
func (a *App) focusedBrowser(s *proto.Session) *browserState {
	if s == nil {
		return nil
	}
	p := s.PaneByID(a.focusOf(s))
	if p == nil || p.View != proto.PaneBrowser {
		return nil
	}
	return a.browserStateOf(p)
}

// browserLocation gives the address of the session's Browser pane the
// keyboard, all of it selected to type over, as Safari's Command-L: the
// pane the user is in, or another, or a new one.
func (a *App) browserLocation(s *proto.Session) {
	if s == nil {
		return
	}
	if st := a.focusedBrowser(s); st != nil {
		st.locate = true
		return
	}
	for i := range s.Panes {
		if p := &s.Panes[i]; p.View == proto.PaneBrowser {
			a.setFocus(s, p.ID)
			a.browserStateOf(p).locate = true
			return
		}
	}
	a.openView(s, "", proto.PaneBrowser, false)
}

// devServer is a server listening on this machine, as the start page
// offers it.
type devServer struct {
	port    int
	command string
}

// safariSuffix ends the pages' user agent as Safari's does.
const safariSuffix = "Version/18.5 Safari/605.1.15"

// browserSendEvery is how often at most the page shown goes to kerod.
const browserSendEvery = time.Second

func (a *App) browserStateOf(p *proto.Pane) *browserState {
	if a.browsers == nil {
		a.browsers = map[string]*browserState{}
	}
	st := a.browsers[p.ID]
	if st == nil {
		// A Target is as typed, by `kero split --browser 3000` too: a URL
		// without its scheme would be a page of the app's own.
		u := browserURL(p.Target)
		st = &browserState{address: u, url: u, sent: p.Target}
		a.browsers[p.ID] = st
	}
	return st
}

// browserURL is the address typed made a URL: a port or a local host a
// page of this machine over http, a host the web's over https, words a
// search. "" for nothing to load.
func browserURL(typed string) string {
	s := strings.TrimSpace(typed)
	if s == "" {
		return ""
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n < 65536 {
		return "http://localhost:" + s
	}
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		switch {
		case (u.Scheme == "http" || u.Scheme == "https") && u.Host != "":
			return s
		case u.Opaque != "" && !startsWithPort(u.Opaque):
			// Another scheme, as javascript: or mailto:, not a host and
			// its port: not the web's, which the pane shows.
			return ""
		}
	}
	if strings.ContainsAny(s, " \t") || !strings.ContainsAny(s, ".:") && s != "localhost" {
		return "https://duckduckgo.com/?q=" + url.QueryEscape(s)
	}
	host := s
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if isLocalHost(host) {
		return "http://" + s
	}
	if strings.Contains(s, "://") {
		// Another scheme: not the web's, which the pane shows.
		return ""
	}
	return "https://" + s
}

// startsWithPort tells what follows a host's colon: a port, then maybe a
// path.
func startsWithPort(s string) bool {
	port, _, _ := strings.Cut(s, "/")
	_, err := strconv.Atoi(port)
	return err == nil
}

func isLocalHost(host string) bool {
	switch {
	case host == "localhost", host == "0.0.0.0", strings.HasSuffix(host, ".localhost"), strings.HasSuffix(host, ".local"):
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// webOnly tells an address the pane's pages may go to: the web's, and the
// blank page.
func webOnly(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || raw == "about:blank")
}

// browserGo loads the address typed.
func (a *App) browserGo(st *browserState) {
	u := browserURL(st.address)
	if u == "" {
		return
	}
	st.address, st.url, st.err, st.editing = u, u, "", false
	if st.wv != nil {
		st.wv.Page().LoadURL(u)
		st.loading = true
		st.wv.Focus()
	}
}

// ensureWebView makes the pane's web view, once there is a page to show
// and a window to show it in.
func (a *App) ensureWebView(st *browserState) {
	if st.wv != nil || st.url == "" || a.win == nil {
		return
	}
	// A user agent naming Safari: sites serve a bare web view older
	// pages, laid out for browsers of years ago.
	wv, err := a.win.NewWebView(mygo.WebViewOptions{Page: mygo.PageOptions{UserAgentSuffix: safariSuffix}})
	if err != nil {
		st.err = err.Error()
		return
	}
	st.wv = wv
	page := wv.Page()
	if st.zoom != 0 {
		page.SetZoomFactor(st.zoom)
	}
	// Nothing the system guards (the camera, the microphone, where one
	// is): the app's own pages would get it unasked.
	page.SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
	refresh := func() {
		back, fwd := page.CanGoBack(), page.CanGoForward()
		a.update(func() { st.canBack, st.canFwd = back, fwd })
	}
	readURL := func() {
		u := page.URL()
		back, fwd := page.CanGoBack(), page.CanGoForward()
		a.update(func() {
			st.canBack, st.canFwd = back, fwd
			if u == "" || u == st.url || !webOnly(u) {
				return
			}
			st.url = u
			if !st.editing {
				st.address = u
			}
		})
	}
	st.offs = append(st.offs,
		page.OnWillNavigate(func(e *mygo.NavigateEvent) {
			if !webOnly(e.URL) {
				e.PreventDefault()
			}
		}),
		page.OnDidNavigate(func(u string) {
			a.update(func() {
				st.url, st.err, st.loading = u, "", true
				if !st.editing {
					st.address = u
				}
			})
			go refresh()
		}),
		page.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
			t := e.Title
			a.update(func() { st.title = t })
			// A page that routes itself (pushState) commits no navigation,
			// but most name their new place.
			go readURL()
		}),
		page.OnDidFinishLoad(func() {
			a.update(func() { st.loading = false })
			go refresh()
			go readURL()
		}),
		page.OnDidFailLoad(func(e *mygo.LoadError) {
			msg := e.Description
			a.update(func() { st.loading, st.err = false, msg })
		}),
	)
	if err := page.LoadURL(st.url); err != nil {
		st.err = err.Error()
	}
	st.loading = true
}

// dropBrowser ends a pane's web view.
func (a *App) dropBrowser(id string) {
	st := a.browsers[id]
	if st == nil {
		return
	}
	for _, off := range st.offs {
		off()
	}
	if st.wv != nil {
		st.wv.Destroy()
	}
	delete(a.browsers, id)
}

// sweepViews lets go of what the views of panes that closed held: their
// web views, read folders, Attention views and plugin views.
func (a *App) sweepViews() {
	if len(a.browsers) == 0 && len(a.files) == 0 && len(a.attention) == 0 && len(a.cardSize) == 0 && len(a.prefs.Files) == 0 && len(a.plugins.panes) == 0 {
		return
	}
	alive := map[string]bool{}
	for i := range a.st.Sessions {
		for _, p := range a.st.Sessions[i].Panes {
			alive[p.ID] = true
		}
	}
	// A session made but not in the state yet has its panes in none.
	if !a.loaded {
		return
	}
	for id := range a.browsers {
		if !alive[id] {
			a.dropBrowser(id)
		}
	}
	for id := range a.files {
		if !alive[id] {
			delete(a.files, id)
		}
	}
	for id := range a.attention {
		if !alive[id] {
			delete(a.attention, id)
		}
	}
	for id := range a.cardSize {
		if !alive[id] {
			delete(a.cardSize, id)
		}
	}
	// A state without sessions, as kerod starting, is no word of panes
	// closed.
	swept := false
	for id := range a.prefs.Files {
		if len(a.st.Sessions) == 0 {
			break
		}
		if !alive[id] {
			delete(a.prefs.Files, id)
			swept = true
		}
	}
	if swept {
		a.savePrefs()
	}
	// The usage popover is a plugin surface too, with no daemon pane behind
	// it: it lives while its card is open, and closes with it.
	if a.navUsage.open {
		alive[navUsagePane] = true
	}
	// A plugin's panes are closed, and one hidden now is told so.
	a.sweepPluginPanes(alive)
}

func (a *App) browserPane(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane, body ui.Element) {
	st := a.browserStateOf(p)
	a.ensureWebView(st)
	// The page shown is the Target, sent at most every second.
	if st.url != st.sent && webOnly(st.url) {
		if wait := browserSendEvery - time.Since(st.sentAt); wait > 0 {
			c.After(wait)
		} else {
			st.sent, st.sentAt = st.url, time.Now()
			a.call("pane.target", proto.TargetParams{Pane: p.ID, Target: st.url}, nil)
		}
	}
	a.browserBar(c, pal, st, p)
	switch {
	case st.url == "":
		a.browserStart(c, pal, st)
	case st.err != "" && !st.loading:
		paneNothing(c, pal, globeIcon, locales.BrowserCantOpen(), st.err)
	case st.wv == nil:
		ui.Box(c).Grow(1).AlignSelf(ui.Stretch)
	default:
		// What paints over the page and takes the pointer, as a dialog,
		// takes it there: the page shows under them.
		web := ui.WebView(c.Key("web"), st.wv).Grow(1).AlignSelf(ui.Stretch).Radius(0)
		a.pageFocus(c, st, s, p, web)
	}
}

// pageFocus focuses the pane as its page is clicked: a press on the page
// is the page's, which the card never sees, but the keyboard leaves the
// app's content for it, under the pointer.
func (a *App) pageFocus(c *ui.Context, st *browserState, s *proto.Session, p *proto.Pane, web ui.Element) {
	cur := a.focusOf(s)
	had := st.focusSeen
	st.focusSeen = cur != "" && a.paneRef(cur).Focused(c)
	if cur != p.ID && had && !st.focusSeen && web.Hovered() {
		a.setFocus(s, p.ID)
	}
}

// browserBar is the pane's back, forward and reload, and its address.
func (a *App) browserBar(c *ui.Context, pal *palette, st *browserState, p *proto.Pane) {
	ui.Row(c.Key("browser-bar")).Height(40).Shrink(0).AlignItems(ui.Center).Gap(2).Padding(0, 8).Children(func() {
		nav := func(key string, svg *ui.SVG, label string, on bool, fn func()) {
			b := ui.ButtonBase(c.Key(key)).Size(28, 26).Radius(13).Center().TextColor(pal.textMuted).Label(label).Tooltip(label).Disabled(!on)
			if !on {
				b.TextColor(pal.textFaint.Alpha(0.5))
			} else if b.Hovered() {
				b.Background(pal.rowHover).TextColor(pal.text)
			}
			tint := pal.textMuted
			if !on {
				tint = pal.textFaint.Alpha(0.5)
			} else if b.Hovered() {
				tint = pal.text
			}
			b.Children(func() { controlIcon(c, svg, 15, tint, pal.dark) })
			if b.Clicked() && on {
				fn()
			}
		}
		live := st.wv != nil
		toolbarGroup(c, "browser-navigation", pal, func() {
			nav("back", goBackIcon, locales.BrowserBack(), live && st.canBack, func() { st.wv.Page().GoBack() })
			nav("fwd", goFwdIcon, locales.BrowserForward(), live && st.canFwd, func() { st.wv.Page().GoForward() })
			// Reload, or Stop while the page loads, as Safari's: no spinner,
			// whose frames would compete with the page for the main thread.
			if st.loading && live {
				nav("stop", closeIcon, locales.BrowserStop(), true, func() { st.loading = false; st.wv.Page().Stop() })
			} else {
				nav("reload", refreshIcon, locales.BrowserReload(), live, func() { st.err = ""; st.loading = true; st.wv.Page().Reload() })
			}
		})
		field := ui.Row(c.Key("address")).Grow(1).MinWidth(0).Height(28).Radius(8).Padding(0, 10).Gap(6).AlignItems(ui.Center).
			Margin(0, 4)
		// The address has no field() outline; keep its rim visible on white.
		field.InsetShadow(0, 0, 0, 0.5, pal.hairline)
		glassToolbar(c, field, pal)
		field.Children(func() {
			controlIcon(c, globeIcon, 13, pal.textFaint, pal.dark)
			in := ui.TextInputBase(c.Key("url"), &st.address).Grow(1).MinWidth(0).FontSize(12.5).
				Placeholder(locales.BrowserPlaceholder()).Bind(a.paneRef("url-" + p.ID))
			if st.url == "" {
				in.AutoFocus()
			}
			// The address taking the keyboard is all selected, to type
			// over, as Safari's.
			if st.locate {
				st.locate = false
				a.paneRef("url-" + p.ID).Focus()
				in.SetTextSelection(0, utf8.RuneCountInString(st.address))
			} else if in.Focused() && !st.editing {
				in.SetTextSelection(0, utf8.RuneCountInString(st.address))
			}
			st.editing = in.Focused()
			if in.Submitted() {
				a.browserGo(st)
			}
			if st.editing && in.Shortcut(0, ui.KeyEscape) {
				st.address = st.url
				a.paneRef(p.ID).Focus()
			}
			// A page bigger or smaller than its own says how much, and
			// a click makes it its own size.
			if z := st.zoom; z != 0 && z != 1 {
				chip := ui.ButtonBase(c.Key("zoom")).Height(20).Padding(0, 6).Radius(5).Center().Shrink(0).
					Background(pal.kbd).Label(locales.BrowserZoomReset()).Tooltip(locales.BrowserZoomReset())
				if chip.Hovered() {
					chip.Background(pal.rowHover)
				}
				if chip.Clicked() {
					st.zoomPage(0)
				}
				chip.Children(func() {
					ui.Text(c, strconv.Itoa(int(math.Round(z*100)))+"%").FontSize(11).FontFeatures("tnum").TextColor(pal.textMuted).SingleLine()
				})
			}
		})
		if st.url != "" && webOnly(st.url) {
			u := st.url
			nav("external", externalIcon, locales.BrowserOpenDefault(), true, func() { openURL(u) })
		}
	})
}

// browserStart is the pane before it shows a page: the servers running
// on this machine to pick, and what typing does.
func (a *App) browserStart(c *ui.Context, pal *palette, st *browserState) {
	a.findServers(c, st)
	ui.Column(c).Fill().Padding(28, 24).Gap(6).Children(func() {
		ui.Text(c, locales.BrowserServers()).FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).Padding(0, 10, 4, 10)
		if len(st.servers) == 0 {
			text := locales.BrowserNoServers()
			if st.finding && st.serverAt.IsZero() {
				text = locales.BrowserFinding()
			}
			ui.Text(c, text).FontSize(12.5).TextColor(pal.textFaint).Padding(0, 10)
		}
		for _, sv := range st.servers {
			addr := "localhost:" + strconv.Itoa(sv.port)
			row := ui.ButtonBase(c.Key("srv-"+addr)).Height(36).Shrink(0).Gap(10).Padding(0, 10).Radius(8).AlignItems(ui.Center).
				Label(addr).MaxWidth(420)
			if row.Hovered() {
				row.Background(pal.rowHover)
			}
			if row.Clicked() {
				st.address = addr
				a.browserGo(st)
			}
			row.Children(func() {
				controlIcon(c, globeIcon, 14, pal.textMuted, pal.dark)
				ui.Text(c, addr).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine()
				ui.Text(c, sv.command).FontSize(12.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			})
		}
		ui.Text(c, locales.BrowserHint()).FontSize(12).TextColor(pal.textFaint).Padding(14, 10, 0, 10).MaxWidth(420)
	})
}

// findServers looks, every few seconds while the start page shows, for
// servers listening on this machine.
func (a *App) findServers(c *ui.Context, st *browserState) {
	if st.finding {
		return
	}
	if wait := filesRefresh - time.Since(st.serverAt); !st.serverAt.IsZero() && wait > 0 {
		c.After(wait)
		return
	}
	st.finding = true
	a.background(func() func() {
		servers := listeningServers()
		return func() {
			st.servers, st.serverAt, st.finding = servers, time.Now(), false
		}
	})
}

// notServers are the system's own listeners, which serve no page one
// would open.
var notServers = map[string]bool{"ControlCe": true, "rapportd": true, "sharingd": true, "launchd": true, "mDNSRespo": true, "identitys": true, "kerod": true, "Shibu": true, "Kero": true}

// listeningServers are the servers listening on TCP on this machine, by
// port, as lsof tells them: nil without lsof.
func listeningServers() []devServer {
	out, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fcn").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parseListeners(out)
}

// parseListeners reads lsof's -Fcn output: a "c" line names the command
// of the process whose "n" lines, host:port, follow.
func parseListeners(out []byte) []devServer {
	seen := map[int]bool{}
	var servers []devServer
	command := ""
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		switch line[0] {
		case 'c':
			command = line[1:]
		case 'n':
			i := strings.LastIndexByte(line, ':')
			if i < 0 {
				continue
			}
			port, err := strconv.Atoi(line[i+1:])
			if err != nil || port < 1024 || seen[port] || notServers[command] {
				continue
			}
			seen[port] = true
			servers = append(servers, devServer{port: port, command: command})
		}
	}
	slices.SortFunc(servers, func(x, y devServer) int { return x.port - y.port })
	if len(servers) > 8 {
		servers = servers[:8]
	}
	return servers
}

func (a *App) browserTitle(p *proto.Pane) string {
	if st := a.browsers[p.ID]; st != nil {
		if st.title != "" {
			return st.title
		}
		if st.url != "" {
			return st.url
		}
	}
	return p.Target
}
