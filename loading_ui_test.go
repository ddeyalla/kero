package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// A view that has nothing yet turns the loading ring, and names it for
// assistive technology.
func TestConnectingShowsTheLoadingRing(t *testing.T) {
	a := testApp(t)
	a.loaded, a.conn, a.selected = false, connConnecting, ""
	tt := ui.NewTester(a.view, 1000, 700)
	// One frame at a time: the ring asks for the next, and the tester
	// stops settling on its own.
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
}
