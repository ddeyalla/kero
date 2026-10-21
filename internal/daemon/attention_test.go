package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"kero/internal/detect"
	"kero/internal/proto"
)

// The attention tests are fixture-driven: what the panes say folds into
// the one state a session's row shows, and what an agent's transcript on
// disk says comes to the pane as the scan's records (applySoft), which
// the screen and the hooks keep off of it. The transcripts here are the
// tests' own synthetics; their contents are never printed.

var scanBase = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

func scanYoungAt() string { return scanBase.Add(-10 * time.Second).Format(time.RFC3339Nano) }
func scanOldAt() string   { return scanBase.Add(-10 * time.Minute).Format(time.RFC3339Nano) }
func scanYoungMs() int64  { return scanBase.Add(-10 * time.Second).UnixMilli() }
func scanOldMs() int64    { return scanBase.Add(-10 * time.Minute).UnixMilli() }

// scanBurn writes the fixtures' lines into a file, joined as the writer
// told.
func scanBurn(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// scanClaudePath is where Claude Code keeps a session's conversation, in
// the tests' home: of the session, as run in dir.
func scanClaudePath(dir, session string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects", claudeProjectDir(dir), session+".jsonl")
}

// scanCodexPath is where Codex keeps a session's rollout, in the tests'
// home, dated as a rollout's is.
func scanCodexPath(session string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions", "2026", "06", "08", "rollout-shibu-"+session+".jsonl")
}

// scanAgent sets the pane's agent session, as the scan sees it.
func scanAgent(p *pane, agent, session string) {
	p.mu.Lock()
	p.info.AgentSession = &proto.AgentSession{Agent: agent, ID: session}
	p.mu.Unlock()
}

// paneInfo reads a pane's fields as the fold does.
func paneInfo(p *pane) proto.Pane {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info
}

// runScan runs one scan pass at now and says whether it claimed to have
// applied what it read.
func runScan(t *testing.T, p *pane, now time.Time) bool {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scanTranscript(now)
}

// runScanIs asserts a pass's claim, by what it says of itself.
func runScanIs(t *testing.T, p *pane, now time.Time, want bool) {
	t.Helper()
	if got := runScan(t, p, now); got != want {
		t.Fatalf("the scan pass claimed %v, want %v", got, want)
	}
}

// scanRecord reads the scan's record on the pane, and whether it stands.
func scanRecord(p *pane) (proto.Record, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.records[scanRecordID]
	return r, ok
}

// scanFakeProc gives a pane a program, as the scan's gates ask: the
// program itself is the tests'.
func scanFakeProc(p *pane) {
	p.mu.Lock()
	p.proc = &ptyProc{}
	p.mu.Unlock()
}

// The fold of a session's panes: one waiting beats one working, an
// unseen end beats rest, and of two as urgent, the first's words show.
func TestAttentionFold(t *testing.T) {
	mk := func(id string, info proto.Pane) *pane {
		info.ID, info.Since = id, 5
		return &pane{info: info, changed: make(chan struct{})}
	}
	idle := mk("p1", proto.Pane{Status: proto.Idle})
	waiting := mk("p2", proto.Pane{Status: proto.Waiting, Kind: "question"})
	other := mk("p3", proto.Pane{Status: proto.Waiting, Kind: "permission"})
	perms := mk("p4", proto.Pane{Status: proto.Waiting, Kind: "auth"})
	working := mk("p5", proto.Pane{Status: proto.Working, Progress: 65})
	unseen := mk("p6", proto.Pane{Status: proto.Idle, Unread: true, Message: "Build passed"})
	gone := mk("p7", proto.Pane{Status: proto.Working})
	gone.mu.Lock()
	gone.closed = true
	gone.mu.Unlock()
	unseen2 := mk("p8", proto.Pane{Status: proto.Idle, Unread: true, Message: "Wrapped up"})
	unseen2.info.Since = 7

	for _, c := range []struct {
		name  string
		panes []*pane
		want  attention
	}{
		{"nothing says rest", []*pane{idle}, attention{State: attentionIdle, Pane: "p1", Session: "s", Since: 5}},
		{"work says work", []*pane{idle, working}, attention{State: attentionRunning, Msg: "Working · 65%", Pane: "p5", Session: "s", Since: 5}},
		{"an unseen end beats rest", []*pane{idle, unseen}, attention{State: attentionUnread, Msg: "Build passed", Pane: "p6", Session: "s", Since: 5}},
		{"work beats an unseen end", []*pane{unseen, working}, attention{State: attentionRunning, Msg: "Working · 65%", Pane: "p5", Session: "s", Since: 5}},
		{"a wait beats work", []*pane{working, waiting}, attention{State: attentionWaitingUser, Msg: "Asked a question", Pane: "p2", Session: "s", Since: 5}},
		{"the first of two waits stands", []*pane{waiting, other}, attention{State: attentionWaitingUser, Msg: "Asked a question", Pane: "p2", Session: "s", Since: 5}},
		{"the first of two unseen ends stands", []*pane{unseen, unseen2}, attention{State: attentionUnread, Msg: "Build passed", Pane: "p6", Session: "s", Since: 5}},
		{"nothing says rest of none", []*pane{}, attention{State: attentionIdle, Session: "s"}},
	} {
		d := &Daemon{changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
		d.viewed.Store(new(string))
		d.mu.Lock()
		d.sessions = []*session{{id: "s", panes: c.panes, focus: "p1"}}
		d.mu.Unlock()
		got := d.attention()
		if len(got) != 1 {
			t.Fatalf("%s: %d attentions", c.name, len(got))
		}
		if got[0] != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got[0], c.want)
		}
	}

	// One of many: the most urgent of the panes that run, none of the
	// ones the user has closed.
	d := &Daemon{changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	d.viewed.Store(new(string))
	d.mu.Lock()
	d.sessions = []*session{{id: "s", panes: []*pane{gone, perms}, focus: "p4"}}
	d.mu.Unlock()
	if got := d.attention(); len(got) != 1 || got[0].State != attentionWaitingUser ||
		got[0].Msg != "Needs authentication" || got[0].Pane != "p4" {
		t.Errorf("of the ones that run: %+v", got)
	}

	// More than one session, one attention each.
	d.mu.Lock()
	d.sessions = []*session{{id: "s1", panes: []*pane{idle}}, {id: "s2", panes: []*pane{waiting}}}
	d.mu.Unlock()
	got := d.attention()
	if len(got) != 2 || got[0].Session != "s1" || got[0].State != attentionIdle ||
		got[1].Session != "s2" || got[1].State != attentionWaitingUser {
		t.Errorf("of two sessions: %+v", got)
	}
}

// What the state kerod broadcasts says of a session: its fold, so that
// the attention pane and the notifications read one truth — the most
// urgent pane, its words, its pane and its age.
func TestStateCarriesAttention(t *testing.T) {
	mk := func(id string, info proto.Pane) *pane {
		info.ID, info.Since = id, 5
		return &pane{info: info, changed: make(chan struct{})}
	}
	d := &Daemon{changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	d.viewed.Store(new(string))
	d.mu.Lock()
	d.sessions = []*session{
		{id: "s1", focus: "a", panes: []*pane{
			mk("a", proto.Pane{Status: proto.Working, Progress: -1}),
			mk("b", proto.Pane{Status: proto.Waiting, Kind: "permission"}),
		}},
		{id: "s2", panes: []*pane{
			mk("c", proto.Pane{Status: proto.Idle, Unread: true, Message: "Build passed"}),
		}},
		{id: "s3", panes: []*pane{
			mk("d", proto.Pane{Status: proto.Idle}),
		}},
	}
	d.mu.Unlock()
	st := d.state()
	if len(st.Sessions) != 3 {
		t.Fatalf("%d sessions in the state", len(st.Sessions))
	}
	want := []*proto.Attention{
		{State: "waiting-user", Msg: "Needs permission", Since: 5, Pane: "b"},
		{State: "unread", Msg: "Build passed", Since: 5, Pane: "c"},
		{State: "idle", Pane: "d", Since: 5},
	}
	for i, w := range want {
		got := st.Sessions[i].Attention
		if got == nil || *got != *w {
			t.Errorf("%s: %+v, want %+v", st.Sessions[i].ID, got, w)
		}
	}
}

// A pane's own attention, from its status and marks; its row shows it.
func TestPaneAttention(t *testing.T) {
	mk := func(info proto.Pane) *pane { return &pane{info: info, changed: make(chan struct{})} }
	restored := &pane{info: proto.Pane{ID: "a", Status: proto.Idle, Unread: true, Since: 5},
		summary: "The build passed\nmore", changed: make(chan struct{})}
	for _, c := range []struct {
		name string
		p    *pane
		want attention
	}{
		{"a view is quiet", mk(proto.Pane{ID: "v", View: proto.PaneFiles, Since: 5}), attention{State: attentionIdle, Since: 5}},
		{"a view keeps its mark", mk(proto.Pane{ID: "v", View: proto.PaneFiles, Unread: true, Since: 5}), attention{State: attentionUnread, Since: 5}},
		{"a view never says a turn", mk(proto.Pane{ID: "v", View: proto.PaneTasks, Status: proto.Failed, Unread: true, Message: "no issue", Since: 5}), attention{State: attentionUnread, Since: 5}},
		{"working by progress", mk(proto.Pane{ID: "a", Status: proto.Working, Progress: 30, Since: 5}), attention{State: attentionRunning, Msg: "Working · 30%", Since: 5}},
		{"working by its word", mk(proto.Pane{ID: "a", Status: proto.Working, Progress: -1, Message: "Writing tests", Since: 5}), attention{State: attentionRunning, Msg: "Writing tests", Since: 5}},
		{"working by nothing", mk(proto.Pane{ID: "a", Status: proto.Working, Progress: -1, Since: 5}), attention{State: attentionRunning, Msg: "Working", Since: 5}},
		{"working prefers its percent", mk(proto.Pane{ID: "a", Status: proto.Working, Progress: 12, Message: "busy", Since: 5}), attention{State: attentionRunning, Msg: "Working · 12%", Since: 5}},
		{"waiting by its word", mk(proto.Pane{ID: "a", Status: proto.Waiting, Message: "May I run make?", Since: 5}), attention{State: attentionWaitingUser, Msg: "May I run make?", Since: 5}},
		{"waiting for permission", mk(proto.Pane{ID: "a", Status: proto.Waiting, Kind: "permission", Since: 5}), attention{State: attentionWaitingUser, Msg: "Needs permission", Since: 5}},
		{"waiting for an answer", mk(proto.Pane{ID: "a", Status: proto.Waiting, Kind: "question", Since: 5}), attention{State: attentionWaitingUser, Msg: "Asked a question", Since: 5}},
		{"waiting for its login", mk(proto.Pane{ID: "a", Status: proto.Waiting, Kind: "auth", Since: 5}), attention{State: attentionWaitingUser, Msg: "Needs authentication", Since: 5}},
		{"waiting for nothing known", mk(proto.Pane{ID: "a", Status: proto.Waiting, Since: 5}), attention{State: attentionWaitingUser, Msg: "Needs you", Since: 5}},
		{"waiting tells its word before its kind", mk(proto.Pane{ID: "a", Status: proto.Waiting, Kind: "auth", Message: "Signing in…", Since: 5}), attention{State: attentionWaitingUser, Msg: "Signing in…", Since: 5}},
		{"an unseen end keeps its words", mk(proto.Pane{ID: "a", Status: proto.Idle, Unread: true, Message: "Build passed\n\nNotes", Since: 5}), attention{State: attentionUnread, Msg: "Build passed", Since: 5}},
		{"an unseen end of nothing", mk(proto.Pane{ID: "a", Status: proto.Idle, Unread: true, Since: 5}), attention{State: attentionUnread, Msg: "Turn ended", Since: 5}},
		{"a read end of a word is done", mk(proto.Pane{ID: "a", Status: proto.Idle, Message: "Build passed", Since: 5}), attention{State: attentionDone, Msg: "Build passed", Since: 5}},
		{"a read end of nothing is rest", mk(proto.Pane{ID: "a", Status: proto.Idle, Since: 5}), attention{State: attentionIdle, Since: 5}},
		{"a failure unseen", mk(proto.Pane{ID: "a", Status: proto.Failed, Unread: true, Message: "tests failed", Since: 5}), attention{State: attentionUnread, Msg: "Failed: tests failed", Since: 5}},
		{"a failure unseen of no word", mk(proto.Pane{ID: "a", Status: proto.Failed, Unread: true, Since: 5}), attention{State: attentionUnread, Msg: "Failed", Since: 5}},
		{"a failure read", mk(proto.Pane{ID: "a", Status: proto.Failed, Message: "boom", Since: 5}), attention{State: attentionDone, Msg: "boom", Since: 5}},
		{"an exited shell unseen", mk(proto.Pane{ID: "a", Status: proto.Exited, Unread: true, ExitCode: 2, Since: 5}), attention{State: attentionUnread, Msg: "Exited with code 2", Since: 5}},
		{"an exited shell read is rest", mk(proto.Pane{ID: "a", Status: proto.Exited, ExitCode: 2, Since: 5}), attention{State: attentionIdle, Since: 5}},
		{"an unseen end of a restore, said once", restored, attention{State: attentionUnread, Msg: "The build passed", Since: 5}},
	} {
		got, want := paneAttention(c.p), c.want
		if got != want {
			t.Errorf("%s: %+v, want %+v", c.name, got, want)
		}
	}
}

// The words a state shows are a text's first line, tidied and cut.
func TestFirstLineShowsOneLine(t *testing.T) {
	for _, c := range []struct {
		text string
		want string
	}{
		{"", ""},
		{"one line only", "one line only"},
		{"first\nsecond\nthird", "first"},
		{">quoted word\tand   spaces", ">quoted word and spaces"},
		{"ctrl\x07chars\x1bgone", "ctrlcharsgone"},
	} {
		if got := firstLine(c.text, 80); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.text, got, c.want)
		}
	}
	// A cut lands between runes, however far they reach.
	line := "-" + strings.Repeat("ß", 100)
	if got := firstLine(line, 80); got != "-"+strings.Repeat("ß", 39) {
		t.Errorf("a cut split a rune: %d bytes", len(got))
	}
}

// Claude Code's transcript lines, as the scan reads them.

func clA(ts, text string, calls ...[2]string) string {
	items := ""
	if text != "" {
		items += `{"type":"text","text":` + quote(text) + `},`
	}
	for _, call := range calls {
		items += `{"type":"tool_use","id":` + quote(call[0]) + `,"name":` + quote(call[1]) + `},`
	}
	return `{"type":"assistant","timestamp":"` + ts + `","message":{"role":"assistant","content":[` + strings.TrimSuffix(items, ",") + `]}}` + "\n"
}

func clU(ts, text string, isMeta bool) string {
	meta := ""
	if isMeta {
		meta = `,"isMeta":true`
	}
	return `{"type":"user","timestamp":"` + ts + `"` + meta + `,"message":{"role":"user","content":[{"type":"text","text":` + quote(text) + `}]}}` + "\n"
}

func clR(ts, toolUseID string) string {
	return `{"type":"user","timestamp":"` + ts + `","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + toolUseID + `"}]}}` + "\n"
}

func clS(ts, subtype string) string {
	return `{"type":"system","subtype":"` + subtype + `","timestamp":"` + ts + `"}` + "\n"
}

// Codex's rollout lines, as the scan reads them; item is the payload's
// extra words, as of a completion's message.
func cxE(ts, kind, item string) string {
	return `{"type":"event_msg","timestamp":"` + ts + `","payload":{"type":"` + kind + `"` + item + `}}` + "\n"
}

func cxI(ts, kind, callID, name string) string {
	b := `{"type":"` + kind + `"`
	if callID != "" {
		b += `,"call_id":"` + callID + `"`
	}
	if name != "" {
		b += `,"name":` + quote(name)
	}
	return `{"type":"response_item","timestamp":"` + ts + `","payload":` + b + "}}" + "\n"
}

func cxM(ts, role string) string {
	return `{"type":"response_item","timestamp":"` + ts + `","payload":{"type":"message","role":"` + role + `"}}` + "\n"
}

// Claude Code's conversation tells a turn by its agent's lines, the
// user's, and the system's: the calls say work while they run, the
// system's lines say a turn done by the words it ended with, and the
// user's own word says the turn answers to them, whatever ran before.
func TestClaudeScanReadsATail(t *testing.T) {
	ts := scanBase.Format(time.RFC3339Nano)
	for _, c := range []struct {
		name  string
		lines []string
		want  scanVerdict
	}{
		{"nothing said, nothing claimed", []string{}, scanVerdict{}},
		{"the agent's word runs a turn", []string{clA(ts, "Thinking it over…")}, scanVerdict{state: "working"}},
		{"a call under way is the work, by when it ran", []string{clA(scanYoungAt(), "", [2]string{"t1", "Bash"})}, scanVerdict{msg: "Bash", callAt: scanYoungMs()}},
		{"words and a call are the call, until it ends", []string{clA(scanYoungAt(), "Building it…", [2]string{"t1", "Bash"})}, scanVerdict{msg: "Bash", callAt: scanYoungMs()}},
		{"a call of no id is not tracked", []string{clA(ts, "", [2]string{"", "Bash"})}, scanVerdict{}},
		{"of two calls, the one that has waited longest", []string{clA(scanOldAt(), "", [2]string{"t1", "Bash"}, [2]string{"t2", "Read"})}, scanVerdict{msg: "Bash", callAt: scanOldMs()}},
		{"a result ends only the call it is of", []string{clA(scanOldAt(), "", [2]string{"t1", "Bash"}), clA(scanYoungAt(), "", [2]string{"t2", "Read"}), clR(ts, "t2")}, scanVerdict{msg: "Bash", callAt: scanOldMs()}},
		{"its result gone, the agent's word is the work", []string{clA(ts, "Building it…", [2]string{"t1", "Bash"}), clR(ts, "t1")}, scanVerdict{state: "working"}},
		{"a turn's end is done, by the words it ended with", []string{clA(ts, "All tests pass"), clS(ts, "turn_duration")}, scanVerdict{state: "done", msg: "All tests pass"}},
		{"a turn's end that a stop hook told", []string{clA(ts, "Deployed."), clS(ts, "stop_hook_summary")}, scanVerdict{state: "done", msg: "Deployed."}},
		{"the user's own word answers the turn, an Esc as an answer", []string{clA(ts, "All done."), clS(ts, "turn_duration"), clU(ts, "thanks", false)}, scanVerdict{}},
		{"the agent acting again is whatever came before", []string{clA(ts, "All done."), clS(ts, "turn_duration"), clA(ts, "One more thing.")}, scanVerdict{state: "working"}},
		{"the user speaking before the agent acts is answered", []string{clU(ts, "soon", false), clA(ts, "On it.")}, scanVerdict{state: "working"}},
		{"what the pane says of its results is no user's word", []string{clA(ts, "Still here."), clU(ts, "wrapper noise", true)}, scanVerdict{state: "working"}},
		{"a line of no form is passed over", []string{clA(ts, "Still here."), "not json at all\n"}, scanVerdict{state: "working"}},
	} {
		v := parseScan("claude", []byte(strings.Join(c.lines, "")), scanBase)
		if !reflect.DeepEqual(v, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, v, c.want)
		}
	}
}

// Codex's rollout tells a turn by its events and by what its responses
// said: a turn begun runs, its calls say work while they run, a
// completion says done by its last words, and the user's own message
// says the turn answers to them.
func TestCodexScanReadsATail(t *testing.T) {
	ts := scanBase.Format(time.RFC3339Nano)
	for _, c := range []struct {
		name  string
		lines []string
		want  scanVerdict
	}{
		{"nothing said, nothing claimed", []string{}, scanVerdict{}},
		{"a turn begun runs", []string{cxE(ts, "task_started", "")}, scanVerdict{state: "working"}},
		{"a call under way is the work, by when it ran", []string{cxE(ts, "task_started", ""), cxI(scanYoungAt(), "function_call", "c1", "shell")}, scanVerdict{msg: "shell", callAt: scanYoungMs()}},
		{"of two calls, the one that has waited longest", []string{cxE(ts, "task_started", ""), cxI(scanOldAt(), "function_call", "c1", "shell"), cxI(scanYoungAt(), "custom_tool_call", "c2", "read")}, scanVerdict{msg: "shell", callAt: scanOldMs()}},
		{"a call of no id is not tracked", []string{cxE(ts, "task_started", ""), cxI(scanYoungAt(), "function_call", "", "shell")}, scanVerdict{state: "working"}},
		{"an output ends its call", []string{cxE(ts, "task_started", ""), cxI(scanOldAt(), "function_call", "c1", "shell"), cxI(ts, "function_call_output", "c1", "")}, scanVerdict{state: "working"}},
		{"a turn complete is done, by its last words", []string{cxE(ts, "task_started", ""), cxE(ts, "task_complete", `,"last_agent_message":"Build finished"`)}, scanVerdict{state: "done", msg: "Build finished"}},
		{"a completion's first line is enough", []string{cxE(ts, "task_started", ""), cxE(ts, "task_complete", `,"last_agent_message":"Done.\nMore to read."`)}, scanVerdict{state: "done", msg: "Done."}},
		{"the user's word last waits on the user", []string{cxE(ts, "task_started", ""), cxM(ts, "user")}, scanVerdict{}},
		{"the agent speaking again is at rest, of nothing begun", []string{cxM(ts, "user"), cxM(ts, "assistant")}, scanVerdict{}},
		{"a turn stopped is over", []string{cxE(ts, "task_started", ""), cxI(scanOldAt(), "function_call", "c1", "shell"), cxE(ts, "turn_aborted", "")}, scanVerdict{}},
		{"a stopped turn, begun again, runs", []string{cxE(ts, "task_started", ""), cxE(ts, "turn_aborted", ""), cxE(ts, "task_started", "")}, scanVerdict{state: "working"}},
		{"a turn begun again is no user's to answer", []string{cxE(ts, "task_started", ""), cxM(ts, "user"), cxE(ts, "task_started", "")}, scanVerdict{state: "working"}},
	} {
		v := parseScan("codex", []byte(strings.Join(c.lines, "")), scanBase)
		if !reflect.DeepEqual(v, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, v, c.want)
		}
	}
}

// A call the transcript shows under way, whose age a pass judges: young
// it is work, and at the hold — exactly at it, never sooner — a wait on
// the user, by the call it hangs on. What a parse said else stands as it
// is; a verdict of no call that says nothing stays nothing.
func TestScanVerdictOfACall(t *testing.T) {
	hold := unresolvedCallHold.Milliseconds()
	for _, c := range []struct {
		name      string
		v         scanVerdict
		now       int64
		wantState string
		wantMsg   string
	}{
		{"young is work", scanVerdict{msg: "Bash", callAt: scanYoungMs()}, scanBase.UnixMilli(), "working", "Working · Bash"},
		{"one short of the hold is still work", scanVerdict{msg: "Bash", callAt: scanYoungMs()}, scanBase.Add(35 * time.Second).Add(-time.Millisecond).UnixMilli(), "working", "Working · Bash"},
		{"at the hold, exactly, it waits", scanVerdict{msg: "Bash", callAt: scanYoungMs()}, scanBase.Add(35 * time.Second).UnixMilli(), "blocked", "Waiting on Bash"},
		{"past the hold it waits", scanVerdict{msg: "Bash", callAt: scanYoungMs()}, scanBase.Add(35 * time.Second).Add(time.Hour).UnixMilli(), "blocked", "Waiting on Bash"},
		{"a state a parse said stands", scanVerdict{state: "done", msg: "Fine", callAt: scanYoungMs()}, scanBase.Add(time.Hour).UnixMilli(), "done", "Fine"},
		{"nothing at all is nothing", scanVerdict{}, 0, "", ""},
		{"nothing at any pass is nothing", scanVerdict{}, scanBase.Add(time.Hour).UnixMilli(), "", ""},
	} {
		gotState, gotMsg := scanVerdictOf(c.v, c.now)
		if gotState != c.wantState || gotMsg != c.wantMsg {
			t.Errorf("%s: %s %q, want %s %q (hold %d)", c.name, gotState, gotMsg, c.wantState, c.wantMsg, hold)
		}
	}
	// A verdict of no time has none to grow: whatever its words, no age
	// makes it work or wait.
	if st, msg := scanVerdictOf(scanVerdict{msg: "Bash", callAt: 0}, scanBase.Add(time.Hour).UnixMilli()); st != "" || msg != "Bash" {
		t.Errorf("a verdict of no time claimed %s %q", st, msg)
	}
}

// A pass reads the end of a transcript, of whole lines only: never one
// half written, and of a tail begun mid-line, the partial first line
// with it goes.
func TestTailLinesKeepsWholeLines(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		path := filepath.Join(dir, name)
		scanBurn(t, path, data)
		return path
	}
	full := write("full", "aaa\nbbb\nccc\n")
	torn := write("torn", "aaa\nbbb\ncct")
	empty := write("empty", "")
	for _, c := range []struct {
		name string
		path string
		n    int64
		want string
	}{
		{"the whole of a short file", full, 20, "aaa\nbbb\nccc\n"},
		{"the lines of the end", full, 6, "ccc\n"},
		{"a torn last line is not a line", torn, 20, "aaa\nbbb\n"},
		{"no whole line in the seek", torn, 4, ""},
		{"an empty file says nothing", empty, 20, ""},
		{"no file at all says nothing", filepath.Join(dir, "none"), 20, ""},
	} {
		if got := tailLines(c.path, c.n); string(got) != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The scan looks a session's transcript up where its agent keeps one, by
// the session's id as a file name; an id of no file shape, or an agent
// of no two, is no file.
func TestScanToTheFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "my logs & notes")

	// Claude Code's own naming first: of the directory as it named it,
	// by the session.
	of := scanClaudePath(dir, "s-1")
	scanBurn(t, of, "{\"type\":\"assistant\"}\n")
	if got := findTranscript("claude", dir, "s-1"); got != of {
		t.Errorf("claude's own file: %q, want %q", got, of)
	}

	// Or the newest of any project's that carries the session, where the
	// pane has moved, or the project is named otherwise.
	oldP := filepath.Join(home, ".claude", "projects", "-earlier", "s-2.jsonl")
	newP := filepath.Join(home, ".claude", "projects", "-newer one", "s-2.jsonl")
	scanBurn(t, oldP, "{}\n")
	scanBurn(t, newP, "{}\n")
	past := scanBase.Add(-2 * time.Hour)
	if err := os.Chtimes(oldP, past, past); err != nil {
		t.Fatal(err)
	}
	if got := findTranscript("claude", home, "s-2"); got != newP {
		t.Errorf("claude's fallback: %q, want %q", got, newP)
	}

	// Codex keeps rollouts the days of, somewhere in its sessions.
	cxOld := filepath.Join(home, ".codex", "sessions", "2026", "05", "01", "rollout-a-s-3.jsonl")
	cxNew := filepath.Join(home, ".codex", "sessions", "2026", "05", "02", "rollout-b-s-3.jsonl")
	scanBurn(t, cxOld, "{}\n")
	scanBurn(t, cxNew, "{}\n")
	if got := findTranscript("codex", dir, "s-3"); got != cxNew {
		t.Errorf("codex's rollout: %q, want %q", got, cxNew)
	}

	// A session id that is not a file name is none, of either agent; a
	// one the scan does not read never has one.
	for _, c := range []struct{ agent, session string }{
		{"claude", "../evil"}, {"claude", ""}, {"codex", "a/b"}, {"vim", "s-1"},
	} {
		if got := findTranscript(c.agent, dir, c.session); got != "" {
			t.Errorf("%s %q came out as %q", c.agent, c.session, got)
		}
	}

	// The directory is the path's own, of letters and digits, for all
	// that is not.
	if got := claudeProjectDir("/Users/pj/My:Work (fork)"); got != "-Users-pj-My-Work--fork-" {
		t.Errorf("claudeProjectDir: %q", got)
	}
}

// claimPane is a pane the scan may claim on: an agent's session of
// agent, whose transcript a pre-arranged file of the test's.
func claimPane(t *testing.T, agent string) (*pane, string) {
	t.Helper()
	p := testPane(t)
	scanAgent(p, agent, "sess-g")
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	p.mu.Lock()
	p.transcriptPath = path
	p.mu.Unlock()
	return p, path
}

// claimState asserts what the pane stands at, and reads it as its
// session's row would.
func claimState(t *testing.T, p *pane, wantStatus proto.Status, wantUnread bool, wantMsg string) {
	t.Helper()
	info := paneInfo(p)
	if info.Status != wantStatus || info.Unread != wantUnread || info.Message != wantMsg {
		t.Fatalf("stands at %s unread %v %q; want %s unread %v %q", info.Status, info.Unread, info.Message, wantStatus, wantUnread, wantMsg)
	}
}

// claimRecord asserts the scan's record, by whether it stands and its
// state.
func claimRecord(t *testing.T, p *pane, wantState string) {
	t.Helper()
	r, ok := scanRecord(p)
	if wantState == "" {
		if ok {
			t.Fatalf("no claim was meant, but the scan said %s", r.State)
		}
		return
	}
	if !ok || r.State != wantState {
		t.Fatalf("the scan's record: %+v (standing %v), want %s", r, ok, wantState)
	}
}

// A record of another's owns the pane until it is dismissed, and no
// claim runs where the pane cannot hold one: no session, an agent of
// no two, and nothing the screen has not at least started.
func TestScanClaimsCarefully(t *testing.T) {
	ts := scanBase.Format(time.RFC3339Nano)

	// Nothing without an agent session, or with an agent of no two.
	p := testPane(t)
	runScanIs(t, p, scanBase, false)
	scanAgent(p, "vim", "sess")
	runScanIs(t, p, scanBase, false)

	// A working it reads is refused where no program runs.
	p, path := claimPane(t, "claude")
	scanBurn(t, path, clA(ts, "Thinking it over…"))
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")

	// Whereas work runs, its end is no news of a datum the scan brings.
	p, path = claimPane(t, "claude")
	scanBurn(t, path, clA(ts, "All tests pass"), clS(ts, "turn_duration"))
	p.mu.Lock()
	p.info.Status = proto.Working
	p.mu.Unlock()
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")

	// But a wait is, where the call runs long with no result.
	p, path = claimPane(t, "claude")
	scanBurn(t, path, clA(scanOldAt(), "", [2]string{"t1", "Bash"}))
	scanFakeProc(p)
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Waiting, true, "Waiting on Bash")
	claimRecord(t, p, "blocked")

	// Not of a pane the scan never spoke for: the screen holds.
	p, path = claimPane(t, "claude")
	scanBurn(t, path, clA(ts, "Working on it."))
	p.mu.Lock()
	p.info.Status = proto.Waiting
	p.mu.Unlock()
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")

	// Behind its own word, the scan may rise to work again...
	p.mu.Lock()
	p.soft = map[string]bool{scanRecordID: true}
	p.mu.Unlock()
	scanFakeProc(p)
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Working, false, "")
	claimRecord(t, p, "working")

	// A call gone old under the work joins it as a wait on the user...
	scanBurn(t, path, clA(scanOldAt(), "", [2]string{"call", "Bash"}))
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Waiting, true, "Waiting on Bash")
	claimRecord(t, p, "blocked")

	// ...but where the screen asks of its own, what the scan said of a
	// wait stands: its detect has seen the ask, and the scan takes
	// nothing back of it, however its call ended.
	p.mu.Lock()
	p.seen = detect.Blocked
	p.mu.Unlock()
	scanBurn(t, path, clA(scanOldAt(), "", [2]string{"call", "Bash"}), clR(ts, "call"), clA(scanYoungAt(), "Back at it."))
	runScanIs(t, p, scanBase, false)
	claimState(t, p, proto.Waiting, true, "Waiting on Bash")
	claimRecord(t, p, "blocked")

	// A pane that failed, or a shell that ended, needs nothing new of
	// its transcript, however the turn ended.
	p, path = claimPane(t, "claude")
	scanBurn(t, path, clA(ts, "All tests pass"), clS(ts, "turn_duration"))
	for _, st := range []proto.Status{proto.Failed, proto.Exited} {
		p.mu.Lock()
		p.info.Status = st
		p.mu.Unlock()
		runScanIs(t, p, scanBase, false)
	}
	claimRecord(t, p, "")

	// Nor is an end the pane has seen said again — said once it is,
	// against the mark, and once again only when a new one comes.
	p, path = claimPane(t, "claude")
	scanBurn(t, path, clA(ts, "All tests pass"), clS(ts, "turn_duration"))
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")
	p.mu.Lock()
	p.info.Unread = true
	p.mu.Unlock()
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Idle, true, "All tests pass")
}

// A story of an agent's turn through Claude Code's own words: the end
// claimed, said once; a new end said, seen or read; what the scan said
// of an end standing for the user's key; and nothing coming back of
// what the user dismissed.
func TestScanClaimsATurnOfClaude(t *testing.T) {
	ts := scanBase.Format(time.RFC3339Nano)
	p, path := claimPane(t, "claude")

	// The turn ended unseen, and its words said of it.
	p.mu.Lock()
	p.info.Unread = true
	p.mu.Unlock()
	scanBurn(t, path, clA(ts, "Build passed"), clS(ts, "turn_duration"))
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Idle, true, "Build passed")
	claimRecord(t, p, "done")
	p.mu.Lock()
	pa := paneAttention(p)
	p.mu.Unlock()
	if pa.State != attentionUnread || pa.Msg != "Build passed" {
		t.Fatalf("the session's row of it: %+v", pa)
	}

	// The same, said again at the same file, is no news.
	runScanIs(t, p, scanBase, false)

	// A new end, before it is seen, is not said while the pane is read
	// — and said once the mark is on it again. What was said does not
	// stand over what came to be.
	scanBurn(t, path, clA(ts, "Wrapped up"), clS(ts, "turn_duration"))
	p.mu.Lock()
	p.info.Unread = false
	p.mu.Unlock()
	runScanIs(t, p, scanBase, false)
	claimState(t, p, proto.Idle, false, "Build passed")
	p.mu.Lock()
	p.info.Unread = true
	p.mu.Unlock()
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Idle, true, "Wrapped up")

	// The user's word last: the turn answers to them, and what the scan
	// said of the end stands for the user's first key, no clear of its
	// own to take it away.
	scanBurn(t, path, clA(ts, "Heading off."), clU(ts, "okay, go on", false))
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "done")

	// The user's key: what the scan said goes, and stays gone while
	// they have read.
	p.input(nil)
	claimRecord(t, p, "")
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")
	claimState(t, p, proto.Idle, false, "Wrapped up")

	// A new end of an unseen pane is its own news, again.
	scanBurn(t, path, clA(ts, "All tests pass"), clS(ts, "turn_duration"))
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")
	p.mu.Lock()
	p.info.Unread = true
	p.mu.Unlock()
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Idle, true, "All tests pass")
}

// A story of Codex's, where there is nothing of the screen's or the
// hooks' to say of a permission: the wait told from a call that runs
// long with no answer, risen to work when its answer came, taken back
// when the turn was stopped; a hook of the agent's mutes it all, and
// when the hook goes, the wait runs again.
func TestScanClaimsAWaitOfCodex(t *testing.T) {
	ts := scanBase.Format(time.RFC3339Nano)
	p, path := claimPane(t, "codex")
	scanFakeProc(p)
	late := scanBase.Add(45 * time.Second)

	// A turn begun, its call old with no result: a wait on the user.
	scanBurn(t, path, cxE(ts, "task_started", ""), cxI(scanOldAt(), "function_call", "c1", "shell"))
	runScanIs(t, p, scanBase, true)
	claimState(t, p, proto.Waiting, true, "Waiting on shell")
	claimRecord(t, p, "blocked")

	// Said, and it stands: no pass says it again.
	runScanIs(t, p, scanBase, false)

	// The call answered, no question open: the scan rises to work
	// under its own word.
	scanBurn(t, path, cxE(ts, "task_started", ""), cxI(scanOldAt(), "function_call", "c1", "shell"),
		cxI(ts, "function_call_output", "c1", ""))
	runScanIs(t, p, late, true)
	claimState(t, p, proto.Working, true, "")
	claimRecord(t, p, "working")

	// The turn stopped: nothing the scan said stands of it, though the
	// pane's mark stays what the wait set.
	scanBurn(t, path, cxE(ts, "task_started", ""), cxE(ts, "turn_aborted", ""))
	runScanIs(t, p, late, false)
	claimRecord(t, p, "")
	claimState(t, p, proto.Idle, true, "")

	// A hook of the agent's speaks, and the scan keeps out of the
	// pane's status, whatever its transcript changed to.
	p.mu.Lock()
	p.applySoft(proto.Record{ID: "hook-x", State: "working", App: "codex"})
	p.mu.Unlock()
	scanBurn(t, path, cxE(ts, "task_started", ""), cxI(scanOldAt(), "function_call", "c1", "shell"))
	runScanIs(t, p, scanBase, false)
	claimRecord(t, p, "")

	// And when the hook goes, the wait is the scan's to say again.
	p.mu.Lock()
	p.applySoft(proto.Record{ID: "hook-x", State: "clear"})
	p.mu.Unlock()
	runScanIs(t, p, late, true)
	claimState(t, p, proto.Waiting, true, "Waiting on shell")
	claimRecord(t, p, "blocked")
}
