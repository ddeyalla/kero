package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// libraryClient is a Web API client that answers from a test server, with a
// saved token and a Client ID, so the library is ready.
func libraryClient(t *testing.T, handler http.Handler) *web.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	raw, err := json.Marshal(web.Token{AccessToken: "access", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	client := web.New(web.Config{
		BaseURL: server.URL,
		Secrets: web.MapSecrets{web.TokensKey: string(raw)},
		OpenURL: web.OpenURLFunc(func(string) error { return nil }),
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

// The large player always shows the library under the playback area, even
// when nothing plays: the idle copy itself says to pick an album or a
// playlist there, so the tabs, the search field and the rows must be
// reachable.
func TestLargeLayoutKeepsTheLibraryWhenNothingPlays(t *testing.T) {
	cases := []struct {
		name  string
		mode  playbackMode
		state string
	}{
		{"starting", modeStarting, "state-starting"},
		{"idle", modeIdle, "state-idle"},
		{"not running", modeNotRunning, "open-spotify"},
		{"automation refused", modeAutomation, "privacy"},
		{"unavailable", modeUnavailable, "state-unavailable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, test.mode, libReady)
			s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}
			root := draw(t, p, s)
			if find(root, test.state) == nil {
				t.Fatalf("no %q playback state in %q", test.state, texts(root))
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
// event the search field sends, not only through runSearch.
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
	kept := append([]entry(nil), s.entries...)
	p.mu.Unlock()
	if len(kept) != 1 || kept[0].title != "Album One" {
		t.Fatalf("entries = %+v, want the results of the last good search", kept)
	}
	root := draw(t, p, s)
	if find(root, "offline") == nil || find(root, "results") == nil {
		t.Fatalf("tree = %q, want the offline note above the kept results", texts(root))
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
	p.event(nil, protocol.EventParams{Pane: "p1", Node: "volume", Kind: protocol.KindChange, Value: 42.0})
	p.mu.Lock()
	preview := s.volume
	p.mu.Unlock()
	if preview == nil || *preview != 42 {
		t.Fatalf("volume preview = %v, want 42", preview)
	}
	if got := len(runner.calls()); got != before {
		t.Fatalf("a volume drag ran %d scripts", got-before)
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "volume", Kind: protocol.KindCommit, Value: 42.0})
	eventuallyScript(t, runner, "set sound volume to 42")
	p.mu.Lock()
	preview = s.volume
	p.mu.Unlock()
	if preview != nil {
		t.Fatalf("the volume preview did not clear on release: %v", preview)
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
	second := *s
	second.pane = "p2"
	p.panes["p2"] = &second

	monitor := transport.NewMonitor(p.player, silentSource{})
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

// A tick moves the clock on from the snapshot's rate and at, and reads
// nothing from the backend.
func TestTickerAdvancesTheClockWithoutReads(t *testing.T) {
	capture := &treeCapture{}
	previous := drawTree
	drawTree = func(_ *sdk.Conn, _ string, tree protocol.Tree) { capture.add(tree) }
	t.Cleanup(func() { drawTree = previous })

	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.conn = &sdk.Conn{}
	p.visible(nil, "p1", true)
	p.mu.Lock()
	s.snap.Position = 50 * time.Second
	s.snap.ReadAt = time.Now().Add(-5 * time.Second)
	p.mu.Unlock()

	p.tick()
	if got := elapsedText(t, capture.last()); got != "0:55" {
		t.Fatalf("elapsed = %q, want 0:55", got)
	}
	reads := len(runner.calls())

	p.mu.Lock()
	s.snap.ReadAt = time.Now().Add(-8 * time.Second)
	p.mu.Unlock()
	p.tick()
	if got := elapsedText(t, capture.last()); got != "0:58" {
		t.Fatalf("elapsed = %q, want 0:58", got)
	}
	if got := len(runner.calls()); got != reads {
		t.Fatalf("the ticker ran %d backend reads", got-reads)
	}
}

// A palette command that fails says so on every open pane, not on a random
// one from the map.
func TestEveryPaneHearsAFailedCommand(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	second := *s
	second.pane = "p2"
	p.panes["p2"] = &second
	runner.err = errors.New("osascript failed")

	p.command(nil, "next")
	eventually(t, "the failure notice on both panes", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.notice == localeCommandFailed() && second.notice == localeCommandFailed()
	})
}
