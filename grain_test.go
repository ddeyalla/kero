package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
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
				grain.Draw(p, r, shader.Options{Uniforms: map[string]any{"Alpha": a}})
			})
		}, 600, 320)
		tt.Frame()
		img := tt.Image()
		var varied bool
		first := img.RGBAAt(0, 0)
		for y := 0; y < 20; y++ {
			for x := 0; x < 300; x++ {
				pixel := img.RGBAAt(x, y)
				varied = varied || pixel != first
				if pixel != img.RGBAAt(x+300, y) || pixel != img.RGBAAt(x, y+300) {
					t.Fatalf("grain does not repeat at 300 points: (%d,%d)", x, y)
				}
			}
		}
		if !varied {
			t.Fatal("grain drew a flat fill")
		}
	}
}
