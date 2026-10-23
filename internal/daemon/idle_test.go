package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"kero/internal/proto"
)

// countReads counts the reads of the kernel a pane's tick makes, with
// name as every program's name.
func countReads(t *testing.T, name string) (names, args, cwds *atomic.Int64) {
	names, args, cwds = new(atomic.Int64), new(atomic.Int64), new(atomic.Int64)
	rn, ra, rc := readName, readArgs, readCwd
	readName = func(int) string { names.Add(1); return name }
	readArgs = func(int) []string { args.Add(1); return []string{name, "/x/codex.js"} }
	readCwd = func(int) string { cwds.Add(1); return "/tmp" }
	t.Cleanup(func() { readName, readArgs, readCwd = rn, ra, rc })
	return
}

// A foreground program is read as its group appears and while it settles,
// then seldom: a minute of ticks reads its name a few times, not 150, and
// its arguments once, and never for a program whose name is an agent's.
func TestForegroundReadsSettle(t *testing.T) {
	for _, c := range []struct {
		name     string
		maxNames int64
		args     int64
	}{
		{"node", 12, 1},   // a runtime: its arguments tell the agent
		{"claude", 12, 0}, // an agent by its name alone
		{"vim", 12, 1},
	} {
		names, args, _ := countReads(t, c.name)
		s := &pane{}
		start := time.Now()
		for i := range 150 { // a minute of 400 ms ticks
			if got, _ := s.foreground(42, start.Add(time.Duration(i)*400*time.Millisecond)); got != c.name {
				t.Fatalf("%s: read %q", c.name, got)
			}
		}
		if names.Load() > c.maxNames || args.Load() != c.args {
			t.Errorf("%s: a minute read the name %d times, the arguments %d; want at most %d, %d", c.name, names.Load(), args.Load(), c.maxNames, c.args)
		}
		t.Logf("%s: a minute of ticks reads the name %d times, the arguments %d (was 150 and 150)", c.name, names.Load(), args.Load())
		// A new group is read at once.
		names.Store(0)
		s.foreground(43, start.Add(time.Minute))
		if names.Load() != 1 {
			t.Errorf("%s: a new group was not read", c.name)
		}
	}
	// A program that execs another, as env does, is followed while the
	// group settles, and later at the recheck.
	s := &pane{}
	countReads(t, "env")
	now := time.Now()
	s.foreground(7, now)
	countReads(t, "node")
	if got, args := s.foreground(7, now.Add(400*time.Millisecond)); got != "node" || len(args) == 0 {
		t.Fatalf("the exec was missed: %q %v", got, args)
	}
	countReads(t, "python")
	if got, _ := s.foreground(7, now.Add(5*time.Second)); got != "node" {
		t.Fatalf("read again before the recheck: %q", got)
	}
	if got, _ := s.foreground(7, now.Add(11*time.Second)); got != "python" {
		t.Fatalf("the recheck missed the exec: %q", got)
	}
}

// The directory is read every 1.5 s for the pane the user is in, and
// every 10 s for the others; the pane the user is in is the focus of the
// session the app shows.
func TestFocusedPane(t *testing.T) {
	if cwdEvery(true) != 1500*time.Millisecond || cwdEvery(false) != 10*time.Second {
		t.Fatal("cwdEvery")
	}
	d := linearTestDaemon(t)
	b := &pane{info: proto.Pane{ID: "b"}}
	d.sessions = append(d.sessions, &session{id: "s2", panes: []*pane{b}, focus: "b"})
	d.viewed.Store(new(string))
	if d.focusedPaneLocked() != nil {
		t.Fatal("a focused pane with no session shown")
	}
	id := "s2"
	d.viewed.Store(&id)
	if d.focusedPaneLocked() != b {
		t.Fatal("not the focus of the session shown")
	}
}

// The state is saved once per wait however many changes come, and once
// more after the last: a storm of status changes writes state.json once
// every 2 s, where it wrote every 300 ms.
func TestStateSaveDebounce(t *testing.T) {
	d := linearTestDaemon(t)
	d.quit = make(chan struct{})
	defer close(d.quit)
	var saves atomic.Int64
	const wait = 100 * time.Millisecond
	go d.saveLoopWith(wait, time.Hour, func() { saves.Add(1) }, func() {})
	storm := time.Now()
	for time.Since(storm) < time.Second {
		d.changed()
		time.Sleep(2 * time.Millisecond)
	}
	during := saves.Load()
	time.Sleep(3 * wait)
	after := saves.Load()
	// 1 s of changes: at most one save per wait.
	if during > int64(time.Second/wait)+1 || during < 3 {
		t.Fatalf("%d saves in a 1 s storm with a wait of %v", during, wait)
	}
	if after != during+1 && after != during {
		t.Fatalf("%d saves after the storm, %d during", after, during)
	}
	if after == 0 {
		t.Fatal("the last change was never saved")
	}
	t.Logf("a 1 s storm saved %d times at a wait of %v", during, wait)
}

// A branch switched is seen at the next check of its HEAD file, which
// costs a stat; nothing moved reads no repository until branchEvery.
func TestBranchTickFollowsHead(t *testing.T) {
	d := linearTestDaemon(t)
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	head := filepath.Join(repo, ".git", "HEAD")
	os.WriteFile(head, []byte("ref: refs/heads/main\n"), 0o644)
	p := d.panes["p"]
	p.info.Agent, p.info.Dir, p.info.AgentSession = "codex", repo, nil
	now := time.Now()
	d.branchTick(now)
	if p.info.TaskID != "" || len(d.branch.heads) != 1 {
		t.Fatalf("task %q heads %v", p.info.TaskID, d.branch.heads)
	}
	read := d.branch.at
	d.branchTick(now.Add(1600 * time.Millisecond))
	if d.branch.at != read {
		t.Fatal("read again with nothing moved")
	}
	// git switch writes HEAD.lock and renames it over HEAD.
	lock := head + ".lock"
	os.WriteFile(lock, []byte("ref: refs/heads/dv/bru-25-task\n"), 0o644)
	later := time.Now().Add(time.Second)
	os.Chtimes(lock, later, later)
	os.Rename(lock, head)
	d.branchTick(now.Add(3200 * time.Millisecond))
	if p.info.TaskID != "uuid" {
		t.Fatal("the switch to the issue's branch was not seen")
	}
	// Due, as when the user focuses a pane: read at once.
	d.branch.due.Store(true)
	d.branchTick(now.Add(4800 * time.Millisecond))
	if d.branch.at.Equal(read) || d.branch.due.Load() {
		t.Fatal("a due read did not happen")
	}
	// And every branchEvery regardless.
	at := d.branch.at
	d.branchTick(at.Add(branchEvery))
	if d.branch.at == at {
		t.Fatal("no read after branchEvery")
	}
}

// A branch switched by another process is noticed even after every pane
// went quiet. The expensive pane tick may sleep for ten seconds.
func TestQuietBranchSwitch(t *testing.T) {
	d := linearTestDaemon(t)
	d.quit, d.tickWake = make(chan struct{}), make(chan struct{}, 1)
	d.viewed.Store(new(string))
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(repo, ".git", "HEAD")
	if err := os.WriteFile(head, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := d.panes["p"]
	p.info.Agent, p.info.Dir, p.info.AgentSession = "codex", repo, nil
	d.branchTick(time.Now())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.tickLoopWith(tickPace{5 * time.Millisecond, time.Hour, 20 * time.Millisecond})
	}()
	t.Cleanup(func() { d.stop(); <-done })
	deadline := time.Now().Add(time.Second)
	for !d.tickSlow.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !d.tickSlow.Load() {
		t.Fatal("pane tick did not become quiet")
	}
	if err := os.WriteFile(head+".lock", []byte("ref: refs/heads/dv/bru-25-task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(head+".lock", head); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		linked := p.info.TaskID == "uuid"
		p.mu.Unlock()
		if linked {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a branch switched outside a quiet pane was not noticed within two seconds")
}

// The tick goes slow once nothing has happened for a while, and fast
// again as soon as something does (BRU-336).
func TestTickPace(t *testing.T) {
	pace := tickPace{tickFast, tickSlow, tickQuiet}
	now := time.Now()
	if got := pace.wait(now, now); got != tickFast {
		t.Fatalf("just stirred: %v", got)
	}
	if got := pace.wait(now, now.Add(-tickQuiet+time.Millisecond)); got != tickFast {
		t.Fatalf("within the quiet: %v", got)
	}
	if got := pace.wait(now, now.Add(-tickQuiet)); got != tickSlow {
		t.Fatalf("quiet: %v", got)
	}
	// A quiet pane is read no less often than one the user is not in
	// was read already.
	if tickSlow > cwdEvery(false) || tickSlow > fgRecheck {
		t.Fatalf("tickSlow %v reads quiet panes less often than before", tickSlow)
	}
}

// A tick loop at its slow pace ticks at once when something happens in a
// pane, and goes slow again once nothing has for a while; one with no
// session and no client keeps the fast pace, to exit on time.
func TestTickLoopStir(t *testing.T) {
	d := &Daemon{log: log.New(io.Discard, "", 0), panes: map[string]*pane{}, changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1),
		tickWake: make(chan struct{}, 1), quit: make(chan struct{})}
	d.viewed.Store(new(string))
	d.sessions = []*session{{id: "s"}}
	defer d.stop()
	ticked := func() time.Time {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.idleAt
	}
	const fast, quiet = 5 * time.Millisecond, 50 * time.Millisecond
	go d.tickLoopWith(tickPace{fast, time.Hour, quiet})
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}
	waitFor("the loop never went slow", d.tickSlow.Load)
	slowAt := ticked()
	time.Sleep(5 * fast)
	if ticked() != slowAt {
		t.Fatal("ticked at its slow pace")
	}
	d.stir()
	waitFor("a stir did not wake the slow loop", func() bool { return ticked().After(slowAt) })
	if d.tickSlow.Load() {
		t.Fatal("slow right after a stir")
	}
	waitFor("the loop did not go slow again", d.tickSlow.Load)
}

// The histories go to disk after their text changed, and not while nothing
// changes: a kerod at rest writes nothing, and wakes for nothing.
func TestHistorySaveOnChange(t *testing.T) {
	d := linearTestDaemon(t)
	d.quit = make(chan struct{})
	d.histCh = make(chan struct{}, 1)
	defer close(d.quit)
	var snaps atomic.Int64
	const history = 20 * time.Millisecond
	go d.saveLoopWith(time.Hour, history, func() {}, func() { snaps.Add(1) })
	// Once at the start, for the histories restored.
	time.Sleep(3 * history)
	if n := snaps.Load(); n != 1 {
		t.Fatalf("%d snapshots at the start, want 1", n)
	}
	time.Sleep(5 * history)
	if n := snaps.Load(); n != 1 {
		t.Fatalf("%d snapshots with nothing changed, want 1", n)
	}
	// A stream of output asks once, and wakes the loop once.
	for range 100 {
		d.historySoon()
	}
	if !d.histArmed.Load() {
		t.Fatal("the stream did not ask")
	}
	time.Sleep(10 * history)
	if n := snaps.Load(); n != 2 {
		t.Fatalf("%d snapshots after a change, want 2", n)
	}
	if d.histArmed.Load() {
		t.Fatal("still armed after the save")
	}
}
