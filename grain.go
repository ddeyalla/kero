package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// The grain: a faint speckle over the wash, so that large flat gradients
// stop reading as flat. One fragment shader, drawn on the backdrop after
// its background and before the sidebar and the cards, at a few percent.

const grainSrc = `package main

// Alpha is how much a speck covers at its strongest, 0 to 1: the grain's
// whole strength; half the specks lighten and half darken.
var Alpha float

// Fragment draws one repeating 300-point tile: a speck of white or black
// at each two points, from a hash of its cell. Premultiplied: white's
// color is its alpha.
func Fragment(dst vec4, pos vec2) vec4 {
	// The cells of the grain, in pixels of the window: two, so that a
	// speck reads at a display's scale instead of fizzling to nothing.
	p := floor(dst.xy / vec2(2.0, 2.0))
	p -= floor(p / vec2(150.0, 150.0)) * vec2(150.0, 150.0)
	h := fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453)
	n := h*2.0 - 1.0
	a := Alpha * abs(n)
	if n > 0.0 {
		return vec4(a, a, a, a)
	}
	return vec4(0.0, 0.0, 0.0, a)
}
`

// grain is the shader, compiled once; it draws in any window.
var grain = shader.MustCompile(grainSrc)

// grainAlpha is how strong the grain is, in the light and the dark, and
// lighter where the desktop shows through, which has a texture of its own.
func grainAlpha(c *ui.Context, dark bool) float32 {
	a := float32(0.033)
	if dark {
		a = 0.04
	}
	if c.Vibrancy() {
		a *= 0.7
	}
	return a
}
