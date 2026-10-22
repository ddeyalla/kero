package linear

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
// the real binary runs as "kero plugin-run linear" against a daemon that
// lives in the process, so the test needs no kerod and no Linear.
const fakeSourceFile = `package linear

import (
	"sync"

	"kero/internal/proto"
)

type fakeDaemon struct {
	mu    sync.Mutex
	fn    func(*proto.State, string)
	state proto.State
}

func newDaemon() Daemon {
	started := int64(1761134400000)
	return &fakeDaemon{state: proto.State{
		Sessions: []proto.Session{{ID: "s1", Name: "work", Panes: []proto.Pane{{ID: "pane-1"}}}},
		Linear: &proto.LinearState{
			Status: proto.LinearOK, FetchedAt: started,
			Viewer: &proto.LinearUser{ID: "me", Name: "Me"},
			States: []proto.LinearWorkflowState{
				{ID: "a", Name: "Todo", Type: "unstarted", Position: 1},
				{ID: "b", Name: "In Progress", Type: "started", Position: 2},
				{ID: "c", Name: "In Review", Type: "started", Position: 3},
				{ID: "d", Name: "Done", Type: "completed", Position: 4},
			},
			Issues: []proto.LinearIssue{
				{ID: "id-1", Identifier: "BRU-1", Title: "Issue number 1", State: "In Progress", StateType: "started", AssigneeID: "me", Assignee: "Me", UpdatedAt: started, URL: "https://linear.app/x/issue/BRU-1"},
				{ID: "id-2", Identifier: "BRU-2", Title: "Issue number 2", State: "Todo", StateType: "unstarted", UpdatedAt: started - 1000},
			},
		},
	}}
}

func (f *fakeDaemon) Watch(fn func(*proto.State, string)) {
	f.mu.Lock()
	f.fn = fn
	st := f.state
	f.mu.Unlock()
	go fn(&st, "")
}

func (f *fakeDaemon) Call(method string, params, out any) error {
	switch method {
	case "linear.issue":
		d := out.(*proto.LinearIssueDetail)
		d.LinearIssue = f.state.Linear.Issues[0]
		d.Description = "The description of the issue."
	case "linear.setState":
		p := params.(proto.LinearParams)
		f.mu.Lock()
		issues := append([]proto.LinearIssue(nil), f.state.Linear.Issues...)
		for i := range issues {
			if issues[i].ID == p.Issue {
				issues[i].State, issues[i].StateType = p.State, "started"
				issues[i].UpdatedAt++
			}
		}
		f.state.Linear.Issues = issues
		st, fn := f.state, f.fn
		f.mu.Unlock()
		go fn(&st, "")
	}
	return nil
}

func (f *fakeDaemon) Close() {}
`

// TestLinearThroughTheRealHost runs the app's own binary as its plugin role,
// with a daemon of its own built in, and checks that the real host starts it,
// draws the list, opens an issue, moves it and keeps what the pane chose.
func TestLinearThroughTheRealHost(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app")
	}
	root := moduleRoot(t)
	bin := buildAppWithFakeDaemon(t, root)

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
	plugin, ok := registry.Find(builtins, "linear")
	if !ok {
		t.Fatal("no linear plugin in the built-in registry")
	}
	if !plugin.Manifest.Permissions.Has(protocol.PermFocus) || !plugin.Manifest.Permissions.Has(protocol.PermOpenURL) {
		t.Fatalf("the manifest should ask for focus and openURL: %v", plugin.Manifest.Permissions)
	}

	ready := make(chan string, 1)
	renders := make(chan protocol.Tree, 64)
	states := make(chan json.RawMessage, 16)
	crashes := make(chan string, 4)
	h := host.New(host.Config{
		Dir:     dir,
		Plugins: []registry.Plugin{plugin},
		Command: func(registry.Plugin) ([]string, error) {
			return []string{bin, "plugin-run", "linear"}, nil
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
				if pane == "pane-1" {
					select {
					case renders <- *tree:
					default:
					}
				}
			},
			State: func(_, _ string, state json.RawMessage) {
				select {
				case states <- state:
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

	if err := h.Open("linear", host.Pane{ID: "pane-1", Widget: "board", Size: protocol.Size{Width: 640, Height: 480}}); err != nil {
		t.Fatalf("open the pane: %v", err)
	}
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the plugin never said hello")
	}
	h.Visible("pane-1", true)
	h.SetFocus("s1")

	list := waitForTree(t, renders, "the issues", func(tr protocol.Tree) bool {
		return find(tr, "i:id-1") != nil && find(tr, "i:id-2") != nil
	})
	if err := list.ValidateTree(); err != nil {
		t.Fatalf("the list is invalid: %v", err)
	}
	if !strings.Contains(treeText(list), "Issue number 1") {
		t.Fatal("no issue title")
	}

	// A fold is kept in the pane's target by the host.
	h.Event("pane-1", "sec:todo", protocol.KindPress, nil)
	waitForTree(t, renders, "the folded section", func(tr protocol.Tree) bool {
		return find(tr, "sec:todo") != nil && find(tr, "i:id-2") == nil
	})
	var saved paneState
	select {
	case raw := <-states:
		if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Collapsed) != 1 || saved.Collapsed[0] != "sec:todo" {
			t.Fatalf("kept %s (%v)", raw, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fold was not kept")
	}

	// Open the issue, then move it.
	h.Event("pane-1", "i:id-1", protocol.KindPress, nil)
	detail := waitForTree(t, renders, "the detail", func(tr protocol.Tree) bool { return find(tr, "d-desc") != nil })
	if err := detail.ValidateTree(); err != nil {
		t.Fatalf("the detail is invalid: %v", err)
	}
	h.Event("pane-1", "d-status", protocol.KindSelect, "s:"+slug("In Review"))
	waitForTree(t, renders, "the move", func(tr protocol.Tree) bool {
		n := find(tr, "d-status")
		return n != nil && n.Label == "In Review"
	})
	h.Event("pane-1", "d-back", protocol.KindPress, nil)
	back := waitForTree(t, renders, "the list again", func(tr protocol.Tree) bool { return find(tr, "sec:current") != nil })
	if n := find(back, "i:id-1-s"); n == nil || n.Icon != "circle-half" {
		t.Fatalf("the issue should still be under way: %+v", n)
	}

	// Hiding the pane leaves the process up.
	h.Visible("pane-1", false)
	time.Sleep(300 * time.Millisecond)
	select {
	case message := <-crashes:
		t.Fatalf("the plugin crashed: %s", message)
	default:
	}
	if !h.Running("linear") {
		t.Fatal("the plugin exited when its pane was hidden")
	}
}

func waitForTree(t *testing.T, ch <-chan protocol.Tree, what string, ok func(protocol.Tree) bool) protocol.Tree {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case tr := <-ch:
			if ok(tr) {
				return tr
			}
		case <-deadline:
			t.Fatalf("no tree showed %s", what)
			return protocol.Tree{}
		}
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

// buildAppWithFakeDaemon builds the app with source.go replaced, so
// plugin-run linear talks to the fake daemon.
func buildAppWithFakeDaemon(t *testing.T, root string) string {
	t.Helper()
	source := filepath.Join(root, "internal", "plugin", "builtin", "linear", "source.go")
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
		t.Fatalf("build the app with the fake daemon: %v\n%s", err, out)
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
