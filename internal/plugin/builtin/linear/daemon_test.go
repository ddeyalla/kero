package linear

import (
	"testing"

	"kero/internal/proto"
)

// A state carries no workspace, and the one a "linear" event gave stands:
// the pane is handed a state with st.Linear as it was when every state
// carried it, and with the sessions of the last state.
func TestEventsKeepTheWorkspace(t *testing.T) {
	var got []*proto.State
	var ev events
	fn := ev.callback(func(st *proto.State, _ string) { got = append(got, st) })
	last := func() *proto.State {
		t.Helper()
		if len(got) == 0 {
			t.Fatal("no state was handed on")
		}
		st := got[len(got)-1]
		got = got[:len(got)-1]
		return st
	}

	fn(proto.Message{Event: "state", State: &proto.State{Sessions: []proto.Session{{ID: "s1"}}}})
	if st := last(); st.Linear != nil || len(st.Sessions) != 1 || st.Sessions[0].ID != "s1" {
		t.Fatalf("the first state came as %+v", st)
	}
	lin := &proto.LinearState{Status: proto.LinearOK, Issues: []proto.LinearIssue{{ID: "i1", Identifier: "BRU-1"}}}
	fn(proto.Message{Event: "linear", Linear: lin})
	if st := last(); st.Linear == nil || len(st.Linear.Issues) != 1 {
		t.Fatalf("the workspace came as %+v", st)
	} else if len(st.Sessions) != 1 || st.Sessions[0].ID != "s1" {
		t.Error("the workspace came without the sessions of the state before it")
	}
	// The state after it has the workspace too, as every state used to.
	fn(proto.Message{Event: "state", State: &proto.State{Sessions: []proto.Session{{ID: "s2"}}}})
	if st := last(); st.Linear == nil || st.Sessions[0].ID != "s2" {
		t.Fatalf("the state after the workspace came as %+v", st)
	}
	// A workspace that went is nil, not an empty one: Has is false again.
	fn(proto.Message{Event: "linear"})
	st := last()
	if st.Linear != nil {
		t.Errorf("a workspace that went came as %+v", st.Linear)
	}
	if snap := snapshotOf(st); snap.Has {
		t.Error("the pane still thinks the daemon has a workspace")
	}
	// What is not a state is not handed on.
	fn(proto.Message{Event: "notice"})
	if len(got) != 0 {
		t.Error("a notice was handed on as a state")
	}
}
