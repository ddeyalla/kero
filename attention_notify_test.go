package main

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"kero/internal/client"
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
	w.shown(got[0])
	// A chatty agent asks again at once: counted, not announced.
	at = at.Add(10 * time.Second)
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

// A second ask a while after the answer is a real one, not a loop: it
// tells inside the window, as long as the first was shown.
func TestAttentionWatchRealSecondAsk(t *testing.T) {
	w := newAttentionWatch()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clockAt(w, &at)
	window := 5 * time.Minute
	sessions := []proto.Session{attentionSession("s1", attentionRunning, "")}
	w.watch(sessions, false, nil, window)
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you")
	got := w.watch(sessions, false, nil, window)
	w.shown(got[0])
	at = at.Add(5 * time.Second)
	sessions[0] = attentionSession("s1", attentionRunning, "")
	w.watch(sessions, false, nil, window)
	// Two minutes of work, then it asks again: inside the window, but a
	// new turn.
	at = at.Add(2 * time.Minute)
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you again")
	if got := w.watch(sessions, false, nil, window); len(got) != 1 {
		t.Fatalf("a real second ask told %v, want one", states(got))
	}
}

// An event the app did not show holds nothing back: only shown starts the
// throttle.
func TestAttentionWatchUnshownDoesNotThrottle(t *testing.T) {
	w := newAttentionWatch()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clockAt(w, &at)
	window := 5 * time.Minute
	sessions := []proto.Session{attentionSession("s1", attentionRunning, "")}
	w.watch(sessions, false, nil, window)
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you")
	if got := w.watch(sessions, false, nil, window); len(got) != 1 {
		t.Fatalf("told %v", states(got))
	}
	// Not shown. It works for a moment, and asks again.
	at = at.Add(5 * time.Second)
	sessions[0] = attentionSession("s1", attentionRunning, "")
	w.watch(sessions, false, nil, window)
	at = at.Add(5 * time.Second)
	sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you")
	if got := w.watch(sessions, false, nil, window); len(got) != 1 {
		t.Fatalf("an unshown ask held the next back: %v", states(got))
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
	a.deliver = func(id, pane, title, body string, done func(bool)) {
		*sent = append(*sent, attentionNote{Session: id, Pane: pane, Title: title, Body: body})
		done(true)
	}
	return a, sent
}

type attentionNote struct {
	Session string
	Pane    string
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

// kerod restores the panes of a busy session idle and unread when it
// restarts. The app reconnects, the fold says unread for each, and none of
// it is news.
func TestReconnectTellsNothingOfRestoredPanes(t *testing.T) {
	a, sent := attentionApp(t,
		attentionSession("s1", attentionRunning, "Working"),
		attentionSession("s2", attentionRunning, "Working"),
	)
	a.selected, a.active = "other", true
	a.notify(false)
	cl := &client.Client{}
	a.cl = cl
	a.disconnected(cl)
	a.st.Sessions[0] = attentionSession("s1", attentionUnread, "Turn ended")
	a.st.Sessions[1] = attentionSession("s2", attentionUnread, "Turn ended")
	a.notify(false)
	if len(*sent) != 0 {
		t.Fatalf("a reconnect told %v", *sent)
	}
	// The watch is new, not blind: the turns after tell.
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "Working")
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you")
	a.notify(false)
	if len(*sent) != 1 {
		t.Fatalf("after a reconnect told %v, want one", *sent)
	}
}

// A session that asks while the user looks at it shows nothing, and does
// not hold back the genuine ask after the user has gone elsewhere.
func TestFocusedAskDoesNotPoisonTheThrottle(t *testing.T) {
	a, sent := attentionApp(t,
		attentionSession("s1", attentionRunning, ""),
		attentionSession("s2", attentionRunning, ""),
	)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clockAt(a.watch, &at)
	a.selected, a.active = "s1", true
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you")
	a.notify(false)
	if len(*sent) != 0 {
		t.Fatalf("the focused session told %v", *sent)
	}
	// Answered; the user goes to s2; s1 asks again inside the window.
	at = at.Add(5 * time.Second)
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "")
	a.notify(false)
	a.selected = "s2"
	at = at.Add(5 * time.Second)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Needs you again")
	a.notify(false)
	if len(*sent) != 1 || (*sent)[0].Session != "s1" {
		t.Fatalf("told %v, want exactly one for s1", *sent)
	}
}

// What an agent wrote reaches a notification nowhere: a session's end says
// the agent and the folder, whatever line the fold holds.
func TestDoneNotificationNeverCarriesTheFoldLine(t *testing.T) {
	a, sent := attentionApp(t, attentionSession("s1", attentionRunning, ""))
	a.selected, a.active = "other", true
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionUnread, "Use sk-fake-token for the deploy")
	a.notify(false)
	if len(*sent) != 1 {
		t.Fatalf("told %v", *sent)
	}
	if body := (*sent)[0].Body; strings.Contains(body, "sk-fake-token") || body != locales.NotifyDoneBody("", proto.ShortDir("")) {
		t.Errorf("body %q", body)
	}
}

// A view with an unread mark, a clean exit of a plain program and a
// program that failed to exit well: the first two say nothing, the last
// is a failure.
func TestFoldOfViewsAndExits(t *testing.T) {
	view := proto.Pane{ID: "v", View: "tasks", Unread: true}
	s := attentionSession("s1", attentionUnread, "", view)
	if f := foldOf(&s); f.State != attentionIdle {
		t.Errorf("an unread view folds %q", f.State)
	}
	clean := proto.Pane{ID: "p", Status: proto.Exited, Unread: true}
	s = attentionSession("s1", attentionUnread, "Exited with code 0", clean)
	if f := foldOf(&s); f.State != attentionDone {
		t.Errorf("a clean exit folds %q", f.State)
	}
	bad := proto.Pane{ID: "p", Status: proto.Exited, ExitCode: 2, Unread: true}
	s = attentionSession("s1", attentionUnread, "Exited with code 2", bad)
	if f := foldOf(&s); f.State != attentionUnread || !f.Failed {
		t.Errorf("a failed exit folds %+v", f)
	}
	// An agent that quit cleanly is still an end the user may want.
	agent := proto.Pane{ID: "p", Status: proto.Exited, Agent: "claude", Unread: true}
	s = attentionSession("s1", attentionUnread, "", agent)
	if f := foldOf(&s); f.State != attentionUnread || f.Failed {
		t.Errorf("an agent's exit folds %+v", f)
	}
}

// A daemon of before the field: every pane has its say, the most urgent
// wins.
func TestFoldOfOlderDaemonReadsEveryPane(t *testing.T) {
	s := proto.Session{ID: "s1", Lead: "a", Panes: []proto.Pane{
		{ID: "a", Status: proto.Idle},
		{ID: "b", Status: proto.Waiting, Message: "Needs permission"},
	}}
	if f := foldOf(&s); f.State != attentionWaitingUser || f.Pane != "b" {
		t.Errorf("fold %+v", f)
	}
}

// The focus filter reads the window, not only the selection, and a zoomed
// session's other panes are out of sight.
func TestLookingReadsWindowAndZoom(t *testing.T) {
	a, _ := attentionApp(t, attentionSession("s1", attentionRunning, ""))
	s := &a.st.Sessions[0]
	a.selected, a.active, a.focused = "s1", true, true
	if !a.looking(s, "p") {
		t.Error("not looking at the selected session")
	}
	a.focused = false
	if a.looking(s, "p") {
		t.Error("looking with the window without the keyboard")
	}
	a.focused = true
	s.Zoomed = "q"
	if a.looking(s, "p") || !a.looking(s, "q") {
		t.Error("zoom does not decide what is in sight")
	}
}

// Silence by session and by state, and idempotent; closed sessions fall
// off the list after the first real state.
func TestSilenceArgsAndPruning(t *testing.T) {
	a, _ := attentionApp(t,
		attentionSession("s1", attentionRunning, ""),
		attentionSession("s2", attentionRunning, ""),
	)
	a.selected = "s1"
	a.run("session.silence", map[string]string{"session": "s2", "state": "on"})
	a.run("session.silence", map[string]string{"session": "s2", "state": "on"})
	if !a.silenced("s2") || a.silenced("s1") || len(a.set.SilencedSessions) != 1 {
		t.Fatalf("silenced %v", a.set.SilencedSessions)
	}
	a.run("session.silence", map[string]string{"session": "s2", "state": "off"})
	if a.silenced("s2") {
		t.Fatal("s2 not resumed")
	}
	a.run("session.silence", nil)
	if !a.silenced("s1") {
		t.Fatal("the selected session is the default")
	}
	a.set.SilencedSessions = []string{"s1", "gone"}
	previous := a.st
	previous.Sessions = append(slices.Clone(previous.Sessions), attentionSession("gone", attentionIdle, ""))
	a.setState(previous)
	next := a.st
	next.Sessions = slices.DeleteFunc(slices.Clone(next.Sessions), func(s proto.Session) bool { return s.ID == "gone" })
	a.setState(next)
	if !slices.Equal(a.set.SilencedSessions, []string{"s1"}) {
		t.Fatalf("pruned %v", a.set.SilencedSessions)
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

// Restored sessions arrive incrementally. Absence is evidence of closure
// only after the session was observed in this same daemon epoch.
func TestSilencedSessionsSurvivePartialRestore(t *testing.T) {
	a := testApp(t)
	a.loaded = false
	a.set.SilencedSessions = []string{"early", "late", "unknown"}
	state := func(epoch int64, ids ...string) proto.State {
		st := proto.State{Started: epoch}
		for _, id := range ids {
			st.Sessions = append(st.Sessions, attentionSession(id, attentionIdle, ""))
		}
		return st
	}
	a.setState(state(1, "early"))
	a.setState(state(1, "early"))
	if !a.silenced("late") || !a.silenced("unknown") {
		t.Fatalf("partial restore removed unobserved silence: %v", a.set.SilencedSessions)
	}
	a.setState(state(1, "early", "late"))
	a.setState(state(1, "late"))
	if a.silenced("early") || !a.silenced("late") || !a.silenced("unknown") {
		t.Fatalf("observed closure pruned wrong IDs: %v", a.set.SilencedSessions)
	}
	// A daemon restart starts a fresh observation epoch; late may return.
	a.setState(state(2, "early"))
	a.setState(state(2, "early"))
	if !a.silenced("late") || !a.silenced("unknown") {
		t.Fatalf("new epoch removed late restored silence: %v", a.set.SilencedSessions)
	}
	a.setState(state(2, "early", "late"))
	a.setState(state(2))
	if a.silenced("late") || !a.silenced("unknown") {
		t.Fatalf("empty state failed observed closure: %v", a.set.SilencedSessions)
	}
}

// Native permission denial or backend unavailability is not a delivered
// notice and cannot silence the next eligible ask when delivery recovers.
func TestNotificationDeliveryFailureDoesNotChargeThrottle(t *testing.T) {
	a, _ := attentionApp(t, attentionSession("s1", attentionRunning, ""))
	a.selected = "other"
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	clockAt(a.watch, &now)
	available, attempts, shown := false, 0, 0
	a.deliver = func(_, _, _, _ string, done func(bool)) {
		attempts++
		if available {
			shown++
		}
		done(available)
	}
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "First ask")
	a.notify(false)
	if len(a.watch.told) != 0 {
		t.Fatal("undelivered native notification charged throttle")
	}
	now = now.Add(time.Second)
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "")
	a.notify(false)
	now = now.Add(time.Second)
	available = true
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Second ask")
	a.notify(false)
	if attempts != 2 || shown != 1 || len(a.watch.told) != 1 {
		t.Fatalf("recovered delivery: attempts=%d shown=%d told=%v", attempts, shown, a.watch.told)
	}
}

// Authorization can wait. Repeated state updates must not post duplicate
// notices; a later eligible ask is queued and the old acknowledgement must
// not consume the newer event's throttle or cross a reconnect epoch.
func TestNotificationPendingDeliveryAndEpoch(t *testing.T) {
	a, _ := attentionApp(t, attentionSession("s1", attentionRunning, ""))
	a.selected = "other"
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	clockAt(a.watch, &now)
	var callbacks []func(bool)
	var messages []string
	a.deliver = func(_, _, _, body string, done func(bool)) {
		messages = append(messages, body)
		callbacks = append(callbacks, done)
	}
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "First ask")
	a.notify(false)
	for range 3 {
		a.notify(false)
	}
	if len(callbacks) != 1 || len(a.watch.told) != 0 {
		t.Fatalf("pending native notice duplicated or charged: callbacks=%d told=%v", len(callbacks), a.watch.told)
	}
	now = now.Add(time.Second)
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "")
	a.notify(false)
	now = now.Add(time.Second)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Newest ask")
	a.notify(false)
	if len(callbacks) != 1 {
		t.Fatal("new ask raced an in-flight native notice")
	}
	callbacks[0](true)
	if len(callbacks) != 2 || messages[1] != "Newest ask" || len(a.watch.told) != 0 {
		t.Fatalf("newest ask lost to old acknowledgement: messages=%v told=%v", messages, a.watch.told)
	}
	// A reconnect retires the old watch even while a native delivery waits.
	old := a.watch
	a.watch = newAttentionWatch()
	clockAt(a.watch, &now)
	callbacks[1](true)
	if len(a.watch.told) != 0 || len(old.told) != 0 {
		t.Fatal("late native result charged a retired watch")
	}
}

// The app removes a row optimistically, so observed-disappearance pruning
// cannot see it later. Persistent cleanup must follow successful daemon
// acknowledgement; a rejected close preserves both saved choices.
func TestSessionCloseAcknowledgementCleansPreferences(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "accepted"}[success], func(t *testing.T) {
			a := testApp(t)
			a.st.Sessions = append(a.st.Sessions, proto.Session{ID: "s2", Project: "p1"})
			a.prefs.SessionPins = map[string]bool{"s2": true, "unknown": true}
			a.set.SilencedSessions = []string{"s2", "unknown"}
			a.savePrefs()
			a.flushPrefs()
			a.saveSettings(a.set)
			a.flushSettings()
			requests := attentionRPC(t, a)
			a.closeSession("s2", false)
			if a.session("s2") != nil {
				t.Fatal("fixture did not exercise optimistic row removal")
			}
			if !a.prefs.SessionPins["s2"] || !a.silenced("s2") {
				t.Fatal("preferences removed before close acknowledgement")
			}
			deadline := time.After(time.Second)
			waiting := true
			for waiting {
				select {
				case req := <-requests:
					waiting = req.Method != "close"
				case <-deadline:
					t.Fatal("no close RPC")
				}
			}
			// The tester has no native callback queue. Deliver the same completion
			// handler explicitly after observing the peer's close request.
			var err error
			if !success {
				err = errors.New("close rejected")
			}
			a.sessionClosed("s2", err)
			a.flushPrefs()
			a.flushSettings()
			a.loadPrefs()
			saved := settings.Load()
			if a.prefs.SessionPins["s2"] == success || slices.Contains(saved.SilencedSessions, "s2") == success {
				t.Fatalf("success=%t pin=%v silence=%v", success, a.prefs.SessionPins, saved.SilencedSessions)
			}
			if !a.prefs.SessionPins["unknown"] || !slices.Contains(saved.SilencedSessions, "unknown") {
				t.Fatal("close removed another session's preferences")
			}
		})
	}
}

func TestNotificationQueuedStateRetiredDoesNotPost(t *testing.T) {
	a, _ := attentionApp(t, attentionSession("s1", attentionRunning, ""))
	a.selected = "other"
	var callbacks []func(bool)
	a.deliver = func(_, _, _, _ string, done func(bool)) { callbacks = append(callbacks, done) }
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "First")
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "")
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Queued")
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "")
	a.notify(false)
	callbacks[0](true)
	if len(callbacks) != 1 {
		t.Fatal("an already resolved queued state posted a stale notice")
	}
}

// Muting a session while native authorization waits applies to a queued
// newer ask too; replay must recheck the user's current silence choice.
func TestNotificationQueuedSilencedSessionDoesNotPost(t *testing.T) {
	a, _ := attentionApp(t, attentionSession("s1", attentionRunning, ""))
	a.selected = "other"
	var callbacks []func(bool)
	a.deliver = func(_, _, _, _ string, done func(bool)) { callbacks = append(callbacks, done) }
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "First")
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionRunning, "")
	a.notify(false)
	a.st.Sessions[0] = attentionSession("s1", attentionWaitingUser, "Queued")
	a.notify(false)
	if len(callbacks) != 1 {
		t.Fatal("fixture did not queue behind native delivery")
	}
	a.setSilence("s1", true)
	callbacks[0](true)
	if len(callbacks) != 1 {
		t.Fatal("queued notice ignored current session silence")
	}
}
