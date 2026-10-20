package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// The title bar is as low as a pane's header allows: the header, which
// centers on barMid as the bar does, still holds a pane's title, 28 tall.
func TestTitleBarHeight(t *testing.T) {
	if topBar > 52 {
		t.Errorf("the title bar is %d tall, taller than macOS's unified toolbar", topBar)
	}
	if headerH < 28+2 {
		t.Errorf("a header of %d leaves no room around a pane's title, 28 tall", headerH)
	}
}

// windowLights is the room the system's traffic lights take in the window,
// as MyGo has it with TrafficLightPosition: the buttons, 14 wide and 20
// apart, from lightsX, in a bar that leaves as much room below them as
// above.
var windowLights = ui.TitleBar{Height: 2 * barMid, Left: lightsX + lightsSpan}

// In full screen macOS hides its traffic lights, and Kero draws its own
// where they were, centered on the bar with the sidebar's buttons, which
// stay where they were in the window.
func TestFullScreenLights(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	tt.SetTitleBar(windowLights)
	tt.Frame()
	hide := a.tip(locales.SidebarHide(), "sidebar.toggle")
	inWindow, ok := tt.Find(hide)
	if !ok {
		t.Fatalf("no button that hides the sidebar; texts %q", tt.Texts())
	}
	if _, ok := tt.Find(locales.WindowExitFullScreen()); ok {
		t.Fatal("Kero draws traffic lights in the window, over the system's")
	}
	if mid := inWindow.Y + inWindow.H/2; mid != barMid {
		t.Errorf("the sidebar's buttons center on %v, not on the lights' %d", mid, barMid)
	}

	// Going full screen: the system's lights hide, and the title bar
	// empties, before the window tells it went full screen. Nothing moves.
	tt.SetTitleBar(ui.TitleBar{})
	tt.Frame()
	if going, _ := tt.Find(hide); going != inWindow {
		t.Errorf("the sidebar's buttons moved as the window went full screen, from %v to %v", inWindow, going)
	}
	if _, ok := tt.Find(locales.WindowExitFullScreen()); ok {
		t.Error("Kero drew its lights before the window was in full screen")
	}

	// Full screen: Kero's lights where the system's were.
	a.fullScreen = true
	tt.SetTitleBar(ui.TitleBar{})
	tt.Frame()
	closeR, ok := tt.Find(locales.CommonClose())
	if !ok {
		t.Fatalf("no close button in full screen; texts %q", tt.Texts())
	}
	mini, _ := tt.Find(locales.WindowMinimize())
	zoom, ok := tt.Find(locales.WindowExitFullScreen())
	if !ok {
		t.Fatalf("no button that leaves full screen; texts %q", tt.Texts())
	}
	if closeR.X != lightsX || zoom.X+zoom.W != windowLights.Left {
		t.Errorf("the lights span %v to %v, not where the system's are, %d to %v", closeR.X, zoom.X+zoom.W, lightsX, windowLights.Left)
	}
	for _, r := range []ui.Rect{closeR, mini, zoom} {
		if mid := r.Y + r.H/2; mid != barMid {
			t.Errorf("a light centers on %v, not on %d", mid, barMid)
		}
	}
	inFull, _ := tt.Find(hide)
	if inFull != inWindow {
		t.Errorf("the sidebar's buttons moved in full screen, from %v to %v", inWindow, inFull)
	}
	if inFull.X < zoom.X+zoom.W {
		t.Errorf("the sidebar's buttons at %v are under the lights, which end at %v", inFull.X, zoom.X+zoom.W)
	}

	// The lights have their colors with the window in front, and grey
	// out behind; minimize, which full screen does not allow, is grey.
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
	shot(t, tt, "full-screen-lights")
	// The pointer over them shows what each does.
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
	shot(t, tt, "full-screen-lights-behind")

	// Back in the window: the system's lights again, and none of Kero's.
	a.fullScreen = false
	tt.SetTitleBar(windowLights)
	tt.Frame()
	if _, ok := tt.Find(locales.WindowExitFullScreen()); ok {
		t.Error("Kero's lights stayed after the window left full screen")
	}
}

// With the sidebar hidden, its buttons come into the header, after the
// lights, where they are in the window and in full screen alike.
func TestFullScreenLightsSidebarHidden(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	a := testApp(t)
	a.prefs.SidebarHidden = true
	tt := ui.NewTester(a.view, 1100, 640)
	tt.SetTitleBar(windowLights)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	show := a.tip(locales.SidebarShow(), "sidebar.toggle")
	inWindow, ok := tt.Find(show)
	if !ok {
		t.Fatalf("no button that shows the sidebar; texts %q", tt.Texts())
	}
	a.fullScreen = true
	tt.SetTitleBar(ui.TitleBar{})
	tt.Frame()
	inFull, _ := tt.Find(show)
	if inFull != inWindow {
		t.Errorf("the sidebar's buttons moved in full screen, from %v to %v", inWindow, inFull)
	}
	if zoom, _ := tt.Find(locales.WindowExitFullScreen()); inFull.X < zoom.X+zoom.W {
		t.Errorf("the sidebar's buttons at %v are under the lights, which end at %v", inFull.X, zoom.X+zoom.W)
	}
	if mid := inFull.Y + inFull.H/2; mid != barMid {
		t.Errorf("the header's buttons center on %v, not on the lights' %d", mid, barMid)
	}
	shot(t, tt, "full-screen-lights-sidebar-hidden")
}

// A window that opens in full screen has had no title bar to keep: the
// lights go where the system's would be.
func TestFullScreenLightsAtLaunch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Kero draws its own window controls elsewhere")
	}
	a := testApp(t)
	a.fullScreen = true
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	zoom, ok := tt.Find(locales.WindowExitFullScreen())
	if !ok {
		t.Fatalf("no button that leaves full screen; texts %q", tt.Texts())
	}
	if right := zoom.X + zoom.W; right != lightsX+lightsSpan {
		t.Errorf("the lights end at %v, not at %d", right, lightsX+lightsSpan)
	}
	hide, _ := tt.Find(a.tip(locales.SidebarHide(), "sidebar.toggle"))
	if hide.X < zoom.X+zoom.W {
		t.Errorf("the sidebar's buttons at %v are under the lights, which end at %v", hide.X, zoom.X+zoom.W)
	}
}
