//go:build darwin

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

func TestTabChromeBudgetMatchesAppearance(t *testing.T) {
	a := navTestApp(t)
	a.session("s2").Name = "readme"
	a.setNavPlacement(navTop)
	measure := func(dark bool) (tab, title ui.Rect) {
		t.Helper()
		tt := ui.NewTester(a.view, 1400, 500)
		tt.SetDark(dark)
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		tab, ok1 := tt.Find(locales.NavSessionTab("readme"))
		title, ok2 := tt.Find("readme")
		if !ok1 || !ok2 {
			t.Fatalf("missing tab or title: %q", tt.Texts())
		}
		return tab, title
	}
	lightTab, lightTitle := measure(false)
	darkTab, darkTitle := measure(true)
	if darkTab.W-lightTab.W != 2 {
		t.Errorf("dark border budget = %v points, want 2: light %v, dark %v", darkTab.W-lightTab.W, lightTab.W, darkTab.W)
	}
	if darkTitle.W != lightTitle.W {
		t.Errorf("appearance changed title room: light %v, dark %v", lightTitle.W, darkTitle.W)
	}
}

func TestTabPinBudgetMatchesSymbolAvailability(t *testing.T) {
	key := viewSymbolKey{name: "pin.fill", pixels: 20, dark: true}
	symbolCache.Lock()
	original, existed := symbolCache.bitmaps[key]
	symbolCache.Unlock()
	t.Cleanup(func() {
		symbolCache.Lock()
		defer symbolCache.Unlock()
		if existed {
			symbolCache.bitmaps[key] = original
		} else {
			delete(symbolCache.bitmaps, key)
		}
	})

	a := navTestApp(t)
	a.session("s2").Name = "readme"
	a.prefs.SessionPins = map[string]bool{"s2": true}
	a.setNavPlacement(navTop)
	measure := func(missing bool) (tab, title ui.Rect, titleW float32) {
		t.Helper()
		symbolCache.Lock()
		if missing {
			symbolCache.bitmaps[key] = nil
		} else {
			delete(symbolCache.bitmaps, key)
		}
		symbolCache.Unlock()
		tt := ui.NewTester(func(c *ui.Context) {
			titleW, _ = c.MeasureText(0, ui.Span{Text: "readme", Size: 13, Weight: 400})
			a.view(c)
		}, 1400, 500)
		tt.SetDark(true)
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		tab, ok1 := tt.Find(locales.NavSessionTab("readme"))
		title, ok2 := tt.Find("readme")
		if !ok1 || !ok2 {
			t.Fatalf("missing pinned tab or title: %q", tt.Texts())
		}
		if !missing && symbolImage("pin.fill", 10, true) == nil {
			t.Fatal("the native pin symbol is unavailable in the available case")
		}
		return tab, title, titleW
	}
	availableTab, availableTitle, availableTextW := measure(false)
	missingTab, missingTitle, missingTextW := measure(true)
	if availableTab.W-missingTab.W != 16 {
		t.Errorf("available pin budget = %v points, want 16: available %v, missing %v", availableTab.W-missingTab.W, availableTab.W, missingTab.W)
	}
	for _, tc := range []struct {
		name string
		got  ui.Rect
		want float32
	}{
		{"available", availableTitle, availableTextW},
		{"missing", missingTitle, missingTextW},
	} {
		if tc.got.W < tc.want {
			t.Errorf("%s symbol leaves %v points for a %v-point title", tc.name, tc.got.W, tc.want)
		}
	}
}
