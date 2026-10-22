package linear

// The model of the Linear pane: what of the daemon's state the pane depends
// on, and the pure functions that turn the issues into the sections, the
// groups and the order the pane shows. Nothing here draws or talks to the
// daemon, so that all of it is tested on plain values.

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/maphash"
	"slices"
	"sort"
	"strconv"
	"strings"

	"kero/internal/proto"
)

// The sections of the pane, in the order it shows them. Canceled is a fifth
// that exists only while a filter asks for it.
const (
	secCurrent  = "current"
	secDone     = "done"
	secTodo     = "todo"
	secBacklog  = "backlog"
	secCanceled = "canceled"
)

// doneShown is how many finished issues the Done section lists before
// "Show more".
const doneShown = 20

// noProject is the group of the issues that are in no Linear project.
const noProject = ""

// snapshot is what of the daemon's state the pane reads: the workspace, and
// the few facts of the sessions it needs. It leaves out what changes with
// every tick of a terminal (activity, progress, timestamps of a status), so
// that the hash of it changes only when the pane has something to redraw.
type snapshot struct {
	Has       bool   `json:"has"` // the daemon has a Linear workspace
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
	FetchedAt int64  `json:"fetchedAt,omitempty"`
	Viewer    string `json:"viewer,omitempty"`

	States   []proto.LinearWorkflowState `json:"states,omitempty"`
	LProject []proto.LinearProject       `json:"lprojects,omitempty"`
	Issues   []proto.LinearIssue         `json:"issues,omitempty"`
	// LabelColors are the labels' colors, by name, from the daemon.
	LabelColors map[string]string `json:"labelColors,omitempty"`

	Sessions []sessionRef    `json:"sessions,omitempty"`
	Projects []proto.Project `json:"projects,omitempty"`
}

// sessionRef is a session as the pane reads it: its name, its project, its
// panes and the tasks its panes are linked to.
type sessionRef struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Project string   `json:"project,omitempty"`
	Panes   []string `json:"panes,omitempty"`
	Tasks   []string `json:"tasks,omitempty"`
}

// snapshotOf reduces the daemon's state to what the pane reads.
func snapshotOf(st *proto.State) snapshot {
	var s snapshot
	if st == nil {
		return s
	}
	if lin := st.Linear; lin != nil {
		s.Has = true
		s.Status, s.Message, s.FetchedAt = lin.Status, lin.Message, lin.FetchedAt
		if lin.Viewer != nil {
			s.Viewer = lin.Viewer.ID
		}
		s.States = lin.States
		s.LProject = lin.Projects
		s.LabelColors = lin.LabelColors
		s.Issues = make([]proto.LinearIssue, len(lin.Issues))
		for i, is := range lin.Issues {
			// What a program reports of its progress ticks; the pane does
			// not draw it.
			is.Progress = 0
			s.Issues[i] = is
		}
	}
	for _, p := range st.Projects {
		s.Projects = append(s.Projects, proto.Project{ID: p.ID, Name: p.Name, LinearProject: p.LinearProject})
	}
	for _, se := range st.Sessions {
		ref := sessionRef{ID: se.ID, Name: se.DisplayName(""), Project: se.Project}
		for _, p := range se.Panes {
			ref.Panes = append(ref.Panes, p.ID)
			if p.TaskID != "" && !slices.Contains(ref.Tasks, p.TaskID) {
				ref.Tasks = append(ref.Tasks, p.TaskID)
			}
		}
		if se.TaskID != "" && !slices.Contains(ref.Tasks, se.TaskID) {
			ref.Tasks = append(ref.Tasks, se.TaskID)
		}
		s.Sessions = append(s.Sessions, ref)
	}
	return s
}

// seed keys the fingerprints of one run of the plugin.
var seed = maphash.MakeSeed()

// fingerprint is the fingerprint of what snapshotOf reads of a state: equal
// while nothing the pane draws changed. It reads the state in place, without
// the copy of the issues that snapshotOf makes or a JSON encoding of them,
// which is the most of the work for a workspace of thousands of issues. A
// test holds it to the fields of the issue, so that a new field cannot be
// forgotten here.
func fingerprint(st *proto.State) uint64 {
	var h maphash.Hash
	h.SetSeed(seed)
	// The fields are gathered in a buffer and hashed in blocks: a call of
	// the hash for each field costs more than the hashing.
	buf := make([]byte, 0, 32<<10)
	flush := func() {
		h.Write(buf)
		buf = buf[:0]
	}
	str := func(s string) {
		if len(buf)+len(s)+1 > cap(buf) {
			flush()
		}
		if len(s) >= cap(buf) {
			h.WriteString(s)
			h.WriteByte(0)
			return
		}
		buf = append(append(buf, s...), 0)
	}
	num := func(n int64) {
		if len(buf)+8 > cap(buf) {
			flush()
		}
		buf = append(buf, byte(n), byte(n>>8), byte(n>>16), byte(n>>24), byte(n>>32), byte(n>>40), byte(n>>48), byte(n>>56))
	}
	if st == nil {
		return h.Sum64()
	}
	if lin := st.Linear; lin != nil {
		num(1)
		str(lin.Status)
		str(lin.Message)
		num(lin.FetchedAt)
		if lin.Viewer != nil {
			str(lin.Viewer.ID)
		}
		num(int64(len(lin.States)))
		for _, s := range lin.States {
			str(s.ID)
			str(s.Name)
			str(s.Type)
			str(s.Color)
			num(int64(s.Position * 1000))
		}
		num(int64(len(lin.Projects)))
		for _, p := range lin.Projects {
			str(p.ID)
			str(p.Name)
			str(p.Status)
			num(int64(p.Progress * 1000))
			num(int64(len(p.Milestones)))
			for _, m := range p.Milestones {
				str(m.ID)
				str(m.Name)
				str(m.Status)
				num(int64(m.Progress * 1000))
			}
		}
		num(int64(len(lin.Issues)))
		for i := range lin.Issues {
			is := &lin.Issues[i]
			str(is.ID)
			str(is.Identifier)
			str(is.Title)
			str(is.URL)
			str(is.State)
			str(is.StateType)
			str(is.Project)
			str(is.Milestone)
			num(int64(len(is.Labels)))
			for _, l := range is.Labels {
				str(l)
			}
			str(is.Assignee)
			str(is.AssigneeID)
			num(int64(is.Priority))
			str(is.Parent)
			num(int64(len(is.Relations)))
			for _, r := range is.Relations {
				str(r.Type)
				str(r.Identifier)
				str(r.Title)
				str(r.State)
				str(r.StateType)
			}
			num(int64(is.Blocked))
			num(int64(is.Blocking))
			num(int64(is.Children))
			str(is.Session)
			str(is.SessionName)
			str(is.ResumeID)
			str(string(is.SessionStatus))
			num(int64(is.Sessions))
			// Progress ticks, and the pane does not draw it.
			num(is.CreatedAt)
			num(is.UpdatedAt)
		}
		// A map has no order: its entries are added up, each hashed alone.
		var sum uint64
		for k, v := range lin.LabelColors {
			sum += maphash.String(seed, k+"\x00"+v)
		}
		num(int64(sum))
		num(int64(len(lin.LabelColors)))
	}
	num(int64(len(st.Projects)))
	for _, p := range st.Projects {
		str(p.ID)
		str(p.Name)
		str(p.LinearProject)
	}
	num(int64(len(st.Sessions)))
	for i := range st.Sessions {
		se := &st.Sessions[i]
		str(se.ID)
		str(se.DisplayName(""))
		str(se.Project)
		num(int64(len(se.Panes)))
		for _, p := range se.Panes {
			str(p.ID)
			str(p.TaskID)
		}
		str(se.TaskID)
	}
	flush()
	return h.Sum64()
}

// session returns the session with the id, nil none.
func (s *snapshot) session(id string) *sessionRef {
	for i := range s.Sessions {
		if s.Sessions[i].ID == id {
			return &s.Sessions[i]
		}
	}
	return nil
}

// sessionOfPane returns the session that holds the pane, nil none.
func (s *snapshot) sessionOfPane(pane string) *sessionRef {
	for i := range s.Sessions {
		if slices.Contains(s.Sessions[i].Panes, pane) {
			return &s.Sessions[i]
		}
	}
	return nil
}

// issue returns the issue with the id or the identifier, nil none.
func (s *snapshot) issue(ref string) *proto.LinearIssue {
	for i := range s.Issues {
		if s.Issues[i].ID == ref || s.Issues[i].Identifier == ref {
			return &s.Issues[i]
		}
	}
	return nil
}

// stateByName returns the team's workflow state with the name.
func (s *snapshot) stateByName(name string) (proto.LinearWorkflowState, bool) {
	for _, st := range s.States {
		if st.Name == name {
			return st, true
		}
	}
	return proto.LinearWorkflowState{}, false
}

// sortedStates are the team's states in the order the team puts them in.
func (s *snapshot) sortedStates() []proto.LinearWorkflowState {
	states := slices.Clone(s.States)
	sort.SliceStable(states, func(i, j int) bool { return states[i].Position < states[j].Position })
	return states
}

// sectionOf is the section a state type belongs to. Triage joins the
// backlog; a dropped or duplicate issue is canceled. A type the app does
// not know (a daemon from before it was told) is work to do.
func sectionOf(stateType string) string {
	switch stateType {
	case "started":
		return secCurrent
	case "completed":
		return secDone
	case "backlog", "triage":
		return secBacklog
	case "canceled", "duplicate":
		return secCanceled
	}
	return secTodo
}

// sectionOrder is the order the sections stand in.
var sectionOrder = []string{secCurrent, secDone, secTodo, secBacklog, secCanceled}

// The values the assignee filter takes besides a person's id.
const (
	assigneeAny = ""
	assigneeMe  = "me"
)

// filters are the choices that narrow the issues, kept with the pane.
type filters struct {
	Assignee string `json:"assignee,omitempty"` // "" anyone, "me", or a person's id
	Project  string `json:"project,omitempty"`  // a Linear project's name
	Label    string `json:"label,omitempty"`
	Priority string `json:"priority,omitempty"` // "" any, else "0" to "4"
	Canceled bool   `json:"canceled,omitempty"` // show the canceled issues
}

// active tells whether any filter narrows the issues (a hidden canceled
// section is the default, not a filter).
func (f filters) active() bool {
	return f.Assignee != "" || f.Project != "" || f.Label != "" || f.Priority != "" || f.Canceled
}

// focusInfo is the session the app has selected, and the tasks it works on.
type focusInfo struct {
	Session string
	Tasks   []string
}

// scopeProject resolves a pane's scope. For "project" it returns the Linear
// project the pane's session's project is linked to; linked is false when
// there is none, and then the pane asks. For "all", and for a pane whose
// session is not known, nothing narrows.
func scopeProject(snap *snapshot, pane, scope string) (name string, linked, scoped bool) {
	if scope != scopeProjectName {
		return "", true, false
	}
	se := snap.sessionOfPane(pane)
	if se == nil {
		return "", true, false
	}
	var link string
	for _, p := range snap.Projects {
		if p.ID == se.Project {
			link = p.LinearProject
		}
	}
	if link == "" {
		return "", false, true
	}
	for _, lp := range snap.LProject {
		if lp.ID == link {
			return lp.Name, true, true
		}
	}
	// A link to a project the workspace no longer has narrows to nothing
	// that exists: ask again.
	return "", false, true
}

// The values of the pane's scope.
const (
	scopeAll         = "all"
	scopeProjectName = "project"
)

// filterIssues are the issues the filters, the search and the scope keep.
// The canceled ones stay in the result: whether they show is the sections'.
func filterIssues(snap *snapshot, f filters, query, scope string) []proto.LinearIssue {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]proto.LinearIssue, 0, len(snap.Issues))
	for _, is := range snap.Issues {
		if scope != "" && is.Project != scope {
			continue
		}
		switch f.Assignee {
		case assigneeAny:
		case assigneeMe:
			if snap.Viewer != "" && is.AssigneeID != snap.Viewer {
				continue
			}
		default:
			if is.AssigneeID != f.Assignee {
				continue
			}
		}
		if f.Project != "" && is.Project != f.Project {
			continue
		}
		if f.Label != "" && !slices.Contains(is.Labels, f.Label) {
			continue
		}
		if f.Priority != "" && strconv.Itoa(is.Priority) != f.Priority {
			continue
		}
		if q != "" {
			hay := strings.ToLower(is.Identifier + " " + is.Title + " " + is.Project + " " + strings.Join(is.Labels, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, is)
	}
	return out
}

// section is one of the pane's sections: its issues in the order shown.
type section struct {
	Key    string
	Issues []proto.LinearIssue
	// Total is how many issues the section holds before Done's cap; More is
	// how many the cap hides.
	Total, More int
}

// rankKey orders the issues of the Current section: the tier says how much
// the issue is the user's right now, then the latest change.
type rankKey struct {
	tier    int
	updated int64
	ident   string
}

func (a rankKey) less(b rankKey) bool {
	if a.tier != b.tier {
		return a.tier < b.tier
	}
	if a.updated != b.updated {
		return a.updated > b.updated
	}
	return a.ident < b.ident
}

// The tiers of the Current section.
const (
	tierFocused  = 0 // linked to the session the app has selected
	tierLive     = 1 // a live session works on it
	tierMine     = 2 // assigned to the viewer
	tierEveryone = 3
)

// rank is the issue's place in the order of its section. Only the Current
// section has tiers; the others order by the latest change.
func rank(is proto.LinearIssue, sec string, snap *snapshot, focus focusInfo) rankKey {
	k := rankKey{tier: tierEveryone, updated: is.UpdatedAt, ident: is.Identifier}
	if sec != secCurrent {
		k.tier = 0
		return k
	}
	switch {
	case focus.Session != "" && (is.Session == focus.Session || slices.Contains(focus.Tasks, is.Identifier) || slices.Contains(focus.Tasks, is.ID)):
		k.tier = tierFocused
	case is.Session != "" || is.Sessions > 0:
		k.tier = tierLive
	case snap.Viewer != "" && is.AssigneeID == snap.Viewer:
		k.tier = tierMine
	}
	return k
}

// buildSections puts the issues in their sections. Done keeps its latest
// doneShown unless doneAll; canceled issues make a section only when
// asked; an empty section is dropped (the Current section too: the pane
// shows what there is).
func buildSections(snap *snapshot, issues []proto.LinearIssue, focus focusInfo, doneAll, canceled bool) []section {
	by := map[string][]proto.LinearIssue{}
	for _, is := range issues {
		sec := sectionOf(is.StateType)
		if sec == secCanceled && !canceled {
			continue
		}
		by[sec] = append(by[sec], is)
	}
	var out []section
	for _, key := range sectionOrder {
		list := by[key]
		if len(list) == 0 {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool {
			return rank(list[i], key, snap, focus).less(rank(list[j], key, snap, focus))
		})
		s := section{Key: key, Issues: list, Total: len(list)}
		if key == secDone && !doneAll && len(list) > doneShown {
			s.Issues = list[:doneShown]
			s.More = len(list) - doneShown
		}
		out = append(out, s)
	}
	return out
}

// projectGroup is the issues of one Linear project inside a section, as a
// forest: an issue stands under its parent when the parent is in the same
// group.
type projectGroup struct {
	Name  string
	Roots []*issueNode
	Count int
	best  int // the place in the section of the group's best issue
}

// issueNode is an issue with the sub-issues that stand under it.
type issueNode struct {
	Issue proto.LinearIssue
	Kids  []*issueNode
}

// count is how many issues the node holds, itself included.
func (n *issueNode) count() int {
	c := 1
	for _, k := range n.Kids {
		c += k.count()
	}
	return c
}

// groupProjects splits a section's issues by Linear project and nests the
// sub-issues. The groups of the Current section stand in the order of the
// best issue in each, so that the issue the user is on comes first; the
// others by name. The issues without a project come last.
func groupProjects(sec section) []projectGroup {
	order := map[string]int{}
	for i, is := range sec.Issues {
		order[is.ID] = i
	}
	byProject := map[string][]proto.LinearIssue{}
	var names []string
	for _, is := range sec.Issues {
		if _, ok := byProject[is.Project]; !ok {
			names = append(names, is.Project)
		}
		byProject[is.Project] = append(byProject[is.Project], is)
	}
	groups := make([]projectGroup, 0, len(names))
	for _, name := range names {
		list := byProject[name]
		g := projectGroup{Name: name, Count: len(list), best: order[list[0].ID]}
		g.Roots = nestIssues(list)
		groups = append(groups, g)
	}
	// The issues are already in the section's order, so a group's first
	// issue is its best one: Current orders the groups by it, so that the
	// issue the user is on comes first. The others go by name.
	sort.SliceStable(groups, func(i, j int) bool {
		if (groups[i].Name == noProject) != (groups[j].Name == noProject) {
			return groups[j].Name == noProject
		}
		if sec.Key == secCurrent {
			return groups[i].best < groups[j].best
		}
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})
	return groups
}

// nestIssues makes a forest of the issues, in their order: an issue whose
// parent is among them stands under it. Issues in a cycle of parents, which
// no root reaches, are roots of their own.
func nestIssues(list []proto.LinearIssue) []*issueNode {
	parentOf := map[*issueNode]*issueNode{}
	byIdent := make(map[string]*issueNode, len(list))
	all := make([]*issueNode, len(list))
	for i, is := range list {
		n := &issueNode{Issue: is}
		all[i] = n
		byIdent[is.Identifier] = n
	}
	var roots []*issueNode
	placed := map[*issueNode]bool{}
	for _, n := range all {
		if p := byIdent[n.Issue.Parent]; n.Issue.Parent != "" && p != nil && p != n {
			p.Kids = append(p.Kids, n)
			parentOf[n] = p
			continue
		}
		roots = append(roots, n)
	}
	var mark func(n *issueNode)
	mark = func(n *issueNode) {
		if placed[n] {
			return
		}
		placed[n] = true
		for _, k := range n.Kids {
			mark(k)
		}
	}
	for _, r := range roots {
		mark(r)
	}
	for _, n := range all {
		if !placed[n] {
			// Only a cycle is left: break it at this issue.
			if p := parentOf[n]; p != nil {
				p.Kids = slices.DeleteFunc(p.Kids, func(k *issueNode) bool { return k == n })
			}
			roots = append(roots, n)
			mark(n)
		}
	}
	return roots
}

// slug is a short, id-safe name for a free text such as a project's or a
// label's, so that it can be part of a node's id. The same text gives the
// same slug.
func slug(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:5])
}

// people are the assignees of the issues, by name, for the assignee
// filter: the viewer is left to the "Me" choice.
func people(snap *snapshot) []proto.LinearUser {
	seen := map[string]bool{}
	var out []proto.LinearUser
	for _, is := range snap.Issues {
		if is.AssigneeID == "" || is.AssigneeID == snap.Viewer || seen[is.AssigneeID] {
			continue
		}
		seen[is.AssigneeID] = true
		out = append(out, proto.LinearUser{ID: is.AssigneeID, Name: is.Assignee})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// labelsOf are the distinct labels of the issues, sorted.
func labelsOf(snap *snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, is := range snap.Issues {
		for _, l := range is.Labels {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// projectNames are the Linear projects, sorted.
func projectNames(snap *snapshot) []string {
	var out []string
	for _, p := range snap.LProject {
		out = append(out, p.Name)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// initials are the first letters of a name's first two words.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		out = append(out, []rune(strings.ToUpper(w))[0])
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}
