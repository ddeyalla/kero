package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// The first run's reveal: once, after the app has its first state, the
// real shell slides and fades in under a wash and the mark, and the
// overlay leaves by 900 ms. It is drawn in the same frame as the shell it
// covers, so there is no frame of plain shell before it, and any input
// cancels it to the end state.

type introState struct {
	first   bool // no app.json when the app opened: this is the first run
	started bool
	done    bool
	since   time.Time
}

const (
	introTotal  = 900 * time.Millisecond
	introEnter  = 180 * time.Millisecond // the shell starts moving
	introShell  = 520 * time.Millisecond
	introRowsAt = 300 * time.Millisecond // the first sidebar row
	introRows   = 260 * time.Millisecond
	introRowGap = 18 * time.Millisecond
	introMarkAt = 80 * time.Millisecond
	introMark   = 220 * time.Millisecond
	introOutAt  = 600 * time.Millisecond // the overlay starts leaving
)

// introStart begins the reveal once the first state is in and the window
// is built. It is silent, and only on a first run.
func (a *App) introStart(c *ui.Context) {
	if !a.intro.first || a.intro.started || !a.loaded {
		return
	}
	a.intro.started = true
	a.intro.since = c.Now()
	// The app.json this writes is what makes it a first run no more; a
	// crash during the reveal does not show it again.
	a.savePrefs()
}

// introReduce reports the desktop's wish for less motion.
func introReduce(c *ui.Context) bool { return c.Preferences().ReduceMotion }

// introProgress is a clamped, eased progress from a start and a length.
func introProgress(d, start, length time.Duration, c *ui.Context) float32 {
	if introReduce(c) {
		return max(0, min(float32(d)/(150*float32(time.Millisecond)), 1))
	}
	p := float32(d-start) / float32(length)
	return slide.Ease(max(0, min(p, 1)))
}

// introShellP is how much of the shell shows, from 0 when the reveal
// starts to 1 when it has landed.
func (a *App) introShellP(c *ui.Context) (float32, bool) {
	if !a.intro.started || a.intro.done {
		return 1, false
	}
	return introProgress(c.Now().Sub(a.intro.since), introEnter, introShell, c), true
}

// introRowP is a sidebar row's own progress: the first ten rows stagger
// in, and the rest come with the tenth, so a long list does not become a
// wave.
func (a *App) introRowP(c *ui.Context, index int) (float32, bool) {
	if !a.intro.started || a.intro.done || index < 1 {
		return 1, false
	}
	start := introRowsAt + time.Duration(min(index-1, 9))*introRowGap
	return introProgress(c.Now().Sub(a.intro.since), start, introRows, c), true
}

// introView draws the wash and the mark over the shell, and ends itself:
// at 900 ms, or at once when the user types, clicks or scrolls.
func (a *App) introView(c *ui.Context, pal *palette) {
	if !a.intro.started || a.intro.done {
		return
	}
	d := c.Now().Sub(a.intro.since)
	if d >= introTotal || introReduce(c) && d >= 150*time.Millisecond {
		a.finishIntro()
		return
	}
	c.AnimationFrame()

	// The shell comes up first, then the overlay leaves over its last
	// third, so the eye lands on the real window before the mark goes.
	overlay := 1 - introProgress(d, introOutAt, introTotal-introOutAt, c)
	if introReduce(c) {
		overlay = 1 - float32(d)/(150*float32(time.Millisecond))
	}
	frame := ui.Box(c.Key("intro")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
		Background(pal.accent.Alpha(0.12 * max(0, min(overlay, 1)))).
		Label(locales.IntroLabel()).Focusable().AutoFocus()
	frame.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputKeyDown, ui.InputPointerDown, ui.InputScroll:
			a.finishIntro()
			return true
		}
		return false
	})
	frame.Opacity(overlay)
	frame.Children(func() {
		ui.Column(c).Fill().Center().Gap(0).Children(func() {
			mark := introProgress(d, introMarkAt, introMark, c)
			box := ui.Box(c).Size(58, 58).Radius(15).Center().Background(pal.accent).
				TextColor(ui.Hex("#ffffff")).Opacity(mark).Scale(0.94+0.06*mark, 0.94+0.06*mark)
			box.Children(func() { ui.Icon(c, terminalIcon).Size(28, 28) })
		})
	})
}

// finishIntro puts the reveal away and gives the keyboard back to the
// pane the user is in.
func (a *App) finishIntro() {
	if a.intro.done {
		return
	}
	a.intro.done = true
	a.focusTerminal()
}
