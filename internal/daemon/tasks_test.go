package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kero/internal/proto"
)

// A shell split beside the agent inherits the task, and must not speak
// for it: the card keeps the agent's status and the id that resumes it.
func TestTaskLinkFollowsTheAgentPaneNotTheSplit(t *testing.T) {
	d := linearTestDaemon(t)
	if err := d.linkSession("s", "BRU-25", "link"); err != nil {
		t.Fatal(err)
	}
	shell := &pane{info: proto.Pane{ID: "q", Status: proto.Idle, TaskID: "uuid", Since: 1 << 40}}
	d.panes["q"] = shell
	d.sessions[0].panes = append(d.sessions[0].panes, shell)
	got := d.linearState().Issues[0]
	if got.Session != "s" || got.ResumeID != "resume-id" || got.SessionStatus != proto.Working {
		t.Fatalf("split pane spoke for the task: %+v", got)
	}
}

// Two sessions on one issue: both count, and the one that needs the user
// shows, whatever their order.
func TestTwoSessionsOnOneTask(t *testing.T) {
	d := linearTestDaemon(t)
	p2 := &pane{info: proto.Pane{ID: "p2", Status: proto.Waiting, Agent: "claude", AgentSession: &proto.AgentSession{Agent: "claude", ID: "other"}}}
	d.panes["p2"] = p2
	d.sessions = append(d.sessions, &session{id: "s2", name: "second", panes: []*pane{p2}})
	d.linkSession("s", "BRU-25", "link")
	d.linkSession("s2", "BRU-25", "task")
	got := d.linearState().Issues[0]
	if got.Sessions != 2 || got.Session != "s2" {
		t.Fatalf("want both sessions, s2 shown: %+v", got)
	}
	d.sessions[0], d.sessions[1] = d.sessions[1], d.sessions[0]
	if got := d.linearState().Issues[0]; got.Session != "s2" {
		t.Fatalf("order decided the session: %+v", got)
	}
}

// The log outlives the agent: after it exits, after the session closes
// and after a restart, the task still resumes its conversation, from the
// directory it ran in.
func TestTaskLogOutlivesAgentSessionAndRestart(t *testing.T) {
	d := linearTestDaemon(t)
	d.panes["p"].info.Agent = "codex"
	d.panes["p"].info.Dir = "/repo"
	if err := d.linkSession("s", "BRU-25", "task"); err != nil {
		t.Fatal(err)
	}
	// The agent exits: the pane forgets its session, the log does not.
	d.panes["p"].info.AgentSession = nil
	d.taskEnd("p")
	if got := d.linearState().Issues[0]; got.ResumeID != "resume-id" {
		t.Fatalf("resume lost with the agent: %+v", got)
	}
	d.removeSessionForTest("s")
	d.saveState()
	b, err := os.ReadFile(filepath.Join(d.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	d2 := linearTestDaemon(t)
	d2.tasks = saved.Tasks
	d2.sessions = nil
	runs := d2.taskRuns("uuid", "BRU-25", nil)
	if len(runs) != 1 {
		t.Fatalf("runs: %+v", runs)
	}
	r := runs[0]
	if r.Agent != "codex" || r.ID != "resume-id" || r.Dir != "/repo" || r.Via != "task" || r.Ended == 0 || r.Session != "s" {
		t.Fatalf("run: %+v", r)
	}
	if got := d2.linearState().Issues[0]; got.ResumeID != "resume-id" || got.Session != "" {
		t.Fatalf("after restart: %+v", got)
	}
}

// A second conversation is one more run, of the same harness after
// /clear or of another; the same conversation taken up again is not.
func TestTaskRunsLogEachConversationOnce(t *testing.T) {
	d := linearTestDaemon(t)
	d.panes["p"].info.AgentSession = nil
	d.panes["p"].info.Agent = ""
	d.linkSession("s", "BRU-25", "link")
	runs := d.taskRuns("uuid", "", nil)
	if len(runs) != 1 || runs[0].Agent != "" {
		t.Fatalf("a linked session with no agent stands as one run: %+v", runs)
	}
	// Claude starts; its hook tells its session.
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", Session: "s", Pane: "p", Dir: "/repo"})
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", ID: "c1", Session: "s", Pane: "p"})
	runs = d.taskRuns("uuid", "", nil)
	if len(runs) != 1 || runs[0].ID != "c1" || runs[0].Dir != "/repo" || runs[0].Via != "link" {
		t.Fatalf("hook filled the run: %+v", runs)
	}
	// /clear: a new conversation in the same pane ends the first.
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", ID: "c2", Session: "s", Pane: "p"})
	// Codex in another pane of the same session.
	d.taskStart("uuid", proto.TaskRun{Agent: "gemini", Session: "s", Pane: "g"})
	d.taskStart("uuid", proto.TaskRun{Agent: "gemini", Session: "s", Pane: "g"}) // again, nothing new
	// c1 resumed in a new session.
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", ID: "c1", Session: "s9", Pane: "p9"})
	runs = d.taskRuns("uuid", "", map[string]string{"s9": "resumed"})
	if len(runs) != 3 {
		t.Fatalf("want c1, c2, gemini: %+v", runs)
	}
	by := map[string]proto.TaskRun{}
	for _, r := range runs {
		by[r.Agent+r.ID] = r
	}
	if c1 := by["claudec1"]; c1.Session != "s9" || c1.Ended != 0 || c1.SessionName != "resumed" {
		t.Fatalf("c1 taken up again: %+v", c1)
	}
	if c2 := by["claudec2"]; c2.Ended != 0 {
		t.Fatalf("c2 runs on: %+v", c2)
	}
	if _, ok := lastResume(runs); !ok {
		t.Fatal("nothing resumes")
	}
	d.taskEndSession("s", "")
	for _, r := range d.taskRuns("uuid", "", nil) {
		if r.Session == "s" && r.Ended == 0 {
			t.Fatalf("closed session's run still open: %+v", r)
		}
	}
}

// Unlinking, or linking another task, ends the session's runs on the old.
func TestRelinkEndsTheOldTasksRuns(t *testing.T) {
	d := linearTestDaemon(t)
	d.linear.Issues = append(d.linear.Issues, proto.LinearIssue{ID: "uuid2", Identifier: "BRU-26"})
	d.linkSession("s", "BRU-25", "link")
	d.linkSession("s", "BRU-26", "link")
	if r := d.taskRuns("uuid", "", nil); len(r) != 1 || r[0].Ended == 0 {
		t.Fatalf("old task's run open: %+v", r)
	}
	if r := d.taskRuns("uuid2", "", nil); len(r) != 1 || r[0].Ended != 0 {
		t.Fatalf("new task's run: %+v", r)
	}
	if d.panes["p"].info.TaskID != "uuid2" {
		t.Fatal("panes keep the issue's id")
	}
}

func TestProtoResumeCommand(t *testing.T) {
	for _, c := range []struct {
		agent, id string
		want      int
	}{{"claude", "abc-1", 3}, {"codex", "x", 3}, {"opencode", "s", 3}, {"gemini", "x", 0}, {"claude", "a b", 0}, {"claude", "", 0}} {
		if got := len(proto.ResumeCommand(c.agent, c.id)); got != c.want {
			t.Errorf("%s %q: %d", c.agent, c.id, got)
		}
	}
}

// removeSessionForTest closes a session of panes without processes.
func (d *Daemon) removeSessionForTest(id string) {
	d.mu.Lock()
	for i, s := range d.sessions {
		if s.id == id {
			d.sessions = append(d.sessions[:i], d.sessions[i+1:]...)
			break
		}
	}
	d.mu.Unlock()
	d.taskEndSession(id, "")
}

func TestIssueInBranch(t *testing.T) {
	for branch, want := range map[string]string{
		"dv/bru-25-fix-login":    "BRU-25",
		"BRU-7":                  "BRU-7",
		"feature/bru-0042-thing": "BRU-42",
		"abru-25-x":              "",
		"bru-25x":                "BRU-25",
		"brunoh-25":              "",
		"main":                   "",
		"dv/eng-25-other-team":   "",
		"divyanshu/bru-133":      "BRU-133",
	} {
		if got := issueInBranch(branch, "BRU"); got != want {
			t.Errorf("%q: %q, want %q", branch, got, want)
		}
	}
}

// A session an agent works in, on an issue's branch, links itself; a
// worktree's branch counts; one the user unlinked stays unlinked.
func TestLinkByBranch(t *testing.T) {
	d := linearTestDaemon(t)
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/dv/bru-25-task\n"), 0o644)
	sub := filepath.Join(repo, "internal", "x")
	os.MkdirAll(sub, 0o755)
	p := d.panes["p"]
	p.info.Agent, p.info.Dir = "gemini", sub
	p.info.AgentSession = nil
	d.linkByBranch()
	if p.info.TaskID != "uuid" {
		t.Fatalf("not linked by branch: %q", p.info.TaskID)
	}
	runs := d.taskRuns("uuid", "", nil)
	if len(runs) != 1 || runs[0].Via != "branch" || runs[0].Agent != "gemini" {
		t.Fatalf("runs: %+v", runs)
	}
	d.linkSession("s", "", "link")
	d.linkByBranch()
	if p.info.TaskID != "" {
		t.Fatal("the branch undid the user's unlink")
	}
	// A worktree: .git is a file naming its git directory.
	wt, gd := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(gd, "HEAD"), []byte("ref: refs/heads/bru-31-wt\n"), 0o644)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gd+"\n"), 0o644)
	if got := branchOf(wt); got != "bru-31-wt" {
		t.Fatalf("worktree branch: %q", got)
	}
	os.WriteFile(filepath.Join(gd, "HEAD"), []byte("4f2a9c1\n"), 0o644)
	if got := branchOf(wt); got != "" {
		t.Fatalf("detached: %q", got)
	}
}

func TestSetProjectLinear(t *testing.T) {
	d := linearTestDaemon(t)
	d.projects = []proto.Project{{ID: "p", Name: "shibu"}}
	d.linear.Projects = []proto.LinearProject{{ID: "lp", Name: "Shibu Code"}}
	if err := d.setProjectLinear("p", "shibu code"); err != nil || d.projects[0].LinearProject != "lp" {
		t.Fatalf("by name: %v %+v", err, d.projects[0])
	}
	if err := d.setProjectLinear("p", "nope"); err == nil || d.projects[0].LinearProject != "lp" {
		t.Fatal("an unknown project replaced the link")
	}
	if err := d.setProjectLinear("p", ""); err != nil || d.projects[0].LinearProject != "" {
		t.Fatal("not unlinked")
	}
	if err := d.setProjectLinear("missing", "lp"); err == nil {
		t.Fatal("unknown project accepted")
	}
}

// Resume as it happens: the new session is linked with no agent yet, the
// tick sees the agent, then its hook tells the old conversation's id. One
// run, taken up again; no stand-in, no run of no id left behind.
func TestResumeLeavesOneRun(t *testing.T) {
	d := linearTestDaemon(t)
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", ID: "c1", Session: "old", Pane: "x", Dir: "/repo", Started: 1})
	d.taskEnd("x")
	d.taskStart("uuid", proto.TaskRun{Session: "new", Via: "link"})                           // created linked
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", Session: "new", Pane: "np"})           // the tick
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", ID: "c1", Session: "new", Pane: "np"}) // the hook
	runs := d.taskRuns("uuid", "", nil)
	if len(runs) != 1 || runs[0].ID != "c1" || runs[0].Session != "new" || runs[0].Ended != 0 || runs[0].Dir != "/repo" {
		t.Fatalf("want one c1 run, open in the new session: %+v", runs)
	}
}

// After a restart that resumes no agent, nothing shows as running.
func TestRestoreEndsRunsOfAgentsNotResumed(t *testing.T) {
	d := linearTestDaemon(t)
	d.projects = []proto.Project{{ID: "project", Name: "Kept"}}
	d.taskStart("uuid", proto.TaskRun{Agent: "claude", ID: "c1", Session: "s", Pane: "p"})
	d.taskStart("uuid", proto.TaskRun{Session: "s2", Via: "link"})
	d.saveState()
	d2 := linearTestDaemon(t)
	d2.dir = d.dir
	d2.sessions, d2.panes = nil, map[string]*pane{}
	t.Setenv("KERO_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	os.WriteFile(os.Getenv("KERO_CONFIG"), []byte("restore-sessions = false\n"), 0o600)
	restoreAll(t, d2)
	if len(d2.sessions) != 0 || len(d2.projects) != 1 || d2.projects[0].ID != "project" {
		t.Fatalf("restore disabled: sessions %v, projects %v", d2.sessions, d2.projects)
	}
	for _, r := range d2.taskRuns("uuid", "", nil) {
		if r.Ended == 0 {
			t.Fatalf("a run still open after a restart without sessions: %+v", r)
		}
	}
}
