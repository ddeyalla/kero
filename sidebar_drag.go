package main

import "github.com/egoist/mygo/ui"

// Only the object carried by the pointer gets glass. Its place stays put
// and fades back, while MyGo paints the lifted copy above the window.
func sidebarDragSource(c *ui.Context, source ui.Element, pal *palette) {
	if source.Dragging() {
		if glassShows(c) {
			// The toolkit composites carried copies at 75% opacity. A stronger
			// tint keeps the title legible over the destination's text.
			material := panelGlass(pal, true)
			material.Tint = pal.popover.Alpha(0.94)
			glassShadow(source.Material(material), pal)
		} else {
			floating(source.Background(pal.popover), pal)
		}
		source.Opacity(0.35)
	}
}

func sidebarDropAfter(target ui.Element) bool {
	_, y, _ := target.PointerPosition()
	return y > target.Bounds().H/2
}

// The insertion rule is paint, not a border: crossing a row never changes
// its content box, text position, selection or neighbors. A neutral rule
// distinguishes placement from the app's accent-colored focus and status.
func sidebarInsertion(target ui.Element, pal *palette, after bool) {
	target.DrawOver(func(p *ui.Painter, r ui.Rect) {
		const thickness = 2
		y := r.Y
		if after {
			y += r.H - thickness
		}
		p.Fill(ui.Rect{X: r.X + 10, Y: y, W: max(0, r.W-20), H: thickness}, pal.textMuted.Alpha(0.8), thickness/2)
	})
}

type projectDrag struct{ id string }

// Project targets cover the whole group, so dropping after an expanded
// project marks its bottom, below its sessions, rather than its header.
func (a *App) projectGroupDrag(pal *palette, group ui.Element, id string) {
	if id == "" || a.conn != connReady {
		return
	}
	if d, over := ui.DragOver[projectDrag](group); over {
		after := sidebarDropAfter(group)
		if _, ok := a.projectMove(d.id, id, after); ok {
			sidebarInsertion(group, pal, after)
		}
	}
	if d, dropped := ui.Drop[projectDrag](group); dropped {
		source, after := d.id, sidebarDropAfter(group)
		a.defer_(func() { a.reorderProject(source, id, after) })
	}
}
