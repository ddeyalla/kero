package daemon

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"kero/internal/proto"
	"kero/internal/settings"
)

// The daemon keeps its projects and sessions in state.json, and each
// session's screen and scrollback in sessions/<id>.vt, so that after the
// daemon itself ends, as when the Mac restarts, the next one starts every
// session again where it was, with what it showed.

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

// historyPath is where a pane's history is kept, for a restore.
func (d *Daemon) historyPath(id string) string { return filepath.Join(d.dir, "sessions", id+".vt") }

// stateWait is how long a change waits for the next before the state is
// saved: a storm of status changes writes state.json once every 2 s.
const stateWait = 2 * time.Second

func (d *Daemon) saveLoop() { d.saveLoopWith(stateWait, d.saveState) }

// saveLoopWith saves the state with save, wait after the first change of
// those that come meanwhile, and the histories every 10 s.
func (d *Daemon) saveLoopWith(wait time.Duration, save func()) {
	snaps := time.NewTicker(10 * time.Second)
	defer snaps.Stop()
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
		case <-snaps.C:
			d.saveHistories(false)
		case <-d.quit:
			return
		}
	}
}

func (d *Daemon) saveState() {
	d.mu.Lock()
	// The Linear workspace is kept in a file of its own, written only
	// as it changes (linear_store.go).
	st := savedState{Version: 2, Projects: d.projects, Saved: time.Now().UnixMilli()}
	lin, linData := d.linearToSaveLocked()
	for _, s := range d.sessions {
		ss := savedSession{ID: s.id, Project: s.project, Name: s.name, AgentNamed: s.agentNamed, Created: s.created, Layout: s.layout, Focus: s.focus,
			Unlinked: slices.Sorted(maps.Keys(s.unlinked))}
		for _, p := range s.panes {
			p.mu.Lock()
			sp := savedPane{ID: p.info.ID, View: p.info.View, Target: p.info.Target, Dir: p.info.Dir, Command: p.info.Command,
				Agent: p.info.AgentSession, Task: p.info.TaskID, Created: p.info.Created, Cols: p.info.Cols, Rows: p.info.Rows,
				Status: p.info.Status, Since: p.info.Since, Unread: p.info.Unread, Summary: paneAttention(p).Msg}
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

// restore starts again the sessions of the last daemon, which ended
// without closing them.
func (d *Daemon) restore() {
	lin := d.loadLinear()
	d.linear, d.linearSaved = lin, lin
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		return
	}
	var st savedState
	if json.Unmarshal(data, &st) != nil {
		return
	}
	// A state file of before keeps the workspace in it: it is taken
	// from there, and moves to its own file with the next save.
	if d.linear = newerLinear(lin, st.Linear); d.linear != lin {
		d.linearSaved = nil
	}
	d.tasks = st.Tasks
	set := settings.Load()
	// The conversations of the last daemon end, but those of the panes
	// restored with their agents resumed, and the stand-ins of the
	// sessions restored: none runs any more.
	resumed, sessions := map[string]bool{}, map[string]bool{}
	defer func() {
		d.taskEndWhere(func(r proto.TaskRun) bool {
			return !(r.Pane != "" && resumed[r.Pane] || r.Pane == "" && sessions[r.Session])
		})
	}()
	if !set.RestoreSessions {
		// The projects stay; the sessions of the last daemon do not.
		d.projects = st.Projects
		return
	}
	d.projects = st.Projects
	when := time.UnixMilli(st.Saved).Format("Mon Jan 2 15:04")
	for _, ss := range st.Sessions {
		s := &session{id: ss.ID, project: ss.Project, name: ss.Name, agentNamed: ss.AgentNamed, created: ss.Created, restored: true, focus: ss.Focus}
		for _, t := range ss.Unlinked {
			if s.unlinked == nil {
				s.unlinked = map[string]bool{}
			}
			s.unlinked[t] = true
		}
		for _, sp := range ss.Panes {
			if !set.ResumeAgents {
				sp.Agent = nil
			}
			if p := d.restorePane(s.id, sp, when, st.Saved); p != nil {
				s.panes = append(s.panes, p)
				d.panes[p.info.ID] = p
				resumed[p.info.ID] = sp.Agent != nil
			}
		}
		if len(s.panes) == 0 {
			continue
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
		d.sessions = append(d.sessions, s)
		sessions[s.id] = true
		d.log.Printf("session %s restored, %d panes", s.id, len(s.panes))
	}
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
	p.summary = sp.Summary
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
	if err := p.start(resumeCommand(sp.Agent), nil); err != nil {
		d.log.Printf("restoring %s: %v", sp.ID, err)
		p.term.Close()
		return nil
	}
	p.dirty = true
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
