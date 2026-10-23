package main

import (
	"github.com/egoist/mygo/ui"
	"os"
	"testing"
	"time"
)

// Toolbar grouping preserves compact hit targets and the system's opaque
// fallback; material changes do not change the navigation's actions.
func TestGlassToolbarActionsAndFallback(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, reduce := range []bool{false, true} {
			pal := lightPalette
			if dark {
				pal = darkPalette
			}
			clicked := 0
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Background(pal.card).Children(func() {
					toolbarGroup(c, "tools", &pal, func() {
						iconButton(c, "back", goBackIcon, "Back", &pal, func() { clicked++ })
						iconButton(c, "next", goFwdIcon, "Forward", &pal, func() { clicked += 10 })
					})
				})
			}, 200, 60)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceTransparency: reduce, TextScale: 1})
			tt.Frame()
			back, ok := tt.Find("Back")
			if !ok {
				t.Fatal("no Back action")
			}
			if back.W < 28 || back.H < 26 {
				t.Fatalf("hit target = %v", back)
			}
			tt.Click("Back")
			tt.Frame()
			if clicked != 1 {
				t.Fatalf("action count = %d", clicked)
			}
		}
	}
}

// Actual app surfaces are captured by MyGo's tester, never screen capture.
func TestGlassToolbarShots(t *testing.T) {
	if dir := os.Getenv("KERO_GLASS_SHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		a := testBrowser(t)
		tt := ui.NewTester(a.view, 1100, 720)
		tt.SetDark(dark)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		glassShot(t, tt, "toolbars-browser", dark)
		b := testAttention(t, attnFixture{state: "waiting-user", msg: "Needs approval", agent: "Claude"})
		at := ui.NewTester(b.view, 1100, 720)
		at.SetDark(dark)
		at.Frame()
		at.Advance(time.Second)
		at.Frame()
		glassShot(t, at, "toolbars-attention", dark)
		a.openSettings("")
		tt.Frame()
		glassShot(t, tt, "toolbars-settings", dark)
	}
}

// Edge effects overlay the viewport and never take pointer events from rows.
func TestGlassToolbarScrollRowsRemainClickable(t *testing.T) {
	sc := ui.ScrollState{}
	clicked := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			scrollUnderBar(c, "rows", &sc, lightPalette.card, func(list ui.Element) {
				list.Children(func() {
					for i := 0; i < 20; i++ {
						b := ui.ButtonBase(c.Key(i)).Height(24).Shrink(0).Label("Row")
						if b.Clicked() {
							clicked = true
						}
					}
				})
			})
		})
	}, 200, 100)
	tt.Frame()
	sc.Y = 25
	tt.Frame()
	tt.Frame()
	if sc.Y <= 0 || sc.MaxY <= sc.Y {
		t.Fatalf("scroll range = %v", sc)
	}
	tt.Press(100, 5)
	tt.Release(100, 5)
	tt.Frame()
	if !clicked {
		t.Fatal("soft edge swallowed row click")
	}
}
