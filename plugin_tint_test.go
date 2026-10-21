package main

import (
	"image"
	"image/color"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// pluginTestImage is a small picture of one color.
func pluginTestImage(c ui.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
	return img
}

// pluginTintTree is a tree with one tinted picture, from src.
func pluginTintTree(src string) *protocol.Tree {
	return pluginTree(sdk.Column("root",
		sdk.Image("pic", src).Tint(),
		sdk.Text("note", "A note"),
	))
}

// pluginTintKey names a source the way the tests' picture loader does.
func pluginTintKey(src pluginSource) string {
	if src.data != "" {
		return src.data
	}
	return src.url
}

// newPluginTintHarness starts a pane and swaps the picture loader for one
// that returns a picture of the source's own color, counted.
func newPluginTintHarness(t *testing.T, colors map[string]ui.Color, calls *int32) *pluginHarness {
	t.Helper()
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.Text("note", "A note"))), nil)
	h.view.fetch = func(src pluginSource) (*ui.Bitmap, image.Image, error) {
		if calls != nil {
			atomic.AddInt32(calls, 1)
		}
		c := colors[pluginTintKey(src)]
		img := pluginTestImage(c)
		return ui.NewBitmap(img), img, nil
	}
	return h
}

// TestPluginDominantColor reads the color that covers most of a picture.
func TestPluginDominantColor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x < 6 {
				img.Set(x, y, color.RGBA{R: 210, G: 60, B: 40, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 30, G: 40, B: 200, A: 255})
			}
		}
	}
	got := pluginDominantColor(img)
	for _, d := range []struct {
		name string
		got  uint8
		want uint8
	}{
		{"red", got.R, 210},
		{"green", got.G, 60},
		{"blue", got.B, 40},
	} {
		if diff := int(d.got) - int(d.want); diff > 12 || diff < -12 {
			t.Errorf("dominant %s = %d, want about %d (whole color %v)", d.name, d.got, d.want, got)
		}
	}
	if got := pluginDominantColor(image.NewRGBA(image.Rect(0, 0, 0, 0))); got.A != 0 {
		t.Errorf("empty picture color = %v, want transparent", got)
	}
}

// TestPluginTintClampKeepsContrast holds the tint in the band the text can
// sit on, in both appearances.
func TestPluginTintClampKeepsContrast(t *testing.T) {
	sources := []ui.Color{
		ui.RGB(255, 255, 255),
		ui.RGB(0, 0, 0),
		ui.RGB(210, 60, 40),
		ui.RGB(60, 210, 40),
		ui.RGB(40, 60, 210),
		ui.RGB(128, 128, 128),
	}
	for _, src := range sources {
		light := pluginTintClamp(src, false)
		dark := pluginTintClamp(src, true)
		if l := pluginLuma(light); l < 0.60 || l > 0.90 {
			t.Errorf("light tint of %v has luma %v, want 0.60..0.90", src, l)
		}
		if l := pluginLuma(dark); l < 0.04 || l > 0.18 {
			t.Errorf("dark tint of %v has luma %v, want 0.04..0.18", src, l)
		}
		if pluginLuma(dark) >= pluginLuma(light) {
			t.Errorf("%v: dark tint is not darker than light tint", src)
		}
	}
	if got := pluginTintClamp(ui.Transparent, false); got.A != 0 {
		t.Errorf("transparent picture tint = %v, want transparent", got)
	}
}

// TestPluginTintSamplesOncePerPicture reads the picture's color once, at
// the picture, and keeps it.
func TestPluginTintSamplesOncePerPicture(t *testing.T) {
	var calls int32
	h := newPluginTintHarness(t, map[string]ui.Color{
		"red": ui.RGB(210, 60, 40),
	}, &calls)
	h.show(pluginTintTree("red"))
	if !waitPluginPicture(t, h.view, pluginSource{url: "red"}) {
		t.Fatal("the picture did not load")
	}
	h.frames()
	h.frames()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("picture fetches = %d, want 1", got)
	}
	if !h.view.tinted {
		t.Fatal("the pane took no tint from the picture")
	}
	want := pluginTintClamp(ui.RGB(210, 60, 40), false)
	if h.view.tintTarget != want {
		t.Errorf("tint = %v, want the clamped picture color %v", h.view.tintTarget, want)
	}
}

// TestPluginTintClampsInBothAppearances draws the same picture in the light
// and the dark appearance: the wash stays in the band of each.
func TestPluginTintClampsInBothAppearances(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h := newPluginTintHarness(t, map[string]ui.Color{
			"green": ui.RGB(60, 210, 40),
		}, nil)
		h.pal = lightPalette
		if dark {
			h.pal = darkPalette
		}
		h.show(pluginTintTree("green"))
		if !waitPluginPicture(t, h.view, pluginSource{url: "green"}) {
			t.Fatal("the picture did not load")
		}
		h.frames()
		if !h.view.tinted {
			t.Fatalf("dark=%v: the pane took no tint", dark)
		}
		l := pluginLuma(h.view.tintTarget)
		if dark {
			if l < 0.04 || l > 0.18 {
				t.Errorf("dark appearance luma = %v, want 0.04..0.18", l)
			}
		} else if l < 0.60 || l > 0.90 {
			t.Errorf("light appearance luma = %v, want 0.60..0.90", l)
		}
	}
}

// TestPluginTintCrossfadeSettles moves the wash from one picture's color to
// the next, and settles on it.
func TestPluginTintCrossfadeSettles(t *testing.T) {
	h := newPluginTintHarness(t, map[string]ui.Color{
		"red":  ui.RGB(210, 60, 40),
		"blue": ui.RGB(40, 60, 210),
	}, nil)
	red, blue := "red", "blue"
	h.show(pluginTintTree(red))
	if !waitPluginPicture(t, h.view, pluginSource{url: red}) {
		t.Fatal("the red picture did not load")
	}
	h.frames()
	if got, want := h.view.tintShown, h.view.tintTarget; got != want {
		t.Fatalf("red wash = %v, want %v", got, want)
	}
	h.show(pluginTintTree(blue))
	if !waitPluginPicture(t, h.view, pluginSource{url: blue}) {
		t.Fatal("the blue picture did not load")
	}
	h.frames()
	if got, want := h.view.tintShown, h.view.tintTarget; got != want {
		t.Fatalf("blue wash = %v, want %v", got, want)
	}
	// Back to red, settled, then to blue: the wash starts from red.
	h.show(pluginTintTree(red))
	h.frames()
	want := h.view.tintTarget
	if want != pluginTintClamp(ui.RGB(210, 60, 40), false) {
		t.Fatalf("red target = %v", want)
	}
	h.tree = pluginTintTree(blue)
	h.tt.Frame()
	if h.view.tintShown == h.view.tintTarget {
		t.Fatalf("the wash jumped to %v instead of crossfading", h.view.tintTarget)
	}
	h.tt.Advance(slide.Duration + 100*time.Millisecond)
	h.tt.Frame()
	if got, want := h.view.tintShown, h.view.tintTarget; got != want {
		t.Errorf("after the crossfade the wash = %v, want %v", got, want)
	}
	if h.view.tintTarget != pluginTintClamp(ui.RGB(40, 60, 210), false) {
		t.Errorf("blue target = %v", h.view.tintTarget)
	}
}
