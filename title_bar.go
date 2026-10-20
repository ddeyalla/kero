package main

import "github.com/egoist/mygo/ui"

// systemBar is the room AppKit gives the traffic lights in the unified
// title bar that MyGo's toolbar makes (TitleBarHiddenInset), where they
// are by default: the close button 19 in, the buttons 14 wide and 23
// apart, as macOS 26 and later lay them out, centered on barMid.
var systemBar = ui.TitleBar{Height: topBar, Left: 19 + 2*23 + 14}

// titleBar is the room the traffic lights take, as Context.TitleBar has
// it. In full screen macOS hides them, to slide them down with the menu
// bar as the pointer reaches the top of the screen, and the bar is empty
// there: it stays as it was in the window, so that the sidebar's buttons
// do not move as the window goes full screen, nor sit under the lights as
// they come down. A window that opens in full screen has had no bar to
// keep, and keeps room where AppKit puts the lights.
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
