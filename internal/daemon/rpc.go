package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"kero/internal/proto"
)

// call runs a control method. Methods taking a pane take a session too,
// for its focused pane.
func (d *Daemon) call(c *controlConn, method string, raw json.RawMessage) (any, error) {
	decode := func(v any) error {
		if len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, v)
	}
	findPane := func(ref string) (*pane, error) {
		if p := d.findPane(ref); p != nil {
			return p, nil
		}
		return nil, fmt.Errorf("no session or pane %q", ref)
	}
	switch method {
	case "state":
		return d.state(), nil

	case "subscribe":
		d.mu.Lock()
		d.subs[c] = struct{}{}
		d.mu.Unlock()
		st := d.broadcastState()
		c.event(proto.Message{Event: "state", State: &st})
		// The workspace is its own event, and this client has none yet: it
		// gets the one the daemon holds, and the others still get the next
		// broadcast, which this does not stand in for.
		d.sendLinearTo(c, d.workspace())
		return true, nil

	case "linear.state":
		return d.linearState(), nil

	case "linear.refresh":
		go d.linearRead(true)
		return true, nil

	case "linear.issue":
		var p proto.LinearParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.Issue == "" {
			return nil, errors.New("linear.issue: which issue?")
		}
		return d.linearDetail(p.Issue)

	case "linear.setState":
		var p proto.LinearParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.Issue == "" || p.State == "" {
			return nil, errors.New("linear.setState: which issue, and which state?")
		}
		return true, d.linearSetState(p.Issue, p.State)

	case "linear.comment":
		var p proto.LinearParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.Issue == "" || strings.TrimSpace(p.Body) == "" || len(p.Body) > 65536 {
			return nil, errors.New("linear.comment: which issue, and what?")
		}
		return true, d.linearComment(p.Issue, p.Body)

	case "session.link":
		var p proto.LinkParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.Session == "" {
			return nil, errors.New("session.link: which session?")
		}
		if p.Via == "" {
			p.Via = "link"
		}
		return true, d.linkSession(p.Session, p.Task, p.Via)

	case "create", "split":
		var p proto.CreateParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if method == "split" && p.Split == "" {
			return nil, errors.New("split: which pane?")
		}
		return d.create(p)

	case "close":
		var p proto.IDParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return true, d.removeSession(p.ID)

	case "pane.close":
		var p proto.IDParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := findPane(p.ID)
		if err != nil {
			return nil, err
		}
		return true, d.removePane(pn.info.ID)

	case "restart":
		var p proto.IDParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := d.findTermPane(p.ID)
		if err != nil {
			return nil, err
		}
		return true, pn.restart()

	case "rename":
		var p proto.RenameParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		s := d.findSessionLocked(p.ID)
		if s != nil {
			s.name = strings.TrimSpace(p.Name)
			s.agentNamed = false
			if by := d.panes[p.By]; by != nil && s.name != "" {
				by.mu.Lock()
				s.agentNamed = by.info.Agent != "" || by.info.AgentSession != nil
				by.mu.Unlock()
			}
		}
		d.mu.Unlock()
		if s == nil {
			return nil, fmt.Errorf("no session %q", p.ID)
		}
		d.changed()
		return true, nil

	case "move":
		var p proto.MoveParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return true, d.move(p)

	case "focus":
		var p proto.FocusParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		s := d.findSessionLocked(p.Session)
		ok := s != nil && slices.ContainsFunc(s.panes, func(o *pane) bool { return o.info.ID == p.Pane })
		if ok {
			s.focus = p.Pane
			if s.zoomed != "" {
				s.zoomed = p.Pane
			}
		}
		d.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("no pane %q in session %q", p.Pane, p.Session)
		}
		d.branch.due.Store(true)
		d.changed()
		return true, nil

	case "zoom":
		var p proto.FocusParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		s := d.findSessionLocked(p.Session)
		if s != nil {
			if p.Pane == "" || s.zoomed == p.Pane {
				s.zoomed = ""
			} else if slices.ContainsFunc(s.panes, func(o *pane) bool { return o.info.ID == p.Pane }) {
				s.zoomed, s.focus = p.Pane, p.Pane
			}
		}
		d.mu.Unlock()
		if s == nil {
			return nil, fmt.Errorf("no session %q", p.Session)
		}
		d.changed()
		return true, nil

	case "layout":
		var p proto.LayoutParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		s := d.findSessionLocked(p.Session)
		ok := s != nil && sameLeaves(p.Layout, s.panes)
		if ok {
			s.layout = p.Layout
			if s.layout.Leaf() {
				s.layout = nil
			}
		}
		d.mu.Unlock()
		if !ok {
			return nil, errors.New("layout: not the session's panes")
		}
		d.changed()
		return true, nil

	case "send":
		var p proto.SendParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := d.findTermPane(p.ID)
		if err != nil {
			return nil, err
		}
		pn.input([]byte(p.Data))
		return true, nil

	case "read":
		var p proto.ReadParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := d.findTermPane(p.ID)
		if err != nil {
			return nil, err
		}
		return proto.ReadResult{Text: pn.readText(p.Scrollback, p.Lines)}, nil

	case "view":
		var p proto.ViewParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		id := ""
		var panes []*pane
		d.mu.Lock()
		if s := d.findSessionLocked(p.ID); s != nil {
			id, panes = s.id, slices.Clone(s.panes)
		}
		d.mu.Unlock()
		d.viewed.Store(&id)
		d.branch.due.Store(true)
		for _, pn := range panes {
			pn.mu.Lock()
			pn.info.Unread = false
			pn.mu.Unlock()
		}
		d.changed()
		return true, nil

	case "select":
		var p proto.FocusParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		s := d.findSessionLocked(p.Session)
		if s != nil && p.Pane != "" {
			if pn := d.findPaneLocked(p.Pane); pn != nil && pn.sid == s.id {
				s.focus = pn.info.ID
			}
		}
		n := 0
		if s != nil {
			for sub := range d.subs {
				sub.event(proto.Message{Event: "select", Select: s.id})
			}
			n = len(d.subs)
		}
		d.mu.Unlock()
		if s == nil {
			return nil, fmt.Errorf("no session %q", p.Session)
		}
		d.changed()
		return n > 0, nil

	case "do":
		var p proto.DoParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		if ref := p.Args["session"]; p.Action == "session.attention" && ref != "" {
			s := d.findSessionLocked(ref)
			if s == nil {
				d.mu.Unlock()
				return nil, fmt.Errorf("no session %q", ref)
			}
			p.Args["session"] = s.id
		}
		for sub := range d.subs {
			sub.event(proto.Message{Event: "action", Action: p.Action, Args: p.Args})
		}
		n := len(d.subs)
		d.mu.Unlock()
		return n > 0, nil

	case "notify":
		var p proto.NotifyParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := d.findTermPane(p.ID)
		if err != nil {
			return nil, err
		}
		pn.mu.Lock()
		pn.flag(p.Title, p.Body)
		pn.mu.Unlock()
		return true, nil

	case "report":
		var p proto.ReportParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return true, d.hookReport(p)

	case "wait":
		var p proto.WaitParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := d.findTermPane(p.ID)
		if err != nil {
			return nil, err
		}
		return d.wait(c, pn, p)

	case "pane.agentSession":
		var p proto.AgentSessionParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		as := &proto.AgentSession{Agent: p.Agent, ID: p.Session}
		if resumeCommand(as) == nil {
			return nil, fmt.Errorf("no agent session %q of %q", p.Session, p.Agent)
		}
		pn, away := d.agentPane(p.PID, p.Agent, p.Dir)
		if pn == nil {
			return nil, fmt.Errorf("no pane runs %s session %q", p.Agent, p.Session)
		}
		pn.mu.Lock()
		pn.info.AgentSession = as
		id, task := pn.info.ID, pn.info.TaskID
		dir := pn.info.Dir
		if pn.info.ProcessDir != "" {
			dir = pn.info.ProcessDir
		}
		if p.Dir != "" {
			dir = p.Dir
		}
		pn.mu.Unlock()
		d.changed()
		d.mu.Lock()
		sid := pn.sid
		d.mu.Unlock()
		// A conversation that begins on a linked task, of any harness, is
		// logged: one more run, or the one its pane began.
		d.taskStart(task, proto.TaskRun{Agent: p.Agent, ID: p.Session, Session: sid, Pane: id, Dir: dir})
		return proto.AgentSessionResult{Pane: id, Session: sid, Away: away}, nil

	case "project.create":
		var p proto.ProjectParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		dir := expandHome(p.Dir)
		name := strings.TrimSpace(p.Name)
		if name == "" && dir != "" {
			name = filepath.Base(dir)
		}
		if name == "" {
			return nil, errors.New("a project needs a name or a directory")
		}
		d.mu.Lock()
		for _, pr := range d.projects {
			if dir != "" && pr.Dir == dir {
				d.mu.Unlock()
				return pr, nil
			}
		}
		pr := proto.Project{ID: newID(), Name: name, Dir: dir}
		d.projects = append(d.projects, pr)
		d.mu.Unlock()
		d.changed()
		return pr, nil

	case "project.rename":
		var p proto.ProjectParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return nil, errors.New("a project needs a name")
		}
		d.mu.Lock()
		for i := range d.projects {
			if d.projects[i].ID == p.ID {
				d.projects[i].Name = name
			}
		}
		d.mu.Unlock()
		d.changed()
		return true, nil

	case "pane.target":
		var p proto.TargetParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		pn, err := findPane(p.Pane)
		if err != nil {
			return nil, err
		}
		if !pn.isView() {
			return nil, fmt.Errorf("pane %s is a terminal", pn.info.ID)
		}
		pn.mu.Lock()
		pn.info.Target = p.Target
		pn.mu.Unlock()
		d.changed()
		return true, nil

	case "pane.replace":
		var p proto.CreateParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return d.replacePane(p)

	case "project.linear":
		var p proto.ProjectLinearParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return true, d.setProjectLinear(p.Project, p.LinearProject)

	case "project.move":
		var p proto.MoveProjectParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		i := slices.IndexFunc(d.projects, func(pr proto.Project) bool { return pr.ID == p.ID })
		if i >= 0 {
			pr := d.projects[i]
			d.projects = slices.Delete(d.projects, i, i+1)
			j := max(0, min(p.Index, len(d.projects)))
			d.projects = slices.Insert(d.projects, j, pr)
		}
		d.mu.Unlock()
		d.changed()
		return true, nil

	case "project.close":
		var p proto.IDParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		d.mu.Lock()
		var ids []string
		for _, s := range d.sessions {
			if s.project == p.ID {
				ids = append(ids, s.id)
			}
		}
		d.projects = slices.DeleteFunc(d.projects, func(pr proto.Project) bool { return pr.ID == p.ID })
		d.mu.Unlock()
		for _, id := range ids {
			d.removeSession(id)
		}
		d.changed()
		return true, nil

	case "shutdown":
		d.mu.Lock()
		d.ending = true
		ids := make([]string, 0, len(d.sessions))
		for _, s := range d.sessions {
			ids = append(ids, s.id)
		}
		d.mu.Unlock()
		for _, id := range ids {
			d.removeSession(id)
		}
		d.saveState()
		go func() {
			time.Sleep(100 * time.Millisecond)
			d.stop()
		}()
		return true, nil
	}
	return nil, fmt.Errorf("unknown method %q", method)
}

// wait waits for a pane's status to be one of p.Status.
func (d *Daemon) wait(c *controlConn, s *pane, p proto.WaitParams) (any, error) {
	want := func(info proto.Pane) bool {
		switch {
		case p.Exit:
			return info.CommandExit != nil || info.Status == proto.Exited
		case len(p.Status) == 0:
			return info.Status != proto.Working
		}
		return slices.Contains(p.Status, info.Status)
	}
	if p.Exit {
		s.mu.Lock()
		unmarked := s.info.CommandExit == nil && !s.awaiting && s.info.Status != proto.Exited
		s.mu.Unlock()
		if unmarked {
			return nil, errors.New("no command to wait for: the pane wasn't started with one, or its shell (cmd) can't tell when it ends")
		}
	}
	var timeout <-chan time.Time
	if p.Timeout > 0 {
		t := time.NewTimer(time.Duration(p.Timeout) * time.Millisecond)
		defer t.Stop()
		timeout = t.C
	}
	// An agent given a prompt takes a moment to start working: waiting
	// for it to stop right away would end at once. Give it a little time
	// to start.
	if len(p.Status) == 0 && !p.Exit {
		s.mu.Lock()
		ch, st, since := s.changed, s.info.Status, s.info.Since
		s.mu.Unlock()
		if st != proto.Working && time.Since(time.UnixMilli(since)) > time.Second {
			select {
			case <-ch:
			case <-time.After(1500 * time.Millisecond):
			case <-c.done:
				return nil, errors.New("canceled")
			}
		}
	}
	for {
		s.mu.Lock()
		info, ch, closed := s.info, s.changed, s.closed
		s.mu.Unlock()
		if closed {
			return info, errors.New("pane closed")
		}
		if want(info) {
			return info, nil
		}
		select {
		case <-ch:
		case <-timeout:
			return info, errors.New("timed out")
		case <-c.done:
			return nil, errors.New("canceled")
		case <-d.quit:
			return nil, errors.New("kerod exited")
		}
	}
}
