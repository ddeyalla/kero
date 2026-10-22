package daemon

import (
	"testing"

	"kero/internal/proto"
)

// A client that still asks for the Tasks view, in create, split or replace,
// gets the Linear plugin's pane, with the target the migration of the saved
// Tasks panes makes.
func TestTasksViewIsTheLinearPlugin(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	want := linearPluginTarget()
	check := func(what string, p proto.Pane) {
		t.Helper()
		if p.View != proto.PanePlugins || p.Target != want {
			t.Fatalf("%s: view %q target %q, want the Linear plugin's %q", what, p.View, p.Target, want)
		}
	}

	s, err := d.create(proto.CreateParams{View: proto.PaneTasks, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	check("create", s.Panes[0])

	s, err = d.split(proto.CreateParams{Split: s.Panes[0].ID, View: proto.PaneTasks})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Panes) != 2 {
		t.Fatalf("split: %+v", s.Panes)
	}
	check("split", s.Panes[1])

	s, err = d.replacePane(proto.CreateParams{Split: s.Panes[0].ID, View: proto.PaneTasks})
	if err != nil {
		t.Fatal(err)
	}
	check("replace", s.Panes[0])
	for _, p := range s.Panes {
		if p.View == proto.PaneTasks {
			t.Fatalf("a Tasks pane was made: %+v", p)
		}
	}
}
