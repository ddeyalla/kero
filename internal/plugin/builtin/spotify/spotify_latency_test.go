package spotify

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/protocol"
)

// gatedEngine is an engine whose Play waits for the test, to see a press
// while its call has not returned. It is for the dedupe of presses only: the
// real engine returns from Play at once.
type gatedEngine struct {
	*fakeEngine
	gate chan struct{}
}

func (g *gatedEngine) Play(ctx context.Context, uri string) error {
	_ = g.fakeEngine.record("play " + uri)
	<-g.gate
	return nil
}

// timers holds the guess timers that the plugin armed, for a test to fire.
type timers struct {
	mu  sync.Mutex
	set []time.Duration
	fns []func()
}

func (t *timers) after(d time.Duration, f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.set = append(t.set, d)
	t.fns = append(t.fns, f)
}

// fire runs the n-th timer armed, counted from 0.
func (t *timers) fire(n int) {
	t.mu.Lock()
	f := t.fns[n]
	t.mu.Unlock()
	f()
}

func (t *timers) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.fns)
}

// useTimers replaces the guess timer for one test.
func useTimers(t *testing.T) *timers {
	t.Helper()
	ts := &timers{}
	old := afterFunc
	afterFunc = ts.after
	t.Cleanup(func() { afterFunc = old })
	return ts
}

var pressRow = protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"}

// pushedPlugin is a plugin with one pane that plays "Test Song" (track
// "spotify:track:old"), a row to press, and an engine that acts like the
// real one: Play returns at once, a failure comes later as an error of the
// page, and the player's state comes by a push.
func pushedPlugin(t *testing.T) (*plugin, *session, *fakeEngine, *timers) {
	t.Helper()
	ts := useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	f := &fakeEngine{emit: p.playback, onError: p.playbackError}
	p.engine, p.player = f, f
	s.snap.URI = "spotify:track:old"
	s.entries = []entry{{id: "album-a1", title: "Album One", subtitle: "Artist One", image: "https://i/640", uri: "spotify:album:a1"}}
	return p, s, f, ts
}

// pressed presses the row and waits until the engine took the play, which
// is the whole of the press: the engine answers later.
func pressed(t *testing.T, p *plugin, f *fakeEngine) {
	t.Helper()
	p.event(nil, pressRow)
	eventually(t, "the play to end", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == "" && len(f.calls()) == 1
	})
}

func TestPressingARowShowsItAtOnce(t *testing.T) {
	p, s, _, ts := pushedPlugin(t)
	// The track is paused: the button must change to pause.
	p.mu.Lock()
	s.snap.State = transport.StatePaused
	p.mu.Unlock()

	p.event(nil, pressRow)

	// The engine has not pushed anything, and the pane shows the row.
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Album One" || s.snap.Artist != "Artist One" || s.snap.ArtworkURL != "https://i/640" {
		t.Fatalf("snap = %+v, want the pressed row", s.snap)
	}
	if s.playIcon() != "pause" {
		t.Errorf("play icon = %q, want pause while it loads", s.playIcon())
	}
	if s.guess == nil || s.guess.snap.Track != "Test Song" {
		t.Errorf("guess = %+v, want the track it replaced", s.guess)
	}
	if got := texts(draw(t, p, s)); !strings.Contains(got, "Album One") {
		t.Errorf("tree = %q, want the row's title", got)
	}
	if ts.count() != 1 || ts.set[0] != guessTimeout {
		t.Errorf("timers = %v, want one of %v", ts.set, guessTimeout)
	}
}

func TestASecondPressOnTheSameRowIsIgnoredWhilePending(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	g := &gatedEngine{fakeEngine: &fakeEngine{}, gate: make(chan struct{})}
	p.engine, p.player = g, g
	s.entries = []entry{{id: "album-a1", title: "Album One", subtitle: "Artist One", image: "https://i/640", uri: "spotify:album:a1"}}
	released := false
	release := func() {
		if !released {
			released = true
			close(g.gate)
		}
	}
	t.Cleanup(release)

	p.event(nil, pressRow)
	p.event(nil, pressRow)
	p.event(nil, pressRow)
	eventually(t, "the first play", func() bool { return len(g.calls()) >= 1 })
	release()
	eventually(t, "the press to end", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.playing == ""
	})
	if calls := g.calls(); len(calls) != 1 || calls[0] != "play spotify:album:a1" {
		t.Fatalf("calls = %v, want one play", calls)
	}

	// Once it ended, the row can be played again.
	p.event(nil, pressRow)
	eventually(t, "the second play", func() bool { return len(g.calls()) == 2 })
}

func TestALaterErrorOfThePageBringsThePaneBack(t *testing.T) {
	p, s, f, _ := pushedPlugin(t)
	pressed(t, p, f)
	p.mu.Lock()
	if s.snap.Track != "Album One" {
		p.mu.Unlock()
		t.Fatalf("snap = %+v, want the guess to stand after Play returned", s.snap)
	}
	p.mu.Unlock()

	// The page reports the failure of the command, as the engine passes it
	// on. The press's own goroutine may still clear a notice as it ends, so
	// the error comes until the notice holds.
	eventually(t, "the notice", func() bool {
		f.onError(errors.New("spotify engine: command: Spotify answered 403"))
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice == localePlaybackFailed()
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Test Song" || s.guess != nil {
		t.Errorf("snap = %+v, guess = %+v, want the earlier track back", s.snap, s.guess)
	}
	if p.playing != "" {
		t.Errorf("playing = %q, want the row free to press again", p.playing)
	}
}

func TestAFailedPlayCallBringsThePaneBack(t *testing.T) {
	useTimers(t)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	f := &failingEngine{fakeEngine: &fakeEngine{}}
	p.engine, p.player = f, f
	s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}

	p.event(nil, pressRow)
	eventually(t, "the notice", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice == localePlayFailed()
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Test Song" || s.guess != nil {
		t.Errorf("snap = %+v, guess = %+v, want the earlier track back", s.snap, s.guess)
	}
}

// failingEngine is an engine whose Play refuses at once, as one that is
// closed does.
type failingEngine struct{ *fakeEngine }

func (failingEngine) Play(context.Context, string) error { return engine.ErrClosed }

func TestAStateOfANewTrackReplacesTheGuess(t *testing.T) {
	p, s, f, ts := pushedPlugin(t)
	pressed(t, p, f)

	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "First Track of the Album", URI: "spotify:track:first", Volume: 70})

	p.mu.Lock()
	if s.snap.Track != "First Track of the Album" || s.guess != nil {
		t.Errorf("snap = %+v, guess = %+v, want the player's state", s.snap, s.guess)
	}
	p.mu.Unlock()

	// The timer of the answered guess does nothing.
	ts.fire(0)
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "First Track of the Album" {
		t.Errorf("snap = %+v, want the timer of an answered guess to leave the state", s.snap)
	}
}

func TestStatesOfTheOldTrackLeaveTheGuess(t *testing.T) {
	p, s, f, _ := pushedPlugin(t)
	pressed(t, p, f)

	// The volume echo and a late state of the track that played before.
	old := transport.Snapshot{State: transport.StatePlaying, Track: "Test Song", URI: "spotify:track:old", Volume: 30}
	f.hold(old)
	f.emit(transport.Snapshot{State: transport.StateStopped, Volume: 30})

	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Album One" || s.guess == nil {
		t.Fatalf("snap = %+v, guess = %+v, want the guess to stand", s.snap, s.guess)
	}
	if s.snap.Volume != 30 {
		t.Errorf("volume = %d, want the echo's 30", s.snap.Volume)
	}
	if s.guess.snap.Track != "Test Song" {
		t.Errorf("guess = %+v, want the earlier track to come back to", s.guess.snap)
	}
}

func TestAGuessThatNoStateAnswersGoesBackOnTimeout(t *testing.T) {
	p, s, f, ts := pushedPlugin(t)
	pressed(t, p, f)

	ts.fire(0)

	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Test Song" || s.guess != nil {
		t.Errorf("snap = %+v, guess = %+v, want the earlier track back", s.snap, s.guess)
	}
}

func TestAPressOfPlayPauseIsAGuessToo(t *testing.T) {
	p, s, f, ts := pushedPlugin(t)
	press := protocol.EventParams{Pane: "p1", Node: "play", Kind: protocol.KindPress}

	p.event(nil, press)
	p.mu.Lock()
	if s.snap.State != transport.StatePaused || s.guess == nil {
		p.mu.Unlock()
		t.Fatalf("snap = %+v, guess = %+v, want paused by guess", s.snap, s.guess)
	}
	p.mu.Unlock()

	// A state that still says playing does not answer the press.
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Test Song", URI: "spotify:track:old", Volume: 70})
	p.mu.Lock()
	if s.snap.State != transport.StatePaused || s.guess == nil {
		p.mu.Unlock()
		t.Fatalf("snap = %+v, guess = %+v, want the guess to stand", s.snap, s.guess)
	}
	p.mu.Unlock()

	// The page reports that the command failed.
	f.onError(errors.New("spotify engine: command: timed out"))
	p.mu.Lock()
	if s.snap.State != transport.StatePlaying || s.guess != nil {
		p.mu.Unlock()
		t.Fatalf("snap = %+v, guess = %+v, want playing back", s.snap, s.guess)
	}
	p.mu.Unlock()

	// And a flip nothing answers goes back on the timeout.
	p.event(nil, press)
	if ts.count() != 2 {
		t.Fatalf("timers = %d, want one for each flip", ts.count())
	}
	ts.fire(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.State != transport.StatePlaying || s.guess != nil {
		t.Errorf("snap = %+v, guess = %+v, want playing back after the timeout", s.snap, s.guess)
	}
}

// libraryCounts is a server of the library that counts its requests, and
// can hold them back.
func libraryCounts(t *testing.T, hold chan struct{}) (http.Handler, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var playlists, albums, searches atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me/playlists":
			playlists.Add(1)
			if hold != nil {
				<-hold
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"p1","name":"One","owner":{"display_name":"Ada","id":"ada"}}]}`))
		case "/v1/me/albums":
			albums.Add(1)
			_, _ = w.Write([]byte(`{"items":[{"album":{"id":"a1","uri":"spotify:album:a1","name":"Album One","artists":[{"name":"Artist"}]}}]}`))
		case "/v1/search":
			searches.Add(1)
			if hold != nil {
				<-hold
			}
			_, _ = w.Write([]byte(`{"albums":{"items":[{"id":"a1","uri":"spotify:album:a1","name":"Album One","artists":[{"name":"Artist"}]}]},"playlists":{"items":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}), &playlists, &albums, &searches
}

func TestThePrefetchGivesATabItsFirstPaint(t *testing.T) {
	handler, playlists, albums, _ := libraryCounts(t, nil)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.library = libraryClient(t, handler)
	s.tab = tabAlbums

	p.prefetchLibrary()
	p.prefetchLibrary() // a second call does not ask again
	eventually(t, "both lists", func() bool {
		_, okPlaylists := p.library.LastPlaylists()
		_, okAlbums := p.library.LastAlbums()
		return okPlaylists && okAlbums
	})
	if playlists.Load() != 1 || albums.Load() != 1 {
		t.Fatalf("requests = %d playlists, %d albums, want one each", playlists.Load(), albums.Load())
	}

	// The tab shows the prefetched list before its own load answers.
	p.loadTab("p1", tabPlaylists)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(s.entries) != 1 || s.entries[0].id != "playlist-p1" {
		t.Fatalf("entries = %+v, want the prefetched playlists at once", s.entries)
	}
}

func TestPanesShareOneLibraryRequest(t *testing.T) {
	hold := make(chan struct{})
	handler, playlists, _, _ := libraryCounts(t, hold)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.library = libraryClient(t, handler)
	p.panes["p2"] = twinPane(s, "p2")
	released := false
	t.Cleanup(func() {
		if !released {
			close(hold)
		}
	})

	p.loadTab("p1", tabPlaylists)
	p.loadTab("p2", tabPlaylists)
	eventually(t, "the request", func() bool { return playlists.Load() >= 1 })
	close(hold)
	released = true
	eventually(t, "both panes to load", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		second := p.panes["p2"]
		return !s.loading && !second.loading && len(s.entries) == 1 && len(second.entries) == 1
	})
	if got := playlists.Load(); got != 1 {
		t.Errorf("requests = %d, want one for the two panes", got)
	}
}

func TestARepeatedSearchShowsItsResultsAtOnce(t *testing.T) {
	hold := make(chan struct{})
	handler, _, _, searches := libraryCounts(t, nil)
	var gate atomic.Bool
	gated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate.Load() {
			<-hold
		}
		handler.ServeHTTP(w, r)
	})
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.library = libraryClient(t, gated)
	t.Cleanup(func() { close(hold) })

	s.query = "one"
	p.runSearch("p1", "one")
	eventually(t, "the first results", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !s.loading && len(s.entries) == 1
	})

	// Type something else, then "one" again, with the network held back.
	gate.Store(true)
	p.mu.Lock()
	s.query = "o"
	s.rebuild()
	empty := len(s.entries)
	s.query = "one"
	p.mu.Unlock()
	if empty != 0 {
		t.Fatalf("entries = %d for another query, want none", empty)
	}
	p.runSearch("p1", "one")
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(s.entries) != 1 || s.entries[0].title != "Album One" {
		t.Fatalf("entries = %+v, want the cached results before the answer", s.entries)
	}
	if got := searches.Load(); got < 1 {
		t.Errorf("searches = %d", got)
	}
}
