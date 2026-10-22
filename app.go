package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"kero/internal/actions"
	"kero/internal/client"
	"kero/internal/proto"
	"kero/internal/settings"

	"kero/locales"
)

// connState is how the app stands with kerod.
type connState int

const (
	connConnecting connState = iota
	connReady
	connVersion // kerod of another version runs
	connFailed
)

// prefs are what the app remembers between runs; the sessions themselves
// are kerod's.
type prefs struct {
	Selected      string          `json:"selected,omitempty"`
	SidebarWidth  float32         `json:"sidebarWidth,omitempty"`
	SidebarHidden bool            `json:"sidebarHidden,omitempty"`
	Collapsed     map[string]bool `json:"collapsed,omitempty"`
	FontSize      float32         `json:"fontSize,omitempty"`
	// Show is what the main card showed last: a session's panes, or
	// the Linear workspace.
	Show string `json:"show,omitempty"`
	// Linear is the Linear view's own state: what it showed, and the
	// filters it showed it with.
	Linear linearPrefs `json:"linear,omitempty"`
	// Files are the folders Files panes unfolded, relative to theirs, by
	// pane.
	Files map[string][]string `json:"files,omitempty"`
}

// linearPrefs is what the Linear view keeps between runs.
type linearPrefs struct {
	View      string `json:"view,omitempty"`
	Project   string `json:"project,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	State     string `json:"state,omitempty"`
	// DetailWidth is the width of the issue's card, as the divider was
	// dragged to; 0 for the default.
	DetailWidth float32 `json:"detailWidth,omitempty"`
}

const (
	defaultSidebar = 268
	minSidebar     = 200
	maxSidebar     = 440
	defaultFont    = 13
	maxTerms       = 8 // terminals kept attached, most recently shown
)

// termEntry is a session's terminal in the app: attached to kerod, which
// streams the session to it.
type termEntry struct {
	term    *terminal.Terminal
	stream  *client.Stream
	err     string
	started time.Time
}

// App is the app's state. It lives on the main thread: other goroutines
// change it through Window.Update.
type App struct {
	win *mygo.Window

	cl        *client.Client
	conn      connState
	connErr   string
	daemonApp string // the version of Kero that kerod runs
	retries   int
	st        proto.State
	loaded    bool // a state came

	prefs       prefs
	prefsOut    *writeBack[prefs]             // writes the prefs, off the main thread
	set         settings.Settings             // the user's settings: Settings' controls edit it
	setOut      *writeBack[settings.Settings] // writes the settings, off the main thread
	applied     settings.Settings             // set, as last applied and saved
	ss          settingsState
	selected    string
	terms       map[string]*termEntry
	lru         []string
	refs        map[string]*ui.Handle // panes' terminals, and splits, by id
	focus       map[string]string     // the pane the user is in, by session
	layoutDirty string                // a session whose divider is dragged
	viewSent    *string

	active  bool
	focused bool
	// watch turns the sessions' attention into notifications
	// (attention_notify.go), and deliver shows one: a test's stub, else
	// the system's notification.
	watch   *attentionWatch
	deliver func(id, title, body string)

	// windowBar is the title bar as it last was in the window, which
	// titleBar keeps in full screen, where the traffic lights are the
	// window's still (followFullScreen).
	windowBar ui.TitleBar

	renaming        string
	renamingProject string
	renameBuf       string

	renameFrames int
	chrome       chrome // where the sidebar's animation puts things

	// show is what the main card shows: a session's panes, or the
	// Linear workspace.
	show string
	// lin is the Linear view's own state: the filters, the issue open
	// beside the board, and what was read of it.
	// lin is the Linear view being built: the root's (linRoot), or a
	// Tasks pane's while that pane builds (withLin).
	lin     *linearState
	linRoot linearState
	// tasks are the Tasks panes' own views, by pane.
	tasks map[string]*linearState
	// files are the Files panes' states, by pane.
	files map[string]*filesState
	// attention are the Attention panes' states, by pane: the filter,
	// the search and the scroll of each.
	attention map[string]*attentionView
	// browsers are the Browser panes' states, with their web views.
	browsers map[string]*browserState
	// cardSize is how big each pane drew last.
	cardSize map[string]ui.Rect

	pal          paletteState
	chooser      chooserState
	find         findState
	confirm      *confirmation
	sideDrag     float32 // the width as a drag of the sidebar's edge began
	pendingToast string
	// hoverSeen and hoverUntil keep hover chrome for a moment after the
	// pointer leaves it, so moving diagonally does not flicker it.
	hoverSeen  map[string]bool
	hoverUntil map[string]time.Time
	// later are actions that change what the view is building, run once
	// it is built.
	later []func()

	// Panes dragged: the zone last under the pointer, the pane whose
	// drop preview shows and since when, and the panes just moved, which
	// spring in.
	lastZone     dropZone
	previewKey   string
	previewSince time.Time
	previewShown bool
	arrived      map[string]bool
	copyText     string
	usage        usageState
	resources    resourceState
	maintenance  maintenanceState
	peek         peek     // the image a path Command hovers shows
	peekLook     peekLook // the link that hover resolved, and what it found
	intro        introState

	// growing are the panes just made, which grow from their edge;
	// closing those giving their room back before they go. opening holds
	// a new pane's first frame, which shows it small, and its second,
	// which animates it in.
	growing map[string]bool
	closing map[string]bool
	opening map[string]int
	// sizeHold is until when the terminals' sizes wait, as the panes
	// glide (gated).
	sizeHold atomic.Int64
}

// confirmation is a question the app asks before something it cannot undo.
type confirmation struct {
	open    bool
	title   string
	detail  string
	action  string
	destroy bool
	run     func()
}

func newApp() *App {
	a := &App{terms: map[string]*termEntry{}, active: true, focused: true,
		refs: map[string]*ui.Handle{}, focus: map[string]string{}, arrived: map[string]bool{},
		hoverSeen: map[string]bool{}, hoverUntil: map[string]time.Time{}, opening: map[string]int{}}
	// The writers name their files now: they write on another thread, by
	// which time the environment may say another, as a test's does.
	prefsFile := prefsPath()
	a.prefsOut = newWriteBack(writeDelay, func(p prefs) error { return writePrefs(prefsFile, p) })
	conf := settings.Path()
	a.setOut = newWriteBack(writeDelay, func(s settings.Settings) error {
		if err := settings.SaveAt(conf, s); err != nil {
			a.update(func() { a.toast(locales.ToastSaveSettingsFailed(err.Error())) })
			return err
		}
		return nil
	})
	a.watch = newAttentionWatch()
	a.deliver = a.showNotification
	a.lin = &a.linRoot
	a.loadPrefs()
	a.set = settings.Load()
	a.setOut.Seed(a.set.Clone())
	a.ss.problems = settings.Problems()
	a.applied = a.set.Clone()
	a.selected = a.prefs.Selected
	a.show = a.prefs.Show
	if a.show == "" {
		a.show = "session"
	}
	a.lin.view = a.prefs.Linear.View
	if a.lin.view == "" {
		a.lin.view = "board"
	}
	a.lin.project = a.prefs.Linear.Project
	a.lin.milestone = a.prefs.Linear.Milestone
	a.lin.state = a.prefs.Linear.State
	a.lin.detailW = a.prefs.Linear.DetailWidth
	return a
}

func prefsPath() string { return filepath.Join(proto.Dir(), "app.json") }

func (a *App) loadPrefs() {
	b, err := os.ReadFile(prefsPath())
	read := err == nil
	if read {
		json.Unmarshal(b, &a.prefs)
	}
	if os.IsNotExist(err) {
		a.intro.first = true
	}
	if a.prefs.SidebarWidth == 0 {
		a.prefs.SidebarWidth = defaultSidebar
	}
	a.prefs.SidebarWidth = min(max(a.prefs.SidebarWidth, minSidebar), maxSidebar)
	if a.prefs.FontSize == 0 {
		a.prefs.FontSize = defaultFont
	}
	if a.prefs.Collapsed == nil {
		a.prefs.Collapsed = map[string]bool{}
	}
	if read {
		// The file holds these: a save of them writes nothing, as a
		// session switch that changes nothing makes.
		a.prefsOut.Seed(clonePrefs(a.prefs))
	}
}

// savePrefs remembers the prefs between runs. It copies them here, and
// leaves the write to the background (writeBack), so a session switch or a
// drag of the sidebar does not wait for the disk.
func (a *App) savePrefs() { a.prefsOut.Set(a.snapshotPrefs()) }

// flushPrefs writes a prefs change that waits, as quitting does.
func (a *App) flushPrefs() { a.prefsOut.Flush() }

// snapshotPrefs is the prefs as they now are, with maps of their own: the
// writer takes it to another thread while the app changes its own.
func (a *App) snapshotPrefs() prefs {
	a.prefs.Selected = a.selected
	a.prefs.Show = a.show
	a.prefs.Linear = a.linearPrefs()
	return clonePrefs(a.prefs)
}

// clonePrefs is p with maps of its own.
func clonePrefs(p prefs) prefs {
	p.Collapsed = maps.Clone(p.Collapsed)
	if p.Files != nil {
		files := make(map[string][]string, len(p.Files))
		for id, dirs := range p.Files {
			files[id] = slices.Clone(dirs)
		}
		p.Files = files
	}
	return p
}

// writePrefs writes the prefs as JSON: a temp file renamed over app.json, as
// the file always was.
func writePrefs(path string, p prefs) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// update runs fn on the main thread, and draws.
func (a *App) update(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
	}
}

// connect connects to kerod, starting it, and follows its state.
func (a *App) connect() {
	go func() {
		cl, err := client.Connect(false)
		a.update(func() {
			if err != nil {
				a.cl = nil
				if errors.Is(err, client.ErrVersion) {
					a.conn = connVersion
					return
				}
				a.conn, a.connErr = connFailed, err.Error()
				a.retries++
				if a.retries < 4 {
					// It may be starting, or restarting: try again.
					time.AfterFunc(time.Duration(a.retries)*400*time.Millisecond, func() { a.update(a.connect) })
				}
				return
			}
			a.cl, a.conn, a.connErr, a.retries = cl, connReady, "", 0
			a.daemonApp = cl.Welcome.App
			cl.OnEvent(func(m proto.Message) { a.update(func() { a.event(cl, m) }) })
			cl.Go("subscribe", nil, nil)
			a.viewSent = nil
			a.reportView()
			go func() {
				<-cl.Done()
				a.update(func() { a.disconnected(cl) })
			}()
		})
	}()
}

// disconnected follows kerod going away, as when it was restarted: the
// terminals attached to it are done, and the app connects again.
func (a *App) disconnected(cl *client.Client) {
	if a.cl != cl {
		return
	}
	a.cl = nil
	a.conn = connConnecting
	for id := range a.terms {
		a.dropTerm(id)
	}
	time.AfterFunc(300*time.Millisecond, func() { a.update(a.connect) })
}

func (a *App) event(cl *client.Client, m proto.Message) {
	if a.cl != cl {
		return
	}
	switch m.Event {
	case "state":
		if m.State != nil {
			a.setState(*m.State)
		}
	case "select":
		if !a.set.RemoteControl {
			return
		}
		a.selectSession(m.Select)
		if a.win != nil {
			a.win.Show()
			a.win.Focus()
			mygo.App.Focus()
		}
	case "action":
		if !a.set.RemoteControl {
			return
		}
		if act, ok := actions.Find(m.Action); ok && act.Remote {
			a.run(act.ID, m.Args)
		}
	case "notice":
		// A program's own notification, as a terminal shows it: not
		// while the user looks at its session.
		n := m.Notice
		s := a.session(n.Session)
		if s == nil || n.Session == a.selected && a.active {
			return
		}
		title := n.Title
		if title == "" {
			title = s.DisplayName(a.set.SessionName)
		}
		a.showNotification(n.Session, title, n.Body)
	}
}

func (a *App) setState(st proto.State) {
	first := !a.loaded
	a.st, a.loaded = st, true
	// Terminals of panes gone go too.
	live := map[string]bool{}
	for _, s := range a.st.Sessions {
		for _, p := range s.Panes {
			live[p.ID] = true
		}
	}
	for id := range a.terms {
		if !live[id] {
			a.dropTerm(id)
		}
	}
	a.reconcileSelection(first)
	a.notify(first)
	a.updateTitle()
}

// reconcileSelection keeps a background state update from navigating away
// from the Linear workspace.
func (a *App) reconcileSelection(first bool) {
	if a.show != "linear" && a.session(a.selected) == nil {
		ids := a.visibleOrder()
		if len(ids) == 0 {
			a.selected = ""
		} else {
			a.selectSession(ids[0])
		}
	} else if first && a.show != "linear" {
		a.selectSession(a.selected)
	}
}

// showNotification shows a notification about a session, which a click
// on it selects.
func (a *App) showNotification(id, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: "session/" + id, Group: id, Title: title, Body: body, Silent: !a.set.NotifySound})
	n.OnClick(func() {
		a.update(func() {
			a.selectSession(id)
			a.win.Show()
			a.win.Focus()
		})
		n.Close()
	})
	go n.Show()
}

// notify tells about the sessions that came to need the user, or ended
// unseen, and badges the Dock with how many wait. The transitions are the
// daemon's attention fold (attention_notify.go): every session the daemon
// holds is watched, so a session in a profile the user is not looking at
// still tells.
func (a *App) notify(first bool) {
	waiting := 0
	for i := range a.st.Sessions {
		if foldOf(&a.st.Sessions[i]).State == attentionWaitingUser {
			waiting++
		}
	}
	for _, e := range a.watch.watch(a.st.Sessions, first, a.silencedSet(), a.notifyWindow()) {
		a.tell(e)
	}
	if !a.set.DockBadge {
		waiting = 0
	}
	// No window, as a test's app, is no Dock to badge either: the badge
	// waits on the main loop, which a test has not.
	if a.win != nil {
		mygo.App.SetBadgeCount(waiting)
	}
}

// tell shows one session's notification, if the user asked for its kind
// and is not looking at it. Its title and body say the session and one
// line of what it needs, as the fold worded it: no prompt text beyond what
// the hooks sent, and never a credential.
func (a *App) tell(e attentionEvent) {
	s := a.session(e.Session)
	if s == nil {
		return
	}
	// The user is in it and the app is in front: it needs no telling.
	// Focus is the only filter, never a profile or a hidden window.
	if s.ID == a.selected && a.active {
		return
	}
	name := s.DisplayName(a.set.SessionName)
	var title, body string
	switch {
	case e.State == attentionWaitingUser && a.set.NotifyWaiting:
		title, body = locales.NotifyNeedsYouTitle(name), e.Msg
		if body == "" {
			body = locales.NotifyWaitingBody()
		}
	case e.State == attentionUnread && e.Failed && a.set.NotifyFailed:
		title, body = locales.NotifyFailedTitle(name), e.Msg
		if body == "" {
			body = locales.NotifyFailedBody(s.PaneName())
		}
	case e.State == attentionUnread && !e.Failed && a.set.NotifyDone:
		title, body = locales.NotifyDoneTitle(name), e.Msg
		if body == "" {
			body = locales.NotifyDoneBody(s.Agent, proto.ShortDir(s.WorkDir()))
		}
	default:
		return
	}
	if a.set.BounceDock && (e.State == attentionWaitingUser || e.Failed) {
		mygo.App.Dock.Bounce(false)
	}
	a.deliver(s.ID, title, body)
}

// silencedSet is the sessions whose notifications the user turned off.
func (a *App) silencedSet() map[string]bool {
	if len(a.set.SilencedSessions) == 0 {
		return nil
	}
	m := make(map[string]bool, len(a.set.SilencedSessions))
	for _, id := range a.set.SilencedSessions {
		m[id] = true
	}
	return m
}

// silenced tells whether the user turned off a session's notifications.
func (a *App) silenced(id string) bool {
	return slices.Contains(a.set.SilencedSessions, id)
}

// notifyWindow is how long a session's same state stays quiet after it
// told, as the user set it: 0 for no throttle.
func (a *App) notifyWindow() time.Duration {
	return time.Duration(a.set.NotifyThrottle) * time.Minute
}

// silenceTitle is the mute toggle's row, as the session's state makes it:
// silencing, or resuming.
func (a *App) silenceTitle(id string) string {
	if a.silenced(id) {
		return locales.SessionResumeNotifications()
	}
	return locales.SessionSilenceNotifications()
}

// toggleSilence silences a session's notifications, or resumes them. The
// choice is saved with the settings, so a silenced session stays quiet
// until the user says otherwise, over a restart of the app too.
func (a *App) toggleSilence(id string) {
	s := a.session(id)
	if s == nil {
		return
	}
	next := slices.Clone(a.set.SilencedSessions)
	i := slices.Index(next, id)
	off := i < 0
	if off {
		next = append(next, id)
	} else {
		next = slices.Delete(next, i, i+1)
	}
	a.set.SilencedSessions = next
	a.applySettings(a.set)
	name := s.DisplayName(a.set.SessionName)
	if off {
		a.toast(locales.ToastSessionSilenced(name))
	} else {
		a.toast(locales.ToastSessionResumed(name))
	}
}

func (a *App) updateTitle() {
	if a.win == nil {
		return
	}
	title := mygo.App.Name()
	if s := a.session(a.selected); s != nil {
		title = s.DisplayName(a.set.SessionName)
	}
	a.win.SetTitle(title)
}

func (a *App) session(id string) *proto.Session {
	if id == "" {
		return nil
	}
	for i := range a.st.Sessions {
		if a.st.Sessions[i].ID == id {
			return &a.st.Sessions[i]
		}
	}
	return nil
}

func (a *App) project(id string) *proto.Project {
	for i := range a.st.Projects {
		if a.st.Projects[i].ID == id {
			return &a.st.Projects[i]
		}
	}
	return nil
}

// group is a project with its sessions, as the sidebar lists them.
type group struct {
	project  proto.Project
	sessions []*proto.Session
}

func (a *App) groups() []group {
	var gs []group
	known := map[string]bool{}
	for _, p := range a.st.Projects {
		known[p.ID] = true
		g := group{project: p}
		for i := range a.st.Sessions {
			if a.st.Sessions[i].Project == p.ID {
				g.sessions = append(g.sessions, &a.st.Sessions[i])
			}
		}
		gs = append(gs, g)
	}
	other := group{project: proto.Project{ID: "", Name: locales.SidebarOtherProject()}}
	for i := range a.st.Sessions {
		if !known[a.st.Sessions[i].Project] {
			other.sessions = append(other.sessions, &a.st.Sessions[i])
		}
	}
	if len(other.sessions) > 0 {
		gs = append(gs, other)
	}
	return gs
}

// visibleOrder returns the sessions in the sidebar's order, those of
// collapsed projects left out unless selected.
func (a *App) visibleOrder() []string {
	var ids []string
	for _, g := range a.groups() {
		for _, s := range g.sessions {
			if !a.prefs.Collapsed[g.project.ID] || s.ID == a.selected {
				ids = append(ids, s.ID)
			}
		}
	}
	return ids
}

func (a *App) selectSession(id string) {
	if a.session(id) == nil {
		return
	}
	changed := a.selected != id
	a.selected = id
	a.show = "session"
	s := a.session(id)
	for _, p := range s.Panes {
		a.ensureTerm(p.ID)
	}
	if pane := a.focusOf(s); pane != "" {
		a.touch(pane)
	}
	a.focusTerminal()
	a.reportView()
	a.updateTitle()
	if changed {
		a.savePrefs()
	}
}

// reportView tells kerod which session the user looks at, which it then
// never marks unread.
func (a *App) reportView() {
	if a.cl == nil {
		return
	}
	id := ""
	if a.active && a.focused {
		id = a.selected
	}
	if a.viewSent != nil && *a.viewSent == id {
		return
	}
	a.viewSent = &id
	a.cl.Go("view", proto.ViewParams{ID: id}, nil)
}

// touch moves a terminal to the front of those kept attached, dropping
// those shown longest ago past the limit (trimTerms).
func (a *App) touch(id string) {
	a.lru = slices.DeleteFunc(a.lru, func(x string) bool { return x == id })
	a.lru = append(a.lru, id)
	a.trimTerms()
}

func (a *App) dropTerm(id string) {
	if e := a.terms[id]; e != nil {
		if e.term != nil {
			e.term.Close()
		}
		if e.stream != nil {
			e.stream.Close()
		}
		delete(a.terms, id)
	}
	if a.find.pane == id {
		a.find.pane = ""
	}
	a.lru = slices.DeleteFunc(a.lru, func(x string) bool { return x == id })
}

// gated holds back the stream's frames until the terminal takes the size
// of the first, which then shows the session as it is; and the sizes the
// terminal asks for while the panes glide, but the last, which goes as
// they stop: a program redraws once, not at each frame of the glide.
type gated struct {
	*client.Stream
	ready chan struct{}
	hold  *atomic.Int64 // until when sizes wait, in Unix nanoseconds

	mu      sync.Mutex
	pending [4]int // the size waiting, and its cells': cols 0 none
	timer   *time.Timer
}

func (g *gated) Read(p []byte) (int, error) {
	<-g.ready
	return g.Stream.Read(p)
}

func (g *gated) ResizePixels(cols, rows, cellWidth, cellHeight int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if wait := g.wait(); wait > 0 {
		g.pending = [4]int{cols, rows, cellWidth, cellHeight}
		if g.timer == nil {
			g.timer = time.AfterFunc(wait, g.flush)
		}
		return nil
	}
	g.pending = [4]int{}
	return g.Stream.ResizePixels(cols, rows, cellWidth, cellHeight)
}

func (g *gated) wait() time.Duration {
	if g.hold == nil {
		return 0
	}
	return time.Until(time.Unix(0, g.hold.Load()))
}

// flush sends the size that waited, once the panes stopped.
func (g *gated) flush() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if wait := g.wait(); wait > 0 {
		g.timer = time.AfterFunc(wait, g.flush)
		return
	}
	g.timer = nil
	if p := g.pending; p[0] > 0 {
		g.pending = [4]int{}
		g.Stream.ResizePixels(p[0], p[1], p[2], p[3])
	}
}

// ensureTerm attaches a terminal to a session, unless one is.
func (a *App) ensureTerm(id string) {
	if a.terms[id] != nil || a.conn != connReady || a.isViewPane(id) {
		return
	}
	e := &termEntry{started: time.Now()}
	a.terms[id] = e
	a.noteTerm(id)
	go func() {
		stream, err := client.Attach(id)
		if err != nil {
			a.update(func() {
				if a.terms[id] == e {
					e.err = err.Error()
				}
			})
			return
		}
		g := &gated{Stream: stream, ready: make(chan struct{}), hold: &a.sizeHold}
		o := a.termOptions(id, g)
		// The terminal shows kerod's frames: kerod's terminal is the
		// session's, ours shows what it shows, cell for cell, and lays out
		// nothing of the program's own. It takes the sizes kerod's does,
		// between the frames of one size and the next.
		o.ResizeByConn = true
		term, err := terminal.New(o)
		if err != nil {
			stream.Close()
			a.update(func() {
				if a.terms[id] == e {
					e.err = err.Error()
				}
			})
			return
		}
		term.Resize(stream.Welcome.Cols, stream.Welcome.Rows)
		stream.OnResized = term.Resize
		close(g.ready)
		a.update(func() {
			if a.terms[id] != e {
				term.Close()
				stream.Close()
				return
			}
			e.term, e.stream = term, stream
			if a.selected == id {
				a.focusTerminal()
			}
		})
	}()
}

func (a *App) termFont() terminal.Font {
	f := terminal.Font{Family: a.termFamily(), Size: a.set.FontSize, Weight: a.set.FontWeight,
		LineHeight: a.set.LineHeight, Thicken: a.set.Thicken}
	if !a.set.Ligatures {
		f.Features = []string{"-calt", "-liga", "-dlig"}
	}
	return f
}

// termThemes returns the terminal's colors in light and dark windows:
// kero's, on the card's color with the accent's selection, or a Ghostty
// theme of the user's choice.
func (a *App) termThemes() (light, dark *terminal.Theme) {
	pick := func(name string, def *terminal.Theme) *terminal.Theme {
		if name == "" || name == "kero" {
			return def
		}
		if t, err := terminal.GhosttyTheme(name); err == nil {
			return t
		}
		return def
	}
	l, d := a.accentPal(false), a.accentPal(true)
	s := accentSpecs[a.accentID()]
	return pick(a.set.LightTheme, termTheme(l, s.lightSel)), pick(a.set.DarkTheme, termTheme(d, s.darkSel))
}

// accentID is the accent the app shows, the default's where none is set,
// as a new install's and the tests' app's.
func (a *App) accentID() string {
	if a.set.Accent == "" {
		return settings.AccentDefault
	}
	return a.set.Accent
}

func (a *App) termOptions(id string, conn *gated) terminal.Options {
	light, dark := a.termThemes()
	o := terminal.Options{
		Conn:          conn,
		Scrollback:    a.set.ScrollbackBytes(),
		Font:          a.termFont(),
		Theme:         light,
		DarkTheme:     dark,
		Cursor:        map[string]terminal.CursorStyle{"block": terminal.CursorBlock, "bar": terminal.CursorBar, "underline": terminal.CursorUnderline}[a.set.Cursor],
		NoBlink:       !a.set.CursorBlink,
		OptionAsAlt:   a.set.OptionAsAlt,
		ClipboardRead: map[string]terminal.ClipboardAccess{"ask": terminal.ClipboardAsk, "allow": terminal.ClipboardAllow, "deny": terminal.ClipboardDeny}[a.set.Clipboard],
		RightClick:    map[string]terminal.RightClickAction{"menu": terminal.RightClickContextMenu, "paste": terminal.RightClickPaste, "copyOrPaste": terminal.RightClickCopyOrPaste}[a.set.RightClick],
		// The pane's card shows under it, dimmed when the user is in
		// another pane.
		Transparent: true,
		// Text lines up with the pane's title; a full-screen program's
		// colors reach the card's edges.
		Padding:      terminal.Padding{X: 16, Y: 6},
		PaddingColor: terminal.PaddingExtend,
		// Text a program colored for the other appearance, as a dark
		// theme's light grays on a light one, stays legible.
		MinimumContrast: a.set.MinContrast,
		// Every terminal zooms together, and the size is remembered.
		OnZoom: func(z terminal.Zoom) bool {
			a.zoom(z.Steps, z.Reset)
			return true
		},
		// ⌘K and the context menu's Clear clear kerod's terminal, the
		// session's: cleared here alone, kerod's next frames, drawn from a
		// screen still full, would put rows back wrong.
		OnClear: func(terminal.Clear) bool {
			conn.Clear(proto.ClearScreen)
			return true
		},
		OpenLink: func(link string) bool { return a.openLink(id, link) },
		OnPaste:  func(d transfer.Data) bool { return a.pasteAttachment(id, d) },
		// A selection copied as it ends shows nothing else: say so.
		OnCopy: func(_ string, by terminal.CopySource) {
			if by == terminal.CopySelect {
				a.toast(locales.ToastCopiedToClipboard())
				if a.win != nil {
					a.win.Invalidate()
				}
			}
		},
	}
	if a.set.CopyOnSelect {
		o.CopyOnSelect = terminal.CopyOnSelectClipboard
	}
	return o
}

// zoom makes every terminal's text bigger or smaller, which Settings
// remembers as the font's size.
func (a *App) zoom(steps int, reset bool) {
	s := a.set
	s.FontSize = min(max(s.FontSize+float32(steps), 8), 36)
	if reset {
		s.FontSize = settings.Default().FontSize
	}
	if s.FontSize != a.set.FontSize {
		a.applySettings(s)
	}
}

// applySettings makes settings the app's, saving them: the terminals take
// what changed at once, and are attached again for what only a new
// terminal takes.
func (a *App) applySettings(s settings.Settings) { a.apply(s, true) }

// saveSettings writes the settings in the background (writeBack): a font
// size stepped through waits for the next step rather than writing each.
func (a *App) saveSettings(s settings.Settings) { a.setOut.Set(s.Clone()) }

// flushSettings writes a settings change that waits, as quitting does.
func (a *App) flushSettings() { a.setOut.Flush() }

// beforeQuit writes what waits for the disk, and stops what reads behind the
// view, as the window closes last.
func (a *App) beforeQuit() {
	a.savePrefs()
	a.flushPrefs()
	a.flushSettings()
	if a.usage.cancel != nil {
		a.usage.cancel()
	}
}

// apply makes settings the app's, saving them to the configuration when
// they come from the app rather than from the file.
func (a *App) apply(s settings.Settings, save bool) {
	old := a.applied
	a.set, a.applied = s, s.Clone()
	if save {
		a.saveSettings(s)
	}
	font := old.FontFamily != s.FontFamily || old.FontSize != s.FontSize || old.FontWeight != s.FontWeight ||
		old.LineHeight != s.LineHeight || old.Ligatures != s.Ligatures || old.Thicken != s.Thicken
	themes := old.LightTheme != s.LightTheme || old.DarkTheme != s.DarkTheme || old.Accent != s.Accent
	anew := old.Cursor != s.Cursor || old.CursorBlink != s.CursorBlink || old.OptionAsAlt != s.OptionAsAlt ||
		old.Clipboard != s.Clipboard || old.RightClick != s.RightClick
	light, dark := a.termThemes()
	for id, e := range a.terms {
		if e.term == nil {
			continue
		}
		if anew {
			a.dropTerm(id) // attached again as it shows
			continue
		}
		if font {
			e.term.SetFont(a.termFont())
		}
		if themes {
			e.term.SetTheme(light, dark)
		}
		if old.MinContrast != s.MinContrast {
			e.term.SetMinimumContrast(s.MinContrast)
		}
		if old.CopyOnSelect != s.CopyOnSelect {
			mode := terminal.CopyOnSelectNone
			if s.CopyOnSelect {
				mode = terminal.CopyOnSelectClipboard
			}
			e.term.SetCopyOnSelect(mode)
		}
	}
	if old.Appearance != s.Appearance && a.win != nil {
		// Only in the running app: before it runs, the call waits for it.
		mygo.Theme.SetSource(mygo.ThemeSource(s.Appearance))
	}
	if old.Language != s.Language && a.win != nil {
		mygo.App.SetLocale(localeTag(s.Language))
	}
	if old.Vibrancy != s.Vibrancy && a.win != nil {
		a.win.SetVibrancy(vibrancyFor(s))
	}
	if !maps.Equal(old.Shortcuts, s.Shortcuts) && a.win != nil {
		mygo.App.SetMenu(a.menu())
	}
	if old.DockBadge != s.DockBadge {
		a.notify(true)
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}

// watchConfig applies the configuration as the user saves it in an
// editor, and tells what it got wrong. The system hints at changes in the
// file's directory (FSEvents on macOS), where an editor's save, which
// replaces the file, shows too: a hint is all it takes, as Load reads the
// file again only when its modification time changed, so the watch keeps
// no snapshot and stats nothing. The app's own saves come back as changes
// already applied, which change nothing.
func (a *App) watchConfig() {
	dir := filepath.Dir(settings.Path())
	for {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		w, err := mygo.WatchFiles(context.Background(), dir, mygo.FileWatchOptions{HintsOnly: true})
		if err != nil {
			return
		}
		for {
			if _, err := w.Next(context.Background()); err != nil {
				break // the directory went: watch it again, made anew
			}
			a.reloadConfig()
		}
		w.Close()
	}
}

// reloadConfig applies the configuration as the file now has it.
func (a *App) reloadConfig() {
	cur := settings.Load()
	problems := settings.Problems()
	a.update(func() { a.applyConfigFile(cur, problems) })
}

// applyConfigFile applies the configuration the file holds, cur, on the main
// thread. A settings write waits in the background (writeBack), so the file
// can hold an older value than the app: while one waits, the app's own value
// stands, and the write that carries it fires the watch again, which reloads
// the file then.
func (a *App) applyConfigFile(cur settings.Settings, problems []string) {
	changed := strings.Join(problems, "\n") != strings.Join(a.ss.problems, "\n")
	a.ss.problems = problems
	if !a.setOut.pending() && !reflect.DeepEqual(cur, a.applied) {
		a.apply(cur, false)
	}
	if changed && len(problems) > 0 {
		a.toast(locales.ToastConfigProblems(len(problems)))
	}
	a.invalidateMain()
}

func vibrancyFor(s settings.Settings) mygo.Vibrancy {
	if s.Vibrancy {
		return mygo.VibrancySidebar
	}
	return mygo.VibrancyNone
}

var lineSuffix = regexp.MustCompile(`(:\d+){1,2}$`)

// openLink opens what a Command+click opens: URLs with the system, and
// file paths printed in the session, relative to its directory, in the
// app that opens them.
func (a *App) openLink(id, link string) bool {
	if strings.Contains(link, "://") {
		return false
	}
	path := a.resolvePath(id, link)
	if path == "" {
		return false
	}
	openPath(path)
	return true
}

// resolvePath returns the file a path printed in a pane names, relative to
// the directory of the program that printed it, or "" for no such file.
// id is a pane's, or a session's for its pane in front.
func (a *App) resolvePath(id, link string) string {
	path := a.pathFor(id, link)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// pathFor returns the file a path printed in a pane names, without asking
// the disk whether it is there.
func (a *App) pathFor(id, link string) string {
	path := lineSuffix.ReplaceAllString(link, "")
	if strings.HasPrefix(path, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(path, `~`) {
		path = filepath.Join(proto.Home(), path[2:])
	}
	if !filepath.IsAbs(path) {
		if dir := a.workDir(id); dir != "" {
			path = filepath.Join(dir, path)
		}
	}
	return path
}

// workDir returns the working directory of the pane, or of the session's
// pane in front, by id.
func (a *App) workDir(id string) string {
	for i := range a.st.Sessions {
		if p := a.st.Sessions[i].PaneByID(id); p != nil {
			return p.WorkDir()
		}
	}
	if s := a.session(id); s != nil {
		return s.WorkDir()
	}
	return ""
}

// --- actions ---

func (a *App) call(method string, params any, done func(err error)) {
	if a.cl == nil {
		return
	}
	a.cl.Go(method, params, func(err error) {
		if done != nil || err != nil {
			a.update(func() {
				if done != nil {
					done(err)
				} else {
					a.toast(err.Error())
				}
			})
		}
	})
}

func (a *App) toast(msg string) { a.pendingToast = msg }

// newSession starts a session in a project: in the directory of the
// session shown when it is the project's, as a terminal's new tab does,
// else the project's.
func (a *App) newSession(projectID string) {
	if a.cl == nil {
		return
	}
	p := proto.CreateParams{Project: projectID}
	if cur := a.session(a.selected); cur != nil {
		if projectID == "" || cur.Project == projectID {
			p.Project, p.After = cur.Project, cur.ID
			if a.set.NewSessionDir == "current" {
				p.Dir = cur.FocusPane().WorkDir()
			}
		}
		p.Cols, p.Rows = cur.Cols, cur.Rows
	}
	if a.set.NewSessionDir == "home" {
		p.Dir = proto.Home()
	}
	if p.Project == "" && len(a.st.Projects) == 0 {
		p.Dir = proto.Home()
	}
	cl := a.cl
	go func() {
		var s proto.Session
		err := cl.Call("create", p, &s)
		a.update(func() {
			if err != nil {
				a.toast(err.Error())
				return
			}
			// The state with it may come after the answer.
			a.addPending(s)
			a.selectSession(s.ID)
		})
	}()
}

// addPending puts a session just created into the state until the
// daemon's comes with it.
func (a *App) addPending(s proto.Session) {
	if a.session(s.ID) != nil {
		return
	}
	a.st.Sessions = append(a.st.Sessions, s)
	for _, p := range s.Panes {
		a.opening[p.ID] = 1
	}
	if a.project(s.Project) == nil {
		a.st.Projects = append(a.st.Projects, proto.Project{ID: s.Project, Name: filepath.Base(s.Dir), Dir: s.Dir})
	}
}

// openProject asks for a folder, and starts a session there, in its
// project.
func (a *App) openProject() {
	if a.cl == nil {
		return
	}
	cl, win := a.cl, a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win, Title: locales.OpenProjectTitle(), Directory: true, ButtonLabel: locales.CommonOpen(),
			Message: locales.OpenProjectMessage(),
		})
		if err != nil || len(paths) == 0 {
			return
		}
		var p proto.Project
		if err := cl.Call("project.create", proto.ProjectParams{Dir: paths[0]}, &p); err != nil {
			a.update(func() { a.toast(err.Error()) })
			return
		}
		var s proto.Session
		err = cl.Call("create", proto.CreateParams{Project: p.ID, Dir: paths[0]}, &s)
		a.update(func() {
			if err != nil {
				a.toast(err.Error())
				return
			}
			delete(a.prefs.Collapsed, p.ID)
			a.addPending(s)
			a.selectSession(s.ID)
		})
	}()
}

// busy reports whether closing a session would end a program the user may
// care about: anything but the shell at its prompt.
func busy(s *proto.Session) bool {
	return s.Status != proto.Exited && s.Process != ""
}

func (a *App) closeSession(id string, ask bool) {
	s := a.session(id)
	if s == nil {
		return
	}
	if ask && a.set.ConfirmClose && busy(s) {
		name := s.DisplayName(a.set.SessionName)
		a.confirm = &confirmation{
			open:    true,
			title:   locales.ConfirmCloseTitle(name),
			detail:  locales.ConfirmCloseSessionDetail(s.Process),
			action:  locales.ActionCloseSession(),
			destroy: true,
			run:     func() { a.closeSession(id, false) },
		}
		return
	}
	if id == a.selected {
		// The next session takes its place, else the one before.
		ids := a.visibleOrder()
		i := slices.Index(ids, id)
		next := ""
		if i >= 0 && i+1 < len(ids) {
			next = ids[i+1]
		} else if i > 0 {
			next = ids[i-1]
		}
		a.selected = next
		if next != "" {
			a.selectSession(next)
		}
	}
	for _, p := range s.Panes {
		a.dropTerm(p.ID)
	}
	a.st.Sessions = slices.DeleteFunc(a.st.Sessions, func(x proto.Session) bool { return x.ID == id })
	a.call("close", proto.IDParams{ID: id}, nil)
}

func (a *App) restartSession(id string) {
	a.call("restart", proto.IDParams{ID: id}, nil)
}

func (a *App) startRename(id string) {
	s := a.session(id)
	if s == nil {
		return
	}
	a.renaming, a.renamingProject, a.renameBuf, a.renameFrames = id, "", s.DisplayName(a.set.SessionName), 0
}

func (a *App) commitRename() {
	id, name := a.renaming, strings.TrimSpace(a.renameBuf)
	a.renaming = ""
	s := a.session(id)
	if s == nil || name == s.DisplayName(a.set.SessionName) {
		a.focusTerminal()
		return
	}
	s.Name = name // at once; the daemon's state follows
	a.call("rename", proto.RenameParams{ID: id, Name: name}, nil)
	a.focusTerminal()
	a.updateTitle()
}

func (a *App) startProjectRename(id string) {
	for _, p := range a.st.Projects {
		if p.ID == id {
			a.renaming, a.renamingProject, a.renameBuf, a.renameFrames = "", id, p.Name, 0
		}
	}
}

func (a *App) commitProjectRename() {
	id, name := a.renamingProject, strings.TrimSpace(a.renameBuf)
	a.renamingProject = ""
	for i := range a.st.Projects {
		if p := &a.st.Projects[i]; p.ID == id && name != "" && name != p.Name {
			p.Name = name // at once; the daemon's state follows
			a.call("project.rename", proto.ProjectParams{ID: id, Name: name}, nil)
		}
	}
	a.focusTerminal()
}

func (a *App) moveSession(id, project string) {
	if s := a.session(id); s != nil {
		s.Project = project
	}
	a.call("move", proto.MoveParams{ID: id, Project: project, Index: -1}, nil)
}

func (a *App) selectOffset(d int) {
	ids := a.visibleOrder()
	if len(ids) == 0 {
		return
	}
	i := slices.Index(ids, a.selected)
	if i < 0 {
		i = 0
	} else {
		i = (i + d + len(ids)) % len(ids)
	}
	a.selectSession(ids[i])
}

// jumpToAttention goes to the next session that needs the user, after the
// one shown, else to the next with something unseen.
// jumpTo goes to the next session that want takes, after the one shown,
// in the sidebar's order, opening its project, and reports whether there
// was one.
func (a *App) jumpTo(want func(*proto.Session) bool) bool {
	var ids []string
	for _, g := range a.groups() {
		for _, s := range g.sessions {
			ids = append(ids, s.ID)
		}
	}
	i := slices.Index(ids, a.selected)
	for k := 1; k <= len(ids); k++ {
		id := ids[(i+k+len(ids))%len(ids)]
		if s := a.session(id); s != nil && id != a.selected && want(s) {
			delete(a.prefs.Collapsed, s.Project)
			a.selectSession(id)
			return true
		}
	}
	return false
}

func (a *App) jumpToAttention() {
	if a.jumpTo(func(s *proto.Session) bool { return s.Status == proto.Waiting }) ||
		a.jumpTo(func(s *proto.Session) bool { return s.Unread }) {
		return
	}
	a.toast(locales.ToastNothingNeedsYou())
}

func (a *App) toggleSidebar() {
	a.prefs.SidebarHidden = !a.prefs.SidebarHidden
	a.savePrefs()
}

func (a *App) toggleProject(id string) {
	a.prefs.Collapsed[id] = !a.prefs.Collapsed[id]
	if !a.prefs.Collapsed[id] {
		delete(a.prefs.Collapsed, id)
	}
	a.savePrefs()
}

// quitAndEnd ends every session and kerod, then quits.
func (a *App) quitAndEnd() {
	n := len(a.st.Sessions)
	a.confirm = &confirmation{
		open:    true,
		title:   locales.ConfirmQuitAllTitle(),
		detail:  locales.ConfirmQuitAllDetail(n),
		action:  locales.ConfirmQuitAllAction(),
		destroy: true,
		run: func() {
			cl := a.cl
			go func() {
				if cl != nil {
					cl.Call("shutdown", nil, nil)
				}
				mygo.App.Quit()
			}()
		},
	}
}

// daemonStale reports whether kerod runs another version of Kero than
// this one, as after an update: it speaks the protocol, but runs the code
// of before until it restarts.
func (a *App) daemonStale() bool {
	return a.conn == connReady && a.daemonApp != mygo.App.Version()
}

// offerRestart restarts kerod into this version of Kero, asking first
// while programs run in sessions, as the restart ends them.
func (a *App) offerRestart() {
	n := 0
	for i := range a.st.Sessions {
		if busy(&a.st.Sessions[i]) {
			n++
		}
	}
	if n == 0 {
		a.restartDaemon()
		return
	}
	was := locales.RestartEarlierVersion()
	if a.daemonApp != "" {
		was = a.daemonApp
	}
	a.confirm = &confirmation{
		open:    true,
		title:   locales.ConfirmRestartServiceTitle(),
		detail:  locales.RestartStaleDetail(was, mygo.App.Version(), locales.RestartRunning(n)),
		action:  locales.CommonRestartService(),
		destroy: true,
		run:     a.restartDaemon,
	}
}

// restartDaemon restarts kerod, as when it is of another version: its
// sessions start again where they were, with their scrollback.
func (a *App) restartDaemon() {
	a.conn = connConnecting
	go func() {
		err := stopDaemon()
		a.update(func() {
			if err != nil {
				a.conn, a.connErr = connFailed, err.Error()
				return
			}
			a.connect()
		})
	}()
}
