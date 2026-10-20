package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// testTasks is the session of testApp with pane b showing the tasks of
// its project, linked to Linear's Rig.
func testTasks(t *testing.T) *App {
	t.Helper()
	a := testLinear(t)
	a.show, a.selected = "", "s1"
	a.st.Projects[0].LinearProject = "p1"
	b := &a.st.Sessions[0].Panes[1]
	b.View, b.Shell = proto.PaneTasks, ""
	return a
}

func frames(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
}

// The chooser offers each kind of pane, with its letter and shortcut,
// and a letter picks one: L, the session's Tasks pane, which has one.
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
		t.Fatalf("Tasks focused pane %q, not the session's Tasks pane", got)
	}
	a.run("pane.new", nil)
	frames(tt)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.chooser.open {
		t.Fatal("Esc left the chooser open")
	}
}

// The Tasks pane lists its project's open issues; what is done shows
// when asked.
func TestTasksPane(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testTasks(t)
		tt := ui.NewTester(a.view, 1280, 720)
		tt.SetDark(dark)
		frames(tt)
		for _, want := range []string{"Tasks", "Rig", "BRU-24", "BRU-27", "BRU-33"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		if tt.HasText("BRU-25") {
			t.Errorf("dark=%v: a done issue shows", dark)
		}
		shot(t, tt, map[bool]string{false: "tasks-pane-light", true: "tasks-pane-dark"}[dark])
	}
	a := testTasks(t)
	a.tasksState("b").hideDone = false
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if !tt.HasText("BRU-25") {
		t.Fatalf("Show Done left out a done issue; texts %q", tt.Texts())
	}
}

// A Tasks pane keeps its own view: an issue open in it, and what it
// leaves out, are not the tray board's, nor the board's its.
func TestTasksPaneStateIsItsOwn(t *testing.T) {
	a := testTasks(t)
	a.linRoot.open, a.linRoot.view = "i1", "board"
	st := a.tasksState("b")
	st.open = "i3"
	tt := ui.NewTester(a.view, 1400, 760)
	frames(tt)
	if a.lin != &a.linRoot {
		t.Fatal("a pane's view stayed the app's after drawing")
	}
	if a.linRoot.open != "i1" || a.linRoot.view != "board" || a.linRoot.pane {
		t.Fatalf("the board's view changed: %+v", a.linRoot)
	}
	if st.open != "i3" || st.view != "list" || !st.hideDone {
		t.Fatalf("the pane's view changed: open %q view %q", st.open, st.view)
	}
	// The pane steps through its list, not a board's column.
	if !tt.HasText("BRU-27") || !tt.HasText("1 / 3") {
		t.Fatalf("the pane's issue did not open in its list; texts %q", tt.Texts())
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
