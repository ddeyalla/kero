package main

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
)

// crop is the part r of the tester's last frame, to compare.
func crop(tt *ui.Tester, r ui.Rect) []byte {
	img := tt.Image()
	part := img.SubImage(image.Rect(int(r.X), int(r.Y), int(r.X+r.W)+1, int(r.Y+r.H)+1)).(*image.RGBA)
	var out []byte
	for y := part.Rect.Min.Y; y < part.Rect.Max.Y; y++ {
		i := part.PixOffset(part.Rect.Min.X, y)
		out = append(out, part.Pix[i:i+4*part.Rect.Dx()]...)
	}
	return out
}

// A view that has nothing yet turns the loading ring, names it for
// assistive technology, and turns it in steps rather than at the
// display's rate; it stands still under Reduce Motion, and asks for no
// frames once the app has loaded.
func TestConnectingShowsTheLoadingRing(t *testing.T) {
	a := testApp(t)
	a.loaded, a.conn, a.selected = false, connConnecting, ""
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	r, ok := tt.Find(locales.StatusConnectingLabel())
	if !ok {
		t.Fatalf("the connecting card does not name what it shows; labels %q", tt.Texts())
	}
	// A ring 16 DIPs across, not a word that happens to say the same.
	if r.W < 15 || r.W > 17 || r.H < 15 || r.H > 17 {
		t.Fatalf("the connecting indicator is %v×%v, not the ring", r.W, r.H)
	}
	shot(t, tt, "connecting-ring")
	// The ring's steps come by Painter.After, which FramePending does not
	// count: the window is not asked for a frame each refresh.
	if tt.FramePending() {
		t.Error("the ring asks for a frame at the display's rate")
	}
	before := crop(tt, r)
	tt.Advance(ringFrame / 4)
	if !bytes.Equal(before, crop(tt, r)) {
		t.Error("the ring moved before its step was due")
	}
	tt.Advance(ringFrame)
	if bytes.Equal(before, crop(tt, r)) {
		t.Error("the ring did not turn")
	}

	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	still := crop(tt, r)
	tt.Advance(time.Second)
	if !bytes.Equal(still, crop(tt, r)) {
		t.Error("the ring turns under Reduce Motion")
	}
	if tt.FramePending() {
		t.Error("the ring asks for frames under Reduce Motion")
	}
	tt.SetPreferences(ui.Preferences{TextScale: 1})

	a.loaded = true
	tt.Frame()
	tt.Advance(time.Second)
	if _, ok := tt.Find(locales.StatusConnectingLabel()); ok {
		t.Error("the ring stayed after the app loaded")
	}
	if tt.FramePending() {
		t.Error("frames go on after the ring has gone")
	}
}

// paints counts how often the window is painted in a second of a view
// showing ring, in a box that draws beside it.
func paints(t *testing.T, reduce bool, ring func(c *ui.Context, pal *palette)) int {
	t.Helper()
	pal := lightPalette
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Size(80, 40).Children(func() {
			ring(c, &pal)
			ui.Box(c).Size(10, 10).Draw(func(*ui.Painter, ui.Rect) { n++ })
		})
	}, 80, 40)
	tt.SetPreferences(ui.Preferences{ReduceMotion: reduce, TextScale: 1})
	tt.Frame()
	n = 0
	tt.Advance(time.Second)
	return n
}

// The rings that turn — the busy views' and a working session's — paint
// the window again 15 times a second, not 60, and not at all under
// Reduce Motion; nothing else paints it while nothing turns.
func TestLoadingRingThrottled(t *testing.T) {
	rings := map[string]func(c *ui.Context, pal *palette){
		"busy": func(c *ui.Context, pal *palette) {
			ui.Box(c).Size(40, 40).Children(func() { working(c, pal, "Busy") })
		},
		"working session": func(c *ui.Context, pal *palette) { statusIconP(c, pal, proto.Working, false, -1, 0, 15) },
		"done session":    func(c *ui.Context, pal *palette) { statusIconP(c, pal, proto.Idle, false, -1, 0, 15) },
	}
	for name, ring := range rings {
		n := paints(t, false, ring)
		switch {
		case name == "done session" && n != 0:
			t.Errorf("a mark that does not turn painted %d times in a second", n)
		case name != "done session" && (n < 10 || n > 20):
			t.Errorf("the %s ring painted %d times in a second, want about %d", name, n, time.Second/ringFrame)
		}
		if n := paints(t, true, ring); n != 0 {
			t.Errorf("the %s ring painted %d times under Reduce Motion", name, n)
		}
	}
}
