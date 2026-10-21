package main

import (
	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"
)

// working fills its room with the ring the app turns while it works, at
// the size of a spinner and in the muted text's color, and nothing else:
// a view waiting on a read, a pane attaching, the app connecting. label
// names the indicator for assistive technology, and is "" where the
// spinner had no name of its own.
func working(c *ui.Context, pal *palette, label string) {
	ui.Column(c).Fill().Center().Children(func() {
		ring := loading.Ring(c, loading.Options{Size: 16, Color: pal.textMuted})
		if label != "" {
			ring.Label(label)
		}
	})
}
