//go:build darwin

package main

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A known SF Symbol draws, at the size asked for, in the tint asked for,
// once: the second ask is the bitmap the first one made.
func TestSymbolImage(t *testing.T) {
	const symbol = "bell.badge"
	tint := ui.Hex("#ff3b30")
	b := symbolImage(symbol, 16, tint, false)
	if b == nil {
		t.Fatalf("no bitmap for SF Symbol %q", symbol)
	}
	if w, h := b.Size(); w != 16*viewSymbolScale || h != 16*viewSymbolScale {
		t.Fatalf("bitmap is %d×%d, want %d×%d", w, h, 16*viewSymbolScale, 16*viewSymbolScale)
	}
	if again := symbolImage(symbol, 16, tint, false); again != b {
		t.Fatal("a second ask for the same symbol drew it again")
	}
	// A symbol the system does not have is none, not a blank mark.
	if b := symbolImage("kero.thruster", 16, tint, false); b != nil {
		t.Fatal("an unknown symbol drew a bitmap")
	}
}

// The drawn pixels carry the symbol's shape in their alpha and the tint
// in their color: a symbol is not a blank bitmap, nor one of the system's
// own colors.
func TestSymbolPixels(t *testing.T) {
	tint := ui.Hex("#ff3b30")
	var solid, tinted int
	for _, px := range []*image.RGBA{drawViewSymbol("bell.badge", 32, tint, false), drawViewSymbol("bell.badge", 32, tint, true)} {
		if px == nil {
			t.Fatal("no pixels for a known symbol")
		}
		for i := 0; i < len(px.Pix); i += 4 {
			a := px.Pix[i+3]
			if a == 0 {
				continue
			}
			if a == 255 {
				solid++
				if px.Pix[i] != tint.R || px.Pix[i+1] != tint.G || px.Pix[i+2] != tint.B {
					tinted++
				}
			}
		}
	}
	if solid == 0 {
		t.Fatal("the symbol drew nothing solid")
	}
	if tinted != 0 {
		t.Fatalf("%d solid pixels are not the tint", tinted)
	}
}
