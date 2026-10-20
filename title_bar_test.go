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

// fullScreenApp is the app on macOS, with the sidebar shown or not, and
// the label of the sidebar's button that hides or shows it.
func fullScreenApp(t *testing.T, hidden bool) (*App, *ui.Tester, string) {
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

// In full screen macOS takes the traffic lights away, and Kero draws its
// own where they were in the window. The sidebar's buttons stay centered
// on them, where they were, with the sidebar shown or hidden.
func TestFullScreenLights(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		a, tt, label := fullScreenApp(t, hidden)
		inWindow, ok := tt.Find(label)
		if !ok {
			t.Fatalf("sidebar hidden %v: no button %q; texts %q", hidden, label, tt.Texts())
		}
		// Their bottom edge: in the header, the card's edge takes the top
		// point of the button's 26, which starts a point above the card.
		if bottom := inWindow.Y + inWindow.H; bottom != barMid+13 {
			t.Errorf("sidebar hidden %v: the sidebar's buttons end at %v, not centered on the lights' %d", hidden, bottom, barMid)
		}
		if inWindow.X < systemBar.Left {
			t.Errorf("sidebar hidden %v: the sidebar's buttons at %v are under the lights, which end at %v", hidden, inWindow.X, systemBar.Left)
		}
		if _, ok := tt.Find(locales.WindowExitFullScreen()); ok {
			t.Fatalf("sidebar hidden %v: Kero draws traffic lights in the window, over the system's", hidden)
		}

		// Full screen: where the system's were, as read in the window.
		a.fullScreen = true
		a.lights, a.lightsRead = windowLights, true
		tt.SetTitleBar(ui.TitleBar{})
		tt.Frame()
		for i, l := range []string{locales.CommonClose(), locales.WindowMinimize(), locales.WindowExitFullScreen()} {
			r, ok := tt.Find(l)
			if !ok {
				t.Fatalf("sidebar hidden %v: no light %q in full screen; texts %q", hidden, l, tt.Texts())
			}
			if r != windowLights[i] {
				t.Errorf("sidebar hidden %v: light %q at %v, not where the system's was, %v", hidden, l, r, windowLights[i])
			}
		}
		if inFull, _ := tt.Find(label); inFull != inWindow {
			t.Errorf("sidebar hidden %v: the sidebar's buttons moved in full screen, from %v to %v", hidden, inWindow, inFull)
		}
		shot(t, tt, "full-screen-lights")

		// Back in the window: the system's lights again, and none of Kero's.
		a.fullScreen = false
		tt.SetTitleBar(systemBar)
		tt.Frame()
		if _, ok := tt.Find(locales.WindowExitFullScreen()); ok {
			t.Errorf("sidebar hidden %v: Kero's lights stayed after the window left full screen", hidden)
		}
		if back, _ := tt.Find(label); back != inWindow {
			t.Errorf("sidebar hidden %v: the sidebar's buttons moved back in the window, from %v to %v", hidden, inWindow, back)
		}
	}
}

// Kero's lights go where the system's were, as read: on another macOS
// they are elsewhere, and the bar kept from the window says so too.
func TestFullScreenLightsWhereRead(t *testing.T) {
	a, tt, label := fullScreenApp(t, true)
	window := ui.TitleBar{Height: 52, Left: 20 + 2*20 + 14}
	tt.SetTitleBar(window)
	tt.Frame()
	inWindow, _ := tt.Find(label)
	read := [3]ui.Rect{{X: 20, Y: 19, W: 14, H: 14}, {X: 40, Y: 19, W: 14, H: 14}, {X: 60, Y: 19, W: 14, H: 14}}
	a.fullScreen, a.lights, a.lightsRead = true, read, true
	tt.SetTitleBar(ui.TitleBar{})
	tt.Frame()
	if zoom, _ := tt.Find(locales.WindowExitFullScreen()); zoom != read[2] {
		t.Errorf("the green light is at %v, not where it was read, %v", zoom, read[2])
	}
	if inFull, _ := tt.Find(label); inFull != inWindow {
		t.Errorf("the sidebar's buttons moved in full screen, from %v to %v", inWindow, inFull)
	}
}

// The lights have their colors with the window in front, show what they do
// under the pointer, and grey out behind; minimize, which full screen does
// not allow, is grey.
func TestFullScreenLightsColors(t *testing.T) {
	a, tt, _ := fullScreenApp(t, false)
	a.fullScreen = true
	tt.SetTitleBar(ui.TitleBar{})
	tt.Frame()
	closeR, _ := tt.Find(locales.CommonClose())
	mini, _ := tt.Find(locales.WindowMinimize())
	at := func(r ui.Rect) [3]uint8 {
		c := tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
		return [3]uint8{c.R, c.G, c.B}
	}
	red := at(closeR)
	if red[0] < 200 || red[1] > 140 {
		t.Errorf("the close light is %v, not red", red)
	}
	if c := at(mini); c[0] > 200 && c[2] < 120 {
		t.Errorf("the minimize light is %v, yellow, though full screen cannot minimize", c)
	}
	tt.Move(closeR.X+closeR.W/2, closeR.Y+closeR.H/2)
	tt.Frame()
	if c := at(closeR); c == red {
		t.Errorf("the close light shows no glyph under the pointer: %v", c)
	}
	shot(t, tt, "full-screen-lights-over")
	tt.Move(600, 400)
	a.focused = false
	tt.Frame()
	if c := at(closeR); c[0] > 200 && c[1] < 140 {
		t.Errorf("the close light is %v, red, with the window behind", c)
	}
}

// A window that opens in full screen has had no bar to keep, nor lights to
// read: Kero's go where AppKit puts the system's, and the sidebar's
// buttons after them.
func TestFullScreenLightsAtLaunch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	a := testApp(t)
	a.prefs.SidebarHidden = true
	a.fullScreen = true
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	zoom, ok := tt.Find(locales.WindowExitFullScreen())
	if !ok {
		t.Fatalf("no button that leaves full screen; texts %q", tt.Texts())
	}
	if zoom != windowLights[2] {
		t.Errorf("the green light is at %v, not where AppKit puts it, %v", zoom, windowLights[2])
	}
	show, _ := tt.Find(a.tip(locales.SidebarShow(), "sidebar.toggle"))
	if show.X < zoom.X+zoom.W {
		t.Errorf("the sidebar's buttons at %v are under the lights, which end at %v", show.X, zoom.X+zoom.W)
	}
	shot(t, tt, "full-screen-lights-at-launch")
}
