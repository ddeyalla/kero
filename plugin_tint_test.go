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

// TestPluginTintClampKeepsContrast holds the tint in the dark band that
// light text sits on, whatever the picture's color.
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
		tint := pluginTintClamp(src)
		if l := pluginLuma(tint); l < 0.03 || l > 0.12 {
			t.Errorf("tint of %v has luma %v, want 0.03..0.12", src, l)
		}
		// White text keeps at least 4.5:1 on it.
		if ratio := 1.05 / (pluginLuma(tint) + 0.05); ratio < 4.5 {
			t.Errorf("tint of %v gives white text %.1f:1", src, ratio)
		}
	}
	// The hue stays: a red picture gives a red wash.
	if red := pluginTintClamp(ui.RGB(210, 60, 40)); red.R <= red.G || red.R <= red.B {
		t.Errorf("red tint = %v, want red to lead", red)
	}
	if got := pluginTintClamp(ui.Transparent); got.A != 0 {
		t.Errorf("transparent picture tint = %v, want transparent", got)
	}
}

// TestPluginDominantColorPrefersAVividColor picks the album's color, not
// the dark or grey ground most covers have.
func TestPluginDominantColorPrefersAVividColor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if y < 3 {
				img.Set(x, y, color.RGBA{R: 200, G: 30, B: 40, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 20, G: 20, B: 22, A: 255})
			}
		}
	}
	if got := pluginDominantColor(img); got.R < 150 || got.G > 80 {
		t.Errorf("dominant = %v, want the red that covers a third", got)
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
	if !waitPluginPicture(t, h.view, pluginSource{url: "red", tint: true}) {
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
	want := pluginTintClamp(ui.RGB(210, 60, 40))
	if h.view.tintTarget != want {
		t.Errorf("tint = %v, want the clamped picture color %v", h.view.tintTarget, want)
	}
}

// TestPluginTintClampsInBothAppearances draws the same picture in the light
// and the dark appearance: the wash stays in the same dark band.
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
		if !waitPluginPicture(t, h.view, pluginSource{url: "green", tint: true}) {
			t.Fatal("the picture did not load")
		}
		h.frames()
		if !h.view.tinted {
			t.Fatalf("dark=%v: the pane took no tint", dark)
		}
		// The wash is dark in both, so the parts on it draw light.
		if l := pluginLuma(h.view.tintTarget); l < 0.03 || l > 0.12 {
			t.Errorf("dark=%v: luma = %v, want 0.03..0.12", dark, l)
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
	if !waitPluginPicture(t, h.view, pluginSource{url: red, tint: true}) {
		t.Fatal("the red picture did not load")
	}
	h.frames()
	if got, want := h.view.tintShown, h.view.tintTarget; got != want {
		t.Fatalf("red wash = %v, want %v", got, want)
	}
	h.show(pluginTintTree(blue))
	if !waitPluginPicture(t, h.view, pluginSource{url: blue, tint: true}) {
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
	if want != pluginTintClamp(ui.RGB(210, 60, 40)) {
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
	if h.view.tintTarget != pluginTintClamp(ui.RGB(40, 60, 210)) {
		t.Errorf("blue target = %v", h.view.tintTarget)
	}
}

// TestPluginTintHoldsWhileNextPictureLoads keeps the wash at the last
// picture's color while the next picture loads, rather than fading it out
// between the two, then crossfades to the new picture's color.
func TestPluginTintHoldsWhileNextPictureLoads(t *testing.T) {
	block := make(chan struct{})
	h := newPluginTintHarness(t, map[string]ui.Color{
		"red":  ui.RGB(210, 60, 40),
		"blue": ui.RGB(40, 60, 210),
	}, nil)
	stub := h.view.fetch
	h.view.fetch = func(src pluginSource) (*ui.Bitmap, image.Image, error) {
		if src.url == "blue" {
			<-block
		}
		return stub(src)
	}
	defer func() {
		select {
		case <-block:
		default:
			close(block)
		}
	}()
	h.show(pluginTintTree("red"))
	if !waitPluginPicture(t, h.view, pluginSource{url: "red", tint: true}) {
		t.Fatal("the red picture did not load")
	}
	h.frames()
	red := pluginTintClamp(ui.RGB(210, 60, 40))
	if h.view.tintShown != red {
		t.Fatalf("red wash = %v, want %v", h.view.tintShown, red)
	}

	// The blue picture is on its way: the wash holds red, frame after
	// frame, and never dips towards transparent.
	h.show(pluginTintTree("blue"))
	for i := 0; i < 5; i++ {
		if h.view.tintTarget != red || h.view.tintShown != red || !h.view.tinted {
			t.Fatalf("while the next picture loads the wash is %v (target %v), want it held at %v", h.view.tintShown, h.view.tintTarget, red)
		}
		h.frames()
	}

	close(block)
	if !waitPluginPicture(t, h.view, pluginSource{url: "blue", tint: true}) {
		t.Fatal("the blue picture did not load")
	}
	h.tt.Frame()
	blue := pluginTintClamp(ui.RGB(40, 60, 210))
	if h.view.tintTarget != blue {
		t.Fatalf("once it arrives the target = %v, want %v", h.view.tintTarget, blue)
	}
	h.tt.Advance(slide.Duration + 100*time.Millisecond)
	h.tt.Frame()
	if h.view.tintShown != blue {
		t.Fatalf("after the crossfade the wash = %v, want %v", h.view.tintShown, blue)
	}

	// A tree without a tinted picture lets the wash go.
	h.show(pluginTree(sdk.Column("root", sdk.Text("note", "A note"))))
	h.tt.Advance(slide.Duration + 100*time.Millisecond)
	h.tt.Frame()
	if h.view.tinted || h.view.tintTarget.A != 0 {
		t.Fatalf("without a tinted picture the wash is %v, want none", h.view.tintShown)
	}
}

// TestPluginTintSampledOnlyForTintedPictures reads no color from a picture
// that does not tint the pane.
func TestPluginTintSampledOnlyForTintedPictures(t *testing.T) {
	h := newPluginTintHarness(t, map[string]ui.Color{"red": ui.RGB(210, 60, 40)}, nil)
	h.show(pluginTree(sdk.Column("root", sdk.Image("pic", "red"))))
	if !waitPluginPicture(t, h.view, pluginSource{url: "red"}) {
		t.Fatal("the picture did not load")
	}
	h.frames()
	v := h.view
	v.mu.Lock()
	pic := v.pictures[pluginSourceKey(pluginSource{url: "red"})]
	tinted := pic != nil && pic.tinted
	v.mu.Unlock()
	if tinted {
		t.Fatal("the pane read the color of a picture that does not tint it")
	}
	if v.tinted {
		t.Fatal("the pane is tinted by a picture that does not ask for it")
	}
}
