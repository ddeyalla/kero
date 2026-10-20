package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	"kero/internal/linear"
	"kero/internal/proto"
)

// The daemon reads the Linear workspace, so that the app can show the
// issues, their projects and their milestones, and so that a session and
// the task it works on are one thing rather than two.
//
// Linear has no way to push to a local program: its webhooks want a
// public address. So the daemon polls: every half minute it asks what
// changed since the last time, which is a small request, and every five
// minutes it reads the projects and the milestones whole, which change
// rarely. A workspace of a hundred issues costs a fraction of the quota
// Linear allows.

const (
	linearPoll  = 30 * time.Second
	linearSlow  = 5 * time.Minute
	linearLimit = 25 * time.Second // background requests never hold the daemon's mutex
)

// linearTeam is the team the daemon reads: LINEAR_TEAM when it is set,
// else the first team the key opens.
func linearTeam() string {
	return strings.TrimSpace(os.Getenv("LINEAR_TEAM"))
}

// linearLoop reads the workspace, and keeps the daemon's copy of it
// fresh. It never fails the daemon: a workspace that cannot be read is
// a state the app shows, not an error the daemon dies of.
func (d *Daemon) linearLoop() {
	t := time.NewTicker(linearPoll)
	defer t.Stop()
	slow := time.NewTicker(linearSlow)
	defer slow.Stop()
	d.linearRead(true)
	for {
		select {
		case <-d.quit:
			return
		case <-t.C:
			d.linearRead(false)
		case <-slow.C:
			d.linearRead(true)
		}
	}
}

// linearRead reads the workspace: whole when full, and only what changed
// since the last read otherwise.
func (d *Daemon) linearRead(full bool) {
	if !d.linearMu.TryLock() {
		return
	}
	defer d.linearMu.Unlock()
	key := linear.Key()
	identity := fmt.Sprintf("%x/%s", sha256.Sum256([]byte(key)), linearTeam())
	if identity != d.linearIdentity {
		changed := d.linearIdentity != ""
		d.linearIdentity = identity
		d.linearRetry = time.Time{}
		full = true
		// Credentials may have changed workspace. Never merge across accounts.
		if changed {
			d.linearSet(&proto.LinearState{Status: proto.LinearNoToken})
		}
	}
	if key == "" {
		d.linearSet(&proto.LinearState{Status: proto.LinearNoToken})
		return
	}
	if time.Now().Before(d.linearRetry) {
		return
	}
	c := linear.New(key)
	ctx, cancel := context.WithTimeout(context.Background(), linearLimit)
	defer cancel()
	go func() {
		select {
		case <-d.quit:
			cancel()
		case <-ctx.Done():
		}
	}()

	d.mu.Lock()
	prev := d.linear
	d.mu.Unlock()

	if !full && prev != nil && prev.Status == proto.LinearOK && prev.Team != "" {
		since := time.UnixMilli(prev.FetchedAt)
		watermark := time.Now().UnixMilli()
		issues, err := c.Delta(ctx, prev.Team, since)
		if err != nil {
			d.linearFailed(prev, err)
			return
		}
		next := *prev
		next.Issues = mergeIssues(prev.Issues, issues)
		next.FetchedAt = watermark
		next.Status = proto.LinearOK
		next.Message = ""
		d.linearSet(&next)
		return
	}

	team := linearTeam()
	snap, err := d.linearBoard(ctx, c, team)
	if err != nil {
		d.linearFailed(prev, err)
		return
	}
	d.linearSet(snap)
}

// linearBoard reads a team whole, choosing the team when none is named.
func (d *Daemon) linearBoard(ctx context.Context, c *linear.Client, team string) (*proto.LinearState, error) {
	if team == "" {
		// No team named: the first one the key opens. A workspace has
		// one team in practice, and naming it is a matter of
		// configuration, not of a guess.
		teams, err := c.Teams(ctx)
		if err != nil {
			return nil, err
		}
		if len(teams) == 0 {
			return nil, errNoTeam
		}
		team = teams[0].Key
	}
	snap, err := c.Board(ctx, team)
	if err != nil {
		return nil, err
	}
	return linearSnapshot(snap), nil
}

// errNoTeam says the key opens a workspace with no team in it.
var errNoTeam = &linearError{"the workspace has no team"}

// linearError is a failure of the workspace that is not Linear's own.
type linearError struct{ msg string }

func (e *linearError) Error() string { return e.msg }

// linearSnapshot turns what the client read into what the app sees.
func linearSnapshot(snap *linear.Snapshot) *proto.LinearState {
	st := &proto.LinearState{
		Status:    proto.LinearOK,
		Team:      snap.Team.Key,
		TeamName:  snap.Team.Name,
		FetchedAt: snap.Read.UnixMilli(),
	}
	for _, s := range snap.Team.States {
		st.States = append(st.States, proto.LinearWorkflowState{
			ID: s.ID, Name: s.Name, Type: s.Type, Position: s.Position,
		})
	}
	for _, p := range snap.Projects {
		lp := proto.LinearProject{
			ID: p.ID, Name: p.Name, Status: p.Status.Type, Progress: p.Progress,
		}
		for _, m := range p.Milestones {
			lp.Milestones = append(lp.Milestones, proto.LinearMilestone{
				ID: m.ID, Name: m.Name, Status: m.Status, Progress: m.Progress,
			})
		}
		st.Projects = append(st.Projects, lp)
	}
	for _, i := range snap.Issues {
		st.Issues = append(st.Issues, linearIssue(i))
	}
	return st
}

// linearIssue turns an issue into what the app sees.
func linearIssue(i linear.Issue) proto.LinearIssue {
	out := proto.LinearIssue{
		ID:         i.ID,
		Identifier: i.Identifier,
		Title:      i.Title,
		URL:        i.URL,
		State:      i.State.Name,
		StateType:  i.State.Type,
		CreatedAt:  i.CreatedAt.UnixMilli(),
		UpdatedAt:  i.UpdatedAt.UnixMilli(),
		Progress:   -1,
	}
	if i.Project != nil {
		out.Project = i.Project.Name
	}
	if i.Milestone != nil {
		out.Milestone = i.Milestone.Name
	}
	for _, l := range i.Labels {
		out.Labels = append(out.Labels, l.Name)
	}
	relations := append([]linear.Relation{}, i.Relations...)
	for _, r := range i.InverseRelations {
		if r.Type == "blocks" {
			r.Type = "blockedBy"
		}
		relations = append(relations, r)
	}
	for _, r := range relations {
		switch r.Type {
		case "blockedBy":
			out.Blocked++
		case "blocks":
			out.Blocking++
		}
		out.Relations = append(out.Relations, proto.LinearRelation{
			Type:       r.Type,
			Identifier: r.Issue.Identifier,
			Title:      r.Issue.Title,
			State:      r.Issue.State.Name,
		})
	}
	out.Children = len(i.Children)
	return out
}

// mergeIssues folds the issues that changed into the ones already read:
// a changed issue replaces its old self, and one that is new is added.
func mergeIssues(old []proto.LinearIssue, changed []linear.Issue) []proto.LinearIssue {
	byID := make(map[string]int, len(old))
	out := make([]proto.LinearIssue, 0, len(old)+len(changed))
	for _, i := range old {
		byID[i.ID] = len(out)
		out = append(out, i)
	}
	for _, i := range changed {
		ni := linearIssue(i)
		if at, ok := byID[ni.ID]; ok {
			out[at] = ni
			continue
		}
		byID[ni.ID] = len(out)
		out = append(out, ni)
	}
	return out
}

// linearFailed keeps what was read, and says what went wrong. A
// workspace that cannot be read is not a workspace that is gone.
func (d *Daemon) linearFailed(prev *proto.LinearState, err error) {
	if linear.Status(err) == linear.StatusRateLimited {
		d.linearRetry = linear.RetryAt(err)
	} else if linear.Status(err) == linear.StatusAuthFailed {
		d.linearRetry = time.Now().Add(linearSlow)
	}
	next := &proto.LinearState{Status: linearStatus(err), Message: err.Error()}
	if prev != nil {
		next.Team = prev.Team
		next.TeamName = prev.TeamName
		next.States = prev.States
		next.Projects = prev.Projects
		next.Issues = prev.Issues
		next.FetchedAt = prev.FetchedAt
	}
	d.linearSet(next)
}

// linearStatus is what a failure means for the workspace.
func linearStatus(err error) string {
	switch linear.Status(err) {
	case linear.StatusAuthFailed:
		return proto.LinearAuthFailed
	case linear.StatusRateLimited:
		return proto.LinearRateLimited
	case linear.StatusNoToken:
		return proto.LinearNoToken
	}
	return proto.LinearUnreachable
}

// linearSet keeps the daemon's copy of the workspace, and tells the app.
func (d *Daemon) linearSet(st *proto.LinearState) {
	d.mu.Lock()
	changed := d.linear == nil || !linearEqual(d.linear, st)
	d.linear = st
	d.mu.Unlock()
	if changed {
		d.changed()
	}
}

// linearEqual says whether two copies of the workspace are the same, so
// that the app is not woken for nothing.
func linearEqual(a, b *proto.LinearState) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Status != b.Status || a.Message != b.Message || a.Team != b.Team ||
		a.FetchedAt != b.FetchedAt || len(a.Issues) != len(b.Issues) ||
		len(a.Projects) != len(b.Projects) || len(a.States) != len(b.States) {
		return false
	}
	for i := range a.Issues {
		if a.Issues[i].UpdatedAt != b.Issues[i].UpdatedAt ||
			a.Issues[i].State != b.Issues[i].State ||
			a.Issues[i].Session != b.Issues[i].Session {
			return false
		}
	}
	return true
}

// linearState is the workspace, as the app sees it.
func (d *Daemon) linearState() *proto.LinearState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.linear == nil {
		return &proto.LinearState{Status: proto.LinearNoToken}
	}
	lin := *d.linear
	lin.Issues = d.linearIssues()
	return &lin
}

// linkSession binds a session to a Linear issue, or unbinds it when the
// task is empty. The task is the issue's identifier, as BRU-25, or its
// id.
func (d *Daemon) linkSession(ref, task string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.findSessionLocked(ref)
	if s == nil {
		return fmt.Errorf("no session %q", ref)
	}
	if task != "" {
		found := false
		if d.linear != nil {
			for _, i := range d.linear.Issues {
				if i.ID == task || strings.EqualFold(i.Identifier, task) {
					task = i.Identifier
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("no issue %q in the cached workspace", task)
		}
	}
	for _, p := range s.panes {
		p.mu.Lock()
		p.info.TaskID = task
		p.mu.Unlock()
	}
	d.changed()
	return nil
}

// linearSetState moves an issue to a workflow state, by its name.
func (d *Daemon) linearSetState(issue, state string) error {
	key := linear.Key()
	if key == "" {
		return linear.ErrNoToken
	}
	var stateID string
	d.mu.Lock()
	if d.linear != nil {
		for _, s := range d.linear.States {
			if strings.EqualFold(s.Name, state) {
				stateID = s.ID
				break
			}
		}
	}
	d.mu.Unlock()
	if stateID == "" {
		return fmt.Errorf("no state %q", state)
	}
	c := linear.New(key)
	ctx, cancel := context.WithTimeout(context.Background(), linearLimit)
	defer cancel()
	if err := c.SetState(ctx, issue, stateID); err != nil {
		return err
	}
	go d.linearRead(true)
	return nil
}

// linearComment writes a comment on an issue.
func (d *Daemon) linearComment(issue, body string) error {
	key := linear.Key()
	if key == "" {
		return linear.ErrNoToken
	}
	c := linear.New(key)
	ctx, cancel := context.WithTimeout(context.Background(), linearLimit)
	defer cancel()
	return c.Comment(ctx, issue, body)
}

// linearDetail reads one issue whole, with its comments and the files
// on it.
func (d *Daemon) linearDetail(id string) (*proto.LinearIssueDetail, error) {
	key := linear.Key()
	if key == "" {
		return nil, linear.ErrNoToken
	}
	c := linear.New(key)
	ctx, cancel := context.WithTimeout(context.Background(), linearLimit)
	defer cancel()
	iss, err := c.Issue(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &proto.LinearIssueDetail{
		LinearIssue: linearIssue(*iss),
		Description: iss.Description,
		BranchName:  iss.BranchName,
	}
	if iss.Assignee != nil {
		out.Assignee = iss.Assignee.Name
	}
	if iss.Creator != nil {
		out.Creator = iss.Creator.Name
	}
	if iss.Parent != nil {
		out.Parent = iss.Parent.Identifier
	}
	for _, a := range iss.Attachments {
		out.Attachments = append(out.Attachments, proto.LinearAttachment{
			ID: a.ID, Title: a.Title, URL: a.URL, Subtitle: a.Subtitle,
		})
	}
	for _, cm := range iss.Comments {
		author := ""
		if cm.User != nil {
			author = cm.User.Name
		}
		out.Comments = append(out.Comments, proto.LinearComment{
			ID: cm.ID, Body: cm.Body, Author: author, CreatedAt: cm.CreatedAt.UnixMilli(),
		})
	}
	for _, sub := range iss.Children {
		out.SubIssues = append(out.SubIssues, proto.LinearIssue{
			ID: sub.ID, Identifier: sub.Identifier, Title: sub.Title, State: sub.State.Name,
		})
	}
	// The session working on the issue, when one is: what a person
	// needs to open the work again.
	d.mu.Lock()
	for task, link := range d.taskLinks() {
		if task == iss.ID || task == iss.Identifier {
			out.Session = link.Session
			out.SessionName = link.Name
			out.ResumeID = link.ResumeID
			out.Progress = link.Progress
			out.SessionStatus = link.Status
		}
	}
	d.mu.Unlock()
	return out, nil
}
