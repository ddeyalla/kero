package main

// Ambient color: a plugin pane that asks for the "ambient" permission lends
// the color it is tinted with, as Spotify's pane has its album's, to the
// whole app. The accent, the focus edge, the terminals' cursor and
// selection take the color, and the cards lean towards it by a few
// percent; the text keeps its contrast. The app knows no plugin by name:
// any pane that is tinted and has the permission may lend its color, the
// one that changed last, or the focused one, winning. The setting
// "Colour from music" turns it off.

import (
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
)

// ambientWash is how far a card leans to the ambient color, at full
// strength.
const ambientWash = 0.07

// ambientFade is how long the palette takes to change from one ambient
// color to the next, or to fade in and out as music starts and stops.
const ambientFade = 600 * time.Millisecond

// ambientLend is what one pane lends.
type ambientLend struct {
	color ui.Color // the tint's color, as the picture has it
	at    uint64   // when it last changed, in ambientState.seq
}

// ambientState is the ambient color of the app: what each pane lends, and
// which of them wins.
type ambientState struct {
	lends   map[string]ambientLend
	seq     uint64
	focused string // the pane the user is in, as the last frame drew it

	// target is the winner's color, transparent when none lends; last is
	// the last color there was, which a fade out keeps while it fades.
	target ui.Color
	last   ui.Color

	// shown and strength are the color and the strength the frame drew,
	// on their way to the target over ambientFade. The terminals' themes
	// follow them, so no terminal jumps ahead of the cards.
	shown    ui.Color
	strength float32

	// applied is the shown color and strength the terminals' themes were
	// last set with, so that a frame that leaves them alone sets nothing.
	applied         ui.Color
	appliedStrength float32
	themePending    bool

	// blended is the last palette ambientPal made, with what it was made
	// from, so that a frame in which nothing moves returns it as it is.
	blended struct {
		base     *palette
		shown    ui.Color
		strength float32
		out      *palette
	}
}

// lend sets the color a pane lends, or takes it back when the color is
// transparent. It tells whether the winner's color changed.
func (s *ambientState) lend(pane string, color ui.Color) bool {
	if color.A == 0 {
		if _, ok := s.lends[pane]; !ok {
			return false
		}
		delete(s.lends, pane)
		return s.choose()
	}
	if cur, ok := s.lends[pane]; ok && cur.color == color {
		return false
	}
	if s.lends == nil {
		s.lends = map[string]ambientLend{}
	}
	s.seq++
	s.lends[pane] = ambientLend{color: color, at: s.seq}
	return s.choose()
}

// drop forgets a pane that is gone.
func (s *ambientState) drop(pane string) bool { return s.lend(pane, ui.Transparent) }

// focus tells which pane the user is in. A pane that lends and has the
// focus wins over one that lends and has not.
func (s *ambientState) focus(pane string) bool {
	if s.focused == pane {
		return false
	}
	s.focused = pane
	return s.choose()
}

// choose picks the winner: the focused pane if it lends, else the pane
// whose color changed last. It tells whether the target changed.
func (s *ambientState) choose() bool {
	want := ui.Transparent
	if l, ok := s.lends[s.focused]; ok {
		want = l.color
	} else {
		var best uint64
		for _, l := range s.lends {
			if l.at > best {
				best, want = l.at, l.color
			}
		}
	}
	if want == s.target {
		return false
	}
	s.target = want
	if want.A > 0 {
		s.last = want
	}
	return true
}

// ambientColor is the color the app takes, transparent when the setting is
// off or no pane lends one.
func (a *App) ambientColor() ui.Color {
	if !a.set.AmbientColor {
		return ui.Transparent
	}
	return a.ambient.target
}

// lendAmbient reads what a plugin pane lends after it drew: its tint, when
// its plugin has the permission, and takes it back when the tint clears,
// as it does when the music stops.
func (a *App) lendAmbient(pane string, plugin string, focused bool, v *pluginView) {
	color := ui.Transparent
	if p, ok := a.pluginByID(plugin); ok && p.Manifest.Permissions.Has(protocol.PermAmbient) && v != nil && v.tinted && v.tintTarget.A > 0 {
		color = v.tintColor
	}
	changed := a.ambient.lend(pane, color)
	if focused {
		changed = a.ambient.focus(pane) || changed
	} else if a.ambient.focused == pane {
		changed = a.ambient.focus("") || changed
	}
	if changed {
		a.ambientChanged()
	}
}

// ambientChanged asks for the frame that moves the ambient color. The
// terminals' themes follow the shown color after that frame, as
// scheduleTermThemes does: a terminal has no animated theme, so the app
// sets one for each shade of the fade.
func (a *App) ambientChanged() {
	a.invalidateMain()
}

// themesPicked tells whether the user chose both terminal themes, which
// the ambient color does not touch.
func (a *App) themesPicked() bool {
	picked := func(name string) bool { return name != "" && name != "kero" }
	return picked(a.set.LightTheme) && picked(a.set.DarkTheme)
}

// ambientPal returns the palette with the ambient color in it, fading the
// color in and out and from one to the next. A plain palette is returned
// when nothing is lent.
func (a *App) ambientPal(c *ui.Context, pal *palette) *palette {
	target := a.ambientColor()
	fade := ui.Animation{Duration: ambientFade, Ease: ui.CubicBezier(0.4, 0, 0.2, 1)}
	// mygo lets colors change smoothly under Reduce Motion, as they move
	// nothing, but the setting asks for the palette to change at once.
	if c.Preferences().ReduceMotion {
		fade = ui.Animation{Duration: time.Millisecond}
	}
	// The color and its strength move apart: a color that fades out keeps
	// its hue, and one that starts fades in from the accent's.
	hue := a.ambient.last
	if hue.A == 0 {
		hue = pal.accent
	}
	root := c.Root()
	shown := root.AnimateColor("ambient", hue, fade)
	on := float32(0)
	if target.A > 0 {
		on = 1
	}
	strength := root.AnimateTo("ambient-on", on, fade)
	a.ambient.shown, a.ambient.strength = shown, strength
	a.scheduleTermThemes()
	if strength <= 0.004 {
		return pal
	}
	b := &a.ambient.blended
	if b.out == nil || b.base != pal || b.shown != shown || b.strength != strength {
		b.base, b.shown, b.strength = pal, shown, strength
		b.out = pal.withAmbient(shown, strength)
	}
	return b.out
}

// scheduleTermThemes sets the terminals' cursor and selection to the shade
// the frame drew, after the frame, as the cards already have it. One update
// is queued for a changed shade, and none once the fade has settled.
func (a *App) scheduleTermThemes() {
	s := &a.ambient
	if a.themesPicked() || s.themePending || (s.shown == s.applied && s.strength == s.appliedStrength) {
		return
	}
	// A shade below the blend's edge is plain: there is nothing to set.
	if s.strength <= 0.004 && s.appliedStrength <= 0.004 {
		return
	}
	s.themePending = true
	a.defer_(func() {
		s.themePending = false
		if a.themesPicked() || (s.shown == s.applied && s.strength == s.appliedStrength) {
			return
		}
		s.applied, s.appliedStrength = s.shown, s.strength
		light, dark := a.termThemes()
		for _, e := range a.terms {
			if e.term != nil {
				e.term.SetTheme(light, dark)
			}
		}
	})
}

// ambientThemePal returns p with the shade the frame drew blended in, or p
// itself once the edge of the fade has arrived. termThemes and ambientPal
// use it, so a terminal and the frame show the same shade.
func (a *App) ambientThemePal(p *palette) *palette {
	if a.ambient.shown.A == 0 || a.ambient.strength <= 0.004 {
		return p
	}
	return p.withAmbient(a.ambient.shown, a.ambient.strength)
}

// withAmbient returns the palette with the color blended in at the
// strength, 0 to 1: the accent and the focus edge, the terminals' cursor
// and selection, and the card a few percent.
func (p *palette) withAmbient(color ui.Color, strength float32) *palette {
	strength = max(0, min(strength, 1))
	if color.A == 0 || strength == 0 {
		return p
	}
	color.A = 255
	out := *p
	// The card leans to the color; in the dark the color is first brought
	// into the band where light text keeps its contrast.
	wash := color
	if p.dark {
		wash = pluginTintClamp(color)
	}
	out.card = p.card.Mix(wash, ambientWash*strength)
	accent := ambientAccent(color, out.card, p.dark)
	out.accent = p.accent.Mix(accent, strength)
	out.focusEdge = out.accent.Alpha(float32(p.focusEdge.A) / 255)
	sel := 0.22
	if p.dark {
		sel = 0.32
	}
	out.selection = out.card.Mix(accent, float32(sel)*strength)
	out.cursor = p.accent.Mix(accent, strength)
	return &out
}

// ambientAccent brings a color to where it stands out on the card, 3 to 1
// as the accents do, and where white stands out on it, 3 to 1.
func ambientAccent(c, card ui.Color, dark bool) ui.Color {
	target := ui.RGB(0, 0, 0)
	if dark {
		target = ui.RGB(255, 255, 255)
	}
	for i := 0; i < 20 && lumaContrast(c, card) < 3; i++ {
		c = c.Mix(target, 0.1)
	}
	// White is drawn on the accent (menu highlights, check badges, the
	// drop pill), so a pale one goes deeper, to where white reads at 3 to
	// 1 too. Between the dark card and white there is room for both.
	white := ui.RGB(255, 255, 255)
	for i := 0; i < 30 && lumaContrast(c, white) < 3; i++ {
		c = c.Mix(ui.RGB(0, 0, 0), 0.08)
	}
	return c
}

// lumaContrast is the WCAG contrast of two colors, 1 to 21.
func lumaContrast(a, b ui.Color) float32 {
	la, lb := pluginLuma(a), pluginLuma(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}
