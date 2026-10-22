package daemon

import (
	"fmt"
	"testing"

	"kero/internal/proto"
)

// broadcastDaemon is a daemon with a client subscribed, whose events the
// test reads, and a session whose pane works on the workspace's one issue.
func broadcastDaemon(t *testing.T) (*Daemon, *controlConn) {
	t.Helper()
	d := &Daemon{
		subs:     map[*controlConn]struct{}{},
		panes:    map[string]*pane{},
		changeCh: make(chan struct{}, 1),
		saveCh:   make(chan struct{}, 1),
	}
	empty := ""
	d.viewed.Store(&empty)
	c := &controlConn{out: make(chan proto.Message, 256), d: d}
	d.subs[c] = struct{}{}
	d.mu.Lock()
	d.linear = &proto.LinearState{Status: proto.LinearOK, Issues: []proto.LinearIssue{
		{ID: "i1", Identifier: "BRU-1", Title: "One", UpdatedAt: 1},
	}}
	d.sessions = []*session{{id: "s1", name: "work", panes: []*pane{
		{
			info:    proto.Pane{ID: "p1", TaskID: "BRU-1", Status: proto.Idle, Progress: -1},
			changed: make(chan struct{}),
		},
		// A shell beside it, which no issue shows.
		{
			info:    proto.Pane{ID: "p2", Status: proto.Idle, Progress: -1},
			changed: make(chan struct{}),
		},
	}}}
	d.mu.Unlock()
	return d, c
}

// events takes what the client queued, and counts the states it got and
// lists the workspaces.
func events(c *controlConn) (states int, linears []*proto.LinearState) {
	for {
		select {
		case m := <-c.out:
			switch m.Event {
			case "state":
				states++
			case "linear":
				linears = append(linears, m.Linear)
			}
		default:
			return states, linears
		}
	}
}

// A storm of title changes sends the sessions and the workspace once: the
// Linear payload goes out only when it is not the one the client has.
func TestBroadcastSendsLinearOnlyWhenChanged(t *testing.T) {
	d, c := broadcastDaemon(t)
	for i := range 25 {
		d.mu.Lock()
		// A spinner's title, and the status of a pane no issue shows.
		d.sessions[0].panes[0].info.Title = fmt.Sprintf("work %d", i%4)
		d.sessions[0].panes[1].info.Status = proto.Status([]string{"idle", "working"}[i%2])
		d.mu.Unlock()
		d.broadcastNow()
	}
	states, linears := events(c)
	if states != 25 {
		t.Errorf("a storm of 25 titles sent %d states, want 25", states)
	}
	if len(linears) != 1 {
		t.Fatalf("a storm of 25 titles sent the workspace %d times, want once", len(linears))
	}
	if len(linears[0].Issues) != 1 || linears[0].Issues[0].Session != "s1" {
		t.Fatalf("the workspace sent is not the one with its links: %+v", linears[0].Issues)
	}

	// The workspace read anew goes out on its own.
	d.mu.Lock()
	d.linear.Issues[0].Title = "One, renamed"
	d.mu.Unlock()
	d.broadcastNow()
	if _, got := events(c); len(got) != 1 || got[0].Issues[0].Title != "One, renamed" {
		t.Fatalf("a workspace read anew sent %+v, want the new title", got)
	}

	// So does a task link: the session working on the issue needs the user.
	d.mu.Lock()
	d.sessions[0].panes[0].info.Status = proto.Waiting
	d.mu.Unlock()
	d.broadcastNow()
	if _, got := events(c); len(got) != 1 || got[0].Issues[0].SessionStatus != proto.Waiting {
		t.Fatalf("a task link that changed sent %+v, want the waiting session", got)
	}

	// A state that changed nothing after it sends no workspace.
	d.broadcastNow()
	if _, got := events(c); len(got) != 0 {
		t.Fatalf("a broadcast of the same sent the workspace %d times", len(got))
	}
}

// A client that subscribes gets the workspace once, and the states after it
// carry none: the sessions are not copied with the whole workspace for
// every title change.
func TestSubscribeSendsTheLinearOnce(t *testing.T) {
	d, _ := broadcastDaemon(t)
	c := &controlConn{out: make(chan proto.Message, 256), d: d}
	if _, err := d.call(c, "subscribe", nil); err != nil {
		t.Fatal(err)
	}
	states, linears := events(c)
	if states != 1 || len(linears) != 1 {
		t.Fatalf("a subscribe got %d states and %d workspaces, want one of each", states, len(linears))
	}
	// The broadcast after it sends the workspace once more, to this client
	// too: the subscribe recorded nothing, so nothing is lost to it.
	d.mu.Lock()
	d.sessions[0].panes[0].info.Title = "renamed"
	d.mu.Unlock()
	d.broadcastNow()
	states, linears = events(c)
	if states != 1 || len(linears) != 1 {
		t.Errorf("the broadcast after a subscribe sent %d states and %d workspaces, want one of each", states, len(linears))
	}
}

// A client that subscribes while the workspace changed does not hide the
// change from the others: the subscribe records nothing, so the next
// broadcast still sends it.
func TestSubscribeDoesNotHideAChange(t *testing.T) {
	d, a := broadcastDaemon(t)
	d.broadcastNow() // a has the first workspace
	events(a)
	d.mu.Lock()
	d.linear.Issues[0].Title = "Two"
	d.mu.Unlock()
	b := &controlConn{out: make(chan proto.Message, 256), d: d}
	if _, err := d.call(b, "subscribe", nil); err != nil {
		t.Fatal(err)
	}
	if _, got := events(b); len(got) != 1 || got[0].Issues[0].Title != "Two" {
		t.Fatalf("the new client got %+v, want the workspace that changed", got)
	}
	d.broadcastNow()
	if _, got := events(a); len(got) != 1 || got[0].Issues[0].Title != "Two" {
		t.Errorf("the client that was there got %+v, want the workspace that changed", got)
	}
}

// A workspace event a client was too far behind to take is sent again: the
// states after it carry none, so it would otherwise stay stale until the
// workspace changed.
func TestDroppedLinearIsResent(t *testing.T) {
	d, c := broadcastDaemon(t)
	d.broadcastNow() // the first workspace, which c takes
	events(c)
	for range cap(c.out) { // c is as far behind as its queue allows
		c.out <- proto.Message{Event: "state"}
	}
	d.mu.Lock()
	d.linear.Issues[0].Title = "Two"
	d.mu.Unlock()
	d.broadcastNow() // the workspace event is dropped
	events(c)        // c catches up
	d.broadcastNow() // nothing changed, and the workspace comes again
	if _, got := events(c); len(got) != 1 || got[0].Issues[0].Title != "Two" {
		t.Errorf("after the drop the client got %+v, want the workspace sent again", got)
	}
}

// A daemon with no workspace tells a subscriber so with a nil one, as
// State.Linear was then: the app and the plugin take nil for none.
func TestNoWorkspaceIsNil(t *testing.T) {
	d := &Daemon{subs: map[*controlConn]struct{}{}, panes: map[string]*pane{}, changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	empty := ""
	d.viewed.Store(&empty)
	c := &controlConn{out: make(chan proto.Message, 8), d: d}
	if _, err := d.call(c, "subscribe", nil); err != nil {
		t.Fatal(err)
	}
	states, linears := events(c)
	if states != 1 || len(linears) != 1 {
		t.Fatalf("a subscribe got %d states and %d workspaces, want one of each", states, len(linears))
	}
	if linears[0] != nil {
		t.Errorf("a daemon with no workspace sent %+v, want nil", linears[0])
	}
	// A state asked for once has none either, as it always had.
	if st := d.state(); st.Linear != nil {
		t.Errorf("a state asked for once has a workspace: %+v", st.Linear)
	}
	// And the RPC that answers the Linear view keeps its status.
	if lin := d.linearState(); lin == nil || lin.Status != proto.LinearNoToken {
		t.Errorf("the linear.state call answers %+v, want no_token", lin)
	}
}

// A state a client asks for once, as the CLI and the app's own reading do,
// still carries the workspace: only the broadcast leaves it to its event.
func TestStateCalledOnceCarriesLinear(t *testing.T) {
	d, _ := broadcastDaemon(t)
	if st := d.state(); st.Linear == nil || len(st.Linear.Issues) != 1 {
		t.Errorf("a state asked for once has no workspace: %+v", st.Linear)
	}
	if st := d.broadcastState(); st.Linear != nil {
		t.Error("a broadcast state carries the workspace")
	}
}

// A title change goes to the window and not to disk: the saved state has no
// titles, and agents rename their panes many times a second.
func TestTitleChangeDoesNotSave(t *testing.T) {
	d := &Daemon{changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	p := &pane{d: d, info: proto.Pane{ID: "p1", Title: "a"}}
	p.onTitle("b")
	if len(d.changeCh) != 1 {
		t.Errorf("a title change asked for %d broadcasts, want 1", len(d.changeCh))
	}
	if len(d.saveCh) != 0 {
		t.Error("a title change wrote the state to disk")
	}
	// A change the saved state holds still saves, as every other does.
	d.changed()
	if len(d.saveCh) != 1 {
		t.Error("a change of the saved state did not save")
	}
}

// An event a client is too far behind to take is counted, not lost in
// silence.
func TestDroppedEventsCounted(t *testing.T) {
	d := &Daemon{}
	c := &controlConn{out: make(chan proto.Message), d: d} // nothing takes it
	c.event(proto.Message{Event: "state"})
	c.event(proto.Message{Event: "linear"})
	if got := d.dropped.Load(); got != 2 {
		t.Errorf("%d events counted as dropped, want 2", got)
	}
}
