package main

import (
	"testing"

	"kero/internal/proto"
)

// A state equal to the one before it never reaches the window: the client
// drops it on its own goroutine, before anything is built or drawn. A state
// that differs does go on, and so does any other event.
func TestEventDropsTheSameState(t *testing.T) {
	a := testApp(t)
	var seen stateSeen
	st := a.st
	if seen.stale(proto.Message{Event: "state", State: &st}) {
		t.Fatal("the first state was dropped")
	}
	if !seen.stale(proto.Message{Event: "state", State: &st}) {
		t.Error("an identical state went on to build the window")
	}
	changed := a.st
	changed.Viewed = "s2"
	if seen.stale(proto.Message{Event: "state", State: &changed}) {
		t.Error("a state that changed did not go on")
	}
	if seen.stale(proto.Message{Event: "select", Select: "s2"}) {
		t.Error("a select was dropped with the states")
	}
	if seen.stale(proto.Message{Event: "linear", Linear: &proto.LinearState{}}) {
		t.Error("a workspace was dropped with the states")
	}
}

// The workspace comes on an event of its own, and a state, which carries
// none, keeps the one the app has: the Tasks pane and the Linear view read
// a.st.Linear.
func TestLinearEventUpdatesTheWorkspace(t *testing.T) {
	a := testApp(t)
	a.st.Linear = nil
	lin := &proto.LinearState{
		Status: proto.LinearOK,
		Issues: []proto.LinearIssue{{ID: "i1", Identifier: "BRU-1", Title: "One", State: "In Progress"}},
	}
	a.event(nil, proto.Message{Event: "linear", Linear: lin})
	if a.st.Linear == nil || len(a.st.Linear.Issues) != 1 || a.st.Linear.Issues[0].Identifier != "BRU-1" {
		t.Fatalf("the workspace did not arrive: %+v", a.st.Linear)
	}
	st := a.st
	st.Viewed = "s1"
	a.event(nil, proto.Message{Event: "state", State: &st})
	if a.st.Linear == nil || len(a.st.Linear.Issues) != 1 {
		t.Error("a state without a workspace dropped the one the app had")
	}
	if a.st.Viewed != "s1" {
		t.Errorf("the state did not come: viewed %q", a.st.Viewed)
	}
}
