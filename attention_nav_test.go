package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/actions"
	"kero/internal/proto"

	"kero/locales"
)

// `kero do` reaches the action from a shell or an agent, it is in the
// Session menu, and no key is taken from a pane's program for it.
func TestAttentionAction(t *testing.T) {
	act, ok := actions.Find("attention.next-blocked")
	if !ok {
		t.Fatal("no attention.next-blocked action")
	}
	if !act.Remote {
		t.Error("a shell or an agent cannot run it")
	}
	if act.Keys != "" {
		t.Errorf("it has the default shortcut %q", act.Keys)
	}
	if act.Menu != "Session" {
		t.Errorf("its menu is %q, want Session", act.Menu)
	}
}

// waitingSession adds a session of one pane that waits on the user, as
// kerod folds it into the state.
func waitingSession(a *App, id string, since int64) {
	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: id, Project: "p1", Name: id, Focus: id + "-pane",
		Panes:     []proto.Pane{{ID: id + "-pane", Shell: "zsh", Dir: "/tmp", Status: proto.Waiting, Progress: -1}},
		Attention: &proto.Attention{State: proto.AttentionWaitingUser, Since: since, Pane: id + "-pane", Msg: "May I run it?"},
	})
}

// The cycle goes through the sessions that wait on the user, the longest
// wait first, then by id; it starts after the session shown when that one
// waits too, goes on from where it went last, and wraps.
func TestAttentionCycleOrder(t *testing.T) {
	a := testApp(t)
	a.st.Sessions[0].Attention = &proto.Attention{State: proto.AttentionWaitingUser, Since: 300, Pane: "b", Msg: "Approve the plan?"}
	waitingSession(a, "s2", 100)
	waitingSession(a, "s3", 200)
	// A session at work does not wait for anyone, and is never visited.
	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: "s4", Project: "p1", Name: "at work", Focus: "s4-pane",
		Panes:     []proto.Pane{{ID: "s4-pane", Shell: "zsh", Dir: "/tmp", Status: proto.Working, Progress: -1}},
		Attention: &proto.Attention{State: "running", Since: 50, Pane: "s4-pane"},
	})

	// s1 waits longest, and is the session shown: the presses go on from
	// it to the shortest wait, then in order.
	for _, want := range []struct{ session, pane string }{{"s2", "s2-pane"}, {"s3", "s3-pane"}} {
		a.run("attention.next-blocked", nil)
		if a.selected != want.session {
			t.Fatalf("went to %q, want %q", a.selected, want.session)
		}
		if got := a.focusOf(a.session(a.selected)); got != want.pane {
			t.Errorf("%s: the pane in front is %q, want %q", want.session, got, want.pane)
		}
	}
	if a.pendingToast != "" {
		t.Errorf("toast %q with sessions waiting", a.pendingToast)
	}

	// The user goes back to a waiting session by hand: the press goes on
	// from it, not from the one it went to last.
	a.selectSession("s2")
	a.run("attention.next-blocked", nil)
	if a.selected != "s3" {
		t.Fatalf("from s2, the press went to %q, want s3", a.selected)
	}

	// The user looks at a session at work: the press goes on from the last
	// waiting one it went to, s3, not from the top of the list.
	a.selectSession("s4")
	a.run("attention.next-blocked", nil)
	if a.selected != "s1" {
		t.Fatalf("after looking at s4, the press went to %q, want s1", a.selected)
	}

	// The session it went to before that stops waiting, as one the user
	// answered, and leaves the cycle: the press goes on from s1 to s3.
	s2 := a.session("s2")
	s2.Attention = &proto.Attention{State: "running", Since: 500, Pane: "s2-pane"}
	a.run("attention.next-blocked", nil)
	if a.selected != "s3" {
		t.Fatalf("with s2 stopped, the press went to %q, want s3", a.selected)
	}
	// And it wraps: after s3 comes s1 again.
	a.run("attention.next-blocked", nil)
	if a.selected != "s1" {
		t.Fatalf("the cycle did not wrap: went to %q, want s1", a.selected)
	}
}

// Two sessions that wait as long go by id, whatever order kerod sent them
// in, so that the cycle does not depend on it.
func TestAttentionCycleTieByID(t *testing.T) {
	a := testApp(t)
	a.st.Sessions = nil
	waitingSession(a, "b", 100)
	waitingSession(a, "a", 100)
	got := a.blockedSessions()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("the order is %v, want a then b", idsOf(got))
	}
}

func idsOf(sessions []*proto.Session) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = s.ID
	}
	return out
}

// With nothing waiting, the action says so and goes nowhere.
func TestAttentionNothingBlocked(t *testing.T) {
	a := testApp(t)
	a.st.Sessions[0].Attention = &proto.Attention{State: "running", Since: 1, Pane: "a"}
	a.run("attention.next-blocked", nil)
	if a.selected != "s1" {
		t.Errorf("nothing waits, and the app went to %q", a.selected)
	}
	if a.pendingToast != locales.ToastNothingNeedsYou() {
		t.Errorf("toast %q, want %q", a.pendingToast, locales.ToastNothingNeedsYou())
	}
	// A daemon of before sends no attention at all: no more to go to.
	a.st.Sessions[0].Attention = nil
	a.pendingToast = ""
	a.run("attention.next-blocked", nil)
	if a.pendingToast != locales.ToastNothingNeedsYou() {
		t.Errorf("with no attention, toast %q", a.pendingToast)
	}
}

// Going to a session shows the pane it needs, which a zoom then fills: the
// focus never lands on a pane the zoom keeps hidden.
func TestAttentionFocusWithZoom(t *testing.T) {
	a := testApp(t)
	s := a.session("s1")
	s.Attention = &proto.Attention{State: proto.AttentionWaitingUser, Since: 1, Pane: "b", Msg: "Approve the migration"}
	s.Zoomed = "a"
	tt := ui.NewTester(a.view, 1100, 640)
	frames(tt)
	if tt.HasText("zsh") || !tt.HasText("fish") {
		t.Fatalf("zoomed on a, the frame shows b too; texts %q", tt.Texts())
	}
	a.run("attention.next-blocked", nil)
	frames(tt)
	if got := a.focusOf(s); got != "b" {
		t.Errorf("the focus is %q, want b", got)
	}
	if s.Zoomed != "b" {
		t.Errorf("the zoom is %q, want b: the pane it needs", s.Zoomed)
	}
	if !tt.HasText("zsh") || tt.HasText("fish") {
		t.Errorf("the pane it needs is not the one shown; texts %q", tt.Texts())
	}
	// The focus on it and a zoom elsewhere, as `kero focus` leaves them:
	// the zoom follows the pane asked for.
	s.Focus, s.Zoomed = "b", "a"
	if !a.focusAttention("s1", "b") {
		t.Fatal("no session to go to")
	}
	if s.Zoomed != "b" {
		t.Errorf("the zoom stayed on %q over the pane focused", s.Zoomed)
	}
}

// Going to a pane that closed lands on the session's own, and a session
// gone is nothing to go to.
func TestAttentionFocusFallsBack(t *testing.T) {
	a := testApp(t)
	if !a.focusAttention("s1", "gone") {
		t.Fatal("no session to go to")
	}
	if got := a.focusOf(a.session("s1")); got != "a" {
		t.Errorf("the pane in front is %q, want the session's own a", got)
	}
	if a.focusAttention("gone", "a") {
		t.Error("went to a session that is not there")
	}
	if a.focusAttentionOf("gone") {
		t.Error("went to what a session that is not there needs")
	}
}

// The issue detail's session opens where it needs the user: the mirror of
// the attention pane's way back to the issue.
func TestLinearIssueOpensAttentionPane(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	a.session("s1").Attention = &proto.Attention{State: proto.AttentionWaitingUser, Since: time.Now().UnixMilli(), Pane: "b", Msg: "Approve the migration"}
	tt := ui.NewTester(a.view, 1280, 760)
	frames(tt)
	if err := tt.Click("Open the session"); err != nil {
		t.Fatalf("no way from the issue to its session: %v", err)
	}
	frames(tt)
	if a.lin.open != "" {
		t.Error("the issue stayed open over the session")
	}
	if a.show != "session" || a.selected != "s1" {
		t.Fatalf("the issue opened show %q, session %q", a.show, a.selected)
	}
	if got := a.focusOf(a.session("s1")); got != "b" {
		t.Errorf("the session opened on %q, want the pane it needs, b", got)
	}
}

// A session of a daemon of before, which says nothing of what it needs,
// still opens where it was last.
func TestLinearIssueOpensSessionWithoutAttention(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	a.session("s1").Focus, a.focus["s1"] = "b", "b"
	tt := ui.NewTester(a.view, 1280, 760)
	frames(tt)
	if err := tt.Click("Open the session"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if a.selected != "s1" || a.focusOf(a.session("s1")) != "b" {
		t.Fatalf("opened %q on %q", a.selected, a.focusOf(a.session("s1")))
	}
}

// A session from a daemon that folds no attention still cycles, read from
// its lead pane as the notifications read it.
func TestAttentionCycleWithoutFold(t *testing.T) {
	a := testApp(t)
	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: "old", Project: "p1", Name: "old", Lead: "old-pane", Focus: "old-pane",
		Panes: []proto.Pane{{ID: "old-pane", Shell: "zsh", Dir: "/tmp", Status: proto.Waiting, Progress: -1}},
	})
	a.run("attention.next-blocked", nil)
	if a.selected != "old" {
		t.Fatalf("the cycle went to %q, want the session with a waiting pane", a.selected)
	}
	if got := a.focusOf(a.session("old")); got != "old-pane" {
		t.Errorf("the pane in front is %q, want old-pane", got)
	}
}

// Opening the session of an issue whose session is at rest keeps the pane
// and the zoom where the user left them: the first pane an idle fold names
// is no pane that needs anyone.
func TestAttentionOfIdleSessionKeepsFocus(t *testing.T) {
	a := testApp(t)
	s := a.session("s1")
	s.Attention = &proto.Attention{State: proto.AttentionIdle, Pane: "a"}
	s.Focus, s.Zoomed = "b", "b"
	if !a.focusAttentionOf("s1") {
		t.Fatal("no session to go to")
	}
	if got := a.focusOf(s); got != "b" {
		t.Errorf("the focus is %q, want b", got)
	}
	if s.Zoomed != "b" {
		t.Errorf("the zoom is %q, want b", s.Zoomed)
	}
}
