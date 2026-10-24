package main

// The usage icon's popover: the Usage plugin's own list, drawn through the
// shared plugin host, with this Mac's CPU and RAM under it. The popover is a
// transient plugin surface with its own id, so it runs beside a real Usage
// pane; closing it closes only its pane, and the plugin's refresh stops
// when no pane of it is visible.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/locales"
)

// navUsagePane is the transient surface's id. It names a pane to the plugin
// host only: there is no daemon pane, and no saved state, behind it.
const navUsagePane = "@nav/usage"

// navUsageChrome is the room the popover's own header and foot take from
// its card, where the plugin's tree draws. navUsageListHeight is the least
// size the plugin is told the popover has: the full list, with every
// allowance and reset time, scrolls in a short window rather than folding
// into the pane's compact form.
const (
	navUsageChrome     = 134
	navUsageListHeight = 420
	// navUsageAnchorGap is the gap from the trigger, and navUsageWindowGap
	// the margin the toolkit keeps between an attached panel and the window.
	navUsageAnchorGap = 6
	navUsageWindowGap = 4
)

// navUsageState is the popover's lifecycle. armed is false after a dismissal
// until the pointer leaves the trigger, so a late hover frame does not open
// what the user just closed.
type navUsageState struct {
	open         bool
	pinned       bool
	armed        bool
	keyboard     bool
	focusPending bool
	cardOver     bool
	hadFocus     bool
	drawn        bool
	triggerOver  bool

	trigger ui.Handle
}

// usageNavButton is the usage icon at the trailing edge, in the sidebar's
// footer and in the tab row. Hover opens the popover; a click pins it, or
// closes it when it is pinned.
func (a *App) usageNavButton(c *ui.Context, pal *palette) ui.Element {
	b := navIconButton(c, "usage-icon", "gauge", glyph("f7-slider-horizontal-3"),
		locales.UsageTitle(), locales.NavUsageTip(), pal)
	b.Bind(&a.navUsage.trigger)
	a.navUsage.drawn, a.navUsage.triggerOver = true, b.Hovered()
	if b.Clicked() {
		a.toggleNavUsage(b.FocusVisible())
	}
	a.navUsageView(c, pal, b)
	return b
}

// toggleNavUsage opens the popover, pins an open one, or closes a pinned
// one. A keyboard open keeps the popover open for the controls, and gives
// the popover the focus.
func (a *App) toggleNavUsage(fromKeyboard bool) {
	st := &a.navUsage
	if st.open {
		if st.pinned {
			a.dismissNavUsage(false)
			return
		}
		st.pinned = true
		// A click is the user asking for the card: the controls and Escape
		// belong to it until it closes. A hover alone does neither.
		st.focusPending = true
		if fromKeyboard {
			st.keyboard = true
		}
		return
	}
	st.open, st.pinned, st.armed = true, true, true
	st.keyboard, st.focusPending = fromKeyboard, true
}

// dismissNavUsage closes the popover, stops its plugin surface, and, when
// the user dismissed it from the keyboard, gives the keyboard back to the
// trigger. A closed popover is still reset: the framework may have closed
// it first, and a stale pin or keyboard flag must not open it again.
func (a *App) dismissNavUsage(restoreFocus bool) {
	st := &a.navUsage
	st.open, st.pinned = false, false
	st.keyboard, st.focusPending = false, false
	st.cardOver, st.hadFocus = false, false
	st.armed = st.drawn && !st.triggerOver
	// A hover timer from before the dismissal must not reopen it.
	delete(a.hoverSeen, "nav-usage")
	delete(a.hoverUntil, "nav-usage")
	a.closeNavUsage()
	if restoreFocus {
		st.trigger.Focus()
	}
}

// refreshNavUsage asks an open popover's plugin to read its accounts and
// logs now, as its own refresh button does.
func (a *App) refreshNavUsage() {
	if !a.navUsage.open {
		return
	}
	if _, ok := a.plugins.panes[navUsagePane]; !ok {
		return
	}
	a.pluginHost().Event(navUsagePane, "usage-refresh", protocol.KindPress, nil)
}

// closeNavUsage stops the transient surface: the plugin hears close, and its
// refresh stops when this was its last visible pane. A real Usage pane, if
// open, keeps its own.
func (a *App) closeNavUsage() {
	if a.plugins.panes == nil {
		return
	}
	if _, ok := a.plugins.panes[navUsagePane]; !ok {
		return
	}
	if a.plugins.host != nil {
		a.plugins.host.Close(navUsagePane)
	}
	delete(a.plugins.panes, navUsagePane)
	if a.ambient.drop(navUsagePane) {
		a.ambientChanged()
	}
}

// navUsageView draws the popover above the shell, anchored to the usage icon
// of whichever layout shows it. Hover opens it; leaving both the icon and
// the card closes it after the shared hover's short grace, which lets the
// pointer cross the gap. Its content is the real Usage plugin.
func (a *App) navUsageView(c *ui.Context, pal *palette, trig ui.Element) {
	st := &a.navUsage
	blocked := a.pal.open || a.chooser.open || a.confirm != nil || a.usage.open || a.ss.open
	if st.open && blocked {
		a.dismissNavUsage(false)
	}
	if !st.open {
		if !st.triggerOver {
			st.armed = true
		}
		if st.armed && !blocked && st.triggerOver {
			st.open, st.pinned, st.keyboard, st.focusPending = true, false, false, false
		}
	}
	if !st.open {
		return
	}
	if !st.pinned {
		over := st.triggerOver || st.cardOver
		stays := a.hoverIntent(c, "nav-usage", over)
		// A hover-open card the keyboard has entered is no longer a passive
		// hover: keep it while the focus is inside, even when the pointer
		// leaves. Focus that leaves lets the pointer rule close it again.
		if !over && !stays && !st.hadFocus {
			a.dismissNavUsage(false)
			return
		}
	}
	wasOpen := st.open
	var panelEl ui.Element
	ui.PopoverBase(c, trig, &st.open, func(panel ui.Element) {
		panelEl = panel
		a.navUsageCard(c, pal, panel, trig)
	})
	if wasOpen && !st.open {
		// Escape closed it if no press came outside; a press outside goes on
		// to the terminal, which keeps the keyboard. With the focus inside,
		// the trigger takes it back.
		outside := panelEl.PressedOutside()
		a.dismissNavUsage(!outside && (st.keyboard || st.hadFocus))
	}
}

// navUsageCard places and draws the popover: the plugin's list, this Mac's
// meters, and the way back to a real Usage pane. It opens inward from a
// sidebar and down from top tabs, up from bottom tabs, and clamps to the
// window.
func (a *App) navUsageCard(c *ui.Context, pal *palette, panel, trig ui.Element) {
	winW, winH := c.Size()
	w := min(380, winW-16)
	h := min(560, winH-24)
	// Top and bottom tabs anchor the card along the window's height. Reserve
	// the gap and the window margin before sizing the card, so its footer
	// keeps its full control height instead of being clipped by the window.
	if b := trig.Bounds(); b.H > 0 {
		switch a.navPlacement() {
		case navTop:
			if room := winH - navUsageWindowGap - (b.Y + b.H + navUsageAnchorGap); room > 0 {
				h = min(h, room)
			}
		case navBottom:
			if room := b.Y - navUsageAnchorGap - navUsageWindowGap; room > 0 {
				h = min(h, room)
			}
		}
	}
	panel.Width(w).Height(h).Radius(12).Clip().Focusable().Label(locales.NavUsagePopover()).Background(pal.popover)
	if pal.dark {
		floating(panel, pal)
	} else {
		floating(panel.Border(1, pal.cardBorder), pal)
	}
	switch a.navPlacement() {
	case navLeft:
		panel.AttachTo(trig, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, navUsageAnchorGap)
	case navRight:
		panel.AttachTo(trig, ui.AnchorLeft, ui.AnchorRight).Margin(0, navUsageAnchorGap, 0, 0)
	case navTop:
		panel.AttachTo(trig, ui.AnchorBottomLeft, ui.AnchorTopLeft).Margin(navUsageAnchorGap, 0, 0, 0)
	default:
		panel.AttachTo(trig, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, navUsageAnchorGap, 0)
	}
	if a.navUsage.focusPending {
		a.navUsage.focusPending = false
		panel.Focus()
	}
	panel.Children(func() {
		ui.Row(c.Key("nav-usage-head")).Height(34).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.UsageAccountTitle()).FontSize(13.5).FontWeight(650).TextColor(pal.text)
			ui.Spacer(c)
			iconButton(c, "nav-usage-close", closeIcon, locales.NavUsageClose(), pal, func() {
				a.defer_(func() { a.dismissNavUsage(true) })
			})
		})
		body := ui.Column(c.Key("nav-usage-body")).Grow(1).MinHeight(0).MinWidth(0).Padding(0, 1)
		body.Children(func() { a.navUsageWidget(c, pal, body, w-24, h-navUsageChrome) })
		ui.Column(c.Key("nav-usage-foot")).Shrink(0).Padding(8, 12, 10, 12).Gap(8).Children(func() {
			a.navUsageResources(c, pal)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if button(c, "nav-usage-pane", locales.NavUsageOpenPane(), "", true, pal) {
					a.defer_(func() {
						a.dismissNavUsage(false)
						if s := a.session(a.selected); s != nil {
							if widget := a.firstWidget("usage"); widget != "" {
								a.openPluginPane(s, "", "usage", widget, false)
							}
						}
					})
				}
			})
		})
	})
	// Escape belongs to the popover while the keyboard is in it: it closes
	// the popover and gives the focus back to the trigger. A passive hover
	// leaves the terminal's own Escape alone.
	panel.OnShortcut(0, ui.KeyEscape, func() {
		a.defer_(func() { a.dismissNavUsage(true) })
	})
	a.navUsage.cardOver = panel.Hovered()
	a.navUsage.hadFocus = panel.FocusWithin()
}

// navUsageWidget draws the Usage plugin's tree for the popover. Until the
// plugin is found or has drawn, it says so; a plugin that stopped shows its
// reason and Restart, through the same view a pane uses.
func (a *App) navUsageWidget(c *ui.Context, pal *palette, body ui.Element, width, height float32) {
	if !a.plugins.knownAt {
		a.discoverPlugins()
		working(c, pal, locales.PluginStarting(locales.UsageTitle()))
		return
	}
	plugin, ok := a.pluginByID("usage")
	if !ok {
		paneNothing(c, pal, projectIcon, locales.PluginUnknownTitle(), locales.PluginUnknownDetail("usage"))
		return
	}
	widget := "usage"
	if plugin.Manifest.WidgetByName(widget) == nil {
		widget = ""
		if w := plugin.Manifest.Widgets; len(w) > 0 {
			widget = w[0].Name
		}
	}
	if widget == "" {
		paneNothing(c, pal, projectIcon, locales.PluginUnknownTitle(), locales.PluginNoWidgets(plugin.Manifest.Name))
		return
	}
	st := a.pluginSurfaceState(navUsagePane, "usage", widget)
	st.shown = true
	if st.err != "" {
		a.pluginCrash(c, pal, st)
		return
	}
	a.syncPluginTheme(pal)
	size := protocol.Size{Width: max(int(width), 1), Height: max(int(height), navUsageListHeight)}
	a.preparePluginSurface(st, size)
	if st.tree == nil {
		working(c, pal, locales.PluginStarting(plugin.Manifest.Name))
		return
	}
	if st.view == nil {
		st.view = newPluginView()
	}
	st.view.openLink = nil
	if plugin.Manifest.Permissions.Has(protocol.PermOpenURL) {
		st.view.openLink = func(link string) bool { openURL(link); return true }
	}
	st.view.keysOn = body.FocusWithin()
	st.view.draw(c, pal, st.tree, func(ev pluginEvent) {
		a.pluginHost().Event(navUsagePane, ev.Node, ev.Kind, ev.Value)
	})
}

// navUsageResources is this Mac's CPU and RAM, from the app's own sampler:
// the same reading the full Usage window and `kero resources` show.
func (a *App) navUsageResources(c *ui.Context, pal *palette) {
	s := a.resources.sample
	if a.resources.err != "" {
		ui.Text(c, locales.NavUsageResourcesUnavailable()).FontSize(11.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
		return
	}
	ui.Row(c.Key("nav-usage-resources")).Gap(16).Children(func() {
		resourceMeter(c.Key("cpu"), pal, "CPU", strings.TrimPrefix(cpuLabel(s), "CPU "), cpuPercent(s), s.CPU != nil)
		resourceMeter(c.Key("memory"), pal, "RAM", strings.TrimPrefix(memoryLabel(s, false), "RAM "), memoryPercent(s), s.MemoryTotal > 0)
	})
}
