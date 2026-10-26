package spotify

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
)

// This file holds what a pane does with the tracks of one playlist or
// album (the drill-in), with the queue (the Up next tab), and with the
// guesses of shuffle and repeat. view.go draws them.

const (
	// kindPlaylist and kindAlbum are the first part of a row's id, and of
	// its URI after "spotify:".
	kindPlaylist = "playlist"
	kindAlbum    = "album"
)

// drillable tells whether a row is an album or a playlist, whose tracks
// the pane can list.
func (e entry) drillable() bool {
	return strings.HasPrefix(e.id, kindPlaylist+"-") || strings.HasPrefix(e.id, kindAlbum+"-")
}

// splitDrill is the kind and the Spotify id of a drill id such as
// "playlist-37i9".
func splitDrill(id string) (kind, spotifyID string) {
	kind, spotifyID, _ = strings.Cut(id, "-")
	return kind, spotifyID
}

// restoreDrill puts back the drill a pane had before a restart. An id that
// is not a playlist or an album is left out.
func (s *session) restoreDrill(id, name string) {
	kind, spotifyID := splitDrill(id)
	if spotifyID == "" || (kind != kindPlaylist && kind != kindAlbum) {
		return
	}
	s.drill, s.drillName = id, name
	s.drillURI = "spotify:" + kind + ":" + spotifyID
	s.tracksState = tracksLoading
}

// coverOf finds the cover of a playlist or album row the pane has loaded.
func (s *session) coverOf(id string) string {
	_, spotifyID := splitDrill(id)
	for _, playlist := range s.playlists {
		if playlist.ID == spotifyID && id == "playlist-"+spotifyID {
			return playlist.Artwork()
		}
	}
	for _, album := range s.albums {
		if album.ID == spotifyID && id == "album-"+spotifyID {
			return album.Artwork()
		}
	}
	for _, playlist := range s.search.Playlists {
		if id == "playlist-"+playlist.ID {
			return playlist.Artwork()
		}
	}
	for _, album := range s.search.Albums {
		if id == "album-"+album.ID {
			return album.Artwork()
		}
	}
	return ""
}

// openDrill shows the tracks of a pressed playlist or album. The last
// answer for them, when the client has one, is on screen at once; the load
// replaces it.
func (p *plugin) openDrill(pane string, picked entry) {
	p.mu.Lock()
	s := p.panes[pane]
	library := p.library
	if s == nil || !picked.drillable() {
		p.mu.Unlock()
		return
	}
	s.drill, s.drillName, s.drillURI, s.drillImage = picked.id, picked.title, picked.uri, picked.image
	s.tracks, s.tracksState, s.needScope = nil, tracksLoading, false
	s.tracksPending = false
	if library != nil {
		kind, spotifyID := splitDrill(picked.id)
		var cached []web.ListTrack
		var ok bool
		if kind == kindPlaylist {
			cached, ok = library.CachedPlaylistTracks(spotifyID)
		} else {
			cached, ok = library.CachedAlbumTracks(spotifyID)
		}
		if ok {
			s.tracks, s.tracksState = cached, tracksReady
		}
	}
	p.saveLocked(pane)
	p.mu.Unlock()
	p.render(pane)
	p.loadTracks(pane)
}

// closeDrill goes back from the tracks to the tabs.
func (p *plugin) closeDrill(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || s.drill == "" {
		p.mu.Unlock()
		return
	}
	s.clearDrill()
	p.saveLocked(pane)
	p.mu.Unlock()
	p.render(pane)
}

// clearDrill leaves the drill-in. The tracks of an earlier load that is
// still in flight are dropped by their number.
func (s *session) clearDrill() {
	s.drill, s.drillName, s.drillURI, s.drillImage = "", "", "", ""
	s.tracks, s.tracksState, s.needScope = nil, tracksLoading, false
	s.tracksPending = false
	s.tracksSeq++
}

// playDrill plays the whole playlist or album of the drill-in.
func (p *plugin) playDrill(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil || s.drill == "" {
		p.mu.Unlock()
		return
	}
	picked := entry{id: s.drill, title: s.drillName, uri: s.drillURI, image: s.drillImage}
	if picked.image == "" {
		picked.image = s.coverOf(s.drill)
	}
	p.mu.Unlock()
	p.playEntry(pane, picked)
}

// loadTracks loads the tracks of the pane's drill-in. A playlist that
// Spotify does not share (403) is a state of its own, not a failure of the
// library: the Play button still plays it.
func (p *plugin) loadTracks(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	library := p.library
	if s == nil || s.drill == "" || library == nil {
		p.mu.Unlock()
		return
	}
	s.tracksSeq++
	seq, id := s.tracksSeq, s.drill
	s.tracksPending, s.tracksState = true, tracksLoading
	cover := s.drillImage
	if cover == "" {
		cover = s.coverOf(id)
	}
	p.mu.Unlock()

	go func() {
		kind, spotifyID := splitDrill(id)
		ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
		defer cancel()
		var (
			tracks []web.ListTrack
			err    error
		)
		if kind == kindPlaylist {
			tracks, err = library.PlaylistTracks(ctx, spotifyID)
		} else {
			tracks, err = library.AlbumTracks(ctx, spotifyID, cover)
		}
		// Reading the saved tokens is a host call, so it happens outside
		// p.mu.
		forbidden := errors.Is(err, web.ErrForbidden)
		needScope := forbidden && !library.HasScope(web.ScopeCollaborative)

		p.mu.Lock()
		s := p.panes[pane]
		if s == nil || s.tracksSeq != seq || s.drill != id {
			p.mu.Unlock()
			return
		}
		lib := libraryError(err)
		s.tracksPending = false
		s.tracksError = lib
		switch {
		case err == nil:
			s.tracks, s.tracksState = tracks, tracksReady
		case keepsResults(lib):
			// A failed read is local to this list. Keep the open album or
			// playlist and its cached tracks, rather than replace it with
			// the library and mistake an empty failed read for no tracks.
			s.tracks, s.tracksState = tracks, tracksFailed
		case forbidden:
			s.tracks, s.tracksState, s.needScope = nil, tracksForbidden, needScope
		default:
			s.tracksState = tracksFailed
			if lib != libFailed {
				s.lib = lib
			}
		}
		p.mu.Unlock()
		p.render(pane)
	}()
}

// selectTrack plays the pressed row of the tracks, or of the queue. A row
// of a drill-in starts inside its playlist or album, so that the next track
// is the next of the list; a row of the queue plays alone. Spotify has no
// call that skips to a position in the queue, and one Next at a time races
// the queue the player is on, so the row starts the track it names now, and
// the pane reads the queue again when that track plays.
func (p *plugin) selectTrack(e protocol.EventParams, queue bool) {
	id, _ := e.Value.(string)
	p.mu.Lock()
	s := p.panes[e.Pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	list, contextURI := s.tracks, s.drillURI
	prefix := "t-"
	if queue {
		list, contextURI, prefix = s.queue, "", "q-"
	}
	var picked web.ListTrack
	found := false
	for i, track := range list {
		if prefix+strconv.Itoa(i) == id {
			picked, found = track, true
			break
		}
	}
	p.mu.Unlock()
	if !found {
		return
	}
	p.playTrack(e.Pane, picked, contextURI)
}

// playTrack plays a track, inside its context when there is one and the
// player can start a track inside a context. The pane shows the track as
// playing at once, as it does for a tile.
func (p *plugin) playTrack(pane string, track web.ListTrack, contextURI string) {
	uri := track.URI
	row := entry{title: track.Name, subtitle: track.ArtistLine(), image: track.Artwork}
	req, seq, ctx, cancel, ok := p.beginPlay(pane, row, uri, uri)
	if !ok {
		return
	}
	request := strconv.FormatUint(req, 10)
	p.actionWithContext(ctx, cancel, pane, localePlayFailed(), func(ctx context.Context) error {
		var err error
		if contextURI != "" {
			err = p.playFromRequest(ctx, req, contextURI, uri, request)
		} else {
			err = p.playRequest(ctx, req, uri, request)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = errPlayTimeout
		}
		return err
	}, p.finishPlay(req, uri, seq))
}

// loadQueue reads the tracks that come next. The queue is not cached by the
// client: it changes with every skip, so the pane shows the last answer
// while it asks again.
func (p *plugin) loadQueue(pane string) {
	p.mu.Lock()
	s := p.panes[pane]
	library := p.library
	if s == nil || library == nil {
		p.mu.Unlock()
		return
	}
	s.queueSeq++
	seq := s.queueSeq
	s.queuePending, s.queueFailed = true, false
	p.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), libraryTimeout)
		defer cancel()
		_, queue, err := library.Queue(ctx)

		p.mu.Lock()
		s := p.panes[pane]
		if s == nil || s.queueSeq != seq {
			p.mu.Unlock()
			return
		}
		s.queuePending = false
		if err == nil {
			s.queue, s.queueLoaded, s.queueFailed = queue, true, false
		} else {
			s.queueLoaded, s.queueFailed = true, true
			if lib := libraryError(err); lib != libFailed && !keepsResults(lib) {
				s.lib = lib
			}
		}
		p.mu.Unlock()
		p.render(pane)
	}()
}

// refreshShown brings a pane that shows again up to date: its playlists or
// albums when they loaded more than libraryFresh ago, and its queue.
func (p *plugin) refreshShown(pane string) {
	if p.libraryState() != libReady {
		return
	}
	p.mu.Lock()
	s := p.panes[pane]
	if s == nil {
		p.mu.Unlock()
		return
	}
	s.leaveQueue()
	tab := s.tab
	p.mu.Unlock()
	switch tab {
	case tabPlaylists, tabAlbums:
		p.loadTab(pane, tab)
	case tabQueue:
		p.loadQueue(pane)
	}
}

// signOut forgets the tokens and shows every pane signed out. The Client ID
// stays, so signing in again is one press. The engine stops with its
// sign-in, and the panes ask to sign in. It is the command of the Sign Out
// button in Settings, so it needs no pane.
func (p *plugin) signOut() {
	p.mu.Lock()
	library := p.library
	p.mu.Unlock()
	if library == nil {
		return
	}
	go func() {
		// Forgetting the tokens waits for a sign-in or a refresh in
		// flight, and writes the host's secrets.
		err := library.SignOut()
		lib := libraryModeOf(library)

		p.mu.Lock()
		for id, s := range p.panes {
			if err != nil {
				s.notice = localeCommandFailed()
				continue
			}
			s.lib = lib
			s.notice = ""
			s.playlists, s.albums, s.search, s.entries = nil, nil, web.SearchResult{}, nil
			s.queue, s.queueLoaded, s.queueFailed = nil, false, false
			s.queuePending = false
			s.loaded = make(map[tabKind]bool)
			s.loadedAt = nil
			s.libSeq++
			s.searchSeq++
			s.queueSeq++
			s.loading = false
			s.clearDrill()
			p.saveLocked(id)
		}
		p.prefetched = false
		p.last, p.lastTried, p.played = nil, false, false
		panes := p.paneIDsLocked()
		p.mu.Unlock()
		// Without the streaming scope the engine stops. When forgetting
		// the tokens failed, the engine has to follow the state the client
		// is in now: it may have lost the tokens in memory.
		p.syncEngine()
		for _, id := range panes {
			p.render(id)
		}
	}()
}

// holdFlags makes a state that Spotify reports show the value a press on
// shuffle or repeat asked for, while that press stands, and ends the guess
// when the report agrees with it. Without this the button would flip, flip
// back and flip again while the command is on its way.
func (s *session) holdFlags(snap *transport.Snapshot) {
	if g := s.shuffleGuess; g != nil {
		if snap.Shuffle == g.on {
			s.shuffleGuess = nil
		} else {
			g.real, snap.Shuffle = snap.Shuffle, g.on
		}
	}
	if g := s.repeatGuess; g != nil {
		if snap.Repeat == g.on {
			s.repeatGuess = nil
		} else {
			g.real, snap.Repeat = snap.Repeat, g.on
		}
	}
}

// coalesce makes a state that says what the pane shows already carry the
// pane's own position and instant. The engine reports the same track, in
// the same state, for reasons such as a volume change; each report has a new
// read time, which would be a new tree. A position within seekSettle of
// where the pane has the track by now is the same position.
func (s *session) coalesce(snap *transport.Snapshot) {
	old := s.snap
	if old.URI == "" || old.ReadAt.IsZero() {
		return
	}
	same, now := old, *snap
	same.Position, same.ReadAt = 0, time.Time{}
	now.Position, now.ReadAt = 0, time.Time{}
	if same != now {
		return
	}
	expected := old.Position
	if snap.State == transport.StatePlaying {
		expected += snap.ReadAt.Sub(old.ReadAt)
	}
	if diff := snap.Position - expected; diff < -seekSettle || diff > seekSettle {
		return
	}
	snap.Position, snap.ReadAt = old.Position, old.ReadAt
}
