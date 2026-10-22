package linear

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
	"kero/locales"
)

// The tree of the pane: the header, the filters and the issues, as a list or
// as a board, or one issue in full. buildTree is a pure function of its
// input, so that every mode is tested without a host.

// The modes of the pane.
const (
	modeList  = "list"
	modeBoard = "board"
)

// boardColumnW is the width of a board column, in points; boardCardCap is
// how many cards the board draws at most.
const (
	boardColumnW = 260
	boardCardCap = 250
)

// wideWidth is how wide a pane is, in points, for the open issue to stand
// beside the issues instead of in their place; sideWidth is how wide the
// issue's side is then.
const (
	wideWidth = 900
	sideWidth = 420
)

// reserve is how many nodes of the budget the pane keeps for what it draws
// after the issues: a note that some are not shown, and the structure.
const reserve = 30

// paneState is what a pane keeps in its target.
type paneState struct {
	Mode      string   `json:"mode,omitempty"`
	Filters   filters  `json:"filters"`
	Query     string   `json:"query,omitempty"`
	Collapsed []string `json:"collapsed,omitempty"`
	Open      string   `json:"open,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	DoneAll   bool     `json:"doneAll,omitempty"`

	// HideDone is what the Tasks pane this one replaces kept: the Done
	// section starts folded.
	HideDone *bool `json:"hideDone,omitempty"`
}

// settings are the plugin's settings.
type settings struct {
	// HideEmptyColumns leaves the board's empty columns out.
	HideEmptyColumns bool
	// Compact draws the rows and the cards with less on them.
	Compact bool
}

// viewInput is everything a tree is drawn from.
type viewInput struct {
	pane     string
	snap     *snapshot
	st       paneState
	focus    focusInfo
	now      time.Time
	settings settings

	// connected is whether the pane reaches the daemon; down says why not.
	connected bool
	down      string
	// notice is the last thing that went wrong, shown under the header.
	notice string

	// detail is the open issue as read; nil while it is read.
	detail    *proto.LinearIssueDetail
	detailErr string
	// moving is the state an issue was asked to move to, until the board
	// has it.
	moving string

	// width is the pane's width in points; sel is the issue the keys have
	// selected, which the open issue overrides; labelColors are the colors
	// of the labels the pane has read, by name.
	width       int
	sel         string
	labelColors map[string]string
}

// builder draws a tree within the budget of nodes the protocol allows.
type builder struct {
	in      viewInput
	left    int
	skipped int
	col     map[string]bool
	// order is the issues in the order the pane draws them, which the keys
	// step through: only those that made it into the tree.
	order []string

	// selID is the issue the pane marks, worked out once for the tree.
	selID   string
	selDone bool
	// moves are the items of the Move To menu, by the state of the issue
	// they belong to: one list for all the issues in a state.
	moves map[string][]protocol.Item
	// words are the spoken names of the marks, by state and by priority:
	// worked out once, not for each row.
	words map[string]string
	// menus are the context menus, by the kind of issue they are for.
	menus map[string][]protocol.Item
}

// selected is the issue the pane marks: the open one, or the one the keys
// chose.
func (b *builder) selected() string {
	if b.selDone {
		return b.selID
	}
	b.selDone = true
	switch {
	case b.in.st.Open == "":
		b.selID = b.in.sel
	default:
		b.selID = b.in.st.Open
		if is := b.in.snap.issue(b.in.st.Open); is != nil {
			b.selID = is.ID
		}
	}
	return b.selID
}

// count is how many nodes a part and its children make.
func count(n *protocol.Node) int {
	c := 1
	for _, k := range n.Children {
		c += count(k)
	}
	return c
}

// take spends the part's nodes from the budget, or says it does not fit.
func (b *builder) take(n *sdk.Node) bool {
	c := count(n.Build())
	if c > b.left {
		return false
	}
	b.left -= c
	return true
}

func txt(id, text string) *sdk.Node { return sdk.Text(id, text) }

func caption(id, text string) *sdk.Node {
	return sdk.Text(id, text).Role(protocol.RoleCaption).Color(protocol.ColorMuted).Lines(1)
}

// buildTree draws the pane.
func buildTree(in viewInput) protocol.Tree {
	t, _ := buildTreeOrder(in)
	return t
}

// The keys a pane asks to hear: the list steps with J and K or the arrows
// and opens with Enter; an open issue steps with those and the sideways
// arrows, and closes with Escape.
var (
	listKeys   = []string{"j", "k", "up", "down", "enter"}
	detailKeys = []string{"j", "k", "up", "down", "left", "right", "escape"}
)

// buildTreeOrder draws the pane, and tells the issues in the order it shows
// them, which the keys step through.
func buildTreeOrder(in viewInput) (protocol.Tree, []string) {
	b := &builder{in: in, left: protocol.MaxNodes - 1, col: map[string]bool{}}
	for _, k := range in.st.Collapsed {
		b.col[k] = true
	}
	var kids []*sdk.Node
	add := func(n *sdk.Node) {
		if n != nil {
			kids = append(kids, n)
			b.left -= count(n.Build())
		}
	}
	// finish makes the tree of the pane: its parts in one scroll, under a
	// header that stays when given.
	finish := func(header *sdk.Node, keys []string) (protocol.Tree, []string) {
		if header != nil {
			b.left -= count(header.Build())
		}
		t := sdk.Column("root", kids...).Padding(10).Gap(8).ScrollingTreeUnder(header)
		t.Keys = keys
		return t, b.order
	}

	snap := in.snap
	if !in.connected || snap == nil || !snap.Has || snap.Status != proto.LinearOK && len(snap.Issues) == 0 {
		add(b.unread())
		return finish(nil, nil)
	}
	scopeName, linked, scoped := scopeProject(snap, in.pane, in.st.Scope)
	var issues []proto.LinearIssue
	var secs []section
	if linked {
		issues = filterIssues(snap, in.st.Filters, in.st.Query, scopeName)
		secs = buildSections(snap, issues, in.focus, in.st.DoneAll, in.st.Filters.Canceled)
	}
	wide := in.width >= wideWidth && in.st.Open != "" && linked
	if in.st.Open != "" && !wide {
		// The issues are not drawn, but the keys still step through them: in
		// the order, and with the cut, of a tree that draws them.
		b.order = b.dryOrder(secs)
		head, body := b.detailParts(true)
		for _, n := range body {
			add(n)
		}
		return finish(sdk.Column("top", head).Padding(10), detailKeys)
	}

	if !linked {
		add(b.linkPrompt())
		return finish(sdk.Column("top", b.header("")).Padding(10).Gap(8), nil)
	}
	top := []*sdk.Node{b.header(b.statusText(scoped)), b.filterRow(scoped)}
	if in.notice != "" {
		top = append(top, sdk.Text("notice", in.notice).Role(protocol.RoleCaption).Color(protocol.ColorDanger).Lines(2))
	}
	if wide {
		t := b.splitTree(top, secs)
		return t, b.order
	}
	header := sdk.Column("top", top...).Padding(10).Gap(8)
	if len(secs) == 0 {
		add(b.nothing())
		return finish(header, nil)
	}
	b.left -= count(header.Build())
	for _, n := range b.body(secs) {
		kids = append(kids, n)
	}
	b.left += count(header.Build())
	return finish(header, listKeys)
}

// body draws the sections as the board or the list, with the note that
// some issues are not shown.
func (b *builder) body(secs []section) []*sdk.Node {
	b.left -= reserve
	var body []*sdk.Node
	if b.in.st.Mode == modeBoard {
		body = b.board(secs)
	} else {
		body = b.list(secs)
	}
	b.left += reserve
	if b.skipped > 0 {
		body = append(body, caption("trunc", locales.LinearNotShown(itoa(b.skipped))))
	}
	return body
}

// dryOrder is the order of the issues a tree would draw with the whole
// budget, for the parts that need it before the issues are drawn.
func (b *builder) dryOrder(secs []section) []string {
	if len(secs) == 0 {
		return nil
	}
	dry := &builder{in: b.in, left: protocol.MaxNodes - 1, col: b.col}
	dry.body(secs)
	return dry.order
}

// splitTree is the pane wide enough for the open issue to stand beside the
// issues: the header above, then the issues and the issue side by side,
// each scrolling on its own.
func (b *builder) splitTree(top []*sdk.Node, secs []section) protocol.Tree {
	// The open issue says its place among the issues, before they are drawn.
	b.order = b.dryOrder(secs)
	head, side := b.detailParts(false)
	b.order = nil
	side = append([]*sdk.Node{head}, side...)
	for _, n := range top {
		b.left -= count(n.Build())
	}
	for _, n := range side {
		b.left -= count(n.Build())
	}
	var main []*sdk.Node
	if len(secs) == 0 {
		main = []*sdk.Node{b.nothing()}
	} else {
		main = b.body(secs)
	}
	root := sdk.Column("root",
		sdk.Column("top", top...).Gap(8),
		sdk.Row("split",
			sdk.Column("main", main...).ScrollY().Grow(1).Gap(8),
			sdk.Column("side", side...).ScrollY().Width(sideWidth).Gap(8),
		).Grow(1).Gap(14),
	).Padding(10).Gap(8).Grow(1)
	t := protocol.Tree{Root: root.Build(), Keys: slices.Clone(detailKeys)}
	return t
}

// unread is what the pane says when it has no workspace to show: the
// daemon is out of reach, or Linear was not read.
func (b *builder) unread() *sdk.Node {
	in := b.in
	switch {
	case !in.connected:
		detail := in.down
		if detail == "" {
			detail = locales.LinearConnecting()
		}
		return sdk.EmptyState("empty-down", locales.LinearServiceDown(), detail)
	case in.snap == nil || !in.snap.Has || in.snap.Status == proto.LinearNoToken:
		return sdk.EmptyState("empty-unread", locales.LinearNotRead(), locales.LinearNotReadDetail()).Action(locales.LinearRefresh())
	}
	return sdk.EmptyState("empty-unread", locales.LinearNotRead(), statusLabel(in.snap, 0, in.now)).Action(locales.LinearRefresh())
}

// statusLabel says how many issues show and when they were read, or why the
// workspace was not.
func statusLabel(snap *snapshot, shown int, now time.Time) string {
	switch snap.Status {
	case proto.LinearNoToken:
		return locales.LinearStatusNoToken()
	case proto.LinearAuthFailed:
		return locales.LinearStatusAuthFailed()
	case proto.LinearRateLimited:
		return locales.LinearStatusRateLimited()
	case proto.LinearUnreachable:
		return locales.LinearStatusUnreachable()
	case proto.LinearDisabled:
		return locales.LinearStatusDisabled()
	}
	text := countLabel(snap, shown)
	if when := proto.Ago(snap.FetchedAt, now); when != "" {
		return text + " · " + locales.LinearSynced(when)
	}
	return text
}

// countLabel says how many issues show.
func countLabel(snap *snapshot, shown int) string {
	if shown >= 0 && shown != len(snap.Issues) {
		return fmt.Sprintf("%d / %d %s", shown, len(snap.Issues), locales.LinearIssues())
	}
	return fmt.Sprintf("%d %s", len(snap.Issues), locales.LinearIssues())
}

// syncedLabel says when the workspace was read, "" when it is not known.
func syncedLabel(snap *snapshot, now time.Time) string {
	if snap == nil || snap.Status != proto.LinearOK {
		return ""
	}
	if when := proto.Ago(snap.FetchedAt, now); when != "" {
		return locales.LinearSynced(when)
	}
	return ""
}

// header is the search, the two ways of showing the issues, and the refresh.
func (b *builder) header(status string) *sdk.Node {
	mode := b.in.st.Mode
	kids := []*sdk.Node{
		sdk.SearchField("q", locales.LinearSearch()).Query(b.in.st.Query).Grow(1),
		sdk.Segmented("mode",
			protocol.Item{ID: modeList, Title: locales.LinearList(), Active: mode != modeBoard},
			protocol.Item{ID: modeBoard, Title: locales.LinearBoard(), Active: mode == modeBoard},
		),
		sdk.IconButton("refresh", "refresh").Label(b.refreshLabel()),
	}
	if status != "" {
		kids = append(kids, caption("status", status))
	}
	return sdk.Row("hdr", kids...).Gap(8).Align(protocol.AlignCenter)
}

// statusText is what stands at the end of the header: how many issues show,
// or why none could be read. When the workspace was read is the refresh
// button's, so that the text fits a narrow pane.
func (b *builder) statusText(scoped bool) string {
	if b.in.snap.Status == proto.LinearOK {
		return countLabel(b.in.snap, b.shownCount(scoped))
	}
	return statusLabel(b.in.snap, 0, b.in.now)
}

// refreshLabel names the refresh button, with when the workspace was read.
func (b *builder) refreshLabel() string {
	if when := syncedLabel(b.in.snap, b.in.now); when != "" {
		return locales.LinearRefreshSynced(when)
	}
	return locales.LinearRefresh()
}

// filterRow is the menus that narrow the issues, the switch for the
// canceled ones, and at the right how many issues show.
func (b *builder) filterRow(scoped bool) *sdk.Node {
	snap, f := b.in.snap, b.in.st.Filters
	label := func(name, value string) string {
		if value == "" {
			return name
		}
		return locales.LinearFilterValue(name, value)
	}

	assignee := []protocol.Item{
		{ID: "any", Title: locales.LinearAnyone(), Active: f.Assignee == assigneeAny},
		{ID: "me", Title: locales.LinearMe(), Active: f.Assignee == assigneeMe},
	}
	value := ""
	switch f.Assignee {
	case assigneeMe:
		value = locales.LinearMe()
	case assigneeAny:
	default:
		value = f.Assignee
	}
	for _, p := range people(snap) {
		assignee = append(assignee, protocol.Item{ID: "u:" + p.ID, Title: p.Name, Active: f.Assignee == p.ID})
		if f.Assignee == p.ID {
			value = p.Name
		}
	}
	nodes := []*sdk.Node{
		sdk.Menu("f-assignee", label(locales.LinearAssignee(), value), capItems(assignee)...).Icon("person"),
	}

	if !scoped {
		projects := []protocol.Item{{ID: "all", Title: locales.LinearAllProjects(), Active: f.Project == ""}}
		for _, name := range projectNames(snap) {
			projects = append(projects, protocol.Item{ID: "p:" + slug(name), Title: name, Active: f.Project == name})
		}
		nodes = append(nodes, sdk.Menu("f-project", label(locales.LinearProject(), f.Project), capItems(projects)...).Icon("folder"))
	}

	if labels := labelsOf(snap); len(labels) > 0 || f.Label != "" {
		items := []protocol.Item{{ID: "any", Title: locales.LinearAnyLabel(), Active: f.Label == ""}}
		for _, l := range labels {
			items = append(items, protocol.Item{ID: "l:" + slug(l), Title: l, Active: f.Label == l})
		}
		nodes = append(nodes, sdk.Menu("f-label", label(locales.LinearLabel(), f.Label), capItems(items)...).Icon("tag"))
	}

	priority := []protocol.Item{{ID: "any", Title: locales.LinearAnyPriority(), Active: f.Priority == ""}}
	pvalue := ""
	for p := 1; p <= 4; p++ {
		on := f.Priority == itoa(p)
		priority = append(priority, protocol.Item{ID: "pr:" + itoa(p), Title: priorityName(p), Active: on})
		if on {
			pvalue = priorityName(p)
		}
	}
	none := f.Priority == "0"
	priority = append(priority, protocol.Item{ID: "pr:0", Title: priorityName(0), Active: none})
	if none {
		pvalue = priorityName(0)
	}
	nodes = append(nodes, sdk.Menu("f-priority", label(locales.LinearPriority(), pvalue), priority...).Icon("flag"))

	nodes = append(nodes, sdk.Button("f-canceled", locales.LinearCanceledToggle()).Active(f.Canceled))
	return sdk.Row("filters", nodes...).Gap(6).Align(protocol.AlignCenter)
}

// shownCount is how many issues the filters keep, for the status text.
func (b *builder) shownCount(scoped bool) int {
	scope := ""
	if scoped {
		scope, _, _ = scopeProject(b.in.snap, b.in.pane, b.in.st.Scope)
	}
	n := 0
	for _, is := range filterIssues(b.in.snap, b.in.st.Filters, b.in.st.Query, scope) {
		if sectionOf(is.StateType) == secCanceled && !b.in.st.Filters.Canceled {
			continue
		}
		n++
	}
	return n
}

// capItems keeps a menu within the protocol's limit.
func capItems(items []protocol.Item) []protocol.Item {
	if len(items) > protocol.MaxMenuItems {
		return items[:protocol.MaxMenuItems]
	}
	return items
}

// linkPrompt asks which Linear project the pane's session's project is
// tracked in: the link is made in the project's menu, and the pane can show
// the whole team's issues until then.
func (b *builder) linkPrompt() *sdk.Node {
	name := ""
	if se := b.in.snap.sessionOfPane(b.in.pane); se != nil {
		for _, p := range b.in.snap.Projects {
			if p.ID == se.Project {
				name = p.Name
			}
		}
	}
	return sdk.EmptyState("empty-scope", locales.TasksLinkTitle(name),
		locales.TasksLinkDetail()+" "+locales.LinearLinkInProjectMenu()).Action(locales.TasksShowAll())
}

// nothing is what the pane says when no issue shows.
func (b *builder) nothing() *sdk.Node {
	in := b.in
	if in.snap.Status != proto.LinearOK {
		return sdk.EmptyState("empty-unread", locales.LinearNotRead(), statusLabel(in.snap, 0, in.now)).Action(locales.LinearRefresh())
	}
	if in.st.Filters.active() || strings.TrimSpace(in.st.Query) != "" {
		return sdk.EmptyState("empty-none", locales.LinearNone(), locales.LinearNoneFiltered()).Action(locales.LinearClearFilters())
	}
	return sdk.EmptyState("empty-none", locales.LinearNone(), locales.LinearNoneDetail()).Action(locales.LinearRefresh())
}

// --- the list ---

// indent is a gap before a row's first part, a level deep.
func indent(id string, level int) *sdk.Node {
	return sdk.Text(id, "").Width(14 * level)
}

// chevron is the fold mark of a section or a group.
func chevron(id string, folded bool) *sdk.Node {
	icon := "chevron-down"
	if folded {
		icon = "chevron-right"
	}
	return sdk.Text(id, "").Icon(icon).Color(protocol.ColorMuted)
}

// list draws the sections one under another: a header, then the groups of
// projects, then the issues, with the sub-issues under their parents. A
// folded section, group or issue draws nothing under its header.
func (b *builder) list(secs []section) []*sdk.Node {
	var out []*sdk.Node
	for _, sec := range secs {
		id := "sec:" + sec.Key
		folded := b.col[id]
		head := sdk.Row(id,
			chevron(id+"-v", folded),
			sdk.Text(id+"-t", sectionTitle(sec.Key)).Icon(stateIcon(sectionStateType(sec.Key))).
				Color(stateColor(sectionStateType(sec.Key))).Role(protocol.RoleStrong),
			caption(id+"-n", itoa(sec.Total)),
		).Pressable().Label(sectionTitle(sec.Key)).Gap(6).Padding(4)
		// A header is never dropped for want of room; the budget reserves
		// the nodes of all of them.
		b.left -= count(head.Build())
		out = append(out, head)
		if folded {
			continue
		}
		groups := groupProjects(sec)
		level := 0
		showGroups := len(groups) > 1 || len(groups) == 1 && groups[0].Name != noProject
		if showGroups {
			level = 1
		}
		for _, g := range groups {
			if showGroups {
				gid := "grp:" + sec.Key + ":" + slug(g.Name)
				gfolded := b.col[gid]
				name := g.Name
				if name == noProject {
					name = locales.LinearNoProject()
				}
				row := sdk.Row(gid,
					indent(gid+"-x", 1),
					chevron(gid+"-v", gfolded),
					sdk.Text(gid+"-t", name).Icon("folder").Lines(1),
					caption(gid+"-n", itoa(g.Count)),
				).Pressable().Label(name).Gap(6).Padding(4)
				if !b.take(row) {
					b.skipped += g.Count
					continue
				}
				out = append(out, row)
				if gfolded {
					continue
				}
			}
			for _, root := range g.Roots {
				out = b.issueRows(out, root, level+1, sec.Key)
			}
		}
		if sec.More > 0 {
			more := sdk.Row("more:"+sec.Key,
				indent("more:"+sec.Key+"-x", 1),
				sdk.Text("more:"+sec.Key+"-t", locales.LinearShowMore(itoa(sec.More))).Color(protocol.ColorAccent),
			).Pressable().Label(locales.LinearShowMore(itoa(sec.More))).Gap(6).Padding(4)
			if b.take(more) {
				out = append(out, more)
			}
		}
	}
	return out
}

// issueRows appends an issue's row and, unless it is folded, the rows of
// its sub-issues.
func (b *builder) issueRows(out []*sdk.Node, n *issueNode, level int, sec string) []*sdk.Node {
	row := b.listRow(n, level)
	if !b.take(row) {
		b.skipped += n.count()
		return out
	}
	out = append(out, row)
	b.order = append(b.order, n.Issue.ID)
	if len(n.Kids) > 0 && !b.col["kid:"+n.Issue.ID] {
		for _, k := range n.Kids {
			out = b.issueRows(out, k, level+1, sec)
		}
	}
	return out
}

// listRow is one issue in the list: the fold mark when it has sub-issues, its
// state, identifier and title, and at the right the session working on it, its
// priority, its assignee and when it changed.
func (b *builder) listRow(n *issueNode, level int) *sdk.Node {
	is := n.Issue
	id := "i:" + is.ID
	kids := []*sdk.Node{}
	if level > 0 {
		kids = append(kids, indent(id+"-x", level))
	}
	if len(n.Kids) > 0 {
		folded := b.col["kid:"+is.ID]
		icon := "chevron-down"
		if folded {
			icon = "chevron-right"
		}
		kids = append(kids, sdk.IconButton("kid:"+is.ID, icon).Size(16, 16).Label(itoa(len(n.Kids))+" "+locales.LinearSubIssues()))
	}
	kids = append(kids,
		b.stateMark(id+"-s", is),
		caption(id+"-n", is.Identifier),
		sdk.Text(id+"-t", is.Title).Lines(1).Grow(1),
	)
	if is.Session != "" {
		kids = append(kids, sdk.Text(id+"-b", is.SessionName).Icon("terminal").Role(protocol.RoleCaption).
			Color(sessionColor(is.SessionStatus)).Lines(1))
	}
	if icon, color := priorityMark(is.Priority); icon != "" {
		kids = append(kids, sdk.Text(id+"-p", "").Icon(icon).Color(color).Label(b.priorityWord(is.Priority)))
	}
	if !b.in.settings.Compact {
		if is.Assignee != "" {
			kids = append(kids, caption(id+"-a", initials(is.Assignee)))
		}
		if when := proto.Ago(is.UpdatedAt, b.in.now); when != "" {
			kids = append(kids, caption(id+"-u", when))
		}
	}
	return sdk.Row(id, kids...).Pressable().Active(b.selected() == is.ID).ContextMenu(b.issueMenu(is)...).
		Label(is.Identifier + " " + is.Title).Gap(8).Align(protocol.AlignCenter).Padding(4)
}

// stateMark is an issue's state as a glyph, in the team's own color of the
// state when the daemon sent it, else in the color of its kind.
func (b *builder) stateMark(id string, is proto.LinearIssue) *sdk.Node {
	n := sdk.Text(id, "").Icon(stateIcon(is.StateType)).Color(stateColor(is.StateType)).Label(b.statusWord(is.State))
	if st, ok := b.in.snap.stateByName(is.State); ok && readableHex(st.Color) {
		n.Hex(st.Color)
	}
	return n
}

// labelColor is a label's color: the one the pane read with an issue, or
// the app's quiet chip when it has not read one.
func (b *builder) labelColor(name string, read []proto.LinearLabel) string {
	for _, l := range read {
		if l.Name == name && protocol.ValidHex(l.Color) {
			return l.Color
		}
	}
	if c := b.in.snap.LabelColors[name]; protocol.ValidHex(c) {
		return c
	}
	if c := b.in.labelColors[name]; protocol.ValidHex(c) {
		return c
	}
	return protocol.ChipNeutral
}

// issueMenu is the menu of a right click on an issue's row or card: open it,
// copy it, move it to another state, start a task on it.
//
// The menu depends on the issue's state, whether its link opens and whether
// a session works on it, so that it is made once for each such kind and the
// issues of it share it.
func (b *builder) issueMenu(is proto.LinearIssue) []protocol.Item {
	openable, linked, working := safeURL(is.URL), is.URL != "", is.Session != ""
	key := fmt.Sprintf("%s|%t|%t|%t", is.State, openable, linked, working)
	if items, ok := b.menus[key]; ok {
		return items
	}
	items := b.makeIssueMenu(is, openable, linked, working)
	if b.menus == nil {
		b.menus = map[string][]protocol.Item{}
	}
	b.menus[key] = items
	return items
}

func (b *builder) makeIssueMenu(is proto.LinearIssue, openable, linked, working bool) []protocol.Item {
	enabled := func(it protocol.Item, on bool) protocol.Item {
		if !on {
			off := false
			it.Enabled = &off
		}
		return it
	}
	items := []protocol.Item{
		sdk.MenuItem("m-open", locales.LinearOpenIssue()),
		enabled(sdk.MenuItem("m-ext", locales.LinearOpenInLinear()), openable),
		sdk.Separator(),
		sdk.MenuItem("m-copy-id", locales.LinearCopyId()),
		enabled(sdk.MenuItem("m-copy-link", locales.LinearCopyLink()), linked),
	}
	if moves := b.moveItems(is.State); len(moves) > 0 {
		items = append(items, sdk.Separator(), sdk.Submenu("m-move", locales.LinearMoveTo(), moves...))
	}
	if !working {
		items = append(items, sdk.Separator(), sdk.MenuItem("m-start", locales.LinearStartTask()))
	}
	return items
}

// statusWord and priorityWord are what assistive technology says of a
// state's and a priority's glyph.
func (b *builder) statusWord(state string) string {
	return b.word("s:"+state, func() string { return locales.LinearStatusIs(state) })
}

func (b *builder) priorityWord(priority int) string {
	return b.word("p:"+itoa(priority), func() string { return locales.LinearPriorityIs(priorityName(priority)) })
}

func (b *builder) word(key string, make func() string) string {
	if w, ok := b.words[key]; ok {
		return w
	}
	if b.words == nil {
		b.words = map[string]string{}
	}
	w := make()
	b.words[key] = w
	return w
}

// moveItems are the choices of the Move To menu of an issue in the state:
// the team's states, the issue's own checked and off. The list is made once
// for each state, and every issue in it shares it.
func (b *builder) moveItems(state string) []protocol.Item {
	if moves, ok := b.moves[state]; ok {
		return moves
	}
	var moves []protocol.Item
	seen := map[string]bool{}
	for _, st := range b.in.snap.sortedStates() {
		id := "mv:" + slug(st.Name)
		if seen[id] || len(moves) >= 20 {
			continue
		}
		seen[id] = true
		it := protocol.Item{ID: id, Title: st.Name, Active: st.Name == state}
		if st.Name == state {
			off := false
			it.Enabled = &off
		}
		moves = append(moves, it)
	}
	if b.moves == nil {
		b.moves = map[string][]protocol.Item{}
	}
	b.moves[state] = moves
	return moves
}

// --- the board ---

// board draws the sections as columns side by side, in a row that scrolls
// sideways. The cards are shared out among the columns, so that a long
// backlog does not leave the others without.
func (b *builder) board(secs []section) []*sdk.Node {
	shown := secs
	if !b.in.settings.HideEmptyColumns {
		shown = b.withEmpty(secs)
	}
	caps := shares(shown, min(boardCardCap, max(b.left, 0)/cardCost))
	cols := make([]*sdk.Node, 0, len(shown))
	for i, sec := range shown {
		cols = append(cols, b.column(sec, caps[i]))
	}
	if len(cols) == 0 {
		return nil
	}
	return []*sdk.Node{sdk.Row("board", cols...).ScrollX().Gap(10).Align(protocol.AlignStart)}
}

// cardCost is the nodes a card takes at most, to share the budget by.
const cardCost = 22

// withEmpty adds the four columns of the board that the issues left empty,
// in their place, with the canceled column only when it has issues.
func (b *builder) withEmpty(secs []section) []section {
	var out []section
	for _, key := range sectionOrder {
		i := slices.IndexFunc(secs, func(s section) bool { return s.Key == key })
		switch {
		case i >= 0:
			out = append(out, secs[i])
		case key != secCanceled:
			out = append(out, section{Key: key})
		}
	}
	return out
}

// shares divides total cards among the columns: each gets what it needs
// while there is an even share, and the rest go to the larger ones.
func shares(secs []section, total int) []int {
	caps := make([]int, len(secs))
	need := make([]int, len(secs))
	for i, s := range secs {
		need[i] = len(s.Issues)
	}
	open := len(secs)
	done := make([]bool, len(secs))
	for open > 0 && total > 0 {
		even := total / open
		if even == 0 {
			even = 1
		}
		progress := false
		for i := range secs {
			if done[i] {
				continue
			}
			if need[i]-caps[i] <= even {
				give := min(need[i]-caps[i], total)
				caps[i] += give
				total -= give
				done[i] = true
				open--
				progress = true
			}
		}
		if !progress {
			for i := range secs {
				if done[i] {
					continue
				}
				give := min(even, total)
				caps[i] += give
				total -= give
			}
		}
	}
	return caps
}

// column is one board column: its header, then its cards up to cap.
func (b *builder) column(sec section, limit int) *sdk.Node {
	id := "col:" + sec.Key
	title := sectionTitle(sec.Key)
	kids := []*sdk.Node{
		sdk.Row(id+"-h",
			sdk.Text(id+"-t", title).Icon(stateIcon(sectionStateType(sec.Key))).
				Color(stateColor(sectionStateType(sec.Key))).Role(protocol.RoleStrong),
			caption(id+"-n", itoa(max(sec.Total, len(sec.Issues)))),
		).Gap(6).Padding(4),
	}
	b.left -= count(kids[0].Build())
	if len(sec.Issues) == 0 {
		kids = append(kids, caption(id+"-e", locales.LinearNone()))
	}
	for i, is := range sec.Issues {
		if i >= limit {
			b.skipped += len(sec.Issues) - i
			break
		}
		card := b.card(is)
		if !b.take(card) {
			b.skipped += len(sec.Issues) - i
			break
		}
		kids = append(kids, card)
		b.order = append(b.order, is.ID)
	}
	if sec.More > 0 {
		kids = append(kids, sdk.Row("more:"+sec.Key,
			sdk.Text("more:"+sec.Key+"-t", locales.LinearShowMore(itoa(sec.More))).Color(protocol.ColorAccent),
		).Pressable().Label(locales.LinearShowMore(itoa(sec.More))).Padding(4))
	}
	return sdk.Column(id, kids...).Width(boardColumnW).Gap(6)
}

// card is one issue on the board: its state and identifier with its
// priority and assignee, its title, and what it has as chips: what blocks
// it, what it blocks, its sub-issues, its milestone and its labels. A
// compact card has the identifier, the state and the title.
func (b *builder) card(is proto.LinearIssue) *sdk.Node {
	id := "c:" + is.ID
	ident := caption(id+"-n", is.Identifier).Icon(stateIcon(is.StateType)).Color(stateColor(is.StateType)).
		Label(is.Identifier + ", " + b.statusWord(is.State))
	if st, ok := b.in.snap.stateByName(is.State); ok && readableHex(st.Color) {
		ident.Hex(st.Color)
	}
	top := []*sdk.Node{ident, sdk.Spacer(id + "-g")}
	if is.Session != "" {
		top = append(top, sdk.Text(id+"-b", "").Icon("terminal").Color(sessionColor(is.SessionStatus)).Label(is.SessionName))
	}
	if icon, color := priorityMark(is.Priority); icon != "" {
		top = append(top, sdk.Text(id+"-p", "").Icon(icon).Color(color).Label(b.priorityWord(is.Priority)))
	}
	if is.Assignee != "" {
		top = append(top, caption(id+"-a", initials(is.Assignee)))
	}
	kids := []*sdk.Node{
		sdk.Row(id+"-r", top...).Gap(6).Align(protocol.AlignCenter),
		sdk.Text(id+"-t", is.Title).Lines(3),
	}
	if !b.in.settings.Compact {
		if is.Project != "" {
			kids = append(kids, caption(id+"-j", is.Project).Icon("folder"))
		}
		counts, labels := b.chips(id, is)
		if len(counts) > 0 {
			kids = append(kids, sdk.Row(id+"-c", counts...).Gap(4).Align(protocol.AlignStart))
		}
		if len(labels) > 0 {
			kids = append(kids, sdk.Row(id+"-cl", labels...).Gap(4).Align(protocol.AlignStart))
		}
	}
	return sdk.Column(id, kids...).Surface(protocol.SurfaceCard).Pressable().Active(b.selected() == is.ID).
		ContextMenu(b.issueMenu(is)...).Label(is.Identifier + " " + is.Title).Padding(8).Gap(4)
}

// maxCardLabels is how many labels a card shows as chips; the rest are
// counted in one more.
const maxCardLabels = 2

// chips are what an issue has, as chips: what blocks it, what it blocks, its
// sub-issues, its milestone, and its labels in their own colors.
func (b *builder) chips(id string, is proto.LinearIssue) (counts, labels []*sdk.Node) {
	if is.Blocked > 0 {
		counts = append(counts, sdk.Text(id+"-cb", itoa(is.Blocked)).Icon("blocked").Chip("#E5484D").Label(locales.LinearBlockedBy(itoa(is.Blocked))))
	}
	if is.Blocking > 0 {
		counts = append(counts, sdk.Text(id+"-ck", itoa(is.Blocking)).Icon("blocking").Chip("#F5A524").Label(locales.LinearBlocking(itoa(is.Blocking))))
	}
	if is.Children > 0 {
		counts = append(counts, sdk.Text(id+"-cs", itoa(is.Children)).Icon("subissue").Chip(protocol.ChipNeutral).Label(locales.LinearSubIssueCount(itoa(is.Children))))
	}
	if is.Milestone != "" {
		counts = append(counts, sdk.Text(id+"-cm", is.Milestone).Icon("milestone").Chip(protocol.ChipNeutral).Lines(1).Width(120))
	}
	for i, l := range is.Labels {
		if i >= maxCardLabels {
			labels = append(labels, sdk.Text(id+"-cx", "+"+itoa(len(is.Labels)-maxCardLabels)).Chip(protocol.ChipNeutral))
			break
		}
		labels = append(labels, sdk.Text(id+"-l"+itoa(i), l).Chip(b.labelColor(l, nil)).Lines(1).Width(80))
	}
	return counts, labels
}
