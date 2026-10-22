package spotify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/transport"
	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/host"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
)

// The child side of TestPlaybackThroughTheHost: the test binary runs
// again as the plugin process, with the plugin-run role the manifest
// names, and a fake backend in place of osascript, Spotify and the network.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "plugin-run" && os.Args[2] == pluginID {
		if os.Getenv("KERO_SPOTIFY_TEST_CHILD") != "1" {
			fmt.Fprintln(os.Stderr, "plugin-run spotify: no test backend")
			os.Exit(1)
		}
		runner := &fileRunner{path: os.Getenv("KERO_SPOTIFY_TEST_SCRIPTS")}
		player := transport.New(runner)
		newBackend = func(*sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
			// No library call of the child reaches Spotify: both roots
			// are a loopback port nothing listens on. The saved sign-in
			// has the streaming scope, so the pane plays.
			client := web.New(web.Config{
				BaseURL:     "http://127.0.0.1:1",
				AccountsURL: "http://127.0.0.1:1",
				OpenURL:     web.OpenURLFunc(func(string) error { return nil }),
				Secrets:     web.MapSecrets{web.TokensKey: streamingTokens()},
			})
			return player, silentSource{}, client
		}
		engineEager = true
		newEngine = func(*sdk.Conn, *web.Client, func(transport.Snapshot), func(error)) playerEngine {
			return desktopEngine{player}
		}
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fileRunner is the fake Runner of the child plugin: it records every
// script in one file, and answers every read with a playing track.
type fileRunner struct {
	path string
	mu   sync.Mutex
}

// Run records the script and answers it.
func (r *fileRunner) Run(_ context.Context, script string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.path == "" {
		return playingRead(), nil
	}
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil
	}
	defer file.Close()
	_, _ = file.WriteString(script + "\n---\n")
	return playingRead(), nil
}

// desktopEngine is the engine of a test: the fake desktop player behind
// the engine's seam, so a test drives the player with a fake runner.
type desktopEngine struct{ *transport.Transport }

func (desktopEngine) Start() error { return nil }
func (desktopEngine) Close() error { return nil }

// streamingTokens is a saved sign-in with the streaming scope.
func streamingTokens() string {
	data, _ := json.Marshal(web.Token{
		AccessToken: "test-access",
		Scope:       web.ScopeStreaming,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	return string(data)
}

// silentSource is a Source that listens to nothing, so the monitor polls.
type silentSource struct{}

func (silentSource) Start(func()) error { return nil }
func (silentSource) Close() error       { return nil }

// recordingRunner is the fake Runner of a unit test.
type recordingRunner struct {
	mu      sync.Mutex
	scripts []string
	output  string
	err     error
}

// Run records the script and answers with the test's output.
func (r *recordingRunner) Run(_ context.Context, script string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts = append(r.scripts, script)
	if r.err != nil {
		return "", r.err
	}
	if r.output != "" {
		return r.output, nil
	}
	return playingRead(), nil
}

// fail makes every later script fail with err, or succeed for nil.
func (r *recordingRunner) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// calls returns the scripts the runner ran so far.
func (r *recordingRunner) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.scripts...)
}

// playingRead is what one read answers while a track plays.
func playingRead() string {
	sep := "\x1f"
	return strings.Join([]string{
		"playing", "Test Song", "Test Artist", "Test Album",
		"https://i.scdn.co/image/test", "200", "50", "70", "false", "false",
	}, sep) + "\n"
}

// testPlugin returns a plugin with one pane, a fake player, and no
// connection: render is a no-op, and a test reads the tree itself.
func testPlugin(size protocol.Size, playback playbackMode, lib libraryMode) (*plugin, *session, *recordingRunner) {
	runner := &recordingRunner{}
	p := newPlugin()
	p.desktop = transport.New(runner)
	p.engine = desktopEngine{p.desktop}
	p.player = p.engine
	s := &session{
		pane:     "p1",
		widget:   widgetPlayer,
		size:     size,
		playback: playback,
		lib:      lib,
		tab:      tabSearch,
		loaded:   make(map[tabKind]bool),
		snap: transport.Snapshot{
			State:      transport.StatePlaying,
			Track:      "Test Song",
			Artist:     "Test Artist",
			Album:      "Test Album",
			ArtworkURL: "https://i.scdn.co/image/test",
			Duration:   200 * time.Second,
			Position:   50 * time.Second,
			Volume:     70,
			ReadAt:     time.Now(),
		},
	}
	p.panes["p1"] = s
	return p, s, runner
}

// twinPane returns another pane that shows what a pane shows. A session
// holds a lock, so a test builds the twin and does not copy it.
func twinPane(s *session, pane string) *session {
	return &session{
		pane:     pane,
		widget:   s.widget,
		size:     s.size,
		playback: s.playback,
		lib:      s.lib,
		tab:      s.tab,
		loaded:   make(map[tabKind]bool),
		snap:     s.snap,
	}
}

// countRenders replaces drawTree with a counter, and returns the count.
func countRenders(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	n := 0
	previous := drawTree
	drawTree = func(*sdk.Conn, string, protocol.Tree) { mu.Lock(); n++; mu.Unlock() }
	t.Cleanup(func() { drawTree = previous })
	return func() int { mu.Lock(); defer mu.Unlock(); return n }
}

// nodes is every node of a tree in draw order.
func nodes(root *protocol.Node) []*protocol.Node {
	all := []*protocol.Node{root}
	for _, child := range root.Children {
		all = append(all, nodes(child)...)
	}
	return all
}

// find returns the node with an id, or nil.
func find(root *protocol.Node, id string) *protocol.Node {
	for _, node := range nodes(root) {
		if node.ID == id {
			return node
		}
	}
	return nil
}

// texts joins the text of every text node, which is what a pane says.
func texts(root *protocol.Node) string {
	var b strings.Builder
	for _, node := range nodes(root) {
		if node.Type == "text" || node.Type == "emptyState" {
			b.WriteString(node.Text)
			b.WriteString(" ")
		}
	}
	return b.String()
}

// draw builds the tree of a session and checks it against the protocol.
func draw(t *testing.T, p *plugin, s *session) *protocol.Node {
	t.Helper()
	tree := p.tree(s)
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("tree: %v", err)
	}
	return tree.Root
}

func TestSmallLayout(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 280, Height: 150}, modePlaying, libReady)
	root := draw(t, p, s)

	art := find(root, "art")
	if art == nil || art.Width != 48 || art.Height != 48 || !art.Tint {
		t.Fatalf("art = %+v, want 48 by 48 and tinted", art)
	}
	title := find(root, "title")
	if title == nil || title.Text != "Test Song" || !title.Marquee {
		t.Fatalf("title = %+v, want the marquee track", title)
	}
	if play := find(root, "play"); play == nil || !play.Primary || play.Width != 40 {
		t.Fatalf("play = %+v, want a 40 point disc", play)
	}
	for _, id := range []string{"artist", "progress"} {
		if find(root, id) == nil {
			t.Errorf("small player has no %q", id)
		}
	}
	for _, id := range []string{"prev", "next", "seek", "search", "results"} {
		if find(root, id) != nil {
			t.Errorf("the small player drew %q", id)
		}
	}
	if got := find(root, "progress"); got.Value < 0.24 || got.Value > 0.26 {
		t.Errorf("progress = %v, want a quarter of the track", got.Value)
	}
}

func TestMediumLayout(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	for i := range 8 {
		s.entries = append(s.entries, entry{id: fmt.Sprintf("album-%d", i), title: "Album", uri: "spotify:album:a1"})
	}
	root := draw(t, p, s)

	if art := find(root, "art"); art == nil || art.Width != 64 || !art.Tint {
		t.Fatalf("art = %+v, want 64 wide and tinted", art)
	}
	if play := find(root, "play"); play == nil || !play.Primary || play.Width != 44 {
		t.Fatalf("play = %+v, want a 44 point disc", play)
	}
	for _, id := range []string{"prev", "next", "progress"} {
		if find(root, id) == nil {
			t.Errorf("medium player has no %q", id)
		}
	}
	results := find(root, "results")
	if results == nil || results.Variant != protocol.VariantGrid || results.Width < 48 {
		t.Fatalf("results = %+v, want a grid of tiles", results)
	}
	// One row: the tiles that fit the pane's width, no more.
	if room := rowRoom(400); len(results.Items)*(results.Width+gridGap)-gridGap > room {
		t.Fatalf("%d tiles of %d do not fit one row of %d", len(results.Items), results.Width, room)
	}
	if find(root, "seek") != nil || find(root, "tabs") != nil {
		t.Error("the medium player drew the large controls")
	}
}

func TestLargeLayout(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}
	root := draw(t, p, s)

	art := find(root, "art")
	if art == nil || art.Width != 56 || art.Height != 56 || !art.Tint {
		t.Fatalf("art = %+v, want 56 by 56 and tinted", art)
	}
	if title := find(root, "title"); title == nil || !title.Marquee || title.Text != "Test Song" || title.Role != protocol.RoleTitle {
		t.Fatalf("title = %+v, want the marquee title", title)
	}
	if play := find(root, "play"); play == nil || !play.Primary || play.Width != 40 {
		t.Fatalf("play = %+v, want a 40 point disc", play)
	}
	seek := find(root, "seek")
	if seek == nil || seek.Type != "slider" || seek.Step == 0 {
		t.Fatalf("seek = %+v, want a slider", seek)
	}
	volume := find(root, "volume")
	if volume == nil || volume.Type != "slider" || volume.Width != volumeWidth || volume.Value != 70 {
		t.Fatalf("volume = %+v, want an 80 point slider at 70", volume)
	}
	// Two rows: the head, then the seek bar and the volume, side by side.
	head, bar := find(root, "head"), find(root, "seek-row")
	if head == nil || bar == nil || find(head, "repeat") == nil || find(bar, "mute") == nil || find(bar, "volume") == nil {
		t.Fatalf("the wide player is not two rows: head %+v, bar %+v", head, bar)
	}
	if mute := find(root, "mute"); mute == nil || mute.Icon != "volume" {
		t.Fatalf("mute = %+v, want the speaker symbol", mute)
	}
	for _, id := range []string{"tab-search", "tab-playlists", "tab-albums", "tab-queue", "search", "results"} {
		if find(root, id) == nil {
			t.Errorf("large player has no %q", id)
		}
	}
	if results := find(root, "results"); results.Variant != protocol.VariantGrid {
		t.Errorf("results = %+v, want a grid", results)
	}
}

func TestLayoutOfASize(t *testing.T) {
	cases := []struct {
		size protocol.Size
		want layout
	}{
		{protocol.Size{}, layoutLarge},
		{protocol.Size{Width: 280, Height: 150}, layoutSmall},
		{protocol.Size{Width: 299, Height: 600}, layoutSmall},
		{protocol.Size{Width: 600, Height: 229}, layoutSmall},
		{protocol.Size{Width: 300, Height: 230}, layoutMedium},
		{protocol.Size{Width: 900, Height: 419}, layoutMedium},
		{protocol.Size{Width: 300, Height: 420}, layoutLarge},
	}
	for _, test := range cases {
		if got := layoutOf(test.size); got != test.want {
			t.Errorf("layoutOf(%+v) = %v, want %v", test.size, got, test.want)
		}
	}
}

// Without the engine the pane asks for the sign-in that plays here, and
// never offers to open the desktop app.
func TestTheGateAsksForASignInBeforePlaying(t *testing.T) {
	cases := []struct {
		name       string
		lib        libraryMode
		streamable bool
		signingIn  bool
		id         string
	}{
		{"no client id", libNoClientID, false, false, "setup"},
		{"signed out", libSignedOut, false, false, "sign-in"},
		{"signed in before streaming", libReady, false, false, "sign-in-again"},
		{"token refused", libTokenRefused, false, false, "sign-in-again"},
		{"engine did not start", libReady, true, false, "state-unavailable"},
		{"signing in", libSignedOut, false, true, "state-signing-in"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, test.lib)
			p.engine, p.player = nil, p.desktop
			p.streamable = test.streamable
			s.signingIn = test.signingIn
			root := draw(t, p, s)
			if find(root, test.id) == nil {
				t.Fatalf("no %q in %q", test.id, texts(root))
			}
			for _, id := range []string{"play", "open-spotify", "get-spotify", "privacy", "results"} {
				if find(root, id) != nil {
					t.Errorf("the gate drew %q", id)
				}
			}
		})
	}
}

func TestEveryStateHasItsOwnView(t *testing.T) {
	playbackCases := []struct {
		mode playbackMode
		id   string
		text string
	}{
		{modeUnavailable, "state-unavailable", ""},
	}
	for _, test := range playbackCases {
		t.Run(fmt.Sprintf("playback %d", test.mode), func(t *testing.T) {
			p, s, _ := testPlugin(protocol.Size{Width: 280, Height: 150}, test.mode, libNoClientID)
			root := draw(t, p, s)
			if state := find(root, test.id); state == nil {
				t.Fatalf("no %q state in %q", test.id, texts(root))
			}
		})
	}

	libraryCases := []struct {
		mode libraryMode
		id   string
	}{
		{libNoClientID, "setup"},
		{libSignedOut, "sign-in"},
		{libTokenRefused, "sign-in-again"},
		{libPremiumLapsed, "state-premium"},
		{libNetworkDown, "state-offline"},
		{libRateLimited, "state-rate-limited"},
		{libFailed, "state-library-failed"},
	}
	for _, test := range libraryCases {
		t.Run(fmt.Sprintf("library %d", test.mode), func(t *testing.T) {
			p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, test.mode)
			root := draw(t, p, s)
			if state := find(root, test.id); state == nil {
				t.Fatalf("no %q state in %q", test.id, texts(root))
			}
		})
	}
}

func TestNetworkDownKeepsTheLastResults(t *testing.T) {
	var down bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()

	raw, err := json.Marshal(web.Token{AccessToken: "access", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	secrets := web.MapSecrets{web.TokensKey: string(raw)}
	client := web.New(web.Config{
		BaseURL: server.URL,
		Secrets: secrets,
		OpenURL: web.OpenURLFunc(func(string) error { return nil }),
	})
	client.SetClientID("client")
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	p.library = client

	s.query = "one"
	p.runSearch("p1", "one")
	eventually(t, "the search results", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(s.entries) == 1 && s.entries[0].title == "Album One"
	})

	// A new query that fails offline gets the client's last results, which
	// are for "one": they stay kept, but out of the list of "two".
	down = true
	s.query = "two"
	p.runSearch("p1", "two")
	eventually(t, "the offline state", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libNetworkDown && !s.loading
	})
	if len(s.entries) != 0 {
		t.Fatalf("entries = %+v under %q, want none of another query", s.entries, "two")
	}
	if s.search.Query != "one" {
		t.Fatalf("kept results are for %q, want the last good search", s.search.Query)
	}

	s.query = "one"
	p.runSearch("p1", "one")
	eventually(t, "the kept results of the same query", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libNetworkDown && !s.loading && len(s.entries) == 1
	})
	if s.entries[0].title != "Album One" {
		t.Fatalf("entries = %+v, want the last results", s.entries)
	}
	root := draw(t, p, s)
	if find(root, "offline") == nil || find(root, "results") == nil {
		t.Fatalf("tree = %q, want the note and the kept results", texts(root))
	}
}

// A drag of the seek slider changes nothing and draws nothing: the host holds
// the knob. The release seeks, and draws once.
func TestSeekCommitsOnRelease(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	renders := countRenders(t)
	p.conn = &sdk.Conn{}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "seek", Kind: protocol.KindChange, Value: 150.0})
	if len(runner.calls()) != 0 || renders() != 0 {
		t.Fatalf("a drag ran %d scripts and drew %d trees", len(runner.calls()), renders())
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "seek", Kind: protocol.KindCommit, Value: 150.0})
	eventually(t, "the seek command", func() bool {
		for _, script := range runner.calls() {
			if strings.Contains(script, "set player position to 150.000") {
				return true
			}
		}
		return false
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.snap.Position != 150*time.Second {
		t.Fatalf("position = %s after the seek, want 150s", s.snap.Position)
	}
	if renders() != 1 {
		t.Fatalf("the release drew %d trees, want 1", renders())
	}
}

// At the large size a tile opens its tracks; where a track list has no room,
// the medium size, a tile plays at once.
func TestPlayFromASearchResult(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 400, Height: 320}, modePlaying, libReady)
	s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "results", Kind: protocol.KindSelect, Value: "album-a1"})
	eventually(t, "the play command", func() bool {
		for _, script := range runner.calls() {
			if strings.Contains(script, `play track "spotify:album:a1"`) {
				return true
			}
		}
		return false
	})
}

func TestCommandsAndPalette(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	_ = s

	p.command(nil, "next")
	eventually(t, "the next command", func() bool {
		for _, script := range runner.calls() {
			if strings.Contains(script, "next track") {
				return true
			}
		}
		return false
	})

	p.command(nil, "previous")
	eventually(t, "the previous command", func() bool {
		for _, script := range runner.calls() {
			if strings.Contains(script, "previous track") {
				return true
			}
		}
		return false
	})
}

// eventually waits for a condition, and fails the test after five seconds.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestPlaybackThroughTheHost starts the real host, which starts this test
// binary with the plugin-run role and the fake backend, and sends a
// palette command through the protocol.
func TestPlaybackThroughTheHost(t *testing.T) {
	scripts := filepath.Join(t.TempDir(), "scripts.log")
	t.Setenv("KERO_SPOTIFY_TEST_CHILD", "1")
	t.Setenv("KERO_SPOTIFY_TEST_SCRIPTS", scripts)

	spotify, ok := registry.Find(registry.Known(""), pluginID)
	if !ok {
		t.Fatal("the registry does not know the spotify plugin")
	}
	renders := make(chan *protocol.Tree, 8)
	crashes := make(chan string, 2)
	ready := make(chan string, 1)
	h := host.New(host.Config{
		Dir:     t.TempDir(),
		Plugins: []registry.Plugin{spotify},
		Callbacks: host.Callbacks{
			Ready:  func(plugin string) { ready <- plugin },
			Render: func(_, _ string, tree *protocol.Tree) { renders <- tree },
			Crash:  func(_, message, _ string) { crashes <- message },
		},
		StopDelay: 100 * time.Millisecond,
		Command: func(registry.Plugin) ([]string, error) {
			return []string{os.Args[0], "plugin-run", pluginID}, nil
		},
	})
	defer h.Stop()

	if err := h.Open(pluginID, host.Pane{
		ID:     "p1",
		Widget: widgetPlayer,
		Size:   protocol.Size{Width: 700, Height: 600},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case plugin := <-ready:
		if plugin != pluginID {
			t.Fatalf("ready for %q", plugin)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not say hello")
	}

	h.Visible("p1", true)
	deadline := time.After(6 * time.Second)
	for {
		select {
		case tree := <-renders:
			if err := tree.ValidateTree(); err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(texts(tree.Root), "Test Song") {
				if find(tree.Root, "setup") == nil {
					t.Fatal("the real host did not draw the pane of a client without a Client ID")
				}
				goto rendered
			}
		case message := <-crashes:
			t.Fatalf("the plugin crashed: %s", message)
		case <-deadline:
			t.Fatal("the plugin never drew the playing track")
		}
	}
rendered:

	h.Command("p1", "next")
	eventually(t, "the next command through the host", func() bool {
		data, err := os.ReadFile(scripts)
		return err == nil && strings.Contains(string(data), "next track")
	})

	h.Stop()
	select {
	case message := <-crashes:
		t.Fatalf("the plugin crashed: %s", message)
	case <-time.After(200 * time.Millisecond):
	}
}

// The manifest is embedded and valid, and the plugin's widget is the
// player the pane opens.
func TestManifestIsRegistered(t *testing.T) {
	plugin, ok := registry.Find(registry.Known(""), pluginID)
	if !ok {
		t.Fatal("spotify is not in the built-in registry")
	}
	if len(plugin.Manifest.Widgets) != 1 || plugin.Manifest.Widgets[0].Name != widgetPlayer {
		t.Fatalf("widgets = %+v", plugin.Manifest.Widgets)
	}
	if !plugin.Builtin {
		t.Fatal("spotify is not builtin")
	}
	want := []string{"secrets", "openURL", "web", "ambient"}
	if len(plugin.Manifest.Permissions) != len(want) {
		t.Fatalf("permissions = %v, want %v", plugin.Manifest.Permissions, want)
	}
	for i, permission := range plugin.Manifest.Permissions {
		if permission != want[i] {
			t.Fatalf("permissions = %v, want %v", plugin.Manifest.Permissions, want)
		}
	}
	var clientID bool
	for _, setting := range plugin.Manifest.Settings {
		if setting.Key == web.SettingClientID {
			clientID = !setting.Secret
		}
	}
	if !clientID {
		t.Fatal("the clientId setting is missing or secret")
	}
}

// The JSON encoding of a saved state is what a restart reads.
func TestSavedStateRoundTrips(t *testing.T) {
	data, err := json.Marshal(persisted{Tab: int(tabAlbums), Query: "miles"})
	if err != nil {
		t.Fatal(err)
	}
	var got persisted
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tab != int(tabAlbums) || got.Query != "miles" {
		t.Fatalf("state = %+v", got)
	}
}

// pluginLine is one JSON-RPC line the test sends as the host would.
func pluginLine(t *testing.T, id int, method string, params any) []byte {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	req := protocol.Request{JSONRPC: "2.0", Method: method, Params: raw}
	if id > 0 {
		req.ID = json.RawMessage(fmt.Sprint(id))
	}
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

// exitsWhenStdinCloses runs cmd as a plugin process, as the host would but
// without the host's kill: it says hello, opens a visible pane so that the
// monitor reads, answers the plugin's calls until a render shows the
// playing track, then closes stdin, as a crashed app does, and waits for
// the process to end on its own.
func exitsWhenStdinCloses(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
	})

	var writeMu sync.Mutex
	write := func(line []byte) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, _ = stdin.Write(line)
	}
	drew := make(chan struct{})
	go func() {
		var once sync.Once
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64<<10), protocol.MaxLine)
		for scanner.Scan() {
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			line := scanner.Bytes()
			if json.Unmarshal(line, &msg) != nil || msg.Method == "" {
				continue
			}
			if msg.Method == protocol.MethodRender && strings.Contains(string(line), "Test Song") {
				once.Do(func() { close(drew) })
			}
			if len(msg.ID) > 0 {
				answer, _ := json.Marshal(protocol.Response{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{}`)})
				write(append(answer, '\n'))
			}
		}
	}()

	write(pluginLine(t, 1, protocol.MethodHello, protocol.HelloParams{Protocol: protocol.Version}))
	write(pluginLine(t, 2, protocol.MethodOpen, protocol.OpenParams{
		Pane: "p1", Widget: widgetPlayer, Size: protocol.Size{Width: 700, Height: 600},
	}))
	write(pluginLine(t, 0, protocol.MethodVisible, protocol.VisibleParams{Pane: "p1", Visible: true}))
	select {
	case <-drew:
	case err := <-exited:
		t.Fatalf("the plugin ended before it drew the track: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the plugin never drew the playing track")
	}

	writeMu.Lock()
	_ = stdin.Close()
	writeMu.Unlock()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("the plugin ended with %v after its stdin closed, want a clean exit", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin process kept running after its stdin closed")
	}
}

// TestClosingStdinEndsThePluginProcess runs this test binary in the
// plugin-run role, with the fake backend, and closes its stdin while the
// monitor reads: the process ends, so a crashed app leaves no orphan.
func TestClosingStdinEndsThePluginProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "plugin-run", pluginID)
	cmd.Env = append(os.Environ(),
		"KERO_SPOTIFY_TEST_CHILD=1",
		"KERO_SPOTIFY_TEST_SCRIPTS="+filepath.Join(t.TempDir(), "scripts.log"),
	)
	exitsWhenStdinCloses(t, cmd)
}
