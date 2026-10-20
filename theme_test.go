package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/settings"
)

// The accent, as the settings' bar holds it: a color of its own in the
// light and the dark, that stands out on the surfaces it is drawn on and
// that the text drawn on it reads on. An accent that fails one of these
// fails the theme, not the test's arithmetic.

// wcagLum is a color's WCAG relative luminance, 0 to 1.
func wcagLum(c ui.Color) float64 {
	f := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

// wcagContrast is the contrast of two colors, 1 to 21.
func wcagContrast(a, b ui.Color) float64 {
	la, lb := wcagLum(a), wcagLum(b)
	hi, lo := max(la, lb), min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

// Every accent stands at least 3 to 1 on the card and the sidebar, in
// the light and the dark, and the white text of a primary button reads
// on it at 4.5 to 1.
func TestAccentContrast(t *testing.T) {
	for _, ac := range settings.Accents {
		s, ok := accentSpecs[ac.ID]
		if !ok {
			t.Fatalf("accent %q has no colors", ac.ID)
		}
		for _, tc := range []struct {
			dark          bool
			accent        ui.Color
			card, sidebar ui.Color
		}{
			{false, s.light, lightPalette.card, lightPalette.sidebar},
			{true, s.dark, darkPalette.card, darkPalette.sidebar},
		} {
			name := ac.ID
			if tc.dark {
				name += " dark"
			}
			if v := wcagContrast(tc.accent, tc.card); v < 3 {
				t.Errorf("%s: accent on card is %.2f, want 3", name, v)
			}
			if v := wcagContrast(tc.accent, tc.sidebar); v < 3 {
				t.Errorf("%s: accent on sidebar is %.2f, want 3", name, v)
			}
			if v := wcagContrast(ui.Hex("#ffffff"), tc.accent); v < 4.5 {
				t.Errorf("%s: white text on accent is %.2f, want 4.5", name, v)
			}
		}
	}
}

// The default accent is the palettes' own: choosing it, as a new install
// has, shows what the app has had.
func TestDefaultAccentIsThePalettes(t *testing.T) {
	s := accentSpecs[settings.AccentDefault]
	if lightPalette.accent != s.light {
		t.Errorf("the light palette's accent %v is not the default's %v", lightPalette.accent, s.light)
	}
	if darkPalette.accent != s.dark {
		t.Errorf("the dark palette's accent %v is not the default's %v", darkPalette.accent, s.dark)
	}
}

// Every accent comes with a wash the window can show, and the terminals'
// selection of each reads on the card: a tint of it, not of nothing.
func TestAccentsWashesAndSelections(t *testing.T) {
	for _, ac := range settings.Accents {
		if _, ok := washTable[ac.Wash]; !ok {
			t.Errorf("accent %q comes with wash %q, which no wash paints", ac.ID, ac.Wash)
		}
		s := accentSpecs[ac.ID]
		if wcagContrast(s.lightSel, lightPalette.card) < 1.25 {
			t.Errorf("accent %q: the light selection is all but invisible on the card", ac.ID)
		}
		if wcagContrast(s.darkSel, darkPalette.card) < 1.25 {
			t.Errorf("accent %q: the dark selection is all but invisible on the card", ac.ID)
		}
	}
}

// A palette comes from the cache the same every time, and the accent's
// is the palette's but the accent.
func TestAccentPalCache(t *testing.T) {
	a := newApp()
	a.set = settings.Default()
	p := a.accentPal(false)
	if p != a.accentPal(false) {
		t.Fatal("two palettes for one frame")
	}
	a.set.Accent = "plum"
	q := a.accentPal(false)
	if q.accent != accentSpecs["plum"].light {
		t.Errorf("the plum palette's accent is %v", q.accent)
	}
	if q.text != p.text || q.card != p.card {
		t.Error("the accent changed more than the accent")
	}
	if a.accentPal(false) != q {
		t.Error("the plum palette is not cached")
	}
}
