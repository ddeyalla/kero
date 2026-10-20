package daemon

import (
	"encoding/json"
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
	Version  int                `json:"version"`
	Projects []proto.Project    `json:"projects"`
	Sessions []savedSession     `json:"sessions"`
	Saved    int64              `json:"saved"`
	Linear   *proto.LinearState `json:"linear,omitempty"`
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
}

func (d *Daemon) statePath() string { return filepath.Join(d.dir, "state.json") }

// historyPath is where a pane's history is kept, for a restore.
func (d *Daemon) historyPath(id string) string { return filepath.Join(d.dir, "sessions", id+".vt") }

func (d *Daemon) saveLoop() {
	snaps := time.NewTicker(10 * time.Second)
	defer snaps.Stop()
	for {
		select {
		case <-d.saveCh:
			time.Sleep(300 * time.Millisecond)
			d.saveState()
		case <-snaps.C:
			d.saveHistories(false)
		case <-d.quit:
			return
		}
	}
}

func (d *Daemon) saveState() {
	d.mu.Lock()
	st := savedState{Version: 2, Projects: d.projects, Saved: time.Now().UnixMilli(), Linear: d.linear}
	for _, s := range d.sessions {
		ss := savedSession{ID: s.id, Project: s.project, Name: s.name, AgentNamed: s.agentNamed, Created: s.created, Layout: s.layout, Focus: s.focus,
			Unlinked: slices.Sorted(maps.Keys(s.unlinked))}
		for _, p := range s.panes {
			p.mu.Lock()
			ss.Panes = append(ss.Panes, savedPane{ID: p.info.ID, View: p.info.View, Target: p.info.Target, Dir: p.info.Dir, Command: p.info.Command,
				Agent: p.info.AgentSession, Task: p.info.TaskID, Created: p.info.Created, Cols: p.info.Cols, Rows: p.info.Rows})
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
	writeAtomic(d.statePath(), data)
}

// saveHistories writes the panes' histories that changed, or all.
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
	data, err := os.ReadFile(d.statePath())
	if err != nil {
		return
	}
	var st savedState
	if json.Unmarshal(data, &st) != nil {
		return
	}
	d.linear = st.Linear
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
			if p := d.restorePane(s.id, sp, when); p != nil {
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

// restorePane starts a pane of the last daemon again, under its
// scrollback, in its directory, with the agent it ran resumed.
func (d *Daemon) restorePane(sid string, sp savedPane, when string) *pane {
	now := time.Now().UnixMilli()
	if sp.View != "" {
		// A view comes back as it was: nothing ran in it.
		p, err := newPane(d, sid, proto.Pane{ID: sp.ID, View: sp.View, Target: sp.Target, Dir: sp.Dir, Status: proto.Idle, Since: now,
			Created: sp.Created, Cols: sp.Cols, Rows: sp.Rows, TaskID: sp.Task})
		if err != nil {
			d.log.Printf("restoring %s: %v", sp.ID, err)
			return nil
		}
		return p
	}
	p, err := newPane(d, sid, proto.Pane{
		ID: sp.ID, Dir: sp.Dir, Command: sp.Command, Status: proto.Idle, Since: now,
		Created: sp.Created, Cols: sp.Cols, Rows: sp.Rows, Restored: true, AgentSession: sp.Agent,
		TaskID: sp.Task,
	})
	if err != nil {
		d.log.Printf("restoring %s: %v", sp.ID, err)
		return nil
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
