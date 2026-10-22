package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/egoist/mygo/plugins/terminal"
)

// glassTerms gives a's two panes terminals full of colored text, what the
// floating panels float over and their glass bends.
func glassTerms(t *testing.T, a *App) {
	t.Helper()
	if testing.Short() {
		t.Skip("downloads libghostty-vt")
	}
	if err := terminal.Load(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		conn := idleConn{make(chan struct{})}
		term, err := terminal.New(terminal.Options{Conn: conn})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { term.Close() })
		term.Resize(120, 40)
		for i := range 40 {
			term.Feed([]byte(fmt.Sprintf("\x1b[%dm%04d\x1b[0m %s \x1b[1;34m%s\x1b[0m\r\n",
				31+i%6, i, strings.Repeat("go test ./... ok kero ", 3), "PASS")))
		}
		a.terms[id] = &termEntry{term: term}
	}
}

// glassShot writes a shot of a floating panel under $KERO_GLASS_SHOTS,
// light or dark: the before and after of BRU-212's panels.
func glassShot(t *testing.T, tt *ui.Tester, name string, dark bool) {
	t.Helper()
	dir := os.Getenv("KERO_GLASS_SHOTS")
	if dir == "" {
		return
	}
	mode := "light"
	if dark {
		mode = "dark"
	}
	t.Setenv("KERO_SHOTS", dir)
	shot(t, tt, filepath.Base(name+"-"+mode))
}

// Each floating panel opens over terminals with text under it, in the
// light and the dark.
func TestGlassPanelsOverTerminal(t *testing.T) {
	surfaces := []struct {
		name string
		open func(a *App, tt *ui.Tester)
		text string
	}{
		{"palette", func(a *App, tt *ui.Tester) { a.pal.open = true }, "Commands"},
		{"confirm", func(a *App, tt *ui.Tester) {
			a.confirm = &confirmation{open: true, title: "Close demo?", detail: "zsh is still running in it.", action: "Close Session", run: func() {}}
		}, "Close demo?"},
		{"confirm-over-settings", func(a *App, tt *ui.Tester) {
			a.openSettings("")
			tt.Frame()
			a.confirm = &confirmation{open: true, title: "Reset settings?", detail: "Every setting goes back to its default.", action: "Reset", run: func() {}}
		}, "Reset settings?"},
		{"chooser", func(a *App, tt *ui.Tester) { a.chooser.open = true }, "New Pane"},
		{"settings", func(a *App, tt *ui.Tester) { a.openSettings("") }, "General"},
		{"drag-chip", func(a *App, tt *ui.Tester) {
			grip, ok := tt.Find("zsh")
			if !ok {
				t.Fatalf("no title of pane b; texts %q", tt.Texts())
			}
			x, y := grip.X+grip.W/2, grip.Y+grip.H/2
			tt.Press(x, y)
			for i := 1; i <= 12; i++ {
				tt.Move(x-float32(i)*30, y+float32(i)*16)
				tt.Advance(16 * time.Millisecond)
				tt.Frame()
			}
		}, "zsh"},
	}
	for _, s := range surfaces {
		for _, dark := range []bool{false, true} {
			a := settingsTestApp(t)
			glassTerms(t, a)
			tt := ui.NewTester(a.view, 1100, 720)
			tt.SetDark(dark)
			tt.Frame()
			s.open(a, tt)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			if !tt.HasText(s.text) {
				t.Errorf("%s: no %q; texts %q", s.name, s.text, tt.Texts())
			}
			glassShot(t, tt, s.name, dark)
		}
	}
}

// The palette is of glass: what is under it shows through, frosted. An
// opaque panel is one color where it has no text; over the terminals'
// colored text, the glass is not.
func TestPaletteIsGlass(t *testing.T) {
	if lo, hi := paletteShades(t, false); hi-lo < 6 {
		t.Errorf("the palette is one shade, %d to %d, where the terminal's text is under it: not glass", lo, hi)
	}
}

// Under Reduce Transparency the palette is opaque: one shade where it
// has no text, whatever is under it.
func TestPaletteOpaqueUnderReduceTransparency(t *testing.T) {
	if lo, hi := paletteShades(t, true); hi-lo > 1 {
		t.Errorf("the palette shows what is under it, %d to %d, under Reduce Transparency", lo, hi)
	}
}

// paletteShades opens the palette, in the dark, over two terminals full
// of colored text, and returns the darkest and the lightest luma of an
// empty stretch of a command's row.
func paletteShades(t *testing.T, reduceTransparency bool) (lo, hi int) {
	t.Helper()
	a := settingsTestApp(t)
	glassTerms(t, a)
	tt := ui.NewTester(a.view, 1100, 720)
	tt.SetDark(true)
	tt.SetPreferences(ui.Preferences{ReduceTransparency: reduceTransparency, TextScale: 1})
	tt.Frame()
	a.pal.open = true
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	// The right of a command's row, empty, over pane a's text.
	r, ok := tt.Find("New Session in Home")
	if !ok || r.Y < 84 || r.Y > 544 {
		t.Fatalf("no command row in the palette, at %+v; texts %q", r, tt.Texts())
	}
	img := tt.Image()
	s := float32(img.Bounds().Dx()) / 1100
	lo, hi = 255, 0
	for y := int((r.Y + 2) * s); y < int((r.Y+r.H-2)*s); y++ {
		for x := int((r.X + r.W + 40) * s); x < int((r.X+r.W+200)*s); x++ {
			c := img.RGBAAt(x, y)
			l := (int(c.R)*299 + int(c.G)*587 + int(c.B)*114) / 1000
			lo, hi = min(lo, l), max(hi, l)
		}
	}
	return lo, hi
}

// The palette's muted text keeps 4.5 to 1 against the glass under it,
// in the light and the dark, wherever the terminal's text shows through
// brightest or darkest: Apple's legibility on its materials.
func TestGlassTextContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		pal := &lightPalette
		if dark {
			pal = &darkPalette
		}
		a := settingsTestApp(t)
		glassTerms(t, a)
		tt := ui.NewTester(a.view, 1100, 720)
		tt.SetDark(dark)
		tt.Frame()
		a.pal.open = true
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		r, ok := tt.Find("New Session in Home")
		if !ok || r.Y < 84 || r.Y > 544 {
			t.Fatalf("no command row in the palette, at %+v", r)
		}
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / 1100
		muted := luminance(pal.textMuted.R, pal.textMuted.G, pal.textMuted.B)
		worst := math.Inf(1)
		for y := int(r.Y * s); y < int((r.Y+r.H)*s); y++ {
			for x := int((r.X + r.W + 20) * s); x < int((r.X+r.W+180)*s); x++ {
				c := img.RGBAAt(x, y)
				worst = min(worst, contrast(muted, luminance(c.R, c.G, c.B)))
			}
		}
		if worst < 4.5 {
			t.Errorf("dark %v: the muted text is %.2f to 1 on the palette's glass at worst, want 4.5", dark, worst)
		}
	}
}

// luminance is WCAG's relative luminance of an sRGB color.
func luminance(r, g, b uint8) float64 {
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrast is WCAG's contrast ratio of two luminances.
func contrast(a, b float64) float64 {
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}
