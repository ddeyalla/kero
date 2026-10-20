package main

import (
	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// In full screen macOS hides a window's traffic lights in the title bar
// that slides down with the menu bar, only as the pointer reaches the top
// of the screen. Kero draws its own there instead, where the system's are
// in the window, so that they stay in sight, and the sidebar's buttons
// after them stay where they were.

const (
	lightsX = 18 // the close button's left edge, in the window
	lightW  = 14 // a button, around its light
	lightH  = 16
	lightD  = 12 // the light
	// lightsSpan is from the close button's left edge to the zoom
	// button's right edge, as AppKit spaces them.
	lightsSpan = 2*20 + lightW
)

var (
	closeLight    = ui.RGB(255, 95, 87)
	minimizeLight = ui.RGB(254, 188, 46)
	zoomLight     = ui.RGB(40, 200, 64)

	// What the lights show as the pointer comes over them: the green one
	// leaves full screen, as two arrows that meet.
	closeGlyph          = lightGlyph(`<path d="M3.8 3.8l4.4 4.4M8.2 3.8l-4.4 4.4" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>`)
	minimizeGlyph       = lightGlyph(`<path d="M3.2 6h5.6" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>`)
	exitFullScreenGlyph = lightGlyph(`<path d="M6.6 2.8v2.6h2.6zM5.4 9.2V6.6H2.8z" fill="currentColor"/>`)
)

func lightGlyph(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 12 12" fill="none">` + shapes + `</svg>`))
}

// drawsLights tells that Kero draws the traffic lights itself: in full
// screen on macOS. Elsewhere it draws its own window controls always
// (ownControls), at the right.
func (a *App) drawsLights() bool { return a.fullScreen && !ownControls }

// titleBar is the room the traffic lights take, as Context.TitleBar has
// it, but in full screen on macOS, where the system's hide and the bar is
// empty, the room Kero's own take: as the bar was in the window, so that
// nothing moves as the window goes full screen. The bar empties as the
// window starts to go full screen, and the window tells it went once
// there: in between, as it grows, the bar is the window's too.
func (a *App) titleBar(c *ui.Context) ui.TitleBar {
	bar := c.TitleBar()
	switch {
	case !a.drawsLights() && bar.Left > 0:
		a.windowBar = bar
		return bar
	case ownControls || a.windowBar.Left == 0 && !a.fullScreen:
		return bar
	case a.windowBar.Left > 0:
		return a.windowBar
	}
	// The window opened in full screen: where the lights would be.
	return ui.TitleBar{Height: topBar, Left: lightsX + lightsSpan}
}

// fullScreenLights draws the traffic lights in full screen on macOS, over
// the sidebar and the cards, centered on barMid as the system's are in
// the window: close; minimize, which a window in full screen cannot do,
// greyed as macOS has it there; and the green one, which leaves full
// screen. They show their glyphs together as the pointer comes over them,
// and grey out while the window is in the background, as the system's do.
func (a *App) fullScreenLights(c *ui.Context, pal *palette, bar ui.TitleBar) {
	if !a.drawsLights() {
		return
	}
	// Spaced to end where the bar does, which is where the system's end.
	gap := max((bar.Left-lightsX-3*lightW)/2, 0)
	group := ui.Row(c.Key("traffic-lights")).Absolute().Left(lightsX).Top(barMid - lightH/2).Height(lightH).Gap(gap).AlignItems(ui.Center)
	over := group.Hovered()
	group.Children(func() {
		a.light(c, pal, "light-close", locales.CommonClose(), closeLight, closeGlyph, over, true, func() {
			if a.win != nil {
				a.win.Close()
			}
		})
		a.light(c, pal, "light-minimize", locales.WindowMinimize(), minimizeLight, minimizeGlyph, over, false, nil)
		a.light(c, pal, "light-zoom", locales.WindowExitFullScreen(), zoomLight, exitFullScreenGlyph, over, true, func() {
			if a.win != nil {
				a.win.SetFullScreen(false)
			}
		})
	})
}

// light is one of the traffic lights: its color while the window is in
// front or the pointer is over the lights, grey otherwise or when it
// cannot be used.
func (a *App) light(c *ui.Context, pal *palette, key, label string, color ui.Color, glyph *ui.SVG, over, enabled bool, fn func()) {
	grey := ui.RGBA(0, 0, 0, 0.14)
	if pal.dark {
		grey = ui.RGBA(255, 255, 255, 0.2)
	}
	b := ui.ButtonBase(c.Key(key)).Size(lightW, lightH).Center().Label(label).Disabled(!enabled)
	fill := color
	if !enabled || !a.focused && !over {
		fill = grey
	}
	ink := ui.RGBA(0, 0, 0, 0.5)
	if b.Pressed() {
		ink = ui.RGBA(0, 0, 0, 0.75)
	}
	b.Children(func() {
		dot := ui.Box(c).Size(lightD, lightD).Radius(lightD/2).Center().Background(fill).Border(0.5, ui.RGBA(0, 0, 0, 0.12))
		dot.Children(func() {
			if over && enabled {
				ui.Icon(c, glyph).Size(lightD, lightD).TextColor(ink)
			}
		})
	})
	if enabled && b.Clicked() && fn != nil {
		fn()
	}
}
