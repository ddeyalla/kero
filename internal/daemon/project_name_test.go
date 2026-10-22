package daemon

import (
	"encoding/json"
	"testing"

	"kero/internal/proto"
)

// A project renamed with an emoji keeps it, whole, in the state the
// clients read and across a restart; its id and its directory stay as
// they were.
func TestProjectRenameKeepsEmoji(t *testing.T) {
	d := linearTestDaemon(t)
	none := ""
	d.viewed.Store(&none)
	d.projects = []proto.Project{{ID: "p1", Name: "kero-deyalla", Dir: "/tmp/kero-deyalla"}}
	const name = "🐛 kero-deyalla"
	raw, _ := json.Marshal(proto.ProjectParams{ID: "p1", Name: "  " + name + " "})
	if _, err := d.call(nil, "project.rename", raw); err != nil {
		t.Fatal(err)
	}
	check := func(when string, ps []proto.Project) {
		t.Helper()
		if len(ps) != 1 || ps[0].Name != name || ps[0].ID != "p1" || ps[0].Dir != "/tmp/kero-deyalla" {
			t.Fatalf("%s: projects %+v, want %q in p1 at its directory", when, ps, name)
		}
	}
	check("renamed", d.state().Projects)

	// Through JSON, as a client reads it.
	b, err := json.Marshal(d.state())
	if err != nil {
		t.Fatal(err)
	}
	var st proto.State
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	check("read by a client", st.Projects)

	d.saveState()
	d2 := linearTestDaemon(t)
	d2.dir = d.dir
	d2.viewed.Store(&none)
	d2.sessions, d2.panes = nil, map[string]*pane{}
	restoreAll(t, d2)
	check("restored", d2.state().Projects)
}
