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
	}
	for _, child := range n.children {
		out.Children = append(out.Children, child.Build())
	}
	return out
}

// Tree makes the node a tree the host can draw.
func (n *Node) Tree() protocol.Tree { return protocol.Tree{Root: n.Build()} }

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

// Size sets an image's size in points.
func (n *Node) Size(width, height int) *Node {
	n.width, n.height = width, height
	return n
}

// Radius rounds a picture's corners.
func (n *Node) Radius(radius int) *Node { n.radius = radius; return n }

// Tint draws a soft gradient behind the pane from the picture's own
// color. BRU-167.
func (n *Node) Tint() *Node { n.tint = true; return n }

// Primary makes a button the pane's main action.
func (n *Node) Primary() *Node { n.primary = true; return n }

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

// Playing makes a progress bar move by rate from at, in unix
// milliseconds, which the host advances on its own.
func (n *Node) Playing(rate float64, at int64) *Node {
	n.rate, n.at = rate, at
	return n
}

// Detail sets an empty state's detail.
func (n *Node) Detail(detail string) *Node { n.detail = detail; return n }
