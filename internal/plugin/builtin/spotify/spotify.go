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

	// compactWidth and compactHeight are the size below which the pane
	// draws its compact layout: the player only, no library.
	compactWidth  = 420
	compactHeight = 340

	// signInPort is the fixed loopback port of the Web API's redirect
	// URI. It must match the redirect URI in the user's Spotify app.
	signInPort = 43821

	// actionTimeout bounds one command, and readTimeout one extra read
	// after a command.
	actionTimeout = 5 * time.Second
	readTimeout   = 2 * time.Second

	// searchTimeout bounds one search, and libraryTimeout one library
	// load.
	searchTimeout  = 15 * time.Second
	libraryTimeout = 20 * time.Second
)

// init registers the plugin's runner, so that the executable knows what
// to do for "kero plugin-run spotify".
func init() {
	registry.RegisterRunner(pluginID, serve)
}

// serve runs the plugin: the SDK reads and writes the protocol on stdio
// in its own goroutine, while this goroutine runs the macOS run loop the
// playback listener needs. The run loop may only run on the main thread,
// so it is never started with "go".
func serve() error {
	p := newPlugin()
	done := make(chan error, 1)
	go func() {
		done <- sdk.Run(sdk.Handlers{
			Open:     p.open,
			Close:    p.close,
			Resize:   p.resize,
			Visible:  p.visible,
			Settings: p.settings,
			Event:    p.event,
			Command:  p.command,
		})
	}()
	transport.RunLoop()
	return <-done
}

// newBackend builds the player, the playback listener, and the Web API
// client of one plugin process. A test replaces it with fakes.
var newBackend = func(conn *sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
	client := web.New(web.Config{
		RedirectPort: signInPort,
		OpenURL:      web.ConnOpenURL(conn),
		Secrets:      web.ConnSecrets(conn),
	})
	return transport.New(transport.OSAscript{}), transport.NewSource(), client
}

// playbackMode is the state of the player the pane draws. It is a type,
// not a string comparison on a message.
type playbackMode int

const (
	modeStarting playbackMode = iota
	modePlaying
	modeIdle
	modeNotRunning
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
)

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
	mu          sync.Mutex
	conn        *sdk.Conn
	panes       map[string]*session
	player      *transport.Transport
	source      transport.Source
	library     *web.Client
	monitor     *transport.Monitor
	ctx         context.Context
	cancel      context.CancelFunc
	values      map[string]string
	monitorOnce sync.Once
}

// session is one pane's state.
type session struct {
	pane    string
	widget  string
	size    protocol.Size
	visible bool

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

	seek   *time.Duration
	volume *int
	notice string

	loading   bool
	signingIn bool
	searchSeq int
	libSeq    int
}

// persisted is the part of a session the host keeps, so the tab and the
// search come back after a restart.
type persisted struct {
	Tab   int    `json:"tab"`
	Query string `json:"query"`
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
	p.mu.Lock()
	p.conn = conn
	if p.player == nil {
		p.player, p.source, p.library = newBackend(conn)
	}
	if p.library != nil {
		p.library.SetClientID(p.values[web.SettingClientID])
	}
	s := &session{
		pane:     e.Pane,
		widget:   e.Widget,
		size:     e.Size,
		playback: modeStarting,
		lib:      p.currentLibrary(),
		loaded:   make(map[tabKind]bool),
	}
	if len(e.State) > 0 {
		var saved persisted
		if json.Unmarshal(e.State, &saved) == nil {
			if saved.Tab >= int(tabSearch) && saved.Tab <= int(tabAlbums) {
				s.tab = tabKind(saved.Tab)
			}
			s.query = saved.Query
		}
	}
	p.panes[e.Pane] = s
	p.mu.Unlock()

	p.startMonitor()
	p.syncLibrary(e.Pane)
	p.render(e.Pane)
}

// startMonitor starts the playback monitor once. It reads while a pane is
// visible and reads at once when Spotify posts a playback notification.
func (p *plugin) startMonitor() {
	p.monitorOnce.Do(func() {
		p.mu.Lock()
		player, source := p.player, p.source
		if player == nil {
			p.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx, p.cancel = ctx, cancel
		monitor := transport.NewMonitor(player, source)
		monitor.Emit = p.playback
		monitor.OnError = p.playbackError
		p.monitor = monitor
		p.mu.Unlock()
		// A source that cannot listen (on anything but macOS) is not an
		// error: the monitor keeps polling instead.
		_ = monitor.Start(ctx)
	})
}

// close forgets a pane.
func (p *plugin) close(_ *sdk.Conn, pane string) {
	p.mu.Lock()
	delete(p.panes, pane)
	p.mu.Unlock()
}

// resize records a pane's size, so the pane draws the compact or the
// large layout.
func (p *plugin) resize(_ *sdk.Conn, pane string, size protocol.Size) {
	p.mu.Lock()
	if s := p.panes[pane]; s != nil {
		s.size = size
	}
	p.mu.Unlock()
	p.render(pane)
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
	panes := make([]string, 0, len(p.panes))
	for pane, s := range p.panes {
		s.lib = p.currentLibrary()
		s.loaded = make(map[tabKind]bool)
		panes = append(panes, pane)
	}
	p.mu.Unlock()
	for _, pane := range panes {
		p.syncLibrary(pane)
		p.render(pane)
	}
}

// currentLibrary is the library state before a call: no Client ID, not
// signed in, or ready.
func (p *plugin) currentLibrary() libraryMode {
	if p.library == nil {
		return libFailed
	}
	if strings.TrimSpace(p.library.ClientID()) == "" {
		return libNoClientID
	}
	if !p.library.SignedIn() {
		return libSignedOut
	}
	return libReady
}

// libraryError turns a Web API error into the state the pane shows. A
// network failure keeps the last results, which the web client returns
// beside its error.
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
	default:
		return libFailed
	}
}

// playbackState maps a read's result to the state the pane draws.
func playbackState(err error, snap transport.Snapshot) playbackMode {
	switch {
	case errors.Is(err, transport.ErrNotRunning):
		return modeNotRunning
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
	p.mu.Lock()
	for _, s := range p.panes {
		s.snap = snap
		s.playback = playbackState(nil, snap)
	}
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
}

// playbackError is the monitor's error hook.
func (p *plugin) playbackError(err error) {
	if err == nil || errors.Is(err, transport.ErrBusy) {
		return
	}
	p.mu.Lock()
	for _, s := range p.panes {
		s.playback = playbackState(err, s.snap)
	}
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
}

// syncLibrary loads the tab or the search a pane shows, once its library
// is ready.
func (p *plugin) syncLibrary(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	s.lib = p.currentLibrary()
	if s.lib != libReady {
		p.mu.Unlock()
		return
	}
	tab, query := s.tab, s.query
	p.mu.Unlock()

	if tab == tabSearch {
		if strings.TrimSpace(query) != "" {
			p.runSearch(pane, query)
		}
		return
	}
	p.loadTab(pane, tab)
}

// loadTab loads the playlists or the albums of a tab.
func (p *plugin) loadTab(pane string, tab tabKind) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || tab == tabSearch {
		p.mu.Unlock()
		return
	}
	if s.loaded[tab] {
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
		}
		s.lib = libraryError(err)
		s.rebuild()
		p.mu.Unlock()
		p.render(pane)
	}()
}

// runSearch searches the catalog for a query.
func (p *plugin) runSearch(pane, query string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	library := p.library
	if library == nil || p.currentLibrary() != libReady {
		p.mu.Unlock()
		return
	}
	s.searchSeq++
	seq := s.searchSeq
	s.loading = true
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
		if !errors.Is(err, web.ErrNetworkDown) {
			s.search = result
		}
		s.lib = libraryError(err)
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
	default:
		s.entries = searchEntries(s.search)
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
			trailing: "Album",
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
			trailing: "Playlist",
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
		p.openLink("https://support.apple.com/guide/mac-help/mchl108e1718/mac")
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
		s.tab = tabSearch
		s.search = web.SearchResult{}
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

// selectTab switches the library tab and loads it when needed.
func (p *plugin) selectTab(pane string, tab tabKind) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || s.tab == tab {
		p.mu.Unlock()
		return
	}
	s.tab = tab
	s.rebuild()
	s.loaded[tab] = s.loaded[tab] && len(s.entries) > 0
	ready := s.lib == libReady
	p.saveLocked(pane)
	p.mu.Unlock()
	p.render(pane)
	if ready && tab != tabSearch {
		p.loadTab(pane, tab)
	}
}

// seekEvent changes the position while the slider is dragged, and seeks
// when it is released.
func (p *plugin) seekEvent(e protocol.EventParams) {
	value, ok := floatValue(e.Value)
	if !ok {
		return
	}
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	duration := s.snap.Duration
	switch e.Kind {
	case protocol.KindChange:
		at := time.Duration(value * float64(duration))
		s.seek = &at
		p.mu.Unlock()
	case protocol.KindCommit:
		at := time.Duration(value * float64(duration))
		s.seek = nil
		s.snap.Position = at
		s.snap.ReadAt = time.Now()
		p.mu.Unlock()
		p.action(e.Pane, localeSeekFailed(), func(ctx context.Context) error {
			return p.player.Seek(ctx, at)
		})
	default:
		p.mu.Unlock()
	}
	p.render(e.Pane)
}

// volumeEvent changes the volume while the slider is dragged, and sets it
// when it is released.
func (p *plugin) volumeEvent(e protocol.EventParams) {
	value, ok := floatValue(e.Value)
	if !ok {
		return
	}
	volume := int(value + 0.5)
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	switch e.Kind {
	case protocol.KindChange:
		s.volume = &volume
		p.mu.Unlock()
	case protocol.KindCommit:
		s.volume = nil
		s.snap.Volume = volume
		p.mu.Unlock()
		p.action(e.Pane, localeVolumeFailed(), func(ctx context.Context) error {
			return p.player.Volume(ctx, volume)
		})
	default:
		p.mu.Unlock()
	}
	p.render(e.Pane)
}

// selectResult plays the album or playlist of a pressed row.
func (p *plugin) selectResult(e protocol.EventParams) {
	id, _ := e.Value.(string)
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	var uri string
	for _, entry := range s.entries {
		if entry.id == id {
			uri = entry.uri
			break
		}
	}
	p.mu.Unlock()
	if uri == "" {
		return
	}
	p.action(e.Pane, localePlayFailed(), func(ctx context.Context) error {
		return p.player.Play(ctx, uri)
	})
}

// toggle flips shuffle or repeat.
func (p *plugin) toggle(pane, what string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	player := p.player
	on := !s.snap.Shuffle
	if what == "repeat" {
		on = !s.snap.Repeat
	}
	if what == "shuffle" {
		s.snap.Shuffle = on
	} else {
		s.snap.Repeat = on
	}
	p.mu.Unlock()
	p.render(pane)

	fail := localeCommandFailed()
	p.action(pane, fail, func(ctx context.Context) error {
		if what == "shuffle" {
			return player.Shuffle(ctx, on)
		}
		return player.Repeat(ctx, on)
	})
}

// doCommand runs one of the player's commands.
func (p *plugin) doCommand(_ *sdk.Conn, pane, fail, command string) {
	player := p.player
	if player == nil {
		return
	}
	p.action(pane, fail, func(ctx context.Context) error {
		switch command {
		case "play-pause":
			return player.PlayPause(ctx)
		case "next":
			return player.Next(ctx)
		case "previous":
			return player.Previous(ctx)
		}
		return nil
	})
}

// command handles a palette command or a "kero do" action: the host sends
// the command to the process, which applies it to the first pane.
func (p *plugin) command(_ *sdk.Conn, id string) {
	p.mu.Lock()
	pane := ""
	for id := range p.panes {
		pane = id
		break
	}
	p.mu.Unlock()
	switch id {
	case "play-pause":
		p.doCommand(nil, pane, localeCommandFailed(), "play-pause")
	case "next":
		p.doCommand(nil, pane, localeCommandFailed(), "next")
	case "previous":
		p.doCommand(nil, pane, localeCommandFailed(), "previous")
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

		p.mu.Lock()
		s := p.panes[pane]
		if s == nil {
			p.mu.Unlock()
			return
		}
		s.signingIn = false
		if err != nil {
			s.notice = localeSignInFailed()
			s.lib = p.currentLibrary()
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
		p.mu.Unlock()
		p.render(pane)
		p.syncLibrary(pane)
	}()
}

// action runs one player command off the handler goroutine, reports a
// failure in the pane, and reads the player again after a success.
func (p *plugin) action(pane, fail string, fn func(context.Context) error) {
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
		p.readNow()
	}()
}

// readNow reads the player once, so a command shows its effect without
// waiting for the next poll.
func (p *plugin) readNow() {
	p.mu.Lock()
	player := p.player
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
	for _, s := range p.panes {
		s.snap = snap
		s.playback = playbackState(err, snap)
	}
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	for _, pane := range panes {
		p.render(pane)
	}
}

// launch starts Spotify, or brings it to the front. Every other script
// refuses to start the app, so this is the one that may.
func (p *plugin) launch(pane string) {
	player := p.player
	if player == nil {
		return
	}
	p.action(pane, localeCommandFailed(), func(ctx context.Context) error {
		return player.Launch(ctx)
	})
}

// openLink opens an http or https link in the browser.
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

// save keeps the tab and the search for the next start.
func (p *plugin) save(pane string) {
	p.mu.Lock()
	p.saveLocked(pane)
	p.mu.Unlock()
}

// saveLocked starts the host call that saves a pane's state. It is called
// with p.mu held, and the call runs in its own goroutine.
func (p *plugin) saveLocked(pane string) {
	s := p.panes[pane]
	conn := p.conn
	if s == nil || conn == nil {
		return
	}
	state := persisted{Tab: int(s.tab), Query: s.query}
	go func() {
		_ = conn.SaveState(pane, state)
	}()
}

// render sends a pane's tree to the host.
func (p *plugin) render(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	tree := p.tree(s)
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return
	}
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

// compact says whether a pane is small enough for the compact layout.
func compact(size protocol.Size) bool {
	return size.Width > 0 && size.Width < compactWidth || size.Height > 0 && size.Height < compactHeight
}
