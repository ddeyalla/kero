package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Translucent hover tints must not make the reduced-transparency fallback
// depend on content painted underneath the control.
func TestGlassControlOpaqueFallback(t *testing.T) {
	for _, dark := range []bool{false, true} {
		pal := lightPalette
		if dark {
			pal = darkPalette
		}
		var first ui.Color
		for i, background := range []ui.Color{ui.Hex("#ff0000"), ui.Hex("#0000ff")} {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Padding(24).Background(background).Children(func() {
					controlFace(c, ui.Box(c).Size(100, 30).Radius(15), &pal, pal.text.Alpha(0.1))
				})
			}, 160, 80)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceTransparency: true, TextScale: 1})
			tt.Frame()
			pixel := tt.Image().RGBAAt(60, 36)
			got := ui.Color{R: pixel.R, G: pixel.G, B: pixel.B, A: pixel.A}
			if i == 0 {
				first = got
			} else if got != first {
				t.Errorf("dark=%v: opaque control depends on backdrop: %v and %v", dark, first, got)
			}
		}
	}
}

// Styling keeps the standard base controls' keyboard, labels and edits intact.
// The held slider must remain opaque when Reduce Transparency is requested.
func TestGlassControlsInteraction(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, reduced := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/reduced=%v", dark, reduced), func(t *testing.T) {
				a := testApp(t)
				var on bool
				level := 0.5
				text := ""
				clicks := 0
				tt := ui.NewTester(func(c *ui.Context) {
					pal := a.paletteFor(c)
					ui.Column(c).Fill().Padding(24).Gap(16).Background(pal.sidebar).AlignItems(ui.Start).Children(func() {
						toggle(c.Key("toggle"), &on, pal).Label("Enabled")
						slider(c, "level", &level, 0, 1, 180, pal).Label("Level")
						textField(c, "name", &text, "Name", 180, pal)
						if button(c, "apply", "Apply", "", false, pal) {
							clicks++
						}
					})
				}, 280, 240)
				tt.SetDark(dark)
				tt.SetPreferences(ui.Preferences{TextScale: 1, ReduceTransparency: reduced})
				frames(tt)
				if err := tt.Click("Enabled"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if !on {
					t.Fatal("switch click did not toggle")
				}
				tt.Key(0, ui.KeySpace)
				tt.Frame()
				if on {
					t.Fatal("focused switch Space did not toggle")
				}
				if err := tt.Click("Name"); err != nil {
					t.Fatal(err)
				}
				tt.Type("hello")
				tt.Frame()
				if text != "hello" {
					t.Fatalf("field value %q", text)
				}
				if err := tt.Click("Apply"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if clicks != 1 {
					t.Fatalf("button fired %d times", clicks)
				}
				if err := tt.Click("Level"); err != nil {
					t.Fatal(err)
				}
				before := level
				tt.Key(0, ui.KeyRight)
				tt.Frame()
				if level <= before {
					t.Fatalf("slider keyboard did not advance: %v -> %v", before, level)
				}
				r, ok := tt.Find("Level")
				if !ok {
					t.Fatal("slider accessible label missing")
				}
				x, y := r.X+r.W/2, r.Y+r.H/2
				tt.Press(x, y)
				tt.Frame()
				tt.Advance(400 * time.Millisecond)
				tt.Frame()
				if reduced {
					pixel := tt.Image().RGBAAt(int(x), int(y))
					if pixel.R < 200 || pixel.G < 200 || pixel.B < 200 {
						t.Fatalf("held knob shows track with Reduce Transparency: %v", pixel)
					}
				}
				shot(t, tt, fmt.Sprintf("controls-held-dark-%v-reduced-%v", dark, reduced))
				tt.Release(x, y)
				tt.Frame()
			})
		}
	}
}
