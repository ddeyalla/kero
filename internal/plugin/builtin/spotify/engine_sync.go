package spotify

import (
	"context"
	"errors"
	"regexp"
	"time"

	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/sdk"
	"kero/locales"
)

// The notices of a play that failed, named once here as view.go names the
// others.
var (
	localePremiumRequired      = locales.SpotifyPremiumRequiredNotice
	localeSignInAgainNotice    = locales.SpotifySignInAgainNotice
	localePlaybackFailedDetail = locales.SpotifyPlaybackFailedDetail
)

// noticeClip is how many characters of Spotify's own words a notice shows.
const noticeClip = 80

// spotifyMessageRe finds the message in the JSON body of a Spotify error. The
// page clips the body, so it is read with a pattern and not parsed.
var spotifyMessageRe = regexp.MustCompile(`"message"\s*:\s*"([^"]*)"`)

// playbackNotice is the line a pane shows for an error of the engine's
// player: that the account needs Premium, that the user has to sign in
// again, or else what Spotify said, clipped. An error that says nothing
// gets the general line.
func playbackNotice(err error) string {
	var pe *engine.PlaybackError
	if !errors.As(err, &pe) {
		return localePlaybackFailed()
	}
	switch {
	case pe.PremiumRequired():
		return localePremiumRequired()
	case pe.SignInRequired():
		return localeSignInAgainNotice()
	}
	said := pe.Message
	if m := spotifyMessageRe.FindStringSubmatch(said); m != nil {
		said = m[1]
	}
	runes := []rune(said)
	if len(runes) > noticeClip {
		said = string(runes[:noticeClip]) + "…"
	}
	if said == "" {
		return localePlaybackFailed()
	}
	return localePlaybackFailedDetail(said)
}

// playerEngine is the player that plays in the app itself: the engine. A
// test replaces newEngine with a fake.
type playerEngine interface {
	transport.Player
	Start() error
	Close() error
}

// contextPlayer is a player that starts a track inside its album or
// playlist. The engine is one, and the desktop app is another.
type contextPlayer interface {
	PlayFrom(ctx context.Context, contextURI, trackURI string) error
}

// requestPlayer is a player that carries the plugin's identity for an
// accepted play, so that a later page error can be matched to it.
type requestPlayer interface {
	PlayRequest(ctx context.Context, uri, request string) error
}

// requestContextPlayer is a requestPlayer that also starts a track inside
// its album or playlist.
type requestContextPlayer interface {
	PlayFromRequest(ctx context.Context, contextURI, trackURI, request string) error
}

// playRequest starts uri, with its request identity when the player takes
// one. A player that does not (the desktop app) plays as before.
func playRequest(ctx context.Context, player transport.Player, uri, request string) error {
	if rp, ok := player.(requestPlayer); ok {
		return rp.PlayRequest(ctx, uri, request)
	}
	return player.Play(ctx, uri)
}

// playFromRequest starts trackURI inside contextURI, with its request
// identity when the player takes one.
func playFromRequest(ctx context.Context, player transport.Player, contextURI, trackURI, request string) error {
	if rp, ok := player.(requestContextPlayer); ok {
		return rp.PlayFromRequest(ctx, contextURI, trackURI, request)
	}
	if cp, ok := player.(contextPlayer); ok {
		return cp.PlayFrom(ctx, contextURI, trackURI)
	}
	return player.Play(ctx, trackURI)
}

// syncEngine makes the engine the player while the user can stream, and the
// desktop app the player otherwise. The user can stream when the Web API
// client holds tokens with the streaming scope: a sign-in before the engine
// has none, and signs in once more.
func (p *plugin) syncEngine() {
	p.engineMu.Lock()
	defer p.engineMu.Unlock()

	p.mu.Lock()
	library, conn, running := p.library, p.conn, p.engine
	p.mu.Unlock()
	// Asking reads the saved tokens, which is a host call, so it happens
	// outside p.mu.
	can := library != nil && library.CanStream()
	p.mu.Lock()
	p.streamable = can
	p.mu.Unlock()
	switch {
	case can && running == nil:
		p.startEngine(conn, library)
	case !can && running != nil:
		p.stopEngine()
	}
}

// startEngine makes the engine the player. The engine opens its web view
// at the first play, and lets it go when nothing plays for a while, so a
// signed-in pane that only shows Spotify costs no web view. A pane shows
// the player meanwhile: stopped, with the track played last.
func (p *plugin) startEngine(conn *sdk.Conn, library *web.Client) {
	var eng *lazyEngine
	var gen int
	eng = newLazyEngine(func(emit func(transport.Snapshot), onError func(error)) playerEngine {
		return newEngine(conn, library, emit, onError)
	}, func(snap transport.Snapshot) {
		p.playbackFor(eng, gen, snap)
	}, func(err error) {
		p.playbackErrorFor(eng, gen, err)
	}, func() { p.engineIdled(eng, gen) })
	if err := eng.Start(); err != nil {
		p.playbackError(err)
		return
	}
	p.mu.Lock()
	p.engineGen++
	gen = p.engineGen
	p.engine = eng
	p.player = eng
	monitor := p.monitor
	p.monitor = nil
	for _, s := range p.panes {
		s.playback = modeStarting
		s.last = nil
	}
	p.last, p.lastTried, p.played = nil, false, false
	panes := p.paneIDsLocked()
	p.mu.Unlock()
	if engineEager {
		_, _ = eng.wake(context.Background())
	}
	if monitor != nil {
		_ = monitor.Close()
	}
	for _, pane := range panes {
		p.render(pane)
	}
	// One read shows what the engine holds already, such as a track it
	// plays since an earlier pane.
	go p.readNow()
	p.armConnect()
}

// stopEngine ends the engine and gives the desktop app the player again.
func (p *plugin) stopEngine() {
	p.mu.Lock()
	eng := p.engine
	p.engine = nil
	p.engineGen++
	if p.desktop != nil {
		p.player = p.desktop
	} else {
		p.player = nil
	}
	for _, s := range p.panes {
		s.playback = modeStarting
	}
	p.mu.Unlock()
	if eng != nil {
		_ = eng.Close()
	}
	p.runMonitor()
}

// engineIdled shows the player as stopped after its web view went away on
// its own, and lets the pane ask for the track played last again, as the
// player that never played does.
func (p *plugin) engineIdled(player transport.Player, gen int) {
	p.mu.Lock()
	if player != nil && (p.player != player || p.engineGen != gen) {
		p.mu.Unlock()
		return
	}
	p.played, p.lastTried = false, false
	p.mu.Unlock()
	p.playbackFor(player, gen, transport.Snapshot{State: transport.StateStopped, ReadAt: time.Now()})
}

// armConnect gives the engine connectTimeout to report ready. A pane that
// still connects then shows the player as unavailable, which a later state
// of the engine replaces.
func (p *plugin) armConnect() {
	p.mu.Lock()
	gen := p.engineGen
	p.mu.Unlock()
	afterFunc(connectTimeout, func() {
		p.mu.Lock()
		if p.engineGen != gen {
			// The engine that this timer waited for is gone.
			p.mu.Unlock()
			return
		}
		var expired []string
		for pane, s := range p.panes {
			if s.playback == modeStarting && s.guess == nil {
				s.playback = modeUnavailable
				expired = append(expired, pane)
			}
		}
		p.mu.Unlock()
		for _, pane := range expired {
			p.render(pane)
		}
	})
}

// settleLocked shows a read or a pushed state in a pane, and is called with
// p.mu held. A state that says what the pane shows already keeps the
// pane's own position and instant, so that its tree does not change. An engine that is ready and holds no track has a new device
// with no state until something plays, so the pane shows the last track
// instead of an empty player, when it knows one.
func (p *plugin) settleLocked(s *session, err error, snap transport.Snapshot) {
	if err == nil {
		// A press on shuffle or repeat stands until Spotify reports it.
		s.holdFlags(&snap)
	}
	mode := playbackState(err, snap)
	if mode == modePlaying {
		// A track plays: a later idle state has no use for the old answer.
		p.played, p.last = true, nil
	}
	s.last = nil
	if mode == modeIdle && p.engine != nil && p.last != nil {
		s.showLast(p.last, snap)
		return
	}
	s.coalesce(&snap)
	s.snap, s.playback = snap, mode
}

// showLast makes a pane show a track as paused, with the volume and the
// shuffle and repeat that the engine reports.
func (s *session) showLast(t *web.Track, from transport.Snapshot) {
	s.last = t
	s.snap = transport.Snapshot{
		State:      transport.StatePaused,
		Track:      t.Name,
		URI:        t.URI,
		Artist:     t.ArtistLine(),
		Album:      t.Album,
		ArtworkURL: t.Image,
		Duration:   t.Duration,
		Position:   t.Position,
		Volume:     from.Volume,
		Shuffle:    from.Shuffle,
		Repeat:     from.Repeat,
		ReadAt:     time.Now(),
	}
	s.playback = modePlaying
}

// wantLastLocked tells whether the plugin should ask for the last track: a
// ready engine, with nothing to play, that has not asked yet. It is called
// with p.mu held.
func (p *plugin) wantLastLocked(snap transport.Snapshot) bool {
	if p.engine == nil || p.lastTried || p.played || p.library == nil {
		return false
	}
	if snap.Track != "" || snap.State != transport.StateStopped {
		return false
	}
	p.lastTried = true
	return true
}

// fetchLast asks Spotify what to show in an idle player: first what plays on
// another device, then the track played last. Any failure shows nothing,
// quietly: a sign-in from before the recently-played scope gets the empty
// player, and is not asked to sign in again.
func (p *plugin) fetchLast() {
	p.mu.Lock()
	library, gen := p.library, p.engineGen
	p.mu.Unlock()
	if library == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), libraryTimeout)
	defer cancel()
	track, err := library.CurrentTrack(ctx)
	if err != nil || track == nil {
		track, err = library.LastPlayed(ctx)
	}
	if err != nil || track == nil {
		// A failure is not worth a notice: the pane shows nothing playing.
		return
	}
	p.mu.Lock()
	if p.engine == nil || p.engineGen != gen || p.played {
		p.mu.Unlock()
		return
	}
	p.last = track
	var shown []string
	for pane, s := range p.panes {
		if s.playback == modeIdle && s.guess == nil {
			s.showLast(track, s.snap)
			shown = append(shown, pane)
		}
	}
	p.mu.Unlock()
	for _, pane := range shown {
		p.render(pane)
	}
}

// playLast starts the track a pane shows in place of a player with
// nothing to play, and tells whether it did. A track from the history
// starts inside its album or playlist; a track of another device is a
// transfer, which the engine's ordinary play does, so that case is left to
// the caller.
func (p *plugin) playLast(pane, fail string) bool {
	p.mu.Lock()
	s := p.panes[pane]
	var last *web.Track
	if s != nil && s.snap.State == transport.StatePaused {
		last = s.last
	}
	player := p.player
	p.mu.Unlock()
	if last == nil || last.OtherDevice {
		return false
	}
	p.toggleShown()
	p.action(pane, fail, func(ctx context.Context) error {
		var err error
		if cp, ok := player.(contextPlayer); ok && last.ContextURI != "" {
			err = cp.PlayFrom(ctx, last.ContextURI, last.URI)
		} else {
			err = player.Play(ctx, last.URI)
		}
		if err != nil {
			p.mu.Lock()
			p.revertGuessLocked()
			p.mu.Unlock()
		}
		return err
	})
	return true
}
