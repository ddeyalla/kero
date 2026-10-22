package linear

import (
	"fmt"
	"strings"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
)

var epoch = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// states is a team's workflow.
func states() []proto.LinearWorkflowState {
	return []proto.LinearWorkflowState{
		{ID: "st-triage", Name: "Triage", Type: "triage", Position: 0},
		{ID: "st-backlog", Name: "Backlog", Type: "backlog", Position: 1},
		{ID: "st-todo", Name: "Todo", Type: "unstarted", Position: 2},
		{ID: "st-progress", Name: "In Progress", Type: "started", Position: 3},
		{ID: "st-review", Name: "In Review", Type: "started", Position: 4},
		{ID: "st-done", Name: "Done", Type: "completed", Position: 5},
		{ID: "st-canceled", Name: "Canceled", Type: "canceled", Position: 6},
	}
}

var stateOf = map[string]string{
	"triage": "Triage", "backlog": "Backlog", "unstarted": "Todo", "started": "In Progress",
	"completed": "Done", "canceled": "Canceled",
}

// issue makes an issue of a state type, changed n minutes before epoch.
func issue(n int, stateType string, mods ...func(*proto.LinearIssue)) proto.LinearIssue {
	is := proto.LinearIssue{
		ID:         fmt.Sprintf("id-%d", n),
		Identifier: fmt.Sprintf("BRU-%d", n),
		Title:      fmt.Sprintf("Issue number %d", n),
		URL:        fmt.Sprintf("https://linear.app/team/issue/BRU-%d", n),
		State:      stateOf[stateType],
		StateType:  stateType,
		UpdatedAt:  epoch.Add(-time.Duration(n) * time.Minute).UnixMilli(),
	}
	for _, m := range mods {
		m(&is)
	}
	return is
}

func inProject(name string) func(*proto.LinearIssue) {
	return func(i *proto.LinearIssue) { i.Project = name }
}

func assignedTo(id, name string) func(*proto.LinearIssue) {
	return func(i *proto.LinearIssue) { i.AssigneeID, i.Assignee = id, name }
}

func withParent(ident string) func(*proto.LinearIssue) {
	return func(i *proto.LinearIssue) { i.Parent = ident }
}

func withSession(id, name string) func(*proto.LinearIssue) {
	return func(i *proto.LinearIssue) {
		i.Session, i.SessionName, i.Sessions, i.SessionStatus = id, name, 1, proto.Working
	}
}

// snap makes a snapshot of a workspace with the issues, whose viewer is "me".
func snap(issues ...proto.LinearIssue) *snapshot {
	return &snapshot{
		Has: true, Status: proto.LinearOK, FetchedAt: epoch.UnixMilli(), Viewer: "me",
		States: states(), Issues: issues,
		LProject: []proto.LinearProject{{ID: "lp-rig", Name: "Rig"}, {ID: "lp-web", Name: "Web"}},
	}
}

// state wraps a snapshot's workspace as the daemon's state, with sessions.
func stateOfSnap(s *snapshot, sessions ...proto.Session) *proto.State {
	return &proto.State{
		Projects: s.Projects,
		Sessions: sessions,
		Linear: &proto.LinearState{
			Status: s.Status, FetchedAt: s.FetchedAt, States: s.States, Projects: s.LProject, Issues: s.Issues,
			Viewer: &proto.LinearUser{ID: s.Viewer, Name: "Me Myself"},
		},
	}
}

// session makes a session with panes.
func session(id, project string, panes ...string) proto.Session {
	s := proto.Session{ID: id, Project: project, Name: "session " + id}
	for _, p := range panes {
		s.Panes = append(s.Panes, proto.Pane{ID: p})
	}
	return s
}

func input(s *snapshot) viewInput {
	return viewInput{pane: "p1", snap: s, now: epoch, connected: true, st: paneState{Mode: modeList, Scope: scopeAll}}
}

// ids are the ids of every node of a tree, in order.
func ids(tree protocol.Tree) []string {
	var out []string
	var walk func(n *protocol.Node)
	walk = func(n *protocol.Node) {
		out = append(out, n.ID)
		for _, k := range n.Children {
			walk(k)
		}
	}
	if tree.Header != nil {
		walk(tree.Header)
	}
	walk(tree.Root)
	return out
}

// find returns the node with the id, nil none.
func find(tree protocol.Tree, id string) *protocol.Node {
	var hit *protocol.Node
	var walk func(n *protocol.Node)
	walk = func(n *protocol.Node) {
		if n.ID == id {
			hit = n
		}
		for _, k := range n.Children {
			walk(k)
		}
	}
	if tree.Header != nil {
		walk(tree.Header)
	}
	walk(tree.Root)
	return hit
}

func nodeCount(tree protocol.Tree) int {
	n := count(tree.Root)
	if tree.Header != nil {
		n += count(tree.Header)
	}
	return n
}

// treeText is every word of a tree, for a search.
func treeText(tree protocol.Tree) string {
	var b strings.Builder
	var walk func(n *protocol.Node)
	walk = func(n *protocol.Node) {
		b.WriteString(n.Text + " " + n.Label + " " + n.Title + " " + n.Detail + "\n")
		for _, it := range n.Items {
			b.WriteString(it.Title + "\n")
		}
		for _, k := range n.Children {
			walk(k)
		}
	}
	if tree.Header != nil {
		walk(tree.Header)
	}
	walk(tree.Root)
	return b.String()
}

// rowIDs are the ids of the issue rows of a list tree, in order.
func rowIDs(tree protocol.Tree) []string {
	var out []string
	for _, id := range ids(tree) {
		if strings.HasPrefix(id, "i:id-") && strings.Count(id, "-") == 1 {
			out = append(out, strings.TrimPrefix(id, "i:"))
		}
	}
	return out
}
