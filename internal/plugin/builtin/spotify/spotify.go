// Package spotify is the Spotify plugin: a player widget that shows
// what the Spotify desktop app plays, controls it, and lets you play an
// album or a playlist from your library.
//
// Playback goes through the transport package's AppleScript commands and
// its playback listener; the library goes through the web package's
// Spotify Web API client. This package only decides what the pane shows
// and what a press does. Every host call goes through the plugin SDK.
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
)

const (
	// pluginID is the id of the built-in, its manifest's id and the
	// argument of "kero plugin-run".
	pluginID = "spotify"

	// widgetPlayer is the widget the manifest registers.
	widgetPlayer = "player"

	// actionTimeout bounds one command, and readTimeout one extra read
	// after a command.
	actionTimeout = 5 * time.Second
	readTimeout   = 2 * time.Second

	// searchTimeout bounds one search, and libraryTimeout one library
	// load. A library load is short, because the pane has the last
	// result to show while it waits.
	searchTimeout  = 15 * time.Second
	libraryTimeout = 8 * time.Second

	// libraryFresh is how long a loaded playlists or albums tab stands
	// before showing the tab again loads it once more.
	libraryFresh = time.Minute

	// maxTiles caps the tiles of a grid, so that a library of hundreds of
	// albums does not cost a tree of hundreds of pictures.
	maxTiles = 48
	// moreID is the id of the last tile of a grid that holds more rows
	// than it shows.
	moreID = "more"

	// seekSettle is how far a pushed position may be from the one the pane
	// already carries forward and still count as the same: the engine's
	// report of an unchanged state is not a new tree.
	seekSettle = 750 * time.Millisecond

	// automationSettingsURL opens System Settings at Privacy & Security ›
	// Automation, the one link outside the web the host lets a plugin
	// open.
	automationSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation"

	// downloadURL is Spotify's download page, for a Mac without the app.
	downloadURL = "https://www.spotify.com/download/mac/"
)

// connectTimeout is how long a pane says it connects to Spotify. An engine
// that has not reported ready by then is shown as unavailable, not as a
// spinner that never ends.
var connectTimeout = 10 * time.Second

// guessTimeout is how long a guess stands without a real state of the
// engine before the pane goes back to what it showed.
var guessTimeout = 5 * time.Second

// afterFunc runs f once after d, off the caller's goroutine. A test
// replaces it to fire the timer by hand.
var afterFunc = func(d time.Duration, f func()) { time.AfterFunc(d, f) }

// init registers the plugin's runner, so that the executable knows what
// to do for "kero plugin-run spotify".
func init() {
	registry.RegisterRunner(pluginID, serve)
}

// serve runs the plugin: the SDK reads and writes the protocol on stdio
// in its own goroutine, while this goroutine runs the macOS run loop the
// playback listener needs. The run loop may only run on the main thread,
// so it is never started with "go".
//
// The SDK loop ends when the host closes stdin, or when the app that
// held it is gone. Then the plugin stops the monitor and stops the run
// loop, so that serve returns and the process ends: no orphan keeps
// reading Spotify after the app crashed.
func serve() error {
	p := newPlugin()
	done := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		err := sdk.Run(sdk.Handlers{
			Open:     p.open,
			Close:    p.close,
			Resize:   p.resize,
			Visible:  p.visible,
			Settings: p.settings,
			Event:    p.event,
			Command:  p.command,
		})
		p.shutdown()
		done <- err
		close(stop)
	}()
	transport.RunLoopUntil(stop)
	return <-done
}

// shutdown stops the playback monitor, once the host is
// gone. A monitor that has not started yet never starts afterwards.
func (p *plugin) shutdown() {
	// An empty Do waits for a start in flight, and keeps a later open
	// from starting the monitor.
	p.monitorOnce.Do(func() {})
	p.mu.Lock()
	monitor, cancel, engine := p.monitor, p.cancel, p.engine
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if monitor != nil {
		_ = monitor.Close()
	}
	if engine != nil {
		_ = engine.Close()
	}
}

// playbackMode is the state of the player the pane draws. It is a type,
// not a string comparison on a message.
type playbackMode int

const (
	modeStarting playbackMode = iota
	modePlaying
	modeIdle
	modeNotRunning
	modeNotInstalled
	modeAutomation
	modeUnavailable
)

// libraryMode is the state of the Web API side of the pane.
type libraryMode int

const (
	libNoClientID libraryMode = iota
	libSignedOut
	libTokenRefused
	libPremiumLapsed
	libNetworkDown
	libRateLimited
	libReady
	libFailed
	libSigningIn
)

// tabKind is one of the library tabs.
type tabKind int

const (
	tabSearch tabKind = iota
	tabPlaylists
	tabAlbums
	// tabQueue is the tracks that come next.
	tabQueue
)

// trackState is what a drill-in track list shows.
type trackState int

const (
	// tracksLoading is a list that loads and has nothing to show yet.
	tracksLoading trackState = iota
	tracksReady
	// tracksForbidden is Spotify refusing the tracks of a playlist that
	// the user neither owns nor collaborates on.
	tracksForbidden
	tracksFailed
)

// flagGuess is the value that a press on shuffle or repeat made the pane
// show, until Spotify says it took it.
type flagGuess struct {
	on bool
	// real is the last value Spotify reported while the guess stood, which
	// the pane goes back to when the press fails or no answer comes.
	real bool
}

// entry is one row of the library list, built from a search result, a
// playlist, or an album.
type entry struct {
	id       string
	title    string
	subtitle string
	trailing string
	image    string
	uri      string
}

// plugin is one plugin process. The host opens every pane of the plugin
// here, so the player, the listener and the Web API client are shared.
type plugin struct {
	mu    sync.Mutex
	conn  *sdk.Conn
	panes map[string]*session
	// player is what the panes control: the engine while the user is
	// signed in with the streaming scope, and the desktop app otherwise.
	// Read it with activePlayer.
	player  transport.Player
	desktop *transport.Transport
	engine  playerEngine
	// streamable says the user's sign-in has the streaming scope, so the
	// engine should play; without an engine it did not start.
	streamable  bool
	engineMu    sync.Mutex
	source      transport.Source
	library     *web.Client
	monitor     *transport.Monitor
	ctx         context.Context
	cancel      context.CancelFunc
	values      map[string]string
	monitorOnce sync.Once
	// playing is the URI of the album or playlist that a press asked to
	// play, from the press until the player answers it, so that a second
	// press on the same row does not play it twice.
	playing string
	// guessSeq numbers the guesses, so that the timer of an older guess
	// does not undo a newer one.
	guessSeq int
	// prefetched says the library loaded the playlists and the albums for
	// this sign-in, so that the tabs have something to show at once.
	prefetched bool
	// last is the track the idle player shows until something plays: what
	// plays on another device, or what the user played last. lastTried says
	// the engine asked for it already, and played that a real track came,
	// which makes a later answer useless.
	last      *web.Track
	lastTried bool
	played    bool
	// engineGen numbers the engines, so that a timer or a fetch of an
	// older engine does not act on a newer one, or on none.
	engineGen int
	// playbackSeq changes when the player pushes a snapshot, so a read
	// already in flight cannot replace it with an older answer.
	playbackSeq uint64
}

// session is one pane's state.
type session struct {
	pane    string
	widget  string
	size    protocol.Size
	visible bool
	// opening is true from the pane's creation until open knows whether the
	// user is signed in and the engine runs. The pane draws nothing before.
	opening bool

	snap     transport.Snapshot
	playback playbackMode
	lib      libraryMode

	tab       tabKind
	query     string
	search    web.SearchResult
	playlists []web.Playlist
	albums    []web.Album
	entries   []entry
	loaded    map[tabKind]bool
	// loadedAt is when each tab loaded, so that showing the tab again after
	// libraryFresh loads it once more.
	loadedAt map[tabKind]time.Time

	// drill is the id of the playlist or the album whose tracks the pane
	// shows in place of the tabs ("playlist-<id>", "album-<id>"), "" for
	// the tabs. drillName, drillURI and drillImage are that row's own.
	drill, drillName, drillURI, drillImage string
	tracks                                 []web.ListTrack
	tracksState                            trackState
	tracksError                            libraryMode
	// needScope says a refused playlist could be one the user collaborates
	// on, which a sign-in with the collaborative scope reads.
	needScope     bool
	tracksSeq     int
	tracksPending bool

	// queue is the tracks of the Up next tab.
	queue        []web.ListTrack
	queueLoaded  bool
	queueFailed  bool
	queueSeq     int
	queuePending bool

	// premute is the volume a press on the speaker took away, which a
	// second press brings back.
	premute int

	shuffleGuess, repeatGuess *flagGuess

	// renderMu keeps this pane's trees in order: a tree is built and sent
	// under it, so a later state is never overtaken by an earlier one.
	renderMu sync.Mutex

	notice string
	// noticeURI is the track that played when a failure notice was set. A
	// state of another track ends the notice; one of the same does not.
	noticeURI string
	// last is the track this pane shows in place of a player that has
	// nothing to play. It is nil once a real state came.
	last *web.Track
	// guess is what the pane showed before a press on a row made it show
	// that row as playing, while the player has not answered. It is nil
	// once a real state came, or the press failed and the pane went back.
	guess *shown

	loading   bool
	signingIn bool
	searchSeq int
	libSeq    int
	// shownTiles is how many tiles of the tab's grid the pane draws. Zero
	// is maxTiles; a press on Show More raises it by maxTiles, and a
	// change of tab or of query puts it back.
	shownTiles int
}

// shown is the player state of a pane, which a guess replaces and a failed
// guess brings back.
type shown struct {
	snap     transport.Snapshot
	playback playbackMode
	// seq is the number of the latest guess that stands on this state.
	seq int
	// toggle says the guess is a flip of play and pause, not a row.
	toggle bool
}

// persisted is the part of a session the host keeps, so the tab and the
// search come back after a restart.
type persisted struct {
	Tab   int    `json:"tab"`
	Query string `json:"query"`
	// Drill is the id of the playlist or the album whose tracks the pane
	// shows, "" for the tabs, and DrillName its name.
	Drill     string `json:"drill,omitempty"`
	DrillName string `json:"drillName,omitempty"`
}

// newPlugin returns a plugin with no panes.
func newPlugin() *plugin {
	return &plugin{
		panes:  make(map[string]*session),
		values: make(map[string]string),
	}
}

// open starts a pane: it creates the backend at the first pane, decodes
// the saved tab and search, starts the playback monitor once, and draws
// the pane.
func (p *plugin) open(conn *sdk.Conn, e sdk.OpenEvent) {
	library := p.ensureBackend(conn)

	// Asking whether the library is signed in reads the saved tokens,
	// which is a host call, so it happens outside p.mu.
	s := &session{
		pane:     e.Pane,
		widget:   e.Widget,
		size:     e.Size,
		opening:  true,
		playback: modeStarting,
		lib:      libraryModeOf(library),
		tab:      tabPlaylists,
		loaded:   make(map[tabKind]bool),
	}
	if len(e.State) > 0 {
		var saved persisted
		if json.Unmarshal(e.State, &saved) == nil {
			if saved.Tab >= int(tabSearch) && saved.Tab <= int(tabQueue) {
				s.tab = tabKind(saved.Tab)
			}
			s.query = saved.Query
			s.restoreDrill(saved.Drill, saved.DrillName)
		}
	}
	p.mu.Lock()
	p.panes[e.Pane] = s
	eng := p.engine
	p.mu.Unlock()
	if i, ok := eng.(idler); ok {
		i.Acquire()
	}

	// The engine, when the user can stream, replaces the desktop app as
	// the player before the monitor decides what to read.
	p.syncEngine()
	// The sign-in and the engine are known now: the pane may draw.
	p.mu.Lock()
	s.opening = false
	p.mu.Unlock()
	p.startMonitor()
	// An engine that runs since an earlier pane pushes nothing for this
	// one, so the pane asks for what it holds.
	if p.pushes() {
		go p.readNow()
		p.armConnect()
	}
	// The prefetch starts first, so that the load of the open tab joins
	// its request.
	p.prefetchLibrary()
	p.syncLibrary(e.Pane)
	p.render(e.Pane)
}

// ensureBackend creates the backend at the first pane, or at the first
// command that needs the Web API, and gives the library the saved Client
// ID. It returns the Web API client.
func (p *plugin) ensureBackend(conn *sdk.Conn) *web.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if conn != nil {
		p.conn = conn
	}
	if p.desktop == nil && p.library == nil {
		p.desktop, p.source, p.library = newBackend(conn)
		if p.desktop != nil && p.engine == nil {
			p.player = p.desktop
		}
	}
	if p.library != nil {
		p.library.SetClientID(p.values[web.SettingClientID])
	}
	return p.library
}

// prefetchLibrary loads the playlists and the saved albums once for a
// sign-in, whatever tab a pane shows, so that a tab opens with the last
// result while the load of the tab goes on. It does nothing before the
// library is ready. The loads of the panes join a request in flight, so a
// second pane does not ask Spotify again.
func (p *plugin) prefetchLibrary() {
	lib := p.libraryState()
	p.mu.Lock()
	library := p.library
	if lib != libReady || library == nil || p.prefetched {
		p.mu.Unlock()
		return
	}
	p.prefetched = true
	p.mu.Unlock()
	go func() {
		// The client keeps the results; a failure is shown when a tab
		// asks again. Each load has its own deadline, so a slow list
		// does not use up the time of the other.
		for _, load := range []func(context.Context){
			func(ctx context.Context) { _, _ = library.Playlists(ctx) },
			func(ctx context.Context) { _, _ = library.Albums(ctx) },
		} {
			ctx, cancel := context.WithTimeout(context.Background(), libraryTimeout)
			load(ctx)
			cancel()
		}
	}()
}

// startMonitor starts the playback monitor once. It reads while a pane is
// visible and reads at once when Spotify posts a playback notification. It
// reads the desktop app, so it does not run while the engine plays: the
// engine pushes its state.
func (p *plugin) startMonitor() {
	p.monitorOnce.Do(func() {
		p.mu.Lock()
		if p.desktop == nil {
			p.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx, p.cancel = ctx, cancel
		p.mu.Unlock()
		p.runMonitor()
	})
}

// runMonitor starts the monitor of the desktop app, unless the engine plays
// or one runs already.
func (p *plugin) runMonitor() {
	p.mu.Lock()
	if p.desktop == nil || p.engine != nil || p.monitor != nil || p.ctx == nil {
		p.mu.Unlock()
		return
	}
	ctx := p.ctx
	monitor := transport.NewMonitor(p.desktop, p.source)
	player, gen := p.player, p.engineGen
	monitor.Emit = func(snap transport.Snapshot) { p.playbackFor(player, gen, snap) }
	monitor.OnError = func(err error) { p.playbackErrorFor(player, gen, err) }
	p.monitor = monitor
	p.mu.Unlock()
	// A source that cannot listen (on anything but macOS) is not an
	// error: the monitor keeps polling instead.
	_ = monitor.Start(ctx)
}

// close forgets a pane and stops the monitor from reading for it.
func (p *plugin) close(_ *sdk.Conn, pane string) {
	p.mu.Lock()
	delete(p.panes, pane)
	monitor := p.monitor
	last := len(p.panes) == 0
	eng := p.engine
	p.mu.Unlock()
	// The last pane gone, the engine lets go of its web view as soon as
	// nothing plays.
	if i, ok := eng.(idler); ok && last {
		i.Release()
	}
	// A closed pane is not visible. Without this the monitor would keep
	// reading for it while another Spotify pane is still open.
	if monitor != nil {
		monitor.Visible(pane, false)
	}
}

// resize records a pane's size, so the pane draws the compact or the
// large layout.
func (p *plugin) resize(_ *sdk.Conn, pane string, size protocol.Size) {
	p.mu.Lock()
	left := false
	if s := p.panes[pane]; s != nil {
		s.size = size
		was := s.tab
		s.leaveQueue()
		left = s.tab != was
		if left {
			p.saveLocked(pane)
		}
	}
	p.mu.Unlock()
	p.render(pane)
	if left && p.libraryState() == libReady {
		p.loadTab(pane, tabPlaylists)
	}
}

// visible tells the monitor which panes show, so it polls only then.
func (p *plugin) visible(_ *sdk.Conn, pane string, visible bool) {
	p.mu.Lock()
	s := p.panes[pane]
	if s != nil {
		s.visible = visible
	}
	monitor := p.monitor
	p.mu.Unlock()
	if monitor != nil {
		monitor.Visible(pane, visible)
	}
	if visible && s != nil {
		// A tab that was shown a while ago loads once more, and the queue,
		// which changes without the pane knowing, is read again.
		p.refreshShown(pane)
	}
}

// settings receives the plugin's non-secret settings. The Client ID is
// the only one.
func (p *plugin) settings(_ *sdk.Conn, values map[string]string) {
	p.mu.Lock()
	p.values = make(map[string]string, len(values))
	for k, v := range values {
		p.values[k] = v
	}
	if p.library != nil {
		p.library.SetClientID(p.values[web.SettingClientID])
	}
	library := p.library
	// Another Client ID is another library.
	p.prefetched = false
	panes := make([]string, 0, len(p.panes))
	for pane, s := range p.panes {
		s.loaded = make(map[tabKind]bool)
		panes = append(panes, pane)
	}
	p.mu.Unlock()

	// The new Client ID decides the library state, and asking for it
	// reads the saved tokens, so this happens outside p.mu.
	lib := libraryModeOf(library)
	p.mu.Lock()
	for _, s := range p.panes {
		s.lib = lib
	}
	p.mu.Unlock()

	p.syncEngine()
	p.prefetchLibrary()
	for _, pane := range panes {
		p.syncLibrary(pane)
		p.render(pane)
	}
}

// libraryState is the library state before a call: no Client ID, not
// signed in, or ready. It reads the Web API client's state outside p.mu,
// because signing in reads the saved tokens with a host call.
func (p *plugin) libraryState() libraryMode {
	p.mu.Lock()
	library := p.library
	p.mu.Unlock()
	return libraryModeOf(library)
}

// libraryModeOf is the library state of a Web API client, from the
// client's own state before a call. A refusal holds there until the next
// sign-in, so the pane keeps asking to sign in again, not only to sign in.
func libraryModeOf(library *web.Client) libraryMode {
	if library == nil {
		return libFailed
	}
	switch library.Current() {
	case web.StateNoClientID:
		return libNoClientID
	case web.StateTokenRefused:
		return libTokenRefused
	case web.StateSignedOut:
		return libSignedOut
	}
	return libReady
}

// libraryError turns a Web API error into the state the pane shows. A
// network failure and a rate limit keep the last results, which the web
// client returns beside its error.
func libraryError(err error) libraryMode {
	switch {
	case err == nil:
		return libReady
	case errors.Is(err, web.ErrNoClientID):
		return libNoClientID
	case errors.Is(err, web.ErrSignedOut):
		return libSignedOut
	case errors.Is(err, web.ErrTokenRefused):
		return libTokenRefused
	case errors.Is(err, web.ErrPremiumLapsed):
		return libPremiumLapsed
	case errors.Is(err, web.ErrNetworkDown):
		return libNetworkDown
	case errors.Is(err, web.ErrRateLimited):
		return libRateLimited
	default:
		return libFailed
	}
}

// keepsResults tells a library state that keeps the last results and
// adds a note above them.
func keepsResults(lib libraryMode) bool {
	return lib == libNetworkDown || lib == libRateLimited
}

// playbackState maps a read's result to the state the pane draws.
func playbackState(err error, snap transport.Snapshot) playbackMode {
	switch {
	case errors.Is(err, engine.ErrStarting):
		return modeStarting
	case errors.Is(err, transport.ErrNotRunning):
		return modeNotRunning
	case errors.Is(err, transport.ErrNotInstalled):
		return modeNotInstalled
	case errors.Is(err, transport.ErrAutomation):
		return modeAutomation
	case err != nil:
		return modeUnavailable
	case snap.Track == "" || snap.State == transport.StateStopped:
		return modeIdle
	default:
		return modePlaying
	}
}

// playback is the monitor's emit hook: a new snapshot goes to every pane.
func (p *plugin) playback(snap transport.Snapshot) {
	p.playbackFor(nil, 0, snap)
}

// Source identity and generation are checked under the same lock that
// applies playback. A callback can already be dispatched when its engine
// is replaced, before Close runs outside this lock.
func (p *plugin) playbackFor(player transport.Player, gen int, snap transport.Snapshot) {
	p.mu.Lock()
	if player != nil && (p.player != player || p.engineGen != gen) {
		p.mu.Unlock()
		return
	}
	p.playbackSeq++
	var queued []string
	for pane, s := range p.panes {
		// The track the player held before a press on a row made the pane
		// show that row: the guess already holds the new URI, so the
		// state that answers it would not look like a change.
		before := s.snap.URI
		if s.guess != nil {
			before = s.guess.snap.URI
		}
		if s.tab == tabQueue && s.visible && layoutOf(s.size) == layoutLarge && snap.URI != before && snap.URI != "" {
			// The queue moved on with the track. The other layouts do not
			// show it.
			queued = append(queued, pane)
		}
		// A track that plays ends the notice of a play that failed, when it
		// is another track than the one that played then, or answers the
		// press of a guess. The echo of a volume or a ready does not.
		if s.notice != "" && snap.State == transport.StatePlaying && p.engine != nil &&
			(snap.URI != "" && snap.URI != s.noticeURI || s.guess != nil && s.guess.answeredBy(s.snap, snap)) {
			clearNotice(s)
		}
		// The engine pushes states that do not answer a press: the
		// ready event, the echo of a volume, a state of the track that
		// played before. Those leave the guess standing, and only keep
		// what they know of the player. The desktop app is read, so any
		// read counts.
		if s.guess != nil && p.engine != nil && !s.guess.answeredBy(s.snap, snap) {
			if snap.Track != "" {
				s.guess.snap = snap
				s.guess.playback = playbackState(nil, snap)
			}
			s.guess.snap.Volume = snap.Volume
			s.snap.Volume = snap.Volume
			continue
		}
		p.settleLocked(s, nil, snap)
		s.guess = nil
	}
	fetch := p.wantLastLocked(snap)
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
	for _, pane := range queued {
		p.loadQueue(pane)
	}
	if fetch {
		go p.fetchLast()
	}
}

// answeredBy tells whether a state of the engine answers the press of the
// guess. A guess of a row is answered by a state of another track than the
// one that played before: the row may be an album, whose URI the state
// never reports, so only the track tells. A guess of a flip is answered by
// the state that the flip asked for. current is what the pane shows now.
func (g *shown) answeredBy(current, state transport.Snapshot) bool {
	if state.URI != "" && state.URI != g.snap.URI {
		return true
	}
	return g.toggle && state.State == current.State
}

// playbackError is the monitor's error hook.
func (p *plugin) playbackError(err error) {
	p.playbackErrorFor(nil, 0, err)
}

func (p *plugin) playbackErrorFor(player transport.Player, gen int, err error) {
	if err == nil || errors.Is(err, transport.ErrBusy) {
		return
	}
	p.mu.Lock()
	if player != nil && (p.player != player || p.engineGen != gen) {
		p.mu.Unlock()
		return
	}
	// The engine's page reports a command or a track that failed, not a
	// player that is gone: the player stays, with a notice.
	starting := p.engine != nil && errors.Is(err, engine.ErrStarting)
	notice := p.engine != nil && !errors.Is(err, engine.ErrStarting)
	// A command that failed will not show its guess, whenever the engine's
	// page tells: the press that made the guess is long answered. An engine
	// that is only starting did not fail the press, so its guess stands.
	if !starting {
		p.revertGuessLocked()
	}
	text := ""
	if notice {
		text = playbackNotice(err)
	}
	for _, s := range p.panes {
		if notice {
			s.notice, s.noticeURI = text, s.snap.URI
		} else if !(starting && s.guess != nil) {
			// A pane that shows a press keeps its mode: the engine that
			// starts again does not take the press back.
			s.playback = playbackState(err, s.snap)
		}
	}
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
	if starting {
		p.armConnect()
	}
}

// toggleShown flips play and pause in every pane at once, before the
// engine reports it, so the button answers the press. The engine's next
// state replaces the guess; a failed command, or no answer in time, brings
// the pane back.
func (p *plugin) toggleShown() {
	p.mu.Lock()
	if p.engine == nil {
		p.mu.Unlock()
		return
	}
	now := time.Now()
	seq := 0
	for _, s := range p.panes {
		if s.snap.State != transport.StatePlaying && s.snap.State != transport.StatePaused {
			continue
		}
		if seq == 0 {
			p.guessSeq++
			seq = p.guessSeq
		}
		if s.guess == nil {
			s.guess = &shown{snap: s.snap, playback: s.playback, toggle: true}
		}
		s.guess.seq = seq
		if s.snap.State == transport.StatePlaying {
			s.snap.Position = s.position()
			s.snap.State = transport.StatePaused
		} else {
			s.snap.State = transport.StatePlaying
		}
		s.snap.ReadAt = now
	}
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
	if seq != 0 {
		p.armGuess(seq)
	}
}

// armGuess brings the panes back after guessTimeout if the guess numbered
// seq still stands then: an engine that never answers must not leave the
// pane with a track that does not play.
func (p *plugin) armGuess(seq int) {
	afterFunc(guessTimeout, func() {
		p.mu.Lock()
		var expired []string
		for pane, s := range p.panes {
			if s.guess != nil && s.guess.seq == seq {
				s.snap, s.playback = s.guess.snap, s.guess.playback
				s.holdFlags(&s.snap)
				s.guess = nil
				expired = append(expired, pane)
			}
		}
		p.mu.Unlock()
		for _, pane := range expired {
			p.render(pane)
		}
	})
}

// revertOnError wraps a command that has a guess: when it fails, the panes
// go back before the failure is shown.
func (p *plugin) revertOnError(fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		err := fn(ctx)
		if err != nil {
			p.mu.Lock()
			p.revertGuessLocked()
			p.mu.Unlock()
		}
		return err
	}
}

// syncLibrary loads the tab or the search a pane shows, once its library
// is ready.
func (p *plugin) syncLibrary(pane string) {
	lib := p.libraryState()

	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	s.lib = lib
	if lib != libReady {
		p.mu.Unlock()
		return
	}
	s.leaveQueue()
	tab, query := s.tab, s.query
	drilled := s.drill != "" && layoutOf(s.size) == layoutLarge
	p.mu.Unlock()

	if drilled {
		// The tracks come back after a restart; the tabs load behind them.
		// Only the large layout shows them.
		p.loadTracks(pane)
	}
	if tab == tabQueue {
		p.loadQueue(pane)
		return
	}
	if tab == tabSearch {
		if strings.TrimSpace(query) != "" {
			p.runSearch(pane, query)
		}
		return
	}
	p.loadTab(pane, tab)
}

// leaveQueue puts a pane that shows the Up next tab but has no room for it
// on the playlists, so that it draws tiles and reads no queue. Only the
// large layout has the tab.
func (s *session) leaveQueue() {
	if s.tab == tabQueue && layoutOf(s.size) != layoutLarge {
		s.tab = tabPlaylists
		s.rebuild()
	}
}

// loadTab loads the playlists or the albums of a tab.
func (p *plugin) loadTab(pane string, tab tabKind) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || tab == tabSearch || tab == tabQueue {
		p.mu.Unlock()
		return
	}
	if at, known := s.loadedAt[tab]; s.loaded[tab] && (!known || time.Since(at) < libraryFresh) {
		p.mu.Unlock()
		return
	}
	library := p.library
	if library == nil {
		p.mu.Unlock()
		return
	}
	s.libSeq++
	seq := s.libSeq
	s.loading = true
	s.tab = tab
	// The last result, from the prefetch or from another pane, is the
	// first paint; the load below replaces it.
	switch tab {
	case tabPlaylists:
		if last, ok := library.LastPlaylists(); ok && len(s.playlists) == 0 {
			s.playlists = last
			s.rebuild()
		}
	case tabAlbums:
		if last, ok := library.LastAlbums(); ok && len(s.albums) == 0 {
			s.albums = last
			s.rebuild()
		}
	}
	p.mu.Unlock()
	p.render(pane)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), libraryTimeout)
		defer cancel()
		var (
			playlists []web.Playlist
			albums    []web.Album
			err       error
		)
		switch tab {
		case tabPlaylists:
			playlists, err = library.Playlists(ctx)
		case tabAlbums:
			albums, err = library.Albums(ctx)
		}

		p.mu.Lock()
		s := p.panes[pane]
		if s == nil || s.libSeq != seq {
			p.mu.Unlock()
			return
		}
		s.loading = false
		if err == nil {
			switch tab {
			case tabPlaylists:
				s.playlists = playlists
			case tabAlbums:
				s.albums = albums
			}
			s.loaded[tab] = true
			if s.loadedAt == nil {
				s.loadedAt = make(map[tabKind]time.Time)
			}
			s.loadedAt[tab] = time.Now()
		}
		s.lib = libraryError(err)
		s.rebuild()
		p.mu.Unlock()
		p.render(pane)
	}()
}

// runSearch searches the catalog for a query.
func (p *plugin) runSearch(pane, query string) {
	lib := p.libraryState()

	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	library := p.library
	if library == nil || lib != libReady {
		p.mu.Unlock()
		return
	}
	s.searchSeq++
	seq := s.searchSeq
	s.loading = true
	// An answer from a minute ago is on screen at once, as the library's
	// last result is; the search below replaces it. A query the client
	// does not know draws the loading state, as before.
	if cached, ok := library.CachedSearch(query); ok {
		s.search = cached
		s.rebuild()
	}
	p.mu.Unlock()
	p.render(pane)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
		defer cancel()
		result, err := library.Search(ctx, query)

		p.mu.Lock()
		s := p.panes[pane]
		if s == nil || s.searchSeq != seq {
			p.mu.Unlock()
			return
		}
		s.loading = false
		s.lib = libraryError(err)
		switch {
		case !keepsResults(s.lib):
			s.search = result
		case result.Query != "":
			// The client's last results, for whatever query they were;
			// rebuild shows them only under that query.
			s.search = result
		}
		s.rebuild()
		p.mu.Unlock()
		p.render(pane)
	}()
}

// rebuild fills the list rows of the current tab.
func (s *session) rebuild() {
	switch s.tab {
	case tabPlaylists:
		s.entries = playlistEntries(s.playlists)
	case tabAlbums:
		s.entries = albumEntries(s.albums)
	case tabQueue:
		s.entries = nil
	default:
		// Results of another query stay out of the list: an offline or
		// rate-limited search keeps the last results, and those can be
		// for the query typed before this one.
		s.entries = nil
		if s.search.Query == strings.TrimSpace(s.query) {
			s.entries = searchEntries(s.search)
		}
	}
}

// searchEntries lists albums first, then playlists, at most ten in all.
func searchEntries(result web.SearchResult) []entry {
	entries := make([]entry, 0, len(result.Albums)+len(result.Playlists))
	for _, album := range result.Albums {
		if album.ID == "" {
			continue
		}
		entries = append(entries, entry{
			id:       "album-" + album.ID,
			title:    album.Name,
			subtitle: album.ArtistLine(),
			trailing: localeAlbum(),
			image:    album.Artwork(),
			uri:      album.URI,
		})
	}
	for _, playlist := range result.Playlists {
		if playlist.ID == "" {
			continue
		}
		entries = append(entries, entry{
			id:       "playlist-" + playlist.ID,
			title:    playlist.Name,
			subtitle: playlist.Owner,
			trailing: localePlaylist(),
			image:    playlist.Artwork(),
			uri:      playlist.URI,
		})
	}
	if len(entries) > 10 {
		entries = entries[:10]
	}
	return entries
}

// playlistEntries lists the user's playlists.
func playlistEntries(playlists []web.Playlist) []entry {
	entries := make([]entry, 0, len(playlists))
	for _, playlist := range playlists {
		if playlist.ID == "" {
			continue
		}
		entries = append(entries, entry{
			id:       "playlist-" + playlist.ID,
			title:    playlist.Name,
			subtitle: playlist.Owner,
			image:    playlist.Artwork(),
			uri:      playlist.URI,
		})
	}
	return entries
}

// albumEntries lists the user's saved albums.
func albumEntries(albums []web.Album) []entry {
	entries := make([]entry, 0, len(albums))
	for _, album := range albums {
		if album.ID == "" {
			continue
		}
		entries = append(entries, entry{
			id:       "album-" + album.ID,
			title:    album.Name,
			subtitle: album.ArtistLine(),
			image:    album.Artwork(),
			uri:      album.URI,
		})
	}
	return entries
}

// event handles a press, a change, a commit, a select or a submit.
func (p *plugin) event(conn *sdk.Conn, e protocol.EventParams) {
	p.mu.Lock()
	s := p.panes[e.Pane]
	p.mu.Unlock()
	if s == nil {
		return
	}

	switch e.Node {
	case "prev":
		p.doCommand(conn, e.Pane, localeCommandFailed(), "previous")
	case "play":
		p.doCommand(conn, e.Pane, localeCommandFailed(), "play-pause")
	case "next":
		p.doCommand(conn, e.Pane, localeCommandFailed(), "next")
	case "open-spotify":
		p.launch(e.Pane)
	case "privacy":
		p.openLink(automationSettingsURL)
	case "get-spotify":
		p.openLink(downloadURL)
	case "setup":
		p.openLink("https://developer.spotify.com/dashboard")
	case "sign-in", "sign-in-again":
		p.signIn(e.Pane)
	case "shuffle":
		p.toggle(e.Pane, "shuffle")
	case "repeat":
		p.toggle(e.Pane, "repeat")
	case "tab-search":
		p.selectTab(e.Pane, tabSearch)
	case "tab-playlists":
		p.selectTab(e.Pane, tabPlaylists)
	case "tab-albums":
		p.selectTab(e.Pane, tabAlbums)
	case "tab-queue":
		p.selectTab(e.Pane, tabQueue)
	case "drill-back":
		p.closeDrill(e.Pane)
	case "drill-play":
		p.playDrill(e.Pane)
	case "tracks":
		p.selectTrack(e, false)
	case "queue":
		p.selectTrack(e, true)
	case "state-tracks-failed":
		p.mu.Lock()
		s := p.panes[e.Pane]
		if s == nil || p.library == nil {
			p.mu.Unlock()
			return
		}
		queue := s.drill == ""
		if queue && s.queuePending || !queue && s.tracksPending {
			p.mu.Unlock()
			return
		}
		if queue {
			s.queuePending = true
		} else {
			s.tracksPending = true
		}
		p.mu.Unlock()
		if queue {
			p.loadQueue(e.Pane)
		} else {
			p.loadTracks(e.Pane)
		}
		p.render(e.Pane)
	case "state-library-failed", "state-offline":
		p.mu.Lock()
		delete(s.loaded, s.tab)
		p.mu.Unlock()
		p.syncLibrary(e.Pane)
		p.render(e.Pane)
	case "mute":
		p.mute(e.Pane)
	case "search":
		p.searchEvent(e)
	case "seek":
		p.seekEvent(e)
	case "volume":
		p.volumeEvent(e)
	case "results":
		p.selectResult(e)
	}
}

// searchEvent applies a typed query and searches when the query is new.
func (p *plugin) searchEvent(e protocol.EventParams) {
	query, _ := e.Value.(string)
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	if e.Kind == protocol.KindSubmit || e.Kind == protocol.KindChange {
		if query == s.query {
			p.mu.Unlock()
			return
		}
		s.query = query
		s.searchSeq++
		s.shownTiles = 0
		s.tab = tabSearch
		// Keep the last results while a search runs: when the search
		// fails offline, they are what the pane still shows.
		if strings.TrimSpace(query) == "" {
			s.search = web.SearchResult{}
		}
		s.rebuild()
		p.saveLocked(e.Pane)
		p.mu.Unlock()
		p.render(e.Pane)
		if strings.TrimSpace(query) == "" {
			return
		}
		p.runSearch(e.Pane, query)
		return
	}
	p.mu.Unlock()
}

// selectTab switches the library tab and loads it when needed. It asks the
// client for the library state, as runSearch does, so that a tab whose
// last load failed loads again.
func (p *plugin) selectTab(pane string, tab tabKind) {
	lib := p.libraryState()

	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || s.tab == tab {
		p.mu.Unlock()
		return
	}
	s.tab = tab
	s.shownTiles = 0
	s.rebuild()
	s.loaded[tab] = s.loaded[tab] && len(s.entries) > 0
	// The state before a call replaces a failure of the last one, so the
	// tab draws its loading state, or its search field, again. Kept
	// results keep their offline or rate-limit note until a call works.
	if !keepsResults(s.lib) {
		s.lib = lib
	}
	ready := lib == libReady
	p.saveLocked(pane)
	p.mu.Unlock()
	p.render(pane)
	if ready && tab == tabQueue {
		p.loadQueue(pane)
	} else if ready && tab != tabSearch {
		p.loadTab(pane, tab)
	}
}

// seekEvent seeks when the slider is released. A drag step changes nothing
// in the pane: the host keeps the knob under the finger, and a tree sent on
// every step would only be redrawn.
func (p *plugin) seekEvent(e protocol.EventParams) {
	value, ok := floatValue(e.Value)
	if !ok || e.Kind != protocol.KindCommit {
		return
	}
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	at := time.Duration(value * float64(time.Second))
	if s.snap.Duration > 0 && at > s.snap.Duration {
		at = s.snap.Duration
	}
	if at < 0 {
		at = 0
	}
	// A new instant with the new position: the slider and the clocks run
	// on from here.
	s.snap.Position = at
	s.snap.ReadAt = time.Now()
	p.mu.Unlock()
	p.render(e.Pane)
	p.action(e.Pane, localeSeekFailed(), func(ctx context.Context) error {
		return p.activePlayer().Seek(ctx, at)
	})
}

// volumeEvent sets the volume when the slider is released.
func (p *plugin) volumeEvent(e protocol.EventParams) {
	value, ok := floatValue(e.Value)
	if !ok || e.Kind != protocol.KindCommit {
		return
	}
	p.setVolume(e.Pane, int(value+0.5))
}

// setVolume sets the volume of the player and shows it at once.
func (p *plugin) setVolume(pane string, volume int) {
	volume = min(max(volume, 0), 100)
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	s.snap.Volume = volume
	p.mu.Unlock()
	p.render(pane)
	p.action(pane, localeVolumeFailed(), func(ctx context.Context) error {
		return p.activePlayer().Volume(ctx, volume)
	})
}

// mute silences the player, or brings back the volume that a press took
// away.
func (p *plugin) mute(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	volume := 0
	if s.snap.Volume > 0 {
		s.premute = s.snap.Volume
	} else {
		volume = s.premute
		if volume == 0 {
			volume = 50
		}
	}
	p.mu.Unlock()
	p.setVolume(pane, volume)
}

// selectResult handles a press on a tile. A playlist or an album opens its
// tracks when the pane has the room for them, and plays otherwise, as the
// medium layout does; the player shows the row as playing at once, before
// it answers.
func (p *plugin) selectResult(e protocol.EventParams) {
	id, _ := e.Value.(string)
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	if id == moreID {
		// Show More: the grid grows by another page of tiles.
		s.shownTiles = s.tileLimit() + maxTiles
		p.mu.Unlock()
		p.render(e.Pane)
		return
	}
	var picked entry
	for _, entry := range s.entries {
		if entry.id == id {
			picked = entry
			break
		}
	}
	drills := layoutOf(s.size) == layoutLarge && picked.drillable()
	p.mu.Unlock()
	if drills {
		p.openDrill(e.Pane, picked)
		return
	}
	p.playEntry(e.Pane, picked)
}

// playEntry plays the album or playlist of a row. The pane shows the row as
// playing at once, before the player answers; the player's own state
// replaces that guess, and a failed press brings the pane back.
func (p *plugin) playEntry(pane string, picked entry) {
	uri := picked.uri
	p.mu.Lock()
	// A second press on the row waits for the first one.
	if p.panes[pane] == nil || uri == "" || p.playing == uri {
		p.mu.Unlock()
		return
	}
	p.playing = uri
	seq := p.guessLocked(picked, "")
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
	if seq != 0 {
		p.armGuess(seq)
	}

	p.action(pane, localePlayFailed(), func(ctx context.Context) error {
		err := p.activePlayer().Play(ctx, uri)
		p.mu.Lock()
		if p.playing == uri {
			p.playing = ""
		}
		if err != nil {
			p.revertGuessLocked()
		}
		p.mu.Unlock()
		return err
	})
}

// guessLocked makes every pane that can play show a row as now playing,
// and is called with p.mu held. The art, the title and the subtitle are the
// row's, and the play button shows pause. A row that is a track also gives
// its URI, so that the list highlights it at once. A pane that cannot play yet
// keeps its state. It returns the number of the guess, or 0 when no pane
// guessed.
func (p *plugin) guessLocked(row entry, trackURI string) int {
	now := time.Now()
	seq := 0
	for _, s := range p.panes {
		if s.playback != modePlaying && s.playback != modeIdle {
			continue
		}
		if seq == 0 {
			p.guessSeq++
			seq = p.guessSeq
		}
		if s.guess == nil {
			s.guess = &shown{snap: s.snap, playback: s.playback}
		}
		s.guess.seq = seq
		s.guess.toggle = false
		s.snap = transport.Snapshot{
			State:      transport.StatePlaying,
			Track:      row.title,
			URI:        trackURI,
			Artist:     row.subtitle,
			ArtworkURL: row.image,
			Volume:     s.snap.Volume,
			Shuffle:    s.snap.Shuffle,
			Repeat:     s.snap.Repeat,
			ReadAt:     now,
		}
		s.playback = modePlaying
	}
	return seq
}

// revertGuessLocked brings back what the panes showed before a guess that
// the player did not take, and is called with p.mu held. A pane whose real
// state came meanwhile has no guess and keeps that state.
func (p *plugin) revertGuessLocked() {
	for _, s := range p.panes {
		if s.guess == nil {
			continue
		}
		volume := s.snap.Volume
		s.snap, s.playback = s.guess.snap, s.guess.playback
		s.snap.Volume = volume
		s.holdFlags(&s.snap)
		s.guess = nil
	}
}

// toggle flips shuffle or repeat. The pane shows the new value at once and
// keeps showing it, whatever state Spotify pushes meanwhile, until Spotify
// reports it, the command fails, or no answer comes in time.
func (p *plugin) toggle(pane, what string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	player := p.player
	shuffle := what == "shuffle"
	was := s.snap.Repeat
	if shuffle {
		was = s.snap.Shuffle
	}
	on := !was
	guess := &flagGuess{on: on, real: was}
	if shuffle {
		s.snap.Shuffle, s.shuffleGuess = on, guess
	} else {
		s.snap.Repeat, s.repeatGuess = on, guess
	}
	p.mu.Unlock()
	p.render(pane)
	afterFunc(guessTimeout, func() { p.expireFlag(pane, guess, shuffle) })

	fail := localeCommandFailed()
	p.action(pane, fail, func(ctx context.Context) error {
		var err error
		if shuffle {
			err = player.Shuffle(ctx, on)
		} else {
			err = player.Repeat(ctx, on)
		}
		if err != nil {
			p.expireFlag(pane, guess, shuffle)
		}
		return err
	})
}

// expireFlag ends a guess of shuffle or repeat that Spotify did not
// confirm, and shows the last value Spotify reported.
func (p *plugin) expireFlag(pane string, guess *flagGuess, shuffle bool) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	stood := false
	if shuffle && s.shuffleGuess == guess {
		s.snap.Shuffle, s.shuffleGuess, stood = guess.real, nil, true
	}
	if !shuffle && s.repeatGuess == guess {
		s.snap.Repeat, s.repeatGuess, stood = guess.real, nil, true
	}
	p.mu.Unlock()
	if stood {
		p.render(pane)
	}
}

// doCommand runs one of the player's commands.
func (p *plugin) doCommand(_ *sdk.Conn, pane, fail, command string) {
	player := p.activePlayer()
	if player == nil {
		return
	}
	if command == "play-pause" && p.playLast(pane, fail) {
		return
	}
	if command == "play-pause" {
		p.toggleShown()
	}
	p.action(pane, fail, func(ctx context.Context) error {
		switch command {
		case "play-pause":
			return p.revertOnError(player.PlayPause)(ctx)
		case "next":
			return player.Next(ctx)
		case "previous":
			return player.Previous(ctx)
		}
		return nil
	})
}

// command handles a palette command or a "kero do" action: the host sends
// the command to the plugin process, which runs it for every open pane.
// The protocol's command names no pane, so a failure notice goes to every
// open pane instead of to one chosen at random.
func (p *plugin) command(conn *sdk.Conn, id string) {
	// Sign Out is the button in Settings, which runs with no pane open.
	if id == "sign-out" {
		p.ensureBackend(conn)
		p.signOut()
		return
	}
	p.mu.Lock()
	player := p.player
	open := len(p.panes)
	// Without the engine the panes ask for a sign-in, and the commands do
	// not drive the desktop app instead.
	standalone := p.engine != nil
	p.mu.Unlock()
	if player == nil || open == 0 || !standalone {
		return
	}
	fail := localeCommandFailed()
	switch id {
	case "play-pause":
		p.toggleShown()
		p.actionAll(fail, p.revertOnError(player.PlayPause))
	case "next":
		p.actionAll(fail, player.Next)
	case "previous":
		p.actionAll(fail, player.Previous)
	}
}

// signIn runs the Web API's PKCE sign-in and then loads the pane again.
func (p *plugin) signIn(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || p.library == nil {
		p.mu.Unlock()
		return
	}
	if s.signingIn {
		p.mu.Unlock()
		return
	}
	s.signingIn = true
	s.notice = localeSignInRunning()
	library := p.library
	p.mu.Unlock()
	p.render(pane)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := library.SignIn(ctx)

		// A failed sign-in leaves the library in whatever state the
		// client reports now, which reads the saved tokens, so it is
		// asked outside p.mu.
		var lib libraryMode
		if err != nil {
			lib = p.libraryState()
		}

		p.mu.Lock()
		s := p.panes[pane]
		if s == nil {
			p.mu.Unlock()
			return
		}
		s.signingIn = false
		if err != nil {
			s.notice = localeSignInFailed()
			s.lib = lib
			if errors.Is(err, web.ErrNoClientID) {
				s.notice = localeNoClientID()
			}
			p.mu.Unlock()
			p.render(pane)
			return
		}
		s.notice = ""
		s.lib = libReady
		s.loaded = make(map[tabKind]bool)
		p.prefetched = false
		p.mu.Unlock()
		// Signed in with the streaming scope, the player is the engine.
		p.syncEngine()
		p.render(pane)
		p.prefetchLibrary()
		p.syncLibrary(pane)
	}()
}

// action runs one player command off the handler goroutine, reports a
// failure in the pane, and reads the player again after a success.
func (p *plugin) action(pane, fail string, fn func(context.Context) error) {
	pushes := p.pushes()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		err := fn(ctx)
		if err != nil {
			p.mu.Lock()
			if s := p.panes[pane]; s != nil {
				s.notice = fail
				s.playback = playbackState(err, s.snap)
			}
			p.mu.Unlock()
			p.render(pane)
			return
		}
		p.mu.Lock()
		cleared := false
		if s := p.panes[pane]; s != nil {
			cleared = clearNotice(s)
		}
		p.mu.Unlock()
		if cleared {
			p.render(pane)
		}
		p.readAfterCommand(pushes)
	}()
}

// clearNotice drops a failure notice after a command that worked, and
// tells whether there was one. The sign-in's own notice stays while the
// sign-in runs.
func clearNotice(s *session) bool {
	if s.notice == "" || s.signingIn {
		return false
	}
	s.notice = ""
	return true
}

// actionAll runs one player command off the handler goroutine, reports a
// failure in every open pane, and reads the player again after a success.
func (p *plugin) actionAll(fail string, fn func(context.Context) error) {
	pushes := p.pushes()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		err := fn(ctx)
		if err != nil {
			p.mu.Lock()
			for _, s := range p.panes {
				s.notice = fail
				s.playback = playbackState(err, s.snap)
			}
			panes := p.paneIDsLocked()
			p.mu.Unlock()
			for _, pane := range panes {
				p.render(pane)
			}
			return
		}
		p.mu.Lock()
		cleared := make([]string, 0, len(p.panes))
		for pane, s := range p.panes {
			if clearNotice(s) {
				cleared = append(cleared, pane)
			}
		}
		p.mu.Unlock()
		for _, pane := range cleared {
			p.render(pane)
		}
		p.readAfterCommand(pushes)
	}()
}

// pushes tells whether the player pushes its state: the engine does, and
// the desktop app has to be read. The engine and the player are set
// together, so the engine tells which one plays.
func (p *plugin) pushes() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.engine != nil
}

// readAfterCommand shows what a command did. The app's player has to be
// read for that. The engine pushes its state, and its Read only gives back
// the last state it pushed, which would also wipe out a guess that the pane
// shows until the new state comes, so nothing is read. pushes is what the
// plugin knew when the command started.
func (p *plugin) readAfterCommand(pushes bool) {
	if !pushes {
		p.readNow()
	}
}

// activePlayer is the player the panes control now.
func (p *plugin) activePlayer() transport.Player {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.player
}

// readNow reads the player once, so a command shows its effect without
// waiting for the next poll.
func (p *plugin) readNow() {
	p.mu.Lock()
	player := p.player
	engineGen, playbackSeq := p.engineGen, p.playbackSeq
	p.mu.Unlock()
	if player == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	snap, err := player.Read(ctx)
	if errors.Is(err, transport.ErrBusy) {
		return
	}
	p.mu.Lock()
	if engineGen != p.engineGen || playbackSeq != p.playbackSeq {
		p.mu.Unlock()
		return
	}
	for _, s := range p.panes {
		// What the engine gives back is the last state it pushed, which
		// a pane that shows a guess has seen already.
		if s.guess != nil && p.engine != nil && err == nil {
			continue
		}
		// A read that found the engine starting is older than the
		// state it pushed since, and must not undo it.
		if p.engine != nil && errors.Is(err, engine.ErrStarting) && s.playback != modeStarting {
			continue
		}
		p.settleLocked(s, err, snap)
		s.guess = nil
	}
	fetch := err == nil && p.wantLastLocked(snap)
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
	if fetch {
		go p.fetchLast()
	}
}

// launch starts Spotify, or brings it to the front. Every other script
// refuses to start the app, so this is the one that may.
func (p *plugin) launch(pane string) {
	player := p.activePlayer()
	if player == nil {
		return
	}
	p.action(pane, localeCommandFailed(), func(ctx context.Context) error {
		return player.Launch(ctx)
	})
}

// openLink opens an http or https link in the browser, or the Privacy &
// Security link in System Settings.
func (p *plugin) openLink(url string) {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return
	}
	go func() {
		_ = conn.OpenURL(url)
	}()
}

// saveLocked starts the host call that saves a pane's state. It is called
// with p.mu held, and the call runs in its own goroutine.
func (p *plugin) saveLocked(pane string) {
	s := p.panes[pane]
	conn := p.conn
	if s == nil || conn == nil {
		return
	}
	state := persisted{Tab: int(s.tab), Query: s.query, Drill: s.drill, DrillName: s.drillName}
	go func() {
		_ = conn.SaveState(pane, state)
	}()
}

// render sends a pane's tree to the host. The tree is built and sent under
// the pane's own lock, so that the handlers, the loads and the engine's
// pushes, which all render, cannot send a tree built from an older state
// after one built from a newer.
func (p *plugin) render(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	p.mu.Lock()
	if p.panes[pane] != s {
		// The pane closed while this render waited.
		p.mu.Unlock()
		return
	}
	tree := p.tree(s)
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return
	}
	drawTree(conn, pane, tree)
}

// drawTree sends one tree to the host. A test replaces it to see what a
// pane would draw, because a zero Conn cannot render.
var drawTree = func(conn *sdk.Conn, pane string, tree protocol.Tree) {
	_ = conn.Render(pane, tree)
}

// paneIDsLocked returns the open panes, and is called with p.mu held.
func (p *plugin) paneIDsLocked() []string {
	panes := make([]string, 0, len(p.panes))
	for pane := range p.panes {
		panes = append(panes, pane)
	}
	return panes
}

// floatValue reads a slider's value from an event.
func floatValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}
