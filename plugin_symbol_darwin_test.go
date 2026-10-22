//go:build darwin

package main

import "testing"

// TestRenderSymbolDraws draws a real SF Symbol, where AppKit is there, at
// the display scale and with a shape in its alpha, and refuses a name that
// is no symbol. Without AppKit it has nothing to draw, and skips.
func TestRenderSymbolDraws(t *testing.T) {
	img, ok := renderSymbol("play.fill", 16, symbolMedium, symbolScale)
	if !ok {
		if !appKit() {
			t.Skip("no AppKit in this run")
		}
		t.Fatal("no play.fill symbol")
	}
	b := img.Bounds()
	if b.Dx() < 16 || b.Dy() < 16 {
		t.Fatalf("the symbol is %v pixels, want at least 16 for 16 points at 2x", b)
	}
	// A real symbol is a solid shape: most of what it draws is opaque, and
	// the shape fills a fair part of its bounds, not a few stray pixels.
	drawn, opaque := 0, 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			drawn++
		}
		if img.Pix[i] >= 0xf0 {
			opaque++
		}
	}
	if drawn == 0 {
		t.Fatal("the symbol has no shape")
	}
	if area := b.Dx() * b.Dy(); opaque*100 < area*25 || opaque*100 < drawn*70 {
		t.Fatalf("%d of %d pixels are opaque, %d are drawn: want the shape solid inside its bounds", opaque, area, drawn)
	}
	if _, ok := renderSymbol("no.such.symbol.name", 16, symbolMedium, symbolScale); ok {
		t.Fatal("a name that is no symbol drew")
	}
}
