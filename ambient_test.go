package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/settings"
)

// ambientApp is an app with a plugin that has the permission, or has not,
// and the setting on.
func ambientApp(perms ...string) *App {
	a := &App{}
	a.set = settings.Default()
	a.plugins.known = []registry.Plugin{{
		Manifest: protocol.Manifest{
			ID: "fake", Name: "Fake", Version: "1.0.0", Run: []string{"fake"},
			Widgets:     []protocol.Widget{{Name: "main", Title: "Main"}},
			Permissions: perms,
		},
		Builtin: true,
	}}
	return a
}

// tintedView is a view whose tint has settled on color.
func tintedView(color ui.Color) *pluginView {
	v := newPluginView()
	v.tintColor, v.tintTarget, v.tintShown, v.tinted = color, pluginTintClamp(color), pluginTintClamp(color), true
	return v
}

// runAmbientLater runs what the view asks to run after its frame, as
// view.go does.
func runAmbientLater(a *App) {
	later := a.later
	a.later = nil
	for _, fn := range later {
		fn()
	}
}

// The winner is the focused pane that lends, else the pane that changed
// last; a pane that stops lending or closes gives way.
func TestAmbientStatePicksTheWinner(t *testing.T) {
	var s ambientState
	red, blue := ui.RGB(200, 40, 40), ui.RGB(40, 40, 200)
	if !s.lend("a", red) || s.target != red {
		t.Fatalf("target = %v", s.target)
	}
	if !s.lend("b", blue) || s.target != blue {
		t.Fatalf("the latest lend should win, target = %v", s.target)
	}
	if !s.focus("a") || s.target != red {
		t.Fatalf("the focused lender should win, target = %v", s.target)
	}
	if s.lend("a", red) {
		t.Error("lending the same color again changed the target")
	}
	if !s.drop("a") || s.target != blue {
		t.Fatalf("after a closed, target = %v", s.target)
	}
	if !s.drop("b") || s.target.A != 0 {
		t.Fatalf("after b cleared, target = %v", s.target)
	}
	if s.last != blue {
		t.Errorf("last = %v, want the color kept for the fade out", s.last)
	}
}

// Only a plugin with the permission lends, and only while its tint lasts.
func TestAmbientNeedsThePermission(t *testing.T) {
	red := ui.RGB(200, 40, 40)
	without := ambientApp(protocol.PermWeb)
	without.lendAmbient("p1", "fake", true, tintedView(red))
	if got := without.ambientColor(); got.A != 0 {
		t.Errorf("a plugin without the permission lent %v", got)
	}

	with := ambientApp(protocol.PermAmbient)
	with.lendAmbient("p1", "fake", true, tintedView(red))
	if got := with.ambientColor(); got != red {
		t.Errorf("ambient = %v, want %v", got, red)
	}
	// The tint clears, as it does when the music stops.
	v := tintedView(red)
	v.tinted, v.tintTarget = false, ui.Transparent
	with.lendAmbient("p1", "fake", true, v)
	if got := with.ambientColor(); got.A != 0 {
		t.Errorf("a cleared tint still lends %v", got)
	}
	// A pane that stopped draws a nil view.
	with.lendAmbient("p1", "fake", true, tintedView(red))
	with.lendAmbient("p1", "fake", true, nil)
	if got := with.ambientColor(); got.A != 0 {
		t.Errorf("a stopped pane still lends %v", got)
	}
}

// With the setting off nothing is blended, whatever is lent.
func TestAmbientSettingOffBlendsNothing(t *testing.T) {
	a := ambientApp(protocol.PermAmbient)
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	a.set.AmbientColor = false
	if got := a.ambientColor(); got.A != 0 {
		t.Fatalf("ambient with the setting off = %v", got)
	}
	var got *palette
	tt := ui.NewTester(func(c *ui.Context) { got = a.paletteFor(c) }, 200, 100)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if got.card != a.accentPal(false).card && got.card != a.accentPal(true).card {
		t.Errorf("card = %v, want the plain palette's", got.card)
	}
	if got.accent != a.accentPal(false).accent && got.accent != a.accentPal(true).accent {
		t.Errorf("accent = %v, want the plain palette's", got.accent)
	}
	if lumaContrast(got.accent, got.card) < 1 {
		t.Fatal("unreachable")
	}
}

// The palette crossfades towards the lent color, and settles on it.
func TestAmbientPaletteFadesInAndSettles(t *testing.T) {
	a := ambientApp(protocol.PermAmbient)
	var got *palette
	tt := ui.NewTester(func(c *ui.Context) { got = a.paletteFor(c) }, 200, 100)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	plain := got
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	tt.Frame()
	tt.Advance(ambientFade / 3)
	tt.Frame()
	mid := got
	if mid.card == plain.card {
		t.Fatal("the card did not move at all")
	}
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	want := plain.withAmbient(ui.RGB(200, 40, 40), 1)
	if got.card != want.card || got.accent != want.accent {
		t.Errorf("settled card/accent = %v/%v, want %v/%v", got.card, got.accent, want.card, want.accent)
	}
	if mid.card == got.card {
		t.Error("the card had already settled at a third of the fade")
	}
	// The music stops: back to the plain palette.
	a.lendAmbient("p1", "fake", true, nil)
	tt.Frame()
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	if got.card != plain.card {
		t.Errorf("after the fade out card = %v, want %v", got.card, plain.card)
	}
}

// Reduce Motion makes the palette change at once.
func TestAmbientReduceMotionIsInstant(t *testing.T) {
	a := ambientApp(protocol.PermAmbient)
	var got *palette
	tt := ui.NewTester(func(c *ui.Context) { got = a.paletteFor(c) }, 200, 100)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	plain := got
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	tt.Frame()
	if got.card == plain.card || got.card != plain.withAmbient(ui.RGB(200, 40, 40), 1).card {
		t.Errorf("card = %v, want the settled blend", got.card)
	}
}

// The blend leans the card by a few percent, keeps the accent standing
// out and the text readable, and sets the terminals' cursor and selection.
func TestPaletteWithAmbient(t *testing.T) {
	colors := []ui.Color{
		ui.RGB(255, 255, 255), ui.RGB(0, 0, 0), ui.RGB(255, 0, 0), ui.RGB(255, 255, 0),
		ui.RGB(0, 255, 255), ui.RGB(30, 40, 200), ui.RGB(120, 120, 120),
	}
	for _, base := range []*palette{&lightPalette, &darkPalette} {
		if got := base.withAmbient(ui.RGB(200, 40, 40), 0); got != base {
			t.Error("strength 0 should return the palette itself")
		}
		for _, c := range colors {
			p := base.withAmbient(c, 1)
			if p == base || base.card != (*base).card {
				t.Fatal("the blend changed the shared palette or returned it")
			}
			if lumaContrast(p.text, p.card) < 7 || lumaContrast(p.textMuted, p.card) < 4.5 {
				t.Errorf("dark=%v %v: text contrast %.2f muted %.2f", base.dark, c, lumaContrast(p.text, p.card), lumaContrast(p.textMuted, p.card))
			}
			if lumaContrast(p.accent, p.card) < 2.9 {
				t.Errorf("dark=%v %v: accent contrast %.2f", base.dark, c, lumaContrast(p.accent, p.card))
			}
			if p.focusEdge.A != base.focusEdge.A {
				t.Errorf("focus edge alpha = %d, want %d", p.focusEdge.A, base.focusEdge.A)
			}
			for _, d := range []struct{ got, was uint8 }{{p.card.R, base.card.R}, {p.card.G, base.card.G}, {p.card.B, base.card.B}} {
				if diff := int(d.got) - int(d.was); diff > 24 || diff < -24 {
					t.Errorf("dark=%v %v: the card moved by %d, more than a few percent", base.dark, c, diff)
				}
			}
			if p.cursor.A == 0 || p.selection.A == 0 {
				t.Error("cursor and selection are not set")
			}
			th := termTheme(p, ui.Hex("#000000"))
			if th.Cursor != p.cursor || th.Selection != p.selection {
				t.Errorf("terminal theme cursor/selection = %v/%v", th.Cursor, th.Selection)
			}
			if lumaContrast(th.Cursor, th.CursorText) < 4.5 {
				t.Errorf("cursor text contrast %.2f", lumaContrast(th.Cursor, th.CursorText))
			}
		}
		// Without it the terminal theme is the accent's own.
		if th := termTheme(base, ui.Hex("#123456")); th.Selection != ui.Hex("#123456") {
			t.Errorf("plain selection = %v", th.Selection)
		}
	}
}

// The terminals' themes follow the shade the frame draws: they wait for the
// same fade in, color change and fade out as the cards, and no terminal
// jumps to the final color at the start.
func TestTermThemesFollowTheAmbientFade(t *testing.T) {
	a := ambientApp(protocol.PermAmbient)
	var got *palette
	tt := ui.NewTester(func(c *ui.Context) {
		got = a.paletteFor(c)
		runAmbientLater(a)
	}, 200, 100)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	plainL, plainD := a.termThemes()
	red := ui.RGB(200, 40, 40)
	a.lendAmbient("p1", "fake", true, tintedView(red))
	tt.Frame()
	tt.Advance(ambientFade / 3)
	tt.Frame()

	// A terminal attached now, mid-fade, takes the shade already on screen.
	opts := a.termOptions("new", nil)
	midL, midD := opts.Theme, opts.DarkTheme
	if midL.Cursor != got.cursor || midL.Selection != got.selection {
		t.Errorf("mid-fade terminal = %v/%v, want the frame's %v/%v",
			midL.Cursor, midL.Selection, got.cursor, got.selection)
	}
	finalL := a.accentPal(false).withAmbient(red, 1)
	finalD := a.accentPal(true).withAmbient(red, 1)
	if midL.Cursor == finalL.cursor || midL.Selection == finalL.selection {
		t.Error("a terminal attached mid-fade jumped to the final color")
	}
	if midD.Cursor == finalD.cursor || midD.Selection == finalD.selection {
		t.Error("the dark terminal attached mid-fade jumped to the final color")
	}
	if got.card == a.accentPal(false).withAmbient(red, 1).card {
		t.Fatal("the palette had already settled at a third of the fade")
	}

	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	if l, d := a.termThemes(); l.Cursor != finalL.cursor || d.Selection != a.accentPal(true).withAmbient(red, 1).selection {
		t.Errorf("settled terminal = %v/%v, want the final shade", l.Cursor, d.Selection)
	}

	// One color changes to another through the middle, not at once.
	blue := ui.RGB(40, 40, 200)
	a.lendAmbient("p1", "fake", true, tintedView(blue))
	tt.Frame()
	tt.Advance(ambientFade / 2)
	tt.Frame()
	midL, _ = a.termThemes()
	finalBlue := a.accentPal(false).withAmbient(blue, 1)
	if midL.Cursor != got.cursor || midL.Selection != got.selection {
		t.Errorf("color-change terminal = %v/%v, want the frame's %v/%v",
			midL.Cursor, midL.Selection, got.cursor, got.selection)
	}
	if midL.Cursor == finalBlue.cursor && midL.Selection == finalBlue.selection {
		t.Error("the terminal changed color at once")
	}

	// Stopping or pausing clears the tint and fades it out.
	a.lendAmbient("p1", "fake", true, nil)
	tt.Frame()
	tt.Advance(ambientFade / 2)
	tt.Frame()
	midL, _ = a.termThemes()
	if midL.Cursor != got.cursor || midL.Selection != got.selection {
		t.Errorf("fade-out terminal = %v/%v, want the frame's %v/%v",
			midL.Cursor, midL.Selection, got.cursor, got.selection)
	}
	if midL.Cursor == plainL.Cursor && midL.Selection == plainD.Selection {
		t.Error("the terminal dropped the ambient color at once")
	}
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	if l, d := a.termThemes(); l.Cursor != plainL.Cursor || d.Selection != plainD.Selection {
		t.Errorf("after the fade out terminal = %v/%v, want the plain theme", l.Cursor, d.Selection)
	}
}

// Turning the setting off fades the terminal theme back with the cards, and
// Reduce Motion changes it at once.
func TestTermThemesSettingOffAndReduceMotion(t *testing.T) {
	a := ambientApp(protocol.PermAmbient)
	var got *palette
	tt := ui.NewTester(func(c *ui.Context) {
		got = a.paletteFor(c)
		runAmbientLater(a)
	}, 200, 100)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	plainL, _ := a.termThemes()
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	tt.Frame()
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()

	a.set.AmbientColor = false
	tt.Frame()
	tt.Advance(ambientFade / 2)
	tt.Frame()
	l, _ := a.termThemes()
	if l.Cursor == plainL.Cursor {
		t.Error("the setting off dropped the terminal color at once")
	}
	if l.Cursor != got.cursor || l.Selection != got.selection {
		t.Errorf("setting-off terminal = %v/%v, want the frame's %v/%v", l.Cursor, l.Selection, got.cursor, got.selection)
	}
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	if l, _ = a.termThemes(); l.Cursor != plainL.Cursor {
		t.Error("the setting off left the terminal color on")
	}

	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	a.set.AmbientColor = true
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	tt.Frame()
	want := a.accentPal(false).withAmbient(ui.RGB(200, 40, 40), 1)
	if l, _ = a.termThemes(); l.Cursor != want.cursor || l.Selection != want.selection {
		t.Errorf("Reduce Motion terminal = %v/%v, want the final shade %v/%v", l.Cursor, l.Selection, want.cursor, want.selection)
	}
	if got.card != want.card {
		t.Error("Reduce Motion left the palette behind the terminal")
	}
}

// Picked Ghostty themes opt out of the ambient color; with one picked, the
// other appearance still follows it.
func TestTermThemesKeepPickedThemes(t *testing.T) {
	red := ui.RGB(200, 40, 40)
	nord, err := terminal.GhosttyTheme("Nord")
	if err != nil {
		t.Fatal(err)
	}

	both := ambientApp(protocol.PermAmbient)
	both.set.LightTheme, both.set.DarkTheme = "Nord", "Nord"
	tt := ui.NewTester(func(c *ui.Context) {
		both.paletteFor(c)
		runAmbientLater(both)
	}, 200, 100)
	both.lendAmbient("p1", "fake", true, tintedView(red))
	tt.Frame()
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	if l, d := both.termThemes(); *l != *nord || *d != *nord {
		t.Error("two picked themes took the ambient color")
	}
	if len(both.later) != 0 {
		t.Error("two picked themes queued a terminal theme update")
	}

	mixed := ambientApp(protocol.PermAmbient)
	mixed.set.LightTheme = "Nord"
	tt = ui.NewTester(func(c *ui.Context) {
		mixed.paletteFor(c)
		runAmbientLater(mixed)
	}, 200, 100)
	mixed.lendAmbient("p1", "fake", true, tintedView(red))
	tt.Frame()
	tt.Advance(ambientFade * 2)
	tt.Frame()
	tt.Frame()
	l, d := mixed.termThemes()
	if *l != *nord {
		t.Error("the picked light theme took the ambient color")
	}
	wantD := termTheme(mixed.accentPal(true).withAmbient(red, 1), accentSpecs[mixed.accentID()].darkSel)
	if d.Cursor != wantD.Cursor || d.Selection != wantD.Selection {
		t.Errorf("the default dark theme = %v/%v, want the ambient shade %v/%v", d.Cursor, d.Selection, wantD.Cursor, wantD.Selection)
	}
}

func TestAmbientPermissionIsKnown(t *testing.T) {
	m := protocol.Manifest{ID: "fake", Name: "Fake", Version: "1", Run: []string{"x"},
		Widgets: []protocol.Widget{{Name: "main", Title: "Main"}}, Permissions: protocol.Permissions{protocol.PermAmbient}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if !m.Permissions.Has("ambient") {
		t.Error("Has(ambient) is false")
	}
}

func TestAmbientColorSettingDefaultsOn(t *testing.T) {
	if !settings.Default().AmbientColor {
		t.Error("Colour from music should default on")
	}
}

// A pale cover gives an accent that white reads on and that stands out on
// the card, in both appearances.
func TestAmbientAccentKeepsWhiteReadable(t *testing.T) {
	white := ui.RGB(255, 255, 255)
	for _, base := range []*palette{&lightPalette, &darkPalette} {
		for _, c := range []ui.Color{ui.RGB(250, 240, 120), ui.RGB(255, 255, 255), ui.RGB(120, 255, 255), ui.RGB(10, 10, 10)} {
			p := base.withAmbient(c, 1)
			if v := lumaContrast(p.accent, white); v < 2.95 {
				t.Errorf("dark=%v %v: white on the accent is %.2f", base.dark, c, v)
			}
			if v := lumaContrast(p.accent, p.card); v < 2.95 {
				t.Errorf("dark=%v %v: accent on the card is %.2f", base.dark, c, v)
			}
		}
	}
}

// A crash, a stopped pane and a closed pane take back what they lent; a
// hidden pane that still plays keeps it.
func TestAmbientLendEndsWithThePane(t *testing.T) {
	red := ui.RGB(200, 40, 40)
	a := ambientApp(protocol.PermAmbient)
	a.plugins.panes = map[string]*pluginPaneState{
		"p1": {plugin: "fake", tree: &protocol.Tree{}, shown: true},
	}
	a.lendAmbient("p1", "fake", true, tintedView(red))

	// Hidden, still playing: it keeps the color, and the focus goes.
	a.plugins.panes["p1"].shown = false
	a.sweepPluginPanes(map[string]bool{"p1": true})
	if a.ambientColor() != red {
		t.Errorf("a hidden pane that plays lost its lend: %v", a.ambientColor())
	}
	if a.ambient.focused != "" {
		t.Errorf("focus = %q on a hidden pane", a.ambient.focused)
	}

	// Its tree goes (the plugin stopped): the lend goes.
	a.plugins.panes["p1"].tree = nil
	a.sweepPluginPanes(map[string]bool{"p1": true})
	if a.ambientColor().A != 0 {
		t.Errorf("a pane with no tree still lends %v", a.ambientColor())
	}

	// A crash.
	a.plugins.panes["p1"].tree = &protocol.Tree{}
	a.lendAmbient("p1", "fake", true, tintedView(red))
	a.pluginCrashed("fake", "boom", "")
	if a.ambientColor().A != 0 {
		t.Errorf("a crashed plugin still lends %v", a.ambientColor())
	}

	// A closed pane.
	a.plugins.panes["p1"].tree = &protocol.Tree{}
	a.plugins.panes["p1"].err = ""
	a.lendAmbient("p1", "fake", true, tintedView(red))
	a.sweepPluginPanes(map[string]bool{})
	if a.ambientColor().A != 0 {
		t.Errorf("a closed pane still lends %v", a.ambientColor())
	}
}

// A settled frame returns the same palette, and the terminals are set once
// per changed shade, after the frame, and never after the fade settles.
func TestAmbientCachesAndSetsThemesOnce(t *testing.T) {
	a := ambientApp(protocol.PermAmbient)
	var got *palette
	var queued int
	tt := ui.NewTester(func(c *ui.Context) {
		got = a.paletteFor(c)
		queued = len(a.later)
		runAmbientLater(a)
	}, 200, 100)
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	tt.Frame()
	first := got
	tt.Frame()
	if got != first {
		t.Error("a settled frame made a new palette")
	}
	if queued != 0 {
		t.Error("a settled frame left a theme update queued")
	}
	if tt.FramePending() {
		t.Error("a settled frame still asks for another")
	}

	tt.Frame()
	if queued != 0 {
		t.Error("an unchanged shade queued a theme update")
	}
	a.ambientChanged()
	if len(a.later) != 0 {
		t.Error("an unchanged color queued a theme update")
	}

	a.set.LightTheme, a.set.DarkTheme = "Nord", "Nord"
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(40, 40, 200)))
	tt.Frame()
	if queued != 0 {
		t.Error("user-picked themes queued a theme update")
	}
}
