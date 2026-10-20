//go:build darwin

package main

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// nsRect is an NSRect, from its bottom-left corner, and nsPoint an
// NSPoint.
type (
	nsRect  struct{ X, Y, W, H float64 }
	nsPoint struct{ X, Y float64 }
)

const (
	toolbarStyleUnifiedCompact = 4       // NSWindowToolbarStyleUnifiedCompact
	styleFullScreen            = 1 << 14 // NSWindowStyleMaskFullScreen
	// NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect
	trackingOptions = 0x01 | 0x80 | 0x200
	// NSViewMaxXMargin|NSViewMaxYMargin: in a flipped view, the top-left
	// corner stays put as the window grows.
	pinTopLeft = 4 | 32
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

// fullScreenLights are the traffic lights a window shows in full screen,
// where macOS takes its own away, to a bar that slides down over the top
// of the window with the menu bar. They are the system's buttons all the
// same, made by NSWindow as its own are, in a group of their own over the
// window's content, where the window's were: close closes the window,
// the green one leaves full screen, and minimize, which a window in full
// screen cannot do, is disabled, as macOS has it there. They show their
// glyphs together as the pointer comes over the group, as in a title bar:
// AppKit's buttons ask their superview with _mouseInGroup:, as Electron's
// own groups of them answer it, and look again on mouseEnteredOrExited.
type fullScreenLights struct {
	win     objc.ID
	group   objc.ID // a ShibuTrafficLights view, over the content
	buttons [3]objc.ID
	at      [3]nsRect // where the window's lights were, from its top-left
	read    bool
	over    bool
}

// lightsByGroup finds a group's lights, for its methods.
var lightsByGroup = map[objc.ID]*fullScreenLights{}

// lightsClass is the class of the lights' group: a flipped view that
// tracks the pointer, and tells the buttons whether it is over them with
// _mouseInGroup:, which AppKit's buttons ask their superview, as the
// title bar answers it.
var lightsClass objc.Class

func lightsGroupClass() objc.Class {
	if lightsClass != 0 {
		return lightsClass
	}
	hover := func(self objc.ID, over bool) {
		l := lightsByGroup[self]
		if l == nil {
			return
		}
		l.over = over
		// Each asks the group again, and shows its glyph or not.
		for _, b := range l.buttons {
			if b != 0 && objc.Send[bool](b, sel("respondsToSelector:"), sel("mouseEnteredOrExited")) {
				b.Send(sel("mouseEnteredOrExited"))
			}
		}
	}
	cls, err := objc.RegisterClass("ShibuTrafficLights", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
		{Cmd: sel("isFlipped"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: sel("_mouseInGroup:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) bool {
			l := lightsByGroup[self]
			return l != nil && l.over
		}},
		{Cmd: sel("mouseEntered:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) { hover(self, true) }},
		{Cmd: sel("mouseExited:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) { hover(self, false) }},
	})
	if err != nil {
		return 0
	}
	lightsClass = cls
	return cls
}

// followFullScreen keeps the window's traffic lights in sight in full
// screen, from as the window starts to go there until it is back.
func followFullScreen(w *mygo.Window) {
	win := objc.ID(w.NativeHandle())
	if win == 0 {
		return
	}
	l := &fullScreenLights{win: win}
	center := objc.ID(objc.GetClass("NSNotificationCenter")).Send(sel("defaultCenter"))
	observe := func(name string, fn func()) {
		b := objc.NewBlock(func(objc.Block, objc.ID) { fn() })
		fullScreenBlocks = append(fullScreenBlocks, b)
		center.Send(sel("addObserverForName:object:queue:usingBlock:"), nsString(name), win, objc.ID(0), b)
	}
	// Going full screen, before the window grows; once there, as a
	// window that opens in full screen goes there before Kero follows
	// it; and as the window changes size or comes to the front, from its
	// style, in case a change went by unseen.
	observe("NSWindowWillEnterFullScreenNotification", l.enter)
	observe("NSWindowDidEnterFullScreenNotification", l.enter)
	observe("NSWindowDidExitFullScreenNotification", l.leave)
	observe("NSWindowDidResizeNotification", l.sync)
	observe("NSWindowDidBecomeKeyNotification", l.sync)
	l.sync()
}

func (l *fullScreenLights) fullScreen() bool {
	return objc.Send[uint](l.win, sel("styleMask"))&styleFullScreen != 0
}

// sync puts the lights as the window's style says.
func (l *fullScreenLights) sync() {
	switch {
	case l.fullScreen():
		l.enter()
	case l.group != 0:
		l.leave()
	}
}

// enter shows the lights in their group where the window's were, then
// hides the window's own, and the title bar's container around them,
// whose background is that of the bar that slides down in full screen,
// which then shows nothing and lets clicks through.
func (l *fullScreenLights) enter() {
	closeBtn := l.win.Send(sel("standardWindowButton:"), 0)
	if closeBtn == 0 {
		return
	}
	// While the lights are still in the window, where they are.
	if closeBtn.Send(sel("window")) == l.win && !objc.Send[bool](closeBtn, sel("isHidden")) {
		height := objc.Send[nsRect](l.win, sel("frame")).H
		for i := range l.at {
			btn := l.win.Send(sel("standardWindowButton:"), i)
			f := objc.Send[nsRect](btn, sel("frame"))
			r := objc.Send[nsRect](btn.Send(sel("superview")), sel("convertRect:toView:"), f, objc.ID(0))
			l.at[i] = nsRect{X: r.X, Y: height - r.Y - r.H, W: r.W, H: r.H}
		}
		l.read = true
	}
	// Without the group, the window keeps macOS's own bar that slides
	// down with the menu bar, lights and all: never no lights at all.
	if l.show(); l.group == 0 || l.buttons[0] == 0 || l.buttons[2] == 0 {
		return
	}
	setLightsHidden(l.win, true)
	if bar := closeBtn.Send(sel("window")); bar != 0 && bar != l.win {
		bar.Send(sel("setIgnoresMouseEvents:"), true)
	}
	// AppKit hides minimize as the window goes full screen, where it
	// cannot: in sight, greyed, as in the bar that slides down.
	for _, b := range l.buttons {
		if b != 0 {
			b.Send(sel("setHidden:"), false)
		}
	}
}

// show puts the lights' group over the window's content.
func (l *fullScreenLights) show() {
	if l.group != 0 {
		return
	}
	cls := lightsGroupClass()
	content := l.win.Send(sel("contentView"))
	if cls == 0 || content == 0 {
		return
	}
	at := l.at
	if !l.read {
		for i, r := range windowLights {
			at[i] = nsRect{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)}
		}
	}
	group := objc.ID(cls).Send(sel("alloc")).Send(sel("initWithFrame:"), nsRect{
		X: at[0].X, Y: at[0].Y, W: at[2].X + at[2].W - at[0].X, H: at[0].H,
	})
	if !objc.Send[bool](content, sel("isFlipped")) {
		// From the bottom, the top kept as the window grows.
		h := objc.Send[nsRect](content, sel("bounds")).H
		f := objc.Send[nsRect](group, sel("frame"))
		f.Y = h - at[0].Y - at[0].H
		group.Send(sel("setFrame:"), f)
		group.Send(sel("setAutoresizingMask:"), 4|8) // NSViewMaxXMargin|NSViewMinYMargin
	} else {
		group.Send(sel("setAutoresizingMask:"), pinTopLeft)
	}
	style := objc.Send[uint](l.win, sel("styleMask")) &^ styleFullScreen
	actions := [3]string{"performClose:", "", "toggleFullScreen:"}
	for i := range l.buttons {
		b := objc.ID(objc.GetClass("NSWindow")).Send(sel("standardWindowButton:forStyleMask:"), i, style)
		if b == 0 {
			continue
		}
		b.Send(sel("setFrameOrigin:"), nsPoint{at[i].X - at[0].X, 0})
		if actions[i] == "" {
			b.Send(sel("setEnabled:"), false)
		} else {
			b.Send(sel("setTarget:"), l.win)
			b.Send(sel("setAction:"), sel(actions[i]))
		}
		group.Send(sel("addSubview:"), b)
		l.buttons[i] = b
	}
	area := objc.ID(objc.GetClass("NSTrackingArea")).Send(sel("alloc")).Send(sel("initWithRect:options:owner:userInfo:"),
		nsRect{}, uint(trackingOptions), group, objc.ID(0))
	group.Send(sel("addTrackingArea:"), area)
	area.Send(sel("release"))
	content.Send(sel("addSubview:"), group)
	l.group = group
	lightsByGroup[group] = l
}

// leave takes the lights' group away, and brings the window's own back.
func (l *fullScreenLights) leave() {
	if l.group != 0 {
		delete(lightsByGroup, l.group)
		l.group.Send(sel("removeFromSuperview"))
		l.group.Send(sel("release"))
		l.group, l.buttons, l.over = 0, [3]objc.ID{}, false
	}
	setLightsHidden(l.win, false)
}

// setLightsHidden hides or shows the window's traffic lights and the
// title bar's container around them.
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
