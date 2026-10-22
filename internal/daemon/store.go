package daemon

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
	"kero/internal/settings"
)

// The daemon keeps its projects and sessions in state.json, and each
// session's screen and scrollback in sessions/<id>.vt, so that after the
// daemon itself ends, as when the Mac restarts, the next one starts every
// session again where it was, with what it showed.

// stateVersion is the version of state.json this daemon writes.
//
//   - 2: the version before Linear became a plugin.
//   - 3: panes of the built-in Tasks view are Linear plugin panes
//     (migrateState).
const stateVersion = 3

// linearPluginTarget is the target a Tasks pane becomes: the Linear plugin's
// board widget, as a list of the session's project with finished issues
// hidden. kero split --tasks makes the same target.
func linearPluginTarget() string {
	t, err := protocol.EncodeTarget(protocol.Target{
		Plugin: "linear",
		Widget: "board",
		State:  json.RawMessage(`{"mode":"list","scope":"project","hideDone":true}`),
	})
	if err != nil {
		// The target is a constant that encodes; a failure is a bug.
		panic("linear plugin target: " + err.Error())
	}
	return t
}

// migrateState brings a state file read from disk to stateVersion, in
// place. It reports whether it changed anything.
//
// Version 2 to 3: every pane whose View is "tasks" (the built-in Tasks
// pane, removed with Linear's move to a plugin) becomes a pane of the
// plugin view with the Linear board target. Everything else in the pane
// (id, dir, task, status, size) and in the file is kept, and a pane with
// any other view, known or not, is not touched. The migration runs only
// on a file older than version 3 and sets the version, so a second run,
// and a file already at 3, change nothing: a Tasks pane found in a version
// 3 file was made on purpose and stays. A file with no version (0) is
// treated as 2. A daemon of before reads a version 3 file without
// complaint (it does not check the version), and shows the Linear pane as
// a plugin pane it may not have.
func migrateState(st *savedState) bool {
	if st.Version >= stateVersion {
		return false
	}
	target := linearPluginTarget()
	for i := range st.Sessions {
		for j := range st.Sessions[i].Panes {
			p := &st.Sessions[i].Panes[j]
			if p.View == proto.PaneTasks {
				p.View, p.Target = proto.PanePlugins, target
			}
		}
	}
	st.Version = stateVersion
	return true
}

type savedState struct {
	Version  int             `json:"version"`
	Projects []proto.Project `json:"projects"`
	Sessions []savedSession  `json:"sessions"`
	Saved    int64           `json:"saved"`
	// Linear is the workspace, which a state file of before kept here;
	// it is now in linear.json (linear_store.go), and read from here
	// only to move it there.
	Linear *proto.LinearState `json:"linear,omitempty"`
	// Tasks are the conversations that worked on each task, by the
	// issue's id: what outlives the sessions, to resume.
	Tasks map[string]*taskLog `json:"tasks,omitempty"`
}

type savedSession struct {
	ID         string        `json:"id"`
	Project    string        `json:"project"`
	Name       string        `json:"name,omitempty"`
	AgentNamed bool          `json:"agentNamed,omitempty"`
	Created    int64         `json:"created"`
	Layout     *proto.Layout `json:"layout,omitempty"`
	Focus      string        `json:"focus,omitempty"`
	Panes      []savedPane   `json:"panes"`
	// Unlinked are the issues the user unlinked the session from, which
	// its branch does not link it to again.
	Unlinked []string `json:"unlinked,omitempty"`
}

type savedPane struct {
	ID string `json:"id"`
	// View and Target are a view's (proto.Pane.View).
	View    string   `json:"view,omitempty"`
	Target  string   `json:"target,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Command []string `json:"command,omitempty"`
	// Agent is the agent's session that ran in it, to resume.
	Agent *proto.AgentSession `json:"agent,omitempty"`
	// Task is the Linear issue the work was on, so that a restored
	// session is still linked to its task.
	Task    string `json:"task,omitempty"`
	Created int64  `json:"created"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	// Status, Since and Unread are where the pane's status stood, Message
	// a failure's and ExitCode an exited shell's, so that a restore keeps
	// what the user has yet to see and how long ago it changed
	// (restoredStatus). A state file of before has none of them: its
	// panes come back idle and read, as of the restore, as they always
	// did. A daemon of before reads past them.
	Status   proto.Status `json:"status,omitempty"`
	Since    int64        `json:"since,omitempty"`
	Unread   bool         `json:"unread,omitempty"`
	Message  string       `json:"message,omitempty"`
	ExitCode int          `json:"exitCode,omitempty"`
	// Summary is what the pane's attention said when last saved
	// (attention.go): the words of an end that came while the app was
	// away, which a restored pane's unread mark shows. A state file of
	// before has none.
	Summary string `json:"summary,omitempty"`
}

func (d *Daemon) statePath() string { return filepath.Join(d.dir, "state.json") }

// savedSummary is the words a pane's attention would show of an unread
// end, which a restore keeps for it: only for a pane whose turn ended
// unseen. What a working or waiting pane says is no end's, and would read
// as one once the restore brings the pane back idle and unread; s.mu is
// held.
func savedSummary(p *pane) string {
	if a := paneAttention(p); a.State == attentionUnread && p.info.Status == proto.Idle {
		return a.Msg
	}
	return ""
}

// historyPath is where a pane's history is kept, for a restore.
func (d *Daemon) historyPath(id string) string { return filepath.Join(d.dir, "sessions", id+".vt") }

// stateWait is how long a change waits for the next before the state is
// saved: a storm of status changes writes state.json once every 2 s.
const stateWait = 2 * time.Second

// historyWait is how long the text of a pane waits to go to disk after it
// changed: its history is at most this old after a crash. A kerod whose
// panes show nothing new writes nothing, and wakes for nothing: the
// histories went to disk every 10 s whatever happened (BRU-336).
const historyWait = 10 * time.Second

func (d *Daemon) saveLoop() {
	d.saveLoopWith(stateWait, historyWait, d.saveState, func() { d.saveHistories(false) })
}

// saveLoopWith saves the state with save, wait after the first change of
// those that come meanwhile, and the histories with snap, history after
// the first change of their text (historySoon), and once at the start,
// for those restored.
func (d *Daemon) saveLoopWith(wait, history time.Duration, save, snap func()) {
	snaps := time.NewTimer(history)
	defer snaps.Stop()
	d.histArmed.Store(true)
	var due <-chan time.Time
	for {
		select {
		case <-d.saveCh:
			if due == nil {
				due = time.After(wait)
			}
		case <-due:
			due = nil
			save()
		case <-d.histCh:
			snaps.Reset(history)
		case <-snaps.C:
			// What changes from now on asks again: a pane's dirty flag
			// is set before historySoon is asked, so a change the save
			// misses asks after this.
			d.histArmed.Store(false)
			profiled("saveHistories", snap)
		case <-d.quit:
			return
		}
	}
}

func (d *Daemon) saveState() {
	// The sessions of the last daemon come back in the background: a save
	// now would write them away, and a crash after it would lose those not
	// back yet. The restore asks for one as it ends.
	if d.restoring() {
		return
	}
	d.mu.Lock()
	// The Linear workspace is kept in a file of its own, written only
	// as it changes (linear_store.go).
	st := savedState{Version: stateVersion, Projects: d.projects, Saved: time.Now().UnixMilli()}
	lin, linData := d.linearToSaveLocked()
	for _, s := range d.sessions {
		ss := savedSession{ID: s.id, Project: s.project, Name: s.name, AgentNamed: s.agentNamed, Created: s.created, Layout: s.layout, Focus: s.focus,
			Unlinked: slices.Sorted(maps.Keys(s.unlinked))}
		for _, p := range s.panes {
			p.mu.Lock()
			sp := savedPane{ID: p.info.ID, View: p.info.View, Target: p.info.Target, Dir: p.info.Dir, Command: p.info.Command,
				Agent: p.info.AgentSession, Task: p.info.TaskID, Created: p.info.Created, Cols: p.info.Cols, Rows: p.info.Rows,
				Status: p.info.Status, Since: p.info.Since, Unread: p.info.Unread, Summary: savedSummary(p)}
			switch p.info.Status {
			case proto.Failed:
				sp.Message = p.info.Message
			case proto.Exited:
				sp.ExitCode = p.info.ExitCode
			}
			ss.Panes = append(ss.Panes, sp)
			p.mu.Unlock()
		}
		st.Sessions = append(st.Sessions, ss)
	}
	d.taskMu.Lock()
	st.Tasks = d.tasks
	data, err := json.MarshalIndent(st, "", "  ")
	d.taskMu.Unlock()
	d.mu.Unlock()
	if err != nil {
		return
	}
	// The workspace first: a state file without it, written before it,
	// would lose it to a crash between the two.
	if linData != nil {
		if writeAtomic(d.linearPath(), linData) != nil {
			return
		}
		d.linearWritten(lin)
	}
	writeAtomic(d.statePath(), data)
}

// saveHistories writes the panes' histories that changed, or all. What
// changed goes as its tail (historyTail); all, as the daemon ends, goes
// whole.
func (d *Daemon) saveHistories(force bool) {
	d.mu.Lock()
	panes := make([]*pane, 0, len(d.panes))
	for _, p := range d.panes {
		panes = append(panes, p)
	}
	d.mu.Unlock()
	for _, p := range panes {
		if p.isView() {
			continue
		}
		if h := p.history(force); h != nil {
			if !force {
				h = historyTail(h, historyTailBytes)
			}
			writeAtomic(d.historyPath(p.info.ID), h)
		}
	}
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// restoreWorkers is how many sessions a restore brings back at a time: each
// feeds a history and forks a shell, and more at once take the machine from
// what the user does meanwhile.
const restoreWorkers = 4

// restoringPane, when set, is a test's hold on a pane of a restore: it runs
// as the pane comes back, and may hold it (a history that takes its time) or
// fail it (a shell that will not start).
var restoringPane func(sp savedPane) error

// restorePlan is what a restore leaves for the background: the sessions as
// saved, and the gate on them. done is closed once every one is back, so
// that no save writes less than the whole state (saveState) and an idle
// daemon does not go while they come (tickLoop).
type restorePlan struct {
	sessions []savedSession
	// place is where the state file had each session, by its id: it comes
	// back there, whatever order the sessions finish in.
	place   map[string]int
	when    string
	saved   int64
	resume  bool // resume the agents the panes ran
	done    chan struct{}
	oldRuns map[proto.TaskRun]bool // only runs read from the last daemon
}

// restore reads what the last daemon left: its workspace, its projects and
// the conversations that worked on the tasks. The panes are not started
// here: they come back in the background (restoreInBackground), so that the
// socket takes clients at once and the app is not left blank while every
// history is fed and every shell forked. It returns nil when there is
// nothing to bring back.
func (d *Daemon) restore() *restorePlan {
	lin := d.loadLinear()
	d.linear, d.linearSaved = lin, lin
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		return nil
	}
	var st savedState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	// A state file of before keeps the workspace in it: it is taken
	// from there, and moves to its own file with the next save. The
	// workspace is read before the migration, which leaves it alone,
	// and the next save writes the migrated version.
	migrateState(&st)
	if d.linear = newerLinear(lin, st.Linear); d.linear != lin {
		d.linearSaved = nil
	}
	d.tasks = st.Tasks
	// The projects stay whether or not the sessions come back.
	d.projects = st.Projects
	set := settings.Load()
	p := &restorePlan{when: time.UnixMilli(st.Saved).Format("Mon Jan 2 15:04"), saved: st.Saved, done: make(chan struct{})}
	if set.RestoreSessions {
		// The sessions come back as they were saved; those of a daemon
		// that keeps none stay away, though its conversations still end
		// (restoreInBackground).
		p.sessions, p.resume = st.Sessions, set.ResumeAgents
		p.place = make(map[string]int, len(st.Sessions))
		for i, ss := range st.Sessions {
			p.place[ss.ID] = i
		}
	}
	p.oldRuns = map[proto.TaskRun]bool{}
	for _, tl := range st.Tasks {
		if tl != nil {
			for _, run := range tl.Runs {
				p.oldRuns[run] = true
			}
		}
	}
	d.restoreDone = p.done
	return p
}

// restoring tells that the sessions of the last daemon are still coming
// back: a state saved now would drop those not back yet, and the daemon is
// not idle with none of them.
func (d *Daemon) restoring() bool {
	if d.restoreDone == nil {
		return false
	}
	select {
	case <-d.restoreDone:
		return false
	default:
		return true
	}
}

// restoreRun is one restore in the background: the sessions still to bring
// back, and what came of them.
type restoreRun struct {
	d    *Daemon
	plan *restorePlan
	next atomic.Int64
	// made is what each saved session came back as: a worker writes the
	// one of its own session, and none is read before they are all done
	// (restoreInBackground).
	made []restoredSession
}

// restoredSession is a session that came back, and whether each of its
// panes' agents resumed, which ends the runs of the last daemon.
type restoredSession struct {
	id      string
	resumed map[string]bool
}

// restoreInBackground brings back the sessions of the last daemon, up to
// restoreWorkers at a time, and publishes each one whole as it comes: the
// app shows them arrive one by one, and nothing sees a pane half made, with
// no emulator or no program under it.
func (d *Daemon) restoreInBackground(p *restorePlan) {
	if p == nil {
		return
	}
	r := &restoreRun{d: d, plan: p, made: make([]restoredSession, len(p.sessions))}
	var wg sync.WaitGroup
	for range min(restoreWorkers, len(p.sessions)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(r.next.Add(1) - 1)
				if i >= len(p.sessions) {
					return
				}
				r.restore(i)
			}
		}()
	}
	wg.Wait()
	// The conversations of the last daemon end, but those of the panes
	// restored with their agents resumed, and the stand-ins of the
	// sessions restored: none runs any more.
	resumed, standing := map[string]bool{}, map[string]bool{}
	for _, m := range r.made {
		if m.id == "" {
			continue
		}
		standing[m.id] = true
		for id, agent := range m.resumed {
			resumed[id] = agent
		}
	}
	d.taskEndWhere(func(t proto.TaskRun) bool {
		return p.oldRuns[t] && !(t.Pane != "" && resumed[t.Pane] || t.Pane == "" && standing[t.Session])
	})
	close(p.done)
	// The saves asked for as the sessions came, and this one, were dropped
	// (saveState): the state with them all goes to disk now.
	d.save()
}

// restore brings back saved session i, outside d.mu: a fork and a history
// take their time, and the clients go on meanwhile.
func (r *restoreRun) restore(i int) {
	d := r.d
	ss := r.plan.sessions[i]
	s := &session{id: ss.ID, project: ss.Project, name: ss.Name, agentNamed: ss.AgentNamed, created: ss.Created, restored: true, focus: ss.Focus}
	for _, t := range ss.Unlinked {
		if s.unlinked == nil {
			s.unlinked = map[string]bool{}
		}
		s.unlinked[t] = true
	}
	agents := map[string]bool{} // private results, never read from published panes
	for _, sp := range ss.Panes {
		if !r.plan.resume {
			sp.Agent = nil
		}
		p := d.restorePane(s.id, sp, r.plan.when, r.plan.saved)
		if p == nil {
			continue
		}
		s.panes = append(s.panes, p)
		agents[sp.ID] = sp.Agent != nil
	}
	if len(s.panes) == 0 {
		return
	}
	if sameLeaves(ss.Layout, s.panes) {
		s.layout = ss.Layout
	} else {
		for _, p := range s.panes[1:] {
			if s.layout == nil {
				s.layout = &proto.Layout{Pane: s.panes[0].info.ID}
			}
			s.layout = s.layout.Split(s.panes[0].info.ID, p.info.ID, "right", 0.5)
		}
	}
	r.made[i] = restoredSession{id: s.id, resumed: agents}
	r.publish(i, s)
}

// publish puts a session that is whole into the daemon's, after the last
// session the state file had before it, and tells the app. The place is
// looked for in d.sessions itself, so that a session whose panes all failed,
// or one the user closed while the others came, counts for nothing.
func (r *restoreRun) publish(i int, s *session) {
	d := r.d
	d.mu.Lock()
	at := 0
	for j, o := range d.sessions {
		if n, ok := r.plan.place[o.id]; ok && n < i {
			at = j + 1
		}
	}
	for _, p := range s.panes {
		d.panes[p.info.ID] = p
	}
	d.sessions = slices.Insert(d.sessions, at, s)
	n := len(s.panes)
	d.mu.Unlock()
	d.changed()
	d.log.Printf("session %s restored, %d panes", s.id, n)
}

// restoredStatus is the status a pane of the last daemon comes back with,
// its time and whether the user has seen it, and its message, as of now in
// Unix milliseconds, from a state file saved at saved. What ran in the
// pane is gone, and a new shell starts:
//
//   - work under way, or a question to the user, stopped: the pane comes
//     back idle and unread, as of the save, so that the user sees it
//     stopped, and it does not read as at rest since the work began;
//   - a shell that exited with an error comes back failed, with its code,
//     while the user had not seen it, and idle once seen, as of the save;
//   - a failure, which the user's first key clears as it would have, and
//     a pane idle, keep their time and their mark.
//
// A pane saved with no status, in a state file of before, is idle and
// read as of now, as it always was.
func restoredStatus(sp savedPane, saved, now int64) (st proto.Status, since int64, unread bool, msg string) {
	if saved <= 0 || saved > now {
		saved = now
	}
	st, since, unread = proto.Idle, sp.Since, sp.Unread
	switch {
	case sp.View != "":
		// A view never works or fails.
	case sp.Status == proto.Working || sp.Status == proto.Waiting:
		since, unread = saved, true
	case sp.Status == proto.Exited && sp.Unread:
		st, since, msg = proto.Failed, saved, fmt.Sprintf("Exited with code %d", sp.ExitCode)
	case sp.Status == proto.Exited:
		since = saved
	case sp.Status == proto.Failed:
		st, msg = proto.Failed, sp.Message
	}
	if since <= 0 || since > now {
		since = now
	}
	return st, since, unread, msg
}

// restorePane starts a pane of the last daemon again, under its
// scrollback, in its directory, with the agent it ran resumed.
func (d *Daemon) restorePane(sid string, sp savedPane, when string, saved int64) *pane {
	if restoringPane != nil {
		if err := restoringPane(sp); err != nil {
			d.log.Printf("restoring %s: %v", sp.ID, err)
			return nil
		}
	}
	now := time.Now().UnixMilli()
	status, since, unread, msg := restoredStatus(sp, saved, now)
	if sp.View != "" {
		// A view comes back as it was: nothing ran in it.
		p, err := newPane(d, sid, proto.Pane{ID: sp.ID, View: sp.View, Target: sp.Target, Dir: sp.Dir, Status: status, Since: since, Unread: unread,
			Created: sp.Created, Cols: sp.Cols, Rows: sp.Rows, TaskID: sp.Task})
		if err != nil {
			d.log.Printf("restoring %s: %v", sp.ID, err)
			return nil
		}
		return p
	}
	p, err := newPane(d, sid, proto.Pane{
		ID: sp.ID, Dir: sp.Dir, Command: sp.Command, Status: status, Since: since, Unread: unread, Message: msg,
		Created: sp.Created, Cols: sp.Cols, Rows: sp.Rows, Restored: true, AgentSession: sp.Agent,
		TaskID: sp.Task,
	})
	if err != nil {
		d.log.Printf("restoring %s: %v", sp.ID, err)
		return nil
	}
	// What the pane's attention said when it was saved, for an unread
	// end to show whose message a restore does not keep.
	// A saved Working or Waiting pane comes back as an unread end of
	// no words: what its row said then ("Working · 30%") was no end's.
	if sp.Status != proto.Working && sp.Status != proto.Waiting {
		p.summary = sp.Summary
	}
	// What the session showed, down to where its cursor was, with the
	// new shell under it: what a program drew below the cursor, as an
	// agent's status bar, goes, rather than show under the new shell's
	// prompt. History carries no modes and no cursor to undo.
	if h, err := os.ReadFile(d.historyPath(sp.ID)); err == nil && len(h) > 0 {
		p.term.Feed(h)
		p.term.Feed([]byte("\x1b[2m── restored · session ended " + when + " ──\x1b[0m\r\n"))
	}
	// The shell alone: what ran in it may not want to run again unasked.
	// But an agent's conversation, which only waits for the user.
	p.dirty = true
	if err := p.start(resumeCommand(sp.Agent), nil); err != nil {
		d.log.Printf("restoring %s: %v", sp.ID, err)
		p.term.Close()
		return nil
	}
	return p
}

// resumeCommand is the command that resumes an agent's session, nil for
// none.
func resumeCommand(a *proto.AgentSession) []string {
	if a == nil {
		return nil
	}
	return proto.ResumeCommand(a.Agent, a.ID)
}
