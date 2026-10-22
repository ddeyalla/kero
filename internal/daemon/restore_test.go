package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"

	"kero/internal/proto"
)

// restoreAll restores what the last daemon left, and waits for its sessions:
// Run does that in the background (restoreInBackground), while the socket
// already takes clients.
func restoreAll(t *testing.T, d *Daemon) {
	t.Helper()
	p := d.restore()
	if p == nil {
		return
	}
	go d.restoreInBackground(p)
	within(t, "the restore did not finish", func() { <-p.done })
}

// within waits for what a test needs, and fails after a while of no sign of
// it.
func within(t *testing.T, what string, wait func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(what)
	}
}

// savedViewSessions are sessions of one view pane each, s1 with p1 and so
// on: nothing of them runs, and each comes back as it was.
func savedViewSessions(n int) []savedSession {
	out := make([]savedSession, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, savedSession{ID: fmt.Sprintf("s%d", i), Created: 1, Panes: []savedPane{{
			ID: fmt.Sprintf("p%d", i), View: proto.PaneFiles, Dir: os.TempDir(), Created: 1, Cols: 80, Rows: 24,
		}}})
	}
	return out
}

// writeRestoreState writes the sessions as the state.json of a daemon that
// ended with them open.
func writeRestoreState(t *testing.T, d *Daemon, sessions []savedSession) {
	t.Helper()
	data, err := json.Marshal(savedState{Version: stateVersion, Sessions: sessions, Saved: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.statePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// restoreGate holds the panes of a restore until the test lets each go, and
// tells when one is held (restoringPane).
type restoreGate struct {
	held    map[string]chan struct{} // closed as the pane's restore starts
	release map[string]chan struct{} // closed to let the pane go
}

func gateOn(panes ...string) *restoreGate {
	g := &restoreGate{held: map[string]chan struct{}{}, release: map[string]chan struct{}{}}
	for _, id := range panes {
		g.held[id], g.release[id] = make(chan struct{}), make(chan struct{})
	}
	return g
}

// hold makes every pane of the restore wait for its release.
func (g *restoreGate) hold(t *testing.T) {
	t.Helper()
	restoringPane = func(sp savedPane) error {
		close(g.held[sp.ID])
		<-g.release[sp.ID]
		return nil
	}
	t.Cleanup(func() { restoringPane = nil })
}

// wait tells that the pane's restore is held.
func (g *restoreGate) wait(t *testing.T, pane string) {
	t.Helper()
	within(t, "the restore of "+pane+" did not start", func() { <-g.held[pane] })
}

// let lets the pane's restore go on.
func (g *restoreGate) let(pane string) { close(g.release[pane]) }

// stateOverTheSocket asks the daemon for its state as the app does when it
// starts, over a control connection.
func stateOverTheSocket(t *testing.T, path string) proto.State {
	t.Helper()
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	hello, err := json.Marshal(proto.Hello{Kind: "control", Version: proto.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(hello, '\n')); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("{\"id\":1,\"method\":\"state\"}\n")); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatalf("no answer to state: %v", err)
		}
		var m struct {
			ID     int64       `json:"id"`
			Result proto.State `json:"result"`
			Error  string      `json:"error"`
		}
		if json.Unmarshal(line, &m) != nil || m.ID != 1 {
			continue // the welcome
		}
		if m.Error != "" {
			t.Fatalf("state: %s", m.Error)
		}
		return m.Result
	}
}

// shown lets the client read events until one shows exactly the sessions
// wanted, in that order: a session comes once, and where the state file had
// it.
func shown(t *testing.T, c *controlConn, want ...string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		select {
		case m := <-c.out:
			if m.Event != "state" || m.State == nil {
				continue // the workspace has an event of its own
			}
			var ids []string
			for _, s := range m.State.Sessions {
				ids = append(ids, s.ID)
			}
			if !slices.Equal(ids, want) {
				t.Fatalf("the app was shown %v, want %v", ids, want)
			}
			return
		default:
			if time.Now().After(deadline) {
				t.Fatalf("no state showed %v", want)
			}
			time.Sleep(time.Millisecond)
		}
	}
}

// waitSessions waits until the daemon holds exactly the sessions wanted, in
// that order.
func waitSessions(t *testing.T, d *Daemon, want ...string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		var ids []string
		for _, s := range d.state().Sessions {
			ids = append(ids, s.ID)
		}
		if slices.Equal(ids, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sessions back are %v, want %v", ids, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// kerod listens before it restores, so that the app reaches it at once: a
// control connection gets the state while the last daemon's sessions are
// still coming back (BRU-229).
func TestSocketAnswersDuringRestore(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	d.viewed.Store(new(string))
	writeRestoreState(t, d, savedViewSessions(1))

	path := testSocketPath(t)
	ln, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()

	g := gateOn("p1")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	g.wait(t, "p1")
	if st := stateOverTheSocket(t, path); len(st.Sessions) != 0 {
		t.Fatalf("the socket showed %d sessions while the restore was held", len(st.Sessions))
	}

	g.let("p1")
	within(t, "the restore did not finish", func() { <-plan.done })
	if st := d.state(); len(st.Sessions) != 1 || st.Sessions[0].ID != "s1" {
		t.Fatalf("after the restore the state has %+v, want s1", st.Sessions)
	}
}

// Sessions keep the order the state file had whatever order they come back
// in, and each shows once: the app watches them arrive, as it had them.
func TestRestoreKeepsSavedOrder(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	d.viewed.Store(new(string))
	writeRestoreState(t, d, savedViewSessions(3))
	c := &controlConn{out: make(chan proto.Message, 64), d: d}
	d.subs = map[*controlConn]struct{}{c: {}}
	go d.broadcastLoop()

	g := gateOn("p1", "p2", "p3")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	for _, p := range []string{"p1", "p2", "p3"} {
		g.wait(t, p)
	}

	// The last of them comes back first, and the others take their place
	// before it.
	g.let("p3")
	shown(t, c, "s3")
	g.let("p1")
	shown(t, c, "s1", "s3")
	g.let("p2")
	shown(t, c, "s1", "s2", "s3")

	within(t, "the restore did not finish", func() { <-plan.done })
	if st := d.state(); len(st.Sessions) != 3 {
		t.Fatalf("after the restore the state has %+v", st.Sessions)
	}
}

// Nothing is saved while the sessions come back: a state written then would
// drop those not back yet, and a crash after it would lose them. A history
// of a pane not back yet is not written either.
func TestRestoreSavesNothingUntilItEnds(t *testing.T) {
	d := linearTestDaemon(t)
	d.viewed.Store(new(string))
	d.sessions, d.panes = nil, map[string]*pane{}
	writeRestoreState(t, d, savedViewSessions(2))
	before, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d.dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	const history = "what the pane showed"
	if err := os.WriteFile(d.historyPath("p2"), []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}

	g := gateOn("p1", "p2")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	g.wait(t, "p1")
	g.wait(t, "p2")

	// A save asked for while they come, as the save loop asks, and the
	// histories as the end of a daemon saves them.
	d.saveState()
	d.saveHistories(true)
	if now, err := os.ReadFile(d.statePath()); err != nil || string(now) != string(before) {
		t.Fatalf("a state was saved during the restore (%v):\n%s", err, now)
	}
	if h, err := os.ReadFile(d.historyPath("p2")); err != nil || string(h) != history {
		t.Fatalf("the history of a pane not back was written (%v): %q", err, h)
	}

	g.let("p1")
	waitSessions(t, d, "s1")
	<-d.saveCh // publish save was suppressed during restore
	g.let("p2")
	waitSessions(t, d, "s1", "s2")
	within(t, "the restore did not finish", func() { <-plan.done })
	within(t, "restore did not request a final save", func() { <-d.saveCh })
	d.saveState()
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var st savedState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Sessions) != 2 || st.Sessions[0].ID != "s1" || st.Sessions[1].ID != "s2" {
		t.Fatalf("the state saved after the restore has %+v", st.Sessions)
	}
}

// A pane that does not come back is logged and skipped: its session comes
// back with the panes that did, and the sessions after it are not held up.
func TestRestoreSkipsAFailedPane(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	sessions := savedViewSessions(2)
	sessions[0].Panes = append(sessions[0].Panes, savedPane{ID: "p1b", View: proto.PaneFiles, Created: 1, Cols: 80, Rows: 24})
	writeRestoreState(t, d, sessions)

	restoringPane = func(sp savedPane) error {
		if sp.ID == "p1b" {
			return errors.New("no shell for it")
		}
		return nil
	}
	t.Cleanup(func() { restoringPane = nil })

	plan := d.restore()
	go d.restoreInBackground(plan)
	within(t, "the restore did not finish", func() { <-plan.done })

	var ids []string
	for _, s := range d.sessions {
		ids = append(ids, s.id)
	}
	if !slices.Equal(ids, []string{"s1", "s2"}) {
		t.Fatalf("the sessions back are %v, want s1 and s2", ids)
	}
	if got := d.sessions[0].panes; len(got) != 1 || got[0].info.ID != "p1" {
		t.Fatalf("s1 came back with %+v, want p1 alone", got)
	}
	if d.panes["p1b"] != nil {
		t.Fatal("the pane that failed is in the daemon's")
	}
}

// Failed sessions do not count toward the insertion position. More sessions
// than workers ensure a failure finishes before the surviving session starts.
func TestRestoreSkipsASessionNothingCameBack(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	d.viewed.Store(new(string))
	writeRestoreState(t, d, savedViewSessions(5))

	restoringPane = func(sp savedPane) error {
		if sp.ID != "p5" {
			return errors.New("nothing came back")
		}
		return nil
	}
	t.Cleanup(func() { restoringPane = nil })

	plan := d.restore()
	go d.restoreInBackground(plan)
	within(t, "the restore did not finish", func() { <-plan.done })
	waitSessions(t, d, "s5")
}

// A session the user closes while the others come back is not counted when
// the next one takes its place: it goes after the one before it that is
// still there.
func TestRestoreInsertsAfterAClosedSession(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	d.viewed.Store(new(string))
	writeRestoreState(t, d, savedViewSessions(3))

	g := gateOn("p1", "p2", "p3")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	for _, p := range []string{"p1", "p2", "p3"} {
		g.wait(t, p)
	}

	g.let("p1")
	waitSessions(t, d, "s1")
	if err := d.removeSession("s1"); err != nil {
		t.Fatal(err)
	}
	g.let("p2")
	waitSessions(t, d, "s2")
	g.let("p3")
	within(t, "the restore did not finish", func() { <-plan.done })
	waitSessions(t, d, "s2", "s3")
}

// A daemon with no session yet does not go while the last daemon's sessions
// are still coming: the idle exit waits for the restore to end.
func TestIdleWaitsForTheRestore(t *testing.T) {
	idleDaemon := func() *Daemon {
		d := &Daemon{log: log.New(io.Discard, "", 0), panes: map[string]*pane{},
			changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1), tickWake: make(chan struct{}, 1), quit: make(chan struct{})}
		d.viewed.Store(new(string))
		// Idle for longer than the exit waits, as after a long restart.
		d.idleAt = time.Now().Add(-time.Minute)
		return d
	}
	// A daemon with no session and no restore to wait for goes at once.
	goes := idleDaemon()
	go goes.tickLoopWith(tickPace{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond})
	within(t, "a daemon idle with no restore did not go", func() { <-goes.quit })

	// One whose sessions are still coming stays.
	d := idleDaemon()
	d.restoreDone = make(chan struct{})
	go d.tickLoopWith(tickPace{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond})
	time.Sleep(100 * time.Millisecond)
	select {
	case <-d.quit:
		t.Fatal("exited with no session while the sessions were coming back")
	default:
	}
	// And once they are back it goes as it would have: the countdown to
	// the exit starts again as the restore ends.
	close(d.restoreDone)
	d.mu.Lock()
	d.idleAt = time.Now().Add(-time.Minute)
	d.mu.Unlock()
	within(t, "the daemon did not go once the restore ended", func() { <-d.quit })
}

// Clients may edit a published session while another worker still restores.
// Cleanup must use the restore's own results and leave new conversations open.
func TestRestoreTaskRunsWhileClientsEdit(t *testing.T) {
	d := linearTestDaemon(t)
	d.viewed.Store(new(string))
	d.sessions, d.panes = nil, map[string]*pane{}
	sessions := savedViewSessions(2)
	sessions[0].Panes[0].Agent = &proto.AgentSession{Agent: "codex", ID: "resumed"}
	sessions[0].Panes = append(sessions[0].Panes, savedPane{ID: "p1b", View: proto.PaneFiles}, savedPane{ID: "p1c", View: proto.PaneFiles, Agent: &proto.AgentSession{Agent: "codex", ID: "kept"}})
	writeRestoreState(t, d, sessions)
	d.taskStart("uuid", proto.TaskRun{ID: "resumed", Agent: "codex", Session: "s1", Pane: "p1"})
	d.taskStart("uuid", proto.TaskRun{ID: "stopped", Agent: "codex", Session: "s1", Pane: "p1b"})
	d.taskStart("uuid", proto.TaskRun{ID: "kept", Agent: "codex", Session: "s1", Pane: "p1c"})
	d.taskStart("uuid", proto.TaskRun{Session: "s2"})
	d.taskStart("uuid", proto.TaskRun{Session: "gone"})
	d.saveState()
	// saveState above has no sessions yet; put the restore fixture back.
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var st savedState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	st.Sessions = sessions
	data, err = json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.statePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	g := gateOn("p1", "p1b", "p1c", "p2")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	g.wait(t, "p1")
	g.wait(t, "p2")
	g.let("p1")
	g.wait(t, "p1b")
	// A session is invisible until every pane has finished.
	waitSessions(t, d)
	g.let("p1b")
	g.wait(t, "p1c")
	g.let("p1c")
	waitSessions(t, d, "s1")
	for _, r := range d.taskRuns("uuid", "", nil) {
		if r.Ended != 0 {
			t.Fatalf("cleanup ran before every worker finished: %+v", r)
		}
	}
	if err := d.removePane("p1"); err != nil {
		t.Fatal(err)
	}
	d.taskStart("uuid", proto.TaskRun{ID: "new", Agent: "codex", Session: "new", Pane: "np"})
	g.let("p2")
	within(t, "restore did not finish", func() { <-plan.done })
	for _, r := range d.taskRuns("uuid", "", nil) {
		wantOpen := r.ID == "new" || r.ID == "kept" || r.Pane == "" && r.Session == "s2"
		if (r.Ended == 0) != wantOpen {
			t.Errorf("wrong end state: %+v, want open %v", r, wantOpen)
		}
	}
}

func TestRestoreKeepsNewSessionsLast(t *testing.T) {
	d := linearTestDaemon(t)
	d.viewed.Store(new(string))
	d.sessions, d.panes = nil, map[string]*pane{}
	writeRestoreState(t, d, savedViewSessions(3))
	g := gateOn("p1", "p2", "p3")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	for _, id := range []string{"p1", "p2", "p3"} {
		g.wait(t, id)
	}
	early, err := d.create(proto.CreateParams{View: proto.PaneFiles})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.removeSession(early.ID) })
	g.let("p3")
	waitSessions(t, d, "s3", early.ID)
	g.let("p1")
	waitSessions(t, d, "s1", "s3", early.ID)
	late, err := d.create(proto.CreateParams{View: proto.PaneFiles, After: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.removeSession(late.ID) })
	waitSessions(t, d, "s1", "s3", early.ID, late.ID)
	g.let("p2")
	within(t, "restore did not finish", func() { <-plan.done })
	waitSessions(t, d, "s1", "s2", "s3", early.ID, late.ID)
}

func TestRestoreWorkersAreBounded(t *testing.T) {
	d := linearTestDaemon(t)
	d.viewed.Store(new(string))
	d.sessions, d.panes = nil, map[string]*pane{}
	writeRestoreState(t, d, savedViewSessions(5))
	g := gateOn("p1", "p2", "p3", "p4", "p5")
	g.hold(t)
	plan := d.restore()
	go d.restoreInBackground(plan)
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		g.wait(t, id)
	}
	select {
	case <-g.held["p5"]:
		t.Fatal("more than four workers")
	case <-time.After(50 * time.Millisecond):
	}
	g.let("p1")
	g.wait(t, "p5")
	for _, id := range []string{"p2", "p3", "p4", "p5"} {
		g.let(id)
	}
	within(t, "restore did not finish", func() { <-plan.done })
}

// Exercise Run itself, including its shutdown handler, in an isolated process.
func TestRunListensBeforeRestore(t *testing.T) {
	if os.Getenv("SHIBU_TEST_RESTORE_PROCESS") == "1" {
		restoringPane = func(savedPane) error {
			if err := os.WriteFile(filepath.Join(proto.Dir(), "held"), nil, 0600); err != nil {
				return err
			}
			select {} // The parent stops this daemon while restoration is held.
		}
		if err := Run(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("shutdown uses SIGTERM")
	}
	d := linearTestDaemon(t)
	writeRestoreState(t, d, savedViewSessions(1))
	before, err := os.ReadFile(d.statePath())
	if err != nil {
		t.Fatal(err)
	}
	socket := testSocketPath(t)
	t.Setenv("KERO_HOME", d.dir)
	t.Setenv("KERO_SOCKET", socket)
	t.Setenv("KERO_PANE", "")
	t.Setenv("KERO_SESSION", "")
	t.Setenv("KERO_PPROF", "")
	t.Setenv("SHIBU_TEST_RESTORE_PROCESS", "1")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunListensBeforeRestore$")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(d.dir, "held")); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("daemon exited: %v: %s", err, output.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("restore did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if st := stateOverTheSocket(t, socket); len(st.Sessions) != 0 {
		t.Fatal("held restore published a session")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon exited: %v: %s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
	after, err := os.ReadFile(d.statePath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("shutdown changed unrestored state: %v: %s", err, after)
	}
}

// The startup scan runs beside clients and terminal output after restoration.
func TestRestoreScanTakesPaneLock(t *testing.T) {
	d := linearTestDaemon(t)
	p := d.panes["p"]
	p.mu.Lock()
	done := make(chan struct{})
	go func() { d.scanPass(time.Now()); close(done) }()
	select {
	case <-done:
		p.mu.Unlock()
		t.Fatal("startup scan bypassed pane lock")
	case <-time.After(50 * time.Millisecond):
	}
	p.mu.Unlock()
	within(t, "scan did not finish", func() { <-done })
}

// Even a restore with no surviving session needs a save after its gate closes.
func TestRestoreRequestsFinalSave(t *testing.T) {
	d := linearTestDaemon(t)
	d.sessions, d.panes = nil, map[string]*pane{}
	writeRestoreState(t, d, nil)
	plan := d.restore()
	go d.restoreInBackground(plan)
	within(t, "restore did not finish", func() { <-plan.done })
	within(t, "restore did not request a save", func() { <-d.saveCh })
}
