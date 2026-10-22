package daemon

import (
	"encoding/json"
	"io"
	"kero/internal/linear"
	"kero/internal/proto"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func linearTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	// No key, so that nothing reaches Linear: an issue not cached fails.
	t.Setenv("KERO_HOME", t.TempDir())
	t.Setenv("KERO_CONFIG", filepath.Join(t.TempDir(), "config"))
	t.Setenv("LINEAR_API_KEY", "")
	d := &Daemon{log: log.New(io.Discard, "", 0), dir: t.TempDir(), panes: map[string]*pane{}, changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	d.linear = &proto.LinearState{Status: proto.LinearOK, Team: "BRU", Issues: []proto.LinearIssue{{ID: "uuid", Identifier: "BRU-25", Title: "Task"}}}
	p := &pane{info: proto.Pane{ID: "p", Status: proto.Working, Progress: 40, AgentSession: &proto.AgentSession{Agent: "codex", ID: "resume-id"}}}
	d.panes["p"] = p
	d.sessions = []*session{{id: "s", name: "working", panes: []*pane{p}, focus: "p"}}
	return d
}

func TestLinearLinkUnlinkAndPersistedIdentity(t *testing.T) {
	d := linearTestDaemon(t)
	if err := d.linkSession("s", "uuid", "link"); err != nil {
		t.Fatal(err)
	}
	got := d.linearState().Issues[0]
	if got.Session != "s" || got.ResumeID != "resume-id" || got.Progress != 40 || got.SessionStatus != proto.Working {
		t.Fatalf("link lost metadata: %+v", got)
	}
	d.saveState()
	b, err := os.ReadFile(filepath.Join(d.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Sessions[0].Panes[0].Task != "uuid" || saved.Linear == nil {
		t.Fatal("task link or offline cache not persisted")
	}
	if err := d.linkSession("s", "", "link"); err != nil {
		t.Fatal(err)
	}
	if got := d.linearState().Issues[0]; got.Session != "" {
		t.Fatal("unlink retained session")
	}
	if err := d.linkSession("s", "BRU-9999", "link"); err == nil {
		t.Fatal("unknown issue accepted")
	}
	if err := d.linkSession("missing", "BRU-25", "link"); err == nil {
		t.Fatal("unknown session accepted")
	}
}

func TestLinearOSCJoinsWithoutBranchGuessing(t *testing.T) {
	d := linearTestDaemon(t)
	p := d.panes["p"]
	p.info.Records = []proto.Record{{ID: "BRU-25/checks", App: "linear", State: "working", Progress: 65}}
	got := d.linearState().Issues[0]
	if got.Session != "s" || got.Progress != 65 {
		t.Fatalf("OSC link lost: %+v", got)
	}
	p.info.Records = nil
	if got := d.linearState().Issues[0]; got.Session != "" {
		t.Fatal("cleared OSC left stale link")
	}
}

func TestLinearDeltaMergePreservesUnchangedIssues(t *testing.T) {
	old := []proto.LinearIssue{{ID: "a", Title: "old"}, {ID: "b", Title: "keep"}}
	got := mergeIssues(old, []linear.Issue{{ID: "a", Title: "new"}, {ID: "c", Title: "added"}})
	if len(got) != 3 || got[0].Title != "new" || got[1].Title != "keep" || old[0].Title != "old" {
		t.Fatal("delta corrupted cache")
	}
}

func TestLinearFailureRetainsOfflineCache(t *testing.T) {
	d := linearTestDaemon(t)
	prev := d.linear
	d.linearFailed(prev, os.ErrDeadlineExceeded)
	if d.linear.Status != proto.LinearUnreachable || len(d.linear.Issues) != 1 || d.linear.Issues[0].Identifier != "BRU-25" {
		t.Fatal("failed request erased cache")
	}
}

// The wait between reads follows the setting, within Linear's quota.
func TestLinearIntervalFollowsTheSetting(t *testing.T) {
	for seconds, want := range map[int]time.Duration{
		0:     linearPoll,
		5:     15 * time.Second,
		60:    time.Minute,
		300:   5 * time.Minute,
		99999: linearSlow,
	} {
		if got := linearInterval(seconds); got != want {
			t.Errorf("linearInterval(%d) = %v, want %v", seconds, got, want)
		}
	}
}

func TestLinearIssueMapsAssigneePriorityParentAndViewer(t *testing.T) {
	snap := &linear.Snapshot{
		Viewer: &linear.User{ID: "u1", Name: "Me"},
		Issues: []linear.Issue{{
			ID: "i1", Identifier: "BRU-2", Title: "Child",
			Assignee: &linear.User{ID: "u2", Name: "Ann"}, Parent: &linear.Issue{Identifier: "BRU-1"},
		}, {ID: "i2", Identifier: "BRU-3"}},
	}
	st := linearSnapshot(snap)
	if st.Viewer == nil || st.Viewer.ID != "u1" || st.Viewer.Name != "Me" {
		t.Fatalf("viewer %+v", st.Viewer)
	}
	i := st.Issues[0]
	if i.Assignee != "Ann" || i.AssigneeID != "u2" || i.Parent != "BRU-1" {
		t.Fatalf("issue %+v", i)
	}
	if u := st.Issues[1]; u.Assignee != "" || u.AssigneeID != "" || u.Parent != "" || u.Priority != 0 {
		t.Fatalf("an unassigned issue has fields: %+v", u)
	}
	if linearSnapshot(&linear.Snapshot{}).Viewer != nil {
		t.Fatal("a snapshot with no viewer has one")
	}
}

func TestLinearSnapshotCollectsLabelColors(t *testing.T) {
	snap := &linear.Snapshot{Issues: []linear.Issue{
		{ID: "i1", Labels: []linear.Label{{Name: "bug", Color: "#E5484D"}, {Name: "plain"}}},
		{ID: "i2", Labels: []linear.Label{{Name: "bug", Color: "#E5484D"}, {Name: "ui", Color: "#2F6FED"}}},
	}}
	got := linearSnapshot(snap).LabelColors
	if len(got) != 2 || got["bug"] != "#E5484D" || got["ui"] != "#2F6FED" {
		t.Fatalf("label colors %v", got)
	}
	if linearSnapshot(&linear.Snapshot{}).LabelColors != nil {
		t.Fatal("a workspace with no labels has colors")
	}
}
