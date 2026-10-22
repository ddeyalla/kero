package spotify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
)

// webAPI is a Spotify Web API that answers the two calls that find the
// track for an idle player, and counts them.
type webAPI struct {
	mu       sync.Mutex
	player   string // the body of /v1/me/player; "" is 204, no device
	recent   string // the body of recently-played; "" is 403
	requests []string
	// gate, when set, holds the answer of recently-played until it closes.
	gate chan struct{}
}

func (w *webAPI) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	w.requests = append(w.requests, r.URL.Path)
	player, recent, gate := w.player, w.recent, w.gate
	w.mu.Unlock()
	if gate != nil && r.URL.Path == "/v1/me/player/recently-played" {
		<-gate
	}
	rw.Header().Set("Content-Type", "application/json")
	body := ""
	switch r.URL.Path {
	case "/v1/me/player":
		if player == "" {
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		body = player
	case "/v1/me/player/recently-played":
		if recent == "" {
			rw.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(rw, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
			return
		}
		body = recent
	default:
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = io.WriteString(rw, body)
}

func (w *webAPI) asked(path string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, got := range w.requests {
		if got == path {
			n++
		}
	}
	return n
}

const recentlyPlayedAlbum = `{"items":[{"track":{"uri":"spotify:track:t1","name":"Juicy","type":"track",
	"duration_ms":300000,"artists":[{"name":"The Notorious B.I.G."}],
	"album":{"name":"Ready to Die","images":[{"url":"https://i/64","width":64},{"url":"https://i/640","width":640}]}},
	"context":{"uri":"spotify:album:a1","type":"album"}}]}`

const recentlyPlayedAlone = `{"items":[{"track":{"uri":"spotify:track:t2","name":"Alone","type":"track",
	"duration_ms":1000,"artists":[{"name":"Ada"}],"album":{"name":"Solo","images":[]}},"context":null}]}`

const otherDevice = `{"is_playing":true,"progress_ms":42000,"context":{"uri":"spotify:playlist:p1","type":"playlist"},
	"item":{"uri":"spotify:track:t3","name":"Elsewhere","type":"track","duration_ms":200000,
	"artists":[{"name":"Grace"}],"album":{"name":"Remote","images":[{"url":"https://i/300","width":300}]}}}`

// idlePlugin is a plugin whose engine is the fake one and whose Web API is
// api, with a sign-in that has the scopes. The engine has not reported
// ready.
func idlePlugin(t *testing.T, api *webAPI, scope string) (*plugin, *session, *fakeEngine) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	f := useFakeEngine(t, nil)
	// These tests follow an engine that opened its web view and has not
	// reported ready, so it opens at once and is not waited for.
	f.ready = false
	oldEager, oldWake := engineEager, engineWakeTimeout
	engineEager, engineWakeTimeout = true, time.Millisecond
	t.Cleanup(func() { engineEager, engineWakeTimeout = oldEager, oldWake })
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modeNotRunning, libReady)
	p.engine, p.player = nil, p.desktop
	raw, err := json.Marshal(web.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", Scope: scope, ExpiresAt: time.Now().Add(time.Hour)})
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
	return p, s, f
}

const allScopes = "streaming user-read-playback-state user-read-recently-played"

// ready is the engine's ready event: a state with no track.
func (f *fakeEngine) idle() {
	snap := transport.Snapshot{Volume: 50, ReadAt: time.Now()}
	f.mu.Lock()
	f.ready, f.snap = true, snap
	f.mu.Unlock()
	f.emit(snap)
}

func paneMode(p *plugin, s *session) playbackMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return s.playback
}

func TestAReadyEngineWithNoStateLeavesConnecting(t *testing.T) {
	api := &webAPI{}
	p, s, f := idlePlugin(t, api, "streaming user-read-playback-state")
	if got := paneMode(p, s); got != modeStarting {
		t.Fatalf("playback = %v, want connecting before ready", got)
	}

	f.idle()
	if got := paneMode(p, s); got != modeIdle {
		t.Fatalf("playback = %v, want an idle player once ready, not a spinner", got)
	}

	// A pane that opens later is told by a read, since the engine pushes
	// nothing for it.
	p.mu.Lock()
	late := &session{pane: "p2", widget: widgetPlayer, size: s.size, playback: modeStarting, lib: libReady, loaded: make(map[tabKind]bool)}
	p.panes["p2"] = late
	p.mu.Unlock()
	p.readNow()
	if got := paneMode(p, late); got != modeIdle {
		t.Fatalf("late pane playback = %v, want idle", got)
	}
}

func TestAPaneStopsConnectingAfterTheTimeout(t *testing.T) {
	var mu sync.Mutex
	var timers []func()
	old := afterFunc
	afterFunc = func(_ time.Duration, fn func()) {
		mu.Lock()
		defer mu.Unlock()
		timers = append(timers, fn)
	}
	t.Cleanup(func() { afterFunc = old })

	p, s, f := idlePlugin(t, &webAPI{}, "streaming")
	mu.Lock()
	fire := slices.Clone(timers)
	mu.Unlock()
	if len(fire) == 0 {
		t.Fatal("no timer armed for the connection")
	}
	for _, fn := range fire {
		fn()
	}
	if got := paneMode(p, s); got != modeUnavailable {
		t.Fatalf("playback = %v, want unavailable once the engine never reports ready", got)
	}
	if find(draw(t, p, s), "state-unavailable") == nil {
		t.Fatal("the pane does not show the unavailable state")
	}

	// An engine that is ready after all replaces it.
	f.idle()
	if got := paneMode(p, s); got != modeIdle {
		t.Fatalf("playback = %v after a late ready, want idle", got)
	}
}

func TestAReadyEngineWhoseTimerFiresAfterReadyStaysReady(t *testing.T) {
	var timers []func()
	old := afterFunc
	afterFunc = func(_ time.Duration, fn func()) { timers = append(timers, fn) }
	t.Cleanup(func() { afterFunc = old })
	p, s, f := idlePlugin(t, &webAPI{}, "streaming")
	f.idle()
	for _, fn := range timers {
		fn()
	}
	if got := paneMode(p, s); got != modeIdle {
		t.Fatalf("playback = %v, want idle: the timer must not undo a ready player", got)
	}
}

func TestTheLastPlayedTrackShowsPausedWithItsArt(t *testing.T) {
	api := &webAPI{recent: recentlyPlayedAlbum}
	p, s, f := idlePlugin(t, api, allScopes)
	f.idle()

	eventually(t, "the last track", func() bool { return paneMode(p, s) == modePlaying })
	p.mu.Lock()
	snap := s.snap
	p.mu.Unlock()
	if snap.Track != "Juicy" || snap.Artist != "The Notorious B.I.G." || snap.State != transport.StatePaused {
		t.Fatalf("snapshot = %+v, want Juicy paused", snap)
	}
	if snap.ArtworkURL != "https://i/640" || snap.Duration != 300*time.Second || snap.Volume != 50 {
		t.Fatalf("art %q, duration %v, volume %d", snap.ArtworkURL, snap.Duration, snap.Volume)
	}
	// The last track alone is no track loaded in this session: the pane
	// draws the library, not the player.
	if root := draw(t, p, s); find(root, "play") != nil || find(root, "title") != nil || strings.Contains(texts(root), "Juicy") {
		t.Fatalf("the pane draws the player of the last track: %q", texts(root))
	}

	// The engine's next idle push, such as the echo of a volume, keeps it.
	f.mu.Lock()
	f.snap = transport.Snapshot{Volume: 60, ReadAt: time.Now()}
	f.mu.Unlock()
	f.emit(f.snap)
	p.mu.Lock()
	kept, volume := s.snap.Track, s.snap.Volume
	p.mu.Unlock()
	if kept != "Juicy" || volume != 60 {
		t.Fatalf("after an idle push: track %q volume %d", kept, volume)
	}
	if n := api.asked("/v1/me/player/recently-played"); n != 1 {
		t.Fatalf("recently-played asked %d times, want once", n)
	}

	// A real track replaces it for good.
	f.hold(transport.Snapshot{State: transport.StatePlaying, Track: "Other", URI: "spotify:track:o"})
	f.mu.Lock()
	f.snap = transport.Snapshot{ReadAt: time.Now()}
	f.mu.Unlock()
	f.emit(transport.Snapshot{ReadAt: time.Now()})
	if got := paneMode(p, s); got != modeIdle {
		t.Fatalf("playback = %v, want idle: the old last track is stale once something played", got)
	}
}

func TestAForbiddenHistoryLeavesNothingPlaying(t *testing.T) {
	api := &webAPI{} // recently-played answers 403
	p, s, f := idlePlugin(t, api, allScopes)
	f.idle()
	eventually(t, "the history was asked", func() bool { return api.asked("/v1/me/player/recently-played") == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := paneMode(p, s); got != modeIdle {
		t.Fatalf("playback = %v, want the Nothing playing state after a 403", got)
	}
	if root := draw(t, p, s); find(root, "state-idle") != nil || find(root, "play") != nil || find(root, "tab-playlists") == nil {
		t.Fatalf("the idle pane draws something but the library: %q", texts(root))
	}
	if !p.library.SignedIn() || p.library.Current() != web.StateReady {
		t.Fatal("a 403 on the history made the sign-in refused")
	}
}

func TestATokenWithoutTheHistoryScopeAsksNothing(t *testing.T) {
	api := &webAPI{recent: recentlyPlayedAlbum}
	p, s, f := idlePlugin(t, api, "streaming user-read-playback-state")
	f.idle()
	eventually(t, "the other device was asked", func() bool { return api.asked("/v1/me/player") == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := api.asked("/v1/me/player/recently-played"); n != 0 {
		t.Fatalf("recently-played asked %d times without its scope", n)
	}
	if got := paneMode(p, s); got != modeIdle {
		t.Fatalf("playback = %v, want idle", got)
	}
	if !p.streamable || p.engine == nil {
		t.Fatal("the missing scope took the engine away")
	}
}

func TestPlayingTheLastTrackStartsItInItsContext(t *testing.T) {
	api := &webAPI{recent: recentlyPlayedAlbum}
	p, s, f := idlePlugin(t, api, allScopes)
	f.idle()
	eventually(t, "the last track", func() bool { return paneMode(p, s) == modePlaying })

	p.doCommand(nil, "p1", "failed", "play-pause")
	eventually(t, "the play", func() bool { return len(f.calls()) == 1 })
	if got := f.calls(); !slices.Equal(got, []string{"play spotify:album:a1 at spotify:track:t1"}) {
		t.Fatalf("engine commands = %v, want a play in the album at the track", got)
	}
	p.mu.Lock()
	state := s.snap.State
	p.mu.Unlock()
	if state != transport.StatePlaying {
		t.Fatalf("state = %v, want playing at once", state)
	}
}

func TestPlayingALastTrackWithNoContextStartsTheTrackAlone(t *testing.T) {
	api := &webAPI{recent: recentlyPlayedAlone}
	p, s, f := idlePlugin(t, api, allScopes)
	f.idle()
	eventually(t, "the last track", func() bool { return paneMode(p, s) == modePlaying })

	p.doCommand(nil, "p1", "failed", "play-pause")
	eventually(t, "the play", func() bool { return len(f.calls()) == 1 })
	if got := f.calls(); !slices.Equal(got, []string{"play spotify:track:t2"}) {
		t.Fatalf("engine commands = %v, want the track alone", got)
	}
}

func TestATrackOfAnotherDeviceShowsAndPlayTransfers(t *testing.T) {
	api := &webAPI{player: otherDevice, recent: recentlyPlayedAlbum}
	p, s, f := idlePlugin(t, api, allScopes)
	f.idle()
	eventually(t, "the track of the other device", func() bool { return paneMode(p, s) == modePlaying })
	p.mu.Lock()
	snap := s.snap
	p.mu.Unlock()
	if snap.Track != "Elsewhere" || snap.State != transport.StatePaused || snap.Position != 42*time.Second {
		t.Fatalf("snapshot = %+v, want Elsewhere paused here", snap)
	}
	if n := api.asked("/v1/me/player/recently-played"); n != 0 {
		t.Fatalf("history asked %d times while another device plays", n)
	}

	// Play is the engine's toggle, which transfers a player that is not
	// active.
	p.doCommand(nil, "p1", "failed", "play-pause")
	eventually(t, "the toggle", func() bool { return len(f.calls()) == 1 })
	if got := f.calls(); !slices.Equal(got, []string{"play-pause"}) {
		t.Fatalf("engine commands = %v, want the toggle that transfers", got)
	}
}

func TestATimerOfAnOldEngineLeavesTheNewEngineConnecting(t *testing.T) {
	var mu sync.Mutex
	var timers []func()
	old := afterFunc
	afterFunc = func(_ time.Duration, fn func()) {
		mu.Lock()
		defer mu.Unlock()
		timers = append(timers, fn)
	}
	t.Cleanup(func() { afterFunc = old })

	p, s, _ := idlePlugin(t, &webAPI{}, "streaming")
	mu.Lock()
	oldTimers := slices.Clone(timers)
	mu.Unlock()
	if len(oldTimers) == 0 {
		t.Fatal("no timer armed for the first engine")
	}
	p.stopEngine()
	p.syncEngine()
	if got := paneMode(p, s); got != modeStarting {
		t.Fatalf("playback = %v after the restart, want connecting", got)
	}
	for _, fn := range oldTimers {
		fn()
	}
	if got := paneMode(p, s); got != modeStarting {
		t.Fatalf("playback = %v, want connecting: the old timer must not flip the new engine", got)
	}
}

func TestAnOldLastTrackAnswerIsDroppedAfterARestart(t *testing.T) {
	api := &webAPI{recent: recentlyPlayedAlbum, gate: make(chan struct{})}
	p, s, f := idlePlugin(t, api, allScopes)
	f.idle()
	eventually(t, "the history request", func() bool { return api.asked("/v1/me/player/recently-played") > 0 })

	// The new engine is not ready, so it asks for nothing of its own.
	f.mu.Lock()
	f.ready, f.snap = false, transport.Snapshot{}
	f.mu.Unlock()
	p.stopEngine()
	p.syncEngine()
	close(api.gate)

	// The old answer lands in a moment, if it lands at all.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		last := p.last
		p.mu.Unlock()
		if last != nil {
			t.Fatal("the answer of an old engine was kept")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := paneMode(p, s); got != modeStarting {
		t.Fatalf("playback = %v, want the new engine still connecting", got)
	}
}
