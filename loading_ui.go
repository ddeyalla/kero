package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The loading ring is loading.dev's Ring, as MyGo's loading plugin draws
// it: in a square of 24, a faint track of radius 10 and an arc on it 1.6
// radians long, both 2.5 wide, turning once in ringTurn. The plugin asks
// for a frame each refresh while it shows, which paints the whole window
// again at the display's rate for as long as a session works; this one
// turns in steps of ringFrame, 15 a second, which reads as the same
// motion at a quarter of the frames.
const (
	ringTurn  = 800 * time.Millisecond
	ringFrame = time.Second / 15
	ringArc   = 1.6 / (2 * math.Pi) // of the circle
)

// ringStartKey keys, in a ring's element, when it began to turn.
type ringStartKey struct{}

// ringStart is when the ring in e began to turn: its first frame's time.
func ringStart(c *ui.Context, e ui.Element) time.Time {
	now := c.Now()
	return *ui.Local(e, ringStartKey{}, func() time.Time { return now })
}

// turningRing draws the loading ring in r, in col, turned as far as it has
// gone since start, and asks for its next step. Where the desktop asks for
// less motion, still, it stands in the plugin's pose at rest, its arc from
// 3 o'clock, and asks for nothing.
func turningRing(p *ui.Painter, r ui.Rect, col ui.Color, start time.Time, still bool) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	side := min(r.W, r.H)
	rad, w := side*10/24, side*2.5/24
	var track ui.Path
	track.Circle(cx, cy, rad)
	p.StrokePath(&track, w, col.Alpha(0.2))
	var turn float32
	if !still {
		turn = float32(p.Now().Sub(start)%ringTurn) / float32(ringTurn) * 360
		p.After(ringFrame)
	}
	p.Transform(ui.Rotation(turn), cx, cy, func() {
		var arc ui.Path
		arc.Circle(cx, cy, rad)
		p.StrokePath(arc.Trim(0, ringArc), w, col)
	})
}

// working fills its room with the ring the app turns while it works, at
// the size of a spinner and in the muted text's color, and nothing else:
// a view waiting on a read, a pane attaching, the app connecting. label
// names it for assistive technology, a progress indicator of unknown
// length.
func working(c *ui.Context, pal *palette, label string) {
	ui.Column(c).Fill().Center().Children(func() {
		ring := ui.Box(c).Size(16, 16).Shrink(0).Role(ui.RoleProgress).Range(0, 1, -1).Label(label)
		start, still, col := ringStart(c, ring), c.Preferences().ReduceMotion, pal.textMuted
		ring.Draw(func(p *ui.Painter, r ui.Rect) { turningRing(p, r, col, start, still) })
	})
}
