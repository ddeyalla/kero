package main

import (
	"fmt"
	"testing"

	"kero/internal/proto"
	"kero/internal/settings"
)

// The app keeps no more than maxTerms terminals attached: the panes of
// sessions shown longest ago go, every pane counted, not only the focused
// ones, and never one of the session the user is in.
func TestTermBudget(t *testing.T) {
	a := testApp(t)
	attach := func(id string) {
		a.terms[id] = &termEntry{}
		a.noteTerm(id)
	}
	hidden := proto.Session{ID: "s2", Project: "p1", Name: "hidden"}
	for i := range 2 * maxTerms {
		id := fmt.Sprintf("h%d", i)
		hidden.Panes = append(hidden.Panes, proto.Pane{ID: id, Status: proto.Idle, Progress: -1})
		attach(id)
	}
	a.st.Sessions = append(a.st.Sessions, hidden)
	attach("b")
	attach("a")
	a.touch("a")
	if len(a.terms) != maxTerms || len(a.lru) != maxTerms {
		t.Fatalf("%d terminals attached, %d counted; want %d", len(a.terms), len(a.lru), maxTerms)
	}
	for _, id := range []string{"a", "b", fmt.Sprintf("h%d", 2*maxTerms-1)} {
		if a.terms[id] == nil {
			t.Errorf("%s was dropped", id)
		}
	}
	if a.terms["h0"] != nil {
		t.Error("the pane shown longest ago stayed attached")
	}

	// The session the user is in keeps every pane, past the limit.
	a.selected = "s2"
	for _, p := range hidden.Panes {
		attach(p.ID)
	}
	a.touch("h0")
	for _, p := range hidden.Panes {
		if a.terms[p.ID] == nil {
			t.Fatalf("%s of the session shown was dropped", p.ID)
		}
	}
	if a.terms["a"] != nil || a.terms["b"] != nil {
		t.Error("the panes of a session not shown stayed past the limit")
	}
}

// The app's terminals keep the image budget, as kerod's do: MyGo's default
// of 320 MB for every pane's images is not what 25 panes can pay.
func TestTermImageLimit(t *testing.T) {
	a := testApp(t)
	if got := a.termOptions("a", nil).ImageLimit; got != settings.ImageLimitBytes {
		t.Errorf("the app's terminal keeps %d bytes of images, want %d", got, settings.ImageLimitBytes)
	}
}

// The app's terminals keep the scrollback the setting says, as kerod's
// do, and none for none: never MyGo's default of 10 MB.
func TestTermScrollback(t *testing.T) {
	a := testApp(t)
	for mb, want := range map[int]int{4: 4 << 20, 0: -1} {
		a.set.Scrollback = mb
		if got := a.termOptions("a", nil).Scrollback; got != want {
			t.Errorf("%d MB: the terminal keeps %d", mb, got)
		}
	}
}
