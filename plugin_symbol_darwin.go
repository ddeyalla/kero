//go:build darwin

package main

import (
	"image"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	appKitPath = "/System/Library/Frameworks/AppKit.framework/AppKit"
	// NSImageSymbolScaleMedium
	symbolScaleMedium = 2
)

// nsSize is an NSSize.
type nsSize struct{ W, H float64 }

var (
	appKitOnce sync.Once
	appKitOK   bool
)

// appKit loads AppKit, which a test binary or a headless run may not have
// loaded, and tells whether it is there.
func appKit() bool {
	appKitOnce.Do(func() {
		if _, err := purego.Dlopen(appKitPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			return
		}
		appKitOK = objc.GetClass("NSImage") != 0
	})
	return appKitOK
}

// renderSymbol draws an SF Symbol, by NSImage's systemSymbolName, at size
// points and weight in a bitmap of scale pixels to the point. It returns
// the symbol's shape in black, with alpha, which the caller tints, and
// false if there is no AppKit or no such symbol. It does its work on the
// calling goroutine, which must not be the UI's.
func renderSymbol(name string, size, weight float64, scale int) (img *image.RGBA, ok bool) {
	if !appKit() || scale < 1 {
		return nil, false
	}
	// A goroutine has no autorelease pool, and AppKit makes temporaries.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	defer func() {
		// A misbehaving binding must not take the app down for an icon.
		if recover() != nil {
			img, ok = nil, false
		}
	}()

	str := func(s string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), s+"\x00")
	}
	nsImage := objc.ID(objc.GetClass("NSImage"))
	symbol := nsImage.Send(sel("imageWithSystemSymbolName:accessibilityDescription:"), str(name), objc.ID(0))
	if symbol == 0 {
		return nil, false
	}
	config := objc.ID(objc.GetClass("NSImageSymbolConfiguration")).Send(
		sel("configurationWithPointSize:weight:scale:"), size, weight, symbolScaleMedium)
	symbol = symbol.Send(sel("imageWithSymbolConfiguration:"), config)
	if symbol == 0 {
		return nil, false
	}
	pts := objc.Send[nsSize](symbol, sel("size"))
	w, h := int(math.Ceil(pts.W))*scale, int(math.Ceil(pts.H))*scale
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return nil, false
	}

	rep := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(sel("alloc")).Send(
		sel("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bitmapFormat:bytesPerRow:bitsPerPixel:"),
		uintptr(0), w, h, 8, 4, true, false, str("NSDeviceRGBColorSpace"), uint(0), w*4, 32)
	if rep == 0 {
		return nil, false
	}
	defer rep.Send(sel("release"))
	rep.Send(sel("setSize:"), nsSize{float64(w) / float64(scale), float64(h) / float64(scale)})

	ctx := objc.ID(objc.GetClass("NSGraphicsContext")).Send(sel("graphicsContextWithBitmapImageRep:"), rep)
	if ctx == 0 {
		return nil, false
	}
	gc := objc.ID(objc.GetClass("NSGraphicsContext"))
	func() {
		gc.Send(sel("saveGraphicsState"))
		// The state is restored even if a binding panics and the recover above
		// takes it, or it would leak into the next drawing of the thread.
		defer gc.Send(sel("restoreGraphicsState"))
		gc.Send(sel("setCurrentContext:"), ctx)
		// drawInRect: takes one rect, which fits the registers; the longer
		// drawInRect:fromRect:operation:fraction: would pass its fraction on the
		// stack. It draws source-over, at full opacity.
		dst := nsRect{0, 0, float64(w) / float64(scale), float64(h) / float64(scale)}
		symbol.Send(sel("drawInRect:"), dst)
	}()

	data := rep.Send(sel("bitmapData"))
	if data == 0 {
		return nil, false
	}
	// The rep owns its pixels, premultiplied, so they are copied out before
	// the rep goes.
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(out.Pix, unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&data))), w*h*4))
	// Only the shape is wanted: black, with the alpha of the drawing.
	for i := 0; i+3 < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2] = 0, 0, 0
	}
	return out, true
}
