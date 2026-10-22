package daemon

import (
	"encoding/json"
	"slices"
	"testing"

	"kero/internal/proto"
)

func TestProjectMoveOrderPersists(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	d.projects = []proto.Project{
		{ID: "one", Name: "one", Dir: t.TempDir()},
		{ID: "two", Name: "two", Dir: t.TempDir()},
		{ID: "three", Name: "three", Dir: t.TempDir()},
	}
	order := func(d *Daemon) []string {
		var ids []string
		for _, p := range d.projects {
			ids = append(ids, p.ID)
		}
		return ids
	}
	for _, tc := range []struct {
		params proto.MoveProjectParams
		want   []string
	}{
		{proto.MoveProjectParams{ID: "three", Index: 0}, []string{"three", "one", "two"}},
		{proto.MoveProjectParams{ID: "three", Index: 2}, []string{"one", "two", "three"}},
		{proto.MoveProjectParams{ID: "one", Index: 1}, []string{"two", "one", "three"}},
	} {
		raw, err := json.Marshal(tc.params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.call(nil, "project.move", raw); err != nil {
			t.Fatal(err)
		}
		if got := order(d); !slices.Equal(got, tc.want) {
			t.Fatalf("project.move %+v: %v, want %v", tc.params, got, tc.want)
		}
	}
	d.saveState()
	restored := linearTestDaemon(t)
	restored.dir = d.dir
	restored.sessions, restored.panes = nil, map[string]*pane{}
	restoreAll(t, restored)
	if got := order(restored); !slices.Equal(got, []string{"two", "one", "three"}) {
		t.Fatalf("restart project order = %v", got)
	}
}
