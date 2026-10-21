package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// attnFixture is a session beside the pane: its attention's state, what
// the hook said of it, and the agent that needs the user.
type attnFixture struct {
	state string
	msg   string
	agent string
}

// testAttention is the app showing an Attention pane in its first
// session, and a session of two panes for each attention given, the
// first pane the one that needs the user. No kerod.
func testAttention(t *testing.T, f ...attnFixture) *App {
	t.Helper()
	a := testApp(t)
	host := &a.st.Sessions[0]
	host.Name = "host"
	host.Panes[1].View, host.Panes[1].Shell = proto.PaneAttention, ""
	now := time.Now()
	for i, x := range f {
		id := fmt.Sprintf("s%d", i+2)
		pane, other := "a"+id, "b"+id
		var l *proto.Layout
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: "p1", Name: fmt.Sprintf("task %d", i+1),
			Panes: []proto.Pane{
				{ID: pane, Shell: "fish", Dir: "/tmp", Agent: x.agent, Status: proto.Idle, Progress: -1},
				{ID: other, Shell: "zsh", Dir: "/tmp", Status: proto.Idle, Progress: -1},
			},
			Layout: l.Split(pane, other, "right", 0),
			Focus:  pane,
			Attention: &proto.Attention{
				State: x.state, Msg: x.msg, Pane: pane,
				Since: now.Add(-time.Duration(i+1) * time.Minute).UnixMilli(),
			},
		})
	}
	a.selected = "s1"
	return a
}

// The pane with nothing that needs the user says so, as the Linear and
// Tasks empty states do.
func TestAttentionPaneEmpty(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testAttention(t)
		tt := ui.NewTester(a.view, 1280, 720)
		tt.SetDark(dark)
		frames(tt)
		for _, want := range []string{"Attention", "Nothing needs you", "Sessions that work"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		for _, unwanted := range []string{"Needs you", "All"} {
			if tt.HasText(unwanted) {
				t.Errorf("dark=%v: an empty pane shows %q", dark, unwanted)
			}
		}
		shot(t, tt, map[bool]string{false: "attention-empty-light", true: "attention-empty-dark"}[dark])
	}
}

// A session that needs the user is a row: its state, its name and its
// project, what the hook said, the agent and how long it has been so.
func TestAttentionPaneRow(t *testing.T) {
	a := testAttention(t, attnFixture{state: proto.AttentionWaitingUser, msg: "Bash command needs approval", agent: "claude"})
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	for _, want := range []string{"Needs you", "task 1", "demo", "Bash command needs approval", "claude", "1m"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the row; texts %q", want, tt.Texts())
		}
	}
	shot(t, tt, "attention-row")
}

// Ten sessions list ten rows, the most urgent first, and each says its
// own state.
func TestAttentionPaneMany(t *testing.T) {
	states := []string{proto.AttentionWaitingUser, proto.AttentionRunning, proto.AttentionUnread, proto.AttentionDone, proto.AttentionIdle}
	var f []attnFixture
	for i := range 10 {
		f = append(f, attnFixture{state: states[i%len(states)], msg: fmt.Sprintf("work %d", i+1), agent: "claude"})
	}
	a := testAttention(t, f...)
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if got := len(a.attentionRows()); got != 10 {
		t.Fatalf("%d rows, want 10", got)
	}
	// The most urgent first: the last fixture of each kind is the
	// oldest, so a kind's own order is newest first.
	rows := a.attentionRows()
	if rows[0].att.State != proto.AttentionWaitingUser || rows[9].att.State != proto.AttentionIdle {
		t.Fatalf("rows go %s…%s, want waiting-user…idle", rows[0].att.State, rows[9].att.State)
	}
	for i, want := range []string{"task 1", "task 2", "task 5"} {
		if !tt.HasText(want) {
			t.Errorf("row %d: no %q; texts %q", i, want, tt.Texts())
		}
	}
	shot(t, tt, "attention-many")
}

// Each chip narrows the list to its state, and All brings every session
// back.
func TestAttentionPaneFilters(t *testing.T) {
	a := testAttention(t,
		attnFixture{state: proto.AttentionWaitingUser, msg: "waits", agent: "claude"},
		attnFixture{state: proto.AttentionRunning, msg: "runs", agent: "claude"},
		attnFixture{state: proto.AttentionUnread, msg: "ended", agent: "claude"},
		attnFixture{state: proto.AttentionDone, msg: "seen", agent: "claude"},
	)
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	for _, want := range []string{"All", "Needs you", "Working", "Unread", "Done"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q chip; texts %q", want, tt.Texts())
		}
	}
	shown := func() []string {
		var names []string
		for _, r := range a.attentionShown(a.attentionRows(), a.attentionViewOf("b")) {
			names = append(names, r.session.Name)
		}
		return names
	}
	if err := tt.Click("Needs you"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if got := shown(); len(got) != 1 || got[0] != "task 1" {
		t.Fatalf("the Needs you chip shows %v, want task 1", got)
	}
	// The sidebar lists every session's name, so a row's own words are
	// what says which rows the list has.
	if tt.HasText("runs") || tt.HasText("seen") {
		t.Errorf("a chip left another kind's row in the list; texts %q", tt.Texts())
	}
	if !tt.HasText("waits") {
		t.Error("the filtered row lost its words")
	}
	if err := tt.Click("Working"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if got := shown(); len(got) != 1 || got[0] != "task 2" {
		t.Fatalf("the Working chip shows %v, want task 2", got)
	}
	if err := tt.Click("All"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if got := shown(); len(got) != 4 {
		t.Fatalf("All shows %d rows, want 4", len(got))
	}
	shot(t, tt, "attention-filters")
}

// The search finds what a row says, and what no row says shows the empty
// state with its way back.
func TestAttentionPaneSearch(t *testing.T) {
	a := testAttention(t,
		attnFixture{state: proto.AttentionWaitingUser, msg: "deploy the site", agent: "claude"},
		attnFixture{state: proto.AttentionRunning, msg: "run the tests", agent: "codex"},
	)
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if err := tt.Click("Search attention"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	tt.Type("deploy")
	frames(tt)
	if !tt.HasText("deploy the site") || tt.HasText("run the tests") {
		t.Errorf("the search left %q", tt.Texts())
	}
	if got := a.attentionViewOf("b").query; got != "deploy" {
		t.Fatalf("the field holds %q", got)
	}
	for range 8 { // back to nothing
		tt.Key(0, ui.KeyBackspace)
	}
	frames(tt)
	tt.Type("nothing says this")
	frames(tt)
	for _, want := range []string{"Nothing in this filter", "Clear Filter"} {
		if !tt.HasText(want) {
			t.Errorf("no %q with nothing found; texts %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Clear Filter"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if got := a.attentionViewOf("b").query; got != "" {
		t.Fatalf("Clear Filter left the search %q", got)
	}
	if !tt.HasText("run the tests") {
		t.Error("Clear Filter left the list empty")
	}
}

// A row's click shows its session and puts the keyboard in the pane that
// needs the user.
func TestAttentionRowFocusesPane(t *testing.T) {
	a := testAttention(t, attnFixture{state: proto.AttentionWaitingUser, msg: "Bash command needs approval", agent: "claude"})
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if err := tt.Click("task 1"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if a.selected != "s2" {
		t.Fatalf("the row selected %q, want s2", a.selected)
	}
	if got := a.focusOf(a.session("s2")); got != "as2" {
		t.Fatalf("the row focused %q, want the pane that needs the user", got)
	}
}

// A Codex pane says nothing of an approval — its state is the transcript
// scan's — so its row says what to do; a hook's message shows as it is.
func TestAttentionCodexApproval(t *testing.T) {
	a := testAttention(t,
		attnFixture{state: proto.AttentionWaitingUser, agent: "codex"},
		attnFixture{state: proto.AttentionWaitingUser, agent: "claude"},
	)
	rows := a.attentionRows()
	if got := a.attentionSummary(rows[0]); got != "Focus pane to approve" {
		t.Fatalf("a silent Codex wait says %q", got)
	}
	if got := a.attentionSummary(rows[1]); got != "Needs you" {
		t.Fatalf("a silent hook says %q", got)
	}
	// A message the hook gave is shown as it is, Codex or not.
	rows[0].att.Msg = "Waiting on exec"
	if got := a.attentionSummary(rows[0]); got != "Waiting on exec" {
		t.Fatalf("the hook's message shows as %q", got)
	}
}

// A row of a session working on an issue offers the issue, which opens in
// the Linear view; a row of a session with none offers nothing.
func TestAttentionRowOpensIssue(t *testing.T) {
	a := testAttention(t,
		attnFixture{state: proto.AttentionWaitingUser, msg: "Approve", agent: "claude"},
		attnFixture{state: proto.AttentionWaitingUser, msg: "Approve too", agent: "claude"})
	a.session("s2").TaskID = "BRU-25"
	a.st.Linear = &proto.LinearState{Status: proto.LinearOK, Team: "BRU"}
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if _, ok := tt.Find("Open issue BRU-25"); !ok {
		t.Fatalf("no way to the issue; texts %q", tt.Texts())
	}
	if _, ok := tt.Find("Open issue"); ok {
		t.Error("a session with no issue offers one")
	}
	if err := tt.Click("Open issue BRU-25"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if a.show != "linear" || a.lin.open != "BRU-25" {
		t.Fatalf("show %q, issue %q; want linear with BRU-25", a.show, a.lin.open)
	}
	if a.selected == "s2" {
		t.Error("the button also opened the row's session")
	}

	// Back on the pane, with the session's issue gone: no button.
	a.show, a.selected = "session", "s1"
	a.session("s2").TaskID = ""
	frames(tt)
	if _, ok := tt.Find("Open issue BRU-25"); ok {
		t.Error("a session with no issue offers one")
	}
}
