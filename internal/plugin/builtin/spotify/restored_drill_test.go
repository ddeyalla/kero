package spotify

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
)

// A drill restored while the pane was compact loads when the pane first
// becomes large. syncLibrary deliberately skips it while the small layout
// cannot show it, so resize must start the one deferred load.
func TestRestoredDrillLoadsWhenPaneBecomesLarge(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	p.mu.Lock()
	s.size = protocol.Size{Width: 200, Height: 120}
	s.restoreDrill("album-a1", "Album One")
	p.mu.Unlock()

	p.resize(nil, "p1", protocol.Size{Width: 700, Height: 600})
	eventually(t, "the restored drill tracks", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.tracksState == tracksReady && len(s.tracks) == 2
	})
}

func TestRestoredDrillWaitsForLibraryThenLoads(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	readyClient := p.library
	p.library = nil
	p.mu.Lock()
	s.size = protocol.Size{Width: 200, Height: 120}
	s.restoreDrill("album-a1", "Album One")
	p.mu.Unlock()

	p.resize(nil, "p1", protocol.Size{Width: 700, Height: 600})
	p.mu.Lock()
	pending := s.tracksPending
	state := s.tracksState
	p.mu.Unlock()
	if pending || state != tracksLoading {
		t.Fatalf("a load started before the library was ready: pending=%v state=%v", pending, state)
	}

	p.library = readyClient
	p.syncLibrary("p1")
	eventually(t, "the restored drill after the library is ready", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.tracksState == tracksReady && len(s.tracks) == 2
	})
}

func TestRestoredDrillRepeatsOneLoadAndKeepsLoadedStates(t *testing.T) {
	p, s, _, _ := drillPlugin(t)
	var calls atomic.Int32
	started, release := make(chan struct{}, 1), make(chan struct{}, 1)
	handler := tracksHandler(nil)
	p.library = libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/albums/a1/tracks" {
			handler.ServeHTTP(w, r)
			return
		}
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		handler.ServeHTTP(w, r)
	}))
	defer close(release)
	p.mu.Lock()
	s.size = protocol.Size{Width: 200, Height: 120}
	s.restoreDrill("album-a1", "Album One")
	p.mu.Unlock()

	p.resize(nil, "p1", protocol.Size{Width: 700, Height: 600})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the restored drill did not start its one track request")
	}
	p.mu.Lock()
	seq, pending := s.tracksSeq, s.tracksPending
	p.mu.Unlock()
	if !pending {
		t.Fatal("the held track request is not pending")
	}

	// While the one request is in flight, resize repeatedly. loadTracks
	// increments tracksSeq and sets tracksPending before it starts a
	// goroutine, so unchanged state proves resize attempted no second load.
	for range 10 {
		p.resize(nil, "p1", protocol.Size{Width: 200, Height: 120})
		p.resize(nil, "p1", protocol.Size{Width: 700, Height: 600})
	}
	p.mu.Lock()
	after, pending := s.tracksSeq, s.tracksPending
	p.mu.Unlock()
	if after != seq || !pending || calls.Load() != 1 {
		t.Fatalf("repeated resizes started another load: seq=%d->%d pending=%v requests=%d", seq, after, pending, calls.Load())
	}

	release <- struct{}{}
	eventually(t, "one restored drill load", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.tracksState == tracksReady && len(s.tracks) == 2
	})
	if calls.Load() != 1 {
		t.Fatalf("repeated resizes made %d track requests, want 1", calls.Load())
	}

	// A loaded list is kept through resizes. Forbidden and failed lists are
	// terminal until their explicit retry, so resize must not refetch them.
	// tracksSeq is the synchronous proof: loadTracks would advance it before
	// spawning a goroutine.
	p.mu.Lock()
	loadedSeq := s.tracksSeq
	p.mu.Unlock()
	for range 3 {
		p.resize(nil, "p1", protocol.Size{Width: 200, Height: 120})
		p.resize(nil, "p1", protocol.Size{Width: 700, Height: 600})
	}
	p.mu.Lock()
	loadedAfter, loadedPending := s.tracksSeq, s.tracksPending
	p.mu.Unlock()
	if loadedAfter != loadedSeq || loadedPending {
		t.Fatalf("resize refetched a loaded list: seq=%d->%d pending=%v", loadedSeq, loadedAfter, loadedPending)
	}
	for _, state := range []trackState{tracksForbidden, tracksFailed} {
		p.mu.Lock()
		s.tracksState, s.tracksPending = state, false
		before := s.tracksSeq
		p.mu.Unlock()
		p.resize(nil, "p1", protocol.Size{Width: 200, Height: 120})
		p.resize(nil, "p1", protocol.Size{Width: 700, Height: 600})
		p.mu.Lock()
		got, after, pending := s.tracksState, s.tracksSeq, s.tracksPending
		p.mu.Unlock()
		if after != before || pending {
			t.Fatalf("resize retried a %v list: seq=%d->%d pending=%v", state, before, after, pending)
		}
		if got != state {
			t.Fatalf("resize changed a %v list to %v", state, got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("loaded/terminal resizes made %d track requests, want 1", calls.Load())
	}
}
