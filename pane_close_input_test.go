package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
)

// A pane gives its keyboard to a neighbour before its exit starts. During
// the fade, its card, header and nested controls must not take that focus
// back or dispatch an action. The live pane stays interactive.
func TestClosingPaneDoesNotTakeInput(t *testing.T) {
	a := testApp(t)
	s := a.session("s1")
	// The state closePane leaves while the exit frame runs: the closing
	// pane still in the layout, focus already on its neighbour.
	a.setFocus(s, "b")
	tt := ui.NewTester(a.view, 1100, 700)
	frames(tt)
	// The first closing frame starts from the laid-out two-pane frame.
	a.closing = map[string]bool{"b": true}
	a.setFocus(s, "a")
	tt.Frame()

	closing, ok := a.cardSize["b"]
	if !ok || closing.W <= 0 || closing.H <= 0 {
		t.Fatalf("the closing pane did not lay out: %+v", closing)
	}
	live, ok := a.cardSize["a"]
	if !ok || live.W <= 0 || live.H <= 0 {
		t.Fatalf("the live pane did not lay out: %+v", live)
	}

	// A press on the closing card's own padding is not a focus route back
	// into it.
	tt.ClickAt(closing.X+4, closing.Y+closing.H-4)
	tt.Frame()
	if got := a.focusOf(s); got != "a" {
		t.Fatalf("a click on the closing card moved focus to %q, want a", got)
	}

	// The header's real New Pane control is a nested built-in action. It must
	// not dispatch from a pane whose exit is already running.
	plusLabel := a.tip(locales.PaneNewButton(), "pane.new")
	// The live pane is focused, so its header control is the label the
	// tester finds. Use the same card-relative position for the closing
	// pane, which has the same width and header chrome.
	livePlus, ok := tt.Find(plusLabel)
	if !ok {
		t.Fatalf("the live pane has no New Pane control; texts %q", tt.Texts())
	}
	plusX := closing.X + (livePlus.X - live.X)
	plusY := closing.Y + (livePlus.Y - live.Y)
	tt.ClickAt(plusX+livePlus.W/2, plusY+livePlus.H/2)
	tt.Frame()
	if got := a.focusOf(s); got != "a" {
		t.Fatalf("the closing pane's child moved focus to %q, want a", got)
	}
	if a.chooser.open {
		t.Fatal("the New Pane control of a closing pane dispatched")
	}

	// The surviving pane keeps its own header control and focus.
	tt.ClickAt(livePlus.X+livePlus.W/2, livePlus.Y+livePlus.H/2)
	tt.Frame()
	if !a.chooser.open {
		t.Fatal("the live pane's New Pane control stopped working")
	}
	if got := a.focusOf(s); got != "a" {
		t.Fatalf("the live pane lost focus to %q", got)
	}
}

func TestClosingPaneKeyboardNavigationSkipsIt(t *testing.T) {
	a := testApp(t)
	s := a.session("s1")
	s.Panes = append(s.Panes, proto.Pane{ID: "c", Shell: "bash", Dir: "/tmp", Status: proto.Idle, Progress: -1})
	s.Layout = &proto.Layout{
		Row: true, Ratio: 0.5,
		A: &proto.Layout{Pane: "a"},
		B: &proto.Layout{Row: true, Ratio: 0.5, A: &proto.Layout{Pane: "b"}, B: &proto.Layout{Pane: "c"}},
	}
	a.setFocus(s, "a")
	a.closing = map[string]bool{"b": true}

	a.cyclePane(1)
	if got := a.focusOf(s); got != "c" {
		t.Fatalf("next selected %q, want live c after closing b", got)
	}
	a.setFocus(s, "a")
	a.cyclePane(-1)
	if got := a.focusOf(s); got != "c" {
		t.Fatalf("previous selected %q, want live c after closing b", got)
	}
	a.setFocus(s, "a")
	a.focusDirection("right")
	if got := a.focusOf(s); got != "c" {
		t.Fatalf("right selected %q, want live c after closing b", got)
	}

	// A direct focus request for an exiting pane is rejected.
	a.setFocus(s, "b")
	if got := a.focusOf(s); got != "c" {
		t.Fatalf("direct setFocus selected closing b: %q", got)
	}
}

func TestFocusDirectionUsesLiveCandidateFromOriginalGeometry(t *testing.T) {
	a := testApp(t)
	s := a.session("s1")
	s.Panes = append(s.Panes,
		proto.Pane{ID: "c", Shell: "bash", Dir: "/tmp", Status: proto.Idle, Progress: -1},
		proto.Pane{ID: "d", Shell: "zsh", Dir: "/tmp", Status: proto.Idle, Progress: -1},
	)
	// a and d sit above b and c; a is over b, so down from a would pick
	// the closing b and dead-end without the live c.
	s.Layout = &proto.Layout{
		Row: false, Ratio: 0.5,
		A: &proto.Layout{Row: true, Ratio: 0.75, A: &proto.Layout{Pane: "a"}, B: &proto.Layout{Pane: "d"}},
		B: &proto.Layout{Row: true, Ratio: 0.75, A: &proto.Layout{Pane: "b"}, B: &proto.Layout{Pane: "c"}},
	}
	a.setFocus(s, "a")
	a.closing = map[string]bool{"b": true}
	a.focusDirection("down")
	if got := a.focusOf(s); got != "c" {
		t.Fatalf("down from a selected %q, want live c beyond closing b", got)
	}

	a.setFocus(s, "d")
	a.closing = map[string]bool{"c": true}
	a.focusDirection("down")
	if got := a.focusOf(s); got != "b" {
		t.Fatalf("down from d selected %q, want live b beyond closing c", got)
	}

	a.setFocus(s, "a")
	a.closing = map[string]bool{"b": true, "c": true}
	a.focusDirection("down")
	if got := a.focusOf(s); got != "a" {
		t.Fatalf("down with no live candidate selected %q, want a", got)
	}
	if a.selected != "s1" {
		t.Fatalf("directional focus changed the selected session to %q", a.selected)
	}

	s.Zoomed = "a"
	a.closing = map[string]bool{"b": true}
	a.focusDirection("down")
	if got := a.focusOf(s); got != "a" || s.Zoomed != "a" {
		t.Fatalf("zoomed directional focus changed to %q, zoom %q", got, s.Zoomed)
	}
}

func TestClosingPaneKeyboardNavigationNoEligibleTarget(t *testing.T) {
	a := testApp(t)
	s := a.session("s1")
	a.setFocus(s, "a")
	a.closing = map[string]bool{"b": true}
	a.cyclePane(1)
	a.cyclePane(-1)
	a.focusDirection("right")
	if got := a.focusOf(s); got != "a" {
		t.Fatalf("navigation with only a closing candidate moved focus to %q", got)
	}

	// Everything closing is also a no-op, not a panic or a focus change.
	a.closing = map[string]bool{"a": true, "b": true}
	a.setFocus(s, "b")
	a.cyclePane(1)
	a.focusDirection("left")
	if got := a.focusOf(s); got != "a" {
		t.Fatalf("all-closing navigation changed focus to %q", got)
	}
}
