package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
)

// testTasks is the session of testApp with pane b showing the Linear
// plugin's board, which the Tasks entry of the chooser opens.
func testTasks(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.show, a.selected = "", "s1"
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: "linear", Widget: "board"})
	if err != nil {
		t.Fatal(err)
	}
	b := &a.st.Sessions[0].Panes[1]
	b.View, b.Shell, b.Target = proto.PanePlugins, "", target
	return a
}

func frames(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
}

// The chooser offers each kind of pane, with its letter and shortcut,
// and a letter picks one: L, the Linear plugin's pane of the session, which has one.
func TestNewPaneChooser(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testTasks(t)
		a.setFocus(a.session("s1"), "a")
		tt := ui.NewTester(a.view, 1280, 720)
		tt.SetDark(dark)
		a.run("pane.new", nil)
		frames(tt)
		for _, want := range []string{"Terminal", "Tasks", "Files", "Browser", "T", "L", "E", "B", "The project's Linear issues"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		shot(t, tt, map[bool]string{false: "pane-chooser-light", true: "pane-chooser-dark"}[dark])
	}
	a := testTasks(t)
	a.setFocus(a.session("s1"), "a")
	tt := ui.NewTester(a.view, 1280, 720)
	a.run("pane.new", nil)
	frames(tt)
	tt.Key(0, ui.KeyL)
	tt.Frame()
	if a.chooser.open {
		t.Fatal("the chooser stayed open after a letter")
	}
	if got := a.focusOf(a.session("s1")); got != "b" {
		t.Fatalf("Tasks focused pane %q, not the session's Linear pane", got)
	}
	a.run("pane.new", nil)
	frames(tt)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.chooser.open {
		t.Fatal("Esc left the chooser open")
	}
}

// Each kind of pane has its action in the Pane menu, with its shortcut.
func TestNewPaneActionsInMenu(t *testing.T) {
	a := testApp(t)
	want := map[string]string{
		"New Pane…":         "CmdOrCtrl+Alt+N",
		"New Terminal Pane": "CmdOrCtrl+Alt+T",
		"New Tasks Pane":    "CmdOrCtrl+Alt+L",
		"New Files Pane":    "CmdOrCtrl+Alt+E",
		"New Browser Pane":  "CmdOrCtrl+Alt+B",
	}
	var walk func([]*mygo.MenuItem)
	walk = func(items []*mygo.MenuItem) {
		for _, it := range items {
			if k, ok := want[it.Label]; ok && it.Accelerator == k {
				delete(want, it.Label)
			}
			walk(it.Submenu)
		}
	}
	walk(a.paneMenuItems())
	if len(want) > 0 {
		t.Fatalf("missing from the menus: %v", want)
	}
}

// A new Tasks pane is the Linear plugin's, made with its board target, so
// that a kerod which has not migrated takes it too.
func TestNewTasksPaneIsALinearPluginPane(t *testing.T) {
	view, target := newPaneView(proto.PaneTasks)
	if view != proto.PanePlugins {
		t.Fatalf("view = %q, want the plugin view", view)
	}
	got, err := protocol.DecodeTarget(target)
	if err != nil || got.Plugin != "linear" || got.Widget != "board" {
		t.Fatalf("target = %q (%v), want the Linear board", target, err)
	}
	if string(got.State) != `{"mode":"list","scope":"project","hideDone":true}` {
		t.Fatalf("state = %s", got.State)
	}
	if v, tg := newPaneView(proto.PaneFiles); v != proto.PaneFiles || tg != "" {
		t.Fatalf("Files became %q %q", v, tg)
	}
}

// A Tasks pane that an older kerod kept is drawn as the Linear plugin's
// board, not blank, and a new Tasks pane takes the focus of it.
func TestUnmigratedTasksPaneShowsThePlugin(t *testing.T) {
	a := testApp(t)
	a.show = ""
	b := &a.st.Sessions[0].Panes[1]
	b.View, b.Shell, b.Target = proto.PaneTasks, "", ""
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	st := a.plugins.panes["b"]
	if st == nil || st.plugin != "linear" || st.widget != "board" {
		t.Fatalf("the Tasks pane did not become the Linear board: %+v", st)
	}
	a.setFocus(a.session("s1"), "a")
	a.openView(a.session("s1"), "", proto.PaneTasks, false)
	if got := a.focusOf(a.session("s1")); got != "b" {
		t.Fatalf("Tasks focused %q, not the session's Tasks pane", got)
	}
}
