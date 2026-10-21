package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// pluginMarqueeTree is one marquee text in a column of the given width.
func pluginMarqueeTree(text string, width int) *protocol.Tree {
	return pluginTree(sdk.Column("root", sdk.Text("line", text).Marquee()).Size(width, 0))
}

// pluginMarqueeLong is a line that overflows a narrow column by a little,
// so that a test does not have to wait out a long scroll.
func pluginMarqueeLong() string {
	return strings.Repeat("A line of plugin text. ", 2)
}

// atTime renders once at when, moving the tester's clock there.
func (h *pluginHarness) atTime(when time.Time) {
	if d := when.Sub(h.tt.Now()); d > 0 {
		h.tt.Advance(d)
	}
	h.tt.Frame()
}

// TestPluginMarqueeOffset checks the motion itself: a pause at the start,
// a slow scroll, a pause at the end, then the start again.
func TestPluginMarqueeOffset(t *testing.T) {
	distance := float32(240)
	scroll := pluginMarqueeScroll(distance)
	if scroll <= pluginMarqueePause {
		t.Fatalf("scroll = %v, want more than the pause %v", scroll, pluginMarqueePause)
	}
	tests := []struct {
		when time.Duration
		want float32
	}{
		{0, 0},
		{pluginMarqueePause / 2, 0},
		{pluginMarqueePause, 0},
		{pluginMarqueePause + scroll/2, distance / 2},
		{pluginMarqueePause + scroll, distance},
		{pluginMarqueePause + scroll + pluginMarqueePause/2, distance},
		{2*pluginMarqueePause + scroll, 0},
	}
	for _, tc := range tests {
		got := pluginMarqueeOffset(distance, tc.when)
		if diff := got - tc.want; diff > 0.5 || diff < -0.5 {
			t.Errorf("offset(%v) = %v, want %v", tc.when, got, tc.want)
		}
	}
	if got := pluginMarqueeOffset(0, time.Second); got != 0 {
		t.Errorf("offset of a text that fits = %v, want 0", got)
	}
}

// TestPluginMarqueeFitsDoesNotMove keeps a short text still: no scroll, and
// no frame asked for.
func TestPluginMarqueeFitsDoesNotMove(t *testing.T) {
	h := newPluginHarness(t, pluginMarqueeTree("Short", 400), nil)
	m := h.view.marquees["line"]
	if m == nil {
		t.Fatal("no marquee state for the text")
	}
	if m.overflow {
		t.Fatalf("a short text overflows, natural %v avail %v", m.natural, m.avail)
	}
	if m.offset != 0 || m.moving {
		t.Fatalf("a short text moves: offset %v moving %v", m.offset, m.moving)
	}
	h.atTime(h.tt.Now().Add(3 * time.Second))
	if m.offset != 0 || m.moving {
		t.Fatalf("after 3s a short text moves: offset %v moving %v", m.offset, m.moving)
	}
}

// TestPluginMarqueeScrollsAndResets scrolls a long text once it does not
// fit, then puts it back at its start.
func TestPluginMarqueeScrollsAndResets(t *testing.T) {
	h := newPluginHarness(t, pluginMarqueeTree(pluginMarqueeLong(), 180), nil)
	m := h.view.marquees["line"]
	if m == nil {
		t.Fatal("no marquee state for the text")
	}
	if !m.overflow {
		t.Fatalf("a long text fits: natural %v avail %v", m.natural, m.avail)
	}
	scroll := pluginMarqueeScroll(m.distance)
	// Still during the pause at the start.
	h.atTime(m.since.Add(pluginMarqueePause / 2))
	if m.offset != 0 {
		t.Fatalf("during the first pause offset = %v, want 0", m.offset)
	}
	// Halfway across the text.
	h.atTime(m.since.Add(pluginMarqueePause + scroll/2))
	if m.offset <= 0 || m.offset >= m.distance {
		t.Fatalf("halfway offset = %v, want between 0 and %v", m.offset, m.distance)
	}
	// Held at the end while it pauses.
	h.atTime(m.since.Add(pluginMarqueePause + scroll + pluginMarqueePause/2))
	if diff := m.offset - m.distance; diff > 0.5 || diff < -0.5 {
		t.Fatalf("pause at the end offset = %v, want %v", m.offset, m.distance)
	}
	// Back at its start, and then moving again.
	h.atTime(m.since.Add(2*pluginMarqueePause + scroll + pluginMarqueePause/2))
	if m.offset != 0 {
		t.Fatalf("after a full turn offset = %v, want 0", m.offset)
	}
	h.atTime(m.since.Add(2*pluginMarqueePause + scroll + pluginMarqueePause + scroll/2))
	if m.offset <= 0 {
		t.Fatalf("second turn offset = %v, want more than 0", m.offset)
	}
}

// TestPluginMarqueeStaticUnderReduceMotion keeps a long text still when
// Reduce Motion is on, and ends it in an ellipsis instead.
func TestPluginMarqueeStaticUnderReduceMotion(t *testing.T) {
	h := newPluginHarness(t, pluginMarqueeTree(pluginMarqueeLong(), 180), nil)
	h.tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	h.frames()
	m := h.view.marquees["line"]
	if m == nil {
		t.Fatal("no marquee state for the text")
	}
	if !m.overflow {
		t.Fatalf("a long text fits: natural %v avail %v", m.natural, m.avail)
	}
	start := h.tt.Now()
	h.atTime(start.Add(pluginMarqueePause + pluginMarqueeScroll(m.distance)/2))
	if m.offset != 0 || m.moving {
		t.Fatalf("under Reduce Motion offset %v moving %v, want 0 and no motion", m.offset, m.moving)
	}
	h.atTime(start.Add(3 * time.Second))
	if m.offset != 0 || m.moving {
		t.Fatalf("under Reduce Motion after 3s offset %v moving %v, want 0 and no motion", m.offset, m.moving)
	}
}

// TestPluginMarqueeResetsWhenTextChanges starts the motion over when the
// same part id gets a new text: the new title must not begin in the middle
// of the old one's turn.
func TestPluginMarqueeResetsWhenTextChanges(t *testing.T) {
	long := pluginMarqueeLong()
	h := newPluginHarness(t, pluginMarqueeTree(long, 180), nil)
	m := h.view.marquees["line"]
	if m == nil || !m.overflow {
		t.Fatalf("the long text does not overflow: %+v", m)
	}
	// Partway through the old text's scroll.
	h.atTime(m.since.Add(pluginMarqueePause + pluginMarqueeScroll(m.distance)/2))
	if m.offset <= 0 {
		t.Fatalf("the long text does not scroll: offset %v", m.offset)
	}

	// The same part id gets a short text: it fits, and it is still.
	h.show(pluginMarqueeTree("Short", 180))
	m = h.view.marquees["line"]
	if m == nil || m.overflow || m.offset != 0 || m.moving {
		t.Fatalf("the short text moves: %+v", m)
	}

	// The long text comes back in the next frame: its turn starts at the
	// beginning, not where the old text had scrolled to.
	h.tree = pluginMarqueeTree(long, 180)
	h.tt.Frame()
	m = h.view.marquees["line"]
	if m == nil || !m.overflow {
		t.Fatal("the long text does not overflow again")
	}
	if m.offset != 0 {
		t.Fatalf("the new turn starts at offset %v, want the pause at 0", m.offset)
	}
	h.atTime(m.since.Add(pluginMarqueePause / 2))
	if m.offset != 0 {
		t.Fatalf("during the new pause offset = %v, want 0", m.offset)
	}
	h.atTime(m.since.Add(pluginMarqueePause + pluginMarqueeScroll(m.distance)/2))
	if m.offset <= 0 {
		t.Fatalf("the new turn does not scroll on: offset %v", m.offset)
	}
}
