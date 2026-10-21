package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// The Attention pane is what needs the user, of every session, in one
// list: the state kerod folds from each session's panes
// (proto.Session.Attention), the words that go with it, and the pane it
// came from. A row takes the user to that pane, where the decision is —
// a permission, a question, the end of a turn. It is a view as the Tasks,
// Files and Browser panes are: kerod keeps where it is and what it shows,
// and the app keeps what it holds while it shows (a.attention, one by
// pane).

// attentionStates are the states a row can show, the most urgent first:
// the order the daemon ranks them in (attention.go), and the list's.
var attentionStates = []string{
	proto.AttentionWaitingUser,
	proto.AttentionRunning,
	proto.AttentionUnread,
	proto.AttentionDone,
	proto.AttentionIdle,
}

// attentionFilters are the pane's chips, in the order they show: all of
// the sessions first, then the states but rest, which is every session's
// own.
var attentionFilters = append([]string{""}, attentionStates[:len(attentionStates)-1]...)

// attentionSymbol is the SF Symbol a state's mark is: a raised hand for a
// session that asks of the user, a turning arrow for one at work, a
// filled check for one seen not yet, a plain check for one seen, and a
// ring for one at rest.
func attentionSymbol(state string) string {
	switch state {
	case proto.AttentionWaitingUser:
		return "hand.raised.fill"
	case proto.AttentionRunning:
		return "arrow.triangle.2.circlepath"
	case proto.AttentionUnread:
		return "checkmark.circle.fill"
	case proto.AttentionDone:
		return "checkmark.circle"
	}
	return "circle"
}

// attentionLabel is a state in the app's words, as the sidebar says the
// same of a session.
func attentionLabel(state string) string {
	switch state {
	case proto.AttentionWaitingUser:
		return locales.StatusNeedsYou()
	case proto.AttentionRunning:
		return locales.StatusWorking()
	case proto.AttentionUnread:
		return locales.StatusUnread()
	case proto.AttentionDone:
		return locales.StatusDone()
	}
	return locales.StatusIdle()
}

// attentionColor is a state's color: the amber the app gives what needs
// the user, the filled check's for a turn ended unseen, and the muted
// text's for the rest, as the sidebar draws them.
func attentionColor(pal *palette, state string) ui.Color {
	switch state {
	case proto.AttentionWaitingUser:
		return pal.attention
	case proto.AttentionRunning:
		return pal.text
	case proto.AttentionUnread:
		return pal.done
	case proto.AttentionDone:
		return pal.textMuted
	}
	return pal.textFaint
}

// attentionFilterLabel is a chip's words: a state's, or all of them.
func attentionFilterLabel(filter string) string {
	if filter == "" {
		return locales.AttentionAll()
	}
	return attentionLabel(filter)
}

// attentionView is what an Attention pane holds while it shows: the
// filter and search of the list, and where it is scrolled to.
type attentionView struct {
	filter string // one of attentionFilters, "" all of them
	query  string // plain text, found in what a row says
	scroll ui.ScrollState
}

// attentionViewOf is the view of the pane with the id, made as it first
// shows.
func (a *App) attentionViewOf(pane string) *attentionView {
	if a.attention == nil {
		a.attention = map[string]*attentionView{}
	}
	st := a.attention[pane]
	if st == nil {
		st = &attentionView{}
		a.attention[pane] = st
	}
	return st
}

// attentionRow is a session on the pane's list: its state, and the pane
// and agent it comes from.
type attentionRow struct {
	session *proto.Session
	att     *proto.Attention
	agent   string
}

// attentionRows are the sessions that have an attention to show — not
// those at rest — the most urgent first, and of two as urgent the one whose state came last, so that a
// new wait is at the top of its kind.
func (a *App) attentionRows() []attentionRow {
	var rows []attentionRow
	for _, g := range a.groups() {
		for _, s := range g.sessions {
			// A session at rest needs nothing: kerod sends the idle state
			// for it, and it is no row.
			if s.Attention == nil || s.Attention.State == "" || s.Attention.State == proto.AttentionIdle {
				continue
			}
			// The agent that needs the user is the one of the pane the
			// state comes from; the lead's when the pane has gone.
			agent := s.Agent
			if p := s.PaneByID(s.Attention.Pane); p != nil && p.Agent != "" {
				agent = p.Agent
			}
			rows = append(rows, attentionRow{session: s, att: s.Attention, agent: agent})
		}
	}
	slices.SortStableFunc(rows, func(x, y attentionRow) int {
		if d := attentionRank(y.att.State) - attentionRank(x.att.State); d != 0 {
			return d
		}
		switch {
		case x.att.Since > y.att.Since:
			return -1
		case x.att.Since < y.att.Since:
			return 1
		}
		return 0
	})
	return rows
}

// attentionRank orders the states as attentionStates does: the first is
// the most urgent.
func attentionRank(state string) int {
	if i := slices.Index(attentionStates, state); i >= 0 {
		return len(attentionStates) - i
	}
	return 0
}

// attentionShown is the rows the pane lists: those of its filter, and
// those that say what it searches for.
func (a *App) attentionShown(rows []attentionRow, st *attentionView) []attentionRow {
	query := strings.ToLower(strings.TrimSpace(st.query))
	shown := make([]attentionRow, 0, len(rows))
	for _, r := range rows {
		if st.filter != "" && r.att.State != st.filter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(a.attentionSay(r)), query) {
			continue
		}
		shown = append(shown, r)
	}
	return shown
}

// attentionSay is all a row says, as a search reads it: the session's
// name, its project, the agent, the state's words and the state.
func (a *App) attentionSay(r attentionRow) string {
	var b strings.Builder
	b.WriteString(r.session.DisplayName(a.set.SessionName))
	if p := a.project(r.session.Project); p != nil {
		b.WriteString(" " + p.Name)
	}
	b.WriteString(" " + r.agent)
	b.WriteString(" " + r.att.Msg)
	b.WriteString(" " + attentionLabel(r.att.State))
	b.WriteString(" " + r.att.State)
	return b.String()
}

// attentionPane is a pane's list: its chips and its search over it, then
// a row for each session that needs the user.
func (a *App) attentionPane(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane, body ui.Element) {
	st := a.attentionViewOf(p.ID)
	rows := a.attentionRows()
	if len(rows) == 0 {
		a.attentionNothing(c, pal, locales.AttentionEmptyTitle(), locales.AttentionEmptyDetail(), nil)
		return
	}
	a.attentionToolbar(c, pal, st, rows)
	shown := a.attentionShown(rows, st)
	if len(shown) == 0 {
		a.attentionNothing(c, pal, locales.AttentionEmptyFilteredTitle(), locales.AttentionEmptyFilteredDetail(), func() {
			if button(c, "attention-clear", locales.AttentionClearFilter(), "", true, pal) {
				st.filter, st.query = "", ""
			}
		})
		return
	}
	list := ui.Scroll(c.Key("attention-list")).Grow(1).MinHeight(0).MinWidth(0).Padding(0, 8, 6, 8).TrackScroll(&st.scroll)
	list.Children(func() {
		for _, r := range shown {
			a.attentionRowView(c, pal, r)
		}
	})
}

// attentionToolbar is the pane's chips, each saying how many sessions it
// shows, and its search.
func (a *App) attentionToolbar(c *ui.Context, pal *palette, st *attentionView, rows []attentionRow) {
	count := func(filter string) int {
		if filter == "" {
			return len(rows)
		}
		n := 0
		for _, r := range rows {
			if r.att.State == filter {
				n++
			}
		}
		return n
	}
	ui.Row(c.Key("attention-toolbar")).Height(40).Shrink(0).Gap(6).Padding(0, 12, 0, 12).AlignItems(ui.Center).Children(func() {
		for _, f := range attentionFilters {
			ch := ui.Row(c.Key("chip-"+f)).Height(22).Padding(0, 8).Gap(5).Radius(11).AlignItems(ui.Center).
				Shrink(0).Cursor(ui.CursorPointer)
			bg, fg := ui.Transparent, pal.textMuted
			switch {
			case st.filter == f:
				bg, fg = pal.accent.Alpha(0.14), pal.accent
			case ch.Hovered():
				bg = pal.rowHover
			}
			ch.Background(ch.AnimateColor("bg", bg, hover))
			if ch.Clicked() {
				st.filter = f
			}
			ch.Children(func() {
				ui.Text(c, attentionFilterLabel(f)).FontSize(11.5).FontWeight(600).TextColor(fg).SingleLine().Shrink(0)
				ui.Textf(c, "%d", count(f)).FontSize(11).TextColor(fg.Alpha(0.7)).FontFeatures("tnum").SingleLine().Shrink(0)
			})
		}
		a.attentionSearch(c, pal, st)
	})
}

// attentionSearch is the pane's search: plain text, found in what each
// row says.
func (a *App) attentionSearch(c *ui.Context, pal *palette, st *attentionView) {
	var in ui.Element
	frame := ui.Row(c.Key("attention-search")).Cursor(ui.CursorText).Grow(1).MinWidth(90).MaxWidth(200)
	frame.Children(func() {
		symbolMark(c, "magnifyingglass", 12, pal.textFaint, pal.dark)
		in = ui.TextInputBase(c.Key("in"), &st.query).Grow(1).MinWidth(0).FontSize(12.5).
			Placeholder(locales.AttentionSearchPlaceholder()).Label(locales.AttentionSearch())
		in.OnShortcut(0, ui.KeyEscape, func() { st.query = "" })
		if st.query != "" {
			x := ui.ButtonBase(c.Key("clear")).Size(15, 15).Radius(8).Center().Shrink(0).
				Background(pal.textFaint.Alpha(0.55)).TextColor(pal.card).Label(locales.AttentionClearFilter())
			x.Children(func() { symbolMark(c, "xmark", 8, pal.card, pal.dark) })
			if x.Clicked() {
				st.query = ""
			}
		}
	})
	field(c, frame, in.Focused(), pal).Height(26)
}

// attentionRowView is one session: its state and its words at the left,
// its name and what it says in the middle, and the agent and how long it
// has been so at the right. A click opens its pane.
func (a *App) attentionRowView(c *ui.Context, pal *palette, r attentionRow) {
	row := ui.Row(c.Key("attention-"+r.session.ID)).Height(52).Padding(0, 12).Gap(12).AlignItems(ui.Center).Radius(8)
	bg := ui.Transparent
	switch {
	case r.session.ID == a.selected:
		bg = pal.rowSelected
	case row.Hovered():
		bg = pal.rowHover
	}
	row.Background(row.AnimateColor("bg", bg, hover))
	if row.Clicked() {
		a.gotoAttention(r)
	}
	color := attentionColor(pal, r.att.State)
	row.Children(func() {
		ui.Row(c.Key("state")).Gap(6).AlignItems(ui.Center).Width(96).Shrink(0).Children(func() {
			symbolMark(c, attentionSymbol(r.att.State), 13, color, pal.dark)
			ui.Text(c, attentionLabel(r.att.State)).FontSize(11.5).FontWeight(600).TextColor(color).SingleLine().Ellipsis("…")
		})
		ui.Column(c.Key("what")).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c.Key("name")).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, r.session.DisplayName(a.set.SessionName)).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidth(280)
				if p := a.project(r.session.Project); p != nil {
					ui.Text(c, p.Name).FontSize(12).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				}
			})
			ui.Text(c, a.attentionSummary(r)).FontSize(12.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
		})
		ui.Column(c.Key("who")).AlignItems(ui.End).Shrink(0).Gap(2).Children(func() {
			if r.agent != "" {
				ui.Text(c, r.agent).FontSize(11.5).TextColor(pal.textMuted).SingleLine().Shrink(0)
			}
			ui.Text(c, ago(r.att.Since, c.Now())).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
		})
	})
}

// attentionSummary is a row's one line: what the hook said, exactly, and
// where an agent says nothing, the state's own words. A Codex pane's
// approval is found by the transcript scan, not asked for by a hook, so
// its row says what to do after whatever kerod worded the wait as.
func (a *App) attentionSummary(r attentionRow) string {
	codexWait := r.att.State == proto.AttentionWaitingUser && r.agent == "codex"
	switch {
	case r.att.Msg != "" && codexWait:
		// kerod words every wait, so a Codex one always has a message
		// ("Needs permission"); the way to answer it follows.
		return r.att.Msg + " · " + locales.AttentionApprove()
	case r.att.Msg != "":
		return r.att.Msg
	case codexWait:
		return locales.AttentionApprove()
	}
	return attentionLabel(r.att.State)
}

// gotoAttention shows a row's session and puts the keyboard in the pane
// that needs the user, where the decision is: not zoomed, so that the
// panes beside it stay in sight. A zoom on another pane is ended, as
// setFocus alone would leave it hiding the target.
func (a *App) gotoAttention(r attentionRow) {
	delete(a.prefs.Collapsed, r.session.Project)
	a.selectSession(r.session.ID)
	s := a.session(r.session.ID)
	if s == nil || r.att.Pane == "" || s.PaneByID(r.att.Pane) == nil {
		return
	}
	if s.Zoomed != "" {
		a.zoomPane(s, s.Zoomed) // a toggle: the zoom ends
	}
	a.setFocus(s, r.att.Pane)
	a.paneRef(r.att.Pane).Focus()
}

// attentionNothing is the pane's empty state, as the Linear and Tasks
// panes say theirs: the mark, what it is, and what can be done about it.
func (a *App) attentionNothing(c *ui.Context, pal *palette, title, detail string, action func()) {
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Center().Background(pal.chip).Margin(0, 0, 6, 0).Children(func() {
			symbolMark(c, paneViewSymbol(proto.PaneAttention), 22, pal.textMuted, pal.dark)
		})
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(pal.text)
		ui.Text(c, detail).FontSize(12.5).TextColor(pal.textMuted).MaxWidth(320).TextAlign(ui.Center).LineHeight(1.4)
		if action != nil {
			ui.Box(c).Height(6)
			action()
		}
	})
}
