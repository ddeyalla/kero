//go:build darwin

package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSelectedSettingsIconUsesAccent(t *testing.T) {
	a := settingsTestApp(t)
	a.ss.page = "general"
	pal := lightPalette
	pal.accent = ui.RGB(220, 20, 40)
	tt := ui.NewTester(func(c *ui.Context) {
		a.settingsNav(c, &pal, settingsPages[0], true)
	}, 40, 40)
	tt.Frame()
	colored := 0
	img := tt.Image()
	for y := 0; y < 32; y++ {
		for x := 0; x < 40; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 180 && p.G < 60 && p.B < 80 {
				colored++
			}
		}
	}
	if colored < 3 {
		t.Fatal("selected Settings icon did not paint its accent")
	}
}
