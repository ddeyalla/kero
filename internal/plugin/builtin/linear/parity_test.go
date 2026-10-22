package linear

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/proto"
)

// These tests hold the pane to the old built-in Linear view: the header that
// stays, the context menu of an issue, the keys, the chips and the issue
// beside the issues.

func validate(t *testing.T, tree protocol.Tree) {
	t.Helper()
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("the tree is invalid: %v", err)
	}
}

func TestHeaderStaysAboveTheScrollingIssues(t *testing.T) {
	tree := buildTree(input(workspace()))
	mustValid(t, tree)
	if tree.Header == nil || tree.Header.ID != "top" {
		t.Fatalf("no sticky header: %+v", tree.Header)
	}
	inHeader := map[string]bool{}
	var walk func(n *protocol.Node)
	walk = func(n *protocol.Node) {
		inHeader[n.ID] = true
		for _, k := range n.Children {
			walk(k)
		}
	}
	walk(tree.Header)
	for _, id := range []string{"hdr", "q", "mode", "refresh", "filters", "f-assignee", "f-canceled", "status"} {
		if !inHeader[id] {
			t.Errorf("%q should stay in the header", id)
		}
	}
	var inRoot func(n *protocol.Node, id string) bool
	inRoot = func(n *protocol.Node, id string) bool {
		if n.ID == id {
			return true
		}
		for _, k := range n.Children {
			if inRoot(k, id) {
				return true
			}
		}
		return false
	}
	for _, id := range []string{"hdr", "filters", "status"} {
		if inRoot(tree.Root, id) {
			t.Errorf("%q scrolls with the issues", id)
		}
	}
	if !slices.Equal(tree.Keys, listKeys) {
		t.Fatalf("keys = %v, want %v", tree.Keys, listKeys)
	}

	// The notice stays too, and an open issue keeps its head.
	in := input(workspace())
	in.notice = "boom"
	if n := find(buildTree(in), "notice"); n == nil || !inHeaderOf(buildTree(in), "notice") {
		t.Fatal("the notice belongs in the header")
	}
	in = input(workspace())
	in.st.Open = "id-1"
	in.detail = fullDetail()
	open := buildTree(in)
	mustValid(t, open)
	if open.Header == nil || find(protocol.Tree{Root: open.Header}, "d-hdr") == nil {
		t.Fatalf("an open issue keeps its back and its copy buttons in view: %+v", open.Header)
	}
	if !slices.Equal(open.Keys, detailKeys) {
		t.Fatalf("open keys = %v", open.Keys)
	}

	// Nothing to read draws no header and asks for no key.
	empty := buildTree(viewInput{pane: "p1", snap: &snapshot{}})
	validate(t, empty)
	if empty.Header != nil || len(empty.Keys) != 0 {
		t.Fatalf("an unread pane has a header or keys: %+v", empty)
	}
}

func inHeaderOf(tree protocol.Tree, id string) bool {
	return tree.Header != nil && find(protocol.Tree{Root: tree.Header}, id) != nil
}

func menuTitles(items []protocol.Item) []string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "-")
		case len(it.Items) > 0:
			out = append(out, it.Title+">")
		default:
			out = append(out, it.Title)
		}
	}
	return out
}

func TestRowsAndCardsOpenTheIssueMenu(t *testing.T) {
	s := workspace()
	for _, mode := range []string{modeList, modeBoard} {
		in := input(s)
		in.st.Mode = mode
		tree := buildTree(in)
		mustValid(t, tree)
		pick := func(id string) *protocol.Node { return find(tree, id) }
		node := pick("i:id-4")
		if mode == modeBoard {
			node = pick("c:id-4")
		}
		if node == nil || len(node.ContextMenu) == 0 || !node.Pressable {
			t.Fatalf("%s: no menu on the issue: %+v", mode, node)
		}
		var move *protocol.Item
		for i, it := range node.ContextMenu {
			if it.ID == "m-move" {
				move = &node.ContextMenu[i]
			}
		}
		ids := map[string]protocol.Item{}
		for _, it := range node.ContextMenu {
			ids[it.ID] = it
		}
		for _, id := range []string{"m-open", "m-ext", "m-copy-id", "m-copy-link", "m-move", "m-start"} {
			if _, ok := ids[id]; !ok {
				t.Errorf("%s: no %q in %v", mode, id, menuTitles(node.ContextMenu))
			}
		}
		if move == nil || len(move.Items) != len(states()) {
			t.Fatalf("%s: Move To lists the team's states: %+v", mode, move)
		}
		for _, it := range move.Items {
			current := it.Title == "Todo"
			if it.Active != current || current != (it.Enabled != nil && !*it.Enabled) {
				t.Errorf("%s: state %q: checked %v, enabled %v; the current state is checked and off", mode, it.Title, it.Active, it.Enabled)
			}
		}
		// An issue a session works on cannot start a task, and one with no
		// link cannot be copied or opened as one.
		withSession := pick("i:id-1")
		if mode == modeBoard {
			withSession = pick("c:id-1")
		}
		for _, it := range withSession.ContextMenu {
			if it.ID == "m-start" {
				t.Errorf("%s: Start task on an issue a session works on", mode)
			}
		}
	}
	noURL := snap(issue(1, "started", func(i *proto.LinearIssue) { i.URL = "" }))
	tree := buildTree(input(noURL))
	for _, it := range find(tree, "i:id-1").ContextMenu {
		if (it.ID == "m-ext" || it.ID == "m-copy-link") && (it.Enabled == nil || *it.Enabled) {
			t.Errorf("%q should be off for an issue with no link", it.ID)
		}
	}
}

func TestSelectionMarksARowAndACard(t *testing.T) {
	s := workspace()
	for _, mode := range []string{modeList, modeBoard} {
		in := input(s)
		in.st.Mode = mode
		prefix := "i:"
		if mode == modeBoard {
			prefix = "c:"
		}
		in.sel = "id-4"
		tree := buildTree(in)
		if !find(tree, prefix+"id-4").Active || find(tree, prefix+"id-3").Active {
			t.Fatalf("%s: the issue the keys chose is marked, and only it", mode)
		}
	}
	// The open issue is the selected one, whatever the keys chose.
	in := input(s)
	in.sel, in.st.Open = "id-4", "id-3"
	in.width = 1000
	in.detail = &proto.LinearIssueDetail{LinearIssue: s.Issues[2]}
	tree := buildTree(in)
	if !find(tree, "i:id-3").Active || find(tree, "i:id-4").Active {
		t.Fatal("the open issue is the one marked")
	}
}

func TestCardChips(t *testing.T) {
	s := snap(issue(1, "started", func(i *proto.LinearIssue) {
		i.Blocked, i.Blocking, i.Children = 2, 1, 3
		i.Milestone = "M1 Foundation"
		i.Labels = []string{"bug", "ui", "web"}
	}))
	in := input(s)
	in.st.Mode = modeBoard
	in.labelColors = map[string]string{"bug": "#E5484D", "ui": "not-a-color"}
	tree := buildTree(in)
	mustValid(t, tree)
	chip := func(id string) *protocol.Node {
		n := find(tree, id)
		if n == nil {
			t.Fatalf("no chip %q in %v", id, ids(tree))
		}
		return n
	}
	if n := chip("c:id-1-cb"); n.Text != "2" || n.Icon != "blocked" || n.Chip == "" || n.Label == "" {
		t.Errorf("blocked chip: %+v", n)
	}
	if n := chip("c:id-1-ck"); n.Text != "1" || n.Icon != "blocking" {
		t.Errorf("blocking chip: %+v", n)
	}
	if n := chip("c:id-1-cs"); n.Text != "3" || n.Icon != "subissue" {
		t.Errorf("sub-issue chip: %+v", n)
	}
	if n := chip("c:id-1-cm"); n.Text != "M1 Foundation" || n.Icon != "milestone" || n.Chip != protocol.ChipNeutral {
		t.Errorf("milestone chip: %+v", n)
	}
	if n := chip("c:id-1-l0"); n.Chip != "#E5484D" {
		t.Errorf("a label takes the color the pane read: %+v", n)
	}
	if n := chip("c:id-1-l1"); n.Chip != protocol.ChipNeutral {
		t.Errorf("a label with a color that is no hex is quiet: %+v", n)
	}
	if n := chip("c:id-1-cx"); n.Text != "+1" {
		t.Errorf("the labels past the card's room are counted: %+v", n)
	}

	in.settings.Compact = true
	tree = buildTree(in)
	if find(tree, "c:id-1-cb") != nil || find(tree, "c:id-1-l0") != nil {
		t.Fatal("a compact card has no chips")
	}
}

func TestTeamStateColorsMarkTheState(t *testing.T) {
	s := workspace()
	for i := range s.States {
		switch s.States[i].Name {
		case "In Progress":
			s.States[i].Color = "#4CB782"
		case "Todo":
			s.States[i].Color = "javascript:alert(1)"
		}
	}
	tree := buildTree(input(s))
	mustValid(t, tree)
	if n := find(tree, "i:id-1-s"); n == nil || n.Hex != "#4CB782" {
		t.Fatalf("the list marks a state in the team's color: %+v", n)
	}
	if n := find(tree, "i:id-4-s"); n == nil || n.Hex != "" {
		t.Fatalf("a color that is no hex is dropped: %+v", n)
	}
	in := input(s)
	in.st.Mode = modeBoard
	tree = buildTree(in)
	mustValid(t, tree)
	if n := find(tree, "c:id-1-n"); n == nil || n.Hex != "#4CB782" {
		t.Fatalf("the card marks a state in the team's color: %+v", n)
	}
}

func TestDetailColorsLabelsAndOffersCopies(t *testing.T) {
	s := workspace()
	in := input(s)
	in.st.Open = "id-1"
	d := fullDetail()
	d.LabelColors = []proto.LinearLabel{{Name: "bug", Color: "#E5484D"}, {Name: "ui", Color: "#2F6FED"}}
	in.detail = d
	tree := buildTree(in)
	mustValid(t, tree)
	if n := find(tree, "lb0"); n == nil || n.Chip != "#E5484D" || n.Text != "bug" {
		t.Fatalf("a label is a chip in its color: %+v", n)
	}
	if n := find(tree, "lb1"); n == nil || n.Chip != "#2F6FED" {
		t.Fatalf("second label: %+v", n)
	}
	for _, id := range []string{"d-copy-id", "d-copy-link", "d-copy-branch", "run0-copy", "run1-copy", "d-pos", "d-prev", "d-next"} {
		if find(tree, id) == nil {
			t.Errorf("no %q in the open issue", id)
		}
	}
	if n := find(tree, "d-pos"); n.Text != "1 / 6" {
		t.Errorf("position = %q", n.Text)
	}
	if find(tree, "d-prev").Enabled == nil || *find(tree, "d-prev").Enabled {
		t.Error("the first issue has no previous one")
	}
	// A run with no resume id has nothing to copy.
	d.Runs[1].ID = ""
	if find(buildTree(in), "run1-copy") != nil {
		t.Error("a run with no id offers to copy one")
	}
}

func TestWidePaneShowsTheIssueBesideTheIssues(t *testing.T) {
	s := workspace()
	for _, mode := range []string{modeList, modeBoard} {
		in := input(s)
		in.st.Mode = mode
		in.st.Open = "id-1"
		in.detail = fullDetail()
		in.notice = "boom"

		in.width = wideWidth - 1
		narrow := buildTree(in)
		mustValid(t, narrow)
		if find(narrow, "split") != nil || find(narrow, "d-title") == nil {
			t.Fatalf("%s: below %d the issue takes the pane's place", mode, wideWidth)
		}

		in.width = wideWidth
		wide := buildTree(in)
		validate(t, wide)
		if wide.Scroll || wide.Header != nil {
			t.Fatalf("%s: a wide pane scrolls in its two sides, not as one", mode)
		}
		split := find(wide, "split")
		if split == nil || len(split.Children) != 2 {
			t.Fatalf("%s: no split: %v", mode, ids(wide))
		}
		main, side := split.Children[0], split.Children[1]
		if main.ID != "main" || !main.ScrollY || main.Grow == 0 || side.ID != "side" || !side.ScrollY || side.Width != sideWidth {
			t.Fatalf("%s: sides = %+v / %+v", mode, main, side)
		}
		if find(protocol.Tree{Root: side}, "d-title") == nil || find(protocol.Tree{Root: side}, "d-hdr") == nil {
			t.Fatalf("%s: the issue is in the side", mode)
		}
		prefix := "i:id-3"
		if mode == modeBoard {
			prefix = "c:id-3"
		}
		if find(protocol.Tree{Root: main}, prefix) == nil {
			t.Fatalf("%s: the issues stay in the main side", mode)
		}
		if !find(wide, map[string]string{modeList: "i:id-1", modeBoard: "c:id-1"}[mode]).Active {
			t.Fatalf("%s: the open issue is marked among the issues", mode)
		}
		if find(wide, "hdr") == nil || find(wide, "filters") == nil || find(wide, "q") == nil {
			t.Fatalf("%s: the search and the filters stay above both sides", mode)
		}
		if !slices.Equal(wide.Keys, detailKeys) {
			t.Fatalf("%s: keys = %v", mode, wide.Keys)
		}
		// One notice only, whichever side holds it.
		count := 0
		for _, id := range ids(wide) {
			if id == "notice" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%s: %d notices", mode, count)
		}
	}

	// Wide with nothing open is the pane as before.
	in := input(s)
	in.width = 1400
	tree := buildTree(in)
	mustValid(t, tree)
	if find(tree, "split") != nil {
		t.Fatal("nothing is open, so nothing is beside the issues")
	}
}

// --- the plugin: keys, the menu, the clipboard ---

type clipboard struct {
	mu    sync.Mutex
	texts []string
}

func (c *clipboard) copy(text string) error {
	c.mu.Lock()
	c.texts = append(c.texts, text)
	c.mu.Unlock()
	return nil
}

// all are the copies so far, sorted: each runs off the handler, so they
// arrive in no order.
func (c *clipboard) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := slices.Clone(c.texts)
	slices.Sort(out)
	return out
}

func (r *rig) key(pane, key string) {
	r.p.event(nil, protocol.EventParams{Pane: pane, Node: "keys", Kind: protocol.KindKey, Value: key})
}

func (r *rig) context(pane, node, item string) {
	r.p.event(nil, protocol.EventParams{Pane: pane, Node: node, Kind: protocol.KindContext, Value: item})
}

func shownRig(t *testing.T) (*rig, *clipboard) {
	t.Helper()
	r := newRig(t)
	clip := &clipboard{}
	r.p.copyText = clip.copy
	r.d.handlers["linear.issue"] = func(params any, out any) error {
		d := fullDetail()
		d.LabelColors = []proto.LinearLabel{{Name: "bug", Color: "#E5484D"}}
		*(out.(*proto.LinearIssueDetail)) = *d
		return nil
	}
	r.open("p1", "")
	r.p.setVisible(nil, "p1", true)
	r.d.push(stateOfSnap(workspace(), session("s1", "pr", "p1")))
	return r, clip
}

func TestKeysStepTheSelectionOpenAndClose(t *testing.T) {
	r, _ := shownRig(t)
	row := func(id string) *protocol.Node { return find(r.out.last(t, "p1"), "i:"+id) }
	order := rowIDs(r.out.last(t, "p1"))
	if len(order) < 3 {
		t.Fatalf("rows = %v", order)
	}
	if find(r.out.last(t, "p1"), "i:"+order[0]).Active {
		t.Fatal("nothing is selected before a key")
	}

	r.key("p1", "j")
	if !row(order[0]).Active {
		t.Fatalf("J from nothing selects the first issue %q", order[0])
	}
	r.key("p1", "down")
	r.key("p1", "j")
	if !row(order[2]).Active || row(order[0]).Active {
		t.Fatal("J and the down arrow step down")
	}
	r.key("p1", "k")
	r.key("p1", "up")
	if !row(order[0]).Active {
		t.Fatal("K and the up arrow step up")
	}
	r.key("p1", "k")
	if !row(order[0]).Active {
		t.Fatal("the selection stops at the first issue")
	}

	r.key("p1", "j")
	r.key("p1", "enter")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })
	opened := find(r.out.last(t, "p1"), "d-ident")
	if opened == nil || opened.Text != "BRU-"+strings.TrimPrefix(order[1], "id-") {
		t.Fatalf("Enter opens the selected issue: %+v", opened)
	}

	// An open issue steps with the sideways arrows and J and K.
	r.key("p1", "right")
	waitFor(t, "the next issue", func() bool {
		n := find(r.out.last(t, "p1"), "d-ident")
		return n != nil && n.Text == "BRU-"+strings.TrimPrefix(order[2], "id-")
	})
	r.key("p1", "left")
	waitFor(t, "the previous issue", func() bool {
		n := find(r.out.last(t, "p1"), "d-ident")
		return n != nil && n.Text == "BRU-"+strings.TrimPrefix(order[1], "id-")
	})
	r.press("p1", "d-next")
	waitFor(t, "the next issue by its button", func() bool {
		n := find(r.out.last(t, "p1"), "d-ident")
		return n != nil && n.Text == "BRU-"+strings.TrimPrefix(order[2], "id-")
	})

	r.key("p1", "escape")
	if find(r.out.last(t, "p1"), "sec:current") == nil || find(r.out.last(t, "p1"), "d-title") != nil {
		t.Fatal("Escape closes the open issue")
	}
	if !row(order[2]).Active {
		t.Fatal("the issue that was open stays selected")
	}
	r.key("p1", "escape")
	r.key("p1", "enter")
	if find(r.out.last(t, "p1"), "d-title") == nil {
		t.Fatal("Enter opens the issue that stayed selected")
	}
}

func TestBoardKeysFollowTheColumns(t *testing.T) {
	r, _ := shownRig(t)
	r.pick("p1", "mode", modeBoard)
	r.key("p1", "j")
	tree := r.out.last(t, "p1")
	first := ""
	for _, id := range ids(tree) {
		if strings.HasPrefix(id, "c:id-") && strings.Count(id, "-") == 1 {
			first = id
			break
		}
	}
	if first == "" || !find(tree, first).Active {
		t.Fatalf("the first card of the board is selected: %q", first)
	}
}

func TestFoldedIssuesAreSkippedByTheKeys(t *testing.T) {
	r, _ := shownRig(t)
	before := rowIDs(r.out.last(t, "p1"))
	r.press("p1", "sec:current")
	after := rowIDs(r.out.last(t, "p1"))
	if len(after) >= len(before) {
		t.Fatal("folding drew no less")
	}
	r.key("p1", "j")
	r.p.mu.Lock()
	sel := r.p.panes["p1"].sel
	r.p.mu.Unlock()
	if !slices.Contains(after, sel) {
		t.Fatalf("selected %q, which is folded away from %v", sel, after)
	}
}

func TestContextMenuActions(t *testing.T) {
	r, clip := shownRig(t)
	r.context("p1", "i:id-4", "m-copy-id")
	r.context("p1", "i:id-4", "m-copy-link")
	waitFor(t, "two copies", func() bool { return len(clip.all()) == 2 })
	if got := clip.all(); !slices.Equal(got, []string{"BRU-4", "https://linear.app/team/issue/BRU-4"}) {
		t.Fatalf("copied %v", got)
	}

	r.context("p1", "c:id-4", "m-ext")
	waitFor(t, "the link to open", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.urls) == 1 })
	if r.urls[0] != "https://linear.app/team/issue/BRU-4" {
		t.Fatalf("opened %v", r.urls)
	}

	r.context("p1", "i:id-4", "mv:"+slug("In Review"))
	waitFor(t, "the move", func() bool { return len(r.d.called("linear.setState")) == 1 })
	if m := r.d.called("linear.setState")[0].params.(proto.LinearParams); m.Issue != "id-4" || m.State != "In Review" {
		t.Fatalf("moved %+v", m)
	}
	// BRU-4 is on its way to In Review: a move to that state, or to none,
	// asks for nothing.
	r.context("p1", "i:id-4", "mv:"+slug("In Review"))
	r.context("p1", "i:id-4", "mv:nowhere")
	time.Sleep(20 * time.Millisecond)
	if got := len(r.d.called("linear.setState")); got != 1 {
		t.Fatalf("setState called %d times; moving to its own state or to nowhere should not call", got)
	}

	r.d.handlers["create"] = func(params any, out any) error {
		*(out.(*proto.Session)) = proto.Session{ID: "fresh"}
		return nil
	}
	r.context("p1", "i:id-4", "m-start")
	waitFor(t, "create", func() bool { return len(r.d.called("create")) == 1 })
	if c := r.d.called("create")[0].params.(proto.CreateParams); c.Task != "BRU-4" || c.Name != "bru-4" {
		t.Fatalf("started %+v", c)
	}

	r.context("p1", "i:id-4", "m-open")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })

	// An unknown issue or entry does nothing.
	n := r.out.count("p1")
	r.context("p1", "i:gone", "m-open")
	r.context("p1", "i:id-4", "m-unknown")
	if r.out.count("p1") != n {
		t.Fatal("an unknown issue or entry drew the pane")
	}
}

func TestDetailCopiesGoToTheClipboard(t *testing.T) {
	r, clip := shownRig(t)
	r.press("p1", "i:id-1")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })
	for _, id := range []string{"d-copy-id", "d-copy-link", "d-copy-branch", "run0-copy", "run1-copy"} {
		r.press("p1", id)
	}
	waitFor(t, "five copies", func() bool { return len(clip.all()) == 5 })
	want := []string{"BRU-1", "abc123", "bru-1-fix", "def456", "https://linear.app/team/issue/BRU-1"}
	if got := clip.all(); !slices.Equal(got, want) {
		t.Fatalf("copied %v, want %v", got, want)
	}
}

func TestLabelColorsReadWithAnIssueColorTheCards(t *testing.T) {
	r, _ := shownRig(t)
	// The board's issues carry names only; the card is quiet until an issue
	// that has the label is read.
	r.d.push(stateOfSnap(snap(issue(1, "started", func(i *proto.LinearIssue) { i.Labels = []string{"bug"} })), session("s1", "pr", "p1")))
	r.pick("p1", "mode", modeBoard)
	if n := find(r.out.last(t, "p1"), "c:id-1-l0"); n == nil || n.Chip != protocol.ChipNeutral {
		t.Fatalf("before a read: %+v", n)
	}
	r.press("p1", "c:id-1")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })
	r.press("p1", "d-back")
	if n := find(r.out.last(t, "p1"), "c:id-1-l0"); n == nil || n.Chip != "#E5484D" {
		t.Fatalf("after a read the card has the label's color: %+v", n)
	}
}

func TestCrossingTheWideWidthDrawsAgain(t *testing.T) {
	r, _ := shownRig(t)
	r.press("p1", "i:id-1")
	waitFor(t, "the detail", func() bool { return find(r.out.last(t, "p1"), "d-desc") != nil })
	if find(r.out.last(t, "p1"), "split") != nil {
		t.Fatal("600 wide is narrow")
	}
	n := r.out.count("p1")
	r.p.resize(nil, "p1", protocol.Size{Width: 700, Height: 500})
	if r.out.count("p1") != n {
		t.Fatal("a resize that stays narrow drew the pane")
	}
	r.p.resize(nil, "p1", protocol.Size{Width: 1000, Height: 500})
	if find(r.out.last(t, "p1"), "split") == nil {
		t.Fatal("1000 wide shows the issue beside the issues")
	}
	r.p.resize(nil, "p1", protocol.Size{Width: 1200, Height: 500})
	n = r.out.count("p1")
	r.p.resize(nil, "p1", protocol.Size{Width: 400, Height: 500})
	if r.out.count("p1") != n+1 || find(r.out.last(t, "p1"), "split") != nil {
		t.Fatal("shrinking below the width takes the issue back to the pane's place")
	}
}

func TestPaleStateColorsFallBackToTheRoleColor(t *testing.T) {
	for hex, want := range map[string]bool{
		"#4CB782": true, "#5E6AD2": true, "#95A2B3": true,
		"#E2E2E2": false, "#BEC2C8": false, "#F2C94C": false, "#0A0A0A": false, "": false, "red": false,
	} {
		if got := readableHex(hex); got != want {
			t.Errorf("readableHex(%q) = %v, want %v", hex, got, want)
		}
	}
	s := workspace()
	for i := range s.States {
		if s.States[i].Name == "Todo" {
			s.States[i].Color = "#E2E2E2"
		}
	}
	tree := buildTree(input(s))
	if n := find(tree, "i:id-4-s"); n == nil || n.Hex != "" {
		t.Fatalf("a pale grey is drawn as it is: %+v", n)
	}
}

func TestContextMoveShowsAtOnceAndRevertsOnError(t *testing.T) {
	r, _ := shownRig(t)
	section := func(id string) string {
		tree := r.out.last(t, "p1")
		for _, sec := range []string{secCurrent, secDone, secTodo, secBacklog} {
			if h := find(tree, "sec:"+sec); h != nil {
				// The issue stands after its section's header and before the next.
				all := ids(tree)
				at, hd := slices.Index(all, "i:"+id), slices.Index(all, "sec:"+sec)
				next := len(all)
				for _, other := range sectionOrder {
					if i := slices.Index(all, "sec:"+other); i > hd && i < next {
						next = i
					}
				}
				if at > hd && at < next {
					return sec
				}
			}
		}
		return ""
	}
	if got := section("id-4"); got != secTodo {
		t.Fatalf("BRU-4 starts in %q", got)
	}
	release := make(chan struct{})
	r.d.handlers["linear.setState"] = func(any, any) error {
		<-release
		return errors.New("linear refused")
	}
	r.context("p1", "i:id-4", "mv:"+slug("In Progress"))
	if got := section("id-4"); got != secCurrent {
		t.Fatalf("the row did not move at once: %q", got)
	}
	close(release)
	waitFor(t, "the move back", func() bool { return section("id-4") == secTodo })
	if section("id-4") != secTodo {
		t.Fatal("a refused move stayed on the board")
	}
}

func TestPruneFoldKeysOfWhatIsGone(t *testing.T) {
	r, _ := shownRig(t)
	r.p.mu.Lock()
	r.p.panes["p1"].st.Collapsed = []string{
		"sec:done", "kid:id-1", "kid:gone", "grp:current:" + slug("Rig"), "grp:current:" + slug("Vanished"),
	}
	r.p.mu.Unlock()
	st := stateOfSnap(workspace(), session("s1", "pr", "p1"))
	st.Linear.FetchedAt++ // a new snapshot
	r.d.push(st)
	r.p.mu.Lock()
	got := slices.Clone(r.p.panes["p1"].st.Collapsed)
	r.p.mu.Unlock()
	want := []string{"sec:done", "kid:id-1", "grp:current:" + slug("Rig")}
	if !slices.Equal(got, want) {
		t.Fatalf("collapsed %v, want %v", got, want)
	}

	// An empty read says nothing of what exists.
	empty := stateOfSnap(snap(), session("s1", "pr", "p1"))
	r.d.push(empty)
	r.p.mu.Lock()
	got = slices.Clone(r.p.panes["p1"].st.Collapsed)
	r.p.mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("an empty snapshot pruned the folds: %v", got)
	}
}

func TestLabelColorsComeWithTheSnapshot(t *testing.T) {
	s := workspace()
	s.Issues[0].Labels = []string{"bug"}
	s.LabelColors = map[string]string{"bug": "#E5484D"}
	in := input(s)
	in.st.Mode = modeBoard
	tree := buildTree(in)
	if n := find(tree, "c:id-1-l0"); n == nil || n.Chip != "#E5484D" {
		t.Fatalf("the card's label has no color from the snapshot: %+v", n)
	}
	st := stateOfSnap(s)
	st.Linear.LabelColors = s.LabelColors
	if got := snapshotOf(st).LabelColors["bug"]; got != "#E5484D" {
		t.Fatalf("snapshotOf dropped the colors: %q", got)
	}
}

func TestKeysStepOnlyThroughDrawnIssues(t *testing.T) {
	st := bigState(1500)
	s := snapshotPtr(st)
	in := input(s)
	in.st.Mode = modeBoard
	tree, order := buildTreeOrder(in)
	mustValid(t, tree)
	drawn := 0
	for _, id := range ids(tree) {
		if strings.HasPrefix(id, "c:") && strings.Count(id, "-") == 1 {
			drawn++
		}
	}
	if drawn == 0 || len(order) != drawn || drawn > boardCardCap {
		t.Fatalf("the board draws %d cards, the keys step through %d (cap %d)", drawn, len(order), boardCardCap)
	}
	in.st.Mode = modeList
	tree, order = buildTreeOrder(in)
	rows := 0
	for _, id := range ids(tree) {
		if strings.HasPrefix(id, "i:") && strings.Count(id, "-") == 1 {
			rows++
		}
	}
	if rows == 0 || len(order) != rows {
		t.Fatalf("the list draws %d rows, the keys step through %d", rows, len(order))
	}
}

func TestFilterRowFitsAndNamesItsControls(t *testing.T) {
	tree := buildTree(input(workspace()))
	if n := find(tree, "status"); n == nil || strings.Contains(n.Text, "Synced") {
		t.Fatalf("the count carries the sync time in a narrow row: %+v", n)
	}
	if n := find(tree, "refresh"); n == nil || !strings.Contains(n.Label, "Synced") {
		t.Fatalf("the refresh button does not say when the workspace was read: %+v", n)
	}
	if n := find(tree, "f-canceled"); n == nil || n.Type != protocol.NodeButton || n.Label == "" {
		t.Fatalf("the canceled switch is no labeled button: %+v", n)
	}
	if n := find(tree, "i:id-1-s"); n == nil || n.Label != "Status: In Progress" {
		t.Fatalf("the state glyph has no label: %+v", n)
	}
	if n := find(tree, "i:id-4-p"); n == nil || n.Label != "Priority: Urgent" {
		t.Fatalf("the priority glyph has no label: %+v", n)
	}
}
