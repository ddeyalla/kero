package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/actions"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
	"kero/locales"
)

func TestPluginPaneComesBackFromItsTarget(t *testing.T) {
	a := &App{}
	target, err := protocol.EncodeTarget(protocol.Target{
		Plugin: "fake",
		Widget: "main",
		State:  json.RawMessage(`{"track":"one"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &proto.Pane{ID: "p1", View: proto.PanePlugins, Target: target}
	st := a.pluginPaneState(p)
	if st.plugin != "fake" || st.widget != "main" {
		t.Fatalf("pane = %q/%q", st.plugin, st.widget)
	}
	if string(st.state) != `{"track":"one"}` {
		t.Fatalf("state = %s", st.state)
	}
	if st.view == nil {
		t.Fatal("the pane has no view")
	}

	// A state the plugin saved reaches the target through the daemon, and
	// the next frame reads it back.
	target, err = protocol.EncodeTarget(protocol.Target{
		Plugin: "fake",
		Widget: "main",
		State:  json.RawMessage(`{"track":"two"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.Target = target
	st = a.pluginPaneState(p)
	if string(st.state) != `{"track":"two"}` {
		t.Fatalf("state after the target changed = %s", st.state)
	}
}

func TestPluginPaneWithABadTargetIsEmpty(t *testing.T) {
	a := &App{}
	st := a.pluginPaneState(&proto.Pane{ID: "p1", View: proto.PanePlugins, Target: "not json"})
	if st.plugin != "" || st.widget != "" {
		t.Fatalf("a bad target read as %q/%q", st.plugin, st.widget)
	}
	if st.view == nil {
		t.Fatal("the pane has no view")
	}
}

func TestOldDaemonSaysRestartTheService(t *testing.T) {
	err := errors.New(`pane.replace: no view "plugin"`)
	got := viewCallError(err)
	if got != locales.PluginNeedsRestart() {
		t.Fatalf("message = %q", got)
	}
	if strings.Contains(got, "no view") {
		t.Fatalf("the raw daemon error reached the user: %q", got)
	}
	// Any other error is the user's to read as it is.
	other := errors.New("the daemon is not running")
	if got := viewCallError(other); got != other.Error() {
		t.Fatalf("message = %q", got)
	}
}

// The first read of proto.Dir()/plugins can wait on a slow volume. It
// happens off the UI goroutine, and drawing reads only the cache: a frame,
// the palette or the chooser never waits for the folder.
func TestPluginDiscoveryIsOffTheUI(t *testing.T) {
	a := testApp(t)
	started, release := make(chan struct{}), make(chan struct{})
	a.plugins.discover = func(string) []registry.Plugin {
		close(started)
		<-release
		return nil
	}
	a.discoverPlugins()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the discovery did not start")
	}
	// The read is still waiting, and already nothing on the UI goroutine
	// waits for it.
	if got := a.knownPlugins(); len(got) != 0 {
		t.Fatalf("the cache holds %d plugins before the read is done", len(got))
	}
	if got := a.pluginWidgets(); len(got) != 0 {
		t.Fatalf("the widgets hold %d rows before the read is done", len(got))
	}
	close(release)
	// The app applies the result with win.Update, which this app has no
	// window for; the test applies the same call.
	a.applyPlugins([]registry.Plugin{{
		Manifest: protocol.Manifest{
			ID:      "fake",
			Name:    "Fake",
			Version: "1.0.0",
			Run:     []string{"fake"},
			Widgets: []protocol.Widget{{Name: "main", Title: "Main"}},
		},
		Builtin: true,
	}})
	if got := a.pluginWidgets(); len(got) != 1 || got[0].plugin != "fake" {
		t.Fatalf("the widgets after the read = %+v", got)
	}
}

func TestPluginWidgetsComeFromTheRegistry(t *testing.T) {
	a := &App{}
	a.plugins.known = []registry.Plugin{{
		Manifest: protocol.Manifest{
			ID:      "fake",
			Name:    "Fake",
			Version: "1.0.0",
			Run:     []string{"fake"},
			Widgets: []protocol.Widget{
				{Name: "main", Title: "Main"},
				{Name: "other", Title: "Other"},
			},
		},
		Builtin: true,
	}}
	widgets := a.pluginWidgets()
	if len(widgets) != 2 {
		t.Fatalf("widgets = %d", len(widgets))
	}
	if widgets[0].plugin != "fake" || widgets[0].widget != "main" || widgets[0].title != "Fake" || widgets[0].detail != "Main" {
		t.Fatalf("first widget = %+v", widgets[0])
	}
	kinds := a.chooserKinds()
	if len(kinds) != len(paneKinds)+2 {
		t.Fatalf("chooser kinds = %d, want %d", len(kinds), len(paneKinds)+2)
	}
	if kinds[len(paneKinds)].plugin != "fake" {
		t.Fatalf("the first plugin row is %+v", kinds[len(paneKinds)])
	}
}

// The fake plugin of the app's tests is this test binary, run again as a
// plugin process. It draws what it heard, in order, as one line: the state
// a pane opened with, whether the pane shows, and the commands it ran. With a mark file, its
// first start crashes on the first open, and the next start lives.
const (
	appFakeEnv    = "KERO_APP_FAKE_PLUGIN"
	appFakeMark   = "KERO_APP_FAKE_MARK"
	appFakeScroll = "KERO_APP_FAKE_SCROLL"
)

func TestAppFakePluginHelper(t *testing.T) {
	if os.Getenv(appFakeEnv) != "1" {
		t.Skip("the fake plugin runs as a child process")
	}
	mark := os.Getenv(appFakeMark)
	var mu sync.Mutex
	last := ""
	heard := map[string][]string{}
	// tell adds what a pane heard to its line, and draws the line.
	tell := func(c *sdk.Conn, pane, what string) {
		mu.Lock()
		heard[pane] = append(heard[pane], what)
		line := strings.Join(heard[pane], " ")
		mu.Unlock()
		if os.Getenv(appFakeScroll) == "1" {
			root := &protocol.Node{Type: "column", ID: "fake-scroll"}
			for i := 0; i < 80; i++ {
				root.Children = append(root.Children, &protocol.Node{Type: "text", ID: fmt.Sprintf("fake-row-%02d", i), Text: fmt.Sprintf("row %02d", i), Role: "body"})
			}
			_ = c.Render(pane, protocol.Tree{Root: root, Scroll: true})
			return
		}
		_ = c.Render(pane, protocol.Tree{Root: &protocol.Node{Type: "text", ID: "fake-text", Text: line, Role: "body"}})
	}
	err := sdk.Run(sdk.Handlers{
		Open: func(c *sdk.Conn, e sdk.OpenEvent) {
			if mark != "" {
				if _, err := os.Stat(mark); err != nil {
					_ = os.WriteFile(mark, nil, 0o600)
					fmt.Fprintln(os.Stderr, "the first start crashes")
					os.Exit(1)
				}
			}
			mu.Lock()
			last = e.Pane
			mu.Unlock()
			tell(c, e.Pane, "state:"+string(e.State))
		},
		Visible: func(c *sdk.Conn, pane string, visible bool) {
			tell(c, pane, fmt.Sprintf("visible:%t", visible))
		},
		Command: func(c *sdk.Conn, id string) {
			mu.Lock()
			pane := last
			mu.Unlock()
			tell(c, pane, "command:"+id)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// fakePluginApp is a test app whose plugin is the fake one, and whose
// session's second pane is a pane of it. What the plugin's process does
// lands on posts, which pump applies between frames, as win.Update would.
func fakePluginApp(t *testing.T, mark bool) (*App, chan func()) {
	t.Helper()
	a := testApp(t)
	t.Setenv(appFakeEnv, "1")
	if mark {
		t.Setenv(appFakeMark, filepath.Join(t.TempDir(), "started"))
	} else {
		t.Setenv(appFakeMark, "")
	}
	posts := make(chan func(), 64)
	a.plugins.post = func(fn func()) { posts <- fn }
	a.plugins.command = func(registry.Plugin) ([]string, error) {
		return []string{os.Args[0], "-test.run=^TestAppFakePluginHelper$"}, nil
	}
	a.plugins.known = []registry.Plugin{{Manifest: fakePluginManifest(), Builtin: true}}
	a.plugins.knownAt = true
	t.Cleanup(a.stopPlugins)
	return a, posts
}

func fakePluginManifest() protocol.Manifest {
	return protocol.Manifest{
		ID:       "fake",
		Name:     "Fake",
		Version:  "1.0.0",
		Run:      []string{"fake"},
		Widgets:  []protocol.Widget{{Name: "main", Title: "Main"}},
		Commands: []protocol.Command{{ID: "refresh", Title: "Refresh Fake"}},
		Settings: []protocol.Setting{
			{Key: "project", Label: "Project", Kind: protocol.SettingText, Help: "Which project it shows", Link: "https://example.com/projects"},
			{Key: "refresh", Label: "Refresh Now", Kind: protocol.SettingAction, Help: "Draw the project again"},
		},
	}
}

// makePluginPane turns pane b of the test session into a pane of the
// fake plugin, with state.
func makePluginPane(t *testing.T, a *App, state string) {
	t.Helper()
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: "fake", Widget: "main", State: json.RawMessage(state)})
	if err != nil {
		t.Fatal(err)
	}
	p := &a.st.Sessions[0].Panes[1]
	p.View, p.Target = proto.PanePlugins, target
}

// heard is the line the fake plugin drew, "" before it drew one.
func heard(tt *ui.Tester) string {
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, "state:") {
			return text
		}
	}
	return ""
}

// pump draws frames and applies what the plugin did until ok holds.
func pump(t *testing.T, tt *ui.Tester, posts chan func(), what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for more := true; more; {
			select {
			case fn := <-posts:
				fn()
			default:
				more = false
			}
		}
		tt.Frame()
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
}

// A plugin that crashes shows why, with a Restart button. The restart
// starts a new process, which hears the open and that the pane shows: a
// pane that showed before the crash must not stay hidden to the new one.
func TestPluginPaneCrashAndRestart(t *testing.T) {
	a, posts := fakePluginApp(t, true)
	makePluginPane(t, a, `{"n":1}`)
	tt := ui.NewTester(a.view, 1100, 640)
	pump(t, tt, posts, "the crash screen", func() bool { return tt.HasText(locales.PluginCrashTitle("Fake")) })
	shot(t, tt, "plugin-crash")
	if !tt.HasText(locales.PluginRestart()) {
		t.Fatalf("the crash has no Restart button; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.PluginRestart()); err != nil {
		t.Fatal(err)
	}
	pump(t, tt, posts, "the new process to hear the pane shows", func() bool { return strings.Contains(heard(tt), "visible:true") })
	if got := heard(tt); !strings.HasPrefix(got, `state:{"n":1}`) {
		t.Fatalf("the new process heard %q, not the open first", got)
	}
	shot(t, tt, "plugin-restarted")
}

// The state a pane saved comes back in its target, and reaches the
// plugin's open on the process the pane starts.
func TestPluginPaneOpensWithItsTargetState(t *testing.T) {
	a, posts := fakePluginApp(t, false)
	makePluginPane(t, a, `{"track":"one"}`)
	tt := ui.NewTester(a.view, 1100, 640)
	pump(t, tt, posts, "the state to reach the plugin", func() bool { return heard(tt) != "" })
	if got := heard(tt); !strings.HasPrefix(got, `state:{"track":"one"}`) {
		t.Fatalf("the plugin heard %q", got)
	}
}

// A plugin's state.save writes the pane's target with the daemon call the
// other views use, once for each new state, and only for its own pane.
func TestPluginStateSaveWritesThePaneTarget(t *testing.T) {
	a := testApp(t)
	var saved []string
	a.plugins.saveTarget = func(pane, target string) { saved = append(saved, pane+" "+target) }
	makePluginPane(t, a, "")
	a.pluginPaneState(&a.st.Sessions[0].Panes[1])

	a.pluginStateSaved("fake", "b", json.RawMessage(`{"n":2}`))
	if len(saved) != 1 {
		t.Fatalf("saved = %q", saved)
	}
	pane, target, _ := strings.Cut(saved[0], " ")
	got, err := protocol.DecodeTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if pane != "b" || got.Plugin != "fake" || got.Widget != "main" || string(got.State) != `{"n":2}` {
		t.Fatalf("the target of %s = %+v", pane, got)
	}
	// The same state again, or a state from another plugin, writes nothing.
	a.pluginStateSaved("fake", "b", json.RawMessage(`{"n":2}`))
	a.pluginStateSaved("other", "b", json.RawMessage(`{"n":3}`))
	if len(saved) != 1 {
		t.Fatalf("saved again: %q", saved)
	}
}

// A plugin's command is a palette item and a `kero do` action. It runs on
// the plugin's pane; with no pane, it waits for the pane it opens.
func TestPluginCommandRunsOnItsPane(t *testing.T) {
	a, posts := fakePluginApp(t, false)
	id := actions.PluginCommand("fake", "refresh")
	found := false
	for _, it := range a.paletteItems() {
		found = found || it.title == "Refresh Fake" && it.detail == "Fake"
	}
	if !found {
		t.Fatal("the palette has no item for the plugin's command")
	}

	// No pane yet: the command waits for one.
	a.run(id, nil)
	if got := a.plugins.pending["fake"]; len(got) != 1 || got[0] != "refresh" {
		t.Fatalf("pending = %q", got)
	}
	// The pane the command asked for arrives, opens, and hears it.
	makePluginPane(t, a, "")
	tt := ui.NewTester(a.view, 1100, 640)
	pump(t, tt, posts, "the waiting command to run", func() bool { return strings.Contains(heard(tt), "command:refresh") })
	if len(a.plugins.pending) != 0 {
		t.Fatalf("pending after the open = %q", a.plugins.pending)
	}

	// `kero do` reaches the same command through the daemon's event.
	a.event(nil, proto.Message{Event: "action", Action: id})
	pump(t, tt, posts, "the kero do command to run", func() bool { return strings.Count(heard(tt), "command:refresh") == 2 })

	// A command the manifest does not declare runs nothing.
	a.run(actions.PluginCommand("fake", "nope"), nil)
	if a.pendingToast != locales.PluginNoCommand("Fake", "nope") {
		t.Fatalf("toast = %q", a.pendingToast)
	}
}

// A click on a plugin's notification shows a pane of the plugin, in its
// own session, not a session named as the plugin.
func TestPluginNotificationFocusesItsPane(t *testing.T) {
	a := testApp(t)
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: "fake", Widget: "main"})
	if err != nil {
		t.Fatal(err)
	}
	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: "s2", Project: "p1", Name: "other",
		Panes: []proto.Pane{
			{ID: "c", Shell: "zsh", Dir: "/tmp", Status: proto.Idle, Progress: -1},
			{ID: "d", View: proto.PanePlugins, Target: target},
		},
		Focus: "c",
	})
	// The notification is for a session whose project is folded, in the
	// tabs: the explicit click opens it and asks the tab row to reveal it.
	a.setNavPlacement(navTop)
	a.prefs.Collapsed = map[string]bool{"p1": true}
	a.selected = "s1"

	a.focusPluginPane("fake")
	if a.selected != "s2" {
		t.Fatalf("selected = %q, want s2", a.selected)
	}
	if a.prefs.Collapsed["p1"] {
		t.Fatal("the plugin notification left the project folded")
	}
	if a.session("s2").Project != "p1" {
		t.Fatalf("the session's project changed to %q", a.session("s2").Project)
	}
	if a.nav.reveal != "s2" {
		t.Fatalf("the notification asked to reveal %q, want s2", a.nav.reveal)
	}
	if got := a.focusOf(a.session("s2")); got != "d" {
		t.Fatalf("focus = %q, want d", got)
	}

	// The next frame shows the opened project's tab and consumes the reveal.
	tt := ui.NewTester(a.view, 1280, 700)
	tt.Frame()
	if _, ok := tt.Find(locales.NavSessionTab("other")); !ok {
		t.Fatalf("the revealed session has no tab: %q", tt.Texts())
	}
	if a.nav.reveal != "" {
		t.Fatalf("the reveal request stayed pending: %q", a.nav.reveal)
	}
}

// Settings › Plugins draws each plugin's fields from its manifest, with the
// saved value in each: a field made before the values were read would be
// empty, and Return on it would save the empty value. Closing Settings lets
// go of the drafts.
func TestPluginSettingsFieldsStartWithTheSavedValue(t *testing.T) {
	a, posts := fakePluginApp(t, false)
	if err := a.pluginHost().Store().SetSettings("fake", map[string]string{"project": "saved"}); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1100, 720)
	a.openSettings("plugins")
	tt.Frame()
	if a.ss.pluginDraft["fake.project"] != nil {
		t.Fatal("a draft was made before the saved values were read")
	}
	pump(t, tt, posts, "the saved values", func() bool { return a.plugins.pageLoaded })
	tt.Frame()
	shot(t, tt, "plugin-settings")
	draft := a.ss.pluginDraft["fake.project"]
	if draft == nil || *draft != "saved" {
		t.Fatalf("the field starts as %v, want the saved value", draft)
	}
	if !tt.HasText("Which project it shows") || !tt.HasText(locales.PluginSettingOpenLink()) {
		t.Fatalf("the field has no help or link; texts %q", tt.Texts())
	}
	if tt.HasText("Spotify") {
		t.Fatal("the page names a plugin no manifest declared")
	}
	a.endSettings()
	if a.ss.pluginDraft != nil || a.plugins.pageLoaded {
		t.Fatal("closing Settings kept the drafts")
	}
}

// An action setting is a button in Settings › Plugins, and pressing it runs
// the plugin command its key names.
func TestPluginActionSettingRunsItsCommand(t *testing.T) {
	a, posts := fakePluginApp(t, false)
	makePluginPane(t, a, "")
	tt := ui.NewTester(a.view, 1100, 720)
	pump(t, tt, posts, "the pane to open", func() bool { return strings.HasPrefix(heard(tt), "state:") })

	a.openSettings("plugins")
	pump(t, tt, posts, "the saved values", func() bool { return a.plugins.pageLoaded })
	tt.Frame()
	if !tt.HasText("Refresh Now") || !tt.HasText("Draw the project again") {
		t.Fatalf("the action has no button or help; texts %q", tt.Texts())
	}
	if a.ss.pluginDraft["fake.refresh"] != nil {
		t.Fatal("the action got a text field")
	}
	if err := tt.Click("Refresh Now"); err != nil {
		t.Fatal(err)
	}
	pump(t, tt, posts, "the command to reach the plugin", func() bool { return strings.Contains(heard(tt), "command:refresh") })
}
