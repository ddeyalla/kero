package daemon

import "time"

// A pane's tick, every 400 ms, follows its foreground program. Asking the
// kernel for the program's name (kern.proc.pid) and its arguments
// (kern.procargs2) every time cost two sysctls a tick per pane for
// nothing: a foreground process group keeps its program. So the pane
// reads them as the group changes (TIOCGPGRP, which it asks every tick),
// and again only while the group settles, as env or a shim hands over to
// the program with exec, and once in a while after, for a program that
// execs another later.
const (
	fgSettle  = 2 * time.Second  // the group's first moments, read every tick
	fgRecheck = 10 * time.Second // then its name, this often
)

// The directory of the shell, and of its foreground program, is read this
// often for the pane the user is in, and seldom for the others, whose
// directory shows only in the sidebar. A pane reads it at once as its
// foreground group changes, as when a command ends.
func cwdEvery(focused bool) time.Duration {
	if focused {
		return 1500 * time.Millisecond
	}
	return 10 * time.Second
}

// The reads of the kernel, which tests count.
var (
	readName = procName
	readArgs = procArgs
	readCwd  = procCwd
)

// fgProgram is a pane's foreground program as last read.
type fgProgram struct {
	pg         int
	name       string
	args       []string
	seen, read time.Time // the group first seen, its name last read
}

// foreground returns the name and the arguments of the program of the
// foreground group pg; s.mu is held. The arguments are read with a name
// that is new, and only when the name alone is not an agent's: they tell
// the agent a runtime or a shell runs (node …/codex.js, bash …/claude),
// and what script it is.
func (s *pane) foreground(pg int, now time.Time) (string, []string) {
	f := &s.fg
	if pg != f.pg {
		*f = fgProgram{pg: pg, seen: now}
	} else if now.Sub(f.seen) >= fgSettle && now.Sub(f.read) < fgRecheck {
		return f.name, f.args
	}
	f.read = now
	name := readName(pg)
	if name != f.name {
		f.name, f.args = name, nil
		if name != "" && detectAgent(name, nil) == "" {
			f.args = readArgs(pg)
		}
	}
	return f.name, f.args
}

// focusedPaneLocked is the pane the user is in: the focus of the session
// the app shows, nil when it shows none or is not in front; d.mu is held.
func (d *Daemon) focusedPaneLocked() *pane {
	id := d.viewedID()
	for _, s := range d.sessions {
		if s.id != id || id == "" {
			continue
		}
		for _, p := range s.panes {
			if p.info.ID == s.focus {
				return p
			}
		}
	}
	return nil
}
