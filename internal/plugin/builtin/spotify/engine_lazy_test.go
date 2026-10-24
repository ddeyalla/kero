package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/engine"
	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// lazyPlugin is a plugin with one pane, a signed-in library whose Web API is
// api, and the fake engine behind the lazy one. The engine is made and its
// web view is not open. The idle period is short.
func lazyPlugin(t *testing.T, api *webAPI) (*plugin, *session, *fakeEngine) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	f := useFakeEngine(t, nil)
	oldIdle := engineIdleTimeout
	engineIdleTimeout = 60 * time.Millisecond
	t.Cleanup(func() { engineIdleTimeout = oldIdle })
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modeNotRunning, libReady)
	p.engine, p.player = nil, p.desktop
	raw, err := json.Marshal(web.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Scope: allScopes, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	p.library = web.New(web.Config{
		BaseURL: srv.URL,
		Secrets: web.MapSecrets{web.TokensKey: string(raw)},
		OpenURL: web.OpenURLFunc(func(string) error { return nil }),
	})
	p.library.SetClientID("client")
	p.syncEngine()
	t.Cleanup(func() { _ = p.engine.Close() })
	return p, s, f
}

func (f *fakeEngine) counts() (started, closed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.closed
}

func TestAnOpenPaneDoesNotOpenTheWebView(t *testing.T) {
	api := &webAPI{recent: recentlyPlayedAlbum}
	p, s, f := lazyPlugin(t, api)
	p.readNow()
	// The pane shows the player and the track played last, from the Web
	// API, with no web view.
	eventually(t, "the last track", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.snap.Track == "Juicy"
	})
	if started, _ := f.counts(); started != 0 {
		t.Fatalf("the web view opened %d times for a pane that only shows", started)
	}
	if got := paneMode(p, s); got == modeUnavailable || p.engine == nil {
		t.Fatalf("playback = %v, engine %v: the pane must keep its player", got, p.engine)
	}
	if s.notice != "" {
		t.Fatalf("notice %q for an engine that is closed on purpose", s.notice)
	}
}

func TestTheWebViewGoesAfterTheIdlePeriod(t *testing.T) {
	p, s, f := lazyPlugin(t, &webAPI{})
	wakeEngine(t, p, f)
	f.idle()
	eventually(t, "the web view to close", func() bool { _, closed := f.counts(); return closed == 1 })
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engine == nil || s.notice != "" || s.playback != modeIdle {
		t.Fatalf("engine %v, notice %q, playback %v: want the player kept, stopped, with no notice", p.engine, s.notice, s.playback)
	}
}

func TestAPlayingTrackKeepsTheWebView(t *testing.T) {
	p, _, f := lazyPlugin(t, &webAPI{})
	wakeEngine(t, p, f)
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Juicy", ReadAt: time.Now()})
	time.Sleep(4 * engineIdleTimeout)
	if _, closed := f.counts(); closed != 0 {
		t.Fatal("a playing track lost its web view")
	}

	// The period starts again when the track stops.
	f.hold(transport.Snapshot{State: transport.StatePaused, Track: "Juicy", ReadAt: time.Now()})
	eventually(t, "the web view to close after the pause", func() bool { _, closed := f.counts(); return closed == 1 })
}

func TestAPlayAfterTheTeardownOpensTheWebViewAgain(t *testing.T) {
	p, _, f := lazyPlugin(t, &webAPI{})
	wakeEngine(t, p, f)
	f.idle()
	eventually(t, "the web view to close", func() bool { _, closed := f.counts(); return closed == 1 })

	// The new web view reports ready only after a cold start, and the play
	// must reach it then, not be dropped for having waited.
	f.mu.Lock()
	f.ready, f.snap = false, transport.Snapshot{}
	f.mu.Unlock()
	readyAt := make(chan time.Time, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		f.mu.Lock()
		f.ready = true
		f.mu.Unlock()
		readyAt <- time.Now()
	}()
	if err := p.activePlayer().Play(context.Background(), "spotify:track:t1"); err != nil {
		t.Fatalf("play: %v", err)
	}
	delivered := time.Now()
	if started, _ := f.counts(); started != 2 {
		t.Fatalf("started %d times, want the web view opened again", started)
	}
	if delivered.Before(<-readyAt) {
		t.Fatal("the play was sent before the web view was ready")
	}
	if got := f.calls(); !slices.Contains(got, "play spotify:track:t1") {
		t.Fatalf("engine commands = %v, want the play delivered", got)
	}
}

func TestAPlayDoesNotWaitLongerThanTheWakeTimeout(t *testing.T) {
	old := engineWakeTimeout
	engineWakeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { engineWakeTimeout = old })
	p, _, f := lazyPlugin(t, &webAPI{})
	f.mu.Lock()
	f.ready = false
	f.mu.Unlock()
	// The fake sends whatever it is given; the real engine answers
	// ErrStarting, which the play passes on.
	err := p.activePlayer().Play(context.Background(), "spotify:track:t1")
	if err != nil && !errors.Is(err, engine.ErrStarting) {
		t.Fatalf("play: %v", err)
	}
}

func TestTheLastPaneClosedLetsGoOfAnIdleWebView(t *testing.T) {
	p, _, f := lazyPlugin(t, &webAPI{})
	oldIdle := engineIdleTimeout
	engineIdleTimeout = time.Hour
	t.Cleanup(func() { engineIdleTimeout = oldIdle })
	wakeEngine(t, p, f)
	f.idle()
	p.close(nil, "p1")
	if _, closed := f.counts(); closed != 1 {
		t.Fatalf("closed %d times, want the web view gone with the last pane", closed)
	}
	// A pane that opens again starts nothing, and a play opens the web view.
	p.open(p.conn, sdk.OpenEvent{Pane: "p2", Widget: widgetPlayer, Size: protocol.Size{Width: 700, Height: 600}})
	if started, _ := f.counts(); started != 1 {
		t.Fatalf("started %d times after a pane opened", started)
	}
	wakeEngine(t, p, f)
	if started, _ := f.counts(); started != 2 {
		t.Fatalf("started %d times after a play, want the web view opened again", started)
	}
}

func TestTheLastPaneClosedKeepsAPlayingTrack(t *testing.T) {
	p, _, f := lazyPlugin(t, &webAPI{})
	wakeEngine(t, p, f)
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Juicy", ReadAt: time.Now()})
	p.close(nil, "p1")
	time.Sleep(4 * engineIdleTimeout)
	if _, closed := f.counts(); closed != 0 {
		t.Fatal("closing the last pane stopped a playing track")
	}
	// It goes as soon as the track stops, with no pane to wait for.
	f.hold(transport.Snapshot{State: transport.StatePaused, Track: "Juicy", ReadAt: time.Now()})
	eventually(t, "the web view to close", func() bool { _, closed := f.counts(); return closed == 1 })
}

// A play whose caller is already gone does not open a web view or send
// anything.
func TestLazyPlayRejectsAnAlreadyCancelledContext(t *testing.T) {
	p, _, f := lazyPlugin(t, &webAPI{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := p.activePlayer().Play(ctx, "spotify:track:t1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("play = %v, want context.Canceled", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Errorf("a cancelled play took %v", time.Since(start))
	}
	if started, _ := f.counts(); started != 0 {
		t.Errorf("the web view opened %d times for a cancelled play", started)
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Errorf("commands = %v, want none", calls)
	}
}

// Taking the wake gate and canceling can happen together: a call that is
// canceled before it owns the web view must not build or start one, and
// must release the gate for the next healthy call.
func TestLazyPlayCancelsAfterTakingTheWakeGate(t *testing.T) {
	p, _, f := lazyPlugin(t, &webAPI{})
	w := p.activePlayer().(*lazyEngine)
	w.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.PlayRequest(ctx, "spotify:track:t1", "1") }()
	eventually(t, "the wake gate", func() bool { return len(w.wakeGate) == 1 })
	cancel()
	w.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("play = %v, want context.Canceled", err)
	}
	if started, closed := f.counts(); started != 0 || closed != 0 {
		t.Fatalf("a canceled play built %d and closed %d web views", started, closed)
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Fatalf("a canceled play sent %v", calls)
	}
	if len(w.wakeGate) != 0 {
		t.Fatal("the canceled play leaked the wake gate")
	}

	// The gate is free, and a later call still opens the view and plays.
	if err := w.PlayRequest(context.Background(), "spotify:track:t2", "2"); err != nil {
		t.Fatal(err)
	}
	if started, _ := f.counts(); started != 1 {
		t.Fatalf("the healthy call built %d web views, want one", started)
	}
	if calls := f.calls(); !slices.Equal(calls, []string{"play spotify:track:t2"}) {
		t.Fatalf("commands = %v, want the healthy play", calls)
	}
}

// requestInner records the request identity that reaches the inner engine.
type requestInner struct {
	*fakeEngine
	mu       sync.Mutex
	requests []string
	from     []string
}

func (e *requestInner) PlayRequest(ctx context.Context, uri, request string) error {
	e.mu.Lock()
	e.requests = append(e.requests, request)
	e.mu.Unlock()
	return e.Play(ctx, uri)
}

func (e *requestInner) PlayFromRequest(ctx context.Context, contextURI, trackURI, request string) error {
	e.mu.Lock()
	e.requests = append(e.requests, request)
	e.from = append(e.from, contextURI+" at "+trackURI)
	e.mu.Unlock()
	return nil
}

func TestTheLazyEngineCarriesThePlayRequestIdentity(t *testing.T) {
	inner := &requestInner{fakeEngine: &fakeEngine{ready: true}}
	w := newLazyEngine(
		func(func(transport.Snapshot), func(error)) playerEngine { return inner },
		func(transport.Snapshot) {}, func(error) {}, func() {},
	)
	if err := w.PlayRequest(context.Background(), "spotify:album:a1", "9"); err != nil {
		t.Fatal(err)
	}
	if err := w.PlayFromRequest(context.Background(), "spotify:album:a1", "spotify:track:t1", "10"); err != nil {
		t.Fatal(err)
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	if !slices.Equal(inner.requests, []string{"9", "10"}) || !slices.Equal(inner.from, []string{"spotify:album:a1 at spotify:track:t1"}) {
		t.Fatalf("requests = %v, from = %v", inner.requests, inner.from)
	}
}

// A play waits for a cold web view only as long as its caller allows: at
// the deadline it returns, and even a web view that becomes ready later
// never receives that play.
func TestLazyPlayStopsAtItsDeadlineWhileTheWebViewStarts(t *testing.T) {
	oldWake := engineWakeTimeout
	engineWakeTimeout = 80 * time.Millisecond
	t.Cleanup(func() { engineWakeTimeout = oldWake })
	oldIdle := engineIdleTimeout
	engineIdleTimeout = time.Hour
	t.Cleanup(func() { engineIdleTimeout = oldIdle })
	p, _, f := lazyPlugin(t, &webAPI{})
	f.mu.Lock()
	f.ready = false
	f.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.activePlayer().Play(ctx, "spotify:track:t1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("play = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Errorf("a play past its deadline took %v", elapsed)
	}
	// The view becomes ready after the caller has gone. Give the wake period
	// and the poll a chance to send the old play late.
	f.mu.Lock()
	f.ready = true
	f.mu.Unlock()
	time.Sleep(3*engineWakeTimeout + 50*time.Millisecond)
	if calls := f.calls(); len(calls) != 0 {
		t.Errorf("commands after the deadline = %v, want none", calls)
	}
}

// A play waiting behind another wake stops as soon as its caller does,
// rather than waiting for the gate and sending late.
func TestLazyPlayCancelsWhileAnotherWakeOwnsTheGate(t *testing.T) {
	old := engineWakeTimeout
	engineWakeTimeout = 2 * time.Second
	t.Cleanup(func() { engineWakeTimeout = old })
	p, _, f := lazyPlugin(t, &webAPI{})
	f.mu.Lock()
	f.ready = false
	f.mu.Unlock()

	first := make(chan struct{})
	go func() {
		defer close(first)
		_ = p.activePlayer().Play(context.Background(), "spotify:track:first")
	}()
	eventually(t, "the first wake", func() bool { started, _ := f.counts(); return started == 1 })
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.activePlayer().Play(ctx, "spotify:track:second")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second play = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Errorf("the waiting play took %v past its deadline", elapsed)
	}

	f.mu.Lock()
	f.ready = true
	f.mu.Unlock()
	<-first
	if calls := f.calls(); !slices.Equal(calls, []string{"play spotify:track:first"}) {
		t.Errorf("commands = %v, want only the first play", calls)
	}
}
