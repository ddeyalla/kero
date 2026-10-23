//go:build darwin

package main

import (
	"image"
	"runtime"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Exercise native cache misses on scheduler-managed workers. A warmed cache
// does not exercise AppKit's per-thread graphics stack or temporary objects.
func TestSymbolUncachedWorkerDrawing(t *testing.T) {
	var workers sync.WaitGroup
	for worker, name := range []string{"hand.raised", "pin", "tray.full", "magnifyingglass"} {
		workers.Go(func() {
			for range 16 {
				runtime.Gosched()
				// Production cache misses serialize native drawing as well.
				symbolCache.Lock()
				pixels := drawViewSymbol(name, 32, ui.Hex("#6e6e78"), worker%2 != 0)
				symbolCache.Unlock()
				if pixels == nil {
					t.Errorf("worker %d: no native symbol %q", worker, name)
					return
				}
				visible := false
				for i := 3; i < len(pixels.Pix); i += 4 {
					visible = visible || pixels.Pix[i] != 0
				}
				if !visible {
					t.Errorf("worker %d: empty native symbol %q", worker, name)
				}
			}
		})
	}
	workers.Go(func() {
		for range 16 {
			runtime.GC()
		}
	})
	workers.Wait()
}

// A known SF Symbol draws, at the size asked for, in the tint asked for,
// once: the second ask is the bitmap the first one made.
func TestSymbolImage(t *testing.T) {
	const symbol = "bell.badge"
	b := symbolImage(symbol, 16, false)
	if b == nil {
		t.Fatalf("no bitmap for SF Symbol %q", symbol)
	}
	if w, h := b.Size(); w != 16*viewSymbolScale || h != 16*viewSymbolScale {
		t.Fatalf("bitmap is %d×%d, want %d×%d", w, h, 16*viewSymbolScale, 16*viewSymbolScale)
	}
	if again := symbolImage(symbol, 16, false); again != b {
		t.Fatal("a second ask for the same symbol drew it again")
	}
	// A symbol the system does not have is none, not a blank mark.
	if b := symbolImage("kero.thruster", 16, false); b != nil {
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

// Runtime settings rebuilds reuse embedded glyph handles; the native mapping
// must not retain a fresh parsed SVG for every frame.
func TestControlGlyphRegistryIsBounded(t *testing.T) {
	first := glyph("f7-chevron-left-slash-chevron-right")
	glyphMu.Lock()
	count := len(controlSymbols)
	glyphMu.Unlock()
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				if got := glyph("f7-chevron-left-slash-chevron-right"); got != first {
					t.Error("a repeated glyph parsed another SVG")
				}
			}
		}()
	}
	workers.Wait()
	glyphMu.Lock()
	defer glyphMu.Unlock()
	if len(controlSymbols) != count {
		t.Fatal("repeated glyph grew symbol registry")
	}
	if controlSymbols[first] != "chevron.left.forwardslash.chevron.right" {
		t.Fatal("control lost native symbol mapping")
	}
	if controlSymbols[programIcons["claude"]] != "" {
		t.Fatal("provider logo replaced by control symbol")
	}
}

// Every compatibility handle names a real native control symbol on macOS.
func TestControlSymbolsExist(t *testing.T) {
	for file, name := range controlSymbolNames {
		if symbolImage(name, 16, false) == nil {
			t.Errorf("%s maps to missing SF Symbol %s", file, name)
		}
	}
}

// Migrated control tints retain their alpha, including subdued toolbar marks.
func TestSymbolKeepsTintOpacity(t *testing.T) {
	tint := ui.RGBA(30, 60, 90, 0.5)
	pixels := drawViewSymbol("magnifyingglass", 32, tint, false)
	if pixels == nil {
		t.Fatal("no search symbol")
	}
	visible := false
	for i := 3; i < len(pixels.Pix); i += 4 {
		if pixels.Pix[i] > tint.A {
			t.Fatalf("symbol alpha %d exceeds tint %d", pixels.Pix[i], tint.A)
		}
		visible = visible || pixels.Pix[i] != 0
	}
	if !visible {
		t.Fatal("translucent symbol disappeared")
	}
}

// Ambient color and theme transitions change control tint every frame.
// That changes painting, not the native shape or the number of bitmaps.
func TestControlTintDoesNotGrowSymbolCache(t *testing.T) {
	symbolCache.Lock()
	before := len(symbolCache.bitmaps)
	symbolCache.Unlock()
	tint := ui.RGB(17, 33, 49)
	tt := ui.NewTester(func(c *ui.Context) {
		controlIcon(c, searchIcon, 17, tint, false)
	}, 24, 24)
	for i := range 32 {
		tint = ui.RGBA(uint8(17+i), uint8(33+i), uint8(49+i), 0.5)
		tt.Frame()
	}
	symbolCache.Lock()
	defer symbolCache.Unlock()
	if added := len(symbolCache.bitmaps) - before; added > 1 {
		t.Fatalf("32 tint changes retained %d native bitmaps, want at most one", added)
	}
}
