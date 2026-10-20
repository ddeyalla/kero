package main

import "github.com/egoist/mygo/ui"

// On macOS the window's title bar is the compact title bar and toolbar
// (setCompactTitleBar), topBar tall, with the traffic lights where AppKit
// puts them in it. In full screen, where macOS takes them away to a bar
// that slides down with the menu bar, the window shows the system's
// buttons where they were, over its content (followFullScreen, in
// title_bar_darwin.go), so that they stay in sight and nothing moves.

// windowLights are where AppKit puts the traffic lights in the compact
// title bar, in the window, as macOS 26 and later lay them out: 14 wide,
// 23 apart, the close button 12 in, centered on barMid. In full screen
// the lights go where they were read in the window, or here.
var windowLights = [3]ui.Rect{
	{X: 12, Y: barMid - 7, W: 14, H: 14},
	{X: 12 + 23, Y: barMid - 7, W: 14, H: 14},
	{X: 12 + 2*23, Y: barMid - 7, W: 14, H: 14},
}

// systemBar is the room the traffic lights take in the compact title bar,
// as Context.TitleBar has it, from where AppKit puts them.
var systemBar = ui.TitleBar{Height: topBar, Left: windowLights[2].X + windowLights[2].W}

// titleBar is the room the traffic lights take, as Context.TitleBar has
// it. In full screen, where the bar is empty, it stays as it was in the
// window, where the lights still are, so that the sidebar's buttons do
// not move. A window that opens in full screen has had no bar to keep:
// the room AppKit gives the lights.
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
