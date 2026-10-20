package main

import (
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"kero/internal/settings"
)

// palette is kero's colors, for the light and the dark appearance.
type palette struct {
	dark bool

	sidebar      ui.Color // under the sidebar where no material shows
	text         ui.Color
	textMuted    ui.Color // project headers, times
	textFaint    ui.Color // counts, hints
	rowHover     ui.Color
	rowSelected  ui.Color
	rowSelBorder ui.Color
	rowShadow    ui.Color

	card        ui.Color // the terminal's card
	cardBorder  ui.Color
	cardShadow  ui.Color
	cardShadow2 ui.Color
	floatShadow ui.Color
	cardDim     ui.Color // a pane the user is not in
	// edge and edgeHigh are the hairlines around surfaces in the dark:
	// cards and rows, and what floats or takes a click.
	edge     ui.Color
	edgeHigh ui.Color
	// veil fades a pane the user is not in, its text too, as Ghostty's
	// unfocused-split-opacity.
	veil     ui.Color
	buttonBg ui.Color
	// Controls' faces: buttons, fields, selects, a segmented control's
	// choice. Light ones stand on a soft shadow; dark ones on their
	// lighter fill alone, with a hint of light along their top, as macOS
	// draws them.
	control       ui.Color
	controlHover  ui.Color
	controlShadow bool
	controlLight  ui.Color
	divider       ui.Color
	chip          ui.Color

	attention ui.Color // needs you
	attnSoft  ui.Color
	done      ui.Color // the filled check
	onDone    ui.Color
	danger    ui.Color
	accent    ui.Color

	overlay   ui.Color // behind dialogs
	popover   ui.Color
	kbd       ui.Color
	kbdBorder ui.Color
}

var lightPalette = palette{
	sidebar:      ui.Hex("#eceef4"),
	text:         ui.Hex("#1d1d1f"),
	textMuted:    ui.Hex("#6e6e78"),
	textFaint:    ui.Hex("#a3a3ad"),
	rowHover:     ui.RGBA(0, 0, 0, 0.04),
	rowSelected:  ui.Hex("#ffffff"),
	rowSelBorder: ui.RGBA(0, 0, 0, 0.05),
	rowShadow:    ui.RGBA(20, 20, 50, 0.08),

	card:          ui.Hex("#ffffff"),
	cardBorder:    ui.RGBA(0, 0, 0, 0.07),
	cardShadow:    ui.RGBA(0, 0, 0, 0.07),
	cardShadow2:   ui.RGBA(0, 0, 0, 0.035),
	floatShadow:   ui.RGBA(20, 20, 40, 0.14),
	cardDim:       ui.RGBA(255, 255, 255, 0.58),
	veil:          ui.RGBA(250, 248, 252, 0.28),
	buttonBg:      ui.Hex("#ffffff"),
	control:       ui.Hex("#ffffff"),
	controlHover:  ui.Hex("#f7f7f9"),
	controlShadow: true,
	divider:       ui.RGBA(0, 0, 0, 0.06),
	chip:          ui.RGBA(0, 0, 0, 0.045),

	attention: ui.Hex("#c27b0f"),
	attnSoft:  ui.RGBA(232, 154, 40, 0.13),
	done:      ui.Hex("#1d1d1f"),
	onDone:    ui.Hex("#ffffff"),
	danger:    ui.Hex("#d93a35"),
	accent:    ui.Hex("#3b6cf6"),

	overlay:   ui.RGBA(10, 10, 30, 0.12),
	popover:   ui.Hex("#ffffff"),
	kbd:       ui.RGBA(0, 0, 0, 0.04),
	kbdBorder: ui.RGBA(0, 0, 0, 0.09),
}

var darkPalette = palette{
	dark:         true,
	sidebar:      ui.Hex("#141418"),
	text:         ui.Hex("#ececf0"),
	textMuted:    ui.Hex("#a9a9b4"),
	textFaint:    ui.Hex("#83838f"),
	rowHover:     ui.RGBA(255, 255, 255, 0.05),
	rowSelected:  ui.RGBA(255, 255, 255, 0.10),
	rowSelBorder: ui.RGBA(255, 255, 255, 0.06),
	rowShadow:    ui.RGBA(0, 0, 0, 0.25),

	card:         ui.Hex("#232328"),
	cardBorder:   ui.RGBA(255, 255, 255, 0.08),
	cardShadow:   ui.RGBA(0, 0, 0, 0.35),
	cardShadow2:  ui.RGBA(0, 0, 0, 0.2),
	floatShadow:  ui.RGBA(0, 0, 0, 0.45),
	cardDim:      ui.RGBA(35, 35, 40, 0.5),
	veil:         ui.RGBA(14, 13, 18, 0.42),
	edge:         ui.RGBA(255, 255, 255, 0.075),
	edgeHigh:     ui.RGBA(255, 255, 255, 0.11),
	buttonBg:     ui.Hex("#404047"),
	control:      ui.Hex("#404047"),
	controlHover: ui.Hex("#4a4a52"),
	controlLight: ui.RGBA(255, 255, 255, 0.09),
	divider:      ui.RGBA(255, 255, 255, 0.07),
	chip:         ui.RGBA(255, 255, 255, 0.07),

	attention: ui.Hex("#f0a33a"),
	attnSoft:  ui.RGBA(240, 163, 58, 0.15),
	done:      ui.Hex("#ececf0"),
	onDone:    ui.Hex("#161618"),
	danger:    ui.Hex("#ff6961"),
	accent:    ui.Hex("#4a71d4"),

	overlay:   ui.RGBA(0, 0, 0, 0.35),
	popover:   ui.Hex("#232327"),
	kbd:       ui.RGBA(255, 255, 255, 0.06),
	kbdBorder: ui.RGBA(255, 255, 255, 0.1),
}

// accentSpec is an accent the user can choose, hand-tuned: its color in
// the light and the dark, and the terminals' selection with it. The
// neutrals stay literal; nothing derives. The blue is the app's own, and
// the light and dark palettes above carry it, so that choosing nothing
// shows what the app has had.
type accentSpec struct {
	light, dark       ui.Color
	lightSel, darkSel ui.Color // the terminals' selection, on the card
}

var accentSpecs = map[string]accentSpec{
	settings.AccentDefault: {light: ui.Hex("#3b6cf6"), dark: ui.Hex("#4a71d4"), lightSel: ui.Hex("#cfdcfb"), darkSel: ui.Hex("#2f4170")},
	"plum":                 {light: ui.Hex("#7a4fd0"), dark: ui.Hex("#8158cd"), lightSel: ui.Hex("#e2d8f5"), darkSel: ui.Hex("#3f3361")},
	"rose":                 {light: ui.Hex("#c74684"), dark: ui.Hex("#d13480"), lightSel: ui.Hex("#f2d6e3"), darkSel: ui.Hex("#5c2a46")},
	"sea":                  {light: ui.Hex("#247e91"), dark: ui.Hex("#258094"), lightSel: ui.Hex("#c9e2e8"), darkSel: ui.Hex("#26414d")},
	"moss":                 {light: ui.Hex("#36872f"), dark: ui.Hex("#35852e"), lightSel: ui.Hex("#d2e4d1"), darkSel: ui.Hex("#284029")},
	"graphite":             {light: ui.Hex("#6f6f78"), dark: ui.Hex("#75757e"), lightSel: ui.Hex("#e0e0e4"), darkSel: ui.Hex("#3b3b41")},
}

// palKey is what a palette in the cache is of: the accent's id and the
// appearance. The zero accent, as a test's app has, is the default's.
type palKey struct {
	accent string
	dark   bool
}

// palCache holds a palette per accent and appearance, built once, not
// per frame.
var palCache sync.Map // palKey → *palette

// accentPal returns the palette of the accent and the appearance: the
// app's colors, with the accent's over them where it is not the blue.
func (a *App) accentPal(dark bool) *palette {
	key := palKey{accent: a.set.Accent, dark: dark}
	if p, ok := palCache.Load(key); ok {
		return p.(*palette)
	}
	p := &lightPalette
	if dark {
		p = &darkPalette
	}
	if key.accent != settings.AccentDefault {
		if s, ok := accentSpecs[key.accent]; ok {
			c := *p
			c.accent = s.dark
			if !dark {
				c.accent = s.light
			}
			p = &c
		}
	}
	palCache.Store(key, p)
	return p
}

// paletteFor returns the palette of the frame, as the accent has it.
func (a *App) paletteFor(c *ui.Context) *palette {
	return a.accentPal(c.Theme().Dark)
}

// Terminal themes on the card's color, with palettes tuned to read on it:
// Apple's system colors, a little deeper in the light; the selection in
// the accent's tint.
func termTheme(pal *palette, sel ui.Color) *terminal.Theme {
	if pal.dark {
		return &terminal.Theme{
			Foreground:    ui.Hex("#e6e6ea"),
			Background:    pal.card,
			Cursor:        ui.Hex("#e6e6ea"),
			CursorText:    ui.Hex("#161618"),
			Selection:     sel,
			SelectionText: ui.Hex("#ffffff"),
			Palette: [16]ui.Color{
				ui.Hex("#3a3a40"), ui.Hex("#ff6b64"), ui.Hex("#4fd17a"), ui.Hex("#f2c14e"),
				ui.Hex("#6b9bff"), ui.Hex("#d07cf0"), ui.Hex("#4cc9e0"), ui.Hex("#c9c9cf"),
				ui.Hex("#6c6c78"), ui.Hex("#ff8a84"), ui.Hex("#74e397"), ui.Hex("#ffd479"),
				ui.Hex("#8fb3ff"), ui.Hex("#e09cf7"), ui.Hex("#7fdcec"), ui.Hex("#f5f5f7"),
			},
		}
	}
	return &terminal.Theme{
		Foreground:    ui.Hex("#1d1d1f"),
		Background:    pal.card,
		Cursor:        ui.Hex("#1d1d1f"),
		CursorText:    ui.Hex("#ffffff"),
		Selection:     sel,
		SelectionText: ui.Hex("#1d1d1f"),
		Palette: [16]ui.Color{
			ui.Hex("#1d1d1f"), ui.Hex("#d3302f"), ui.Hex("#22863a"), ui.Hex("#b08800"),
			ui.Hex("#2f5bd8"), ui.Hex("#a033c4"), ui.Hex("#0f8199"), ui.Hex("#8e8e93"),
			ui.Hex("#5f5f66"), ui.Hex("#ef4444"), ui.Hex("#2fa84f"), ui.Hex("#d4a017"),
			ui.Hex("#3b82f6"), ui.Hex("#c04ee0"), ui.Hex("#14a3bf"), ui.Hex("#b4b4bb"),
		},
	}
}

// backdrop paints the window's wash of color, under the sidebar and
// between the cards: the base on the root, a glow from the top right on
// row, which holds everything else, and the grain over them both when it
// is on. It lies over the window's material, which shows through a
// little; the plain one is the material alone.
func backdrop(c *ui.Context, pal *palette, row ui.Element, s settings.Settings) {
	if s.Grain {
		row.Draw(func(p *ui.Painter, r ui.Rect) {
			grain.Draw(p, r, shader.Options{Uniforms: map[string]any{"Alpha": grainAlpha(c, pal.dark)}})
		})
	}
	base, glow := washes(s.Background, pal.dark)
	if s.Background == "plain" {
		// The material alone is as light or as dark as the wallpaper
		// under it: a base of the sidebar's color keeps the text legible,
		// letting the desktop show a little.
		bg := pal.sidebar
		if c.Vibrancy() {
			bg = bg.Alpha(0.82)
		}
		c.Root().Background(bg)
		return
	}
	alpha := float32(1)
	if c.Vibrancy() {
		alpha = 0.95
	}
	stops := make([]ui.GradientStop, len(base.Stops))
	for i, st := range base.Stops {
		stops[i] = ui.GradientStop{Color: st.Color.Alpha(alpha), At: st.At}
	}
	base.Stops = stops
	c.Root().LinearGradient(base)
	row.LinearGradient(glow)
}

// washes returns a background's base and glow.
func washes(id string, dark bool) (base, glow ui.LinearGradient) {
	w, ok := washTable[id]
	if !ok {
		w = washTable["rex"]
	}
	stops := func(hexes ...string) []ui.GradientStop {
		out := make([]ui.GradientStop, len(hexes))
		for i, h := range hexes {
			out[i] = ui.GradientStop{Color: ui.Hex(h), At: float32(i) / float32(len(hexes)-1)}
		}
		return out
	}
	glowOf := func(c ui.Color) ui.LinearGradient {
		return ui.LinearGradient{Angle: 225, Oklab: true, Stops: []ui.GradientStop{{Color: c, At: 0}, {Color: c.Alpha(0), At: 0.5}}}
	}
	if dark {
		return ui.LinearGradient{Angle: 180, Oklab: true, Stops: stops(w.dark...)}, glowOf(w.darkGlow)
	}
	return ui.LinearGradient{Angle: 180, Oklab: true, Stops: stops(w.light...)}, glowOf(w.lightGlow)
}

type wash struct {
	light, dark         []string
	lightGlow, darkGlow ui.Color
}

var washTable = map[string]wash{
	// Rex's: pink lavender above, rose at the top right, butter cream below.
	"rex": {
		light: []string{"#ebe0f3", "#f3e7f1", "#f7eee4", "#f6efcf"}, lightGlow: ui.RGBA(247, 220, 241, 0.75),
		dark: []string{"#17141f", "#191620", "#1b1916"}, darkGlow: ui.RGBA(120, 70, 120, 0.22),
	},
	"mist": {
		light: []string{"#e3e9f4", "#ebeff6", "#f1f2f3", "#f2f1ec"}, lightGlow: ui.RGBA(210, 225, 248, 0.75),
		dark: []string{"#12151b", "#14171d", "#17181b"}, darkGlow: ui.RGBA(70, 100, 150, 0.22),
	},
	"meadow": {
		light: []string{"#e2eee5", "#ebf2e7", "#f3f2e2", "#f5f0d6"}, lightGlow: ui.RGBA(214, 240, 222, 0.75),
		dark: []string{"#111814", "#141a16", "#181a14"}, darkGlow: ui.RGBA(70, 130, 95, 0.2),
	},
	"dusk": {
		light: []string{"#e4dbf3", "#efdeee", "#f6e3dc", "#f7e6cf"}, lightGlow: ui.RGBA(246, 206, 222, 0.8),
		dark: []string{"#191321", "#1e161e", "#221a17"}, darkGlow: ui.RGBA(150, 80, 110, 0.25),
	},
	// The accents' own washes, with their presets: a violet's lavender, a
	// rose's warm pink, a sea's cool green-blue.
	"plum": {
		light: []string{"#e3dcf6", "#e8def2", "#efe2ea", "#f4e8dc"}, lightGlow: ui.RGBA(236, 214, 245, 0.75),
		dark: []string{"#171425", "#191623", "#1b1918"}, darkGlow: ui.RGBA(120, 80, 170, 0.22),
	},
	"rose": {
		light: []string{"#f6dfe7", "#f5e1e2", "#f7e7dd", "#f8ecd4"}, lightGlow: ui.RGBA(248, 214, 226, 0.78),
		dark: []string{"#1f1319", "#211518", "#241918"}, darkGlow: ui.RGBA(200, 82, 128, 0.24),
	},
	"sea": {
		light: []string{"#dde9ee", "#e0edf1", "#e5f1ee", "#e9f1e6"}, lightGlow: ui.RGBA(196, 228, 236, 0.75),
		dark: []string{"#101820", "#121b20", "#141d1c"}, darkGlow: ui.RGBA(58, 128, 148, 0.22),
	},
	"plain": {
		light: []string{"#eceef4", "#eceef4"}, lightGlow: ui.RGBA(0, 0, 0, 0),
		dark: []string{"#141418", "#141418"}, darkGlow: ui.RGBA(0, 0, 0, 0),
	},
}
