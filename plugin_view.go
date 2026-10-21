package main

import (
	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/locales"
)

// pluginEvent is what the user did in a plugin's tree, as the plugin's
// protocol names it: the node, the kind of the gesture, and its value.
type pluginEvent struct {
	Node  string
	Kind  string
	Value any
}

// pluginView draws the tree of one plugin in a pane, and sends what the
// user does in it back to the plugin.
//
// One view lives with each pane of a plugin, as a terminal keeps its
// screen: a tree arrives on every render, and the view keeps what the
// drawing needs between them, such as a scroll position.
type pluginView struct{}

// newPluginView makes the view of one plugin in one pane.
func newPluginView() *pluginView { return &pluginView{} }

// draw draws the tree the plugin last rendered, and sends the user's
// gestures through send.
func (v *pluginView) draw(c *ui.Context, pal *palette, tree *protocol.Tree, send func(pluginEvent)) {
	drawPluginTree(c, pal, tree, send)
}

// drawPluginTree draws a plugin's tree.
//
// Until Lane B's renderer lands, this draws what the tree holds: how many
// nodes the plugin sent. The seam is this one function, and the call
// above: the renderer replaces its body, and the panes keep working.
func drawPluginTree(c *ui.Context, pal *palette, tree *protocol.Tree, send func(pluginEvent)) {
	count := 0
	var walk func(*protocol.Node)
	walk = func(n *protocol.Node) {
		if n == nil {
			return
		}
		count++
		for _, child := range n.Children {
			walk(child)
		}
	}
	if tree != nil {
		walk(tree.Root)
	}
	title := locales.PluginTreePlaceholder()
	detail := locales.PluginTreeCount(count)
	if count == 1 {
		detail = locales.PluginTreeCountOne()
	}
	paneNothing(c, pal, projectIcon, title, detail)
	_ = send
}
