package main

// image.tint: the pane takes the color of the picture it shows. The host
// reads the picture's dominant color once, on the goroutine that already
// decodes it, keeps it with the picture, and washes it behind the plugin's
// parts. When the picture changes the wash crossfades. Its lightness is
// clamped so that the pane's text keeps its contrast, in both appearances.

import (
	"image"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
)

// pluginTintSamples caps the pixels the dominant color reads on each side
// of the picture, so that a large picture costs no more than a small one.
const pluginTintSamples = 64

// pluginDominantColor returns the color that covers most of img, ignoring
// transparent pixels. It averages the winning bucket, so that noise in a
// picture does not decide the pane's color.
func pluginDominantColor(img image.Image) ui.Color {
	if img == nil {
		return ui.Transparent
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ui.Transparent
	}
	step := 1
	if w > pluginTintSamples || h > pluginTintSamples {
		step = w
		if h > w {
			step = h
		}
		step /= pluginTintSamples
	}
	type bucket struct {
		r, g, b, n int
	}
	buckets := map[[3]uint8]*bucket{}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			key := [3]uint8{uint8(r >> 11), uint8(g >> 11), uint8(bl >> 11)}
			k := buckets[key]
			if k == nil {
				k = &bucket{}
				buckets[key] = k
			}
			k.r += int(r >> 8)
			k.g += int(g >> 8)
			k.b += int(bl >> 8)
			k.n++
		}
	}
	var best *bucket
	for _, k := range buckets {
		if k.n == 0 {
			continue
		}
		if best == nil || k.n > best.n {
			best = k
		}
	}
	if best == nil {
		return ui.Transparent
	}
	return ui.RGB(uint8(best.r/best.n), uint8(best.g/best.n), uint8(best.b/best.n))
}

// pluginLuma is the color's relative luminance: 0 for black, 1 for white.
func pluginLuma(c ui.Color) float32 {
	lin := func(v uint8) float32 {
		f := float32(v) / 255
		if f <= 0.04045 {
			return f / 12.92
		}
		return (f + 0.055) / 1.055 * ((f + 0.055) / 1.055) * ((f + 0.055) / 1.055)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// pluginTintClamp keeps a tint light in the light appearance, where the
// text is dark, and dark in the dark one, where the text is light, so the
// text that sits on the wash keeps its contrast.
func pluginTintClamp(c ui.Color, dark bool) ui.Color {
	if c.A == 0 {
		return ui.Transparent
	}
	c.A = 255
	lo, hi := float32(0.62), float32(0.88)
	if dark {
		lo, hi = 0.05, 0.16
	}
	l := pluginLuma(c)
	for i := 0; i < 40 && l > hi; i++ {
		c = c.Mix(ui.RGB(0, 0, 0), 0.2)
		l = pluginLuma(c)
	}
	for i := 0; i < 40 && l < lo; i++ {
		c = c.Mix(ui.RGB(255, 255, 255), 0.2)
		l = pluginLuma(c)
	}
	return c
}

// treeTint finds the first tinted picture of the tree, and returns the
// color the pane takes from it. A picture still loading has no color yet.
func (v *pluginView) treeTint(n *protocol.Node) (ui.Color, bool) {
	if n == nil {
		return ui.Transparent, false
	}
	if n.Type == protocol.NodeImage && n.Tint {
		if c, ok := v.pictureTint(pluginSource{url: n.URL, data: n.Data}); ok {
			return c, true
		}
		return ui.Transparent, false
	}
	for _, child := range n.Children {
		if c, ok := v.treeTint(child); ok {
			return c, true
		}
	}
	return ui.Transparent, false
}

// drawTint paints the picture's wash behind the plugin's parts, and
// crossfades it when the picture, and so the color, changes.
func (v *pluginView) drawTint(c *ui.Context, pal *palette, target ui.Color, ok bool) {
	if !ok {
		target = ui.Transparent
	} else {
		target = pluginTintClamp(target, pal.dark)
	}
	box := ui.Box(c.Key("plugin-tint")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Radius(14)
	shown := box.AnimateColor("tint", target, slide)
	v.tintTarget, v.tintShown = target, shown
	v.tinted = shown.A > 0
	if !v.tinted {
		return
	}
	box.LinearGradient(ui.LinearGradient{
		Angle: 180,
		Stops: []ui.GradientStop{
			{Color: shown.Alpha(0.30), At: 0},
			{Color: shown.Alpha(0), At: 1},
		},
	})
}
