package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// The title bar is macOS's compact title bar and toolbar, 40 tall, with
// the traffic lights centered in it, 12 in, as AppKit puts them; a pane's
// header, apart from it, holds a pane's title, 28 tall, with room.
func TestTitleBarHeight(t *testing.T) {
	if topBar != 40 || barMid != 20 {
		t.Errorf("the title bar is %d tall, centered on %d, not macOS's compact 40, centered on 20", topBar, barMid)
	}
	if systemBar.Left != 72 || windowLights[0].X != 12 || windowLights[0].Y+windowLights[0].H/2 != barMid {
		t.Errorf("the lights go at %v, up to %v, not where AppKit puts them in the compact bar", windowLights, systemBar.Left)
	}
	if headerH < 28+2 {
		t.Errorf("a header of %d leaves no room around a pane's title, 28 tall", headerH)
	}
}

// macApp is the app on macOS in its window, with the sidebar shown or not,
// and the label of the sidebar's button that hides or shows it.
func macApp(t *testing.T, hidden bool) (*App, *ui.Tester, string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	a := testApp(t)
	a.prefs.SidebarHidden = hidden
	label := a.tip(locales.SidebarHide(), "sidebar.toggle")
	if hidden {
		label = a.tip(locales.SidebarShow(), "sidebar.toggle")
	}
	tt := ui.NewTester(a.view, 1100, 640)
	tt.SetTitleBar(systemBar)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	return a, tt, label
}

func mid(r ui.Rect) float32 { return r.Y + r.H/2 }

// The sidebar's buttons sit after the traffic lights and center on them,
// with the sidebar shown or hidden. In full screen the lights are still
// the system's, put back where they were in the window
// (followFullScreen), though MyGo reports no bar there: the buttons stay
// where they were, and come back to it in the window.
func TestSidebarButtonsOnTheLights(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		_, tt, label := macApp(t, hidden)
		inWindow, ok := tt.Find(label)
		if !ok {
			t.Fatalf("sidebar hidden %v: no button %q; texts %q", hidden, label, tt.Texts())
		}
		if mid(inWindow) != barMid {
			t.Errorf("sidebar hidden %v: the sidebar's buttons center on %v, not the lights' %d", hidden, mid(inWindow), barMid)
		}
		if inWindow.X < systemBar.Left {
			t.Errorf("sidebar hidden %v: the sidebar's buttons at %v are under the lights, which end at %v", hidden, inWindow.X, systemBar.Left)
		}

		tt.SetTitleBar(ui.TitleBar{})
		tt.Frame()
		if inFull, _ := tt.Find(label); inFull != inWindow {
			t.Errorf("sidebar hidden %v: the sidebar's buttons moved in full screen, from %v to %v", hidden, inWindow, inFull)
		}
		tt.SetTitleBar(systemBar)
		tt.Frame()
		if back, _ := tt.Find(label); back != inWindow {
			t.Errorf("sidebar hidden %v: the sidebar's buttons moved back in the window, from %v to %v", hidden, inWindow, back)
		}
	}
}

// With the sidebar hidden, the first header has the title bar: it comes
// up to center on the lights' line, and the pane's title with it, as the
// sidebar's buttons do. With the sidebar shown the cards start cardGap
// down, below the sidebar's title bar.
func TestHeaderOnTheLights(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		_, tt, _ := macApp(t, hidden)
		title, ok := tt.Find("fish") // pane a's title
		if !ok {
			t.Fatalf("sidebar hidden %v: no title of pane a; texts %q", hidden, tt.Texts())
		}
		want := float32(cardGap + headerH/2)
		name := "header-sidebar-shown"
		if hidden {
			want, name = barMid, "header-sidebar-hidden"
		}
		if mid(title) != want {
			t.Errorf("sidebar hidden %v: the pane's title centers on %v, not %v", hidden, mid(title), want)
		}
		shot(t, tt, name)
	}
}

// The sidebar's first row starts as far below the lights' line as it did
// under the taller bar, 30 down, and not up against the buttons; the
// rows' pills start at the close light's left edge, 12 in.
func TestSidebarTopSpacing(t *testing.T) {
	a, tt, label := macApp(t, false)
	if !a.set.LinearSidebar {
		t.Fatal("the Linear row is off; the first row is another")
	}
	buttons, _ := tt.Find(label)
	first, ok := tt.Find(locales.LinearGroup()) // the Linear row's, 30 tall
	if !ok {
		t.Fatalf("no Linear row; texts %q", tt.Texts())
	}
	top := mid(first) - 30/2
	if top-barMid != 30 {
		t.Errorf("the first row starts %v below the lights' line, not 30", top-barMid)
	}
	if gap := top - (buttons.Y + buttons.H); gap < 16 {
		t.Errorf("the first row is %v below the sidebar's buttons, crowded", gap)
	}
	project, ok := tt.Find("demo") // a project's name, 10 into its row
	if !ok {
		t.Fatalf("no project row; texts %q", tt.Texts())
	}
	if x := project.X - 10; x != windowLights[0].X {
		t.Errorf("the project's row starts %v in, not at the close light's %v", x, windowLights[0].X)
	}
}

// A window that opens in full screen has had no bar to keep: the sidebar's
// buttons go after the lights where AppKit puts them.
func TestTitleBarAtLaunchInFullScreen(t *testing.T) {
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
	if show.X < systemBar.Left || mid(show) != barMid {
		t.Errorf("the sidebar's buttons at %v are not after the lights, up to %v, and on their line %d", show, systemBar.Left, barMid)
	}
}
