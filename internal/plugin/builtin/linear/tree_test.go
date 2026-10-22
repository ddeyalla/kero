package linear

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
)

func mustValid(t *testing.T, tree protocol.Tree) {
	t.Helper()
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}
	if !tree.Scroll {
		t.Fatal("the pane scrolls as one")
	}
}

func workspace() *snapshot {
	return snap(
		issue(1, "started", assignedTo("me", "Me Myself"), inProject("Rig"), withSession("s1", "bru-1")),
		issue(2, "started", inProject("Rig"), withParent("BRU-1")),
		issue(3, "completed", inProject("Rig")),
		issue(4, "unstarted", inProject("Web"), func(i *proto.LinearIssue) { i.Priority = 1 }),
		issue(5, "backlog"),
		issue(6, "triage", inProject("Web")),
		issue(7, "canceled"),
	)
}

func TestListTreeSectionsAndRows(t *testing.T) {
	tree := buildTree(input(workspace()))
	mustValid(t, tree)
	got := ids(tree)
	pos := func(id string) int { return slices.Index(got, id) }
	for _, id := range []string{"hdr", "q", "mode", "refresh", "filters", "f-assignee", "f-project", "f-priority", "f-canceled"} {
		if pos(id) < 0 {
			t.Fatalf("no %q in the header: %v", id, got)
		}
	}
	if !(pos("sec:current") < pos("sec:done") && pos("sec:done") < pos("sec:todo") && pos("sec:todo") < pos("sec:backlog")) {
		t.Fatalf("sections out of order: %v", got)
	}
	if pos("sec:canceled") >= 0 || pos("i:id-7") >= 0 {
		t.Fatal("canceled issues must be hidden by default")
	}
	// Nested: the project group, the parent, and the sub-issue under it.
	if !(pos("grp:current:"+slug("Rig")) < pos("i:id-1") && pos("i:id-1") < pos("i:id-2")) {
		t.Fatalf("current is not section > project > parent > sub-issue: %v", got)
	}
	if pos("kid:id-1") < 0 {
		t.Fatal("a parent needs its fold control")
	}
	row := find(tree, "i:id-1")
	if !row.Pressable || row.Type != protocol.NodeRow {
		t.Fatalf("an issue row is a pressable row: %+v", row)
	}
	if !strings.Contains(treeText(tree), "bru-1") {
		t.Fatal("the session working on an issue shows on its row")
	}
	// Triage is in the backlog.
	if !(pos("sec:backlog") < pos("i:id-6")) {
		t.Fatal("triage should be under the backlog")
	}
}

func TestCanceledShowsWhenFiltered(t *testing.T) {
	in := input(workspace())
	in.st.Filters.Canceled = true
	tree := buildTree(in)
	mustValid(t, tree)
	if find(tree, "sec:canceled") == nil || find(tree, "i:id-7") == nil {
		t.Fatal("the canceled section should show")
	}
	if !find(tree, "f-canceled").Active {
		t.Fatal("the canceled switch should be on")
	}
}

func TestCollapsedGroupsEmitNothing(t *testing.T) {
	in := input(workspace())
	full := buildTree(in)
	in.st.Collapsed = []string{"sec:current"}
	tree := buildTree(in)
	mustValid(t, tree)
	if find(tree, "sec:current") == nil {
		t.Fatal("a folded section keeps its header")
	}
	for _, id := range []string{"i:id-1", "i:id-2", "grp:current:" + slug("Rig")} {
		if find(tree, id) != nil {
			t.Fatalf("a folded section drew %q", id)
		}
	}
	if nodeCount(tree) >= nodeCount(full) {
		t.Fatalf("folding drew no less: %d of %d", nodeCount(tree), nodeCount(full))
	}

	in.st.Collapsed = []string{"grp:current:" + slug("Rig")}
	tree = buildTree(in)
	if find(tree, "grp:current:"+slug("Rig")) == nil || find(tree, "i:id-1") != nil {
		t.Fatal("a folded project keeps its header and hides its issues")
	}

	in.st.Collapsed = []string{"kid:id-1"}
	tree = buildTree(in)
	if find(tree, "i:id-1") == nil || find(tree, "i:id-2") != nil {
		t.Fatal("a folded parent hides its sub-issues only")
	}
	if find(tree, "kid:id-1").Icon != "chevron-right" {
		t.Fatal("a folded parent points right")
	}
}

func TestListCurrentOrderUsesFocus(t *testing.T) {
	s := workspace()
	s.Issues = append(s.Issues, issue(8, "started", inProject("Zed")))
	s.Sessions = []sessionRef{{ID: "focus", Tasks: []string{"BRU-8"}}}
	in := input(s)
	if rows := rowIDs(buildTree(in)); rows[0] != "id-1" {
		t.Fatalf("without focus the live session leads: %v", rows)
	}
	in.focus = focusInfo{Session: "focus", Tasks: []string{"BRU-8"}}
	rows := rowIDs(buildTree(in))
	if rows[0] != "id-8" {
		t.Fatalf("the focused session's issue should lead its section: %v", rows)
	}
}

func TestBoardTree(t *testing.T) {
	in := input(workspace())
	in.st.Mode = modeBoard
	tree := buildTree(in)
	mustValid(t, tree)
	board := find(tree, "board")
	if board == nil || !board.ScrollX || board.Type != protocol.NodeRow {
		t.Fatalf("the board is a row that scrolls sideways: %+v", board)
	}
	var cols []string
	for _, c := range board.Children {
		cols = append(cols, c.ID)
		if c.Width != 260 {
			t.Fatalf("column %s is %d wide", c.ID, c.Width)
		}
	}
	if want := []string{"col:current", "col:done", "col:todo", "col:backlog"}; !slices.Equal(cols, want) {
		t.Fatalf("columns %v, want %v", cols, want)
	}
	card := find(tree, "c:id-4")
	if card == nil || !card.Pressable || card.Surface != protocol.SurfaceCard {
		t.Fatalf("a card is a pressable surface: %+v", card)
	}
	if find(tree, "c:id-4-p") == nil || find(tree, "c:id-4-j") == nil {
		t.Fatal("a card shows the priority and the project")
	}
	if find(tree, "c:id-1-a") == nil {
		t.Fatal("a card shows its assignee")
	}
	// Triage joins the backlog column; a list's rows are not on the board.
	if find(tree, "c:id-6") == nil || find(tree, "i:id-1") != nil {
		t.Fatal("board holds cards, not rows")
	}

	// An empty column shows unless the setting hides it.
	empty := snap(issue(1, "started"))
	in = input(empty)
	in.st.Mode = modeBoard
	if find(buildTree(in), "col:backlog") == nil {
		t.Fatal("empty columns show by default")
	}
	in.settings.HideEmptyColumns = true
	tree = buildTree(in)
	mustValid(t, tree)
	if find(tree, "col:backlog") != nil || find(tree, "col:current") == nil {
		t.Fatal("the setting should leave out empty columns only")
	}
}

func bigWorkspace(n int) *snapshot {
	var issues []proto.LinearIssue
	types := []string{"started", "completed", "unstarted", "backlog", "triage"}
	for i := 1; i <= n; i++ {
		issues = append(issues, issue(i, types[i%len(types)], inProject(fmt.Sprintf("P%d", i%7)),
			assignedTo("ann", "Ann Lee"), func(is *proto.LinearIssue) {
				is.Priority = i % 5
				is.Labels = []string{"x"}
				if i%9 == 0 {
					is.Session, is.SessionName = "s", "named"
				}
			}))
	}
	return snap(issues...)
}

func TestNodeBudgetList(t *testing.T) {
	in := input(bigWorkspace(5000))
	in.st.DoneAll = true
	tree := buildTree(in)
	mustValid(t, tree)
	if n := nodeCount(tree); n > protocol.MaxNodes {
		t.Fatalf("%d nodes, over the %d allowed", n, protocol.MaxNodes)
	}
	if find(tree, "trunc") == nil {
		t.Fatal("a pane that cut issues says how many")
	}
	if got := len(rowIDs(tree)); got < 100 {
		t.Fatalf("only %d rows fit", got)
	}
	for _, key := range []string{"sec:current", "sec:done", "sec:todo", "sec:backlog"} {
		if find(tree, key) == nil {
			t.Fatalf("section %s lost its header to the budget", key)
		}
	}
}

func TestNodeBudgetBoard(t *testing.T) {
	in := input(bigWorkspace(5000))
	in.st.Mode = modeBoard
	in.st.DoneAll = true
	tree := buildTree(in)
	mustValid(t, tree)
	if n := nodeCount(tree); n > protocol.MaxNodes {
		t.Fatalf("%d nodes, over the %d allowed", n, protocol.MaxNodes)
	}
	cards := 0
	per := map[string]int{}
	for _, id := range ids(tree) {
		if strings.HasPrefix(id, "c:id-") && strings.Count(id, "-") == 1 {
			cards++
		}
	}
	for _, c := range find(tree, "board").Children {
		for _, k := range c.Children {
			if strings.HasPrefix(k.ID, "c:id-") {
				per[c.ID]++
			}
		}
	}
	if cards > boardCardCap {
		t.Fatalf("%d cards, over the cap of %d", cards, boardCardCap)
	}
	for _, key := range []string{"col:current", "col:done", "col:todo", "col:backlog"} {
		if per[key] == 0 {
			t.Fatalf("column %s got no cards: %v", key, per)
		}
	}
	if find(tree, "trunc") == nil {
		t.Fatal("the board says how many cards it left out")
	}
}

func TestSharesFairAndExact(t *testing.T) {
	mk := func(n int) section { return section{Issues: make([]proto.LinearIssue, n)} }
	got := shares([]section{mk(1000), mk(3), mk(0), mk(1000)}, 100)
	if got[1] != 3 || got[2] != 0 || got[0]+got[3] != 97 || got[0]-got[3] > 1 || got[3]-got[0] > 1 {
		t.Fatalf("shares %v", got)
	}
	if got := shares([]section{mk(5), mk(5)}, 100); got[0] != 5 || got[1] != 5 {
		t.Fatalf("shares of a small board %v", got)
	}
}

func TestEmptyStates(t *testing.T) {
	// The daemon is out of reach.
	in := input(&snapshot{})
	in.connected, in.down = false, "dial: no such file"
	tree := buildTree(in)
	mustValid(t, tree)
	if find(tree, "empty-down") == nil {
		t.Fatal("no daemon state")
	}
	// No key.
	in = input(&snapshot{Has: true, Status: proto.LinearNoToken})
	if n := find(buildTree(in), "empty-unread"); n == nil || n.Action == "" {
		t.Fatal("an unread workspace offers to read again")
	}
	// Nothing matches.
	in = input(workspace())
	in.st.Query = "nothing like this"
	tree = buildTree(in)
	mustValid(t, tree)
	if find(tree, "empty-none") == nil || find(tree, "q") == nil {
		t.Fatal("a search with no result keeps the header and says so")
	}
	// A project not linked to a Linear project.
	s := workspace()
	s.Projects = []proto.Project{{ID: "pr", Name: "bare"}}
	s.Sessions = []sessionRef{{ID: "s", Project: "pr", Panes: []string{"p1"}}}
	in = input(s)
	in.st.Scope = scopeProjectName
	tree = buildTree(in)
	mustValid(t, tree)
	n := find(tree, "empty-scope")
	if n == nil || !strings.Contains(n.Detail, "menu") || n.Action == "" {
		t.Fatalf("an unlinked project asks: %+v", n)
	}
	if find(tree, "sec:current") != nil {
		t.Fatal("the prompt replaces the issues")
	}
	// Linked: only that project's issues, and no project filter.
	s.Projects[0].LinearProject = "lp-web"
	tree = buildTree(in)
	mustValid(t, tree)
	if find(tree, "i:id-4") == nil || find(tree, "i:id-1") != nil || find(tree, "f-project") != nil {
		t.Fatalf("a scoped pane shows its project's issues alone: %v", ids(tree))
	}
}

func fullDetail() *proto.LinearIssueDetail {
	d := &proto.LinearIssueDetail{
		LinearIssue: issue(1, "started", assignedTo("me", "Me Myself"), inProject("Rig"), withSession("s1", "bru-1"), func(i *proto.LinearIssue) {
			i.Priority = 2
			i.Labels = []string{"bug", "ui"}
			i.Milestone = "M1"
			i.Parent = "BRU-9"
			i.Relations = []proto.LinearRelation{{Type: "blocks", Identifier: "BRU-3", Title: "Blocked thing", State: "Done", StateType: "completed"}}
		}),
		Description: "## Plan\nline one\nline two\n\n- a\n- b",
		BranchName:  "bru-1-fix",
		ParentTitle: "The parent",
		SubIssues:   []proto.LinearIssue{issue(2, "started")},
		Attachments: []proto.LinearAttachment{{ID: "a", Title: "Prompt", URL: "https://example.com/p.md", Subtitle: "md"}},
		Comments:    []proto.LinearComment{{ID: "c1", Body: "Done **now**", Author: "Ann", CreatedAt: epoch.UnixMilli()}},
		Runs: []proto.TaskRun{
			{Agent: "claude", ID: "abc123", Session: "s1", SessionName: "bru-1", Dir: "/x", Via: "task", Started: epoch.UnixMilli()},
			{Agent: "codex", ID: "def456", Session: "gone", Dir: "/y", Started: epoch.UnixMilli(), Ended: epoch.UnixMilli()},
		},
	}
	return d
}

func TestDetailTreeHasFullParity(t *testing.T) {
	s := workspace()
	s.Issues[0].Priority = 2
	s.Sessions = []sessionRef{{ID: "s1", Name: "bru-1"}, {ID: "s2", Name: "other"}}
	in := input(s)
	in.st.Open = "id-1"
	in.detail = fullDetail()
	tree := buildTree(in)
	mustValid(t, tree)
	for _, id := range []string{
		"d-back", "d-ext", "d-title", "props", "d-status", "p-priority", "p-assignee", "p-project", "p-milestone",
		"p-labels", "p-branch", "sessions", "run0", "run0-open", "run1-resume", "s-unlink", "d-desc",
		"rel-parent", "sub:id-2", "att0", "rel0", "cm0", "cm0-b",
	} {
		if find(tree, id) == nil {
			t.Errorf("no %q in the detail", id)
		}
	}
	if d := find(tree, "d-desc"); d == nil || d.Type != protocol.NodeMarkdown || !strings.Contains(d.Text, "line one\\\nline two") {
		t.Fatalf("the description is markdown with hard breaks: %+v", d)
	}
	if b := find(tree, "cm0-b"); b == nil || b.Type != protocol.NodeMarkdown {
		t.Fatal("a comment is markdown")
	}
	status := find(tree, "d-status")
	if status.Type != protocol.NodeMenu || len(status.Items) != len(states()) {
		t.Fatalf("the status is a menu of the team's states: %+v", status)
	}
	active := ""
	for _, it := range status.Items {
		if it.Active {
			active = it.Title
		}
	}
	if active != "In Progress" {
		t.Fatalf("the checked state is %q", active)
	}
	// A move asked for shows at once.
	in.moving = "In Review"
	status = find(buildTree(in), "d-status")
	if status.Label != "In Review" {
		t.Fatalf("the menu shows %q while the move is on its way", status.Label)
	}
	// A running session opens; the one that ended resumes; no run is without a start.
	if find(tree, "s-start") != nil {
		t.Fatal("an issue with runs has no Start task")
	}

	// An issue with no session offers to start one, or to link.
	s2 := snap(issue(1, "unstarted"))
	s2.Sessions = []sessionRef{{ID: "s2", Name: "other"}}
	in = input(s2)
	in.st.Open = "id-1"
	in.detail = &proto.LinearIssueDetail{LinearIssue: s2.Issues[0]}
	tree = buildTree(in)
	mustValid(t, tree)
	if find(tree, "s-start") == nil || find(tree, "s-link") == nil || find(tree, "s-unlink") != nil {
		t.Fatalf("an idle issue offers Start task and a session to link: %v", ids(tree))
	}

	// While it is read, and when it could not be.
	in.detail = nil
	tree = buildTree(in)
	mustValid(t, tree)
	if find(tree, "d-reading") == nil || find(tree, "d-back") == nil {
		t.Fatal("a detail being read still has its way back")
	}
	in.detailErr = "boom"
	if n := find(buildTree(in), "d-err"); n == nil || !strings.Contains(n.Detail, "boom") {
		t.Fatal("an unread issue shows why and offers Retry")
	}
}

func TestDetailBoundsItsRows(t *testing.T) {
	d := fullDetail()
	for i := 0; i < 500; i++ {
		d.Comments = append(d.Comments, proto.LinearComment{ID: fmt.Sprint(i), Body: "x", Author: "A"})
		d.SubIssues = append(d.SubIssues, issue(100+i, "started"))
	}
	in := input(workspace())
	in.st.Open = "id-1"
	in.detail = d
	tree := buildTree(in)
	mustValid(t, tree)
	if n := nodeCount(tree); n > protocol.MaxNodes {
		t.Fatalf("%d nodes in a detail", n)
	}
}
