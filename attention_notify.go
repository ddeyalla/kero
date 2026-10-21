package main

import (
	"time"

	"kero/internal/proto"
)

// This file turns the attention kerod folds (internal/daemon/attention.go)
// into notifications: the app compares each session's fold with the one
// before, and tells of the sessions that came to wait on the user or
// ended unseen. The fold stays the one truth, so a notification says what
// the daemon computed, not what a pane's status happened to be as the app
// read it. The notifications themselves are the app's (app.go, tell): a
// daemon holds no notification of the desktop.

// The states a session's attention takes, as the fold says them
// (proto.Attention.State).
const (
	attentionRunning     = "running"      // work under way
	attentionWaitingUser = "waiting-user" // asks of the user
	attentionUnread      = "unread"       // ended, not seen
	attentionDone        = "done"         // ended, read or unmarked
	attentionIdle        = "idle"         // at rest
)

// attentionFold is one session's attention, as the transitions read it:
// the state, one line of what it says, the pane it comes from, and
// whether an unseen end was a failure, which tells with the error's
// title.
type attentionFold struct {
	State  string
	Msg    string
	Pane   string
	Failed bool
}

// foldOf is a session's attention. The state carries it (Session.Attention,
// filled by kerod); a daemon of before the field says none, so the fold is
// read from the session's panes, as kerod folds them.
func foldOf(s *proto.Session) attentionFold {
	if a := s.Attention; a != nil && a.State != "" {
		f := attentionFold{State: a.State, Msg: a.Msg, Pane: a.Pane}
		if f.State == attentionUnread {
			if p := s.PaneByID(f.Pane); p != nil {
				f = unreadFold(f, p)
			}
		}
		return f
	}
	if len(s.Panes) == 0 {
		return paneFold(&s.Pane)
	}
	// Every pane has its say, and the most urgent wins, as kerod folds
	// them; the lead pane wins a tie.
	var best attentionFold
	for _, p := range orderedPanes(s) {
		if f := paneFold(p); best.State == "" || attentionRank(f.State) > attentionRank(best.State) {
			best = f
		}
	}
	return best
}

// orderedPanes are a session's panes, its lead first.
func orderedPanes(s *proto.Session) []*proto.Pane {
	out := make([]*proto.Pane, 0, len(s.Panes))
	if p := s.PaneByID(s.Lead); p != nil {
		out = append(out, p)
	}
	for i := range s.Panes {
		if s.Panes[i].ID != s.Lead {
			out = append(out, &s.Panes[i])
		}
	}
	return out
}

// attentionRank orders the states as kerod does: a wait beats work, and
// an unseen end beats rest.
func attentionRank(state string) int {
	switch state {
	case attentionWaitingUser:
		return 5
	case attentionRunning:
		return 4
	case attentionUnread:
		return 3
	case attentionDone:
		return 2
	}
	return 1
}

// unreadFold narrows an unseen end by the pane it comes from. A view (Tasks,
// Files, Browser, Attention) never works or fails: its mark is the user's
// to clear, and it ends no turn, so it tells nothing. A program that
// exited with a code failed, and one that exited cleanly with no agent in
// it and no word to say ended nothing worth a notification.
func unreadFold(f attentionFold, p *proto.Pane) attentionFold {
	switch {
	case p.View != "":
		f.State = attentionIdle
	case p.Status == proto.Failed:
		f.Failed = true
	case p.Status == proto.Exited && p.ExitCode != 0:
		f.Failed = true
	case p.Status == proto.Exited && p.Agent == "" && p.Message == "":
		f.State = attentionDone
	}
	return f
}

// paneFold is one pane's attention, for a daemon that does not fold it.
func paneFold(p *proto.Pane) attentionFold {
	f := attentionFold{Pane: p.ID, Msg: p.Message}
	if p.View != "" {
		// A view never works or fails; its mark is the user's to clear.
		f.State = attentionIdle
		return f
	}
	switch p.Status {
	case proto.Working:
		f.State = attentionRunning
	case proto.Waiting:
		f.State = attentionWaitingUser
	default:
		switch {
		case p.Unread:
			f.State = attentionUnread
			f = unreadFold(f, p)
		case p.Status == proto.Failed:
			f.State = attentionDone
		default:
			f.State = attentionIdle
		}
	}
	return f
}

// attentionEvent is a session that came to need the user: what the
// notification says, and how it is named.
type attentionEvent struct {
	Session string
	State   string // attentionWaitingUser or attentionUnread
	Msg     string // one line, from the fold
	Pane    string
	Failed  bool // an unseen end whose pane failed
}

// attentionKey is one session's one state, as the throttle counts it.
type attentionKey struct{ session, state string }

// loopGap is how long a session must have been out of a state for its
// return to it to count as a new turn. A session that asks again sooner
// than this is a loop: one that asks, is answered, and asks at once. One
// that asks again a minute or more after the answer is a real second ask,
// and tells even inside the throttle window.
const loopGap = 30 * time.Second

// attentionWatch turns the sessions' attention into what to tell: it keeps
// each session's last state, and compares. A session that comes to
// waiting-user or unread tells once; one that goes back to running tells
// nothing (the wait or the end before it is past). A session that told,
// and comes back to the same state within loopGap of leaving it, inside
// the window, does not tell again, so a chatty agent's loops are counted,
// not announced.
//
// The watch only says what to tell. What was shown is the app's to say
// (shown), since the app may decide not to show an event, as when the user
// looks at its session; an event not shown must not hold the next one back.
//
// It knows nothing of windows or profiles: it is handed every session the
// daemon holds, so a session in a profile the user is not looking at still
// tells. What the app does with the user's focus is its own (tell).
type attentionWatch struct {
	state map[string]string          // session id: its state as last seen
	told  map[attentionKey]time.Time // session and state: when it last showed
	left  map[attentionKey]time.Time // session and state: when it last left it
	now   func() time.Time
}

func newAttentionWatch() *attentionWatch {
	return &attentionWatch{
		state: map[string]string{},
		told:  map[attentionKey]time.Time{},
		left:  map[attentionKey]time.Time{},
		now:   time.Now,
	}
}

// shown records that an event reached the user, which starts its throttle.
func (w *attentionWatch) shown(e attentionEvent) {
	w.told[attentionKey{session: e.Session, state: e.State}] = w.now()
}

// watch returns what to tell, of the state now. first says the app has just
// seen its first state, which it tells nothing of. quiet names the sessions
// the user silenced, which never tell; window is how long the same session
// and state stays quiet after it was shown, 0 for no throttle.
func (w *attentionWatch) watch(sessions []proto.Session, first bool, quiet map[string]bool, window time.Duration) []attentionEvent {
	now := w.now()
	live := make(map[string]bool, len(sessions))
	var out []attentionEvent
	for i := range sessions {
		s := &sessions[i]
		live[s.ID] = true
		f := foldOf(s)
		prev, seen := w.state[s.ID]
		w.state[s.ID] = f.State
		if seen && prev != f.State && (prev == attentionWaitingUser || prev == attentionUnread) {
			w.left[attentionKey{session: s.ID, state: prev}] = now
		}
		if first || !seen || f.State == prev {
			continue
		}
		if f.State != attentionWaitingUser && f.State != attentionUnread {
			// A turn began, or the session rests: what it said before is
			// past. No telling, and the throttle stays as it was.
			continue
		}
		if quiet[s.ID] {
			continue
		}
		k := attentionKey{session: s.ID, state: f.State}
		if at, ok := w.told[k]; ok && window > 0 && now.Sub(at) < window && now.Sub(w.left[k]) < loopGap {
			continue
		}
		out = append(out, attentionEvent{Session: s.ID, State: f.State, Msg: f.Msg, Pane: f.Pane, Failed: f.Failed})
	}
	for id := range w.state {
		if !live[id] {
			delete(w.state, id)
		}
	}
	for k, at := range w.told {
		if !live[k.session] || window > 0 && now.Sub(at) >= window {
			delete(w.told, k)
			delete(w.left, k)
		}
	}
	for k := range w.left {
		if !live[k.session] {
			delete(w.left, k)
		}
	}
	return out
}
