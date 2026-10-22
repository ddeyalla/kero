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
// cost are fixed, so the test needs no network and no real Mac.
const fakeSourceFile = `package usage

import (
	"context"
	"sync/atomic"
	"time"

	accountusage "kero/internal/usage"
	"kero/internal/usage/cost"
)

type fakeSource struct{}

func (fakeSource) Providers() []accountusage.Report { return fakeReports() }

func (fakeSource) Installed() []string { return nil }

// refreshes counts the reads of Refresh, so that each one differs from the
// last: a refresh whose tree equals the one before is not sent again.
var refreshes atomic.Int64

func (fakeSource) Refresh(context.Context, []accountusage.Report) []accountusage.Report {
	reports := fakeReports()
	reports[0].Windows[0].Used += float64(refreshes.Add(1))
	return reports
}

// costReads counts the scans of the logs.
var costReads atomic.Int64

func (fakeSource) RefreshCost(context.Context, time.Time) error {
	costReads.Add(1)
	return nil
}

func (fakeSource) Spends(time.Time) map[cost.Range][]cost.Spend {
	return map[cost.Range][]cost.Spend{cost.Today: {{Provider: "claude", USD: 12.5, Tokens: 1000}}}
}

func (fakeSource) Flush() error { return nil }

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
				{Label: "5 hours", Used: 8, Resets: now.Add(2 * time.Hour)},
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
	renders := make(chan renderEvent, 32)
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
			Render: func(_, pane string, tree *protocol.Tree) {
				select {
				case renders <- renderEvent{pane: pane, tree: *tree}:
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
	first := waitForRender(t, renders, "pane-1", "58% left")
	if err := first.ValidateTree(); err != nil {
		t.Fatalf("the first tree is invalid: %v", err)
	}
	if !strings.Contains(treeText(first), "Claude") {
		t.Fatalf("the first tree does not show the accounts:\n%s", treeText(first))
	}

	// Showing the pane reads the fake source again and draws again, this
	// time with the cost the visible pane reads.
	drain(renders)
	h.Visible("pane-1", true)
	// Cost and account reads finish independently. Wait for both before
	// pressing Refresh, which coalesces presses while a read is in flight.
	second := waitForRender(t, renders, "pane-1", "$12.50", "57% left")
	if err := second.ValidateTree(); err != nil {
		t.Fatalf("the visible tree is invalid: %v", err)
	}

	// A second pane stays hidden while the first is on screen. The reads the
	// visible pane asks for draw it alone, not the hidden one.
	h.Open("usage", host.Pane{
		ID:     "pane-2",
		Widget: "usage",
		Size:   protocol.Size{Width: 280, Height: 150},
	})
	waitForRender(t, renders, "pane-2", "Claude")
	h.Visible("pane-2", false)

	drain(renders)
	h.Event("pane-1", "usage-refresh", protocol.KindPress, nil)
	// Each read of the fake source is one point higher: showing the pane
	// was the first (43), so the button's is the second (44). A refresh
	// whose tree equals the last is not sent again, so this one must differ.
	waitForRender(t, renders, "pane-1", "56% left")
	time.Sleep(300 * time.Millisecond)
	assertNoRender(t, renders, "pane-2")

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
// plugin-run usage reads the fake accounts and cost.
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

// renderEvent is one tree the host received, and the pane it drew.
type renderEvent struct {
	pane string
	tree protocol.Tree
}

func waitForRender(t *testing.T, ch <-chan renderEvent, pane string, contains ...string) protocol.Tree {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event := <-ch:
			if event.pane != pane {
				continue
			}
			text := treeText(event.tree)
			matches := true
			for _, part := range contains {
				if !strings.Contains(text, part) {
					matches = false
					break
				}
			}
			if matches {
				return event.tree
			}
		case <-deadline:
			t.Fatalf("no %s tree said %q", pane, contains)
			return protocol.Tree{}
		}
	}
}

// assertNoRender checks that a pane the host is not drawing got no frame
// in the ones already queued.
func assertNoRender(t *testing.T, ch <-chan renderEvent, pane string) {
	t.Helper()
	for {
		select {
		case event := <-ch:
			if event.pane == pane {
				t.Fatalf("the hidden pane %s was drawn:\n%s", pane, treeText(event.tree))
			}
		default:
			return
		}
	}
}

func drain(ch <-chan renderEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
