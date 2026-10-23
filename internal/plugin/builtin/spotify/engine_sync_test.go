package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"kero/internal/plugin/builtin/spotify/engine"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// fakeEngine is the player that plays in the app, as a test sees it.
type fakeEngine struct {
	mu       sync.Mutex
	started  int
	closed   int
	commands []string
	snap     transport.Snapshot
	ready    bool
	startErr error
	emit     func(transport.Snapshot)
	onError  func(error)
}

func (f *fakeEngine) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	return f.startErr
}

func (f *fakeEngine) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

func (f *fakeEngine) record(c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, c)
	return nil
}

func (f *fakeEngine) PlayPause(context.Context) error { return f.record("play-pause") }
func (f *fakeEngine) Next(context.Context) error      { return f.record("next") }
func (f *fakeEngine) Previous(context.Context) error  { return f.record("previous") }
func (f *fakeEngine) Launch(context.Context) error    { return transport.ErrNotInstalled }
func (f *fakeEngine) Seek(context.Context, time.Duration) error {
	return f.record("seek")
}
func (f *fakeEngine) Volume(context.Context, int) error   { return f.record("volume") }
func (f *fakeEngine) Shuffle(context.Context, bool) error { return f.record("shuffle") }
func (f *fakeEngine) Repeat(context.Context, bool) error  { return f.record("repeat") }
func (f *fakeEngine) Play(_ context.Context, uri string) error {
	return f.record("play " + uri)
}
func (f *fakeEngine) PlayFrom(_ context.Context, contextURI, trackURI string) error {
	return f.record("play " + contextURI + " at " + trackURI)
}

// Read answers like the engine: starting until the test gives it a
// track it holds.
func (f *fakeEngine) Read(context.Context) (transport.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snap.Track == "" && !f.ready {
		return transport.Snapshot{}, engine.ErrStarting
	}
	return f.snap, nil
}

// hold makes the engine hold a track, and pushes it to the plugin.
func (f *fakeEngine) hold(snap transport.Snapshot) {
	f.mu.Lock()
	f.snap = snap
	f.mu.Unlock()
	f.emit(snap)
}

func (f *fakeEngine) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

// useFakeEngine replaces how the plugin makes its engine for one test.
func useFakeEngine(t *testing.T, startErr error) *fakeEngine {
	t.Helper()
	f := &fakeEngine{startErr: startErr, ready: true}
	old := newEngine
	newEngine = func(_ *sdk.Conn, _ *web.Client, emit func(transport.Snapshot), onError func(error)) playerEngine {
		f.emit, f.onError = emit, onError
		return f
	}
	t.Cleanup(func() { newEngine = old })
	return f
}

// wakeEngine plays once, which opens the web view of the lazy engine, and
// waits until the fake engine started.
func wakeEngine(t *testing.T, p *plugin, f *fakeEngine) {
	t.Helper()
	_ = p.activePlayer().PlayPause(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.started == 0 {
		t.Fatal("a play did not open the engine")
	}
}

// streamingLibrary is a Web API client whose tokens carry the scopes.
func streamingLibrary(t *testing.T, scope string) *web.Client {
	t.Helper()
	raw, err := json.Marshal(web.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Scope: scope, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	client := web.New(web.Config{
		Secrets: web.MapSecrets{web.TokensKey: string(raw)},
		OpenURL: web.OpenURLFunc(func(string) error { return nil }),
	})
	client.SetClientID("client")
	return client
}

func TestSigningInWithTheStreamingScopeMakesTheEngineThePlayer(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, s, runner := testPlugin(protocol.Size{Width: 400, Height: 320}, modeNotRunning, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "user-library-read streaming user-read-email")

	p.syncEngine()
	if _, ok := p.activePlayer().(*lazyEngine); !ok || f.started != 0 {
		t.Fatalf("started %d times, player %T: want the lazy engine to be the player, with no web view yet", f.started, p.activePlayer())
	}
	// A second sync does not make another engine.
	player := p.activePlayer()
	p.syncEngine()
	if p.activePlayer() != player {
		t.Fatal("a second sync replaced the engine")
	}
	wakeEngine(t, p, f)
	if f.started != 1 {
		t.Fatalf("started %d times, want once", f.started)
	}

	// A pushed snapshot reaches the pane, with no read of the desktop app.
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Juicy", Artist: "The Notorious B.I.G."})
	p.mu.Lock()
	track, mode := s.snap.Track, s.playback
	p.mu.Unlock()
	if track != "Juicy" || mode != modePlaying {
		t.Fatalf("pane has %q in mode %v after a pushed snapshot", track, mode)
	}

	// The pane's commands go to the engine, not to AppleScript.
	p.doCommand(nil, "p1", "failed", "next")
	p.mu.Lock()
	s.entries = []entry{{id: "row", uri: "spotify:album:a1"}}
	p.mu.Unlock()
	p.selectResult(protocol.EventParams{Pane: "p1", Value: "row"})
	eventually(t, "the engine's commands", func() bool { return len(f.calls()) == 3 })
	// Each command runs in its own goroutine, so their order is not fixed.
	got := f.calls()
	slices.Sort(got)
	if !slices.Equal(got, []string{"next", "play spotify:album:a1", "play-pause"}) {
		t.Fatalf("engine commands = %v", got)
	}
	if n := len(runner.calls()); n != 0 {
		t.Fatalf("%d AppleScripts ran while the engine plays: %v", n, runner.calls())
	}
}

func TestTokensWithoutTheStreamingScopeLeaveTheDesktopAppThePlayer(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, _, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "playlist-read-private user-library-read user-read-playback-state")
	desktop := p.desktop

	p.syncEngine()
	if f.started != 0 || p.activePlayer() != transport.Player(desktop) {
		t.Fatalf("started %d times, player %T: an old sign-in must keep the desktop app", f.started, p.activePlayer())
	}
}

func TestSigningOutGivesTheDesktopAppThePlayerAgain(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, _, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "streaming")
	desktop := p.desktop
	p.syncEngine()
	if _, ok := p.activePlayer().(*lazyEngine); !ok {
		t.Fatal("the engine is not the player")
	}
	wakeEngine(t, p, f)

	if err := p.library.SignOut(); err != nil {
		t.Fatal(err)
	}
	p.syncEngine()
	if f.closed != 1 || p.activePlayer() != transport.Player(desktop) || p.engine != nil {
		t.Fatalf("closed %d times, player %T, engine %v: want the desktop app back and the engine ended", f.closed, p.activePlayer(), p.engine)
	}
}

func TestAnEngineThatCannotStartFailsThePlayNotTheSignIn(t *testing.T) {
	f := useFakeEngine(t, errors.New("web.open: refused"))
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "streaming")

	// Opening the pane starts nothing, so nothing is refused yet.
	p.syncEngine()
	if f.started != 0 || p.engine == nil {
		t.Fatalf("started %d, engine %v: want the lazy engine and no web view", f.started, p.engine)
	}
	// The play opens the web view, which the host refuses.
	if err := p.activePlayer().PlayPause(context.Background()); err == nil {
		t.Fatal("a play on a web view that cannot open worked")
	}
	if f.started != 1 {
		t.Fatalf("started %d times, want one try", f.started)
	}
	_ = s
}

func TestShutdownEndsTheEngine(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, _, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "streaming")
	p.syncEngine()
	wakeEngine(t, p, f)
	p.shutdown()
	if f.closed != 1 {
		t.Fatalf("closed %d times, want the engine and its web view ended at shutdown", f.closed)
	}
}

func TestAnEngineErrorIsANoticeNotALostPlayer(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "streaming")
	p.syncEngine()
	wakeEngine(t, p, f)
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Juicy"})

	f.onError(errors.New("playback_error: Playback error"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.playback != modePlaying || s.notice != localePlaybackFailed() {
		t.Fatalf("playback %v, notice %q: want the player kept and a notice", s.playback, s.notice)
	}
	if find(draw(t, p, s), "play") == nil {
		t.Fatal("the player is gone after the engine's error")
	}
}

func TestPlayPauseAnswersBeforeTheEngine(t *testing.T) {
	f := useFakeEngine(t, nil)
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	p.engine, p.player = nil, p.desktop
	p.library = streamingLibrary(t, "streaming")
	p.syncEngine()
	wakeEngine(t, p, f)
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Juicy", Duration: time.Minute, ReadAt: time.Now()})

	p.doCommand(nil, "p1", "failed", "play-pause")
	p.mu.Lock()
	state := s.snap.State
	icon := find(draw(t, p, s), "play").Icon
	p.mu.Unlock()
	if state != transport.StatePaused || icon != "play" {
		t.Fatalf("state %v, icon %q: want paused at once", state, icon)
	}
	eventually(t, "the engine's toggle", func() bool { return slices.Contains(f.calls(), "play-pause") })
}

func TestPlaybackNoticeNamesPremiumSignInOrSpotifysWords(t *testing.T) {
	long := strings.Repeat("x", 200)
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"premium":        {&engine.PlaybackError{Kind: "account_error", Message: "Premium required"}, localePremiumRequired()},
		"premium by 403": {&engine.PlaybackError{Kind: "command", Message: "Spotify answered 403 PREMIUM_REQUIRED"}, localePremiumRequired()},
		"sign in":        {&engine.PlaybackError{Kind: "authentication_error", Message: "bad token"}, localeSignInAgainNotice()},
		"401":            {&engine.PlaybackError{Kind: "command", Message: "Spotify answered 401 expired"}, localeSignInAgainNotice()},
		"spotify words": {
			&engine.PlaybackError{Kind: "command", Message: `Spotify answered 403 {"error":{"status":403,"message":"Restriction violated"}}`},
			localePlaybackFailedDetail("Restriction violated"),
		},
		"plain words": {&engine.PlaybackError{Kind: "playback_error", Message: "no list was loaded"}, localePlaybackFailedDetail("no list was loaded")},
		"clipped":     {&engine.PlaybackError{Kind: "playback_error", Message: long}, localePlaybackFailedDetail(long[:noticeClip] + "…")},
		"token":       {&engine.PlaybackError{Kind: "token", Message: "token 503"}, localeSignInAgainNotice()},
		"no words":    {&engine.PlaybackError{Kind: "playback_error"}, localePlaybackFailed()},
		"other error": {errors.New("boom"), localePlaybackFailed()},
	} {
		if got := playbackNotice(tc.err); got != tc.want {
			t.Errorf("%s: notice = %q, want %q", name, got, tc.want)
		}
	}
}

func TestAStartingEngineKeepsTheGuessAndAFailureClearsIt(t *testing.T) {
	p, s, f, _ := pushedPlugin(t)
	pressed(t, p, f)

	p.mu.Lock()
	mode := s.playback
	p.mu.Unlock()
	f.onError(engine.ErrStarting)
	p.mu.Lock()
	if s.snap.Track != "Album One" || s.guess == nil || s.notice != "" || s.playback != mode {
		t.Errorf("after ErrStarting snap = %+v guess = %+v notice = %q mode = %v (was %v), want the guess and the mode to stand", s.snap, s.guess, s.notice, s.playback, mode)
	}
	p.mu.Unlock()

	f.onError(&engine.PlaybackError{Kind: "account_error", Message: "Premium required"})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.guess != nil || s.snap.Track != "Test Song" || s.notice != localePremiumRequired() {
		t.Errorf("after a failure snap = %+v guess = %+v notice = %q, want the earlier track and the Premium notice", s.snap, s.guess, s.notice)
	}
}

func TestAPlayingStateEndsTheFailureNotice(t *testing.T) {
	p, s, _, _ := pushedPlugin(t)
	p.mu.Lock()
	s.notice = localePremiumRequired()
	p.mu.Unlock()
	p.playback(transport.Snapshot{State: transport.StatePlaying, Track: "Test Song", URI: "spotify:track:old"})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice != "" {
		t.Errorf("notice = %q, want it cleared by the playing state", s.notice)
	}
}

func TestOnlyAStateOfAnotherTrackEndsTheFailureNotice(t *testing.T) {
	p, s, f, _ := pushedPlugin(t)
	f.onError(&engine.PlaybackError{Kind: "command", Message: "Spotify answered 403 Restriction violated"})
	p.mu.Lock()
	if s.notice == "" || s.noticeURI != "spotify:track:old" {
		t.Fatalf("notice = %q for %q, want a notice set for the playing track", s.notice, s.noticeURI)
	}
	p.mu.Unlock()

	// The echo of a volume, or a state of the same track, leaves it.
	p.playback(transport.Snapshot{State: transport.StatePlaying, Track: "Test Song", URI: "spotify:track:old", Volume: 30})
	p.mu.Lock()
	if s.notice == "" {
		p.mu.Unlock()
		t.Fatal("a state of the same track ended the notice")
	}
	p.mu.Unlock()

	p.playback(transport.Snapshot{State: transport.StatePlaying, Track: "Other", URI: "spotify:track:new"})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice != "" {
		t.Errorf("notice = %q, want it ended by another track", s.notice)
	}
}
