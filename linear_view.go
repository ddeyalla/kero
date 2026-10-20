package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"kero/internal/proto"

	"kero/locales"
)

// The Linear view is the workspace as the app shows it: the issues of the
// team, as a board or a list, with the issue open beside them and the
// session working on it linked.
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
}

// linearViews are the two ways the issues are shown.
var linearViews = []string{"board", "list"}

// linearCols are the widths of the board's columns, and of the detail
// beside it.
const (
	linearColW   = 232
	linearDetail = 380
)

// openLinear shows the workspace, in the card the app shows.
func (a *App) openLinear() {
	a.show = "linear"
	a.selected = ""
	a.savePrefs()
	a.call("linear.refresh", nil, nil)
}

// linearNav is the sidebar's row that opens the Linear workspace, with
// the count of the issues a session is working on.
func (a *App) linearNav(c *ui.Context, pal *palette) {
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
		linearBoardIcon(c, pal, 13)
		ui.Text(c, locales.LinearGroup()).FontSize(12.5).FontWeight(600).TextColor(pal.text).Grow(1).MinWidth(0)
		if working > 0 {
			ui.Textf(c, "%d", working).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
		}
	})
}

// linearBoardIcon is the mark of the board: three columns.
func linearBoardIcon(c *ui.Context, pal *palette, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		fg := pal.textMuted
		h := r.H / 2
		for n := range 3 {
			x := r.X + float32(n)*(r.W/3) + 1
			var col ui.Path
			col.MoveTo(x, r.Y).LineTo(x, r.Y+h)
			p.StrokePath(&col, 1.4, fg)
		}
		var top, bottom ui.Path
		top.MoveTo(r.X, r.Y).LineTo(r.X+r.W, r.Y)
		bottom.MoveTo(r.X, r.Y+h).LineTo(r.X+r.W, r.Y+h)
		p.StrokePath(&top, 1.4, fg)
		p.StrokePath(&bottom, 1.4, fg)
	})
	return box
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

// linearView is the workspace, in the main card.
func (a *App) linearView(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	width, _ := c.Size()
	compactDetail := width < 1000 && a.lin.open != ""
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		a.linearHeader(c, pal, bar, shown)
		a.linearToolbar(c, pal)
		ui.Row(c).Grow(1).MinHeight(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
			if !compactDetail {
				ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
					if a.lin.view == "list" {
						a.linearList(c, pal)
					} else {
						a.linearBoard(c, pal)
					}
				})
			}
			if a.lin.open != "" {
				a.linearDetailPane(c, pal)
			}
		})
	})
}

// linearHeader is the view's header: what is shown, what is looked for,
// and the filters.
func (a *App) linearHeader(c *ui.Context, pal *palette, bar ui.TitleBar, shown float32) {
	left := a.chrome.headerPad
	ui.Row(c).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 12, 0, left).Gap(10).DragWindow().Children(func() {
		if shown < 0.5 {
			ui.Row(c).Absolute().Left(a.chrome.buttonsLeft(bar)).Top(0).Bottom(0).AlignItems(ui.Center).Gap(2).Opacity(1 - 2*shown).Children(func() {
				iconButton(c, "show-sidebar", sidebarIcon, a.tip(locales.SidebarShow(), "sidebar.toggle"), pal, a.toggleSidebar)
			})
		}
		ui.Text(c, locales.LinearGroup()).FontSize(14).FontWeight(600).TextColor(pal.text)
		segmented(c, "linear-view", &a.lin.view, linearViews, []string{locales.LinearBoard(), locales.LinearList()})
		ui.Spacer(c)
		a.linearSearch(c, pal)
		iconButton(c, "linear-refresh", glyph("f7-return"), locales.LinearRefresh(), pal, func() {
			a.call("linear.refresh", nil, nil)
		})
		windowControls(c, pal, a.win)
	})
}

// A second row keeps filters usable on small windows and with detail open.
func (a *App) linearToolbar(c *ui.Context, pal *palette) {
	before := linearPrefs{View: a.lin.view, Project: a.lin.project, Milestone: a.lin.milestone, State: a.lin.state}
	a.linearValidateFilters()
	ui.ScrollHorizontal(c.Key("linear-toolbar")).Height(40).Shrink(0).Gap(10).Padding(0, 14).AlignItems(ui.Center).Children(func() {
		a.linearFilters(c, pal)
		ui.Text(c, a.linearStatusLabel()).FontSize(11).TextColor(pal.textFaint).SingleLine()
		if a.lin.filterNotice != "" {
			ui.Text(c, a.lin.filterNotice).FontSize(11).TextColor(pal.textMuted)
		}
	})
	after := linearPrefs{View: a.lin.view, Project: a.lin.project, Milestone: a.lin.milestone, State: a.lin.state}
	if before != after || a.prefs.Linear != after {
		a.savePrefs()
	}
}

func (a *App) linearStatusLabel() string {
	st := a.st.Linear
	if st == nil {
		return "Connect Linear in Settings → Agents"
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
	return fmt.Sprintf("%s · %d issues", st.TeamName, len(st.Issues))
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
		a.lin.filterNotice = "Previous project or milestone is unavailable; showing its parent."
	}
}

// linearSearch looks for an issue by its identifier or its title.
func (a *App) linearSearch(c *ui.Context, pal *palette) {
	var in ui.Element
	frame := ui.Row(c.Key("linear-search")).Cursor(ui.CursorText).Width(190).Shrink(0)
	frame.Children(func() {
		ui.Icon(c, searchIcon).Size(13, 13).TextColor(pal.textFaint).Shrink(0)
		in = ui.TextInputBase(c.Key("linear-in"), &a.lin.query).Grow(1).MinWidth(0).FontSize(12.5).
			Placeholder(locales.LinearSearch()).Label(locales.LinearSearch())
		in.OnShortcut(0, ui.KeyEscape, func() { a.lin.query = "" })
		if a.lin.query != "" {
			x := ui.ButtonBase(c.Key("linear-clear")).Size(16, 16).Radius(8).Center().Shrink(0).
				Background(pal.textFaint.Alpha(0.55)).TextColor(pal.card)
			x.Children(func() { ui.Icon(c, closeIcon).Size(8, 8) })
			if x.Clicked() {
				a.lin.query = ""
			}
		}
	})
	field(c, frame, in.Focused(), pal)
}

// linearFilters are the pop-ups that narrow the issues: the project, the
// milestone, and the state.
func (a *App) linearFilters(c *ui.Context, pal *palette) {
	lin := a.st.Linear
	if lin == nil {
		return
	}
	projects := []string{""}
	for _, p := range lin.Projects {
		projects = append(projects, p.Name)
	}
	popup(c, "linear-project", &a.lin.project, projects, func(s string) string {
		if s == "" {
			return locales.LinearAllProjects()
		}
		return s
	}, 150, pal)

	milestones := []string{""}
	for _, p := range lin.Projects {
		if a.lin.project != "" && p.Name != a.lin.project {
			continue
		}
		for _, m := range p.Milestones {
			milestones = append(milestones, m.Name)
		}
	}
	popup(c, "linear-milestone", &a.lin.milestone, milestones, func(s string) string {
		if s == "" {
			return locales.LinearAllMilestones()
		}
		return s
	}, 150, pal)

	states := []string{""}
	for _, s := range lin.States {
		states = append(states, s.Name)
	}
	popup(c, "linear-state", &a.lin.state, states, func(s string) string {
		if s == "" {
			return locales.LinearAllStates()
		}
		return s
	}, 120, pal)
}

// linearIssues are the issues the filters keep.
func (a *App) linearIssues() []proto.LinearIssue {
	lin := a.st.Linear
	if lin == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(a.lin.query))
	out := make([]proto.LinearIssue, 0, len(lin.Issues))
	for _, i := range lin.Issues {
		if a.lin.project != "" && i.Project != a.lin.project {
			continue
		}
		if a.lin.milestone != "" && i.Milestone != a.lin.milestone {
			continue
		}
		if a.lin.state != "" && i.State != a.lin.state {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(i.Identifier+" "+i.Title), q) {
			continue
		}
		out = append(out, i)
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].UpdatedAt != out[y].UpdatedAt {
			return out[x].UpdatedAt > out[y].UpdatedAt
		}
		return out[x].Identifier < out[y].Identifier
	})
	return out
}

// linearBoard is the issues as columns, one per state, in the order the
// team puts them in.
func (a *App) linearBoard(c *ui.Context, pal *palette) {
	lin := a.st.Linear
	if lin == nil || (lin.Status != proto.LinearOK && len(lin.Issues) == 0) {
		a.linearNothing(c, pal, locales.LinearNotRead(), a.linearStatusLabel())
		return
	}
	issues := a.linearIssues()
	// The columns are the team's states, in the order the team puts
	// them in; a state with no issues is not a column.
	states := append([]proto.LinearWorkflowState{}, lin.States...)
	sort.SliceStable(states, func(i, j int) bool { return states[i].Position < states[j].Position })
	byState := map[string][]proto.LinearIssue{}
	for _, i := range issues {
		byState[i.State] = append(byState[i.State], i)
	}
	shown := make([]proto.LinearWorkflowState, 0, len(states))
	for _, s := range states {
		if len(byState[s.Name]) > 0 {
			shown = append(shown, s)
		}
	}
	if len(shown) == 0 {
		a.linearNothing(c, pal, locales.LinearNone(), a.linearEmptyDetail())
		return
	}
	// The board is a row of columns that scrolls sideways: each column
	// is as tall as the board, and its cards scroll within it.
	ui.ScrollHorizontal(c.Key("linear-board")).Grow(1).MinHeight(0).Gap(12).Padding(12, 14, 14, 14).Children(func() {
		for _, s := range shown {
			col := byState[s.Name]
			ui.Column(c).Width(linearColW).FillHeight().Shrink(0).Gap(8).Children(func() {
				ui.Row(c).Height(22).Shrink(0).AlignItems(ui.Center).Gap(7).Children(func() {
					linearStateGlyph(c, pal, s.Type, 11)
					ui.Text(c, s.Name).FontSize(12.5).FontWeight(600).TextColor(pal.textMuted)
					ui.Textf(c, "%d", len(col)).FontSize(12.5).TextColor(pal.textFaint).FontFeatures("tnum")
				})
				ui.Scroll(c.Key("linear-col-" + s.Name)).Grow(1).MinHeight(0).Gap(8).Children(func() {
					for _, i := range col {
						a.linearCard(c, pal, i)
					}
				})
			})
		}
	})
}

// linearCard is one issue on the board.
func (a *App) linearCard(c *ui.Context, pal *palette, i proto.LinearIssue) {
	open := a.lin.open == i.ID
	card := ui.ButtonBase(c.Key("card-"+i.ID)).Column().Width(linearColW).Shrink(0).Gap(6).Padding(11, 12, 11, 12).
		Radius(11).Background(pal.chip)
	if open {
		card.Background(pal.rowSelected)
		chosen(card, pal)
	} else if card.Hovered() {
		card.Background(pal.rowHover)
	}
	if card.Clicked() {
		a.linearOpen(i.ID)
	}
	card.Children(func() {
		ui.Row(c).Height(16).Shrink(0).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, i.Identifier).FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).FontFeatures("tnum")
			ui.Spacer(c)
			if i.Session != "" {
				linearSessionBadge(c, pal, i)
			}
		})
		ui.Text(c, i.Title).FontSize(13).TextColor(pal.text).MaxWidth(linearColW - 24).LineHeight(1.3)
		ui.Row(c).Height(14).Shrink(0).AlignItems(ui.Center).Gap(6).Children(func() {
			if i.Milestone != "" {
				ui.Text(c, i.Milestone).FontSize(11).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
			}
			ui.Spacer(c)
			if i.Blocked > 0 {
				ui.Textf(c, "⊣%d", i.Blocked).FontSize(11).TextColor(pal.textFaint)
			}
			if i.Blocking > 0 {
				ui.Textf(c, "⊢%d", i.Blocking).FontSize(11).TextColor(pal.textFaint)
			}
			ui.Text(c, ago(i.UpdatedAt, time.Now())).FontSize(11).TextColor(pal.textFaint)
		})
	})
}

// linearSessionBadge is the mark of the session working on an issue: a
// dot of its status, and the name it goes by.
func linearSessionBadge(c *ui.Context, pal *palette, i proto.LinearIssue) {
	row := ui.Row(c).Height(16).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
		statusIconP(c, pal, i.SessionStatus, false, i.Progress, 10)
		ui.Text(c, i.SessionName).FontSize(11).TextColor(pal.textMuted).SingleLine().MaxWidth(90).Ellipsis("…")
	})
	row.Tooltip("The session working on this issue")

}

// linearList is the issues as rows, one under another.
func (a *App) linearList(c *ui.Context, pal *palette) {
	issues := a.linearIssues()
	if len(issues) == 0 {
		a.linearNothing(c, pal, locales.LinearNone(), a.linearEmptyDetail())
		return
	}
	ui.Scroll(c.Key("linear-list")).Grow(1).MinHeight(0).Padding(4, 14, 14, 14).Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).Gap(2).Children(func() {
			for _, i := range issues {
				a.linearRow(c, pal, i)
			}
		})
	})
}

// linearRow is one issue in the list.
func (a *App) linearRow(c *ui.Context, pal *palette, i proto.LinearIssue) {
	open := a.lin.open == i.ID
	row := ui.ButtonBase(c.Key("row-"+i.ID)).Height(36).Shrink(0).AlignItems(ui.Center).Gap(10).Padding(0, 10).Radius(8)
	if open {
		row.Background(pal.rowSelected)
		chosen(row, pal)
	} else if row.Hovered() {
		row.Background(pal.rowHover)
	}
	if row.Clicked() {
		a.linearOpen(i.ID)
	}
	row.Children(func() {
		ui.Text(c, i.Identifier).FontSize(12).FontWeight(600).TextColor(pal.textFaint).FontFeatures("tnum").Width(64).Shrink(0)
		ui.Text(c, i.Title).FontSize(13).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if i.Project != "" {
			ui.Text(c, i.Project).FontSize(11.5).TextColor(pal.textFaint).SingleLine().MaxWidth(90).Ellipsis("…").Shrink(0)
		}
		if i.Milestone != "" {
			ui.Text(c, i.Milestone).FontSize(11.5).TextColor(pal.textFaint).SingleLine().MaxWidth(120).Ellipsis("…").Shrink(0)
		}
		if i.Session != "" {
			ui.Text(c, i.SessionName).FontSize(11.5).TextColor(pal.textMuted).SingleLine().MaxWidth(110).Ellipsis("…").Shrink(0)
		}
		linearStateChip(c, pal, i.State, i.StateType)
	})
}

// linearStateChip is the state of an issue, as a word with a mark.
func linearStateChip(c *ui.Context, pal *palette, name, typ string) {
	row := ui.Row(c).Height(20).Shrink(0).Gap(5).AlignItems(ui.Center).Padding(0, 7).Radius(6).Background(pal.chip)
	row.Children(func() {
		linearStateGlyph(c, pal, typ, 9)
		ui.Text(c, name).FontSize(11).TextColor(pal.textMuted).SingleLine()
	})
}

// linearStateGlyph is the mark of a state: what the state means, drawn
// rather than named, as the sidebar draws a session's status.
func linearStateGlyph(c *ui.Context, pal *palette, typ string, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0)
	fg := pal.textMuted
	switch typ {
	case "started":
		fg = pal.attention
	case "completed":
		fg = pal.done
	case "canceled", "duplicate":
		fg = pal.textFaint
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := min(r.W, r.H)/2 - 0.5
		switch typ {
		case "completed":
			var disc ui.Path
			disc.Circle(cx, cy, rad)
			p.FillPath(&disc, fg)
			check := (&ui.Path{}).MoveTo(cx-rad*0.42, cy+0.2).LineTo(cx-rad*0.08, cy+rad*0.36).LineTo(cx+rad*0.46, cy-rad*0.34)
			p.StrokePath(check, 1.4, pal.onDone)
		case "started":
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, 1.5, fg)
			var dot ui.Path
			dot.Circle(cx, cy, rad*0.38)
			p.FillPath(&dot, fg)
		case "canceled":
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, 1.4, fg)
			var cross ui.Path
			cross.MoveTo(cx-rad*0.4, cy-rad*0.4).LineTo(cx+rad*0.4, cy+rad*0.4)
			p.StrokePath(&cross, 1.4, fg)
		case "duplicate":
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, 1.4, fg)
			var slash ui.Path
			slash.MoveTo(cx-rad*0.55, cy+rad*0.55).LineTo(cx+rad*0.55, cy-rad*0.55)
			p.StrokePath(&slash, 1.4, fg)
		case "backlog":
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, 1.2, fg)
		default:
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, 1.4, fg)
		}
	})
	return box
}

// linearNothing is what the view says when there is nothing to show.
func (a *App) linearNothing(c *ui.Context, pal *palette, title, detail string) {
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(pal.text)
		ui.Text(c, detail).FontSize(12.5).TextColor(pal.textMuted).MaxWidth(320).TextAlign(ui.Center).LineHeight(1.4)
	})
}

// linearOpen opens an issue in the detail, and reads it.
func (a *App) linearEmptyDetail() string {
	if a.st.Linear == nil || a.st.Linear.Status != proto.LinearOK {
		return a.linearStatusLabel()
	}
	return locales.LinearNoneFiltered()
}

func (a *App) linearOpen(id string) {
	if a.lin.open != id {
		a.lin.detail = nil
	}
	a.lin.open = id
	a.lin.request++
	request := a.lin.request
	a.lin.err = ""
	a.lin.loading = true
	cl := a.cl
	if cl == nil {
		a.lin.loading = false
		a.lin.err = "Connect to the updated Shibu daemon to read this issue."
		return
	}
	go func() {
		var d proto.LinearIssueDetail
		err := cl.Call("linear.issue", proto.LinearParams{Issue: id}, &d)
		a.update(func() {
			a.linearComplete(id, request, &d, err)
			if err != nil || a.lin.open != id || a.lin.request != request {
				return
			}
			time.AfterFunc(10*time.Second, func() {
				a.update(func() {
					if a.show == "linear" && a.lin.open == id && a.lin.request == request {
						a.linearOpen(id)
					}
				})
			})
		})
	}()
}

func (a *App) linearComplete(id string, request uint64, detail *proto.LinearIssueDetail, err error) {
	if a.lin.open != id || a.lin.request != request {
		return
	}
	a.lin.loading = false
	if err != nil {
		a.lin.err = "Could not read the issue. Check the connection and retry."
		return
	}
	a.lin.detail = detail
	a.lin.err = ""
}

// linearDetailPane is the issue open beside the board: what it says, and
// the session working on it.
func (a *App) linearDetailPane(c *ui.Context, pal *palette) {
	services := c.Services()
	openURL := func(link string) {
		if linearSafeURL(link) {
			services.OpenURL(link)
		}
	}
	panel := ui.Column(c).MinHeight(0).Background(pal.cardDim)
	if width, _ := c.Size(); width < 1000 {
		panel.Grow(1).MinWidth(0)
	} else {
		panel.Width(linearDetail).Shrink(0)
	}
	panel.Children(func() {
		ui.Row(c).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 8, 0, 14).Gap(8).DragWindow().Children(func() {
			ui.Text(c, locales.LinearIssue()).FontSize(13).FontWeight(600).TextColor(pal.textMuted)
			ui.Spacer(c)
			iconButton(c, "linear-close", closeIcon, "Close", pal, func() {
				a.lin.open = ""
				a.lin.request++
				a.lin.detail = nil
			})
		})
		if a.lin.err != "" {
			a.linearNothing(c, pal, "Issue unavailable", a.lin.err)
			if button(c, "linear-retry", "Retry", "", false, pal) {
				a.linearOpen(a.lin.open)
			}
			return
		}
		if a.lin.detail == nil {
			ui.Column(c).Fill().Center().Children(func() {
				ui.Spinner(c).Label("Reading the issue")
			})
			return
		}
		d := a.lin.detail
		ui.Scroll(c.Key("linear-detail")).Grow(1).MinHeight(0).Padding(0, 16, 16, 16).Children(func() {
			ui.Column(c).Grow(1).MinHeight(0).Gap(14).Children(func() {
				ui.Text(c, d.Identifier).FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).FontFeatures("tnum")
				ui.Text(c, d.Title).FontSize(17).FontWeight(650).TextColor(pal.text).LineHeight(1.25)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					linearStateChip(c, pal, d.State, d.StateType)
					if d.Project != "" {
						ui.Text(c, d.Project).FontSize(11.5).TextColor(pal.textFaint)
					}
					if d.Milestone != "" {
						ui.Text(c, "· "+d.Milestone).FontSize(11.5).TextColor(pal.textFaint)
					}
				})
				a.linearSession(c, pal, d)
				if d.Assignee != "" {
					ui.Text(c, "Assigned to "+d.Assignee).FontSize(12).TextColor(pal.textMuted)
				}
				if len(d.Labels) > 0 {
					ui.Text(c, strings.Join(d.Labels, " · ")).FontSize(12).TextColor(pal.textMuted)
				}
				if d.Parent != "" && ui.Button(c, "Parent: "+d.Parent).Clicked() {
					a.linearOpen(d.Parent)
				}
				for _, sub := range d.SubIssues {
					if ui.Button(c, sub.Identifier+" · "+sub.Title).Clicked() {
						a.linearOpen(sub.ID)
					}
				}
				if button(c, "linear-browser", "Open in Linear", "", false, pal) {
					openURL(d.URL)
				}
				if d.Description != "" {
					ui.Box(c).Height(1).Background(pal.divider)
					ui.Column(c).Grow(1).MinHeight(0).Children(func() {
						markdown.View(c, linearMarkdown(d.Description), &markdown.Options{OnLink: openURL})
					})
				}
				if len(d.Attachments) > 0 {
					ui.Box(c).Height(1).Background(pal.divider)
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, locales.LinearResources()).FontSize(12).FontWeight(600).TextColor(pal.textMuted)
						for _, at := range d.Attachments {
							link := ui.Text(c, at.Title).FontSize(12.5).TextColor(pal.accent)
							if link.Clicked() {
								openURL(at.URL)
							}
							if at.URL != "" {
								link.Tooltip(at.URL)
							}
						}
					})
				}
				if len(d.Relations) > 0 {
					ui.Box(c).Height(1).Background(pal.divider)
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, locales.LinearRelated()).FontSize(12).FontWeight(600).TextColor(pal.textMuted)
						for _, r := range d.Relations {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, relationMark(r.Type)).FontSize(11.5).TextColor(pal.textFaint)
								if ui.Button(c, r.Identifier).Clicked() {
									a.linearOpen(r.Identifier)
								}
								ui.Text(c, r.Title).FontSize(12.5).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
							})
						}
					})
				}
				if len(d.Comments) > 0 {
					ui.Box(c).Height(1).Background(pal.divider)
					ui.Column(c).Gap(12).Children(func() {
						ui.Text(c, locales.LinearActivity()).FontSize(12).FontWeight(600).TextColor(pal.textMuted)
						for _, cm := range d.Comments {
							ui.Column(c).Gap(4).Children(func() {
								ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
									ui.Text(c, cm.Author).FontSize(11.5).FontWeight(600).TextColor(pal.textMuted)
									ui.Text(c, ago(cm.CreatedAt, time.Now())).FontSize(11).TextColor(pal.textFaint)
								})
								ui.Column(c).Grow(1).MinHeight(0).Children(func() {
									markdown.View(c, linearMarkdown(cm.Body), &markdown.Options{OnLink: openURL})
								})
							})
						}
					})
				}
			})
		})
	})
}

// relationMark is the mark of a relation: what it does, in a word.
func relationMark(typ string) string {
	switch typ {
	case "blocks":
		return "⊢ blocks"
	case "blockedBy":
		return "⊣ blocked by"
	case "duplicate":
		return "⧉ duplicate"
	}
	return "↔ related"
}

// linearSession is the session working on the issue, and the id that
// resumes it: what a person needs to open the work again.
func (a *App) linearSession(c *ui.Context, pal *palette, d *proto.LinearIssueDetail) {
	if d.Session == "" {
		ids := []string{""}
		for _, s := range a.st.Sessions {
			ids = append(ids, s.ID)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			popup(c, "linear-link-session", &a.lin.linkSession, ids, func(id string) string {
				if s := a.session(id); s != nil {
					return s.DisplayName()
				}
				return "Choose session…"
			}, 200, pal)
			if ui.Button(c, "Link session").Disabled(a.lin.linkSession == "").Clicked() {
				a.call("session.link", proto.LinkParams{Session: a.lin.linkSession, Task: d.Identifier}, func(err error) {
					if err != nil {
						a.toast(err.Error())
						return
					}
					a.linearOpen(d.ID)
				})
			}
		})
		return
	}
	ui.Column(c).Gap(8).Padding(12, 12, 12, 12).Radius(10).Background(pal.chip).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, locales.LinearSession()).FontSize(11.5).FontWeight(600).TextColor(pal.textMuted)
			ui.Spacer(c)
			ui.Text(c, d.SessionName).FontSize(11.5).TextColor(pal.textFaint).SingleLine().MaxWidth(150).Ellipsis("…")
		})
		if d.ResumeID != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, locales.LinearResume()).FontSize(11.5).FontWeight(500).TextColor(pal.textMuted)
				ui.Text(c, d.ResumeID).FontSize(11.5).Font("monospace").TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				iconButton(c, "linear-copy", glyph("f7-square-split-2x1"), locales.LinearCopyResume(), pal, func() {
					a.copyText = d.ResumeID
					a.toast("The resume id was copied")
				})
			})
		}
		ui.Column(c).Gap(8).Children(func() {
			if button(c, "linear-unlink", "Unlink", "", false, pal) {
				a.call("session.link", proto.LinkParams{Session: d.Session}, func(err error) {
					if err != nil {
						a.toast(err.Error())
						return
					}
					a.linearOpen(d.ID)
				})
			}
			if button(c, "linear-open-session", locales.LinearOpenSession(), "", false, pal) {
				a.selectSession(d.Session)
				a.lin.open = ""
				a.lin.request++
				a.lin.detail = nil
			}
			if button(c, "linear-copy-session", locales.LinearCopySession(), "", false, pal) {
				a.copyText = d.Session
				a.toast("The session id was copied")
			}
		})
	})
}

// Remote issue content can only open web links, never local files or
// command/custom URL handlers.
func linearSafeURL(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
