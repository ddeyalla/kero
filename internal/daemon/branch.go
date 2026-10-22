package daemon

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// A session an agent works in, on a git branch named after an issue, as
// Linear names them (dv/bru-25-fix-login), is on that issue: the daemon
// links it, for the harnesses that don't run kero task start, and for
// the agents that forget. Only a session on no task yet, and never back
// to an issue the user unlinked it from.

// The daemon reads the branches of those sessions' repositories every
// branchEvery, when it is told something may have moved (branchWatch.due:
// the user focused a pane or looked at a session, or a pane's agent or
// directory changed), and as the HEAD file it read last changes, which
// the tick checks every 1.6 s with a stat each: a branch switched shows
// within 2 s, and an idle daemon reads no repository.
const branchEvery = 30 * time.Second

// branchWatch is what the tick knows of the branches; only the tick's
// goroutine uses it, but due, which others set.
type branchWatch struct {
	due   atomic.Bool
	at    time.Time            // the last full read
	heads map[string]time.Time // the HEAD files read then, with their times
}

// branchTick reads the branches again when due, or when a HEAD file
// changed since.
func (d *Daemon) branchTick(now time.Time) {
	w := &d.branch
	read := w.due.Swap(false) || now.Sub(w.at) >= branchEvery
	for path, mod := range w.heads {
		if read {
			break
		}
		fi, err := os.Stat(path)
		read = err != nil || !fi.ModTime().Equal(mod)
	}
	if read {
		w.at = now
		w.heads = d.linkByBranch()
	}
}

// linkByBranch links the sessions an agent works in, on no task, whose
// agent's branch names an issue of the workspace. It returns the HEAD
// files it read, with their times, to tell when to read them again.
func (d *Daemon) linkByBranch() map[string]time.Time {
	type cand struct{ sid, dir string }
	var cands []cand
	d.mu.Lock()
	if d.linear == nil || d.linear.Team == "" {
		d.mu.Unlock()
		return nil
	}
	team := d.linear.Team
	byIdent := make(map[string]string, len(d.linear.Issues))
	for _, i := range d.linear.Issues {
		byIdent[strings.ToUpper(i.Identifier)] = i.ID
	}
	skip := map[string]map[string]bool{}
	for _, s := range d.sessions {
		linked, dir := false, ""
		for _, p := range s.panes {
			p.mu.Lock()
			if p.info.TaskID != "" {
				linked = true
			}
			if p.info.Agent != "" && dir == "" {
				dir = p.info.Dir
				if p.info.ProcessDir != "" {
					dir = p.info.ProcessDir
				}
			}
			p.mu.Unlock()
		}
		if !linked && dir != "" {
			cands = append(cands, cand{s.id, dir})
			skip[s.id] = maps.Clone(s.unlinked)
		}
	}
	d.mu.Unlock()
	heads := map[string]time.Time{}
	for _, c := range cands {
		head := headOf(c.dir)
		if fi, err := os.Stat(head); err == nil {
			heads[head] = fi.ModTime()
		}
		ident := issueInBranch(branchIn(head), team)
		id := byIdent[ident]
		if id == "" || skip[c.sid][id] {
			continue
		}
		if err := d.linkSession(c.sid, id, "branch"); err == nil {
			d.log.Printf("session %s linked to %s by its branch", c.sid, ident)
		}
	}
	return heads
}

// issueInBranch is the issue identifier a branch names, as BRU-25 for
// dv/bru-25-fix-login, of the team's key; "" none.
func issueInBranch(branch, team string) string {
	if branch == "" || team == "" {
		return ""
	}
	re := regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(` + regexp.QuoteMeta(team) + `)-([0-9]+)(?:[^0-9]|$)`)
	m := re.FindStringSubmatch(branch)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + strings.TrimLeft(m[2], "0")
}

// branchOf is the branch checked out in the repository dir is in, from
// its HEAD; "" for none, or a detached HEAD.
func branchOf(dir string) string { return branchIn(headOf(dir)) }

// branchIn is the branch a HEAD file names; "" for none, or a detached
// HEAD.
func branchIn(head string) string {
	if head == "" {
		return ""
	}
	b, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	if ref, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/"); ok {
		return ref
	}
	return ""
}

// headOf is the HEAD file of the repository dir is in; "" for none. A
// worktree's .git is a file naming its own git directory.
func headOf(dir string) string {
	for i := 0; i < 32 && dir != ""; i++ {
		gitPath := filepath.Join(dir, ".git")
		if fi, err := os.Stat(gitPath); err == nil {
			gitDir := gitPath
			if !fi.IsDir() {
				b, err := os.ReadFile(gitPath)
				if err != nil {
					return ""
				}
				line := strings.TrimSpace(string(b))
				if !strings.HasPrefix(line, "gitdir:") {
					return ""
				}
				gitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(dir, gitDir)
				}
			}
			return filepath.Join(gitDir, "HEAD")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}
