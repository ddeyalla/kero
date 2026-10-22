package sdk

import (
	"strings"

	"kero/internal/plugin/protocol"
)

// The parts a plugin draws with, built by the functions below and
// chained where a field is optional:
//
//	sdk.Column("root",
//		sdk.Text("title", "Hello").Role("title").Marquee(),
//		sdk.Button("go", "Start").Primary(),
//	)
type Node struct {
	typ, id  string
	children []*Node

	text, role, color string
	lines             int
	marquee           bool

	src, data             string
	width, height, radius int
	tint                  bool

	label, icon, mark string
	primary           bool
	active            bool
	enabled           *bool
	action            string
	placeholder       string

	value, min, max, step, rate float64
	at                          int64
	marker                      *float64

	grow    float64
	gap     int
	padding int
	align   string

	title, detail string
	items         []protocol.Item
	variant       string

	surface  string
	segments []protocol.Segment
}

// Build returns the protocol node, with the children built.
func (n *Node) Build() *protocol.Node {
	out := &protocol.Node{
		Type:        n.typ,
		ID:          n.id,
		Items:       n.items,
		Text:        n.text,
		Role:        n.role,
		Color:       n.color,
		Lines:       n.lines,
		Marquee:     n.marquee,
		URL:         n.src,
		Data:        n.data,
		Width:       n.width,
		Height:      n.height,
		Radius:      n.radius,
		Tint:        n.tint,
		Label:       n.label,
		Icon:        n.icon,
		Mark:        n.mark,
		Primary:     n.primary,
		Active:      n.active,
		Enabled:     n.enabled,
		Action:      n.action,
		Placeholder: n.placeholder,
		Value:       n.value,
		Min:         n.min,
		Max:         n.max,
		Step:        n.step,
		Rate:        n.rate,
		At:          n.at,
		Marker:      n.marker,
		Grow:        n.grow,
		Gap:         n.gap,
		Padding:     n.padding,
		Align:       n.align,
		Title:       n.title,
		Detail:      n.detail,
		Variant:     n.variant,
		Surface:     n.surface,
		Segments:    n.segments,
	}
	for _, child := range n.children {
		out.Children = append(out.Children, child.Build())
	}
	return out
}

// Tree makes the node a tree the host can draw.
func (n *Node) Tree() protocol.Tree { return protocol.Tree{Root: n.Build()} }

// ScrollingTree is the tree of the part as a pane that scrolls as one: its
// lists show all their rows, and the pane scrolls past them.
func (n *Node) ScrollingTree() protocol.Tree {
	return protocol.Tree{Root: n.Build(), Scroll: true}
}

func newNode(typ, id string) *Node { return &Node{typ: typ, id: id} }

// Row lays its children out side by side.
func Row(id string, children ...*Node) *Node {
	n := newNode(protocol.NodeRow, id)
	n.children = children
	return n
}

// Column lays its children out one under another.
func Column(id string, children ...*Node) *Node {
	n := newNode(protocol.NodeColumn, id)
	n.children = children
	return n
}

// Text draws a line of text.
func Text(id, text string) *Node {
	n := newNode(protocol.NodeText, id)
	n.text = text
	return n
}

// Clock draws an elapsed time, in seconds, as m:ss, capped at max
// seconds when max is above zero. With Playing it counts on its own in
// the host. The fallback text is what a host without clocks shows.
func Clock(id string, seconds, max float64) *Node {
	n := newNode(protocol.NodeText, id)
	n.value, n.max = seconds, max
	n.text = FormatClock(seconds)
	return n
}

// FormatClock writes seconds as m:ss, or h:mm:ss from an hour.
func FormatClock(seconds float64) string { return protocol.FormatClock(seconds) }

// Image draws a picture from a URL, or from data for an inline image.
func Image(id, src string) *Node {
	n := newNode(protocol.NodeImage, id)
	if strings.HasPrefix(src, "data:") {
		n.data = src
	} else {
		n.src = src
	}
	return n
}

// Button draws a button with a label.
func Button(id, label string) *Node {
	n := newNode(protocol.NodeButton, id)
	n.label = label
	return n
}

// IconButton draws a button with one of the protocol's icons.
func IconButton(id, icon string) *Node {
	n := newNode(protocol.NodeIconButton, id)
	n.icon = icon
	return n
}

// Slider draws a slider between min and max.
func Slider(id string, value, min, max float64) *Node {
	n := newNode(protocol.NodeSlider, id)
	n.value, n.min, n.max = value, min, max
	return n
}

// Progress draws a bar: a value, or a playing bar that moves by rate
// from at, which the host advances on its own.
func Progress(id string, value float64) *Node {
	n := newNode(protocol.NodeProgress, id)
	n.value = value
	return n
}

// List draws rows, each with a picture, a title, a subtitle or a
// trailing text.
func List(id string, items ...protocol.Item) *Node {
	n := newNode(protocol.NodeList, id)
	n.items = items
	return n
}

// SearchField draws a field that sends change and submit events.
func SearchField(id, placeholder string) *Node {
	n := newNode(protocol.NodeSearchField, id)
	n.placeholder = placeholder
	return n
}

// Query sets the text a search field shows, such as a query saved before
// a restart. The host puts it in the field when the field first shows,
// and again only when the plugin sends another text, so a render does
// not undo what the user is typing.
func (n *Node) Query(query string) *Node { n.text = query; return n }

// EmptyState draws a title, a detail and one action.
func EmptyState(id, title, detail string) *Node {
	n := newNode(protocol.NodeEmptyState, id)
	n.title, n.detail = title, detail
	return n
}

// Spacer draws empty space that grows to fill the room.
func Spacer(id string) *Node {
	n := newNode(protocol.NodeSpacer, id)
	n.grow = 1
	return n
}

// Divider draws a thin line.
func Divider(id string) *Node { return newNode(protocol.NodeDivider, id) }

// Segmented draws a control of two or more choices, one chosen: the item
// that is Active. Choosing another sends a select with the item's id.
func Segmented(id string, items ...protocol.Item) *Node {
	n := newNode(protocol.NodeSegmented, id)
	n.items = items
	return n
}

// Donut draws a ring cut into slices by their values. Center puts words
// in its middle, and Width sets its diameter.
func Donut(id string, segments ...protocol.Segment) *Node {
	n := newNode(protocol.NodeDonut, id)
	n.segments = segments
	return n
}

// Role sets a text's role: headline, title, body, caption, mono or strong.
func (n *Node) Role(role string) *Node { n.role = role; return n }

// Color sets a part's color role: text, muted, accent, danger, warning,
// success, or one of the hues (orange, green, teal, blue, purple, gray).
func (n *Node) Color(color string) *Node { n.color = color; return n }

// Lines limits a text to a number of lines.
func (n *Node) Lines(lines int) *Node { n.lines = lines; return n }

// Marquee scrolls a title that does not fit. BRU-169.
func (n *Node) Marquee() *Node { n.marquee = true; return n }

// Size sets an image's size in points, or an icon button's, which is
// square and takes the width. A donut's diameter is its width.
func (n *Node) Size(width, height int) *Node {
	n.width, n.height = width, height
	return n
}

// Icon draws one of the protocol's icons before a text's words, in the
// text's color. A text with an icon and no words is just the icon.
func (n *Node) Icon(icon string) *Node { n.icon = icon; return n }

// Mark draws a program's logo, such as claude, before a text's words.
func (n *Node) Mark(mark string) *Node { n.mark = mark; return n }

// Surface draws a row or column as a card.
func (n *Node) Surface(surface string) *Node { n.surface = surface; return n }

// Marker draws a tick across a progress bar at a fraction of its length,
// from 0 to 1, such as where the usage of a window should be by now.
func (n *Node) Marker(fraction float64) *Node { n.marker = &fraction; return n }

// Center sets the words in a donut's middle and the caption under them.
func (n *Node) Center(text, detail string) *Node {
	n.text, n.detail = text, detail
	return n
}

// Width sets a donut's diameter in points, or the width of a part that
// has one of its own.
func (n *Node) Width(width int) *Node { n.width = width; return n }

// Label names an icon button, an icon with no words, or a bar, for its
// tooltip and for assistive technology. Without it the app names the button by its icon.
func (n *Node) Label(label string) *Node { n.label = label; return n }

// Radius rounds a picture's corners, or a card's.
func (n *Node) Radius(radius int) *Node { n.radius = radius; return n }

// Tint draws a soft gradient behind the pane from the picture's own
// color. BRU-167.
func (n *Node) Tint() *Node { n.tint = true; return n }

// Primary makes a button the pane's main action. An icon button that is
// primary is a filled disc, as a player's play button.
func (n *Node) Primary() *Node { n.primary = true; return n }

// Active draws an icon button as switched on, in the accent color, such as
// shuffle while it is on.
func (n *Node) Active(on bool) *Node { n.active = on; return n }

// Enabled sets whether a part takes input.
func (n *Node) Enabled(enabled bool) *Node { n.enabled = &enabled; return n }

// Disabled stops a part taking input.
func (n *Node) Disabled() *Node { return n.Enabled(false) }

// Action gives an empty state its one button.
func (n *Node) Action(label string) *Node { n.action = label; return n }

// Grow sets how much of the free room a part takes.
func (n *Node) Grow(grow float64) *Node { n.grow = grow; return n }

// Gap sets the space between a row's or column's children.
func (n *Node) Gap(gap int) *Node { n.gap = gap; return n }

// Padding sets the space inside a row or column.
func (n *Node) Padding(padding int) *Node { n.padding = padding; return n }

// Align sets how a row or column lines up: start, center, end or
// spaceBetween.
func (n *Node) Align(align string) *Node { n.align = align; return n }

// Step sets a slider's step.
func (n *Node) Step(step float64) *Node { n.step = step; return n }

// Range sets a slider's value, min and max.
func (n *Node) Range(value, min, max float64) *Node {
	n.value, n.min, n.max = value, min, max
	return n
}

// Playing makes a progress bar, a slider or a clock move by rate (value
// units a second) from at, in unix milliseconds, the instant the value
// held. The host advances it on its own, so the plugin sends it once per
// change of the playback, not on every tick. A slider the user drags
// does not move under the finger.
func (n *Node) Playing(rate float64, at int64) *Node {
	n.rate, n.at = rate, at
	return n
}

// Grid draws a list as tiles of tileWidth points: each item's picture,
// square, with its title and subtitle under it, wrapped into rows. The
// plugin chooses the width that fills the pane's row.
func (n *Node) Grid(tileWidth int) *Node {
	n.variant, n.width = protocol.VariantGrid, tileWidth
	return n
}

// Detail sets an empty state's detail.
func (n *Node) Detail(detail string) *Node { n.detail = detail; return n }
