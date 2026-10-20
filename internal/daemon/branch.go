package daemon

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// A session an agent works in, on a git branch named after an issue, as
// Linear names them (dv/bru-25-fix-login), is on that issue: the daemon
// links it, for the harnesses that don't run kero task start, and for
// the agents that forget. Only a session on no task yet, and never back
// to an issue the user unlinked it from.

const branchEvery = 5 * time.Second

func (d *Daemon) branchLoop() {
	t := time.NewTicker(branchEvery)
	defer t.Stop()
	for {
		select {
		case <-d.quit:
			return
		case <-t.C:
			d.linkByBranch()
		}
	}
}

// linkByBranch links the sessions an agent works in, on no task, whose
// agent's branch names an issue of the workspace.
func (d *Daemon) linkByBranch() {
	type cand struct{ sid, dir string }
	var cands []cand
	d.mu.Lock()
	if d.linear == nil || d.linear.Team == "" {
		d.mu.Unlock()
		return
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
	for _, c := range cands {
		ident := issueInBranch(branchOf(c.dir), team)
		id := byIdent[ident]
		if id == "" || skip[c.sid][id] {
			continue
		}
		if err := d.linkSession(c.sid, id, "branch"); err == nil {
			d.log.Printf("session %s linked to %s by its branch", c.sid, ident)
		}
	}
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
// its HEAD; "" for none, or a detached HEAD. A worktree's .git is a file
// naming its own git directory.
func branchOf(dir string) string {
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
			b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil {
				return ""
			}
			head := strings.TrimSpace(string(b))
			if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
				return ref
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}
