package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kero/internal/proto"
)

// hookDaemon is a daemon of panes running agents, by pane: the agent and
// the session its hook told, "" for none yet.
func hookDaemon(t *testing.T, panes map[string][2]string) *Daemon {
	t.Helper()
	t.Setenv("KERO_CONFIG", filepath.Join(t.TempDir(), "config"))
	d := &Daemon{changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1), panes: map[string]*pane{}}
	d.viewed.Store(new(string))
	for id, a := range panes {
		info := proto.Pane{ID: id, Agent: a[0], Status: proto.Idle, Cols: 80, Rows: 24}
		if a[1] != "" {
			info.AgentSession = &proto.AgentSession{Agent: a[0], ID: a[1]}
		}
		p, err := newPane(d, "s", info)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.term.Close() })
		d.panes[id] = p
	}
	return d
}

func (s *pane) statusNow() proto.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info.Status
}

// A hook's report goes to the pane whose agent's session it is, and to
// none when no pane runs that session: a `claude -p` an agent runs in its
// pane is a session of its own, whose turns are not the pane's.
func TestHookReportFindsItsPane(t *testing.T) {
	d := hookDaemon(t, map[string][2]string{
		"a": {"claude", "s-a"}, "b": {"claude", "s-b"}, "fresh": {"claude", ""},
		"x": {"codex", "t-x"}, "y": {"codex", "t-y"},
	})
	report := func(pane, agent, session, state string) error {
		raw, _ := json.Marshal(proto.ReportParams{Pane: pane, Agent: agent, Session: session, State: state})
		_, err := d.call(nil, "report", raw)
		return err
	}
	if err := report("a", "claude", "s-a", "working"); err != nil {
		t.Fatal(err)
	}
	if st := d.panes["a"].statusNow(); st != proto.Working {
		t.Fatalf("its pane: %s", st)
	}
	if st := d.panes["b"].statusNow(); st != proto.Idle {
		t.Fatalf("another pane: %s", st)
	}
	// A nested claude -p in pane b, of a session of its own.
	if err := report("b", "claude", "nested", "working"); err == nil {
		t.Fatal("a session no pane runs reported")
	}
	if err := report("fresh", "claude", "s-new", "working"); err == nil {
		t.Fatal("a pane that told no session took a report")
	}
	if st := d.panes["b"].statusNow() + d.panes["fresh"].statusNow(); st != proto.Idle+proto.Idle {
		t.Fatalf("the nested session's report changed a pane: %s", st)
	}
	// Codex's app server, which panes share, runs pane y's hooks with
	// pane x's environment.
	if err := report("x", "codex", "t-y", "blocked"); err != nil {
		t.Fatal(err)
	}
	if x, y := d.panes["x"].statusNow(), d.panes["y"].statusNow(); x != proto.Idle || y != proto.Waiting {
		t.Fatalf("x %s, y %s", x, y)
	}
	if err := report("a", "codex", "s-a", "working"); err == nil {
		t.Fatal("another agent's report of the session went in")
	}
	if err := report("a", "claude", "s-a", "nonsense"); err == nil {
		t.Fatal("an unknown state went in")
	}
	if err := report("a", "claude", "", "working"); err == nil {
		t.Fatal("no session went in")
	}
}

func hookPane(t *testing.T) *pane {
	t.Helper()
	s := testPane(t)
	s.info.Agent, s.info.Title = "claude", "✳ Claude Code"
	s.info.AgentSession = &proto.AgentSession{Agent: "claude", ID: "s"}
	return s
}

func (s *pane) soften(state, kind, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applySoft(proto.Record{ID: "claude", App: "claude", State: state, Kind: kind, Msg: msg, Progress: -1, Updated: time.Now().UnixMilli()})
}

func (s *pane) recordStates() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.info.Records {
		out = append(out, r.ID+"="+r.State)
	}
	return strings.Join(out, ",")
}

// Esc ends Claude Code's turn without a Stop: the hook's working holds as
// the prompt shows a moment, then the screen's prompt settles the pane
// idle, and what the hook said goes.
func TestHookWorkingSettlesOnEsc(t *testing.T) {
	for _, sawSpinner := range []bool{true, false} {
		s := hookPane(t)
		now := time.Now()
		idle := claudeScreen(t, "idle")
		s.show(idle)
		if st := s.status(now); st != proto.Idle {
			t.Fatalf("at the prompt: %s", st)
		}
		s.soften("working", "", "")
		if st := s.statusNow(); st != proto.Working {
			t.Fatalf("prompt submitted: %s", st)
		}
		s.mu.Lock()
		declared := s.declared()
		s.mu.Unlock()
		if declared {
			t.Fatal("a hook's working silenced the screen")
		}
		// The prompt shows a moment before the spinner: still working.
		if st := s.status(now); st != proto.Working {
			t.Fatalf("before the spinner: %s", st)
		}
		if sawSpinner {
			now = now.Add(400 * time.Millisecond)
			s.show(claudeScreen(t, "working"))
			if st := s.status(now); st != proto.Working {
				t.Fatalf("in the turn: %s", st)
			}
			// Esc: the prompt back, and no Stop.
			s.show(idle)
			if st := s.status(now); st != proto.Working {
				t.Fatalf("Esc, at once: %s", st)
			}
		}
		if st := s.status(now.Add(idleHold)); st != proto.Idle {
			t.Fatalf("Esc (spinner seen %v), after %s: %s", sawSpinner, idleHold, st)
		}
		if rs := s.recordStates(); rs != "" {
			t.Fatalf("records after Esc: %s", rs)
		}
		// Another program's report won't bring the turn back.
		s.output([]byte("\x1b]7501;state=done:id=build\x1b\\"))
		if st := s.statusNow(); st != proto.Idle {
			t.Fatalf("after a done of another: %s", st)
		}
	}
}

// A permission prompt waits from the hook's word; once granted, the
// screen's work ends the wait, before the tool's PostToolUse says so;
// Stop finishes the turn.
func TestHookPermissionThenDone(t *testing.T) {
	s := hookPane(t)
	now := time.Now()
	s.show(claudeScreen(t, "idle"))
	s.status(now)
	s.soften("working", "", "")
	s.show(claudeScreen(t, "working"))
	s.status(now)
	s.soften("blocked", "permission", "Claude needs your permission to use Bash")
	s.mu.Lock()
	st, msg, kind := s.info.Status, s.info.Message, s.info.Kind
	s.mu.Unlock()
	if st != proto.Waiting || msg != "Claude needs your permission to use Bash" || kind != "permission" {
		t.Fatalf("asked: %s %q %q", st, msg, kind)
	}
	s.show(claudeScreen(t, "blocked"))
	if st := s.status(now); st != proto.Waiting {
		t.Fatalf("the dialog: %s", st)
	}
	// Typing answers it: the dialog stands until it goes.
	s.input([]byte("1"))
	if st := s.statusNow(); st != proto.Waiting {
		t.Fatalf("answering: %s", st)
	}
	s.show(claudeScreen(t, "working"))
	if st := s.status(now); st != proto.Working {
		t.Fatalf("granted: %s", st)
	}
	s.mu.Lock()
	kind = s.info.Kind
	s.mu.Unlock()
	if kind != "" {
		t.Fatalf("granted, still a wait of kind %q", kind)
	}
	if rs := s.recordStates(); strings.Contains(rs, "blocked") {
		t.Fatalf("a stale wait: %s", rs)
	}
	s.soften("working", "", "") // PostToolUse
	s.soften("done", "", "")    // Stop
	s.mu.Lock()
	st, unread := s.info.Status, s.info.Unread
	s.mu.Unlock()
	if st != proto.Idle || !unread {
		t.Fatalf("done: %s unread=%v", st, unread)
	}
	s.show(claudeScreen(t, "idle"))
	if st := s.status(now.Add(idleHold)); st != proto.Idle {
		t.Fatalf("at the prompt after: %s", st)
	}
	if rs := s.recordStates(); rs != "claude=done" {
		t.Fatalf("records: %s", rs)
	}
	// The user types: done is dismissed.
	s.input([]byte("x"))
	if rs := s.recordStates(); rs != "" {
		t.Fatalf("records after typing: %s", rs)
	}
}

// A hook's records stand beside what programs report, as docs/status.md
// says: blocked over working over error over done; and SessionEnd clears
// only the hook's.
func TestHookReportPrecedence(t *testing.T) {
	s := hookPane(t)
	s.soften("working", "", "")
	s.output([]byte("\x1b]7501;state=blocked:id=deploy:kind=permission\x1b\\"))
	if st := s.statusNow(); st != proto.Waiting {
		t.Fatalf("a program's blocked beside the hook's working: %s", st)
	}
	s.output([]byte("\x1b]7501;state=done:id=deploy\x1b\\"))
	if st := s.statusNow(); st != proto.Working {
		t.Fatalf("the hook's working beside a done: %s", st)
	}
	s.soften("error", "", "API Error")
	if st := s.statusNow(); st != proto.Failed {
		t.Fatalf("the hook's error beside a done: %s", st)
	}
	s.output([]byte("\x1b]7501;state=working:id=deploy\x1b\\"))
	if st := s.statusNow(); st != proto.Working {
		t.Fatalf("a program's working beside an error: %s", st)
	}
	s.mu.Lock()
	declared := s.declared()
	s.mu.Unlock()
	if !declared {
		t.Fatal("a program's own working no longer stands")
	}
	s.soften("clear", "", "") // SessionEnd
	if rs := s.recordStates(); rs != "deploy=working" {
		t.Fatalf("SessionEnd cleared: %s", rs)
	}
	if st := s.statusNow(); st != proto.Working {
		t.Fatalf("after SessionEnd: %s", st)
	}
}

// Claude Code reports its own status (OSC 7501, its root record) as
// well: what it says after its hooks stands, so a granted permission is
// work again, and Esc's idle ends the turn, though the screen goes
// unread while it declares work.
func TestAgentsOwnReportSupersedesHooks(t *testing.T) {
	s := hookPane(t)
	own := func(state string) { s.output([]byte("\x1b]7501;state=" + state + ":app=claude-code\x1b\\")) }
	own("working")
	s.soften("working", "", "")
	own("blocked")
	s.soften("blocked", "permission", "Claude needs your permission to use Bash")
	if st := s.statusNow(); st != proto.Waiting {
		t.Fatalf("asked: %s", st)
	}
	own("working") // granted; its tool runs long before PostToolUse
	if st := s.statusNow(); st != proto.Working {
		t.Fatalf("granted: %s", st)
	}
	if rs := s.recordStates(); rs != "=working" {
		t.Fatalf("records: %s", rs)
	}
	s.soften("working", "", "") // PostToolUse
	own("idle")                 // Esc: no Stop
	if st := s.statusNow(); st != proto.Idle {
		t.Fatalf("after Esc: %s", st)
	}
	if rs := s.recordStates(); rs != "=idle" {
		t.Fatalf("records after Esc: %s", rs)
	}
}

// A hook's working cleared alone, at SessionEnd, leaves no work behind.
func TestHookClearEndsWork(t *testing.T) {
	s := hookPane(t)
	s.soften("working", "", "")
	s.soften("clear", "", "")
	if st := s.statusNow(); st != proto.Idle {
		t.Fatalf("after SessionEnd: %s", st)
	}
}

// Text from a hook shows on one line, without controls, and cut short.
func TestPlainText(t *testing.T) {
	for in, want := range map[string]string{
		"Claude needs\nyour permission\t": "Claude needs your permission",
		"\x1b[31mred\x07":                 "[31mred",
		"a‮bc":                            "abc",
		strings.Repeat("é", 1500):         strings.Repeat("é", 1024),
	} {
		if got := plainText(in, 2048); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
