package main

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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

// A pane opening starts small and fades in over the same 160 ms it takes
// to close, and its first frame is not the frame it animates on.
func TestPaneOpeningAndClosing(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	frames(tt)

	a.opening["b"] = 1
	tt.Frame()
	if !tt.FramePending() {
		t.Error("the pane opening asks for no more frames")
	}
	tt.Frame()
	if _, ok := a.opening["b"]; ok {
		t.Error("the pane opening still holds after its two frames")
	}
	tt.Advance(time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Error("the pane opening is still moving after a second")
	}

	a.closing = map[string]bool{"b": true}
	tt.Frame()
	if !tt.FramePending() {
		t.Error("the pane closing asks for no more frames")
	}
	tt.Advance(time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Error("the pane closing is still moving after a second")
	}
}
