package daemon

import (
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
	go d.saveLoopWith(wait, func() { saves.Add(1) })
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
