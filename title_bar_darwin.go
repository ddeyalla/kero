//go:build darwin

package main

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// nsRect is an NSRect, from its bottom-left corner.
type nsRect struct{ X, Y, W, H float64 }

const (
	toolbarStyleUnifiedCompact = 4       // NSWindowToolbarStyleUnifiedCompact
	styleFullScreen            = 1 << 14 // NSWindowStyleMaskFullScreen
)

var sel = objc.RegisterName

// fullScreenBlocks are the blocks that follow the window in and out of
// full screen, kept for as long as the app runs.
var fullScreenBlocks []objc.Block

// setCompactTitleBar gives a window that MyGo made with an inset title
// bar (TitleBarHiddenInset) the compact style of toolbar: its empty
// toolbar makes the title bar topBar tall, with the traffic lights
// centered in it, where AppKit puts them.
func setCompactTitleBar(w *mygo.Window) {
	win := objc.ID(w.NativeHandle())
	if win == 0 || win.Send(sel("toolbar")) == 0 {
		return
	}
	win.Send(sel("setToolbarStyle:"), toolbarStyleUnifiedCompact)
}

// followFullScreen keeps the traffic lights in sight in full screen. From
// as the window starts to go full screen, Kero draws them (drawsLights),
// where the system's were in the window, and the system's hide, with the
// bar that would bring them down over the window with the menu bar, which
// clicks then go through. Back in the window, the system's come back.
func (a *App) followFullScreen(w *mygo.Window) {
	win := objc.ID(w.NativeHandle())
	if win == 0 {
		return
	}
	center := objc.ID(objc.GetClass("NSNotificationCenter")).Send(sel("defaultCenter"))
	observe := func(name string, fn func()) {
		b := objc.NewBlock(func(objc.Block, objc.ID) { fn() })
		fullScreenBlocks = append(fullScreenBlocks, b)
		center.Send(sel("addObserverForName:object:queue:usingBlock:"), nsString(name), win, objc.ID(0), b)
	}
	// Going full screen, before the window grows, and once there, where
	// the lights have moved to the bar that slides down: a window that
	// opens in full screen goes there before Kero follows it.
	observe("NSWindowWillEnterFullScreenNotification", func() { a.hideLights(win) })
	observe("NSWindowDidEnterFullScreenNotification", func() { a.hideLights(win) })
	observe("NSWindowDidExitFullScreenNotification", func() { a.showLights(win) })
	if objc.Send[uint](win, sel("styleMask"))&styleFullScreen != 0 {
		a.hideLights(win)
	}
}

// hideLights hides the system's traffic lights and the title bar around
// them, and has Kero draw its own. While the lights are still in the
// window, it reads where they are, for Kero's to go there.
func (a *App) hideLights(win objc.ID) {
	closeBtn := win.Send(sel("standardWindowButton:"), 0)
	if closeBtn == 0 {
		return
	}
	var at [3]ui.Rect
	read := closeBtn.Send(sel("window")) == win && !objc.Send[bool](closeBtn, sel("isHidden"))
	if read {
		height := objc.Send[nsRect](win, sel("frame")).H
		for i := range at {
			btn := win.Send(sel("standardWindowButton:"), i)
			f := objc.Send[nsRect](btn, sel("frame"))
			r := objc.Send[nsRect](btn.Send(sel("superview")), sel("convertRect:toView:"), f, objc.ID(0))
			at[i] = ui.Rect{X: float32(r.X), Y: float32(height - r.Y - r.H), W: float32(r.W), H: float32(r.H)}
		}
	}
	// In full screen the title bar is in a window of its own, clear, which
	// slides down over the top of the window with the menu bar: with the
	// title bar's container hidden, its background with it, nothing shows
	// in it, and the pointer reaches Kero's lights through it.
	setLightsHidden(win, true)
	if bar := closeBtn.Send(sel("window")); bar != 0 && bar != win {
		bar.Send(sel("setIgnoresMouseEvents:"), true)
	}
	a.update(func() {
		a.fullScreen = true
		if read {
			a.lights, a.lightsRead = at, true
		}
	})
}

// showLights brings the system's traffic lights back, in the window.
func (a *App) showLights(win objc.ID) {
	setLightsHidden(win, false)
	a.update(func() { a.fullScreen = false })
}

// setLightsHidden hides or shows the traffic lights and the title bar's
// container around them.
func setLightsHidden(win objc.ID, hidden bool) {
	for i := range 3 {
		if btn := win.Send(sel("standardWindowButton:"), i); btn != 0 {
			btn.Send(sel("setHidden:"), hidden)
		}
	}
	closeBtn := win.Send(sel("standardWindowButton:"), 0)
	if closeBtn == 0 {
		return
	}
	if container := closeBtn.Send(sel("superview")).Send(sel("superview")); container != 0 {
		container.Send(sel("setHidden:"), hidden)
	}
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), s+"\x00")
}
