//go:build darwin

package main

import (
	"image"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
)

// SF Symbols are the system's icons, which macOS draws itself: what the
// menu bar, Settings and the system apps show. MyGo has no way to show
// one (icons are SVG, see icons.go), so a symbol is drawn once into a
// bitmap through AppKit, and the bitmap shows as any other.
//
// A new icon of the app's is a symbol where one says what it means: a
// view's mark, a state's. The app's older icons stay as they are.

// symbolScale is the pixels a point of a symbol is drawn at: twice, so
// that it stays crisp on the screens the app runs on, and the bitmap
// shows at half its pixel size, where the pane draws it.
const symbolScale = 2

// symbolWeightRegular is NSFontWeightRegular: AppKit's weight for a
// symbol drawn as the system draws its own controls.
const symbolWeightRegular = 0.0

// nsSize is an NSSize, a width and a height in points.
type nsSize struct{ X, Y float64 }

// symbolKey is one drawn symbol: its name, the size in pixels it was
// drawn at, the tint it took and the appearance it was drawn for.
type symbolKey struct {
	name   string
	pixels int
	tint   ui.Color
	dark   bool
}

// symbolCache holds the symbols drawn so far, one bitmap by key: the app
// draws a symbol once, as it first shows, and shows the same bitmap after.
var symbolCache = struct {
	sync.Mutex
	bitmaps map[symbolKey]*ui.Bitmap
}{bitmaps: map[symbolKey]*ui.Bitmap{}}

// symbolAppKit loads AppKit, whose classes a symbol is drawn with. The
// app has loaded it before it draws a window, and a test has not: loading
// it again is the system's to answer at once.
var symbolAppKit sync.Once

func loadAppKit() {
	symbolAppKit.Do(func() {
		purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	})
}

// symbolImage returns the SF Symbol name at points, tinted, drawn for the
// light or the dark appearance; nil when the system has no such symbol.
// It draws a symbol the first time it is asked for one, and never in a
// frame after that.
func symbolImage(name string, points float32, tint ui.Color, dark bool) *ui.Bitmap {
	px := int(points*symbolScale + 0.5)
	if name == "" || px <= 0 {
		return nil
	}
	key := symbolKey{name: name, pixels: px, tint: tint, dark: dark}
	symbolCache.Lock()
	defer symbolCache.Unlock()
	if b, ok := symbolCache.bitmaps[key]; ok {
		return b
	}
	var b *ui.Bitmap
	if rgba := drawSymbol(name, px, tint, dark); rgba != nil {
		b = ui.NewBitmap(rgba)
	}
	// A symbol the system does not have is remembered as none, so that
	// a missing name costs one ask, not one a frame.
	symbolCache.bitmaps[key] = b
	return b
}

// drawSymbol draws a symbol into a bitmap of px pixels, through AppKit:
// the symbol image, configured for the point size it shows at, into an
// NSBitmapImageRep, whose pixels are then tinted. It returns nil when the
// system has no such symbol, or when it cannot draw it.
func drawSymbol(name string, px int, tint ui.Color, dark bool) *image.RGBA {
	loadAppKit()
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("imageWithSystemSymbolName:accessibilityDescription:"), nsString(name), objc.ID(0))
	if img == 0 {
		return nil
	}
	points := float64(px) / symbolScale
	if conf := objc.ID(objc.GetClass("NSImageSymbolConfiguration")).Send(sel("configurationWithPointSize:weight:"), points, symbolWeightRegular); conf != 0 {
		if configured := img.Send(sel("imageWithSymbolConfiguration:"), conf); configured != 0 {
			img = configured
		}
	}
	rep := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(sel("alloc"))
	rep = rep.Send(sel("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:"),
		objc.ID(0), px, px, 8, 4, true, false, nsString("NSCalibratedRGBColorSpace"), 0, 0)
	if rep == 0 {
		return nil
	}
	defer rep.Send(sel("release"))
	ctxCls := objc.ID(objc.GetClass("NSGraphicsContext"))
	ctx := objc.ID(ctxCls).Send(sel("graphicsContextWithBitmapImageRep:"), rep)
	if ctx == 0 {
		return nil
	}
	// The appearance the symbol is drawn for: a symbol may draw itself
	// differently in the dark.
	apName := "NSAppearanceNameAqua"
	if dark {
		apName = "NSAppearanceNameDarkAqua"
	}
	var appearance objc.ID
	if ap := objc.ID(objc.GetClass("NSAppearance")).Send(sel("appearanceNamed:"), nsString(apName)); ap != 0 {
		appearance = ap
	}
	objc.ID(ctxCls).Send(sel("saveGraphicsState"))
	objc.ID(ctxCls).Send(sel("setCurrentContext:"), ctx)
	// The symbol, as large as fits in the square and no larger, at its
	// own shape: an image is wider or narrower than tall by its nature.
	size := objc.Send[nsSize](img, sel("size"))
	r := nsRect{W: float64(px), H: float64(px)}
	if size.X > 0 && size.Y > 0 {
		s := min(float64(px)/size.X, float64(px)/size.Y)
		r = nsRect{X: (float64(px) - size.X*s) / 2, Y: (float64(px) - size.Y*s) / 2, W: size.X * s, H: size.Y * s}
	}
	draw := func() {
		img.Send(sel("drawInRect:fromRect:operation:fraction:"), r, nsRect{}, 2 /* NSCompositingOperationSourceOver */, 1.0)
	}
	if appearance != 0 {
		block := objc.NewBlock(func(objc.Block, objc.ID) { draw() })
		appearance.Send(sel("performAsCurrentDrawingAppearance:"), block)
		block.Release()
	} else {
		draw()
	}
	objc.ID(ctxCls).Send(sel("restoreGraphicsState"))

	data := objc.Send[*byte](rep, sel("bitmapData"))
	stride := objc.Send[int](rep, sel("bytesPerRow"))
	if data == nil || stride < 4*px {
		return nil
	}
	src := unsafe.Slice(data, stride*px)
	out := image.NewRGBA(image.Rect(0, 0, px, px))
	for y := range px {
		row := src[y*stride:]
		for x := range px {
			// The rep's pixels carry premultiplied alpha, as an
			// image.RGBA's do: the symbol draws dark on nothing, so its
			// alpha is what the tint takes.
			a := row[4*x+3]
			o := out.Pix[out.PixOffset(x, y):]
			o[0], o[1], o[2], o[3] = mulAlpha(tint.R, a), mulAlpha(tint.G, a), mulAlpha(tint.B, a), a
		}
	}
	return out
}

// mulAlpha is c premultiplied by a, as an image.RGBA holds a color.
func mulAlpha(c, a uint8) uint8 {
	return uint8(uint16(c) * uint16(a) / 255)
}
