package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
)

// libraryClient is a Web API client that answers from a test server, with a
// saved token and a Client ID, so the library is ready. The server is the
// accounts root too, so a refresh after a 401 reaches the test's
// "/api/token", never accounts.spotify.com.
func libraryClient(t *testing.T, handler http.Handler) *web.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	raw, err := json.Marshal(web.Token{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	client := web.New(web.Config{
		BaseURL:     server.URL,
		AccountsURL: server.URL,
		Secrets:     web.MapSecrets{web.TokensKey: string(raw)},
		OpenURL:     web.OpenURLFunc(func(string) error { return nil }),
	})
	client.SetClientID("client")
	return client
}

// eventuallyScript waits for a script through the fake player.
func eventuallyScript(t *testing.T, runner *recordingRunner, want string) {
	t.Helper()
	eventually(t, "the script "+want, func() bool {
		for _, script := range runner.calls() {
			if strings.Contains(script, want) {
				return true
			}
		}
		return false
	})
}

// The large layout always shows the library, even when nothing plays. A
// player with nothing loaded draws no player at all, so the library has the
// whole pane; a player that failed says so above the library.
func TestLargeLayoutKeepsTheLibraryWhenNothingPlays(t *testing.T) {
	cases := []struct {
		name  string
		mode  playbackMode
		state string
	}{
		{"starting", modeStarting, ""},
		{"idle", modeIdle, ""},
		{"unavailable", modeUnavailable, "state-unavailable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, test.mode, libReady)
			s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}
			root := draw(t, p, s)
			if test.state != "" && find(root, test.state) == nil {
				t.Fatalf("no %q playback state in %q", test.state, texts(root))
			}
			if test.state == "" {
				for _, id := range []string{"state-idle", "state-starting", "play", "art", "head"} {
					if find(root, id) != nil {
						t.Errorf("a pane with nothing loaded draws %q", id)
					}
				}
			}
			for _, id := range []string{"tab-search", "tab-playlists", "tab-albums", "search", "results"} {
				if find(root, id) == nil {
					t.Errorf("the large %s layout has no %q, so the library is unreachable", test.name, id)
				}
			}
		})
	}
}

// An offline search keeps the results of the last good search, through the
// event the search field sends, not only through runSearch, but shows them
// only under the query they are for: a new query that fails offline draws
// the offline state, and the old query typed again draws its kept rows.
func TestOfflineSearchKeepsTheLastResultsThroughTheEventPath(t *testing.T) {
	var down bool
	client := libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/v1/search" {
			_, _ = w.Write([]byte(`{"albums":{"items":[{"id":"a1","uri":"spotify:album:a1","name":"Album One","artists":[{"name":"Artist"}]}]},"playlists":{"items":[]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = client

	s.query = "one"
	p.runSearch("p1", "one")
	eventually(t, "the first results", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(s.entries) == 1 && s.entries[0].title == "Album One"
	})

	down = true
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "search", Kind: protocol.KindSubmit, Value: "two"})
	eventually(t, "the offline search", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libNetworkDown && s.query == "two"
	})
	p.mu.Lock()
	under := append([]entry(nil), s.entries...)
	p.mu.Unlock()
	if len(under) != 0 {
		t.Fatalf("entries = %+v under %q, want no rows of the last query", under, "two")
	}
	if root := draw(t, p, s); find(root, "state-offline") == nil || find(root, "results") != nil {
		t.Fatalf("tree = %q, want the offline state and no rows of another query", texts(root))
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "search", Kind: protocol.KindSubmit, Value: "one"})
	eventually(t, "the kept results under their own query", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libNetworkDown && !s.loading && len(s.entries) == 1 && s.entries[0].title == "Album One"
	})
	root := draw(t, p, s)
	if find(root, "offline") == nil || find(root, "results") == nil {
		t.Fatalf("tree = %q, want the offline note above the kept results", texts(root))
	}

	// A trip to another tab and back does not make the kept rows look
	// fresh: the note stays until a call works.
	p.selectTab("p1", tabPlaylists)
	eventually(t, "the playlists load to end", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !s.loading
	})
	p.selectTab("p1", tabSearch)
	p.mu.Lock()
	root = draw(t, p, s)
	p.mu.Unlock()
	if find(root, "offline") == nil || find(root, "results") == nil {
		t.Fatalf("tree = %q after a tab round trip, want the offline note above the kept results", texts(root))
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "search", Kind: protocol.KindSubmit, Value: ""})
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(s.entries) != 0 {
		t.Fatalf("entries = %+v after clearing the query", s.entries)
	}
}

// The transport controls send their commands: play/pause, the volume commit,
// shuffle and repeat.
func TestTransportControlsSendTheirCommands(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "play", Kind: protocol.KindPress})
	eventuallyScript(t, runner, "playpause")

	before := len(runner.calls())
	// A drag step is neither a command nor a tree: the host holds the knob.
	renders := countRenders(t)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "volume", Kind: protocol.KindChange, Value: 42.0})
	if got := len(runner.calls()); got != before {
		t.Fatalf("a volume drag ran %d scripts", got-before)
	}
	if got := renders(); got != 0 {
		t.Fatalf("a volume drag rendered %d trees", got)
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "volume", Kind: protocol.KindCommit, Value: 42.0})
	eventuallyScript(t, runner, "set sound volume to 42")
	p.mu.Lock()
	volume := s.snap.Volume
	p.mu.Unlock()
	if volume != 42 {
		t.Fatalf("the volume = %d after the release, want 42", volume)
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "shuffle", Kind: protocol.KindPress})
	eventuallyScript(t, runner, "set shuffling to true")
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "repeat", Kind: protocol.KindPress})
	eventuallyScript(t, runner, "set repeating to true")
}

// hasRow reports whether a list part has a row with an id, because the
// rows of a list are its items, not its nodes.
func hasRow(list *protocol.Node, id string) bool {
	if list == nil {
		return false
	}
	for _, item := range list.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

// The Playlists and Albums tabs load their rows from the Web API.
func TestTabsLoadPlaylistsAndAlbums(t *testing.T) {
	client := libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me/playlists":
			_, _ = w.Write([]byte(`{"items":[{"id":"p1","name":"One","description":"First","owner":{"display_name":"Ada","id":"ada"},"tracks":{"total":3},"images":[{"url":"https://i/300","width":300}]}]}`))
		case "/v1/me/albums":
			_, _ = w.Write([]byte(`{"items":[{"added_at":"2026-01-01T00:00:00Z","album":{"id":"a1","uri":"spotify:album:a1","name":"Discovery","album_type":"album","release_date":"2001-03-12","total_tracks":14,"artists":[{"name":"Daft Punk"}],"images":[{"url":"https://i/640","width":640}]}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = client

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-playlists", Kind: protocol.KindPress})
	eventually(t, "the playlists", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.tab == tabPlaylists && len(s.entries) == 1 && s.entries[0].title == "One"
	})
	if root := draw(t, p, s); !hasRow(find(root, "results"), "playlist-p1") {
		t.Fatalf("the playlists tab drew no row: %q", texts(root))
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-albums", Kind: protocol.KindPress})
	eventually(t, "the albums", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.tab == tabAlbums && len(s.entries) == 1 && s.entries[0].title == "Discovery"
	})
	if root := draw(t, p, s); !hasRow(find(root, "results"), "album-a1") {
		t.Fatalf("the albums tab drew no row: %q", texts(root))
	}
}

// A search mixes albums and playlists, and each row says which it is, from
// the locale.
func TestSearchRowsLabelAlbumsAndPlaylists(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	s.search = web.SearchResult{
		Albums:    []web.Album{{ID: "a1", Name: "Album One", URI: "spotify:album:a1"}},
		Playlists: []web.Playlist{{ID: "p1", Name: "Playlist One", URI: "spotify:playlist:p1"}},
	}
	p.mu.Lock()
	s.rebuild()
	entries := append([]entry(nil), s.entries...)
	p.mu.Unlock()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want an album and a playlist", entries)
	}
	if entries[0].trailing != localeAlbum() || entries[1].trailing != localePlaylist() {
		t.Fatalf("trailing = %q and %q, want %q and %q", entries[0].trailing, entries[1].trailing, localeAlbum(), localePlaylist())
	}
}

// The signing-in state replaces the library, and a ready library shows the
// tabs, the search field and the prompt.
func TestSignInAndReadyLibraryStates(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libSignedOut)
	s.signingIn = true
	root := draw(t, p, s)
	if find(root, "state-signing-in") == nil {
		t.Fatalf("tree = %q, want the signing-in state", texts(root))
	}

	ready, readySession, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	root = draw(t, ready, readySession)
	for _, id := range []string{"tab-search", "tab-playlists", "tab-albums", "search", "state-search-prompt"} {
		if find(root, id) == nil {
			t.Errorf("a ready library has no %q", id)
		}
	}
}

// Closing a pane tells the playback monitor it is not visible, so the monitor
// only reads while another Spotify pane is open, and stops when the last one
// closes.
func TestClosingAPaneStopsItsPolling(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.panes["p2"] = twinPane(s, "p2")

	monitor := transport.NewMonitor(p.desktop, silentSource{})
	monitor.Poll = 10 * time.Millisecond
	// A source that starts counts as listening, so the fallback interval
	// is the safety one: shorten both.
	monitor.Safety = 10 * time.Millisecond
	p.monitor = monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := monitor.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()

	p.visible(nil, "p1", true)
	p.visible(nil, "p2", true)
	if !monitor.Showing("p1") || !monitor.Showing("p2") {
		t.Fatal("the monitor does not know both panes are visible")
	}
	reads := func() int { return len(runner.calls()) }
	before := reads()
	eventually(t, "the monitor to read for the visible panes", func() bool { return reads() > before })

	p.close(nil, "p1")
	if monitor.Showing("p1") {
		t.Error("the closed pane is still polled")
	}
	if !monitor.Showing("p2") {
		t.Error("closing p1 also stopped the open pane")
	}

	before = reads()
	eventually(t, "the monitor to keep reading for the open pane", func() bool { return reads() > before })

	p.close(nil, "p2")
	if monitor.Showing("p1") || monitor.Showing("p2") {
		t.Fatal("a closed pane is still in the monitor's visible set")
	}
	eventually(t, "the monitor to stop reading", func() bool {
		first := reads()
		time.Sleep(100 * time.Millisecond)
		return reads() == first
	})
}

// treeCapture records the trees a render would send.
type treeCapture struct {
	mu    sync.Mutex
	roots []*protocol.Node
}

func (c *treeCapture) add(tree protocol.Tree) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roots = append(c.roots, tree.Root)
}

func (c *treeCapture) at(i int) *protocol.Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.roots[i]
}

func (c *treeCapture) last() *protocol.Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.roots) == 0 {
		return nil
	}
	return c.roots[len(c.roots)-1]
}

// elapsedText is the clock of a captured tree.
func elapsedText(t *testing.T, root *protocol.Node) string {
	t.Helper()
	if root == nil {
		t.Fatal("the pane rendered nothing")
	}
	node := find(root, "elapsed")
	if node == nil {
		t.Fatalf("the tree has no elapsed clock: %q", texts(root))
	}
	return node.Text
}

// The tree carries the position at the instant of the read and a rate, and
// the host runs the clocks on: no timer redraws the pane, and a state that
// says nothing new is not drawn again.
func TestPlayingTreeHoldsStillBetweenStates(t *testing.T) {
	capture := &treeCapture{}
	previous := drawTree
	drawTree = func(_ *sdk.Conn, _ string, tree protocol.Tree) { capture.add(tree) }
	t.Cleanup(func() { drawTree = previous })

	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.conn = &sdk.Conn{}
	at := time.Now().Add(-5 * time.Second)
	p.mu.Lock()
	s.snap.URI = "spotify:track:t1"
	s.snap.Position = 50 * time.Second
	s.snap.ReadAt = at
	p.mu.Unlock()

	p.render("p1")
	root := capture.last()
	seek, elapsed := find(root, "seek"), find(root, "elapsed")
	if seek == nil || seek.Value != 50 || seek.Max != 200 || seek.Rate != 1 || seek.At != at.UnixMilli() {
		t.Fatalf("seek = %+v, want 50 of 200 running from the read", seek)
	}
	if elapsed == nil || elapsed.Value != 50 || elapsed.Max != 200 || elapsed.Rate != 1 || elapsed.At != at.UnixMilli() || elapsed.Text != "0:50" {
		t.Fatalf("elapsed = %+v, want 0:50 running from the read", elapsed)
	}
	reads := len(runner.calls())

	// The same state again, a second later, with a slightly different read:
	// the pane keeps its own position and instant, so the tree is the same.
	again := s.snap
	again.ReadAt = at.Add(time.Second)
	again.Position = 51*time.Second + 100*time.Millisecond
	p.playback(again)
	p.playback(again)
	first, last := capture.at(0), capture.last()
	if !reflect.DeepEqual(first, last) {
		t.Fatalf("a repeated state changed the tree:\n%+v\n%+v", find(first, "seek"), find(last, "seek"))
	}
	if got := len(runner.calls()); got != reads {
		t.Fatalf("the pane ran %d backend reads", got-reads)
	}
}

// A paused track has no rate and no instant, and a seek gives the clock a
// new instant to run on from.
func TestPausedAndSeekedTrees(t *testing.T) {
	capture := &treeCapture{}
	previous := drawTree
	drawTree = func(_ *sdk.Conn, _ string, tree protocol.Tree) { capture.add(tree) }
	t.Cleanup(func() { drawTree = previous })

	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.conn = &sdk.Conn{}
	p.mu.Lock()
	s.snap.State = transport.StatePaused
	p.mu.Unlock()
	p.render("p1")
	seek, elapsed := find(capture.last(), "seek"), find(capture.last(), "elapsed")
	if seek.Rate != 0 || seek.At != 0 || elapsed.Rate != 0 || elapsed.At != 0 {
		t.Fatalf("paused seek %+v and elapsed %+v still run", seek, elapsed)
	}

	p.mu.Lock()
	s.snap.State = transport.StatePlaying
	p.mu.Unlock()
	before := time.Now().UnixMilli()
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "seek", Kind: protocol.KindCommit, Value: 120.0})
	seek, elapsed = find(capture.last(), "seek"), find(capture.last(), "elapsed")
	if seek.Value != 120 || elapsed.Value != 120 || elapsed.Text != "2:00" || seek.At < before || elapsed.At != seek.At {
		t.Fatalf("after a seek: seek %+v, elapsed %+v, want 120 from a new instant", seek, elapsed)
	}
}

// A palette command that fails says so on every open pane, not on a random
// one from the map.
func TestEveryPaneHearsAFailedCommand(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.panes["p2"] = twinPane(s, "p2")
	runner.err = errors.New("osascript failed")

	p.command(nil, "next")
	eventually(t, "the failure notice on both panes", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice == localeCommandFailed() && p.panes["p2"].notice == localeCommandFailed()
	})
}

// The compact bar carries the snapshot's fraction at its read time, and the
// host moves it on by rate from at: the time since the read counts once.
func TestCompactProgressCountsTheTimeSinceTheReadOnce(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 280, Height: 150}, modePlaying, libReady)
	readAt := time.Now().Add(-10 * time.Second)
	s.snap.Position = 50 * time.Second
	s.snap.Duration = 200 * time.Second
	s.snap.ReadAt = readAt

	progress := find(draw(t, p, s), "progress")
	if progress == nil {
		t.Fatal("the compact player has no progress bar")
	}
	if progress.Value != 0.25 {
		t.Fatalf("value = %v, want 0.25, the fraction at the read", progress.Value)
	}
	if want := 1.0 / 200; math.Abs(progress.Rate-want) > 1e-12 {
		t.Fatalf("rate = %v, want %v of the track a second", progress.Rate, want)
	}
	if progress.At != readAt.UnixMilli() {
		t.Fatalf("at = %d, want the read time %d", progress.At, readAt.UnixMilli())
	}
	// What the host draws now: value + rate × (now − at). Ten seconds of a
	// 200-second track past the read is 0.30, not 0.35.
	host := progress.Value + progress.Rate*float64(time.Now().UnixMilli()-progress.At)/1000
	if math.Abs(host-0.30) > 0.005 {
		t.Fatalf("the host would draw %v, want 0.30", host)
	}

	s.snap.State = transport.StatePaused
	paused := find(draw(t, p, s), "progress")
	if paused.Rate != 0 || paused.Value != 0.25 {
		t.Fatalf("paused bar = value %v rate %v, want a still 0.25", paused.Value, paused.Rate)
	}
}

// webErrorHandler answers every Web API call with one failure, and refuses
// the refresh that a 401 starts.
func webErrorHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Refresh token revoked"}`))
			return
		}
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "30")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// A real Web API failure, through runSearch and through loadTab, reaches the
// state the pane draws: a refused sign-in, a lapsed Premium account and a
// rate limit each draw their own.
func TestWebErrorsReachThePane(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		lib    libraryMode
		node   string
	}{
		{"token refused", http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`, libTokenRefused, "sign-in-again"},
		{"premium lapsed", http.StatusForbidden, `{"error":{"status":403,"message":"Premium required"}}`, libPremiumLapsed, "state-premium"},
		{"rate limited", http.StatusTooManyRequests, `{"error":{"status":429,"message":"API rate limit exceeded"}}`, libRateLimited, "state-rate-limited"},
	}
	for _, test := range cases {
		for _, path := range []string{"search", "playlists"} {
			t.Run(test.name+" through "+path, func(t *testing.T) {
				p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
				p.library = libraryClient(t, webErrorHandler(test.status, test.body))
				if path == "search" {
					s.query = "one"
					p.runSearch("p1", "one")
				} else {
					p.loadTab("p1", tabPlaylists)
				}
				eventually(t, "the "+test.name+" state", func() bool {
					p.mu.Lock()
					defer p.mu.Unlock()
					return !s.loading && s.lib == test.lib
				})
				p.mu.Lock()
				root := draw(t, p, s)
				p.mu.Unlock()
				if find(root, test.node) == nil {
					t.Fatalf("tree = %q, want %q", texts(root), test.node)
				}
			})
		}
	}
}

// A refused refresh holds until the next sign-in: the state before the next
// call is still Sign In Again, not a plain Sign In.
func TestARefusedSignInLastsUntilTheNextSignIn(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = libraryClient(t, webErrorHandler(http.StatusUnauthorized, `{"error":{"status":401,"message":"Invalid access token"}}`))

	s.query = "one"
	p.runSearch("p1", "one")
	eventually(t, "the refused sign-in", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !s.loading && s.lib == libTokenRefused
	})
	if got := p.libraryState(); got != libTokenRefused {
		t.Fatalf("libraryState = %d after a refused refresh, want libTokenRefused (%d)", got, libTokenRefused)
	}

	// A tab switch and a sync ask the client again, and keep the refusal.
	p.selectTab("p1", tabPlaylists)
	p.syncLibrary("p1")
	p.mu.Lock()
	lib := s.lib
	root := draw(t, p, s)
	p.mu.Unlock()
	if lib != libTokenRefused || find(root, "sign-in-again") == nil {
		t.Fatalf("lib = %d, tree = %q, want Sign In Again to last", lib, texts(root))
	}
}

// A rate limit keeps the last results, of a search and of a tab, and says
// why above them.
func TestARateLimitKeepsTheLastResults(t *testing.T) {
	handler := func(limited *atomic.Bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limited.Load() {
				w.Header().Set("Retry-After", "30")
				http.Error(w, `{"error":{"status":429,"message":"API rate limit exceeded"}}`, http.StatusTooManyRequests)
				return
			}
			switch r.URL.Path {
			case "/v1/search":
				_, _ = w.Write([]byte(`{"albums":{"items":[{"id":"a1","uri":"spotify:album:a1","name":"Album One","artists":[{"name":"Artist"}]}]},"playlists":{"items":[]}}`))
			case "/v1/me/playlists":
				_, _ = w.Write([]byte(`{"items":[{"id":"p1","name":"One","owner":{"display_name":"Ada","id":"ada"}}]}`))
			default:
				http.NotFound(w, r)
			}
		})
	}

	t.Run("search", func(t *testing.T) {
		var limited atomic.Bool
		p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
		p.library = libraryClient(t, handler(&limited))
		s.query = "one"
		p.runSearch("p1", "one")
		eventually(t, "the first results", func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return !s.loading && len(s.entries) == 1
		})
		limited.Store(true)
		p.runSearch("p1", "one")
		eventually(t, "the rate limit", func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return !s.loading && s.lib == libRateLimited
		})
		p.mu.Lock()
		defer p.mu.Unlock()
		root := draw(t, p, s)
		if find(root, "rate-limited") == nil || !hasRow(find(root, "results"), "album-a1") {
			t.Fatalf("tree = %q, want the note above the kept results", texts(root))
		}
	})

	t.Run("tab", func(t *testing.T) {
		var limited atomic.Bool
		p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
		p.library = libraryClient(t, handler(&limited))
		p.loadTab("p1", tabPlaylists)
		eventually(t, "the playlists", func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return !s.loading && len(s.entries) == 1
		})
		limited.Store(true)
		p.mu.Lock()
		s.loaded[tabPlaylists] = false
		p.mu.Unlock()
		p.loadTab("p1", tabPlaylists)
		eventually(t, "the rate limit", func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return !s.loading && s.lib == libRateLimited
		})
		p.mu.Lock()
		defer p.mu.Unlock()
		root := draw(t, p, s)
		if find(root, "rate-limited") == nil || !hasRow(find(root, "results"), "playlist-p1") {
			t.Fatalf("tree = %q, want the note above the kept playlists", texts(root))
		}
	})
}

// A tab whose load failed loads again when it is picked again: the state
// before the call decides, not the failure of the last one.
func TestATabLoadsAgainAfterAFailure(t *testing.T) {
	var broken atomic.Bool
	broken.Store(true)
	client := libraryClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			http.Error(w, `{"error":{"status":400,"message":"Bad request"}}`, http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/v1/me/playlists" {
			_, _ = w.Write([]byte(`{"items":[{"id":"p1","name":"One","owner":{"display_name":"Ada","id":"ada"}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = client

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-playlists", Kind: protocol.KindPress})
	eventually(t, "the failed load", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !s.loading && s.lib == libFailed
	})

	broken.Store(false)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-search", Kind: protocol.KindPress})
	p.mu.Lock()
	root := draw(t, p, s)
	p.mu.Unlock()
	if find(root, "search") == nil {
		t.Fatalf("tree = %q, want the search field after the failure", texts(root))
	}
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "tab-playlists", Kind: protocol.KindPress})
	eventually(t, "the playlists on the retry", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !s.loading && s.lib == libReady && len(s.entries) == 1 && s.entries[0].title == "One"
	})
}

// A failed command's notice shows in every layout, and goes once a command
// works. The sign-in's own notice stays while the sign-in runs.
func TestANoticeShowsEverywhereAndClearsAfterASuccess(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	// The desktop app's player has to be read after a command.
	p.engine = nil
	runner.fail(errors.New("osascript failed"))
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "next", Kind: protocol.KindPress})
	eventually(t, "the failure notice", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice == localeCommandFailed()
	})

	p.mu.Lock()
	for _, layout := range []struct {
		name string
		size protocol.Size
		mode playbackMode
	}{
		{"compact playing", protocol.Size{Width: 280, Height: 150}, modePlaying},
		{"compact idle", protocol.Size{Width: 280, Height: 150}, modeIdle},
		{"compact unavailable", protocol.Size{Width: 280, Height: 150}, modeUnavailable},
		{"large playing", protocol.Size{Width: 700, Height: 600}, modePlaying},
		{"large idle", protocol.Size{Width: 700, Height: 600}, modeIdle},
	} {
		s.size, s.playback = layout.size, layout.mode
		if notice := find(draw(t, p, s), "notice"); notice == nil || notice.Text != localeCommandFailed() {
			t.Errorf("the %s layout does not show the notice", layout.name)
		}
	}
	s.size, s.playback = protocol.Size{Width: 700, Height: 600}, modePlaying
	p.mu.Unlock()

	runner.fail(nil)
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "next", Kind: protocol.KindPress})
	eventually(t, "the notice to clear", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice == ""
	})
	p.mu.Lock()
	if find(draw(t, p, s), "notice") != nil {
		t.Error("the pane still draws a notice after a command worked")
	}
	s.signingIn = true
	s.notice = localeSignInRunning()
	p.mu.Unlock()

	before := len(runner.calls())
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "play", Kind: protocol.KindPress})
	eventually(t, "the play command", func() bool { return len(runner.calls()) > before+1 })
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.notice != localeSignInRunning() {
		t.Fatalf("notice = %q while signing in, want the sign-in's own", s.notice)
	}
}

// The Set Up state and the manifest's help name the redirect URI the
// client signs in with, so the user registers the right one.
func TestSetUpNamesTheRedirectURI(t *testing.T) {
	uri := web.New(web.Config{}).RedirectURI()
	if uri != web.DefaultRedirectURI() {
		t.Fatalf("a client's redirect URI %q is not the default %q", uri, web.DefaultRedirectURI())
	}

	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libNoClientID)
	if setup := find(draw(t, p, s), "setup"); setup == nil || !strings.Contains(setup.Detail, uri) {
		t.Fatalf("setup = %+v, want its detail to name %s", setup, uri)
	}
	p.library = web.New(web.Config{})
	if setup := find(draw(t, p, s), "setup"); setup == nil || !strings.Contains(setup.Detail, p.library.RedirectURI()) {
		t.Fatalf("setup = %+v, want its detail to name the client's %s", setup, p.library.RedirectURI())
	}

	plugin, ok := registry.Find(registry.Known(""), pluginID)
	if !ok {
		t.Fatal("spotify is not in the built-in registry")
	}
	var help string
	for _, setting := range plugin.Manifest.Settings {
		if setting.Key == web.SettingClientID {
			help = setting.Help
		}
	}
	if !strings.Contains(help, uri) {
		t.Fatalf("the Client ID help %q does not name the redirect URI %s", help, uri)
	}
}
