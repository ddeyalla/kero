package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Shibu's Liquid Glass, the material of macOS 26 and later, for the
// panels that float over the window for a moment: the command palette,
// the confirmation dialog, the New Pane chooser, Settings and a pane's
// drag chip. Apple keeps the glass for this layer, the controls and
// navigation that float over content; panes, rows and cards are the
// content, and stay opaque (Human Interface Guidelines, Materials).
//
// The panels use the regular glass, as Apple's alerts, popovers and
// sheets do: it frosts what is under it and lightens it, or darkens it in
// the dark, so that the text on it reads over a terminal full of colored
// text. The glass draws its own rim, a lit hairline, so a panel draws no
// border; the shadow under it says that it floats over the window.

// glassShows reports whether the floating panels of c's window are of
// glass, or opaque, as they must be while the desktop asks for less
// transparency (macOS's Reduce Transparency). It is the one switch every
// panel follows; MyGo reads the setting again as it changes.
func glassShows(c *ui.Context) bool {
	return !c.Preferences().ReduceTransparency
}

// glassPanel puts a floating panel, as e, on regular glass shaped by its
// Radius, lifted off the window by a soft shadow. Where glassShows says
// no, it is the opaque floating panel kero drew before the glass.
func glassPanel(c *ui.Context, e ui.Element, pal *palette) ui.Element {
	if !glassShows(c) {
		return floating(e.Background(pal.popover), pal)
	}
	return glassShadow(e.Material(panelGlass(pal, false)), pal)
}

// glassChip is glassPanel for a small thing the pointer carries, a
// pane's drag chip: interactive glass, which swells a little while it is
// held, as AppKit's does.
func glassChip(c *ui.Context, e ui.Element, pal *palette) ui.Element {
	if !glassShows(c) {
		return floating(e.Background(pal.popover), pal)
	}
	return glassShadow(e.Material(panelGlass(pal, true)), pal)
}

// panelGlass is the glass of a floating panel. In the dark it is tinted
// toward the panel's own color, by 60%: the dark glass maps what is under
// it to between 15% and 51% of its lightness, and a terminal's white text
// under it, blurred, would bring the panel's muted text below 4.5 to 1.
// Tinted, it stays above, as Apple asks of text on its materials
// (TestGlassTextContrast), and the terminal still shows through, frosted.
// The light glass, which maps what is under it to between 54% and 100%,
// needs no tint.
func panelGlass(pal *palette, interactive bool) glass.Glass {
	g := glass.Glass{Interactive: interactive}
	if pal.dark {
		g.Tint = pal.popover.Alpha(0.6)
	}
	return g
}

// glassShadow is the shadow under a floating pane of glass: the glass
// casts none of its own, as NSGlassEffectView does not, but the window
// that AppKit shows a popover or a sheet in does, soft and a little
// below, which keeps a panel apart from the panes it floats over.
func glassShadow(e ui.Element, pal *palette) ui.Element {
	return e.Shadow(0, 1, 3, 0, pal.cardShadow).Shadow(0, 16, 40, 0, pal.floatShadow)
}

// concentric is the radius of a panel whose content, rounded by inner,
// sits inset in from its edge: the same center for both curves, as
// Apple's concentric corners have it.
func concentric(inner, inset float32) float32 { return inner + inset }

// glassScrim is what a floating panel's backdrop lays over the window.
// Over glass, nothing: macOS dims no window under a sheet, an alert or
// Spotlight, and the glass needs the panes under it as they are to bend
// and frost them; its shadow and rim set it apart. The opaque fallback
// keeps kero's dimming, and so does a confirmation (confirmView), which
// asks before something that cannot be undone.
func glassScrim(c *ui.Context, pal *palette) ui.Color {
	if !glassShows(c) {
		return pal.overlay
	}
	return ui.Transparent
}

// glassToolbar groups related navigation controls on one quiet capsule.
// Content stays opaque; only the controls use the regular glass material.
// The system transparency preference restores an opaque control surface.
func glassToolbar(c *ui.Context, e ui.Element, pal *palette) ui.Element {
	e.Radius(16)
	if !glassShows(c) {
		return e.Background(pal.control)
	}
	return e.Background(ui.Transparent).Material(panelGlass(pal, false))
}

// toolbarGroup keeps the existing compact hit targets in a single capsule.
func toolbarGroup(c *ui.Context, key string, pal *palette, children func()) {
	e := ui.Row(c.Key(key)).Shrink(0).Gap(0).Padding(2).AlignItems(ui.Center)
	// Fields that already have an outline use glassToolbar directly.
	e.InsetShadow(0, 0, 0, 0.5, pal.hairline)
	glassToolbar(c, e, pal).Children(children)
}

// scrollUnderBar softens only content that continues beyond a navigation
// edge. The effect overlays the scroller, never its controls or hit targets.
func scrollUnderBar(c *ui.Context, key string, sc *ui.ScrollState, background ui.Color, build func(ui.Element)) {
	ui.Column(c.Key(key + "-viewport")).Grow(1).MinHeight(0).MinWidth(0).Clip().Children(func() {
		build(ui.Scroll(c.Key(key)).Grow(1).MinHeight(0).MinWidth(0).TrackScroll(sc))
		if sc.Y > 0.5 {
			ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(14).PassThrough().Material(glass.ScrollEdge{Background: background})
		}
		if sc.MaxY-sc.Y > 0.5 {
			ui.Box(c.Key("bottom-edge")).Absolute().Bottom(0).Left(0).Right(0).Height(14).PassThrough().Material(glass.ScrollEdge{Bottom: true, Background: background})
		}
	})
}
