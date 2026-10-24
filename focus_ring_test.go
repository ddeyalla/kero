package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/settings"
)

// focusRingSample returns the pixel around a slider's knob that stands
// farthest from the background, and that distance: the keyboard focus
// ring, where one is drawn, and how strongly. Only the bands above and
// below the knob are read: the filled track runs through its middle and
// would pass for the ring.
func focusRingSample(tt *ui.Tester, bounds ui.Rect, value float32, bg ui.Color) (ui.Color, int) {
	const knobW, knobH = 20, 16
	x0, x1 := bounds.X+knobW/2, bounds.X+bounds.W-knobW/2
	cx := x0 + (x1-x0)*value
	cy := bounds.Y + bounds.H/2
	best, bestD := ui.Color{}, -1
	for y := int(cy-knobH/2) - 6; y <= int(cy+knobH/2)+6; y++ {
		if d := y - int(cy); d > -knobH/2-1 && d < knobH/2+1 {
			continue
		}
		for x := int(cx-knobW/2) - 6; x <= int(cx+knobW/2)+6; x++ {
			p := tt.Image().RGBAAt(x, y)
			d := iabs(int(p.R)-int(bg.R)) + iabs(int(p.G)-int(bg.G)) + iabs(int(p.B)-int(bg.B))
			if d > bestD {
				best, bestD = ui.Color{R: p.R, G: p.G, B: p.B, A: p.A}, d
			}
		}
	}
	return best, bestD
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// focusedSlider renders one slider in kero's own control style, with the
// same theme seam the view applies, and gives it the keyboard focus with
// Tab, as a keyboard user does. It returns the ring pixel and its distance
// from the card.
func focusedSlider(t *testing.T, accent string, dark, contrast bool) (ui.Color, int, uint8) {
	t.Helper()
	a := testApp(t)
	a.set = settings.Default()
	a.set.Accent = accent
	level := 0.5
	var visible bool
	var focusA uint8
	tt := ui.NewTester(func(c *ui.Context) {
		pal := a.paletteFor(c)
		c.SetTheme(borderless(c, pal))
		focusA = c.Theme().Focus.A
		ui.Column(c).Fill().Padding(40).Background(pal.card).Children(func() {
			el := slider(c, "level", &level, 0, 1, 220, pal).Label("Level")
			visible = el.FocusVisible()
		})
	}, 320, 160)
	tt.SetDark(dark)
	tt.SetPreferences(ui.Preferences{TextScale: 1, HighContrast: contrast})
	frames(tt)
	tt.Key(0, ui.KeyTab)
	frames(tt)
	if !visible {
		t.Fatal("the slider did not take visible keyboard focus")
	}
	r, ok := tt.Find("Level")
	if !ok {
		t.Fatal("no slider")
	}
	bg := lightPalette.card
	if dark {
		bg = darkPalette.card
	}
	ring, d := focusRingSample(tt, r, float32(level), bg)
	return ring, d, focusA
}

// A shared control that draws MyGo's keyboard focus ring takes the chosen
// accent: it is not MyGo's system blue. The accent's own hue is checked
// light and dark; the default keeps its blue.
func TestSharedFocusRingTakesTheAccent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		accent string
		dark   bool
		hue    func(ui.Color) bool
	}{
		{"plum-light", "plum", false, func(c ui.Color) bool { return c.R > c.G+12 && c.B > c.G }},
		{"sea-light", "sea", false, func(c ui.Color) bool { return c.G > c.R+20 && c.B > c.R && c.B < c.G+45 }},
		{"plum-dark", "plum", true, func(c ui.Color) bool { return c.R > c.G+12 && c.B > c.G }},
		{"sea-dark", "sea", true, func(c ui.Color) bool { return c.G > c.R+20 && c.B > c.R && c.B < c.G+45 }},
		{"blue-light", settings.AccentDefault, false, func(c ui.Color) bool { return c.B > c.G+40 && c.G > c.R }},
		{"blue-dark", settings.AccentDefault, true, func(c ui.Color) bool { return c.B > c.G+30 && c.G > c.R }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring, d, _ := focusedSlider(t, tc.accent, tc.dark, false)
			if d < 8 {
				t.Fatalf("no focus ring around the slider: %v (%d from the card)", ring, d)
			}
			if !tc.hue(ring) {
				t.Errorf("the focus ring is %v, not the %s accent", ring, tc.accent)
			}
		})
	}
}

// Increase Contrast asks for a full-opacity ring; the accent changes its
// hue, not the desktop's opacity.
func TestSharedFocusRingKeepsHighContrastOpacity(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			normal, nd, na := focusedSlider(t, "plum", dark, false)
			contrast, cd, ca := focusedSlider(t, "plum", dark, true)
			if na >= 255 {
				t.Errorf("the usual focus ring is already opaque: alpha %d", na)
			}
			if ca != 255 {
				t.Errorf("Increase Contrast focus alpha is %d, want 255", ca)
			}
			if !(contrast.R > contrast.G+12 && contrast.B > contrast.G) {
				t.Errorf("HighContrast ring is %v, not the plum accent", contrast)
			}
			if cd <= nd+6 {
				t.Errorf("HighContrast ring is not fuller: %d vs %d from the card (%v vs %v)", cd, nd, contrast, normal)
			}
		})
	}
}

// The accent can change while the app runs: the next frame's ring is the
// new accent, and the default after it is blue again, so no theme or
// palette cache keeps the old one.
func TestFocusRingAccentSwitchDoesNotLeak(t *testing.T) {
	a := testApp(t)
	a.set = settings.Default()
	a.set.Accent = "plum"
	level := 0.5
	tt := ui.NewTester(func(c *ui.Context) {
		pal := a.paletteFor(c)
		c.SetTheme(borderless(c, pal))
		ui.Column(c).Fill().Padding(40).Background(pal.card).Children(func() {
			slider(c, "level", &level, 0, 1, 220, pal).Label("Level")
		})
	}, 320, 160)
	tt.SetPreferences(ui.Preferences{TextScale: 1})
	frames(tt)
	tt.Key(0, ui.KeyTab)
	frames(tt)
	r, _ := tt.Find("Level")
	plum, _ := focusRingSample(tt, r, float32(level), lightPalette.card)
	if !(plum.R > plum.G+20) {
		t.Fatalf("the plum ring is %v", plum)
	}

	a.set.Accent = "sea"
	frames(tt)
	r, _ = tt.Find("Level")
	sea, _ := focusRingSample(tt, r, float32(level), lightPalette.card)
	if !(sea.G > sea.R+20 && sea.B > sea.R) {
		t.Errorf("after switching to sea the ring is %v", sea)
	}

	a.set.Accent = settings.AccentDefault
	frames(tt)
	r, _ = tt.Find("Level")
	blue, _ := focusRingSample(tt, r, float32(level), lightPalette.card)
	if !(blue.B > blue.G+40 && blue.G > blue.R) {
		t.Errorf("after returning to the default the ring is %v, not blue", blue)
	}
}
