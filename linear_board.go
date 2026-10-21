package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// The board and the list: the issues by state, as columns of cards or
// as groups of rows. Each issue opens in the detail on a click, and has
// its menu on a right click: open it, copy it, move it to another state.

// linearSurfaces are the board's grounds: the columns' faint fill, and
// the cards on it, white on gray in the light, a lighter gray on the
// card's in the dark.
func linearSurfaces(pal *palette) (column, card, cardHover ui.Color) {
	if pal.dark {
		return ui.RGBA(255, 255, 255, 0.025), ui.RGBA(255, 255, 255, 0.055), ui.RGBA(255, 255, 255, 0.085)
	}
	return ui.RGBA(20, 20, 50, 0.035), ui.Hex("#ffffff"), ui.Hex("#ffffff")
}

// linearShownStates are the states the board shows as columns, and those
// it keeps in the rail of hidden columns, as the setting says: a state
// with no issues is a column, a row of the rail, or nothing.
func (a *App) linearShownStates(m *linearModel) (shown, hidden []proto.LinearWorkflowState) {
	for _, s := range m.states {
		switch {
		case len(m.byState[s.Name]) > 0:
			shown = append(shown, s)
		case a.lin.state != "" && s.Name != a.lin.state:
			// Filtered out: neither a column nor hidden, but gone.
		case a.set.LinearEmptyColumns == "shown":
			shown = append(shown, s)
		case a.set.LinearEmptyColumns != "hidden":
			hidden = append(hidden, s)
		}
	}
	return shown, hidden
}

// linearBoard is the issues as columns, one per state, in the order the
// team puts them in, with the states no issue is in in a rail at the end.
func (a *App) linearBoard(c *ui.Context, pal *palette, m *linearModel) {
	lin := a.st.Linear
	if lin == nil || (lin.Status != proto.LinearOK && len(lin.Issues) == 0) {
		a.linearNothingAction(c, pal, locales.LinearNotRead(), a.linearStatusLabel(nil))
		return
	}
	shown, hidden := a.linearShownStates(m)
	if len(m.issues) == 0 && a.set.LinearEmptyColumns != "shown" {
		a.linearNothingAction(c, pal, locales.LinearNone(), a.linearEmptyDetail())
		return
	}
	colBg, _, _ := linearSurfaces(pal)
	// The board is a row of columns that scrolls sideways: each column
	// is as tall as the board, and its cards scroll within it.
	ui.ScrollHorizontal(c.Key("linear-board")).Grow(1).MinHeight(0).Gap(10).Padding(4, 12, 12, 12).Children(func() {
		for _, s := range shown {
			col := m.byState[s.Name]
			mark := markOf(m.marks, s.Name, s.Type, pal)
			ui.Column(c.Key("col-" + s.ID)).Width(linearColW).FillHeight().Shrink(0).Radius(12).Background(colBg).Children(func() {
				ui.Row(c).Height(38).Shrink(0).AlignItems(ui.Center).Gap(8).Padding(0, 12).Children(func() {
					stateGlyph(c, pal, mark, 14)
					ui.Text(c, s.Name).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine()
					ui.Textf(c, "%d", len(col)).FontSize(13).TextColor(pal.textFaint).FontFeatures("tnum")
				})
				ui.Scroll(c.Key("linear-col-"+s.ID)).Grow(1).MinHeight(0).Gap(6).Padding(0, 6, 6, 6).Children(func() {
					for _, i := range col {
						a.linearCard(c, pal, m, i)
					}
				})
			})
		}
		if len(hidden) > 0 {
			a.linearRail(c, pal, m, hidden)
		}
	})
}

// linearRail is the states no issue is in, folded into a list at the
// board's end, as Linear's hidden columns.
func (a *App) linearRail(c *ui.Context, pal *palette, m *linearModel, hidden []proto.LinearWorkflowState) {
	colBg, _, _ := linearSurfaces(pal)
	ui.Column(c.Key("linear-rail")).Width(linearRailW).Shrink(0).Gap(6).AlignSelf(ui.Start).Children(func() {
		head := ui.ButtonBase(c.Key("rail-head")).Height(38).Padding(0, 8).Gap(6).Radius(8).AlignItems(ui.Center).
			Label(locales.LinearHiddenColumns())
		if head.Hovered() {
			head.Background(pal.rowHover)
		}
		if head.Clicked() {
			a.lin.railShut = !a.lin.railShut
		}
		head.Children(func() {
			chev := chevronDownIcon
			if a.lin.railShut {
				chev = chevronIcon
			}
			ui.Icon(c, chev).Size(10, 10).TextColor(pal.textFaint)
			ui.Text(c, locales.LinearHiddenColumns()).FontSize(12.5).FontWeight(600).TextColor(pal.textMuted).SingleLine()
			ui.Spacer(c)
			ui.Textf(c, "%d", len(hidden)).FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
		})
		if a.lin.railShut {
			return
		}
		for _, s := range hidden {
			ui.Row(c.Key("rail-"+s.ID)).Height(34).Shrink(0).Padding(0, 12).Gap(8).Radius(9).AlignItems(ui.Center).Background(colBg).Children(func() {
				stateGlyph(c, pal, markOf(m.marks, s.Name, s.Type, pal), 13)
				ui.Text(c, s.Name).FontSize(12.5).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				ui.Text(c, "0").FontSize(12).TextColor(pal.textFaint).FontFeatures("tnum")
			})
		}
	})
}

// linearCard is one issue on the board: its identifier and the session
// on it, its state and title, what it has as chips, and when it changed.
// Compact, it is the identifier, the state and the title.
func (a *App) linearCard(c *ui.Context, pal *palette, m *linearModel, i proto.LinearIssue) {
	_, bg, hoverBg := linearSurfaces(pal)
	open := a.lin.open == i.ID
	compact := a.set.LinearDensity == "compact"
	card := ui.ButtonBase(c.Key("card-" + i.ID)).Column().AlignItems(ui.Stretch).Shrink(0).Gap(7).Radius(9).
		Label(i.Identifier + " " + i.Title)
	if compact {
		card.Padding(9, 11).Gap(5)
	} else {
		card.Padding(11, 12, 10, 12)
	}
	if card.Hovered() {
		bg = hoverBg
	}
	raised(card.Background(card.AnimateColor("bg", bg, hover)), pal)
	if open {
		card.InsetShadow(0, 0, 0, 1.5, pal.accent.Alpha(0.85))
	}
	if card.Clicked() {
		a.linearOpen(i.ID)
	}
	card.ContextMenu(func(menu *ui.Menu) { a.linearIssueMenu(menu, m, i) })
	card.Children(func() {
		ui.Row(c).Height(16).Shrink(0).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, i.Identifier).FontSize(11.5).FontWeight(500).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine()
			ui.Spacer(c)
			if i.Session != "" {
				linearSessionBadge(c, pal, i)
			}
		})
		ui.Row(c).AlignItems(ui.Start).Gap(7).Children(func() {
			// The glyph sits on the title's first line.
			ui.Box(c).Height(18).Center().Shrink(0).Children(func() {
				stateGlyph(c, pal, markOf(m.marks, i.State, i.StateType, pal), 13)
			})
			title := ui.Text(c, i.Title).FontSize(13).FontWeight(500).TextColor(pal.text).LineHeight(1.38).Grow(1).MinWidth(0)
			if compact {
				title.MaxLines(2).Ellipsis("…")
			}
		})
		if compact {
			return
		}
		a.linearChips(c, pal, i)
		if when := ago(i.UpdatedAt, time.Now()); when != "" {
			ui.Text(c, locales.LinearUpdated(when)).FontSize(11.5).TextColor(pal.textFaint).SingleLine()
		}
	})
}

// linearChips are what an issue has, as chips: what blocks it, what it
// blocks, its sub-issues, its milestone while the board shows more than
// one, and its labels. None when it has none.
func (a *App) linearChips(c *ui.Context, pal *palette, i proto.LinearIssue) {
	milestone := i.Milestone != "" && a.lin.milestone == ""
	if i.Blocked == 0 && i.Blocking == 0 && i.Children == 0 && !milestone && len(i.Labels) == 0 {
		return
	}
	ui.Row(c).Gap(5).Wrap().AlignItems(ui.Center).Children(func() {
		if i.Blocked > 0 {
			propChip(c, pal, func() {
				blockedGlyph(c, pal, 11)
				ui.Textf(c, "%d", i.Blocked).FontSize(11).FontFeatures("tnum")
			}).Tooltip(locales.LinearBlockedBy(itoa(i.Blocked)))
		}
		if i.Blocking > 0 {
			propChip(c, pal, func() {
				ui.Icon(c, blockingIcon).Size(11, 11).TextColor(pal.attention)
				ui.Textf(c, "%d", i.Blocking).FontSize(11).FontFeatures("tnum")
			}).Tooltip(locales.LinearBlocking(itoa(i.Blocking)))
		}
		if i.Children > 0 {
			propChip(c, pal, func() {
				ui.Icon(c, subIssueIcon).Size(11, 11)
				ui.Textf(c, "%d", i.Children).FontSize(11).FontFeatures("tnum")
			}).Tooltip(locales.LinearSubIssueCount(itoa(i.Children)))
		}
		if milestone {
			propChip(c, pal, func() {
				ui.Icon(c, milestoneIcon).Size(10, 10)
				ui.Text(c, i.Milestone).FontSize(11).SingleLine().Ellipsis("…").MaxWidth(150)
			})
		}
		for _, l := range i.Labels {
			propChip(c, pal, func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(pal.textFaint).Shrink(0)
				ui.Text(c, l).FontSize(11).SingleLine().Ellipsis("…").MaxWidth(110)
			})
		}
	})
}

// linearSessionBadge is the mark of the session working on an issue: a
// dot of its status, and the name it goes by.
func linearSessionBadge(c *ui.Context, pal *palette, i proto.LinearIssue) {
	row := ui.Row(c).Height(18).Shrink(1).MinWidth(0).Gap(5).Padding(0, 6, 0, 4).Radius(9).AlignItems(ui.Center).Background(pal.chip).Children(func() {
		statusIconP(c, pal, i.SessionStatus, false, i.Progress, 10)
		ui.Text(c, i.SessionName).FontSize(11).TextColor(pal.textMuted).SingleLine().MaxWidth(110).Ellipsis("…")
	})
	row.Tooltip(locales.LinearSessionTip())
}

// linearIssueMenu is an issue's menu, on its card or its row.
func (a *App) linearIssueMenu(menu *ui.Menu, m *linearModel, i proto.LinearIssue) {
	if menu.Item(locales.LinearOpenIssue()).Chosen() {
		a.linearOpen(i.ID)
	}
	if menu.Item(locales.LinearOpenInLinear()).Disabled(!linearSafeURL(i.URL)).Chosen() {
		a.openLinearURL(i.URL)
	}
	menu.Separator()
	if menu.Item(locales.LinearCopyId()).Chosen() {
		a.copyText = i.Identifier
	}
	if menu.Item(locales.LinearCopyLink()).Disabled(i.URL == "").Chosen() {
		a.copyText = i.URL
	}
	menu.Separator()
	menu.Submenu(locales.LinearMoveTo(), func(sub *ui.Menu) {
		for _, s := range m.states {
			if sub.Item(s.Name).Checked(s.Name == i.State).Disabled(s.Name == i.State).Chosen() {
				id, ident, state := i.ID, i.Identifier, s.Name
				a.defer_(func() { a.linearMoveTo(id, ident, state) })
			}
		}
	})
}

// openLinearURL opens a link of the workspace's in the browser, when it
// is a web link.
func (a *App) openLinearURL(link string) {
	if linearSafeURL(link) {
		a.lin.openURL = link
	}
}

// linearList is the issues as rows, grouped by state as the board's
// columns are.
func (a *App) linearList(c *ui.Context, pal *palette, m *linearModel) {
	if len(m.issues) == 0 {
		a.linearNothingAction(c, pal, locales.LinearNone(), a.linearEmptyDetail())
		return
	}
	colBg, _, _ := linearSurfaces(pal)
	ui.Scroll(c.Key("linear-list")).Grow(1).MinHeight(0).Padding(4, 12, 14, 12).Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).Gap(2).Children(func() {
			for _, s := range m.states {
				rows := m.byState[s.Name]
				if len(rows) == 0 {
					continue
				}
				ui.Row(c.Key("group-"+s.ID)).Height(34).Shrink(0).Gap(8).Padding(0, 12).Margin(6, 0, 2, 0).Radius(8).AlignItems(ui.Center).Background(colBg).Children(func() {
					stateGlyph(c, pal, markOf(m.marks, s.Name, s.Type, pal), 13)
					ui.Text(c, s.Name).FontSize(12.5).FontWeight(600).TextColor(pal.text)
					ui.Textf(c, "%d", len(rows)).FontSize(12.5).TextColor(pal.textFaint).FontFeatures("tnum")
				})
				for _, i := range rows {
					a.linearRow(c, pal, m, i)
				}
			}
		})
	})
}

// linearRow is one issue in the list.
func (a *App) linearRow(c *ui.Context, pal *palette, m *linearModel, i proto.LinearIssue) {
	open := a.lin.open == i.ID
	row := ui.ButtonBase(c.Key("row-"+i.ID)).Height(38).Shrink(0).AlignItems(ui.Center).Gap(10).Padding(0, 12).Radius(8).
		Label(i.Identifier + " " + i.Title)
	if open {
		row.Background(pal.rowSelected)
		chosen(row, pal)
	} else if row.Hovered() {
		row.Background(pal.rowHover)
	}
	if row.Clicked() {
		a.linearOpen(i.ID)
	}
	row.ContextMenu(func(menu *ui.Menu) { a.linearIssueMenu(menu, m, i) })
	row.Children(func() {
		ui.Text(c, i.Identifier).FontSize(12).FontWeight(500).TextColor(pal.textFaint).FontFeatures("tnum").Width(58).Shrink(0).SingleLine()
		stateGlyph(c, pal, markOf(m.marks, i.State, i.StateType, pal), 13)
		ui.Text(c, i.Title).FontSize(13).FontWeight(500).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if i.Blocked > 0 {
			blockedGlyph(c, pal, 11).Tooltip(locales.LinearBlockedBy(itoa(i.Blocked)))
		}
		if i.Session != "" {
			linearSessionBadge(c, pal, i)
		}
		if i.Milestone != "" && a.lin.milestone == "" {
			ui.Row(c).Gap(4).Shrink(1).MinWidth(0).AlignItems(ui.Center).TextColor(pal.textFaint).Children(func() {
				ui.Icon(c, milestoneIcon).Size(10, 10).Shrink(0)
				ui.Text(c, i.Milestone).FontSize(11.5).SingleLine().MaxWidth(150).Ellipsis("…")
			})
		}
		ui.Text(c, ago(i.UpdatedAt, time.Now())).FontSize(11.5).TextColor(pal.textFaint).Width(30).TextAlign(ui.End).Shrink(0).SingleLine()
	})
}
