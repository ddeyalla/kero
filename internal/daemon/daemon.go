// Package daemon is kerod: the process that owns every session's
// pseudo-terminal, so that sessions outlive the app. The app, and the kero
// CLI, attach to it through a Unix socket; quitting the app detaches it,
// and opening it again attaches it where it left off.
package daemon

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"

	"kero/internal/clilink"
	"kero/internal/proto"
	"kero/internal/settings"
	"kero/internal/shell"
)

// Daemon is kerod's state.
type Daemon struct {
	dir     string
	exe     string
	started time.Time
	log     *log.Logger

	mu       sync.Mutex
	projects []proto.Project
	sessions []*session
	panes    map[string]*pane
	subs     map[*controlConn]struct{}
	conns    int
	idleAt   time.Time
	ending   bool
	// linear is the Linear workspace, as last read. Nil when there is
	// no key, or when it was never read.
	linear *proto.LinearState
	// linearSaved is the copy of linear last written to linear.json
	// (linear_store.go), under mu.
	linearSaved    *proto.LinearState
	linearMu       sync.Mutex // serialize polls and manual refreshes
	linearRetry    time.Time
	linearIdentity string
	// tasks are the conversations that worked on each task (tasks.go).
	taskMu sync.Mutex
	tasks  map[string]*taskLog

	// profiles are the saved work profiles, and activeProfile the id of
	// the one active, empty when none. restore loads them, saveState
	// saves them; applying one is the switch work of epic A.
	profiles      []savedProfile
	activeProfile string

	viewed atomic.Pointer[string]
	// branch follows the branches agents work on (branch.go).
	branch branchWatch
	// scanning holds that a transcript scan pass runs (scanPass).
	scanning atomic.Bool
	// theme is the colors of the client that last told them, which the
	// panes answer programs' questions about the colors with.
	theme    atomic.Pointer[terminal.Theme]
	changeCh chan struct{}
	saveCh   chan struct{}
	// histCh asks for the histories to go to disk (historySoon), once
	// until they do: histArmed tells that it asked.
	histCh    chan struct{}
	histArmed atomic.Bool
	// The tick loop's pace (tickLoop): stirred tells that something
	// happened in a pane since its last tick, and tickSlow that it waits
	// tickSlow for the next, which tickWake cuts short.
	stirred  atomic.Bool
	tickSlow atomic.Bool
	tickWake chan struct{}
	quit     chan struct{}
	quitOnce sync.Once

	// linearOut is the workspace last sent to the subscribers, and
	// linearKnown tells that it reached them all: a Linear event a client
	// was too far behind to take clears it (forgetLinear), and the next
	// broadcast sends the workspace again (sendLinear), under linearOutMu.
	linearOutMu sync.Mutex
	linearOut   *proto.LinearState
	linearKnown bool
	// dropped counts the events a client was too far behind to take, and
	// dropAt is when the log last said so (dropEvent).
	dropped atomic.Int64
	dropAt  atomic.Int64
	// restoreDone is closed when the sessions of the last daemon are back
	// (restoreInBackground), and nil when there is nothing to bring back:
	// nothing is saved before it closes (saveState), and the daemon is not
	// idle with none of the sessions (tickLoop).
	restoreDone chan struct{}
}

// Run runs the daemon until it is told to shut down, or has had no session
// and no client for a while.
func Run() error {
	dir := proto.Dir()
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "kerod.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	if st, _ := logf.Stat(); st != nil && st.Size() > 4<<20 {
		logf.Truncate(0)
	}
	lg := log.New(logf, "", log.LstdFlags|log.Lmicroseconds)

	// One daemon at a time: the lock is the daemon's while it runs.
	lock, err := os.OpenFile(filepath.Join(dir, "kerod.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return errors.New("kerod is already running")
	}

	sock := proto.SocketPath()
	ln, err := listenSocket(sock)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer serveProfiles(os.Getenv("KERO_PPROF"), lg)() // pprof.go

	exe, _ := os.Executable()
	d := &Daemon{
		dir: dir, exe: exe, started: time.Now(), log: lg,
		subs:     map[*controlConn]struct{}{},
		panes:    map[string]*pane{},
		changeCh: make(chan struct{}, 1),
		saveCh:   make(chan struct{}, 1),
		histCh:   make(chan struct{}, 1),
		tickWake: make(chan struct{}, 1),
		quit:     make(chan struct{}),
		idleAt:   time.Now(),
	}
	empty := ""
	d.viewed.Store(&empty)
	lg.Printf("kerod %d started (pid %d, %s)", proto.Version, os.Getpid(), exe)
	d.linkCLI()
	// What the last daemon left, its sessions to come back in the
	// background once the socket takes clients (restoreInBackground).
	plan := d.restore()

	signal.Ignore(syscall.SIGHUP, syscall.SIGPIPE)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	notifyStop(sigs)
	go func() {
		s := <-sigs
		lg.Printf("%v: saving and exiting", s)
		d.saveHistories(true)
		d.saveState()
		d.stop()
	}()

	go d.broadcastLoop()
	go d.saveLoop()
	go d.tickLoop()
	go d.linearLoop()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()
	go func() {
		d.restoreInBackground(plan)
		// What the transcripts of the panes say, once at the start: the
		// ends of turns that came while nothing watched.
		d.scanPass(time.Now())
	}()
	<-d.quit
	ln.Close()
	return nil
}

func (d *Daemon) stop() { d.quitOnce.Do(func() { close(d.quit) }) }

// linkCLI keeps bin/kero pointing at this executable (package clilink),
// which sessions have on their PATH: the kero CLI, for agents to drive
// other sessions.
func (d *Daemon) linkCLI() {
	if d.exe == "" {
		return
	}
	if err := clilink.Link(d.exe, filepath.Join(d.dir, "bin")); err != nil {
		d.log.Printf("linking the CLI: %v", err)
	}
}

func (d *Daemon) viewedID() string { return *d.viewed.Load() }

// changed asks for the state to go to the subscribers, and to disk.
func (d *Daemon) changed() {
	d.broadcast()
	d.save()
}

// broadcast asks for the state to go to the subscribers, and nothing to
// disk: what only the window shows, as a pane's title, has nothing saved.
func (d *Daemon) broadcast() {
	select {
	case d.changeCh <- struct{}{}:
	default:
	}
}

// save asks for the state to go to disk.
func (d *Daemon) save() {
	select {
	case d.saveCh <- struct{}{}:
	default:
	}
}

// historySoon asks for the histories to go to disk within historyWait,
// as a pane's text changed. Once asked, it costs an atomic load until they
// do, so streaming output does not wake the save loop for each read.
func (d *Daemon) historySoon() {
	if d == nil || d.histArmed.Load() || !d.histArmed.CompareAndSwap(false, true) {
		return
	}
	select {
	case d.histCh <- struct{}{}:
	default:
	}
}

// notice passes a program's desktop notification on to the app.
func (d *Daemon) notice(n proto.Notice) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for c := range d.subs {
		c.event(proto.Message{Event: "notice", Notice: &n})
	}
}

func (d *Daemon) broadcastLoop() {
	for range d.changeCh {
		// Coalesce a burst of changes into one message.
		time.Sleep(25 * time.Millisecond)
		select {
		case <-d.changeCh:
		default:
		}
		d.broadcastNow()
	}
}

// broadcastNow sends the subscribers what changed: the sessions, every
// time, and the Linear workspace, only when it is not the one they have.
func (d *Daemon) broadcastNow() {
	st := d.broadcastState()
	lin := d.workspace()
	d.mu.Lock()
	for c := range d.subs {
		c.event(proto.Message{Event: "state", State: &st})
	}
	d.mu.Unlock()
	d.sendLinear(lin)
}

// sendLinear sends the workspace as an event of its own, when it differs
// from the last one sent: it changes with the workspace and with the
// sessions tasks show, not with a title, and encoding it per subscriber is
// what a storm of titles would pay. A nil workspace is one too: it says the
// daemon has none, as State.Linear did.
func (d *Daemon) sendLinear(lin *proto.LinearState) {
	d.linearOutMu.Lock()
	same := d.linearKnown && reflect.DeepEqual(d.linearOut, lin)
	// linearOutMu is let go before d.mu is taken: event holds d.mu and
	// takes linearOutMu (forgetLinear), so that is the one order.
	d.linearOut, d.linearKnown = lin, true
	d.linearOutMu.Unlock()
	if same {
		return
	}
	d.mu.Lock()
	for c := range d.subs {
		c.event(proto.Message{Event: "linear", Linear: lin})
	}
	d.mu.Unlock()
}

// sendLinearTo gives a client that has just subscribed the workspace it
// holds. It records nothing: the others did not get it, so the next
// broadcast must still send it to them, and a second copy for this one is
// cheap.
func (d *Daemon) sendLinearTo(c *controlConn, lin *proto.LinearState) {
	c.event(proto.Message{Event: "linear", Linear: lin})
}

// forgetLinear says that a workspace did not reach every client: the next
// broadcast sends it once more, whether or not it changed.
func (d *Daemon) forgetLinear() {
	d.linearOutMu.Lock()
	d.linearKnown = false
	d.linearOutMu.Unlock()
}

// The tick follows the panes (pane.tick) every tickFast while something
// happens in them: output, a key, a request of a client. Once nothing has
// for tickQuiet, it ticks every tickSlow, as often as it already read the
// directory and the program of a pane the user is not in (cwdEvery,
// fgRecheck), until the next thing happens (stir). Each tick woke kerod a
// few times, so the fixed 400 ms tick alone was some 1,000 wakeups a minute
// of a kerod at rest (docs/perf.md, BRU-336). What a tick reads of a quiet
// pane changes with output, as a command that starts or ends, a cd or a
// branch switched in it, so a quiet pane loses nothing but what changed
// outside it: a transcript written without a word on the screen is seen
// within tickSlow rather than seconds. Branch HEAD checks have their own
// cheap loop (branchLoop), so an external checkout is still seen promptly.
const (
	tickFast  = 400 * time.Millisecond
	tickSlow  = 10 * time.Second
	tickQuiet = 10 * time.Second
	// The agents' transcripts are scanned this often: slow reads, of
	// what else told nothing.
	scanEvery = 12 * tickFast
)

// tickPace is the pace of the tick loop: fast within quiet of when
// something last happened, and slow after.
type tickPace struct{ fast, slow, quiet time.Duration }

// wait returns how long the tick loop waits for its next tick, active
// being when something last happened.
func (p tickPace) wait(now, active time.Time) time.Duration {
	if now.Sub(active) < p.quiet {
		return p.fast
	}
	return p.slow
}

// stir tells the tick loop that something happened in a pane: a loop that
// waits tickSlow ticks at once, and at its fast pace for tickQuiet after.
// It costs two atomic loads while the loop already ticks fast, as it does
// while output streams.
func (d *Daemon) stir() {
	if d == nil {
		return
	}
	if !d.stirred.Load() {
		d.stirred.Store(true)
	}
	if d.tickSlow.Load() && d.tickSlow.CompareAndSwap(true, false) {
		select {
		case d.tickWake <- struct{}{}:
		default:
		}
	}
}

func (d *Daemon) tickLoop() { d.tickLoopWith(tickPace{tickFast, tickSlow, tickQuiet}) }

func (d *Daemon) tickLoopWith(pace tickPace) {
	go d.branchLoop()
	t := time.NewTimer(pace.fast)
	defer t.Stop()
	start := time.Now()
	active, scanned := start, start
	for {
		select {
		case <-d.quit:
			return
		case <-t.C:
		case <-d.tickWake:
		}
		d.tickSlow.Store(false)
		now := time.Now()
		d.mu.Lock()
		panes := make([]*pane, 0, len(d.panes))
		for _, p := range d.panes {
			panes = append(panes, p)
		}
		idle := len(d.sessions) == 0 && d.conns == 0 && !d.restoring()
		if !idle {
			d.idleAt = now
		}
		idleFor := now.Sub(d.idleAt)
		focused := d.focusedPaneLocked()
		d.mu.Unlock()
		changed, busy := false, false
		for _, s := range panes {
			profiled("pane.tick", func() {
				c, b := s.tick(now, s == focused)
				changed = changed || c
				busy = busy || b
			})
		}
		if changed {
			d.changed()
		}
		if now.Sub(scanned) >= scanEvery {
			scanned = now
			// On its own goroutine: a slow disk must not stall the tick.
			go d.scanPass(now)
		}
		// Nothing to keep: go, rather than linger in the background.
		if idle && idleFor > 30*time.Second {
			d.log.Printf("idle: exiting")
			d.stop()
			return
		}
		// A pane that waits out idleHold, and a kerod that counts down to
		// its exit, keep the fast pace too.
		if d.stirred.Swap(false) || changed || busy || idle {
			active = now
		}
		wait := pace.wait(now, active)
		if wait != pace.fast {
			d.tickSlow.Store(true)
			// What stirred since the swap above finds the loop not yet
			// slow, and wakes nothing: the loop sees it here instead.
			if d.stirred.Load() && d.tickSlow.CompareAndSwap(true, false) {
				wait = pace.fast
			}
		}
		t.Reset(wait)
	}
}

func newID() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	var b [7]byte
	rand.Read(b[:])
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:])
}

// projectFor returns the project a new session goes in: the one named, or
// the one whose directory holds dir, or a new one for dir's repository.
// d.mu is held.
func (d *Daemon) projectFor(ref, dir string) string {
	if ref != "" {
		for _, p := range d.projects {
			if p.ID == ref || strings.EqualFold(p.Name, ref) {
				return p.ID
			}
		}
		p := proto.Project{ID: newID(), Name: ref, Dir: dir}
		d.projects = append(d.projects, p)
		return p.ID
	}
	if dir == "" {
		if len(d.projects) > 0 {
			return d.projects[0].ID
		}
		dir = proto.Home()
	}
	best, bestLen := "", -1
	for _, p := range d.projects {
		if p.Dir != "" && proto.Within(dir, p.Dir) && len(p.Dir) > bestLen {
			best, bestLen = p.ID, len(p.Dir)
		}
	}
	if best != "" {
		return best
	}
	root := repoRoot(dir)
	name := filepath.Base(root)
	if root == proto.Home() {
		name = "Home"
	}
	p := proto.Project{ID: newID(), Name: name, Dir: root}
	d.projects = append(d.projects, p)
	return p.ID
}

// repoRoot returns the root of the git repository holding dir, or dir.
func repoRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d || d == proto.Home() {
			return dir
		}
		d = parent
	}
}

// environment returns the environment of a session's shell.
// ghosttyVersion is the version of Ghostty the terminal emulator, the
// terminal plugin's libghostty-vt, is built from (its build.zig.zon).
const ghosttyVersion = "1.3.2-dev"

func (d *Daemon) environment(sid, pid, shell string, extra []string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "CLAUDECODE", k == "TERM_PROGRAM", k == "TERM_PROGRAM_VERSION", k == "TERM_SESSION_ID",
			k == "ITERM_SESSION_ID", k == "WINDOWID", k == "KITTY_WINDOW_ID", k == "LC_TERMINAL", k == "LC_TERMINAL_VERSION",
			k == "KERO_SESSION", k == "KERO_PANE", k == "MYGO_GHOSTTY_VT", k == "TMUX", k == "TMUX_PANE",
			strings.HasPrefix(k, "CLAUDE_CODE_"), strings.HasPrefix(k, "GHOSTTY_"),
			strings.HasPrefix(k, "VSCODE_"), strings.HasPrefix(k, "__CF"), k == "XPC_SERVICE_NAME":
			continue
		}
		env = append(env, kv)
	}
	set := func(kv string) {
		k, _, _ := strings.Cut(kv, "=")
		for i, e := range env {
			// Windows' names are of any case: its PATH is "Path".
			if ek, _, _ := strings.Cut(e, "="); ek == k || runtime.GOOS == "windows" && strings.EqualFold(ek, k) {
				env[i] = kv
				return
			}
		}
		env = append(env, kv)
	}
	set("TERM=xterm-256color")
	set("COLORTERM=truecolor")
	// kero's emulator is Ghostty's: programs that know Ghostty, as coding
	// agents for their notifications and keyboard, take it for one.
	// LC_TERMINAL names kero, as iTerm2 names itself there.
	set("TERM_PROGRAM=ghostty")
	set("TERM_PROGRAM_VERSION=" + ghosttyVersion)
	set("LC_TERMINAL=kero")
	set(fmt.Sprintf("LC_TERMINAL_VERSION=%d", proto.Version))
	// The shell the pane runs, not the one whoever started kerod had. On
	// Windows, only a POSIX shell's: programs there take $SHELL for one.
	if runtime.GOOS != "windows" || posixShell(shellName(shell)) {
		set("SHELL=" + shell)
	}
	set("KERO_SESSION=" + sid)
	set("KERO_PANE=" + pid)
	set("KERO_SOCKET=" + proto.SocketPath())
	if os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" && os.Getenv("LC_CTYPE") == "" {
		set("LANG=en_US.UTF-8")
	}
	// The CLI, after what the user's profile puts first.
	path := os.Getenv("PATH")
	if path == "" && runtime.GOOS != "windows" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	set("PATH=" + path + string(os.PathListSeparator) + filepath.Join(d.dir, "bin"))
	for _, kv := range settings.Load().Env {
		if strings.Contains(kv, "=") {
			set(kv)
		}
	}
	for _, kv := range extra {
		set(kv)
	}
	return env
}

// userShell returns the shell of new panes: the one Settings names, else
// the user's login shell.
func userShell() string {
	if sh := settings.Load().Shell; sh != "" {
		if st, err := os.Stat(expandHome(sh)); err == nil && !st.IsDir() {
			return expandHome(sh)
		}
		// A name on PATH, as "pwsh" or "bash" on Windows.
		if !strings.ContainsAny(sh, `/\`) {
			if p, err := exec.LookPath(sh); err == nil {
				return p
			}
		}
	}
	return shell.Login()
}

func expandHome(p string) string {
	if p == "~" {
		return proto.Home()
	}
	if strings.HasPrefix(p, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(p, `~\`) {
		return filepath.Join(proto.Home(), p[2:])
	}
	return p
}

// agents are the coding agents the sidebar recognizes, by the name of
// their executable or script.
var agents = []string{
	"claude", "codex", "gemini", "opencode", "amp", "aider", "cursor-agent", "droid",
	"qwen", "goose", "crush", "copilot", "kiro-cli", "cline", "auggie", "kimi", "pi",
}

func detectAgent(name string, args []string) string {
	cands := []string{name}
	for i, a := range args {
		if i > 2 {
			break
		}
		cands = append(cands, filepath.Base(a))
	}
	for _, c := range cands {
		c = strings.ToLower(strings.TrimSuffix(c, filepath.Ext(c)))
		for _, a := range agents {
			if c == a || strings.HasPrefix(c, a+"-") && a != "pi" {
				return a
			}
		}
	}
	// Scripts installed by npm: …/node_modules/@openai/codex/bin/codex.js,
	// or …\@anthropic-ai\claude-code\cli.js, which npm's shims run on
	// Windows, where nothing names the script after the command.
	for i, a := range args {
		if i > 2 {
			break
		}
		a = strings.ReplaceAll(a, `\`, "/")
		for _, ag := range agents {
			if len(ag) > 3 && (strings.Contains(a, "/"+ag+"/") || strings.Contains(a, "/"+ag+"-code/")) {
				return ag
			}
		}
	}
	return ""
}

// controlConn is a client's control connection.
type controlConn struct {
	conn net.Conn
	wmu  sync.Mutex
	enc  *json.Encoder
	out  chan proto.Message
	done chan struct{}
	d    *Daemon // for the count of what a client was too slow to take
}

func (c *controlConn) write(m proto.Message) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := c.enc.Encode(m); err != nil {
		c.conn.Close()
	}
}

// event queues an event for a subscriber, dropping it when it is far
// behind: the next state replaces it anyway, and a Linear event is sent
// again by the next broadcast, so one dropped is not lost.
func (c *controlConn) event(m proto.Message) {
	select {
	case c.out <- m:
	default:
		if c.d != nil {
			if m.Event == "linear" {
				// d.mu is held here, as the broadcast sends: forgetLinear
				// takes linearOutMu under it, and nothing takes d.mu while
				// holding linearOutMu.
				c.d.forgetLinear()
			}
			c.d.dropEvent()
		}
	}
}

// dropEvent counts an event a client was too far behind to take, and says so
// in the log at most once a minute while it happens: a window that builds
// for every state can fall this far behind, and the count says that it did.
func (d *Daemon) dropEvent() {
	n := d.dropped.Add(1)
	now := time.Now().UnixMilli()
	last := d.dropAt.Load()
	if now-last < 60_000 || !d.dropAt.CompareAndSwap(last, now) {
		return
	}
	// The log is a file write, and the event dropped may have come with
	// d.mu held: it goes on a goroutine of its own.
	if lg := d.log; lg != nil {
		go lg.Printf("%d events dropped: a client is behind", n)
	}
}

func (d *Daemon) serve(conn net.Conn) {
	d.mu.Lock()
	d.conns++
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.conns--
		d.mu.Unlock()
	}()
	defer conn.Close()

	r := bufio.NewReaderSize(conn, 64<<10)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})
	var hello proto.Hello
	if err := json.Unmarshal(line, &hello); err != nil {
		return
	}
	welcome := proto.Welcome{Version: proto.Version, PID: os.Getpid(), Started: d.started.UnixMilli(), App: mygo.App.Version()}
	if hello.Version != proto.Version {
		welcome.Error = fmt.Sprintf("kerod speaks protocol %d, the client %d", proto.Version, hello.Version)
		json.NewEncoder(conn).Encode(welcome)
		return
	}
	switch hello.Kind {
	case "attach":
		d.serveAttach(conn, r, hello, welcome)
	default:
		json.NewEncoder(conn).Encode(welcome)
		d.serveControl(conn, r)
	}
}

func (d *Daemon) serveAttach(conn net.Conn, r *bufio.Reader, hello proto.Hello, welcome proto.Welcome) {
	s, err := d.findTermPane(hello.Session)
	if err != nil {
		welcome.Error = err.Error()
		json.NewEncoder(conn).Encode(welcome)
		return
	}
	a := newAttachment(conn)
	first, cols, rows, ok := s.attach(a)
	if !ok {
		welcome.Error = "session closed"
		json.NewEncoder(conn).Encode(welcome)
		return
	}
	defer s.detach(a)
	defer a.finish()
	welcome.Cols, welcome.Rows = cols, rows
	if err := json.NewEncoder(conn).Encode(welcome); err != nil {
		return
	}
	if _, err := conn.Write(proto.AppendFrames(nil, proto.FrameOutput, first)); err != nil {
		return
	}
	go a.write()
	for {
		kind, payload, err := proto.ReadFrame(r)
		if err != nil {
			return
		}
		// A key or a resize: the pane is followed closely for a while.
		d.stir()
		switch kind {
		case proto.FrameInput:
			s.input(payload)
		case proto.FrameResize:
			if c, rr, cw, ch, ok := proto.ParseResizePixels(payload); ok {
				s.resize(c, rr, cw, ch)
			}
		case proto.FrameTheme:
			if th, ok := proto.ParseTheme(payload); ok {
				d.useTheme(th)
			}
		case proto.FrameClear:
			if len(payload) == 1 {
				s.clear(payload[0])
			}
		}
	}
}

func (d *Daemon) serveControl(conn net.Conn, r *bufio.Reader) {
	c := &controlConn{conn: conn, enc: json.NewEncoder(conn), out: make(chan proto.Message, 64), done: make(chan struct{}), d: d}
	defer close(c.done)
	defer func() {
		d.mu.Lock()
		delete(d.subs, c)
		d.mu.Unlock()
	}()
	go func() {
		for {
			select {
			case m := <-c.out:
				c.write(m)
			case <-c.done:
				return
			}
		}
	}()
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		go func() {
			res, err := d.call(c, req.Method, req.Params)
			// A request may start, end or tell of the work of a pane:
			// the panes are followed closely for a while.
			d.stir()
			m := proto.Message{ID: req.ID, Result: res}
			if err != nil {
				m.Error = err.Error()
			}
			c.write(m)
		}()
	}
}
