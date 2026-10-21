package main

import (
	"cmp"
	"slices"

	"kero/internal/proto"

	"kero/locales"
)

// Navigating between what sessions need of the user. A session says it in
// one state, folded by kerod from its panes (proto.Attention); the pane
// that showed it is what the user goes to.

// attentionWaitingUser is the state that asks for the user, which
// `attention.next-blocked` cycles.
const attentionWaitingUser = "waiting-user"

// blockedSessions returns the sessions waiting on the user, in the order
// `attention.next-blocked` visits them: the longest wait first, and of two
// waiting as long, by the session's id, so that a press never depends on
// the order kerod happened to send its sessions in.
func (a *App) blockedSessions() []*proto.Session {
	var out []*proto.Session
	for i := range a.st.Sessions {
		s := &a.st.Sessions[i]
		if s.Attention != nil && s.Attention.State == attentionWaitingUser {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(x, y *proto.Session) int {
		return cmp.Or(cmp.Compare(x.Attention.Since, y.Attention.Since), cmp.Compare(x.ID, y.ID))
	})
	return out
}

// nextBlocked goes to the next session waiting on the user: after the
// session shown when that one waits too, else after the one it went to
// last, else the longest wait. With none waiting it says so and goes
// nowhere.
//
// The place in the cycle is kept in attentionLast as well, so that looking
// at a session that does not wait, between two presses, does not start the
// cycle over.
func (a *App) nextBlocked() {
	blocked := a.blockedSessions()
	if len(blocked) == 0 {
		a.toast(locales.ToastNothingNeedsYou())
		return
	}
	after := a.selected
	if !slices.ContainsFunc(blocked, func(s *proto.Session) bool { return s.ID == after }) {
		after = a.attentionLast
	}
	i := slices.IndexFunc(blocked, func(s *proto.Session) bool { return s.ID == after })
	next := blocked[(i+1)%len(blocked)] // no anchor: the first
	a.attentionLast = next.ID
	a.focusAttention(next.ID, next.Attention.Pane)
}

// focusAttention takes the user to what a session needs: the session
// shown, and pane — the pane its attention came from — in front and with
// the keyboard. pane "" is the session's own focus, as when the pane closed
// since the state came.
//
// A zoom follows the focus, as it does when panes are cycled while zoomed:
// the pane the session needs fills the session, rather than the focus
// landing on a pane a zoom keeps hidden.
//
// It reports whether there was a session to go to. The issue detail calls it
// with the session linked to the issue; the attention pane's rows are its
// other caller, once that pane lands.
func (a *App) focusAttention(session, pane string) bool {
	s := a.session(session)
	if s == nil {
		return false
	}
	if s.PaneByID(pane) == nil {
		pane = ""
	}
	// The session's row shows in the sidebar, as it does when jumped to.
	delete(a.prefs.Collapsed, s.Project)
	if a.show != "session" || a.selected != s.ID {
		a.selectSession(s.ID)
	}
	if pane == "" {
		pane = a.focusOf(s)
	} else {
		a.setFocus(s, pane)
		// setFocus leaves a zoomed session alone when the pane is already
		// the focused one: a zoom on another pane would hide this one.
		if s.Zoomed != "" && s.Zoomed != pane {
			s.Zoomed = pane
			a.call("zoom", proto.FocusParams{Session: s.ID, Pane: pane}, nil)
		}
	}
	if pane != "" {
		a.paneRef(pane).Focus()
	}
	return true
}

// focusAttentionOf takes the user to what a session needs, read from the
// state alone: the pane its attention came from, else the pane it was in
// last. It is the way back from an issue to its session, and the one the
// attention pane's rows will take.
func (a *App) focusAttentionOf(session string) bool {
	pane := ""
	if s := a.session(session); s != nil && s.Attention != nil {
		pane = s.Attention.Pane
	}
	return a.focusAttention(session, pane)
}
