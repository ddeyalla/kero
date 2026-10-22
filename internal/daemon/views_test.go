package daemon

import (
	"strings"
	"testing"

	"kero/internal/proto"
)

// A view runs nothing: a session of one starts no shell, the terminal's
// commands refuse it, and it comes back as it was after a restart.
func TestViewPanes(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	dir := t.TempDir()
	s, err := d.create(proto.CreateParams{View: proto.PaneTasks, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	p := d.panes[s.Panes[0].ID]
	if p.proc != nil || !p.isView() || s.Panes[0].View != proto.PanePlugins || s.Panes[0].Dir != dir {
		t.Fatalf("a view started something: %+v", s.Panes[0])
	}
	if _, err := d.findTermPane(s.ID); err == nil || !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("a session of views gave a terminal: %v", err)
	}
	if err := p.restart(); err == nil {
		t.Fatal("restarted a view")
	}
	if _, err := d.create(proto.CreateParams{View: "nope", Dir: dir}); err == nil {
		t.Fatal("an unknown view was made")
	}

	// The Tasks view is a plugin now; the daemon no longer limits it.
	if _, err := d.replacePane(proto.CreateParams{Split: p.info.ID, View: proto.PaneTasks}); err != nil {
		t.Fatalf("the Tasks pane could not be replaced: %v", err)
	}
	p = d.panes[d.sessions[0].panes[0].info.ID]

	// A browser beside it; its page moves, and is kept.
	ns, err := d.split(proto.CreateParams{Split: p.info.ID, View: proto.PaneBrowser, Target: "http://localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	b := d.panes[ns.Focus]
	b.info.Target = "http://localhost:3000/docs"
	d.saveState()
	d2 := linearTestDaemon(t)
	d2.dir = d.dir
	d2.sessions, d2.panes = nil, map[string]*pane{}
	restoreAll(t, d2)
	if len(d2.sessions) != 1 || len(d2.sessions[0].panes) != 2 {
		t.Fatalf("restored: %+v", d2.sessions)
	}
	for _, rp := range d2.sessions[0].panes {
		if rp.proc != nil || !rp.isView() {
			t.Fatalf("a view came back running: %+v", rp.info)
		}
		if rp.info.View == proto.PaneBrowser && rp.info.Target != "http://localhost:3000/docs" {
			t.Fatalf("the page was not kept: %q", rp.info.Target)
		}
	}
	if d2.sessions[0].layout == nil || len(d2.sessions[0].layout.Panes()) != 2 {
		t.Fatal("the layout was not kept")
	}

	// The tasks become files, in their place.
	rs, err := d.replacePane(proto.CreateParams{Split: p.info.ID, View: proto.PaneFiles})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Panes) != 2 || d.panes[p.info.ID] != nil || rs.Layout.Panes()[0] == p.info.ID {
		t.Fatalf("replace: %+v", rs)
	}
	if got := d.panes[rs.Layout.Panes()[0]]; got == nil || got.info.View != proto.PaneFiles || got.info.Dir != dir {
		t.Fatalf("the files pane: %+v", got)
	}
}

// A view speaks for its session only when it has no terminal.
func TestLeadSkipsViews(t *testing.T) {
	panes := []proto.Pane{{ID: "v", View: proto.PaneTasks, Status: proto.Idle}, {ID: "t", Status: proto.Idle}}
	if i := proto.Lead(panes, "v"); panes[i].ID != "t" {
		t.Fatalf("lead: %s", panes[i].ID)
	}
	if i := proto.Lead(panes[:1], "v"); i != 0 {
		t.Fatalf("a session of views has no lead: %d", i)
	}
}
