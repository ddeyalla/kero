package daemon

import (
	"slices"
	"time"

	"kero/internal/proto"
)

// The daemon keeps, for each task, the conversations that worked on it:
// which harness, its own session, in which of kero's sessions and where,
// and from when to when. A link lives on the panes and goes with them;
// the log stays, so that a task's conversation resumes after the agent
// ended, the session closed or the daemon restarted, and so that a
// second conversation, of the same harness or of another, is one more
// run rather than one that replaces the first.
//
// taskMu guards the log. It is taken last: under d.mu, under a pane's
// mu, never the other way.

// maxRuns is how many runs a task keeps, the latest.
const maxRuns = 50

// taskLog is a task's runs, the oldest first.
type taskLog struct {
	Identifier string          `json:"identifier,omitempty"`
	Runs       []proto.TaskRun `json:"runs"`
}

// taskStart notes that r works on task: a conversation already known
// (the same agent's same session) is taken up again where it now runs,
// one whose session the agent's hook now tells fills the run its pane
// had, and a new one is a new run. A run of no agent stands for a linked
// session until an agent starts in it.
func (d *Daemon) taskStart(task string, r proto.TaskRun) {
	if task == "" || r.Session == "" {
		return
	}
	if r.Started == 0 {
		r.Started = time.Now().UnixMilli()
	}
	d.taskMu.Lock()
	defer d.taskMu.Unlock()
	if d.tasks == nil {
		d.tasks = map[string]*taskLog{}
	}
	tl := d.tasks[task]
	if tl == nil {
		tl = &taskLog{}
		d.tasks[task] = tl
	}
	open := func(o proto.TaskRun) bool { return o.Ended == 0 }
	if r.Via == "" {
		// A conversation in a session already on the task was linked as
		// the session was.
		for i := len(tl.Runs) - 1; i >= 0; i-- {
			if tl.Runs[i].Session == r.Session {
				r.Via = tl.Runs[i].Via
				break
			}
		}
	}
	switch {
	case r.ID != "":
		if slices.ContainsFunc(tl.Runs, func(o proto.TaskRun) bool { return o.Agent == r.Agent && o.ID == r.ID }) {
			// The conversation taken up again, as by Resume: the run its
			// pane began before the hook told the session, and the
			// session's stand-in, were this one all along.
			tl.Runs = slices.DeleteFunc(tl.Runs, func(o proto.TaskRun) bool {
				return open(o) && o.ID == "" && (o.Pane == r.Pane && o.Agent == r.Agent || o.Agent == "" && o.Session == r.Session)
			})
			i := slices.IndexFunc(tl.Runs, func(o proto.TaskRun) bool { return o.Agent == r.Agent && o.ID == r.ID })
			o := &tl.Runs[i]
			o.Session, o.Pane, o.Ended = r.Session, r.Pane, 0
			if r.Dir != "" {
				o.Dir = r.Dir
			}
			if r.SessionName != "" {
				o.SessionName = r.SessionName
			}
			d.endOthersLocked(tl, r.Pane, i)
			d.save()
			return
		}
		if i := slices.IndexFunc(tl.Runs, func(o proto.TaskRun) bool {
			return open(o) && o.Pane == r.Pane && o.Agent == r.Agent && o.ID == ""
		}); i >= 0 {
			o := &tl.Runs[i]
			o.ID = r.ID
			if r.Dir != "" {
				o.Dir = r.Dir
			}
			d.endOthersLocked(tl, r.Pane, i)
			d.save()
			return
		}
	case r.Agent != "":
		if slices.ContainsFunc(tl.Runs, func(o proto.TaskRun) bool { return open(o) && o.Pane == r.Pane && o.Agent == r.Agent }) {
			return
		}
	default:
		if slices.ContainsFunc(tl.Runs, func(o proto.TaskRun) bool { return open(o) && o.Session == r.Session }) {
			return
		}
	}
	// A new conversation: the one the pane ran before it ended, and the
	// session's stand-in of no agent goes.
	now := time.Now().UnixMilli()
	for i := range tl.Runs {
		o := &tl.Runs[i]
		if open(*o) && r.Pane != "" && o.Pane == r.Pane {
			o.Ended = now
		}
	}
	if r.Agent != "" {
		tl.Runs = slices.DeleteFunc(tl.Runs, func(o proto.TaskRun) bool {
			return open(o) && o.Agent == "" && o.Session == r.Session
		})
	}
	tl.Runs = append(tl.Runs, r)
	if len(tl.Runs) > maxRuns {
		tl.Runs = slices.Delete(tl.Runs, 0, len(tl.Runs)-maxRuns)
	}
	d.save()
}

// endOthersLocked ends the runs open in pane but the one at keep: a pane
// runs one conversation at a time.
func (d *Daemon) endOthersLocked(tl *taskLog, pane string, keep int) {
	if pane == "" {
		return
	}
	now := time.Now().UnixMilli()
	for i := range tl.Runs {
		if i != keep && tl.Runs[i].Pane == pane && tl.Runs[i].Ended == 0 {
			tl.Runs[i].Ended = now
		}
	}
}

// taskEnd ends the runs open in a pane, of any task: its agent ended, or
// the pane closed.
func (d *Daemon) taskEnd(pane string) {
	d.taskEndWhere(func(r proto.TaskRun) bool { return r.Pane == pane })
}

// taskEndSession ends the runs open in a session, of task, or of any
// task when task is "": it was unlinked, or it closed.
func (d *Daemon) taskEndSession(sid, task string) {
	d.taskMu.Lock()
	logs := d.tasks
	if task != "" {
		logs = map[string]*taskLog{task: d.tasks[task]}
	}
	changed := endRuns(logs, func(r proto.TaskRun) bool { return r.Session == sid })
	d.taskMu.Unlock()
	if changed {
		d.save()
	}
}

func (d *Daemon) taskEndWhere(match func(proto.TaskRun) bool) {
	d.taskMu.Lock()
	changed := endRuns(d.tasks, match)
	d.taskMu.Unlock()
	if changed {
		d.save()
	}
}

func endRuns(logs map[string]*taskLog, match func(proto.TaskRun) bool) bool {
	now := time.Now().UnixMilli()
	changed := false
	for _, tl := range logs {
		if tl == nil {
			continue
		}
		for i := range tl.Runs {
			if r := &tl.Runs[i]; r.Ended == 0 && match(*r) {
				r.Ended = now
				changed = true
			}
		}
	}
	return changed
}

// taskRuns are the runs of a task, by its id or its identifier, the
// latest first, named as their sessions now are.
func (d *Daemon) taskRuns(id, identifier string, names map[string]string) []proto.TaskRun {
	d.taskMu.Lock()
	defer d.taskMu.Unlock()
	var out []proto.TaskRun
	for key, tl := range d.tasks {
		if key == id || key == identifier || (tl.Identifier != "" && tl.Identifier == identifier) {
			out = append(out, tl.Runs...)
		}
	}
	for i := range out {
		if n, ok := names[out[i].Session]; ok && n != "" {
			out[i].SessionName = n
		}
	}
	slices.SortStableFunc(out, func(a, b proto.TaskRun) int {
		switch {
		case a.Started > b.Started:
			return -1
		case a.Started < b.Started:
			return 1
		}
		return 0
	})
	return out
}

// lastResume is the latest run of a task that resumes, for an issue
// whose sessions run no agent now: what its card offers.
func lastResume(runs []proto.TaskRun) (proto.TaskRun, bool) {
	for _, r := range runs {
		if proto.ResumeCommand(r.Agent, r.ID) != nil {
			return r, true
		}
	}
	return proto.TaskRun{}, false
}

// nameTask notes a task's identifier, which a person reads, under its id.
func (d *Daemon) nameTask(id, identifier string) {
	d.taskMu.Lock()
	if tl := d.tasks[id]; tl != nil && tl.Identifier != identifier {
		tl.Identifier = identifier
	}
	d.taskMu.Unlock()
}
