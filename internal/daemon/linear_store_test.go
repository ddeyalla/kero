package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"kero/internal/proto"
)

// fixtureDaemon is a daemon whose state.json is a fixture, as one of
// before left it, and nothing else.
func fixtureDaemon(t *testing.T, fixture string) (*Daemon, savedState) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var want savedState
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	d := linearTestDaemon(t)
	d.linear, d.sessions, d.panes = nil, nil, map[string]*pane{}
	if err := os.WriteFile(d.statePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range d.panes {
			p.close()
		}
	})
	return d, want
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A state.json of the release before, with the Linear workspace in it,
// loads whole; the next save moves the workspace to linear.json and
// leaves it out of state.json; a daemon after loads both the same; and a
// daemon of before, given the new state.json, loads it with no workspace
// rather than fail.
func TestLinearMovesOutOfState(t *testing.T) {
	d, want := fixtureDaemon(t, "state-linear-v2.json")
	if want.Linear == nil || len(want.Tasks) == 0 {
		t.Fatal("the fixture has no workspace or tasks")
	}
	d.restore()
	check := func(d *Daemon, when string) {
		t.Helper()
		if got := asJSON(t, d.linear); got != asJSON(t, want.Linear) {
			t.Fatalf("%s: the workspace\n%s\nwant\n%s", when, got, asJSON(t, want.Linear))
		}
		// The restore ends the runs of the last daemon (taskEndWhere):
		// the conversations stay, to resume.
		lt, wt := d.tasks["uuid"], want.Tasks["uuid"]
		if len(d.tasks) != len(want.Tasks) || lt == nil || lt.Identifier != wt.Identifier || len(lt.Runs) != len(wt.Runs) ||
			lt.Runs[0].Agent != wt.Runs[0].Agent || lt.Runs[0].Session != wt.Runs[0].Session || !reflect.DeepEqual(d.projects, want.Projects) {
			t.Fatalf("%s: tasks %+v projects %+v", when, d.tasks, d.projects)
		}
		if len(d.sessions) != 1 || len(d.sessions[0].panes) != 2 {
			t.Fatalf("%s: sessions %+v", when, d.sessions)
		}
		s := d.sessions[0]
		if s.focus != "p" || !s.unlinked["uuid-old"] || asJSON(t, s.layout) != asJSON(t, want.Sessions[0].Layout) {
			t.Fatalf("%s: session focus %q unlinked %v layout %s", when, s.focus, s.unlinked, asJSON(t, s.layout))
		}
		// A view never fails (restoredStatus): it keeps its mark and time.
		info := paneInfo(s.panes[0])
		if info.TaskID != "uuid" || info.Status != proto.Idle || !info.Unread || info.Since != 1700000001000 || info.View != proto.PaneFiles {
			t.Fatalf("%s: pane %+v", when, info)
		}
	}
	check(d, "the release before's file")

	d.saveState()
	state, _ := os.ReadFile(d.statePath())
	if strings.Contains(string(state), `"linear"`) {
		t.Fatalf("state.json still has the workspace:\n%s", state)
	}
	if lin := d.loadLinear(); asJSON(t, lin) != asJSON(t, want.Linear) {
		t.Fatalf("linear.json has %s", asJSON(t, lin))
	}

	d2 := linearTestDaemon(t)
	d2.dir, d2.linear, d2.sessions, d2.panes = d.dir, nil, nil, map[string]*pane{}
	d2.restore()
	t.Cleanup(func() {
		for _, p := range d2.panes {
			p.close()
		}
	})
	check(d2, "the files after")

	// A daemon of before reads state.json into the same struct, and
	// takes its workspace from it: none, which it reads from Linear.
	var old savedState
	if err := json.Unmarshal(state, &old); err != nil {
		t.Fatalf("a daemon of before could not read the new state.json: %v", err)
	}
	if old.Linear != nil || len(old.Sessions) != 1 || len(old.Tasks) != 1 {
		t.Fatalf("a daemon of before would read %+v", old)
	}
}

// The workspace is written as it changes only, and before the state.
func TestLinearWrittenAsItChanges(t *testing.T) {
	d := linearTestDaemon(t)
	d.saveState()
	fi, err := os.Stat(d.linearPath())
	if err != nil {
		t.Fatal("no linear.json:", err)
	}
	os.Chtimes(d.linearPath(), time.Unix(1, 0), time.Unix(1, 0))
	d.saveState()
	if fi, _ = os.Stat(d.linearPath()); fi.ModTime().Unix() != 1 {
		t.Fatal("an unchanged workspace was written again")
	}
	d.linearSet(&proto.LinearState{Status: proto.LinearOK, Team: "BRU", FetchedAt: 5})
	d.saveState()
	if lin := d.loadLinear(); lin == nil || lin.FetchedAt != 5 {
		t.Fatalf("a new workspace was not written: %+v", lin)
	}
}

// Where both files have a workspace, as after a daemon of before ran on
// the new ones, the one read last wins; a linear.json that does not
// parse is passed over; one alone, without state.json, loads.
func TestLinearPicksTheNewer(t *testing.T) {
	d, want := fixtureDaemon(t, "state-linear-v2.json")
	os.WriteFile(d.linearPath(), []byte(`{"status":"ok","team":"BRU","fetchedAt":1}`), 0o600)
	d.restore()
	if d.linear == nil || d.linear.FetchedAt != want.Linear.FetchedAt || d.linearSaved != nil {
		t.Fatalf("an older linear.json won: %+v", d.linear)
	}

	d, _ = fixtureDaemon(t, "state-linear-v2.json")
	os.WriteFile(d.linearPath(), []byte(`{"status":"ok","team":"BRU","fetchedAt":9999999999999}`), 0o600)
	d.restore()
	if d.linear == nil || d.linear.FetchedAt != 9999999999999 || d.linearSaved != d.linear {
		t.Fatalf("a newer linear.json lost: %+v", d.linear)
	}

	d, _ = fixtureDaemon(t, "state-linear-v2.json")
	os.WriteFile(d.linearPath(), []byte(`{`), 0o600)
	d.restore()
	if d.linear == nil || d.linear.FetchedAt != want.Linear.FetchedAt {
		t.Fatalf("a broken linear.json: %+v", d.linear)
	}

	d = linearTestDaemon(t)
	d.linear = nil
	os.WriteFile(d.linearPath(), []byte(`{"status":"ok","team":"BRU","fetchedAt":7}`), 0o600)
	d.restore()
	if d.linear == nil || d.linear.FetchedAt != 7 {
		t.Fatalf("linear.json alone did not load: %+v", d.linear)
	}
}
