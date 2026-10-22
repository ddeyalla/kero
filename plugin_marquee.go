package main

// text.marquee: a text that scrolls, slowly, only when it does not fit.
// The text pauses at its start, scrolls to its end, pauses, and starts over.
// With Reduce Motion on, it stays still and ends in an ellipsis instead.

import (
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
)

// The marquee's motion: a pause at each end, and a slow scroll between
// them. The speed is in pixels a second, so a longer text scrolls longer.
const (
	pluginMarqueePause = 900 * time.Millisecond
	pluginMarqueeSpeed = 24.0
	pluginMarqueeFrame = 16 * time.Millisecond
)

// pluginMarquee is one marquee text's state across renders.
type pluginMarquee struct {
	since    time.Time // when this overflow began
	text     string    // the text this turn belongs to
	natural  float32   // the text's own width
	avail    float32   // the room the pane gives it
	distance float32   // how far it scrolls
	overflow bool      // the text does not fit this frame
	reduce   bool      // Reduce Motion is on
	offset   float32   // how far it is scrolled this frame
	moving   bool      // this frame asked for another one
}

// text draws a text part: its role's size and weight, its color, and its
// lines, as many as it takes unless the plugin limits them.
func (v *pluginView) text(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool) {
	size, weight, family := pluginText(n.Role)
	if n.Marquee {
		v.marquee(c, pal, n, horizontal, size, weight, family)
		return
	}
	e := ui.Text(c.Key(n.ID), n.Text).FontSize(size).FontWeight(weight).TextColor(pluginTextColor(pal, n)).MinWidth(0)
	// Digits keep one width, so that figures in a column line up.
	e.FontFeatures("tnum")
	if n.Label != "" {
		e.Label(n.Label).Tooltip(n.Label)
	}
	if n.Width > 0 {
		e.Width(float32(n.Width))
	}
	if n.Height > 0 {
		e.Height(float32(n.Height))
	}
	if family != "" {
		e.Font(family)
	}
	switch {
	case n.Lines == 1:
		e.SingleLine().Ellipsis("…")
	case n.Lines > 1:
		e.MaxLines(n.Lines).Ellipsis("…")
	}
	if n.Grow > 0 {
		e.Grow(float32(n.Grow))
	}
}

// marquee draws a text part that scrolls when it does not fit its room.
// The text is measured at its own width, and the room it has is the width
// its window had last frame, so the first frame shows it still and the
// next one knows whether it must move. The window takes the room its parent
// gives it, never the width it had: a room that grows, as the pane widens,
// is the room the text has next frame, and a text that now fits stops.
func (v *pluginView) marquee(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool, size float32, weight int, family string) {
	m := v.marquees[n.ID]
	if m == nil {
		m = &pluginMarquee{}
		v.marquees[n.ID] = m
	}
	natural, _ := c.MeasureText(0, ui.Span{Text: n.Text, Size: size, Weight: weight, Font: family})
	avail := m.avail
	if n.Width > 0 {
		// The plugin says how wide the text is: that is the room it
		// must fit, whatever the pane around it is.
		avail = float32(n.Width)
	}
	reduce := c.Preferences().ReduceMotion
	fits := avail > 0 && natural <= avail+0.5
	overflow := avail > 0 && !fits
	// A new text, a new room, or Reduce Motion turning on or off starts
	// the motion over: a new title must not begin in the middle of the
	// old one's turn, nor in its pause at the end.
	if m.text != n.Text || m.natural != natural || m.overflow != overflow || m.reduce != reduce {
		m.since = time.Time{}
	}
	m.text, m.natural, m.overflow, m.reduce = n.Text, natural, overflow, reduce
	m.offset, m.moving = 0, false

	clip := ui.Row(c.Key("plugin/window:" + n.ID)).Clip().MinWidth(0).AlignItems(ui.Center)
	if horizontal {
		// In a row the window starts at its text's width, grows into
		// the row's free room, and shrinks to the room the row has.
		clip.Grow(1).Shrink(1)
	} else {
		// In a column the window is as wide as the column.
		clip.AlignSelf(ui.Stretch)
	}
	if n.Width > 0 {
		clip.Width(float32(n.Width)).Shrink(0)
	}
	if n.Grow > 0 {
		clip.Grow(float32(n.Grow))
	}
	clip.Children(func() {
		e := func() ui.Element {
			return ui.Text(c.Key(n.ID), n.Text).FontSize(size).FontWeight(weight).TextColor(pluginTextColor(pal, n)).SingleLine()
		}
		switch {
		case avail <= 0:
			// The first frame: let the text size itself, and learn the
			// room next frame.
			text := e().MinWidth(0).Ellipsis("…")
			if family != "" {
				text.Font(family)
			}
			text.Grow(1)
		case fits:
			// It fits: no motion at all.
			text := e().MinWidth(0).Ellipsis("…")
			if family != "" {
				text.Font(family)
			}
			text.Grow(1)
		case reduce:
			// Reduce Motion: still, and the end of the line is an
			// ellipsis instead.
			text := e().MinWidth(0).Ellipsis("…")
			if family != "" {
				text.Font(family)
			}
			text.Grow(1)
		default:
			if distance := natural - avail; distance != m.distance {
				// The room the text has to cross changed: the turn
				// starts over from the beginning of the new one.
				m.since = time.Time{}
				m.distance = distance
			}
			if m.since.IsZero() {
				m.since = c.Now()
			}
			elapsed := c.Now().Sub(m.since)
			m.offset = pluginMarqueeOffset(m.distance, elapsed)
			text := e().Width(natural).Shrink(0).Translate(-m.offset, 0)
			if family != "" {
				text.Font(family)
			}
			if wait := pluginMarqueeNext(m.distance, elapsed); wait > 0 {
				c.After(wait)
				m.moving = true
			}
		}
	})
	if n.Width > 0 {
		m.avail = float32(n.Width)
	} else if b := clip.Bounds(); b.W > 0 {
		m.avail = b.W
	}
}

// pluginMarqueeOffset returns how far a marquee of distance has scrolled
// after elapsed: still during the pause at the start, out and back across
// the text, held while it pauses at the end, then at its start again.
func pluginMarqueeOffset(distance float32, elapsed time.Duration) float32 {
	if distance <= 0 || elapsed <= 0 {
		return 0
	}
	scroll := pluginMarqueeScroll(distance)
	period := 2*pluginMarqueePause + scroll
	if period <= 0 {
		return 0
	}
	t := elapsed % period
	switch {
	case t < pluginMarqueePause:
		return 0
	case t < pluginMarqueePause+scroll:
		return distance * float32(t-pluginMarqueePause) / float32(scroll)
	default:
		return distance
	}
}

// pluginMarqueeNext returns how long the marquee may rest before its next
// frame: a frame at a time while it scrolls, and the rest of the pause
// while it waits at either end.
func pluginMarqueeNext(distance float32, elapsed time.Duration) time.Duration {
	if distance <= 0 {
		return 0
	}
	scroll := pluginMarqueeScroll(distance)
	period := 2*pluginMarqueePause + scroll
	if period <= 0 {
		return 0
	}
	t := elapsed % period
	switch {
	case t < pluginMarqueePause:
		return pluginMarqueePause - t
	case t < pluginMarqueePause+scroll:
		return pluginMarqueeFrame
	default:
		return period - t
	}
}

// pluginMarqueeScroll is how long the text takes to cross its overflow.
func pluginMarqueeScroll(distance float32) time.Duration {
	if distance <= 0 {
		return 0
	}
	return time.Duration(float64(distance) / pluginMarqueeSpeed * float64(time.Second))
}
