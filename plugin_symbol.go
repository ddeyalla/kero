package main

import (
	"image"
	"math"

	"github.com/egoist/mygo/ui"
)

// pluginSymbols maps the protocol's fixed icon set to the SF Symbols that
// draw them on macOS, which the plugin parts use in place of the SVGs of
// pluginIcons.
var pluginSymbols = map[string]string{
	"play":          "play.fill",
	"pause":         "pause.fill",
	"next":          "forward.fill",
	"previous":      "backward.fill",
	"shuffle":       "shuffle",
	"repeat":        "repeat",
	"volume":        "speaker.wave.2.fill",
	"volume-off":    "speaker.slash.fill",
	"search":        "magnifyingglass",
	"close":         "xmark",
	"refresh":       "arrow.clockwise",
	"plus":          "plus",
	"chevron-right": "chevron.right",
	"chevron-up":    "chevron.up",
	"chevron-down":  "chevron.down",
	"folder":        "folder",
	"external":      "arrow.up.right.square",
	"copy":          "doc.on.doc",
	"link":          "link",
	"gear":          "gearshape",
	"files":         "folder.fill",
	"globe":         "globe",
	"terminal":      "terminal",
	"image":         "photo",
	"back":          "arrow.left",
	"forward":       "arrow.right",
	"preview":       "eye",
	"source":        "chevron.left.forwardslash.chevron.right",
	"doc":           "doc",
	"list":          "list.bullet",
	"board":         "rectangle.split.3x1",
	"flame":         "flame.fill",
	"dot":           "circle.fill",
	"warning":       "exclamationmark.triangle.fill",
	"info":          "info.circle",

	"circle":          "circle",
	"circle-dashed":   "circle.dashed",
	"circle-half":     "circle.lefthalf.filled",
	"check-circle":    "checkmark.circle.fill",
	"xmark-circle":    "xmark.circle.fill",
	"filter":          "line.3.horizontal.decrease.circle",
	"person":          "person.crop.circle",
	"tag":             "tag",
	"flag":            "flag",
	"pin":             "pin",
	"priority-urgent": "exclamationmark.square.fill",
	"priority-high":   "chevron.up.2",
	"priority-medium": "equal",
	"priority-low":    "chevron.down",
	"milestone":       "diamond",
	"blocked":         "nosign",
	"blocking":        "arrow.turn.down.right",
	"subissue":        "list.bullet.indent",
}

// symbolScale is how many pixels a symbol has for each point: the display
// scale of the Retina screens that the app runs on.
const symbolScale = 2

// The weights of a symbol, as AppKit's NSFontWeight has them.
const (
	symbolRegular  = 0.0
	symbolMedium   = 0.23
	symbolSemibold = 0.3
)

// pluginSymbolBudget is how many renderings of symbols a pane keeps: those
// the tree shows, and as many of those it no longer shows as fit.
const pluginSymbolBudget = 128

// symbolSizes are the point sizes that a symbol is drawn at. A size that is
// asked for is rounded to the nearest, so that a size that animates does not
// make a bitmap for each pixel.
var symbolSizes = []float64{12, 16, 20, 24, 32, 48, 64}

// symbolSize rounds a point size to the nearest of symbolSizes.
func symbolSize(size float64) float64 {
	best := symbolSizes[0]
	for _, s := range symbolSizes[1:] {
		if math.Abs(s-size) < math.Abs(best-size) {
			best = s
		}
	}
	return best
}

// symbolKey names one rendering of a symbol. It has no color: a symbol is
// drawn once as a white template and tinted when it is painted, so that a
// part that changes color (a toggle, a hover) keeps its bitmap and does
// not flash the SVG while a new one is drawn.
type symbolKey struct {
	name   string
	size   float64
	weight float64
	scale  int
}

// pluginSymbol is a symbol the pane has asked for: its bitmap once it is
// drawn, or none if the system has no such symbol.
type pluginSymbol struct {
	bitmap  *ui.Bitmap
	used    uint64 // the draw that last asked for it
	pending bool   // AppKit is still drawing it
}

// symbolTemplate is the color a symbol is drawn in: white, which a tint
// multiplies into the color it is painted with.
var symbolTemplate = ui.RGB(255, 255, 255)

// symbol returns the SF Symbol of a protocol icon, drawn as a template at
// the point size and weight, or nil if the system has no such symbol, when
// the caller draws the SVG. AppKit draws the symbol off the UI goroutine,
// once for each key, and the pane asks for a frame as it arrives. While a
// size or weight is not drawn yet, the last bitmap of the same icon is
// returned in its place, so that the icon does not flash to the SVG; nil
// comes only before the first one.
func (v *pluginView) symbol(c *ui.Context, icon string, size, weight float64) (*ui.Bitmap, float64) {
	name, ok := pluginSymbols[icon]
	if !ok || v.render == nil {
		return nil, 1
	}
	size = symbolSize(size)
	key := symbolKey{name, size, weight, symbolScale}
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.symbols[key]; ok {
		s.used = v.frame
		if s.bitmap != nil {
			return s.bitmap, 1
		}
		return v.standIn(name, size)
	}
	v.symbols[key] = &pluginSymbol{used: v.frame, pending: true}
	render, invalidate := v.render, c.Invalidate
	go func() {
		var bm *ui.Bitmap
		if mask, ok := render(name, size, weight, key.scale); ok {
			bm = ui.NewBitmap(tintSymbol(mask, symbolTemplate))
		}
		v.mu.Lock()
		// A pruned key stays pruned: the next draw asks again.
		if s := v.symbols[key]; s != nil {
			s.bitmap, s.pending = bm, false
			if bm != nil {
				v.lastSymbol[name] = symbolStandIn{bm, size}
			}
		}
		v.mu.Unlock()
		if bm != nil {
			invalidate()
		}
	}()
	return v.standIn(name, size)
}

// symbolStandIn is the newest bitmap drawn for a symbol name, and the point
// size it was drawn at.
type symbolStandIn struct {
	bitmap *ui.Bitmap
	size   float64
}

// standIn returns the last bitmap of a symbol and the factor that stretches
// it to size points, or nil. It is called with v.mu held.
func (v *pluginView) standIn(name string, size float64) (*ui.Bitmap, float64) {
	s, ok := v.lastSymbol[name]
	if !ok || s.size <= 0 {
		return nil, 1
	}
	return s.bitmap, size / s.size
}

// drawSymbol shows a symbol bitmap at its own proportions, times scale for
// a bitmap of another size standing in, tinted col as it is painted.
func drawSymbol(c *ui.Context, key string, sym *ui.Bitmap, scale float64, col ui.Color) {
	pw, ph := sym.Size()
	k := float32(scale) / symbolScale
	ui.Box(c.Key(key)).Size(float32(pw)*k, float32(ph)*k).Draw(func(p *ui.Painter, r ui.Rect) {
		p.ImageRegion(sym, ui.Rect{}, r, ui.ImageOptions{Tint: col})
	})
}

// tintSymbol makes a template of a symbol's drawing: the shape of its
// alpha, in color, as a template image is tinted. The pixels are
// premultiplied, as an image.RGBA has them.
func tintSymbol(mask *image.RGBA, col ui.Color) *image.RGBA {
	out := image.NewRGBA(mask.Bounds())
	for i := 3; i < len(mask.Pix) && i < len(out.Pix); i += 4 {
		alpha := uint32(mask.Pix[i]) * uint32(col.A) / 255
		out.Pix[i-3] = uint8(uint32(col.R) * alpha / 255)
		out.Pix[i-2] = uint8(uint32(col.G) * alpha / 255)
		out.Pix[i-1] = uint8(uint32(col.B) * alpha / 255)
		out.Pix[i] = uint8(alpha)
	}
	return out
}

// pruneSymbols drops the renderings the tree no longer asks for, the
// longest unasked first, while the pane keeps more than its budget. One that
// this draw asked for, or that is still drawn, stays.
func (v *pluginView) pruneSymbols() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for len(v.symbols) > pluginSymbolBudget {
		var oldestKey symbolKey
		var oldest uint64
		found := false
		for key, s := range v.symbols {
			if s.pending || s.used == v.frame {
				continue
			}
			if !found || s.used < oldest {
				oldestKey, oldest, found = key, s.used, true
			}
		}
		if !found {
			break
		}
		delete(v.symbols, oldestKey)
	}
}
