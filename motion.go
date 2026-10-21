package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The app's motion, as tokens: one place holds the numbers, so that the
// app moves as one, and a change to how it moves changes it everywhere.
// Reduce Motion is mygo's own; nothing here does more.

var (
	// press is how a control answers the pointer going down: at once,
	// over before the finger is off it.
	press = ui.Animation{Duration: 90 * time.Millisecond}
	// hover is a tint that follows the pointer: a row's, a button's, the
	// pane switcher's glow.
	hover = ui.Animation{Duration: 120 * time.Millisecond}
	// row is a session row's own changes: its color, its chevron, and how
	// rows arrive and go as a project collapses.
	row = ui.Animation{Duration: 160 * time.Millisecond}
	// focus is a pane taking the focus: its edge lights, and a pane
	// opening or closing keeps its face, rather than snapping.
	focus = ui.Animation{Duration: 160 * time.Millisecond}
	// slide is the bigger moves: the sidebar's, the panes' layout. A
	// curve, not a spring: a spring's long tail crawls under a pixel a
	// frame, which whole pixels turn into hops and holds, a shake as it
	// settles. This one lands in about 13 frames, moving in every one.
	slide = ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.CubicBezier(0.25, 1, 0.5, 1)}
	// springDrag is how what the user dragged arrives where it went: a
	// pane springs into its new place.
	springDrag = ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.18}}
	// springUI is a spring that is not a drag's: a preview pill's.
	springUI = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0.12}}
)

// rowTransition is how session rows arrive and go as a project collapses:
// each folds to nothing along the list's direction, as its neighbours
// make room, and unfolds as it comes back. It moves position only, and
// the fold: the row's colors ease as they did, and twice would be twice,
// and its width follows the sidebar's divider at once, as the headers do.
var rowTransition = ui.ElementTransition{
	Position: true,
	Enter:    &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true},
	Duration: row.Duration,
}

// headerTransition is how a project's header moves as the rows over and
// under it arrive and go.
var headerTransition = ui.ElementTransition{Position: true, Duration: row.Duration}
