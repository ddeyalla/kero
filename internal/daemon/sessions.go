package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"kero/internal/proto"
)

// session is a row of the sidebar: a task's panes, in a layout. d.mu
// guards its fields.
type session struct {
	id, project, name string
	agentNamed        bool // name is an agent's (proto.Session.AgentNamed)
	created           int64
	restored          bool
	panes             []*pane
	layout            *proto.Layout
	focus, zoomed     string
	// unlinked are the issues the user unlinked the session from, by
	// id, which its branch does not link it to again.
	unlinked map[string]bool
}

// state returns what the subscribers see.
func (d *Daemon) state() proto.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := proto.State{
		Projects: slices.Clone(d.projects),
		Sessions: make([]proto.Session, 0, len(d.sessions)),
		Viewed:   d.viewedID(),
		Started:  d.started.UnixMilli(),
	}
	for _, s := range d.sessions {
		st.Sessions = append(st.Sessions, d.sessionInfo(s))
	}
	if d.linear != nil {
		lin := *d.linear
		lin.Issues = d.linearIssues()
		st.Linear = &lin
	}
	return st
}

// linearIssues is the workspace's issues, each with the session working
// on it, when one is, and the conversation that resumes it: the live
// agent's, or, when none runs, the latest one logged. d.mu is held.
func (d *Daemon) linearIssues() []proto.LinearIssue {
	if d.linear == nil {
		return nil
	}
	out := make([]proto.LinearIssue, len(d.linear.Issues))
	copy(out, d.linear.Issues)
	// A task is named by the identifier a person reads, BRU-25, and by
	// the id the API uses; both find the issue.
	byTask := make(map[string]int, len(out)*2)
	for i, is := range out {
		byTask[is.ID] = i
		byTask[is.Identifier] = i
	}
	for task, link := range d.taskLinks() {
		at, ok := byTask[task]
		if !ok {
			continue
		}
		out[at].Session = link.Session
		out[at].SessionName = link.Name
		out[at].ResumeID = link.ResumeID
		out[at].Progress = link.Progress
		out[at].SessionStatus = link.Status
		out[at].Sessions = link.Sessions
	}
	d.taskMu.Lock()
	for i := range out {
		if out[i].ResumeID != "" {
			continue
		}
		for _, key := range []string{out[i].ID, out[i].Identifier} {
			if tl := d.tasks[key]; tl != nil {
				for j := len(tl.Runs) - 1; j >= 0; j-- {
					if r := tl.Runs[j]; proto.ResumeCommand(r.Agent, r.ID) != nil {
						out[i].ResumeID = r.ID
						break
					}
				}
			}
		}
	}
	d.taskMu.Unlock()
	return out
}

// taskLink is a session working on a task.
type taskLink struct {
	Session  string
	Name     string
	ResumeID string
	Progress int
	Status   proto.Status
	Since    int64
	// rank is how much the pane tells of the work: an agent with its
	// session over an agent over any other program.
	rank int
	// Sessions is how many sessions work on the task.
	Sessions int
}

// statusWeight orders sessions on one task: the one that needs the user
// first, then one at work.
func statusWeight(s proto.Status) int {
	switch s {
	case proto.Waiting:
		return 4
	case proto.Failed:
		return 3
	case proto.Working:
		return 2
	case proto.Idle:
		return 1
	}
	return 0
}

// taskLinks are the tasks the sessions work on, by the issue's id. A
// session speaks for a task through its agent's pane, not through the
// shell split beside it; of two sessions on one task, the one that
// most needs the user does. d.mu is held.
func (d *Daemon) taskLinks() map[string]taskLink {
	out := map[string]taskLink{}
	for _, s := range d.sessions {
		mine := map[string]taskLink{}
		keep := func(task string, l taskLink) {
			if cur, ok := mine[task]; !ok || l.rank > cur.rank {
				mine[task] = l
			}
		}
		for _, p := range s.panes {
			p.mu.Lock()
			info := p.info
			records := slices.Clone(info.Records)
			p.mu.Unlock()
			link := taskLink{Session: s.id, Name: s.name, Progress: info.Progress, Status: info.Status, Since: info.Since}
			if link.Name == "" {
				link.Name = info.Agent
			}
			switch {
			case info.AgentSession != nil:
				link.ResumeID, link.rank = info.AgentSession.ID, 3
			case info.Agent != "":
				link.rank = 2
			case info.Process != "":
				link.rank = 1
			}
			if info.TaskID != "" {
				keep(info.TaskID, link)
			}
			for _, r := range records {
				if r.App != "linear" || r.ID == "" {
					continue
				}
				issue, _, _ := strings.Cut(r.ID, "/")
				report := link
				report.Progress = r.Progress
				report.rank++ // a program that reports on the task speaks for it
				keep(issue, report)
			}
		}
		for task, l := range mine {
			cur, ok := out[task]
			l.Sessions = cur.Sessions + 1
			if !ok || statusWeight(l.Status) > statusWeight(cur.Status) ||
				statusWeight(l.Status) == statusWeight(cur.Status) && (l.rank > cur.rank || l.rank == cur.rank && l.Since > cur.Since) {
				out[task] = l
			} else {
				cur.Sessions = l.Sessions
				out[task] = cur
			}
		}
	}
	return out
}

// sessionInfo describes a session; d.mu is held.
func (d *Daemon) sessionInfo(s *session) proto.Session {
	info := proto.Session{
		ID: s.id, Project: s.project, Name: s.name, AgentNamed: s.agentNamed,
		Layout: s.layout.Clone(), Focus: s.focus, Zoomed: s.zoomed,
		Panes: make([]proto.Pane, 0, len(s.panes)),
	}
	// Panes in the layout's order: left to right, top to bottom.
	order := s.layout.Panes()
	panes := slices.Clone(s.panes)
	slices.SortStableFunc(panes, func(a, b *pane) int {
		return slices.Index(order, a.info.ID) - slices.Index(order, b.info.ID)
	})
	for _, p := range panes {
		p.mu.Lock()
		pi := p.info
		pi.Command = slices.Clone(pi.Command)
		pi.Records = slices.Clone(pi.Records)
		p.mu.Unlock()
		info.Panes = append(info.Panes, pi)
	}
	if i := proto.Lead(info.Panes, s.focus); i >= 0 {
		info.Pane = info.Panes[i]
		info.Lead = info.Panes[i].ID
	}
	info.Attention = protoAttention(sessionAttention(s))
	info.Created, info.Restored = s.created, s.restored
	return info
}

func (d *Daemon) findPaneLocked(id string) *pane {
	if p := d.panes[id]; p != nil {
		return p
	}
	return nil
}

// findSessionLocked finds a session by id, a unique prefix of it, its
// name, or one of its panes; d.mu is held.
func (d *Daemon) findSessionLocked(ref string) *session {
	if ref == "" {
		return nil
	}
	for _, s := range d.sessions {
		if s.id == ref {
			return s
		}
	}
	if p := d.panes[ref]; p != nil {
		return d.sessionOfLocked(p)
	}
	var found *session
	for _, s := range d.sessions {
		if strings.HasPrefix(s.id, ref) || s.name != "" && strings.EqualFold(s.name, ref) {
			if found != nil && found != s {
				return nil
			}
			found = s
		}
	}
	return found
}

func (d *Daemon) sessionOfLocked(p *pane) *session {
	for _, s := range d.sessions {
		if s.id == p.sid {
			return s
		}
	}
	return nil
}

// findPane finds a pane by its id, or a session's focused pane.
func (d *Daemon) findPane(ref string) *pane {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p := d.panes[ref]; p != nil {
		return p
	}
	// A unique prefix of a pane's id.
	var found *pane
	for id, p := range d.panes {
		if ref != "" && strings.HasPrefix(id, ref) {
			if found != nil {
				found = nil
				break
			}
			found = p
		}
	}
	if found != nil {
		return found
	}
	if s := d.findSessionLocked(ref); s != nil {
		return focusedLocked(s)
	}
	return nil
}

// agentPane finds the pane an agent's hook tells of, from the hook's
// process pid: the pane whose shell runs it. An agent's service that
// panes share, as Codex's app server, runs its hooks under no pane's
// shell, and with the environment of the pane that started it, which
// names no pane to trust: then the pane is the one that can only be
// the agent's, its only pane yet to tell a session (or, of several, the
// only one in the agent's dir), else its only pane; away tells it.
func (d *Daemon) agentPane(pid int, agent, dir string) (found *pane, away bool) {
	d.mu.Lock()
	panes := make([]*pane, 0, len(d.panes))
	for _, p := range d.panes {
		panes = append(panes, p)
	}
	d.mu.Unlock()
	byShell := make(map[int]*pane, len(panes))
	var running, pending, inDir []*pane
	for _, p := range panes {
		p.mu.Lock()
		if p.info.Status != proto.Exited {
			if p.shellPID > 0 {
				byShell[p.shellPID] = p
			}
			if p.info.Agent == agent {
				running = append(running, p)
				if p.info.AgentSession == nil {
					pending = append(pending, p)
					if dir != "" && p.info.WorkDir() == dir {
						inDir = append(inDir, p)
					}
				}
			}
		}
		p.mu.Unlock()
	}
	for i := 0; i < 64 && pid > 1; i++ {
		if p := byShell[pid]; p != nil {
			return p, false
		}
		pid = procParent(pid)
	}
	switch {
	case len(pending) == 1:
		return pending[0], true
	case len(pending) > 1:
		if len(inDir) == 1 {
			return inDir[0], true
		}
	case len(running) == 1:
		return running[0], true
	}
	return nil, true
}

// focusedLocked returns a session's focused pane, else its first; d.mu is
// held.
// findTermPane finds a terminal: a pane, which must be one, or a
// session, which stands for its focused pane when that is a terminal,
// else for its first in the layout. Views have no terminal to type
// into, read or wait on.
func (d *Daemon) findTermPane(ref string) (*pane, error) {
	p := d.findPane(ref)
	if p == nil {
		return nil, fmt.Errorf("no session or pane %q", ref)
	}
	if !p.isView() {
		return p, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if s := d.findSessionLocked(ref); s != nil && d.panes[ref] == nil {
		for _, id := range s.layout.Panes() {
			for _, o := range s.panes {
				if o.info.ID == id && !o.isView() {
					return o, nil
				}
			}
		}
		for _, o := range s.panes {
			if !o.isView() {
				return o, nil
			}
		}
	}
	return nil, fmt.Errorf("pane %s is a %s view, not a terminal", p.info.ID, p.info.View)
}

func focusedLocked(s *session) *pane {
	for _, p := range s.panes {
		if p.info.ID == s.focus {
			return p
		}
	}
	if order := s.layout.Panes(); len(order) > 0 {
		for _, p := range s.panes {
			if p.info.ID == order[0] {
				return p
			}
		}
	}
	if len(s.panes) > 0 {
		return s.panes[0]
	}
	return nil
}

func (d *Daemon) cleanDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	dir = expandHome(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("no directory %s", dir)
	}
	return dir, nil
}

// startPane makes a pane and starts its shell; a view's runs nothing.
func (d *Daemon) startPane(sid string, p proto.CreateParams, dir string) (*pane, error) {
	// The built-in Tasks view is gone: an older client or script that asks
	// for one gets the Linear plugin's pane, as the saved panes of the view
	// became (migrateState).
	if p.View == proto.PaneTasks {
		p.View, p.Target = proto.PanePlugins, linearPluginTarget()
	}
	if p.View != "" && !slices.Contains(proto.PaneViews, p.View) {
		return nil, fmt.Errorf("no view %q", p.View)
	}
	now := time.Now().UnixMilli()
	info := proto.Pane{
		ID: newID(), View: p.View, Target: p.Target, Dir: dir, Command: p.Command, Status: proto.Idle, Since: now, Created: now,
		Cols: p.Cols, Rows: p.Rows,
	}
	if p.View != "" {
		info.Command = nil
	}
	pn, err := newPane(d, sid, info)
	if err != nil {
		return nil, err
	}
	if pn.isView() {
		return pn, nil
	}
	if err := pn.start(p.Command, p.Env); err != nil {
		pn.term.Close()
		return nil, err
	}
	return pn, nil
}

// create starts a session, or a pane of one with p.Split.
func (d *Daemon) create(p proto.CreateParams) (proto.Session, error) {
	if p.Split != "" {
		return d.split(p)
	}
	dir, err := d.cleanDir(p.Dir)
	if err != nil {
		return proto.Session{}, err
	}
	if p.Task != "" {
		// An issue that is not one fails the session before it starts.
		if p.Task, _, err = d.resolveIssue(p.Task); err != nil {
			return proto.Session{}, err
		}
	}
	d.mu.Lock()
	project := d.projectFor(p.Project, dir)
	if dir == "" {
		for _, pr := range d.projects {
			if pr.ID == project {
				dir = pr.Dir
			}
		}
	}
	d.mu.Unlock()
	s := &session{id: newID(), project: project, name: strings.TrimSpace(p.Name), created: time.Now().UnixMilli()}
	pn, err := d.startPane(s.id, p, dir)
	if err != nil {
		return proto.Session{}, err
	}
	s.panes, s.focus = []*pane{pn}, pn.info.ID
	d.mu.Lock()
	i := len(d.sessions)
	if p.After != "" {
		if after := d.findSessionLocked(p.After); after != nil {
			i = slices.Index(d.sessions, after) + 1
		}
	}
	d.sessions = slices.Insert(d.sessions, i, s)
	d.panes[pn.info.ID] = pn
	info := d.sessionInfo(s)
	d.mu.Unlock()
	if p.Task != "" {
		if err := d.linkSession(s.id, p.Task, "link"); err == nil {
			d.mu.Lock()
			info = d.sessionInfo(s)
			d.mu.Unlock()
		}
	}
	d.changed()
	d.log.Printf("session %s created in %s", s.id, dir)
	return info, nil
}

// split starts a pane beside another, in its session.
func (d *Daemon) split(p proto.CreateParams) (proto.Session, error) {
	d.mu.Lock()
	at := d.findPaneLocked(p.Split)
	var s *session
	if at != nil {
		s = d.sessionOfLocked(at)
	} else if s = d.findSessionLocked(p.Split); s != nil {
		at = focusedLocked(s)
	}
	if s == nil || at == nil {
		d.mu.Unlock()
		return proto.Session{}, fmt.Errorf("no session or pane %q", p.Split)
	}
	dir := at.info.Dir
	if p.View != "" && at.info.ProcessDir != "" {
		// A view of the files the agent beside it works on, in its
		// worktree.
		dir = at.info.ProcessDir
	}
	if p.Cols == 0 {
		// Half of the pane split, as a start: the view sets the size.
		p.Cols, p.Rows = at.info.Cols, at.info.Rows
		if p.Direction == "down" || p.Direction == "up" {
			p.Rows = max(p.Rows/2, 4)
		} else {
			p.Cols = max(p.Cols/2, 10)
		}
	}
	sid, atID := s.id, at.info.ID
	d.mu.Unlock()
	if p.Dir != "" {
		var err error
		if dir, err = d.cleanDir(p.Dir); err != nil {
			return proto.Session{}, err
		}
	}
	pn, err := d.startPane(sid, p, dir)
	if err != nil {
		return proto.Session{}, err
	}
	d.mu.Lock()
	if !slices.Contains(d.sessions, s) {
		d.mu.Unlock()
		pn.close()
		return proto.Session{}, errors.New("the session closed")
	}
	if s.layout == nil {
		s.layout = &proto.Layout{Pane: s.panes[0].info.ID}
	}
	at.mu.Lock()
	pn.mu.Lock()
	pn.info.TaskID = at.info.TaskID
	pn.mu.Unlock()
	at.mu.Unlock()
	s.layout = s.layout.Split(atID, pn.info.ID, p.Direction, p.Ratio)
	s.panes = append(s.panes, pn)
	s.focus, s.zoomed = pn.info.ID, ""
	d.panes[pn.info.ID] = pn
	info := d.sessionInfo(s)
	d.mu.Unlock()
	d.changed()
	d.log.Printf("pane %s split from %s", pn.info.ID, atID)
	return info, nil
}

// replacePane puts a new pane in a pane's place, of another kind, as a
// terminal that becomes the project's tasks: p.Split is the pane, and
// p.View, p.Target and p.Command the new one's. The old one closes, and
// what ran in it ends.
func (d *Daemon) replacePane(p proto.CreateParams) (proto.Session, error) {
	d.mu.Lock()
	at := d.panes[p.Split]
	var s *session
	if at != nil {
		s = d.sessionOfLocked(at)
	}
	if s == nil {
		d.mu.Unlock()
		return proto.Session{}, fmt.Errorf("no pane %q", p.Split)
	}
	at.mu.Lock()
	dir, task := at.info.Dir, at.info.TaskID
	if at.info.ProcessDir != "" {
		dir = at.info.ProcessDir
	}
	if p.Cols == 0 {
		p.Cols, p.Rows = at.info.Cols, at.info.Rows
	}
	at.mu.Unlock()
	sid, atID := s.id, at.info.ID
	d.mu.Unlock()
	pn, err := d.startPane(sid, p, dir)
	if err != nil {
		return proto.Session{}, err
	}
	pn.info.TaskID = task
	d.mu.Lock()
	if !slices.Contains(d.sessions, s) || d.panes[atID] == nil {
		d.mu.Unlock()
		pn.close()
		return proto.Session{}, errors.New("the pane closed")
	}
	delete(d.panes, atID)
	d.panes[pn.info.ID] = pn
	for i, o := range s.panes {
		if o == at {
			s.panes[i] = pn
		}
	}
	if s.layout != nil {
		s.layout = s.layout.Replace(atID, pn.info.ID)
	}
	if s.focus == atID {
		s.focus = pn.info.ID
	}
	if s.zoomed == atID {
		s.zoomed = pn.info.ID
	}
	info := d.sessionInfo(s)
	d.mu.Unlock()
	at.close()
	d.taskEnd(atID)
	os.Remove(d.historyPath(atID))
	d.changed()
	d.log.Printf("pane %s replaced by %s", atID, pn.info.ID)
	return info, nil
}

// removePane closes a pane; its session goes with its last pane.
func (d *Daemon) removePane(id string) error {
	d.mu.Lock()
	p := d.panes[id]
	if p == nil {
		d.mu.Unlock()
		return fmt.Errorf("no pane %s", id)
	}
	delete(d.panes, id)
	s := d.sessionOfLocked(p)
	if s != nil {
		s.panes = slices.DeleteFunc(s.panes, func(o *pane) bool { return o == p })
		if s.layout != nil {
			// The focus goes to the neighbor taking the room.
			if s.focus == id {
				for _, dir := range []string{"left", "up", "right", "down"} {
					if n := s.layout.Neighbor(id, dir); n != "" {
						s.focus = n
						break
					}
				}
			}
			s.layout = s.layout.Remove(id)
			if s.layout.Leaf() {
				s.layout = nil
			}
		}
		if s.zoomed == id {
			s.zoomed = ""
		}
		if len(s.panes) == 0 {
			d.sessions = slices.DeleteFunc(d.sessions, func(o *session) bool { return o == s })
		} else if s.focus == id {
			s.focus = s.panes[0].info.ID
		}
	}
	d.mu.Unlock()
	p.close()
	d.taskEnd(id)
	os.Remove(d.historyPath(id))
	d.changed()
	d.log.Printf("pane %s closed", id)
	return nil
}

// removeSession closes a session and its panes.
func (d *Daemon) removeSession(ref string) error {
	d.mu.Lock()
	s := d.findSessionLocked(ref)
	if s == nil {
		d.mu.Unlock()
		return fmt.Errorf("no session %s", ref)
	}
	d.sessions = slices.DeleteFunc(d.sessions, func(o *session) bool { return o == s })
	panes := s.panes
	for _, p := range panes {
		delete(d.panes, p.info.ID)
	}
	d.mu.Unlock()
	for _, p := range panes {
		p.close()
		os.Remove(d.historyPath(p.info.ID))
	}
	d.taskEndSession(s.id, "")
	d.changed()
	d.log.Printf("session %s closed", s.id)
	return nil
}

func (d *Daemon) move(p proto.MoveParams) error {
	d.mu.Lock()
	defer func() {
		d.mu.Unlock()
		d.changed()
	}()
	s := d.findSessionLocked(p.ID)
	if s == nil {
		return fmt.Errorf("no session %s", p.ID)
	}
	if p.Project != "" {
		s.project = d.projectFor(p.Project, "")
	}
	i := slices.Index(d.sessions, s)
	d.sessions = slices.Delete(d.sessions, i, i+1)
	// Index counts the project's sessions: insert before the one there.
	at, n := len(d.sessions), 0
	for j, o := range d.sessions {
		if o.project != s.project {
			continue
		}
		if p.Index >= 0 && n == p.Index {
			at = j
			break
		}
		n++
		at = j + 1
	}
	d.sessions = slices.Insert(d.sessions, at, s)
	return nil
}

// sameLeaves reports whether a layout shows exactly a session's panes.
func sameLeaves(l *proto.Layout, panes []*pane) bool {
	got := l.Panes()
	if len(got) != len(panes) {
		return false
	}
	for _, p := range panes {
		if !slices.Contains(got, p.info.ID) {
			return false
		}
	}
	return true
}
