package main

import (
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// The Tasks pane is the Linear issues of the session's project, those
// of the Linear project it is linked to: a list, with what is done left
// out until asked, the issue this session works on first, and an issue
// open in the pane's place, with the way back. Each pane keeps its own
// view (a.tasks); the tray's board keeps its.

// tasksState is the view of a Tasks pane, made as it first shows.
func (a *App) tasksState(pane string) *linearState {
	if a.tasks == nil {
		a.tasks = map[string]*linearState{}
	}
	st := a.tasks[pane]
	if st == nil {
		st = &linearState{view: "list", pane: true, hideDone: true}
		a.tasks[pane] = st
	}
	return st
}

func (a *App) tasksPane(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane, body ui.Element) {
	lin := a.st.Linear
	if lin == nil || lin.Status != proto.LinearOK && len(lin.Issues) == 0 {
		a.linearNothingAction(c, pal, locales.LinearNotRead(), locales.LinearNotReadDetail())
		return
	}
	st := a.tasksState(p.ID)
	lp := a.projectLinear(s.Project)
	if lp == nil && !st.allTasks {
		a.tasksLinkPrompt(c, pal, s, st)
		return
	}
	st.project = ""
	if lp != nil {
		st.project = lp.Name
	}
	a.withLin(st, func() {
		m := a.linearModel(pal)
		a.linearFreshen()
		if st.openURL != "" {
			c.OpenURL(st.openURL)
			st.openURL = ""
		}
		// The keys are the pane's: only while it has the keyboard, and
		// not while its search has.
		if st.open != "" && !st.searching {
			if body.Shortcut(0, ui.KeyEscape) {
				a.linearClose()
			}
			if body.Shortcut(0, ui.KeyJ) || body.Shortcut(0, ui.KeyDown) {
				a.linearStep(m, 1)
			}
			if body.Shortcut(0, ui.KeyK) || body.Shortcut(0, ui.KeyUp) {
				a.linearStep(m, -1)
			}
		}
		st.searching = false
		if st.open != "" {
			w := a.paneRef("card-" + p.ID).Bounds(c).W
			a.linearDetailPane(c, pal, m, ui.TitleBar{}, 0, true, w)
			return
		}
		a.tasksToolbar(c, pal, m, lp)
		a.tasksMine(c, pal, m, s)
		ui.Column(c).Grow(1).MinHeight(0).Children(func() {
			a.linearList(c, pal, m)
		})
	})
}

// tasksToolbar is the pane's search, whether what is done shows, and
// how many issues show.
func (a *App) tasksToolbar(c *ui.Context, pal *palette, m *linearModel, lp *proto.LinearProject) {
	st := a.lin
	ui.Row(c).Height(40).Shrink(0).Gap(8).Padding(0, 12, 0, 12).AlignItems(ui.Center).Children(func() {
		a.linearSearch(c, pal)
		ui.Box(c).Grow(1)
		label := locales.TasksShowDone()
		if !st.hideDone {
			label = locales.TasksHideDone()
		}
		t := ui.Text(c.Key("tasks-done"), label).FontSize(12).TextColor(pal.accent).Padding(0, 4).Cursor(ui.CursorPointer).SingleLine().Shrink(0)
		if t.Clicked() {
			st.hideDone = !st.hideDone
		}
		ui.Textf(c, "%d", len(m.issues)).FontSize(11.5).TextColor(pal.textFaint).FontFeatures("tnum").Shrink(0).Tooltip(locales.TasksCountTip())
	})
}

// tasksMine is the issue the session works on, above the list, when it
// is one the list has.
func (a *App) tasksMine(c *ui.Context, pal *palette, m *linearModel, s *proto.Session) {
	var mine *proto.LinearIssue
	for i := range m.issues {
		is := &m.issues[i]
		if is.Session == s.ID {
			mine = is
			break
		}
		for _, p := range s.Panes {
			if p.TaskID != "" && (p.TaskID == is.ID || p.TaskID == is.Identifier) {
				mine = is
			}
		}
		if mine != nil {
			break
		}
	}
	if mine == nil {
		return
	}
	ui.Column(c).Shrink(0).Gap(2).Padding(0, 12, 6, 12).Children(func() {
		ui.Text(c, locales.TasksThisSession()).FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).Padding(0, 12)
		a.linearRow(c, pal, m, *mine)
	})
}

// tasksLinkPrompt asks which Linear project the session's project is
// tracked in: the one named as it, offered first, or any; or to show
// the whole team's issues instead.
func (a *App) tasksLinkPrompt(c *ui.Context, pal *palette, s *proto.Session, st *linearState) {
	proj := a.project(s.Project)
	if proj == nil {
		a.linearNothingAction(c, pal, locales.LinearNotRead(), locales.LinearNotReadDetail())
		return
	}
	lin := a.st.Linear
	guess := linearSuggest(*proj, lin.Projects)
	ids := []string{""}
	for _, lp := range linearProjectsFor(lin.Projects, guess) {
		ids = append(ids, lp.ID)
	}
	if st.pick == "" {
		st.pick = guess
	}
	name := func(id string) string {
		for _, lp := range lin.Projects {
			if lp.ID == id {
				return lp.Name
			}
		}
		return locales.TasksChooseProject()
	}
	link := func(id string) {
		a.call("project.linear", proto.ProjectLinearParams{Project: proj.ID, LinearProject: id}, nil)
		// Shown at once; the state with it follows.
		for i := range a.st.Projects {
			if a.st.Projects[i].ID == proj.ID {
				a.st.Projects[i].LinearProject = id
			}
		}
	}
	a.linearNothingThen(c, pal, locales.TasksLinkTitle(proj.Name), locales.TasksLinkDetail(), func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			popup(c, "tasks-pick", &st.pick, ids, name, 200, pal).Shrink(1).MinWidth(0)
			if st.pick != "" && button(c, "tasks-link", locales.LinearLinkSession(), "", true, pal) {
				link(st.pick)
			}
		})
		all := ui.Text(c.Key("tasks-all"), locales.TasksShowAll()).FontSize(12).TextColor(pal.accent).Cursor(ui.CursorPointer).Padding(6, 0, 0, 0)
		if all.Clicked() {
			st.allTasks = true
		}
	})
}
