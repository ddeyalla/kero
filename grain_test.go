package main

import (
	"bytes"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"kero/internal/settings"
)

// The backdrop grain stays within its requested strength and repeats as
// one 300-point tile, instead of changing its pattern across a wide window.
func TestGrainStrengthAndTile(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(func(c *ui.Context) {
			a := grainAlpha(c, dark)
			if a < 0.02 || a > 0.04 {
				t.Errorf("grain alpha %v, want 2–4%%", a)
			}
			ui.Box(c).Fill().Background(ui.RGB(128, 128, 128)).Draw(func(p *ui.Painter, r ui.Rect) {
				grain().Draw(p, r, shader.Options{Uniforms: map[string]any{"Alpha": a}})
			})
		}, 600, 320)
		tt.Frame()
		img := tt.Image()
		var varied bool
		maxDeviation := 0
		first := img.RGBAAt(0, 0)
		for y := 0; y < 20; y++ {
			for x := 0; x < 300; x++ {
				pixel := img.RGBAAt(x, y)
				deviation := int(pixel.R) - 128
				if deviation < 0 {
					deviation = -deviation
				}
				maxDeviation = max(maxDeviation, deviation)
				varied = varied || pixel != first
				if pixel != img.RGBAAt(x+300, y) || pixel != img.RGBAAt(x, y+300) {
					t.Fatalf("grain does not repeat at 300 points: (%d,%d)", x, y)
				}
			}
		}
		if !varied {
			t.Fatal("grain drew a flat fill")
		}
		// At4% a white or black speck over gray can move a channel by
		// at most ceil(128*.04), with one value for raster rounding.
		if maxDeviation < 2 || maxDeviation > 6 {
			t.Fatalf("rendered grain deviates %d from gray, want visible grain within4%%", maxDeviation)
		}
	}
	// An ignored Alpha uniform would still draw grain at zero strength.
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(128, 128, 128)).Draw(func(p *ui.Painter, r ui.Rect) {
			grain().Draw(p, r, shader.Options{Uniforms: map[string]any{"Alpha": float32(0)}})
		})
	}, 32, 32)
	tt.Frame()
	img := tt.Image()
	for y := range 32 {
		for x := range 32 {
			if p := img.RGBAAt(x, y); p.R != 128 || p.G != 128 || p.B != 128 {
				t.Fatalf("zero-strength grain altered gray at %d,%d: %v", x, y, p)
			}
		}
	}
}

// A tester can draw without running the GUI startup prewarm. Switching
// grain off and back on must preserve both the wash and the shader's tile.
func TestBackdropGrainToggle(t *testing.T) {
	for _, pal := range []*palette{&lightPalette, &darkPalette} {
		s := settings.Default()
		s.Grain = false
		tt := ui.NewTester(func(c *ui.Context) {
			row := ui.Box(c).Fill()
			backdrop(c, pal, row, s)
		}, 320, 320)
		tt.Frame()
		off := bytes.Clone(tt.Image().Pix)
		s.Grain = true
		tt.Frame()
		on := bytes.Clone(tt.Image().Pix)
		if bytes.Equal(off, on) {
			t.Fatal("enabling grain did not change the backdrop")
		}
		s.Grain = false
		tt.Frame()
		if !bytes.Equal(off, tt.Image().Pix) {
			t.Fatal("disabling grain did not restore the wash")
		}
		s.Grain = true
		tt.Frame()
		if !bytes.Equal(on, tt.Image().Pix) {
			t.Fatal("reenabling grain changed its tile")
		}
	}
}
