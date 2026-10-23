package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
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

// A real kerod sends the idle state for a quiet session, not no state:
// with every session idle the pane is still empty, and has no rows.
func TestAttentionPaneEmptyIdle(t *testing.T) {
	a := testAttention(t,
		attnFixture{state: proto.AttentionIdle, agent: "claude"},
		attnFixture{state: proto.AttentionIdle, agent: "codex"},
	)
	if rows := a.attentionRows(); len(rows) != 0 {
		t.Fatalf("idle sessions made %d rows, want 0", len(rows))
	}
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if !tt.HasText("Nothing needs you") {
		t.Errorf("no empty state with every session idle; texts %q", tt.Texts())
	}
	if tt.HasText("All") {
		t.Errorf("the toolbar shows with no rows; texts %q", tt.Texts())
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
	// The two idle sessions are no rows.
	if got := len(a.attentionRows()); got != 8 {
		t.Fatalf("%d rows, want 8", got)
	}
	// The most urgent first: the last fixture of each kind is the
	// oldest, so a kind's own order is newest first.
	rows := a.attentionRows()
	if rows[0].att.State != proto.AttentionWaitingUser || rows[7].att.State != proto.AttentionDone {
		t.Fatalf("rows go %s…%s, want waiting-user…done", rows[0].att.State, rows[7].att.State)
	}
	// The sidebar names every session, so the rows' own words are what
	// prove each is listed: "work N" is only in a row.
	for _, i := range []int{1, 2, 3, 4, 6, 7, 8, 9} {
		if want := fmt.Sprintf("work %d", i); !tt.HasText(want) {
			t.Errorf("no row saying %q; texts %q", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"work 5", "work 10"} {
		if tt.HasText(unwanted) {
			t.Errorf("an idle session is a row saying %q", unwanted)
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
// needs the user, though the session was left in another pane, zoomed.
func TestAttentionRowFocusesPane(t *testing.T) {
	a := testAttention(t, attnFixture{state: proto.AttentionWaitingUser, msg: "Bash command needs approval", agent: "claude"})
	s := a.session("s2")
	s.Focus = "bs2"
	s.Zoomed = "bs2"
	a.focus = map[string]string{"s2": "bs2"}
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	// The row's own words, which the sidebar does not say.
	if err := tt.Click("Bash command needs approval"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if a.selected != "s2" {
		t.Fatalf("the row selected %q, want s2", a.selected)
	}
	s = a.session("s2")
	if got := a.focusOf(s); got != "as2" {
		t.Fatalf("the row focused %q, want the pane that needs the user", got)
	}
	if s.Zoomed != "" {
		t.Fatalf("the zoom on %q stayed, hiding the pane that needs the user", s.Zoomed)
	}
}

// The state of a pane that closed is dropped with it.
func TestAttentionViewPruned(t *testing.T) {
	a := testAttention(t)
	a.loaded = true
	a.attentionViewOf("b").query = "x"
	a.attentionViewOf("gone").query = "y"
	a.sweepViews()
	if _, ok := a.attention["gone"]; ok {
		t.Error("the view of a closed pane stayed")
	}
	if _, ok := a.attention["b"]; !ok {
		t.Error("the view of an open pane was dropped")
	}
}

// kerod words every wait (paneWaitMsg: "Needs you" at the least), so a
// Codex approval always has a message; its row still says what to do.
func TestAttentionCodexApproval(t *testing.T) {
	a := testAttention(t,
		attnFixture{state: proto.AttentionWaitingUser, msg: "Needs you", agent: "codex"},
		attnFixture{state: proto.AttentionWaitingUser, msg: "Needs you", agent: "claude"},
	)
	rows := a.attentionRows()
	if got := a.attentionSummary(rows[0]); got != "Needs you · Focus pane to approve" {
		t.Fatalf("a Codex wait says %q", got)
	}
	if got := a.attentionSummary(rows[1]); got != "Needs you" {
		t.Fatalf("a Claude wait says %q", got)
	}
	rows[0].att.Msg = "Waiting on exec"
	if got := a.attentionSummary(rows[0]); got != "Waiting on exec · Focus pane to approve" {
		t.Fatalf("the hook's message shows as %q", got)
	}
	// Only a wait: a Codex turn that ran has no approval to give.
	rows[0].att.State, rows[0].att.Msg = proto.AttentionRunning, ""
	if got := a.attentionSummary(rows[0]); got != "Working" {
		t.Fatalf("a Codex run says %q", got)
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
	target, _ := protocol.EncodeTarget(protocol.Target{Plugin: "linear", Widget: "board", State: json.RawMessage(`{"query":"keep"}`)})
	a.session("s1").Panes = append(a.session("s1").Panes, proto.Pane{ID: "linear", View: proto.PanePlugins, Target: target})
	var saved string
	a.plugins.saveTarget = func(pane, target string) { saved = target }
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
	targetState, err := protocol.DecodeTarget(saved)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]string
	if err := json.Unmarshal(targetState.State, &state); err != nil {
		t.Fatal(err)
	}
	if state["open"] != "BRU-25" || state["query"] != "keep" || a.focusOf(a.session("s1")) != "linear" {
		t.Fatalf("issue target %s, focus %s", saved, a.focusOf(a.session("s1")))
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

// Filters, search and scroll belong to the pane target, so reconstructing
// the client keeps the same view without a daemon format migration.
func TestAttentionTargetRoundTrip(t *testing.T) {
	a := testAttention(t, attnFixture{state: proto.AttentionWaitingUser, msg: "Approve", agent: "codex"})
	p := a.paneOf("b")
	p.Target = `{"filter":"waiting-user","query":"Approve","scroll":104}`
	st := a.attentionViewOf("b")
	if st.filter != proto.AttentionWaitingUser || st.query != "Approve" || st.scroll.Y != 104 {
		t.Fatalf("restored filter=%q query=%q scroll=%v", st.filter, st.query, st.scroll.Y)
	}
	st.filter, st.query, st.scroll.Y = proto.AttentionRunning, "next", 52
	a.saveAttentionView(p, st)
	target := p.Target
	delete(a.attention, "b")
	restored := a.attentionViewOf("b")
	if restored.filter != proto.AttentionRunning || restored.query != "next" || restored.scroll.Y != 52 {
		t.Fatalf("round trip target %s: %+v", target, restored)
	}
	a.saveAttentionView(p, restored)
	if p.Target != target {
		t.Fatal("unchanged view rewrote its target")
	}
}

func TestAttentionTargetInvalid(t *testing.T) {
	for _, target := range []string{`broken`, `null`, `{"filter":"invalid","query":"keep","scroll":-20}`} {
		a := testAttention(t)
		a.paneOf("b").Target = target
		st := a.attentionViewOf("b")
		if st.filter != "" || st.scroll.Y != 0 {
			t.Fatalf("invalid target %q restored %+v", target, st)
		}
	}
}

func TestAttentionInputSavesTarget(t *testing.T) {
	a := testAttention(t, attnFixture{state: proto.AttentionRunning, msg: "Working", agent: "codex"})
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	if err := tt.Click("Search attention"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	tt.Type("Working")
	frames(tt)
	var saved struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(a.paneOf("b").Target), &saved); err != nil || saved.Query != "Working" {
		t.Fatalf("input not saved: %q, %v", a.paneOf("b").Target, err)
	}
}

// State changes while a target write is pending cannot overtake it. A
// tester has no native window callback queue, so it delivers the same
// completion handler explicitly after the peer acknowledges the request.
func TestAttentionTargetWritesNewestAfterPending(t *testing.T) {
	a := testAttention(t)
	p := a.paneOf("b")
	st := a.attentionViewOf("b")
	requests := attentionRPC(t, a)
	st.query = "first"
	a.saveAttentionView(p, st)
	sent := p.Target
	st.query = "final"
	a.saveAttentionView(p, st)
	select {
	case req := <-requests:
		var params proto.TargetParams
		if req.Method != "pane.target" || json.Unmarshal(req.Params, &params) != nil || params.Target != sent {
			t.Fatalf("first request %+v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("no initial target write")
	}
	select {
	case req := <-requests:
		t.Fatalf("pending target was overtaken: %+v", req)
	case <-time.After(10 * time.Millisecond):
	}
	a.attentionViewSaved(p.ID, st, sent, nil)
	select {
	case req := <-requests:
		var params proto.TargetParams
		if json.Unmarshal(req.Params, &params) != nil {
			t.Fatal("invalid target params")
		}
		var target attentionTarget
		if json.Unmarshal([]byte(params.Target), &target) != nil || target.Query != "final" {
			t.Fatalf("final target %q", params.Target)
		}
	case <-time.After(time.Second):
		t.Fatal("latest state was not saved after completion")
	}
}

// A rejected target remains dirty without causing a redraw/request loop.
// New input or a new connection may retry, always with the newest view.
func TestAttentionTargetFailureWaitsForChangeOrReconnect(t *testing.T) {
	a := testAttention(t)
	p := a.paneOf("b")
	st := a.attentionViewOf("b")
	requests := attentionRPC(t, a)
	st.query = "first"
	a.saveAttentionView(p, st)
	sent := p.Target
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("no initial save")
	}
	a.attentionViewSaved(p.ID, st, sent, errors.New("target rejected"))
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	frames(tt)
	select {
	case req := <-requests:
		t.Fatalf("failed save retried without a change: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}
	if st.saved == sent || st.query != "first" {
		t.Fatalf("failure acknowledged or lost dirty state: saved=%q query=%q", st.saved, st.query)
	}
	st.query = "newest"
	frames(tt)
	select {
	case req := <-requests:
		var params proto.TargetParams
		var target attentionTarget
		if json.Unmarshal(req.Params, &params) != nil || json.Unmarshal([]byte(params.Target), &target) != nil || target.Query != "newest" {
			t.Fatalf("changed target not sent: %+v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("new input did not permit save")
	}
	sent = p.Target
	a.attentionViewSaved(p.ID, st, sent, errors.New("still rejected"))
	// The client object changes when the daemon control connection recovers.
	// Its first frame must retry the latest dirty state once.
	recovered := attentionRPC(t, a)
	frames(tt)
	select {
	case req := <-recovered:
		var params proto.TargetParams
		if json.Unmarshal(req.Params, &params) != nil || params.Target != sent {
			t.Fatalf("recovery lost newest target: %+v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("reconnection did not permit retry")
	}
	a.attentionViewSaved(p.ID, st, sent, nil)
	frames(tt)
	select {
	case req := <-recovered:
		t.Fatalf("acknowledged target resent: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestAttentionRowsWithoutDaemonFold(t *testing.T) {
	a := testAttention(t, attnFixture{state: proto.AttentionWaitingUser, agent: "codex"})
	s := a.session("s2")
	s.Attention = nil
	s.Panes[0].Status, s.Panes[0].Message = proto.Waiting, "Needs permission"
	rows := a.attentionRows()
	if len(rows) != 1 || rows[0].session.ID != "s2" || rows[0].att.State != proto.AttentionWaitingUser || rows[0].att.Pane != "as2" || rows[0].agent != "codex" {
		t.Fatalf("older-daemon wait omitted: %+v", rows)
	}
}
