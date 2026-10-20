package main

import (
	"hash/fnv"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// The marks the Linear view draws: a state's glyph, in the team's own
// color, as Linear draws it; the chips a card shows what an issue has
// with; and the initials of a person, for the comments.

// stateMark is how a workflow state is drawn: what it means, its color,
// and, for a started state, how far along the workflow it is, which the
// glyph fills a pie of.
type stateMark struct {
	typ   string
	color ui.Color
	fill  float32
}

// linearMarks are the marks of a team's states, by name. A started state
// fills its pie by its place among the started states: In Progress half,
// In Review more.
func linearMarks(states []proto.LinearWorkflowState, pal *palette) map[string]stateMark {
	marks := make(map[string]stateMark, len(states))
	var started []string
	for _, s := range states {
		if s.Type == "started" {
			started = append(started, s.Name)
		}
	}
	for _, s := range states {
		m := stateMark{typ: s.Type, color: linearStateColor(s.Type, s.Color, pal)}
		if s.Type == "started" {
			m.fill = 0.5
			if n := len(started); n > 1 {
				for i, name := range started {
					if name == s.Name {
						m.fill = 0.5 + 0.25*float32(i)/float32(n-1)
					}
				}
			}
		}
		marks[s.Name] = m
	}
	return marks
}

// linearStateColor is a state's color: the team's own, as Linear shows
// it, but for the states not yet started, which Linear draws gray, and
// which the team's pale gray would lose on a white card. A daemon from
// before the colors were read sends none: then one by what the state
// means.
func linearStateColor(typ, color string, pal *palette) ui.Color {
	switch typ {
	case "backlog", "unstarted", "canceled", "duplicate":
		return pal.textFaint
	}
	if strings.HasPrefix(color, "#") && (len(color) == 7 || len(color) == 4) {
		return ui.Hex(color)
	}
	switch typ {
	case "started":
		return ui.Hex("#f2c94c")
	case "completed":
		return ui.Hex("#5e6ad2")
	}
	return pal.textMuted
}

// markOf is the mark of the state named, falling back on what the type
// says when the state is not the team's (a relation's, say).
func markOf(marks map[string]stateMark, name, typ string, pal *palette) stateMark {
	if m, ok := marks[name]; ok {
		return m
	}
	m := stateMark{typ: typ, color: linearStateColor(typ, "", pal)}
	if typ == "started" {
		m.fill = 0.5
	}
	return m
}

// stateGlyph draws a state as Linear does: a dashed ring for the
// backlog, a ring for what is to do, a ring with a pie for what is
// under way, a filled check for what is done, a filled cross for what
// was dropped.
func stateGlyph(c *ui.Context, pal *palette, m stateMark, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := min(r.W, r.H)/2 - 0.75
		stroke := max(1.2, size/9)
		switch m.typ {
		case "backlog":
			for k := range 8 {
				var seg ui.Path
				seg.Circle(cx, cy, rad)
				from := float32(k) / 8
				p.StrokePath(seg.Trim(from, from+0.075), stroke, m.color)
			}
		case "started":
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, stroke, m.color)
			// A pie inside the ring, from the top, clockwise, a hair
			// clear of it.
			inner := rad - stroke - max(0.8, size/16)
			steps := int(40*m.fill) + 2
			var pie ui.Path
			pie.MoveTo(cx, cy)
			for k := 0; k <= steps; k++ {
				t := -math.Pi/2 + 2*math.Pi*float64(m.fill)*float64(k)/float64(steps)
				pie.LineTo(cx+inner*float32(math.Cos(t)), cy+inner*float32(math.Sin(t)))
			}
			pie.Close()
			p.FillPath(&pie, m.color)
		case "completed":
			var disc ui.Path
			disc.Circle(cx, cy, rad+0.5)
			p.FillPath(&disc, m.color)
			check := (&ui.Path{}).MoveTo(cx-rad*0.42, cy+0.1).LineTo(cx-rad*0.1, cy+rad*0.36).LineTo(cx+rad*0.46, cy-rad*0.32)
			p.StrokePath(check, stroke, ui.Hex("#ffffff"))
		case "canceled", "duplicate":
			var disc ui.Path
			disc.Circle(cx, cy, rad+0.5)
			p.FillPath(&disc, m.color)
			k := rad * 0.36
			p.Line(cx-k, cy-k, cx+k, cy+k, stroke, pal.card)
			if m.typ == "canceled" {
				p.Line(cx+k, cy-k, cx-k, cy+k, stroke, pal.card)
			}
		default:
			var ring ui.Path
			ring.Circle(cx, cy, rad)
			p.StrokePath(&ring, stroke, m.color)
		}
	})
	return box
}

// blockedGlyph is the mark of an issue another blocks: a red disc with a
// bar, as Linear's.
func blockedGlyph(c *ui.Context, pal *palette, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := min(r.W, r.H) / 2
		var disc ui.Path
		disc.Circle(cx, cy, rad)
		p.FillPath(&disc, pal.danger)
		p.Line(cx-rad*0.48, cy, cx+rad*0.48, cy, max(1.3, size/8), ui.Hex("#ffffff"))
	})
	return box
}

// propChip is a small outlined chip of what an issue has, as Linear's
// cards show them: a mark and a few words.
func propChip(c *ui.Context, pal *palette, fn func()) ui.Element {
	return ui.Row(c).Height(20).Shrink(0).Gap(4).Padding(0, 6).Radius(6).AlignItems(ui.Center).
		InsetShadow(0, 0, 0, 1, pal.text.Alpha(0.1)).TextColor(pal.textMuted).Children(fn)
}

// avatarColors are the fills of people's initials: soft, and told apart.
var avatarColors = []string{"#7c8b2e", "#b0603a", "#4a7fb5", "#8e5bb5", "#2f8f7a", "#b5487a", "#a07a2a", "#5a6bb5"}

// avatar is a person's initials in a disc of a color their name picks.
func avatar(c *ui.Context, name string, size float32) ui.Element {
	h := fnv.New32a()
	h.Write([]byte(name))
	bg := ui.Hex(avatarColors[h.Sum32()%uint32(len(avatarColors))])
	return ui.Box(c).Size(size, size).Radius(size / 2).Center().Shrink(0).Background(bg).Children(func() {
		ui.Text(c, initials(name)).FontSize(size * 0.42).FontWeight(650).TextColor(ui.Hex("#ffffff")).SingleLine()
	})
}

// initials are the first letters of a name's first two words, as "DD"
// for Divyanshu DV.
func initials(name string) string {
	words := strings.Fields(name)
	out := ""
	for _, w := range words {
		r := []rune(w)
		out += strings.ToUpper(string(r[0]))
		if len([]rune(out)) == 2 {
			break
		}
	}
	if out == "" {
		return "?"
	}
	return out
}
