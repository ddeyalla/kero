package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// The first run's reveal: from the window's first frame, a wash and the
// mark over a shell that is not shown yet; once the app has its first
// state, the real shell slides and fades in under them, and the overlay
// leaves by 900 ms. The shell is never shown before the overlay, and any
// input in the window cancels it to the end state. A state that takes
// longer than introWait, or a kerod that cannot be reached, skips the
// reveal: the shell fades in plainly, and the error, if any, shows.

type introState struct {
	first   bool      // no app.json when the app opened: this is the first run
	shown   time.Time // the reveal's first frame: the wash and the mark show from it
	started bool      // the state came, and the shell's timeline began
	done    bool
	since   time.Time // when the shell's timeline began
	skipped time.Time // the reveal gave up waiting: the shell fades in from it
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
	introWait   = 2 * time.Second        // the longest the reveal waits for the state
	introFade   = 150 * time.Millisecond // Reduce Motion's fade, and a skipped reveal's
)

// introStart shows the reveal from the first frame of a first run, and
// starts the shell's timeline once the first state is in. It gives up,
// rather than cover a kerod that cannot be reached, or wait on one that
// is slow. It is silent.
func (a *App) introStart(c *ui.Context) {
	if !a.intro.first || a.intro.started || a.intro.done {
		return
	}
	now := c.Now()
	if a.intro.shown.IsZero() {
		a.intro.shown = now
	}
	waited := now.Sub(a.intro.shown)
	if a.conn == connVersion || a.conn == connFailed && a.cl == nil || !a.loaded && waited >= introWait {
		a.intro.done, a.intro.skipped = true, now
		return
	}
	if !a.loaded {
		// A frame when the wait is over, should nothing else ask for one.
		c.After(introWait - waited)
		return
	}
	a.intro.started = true
	a.intro.since = now
	// The app.json this writes is what makes it a first run no more; a
	// crash during the reveal does not show it again.
	a.savePrefs()
}

// introShowing reports whether the reveal is over the window: waiting for
// the state, or on its timeline.
func (a *App) introShowing() bool {
	return a.intro.first && !a.intro.done && !a.intro.shown.IsZero()
}

// introReduce reports the desktop's wish for less motion.
func introReduce(c *ui.Context) bool { return c.Preferences().ReduceMotion }

// introProgress is a clamped, eased progress from a start and a length.
// With Reduce Motion, everything fades in together, over introFade.
func introProgress(d, start, length time.Duration, c *ui.Context) float32 {
	if introReduce(c) {
		return max(0, min(float32(d)/float32(introFade), 1))
	}
	p := float32(d-start) / float32(length)
	return slide.Ease(max(0, min(p, 1)))
}

// introShellP is how much of the shell shows, from 0 while the reveal
// waits and as its timeline starts, to 1 when it has landed.
func (a *App) introShellP(c *ui.Context) (float32, bool) {
	switch {
	case !a.intro.skipped.IsZero():
		p := float32(c.Now().Sub(a.intro.skipped)) / float32(introFade)
		if p >= 1 {
			return 1, false
		}
		c.AnimationFrame()
		return max(p, 0), true
	case a.introShowing() && !a.intro.started:
		return 0, true
	case !a.intro.started || a.intro.done:
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

// introLift is how far up an element arriving at progress p comes from:
// none with Reduce Motion, which only fades, nor for a skipped reveal's
// plain fade.
func (a *App) introLift(c *ui.Context, p, by float32) float32 {
	if introReduce(c) || !a.intro.skipped.IsZero() {
		return 0
	}
	return (1 - p) * by
}

// modalOpen reports whether a dialog has the keyboard: the palette, the
// chooser, Settings, a confirmation or the usage.
func (a *App) modalOpen() bool {
	return a.pal.open || a.chooser.open || a.ss.open || a.confirm != nil || a.usage.open
}

// introView draws the wash and the mark over the shell, and ends itself:
// at 900 ms, or at once when the user types, clicks or scrolls.
func (a *App) introView(c *ui.Context, pal *palette) {
	if !a.introShowing() {
		return
	}
	// A dialog opened during it, by a shortcut or from kerod, ends it, and
	// keeps the keyboard: the overlay, built after the dialogs, would
	// otherwise take it from the dialog's field.
	if a.modalOpen() {
		a.finishIntro()
		return
	}
	// The shell's timeline: none while the reveal waits for the state.
	var d time.Duration
	if a.intro.started {
		d = c.Now().Sub(a.intro.since)
		if d >= introTotal || introReduce(c) && d >= introFade {
			a.finishIntro()
			return
		}
	}
	c.AnimationFrame()

	// The shell comes up first, then the overlay leaves over its last
	// third, so the eye lands on the real window before the mark goes.
	overlay := float32(1)
	if a.intro.started {
		overlay = 1 - introProgress(d, introOutAt, introTotal-introOutAt, c)
	}
	// The overlay's opacity fades the wash and the mark together; the
	// wash's own color is constant.
	frame := ui.Box(c.Key("intro")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
		Background(pal.accent.Alpha(0.12)).Opacity(max(0, min(overlay, 1))).
		Label(locales.IntroLabel()).Focusable().Focus()
	// It holds the keyboard while it shows, and takes every event: a
	// press or a scroll ends it, and a key ends it as it goes up, so that
	// the key, and the text it types, go nowhere rather than to the pane
	// the focus goes back to.
	frame.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputKeyUp, ui.InputPointerDown, ui.InputScroll:
			a.finishIntro()
		}
		return true
	})
	frame.Children(func() {
		ui.Column(c).Fill().Center().Gap(0).Children(func() {
			// The mark comes in from the reveal's first frame, so a wait
			// for the state does not fade it in twice.
			mark := introProgress(c.Now().Sub(a.intro.shown), introMarkAt, introMark, c)
			box := ui.Box(c).Size(58, 58).Radius(15).Center().Background(pal.accent).
				TextColor(ui.Hex("#ffffff")).Opacity(mark)
			if !introReduce(c) {
				box.Scale(0.94+0.06*mark, 0.94+0.06*mark)
			}
			box.Children(func() { controlIcon(c, terminalIcon, 28, ui.Hex("#ffffff"), pal.dark) })
		})
	})
}

// finishIntro puts the reveal away and, once the frame it ends in is
// built, gives the keyboard back to the pane the user is in, unless a
// dialog has it. A reveal the user ends while it waits for the state
// counts as seen.
func (a *App) finishIntro() {
	if a.intro.done {
		return
	}
	if !a.intro.started {
		a.savePrefs()
	}
	a.intro.done = true
	a.defer_(func() {
		if !a.modalOpen() {
			a.focusTerminal()
		}
	})
}
