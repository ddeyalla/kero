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
// read from the session's lead pane, as kerod folds a pane's status.
func foldOf(s *proto.Session) attentionFold {
	if a := s.Attention; a != nil && a.State != "" {
		f := attentionFold{State: a.State, Msg: a.Msg, Pane: a.Pane}
		if f.State == attentionUnread {
			if p := s.PaneByID(f.Pane); p != nil && p.Status == proto.Failed {
				f.Failed = true
			}
		}
		return f
	}
	p := s.PaneByID(s.Lead)
	if p == nil {
		p = &s.Pane
	}
	f := attentionFold{Pane: p.ID, Msg: p.Message}
	if p.View != "" {
		// A view never works or fails; its mark is the user's to clear.
		if p.Unread {
			f.State = attentionUnread
		} else {
			f.State = attentionIdle
		}
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
			f.Failed = p.Status == proto.Failed
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

// attentionWatch turns the sessions' attention into what to tell: it keeps
// each session's last state, and compares. A session that comes to
// waiting-user or unread tells once; one that goes back to running tells
// nothing (the wait or the end before it is past); the same session and
// state inside the window does not tell again, so a chatty agent's loops
// are counted, not announced.
//
// It knows nothing of windows or profiles: it is handed every session the
// daemon holds, so a session in a profile the user is not looking at still
// tells. What the app does with the user's focus is its own (tell).
type attentionWatch struct {
	state map[string]string          // session id: its state as last seen
	told  map[attentionKey]time.Time // session and state: when it last told
	now   func() time.Time
}

func newAttentionWatch() *attentionWatch {
	return &attentionWatch{state: map[string]string{}, told: map[attentionKey]time.Time{}, now: time.Now}
}

// watch returns what to tell, of the state now. first says the app has just
// seen its first state, which it tells nothing of. quiet names the sessions
// the user silenced, which never tell; window is how long the same session
// and state stays quiet after it told, 0 for no throttle.
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
		if first || !seen || f.State == prev {
			continue
		}
		if f.State != attentionWaitingUser && f.State != attentionUnread {
			// A turn began, or the session rests: what it said before is
			// past. No telling, and the throttle stays as it was, so a
			// wait that comes again inside the window stays quiet.
			continue
		}
		if quiet[s.ID] {
			continue
		}
		k := attentionKey{session: s.ID, state: f.State}
		if at, told := w.told[k]; told && window > 0 && now.Sub(at) < window {
			continue
		}
		w.told[k] = now
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
		}
	}
	return out
}
