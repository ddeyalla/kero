package main

// image.tint: the pane takes the color of the picture it shows, as Spotify
// colors a player with its album. The host reads the picture's color once,
// on the goroutine that already decodes it, keeps it with the picture, and
// fills the pane with it behind the plugin's parts. When the picture
// changes the color crossfades. The color is darkened into a band where
// light text keeps its contrast, and the parts on it draw light, in both
// appearances.

import (
	"image"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
)

// pluginTintSamples caps the pixels the dominant color reads on each side
// of the picture, so that a large picture costs no more than a small one.
const pluginTintSamples = 64

// pluginDominantColor returns the color a pane takes from img, ignoring
// transparent pixels. It prefers a vivid color that covers much of the
// picture to the paper white or the black around it, as a cover's red on
// a white sleeve; a picture with no vivid color gives the one that covers
// most of it. It averages the winning bucket, so that noise in a picture
// does not decide the pane's color.
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
	total := 0
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
			total++
		}
	}
	var best, vivid ui.Color
	bestN, vividScore := 0, float32(0)
	for _, k := range buckets {
		if k.n == 0 {
			continue
		}
		c := ui.RGB(uint8(k.r/k.n), uint8(k.g/k.n), uint8(k.b/k.n))
		if k.n > bestN {
			best, bestN = c, k.n
		}
		// A vivid color counts once it covers a twentieth of the
		// picture; then the more saturated and the wider, the better.
		sat, l := pluginSaturation(c), pluginLuma(c)
		if sat < 0.3 || l < 0.01 || l > 0.8 || k.n*20 < total {
			continue
		}
		if score := float32(k.n) * sat; score > vividScore {
			vivid, vividScore = c, score
		}
	}
	if bestN == 0 {
		return ui.Transparent
	}
	if vividScore > 0 {
		return vivid
	}
	return best
}

// pluginSaturation is the color's saturation, as HSV has it: 0 for a grey,
// 1 for a pure hue.
func pluginSaturation(c ui.Color) float32 {
	hi := max(c.R, c.G, c.B)
	lo := min(c.R, c.G, c.B)
	if hi == 0 {
		return 0
	}
	return float32(hi-lo) / float32(hi)
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

// pluginTintClamp darkens a tint, or lightens a black one, into the band
// where light text keeps its contrast, keeping its hue. The pane's parts
// draw light on it in both appearances.
func pluginTintClamp(c ui.Color) ui.Color {
	if c.A == 0 {
		return ui.Transparent
	}
	c.A = 255
	lo, hi := float32(0.03), float32(0.12)
	l := pluginLuma(c)
	for i := 0; i < 40 && l > hi; i++ {
		c = c.Mix(ui.RGB(0, 0, 0), 0.12)
		l = pluginLuma(c)
	}
	for i := 0; i < 40 && l < lo; i++ {
		c = c.Mix(ui.RGB(255, 255, 255), 0.08)
		l = pluginLuma(c)
	}
	return c
}

// pluginOnColor is the palette of the parts that stand on a tint: light
// text, and light faces for controls, rows and tracks, as on a dark pane
// of that color. The accent is white, as Spotify draws its play button and
// its progress on an album's color.
func pluginOnColor(pal *palette) *palette {
	on := *pal
	white := ui.RGB(255, 255, 255)
	on.dark = true
	on.text = white
	on.textMuted = white.Alpha(0.72)
	on.textFaint = white.Alpha(0.5)
	on.rowHover = white.Alpha(0.08)
	on.rowSelected = white.Alpha(0.14)
	on.control = white.Alpha(0.14)
	on.controlHover = white.Alpha(0.2)
	on.divider = white.Alpha(0.14)
	on.chip = white.Alpha(0.1)
	on.accent = white
	return &on
}

// treeTint finds the first tinted picture of the tree, and returns the
// color the pane takes from it, and whether that picture is still on its
// way: one that loads has no color yet, but the pane is still tinted.
func (v *pluginView) treeTint(n *protocol.Node) (tint ui.Color, ok, pending bool) {
	if n == nil {
		return ui.Transparent, false, false
	}
	if n.Type == protocol.NodeImage && n.Tint {
		return v.pictureTint(pluginImageSource(n))
	}
	for _, child := range n.Children {
		if tint, ok, pending := v.treeTint(child); ok || pending {
			return tint, ok, pending
		}
	}
	return ui.Transparent, false, false
}

// drawTint paints the picture's wash behind the plugin's parts, and
// crossfades it when the picture, and so the color, changes. While the
// next picture loads the wash holds the last one's color, so it does not
// fade out between two pictures and back in.
func (v *pluginView) drawTint(box ui.Element, color ui.Color, ok, pending bool) {
	switch {
	case ok:
		v.tintColor = color
	case pending:
		color = v.tintColor
	default:
		v.tintColor = ui.Transparent
		color = ui.Transparent
	}
	target := pluginTintClamp(color)
	shown := box.AnimateColor("tint", target, slide)
	v.tintTarget, v.tintShown = target, shown
	v.tinted = shown.A > 0
	if !v.tinted {
		return
	}
	// The color fills the pane, behind its parts, a little deeper towards
	// the bottom, where the browsing sits.
	box.Radius(14).LinearGradient(ui.LinearGradient{
		Angle: 180,
		Stops: []ui.GradientStop{
			{Color: shown, At: 0},
			{Color: shown.Mix(ui.RGB(0, 0, 0), 0.28).Alpha(float32(shown.A) / 255), At: 1},
		},
	})
}
