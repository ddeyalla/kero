package spotify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"kero/internal/plugin/builtin/spotify/web"
	"kero/internal/plugin/host"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
)

// spotifyModuleRoot is the checkout the test builds.
func spotifyModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

// goCommand is the Go that built the test, or the one on PATH.
func goCommand(t *testing.T) string {
	t.Helper()
	candidate := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command to build the app")
	}
	return path
}

// buildTestApp builds the app with the fake backend from testdata in place
// of the real one, as usage's end-to-end test does.
func buildTestApp(t *testing.T) string {
	t.Helper()
	root := spotifyModuleRoot(t)
	backend := filepath.Join(root, "internal", "plugin", "builtin", "spotify", "backend.go")
	fake := filepath.Join(root, "internal", "plugin", "builtin", "spotify", "testdata", "e2e_backend.go")
	if _, err := os.Stat(fake); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{backend: fake}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "kero-e2e")
	build := exec.Command(goCommand(t), "build", "-overlay", overlayPath, "-o", bin, ".")
	build.Dir = root
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// waitForSpotifyRender waits for a render the test accepts.
func waitForSpotifyRender(t *testing.T, renders <-chan *protocol.Tree, crashes <-chan string, ok func(*protocol.Tree) bool) *protocol.Tree {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case tree := <-renders:
			if err := tree.ValidateTree(); err != nil {
				t.Fatalf("render: %v", err)
			}
			if ok(tree) {
				return tree
			}
		case message := <-crashes:
			t.Fatalf("the plugin crashed: %s", message)
		case <-deadline:
			t.Fatal("the plugin never drew the pane the test wanted")
		}
	}
}

// TestSpotifyThroughTheRealApp runs the built app as
// "kero plugin-run spotify", with a fake backend, and drives it through the
// real host: the handshake, a render, a palette command and a close.
func TestSpotifyThroughTheRealApp(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app")
	}
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts.log")
	t.Setenv("KERO_HOME", filepath.Join(dir, "home"))
	t.Setenv("KERO_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("KERO_SOCKET", filepath.Join(dir, "kerod.sock"))
	t.Setenv("KERO_PANE", "")
	t.Setenv("KERO_SESSION", "")
	t.Setenv("KERO_SPOTIFY_E2E_SCRIPTS", scripts)
	// The fake library's Web API and accounts root: the built app never
	// calls api.spotify.com or accounts.spotify.com.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			_, _ = w.Write([]byte(`{"albums":{"items":[]},"playlists":{"items":[]}}`))
		case "/v1/me/playlists", "/v1/me/albums":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	t.Setenv("KERO_SPOTIFY_E2E_API", api.URL)

	bin := buildTestApp(t)
	plugins, err := registry.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	plugin, ok := registry.Find(plugins, pluginID)
	if !ok {
		t.Fatal("the registry does not know the spotify plugin")
	}

	ready := make(chan string, 1)
	renders := make(chan *protocol.Tree, 8)
	crashes := make(chan string, 2)
	h := host.New(host.Config{
		Dir:     t.TempDir(),
		Plugins: []registry.Plugin{plugin},
		Callbacks: host.Callbacks{
			Ready:  func(plugin string) { ready <- plugin },
			Render: func(_, _ string, tree *protocol.Tree) { renders <- tree },
			Crash:  func(_, message, _ string) { crashes <- message },
		},
		StopDelay: 200 * time.Millisecond,
		Command: func(registry.Plugin) ([]string, error) {
			return []string{bin, "plugin-run", pluginID}, nil
		},
	})
	defer h.Stop()

	// The app passes its saved settings to the plugin; the fake library
	// needs the Client ID the real app would store.
	if err := h.Store().SetSettings(pluginID, map[string]string{web.SettingClientID: "e2e-client"}); err != nil {
		t.Fatal(err)
	}

	if err := h.Open(pluginID, host.Pane{
		ID:     "pane-1",
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
	case message := <-crashes:
		t.Fatalf("the plugin crashed: %s", message)
	case <-time.After(10 * time.Second):
		t.Fatal("the plugin did not say hello")
	}

	h.Visible("pane-1", true)
	tree := waitForSpotifyRender(t, renders, crashes, func(tree *protocol.Tree) bool {
		return strings.Contains(texts(tree.Root), "Test Song") && find(tree.Root, "search") != nil
	})
	for _, id := range []string{
		"tab-search", "tab-playlists", "tab-albums", "search", "seek",
		"volume", "shuffle", "repeat", "play",
	} {
		if find(tree.Root, id) == nil {
			t.Errorf("the real app drew no %q", id)
		}
	}

	if err := h.Command("pane-1", "next"); err != nil {
		t.Fatalf("command: %v", err)
	}
	eventually(t, "the next command in the real app", func() bool {
		data, err := os.ReadFile(scripts)
		return err == nil && strings.Contains(string(data), "next track")
	})

	h.Close("pane-1")
	eventually(t, "the plugin process to stop", func() bool { return !h.Running(pluginID) })
	h.Stop()
	select {
	case message := <-crashes:
		t.Fatalf("the plugin crashed: %s", message)
	case <-time.After(200 * time.Millisecond):
	}

	// The host kills its process on a stop, which a crashed app cannot:
	// run the real binary without the host, and close its stdin while the
	// macOS run loop runs on its main thread. It must end on its own.
	exitsWhenStdinCloses(t, exec.Command(bin, "plugin-run", pluginID))
}
