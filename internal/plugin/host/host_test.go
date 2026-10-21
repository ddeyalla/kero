package host

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
)

// The fake plugin is this test binary, run again as a plugin process. Its
// mode comes from the environment, so that one binary can render, save,
// crash, refuse a call or speak a protocol from the future.
const (
	fakeEnv    = "KERO_FAKE_PLUGIN"
	fakeMode   = "KERO_FAKE_MODE"
	fakeHelper = "^TestFakePluginHelper$"
)

func TestFakePluginHelper(t *testing.T) {
	if os.Getenv(fakeEnv) != "1" {
		t.Skip("the fake plugin runs as a child process")
	}
	mode := os.Getenv(fakeMode)
	if mode == "version" {
		fakeVersion()
		return
	}
	if mode == "early" {
		fakeEarly()
		return
	}
	if mode == "prompt" {
		fakePrompt()
		return
	}
	if mode == "stall" {
		// A plugin that stops reading its stdin: the app must not wait
		// for it, and must end it instead.
		fakeStall()
		return
	}
	if mode == "bad" {
		// A line that is not JSON: the host answers a parse error and
		// keeps the process.
		fmt.Fprintln(os.Stdout, "this is not a message")
	}
	err := sdk.Run(sdk.Handlers{
		Open: func(c *sdk.Conn, e sdk.OpenEvent) {
			switch mode {
			case "crash":
				fmt.Fprintln(os.Stderr, "the fake plugin says bye")
				os.Exit(1)
			case "state":
				// The state the pane came back with, as the plugin got it.
				_ = c.Render(e.Pane, textTree("state:"+string(e.State)))
			case "deny":
				text := "the call was allowed"
				if err := c.Notify("Hello", "from the fake plugin"); err != nil {
					text = err.Error()
				}
				_ = c.Render(e.Pane, textTree(text))
			default:
				_ = c.Render(e.Pane, textTree("hello"))
				_ = c.SaveState(e.Pane, map[string]int{"count": 1})
			}
		},
		Settings: func(c *sdk.Conn, values map[string]string) {
			if mode == "settings" {
				b, _ := json.Marshal(values)
				_ = c.Render("p1", textTree(string(b)))
			}
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// fakeStall says hello, then reads nothing more from its stdin. The host's
// next writes fill the pipe; the host must end the process rather than
// block the caller.
func fakeStall() {
	dec := protocol.NewDecoder(os.Stdin)
	enc := protocol.NewEncoder(os.Stdout)
	var req protocol.Request
	if err := dec.Decode(&req); err != nil {
		os.Exit(1)
	}
	resp, err := protocol.NewResult(req.ID, protocol.HelloResult{Protocol: protocol.Version})
	if err != nil {
		os.Exit(1)
	}
	if err := enc.Encode(resp); err != nil {
		os.Exit(1)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// fakeEarly calls the app before it answers the handshake, and waits for
// the app's answer to that call before it answers hello. Then it draws
// the answer in the first pane that opens.
func fakeEarly() {
	dec := protocol.NewDecoder(os.Stdin)
	enc := protocol.NewEncoder(os.Stdout)
	var hello protocol.Request
	if err := dec.Decode(&hello); err != nil {
		os.Exit(1)
	}
	early, err := protocol.NewRequest(7, protocol.MethodNotify, protocol.NotifyParams{Title: "Too", Body: "early"})
	if err != nil {
		os.Exit(1)
	}
	if err := enc.Encode(early); err != nil {
		os.Exit(1)
	}
	var answer protocol.Response
	if err := dec.Decode(&answer); err != nil {
		os.Exit(1)
	}
	text := "the early call was allowed"
	if answer.Error != nil {
		text = answer.Error.Message
	}
	resp, err := protocol.NewResult(hello.ID, protocol.HelloResult{Protocol: protocol.Version})
	if err != nil {
		os.Exit(1)
	}
	if err := enc.Encode(resp); err != nil {
		os.Exit(1)
	}
	for {
		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			os.Exit(0)
		}
		if req.Method != protocol.MethodOpen {
			continue
		}
		var open protocol.OpenParams
		_ = json.Unmarshal(req.Params, &open)
		render, err := protocol.NewNotification(protocol.MethodRender, protocol.RenderParams{Pane: open.Pane, Tree: textTree(text)})
		if err != nil {
			os.Exit(1)
		}
		_ = enc.Encode(render)
	}
}

// fakePrompt answers hello and calls the app at once, in the same breath:
// the call comes after the answer, so the app must take it.
func fakePrompt() {
	dec := protocol.NewDecoder(os.Stdin)
	enc := protocol.NewEncoder(os.Stdout)
	var hello protocol.Request
	if err := dec.Decode(&hello); err != nil {
		os.Exit(1)
	}
	resp, err := protocol.NewResult(hello.ID, protocol.HelloResult{Protocol: protocol.Version})
	if err != nil {
		os.Exit(1)
	}
	call, err := protocol.NewRequest(8, protocol.MethodNotify, protocol.NotifyParams{Title: "Right", Body: "after hello"})
	if err != nil {
		os.Exit(1)
	}
	if enc.Encode(resp) != nil || enc.Encode(call) != nil {
		os.Exit(1)
	}
	for {
		var line json.RawMessage
		if err := dec.Decode(&line); err != nil {
			os.Exit(0)
		}
	}
}

// fakeVersion answers the handshake with a protocol the app does not
// speak, then waits to be killed.
func fakeVersion() {
	dec := protocol.NewDecoder(os.Stdin)
	enc := protocol.NewEncoder(os.Stdout)
	var req protocol.Request
	if err := dec.Decode(&req); err != nil {
		os.Exit(1)
	}
	resp, err := protocol.NewResult(req.ID, protocol.HelloResult{Protocol: protocol.Version + 98})
	if err != nil {
		os.Exit(1)
	}
	_ = enc.Encode(resp)
	time.Sleep(10 * time.Second)
	os.Exit(0)
}

func textTree(text string) protocol.Tree {
	return protocol.Tree{Root: &protocol.Node{Type: "text", ID: "fake-text", Text: text, Role: "body"}}
}

func textOf(t *protocol.Tree) string {
	var find func(n *protocol.Node) string
	find = func(n *protocol.Node) string {
		if n == nil {
			return ""
		}
		if n.Text != "" {
			return n.Text
		}
		for _, child := range n.Children {
			if s := find(child); s != "" {
				return s
			}
		}
		return ""
	}
	return find(t.Root)
}

type fakeCallbacks struct {
	ready   chan string
	renders chan renderCall
	states  chan stateCall
	crashes chan crashCall
	notify  chan [3]string
}

type renderCall struct {
	plugin string
	pane   string
	tree   *protocol.Tree
}

type stateCall struct {
	plugin string
	pane   string
	state  json.RawMessage
}

type crashCall struct {
	plugin  string
	message string
	logPath string
}

func newFakeCallbacks() *fakeCallbacks {
	return &fakeCallbacks{
		ready:   make(chan string, 4),
		renders: make(chan renderCall, 8),
		states:  make(chan stateCall, 8),
		crashes: make(chan crashCall, 4),
		notify:  make(chan [3]string, 4),
	}
}

func (f *fakeCallbacks) callbacks() Callbacks {
	return Callbacks{
		Ready:   func(plugin string) { f.ready <- plugin },
		Render:  func(plugin, pane string, tree *protocol.Tree) { f.renders <- renderCall{plugin, pane, tree} },
		State:   func(plugin, pane string, state json.RawMessage) { f.states <- stateCall{plugin, pane, state} },
		Notify:  func(plugin, title, body string) { f.notify <- [3]string{plugin, title, body} },
		Crash:   func(plugin, message, logPath string) { f.crashes <- crashCall{plugin, message, logPath} },
		OpenURL: func(plugin, url string) {},
	}
}

func fakeManifest() protocol.Manifest {
	return protocol.Manifest{
		ID:      "fake",
		Name:    "Fake",
		Version: "1.0.0",
		Run:     []string{"fake"},
		Widgets: []protocol.Widget{{Name: "main", Title: "Main"}},
	}
}

// newFakeHost makes a host whose plugin is this test binary. It turns the
// stop delay down so that a test does not wait five seconds for a stop.
func newFakeHost(t *testing.T, manifest protocol.Manifest, mode string) (*Host, *fakeCallbacks) {
	return newFakeHostTuned(t, manifest, mode, 0)
}

// newFakeHostTuned is newFakeHost with the stall timeout a test needs: a
// plugin that stops reading is ended after stall, not after five seconds.
func newFakeHostTuned(t *testing.T, manifest protocol.Manifest, mode string, stall time.Duration) (*Host, *fakeCallbacks) {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeMode, mode)
	f := newFakeCallbacks()
	h := New(Config{
		Dir:          t.TempDir(),
		Plugins:      []registry.Plugin{{Manifest: manifest, Builtin: true}},
		Callbacks:    f.callbacks(),
		StopDelay:    150 * time.Millisecond,
		HelloTimeout: 5 * time.Second,
		StallTimeout: stall,
		Command: func(registry.Plugin) ([]string, error) {
			return []string{os.Args[0], "-test.run=" + fakeHelper}, nil
		},
	})
	t.Cleanup(h.Stop)
	return h, f
}

func waitReady(t *testing.T, f *fakeCallbacks) string {
	t.Helper()
	select {
	case plugin := <-f.ready:
		return plugin
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not say hello")
		return ""
	}
}

func waitRender(t *testing.T, f *fakeCallbacks) renderCall {
	t.Helper()
	select {
	case call := <-f.renders:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin drew nothing")
		return renderCall{}
	}
}

func waitCrash(t *testing.T, f *fakeCallbacks) crashCall {
	t.Helper()
	select {
	case call := <-f.crashes:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not crash")
		return crashCall{}
	}
}

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

func TestPluginDrawsAndSavesState(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "render")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main", Size: protocol.Size{Width: 400, Height: 300}}); err != nil {
		t.Fatal(err)
	}
	if plugin := waitReady(t, f); plugin != "fake" {
		t.Fatalf("ready for %q, want fake", plugin)
	}
	render := waitRender(t, f)
	if render.pane != "p1" || textOf(render.tree) != "hello" {
		t.Fatalf("render = %q on %q", textOf(render.tree), render.pane)
	}
	select {
	case state := <-f.states:
		if state.plugin != "fake" || state.pane != "p1" {
			t.Fatalf("state for %q/%q", state.plugin, state.pane)
		}
		if !json.Valid(state.state) {
			t.Fatalf("state is not JSON: %s", state.state)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin saved no state")
	}
}

func TestOneProcessForTwoPanes(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "render")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main", Size: protocol.Size{Width: 400, Height: 300}}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	if err := h.Open("fake", Pane{ID: "p2", Widget: "main", Size: protocol.Size{Width: 400, Height: 300}}); err != nil {
		t.Fatal(err)
	}
	if !h.Running("fake") {
		t.Fatal("the plugin is not running")
	}
}

func TestStopAfterTheLastPaneCloses(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "render")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	h.Close("p1")
	if !h.Running("fake") {
		t.Fatal("the plugin stopped at once; a reopened pane should find it warm")
	}
	eventually(t, "the plugin to stop", func() bool { return !h.Running("fake") })
}

func TestUnpermittedCallIsRefused(t *testing.T) {
	// The manifest asks for no permission, so notify must be refused.
	h, f := newFakeHost(t, fakeManifest(), "deny")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	render := waitRender(t, f)
	if got := textOf(render.tree); !strings.Contains(got, "did not ask for the permission") {
		t.Fatalf("the refusal reached the plugin as %q", got)
	}
}

func TestBadLineDoesNotKillThePlugin(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "bad")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	if textOf(waitRender(t, f).tree) != "hello" {
		t.Fatal("the plugin did not draw after the bad line")
	}
	if !h.Running("fake") {
		t.Fatal("a bad line killed the plugin")
	}
}

func TestCrashStopsAndOffersNoLoop(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "crash")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	crash := waitCrash(t, f)
	if !strings.Contains(crash.message, "exited") {
		t.Fatalf("crash = %q", crash.message)
	}
	if crash.logPath != h.LogPath("fake") {
		t.Fatalf("log = %q, want %q", crash.logPath, h.LogPath("fake"))
	}
	b, err := os.ReadFile(crash.logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "says bye") {
		t.Fatalf("log = %q", b)
	}
	eventually(t, "the crash to be settled", func() bool { return !h.Running("fake") })
	time.Sleep(200 * time.Millisecond)
	if h.Running("fake") {
		t.Fatal("the plugin restarted itself: a crash loop")
	}
	// The Restart button calls this, and only this.
	if err := h.Restart("fake"); err != nil {
		t.Fatal(err)
	}
	if !h.Running("fake") {
		t.Fatal("restart did not start the plugin")
	}
}

func TestVersionMismatchIsACrash(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "version")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	crash := waitCrash(t, f)
	if !strings.Contains(crash.message, "speaks protocol") {
		t.Fatalf("crash = %q", crash.message)
	}
	eventually(t, "the mismatched plugin to stop", func() bool { return !h.Running("fake") })
}

func TestOpenUnknownPlugin(t *testing.T) {
	h := New(Config{Dir: t.TempDir()})
	t.Cleanup(h.Stop)
	if err := h.Open("nope", Pane{ID: "p1"}); err == nil {
		t.Fatal("opening a plugin the app does not know succeeded")
	}
}

func TestSettingsNeverCarryASecret(t *testing.T) {
	manifest := fakeManifest()
	manifest.Settings = []protocol.Setting{
		{Key: "clientId", Label: "Client ID", Kind: protocol.SettingText},
		{Key: "token", Label: "Token", Kind: protocol.SettingText, Secret: true},
	}
	p := registry.Plugin{Manifest: manifest, Builtin: true}
	h := New(Config{Dir: t.TempDir(), Plugins: []registry.Plugin{p}})
	t.Cleanup(h.Stop)
	if err := h.Store().SetSettings("fake", map[string]string{"clientId": "public", "token": "private"}); err != nil {
		t.Fatal(err)
	}
	values := h.settingsFor(p)
	if values["clientId"] != "public" {
		t.Fatalf("clientId = %q", values["clientId"])
	}
	if _, ok := values["token"]; ok {
		t.Fatal("a secret was sent in settings")
	}
}

func TestSecretsLiveInAFileOnlyTheUserReads(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	// A folder that is there already, open to others, is closed.
	folder := dir + "/fake"
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecret("fake", "token", "sekrit"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(folder)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("folder mode = %o, want 700", got)
	}
	info, err = os.Stat(folder + "/" + secretsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("secrets mode = %o, want 600", got)
	}
	if store.Secret("fake", "token") != "sekrit" {
		t.Fatal("the secret did not read back")
	}
}

func TestABrokenSecretsFileIsKept(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.SetSecret("fake", "token", "sekrit"); err != nil {
		t.Fatal(err)
	}
	path := dir + "/fake/" + secretsFile
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := store.Secret("fake", "token"); got != "" {
		t.Fatalf("a broken file read as %q", got)
	}
	if err := store.SetSecret("fake", "token", "other"); err == nil {
		t.Fatal("writing over a broken file succeeded")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{not json" {
		t.Fatalf("the broken file changed: %q", b)
	}
}

func TestLogRollsAtACap(t *testing.T) {
	dir := t.TempDir()
	w, err := openLog(dir, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	chunk := strings.Repeat("x", 600<<10)
	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > logLimit {
		t.Fatalf("log is %d bytes, over the %d cap", info.Size(), logLimit)
	}
	if _, err := os.Stat(w.Path() + ".1"); err != nil {
		t.Fatal("the full log was not rolled aside")
	}
}

// A plugin that stops reading its stdin must not block the app's calls:
// they come from the UI goroutine. The host ends the process, and the pane
// shows the crash with its Restart button.
func TestAPluginThatStopsReadingDoesNotBlockTheHost(t *testing.T) {
	h, f := newFakeHostTuned(t, fakeManifest(), "stall", 200*time.Millisecond)
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		// More than the pipe holds: a host that writes from the caller
		// would never get here.
		for i := 0; i < 400; i++ {
			h.Event("p1", "node", "click", strings.Repeat("x", 2048))
		}
	}()
	select {
	case <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("the host blocked its caller on a plugin that stopped reading")
	}

	crash := waitCrash(t, f)
	if !strings.Contains(crash.message, "stopped reading") {
		t.Fatalf("crash = %q", crash.message)
	}
	eventually(t, "the plugin to stop", func() bool { return !h.Running("fake") })
	if err := h.Restart("fake"); err != nil {
		t.Fatalf("restart: %v", err)
	}
}

// The state a pane saved in its target reaches the plugin's open, through
// the host, as it was saved.
func TestStateFromTheTargetReachesOpen(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "state")
	state := json.RawMessage(`{"track":"one"}`)
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main", State: state}); err != nil {
		t.Fatal(err)
	}
	call := waitRender(t, f)
	if got := textOf(call.tree); got != `state:{"track":"one"}` {
		t.Fatalf("the plugin opened with %q", got)
	}
}

// A plugin answers hello before it calls the app. A call that comes
// before the handshake is refused, not queued, even with its permission.
func TestCallsBeforeTheHandshakeAreRefused(t *testing.T) {
	manifest := fakeManifest()
	manifest.Permissions = protocol.Permissions{protocol.PermNotify}
	h, f := newFakeHost(t, manifest, "early")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	call := waitRender(t, f)
	if got := textOf(call.tree); !strings.Contains(got, "handshake") {
		t.Fatalf("the early call got %q", got)
	}
	select {
	case n := <-f.notify:
		t.Fatalf("an early call notified: %v", n)
	default:
	}
}

// A call the plugin makes right after its answer to hello is taken, even
// when the app reads both at once: only a call before the answer is early.
func TestACallRightAfterTheHandshakeIsTaken(t *testing.T) {
	manifest := fakeManifest()
	manifest.Permissions = protocol.Permissions{protocol.PermNotify}
	h, f := newFakeHost(t, manifest, "prompt")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-f.notify:
		if n[1] != "Right" {
			t.Fatalf("notify = %v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a call right after hello was refused")
	}
}

// A process that is no longer the plugin's, as the one a restart
// replaced, can end late: that end must not paint a crash over the
// process that runs now.
func TestAStaleCrashChangesNothing(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "render")
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	old := &proc{h: h, plugin: registry.Plugin{Manifest: fakeManifest()}, done: make(chan struct{})}
	h.crashed(old, "the old process exited")
	select {
	case c := <-f.crashes:
		t.Fatalf("a stale process crashed the plugin: %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
	if !h.Running("fake") {
		t.Fatal("a stale crash stopped the running plugin")
	}
}

// A stop timer that fires late, after Close, Open and Close, stops
// nothing: only the timer of the last Close may.
func TestAStaleIdleTimerStopsNothing(t *testing.T) {
	h, f := newFakeHost(t, fakeManifest(), "render")
	h.stopDelay = time.Hour
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	waitReady(t, f)
	h.Close("p1")
	h.mu.Lock()
	stale := h.idle["fake"]
	h.mu.Unlock()
	if err := h.Open("fake", Pane{ID: "p1", Widget: "main"}); err != nil {
		t.Fatal(err)
	}
	h.Close("p1")
	// The first timer, as if it had fired while the pane opened again.
	h.stopIfIdle("fake", stale)
	if !h.Running("fake") {
		t.Fatal("a stale timer stopped the plugin before its delay")
	}
	h.mu.Lock()
	current := h.idle["fake"]
	h.mu.Unlock()
	h.stopIfIdle("fake", current)
	if h.Running("fake") {
		t.Fatal("the last timer did not stop the plugin")
	}
}

// The process can write its stderr after its log closed. The write is
// dropped, and opens no file that nothing would close.
func TestALogWriteAfterCloseIsDropped(t *testing.T) {
	w, err := openLog(t.TempDir(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	n, err := w.Write([]byte("late\n"))
	if err != nil || n != 5 {
		t.Fatalf("Write after Close = %d, %v", n, err)
	}
	if w.file != nil {
		t.Fatal("a write after Close opened the log again")
	}
}

// The secrets live apart from the plugin's code: an install that replaces
// proto.Dir()/plugins/<id> neither wipes them nor brings its own.
func TestTheStoreIsApartFromThePluginsCode(t *testing.T) {
	dir := t.TempDir()
	h := New(Config{Dir: dir, Plugins: []registry.Plugin{{Manifest: fakeManifest(), Builtin: true}}})
	t.Cleanup(h.Stop)
	if err := h.Store().SetSecret("fake", "token", "sekrit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugin-data", "fake", secretsFile)); err != nil {
		t.Fatalf("the secrets are not in the data folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "fake")); !os.IsNotExist(err) {
		t.Fatalf("the store wrote in the code folder: %v", err)
	}
}

// A plugin may open a web link and System Settings' Privacy & Security,
// where the user allows Automation, and no other scheme.
func TestOpenURLAllowsOnlyTheWebAndPrivacySettings(t *testing.T) {
	for _, link := range []string{
		"https://developer.spotify.com/dashboard",
		"http://127.0.0.1:43017/callback",
		"x-apple.systempreferences:com.apple.preference.security",
		"x-apple.systempreferences:com.apple.preference.security?Privacy_Automation",
	} {
		if !openableURL(link) {
			t.Errorf("openableURL(%q) = false, want the link to open", link)
		}
	}
	for _, link := range []string{
		"",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"spotify:album:a1",
		"x-apple.systempreferences:com.apple.preference.general",
		"x-apple.systempreferences:com.apple.preference.securityextra",
		"x-apple.systempreferences:com.apple.preference.security?Privacy Automation",
		"x-apple.systempreferences:com.apple.preference.security?a\nb",
		"X-APPLE.SYSTEMPREFERENCES:com.apple.preference.security",
		"https://",
	} {
		if openableURL(link) {
			t.Errorf("openableURL(%q) = true, want it refused", link)
		}
	}
}
