package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"kero/internal/proto"

	"kero/locales"
)

// The detail is the issue open, in a card of its own at the board's
// right: where it is in the workspace and in its column, its title, its
// properties as an inspector, the session on it, what it says, what it
// links to, and what was said under it.

// linearDetailPane is the issue open. alone says the card stands in the
// board's place, on a window too narrow for both: it then starts after
// the traffic lights, and leads back to the board.
func (a *App) linearDetailPane(c *ui.Context, pal *palette, m *linearModel, bar ui.TitleBar, shown float32, alone bool, width float32) {
	board := a.linearIssue(a.lin.open)
	d := a.lin.detail
	a.linearDetailHeader(c, pal, m, bar, shown, alone, board, d)
	switch {
	case a.lin.err != "":
		a.linearNothingThen(c, pal, locales.LinearUnavailable(), a.lin.err, func() {
			if button(c, "linear-retry", locales.LinearRetry(), "", false, pal) {
				a.linearOpen(a.lin.open)
			}
		})
		return
	case d == nil:
		working(c, pal, locales.LinearReading())
		return
	}
	// The board's copy is the live one: its state and its session move
	// as the daemon reads, while what was read of the issue waits for
	// its next read.
	live := d.LinearIssue
	if board != nil {
		live.State, live.StateType = board.State, board.StateType
		live.Session, live.SessionName, live.SessionStatus, live.Progress = board.Session, board.SessionName, board.SessionStatus, board.Progress
	}
	wide := width >= linearWide && !alone
	ui.Scroll(c.Key("linear-detail")).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).AlignItems(ui.Start).Gap(0).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(18).Padding(14, 24, 28, 24).Children(func() {
				ui.Text(c, d.Title).FontSize(20).FontWeight(650).TextColor(pal.text).LineHeight(1.25).Selectable()
				if !wide {
					a.linearProperties(c, pal, m, d, live, false)
				}
				if d.Description != "" {
					ui.Column(c).Children(func() {
						markdown.View(c, linearMarkdown(d.Description), &markdown.Options{OnLink: a.openLinearURL})
					})
				}
				a.linearSubIssues(c, pal, m, d)
				a.linearResources(c, pal, d)
				if !wide {
					a.linearRelations(c, pal, m, d)
				}
				a.linearActivity(c, pal, d)
			})
			if wide {
				ui.Column(c).Width(264).Shrink(0).Gap(18).Padding(14, 20, 28, 4).Children(func() {
					a.linearProperties(c, pal, m, d, live, true)
					a.linearRelations(c, pal, m, d)
				})
			}
		})
	})
}

// linearDetailHeader is the detail card's header: the way back on a
// narrow window, where the issue is, its place in its column with the
// arrows that step through it, and the issue's buttons.
func (a *App) linearDetailHeader(c *ui.Context, pal *palette, m *linearModel, bar ui.TitleBar, shown float32, alone bool, board *proto.LinearIssue, d *proto.LinearIssueDetail) {
	pad := float32(14)
	if alone && !a.lin.pane {
		pad = a.chrome.headerPad
	}
	if a.lin.pane {
		pad = 8
	}
	project, ident, link := "", "", ""
	switch {
	case d != nil:
		project, ident, link = d.Project, d.Identifier, d.URL
	case board != nil:
		project, ident, link = board.Project, board.Identifier, board.URL
	}
	col, at := a.linearColumn(m)
	hdr := ui.Row(c).Height(headerH).Shrink(0).AlignItems(ui.Center).Padding(0, 10, 0, pad).Gap(6)
	if a.lin.pane {
		// In a pane, under the pane's own header: a bar of the issue's,
		// which moves no window.
		hdr.Height(38)
	} else {
		hdr.DragWindow()
	}
	hdr.Children(func() {
		if alone {
			if !a.lin.pane {
				a.linearSidebarButtons(c, pal, bar, shown)
			}
			iconButton(c, "linear-back", backIcon, locales.LinearAllIssues(), pal, a.linearClose)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
			if project != "" {
				ui.Icon(c, projectIcon).Size(13, 13).TextColor(pal.textFaint).Shrink(0)
				ui.Text(c, project).FontSize(13).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Shrink(2).MinWidth(0)
				ui.Icon(c, chevronIcon).Size(9, 9).TextColor(pal.textFaint).Shrink(0)
			}
			ui.Text(c, ident).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().FontFeatures("tnum").Shrink(0)
		})
		gap := ui.Box(c).Grow(1).AlignSelf(ui.Stretch)
		if !a.lin.pane {
			gap.DragWindow()
		}
		if at >= 0 && len(col) > 1 {
			ui.Text(c, locales.LinearPosition(itoa(at+1), itoa(len(col)))).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Padding(0, 4)
			stepButton(c, pal, "linear-prev", chevronUpIcon, locales.LinearPrevIssue()+" (K)", at > 0, func() { a.linearStep(m, -1) })
			stepButton(c, pal, "linear-next", chevronDownIcon, locales.LinearNextIssue()+" (J)", at < len(col)-1, func() { a.linearStep(m, 1) })
		}
		if linearSafeURL(link) {
			iconButton(c, "linear-browser", externalIcon, locales.LinearOpenInLinear(), pal, func() { a.openLinearURL(link) })
		}
		if !a.lin.pane {
			iconButton(c, "linear-close", closeIcon, locales.LinearCloseIssue()+" (Esc)", pal, a.linearClose)
			windowControls(c, pal, a.win)
		}
	})
}

// stepButton is an arrow of the detail's header, dimmed at the column's
// ends.
func stepButton(c *ui.Context, pal *palette, key string, svg *ui.SVG, label string, enabled bool, fn func()) {
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(6).Center().TextColor(pal.textMuted).Label(label).Tooltip(label).Disabled(!enabled)
	switch {
	case !enabled:
		b.TextColor(pal.textFaint.Alpha(0.5))
	case b.Pressed():
		b.Background(pal.rowHover.Alpha(0.12))
	case b.Hovered():
		b.Background(pal.rowHover).TextColor(pal.text)
	}
	b.Children(func() { ui.Icon(c, svg).Size(13, 13) })
	if enabled && b.Clicked() {
		fn()
	}
}

// linearSection is a part of the detail, under a heading.
func linearSection(c *ui.Context, pal *palette, title string, count int, fn func()) {
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(12.5).FontWeight(600).TextColor(pal.textMuted)
			if count > 0 {
				ui.Textf(c, "%d", count).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
			}
		})
		fn()
	})
}

// linearProperties is the issue's inspector: its state, which a pop-up
// moves, its project and milestone, its labels, whom it is assigned,
// its branch, and the session on it. Beside the description, on a wide
// card, the rows stand bare, as Linear's sidebar; above it, they are a
// group, as Settings' rows.
func (a *App) linearProperties(c *ui.Context, pal *palette, m *linearModel, d *proto.LinearIssueDetail, live proto.LinearIssue, side bool) {
	group := ui.Column(c).Gap(2)
	if !side {
		colBg, _, _ := linearSurfaces(pal)
		group.Padding(6, 12).Radius(10).Background(colBg)
	}
	group.Children(func() {
		if side {
			ui.Text(c, locales.LinearProperties()).FontSize(12.5).FontWeight(600).TextColor(pal.textMuted).Padding(0, 0, 4, 0)
		}
		propRow(c, pal, locales.LinearStatus(), func() { a.linearStatusPicker(c, pal, m, live) })
		if live.Project != "" {
			propRow(c, pal, locales.LinearProject(), func() {
				ui.Icon(c, projectIcon).Size(13, 13).TextColor(pal.textMuted).Shrink(0)
				ui.Text(c, live.Project).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
		if live.Milestone != "" {
			propRow(c, pal, locales.LinearMilestone(), func() {
				ui.Icon(c, milestoneIcon).Size(12, 12).TextColor(pal.attention).Shrink(0)
				ui.Text(c, live.Milestone).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
		if len(d.Labels) > 0 {
			propRow(c, pal, locales.LinearLabels(), func() {
				ui.Row(c).Gap(4).Wrap().Grow(1).MinWidth(0).Children(func() {
					for _, l := range d.Labels {
						propChip(c, pal, func() {
							ui.Box(c).Size(6, 6).Radius(3).Background(pal.textFaint).Shrink(0)
							ui.Text(c, l).FontSize(11.5).SingleLine()
						})
					}
				})
			})
		}
		if d.Assignee != "" {
			propRow(c, pal, locales.LinearAssignee(), func() {
				avatar(c, d.Assignee, 18)
				ui.Text(c, d.Assignee).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
		if d.BranchName != "" {
			propRow(c, pal, locales.LinearBranch(), func() {
				ui.Text(c, d.BranchName).Font("monospace").FontSize(11.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Tooltip(d.BranchName)
				smallIcon(c, pal, "linear-copy-branch", copyIcon, locales.LinearCopyBranch(), func() { a.copyText = d.BranchName })
			})
		}
	})
	a.linearSession(c, pal, d, live)
}

// propRow is a row of the inspector: a property's name, and its value.
func propRow(c *ui.Context, pal *palette, label string, value func()) {
	ui.Row(c).MinHeight(30).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(pal.textFaint).Width(72).Shrink(0).SingleLine()
		ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(value)
	})
}

// smallIcon is a small quiet button with an icon, as beside a value.
func smallIcon(c *ui.Context, pal *palette, key string, svg *ui.SVG, label string, fn func()) {
	b := ui.ButtonBase(c.Key(key)).Size(22, 22).Radius(5).Center().Shrink(0).TextColor(pal.textFaint).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(pal.rowHover).TextColor(pal.text)
	}
	b.Children(func() { ui.Icon(c, svg).Size(12, 12) })
	if b.Clicked() {
		fn()
	}
}

// linearStatusPicker is the issue's state, as a pop-up that moves it.
// The choice shows at once; the board catches up as the daemon reads,
// and a move Linear refuses puts the state back.
func (a *App) linearStatusPicker(c *ui.Context, pal *palette, m *linearModel, live proto.LinearIssue) {
	if a.lin.statusOf != live.ID {
		a.lin.statusOf, a.lin.statusSel, a.lin.statusSent = live.ID, live.State, ""
	}
	// The move asked for landed, or was asked long enough ago that the
	// board, read since, has the last word.
	if a.lin.statusSent != "" && (live.State == a.lin.statusSent || time.Since(a.lin.statusAt) > 20*time.Second) {
		a.lin.statusSent = ""
	}
	if a.lin.statusSent == "" {
		a.lin.statusSel = live.State
	}
	names := make([]string, 0, len(m.states))
	for _, s := range m.states {
		names = append(names, s.Name)
	}
	if len(names) == 0 {
		names = []string{live.State}
	}
	filterChip(c, "linear-status", &a.lin.statusSel, names, filterIcon, func(s string) string { return s }, func(s string) {
		stateGlyph(c, pal, markOf(m.marks, s, live.StateType, pal), 13)
	}, pal).Margin(0, 0, 0, -8)
	// What the issue is moving to: the last move asked for, or where it
	// is. A choice that differs is sent, even back to where the issue
	// was, which undoes a move still on its way.
	target := live.State
	if a.lin.statusSent != "" {
		target = a.lin.statusSent
	}
	if sel := a.lin.statusSel; sel != target {
		a.lin.statusSent, a.lin.statusAt = sel, time.Now()
		id, ident, st := live.ID, live.Identifier, a.lin
		a.call("linear.setState", proto.LinearParams{Issue: id, State: sel}, func(err error) {
			if err != nil {
				a.toast(err.Error())
				if st.statusOf == id && st.statusSent == sel {
					st.statusSent = ""
				}
				return
			}
			a.toast(locales.LinearMoved(ident, sel))
		})
	}
}

// linearSession is every conversation that worked on the issue, the
// latest first: its harness, its own session and where it ran, which
// resumes it in a new session there, or opens the session still running
// it; and, when no session works on the issue now, the way to link one.
func (a *App) linearSession(c *ui.Context, pal *palette, d *proto.LinearIssueDetail, live proto.LinearIssue) {
	colBg, _, _ := linearSurfaces(pal)
	ui.Column(c).Gap(10).Padding(12, 14).Radius(10).Background(colBg).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.LinearSessions()).FontSize(12.5).FontWeight(600).TextColor(pal.textMuted)
			if len(d.Runs) > 0 {
				ui.Textf(c, "%d", len(d.Runs)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
			}
		})
		if len(d.Runs) == 0 && live.Session == "" {
			ui.Text(c, locales.LinearNoSession()).FontSize(12).TextColor(pal.textFaint).LineHeight(1.35)
		}
		for i, r := range d.Runs {
			if i > 0 {
				ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
			}
			a.linearRun(c, pal, d, live, r, i)
		}
		if live.Session != "" {
			ui.Row(c).Gap(8).Wrap().Children(func() {
				if len(d.Runs) == 0 && button(c, "linear-open-session", locales.LinearOpenSession(), "", true, pal) {
					// The issue opens the session where it needs the
					// user, its attention pane.
					a.focusAttentionOf(live.Session)
					a.linearClose()
				}
				if button(c, "linear-unlink", locales.LinearUnlink(), "", false, pal) {
					st := a.lin
					a.call("session.link", proto.LinkParams{Session: live.Session}, func(err error) {
						if err != nil {
							a.toast(err.Error())
							return
						}
						a.withLin(st, func() {
							if a.lin.open == d.ID {
								a.linearOpen(d.ID)
							}
						})
					})
				}
			})
			return
		}
		ids := []string{""}
		for _, s := range a.st.Sessions {
			ids = append(ids, s.ID)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			popup(c, "linear-link-session", &a.lin.linkSession, ids, func(id string) string {
				if s := a.session(id); s != nil {
					return s.DisplayName(a.set.SessionName)
				}
				return locales.LinearChooseSession()
			}, 190, pal).Shrink(1).MinWidth(0)
			if a.lin.linkSession != "" && button(c, "linear-link", locales.LinearLinkSession(), "", true, pal) {
				st := a.lin
				a.call("session.link", proto.LinkParams{Session: a.lin.linkSession, Task: d.Identifier}, func(err error) {
					if err != nil {
						a.toast(err.Error())
						return
					}
					a.withLin(st, func() {
						a.lin.linkSession = ""
						if a.lin.open == d.ID {
							a.linearOpen(d.ID)
						}
					})
				})
			}
		})
	})
}

// linearRun is one conversation on the issue: the harness and the
// session it ran in, its own id, when, and how the task was linked; and
// Open while its session runs it, or Resume, which starts it again in a
// new session where it ran.
func (a *App) linearRun(c *ui.Context, pal *palette, d *proto.LinearIssueDetail, live proto.LinearIssue, r proto.TaskRun, i int) {
	key := fmt.Sprintf("run-%d-%s", i, r.ID)
	running := r.Ended == 0 && a.session(r.Session) != nil
	resume := proto.ResumeCommand(r.Agent, r.ID)
	ui.Column(c.Key(key)).Gap(4).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if running && r.Session == live.Session {
				statusIconP(c, pal, live.SessionStatus, false, live.Progress, 0, 12)
			} else {
				dot := pal.textFaint.Alpha(0.6)
				if running {
					dot = pal.done
				}
				ui.Box(c).Size(12, 12).Center().Shrink(0).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(dot)
				})
			}
			ui.Text(c, agentTitle(r.Agent)).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
			if r.SessionName != "" {
				ui.Text(c, r.SessionName).FontSize(12.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			} else {
				ui.Box(c).Grow(1)
			}
			switch {
			case running:
				if miniButton(c, pal, key+"-open", locales.LinearOpenRun()) {
					a.focusAttentionOf(r.Session)
					a.linearClose()
				}
			case resume != nil:
				if miniButton(c, pal, key+"-resume", locales.LinearResume()) {
					a.resumeRun(r, d.Identifier)
				}
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, 0, 0, 20).Children(func() {
			when := locales.LinearRunStarted(ago(r.Started, time.Now()))
			if r.Ended != 0 {
				when = locales.LinearRunEnded(ago(r.Ended, time.Now()))
			}
			ui.Text(c, when).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Shrink(0)
			if via := viaWord(r.Via); via != "" {
				ui.Text(c, "· "+via).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Shrink(0)
			}
			if r.ID != "" {
				ui.Text(c, r.ID).Font("monospace").FontSize(11).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Tooltip(r.ID)
				smallIcon(c, pal, key+"-copy", copyIcon, locales.LinearCopyResume(), func() {
					a.copyText = r.ID
					a.toast(locales.LinearResumeCopied())
				})
			}
		})
	})
}

// resumeRun starts a conversation again in a new session, where it ran,
// linked to the task from the start: Claude Code finds a conversation
// only from the directory it began in.
func (a *App) resumeRun(r proto.TaskRun, identifier string) {
	p, ok := a.resumeParams(r, identifier)
	if !ok || a.cl == nil {
		return
	}
	cl, st := a.cl, a.lin
	go func() {
		var s proto.Session
		err := cl.Call("create", p, &s)
		a.update(func() {
			if err != nil {
				a.toast(err.Error())
				return
			}
			a.addPending(s)
			a.selectSession(s.ID)
			if st == &a.linRoot {
				a.withLin(st, a.linearClose)
			}
		})
	}()
}

// resumeParams are the new session's that resumes r: its agent's
// command, where it ran, in the project of the session it ran in while
// that one is open, linked to the task.
func (a *App) resumeParams(r proto.TaskRun, identifier string) (proto.CreateParams, bool) {
	cmd := proto.ResumeCommand(r.Agent, r.ID)
	if cmd == nil {
		return proto.CreateParams{}, false
	}
	p := proto.CreateParams{Dir: r.Dir, Command: cmd, Name: strings.ToLower(identifier) + "-" + r.Agent, Task: identifier}
	if s := a.session(r.Session); s != nil {
		p.Project = s.Project
	}
	if cur := a.session(a.selected); cur != nil {
		p.Cols, p.Rows = cur.Cols, cur.Rows
	}
	return p, true
}

// agentTitle is a harness's name, as its makers write it.
func agentTitle(agent string) string {
	switch agent {
	case "":
		return locales.LinearNoAgent()
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "pi":
		return "Pi"
	case "gemini":
		return "Gemini CLI"
	case "amp":
		return "Amp"
	case "cursor-agent":
		return "Cursor"
	case "copilot":
		return "Copilot"
	}
	return strings.ToUpper(agent[:1]) + agent[1:]
}

// viaWord says how a task was linked, when not by a person.
func viaWord(via string) string {
	switch via {
	case "task":
		return locales.LinearViaAgent()
	case "branch":
		return locales.LinearViaBranch()
	}
	return ""
}

// miniButton is a small bordered button, as in a row of a list.
func miniButton(c *ui.Context, pal *palette, key, label string) bool {
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 10).Radius(6).AlignItems(ui.Center).Shrink(0).Label(label)
	bg := pal.control
	switch {
	case b.Pressed():
		bg = bg.Mix(ui.Hex("#000000"), 0.08)
	case b.Hovered():
		bg = pal.controlHover
	}
	face(b, pal, bg).TextColor(pal.text)
	b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine() })
	return b.Clicked()
}

// linearIssueRow is a row naming another issue — a sub-issue, the
// parent, a related one — which opens it.
func (a *App) linearIssueRow(c *ui.Context, pal *palette, m *linearModel, key, prefix, ident, title, state string) {
	row := ui.ButtonBase(c.Key(key)).Height(30).Gap(8).Padding(0, 8).Margin(0, -8).Radius(7).AlignItems(ui.Center).Label(ident + " " + title)
	if row.Hovered() {
		row.Background(pal.rowHover)
	}
	if row.Clicked() {
		id := ident
		if is := a.linearIssue(ident); is != nil {
			id = is.ID
		}
		a.linearOpen(id)
	}
	row.Children(func() {
		if prefix != "" {
			ui.Text(c, prefix).FontSize(11.5).TextColor(pal.textFaint).Width(66).Shrink(0).SingleLine()
		}
		stateGlyph(c, pal, markOf(m.marks, state, "", pal), 13)
		ui.Text(c, ident).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum").Shrink(0).SingleLine()
		ui.Text(c, title).FontSize(12.5).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
	})
}

// linearSubIssues are the issue's parent and its sub-issues.
func (a *App) linearSubIssues(c *ui.Context, pal *palette, m *linearModel, d *proto.LinearIssueDetail) {
	if d.Parent != "" {
		linearSection(c, pal, locales.LinearParent(), 0, func() {
			title, state := "", ""
			if is := a.linearIssue(d.Parent); is != nil {
				title, state = is.Title, is.State
			}
			a.linearIssueRow(c, pal, m, "parent", "", d.Parent, title, state)
		})
	}
	if len(d.SubIssues) > 0 {
		linearSection(c, pal, locales.LinearSubIssues(), len(d.SubIssues), func() {
			for _, sub := range d.SubIssues {
				a.linearIssueRow(c, pal, m, "sub-"+sub.ID, "", sub.Identifier, sub.Title, sub.State)
			}
		})
	}
}

// linearResources are the files and links on the issue, as the task's
// prompt.
func (a *App) linearResources(c *ui.Context, pal *palette, d *proto.LinearIssueDetail) {
	if len(d.Attachments) == 0 {
		return
	}
	linearSection(c, pal, locales.LinearResources(), len(d.Attachments), func() {
		for _, at := range d.Attachments {
			row := ui.ButtonBase(c.Key("res-"+at.ID)).MinHeight(32).Gap(9).Padding(5, 8).Margin(0, -8).Radius(7).AlignItems(ui.Center).Label(at.Title)
			hovered := row.Hovered()
			if hovered {
				row.Background(pal.rowHover)
			}
			if at.URL != "" {
				row.Tooltip(at.URL)
			}
			if row.Clicked() {
				a.openLinearURL(at.URL)
			}
			row.Children(func() {
				mark := attachIcon
				if strings.HasSuffix(strings.ToLower(at.URL), ".md") || strings.Contains(strings.ToLower(at.Title), "prompt") {
					mark = docIcon
				}
				ui.Icon(c, mark).Size(14, 14).TextColor(pal.textMuted).Shrink(0)
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, at.Title).FontSize(12.5).TextColor(pal.text).SingleLine().Ellipsis("…")
					if at.Subtitle != "" {
						ui.Text(c, at.Subtitle).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…")
					}
				})
				ext := ui.Icon(c, externalIcon).Size(12, 12).TextColor(pal.textFaint).Shrink(0)
				if !hovered {
					ext.Opacity(0)
				}
			})
		}
	})
}

// linearRelations are the issues related to this one: what blocks it,
// what it blocks, the others.
func (a *App) linearRelations(c *ui.Context, pal *palette, m *linearModel, d *proto.LinearIssueDetail) {
	if len(d.Relations) == 0 {
		return
	}
	linearSection(c, pal, locales.LinearRelated(), len(d.Relations), func() {
		for n, r := range d.Relations {
			a.linearIssueRow(c, pal, m, "rel-"+itoa(n), relationWord(r.Type), r.Identifier, r.Title, r.State)
		}
	})
}

// relationWord is what a relation does, in a word or two.
func relationWord(typ string) string {
	switch typ {
	case "blocks":
		return "Blocks"
	case "blockedBy":
		return "Blocked by"
	case "duplicate":
		return "Duplicate"
	}
	return "Related"
}

// linearActivity is what was said under the issue: each comment a card,
// with who said it and when.
func (a *App) linearActivity(c *ui.Context, pal *palette, d *proto.LinearIssueDetail) {
	if len(d.Comments) == 0 {
		return
	}
	colBg, _, _ := linearSurfaces(pal)
	ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
	linearSection(c, pal, locales.LinearActivity(), len(d.Comments), func() {
		ui.Column(c).Gap(10).Padding(4, 0, 0, 0).Children(func() {
			for _, cm := range d.Comments {
				author := cm.Author
				if author == "" {
					author = "Linear"
				}
				ui.Column(c.Key("cm-"+cm.ID)).Gap(8).Padding(12, 14, 12, 14).Radius(10).Background(colBg).
					InsetShadow(0, 0, 0, 1, pal.text.Alpha(0.05)).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						avatar(c, author, 20)
						ui.Text(c, author).FontSize(12.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
						ui.Text(c, ago(cm.CreatedAt, time.Now())).FontSize(11.5).TextColor(pal.textFaint).SingleLine()
					})
					ui.Column(c).Children(func() {
						markdown.View(c, linearMarkdown(cm.Body), &markdown.Options{OnLink: a.openLinearURL})
					})
				})
			}
		})
	})
}
