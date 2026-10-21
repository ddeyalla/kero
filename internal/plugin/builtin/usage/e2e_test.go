package usage

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"kero/internal/plugin/host"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
)

// fakeSourceFile takes the place of source.go when the test builds the app:
// the real binary runs as "kero plugin-run usage", and its accounts and
// machine are fixed, so the test needs no network and no real Mac.
const fakeSourceFile = `package usage

import (
	"context"
	"time"

	"kero/internal/resources"
	accountusage "kero/internal/usage"
)

type fakeSource struct{}

func (fakeSource) Providers() []accountusage.Report { return fakeReports() }

func (fakeSource) Installed() []string { return nil }

func (fakeSource) Refresh(context.Context, []accountusage.Report) []accountusage.Report {
	return fakeReports()
}

func (fakeSource) Sample() (resources.Sample, error) {
	cpu := 23.0
	return resources.Sample{
		CPU:         &cpu,
		MemoryUsed:  10 << 30,
		MemoryTotal: 32 << 30,
		At:          time.Now(),
	}, nil
}

func fakeReports() []accountusage.Report {
	now := time.Now()
	return []accountusage.Report{
		{
			ID:   "claude",
			Name: "Claude",
			URL:  "https://claude.ai/settings/usage",
			Plan: "max",
			Windows: []accountusage.Window{
				{Label: "Weekly", Used: 42, Resets: now.Add(3 * 24 * time.Hour)},
				{Label: "Session", Used: 8, Resets: now.Add(2 * time.Hour)},
			},
			Checked: now,
		},
		{ID: "deepseek", Name: "DeepSeek", URL: "https://platform.deepseek.com/usage", Error: "Not connected"},
	}
}

func newSource() Source { return fakeSource{} }
`

// TestUsageThroughTheRealHost runs the app's own binary as its plugin role,
// with a fake source built in, and checks that the real host starts it, draws
// its pane and keeps it alive while it is shown and hidden.
func TestUsageThroughTheRealHost(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app")
	}
	root := moduleRoot(t)
	bin := buildAppWithFakeUsage(t, root)

	dir := t.TempDir()
	t.Setenv("KERO_HOME", dir)
	t.Setenv("KERO_CONFIG", filepath.Join(dir, "config"))
	t.Setenv("KERO_SOCKET", filepath.Join(dir, "kero.sock"))
	t.Setenv("KERO_PANE", "")
	t.Setenv("KERO_SESSION", "")

	builtins, err := registry.Builtins()
	if err != nil {
		t.Fatalf("the built-in registry failed: %v", err)
	}
	plugin, ok := registry.Find(builtins, "usage")
	if !ok {
		t.Fatal("no usage plugin in the built-in registry")
	}

	ready := make(chan string, 1)
	renders := make(chan protocol.Tree, 32)
	crashes := make(chan string, 4)
	h := host.New(host.Config{
		Dir:     dir,
		Plugins: []registry.Plugin{plugin},
		// The app's own binary, run as the plugin role the manifest names.
		Command: func(registry.Plugin) ([]string, error) {
			return []string{bin, "plugin-run", "usage"}, nil
		},
		StopDelay: 200 * time.Millisecond,
		Callbacks: host.Callbacks{
			Ready: func(name string) {
				select {
				case ready <- name:
				default:
				}
			},
			Render: func(_, _ string, tree *protocol.Tree) {
				select {
				case renders <- *tree:
				default:
				}
			},
			Crash: func(_, message, _ string) {
				select {
				case crashes <- message:
				default:
				}
			},
		},
	})
	defer h.Stop()

	if err := h.Open("usage", host.Pane{
		ID:     "pane-1",
		Widget: "usage",
		Size:   protocol.Size{Width: 640, Height: 480},
	}); err != nil {
		t.Fatalf("open the pane: %v", err)
	}

	waitForEvent(t, "the plugin to say hello", ready)
	first := waitForRender(t, renders, "42% used")
	if err := first.ValidateTree(); err != nil {
		t.Fatalf("the first tree is invalid: %v", err)
	}
	if !strings.Contains(treeText(first), "Claude") {
		t.Fatalf("the first tree does not show the accounts:\n%s", treeText(first))
	}

	// Showing the pane reads the fake source again and draws again, this
	// time with the machine sample the visible pane takes.
	h.Visible("pane-1", true)
	drain(renders)
	second := waitForRender(t, renders, "This Mac")
	if err := second.ValidateTree(); err != nil {
		t.Fatalf("the visible tree is invalid: %v", err)
	}

	// Hiding it leaves the process up, and it stays up: the plugin stops
	// reading instead of exiting.
	h.Visible("pane-1", false)
	time.Sleep(300 * time.Millisecond)
	select {
	case message := <-crashes:
		t.Fatalf("the plugin crashed after its pane was hidden: %s", message)
	default:
	}
	if !h.Running("usage") {
		t.Fatal("the plugin exited when its pane was hidden")
	}
}

// moduleRoot is the checkout the test runs in, to build the app from it.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller to find the checkout from")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", file)
		}
		dir = parent
	}
}

// buildAppWithFakeUsage builds the app with source.go replaced, so
// plugin-run usage reads the fake accounts and machine.
func buildAppWithFakeUsage(t *testing.T, root string) string {
	t.Helper()
	source := filepath.Join(root, "internal", "plugin", "builtin", "usage", "source.go")
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("no plugin source to replace: %v", err)
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake_source.go")
	if err := os.WriteFile(fake, []byte(fakeSourceFile), 0o644); err != nil {
		t.Fatalf("write the fake source: %v", err)
	}
	body, err := json.Marshal(map[string]any{"Replace": map[string]string{source: fake}})
	if err != nil {
		t.Fatalf("make the overlay: %v", err)
	}
	overlay := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlay, body, 0o644); err != nil {
		t.Fatalf("write the overlay: %v", err)
	}

	bin := filepath.Join(dir, "kero")
	cmd := exec.Command(goTool(t), "build", "-overlay", overlay, "-o", bin, ".")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the app with the fake usage source: %v\n%s", err, out)
	}
	return bin
}

// goTool is the go command that runs this test, which is the toolchain the
// app builds with.
func goTool(t *testing.T) string {
	t.Helper()
	if root := runtime.GOROOT(); root != "" {
		path := filepath.Join(root, "bin", "go")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go tool to build the app with: %v", err)
	}
	return path
}

func waitForEvent(t *testing.T, what string, ch <-chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return ""
	}
}

func waitForRender(t *testing.T, ch <-chan protocol.Tree, contains string) protocol.Tree {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case tree := <-ch:
			if strings.Contains(treeText(tree), contains) {
				return tree
			}
		case <-deadline:
			t.Fatalf("no tree said %q", contains)
			return protocol.Tree{}
		}
	}
}

func drain(ch <-chan protocol.Tree) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
