package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// settingsTestApp is testApp with Settings' own asks kept from the daily
// app: refreshService connects to kerod, which an inherited socket would
// make the user's.
func settingsTestApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	t.Setenv("KERO_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	t.Setenv("KERO_PANE", "")
	t.Setenv("KERO_SESSION", "")
	return a
}

// Settings opens over the main window, on the page asked for, a card
// sized to the window that keeps inside it however small it is.
func TestSettingsModalOpensOnPage(t *testing.T) {
	// The main window's default size, about, and its smallest.
	for _, size := range [][2]int{{1100, 720}, {560, 360}} {
		w, h := size[0], size[1]
		for _, dark := range []bool{false, true} {
			a := settingsTestApp(t)
			tt := ui.NewTester(a.view, w, h)
			tt.SetDark(dark)
			tt.Frame()
			a.openSettings("terminal")
			tt.Frame()
			if !a.ss.open || a.ss.page != "terminal" {
				t.Fatalf("%dx%d: open %v, page %q", w, h, a.ss.open, a.ss.page)
			}
			if !tt.HasText("Line height") {
				t.Fatalf("%dx%d: not on the terminal page; texts %q", w, h, tt.Texts())
			}
			cw, ch := float32(min(900, w-48)), float32(min(640, h-48))
			left, top := (float32(w)-cw)/2, (float32(h)-ch)/2
			// The close button and the configuration's link are on the
			// card, and the last page is in it, scrolled to in a short one.
			for _, label := range []string{a.tip(locales.SettingsClose(), "settings.open"), "Advanced", "Open the config file"} {
				r, ok := tt.Find(label)
				if !ok || r.X < left || r.Y < top || r.X+r.W > left+cw+0.5 || r.Y+r.H > top+ch+0.5 {
					t.Errorf("%dx%d: %q at %+v, off the card", w, h, label, r)
				}
			}
			// The card is no wider than 900, centered with the window around it.
			g, ok := tt.Find("General")
			if !ok || g.X < float32(max(w-900, 48))/2 {
				t.Errorf("%dx%d: General at %+v, off the card", w, h, g)
			}
			name := "settings-modal-light"
			if dark {
				name = "settings-modal-dark"
			}
			if w < 900 {
				name += "-small"
			}
			shot(t, tt, name)
		}
	}
}

// Escape closes Settings, and gives the focus back; with a search typed
// it clears the search first. Recording a shortcut, it ends the recording
// alone.
func TestSettingsModalEscape(t *testing.T) {
	a := settingsTestApp(t)
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	a.openSettings("")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.ss.open {
		t.Fatal("Escape left Settings open")
	}

	a.openSettings("general")
	tt.Frame()
	if err := tt.Click("Search settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("font")
	tt.Frame()
	if a.ss.query != "font" {
		t.Fatalf("the search has %q", a.ss.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !a.ss.open || a.ss.query != "" {
		t.Fatalf("Escape in the search: open %v, query %q; want the search cleared", a.ss.open, a.ss.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.ss.open {
		t.Fatal("Escape in the empty search left Settings open")
	}

	a.openSettings("keyboard")
	a.ss.filter = "split right" // in view, in a card the window's height
	tt.Frame()
	if err := tt.Click("Shortcut for Split Right"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.ss.recording != "pane.split.right" {
		t.Fatalf("recording %q", a.ss.recording)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.ss.recording != "" || !a.ss.open {
		t.Errorf("Escape while recording: recording %q, open %v; want the recording ended, Settings open", a.ss.recording, a.ss.open)
	}
}

// A click beside the card closes Settings; one on it does not.
func TestSettingsModalBackdropClick(t *testing.T) {
	a := settingsTestApp(t)
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	a.openSettings("general")
	tt.Frame()
	if err := tt.Click("Appearance"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.ss.open || a.ss.page != "appearance" {
		t.Fatalf("a click on the card: open %v, page %q", a.ss.open, a.ss.page)
	}
	tt.ClickAt(10, 710)
	tt.Frame()
	if a.ss.open {
		t.Error("a click on the backdrop left Settings open")
	}
}

// Settings' shortcut opens and closes it, and Close, as ⌘W, closes it,
// leaving the panes under it alone.
func TestSettingsModalShortcuts(t *testing.T) {
	a := settingsTestApp(t)
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	a.run("settings.open", nil)
	tt.Frame()
	if !a.ss.open || !tt.HasText("Restore sessions after a restart") {
		t.Fatalf("settings.open: open %v; texts %q", a.ss.open, tt.Texts())
	}
	a.run("settings.open", nil)
	tt.Frame()
	if a.ss.open {
		t.Fatal("settings.open again left Settings open")
	}
	a.run("settings.open", nil)
	tt.Frame()
	a.run("pane.close", nil)
	tt.Frame()
	if a.ss.open {
		t.Error("Close left Settings open")
	}
	if n := len(a.session("s1").Panes); n != 2 {
		t.Errorf("Close under Settings closed a pane: %d left", n)
	}
	// The palette's pages open Settings on theirs.
	a.openSettings("linear")
	tt.Frame()
	if a.ss.page != "linear" || !tt.HasText("Check for changes") {
		t.Errorf("on page %q; texts %q", a.ss.page, tt.Texts())
	}
}

// Escape closes a menu open in Settings, and Settings with the next.
func TestSettingsModalMenuEscape(t *testing.T) {
	a := settingsTestApp(t)
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	a.openSettings("terminal")
	tt.Frame()
	if err := tt.Click("JetBrains Mono Nerd Font"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if m := a.ss.menus["font"]; m == nil || !m.open {
		t.Fatal("the font menu did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.ss.menus["font"].open || !a.ss.open {
		t.Fatalf("Escape on the menu: menu open %v, Settings open %v; want the menu closed alone", a.ss.menus["font"].open, a.ss.open)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.ss.open {
		t.Error("the next Escape left Settings open")
	}
}

// The main window's closing ends Settings, letting go of the updater's
// news and the shortcut being typed, so that the window, shown again,
// starts without it.
func TestSettingsEndsWithWindow(t *testing.T) {
	a := settingsTestApp(t)
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	a.openSettings("keyboard")
	off := 0
	a.ss.offUpdates = func() { off++ }
	a.ss.recording, a.ss.query, a.ss.linearKey = "pane.split.right", "split", "lin_key"
	tt.Frame()
	a.endSettings() // as the window's OnClosed does
	if a.ss.open || off != 1 || a.ss.offUpdates != nil || a.ss.recording != "" || a.ss.query != "" || a.ss.linearKey != "" {
		t.Fatalf("open %v, updates let go %d times, recording %q, query %q, key %q", a.ss.open, off, a.ss.recording, a.ss.query, a.ss.linearKey)
	}
	again := ui.NewTester(a.view, 1100, 720)
	again.Frame()
	if _, ok := again.Find(a.tip(locales.SettingsClose(), "settings.open")); ok {
		t.Error("the window shown again shows Settings")
	}
	a.endSettings() // twice is harmless
	if off != 1 {
		t.Errorf("the updater let go of %d times", off)
	}
}
