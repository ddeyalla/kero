package main

import (
	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
	"kero/locales"
)

// projectNeighbor follows the daemon's saved order. The synthetic Other
// group has no project id and cannot be moved.
func (a *App) projectNeighbor(id string, offset int) string {
	if id == "" {
		return ""
	}
	for i, p := range a.st.Projects {
		j := i + offset
		if p.ID == id && j >= 0 && j < len(a.st.Projects) {
			return a.st.Projects[j].ID
		}
	}
	return ""
}

// projectMove counts the destination after removing the source, matching
// project.move. Session membership and session order remain unchanged.
func (a *App) projectMove(id, target string, after bool) (proto.MoveProjectParams, bool) {
	if id == "" || target == "" || id == target || a.project(id) == nil || a.project(target) == nil {
		return proto.MoveProjectParams{}, false
	}
	if (!after && a.projectNeighbor(id, 1) == target) || (after && a.projectNeighbor(id, -1) == target) {
		return proto.MoveProjectParams{}, false
	}
	index := 0
	for _, p := range a.st.Projects {
		if p.ID == id {
			continue
		}
		if p.ID == target {
			if after {
				index++
			}
			return proto.MoveProjectParams{ID: id, Index: index}, true
		}
		index++
	}
	return proto.MoveProjectParams{}, false
}

func (a *App) reorderProject(id, target string, after bool) {
	p, ok := a.projectMove(id, target, after)
	if !ok {
		return
	}
	if a.cl == nil || a.conn != connReady {
		a.toast(locales.SessionOrderUnavailable())
		return
	}
	// The daemon broadcasts the saved order; never imply success locally.
	a.call("project.move", p, nil)
}

func (a *App) projectOrderAction(action string, args map[string]string) {
	id := args["project"]
	if id == "" {
		if s := a.session(a.selected); s != nil {
			id = s.Project
		}
	}
	if id == "" || a.project(id) == nil {
		a.toast(locales.ProjectOrderMissing())
		return
	}
	switch action {
	case "project.moveUp":
		a.reorderProject(id, a.projectNeighbor(id, -1), false)
	case "project.moveDown":
		a.reorderProject(id, a.projectNeighbor(id, 1), true)
	}
}

func (a *App) projectOrderMenu(m *ui.Menu, p *proto.Project) {
	if p == nil || p.ID == "" {
		return
	}
	for _, offset := range []int{-1, 1} {
		label := locales.ProjectMoveUp()
		if offset > 0 {
			label = locales.ProjectMoveDown()
		}
		target := a.projectNeighbor(p.ID, offset)
		if m.Item(label).Disabled(target == "" || a.conn != connReady).Chosen() {
			id, after := p.ID, offset > 0
			a.defer_(func() { a.reorderProject(id, target, after) })
		}
	}
}
