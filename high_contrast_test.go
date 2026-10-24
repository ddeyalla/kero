package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/settings"
)

// maxContrast returns the strongest contrast any pixel in the label's
// bounds has with the surface behind it: the rendered text at its core,
// rather than a lone antialiased edge.
func maxContrast(t *testing.T, tt *ui.Tester, label string, bg ui.Color) float64 {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no %q to sample", label)
	}
	img := tt.Image()
	best := 1.0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			c := wcagContrast(ui.Color{R: p.R, G: p.G, B: p.B, A: p.A}, bg)
			if c > best {
				best = c
			}
		}
	}
	return best
}

// edgeContrast returns the contrast of a box's top edge with its fill just
// inside it: the boundary a control or card draws.
func edgeContrast(t *testing.T, tt *ui.Tester, label string) float64 {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no %q to sample", label)
	}
	img := tt.Image()
	at := func(x, y int) ui.Color {
		p := img.RGBAAt(x, y)
		return ui.Color{R: p.R, G: p.G, B: p.B, A: p.A}
	}
	x := int(r.X + r.W/2)
	return wcagContrast(at(x, int(r.Y)), at(x, int(r.Y)+6))
}

// over composites a translucent color over an opaque one.
func over(fg, bg ui.Color) ui.Color {
	a := float32(fg.A) / 255
	mix := func(f, b uint8) uint8 { return uint8(float32(f)*a + float32(b)*(1-a) + 0.5) }
	return ui.RGB(mix(fg.R, bg.R), mix(fg.G, bg.G), mix(fg.B, bg.B))
}

// hcFixture draws the opaque pieces High Contrast owns: muted and faint
// text, a card's boundary, and a styled field's boundary, on the sidebar.
// It also records the frame's palette.
func hcFixture(a *App, pal **palette) func(*ui.Context) {
	return func(c *ui.Context) {
		p := a.paletteFor(c)
		*pal = p
		ui.Column(c).Fill().Padding(16).Gap(8).Background(p.sidebar).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "Muted").TextColor(p.textMuted).FontSize(13).SingleLine()
			ui.Text(c, "Faint").TextColor(p.textFaint).FontSize(12).SingleLine()
			ui.Box(c.Key("card")).Label("BoundaryCard").Size(200, 54).Radius(8).
				Background(p.card).Border(1, p.cardBorder)
			frame := ui.Box(c.Key("field")).Label("BoundaryField").Size(200, controlH)
			field(c, frame, false, p)
		})
	}
}

// Increase Contrast strengthens muted and faint text and the opaque
// boundaries of cards and fields, in both appearances, without changing
// the accent, the card or the ordinary appearance. Turning it off returns
// the exact palette and pixels of the ordinary frame: no cache leak.
func TestHighContrastImprovesTextAndBoundaries(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			a.set = settings.Default()
			a.set.Accent = "plum"
			var pal *palette
			tt := ui.NewTester(hcFixture(a, &pal), 260, 240)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{TextScale: 1})
			frames(tt)

			normal := pal
			normalMuted := maxContrast(t, tt, "Muted", normal.sidebar)
			normalFaint := maxContrast(t, tt, "Faint", normal.sidebar)
			normalCard := edgeContrast(t, tt, "BoundaryCard")
			normalField := edgeContrast(t, tt, "BoundaryField")

			tt.SetPreferences(ui.Preferences{TextScale: 1, HighContrast: true})
			frames(tt)
			hc := pal
			if hc == normal {
				t.Fatal("High Contrast returned the ordinary palette")
			}
			hcMuted := maxContrast(t, tt, "Muted", hc.sidebar)
			hcFaint := maxContrast(t, tt, "Faint", hc.sidebar)
			hcCard := edgeContrast(t, tt, "BoundaryCard")
			hcField := edgeContrast(t, tt, "BoundaryField")

			if hcMuted < 4.5 || hcMuted < normalMuted*1.2 {
				t.Errorf("High Contrast muted text %.2f, ordinary %.2f; want >=4.5 and a visible rise", hcMuted, normalMuted)
			}
			if hcFaint < 4.5 || hcFaint < normalFaint*1.5 {
				t.Errorf("High Contrast faint text %.2f, ordinary %.2f; want >=4.5 and a visible rise", hcFaint, normalFaint)
			}
			if hcCard < 2.5 || hcCard < normalCard*1.5 {
				t.Errorf("High Contrast card edge %.2f, ordinary %.2f", hcCard, normalCard)
			}
			if hcField < 3 || hcField < normalField*1.5 {
				t.Errorf("High Contrast field edge %.2f, ordinary %.2f", hcField, normalField)
			}
			if hc.accent != normal.accent {
				t.Errorf("High Contrast changed the accent: %v -> %v", normal.accent, hc.accent)
			}
			if hc.card != normal.card {
				t.Errorf("High Contrast changed the card: %v -> %v", normal.card, hc.card)
			}
			if hc.text != normal.text {
				t.Errorf("High Contrast changed the body text: %v -> %v", normal.text, hc.text)
			}
			// The boundaries are the colors themselves, not a one-off
			// stronger fill: contrast against the surface they sit on.
			if v := wcagContrast(over(hc.cardBorder, hc.card), hc.card); v < 2.5 {
				t.Errorf("High Contrast card border is %.2f on the card", v)
			}
			if v := wcagContrast(over(hc.controlBorder, ui.Hex("#ffffff")), ui.Hex("#ffffff")); !dark && v < 3 {
				t.Errorf("High Contrast field border is %.2f on white", v)
			}

			tt.SetPreferences(ui.Preferences{TextScale: 1})
			frames(tt)
			if pal != normal {
				t.Fatalf("turning High Contrast off returned %p, want the ordinary palette %p", pal, normal)
			}
			if got := maxContrast(t, tt, "Muted", pal.sidebar); got != normalMuted {
				t.Errorf("ordinary muted text changed after High Contrast: %.2f -> %.2f", normalMuted, got)
			}
			if got := edgeContrast(t, tt, "BoundaryCard"); got != normalCard {
				t.Errorf("ordinary card edge changed after High Contrast: %.2f -> %.2f", normalCard, got)
			}
		})
	}
}

// High Contrast keeps a selected accent, and an ambient color still wins
// the accent while the muted text stays strengthened, in both appearances.
func TestHighContrastKeepsSelectedAndAmbientAccent(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			a.set = settings.Default()
			a.set.Accent = "plum"
			var pal *palette
			tt := ui.NewTester(hcFixture(a, &pal), 260, 240)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{TextScale: 1, HighContrast: true})
			frames(tt)
			spec := accentSpecs["plum"].light
			if dark {
				spec = accentSpecs["plum"].dark
			}
			if pal.accent != spec {
				t.Fatalf("High Contrast accent is %v, not Plum", pal.accent)
			}
			if muted := maxContrast(t, tt, "Muted", pal.sidebar); muted < 4.5 {
				t.Errorf("High Contrast muted text is %.2f", muted)
			}

			a.set.AmbientColor = true
			a.ambient.target = ui.Hex("#2f9e63")
			a.ambient.last = a.ambient.target
			frames(tt)
			tt.Advance(ambientFade + 100*time.Millisecond)
			tt.Frame()
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
			if pal.accent == spec {
				t.Fatal("the ambient color did not reach the accent")
			}
			if pal.textMuted != highContrastPal(a.accentPal(dark)).textMuted {
				t.Error("the ambient blend changed High Contrast's muted text")
			}
			if pal.focusEdge.A != 255 {
				t.Errorf("High Contrast focus edge alpha is %d", pal.focusEdge.A)
			}
		})
	}
}

// The High Contrast variant is cached per ordinary accent palette:
// switching presets gives that preset's variant, switching back returns
// the first, another app instance gets the same preset's variant, and the
// ordinary palettes are not mutated. Both appearances.
func TestHighContrastPaletteCacheIsPerPreset(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			a.set = settings.Default()
			a.set.Accent = "plum"
			plum := highContrastPal(a.accentPal(dark))
			a.set.Accent = "sea"
			sea := highContrastPal(a.accentPal(dark))
			a.set.Accent = "plum"
			again := highContrastPal(a.accentPal(dark))

			b := testApp(t)
			b.set = settings.Default()
			b.set.Accent = "sea"
			otherApp := highContrastPal(b.accentPal(dark))

			if plum == sea || plum == otherApp {
				t.Fatal("two presets or app instances share one High Contrast palette")
			}
			wantPlum, wantSea := accentSpecs["plum"].light, accentSpecs["sea"].light
			if dark {
				wantPlum, wantSea = accentSpecs["plum"].dark, accentSpecs["sea"].dark
			}
			if plum.accent != wantPlum || sea.accent != wantSea || otherApp.accent != wantSea {
				t.Errorf("preset accents: plum %v, sea %v, other app %v", plum.accent, sea.accent, otherApp.accent)
			}
			if again != plum {
				t.Error("the plum variant is not cached")
			}
			base, wantMuted := lightPalette, ui.Hex("#6e6e78")
			if dark {
				base, wantMuted = darkPalette, ui.Hex("#a9a9b4")
			}
			if base.textMuted != wantMuted || base.highContrast || base.cardBorder.A == plum.cardBorder.A {
				t.Error("the ordinary palette was mutated")
			}
		})
	}
}

// With Reduce Transparency, controls fall back to opaque faces; High
// Contrast still strengthens their boundary, in both appearances, and the
// ordinary fallback is unchanged after a High Contrast frame.
func TestHighContrastOpaqueControlFallback(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			a.set = settings.Default()
			tt := ui.NewTester(func(c *ui.Context) {
				p := a.paletteFor(c)
				ui.Column(c).Fill().Padding(20).Background(p.sidebar).Children(func() {
					e := ui.Box(c.Key("control")).Label("BoundaryControl").Size(180, controlH).Radius(8)
					controlFace(c, e, p, p.accent.Alpha(0.1))
				})
			}, 240, 100)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{TextScale: 1, ReduceTransparency: true})
			frames(tt)
			normal := edgeContrast(t, tt, "BoundaryControl")
			tt.SetPreferences(ui.Preferences{TextScale: 1, ReduceTransparency: true, HighContrast: true})
			frames(tt)
			hc := edgeContrast(t, tt, "BoundaryControl")
			if hc < 1.6 || hc < normal*1.3 {
				t.Errorf("High Contrast opaque control edge %.2f, ordinary %.2f", hc, normal)
			}
			tt.SetPreferences(ui.Preferences{TextScale: 1, ReduceTransparency: true})
			frames(tt)
			if got := edgeContrast(t, tt, "BoundaryControl"); got != normal {
				t.Errorf("ordinary opaque control changed after High Contrast: %.2f -> %.2f", normal, got)
			}
		})
	}
}

// rectEdgeContrast returns the contrast of a rendered rectangle's top
// edge with its body.
func rectEdgeContrast(tt *ui.Tester, r ui.Rect) float64 {
	img := tt.Image()
	at := func(x, y int) ui.Color {
		p := img.RGBAAt(x, y)
		return ui.Color{R: p.R, G: p.G, B: p.B, A: p.A}
	}
	x := int(r.X + r.W/2)
	return wcagContrast(at(x, int(r.Y)), at(x, int(r.Y+r.H/2)))
}

// topEdgeColor returns the most saturated pixel in the top few rows of a
// rectangle: the focused pane's accent edge where it is drawn, or the
// neutral rim of an unfocused one.
func topEdgeColor(tt *ui.Tester, r ui.Rect) ui.Color {
	best, col := -1, ui.Color{}
	img := tt.Image()
	for y := int(r.Y); y <= int(r.Y)+2; y++ {
		for x := int(r.X+r.W/2) - 24; x <= int(r.X+r.W/2)+24; x++ {
			p := img.RGBAAt(x, y)
			c := ui.Color{R: p.R, G: p.G, B: p.B, A: p.A}
			spread := max(int(c.R), max(int(c.G), int(c.B))) - min(int(c.R), min(int(c.G), int(c.B)))
			if spread > best {
				best, col = spread, c
			}
		}
	}
	return col
}

// The real pane cards, through paneCard's raised path, gain a visible
// light boundary under Increase Contrast. The focused and unfocused cards
// stay distinguishable, and the focus still moves its edge.
func TestHighContrastPaneCardBoundary(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			tt := ui.NewTester(a.view, 1100, 640)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{TextScale: 1})
			frames(tt)
			unfocused := a.cardSize["b"]
			normal := rectEdgeContrast(tt, unfocused)

			tt.SetPreferences(ui.Preferences{TextScale: 1, HighContrast: true})
			frames(tt)
			hc := rectEdgeContrast(tt, a.cardSize["b"])
			if hc < 2.5 || hc < normal*1.5 {
				t.Errorf("High Contrast pane card edge %.2f, ordinary %.2f", hc, normal)
			}

			focused, plain := topEdgeColor(tt, a.cardSize["a"]), topEdgeColor(tt, a.cardSize["b"])
			if focused == plain {
				t.Errorf("the focused and unfocused pane edges are the same in High Contrast: %v", focused)
			}
			a.setFocus(&a.st.Sessions[0], "b")
			frames(tt)
			if topEdgeColor(tt, a.cardSize["b"]) == plain || topEdgeColor(tt, a.cardSize["a"]) == focused {
				t.Error("the High Contrast focus edge did not move")
			}
		})
	}
}

// Turning Increase Contrast on and off must not move pane content: the
// light rim is painted inside the card, not a Border that takes a point.
func TestHighContrastPaneCardsKeepContentGeometry(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	type geometry struct {
		cards map[string]ui.Rect
		fish  ui.Rect
		zsh   ui.Rect
	}
	capture := func() geometry {
		frames(tt)
		g := geometry{cards: map[string]ui.Rect{}}
		for id, r := range a.cardSize {
			g.cards[id] = r
		}
		g.fish, _ = tt.Find("fish")
		g.zsh, _ = tt.Find("zsh")
		return g
	}
	plain := capture()
	tt.SetPreferences(ui.Preferences{TextScale: 1, HighContrast: true})
	hc := capture()
	if len(hc.cards) != len(plain.cards) {
		t.Fatalf("pane count changed: %v vs %v", plain.cards, hc.cards)
	}
	for id, r := range plain.cards {
		if hc.cards[id] != r {
			t.Errorf("pane %s moved under High Contrast: %v -> %v", id, r, hc.cards[id])
		}
	}
	if hc.fish != plain.fish || hc.zsh != plain.zsh {
		t.Errorf("pane content moved under High Contrast: headers %v/%v -> %v/%v", plain.fish, plain.zsh, hc.fish, hc.zsh)
	}
	tt.SetPreferences(ui.Preferences{TextScale: 1})
	back := capture()
	for id, r := range plain.cards {
		if back.cards[id] != r {
			t.Errorf("pane %s did not return: %v -> %v", id, r, back.cards[id])
		}
	}
	if back.fish != plain.fish || back.zsh != plain.zsh {
		t.Error("pane content did not return after High Contrast")
	}
}
