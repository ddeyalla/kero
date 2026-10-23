package daemon

import (
	"slices"
	"testing"

	"kero/internal/proto"
)

// The sidebar reuses move's project-relative index. Interleaved projects
// must keep their own order, and the chosen order must survive a restart.
func TestSessionMoveWithinProjectPersists(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	d.projects = []proto.Project{{ID: "one", Name: "one", Dir: t.TempDir()}, {ID: "two", Name: "two", Dir: t.TempDir()}}
	var ids []string
	for _, project := range []string{"one", "two", "one", "one"} {
		s, err := d.create(proto.CreateParams{Project: project, View: proto.PaneFiles})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	order := func(d *Daemon, project string) []string {
		var out []string
		for _, s := range d.sessions {
			if s.project == project {
				out = append(out, s.id)
			}
		}
		return out
	}
	for _, tc := range []struct {
		id    string
		index int
		want  []string
	}{
		{ids[3], 0, []string{ids[3], ids[0], ids[2]}},
		{ids[3], 2, []string{ids[0], ids[2], ids[3]}},
		{ids[2], -1, []string{ids[0], ids[3], ids[2]}},
	} {
		if err := d.move(proto.MoveParams{ID: tc.id, Index: tc.index}); err != nil {
			t.Fatal(err)
		}
		if got := order(d, "one"); !slices.Equal(got, tc.want) {
			t.Fatalf("index %d: %v, want %v", tc.index, got, tc.want)
		}
		if got := order(d, "two"); !slices.Equal(got, []string{ids[1]}) {
			t.Fatal("other project moved")
		}
	}
	d.saveState()
	restored := linearTestDaemon(t)
	restored.dir = d.dir
	restored.sessions, restored.panes = nil, map[string]*pane{}
	restoreAll(t, restored)
	if got := order(restored, "one"); !slices.Equal(got, []string{ids[0], ids[3], ids[2]}) {
		t.Fatalf("restart order = %v", got)
	}
}
