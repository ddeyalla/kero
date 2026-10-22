package main

import (
	"testing"
	"time"

	"kero/internal/proto"
	"kero/internal/settings"

	"kero/locales"
)

// attentionSession is a session as kerod folds it: its attention on the
// state, and the panes that gave it.
func attentionSession(id, state, msg string, panes ...proto.Pane) proto.Session {
	att := &proto.Attention{State: state, Msg: msg}
	if len(panes) > 0 {
		att.Pane = panes[0].ID
	}
	return proto.Session{
		ID:        id,
		Name:      "demo",
		Attention: att,
		Panes:     panes,
	}
}

// clockAt fixes the watch's clock at t, and moves it.
func clockAt(w *attentionWatch, t *time.Time) {
	w.now = func() time.Time { return *t }
}

func states(events []attentionEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Session + ":" + e.State
	}
	return out
}

// A turn tells once: the wait, then nothing more of it, and the same wait
// again only after the window. A return to work tells nothing.
func TestAttentionWatchTellsEachTurnOnce(t *testing.T) {
	w := newAttentionWatch()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clockAt(w, &at)
	window := 5 * time.Minute
	sessions := []proto.Session{attentionSession("s1", attentionRunning, "Working")}
	// The app's first look tells nothing of it.
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("the first look told %v", states(got))
	}
	// The agent asks: one telling.
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs permission to run rm")
	got := w.watch(sessions, false, nil, window)
	if want := []string{"s1:waiting-user"}; !equalStrings(states(got), want) {
		t.Fatalf("asking told %v, want %v", states(got), want)
	}
	if got[0].Msg != "Needs permission to run rm" {
		t.Errorf("body %q", got[0].Msg)
	}
	// More of the same wait, with a new line, is the same turn: nothing.
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs permission to run rm -rf")
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("the same wait told again: %v", states(got))
	}
	// The user answers and the agent works: a clear, not a telling.
	sessions[0] = attentionSession("s1", attentionRunning, "Working")
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("work told %v", states(got))
	}
	// A chatty agent asks again at once: counted, not announced.
	at = at.Add(time.Minute)
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs permission once more")
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("a repeat inside the window told %v", states(got))
	}
	// Past the window, the next turn tells: the loop is not one turn. The
	// turn begins again first, as every turn does.
	at = at.Add(window + time.Second)
	sessions[0] = attentionSession("s1", attentionRunning, "Working")
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("the next turn's work told %v", states(got))
	}
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs permission once more")
	if got := w.watch(sessions, false, nil, window); len(got) != 1 {
		t.Fatalf("past the window told %v, want one", states(got))
	}
}

// An end the user did not see tells, once: done and idle tell nothing, as
// the user saw them or the daemon did not mark them.
func TestAttentionWatchUnreadAndEnds(t *testing.T) {
	w := newAttentionWatch()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clockAt(w, &at)
	window := 5 * time.Minute
	failed := proto.Pane{ID: "p1", Status: proto.Failed, Unread: true}
	sessions := []proto.Session{attentionSession("s1", attentionRunning, "", failed)}
	w.watch(sessions, false, nil, window)

	sessions[0] = attentionSession("s1", attentionUnread, "Failed: the tests did not pass", failed)
	got := w.watch(sessions, false, nil, window)
	if len(got) != 1 || got[0].State != attentionUnread || !got[0].Failed {
		t.Fatalf("an unseen failure told %v", got)
	}
	if got[0].Pane != "p1" {
		t.Errorf("pane %q", got[0].Pane)
	}
	// Seen: the fold says done, which tells nothing.
	sessions[0] = attentionSession("s1", attentionDone, "", proto.Pane{ID: "p1", Status: proto.Failed})
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("a read end told %v", states(got))
	}
	// Rest tells nothing.
	sessions[0] = attentionSession("s1", attentionIdle, "")
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("rest told %v", states(got))
	}
}

// A silenced session never tells, and the sessions beside it still do.
func TestAttentionWatchQuietSession(t *testing.T) {
	w := newAttentionWatch()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clockAt(w, &at)
	window := 5 * time.Minute
	sessions := []proto.Session{
		attentionSession("quiet", attentionRunning, ""),
		attentionSession("loud", attentionRunning, ""),
	}
	w.watch(sessions, false, nil, window)

	sessions[0] = attentionSession("quiet", attentionWaitingUser, "Needs you")
	sessions[1] = attentionSession("loud", attentionWaitingUser, "Needs you")
	got := w.watch(sessions, false, map[string]bool{"quiet": true}, window)
	if want := []string{"loud:waiting-user"}; !equalStrings(states(got), want) {
		t.Fatalf("told %v, want %v", states(got), want)
	}

	// Unsilenced, the wait is still the same turn: it does not tell late.
	sessions[0] = attentionSession("quiet", attentionWaitingUser, "Needs you")
	if got := w.watch(sessions, false, nil, window); len(got) != 0 {
		t.Fatalf("a silenced wait told after it was let go: %v", states(got))
	}
}

// The first state the app sees tells nothing: starting the app over a
// session that already waits is no news.
func TestAttentionWatchFirstState(t *testing.T) {
	w := newAttentionWatch()
	sessions := []proto.Session{attentionSession("s1", attentionWaitingUser, "Needs you")}
	if got := w.watch(sessions, true, nil, 5*time.Minute); len(got) != 0 {
		t.Fatalf("the first state told %v", states(got))
	}
	if got := w.watch(sessions, false, nil, 5*time.Minute); len(got) != 0 {
		t.Fatalf("the same state told %v", states(got))
	}
}

// A daemon of before the Attention field: the fold is read from the
// session's lead pane, so its waits and unseen ends still tell.
func TestAttentionWatchOlderDaemon(t *testing.T) {
	w := newAttentionWatch()
	old := proto.Session{ID: "s1", Lead: "p1", Panes: []proto.Pane{
		{ID: "p1", Status: proto.Working, Progress: -1},
	}}
	w.watch([]proto.Session{old}, false, nil, 0)
	old.Panes[0] = proto.Pane{ID: "p1", Status: proto.Waiting, Message: "Needs permission"}
	got := w.watch([]proto.Session{old}, false, nil, 0)
	if len(got) != 1 || got[0].State != attentionWaitingUser || got[0].Msg != "Needs permission" {
		t.Fatalf("an older daemon's wait told %v", got)
	}
}

// attentionApp is an app whose notifications go to a stub: no test asks
// the system to show one.
func attentionApp(t *testing.T, sessions ...proto.Session) (*App, *[]attentionNote) {
	t.Helper()
	a := testApp(t)
	a.st.Sessions = sessions
	sent := &[]attentionNote{}
	a.deliver = func(id, title, body string) {
		*sent = append(*sent, attentionNote{Session: id, Title: title, Body: body})
	}
	return a, sent
}

type attentionNote struct {
	Session string
	Title   string
	Body    string
}

// A session the user does not look at tells, wherever it is, and one it
// does not is left quiet: focus is the only filter, never a window or a
// profile.
func TestNotifyTellsEverySessionButTheFocusedOne(t *testing.T) {
	a, sent := attentionApp(t,
		attentionSession("s1", attentionRunning, ""),
		attentionSession("s2", attentionRunning, ""),
		attentionSession("s3", attentionRunning, ""),
	)
	a.selected, a.active = "s1", true
	a.notify(false) // the first look: nothing
	if len(*sent) != 0 {
		t.Fatalf("the first look told %v", *sent)
	}
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you")
	a.st.Sessions[1] = attentionSession("s2", attentionWaitingUser, "Needs permission to push")
	a.st.Sessions[2] = attentionSession("s3", attentionUnread, "The turn ended")
	a.notify(false)
	if len(*sent) != 2 {
		t.Fatalf("told %v, want two: the focused session is quiet", *sent)
	}
	if (*sent)[0].Session != "s2" || (*sent)[0].Title != locales.NotifyNeedsYouTitle("demo") {
		t.Errorf("s2's note %+v", (*sent)[0])
	}
	if (*sent)[0].Body != "Needs permission to push" {
		t.Errorf("s2's body %q", (*sent)[0].Body)
	}
	if (*sent)[1].Session != "s3" || (*sent)[1].Title != locales.NotifyDoneTitle("demo") {
		t.Errorf("s3's note %+v", (*sent)[1])
	}
}

// A silenced session tells nothing, and the choice is remembered in the
// settings; resuming it lets the next turn tell.
func TestSilencedSessionStaysQuiet(t *testing.T) {
	a, sent := attentionApp(t,
		attentionSession("s1", attentionRunning, ""),
		attentionSession("s2", attentionRunning, ""),
	)
	a.selected, a.active = "s1", true
	a.notify(false)
	// The action, as the Session menu, the palette and `kero do` run it.
	a.selected = "s2"
	a.run("session.silence", nil)
	a.selected = "s1"
	if !a.silenced("s2") {
		t.Fatal("s2 is not silenced")
	}
	a.flushSettings()
	if saved := settings.Load(); len(saved.SilencedSessions) != 1 || saved.SilencedSessions[0] != "s2" {
		t.Fatalf("saved silence %q", saved.SilencedSessions)
	}
	a.st.Sessions[1] = attentionSession("s2", attentionWaitingUser, "Needs you")
	a.notify(false)
	if len(*sent) != 0 {
		t.Fatalf("a silenced session told %v", *sent)
	}
	// Resumed, the next turn tells again.
	a.toggleSilence("s2")
	if a.silenced("s2") {
		t.Fatal("s2 is still silenced")
	}
	a.st.Sessions[1] = attentionSession("s2", attentionRunning, "")
	a.notify(false)
	a.st.Sessions[1] = attentionSession("s2", attentionUnread, "Done")
	a.notify(false)
	if len(*sent) != 1 || (*sent)[0].Session != "s2" {
		t.Fatalf("a resumed session told %v", *sent)
	}
}

// The settings' toggles still decide which notices show, as they did when
// the statuses said it.
func TestNotifyRespectsTheToggles(t *testing.T) {
	a, sent := attentionApp(t,
		attentionSession("s1", attentionRunning, ""),
		attentionSession("s2", attentionRunning, ""),
	)
	a.selected, a.active = "s1", true
	a.notify(false)
	off := a.set
	off.NotifyWaiting, off.NotifyDone = false, false
	a.apply(off, false)
	a.st.Sessions[1] = attentionSession("s2", attentionWaitingUser, "Needs you")
	a.notify(false)
	if len(*sent) != 0 {
		t.Fatalf("notices off, told %v", *sent)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
