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

	label, icon string
	primary     bool
	active      bool
	enabled     *bool
	action      string
	placeholder string

	value, min, max, step, rate float64
	at                          int64

	grow    float64
	gap     int
	padding int
	align   string

	title, detail string
	items         []protocol.Item
	variant       string
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
		Grow:        n.grow,
		Gap:         n.gap,
		Padding:     n.padding,
		Align:       n.align,
		Title:       n.title,
		Detail:      n.detail,
		Variant:     n.variant,
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

// Role sets a text's role: title, body, caption or mono.
func (n *Node) Role(role string) *Node { n.role = role; return n }

// Color sets a part's color role: text, muted or accent.
func (n *Node) Color(color string) *Node { n.color = color; return n }

// Lines limits a text to a number of lines.
func (n *Node) Lines(lines int) *Node { n.lines = lines; return n }

// Marquee scrolls a title that does not fit. BRU-169.
func (n *Node) Marquee() *Node { n.marquee = true; return n }

// Size sets an image's size in points, or an icon button's, which is
// square and takes the width.
func (n *Node) Size(width, height int) *Node {
	n.width, n.height = width, height
	return n
}

// Label names an icon button for its tooltip and for assistive
// technology. Without it the app names the button by its icon.
func (n *Node) Label(label string) *Node { n.label = label; return n }

// Radius rounds a picture's corners.
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
