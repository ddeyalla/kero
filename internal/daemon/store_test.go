package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kero/internal/proto"
)

// A pane comes back from a restart with what the user has yet to see and
// how long ago its status changed; what ran in it is gone, so it never
// comes back working or waiting but unread as of the save, a shell that
// crashed unseen comes back failed, and a pane of a state file of before
// is idle and read as of the restore.
func TestRestoredStatus(t *testing.T) {
	now := time.Now().UnixMilli()
	hour := now - time.Hour.Milliseconds()
	day := now - 25*time.Hour.Milliseconds()
	saved := now - time.Minute.Milliseconds()
	for _, c := range []struct {
		name   string
		saved  savedPane
		st     proto.Status
		since  int64
		unread bool
		msg    string
	}{
		{"of before", savedPane{ID: "a"}, proto.Idle, now, false, ""},
		{"a day's work stopped", savedPane{Status: proto.Working, Since: day}, proto.Idle, saved, true, ""},
		{"waiting", savedPane{Status: proto.Waiting, Since: hour}, proto.Idle, saved, true, ""},
		{"done, unseen", savedPane{Status: proto.Idle, Since: hour, Unread: true}, proto.Idle, hour, true, ""},
		{"done, read", savedPane{Status: proto.Idle, Since: day}, proto.Idle, day, false, ""},
		{"crashed, unseen", savedPane{Status: proto.Exited, Since: hour, Unread: true, ExitCode: 2}, proto.Failed, saved, true, "Exited with code 2"},
		{"crashed, read", savedPane{Status: proto.Exited, Since: hour, ExitCode: 2}, proto.Idle, saved, false, ""},
		{"failed", savedPane{Status: proto.Failed, Since: hour, Unread: true, Message: "tests failed"}, proto.Failed, hour, true, "tests failed"},
		{"a failed view", savedPane{View: proto.PaneFiles, Status: proto.Failed, Since: hour}, proto.Idle, hour, false, ""},
		{"from the future", savedPane{Status: proto.Idle, Since: now + time.Hour.Milliseconds()}, proto.Idle, now, false, ""},
	} {
		st, since, unread, msg := restoredStatus(c.saved, saved, now)
		if st != c.st || since != c.since || unread != c.unread || msg != c.msg {
			t.Errorf("%s: %s since %d unread %v %q; want %s since %d unread %v %q",
				c.name, st, since, unread, msg, c.st, c.since, c.unread, c.msg)
		}
	}
	// A save time to come, or none, is now.
	if _, since, _, _ := restoredStatus(savedPane{Status: proto.Working, Since: day}, now+time.Hour.Milliseconds(), now); since != now {
		t.Errorf("a save from the future dates the pane %d, want now %d", since, now)
	}
}

// An exited shell's code is saved, for its failure to say it after a
// restart.
func TestSaveKeepsExitCode(t *testing.T) {
	d := linearTestDaemon(t)
	p := d.panes["p"]
	p.info.Status, p.info.ExitCode, p.info.Unread, p.info.AgentSession = proto.Exited, 2, true, nil
	d.saveState()
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	sp := saved.Sessions[0].Panes[0]
	if sp.Status != proto.Exited || sp.ExitCode != 2 || !sp.Unread {
		t.Fatalf("saved: %+v", sp)
	}
	if st, _, _, msg := restoredStatus(sp, saved.Saved, time.Now().UnixMilli()); st != proto.Failed || msg != "Exited with code 2" {
		t.Fatalf("restored as %s %q", st, msg)
	}
}

// The status a pane had is saved, and a restore brings it back with its
// time and its mark; a state file of before, without it, loads as it
// always did.
func TestRestoreKeepsStatus(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	dir := t.TempDir()
	s, err := d.create(proto.CreateParams{View: proto.PaneFiles, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	p := d.panes[s.Panes[0].ID]
	since := time.Now().Add(-26 * time.Hour).UnixMilli()
	p.info.Status, p.info.Since, p.info.Unread = proto.Idle, since, true
	d.saveState()
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if sp := saved.Sessions[0].Panes[0]; sp.Status != proto.Idle || sp.Since != since || !sp.Unread {
		t.Fatalf("saved: %+v", sp)
	}

	d2 := linearTestDaemon(t)
	d2.dir = d.dir
	d2.sessions, d2.panes = nil, map[string]*pane{}
	d2.restore()
	if len(d2.sessions) != 1 || len(d2.sessions[0].panes) != 1 {
		t.Fatalf("restored: %+v", d2.sessions)
	}
	if got := d2.sessions[0].panes[0].info; got.Status != proto.Idle || got.Since != since || !got.Unread {
		t.Fatalf("restored as %s since %d unread %v; want idle since %d, unread", got.Status, got.Since, got.Unread, since)
	}

	// A state file of before: no status, no time, no mark.
	old := `{"version":2,"projects":[],"saved":1700000000000,"sessions":[{"id":"s1","project":"","created":1700000000000,
		"panes":[{"id":"v1","view":"files","dir":` + quote(dir) + `,"created":1700000000000,"cols":80,"rows":24}]}]}`
	d3 := linearTestDaemon(t)
	d3.sessions, d3.panes = nil, map[string]*pane{}
	if err := os.WriteFile(filepath.Join(d3.dir, "state.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UnixMilli()
	d3.restore()
	if len(d3.sessions) != 1 || len(d3.sessions[0].panes) != 1 {
		t.Fatalf("an old state file did not load: %+v", d3.sessions)
	}
	if got := d3.sessions[0].panes[0].info; got.Status != proto.Idle || got.Unread || got.Since < before || got.Since > time.Now().UnixMilli() {
		t.Fatalf("an old pane came back %s since %d unread %v; want idle, read, as of now", got.Status, got.Since, got.Unread)
	}
	// And what it saves again has the fields, a file a daemon of before
	// reads past.
	d3.saveState()
	data, _ = os.ReadFile(d3.statePath())
	if !strings.Contains(string(data), `"since"`) {
		t.Fatalf("the status was not saved: %s", data)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
