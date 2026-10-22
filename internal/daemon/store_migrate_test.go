package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
)

const migrateFixture = `{"version":%d,"projects":[],"saved":1700000000000,"sessions":[{"id":"s1","project":"","created":1700000000000,
  "panes":[
   {"id":"t1","view":"tasks","target":"old","dir":"/work","task":"abc","created":1700000000000,"cols":80,"rows":24},
   {"id":"f1","view":"files","target":"/work/a.go","created":1700000000000,"cols":80,"rows":24},
   {"id":"x1","view":"from-the-future","target":"keep me","created":1700000000000,"cols":80,"rows":24},
   {"id":"sh","dir":"/work","command":["zsh"],"created":1700000000000,"cols":80,"rows":24}]}]}`

func readFixture(t *testing.T, version int) savedState {
	t.Helper()
	var st savedState
	if err := json.Unmarshal([]byte(fmt.Sprintf(migrateFixture, version)), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMigrateV2TasksToLinearPlugin(t *testing.T) {
	st := readFixture(t, 2)
	before := readFixture(t, 2)
	if !migrateState(&st) || st.Version != 3 {
		t.Fatalf("not migrated: version %d", st.Version)
	}
	panes := st.Sessions[0].Panes
	tp := panes[0]
	if tp.View != proto.PanePlugins {
		t.Fatalf("view %q", tp.View)
	}
	tg, err := protocol.DecodeTarget(tp.Target)
	if err != nil || tg.Plugin != "linear" || tg.Widget != "board" {
		t.Fatalf("target %q: %+v %v", tp.Target, tg, err)
	}
	var state map[string]any
	if err := json.Unmarshal(tg.State, &state); err != nil || !reflect.DeepEqual(state, map[string]any{"mode": "list", "scope": "project", "hideDone": true}) {
		t.Fatalf("state %s: %v", tg.State, err)
	}
	// Everything else of the pane stays.
	if tp.ID != "t1" || tp.Dir != "/work" || tp.Task != "abc" || tp.Cols != 80 || tp.Rows != 24 || tp.Created != 1700000000000 {
		t.Fatalf("pane changed: %+v", tp)
	}
	// Other panes, known or not, are untouched.
	for i := 1; i < 4; i++ {
		if !reflect.DeepEqual(panes[i], before.Sessions[0].Panes[i]) {
			t.Fatalf("pane %d changed: %+v", i, panes[i])
		}
	}
	// A second run changes nothing.
	again := st
	if migrateState(&again) || !reflect.DeepEqual(again, st) {
		t.Fatal("migration is not idempotent")
	}
}

func TestMigrateV3Unchanged(t *testing.T) {
	st := readFixture(t, 3)
	before := readFixture(t, 3)
	if migrateState(&st) || !reflect.DeepEqual(st, before) {
		t.Fatalf("a version 3 file changed: %+v", st.Sessions[0].Panes[0])
	}
}

func TestRestoreMigratesAndSaveWritesV3(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	dir := t.TempDir()
	data := []byte(`{"version":2,"projects":[],"saved":1700000000000,"sessions":[{"id":"s1","project":"","created":1700000000000,
	  "panes":[{"id":"t1","view":"tasks","dir":` + quote(dir) + `,"created":1700000000000,"cols":80,"rows":24}]}]}`)
	if err := os.WriteFile(filepath.Join(d.dir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	d.restore()
	if len(d.sessions) != 1 {
		t.Fatalf("restored %d sessions", len(d.sessions))
	}
	info := d.sessions[0].panes[0].info
	if info.View != proto.PanePlugins || info.Target != linearPluginTarget() {
		t.Fatalf("restored as %q %q", info.View, info.Target)
	}
	d.saveState()
	out, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := json.Unmarshal(out, &saved); err != nil || saved.Version != 3 {
		t.Fatalf("saved version %d: %v", saved.Version, err)
	}
}
