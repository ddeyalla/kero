package main

import (
	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
	"kero/locales"
)

// pinnedSessionsFirst keeps the daemon's order within each section. The
// same groups feed the sidebar, the palette and keyboard navigation.
func (a *App) pinnedSessionsFirst(ss []*proto.Session) []*proto.Session {
	if len(a.prefs.SessionPins) == 0 {
		return ss
	}
	out := make([]*proto.Session, 0, len(ss))
	for _, pinned := range []bool{true, false} {
		for _, s := range ss {
			if a.prefs.SessionPins[s.ID] == pinned {
				out = append(out, s)
			}
		}
	}
	return out
}

func (a *App) setSessionPin(id string, pinned bool) {
	if a.session(id) == nil || a.prefs.SessionPins[id] == pinned {
		return
	}
	if a.prefs.SessionPins == nil {
		a.prefs.SessionPins = map[string]bool{}
	}
	if pinned {
		a.prefs.SessionPins[id] = true
	} else {
		delete(a.prefs.SessionPins, id)
	}
	a.savePrefs()
}

// pruneSessionPins drops sessions seen to disappear from this daemon.
// Startup restoration is incremental: an id absent from its first state
// may still return, so never prune pins merely missing on connection.
func (a *App) pruneSessionPins(st proto.State) {
	if !a.loaded || a.st.Started != st.Started || len(a.prefs.SessionPins) == 0 {
		return
	}
	live := make(map[string]bool, len(st.Sessions))
	for _, s := range st.Sessions {
		live[s.ID] = true
	}
	changed := false
	for _, s := range a.st.Sessions {
		if !live[s.ID] && a.prefs.SessionPins[s.ID] {
			delete(a.prefs.SessionPins, s.ID)
			changed = true
		}
	}
	if changed {
		a.savePrefs()
	}
}

// sessionNeighbor finds the adjacent session in the same project and pin
// section. Reordering never changes project membership or pin state.
func (a *App) sessionNeighbor(id string, offset int) string {
	s := a.session(id)
	if s == nil {
		return ""
	}
	for _, g := range a.groups() {
		for i, other := range g.sessions {
			j := i + offset
			if other.ID == id && j >= 0 && j < len(g.sessions) && a.prefs.SessionPins[g.sessions[j].ID] == a.prefs.SessionPins[id] {
				return g.sessions[j].ID
			}
		}
	}
	return ""
}

// sessionMove translates a visible drop to the existing RPC's project
// index, counting after the source has been removed. Other projects and
// the other pin section keep their relative order. Project is omitted so
// the daemon preserves membership even if another client moved the session.
func (a *App) sessionMove(id, target string, after bool) (proto.MoveParams, bool) {
	s, to := a.session(id), a.session(target)
	if s == nil || to == nil || id == target || s.Project != to.Project || a.prefs.SessionPins[id] != a.prefs.SessionPins[target] {
		return proto.MoveParams{}, false
	}
	if (!after && a.sessionNeighbor(id, 1) == target) || (after && a.sessionNeighbor(id, -1) == target) {
		return proto.MoveParams{}, false
	}
	index := 0
	for _, other := range a.st.Sessions {
		if other.ID == id || other.Project != s.Project {
			continue
		}
		if other.ID == target {
			if after {
				index++
			}
			return proto.MoveParams{ID: id, Index: index}, true
		}
		index++
	}
	return proto.MoveParams{}, false
}

func (a *App) reorderSession(id, target string, after bool) {
	p, ok := a.sessionMove(id, target, after)
	if !ok {
		return
	}
	if a.cl == nil || a.conn != connReady {
		a.toast(locales.SessionOrderUnavailable())
		return
	}
	// Wait for the daemon's state: a failed write must not leave a local
	// order that appears saved. The existing call path reports its error.
	a.call("move", p, nil)
}

func (a *App) sessionOrderAction(action string, args map[string]string) {
	id := args["session"]
	if id == "" {
		id = a.selected
	}
	if a.session(id) == nil {
		a.toast(locales.SessionOrderMissing())
		return
	}
	switch action {
	case "session.pin":
		a.setSessionPin(id, true)
	case "session.unpin":
		a.setSessionPin(id, false)
	case "session.moveUp":
		a.reorderSession(id, a.sessionNeighbor(id, -1), false)
	case "session.moveDown":
		a.reorderSession(id, a.sessionNeighbor(id, 1), true)
	}
}

func (a *App) sessionOrderMenu(m *ui.Menu, s *proto.Session) {
	label := locales.SessionPin()
	if a.prefs.SessionPins[s.ID] {
		label = locales.SessionUnpin()
	}
	if m.Item(label).Chosen() {
		id, pinned := s.ID, !a.prefs.SessionPins[s.ID]
		a.defer_(func() { a.setSessionPin(id, pinned) })
	}
	for _, offset := range []int{-1, 1} {
		label := locales.SessionMoveUp()
		if offset > 0 {
			label = locales.SessionMoveDown()
		}
		target := a.sessionNeighbor(s.ID, offset)
		if m.Item(label).Disabled(target == "" || a.conn != connReady).Chosen() {
			id, after := s.ID, offset > 0
			a.defer_(func() { a.reorderSession(id, target, after) })
		}
	}
}

type sessionDrag struct{ id string }

func (a *App) sessionDrag(c *ui.Context, pal *palette, row ui.Element, s *proto.Session) (over bool) {
	if a.renaming == s.ID || a.conn != connReady {
		return false
	}
	row.Drag(sessionDrag{s.ID}).Cursor(ui.CursorGrab)
	sidebarDragSource(c, row, pal)
	if d, hovering := ui.DragOver[sessionDrag](row); hovering {
		after := sidebarDropAfter(row)
		if _, ok := a.sessionMove(d.id, s.ID, after); ok {
			sidebarInsertion(row, pal, after)
			over = true
		}
	}
	if d, dropped := ui.Drop[sessionDrag](row); dropped {
		id, target, after := d.id, s.ID, sidebarDropAfter(row)
		a.defer_(func() { a.reorderSession(id, target, after) })
	}
	return over
}
