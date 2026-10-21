package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
		newBackend = func(*sdk.Conn) (*transport.Transport, transport.Source, *web.Client) {
			client := web.New(web.Config{
				OpenURL: web.OpenURLFunc(func(string) error { return nil }),
				Secrets: web.MapSecrets{},
			})
			return transport.New(runner), silentSource{}, client
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
	p.player = transport.New(runner)
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

func TestCompactLayout(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 280, Height: 150}, modePlaying, libReady)
	root := draw(t, p, s)

	art := find(root, "art")
	if art == nil || art.Width != 40 || art.Height != 40 || !art.Tint {
		t.Fatalf("art = %+v, want 40 by 40 and tinted", art)
	}
	title := find(root, "title")
	if title == nil || title.Text != "Test Song" || !title.Marquee {
		t.Fatalf("title = %+v, want the marquee track", title)
	}
	for _, id := range []string{"artist", "prev", "play", "next", "progress"} {
		if find(root, id) == nil {
			t.Errorf("compact player has no %q", id)
		}
	}
	if find(root, "seek") != nil || find(root, "search") != nil {
		t.Error("the compact player drew the large controls")
	}
	if got := find(root, "progress"); got.Value < 0.24 || got.Value > 0.26 {
		t.Errorf("progress = %v, want a quarter of the track", got.Value)
	}
}

func TestLargeLayout(t *testing.T) {
	p, s, _ := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
	s.entries = []entry{{id: "album-a1", title: "Album One", uri: "spotify:album:a1"}}
	root := draw(t, p, s)

	art := find(root, "art")
	if art == nil || art.Width != 96 || art.Height != 96 || !art.Tint {
		t.Fatalf("art = %+v, want 96 by 96 and tinted", art)
	}
	if title := find(root, "title"); title == nil || !title.Marquee || title.Text != "Test Song" {
		t.Fatalf("title = %+v", title)
	}
	seek := find(root, "seek")
	if seek == nil || seek.Type != "slider" || seek.Step == 0 {
		t.Fatalf("seek = %+v, want a slider", seek)
	}
	volume := find(root, "volume")
	if volume == nil || volume.Type != "slider" {
		t.Fatalf("volume = %+v, want a slider", volume)
	}
	for _, id := range []string{"tab-search", "tab-playlists", "tab-albums", "search", "results"} {
		if find(root, id) == nil {
			t.Errorf("large player has no %q", id)
		}
	}
}

func TestEveryStateHasItsOwnView(t *testing.T) {
	playbackCases := []struct {
		mode playbackMode
		id   string
		text string
	}{
		{modeStarting, "state-starting", ""},
		{modeIdle, "state-idle", ""},
		{modeNotRunning, "open-spotify", ""},
		{modeAutomation, "privacy", ""},
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

	down = true
	p.runSearch("p1", "two")
	eventually(t, "the offline state", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return s.lib == libNetworkDown
	})
	if len(s.entries) != 1 || s.entries[0].title != "Album One" {
		t.Fatalf("entries = %+v, want the last results", s.entries)
	}
	root := draw(t, p, s)
	if find(root, "offline") == nil || find(root, "results") == nil {
		t.Fatalf("tree = %q, want the note and the kept results", texts(root))
	}
}

func TestSeekPreviewsThenCommits(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "seek", Kind: protocol.KindChange, Value: 0.75})
	p.mu.Lock()
	if s.seek == nil || *s.seek != 150*time.Second {
		t.Fatalf("seek preview = %v, want 150s", s.seek)
	}
	preview := s.position()
	p.mu.Unlock()
	if preview != 150*time.Second {
		t.Fatalf("position while dragging = %s, want 150s", preview)
	}
	if len(runner.calls()) != 0 {
		t.Fatal("a drag ran a seek before the release")
	}

	p.event(nil, protocol.EventParams{Pane: "p1", Node: "seek", Kind: protocol.KindCommit, Value: 0.75})
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
	if s.seek != nil {
		t.Fatal("the preview did not clear on release")
	}
}

func TestPlayFromASearchResult(t *testing.T) {
	p, s, runner := testPlugin(protocol.Size{Width: 700, Height: 600}, modePlaying, libReady)
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
	want := []string{"secrets", "openURL"}
	for i, permission := range plugin.Manifest.Permissions {
		if i >= len(want) || permission != want[i] {
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
