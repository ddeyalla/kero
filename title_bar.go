package main

import (
	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// On macOS the window's title bar is the compact title bar and toolbar
// (setCompactTitleBar), topBar tall, with the traffic lights where AppKit
// puts them in it. In full screen macOS takes the lights away, to a bar
// that slides down over the top of the window with the menu bar. Kero
// keeps that bar empty and lets clicks through it (title_bar_darwin.go),
// and draws the lights itself where they were in the window, so that they
// stay in sight and nothing moves going in or out of full screen.

// windowLights are where AppKit puts the traffic lights in the compact
// title bar, in the window, as macOS 26 and later lay them out: 14 wide,
// 23 apart, the close button 12 in, centered on barMid. The lights as
// they were in the window replace them once read (lightsRead).
var windowLights = [3]ui.Rect{
	{X: 12, Y: barMid - 7, W: 14, H: 14},
	{X: 12 + 23, Y: barMid - 7, W: 14, H: 14},
	{X: 12 + 2*23, Y: barMid - 7, W: 14, H: 14},
}

// systemBar is the room the traffic lights take in the compact title bar,
// as Context.TitleBar has it, from where AppKit puts them.
var systemBar = ui.TitleBar{Height: topBar, Left: windowLights[2].X + windowLights[2].W}

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
// screen on macOS, from as the window starts to go there until it is back
// (fullScreen). Elsewhere it draws its own window controls always
// (ownControls), at the right.
func (a *App) drawsLights() bool { return a.fullScreen && !ownControls }

// titleBar is the room the traffic lights take, as Context.TitleBar has
// it. In full screen, where the system's are away and the bar is empty,
// it stays as it was in the window, so that the sidebar's buttons do not
// move, and keep clear of the lights Kero draws there. A window that
// opens in full screen has had no bar to keep: the room AppKit gives the
// lights.
func (a *App) titleBar(c *ui.Context) ui.TitleBar {
	bar := c.TitleBar()
	switch {
	case ownControls:
		return bar
	case bar.Left > 0:
		a.windowBar = bar
		return bar
	case a.windowBar.Left > 0:
		return a.windowBar
	}
	return systemBar
}

// fullScreenLights draws the traffic lights in full screen on macOS, over
// the sidebar and the cards, where the system's were in the window: close;
// minimize, which a window in full screen cannot do, greyed as macOS has
// it there; and the green one, which leaves full screen. They show their
// glyphs together as the pointer comes over them, and grey out while the
// window is in the background, as the system's do.
func (a *App) fullScreenLights(c *ui.Context, pal *palette) {
	if !a.drawsLights() {
		return
	}
	at := windowLights
	if a.lightsRead {
		at = a.lights
	}
	gap := max(at[1].X-at[0].X-at[0].W, 0)
	group := ui.Row(c.Key("traffic-lights")).Absolute().Left(at[0].X).Top(at[0].Y).Height(at[0].H).Gap(gap).AlignItems(ui.Center)
	over := group.Hovered()
	group.Children(func() {
		a.light(c, pal, at[0], "light-close", locales.CommonClose(), closeLight, closeGlyph, over, true, func() {
			if a.win != nil {
				a.win.Close()
			}
		})
		a.light(c, pal, at[1], "light-minimize", locales.WindowMinimize(), minimizeLight, minimizeGlyph, over, false, nil)
		a.light(c, pal, at[2], "light-zoom", locales.WindowExitFullScreen(), zoomLight, exitFullScreenGlyph, over, true, func() {
			if a.win != nil {
				a.win.SetFullScreen(false)
			}
		})
	})
}

// light is one of the traffic lights, in its button's room r: its color
// while the window is in front or the pointer is over the lights, grey
// otherwise or when it cannot be used.
func (a *App) light(c *ui.Context, pal *palette, r ui.Rect, key, label string, color ui.Color, glyph *ui.SVG, over, enabled bool, fn func()) {
	grey := ui.RGBA(0, 0, 0, 0.14)
	if pal.dark {
		grey = ui.RGBA(255, 255, 255, 0.2)
	}
	b := ui.ButtonBase(c.Key(key)).Size(r.W, r.H).Center().Label(label).Disabled(!enabled)
	fill := color
	if !enabled || !a.focused && !over {
		fill = grey
	}
	ink := ui.RGBA(0, 0, 0, 0.5)
	if b.Pressed() {
		ink = ui.RGBA(0, 0, 0, 0.75)
	}
	d := min(r.W, r.H) - 2 // the light, inside its button
	b.Children(func() {
		dot := ui.Box(c).Size(d, d).Radius(d/2).Center().Background(fill).Border(0.5, ui.RGBA(0, 0, 0, 0.12))
		dot.Children(func() {
			if over && enabled {
				ui.Icon(c, glyph).Size(d, d).TextColor(ink)
			}
		})
	})
	if enabled && b.Clicked() && fn != nil {
		fn()
	}
}
