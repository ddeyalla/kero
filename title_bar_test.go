package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// The title bar is the unified title bar and toolbar of macOS, 52 tall,
// and a pane's header, which centers on its middle as the traffic lights
// do, still holds a pane's title, 28 tall.
func TestTitleBarHeight(t *testing.T) {
	if topBar != 52 || barMid != 26 {
		t.Errorf("the title bar is %d tall, centered on %d, not macOS's unified 52, centered on 26", topBar, barMid)
	}
	if headerH < 28+2 {
		t.Errorf("a header of %d leaves no room around a pane's title, 28 tall", headerH)
	}
}

// In full screen macOS hides the traffic lights, and brings them down with
// the menu bar as the pointer reaches the top of the screen; the title
// bar is empty there. The sidebar's buttons stay where they were in the
// window, centered with the lights, clear of them as they come down.
func TestTitleBarInFullScreen(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	// The lights as an older macOS spaces them, apart from systemBar: the
	// bar in full screen is the window's, not where AppKit puts them now.
	window := ui.TitleBar{Height: topBar, Left: 20 + 2*20 + 14}
	for _, hidden := range []bool{false, true} {
		a := testApp(t)
		a.prefs.SidebarHidden = hidden
		label := a.tip(locales.SidebarHide(), "sidebar.toggle")
		if hidden {
			label = a.tip(locales.SidebarShow(), "sidebar.toggle")
		}
		tt := ui.NewTester(a.view, 1100, 640)
		tt.SetTitleBar(window)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		inWindow, ok := tt.Find(label)
		if !ok {
			t.Fatalf("sidebar hidden %v: no button %q; texts %q", hidden, label, tt.Texts())
		}
		if mid := inWindow.Y + inWindow.H/2; mid != barMid {
			t.Errorf("sidebar hidden %v: the sidebar's buttons center on %v, not on the lights' %d", hidden, mid, barMid)
		}
		if inWindow.X < window.Left {
			t.Errorf("sidebar hidden %v: the sidebar's buttons at %v are under the lights, which end at %v", hidden, inWindow.X, window.Left)
		}
		// Full screen: the system's lights hide, and the bar is empty.
		tt.SetTitleBar(ui.TitleBar{})
		tt.Frame()
		if inFull, _ := tt.Find(label); inFull != inWindow {
			t.Errorf("sidebar hidden %v: the sidebar's buttons moved in full screen, from %v to %v", hidden, inWindow, inFull)
		}
		shot(t, tt, "title-bar-full-screen")
	}
}

// A window that opens in full screen has had no bar to keep: the sidebar's
// buttons keep clear of where AppKit puts the lights.
func TestTitleBarOpensInFullScreen(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	a := testApp(t)
	a.prefs.SidebarHidden = true
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	show, ok := tt.Find(a.tip(locales.SidebarShow(), "sidebar.toggle"))
	if !ok {
		t.Fatalf("no button that shows the sidebar; texts %q", tt.Texts())
	}
	if show.X < systemBar.Left {
		t.Errorf("the sidebar's buttons at %v are under the lights, which end at %v", show.X, systemBar.Left)
	}
}
