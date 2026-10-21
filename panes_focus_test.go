package main

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// A pane the user is not in is a pane like any other: its card and its
// text do not change when the focus leaves it. Only the focused pane's
// edge lights, and the light follows the focus.
func TestPaneFocusEdge(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	frames(tt)

	body := func(id string) color.RGBA {
		r := a.cardSize[id]
		return tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
	}
	edge := func(id string) color.RGBA {
		r := a.cardSize[id]
		return tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y))
	}
	if body("a") != body("b") {
		t.Errorf("the unfocused pane's card differs: %v, %v", body("a"), body("b"))
	}
	aEdge, bEdge := edge("a"), edge("b")
	if aEdge == bEdge {
		t.Fatalf("both pane edges are the same %v; no focus edge", aEdge)
	}

	// The focus goes to b: the edge goes with it, and neither card's
	// body changes.
	a.setFocus(&a.st.Sessions[0], "b")
	frames(tt)
	if got := edge("a"); got == aEdge {
		t.Errorf("pane a kept its edge after losing the focus: %v", got)
	}
	if got := edge("b"); got == bEdge {
		t.Errorf("pane b has no edge after taking the focus: %v", got)
	}
	if body("a") != body("b") {
		t.Errorf("the cards differ after the focus moved: %v, %v", body("a"), body("b"))
	}
}

// The edge tells the pane the user is in from the others: a pane alone,
// or one zoomed, has none to be told from, and no edge lights.
func TestPaneFocusEdgeOnlyAmongPanes(t *testing.T) {
	edge := func(a *App, tt *ui.Tester, id string) color.RGBA {
		r := a.cardSize[id]
		return tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y))
	}
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	frames(tt)
	lit, unlit := edge(a, tt, "a"), edge(a, tt, "b")
	if lit == unlit {
		t.Fatalf("the focused pane of two has no edge: %v", lit)
	}
	for _, tc := range []struct {
		name string
		set  func(s *proto.Session)
	}{
		{"alone", func(s *proto.Session) { s.Panes, s.Layout = s.Panes[:1], nil }},
		{"zoomed", func(s *proto.Session) { s.Zoomed = "a" }},
	} {
		a := testApp(t)
		tc.set(&a.st.Sessions[0])
		tt := ui.NewTester(a.view, 1100, 640)
		frames(tt)
		if got := edge(a, tt, "a"); got != unlit {
			t.Errorf("%s: the pane's edge lights: %v, want %v as an unfocused pane's", tc.name, got, unlit)
		}
	}
}

// A pane opening starts small and fades in over the same 160 ms it takes
// to close, and its first frame is not the frame it animates on: a pane
// that kerod puts in another's place, as focusNew takes it, is not its
// card yet on the frame it arrives, nor halfway, and is once it has
// landed. (One split off grows from its edge as well, which would hide
// whether it fades.)
func TestPaneOpeningAndClosing(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	frames(tt)
	card := rgba(lightPalette.card)

	ns := a.st.Sessions[0]
	ns.Panes = []proto.Pane{ns.Panes[0], {ID: "c", Shell: "bash", Dir: "/tmp", Status: proto.Idle, Progress: -1}}
	var l *proto.Layout
	ns.Layout = l.Split("a", "c", "right", 0)
	ns.Focus = "c"
	a.focusNew(ns)
	tt.Frame()
	// Its body, under its title, where it lands.
	title, ok := tt.Find("bash")
	if !ok {
		t.Fatalf("no new pane; texts %q", tt.Texts())
	}
	body := func() color.RGBA {
		return tt.Image().RGBAAt(int(title.X+title.W/2), int(title.Y+title.H+60))
	}
	if got := body(); got == card {
		t.Errorf("the new pane shows whole on its first frame: %v", got)
	}
	if !tt.FramePending() {
		t.Error("the pane opening asks for no more frames")
	}
	tt.Advance(focus.Duration / 2)
	tt.Frame()
	if got := body(); got == card {
		t.Errorf("the new pane shows whole halfway through its fade: %v", got)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Error("the pane opening is still moving after a second")
	}
	if _, ok := a.opening["c"]; ok {
		t.Error("the pane opening still holds after it landed")
	}
	if got := body(); got != card {
		t.Errorf("the new pane is not its card after it landed: %v, want %v", got, card)
	}

	// Closing, the pane gives its room back from its far edge as it
	// fades: halfway, what is left of it is no longer its card.
	a.closing = map[string]bool{"c": true}
	tt.Frame()
	if !tt.FramePending() {
		t.Error("the pane closing asks for no more frames")
	}
	tt.Advance(focus.Duration / 2)
	tt.Frame()
	c := a.cardSize["c"]
	if got := tt.Image().RGBAAt(int(c.X+c.W-12), int(c.Y+c.H/2)); got == card {
		t.Errorf("the closing pane is still its card halfway: %v", got)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Error("the pane closing is still moving after a second")
	}
}
