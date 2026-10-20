package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// A pane too narrow for two is not split across, and says why; one wide
// enough is.
func TestSplitNeedsRoom(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	s := a.session("s1")
	if b := a.cardSize["a"]; b.W == 0 || b.W >= 2*minPaneW+paneGap {
		t.Fatalf("pane a is %v wide: the test wants one too narrow for two", b.W)
	}
	a.splitPane(s, "a", "right")
	if a.pendingToast != "Not enough room for another pane here" {
		t.Fatalf("a split with no room said %q", a.pendingToast)
	}
	// Down, there is room.
	a.pendingToast = ""
	a.splitPane(s, "a", "down")
	if a.pendingToast != "" {
		t.Fatalf("a split down with room was refused: %q", a.pendingToast)
	}
}

// No pane is drawn smaller than the least a pane gets, however the room
// is shared.
func TestPanesKeepTheirLeastSize(t *testing.T) {
	a := testApp(t)
	a.st.Sessions[0].Layout.Ratio = 0.05
	tt := ui.NewTester(a.view, 1280, 700)
	frames(tt)
	if w := a.cardSize["a"].W; w < minPaneW-1 {
		t.Fatalf("pane a is %v wide, under %d", w, minPaneW)
	}
}

func TestMinExtent(t *testing.T) {
	var l *proto.Layout
	l = l.Split("a", "b", "right", 0.5)
	l = l.Split("b", "c", "down", 0.5)
	if got := minExtent(l, true); got != 2*minPaneW+paneGap {
		t.Errorf("across %v", got)
	}
	if got := minExtent(l, false); got != 2*minPaneH+paneGap {
		t.Errorf("down %v", got)
	}
}
