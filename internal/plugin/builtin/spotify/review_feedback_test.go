package spotify

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/locales"
)

func TestRetiredEngineCallbacksCannotReplaceCurrentPlayback(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "streaming user-read-email")
	p.syncEngine()
	wakeEngine(t, p, f)
	retired := p.engine
	t.Cleanup(func() { _ = retired.Close() })
	p.mu.Lock()
	p.engineGen++
	p.engine = &fakeEngine{ready: true}
	p.player = p.engine
	s.snap = transport.Snapshot{State: transport.StatePlaying, URI: "spotify:track:current", Track: "Current"}
	seq := p.playbackSeq
	p.mu.Unlock()
	// A callback already dispatched by the old engine can arrive between
	// replacing the player and closing the retired engine outside p.mu.
	f.hold(transport.Snapshot{State: transport.StatePlaying, URI: "spotify:track:old", Track: "Old"})
	f.onError(errors.New("retired error"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Track != "Current" || s.notice != "" || p.playbackSeq != seq {
		t.Fatalf("retired callback changed current state: track=%q notice=%q seq=%d want %d", s.snap.Track, s.notice, p.playbackSeq, seq)
	}
}

func TestRetryOfEmptySearchRendersPrompt(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.mu.Lock()
	s.tab, s.query, s.lib = tabSearch, "", libNetworkDown
	p.conn = &sdk.Conn{}
	p.mu.Unlock()
	count := countRenders(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "state-offline", Kind: protocol.KindPress})
	if count() == 0 {
		t.Fatal("retry changed the empty search state without rendering it")
	}
}

func TestRetryDoesNotDuplicatePendingListRead(t *testing.T) {
	for _, queue := range []bool{false, true} {
		t.Run(map[bool]string{false: "tracks", true: "queue"}[queue], func(t *testing.T) {
			p, s, _, _ := drillPlugin(t)
			var calls atomic.Int32
			started, release := make(chan struct{}, 10), make(chan struct{})
			handler := tracksHandler(nil)
			p.library = libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				started <- struct{}{}
				<-release
				handler.ServeHTTP(w, r)
			}))
			defer close(release)
			p.mu.Lock()
			if queue {
				s.tab, s.queueLoaded, s.queueFailed = tabQueue, true, true
			} else {
				s.restoreDrill("album-a1", "One")
				s.tracksState = tracksFailed
			}
			p.mu.Unlock()
			event := protocol.EventParams{Pane: "p1", Node: "state-tracks-failed", Kind: protocol.KindPress}
			p.event(nil, event)
			<-started
			p.mu.Lock()
			seq := s.tracksSeq
			if queue {
				seq = s.queueSeq
			}
			p.mu.Unlock()
			for range 10 {
				p.event(nil, event)
			}
			p.mu.Lock()
			after := s.tracksSeq
			if queue {
				after = s.queueSeq
			}
			p.mu.Unlock()
			if after != seq || calls.Load() != 1 {
				t.Fatalf("retries started concurrent loads: seq=%d->%d requests=%d", seq, after, calls.Load())
			}
		})
	}
}

func TestCachedTracksRetainFailureAndRetry(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	var fail atomic.Bool
	handler := tracksHandler(nil)
	p.library = libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	s.restoreDrill("album-a1", "One")
	p.loadTracks("p1")
	drillReady(t, p, s)
	fail.Store(true)
	// Read synchronously to prove the cache and failure before asking the
	// pane to refresh from that same client.
	if tracks, err := p.library.AlbumTracks(context.Background(), "a1", ""); err == nil || len(tracks) != 2 {
		t.Fatalf("fixture did not retain stale tracks: %d %v", len(tracks), err)
	}
	p.loadTracks("p1")
	eventually(t, "a warning beside cached tracks", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		n := find(draw(t, p, s), "state-tracks-failed")
		return len(s.tracks) == 2 && n != nil && n.Action != "" && n.Detail == locales.SpotifyOffline() && find(draw(t, p, s), "tracks") != nil
	})
}
