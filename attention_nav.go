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

// blockedSessions returns the sessions waiting on the user, in the order
// `attention.next-blocked` visits them: the longest wait first, and of two
// waiting as long, by the session's id, so that a press never depends on
// the order kerod happened to send its sessions in. A session from a daemon
// that does not fold its attention is read from its lead pane, as the
// notifications read it (foldOf).
func (a *App) blockedSessions() []*proto.Session {
	var out []*proto.Session
	for i := range a.st.Sessions {
		s := &a.st.Sessions[i]
		if foldOf(s).State == proto.AttentionWaitingUser {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(x, y *proto.Session) int {
		return cmp.Or(cmp.Compare(waitingSince(x), waitingSince(y)), cmp.Compare(x.ID, y.ID))
	})
	return out
}

// waitingSince is when a session's attention came, in milliseconds. A
// daemon of before the field sends none, and its sessions all count as the
// longest wait, so that they are told apart by id alone.
func waitingSince(s *proto.Session) int64 {
	if s.Attention != nil {
		return s.Attention.Since
	}
	return 0
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
	a.focusAttention(next.ID, foldOf(next).Pane)
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
// with the session linked to the issue.
func (a *App) focusAttention(session, pane string) bool {
	s := a.session(session)
	if s == nil {
		return false
	}
	if s.PaneByID(pane) == nil {
		pane = ""
	}
	// The session's row shows in the sidebar, as it does when jumped to.
	if _, ok := a.prefs.Collapsed[s.Project]; ok {
		delete(a.prefs.Collapsed, s.Project)
		a.savePrefs()
	}
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
// state alone: the pane its attention came from when it waits on the user,
// else the pane it was in last. A session at rest names its first pane in
// its attention, which is no reason to leave the pane the user was in, so
// only a wait moves the focus. It is the way back from an issue to its
// session.
func (a *App) focusAttentionOf(session string) bool {
	pane := ""
	if s := a.session(session); s != nil {
		if f := foldOf(s); f.State == proto.AttentionWaitingUser {
			pane = f.Pane
		}
	}
	return a.focusAttention(session, pane)
}
