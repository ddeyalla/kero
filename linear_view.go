package main

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// The Linear view is the workspace as the app shows it: the issues of the
// team, as a board or a list, in a card of their own, with the issue open
// in a second card beside it, as a session's panes stand side by side.
//
// The view reads what the daemon read, which it keeps fresh; nothing
// here talks to Linear itself, so a slow or unreachable workspace never
// holds the window.

// linearState is what the view keeps besides the workspace: the filters,
// the issue open beside the board, and what was read of it.
type linearState struct {
	view         string // "board" or "list"
	query        string
	project      string
	milestone    string
	state        string
	open         string // the issue open in the detail
	detail       *proto.LinearIssueDetail
	loading      bool
	linkSession  string
	err          string
	request      uint64
	filterNotice string

	// detailW is the detail card's width, which the divider before it
	// drags; 0 for the default.
	detailW  float32
	dragFrom float32 // detailW as a drag of the divider began
	// dragReset holds the divider still from a double click until the
	// press ends.
	dragReset bool
	// arrive is set as the detail opens, for one frame: it springs in.
	arrive bool
	// searching is whether the search field had the focus last frame:
	// the view's keys are then the field's.
	searching bool
	// syncing is when a refresh was asked for, until a read lands.
	syncing  time.Time
	syncedAt int64
	railShut bool // the hidden columns' rail, folded
	// statusSel is the state the detail's pop-up shows for the issue
	// statusOf; statusSent the move asked for, until the board has it.
	statusSel, statusOf, statusSent string
	statusAt                        time.Time // when statusSent was sent
	openURL                         string    // a link a menu chose, which the next frame opens
	model                           linearModel

	// A Tasks pane's view: pane says it builds in a pane, not the
	// window; hideDone leaves out what is done or dropped; allTasks shows
	// the whole team for a project linked to no Linear project; pick is
	// the project the link prompt's pop-up chose.
	pane, hideDone, allTasks bool
	pick                     string
}

// linearViews are the two ways the issues are shown.
var linearViews = []string{"board", "list"}

const (
	linearColW      = 280 // a board column, and its cards
	linearRailW     = 216 // the rail of the hidden columns
	linearDetailW   = 480 // the detail card, until dragged
	linearMinDetail = 360
	linearMinBoard  = 400
	// linearWide is the detail's width from which its properties stand
	// in a column of their own at its right, as Linear's.
	linearWide = 760
)

// openLinear shows the workspace, in the card the app shows.
func (a *App) openLinear() {
	a.show = "linear"
	a.selected = ""
	a.savePrefs()
	a.call("linear.refresh", nil, nil)
}

// linearNav is the sidebar's row that opens the Linear workspace, with
// the count of the issues a session is working on, as a source list's
// badge.
func (a *App) linearNav(c *ui.Context, pal *palette) {
	if !a.set.LinearSidebar {
		return
	}
	lin := a.st.Linear
	working := 0
	if lin != nil {
		for _, i := range lin.Issues {
			if i.Session != "" {
				working++
			}
		}
	}
	sel := a.show == "linear"
	item := ui.ButtonBase(c.Key("linear-nav")).Height(30).Padding(0, 10).Gap(9).AlignItems(ui.Center).Radius(8)
	bg := ui.Transparent
	switch {
	case sel:
		bg = pal.rowSelected
		chosen(item, pal)
	case item.Hovered():
		bg = pal.rowHover
	}
	item.Background(item.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))
	if item.Clicked() {
		a.openLinear()
	}
	item.Children(func() {
		fg := pal.textMuted
		if sel {
			fg = pal.text
		}
		ui.Icon(c, trayIcon).Size(15, 15).TextColor(fg).Shrink(0)
		ui.Text(c, locales.LinearGroup()).FontSize(12.5).FontWeight(600).TextColor(pal.text).Grow(1).MinWidth(0)
		if working > 0 {
			ui.Textf(c, "%d", working).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
		}
	})
}

// linearMarkdown is Linear's markdown: a line break in it is a break, as
// it is in Linear's editor, and not the soft wrap markdown reads it as.
// The ends of a paragraph's lines get the backslash that makes a hard
// break; those inside a code block are left alone.
func linearMarkdown(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	fence := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			fence = !fence
		}
		if !fence && i < len(lines)-1 {
			next := strings.TrimSpace(lines[i+1])
			switch {
			case l == "" || next == "":
			case linearIsBlock(next):
			case strings.HasSuffix(l, "\\"):
				// The line already breaks.
			default:
				l = strings.TrimRight(l, " \t") + "\\"
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// linearIsBlock says whether a line starts a block of its own — a list
// item, a heading, a quote — which a line before it does not break on.
// A bold word is not a list: "**Design:**" is a list only when a space
// follows the star.
func linearIsBlock(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '-', '*', '+':
		return len(line) > 1 && line[1] == ' '
	case '>':
		return true
	case '#':
		return len(line) > 1 && line[1] == ' '
	}
	return false
}

// --- the model: the issues the filters keep, read once per state ---

// linearModel is what the board and the list show, worked out once for
// each state the daemon sends and each change of the filters, not once a
// frame: the issues kept, by state, in the order they are shown.
type linearModel struct {
	key     linearModelKey
	issues  []proto.LinearIssue
	states  []proto.LinearWorkflowState // the team's, in its order
	byState map[string][]proto.LinearIssue
	marks   map[string]stateMark
	dark    bool
}

// linearModelKey is what the model depends on. The daemon's issues are
// a new slice with each state it sends, so their first element's address
// tells a new state from the last one.
type linearModelKey struct {
	issues                          *proto.LinearIssue
	n, nStates                      int
	project, milestone, state, find string
	hideDone                        bool
}

// linearModel returns the model, working it out again when what it
// depends on changed.
func (a *App) linearModel(pal *palette) *linearModel {
	lin := a.st.Linear
	m := &a.lin.model
	key := linearModelKey{project: a.lin.project, milestone: a.lin.milestone, state: a.lin.state,
		find: strings.ToLower(strings.TrimSpace(a.lin.query)), hideDone: a.lin.hideDone}
	if lin != nil {
		key.n, key.nStates = len(lin.Issues), len(lin.States)
		if len(lin.Issues) > 0 {
			key.issues = &lin.Issues[0]
		}
	}
	if m.byState != nil && m.key == key && m.dark == pal.dark {
		return m
	}
	*m = linearModel{key: key, dark: pal.dark, byState: map[string][]proto.LinearIssue{}}
	if lin == nil {
		return m
	}
	m.states = slices.Clone(lin.States)
	sort.SliceStable(m.states, func(i, j int) bool { return m.states[i].Position < m.states[j].Position })
	m.marks = linearMarks(m.states, pal)
	m.issues = make([]proto.LinearIssue, 0, len(lin.Issues))
	for _, i := range lin.Issues {
		if key.project != "" && i.Project != key.project ||
			key.milestone != "" && i.Milestone != key.milestone ||
			key.state != "" && i.State != key.state ||
			key.find != "" && !strings.Contains(strings.ToLower(i.Identifier+" "+i.Title), key.find) ||
			key.hideDone && linearDone(i.StateType) {
			continue
		}
		m.issues = append(m.issues, i)
	}
	sort.SliceStable(m.issues, func(x, y int) bool {
		if m.issues[x].UpdatedAt != m.issues[y].UpdatedAt {
			return m.issues[x].UpdatedAt > m.issues[y].UpdatedAt
		}
		return m.issues[x].Identifier < m.issues[y].Identifier
	})
	for _, i := range m.issues {
		m.byState[i.State] = append(m.byState[i.State], i)
	}
	return m
}

// linearDone tells the states of work over: done, dropped, duplicate.
func linearDone(typ string) bool {
	return typ == "completed" || typ == "canceled" || typ == "duplicate"
}

// linearIssues are the issues the filters keep.
func (a *App) linearIssues() []proto.LinearIssue {
	return a.linearModel(paletteForDark(false)).issues
}

// paletteForDark is a palette for work outside a frame, as the tests'.
func paletteForDark(dark bool) *palette {
	if dark {
		return &darkPalette
	}
	return &lightPalette
}

// linearIssue is the issue of the workspace with the id or the
// identifier, as the board has it.
func (a *App) linearIssue(id string) *proto.LinearIssue {
	if a.st.Linear == nil || id == "" {
		return nil
	}
	for i := range a.st.Linear.Issues {
		is := &a.st.Linear.Issues[i]
		if is.ID == id || strings.EqualFold(is.Identifier, id) {
			return is
		}
	}
	return nil
}

// linearColumn is the issues shown with the one open, in the order shown:
// its column on the board, the whole list in the list. The detail steps
// through them, as Linear's "2 / 19".
func (a *App) linearColumn(m *linearModel) ([]proto.LinearIssue, int) {
	open := a.linearIssue(a.lin.open)
	if open == nil {
		return nil, -1
	}
	col := m.byState[open.State]
	if a.lin.view == "list" {
		// The list is one column: its groups, one after another.
		col = nil
		for _, st := range m.states {
			col = append(col, m.byState[st.Name]...)
		}
	}
	for n, i := range col {
		if i.ID == open.ID {
			return col, n
		}
	}
	return col, -1
}

// linearStep opens the issue d places from the open one in its column,
// or in the list.
func (a *App) linearStep(m *linearModel, d int) {
	col, at := a.linearColumn(m)
	if at < 0 {
		return
	}
	if n := at + d; n >= 0 && n < len(col) {
		a.linearOpen(col[n].ID)
	}
}

// --- the view ---

// linearView is the workspace: the board's card and, while an issue is
// open, the issue's card at its right, with a divider between them that
// a drag moves, as between a session's panes. On a window too narrow for
// both, the issue takes the board's place, with the way back in its
// header.
func (a *App) linearView(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	m := a.linearModel(pal)
	a.linearKeys(c, m)
	// The search sets it again as it builds; on a window where only the
	// issue shows, there is no search to have the focus.
	a.lin.searching = false
	a.linearFreshen()
	if a.lin.openURL != "" {
		c.OpenURL(a.lin.openURL)
		a.lin.openURL = ""
	}

	width, _ := c.Size()
	avail := width - a.chrome.sideW - a.chrome.mainPad - cardGap
	open := a.lin.open != ""
	detailW := a.lin.detailW
	if detailW == 0 {
		detailW = linearDetailW
	}
	maxW := avail - linearMinBoard - paneGap
	alone := open && maxW < linearMinDetail
	detailW = max(linearMinDetail, min(detailW, maxW))

	ui.Row(c).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		if !alone {
			board := linearPaneCard(c, pal, "linear-board").Grow(1).Basis(0).MinWidth(0)
			board.Children(func() {
				a.linearHeader(c, pal, bar, shown, !open)
				a.linearToolbar(c, pal, m)
				ui.Column(c).Grow(1).MinHeight(0).Children(func() {
					if a.lin.view == "list" {
						a.linearList(c, pal, m)
					} else {
						a.linearBoard(c, pal, m)
					}
				})
			})
		}
		if !open {
			return
		}
		if !alone {
			a.linearDivider(c, pal, detailW, maxW)
		}
		detail := linearPaneCard(c, pal, "linear-detail").Shrink(0)
		if alone {
			detail.Grow(1).MinWidth(0)
		} else {
			detail.Width(detailW)
		}
		detail.Children(func() {
			a.linearDetailPane(c, pal, m, bar, shown, alone, detailW)
		})
		// The detail springs in as it opens; stepping from one issue to
		// the next, with the keys, does not move it.
		pose := ui.Pose{}
		if a.lin.arrive {
			pose = ui.Pose{X: 12, Scale: 0.99, Fade: 0.4}
			a.lin.arrive = false
			c.Invalidate()
		}
		detail.AnimatePose(pose, ui.Animation{Spring: ui.SpringOptions{Duration: 320 * time.Millisecond}})
	})
}

// linearPaneCard is one of the view's cards: raised, as a pane is.
func linearPaneCard(c *ui.Context, pal *palette, key string) ui.Element {
	card := ui.Column(c.Key(key)).MinHeight(0).Radius(14).Background(pal.card).Clip()
	return raised(card, pal)
}

// linearDivider is the gap between the board and the issue, which a drag
// moves and a double click puts back.
func (a *App) linearDivider(c *ui.Context, pal *palette, w, maxW float32) {
	d := ui.Box(c.Key("linear-div")).Width(paneGap).Shrink(0).Cursor(ui.CursorResizeColumn).Label("Resize the issue")
	if d.Hovered() || d.Pressed() {
		d.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, pal.textFaint.Alpha(0.7), 1.5)
		})
	}
	if d.DoubleClicked() {
		// Back to the default; the press that double-clicked moves
		// nothing until it is let go.
		a.lin.detailW, a.lin.dragFrom, a.lin.dragReset = 0, 0, true
		a.savePrefs()
	}
	// A press that does not move keeps the width as it was: on a narrow
	// window, a click would otherwise keep the narrowed width for good.
	if dx, _, ok := d.Dragged(); ok && dx != 0 && !a.lin.dragReset {
		if a.lin.dragFrom == 0 {
			a.lin.dragFrom = w
		}
		// The divider moves right, the issue narrows.
		a.lin.dragFrom -= dx
		a.lin.detailW = max(linearMinDetail, min(a.lin.dragFrom, maxW))
	}
	if !d.Pressed() {
		a.lin.dragReset = false
		if a.lin.dragFrom != 0 {
			a.lin.dragFrom = 0
			a.savePrefs()
		}
	}
}

// linearHeader is the board card's header: the workspace's name, the
// search, the two ways of showing the issues, and the refresh. right is
// whether the card ends at the window's right, where the window's
// controls are off macOS.
func (a *App) linearHeader(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32, right bool) {
	ui.Row(c).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 10, 0, a.chrome.headerPad).Gap(8).DragWindow().Children(func() {
		a.linearSidebarButtons(c, pal, bar, shown)
		ui.Icon(c, trayIcon).Size(15, 15).TextColor(pal.textMuted).Shrink(0)
		ui.Text(c, locales.LinearGroup()).FontSize(14).FontWeight(600).TextColor(pal.text).SingleLine()
		if lin := a.st.Linear; lin != nil && lin.TeamName != "" {
			ui.Text(c, lin.TeamName).FontSize(13).TextColor(pal.textFaint).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		ui.Box(c).Grow(1).AlignSelf(ui.Stretch).DragWindow()
		a.linearSearch(c, pal)
		iconSegmented(c, "linear-view", &a.lin.view, linearViews, []*ui.SVG{boardIcon, listIcon},
			[]string{locales.LinearBoard(), locales.LinearList()}, pal)
		iconButton(c, "linear-refresh", refreshIcon, a.tip(locales.LinearRefresh(), "linear.refresh"), pal, a.linearRefresh)
		if right {
			windowControls(c, pal, a.win)
		}
	})
}

// linearSidebarButtons are the sidebar's buttons, which come to the
// first card's header as the sidebar hides, after the traffic lights.
func (a *App) linearSidebarButtons(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	if shown >= 0.5 {
		return
	}
	ui.Row(c).Absolute().Left(a.chrome.buttonsLeft(bar)).Top(0).Bottom(0).AlignItems(ui.Center).Gap(2).Opacity(1 - 2*shown).Children(func() {
		iconButton(c, "show-sidebar", sidebarIcon, a.tip(locales.SidebarShow(), "sidebar.toggle"), pal, a.toggleSidebar)
	})
}

// linearRefresh asks the daemon to read the workspace now.
func (a *App) linearRefresh() {
	a.lin.syncing = time.Now()
	if lin := a.st.Linear; lin != nil {
		a.lin.syncedAt = lin.FetchedAt
	}
	a.call("linear.refresh", nil, nil)
}

// linearToolbar is the filter bar under the header: the project, its
// milestone and the state, as pop-up chips, the milestone's progress, and
// at the right how many issues show and when they were read.
func (a *App) linearToolbar(c *ui.Context, pal *palette, m *linearModel) {
	before := a.linearPrefs()
	a.linearValidateFilters()
	lin := a.st.Linear
	ui.Row(c).Height(36).Shrink(0).Gap(8).Padding(0, 14, 4, 0).AlignItems(ui.Center).Children(func() {
		ui.ScrollHorizontal(c.Key("linear-toolbar")).Grow(1).MinWidth(0).Gap(4).Padding(0, 0, 0, 12).AlignItems(ui.Center).Children(func() {
			if lin != nil {
				a.linearFilters(c, pal, lin, m)
			}
			if a.lin.project != "" || a.lin.milestone != "" || a.lin.state != "" || a.lin.query != "" {
				clear := ui.Text(c.Key("linear-clear-filters"), locales.LinearClearFilters()).FontSize(12).TextColor(pal.accent).
					Padding(0, 6).Cursor(ui.CursorPointer)
				if clear.Clicked() {
					a.lin.project, a.lin.milestone, a.lin.state, a.lin.query = "", "", "", ""
					a.lin.filterNotice = ""
				}
			}
			if a.lin.filterNotice != "" {
				ui.Text(c, a.lin.filterNotice).FontSize(11.5).TextColor(pal.textMuted).SingleLine()
			}
		})
		ui.Text(c, a.linearStatusLabel(m)).FontSize(11.5).TextColor(pal.textFaint).SingleLine().FontFeatures("tnum").Shrink(0)
	})
	// The width is the divider's to save, as its drag ends.
	after, saved := a.linearPrefs(), a.prefs.Linear
	after.DetailWidth, saved.DetailWidth = 0, 0
	if before.DetailWidth = 0; before != after || saved != after {
		a.savePrefs()
	}
}

// linearPrefs is what of the view the app keeps between runs.
// They are the root view's, whichever view builds.
func (a *App) linearPrefs() linearPrefs {
	r := &a.linRoot
	return linearPrefs{View: r.view, Project: r.project, Milestone: r.milestone, State: r.state, DetailWidth: r.detailW}
}

// withLin builds or answers for the view st, as a Tasks pane's: the
// view's functions work on a.lin. A callback of a request a view made
// answers for that view, not for the one building as it lands.
func (a *App) withLin(st *linearState, fn func()) {
	prev := a.lin
	a.lin = st
	defer func() { a.lin = prev }()
	fn()
}

// linearStatusLabel says how many issues show and when they were read,
// or why the workspace was not.
func (a *App) linearStatusLabel(m *linearModel) string {
	st := a.st.Linear
	if st == nil {
		return locales.LinearStatusNoToken()
	}
	switch st.Status {
	case proto.LinearNoToken:
		return locales.LinearStatusNoToken()
	case proto.LinearAuthFailed:
		return locales.LinearStatusAuthFailed()
	case proto.LinearRateLimited:
		return locales.LinearStatusRateLimited()
	case proto.LinearUnreachable:
		return locales.LinearStatusUnreachable()
	}
	count := fmt.Sprintf("%d %s", len(st.Issues), locales.LinearIssues())
	if m != nil && len(m.issues) != len(st.Issues) {
		count = fmt.Sprintf("%d / %d %s", len(m.issues), len(st.Issues), locales.LinearIssues())
	}
	if a.lin.syncing.IsZero() {
		if when := ago(st.FetchedAt, time.Now()); when != "" {
			return count + " · " + locales.LinearSynced(when)
		}
		return count
	}
	return count + " · " + locales.LinearSyncing()
}

// linearFreshen keeps what the view shows of the daemon's reads current:
// the refresh asked for ends as a read lands, or after a while without
// one; and the issue open is read again when the board says it changed,
// rather than on a timer, which spent Linear's quota on issues that sat
// still.
func (a *App) linearFreshen() {
	lin := a.st.Linear
	if !a.lin.syncing.IsZero() && (lin != nil && lin.FetchedAt != a.lin.syncedAt || time.Since(a.lin.syncing) > 15*time.Second) {
		a.lin.syncing = time.Time{}
	}
	d := a.lin.detail
	if d == nil || a.lin.loading || a.lin.err != "" {
		return
	}
	if is := a.linearIssue(a.lin.open); is != nil && is.UpdatedAt > d.UpdatedAt {
		a.linearOpen(a.lin.open)
	}
}

func (a *App) linearValidateFilters() {
	st := a.st.Linear
	if st == nil || st.Status != proto.LinearOK {
		return
	}
	validProject := a.lin.project == ""
	validMilestone := a.lin.milestone == ""
	for _, p := range st.Projects {
		if p.Name == a.lin.project {
			validProject = true
		}
		if a.lin.project != "" && p.Name != a.lin.project {
			continue
		}
		for _, m := range p.Milestones {
			if m.Name == a.lin.milestone {
				validMilestone = true
			}
		}
	}
	if !validProject || !validMilestone {
		if !validProject {
			a.lin.project = ""
		}
		a.lin.milestone = ""
		a.lin.filterNotice = locales.LinearFiltersReset()
	}
}

// linearSearch looks for an issue by its identifier or its title.
func (a *App) linearSearch(c *ui.Context, pal *palette) {
	var in ui.Element
	frame := ui.Row(c.Key("linear-search")).Cursor(ui.CursorText).Width(180).Shrink(1).MinWidth(110)
	frame.Children(func() {
		ui.Icon(c, searchIcon).Size(12, 12).TextColor(pal.textFaint).Shrink(0)
		in = ui.TextInputBase(c.Key("linear-in"), &a.lin.query).Grow(1).MinWidth(0).FontSize(12.5).
			Placeholder(locales.LinearSearch()).Label(locales.LinearSearch())
		in.OnShortcut(0, ui.KeyEscape, func() { a.lin.query = "" })
		if a.lin.query != "" {
			x := ui.ButtonBase(c.Key("linear-clear")).Size(15, 15).Radius(8).Center().Shrink(0).
				Background(pal.textFaint.Alpha(0.55)).TextColor(pal.card).Label(locales.LinearClearFilters())
			x.Children(func() { ui.Icon(c, closeIcon).Size(7, 7) })
			if x.Clicked() {
				a.lin.query = ""
			}
		}
	})
	field(c, frame, in.Focused(), pal).Height(26)
	a.lin.searching = in.Focused()
}

// linearFilters are the chips that narrow the issues: the project, the
// milestone, and the state.
func (a *App) linearFilters(c *ui.Context, pal *palette, lin *proto.LinearState, m *linearModel) {
	projects := []string{""}
	for _, p := range lin.Projects {
		projects = append(projects, p.Name)
	}
	if len(projects) > 1 {
		filterChip(c, "linear-project", &a.lin.project, projects, projectIcon, func(s string) string {
			if s == "" {
				return locales.LinearAllProjects()
			}
			return s
		}, nil, pal)
	}

	milestones := []string{""}
	var progress float64 = -1
	for _, p := range lin.Projects {
		if a.lin.project != "" && p.Name != a.lin.project {
			continue
		}
		for _, ms := range p.Milestones {
			milestones = append(milestones, ms.Name)
			if ms.Name == a.lin.milestone {
				progress = ms.Progress
			}
		}
	}
	if len(milestones) > 1 {
		filterChip(c, "linear-milestone", &a.lin.milestone, milestones, milestoneIcon, func(s string) string {
			if s == "" {
				return locales.LinearAllMilestones()
			}
			return s
		}, nil, pal)
	}
	if progress >= 0 {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(0, 4).Children(func() {
			progressRing(c, pal, float32(progress), 12)
			ui.Textf(c, "%d%%", int(progress*100+0.5)).FontSize(11.5).TextColor(pal.textMuted).FontFeatures("tnum")
		})
	}

	states := []string{""}
	for _, s := range m.states {
		states = append(states, s.Name)
	}
	filterChip(c, "linear-state", &a.lin.state, states, filterIcon, func(s string) string {
		if s == "" {
			return locales.LinearAllStates()
		}
		return s
	}, func(s string) {
		if s != "" {
			stateGlyph(c, pal, markOf(m.marks, s, "", pal), 12)
		}
	}, pal)
}

// progressRing is a milestone's progress: a ring the done part of which
// is drawn.
func progressRing(c *ui.Context, pal *palette, done float32, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := min(r.W, r.H)/2 - 1
		var track ui.Path
		track.Circle(cx, cy, rad)
		p.StrokePath(&track, 2, pal.text.Alpha(0.12))
		p.Transform(ui.Rotation(-90), cx, cy, func() {
			var arc ui.Path
			arc.Circle(cx, cy, rad)
			p.StrokePath(arc.Trim(0, max(0.02, min(done, 1))), 2, pal.accent)
		})
	})
	return box
}

// filterChip chooses *value among options, as a chip with a mark: quiet
// while it filters nothing, filled while it does. lead, when set, draws
// a mark of the value chosen in place of the chip's own.
func filterChip(c *ui.Context, key string, value *string, options []string, icon *ui.SVG, label func(string) string, lead func(string), pal *palette) ui.Element {
	sel := ui.SelectBase(c.Key(key), value)
	trig := sel.Trigger.MaxWidth(220).Height(26).Padding(0, 8).Gap(6).Radius(7).AlignItems(ui.Center).Shrink(0).Label(label(*value))
	open := sel.Open()
	bg, fg := ui.Transparent, pal.textMuted
	switch {
	case *value != "":
		bg, fg = pal.chip, pal.text
	}
	if open || trig.Pressed() {
		bg = pal.chip.Mix(pal.text, 0.06)
	} else if trig.Hovered() {
		bg = pal.rowHover
		if *value != "" {
			bg = pal.chip.Mix(pal.text, 0.04)
		}
	}
	trig.Background(trig.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond})).TextColor(fg)
	trig.Children(func() {
		if lead != nil && *value != "" {
			lead(*value)
		} else {
			ui.Icon(c, icon).Size(12, 12).Shrink(0)
		}
		ui.Text(c, label(*value)).FontSize(12.5).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		ui.Icon(c, chevronDownIcon).Size(9, 9).TextColor(pal.textFaint).Shrink(0)
	})
	var spot ui.Element
	maxH := float32(0)
	if open {
		spot, maxH = menuSpot(c, key, trig.Bounds(), max(slices.Index(options, *value), 0), len(options))
	}
	sel.Popup(func(panel ui.Element) {
		menuPanel(panel, pal).AttachTo(spot, ui.AnchorTopLeft, ui.AnchorTopLeft)
		menuWidths[key] = panel.Bounds().W
		over := panel.Hovered()
		ui.Scroll(c.Key("items")).MaxHeight(maxH - 2*menuPad).Children(func() {
			for _, o := range options {
				item := sel.Item(o)
				hi := item.Highlighted() && (item.Hovered() || !over && o != *value)
				menuRow(c, item, label(o), o == *value, hi, pal)
			}
		})
	})
	return trig
}

// iconSegmented chooses *value among ids as a segmented control of icons,
// as a toolbar's view switch, each named by its label for its tooltip and
// for VoiceOver.
func iconSegmented(c *ui.Context, key string, value *string, ids []string, icons []*ui.SVG, labels []string, pal *palette) {
	i := max(slices.Index(ids, *value), 0)
	track, thumb := ui.RGBA(0, 0, 0, 0.06), ui.Hex("#ffffff")
	if pal.dark {
		track, thumb = ui.RGBA(255, 255, 255, 0.07), ui.RGBA(255, 255, 255, 0.2)
	}
	seg := ui.SegmentedBase(c.Key(key), &i, len(ids))
	seg.Track.Height(26).Padding(2).Radius(7).Background(track).AlignItems(ui.Center).Shrink(0).Children(func() {
		for n := range ids {
			s := seg.Segment(n).FillHeight().Width(30).Radius(5).Center().Label(labels[n]).Tooltip(labels[n])
			fg := pal.textMuted
			if n == i {
				fg = pal.text
				s.Background(thumb)
				if pal.dark {
					s.Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, 0.25))
				} else {
					s.Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.16)).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.04))
				}
			}
			s.Children(func() { ui.Icon(c, icons[n]).Size(14, 14).TextColor(fg) })
		}
	})
	seg.Track.OnChange(func() { *value = ids[i] })
}

// linearKeys are the view's keys, as Linear's: Escape closes the issue,
// J and K and the arrows step through its column. They are the search
// field's while it has the focus, and nothing's while a dialog is open.
func (a *App) linearKeys(c *ui.Context, m *linearModel) {
	if a.lin.searching || a.lin.open == "" || a.pal.open || a.chooser.open || a.confirm != nil || a.usage.open || a.renaming != "" {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.linearClose()
	}
	if c.Shortcut(0, ui.KeyJ) || c.Shortcut(0, ui.KeyDown) {
		a.linearStep(m, 1)
	}
	if c.Shortcut(0, ui.KeyK) || c.Shortcut(0, ui.KeyUp) {
		a.linearStep(m, -1)
	}
}

// linearNothing is what the view says when there is nothing to show.
func (a *App) linearNothing(c *ui.Context, pal *palette, title, detail string) {
	a.linearNothingThen(c, pal, title, detail, nil)
}

// linearNothingThen is linearNothing with what can be done about it,
// under what it says.
func (a *App) linearNothingThen(c *ui.Context, pal *palette, title, detail string, action func()) {
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Center().Background(pal.chip).TextColor(pal.textMuted).Margin(0, 0, 6, 0).Children(func() {
			ui.Icon(c, trayIcon).Size(22, 22)
		})
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(pal.text)
		ui.Text(c, detail).FontSize(12.5).TextColor(pal.textMuted).MaxWidth(320).TextAlign(ui.Center).LineHeight(1.4)
		if action != nil {
			ui.Box(c).Height(6)
			action()
		}
	})
}

// linearEmptyDetail says why no issue shows.
func (a *App) linearEmptyDetail() string {
	if a.st.Linear == nil || a.st.Linear.Status != proto.LinearOK {
		return a.linearStatusLabel(nil)
	}
	return locales.LinearNoneFiltered()
}

// linearOpen opens an issue in the detail, and reads it.
func (a *App) linearOpen(id string) {
	if a.lin.open == "" {
		a.lin.arrive = true
	}
	if a.lin.open != id {
		a.lin.detail = nil
		a.lin.linkSession = ""
	}
	a.lin.open = id
	a.lin.request++
	request := a.lin.request
	a.lin.err = ""
	a.lin.loading = true
	cl := a.cl
	if cl == nil {
		a.lin.loading = false
		if !a.linearShowing(id) {
			a.lin.err = locales.LinearNeedsDaemon()
		}
		return
	}
	st := a.lin
	go func() {
		var d proto.LinearIssueDetail
		err := cl.Call("linear.issue", proto.LinearParams{Issue: id}, &d)
		a.update(func() { a.withLin(st, func() { a.linearComplete(id, request, &d, err) }) })
	}()
}

// linearClose closes the issue open.
func (a *App) linearClose() {
	a.lin.open = ""
	a.lin.request++
	a.lin.detail = nil
	a.lin.err = ""
	a.lin.loading = false
}

func (a *App) linearComplete(id string, request uint64, detail *proto.LinearIssueDetail, err error) {
	if a.lin.open != id || a.lin.request != request {
		return
	}
	a.lin.loading = false
	if err != nil {
		// A read again of the issue shown that fails keeps what shows,
		// and is not tried again until the board says the issue changed
		// once more: the next read of a workspace Linear refused would
		// be refused too.
		if !a.linearShowing(id) {
			a.lin.err = locales.LinearUnavailableDetail()
		} else if is := a.linearIssue(id); is != nil && is.UpdatedAt > a.lin.detail.UpdatedAt {
			a.lin.detail.UpdatedAt = is.UpdatedAt
		}
		return
	}
	// The board's copy may be newer than what was read: what was read is
	// then as new as the board, so that it is not read again at once.
	if is := a.linearIssue(id); is != nil && is.UpdatedAt > detail.UpdatedAt {
		detail.UpdatedAt = is.UpdatedAt
	}
	a.lin.detail = detail
	a.lin.err = ""
}

// linearShowing says whether the detail shows the issue already, as
// read before.
func (a *App) linearShowing(id string) bool {
	d := a.lin.detail
	return d != nil && (d.ID == id || strings.EqualFold(d.Identifier, id))
}

// linearMoveTo moves an issue to a state of the team's workflow.
func (a *App) linearMoveTo(id, identifier, state string) {
	a.call("linear.setState", proto.LinearParams{Issue: id, State: state}, func(err error) {
		if err != nil {
			a.toast(err.Error())
			return
		}
		a.toast(locales.LinearMoved(identifier, state))
	})
}

// Remote issue content can only open web links, never local files or
// command/custom URL handlers.
func linearSafeURL(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
