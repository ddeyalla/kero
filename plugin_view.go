package main

// A plugin pane's parts. A plugin hands its pane a tree of parts (the
// protocol's Node), and drawPluginTree draws them as kero's own controls:
// in the app's palette, fonts and motion, with the focus and the scrolling
// kept by part id, and with what the user does sent back to the plugin as
// events that name the part.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	xdraw "golang.org/x/image/draw"

	"kero/internal/plugin/protocol"
	"kero/locales"
)

// pluginEvent is what a part of the tree sends the plugin: the id of the
// part, the kind of event, and its value, if it has one.
type pluginEvent struct {
	Node  string
	Kind  string
	Value any
}

// pluginSource is where a picture comes from: a URL to fetch, or a data:
// URI to read. A picture the pane takes its tint from is read for its
// color as well, and is its own entry in the cache.
type pluginSource struct {
	url  string
	data string
	tint bool
	// side is the longest side, in pixels, that the picture is kept at:
	// the size it is first drawn at on a 2x screen, in a bucket. Zero keeps
	// up to pluginMaxImageSide. It is not part of the cache key: the same
	// picture drawn at another size is the same entry, drawn scaled, and
	// never fetched again.
	side int
}

// pluginPicture is one picture of the tree, as the pane shows it: nil
// while it loads, or when it could not be loaded.
type pluginPicture struct {
	src     pluginSource
	loading bool
	bm      *ui.Bitmap
	failed  bool
	tries   int       // fetches started so far
	retryAt time.Time // when a failed fetch may be tried again
	used    uint64    // the draw that last asked for it
	tint    ui.Color  // the picture's dominant color, read once
	tinted  bool
}

// The pictures a pane keeps: those the tree shows, and as many of those it
// no longer shows as fit the budget, the most recent first. A data: URI can
// be large, so the pane does not keep every picture it has ever seen for
// the session; a picture it shows it keeps, whatever their number, or it
// would fetch them again in every draw.
const (
	pluginPictureBudget = 48
	pluginPictureTries  = 3
	pluginPictureRetry  = 2 * time.Second
)

// pluginProgressTick is how often a progress bar that is moving asks for a
// frame, so that it does not stand still between the plugin's renders.
const pluginProgressTick = 100 * time.Millisecond

// pluginSlider is a slider's value while the pane shows it: the value the
// user set, which the plugin's own comes back to.
type pluginSlider struct {
	value     float64
	seen      float64   // the value the plugin last sent
	seenAt    int64     // and the instant it gave with it
	known     bool      // seen is set
	pressed   bool      // the pointer holds the knob
	holdUntil time.Time // the user's value stands, not the run on, until then
}

// pluginClockTick is how often a slider or a clock text that runs on its
// own asks for a frame. The part moves a pixel or a second at a time, so
// it needs fewer frames than a progress bar.
const pluginClockTick = 250 * time.Millisecond

// pluginSliderTick is how often a running slider asks for a repaint: not
// faster than a progress bar, and not faster than it moves a pixel on a
// pane of up to 400.
func pluginSliderTick(n *protocol.Node) time.Duration {
	perPixel := time.Duration((n.Max - n.Min) / n.Rate / 400 * float64(time.Second))
	return max(pluginProgressTick, perPixel)
}

// pluginSliderHold is how long a value the user set stands after they let
// go, for the plugin to answer it with a value of its own. A slider that
// runs on its own would otherwise jump back to the time it had before the
// seek until the answer came; if none comes, it runs on again.
const pluginSliderHold = 3 * time.Second

// pluginSegments is a segmented control's choice while the pane shows it:
// the index the user chose, which the plugin's own comes back to when it
// changes.
type pluginSegments struct {
	index int
	seen  string // the item the plugin last sent as chosen
	known bool   // seen is set
}

// pluginSearch is a search field's text, and the change it owes the
// plugin, which waits for the typing to pause.
type pluginSearch struct {
	query string
	sent  string    // the text the plugin last heard
	given string    // the text the plugin last sent, in the node's text
	due   time.Time // when the change is due
	armed bool
}

// pluginView is what a plugin pane keeps between renders: the focus, the
// scrolling and the controls' values by part id, and the pictures, which it
// fetches and decodes off the UI goroutine. A pane keeps one pluginView.
type pluginView struct {
	mu       sync.Mutex
	focus    string // the part that has the focus, bound with FocusBind
	scroll   map[string]*ui.ScrollState
	sliders  map[string]*pluginSlider
	searches map[string]*pluginSearch
	segments map[string]*pluginSegments
	marquees map[string]*pluginMarquee
	progress map[string]float64 // the value each bar shows this frame
	pictures map[string]*pluginPicture
	markdown map[string]*markdown.Options
	symbols  map[symbolKey]*pluginSymbol
	// lastSymbol is the newest bitmap drawn for each symbol name, which
	// stands in while another size or weight of it is drawn.
	lastSymbol map[string]symbolStandIn
	frame      uint64 // the draw this state was last used in

	// The tint the pane washes itself with: the tinted picture's color,
	// which the pane holds while the next picture loads, and the wash's
	// color this frame, on its crossfade to that target.
	tintColor  ui.Color // the last tinted picture's color, unclamped
	tintTarget ui.Color // that color, clamped for the appearance
	tintShown  ui.Color
	tinted     bool

	// fetch loads a picture, off the UI goroutine; tests replace it. The
	// default reads a data: URI, or fetches a URL.
	fetch func(pluginSource) (*ui.Bitmap, image.Image, error)
	// invalidate asks for a frame, once a picture arrives.
	invalidate func()
	// render draws an SF Symbol's shape, off the UI goroutine; tests
	// replace it. It is nil where the system has none.
	render func(name string, size, weight float64, scale int) (*image.RGBA, bool)

	// openLink opens a link of a markdown part in the browser and tells
	// that it did, which the pane sets when its plugin may (the openURL
	// permission). A link it does not open goes to the plugin as a press.
	openLink func(url string) bool
	// sideways tells that the part being drawn is a child of a row that
	// scrolls sideways, whose children keep their widths.
	sideways bool

	// ring is the path a donut strokes, rebuilt for each slice so that its
	// storage is kept from one frame to the next.
	ring ui.Path

	// keysOn tells that the pane may hear the keys its tree asks for
	// (Tree.Keys): the pane has the focus and nothing else takes the keys.
	// The pane sets it each frame. active and wasActive are the pressable
	// parts that are selected (Node.Active) this frame and the last, so
	// that a part that becomes so is scrolled into view.
	keysOn            bool
	active, wasActive map[string]bool

	// flow tells that the pane scrolls as one this frame, and its lists
	// show all their rows, as the scroll around them moves them.
	flow bool

	ids map[string]bool // the parts drawn this frame
	// The controls that can take the focus, by part id: this frame's, and
	// the last frame's, which tell a part that MyGo sees anew.
	controls, lastControls map[string]uint64
}

// drawPluginTree draws tree for one frame in the pane pal belongs to,
// sending what the user does to the plugin through send. The host keeps
// one pluginView for each pane, so that two panes with a plugin each do
// not share their focus, their scrolling or their pictures.
func drawPluginTree(c *ui.Context, v *pluginView, pal *palette, tree *protocol.Tree, send func(pluginEvent)) {
	if v == nil {
		return
	}
	v.draw(c, pal, tree, send)
}

// newPluginView returns the state of a plugin pane, empty.
func newPluginView() *pluginView {
	return &pluginView{
		scroll:     map[string]*ui.ScrollState{},
		sliders:    map[string]*pluginSlider{},
		searches:   map[string]*pluginSearch{},
		segments:   map[string]*pluginSegments{},
		marquees:   map[string]*pluginMarquee{},
		progress:   map[string]float64{},
		pictures:   map[string]*pluginPicture{},
		markdown:   map[string]*markdown.Options{},
		symbols:    map[symbolKey]*pluginSymbol{},
		lastSymbol: map[string]symbolStandIn{},
		fetch:      pluginFetch,
		render:     renderSymbol,
	}
}

// draw draws the tree for one frame.
func (v *pluginView) draw(c *ui.Context, pal *palette, tree *protocol.Tree, send func(pluginEvent)) {
	if tree == nil || tree.Root == nil {
		return
	}
	v.mu.Lock()
	v.invalidate = c.Invalidate
	v.mu.Unlock()
	v.ids = make(map[string]bool, 64)
	v.wasActive, v.active = v.active, make(map[string]bool)
	v.lastControls, v.controls = v.controls, make(map[string]uint64, len(v.controls))
	v.frame++
	// The root's own key holds the pane's place: a tree of the same root
	// id keeps its controls, and their state, from one render to the next.
	pane := ui.Column(c.Key("plugin-" + tree.Root.ID)).Fill().MinWidth(0).MinHeight(0)
	tint, ok, pending := v.treeTint(tree.Root)
	v.drawTint(pane, tint, ok, pending)
	pane.Children(func() {
		// The parts on a tint draw light, whatever the appearance.
		parts := pal
		if v.tinted {
			parts = pluginOnColor(pal)
		}
		v.flow = tree.Scroll
		if !tree.Scroll {
			v.part(c, parts, tree.Root, true, send)
			return
		}
		// A header stays above the scroll, where the pane's parts that the
		// user needs while reading stay in reach.
		if tree.Header != nil {
			v.part(c, parts, tree.Header, true, send)
		}
		// One scroll holds the whole tree, which keeps where it was
		// scrolled to by the root's id.
		st := v.scroll[tree.Root.ID]
		if st == nil {
			st = &ui.ScrollState{}
			v.scroll[tree.Root.ID] = st
		}
		if tree.Header != nil {
			scrollUnderBar(c, "page", st, parts.card, func(page ui.Element) {
				page.Label(tree.Root.ID).Children(func() {
					v.part(c, parts, tree.Root, true, send)
				})
			})
		} else {
			ui.Scroll(c.Key("page")).TrackScroll(st).MinWidth(0).MinHeight(0).Label(tree.Root.ID).Fill().Children(func() {
				v.part(c, parts, tree.Root, true, send)
			})
		}
	})
	v.hearKeys(c, tree, send)
	v.forget()
	v.prunePictures()
	v.pruneSymbols()
}

// forget drops the state of the parts that the tree no longer has, while
// keeping the pictures, which a plugin asks for again by their source.
func (v *pluginView) forget() {
	if !v.ids[v.focus] {
		v.focus = ""
	}
	for id := range v.scroll {
		if !v.ids[id] {
			delete(v.scroll, id)
		}
	}
	for id := range v.sliders {
		if !v.ids[id] {
			delete(v.sliders, id)
		}
	}
	for id := range v.searches {
		if !v.ids[id] {
			delete(v.searches, id)
		}
	}
	for id := range v.segments {
		if !v.ids[id] {
			delete(v.segments, id)
		}
	}
	for id := range v.marquees {
		if !v.ids[id] {
			delete(v.marquees, id)
		}
	}
	for id := range v.progress {
		if !v.ids[id] {
			delete(v.progress, id)
		}
	}
	for id := range v.markdown {
		if !v.ids[id] {
			delete(v.markdown, id)
		}
	}
}

// pluginKeys maps the names of protocol.KnownKeys to the keys they are.
var pluginKeys = map[string]ui.Key{
	"j": ui.KeyJ, "k": ui.KeyK, "up": ui.KeyUp, "down": ui.KeyDown,
	"left": ui.KeyLeft, "right": ui.KeyRight, "enter": ui.KeyEnter, "escape": ui.KeyEscape,
}

// hearKeys sends the plugin the keys its tree asked for, when the pane may
// hear them: it has the focus, and no field of the tree takes the typing. It
// runs after the parts are drawn, so that a focused control that took a key
// first, such as Enter on a button, has taken it. A key the tree did not ask
// for is never read.
func (v *pluginView) hearKeys(c *ui.Context, tree *protocol.Tree, send func(pluginEvent)) {
	if !v.keysOn || len(tree.Keys) == 0 {
		return
	}
	if _, typing := v.searches[v.focus]; typing && v.ids[v.focus] {
		return
	}
	for _, name := range tree.Keys {
		if key, ok := pluginKeys[name]; ok && c.Shortcut(0, key) {
			v.emit(send, "keys", protocol.KindKey, name)
		}
	}
}

// part draws one part of the tree, and its children. horizontal tells
// that its parent lays its children out in a row, which decides how a
// control that can be wide takes its room.
func (v *pluginView) part(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool, send func(pluginEvent)) {
	if n == nil {
		return
	}
	v.ids[n.ID] = true
	switch n.Type {
	case protocol.NodeRow, protocol.NodeColumn:
		v.box(c, pal, n, send)
	case protocol.NodeText:
		if text, ok, next := pluginClockText(n, c.Now()); ok {
			// A clock: the pane counts it itself between the plugin's
			// renders, and asks for the frame in which it reads the next
			// second.
			clock := *n
			clock.Text = text
			n = &clock
			if next > 0 {
				c.After(next)
			}
		}
		if n.Chip != "" {
			v.chip(c, pal, n)
		} else if (n.Icon != "" || n.Mark != "") && !n.Marquee {
			v.iconText(c, pal, n)
		} else {
			v.text(c, pal, n, horizontal)
		}
	case protocol.NodeImage:
		v.image(c, pal, n)
	case protocol.NodeButton, protocol.NodeIconButton:
		v.button(c, pal, n, horizontal, send)
	case protocol.NodeSlider:
		v.slider(c, pal, n, horizontal, send)
	case protocol.NodeProgress:
		v.progressBar(c, pal, n)
	case protocol.NodeList:
		v.list(c, pal, n, send)
	case protocol.NodeSearchField:
		v.searchField(c, pal, n, send)
	case protocol.NodeSegmented:
		v.segmented(c, pal, n, horizontal, send)
	case protocol.NodeDonut:
		v.donut(c, pal, n)
	case protocol.NodeMenu:
		v.menu(c, pal, n, horizontal, send)
	case protocol.NodeMarkdown:
		v.markdownPart(c, pal, n, send)
	case protocol.NodeEmptyState:
		v.emptyState(c, pal, n, send)
	case protocol.NodeSpacer:
		grow := float32(n.Grow)
		if grow <= 0 {
			grow = 1
		}
		ui.Spacer(c.Key(n.ID)).Grow(grow)
	case protocol.NodeDivider:
		ui.Box(c.Key(n.ID)).Height(1).MinWidth(0).AlignSelf(ui.Stretch).Margin(4, 0).Background(pal.divider)
	default:
		// A part this build does not know, which a later protocol may:
		// it takes no room rather than breaking the pane.
		ui.Box(c.Key(n.ID)).Size(0, 0)
	}
}

// box draws a row or a column and its children. A pressable one is a
// control: it shades under the pointer, takes the focus, and sends a press
// naming it. A row that scrolls sideways keeps its children at their
// widths, and the columns of a board stand as tall as it is.
func (v *pluginView) box(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	// A child of a row that scrolls sideways does not give up its width.
	keep := v.sideways
	v.sideways = false
	row := n.Type == protocol.NodeRow
	var e ui.Element
	switch {
	case n.Pressable:
		e = ui.ButtonBase(c.Key(n.ID))
		// A button centers what it holds; a row or column of a plugin
		// starts it, unless its Align says otherwise.
		if row {
			e.Row().AlignItems(ui.Center).Justify(ui.Start)
		} else {
			e.Column().AlignItems(ui.Stretch).Justify(ui.Start)
		}
		e.Label(pluginPartLabel(n, n.ID))
	case !row && n.ScrollY:
		st := v.scroll[n.ID]
		if st == nil {
			st = &ui.ScrollState{}
			v.scroll[n.ID] = st
		}
		e = ui.Scroll(c.Key(n.ID)).TrackScroll(st).MinHeight(0).AlignItems(ui.Stretch)
		v.flow = true
	case row && stretchesChildren(n):
		e = ui.Row(c.Key(n.ID)).AlignItems(ui.Stretch)
	case row && n.ScrollX:
		st := v.scroll[n.ID]
		if st == nil {
			st = &ui.ScrollState{}
			v.scroll[n.ID] = st
		}
		e = ui.ScrollHorizontal(c.Key(n.ID)).TrackScroll(st).AlignItems(ui.Stretch)
	case row:
		e = ui.Row(c.Key(n.ID)).AlignItems(ui.Center)
	default:
		e = ui.Column(c.Key(n.ID)).AlignItems(ui.Stretch)
	}
	e.Gap(float32(n.Gap)).Padding(float32(n.Padding)).MinWidth(0)
	if n.Width > 0 {
		e.Width(float32(n.Width))
	}
	if n.Height > 0 {
		e.Height(float32(n.Height))
	}
	if keep || n.ScrollY && n.Width > 0 {
		e.Shrink(0)
	}
	pluginAlign(e, n.Align)
	if n.Grow > 0 {
		e.Grow(float32(n.Grow))
	}
	switch {
	case n.Pressable:
		v.bindFocus(e, n.ID)
		fill := ui.Transparent
		if n.Surface == protocol.SurfaceCard {
			pluginCard(e, pal, n)
			fill = pluginCardFill(pal)
		}
		card := n.Surface == protocol.SurfaceCard
		if n.Active {
			// The selected part: a card gets the accent's edge, a row the
			// selection's fill.
			if v.active == nil {
				v.active = map[string]bool{}
			}
			v.active[n.ID] = true
			if !v.wasActive[n.ID] {
				e.ScrollIntoView()
			}
			if card {
				e.InsetShadow(0, 0, 0, 1.5, pal.accent.Alpha(0.85))
			} else {
				fill = pal.rowSelected
			}
		}
		e.Background(e.AnimateColor("bg", pluginPressShade(pal, card, fill, e.Hovered(), e.Pressed()), hover))
		if len(n.ContextMenu) > 0 {
			e.ContextMenu(func(m *ui.Menu) { v.contextEntries(m, n, n.ContextMenu, send) })
		}
	case n.Surface == protocol.SurfaceCard:
		pluginCard(e, pal, n)
	}
	e.Children(func() {
		for _, child := range n.Children {
			v.sideways = row && n.ScrollX
			v.part(c, pal, child, row, send)
		}
		v.sideways = false
	})
	if n.Pressable && e.Clicked() {
		v.emit(send, n.ID, protocol.KindPress, nil)
	}
}

// stretchesChildren tells a row that holds a column that scrolls on its own:
// its children stand as tall as it is, so that each side fills the room.
func stretchesChildren(n *protocol.Node) bool {
	for _, child := range n.Children {
		if child != nil && child.ScrollY {
			return true
		}
	}
	return false
}

// contextEntries builds a context menu from the plugin's entries, and sends
// the one the user chose.
func (v *pluginView) contextEntries(m *ui.Menu, n *protocol.Node, entries []protocol.Item, send func(pluginEvent)) {
	for _, item := range entries {
		switch {
		case item.Separator:
			m.Separator()
		case len(item.Items) > 0:
			m.Submenu(item.Title, func(sub *ui.Menu) { v.contextEntries(sub, n, item.Items, send) })
		default:
			off := item.Enabled != nil && !*item.Enabled
			if m.Item(item.Title).Checked(item.Active).Disabled(off).Chosen() {
				v.emit(send, n.ID, protocol.KindContext, item.ID)
			}
		}
	}
}

// chip draws a text as a chip: its words, and its icon, on a soft fill of
// the chip's color, rounded. The words take the color mixed toward the
// text's, so that they read on the fill in both appearances.
func (v *pluginView) chip(c *ui.Context, pal *palette, n *protocol.Node) {
	fill, fg := pal.chip, pal.textMuted
	if n.Chip != protocol.ChipNeutral && protocol.ValidHex(n.Chip) {
		col := ui.Hex(n.Chip)
		alpha := float32(0.18)
		if pal.dark {
			alpha = 0.26
		}
		fill, fg = col.Alpha(alpha), col.Mix(pal.text, 0.55)
	}
	inner := *n
	inner.ID, inner.Chip, inner.Hex, inner.Width, inner.Grow = "chip", "", "", 0, 0
	if inner.Role == "" {
		inner.Role = protocol.RoleCaption
	}
	inner.Color = ""
	tinted := *pal
	tinted.text, tinted.textMuted = fg, fg
	row := ui.Row(c.Key(n.ID)).AlignItems(ui.Center).Padding(1, 7).Radius(9).Background(fill).MinWidth(0).Shrink(0)
	if n.Label != "" {
		row.Label(n.Label).Tooltip(n.Label)
	}
	if n.Width > 0 {
		row.MaxWidth(float32(n.Width))
	}
	row.Children(func() {
		if (inner.Icon != "" || inner.Mark != "") && !inner.Marquee {
			v.iconText(c, &tinted, &inner)
		} else {
			v.text(c, &tinted, &inner, true)
		}
	})
}

// pluginPressShade is the fill of a pressable row or column: its card's
// fill, or none, and a step toward the text color when the pointer is over
// it, and another when it is held.
func pluginPressShade(pal *palette, card bool, fill ui.Color, hovered, pressed bool) ui.Color {
	if !card {
		switch {
		case pressed:
			return pal.rowSelected
		case hovered:
			return pal.rowHover
		}
		return fill
	}
	switch {
	case pressed:
		return fill.Mix(pal.text, 0.12)
	case hovered:
		return fill.Mix(pal.text, 0.06)
	}
	return fill
}

// pluginAlign places the children of a row or a column along its main
// axis: start, center, end, or spread with space between.
func pluginAlign(e ui.Element, a string) {
	switch a {
	case protocol.AlignCenter:
		e.Justify(ui.Center)
	case protocol.AlignEnd:
		e.Justify(ui.End)
	case protocol.AlignSpaceBetween:
		e.Justify(ui.SpaceBetween)
	}
}

// pluginCard makes a row or column a card: a soft panel, lighter than the
// pane, with a hairline edge in the dark, where a fill alone is faint. It
// has no shadow, so cards stack without noise.
func pluginCard(e ui.Element, pal *palette, n *protocol.Node) {
	radius := float32(10)
	if n.Radius > 0 {
		radius = float32(n.Radius)
	}
	e.Background(pluginCardFill(pal)).Radius(radius)
	if pal.dark {
		e.Border(1, pal.text.Alpha(0.08))
	}
}

// pluginCardFill is the fill of a card.
func pluginCardFill(pal *palette) ui.Color {
	if pal.dark {
		return ui.RGBA(255, 255, 255, 0.06)
	}
	return pal.text.Alpha(0.045)
}

// pluginText is a role's size and weight, and the family it draws in, if
// not the system's.
func pluginText(role string) (size float32, weight int, family string) {
	switch role {
	case protocol.RoleHeadline:
		return 22, 700, ""
	case protocol.RoleTitle:
		return 17, 600, ""
	case protocol.RoleCaption:
		return 11.5, 400, ""
	case protocol.RoleStrong:
		return 13, 600, ""
	case protocol.RoleMono:
		if fontsLoaded {
			return 12, 400, bundledFont
		}
		return 12, 400, "monospace"
	}
	return 13, 400, ""
}

// pluginTextColor is a text part's color: the one it names, or its role's
// own, which fades captions.
func pluginTextColor(pal *palette, n *protocol.Node) ui.Color {
	if n.Hex != "" && protocol.ValidHex(n.Hex) {
		return ui.Hex(n.Hex)
	}
	switch n.Color {
	case protocol.ColorMuted:
		return pal.textMuted
	case protocol.ColorAccent:
		return pal.accent
	case protocol.ColorText:
		return pal.text
	case protocol.ColorDanger:
		return pal.danger
	case protocol.ColorWarning:
		return pal.attention
	case protocol.ColorSuccess:
		if pal.dark {
			return ui.Hex("#3FBF6F")
		}
		return ui.Hex("#2F9E5B")
	}
	if col, ok := pluginHue(pal, n.Color); ok {
		return col
	}
	if n.Role == protocol.RoleCaption {
		return pal.textMuted
	}
	return pal.text
}

// pluginHue is the color of one of the hues a chart tells its slices apart
// by, in the pair for the appearance, and whether name is one. The dark
// ones are lifted a step so that they hold against a dark card.
func pluginHue(pal *palette, name string) (ui.Color, bool) {
	pick := func(light, dark string) (ui.Color, bool) {
		if pal.dark {
			return ui.Hex(dark), true
		}
		return ui.Hex(light), true
	}
	switch name {
	case protocol.ColorOrange:
		return pick("#D97757", "#E58F72")
	case protocol.ColorGreen:
		return pick("#10A37F", "#2BB896")
	case protocol.ColorTeal:
		return pick("#14B8A6", "#2DD4BF")
	case protocol.ColorBlue:
		return pick("#2F6FED", "#5B9BFF")
	case protocol.ColorPurple:
		return pick("#8B5CF6", "#A78BFA")
	case protocol.ColorGray:
		return pal.textMuted, true
	}
	return ui.Transparent, false
}

// iconText draws a text part that has an icon or a program's logo before
// its words, in a row of its own: the icon in the text's color, at the
// size of its words. A text with no words is the icon alone.
func (v *pluginView) iconText(c *ui.Context, pal *palette, n *protocol.Node) {
	size, _, _ := pluginText(n.Role)
	col := pluginTextColor(pal, n)
	row := ui.Row(c.Key(n.ID)).AlignItems(ui.Center).Gap(4).MinWidth(0)
	if n.Label != "" {
		// An icon with no words, or words a longer text explains, is named
		// by its label, for assistive technology and on hover.
		row.Label(n.Label).Tooltip(n.Label)
	}
	if n.Width > 0 {
		row.Width(float32(n.Width))
	}
	if n.Height > 0 {
		row.Height(float32(n.Height))
	}
	if n.Grow > 0 {
		row.Grow(float32(n.Grow))
	}
	row.Children(func() {
		if n.Mark != "" {
			if logo := programIcons[n.Mark]; logo != nil {
				ui.Icon(c.Key("mark"), logo).Size(size, size).TextColor(col).Shrink(0)
			}
		} else if sym, scale := v.symbol(c, n.Icon, float64(size), symbolMedium); sym != nil {
			drawSymbol(c, "symbol", sym, scale, col)
		} else if svg, ok := pluginIcon(n.Icon); ok {
			controlIcon(c.Key("icon"), svg, size, col, pal.dark).Shrink(0)
		}
		if n.Text == "" {
			return
		}
		words := *n
		words.ID, words.Icon, words.Mark, words.Label = "text", "", "", ""
		words.Width, words.Height, words.Grow = 0, 0, 0
		if n.Grow > 0 || n.Width > 0 {
			// The words take the row's room, so that a long text ends in
			// an ellipsis where the row does.
			words.Grow = 1
		}
		v.text(c, pal, &words, true)
	})
}

// image draws a picture part: the picture once it is loaded, at its own
// size, or the one the plugin asks for, rounded as it says.
func (v *pluginView) image(c *ui.Context, pal *palette, n *protocol.Node) {
	bm := v.picture(c, pluginImageSource(n))
	r := float32(n.Radius)
	var w, h float32
	if n.Width > 0 {
		w = float32(n.Width)
	}
	if n.Height > 0 {
		h = float32(n.Height)
	}
	if bm == nil {
		// While it loads, a part of its size, or a small square, holds
		// its place, so the tree does not jump as the picture arrives.
		if w == 0 && h == 0 {
			w, h = 64, 64
		} else if w == 0 {
			w = h
		} else if h == 0 {
			h = w
		}
		ui.Box(c.Key(n.ID)).Size(w, h).Radius(r).Shrink(0).Background(pal.chip)
		return
	}
	pw, ph := bm.Size()
	if w == 0 && h == 0 {
		w, h = peekSize(bm)
	} else if w == 0 {
		w = max(h*float32(pw)/max(float32(ph), 1), 1)
	} else if h == 0 {
		h = max(w*float32(ph)/max(float32(pw), 1), 1)
	}
	ui.Image(c.Key(n.ID), bm).Size(w, h).Fit(ui.Contain).Radius(r).Shrink(0)
}

// button draws a button or an icon button, and sends a press naming it.
func (v *pluginView) button(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool, send func(pluginEvent)) {
	enabled := n.Enabled == nil || *n.Enabled
	label := n.Label
	if label == "" {
		label = n.Icon
	}
	if n.Type == protocol.NodeIconButton {
		icon, ok := pluginIcon(n.Icon)
		// The plain one is a small toolbar button; a sized one is a
		// player's control, as large as the plugin asks, and a primary
		// one a filled disc, as a play button.
		w, h, radius, glyph := float32(28), float32(26), float32(6), float32(16)
		if n.Width > 0 {
			w = float32(n.Width)
			h, radius, glyph = w, w/2, max(16, w*0.44)
		}
		b := ui.ButtonBase(c.Key(n.ID)).Size(w, h).Radius(radius).Center().Shrink(0).Label(label).Tooltip(label)
		if !horizontal || n.Width == 0 {
			b.AlignSelf(ui.Start)
		}
		v.bindFocus(b, n.ID)
		bg, fg := ui.Transparent, pal.textMuted
		if n.Width > 0 {
			fg = pal.text
		}
		if n.Active && !n.Primary {
			// A switched-on toggle, such as shuffle: its glyph takes the
			// accent.
			fg = pal.accent
		}
		switch {
		case n.Primary:
			bg = pal.text
			fg = ui.RGB(18, 18, 18)
			if pluginLuma(bg) < 0.5 {
				fg = ui.RGB(255, 255, 255)
			}
			if b.Pressed() {
				bg = bg.Mix(fg, 0.15)
			}
		case b.Pressed():
			bg = pal.rowHover.Mix(pal.text, 0.06)
		case b.Hovered():
			bg = pal.rowHover
		}
		b.Background(b.AnimateColor("bg", bg, hover)).TextColor(fg)
		if n.Primary {
			// A primary disc grows a touch under the pointer, as
			// Spotify's play button does.
			grow := b.Animate("grow", b2f(b.Hovered() && !b.Pressed()), hover.Duration)
			b.Scale(1+0.05*grow, 1+0.05*grow)
		}
		b.Children(func() {
			weight := symbolMedium
			if n.Primary {
				weight = symbolSemibold
			}
			if sym, scale := v.symbol(c, n.Icon, float64(glyph), weight); sym != nil {
				// The system's own symbol, at its own proportions.
				drawSymbol(c, "symbol", sym, scale, fg)
			} else if ok {
				controlIcon(c, icon, glyph, fg, pal.dark)
			} else {
				// An icon this build does not know: its name, rather than
				// a hole in the bar.
				ui.Text(c, label).FontSize(12).SingleLine()
			}
		})
		if !enabled {
			b.Disabled(true).Opacity(0.45)
		} else if b.Clicked() {
			v.emit(send, n.ID, protocol.KindPress, nil)
		}
		return
	}
	b := ui.ButtonBase(c.Key(n.ID)).Height(30).Padding(0, 14).Gap(8).Radius(15).AlignItems(ui.Center).Shrink(0).Label(label)
	v.bindFocus(b, n.ID)
	bg, fg := pal.control, pal.text
	if n.Primary {
		bg, fg = pal.accent, ui.Hex("#ffffff")
		if pluginLuma(bg) > 0.6 {
			// A light accent, as white on a tint, takes dark words.
			fg = ui.RGB(18, 18, 18)
		}
	}
	switch {
	case !enabled:
	case b.Pressed():
		bg = bg.Mix(ui.Hex("#000000"), 0.08)
	case b.Hovered() && n.Primary:
		bg = bg.Mix(ui.Hex("#ffffff"), 0.08)
	case b.Hovered():
		bg = pal.controlHover
	}
	if n.Primary {
		face(b, pal, b.AnimateColor("bg", bg, press)).TextColor(fg)
	} else {
		controlFace(c, b, pal, b.AnimateColor("bg", bg, press)).TextColor(fg)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine().Ellipsis("…")
	})
	if horizontal && n.Grow > 0 {
		b.Grow(float32(n.Grow))
	} else if !n.Primary {
		b.AlignSelf(ui.Start)
	}
	if !enabled {
		b.Disabled(true).Opacity(0.45)
	} else if b.Clicked() {
		v.emit(send, n.ID, protocol.KindPress, nil)
	}
}

// slider draws a slider, sends a change as the user drags it, and a
// commit as the pointer lets it go.
func (v *pluginView) slider(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool, send func(pluginEvent)) {
	st := v.sliders[n.ID]
	if st == nil {
		st = &pluginSlider{}
		v.sliders[n.ID] = st
	}
	// The plugin's own value wins, unless the user is holding the knob:
	// a value the user set does not snap back under their finger.
	now := c.Now()
	live := n.Rate > 0 && n.At > 0
	if !st.known || !st.pressed && (st.seen != n.Value || st.seenAt != n.At) {
		st.value, st.seen, st.seenAt, st.known = pluginSliderValue(n, now), n.Value, n.At, true
		st.holdUntil = time.Time{}
	} else if live && !st.pressed && !now.Before(st.holdUntil) {
		// The plugin gave a rate, not a frame: the knob runs on by
		// itself, and asks for its own frames.
		st.value = pluginSliderValue(n, now)
	}
	width := float32(n.Width)
	if width <= 0 {
		width = 150
	}
	// A running slider draws from the painter's clock and asks for repaints
	// itself, so it needs no builds. It stands still under the pointer and
	// during the hold, and runs on from the plugin's value after.
	var runOn func(time.Time) (float64, time.Duration)
	if live {
		runOn = func(now time.Time) (float64, time.Duration) {
			switch {
			case st.pressed:
				return st.value, 0
			case now.Before(st.holdUntil):
				return st.value, st.holdUntil.Sub(now)
			}
			st.value = pluginSliderValue(n, now)
			if st.value >= n.Max {
				return st.value, 0
			}
			return st.value, pluginSliderTick(n)
		}
	}
	s := sliderLive(c, n.ID, &st.value, n.Min, n.Max, width, pal, runOn).Label(pluginPartLabel(n, locales.PluginPartSlider()))
	if n.Step > 0 {
		s.Step(n.Step)
	}
	v.bindFocus(s, n.ID)
	if horizontal && n.Width > 0 {
		// A slider with a width of its own, such as a volume, keeps it.
		s.Width(width).MinWidth(width).Shrink(0)
	} else if horizontal {
		s.Grow(1).MinWidth(120)
	} else {
		s.AlignSelf(ui.Stretch).MinWidth(120)
		// In a column a slider with no width of its own spans it.
		if n.Width <= 0 {
			s.FillWidth()
		}
	}
	if n.Enabled != nil && !*n.Enabled {
		s.Disabled(true).Opacity(0.45)
	}
	// Pressed drops once the pointer strays off the thin track mid-drag,
	// which would commit each step of the drag; Dragged holds for as long
	// as the pointer holds the knob.
	_, _, pressed := s.Dragged()
	switch {
	case n.Enabled != nil && !*n.Enabled:
	case s.Changed():
		st.holdUntil = now.Add(pluginSliderHold)
		v.emit(send, n.ID, protocol.KindChange, st.value)
		if !pressed && !st.pressed {
			// A key moved it, not the pointer: each press of an arrow is
			// a value the user settled on, so it commits at once.
			v.emit(send, n.ID, protocol.KindCommit, st.value)
		}
	}
	if st.pressed && !pressed {
		v.emit(send, n.ID, protocol.KindCommit, st.value)
	}
	st.pressed = pressed
}

// pluginPartLabel is what assistive technology calls a part: its label,
// or its text, as the plugin gives them, or the name of its kind.
func pluginPartLabel(n *protocol.Node, kind string) string {
	switch {
	case n.Label != "":
		return n.Label
	case n.Text != "":
		return n.Text
	}
	return kind
}

// pluginProgressValue is the value a progress bar shows: the one the
// plugin sent, moved on by its rate from the instant it sent it, so that
// the bar runs on its own between renders.
func pluginProgressValue(n *protocol.Node, now time.Time) float64 {
	return protocol.AdvanceValue(n.Value, n.Rate, n.At, now.UnixMilli(), 0, 1)
}

// pluginSliderValue is the value a slider runs on to: the one the plugin
// sent, moved on by its rate, within the slider's range.
func pluginSliderValue(n *protocol.Node, now time.Time) float64 {
	return protocol.AdvanceValue(n.Value, n.Rate, n.At, now.UnixMilli(), n.Min, n.Max)
}

// pluginClockText is what a clock text shows: its value in seconds, moved
// on by its rate and capped at its max, as m:ss. ok is false for a text
// that is not a running clock, and next is how long until it reads another
// second, zero once it stopped at its max.
func pluginClockText(n *protocol.Node, now time.Time) (text string, ok bool, next time.Duration) {
	if n.Rate <= 0 || n.At <= 0 {
		return n.Text, false, 0
	}
	hi := math.MaxFloat64
	if n.Max > 0 {
		hi = n.Max
	}
	v := protocol.AdvanceValue(n.Value, n.Rate, n.At, now.UnixMilli(), 0, hi)
	if v < hi {
		// The value reads a new second when it reaches the next whole one,
		// or its max.
		boundary := min(math.Floor(v)+1, hi)
		next = time.Duration((boundary-v)/n.Rate*float64(time.Second)) + time.Millisecond
	}
	return protocol.FormatClock(v), true, next
}

// progressBar draws a progress part: a thin track, the accent up to its
// value, which the pane moves itself from the rate and the instant.
func (v *pluginView) progressBar(c *ui.Context, pal *palette, n *protocol.Node) {
	const h = 6
	v.progress[n.ID] = pluginProgressValue(n, c.Now())
	running := n.Rate > 0 && n.At > 0
	track := pal.text.Alpha(0.12)
	if pal.dark {
		track = ui.RGBA(255, 255, 255, 0.16)
	}
	fill := pal.accent
	if n.Color != "" {
		fill = pluginTextColor(pal, n)
	}
	e := ui.Box(c.Key(n.ID)).Height(h).MinHeight(h).MinWidth(0).AlignSelf(ui.Stretch).Label(pluginPartLabel(n, locales.PluginPartProgress()))
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		// The bar moves with the painter's clock, and asks to be painted
		// again, not to be built again: a running bar costs no view build.
		value := pluginProgressValue(n, p.Now())
		v.progress[n.ID] = value
		if running && value < 1 {
			p.After(pluginProgressTick)
		}
		p.Fill(r, track, h/2)
		if value > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*float32(value), h), H: r.H}, fill, h/2)
		}
		if n.Marker != nil {
			// A tick over the fill, a little taller than the bar.
			x := r.X + r.W*float32(*n.Marker) - 1
			p.Fill(ui.Rect{X: x, Y: r.Y - 2, W: 2, H: r.H + 4}, pal.text.Alpha(0.6), 1)
		}
	})
}

// segmented draws a segmented control: the choices of its items, the
// chosen one raised. A click chooses at once and sends a select that
// names the item; the plugin's own choice wins when it changes.
func (v *pluginView) segmented(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool, send func(pluginEvent)) {
	if len(n.Items) == 0 {
		return
	}
	st := v.segments[n.ID]
	if st == nil {
		st = &pluginSegments{}
		v.segments[n.ID] = st
	}
	chosen, chosenID := -1, ""
	for i, item := range n.Items {
		if item.Active {
			chosen, chosenID = i, item.ID
		}
	}
	if !st.known || chosenID != st.seen {
		if chosen >= 0 {
			st.index = chosen
		}
		st.seen, st.known = chosenID, true
	}
	st.index = min(max(st.index, 0), len(n.Items)-1)
	i := st.index

	track, thumb := ui.RGBA(0, 0, 0, 0.06), ui.Hex("#ffffff")
	if pal.dark {
		track, thumb = ui.RGBA(255, 255, 255, 0.07), ui.RGBA(255, 255, 255, 0.2)
	}
	label := n.Label
	if label == "" {
		label = n.Items[i].Title
	}
	seg := ui.SegmentedBase(c.Key(n.ID), &st.index, len(n.Items))
	seg.Track.Height(24).Padding(2).Radius(12).Background(track).AlignItems(ui.Center).MinWidth(0).Label(label)
	if !horizontal {
		seg.Track.AlignSelf(ui.Stretch)
	}
	seg.Track.Children(func() {
		for k, item := range n.Items {
			if k > 0 {
				// A separator, where neither segment beside it is chosen.
				sep := ui.Box(c).Width(1).Height(12).Shrink(0)
				if k != i && k-1 != i {
					sep.Background(pal.text.Alpha(0.14))
				}
			}
			// The segments share the room alike, but none gives up the width
			// of its own title.
			natural, _ := c.MeasureText(0, ui.Span{Text: item.Title, Size: 12.5})
			s := seg.Segment(k).FillHeight().Padding(0, 12).Radius(10).Center().Grow(1).Shrink(0).MinWidth(natural + 24)
			if k == i {
				controlFace(c, s, pal, thumb)
				if pal.dark {
					s.Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, 0.25))
				} else {
					s.Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.16)).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.04))
				}
			}
			s.Children(func() {
				ui.Text(c, item.Title).FontSize(12.5).SingleLine().TextColor(pal.text)
			})
		}
	})
	if n.Enabled != nil && !*n.Enabled {
		seg.Track.Disabled(true).Opacity(0.45)
		return
	}
	// The choice lands after this runs: take it then.
	seg.Track.OnChange(func() {
		if st.index >= 0 && st.index < len(n.Items) {
			v.emit(send, n.ID, protocol.KindSelect, n.Items[st.index].ID)
		}
	})
}

// menu draws a menu part: a pop-up button that shows its label, with the
// chevron of a pop-up, which opens the items below it. The items that are
// Active are checked, and choosing one sends a select that names it.
func (v *pluginView) menu(c *ui.Context, pal *palette, n *protocol.Node, horizontal bool, send func(pluginEvent)) {
	enabled := n.Enabled == nil || *n.Enabled
	b := ui.ButtonBase(c.Key(n.ID)).Height(26).Padding(0, 8, 0, 9).Gap(6).Radius(13).AlignItems(ui.Center).Shrink(0).Label(n.Label)
	v.bindFocus(b, n.ID)
	bg := pal.control
	switch {
	case !enabled:
	case b.Pressed():
		bg = bg.Mix(ui.Hex("#000000"), 0.08)
	case b.Hovered():
		bg = pal.controlHover
	}
	controlFace(c, b, pal, b.AnimateColor("bg", bg, press)).TextColor(pal.text)
	if !horizontal {
		b.AlignSelf(ui.Start)
	}
	b.Children(func() {
		if n.Icon != "" {
			if sym, scale := v.symbol(c, n.Icon, 13, symbolMedium); sym != nil {
				drawSymbol(c, "symbol", sym, scale, pal.textMuted)
			} else if svg, ok := pluginIcon(n.Icon); ok {
				controlIcon(c.Key("icon"), svg, 13, pal.textMuted, pal.dark).Shrink(0)
			}
		}
		ui.Text(c, n.Label).FontSize(12.5).FontWeight(500).SingleLine().Ellipsis("…")
		if sym, scale := v.symbol(c, "chevron-down", 12, symbolSemibold); sym != nil {
			drawSymbol(c, "chevron", sym, scale, pal.textFaint)
		} else {
			controlIcon(c.Key("chevron"), chevronDownIcon, 9, pal.textFaint, pal.dark).Shrink(0)
		}
	})
	if !enabled {
		b.Disabled(true).Opacity(0.45)
		return
	}
	b.Menu(func(m *ui.Menu) {
		for _, item := range n.Items {
			it := m.Item(item.Title).Checked(item.Active)
			if item.Enabled != nil && !*item.Enabled {
				it.Disabled(true)
			}
			if it.Chosen() {
				v.emit(send, n.ID, protocol.KindSelect, item.ID)
			}
		}
	})
}

// markdownPart draws a markdown part with the app's markdown view, as the
// Files pane reads a markdown file: images show as their descriptions. A
// link opens in the browser if the plugin may open links, and otherwise goes
// to the plugin as a press that carries it.
func (v *pluginView) markdownPart(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	opts := v.markdown[n.ID]
	if opts == nil {
		opts = &markdown.Options{}
		v.markdown[n.ID] = opts
	}
	id := n.ID
	opts.OnLink = func(link string) {
		if v.openLink != nil && pluginSafeLink(link) && v.openLink(link) {
			return
		}
		v.emit(send, id, protocol.KindPress, link)
	}
	ui.Column(c.Key(n.ID)).MinWidth(0).AlignItems(ui.Stretch).Label(markdownLabel(n)).Children(func() {
		markdown.View(c, n.Text, opts)
	})
}

// markdownLabel names a markdown part for assistive technology: its label,
// or its kind, never its source.
func markdownLabel(n *protocol.Node) string {
	if n.Label != "" {
		return n.Label
	}
	return locales.PluginPartMarkdown()
}

// pluginSafeLink tells a link a plugin's markdown may open in the browser:
// a web address with no user info, as remote issue content may open.
func pluginSafeLink(link string) bool {
	return linearSafeURL(link)
}

// donut draws a donut part: a track ring, a slice of it for each segment
// by its share of the total, and words in the middle. The slices start at
// twelve o'clock and run clockwise. With more than one they are sectors with
// flat ends and a hairline between; a ring of no segments, or of values
// that are all zero, is the track alone.
func (v *pluginView) donut(c *ui.Context, pal *palette, n *protocol.Node) {
	d := float32(n.Width)
	if d <= 0 {
		d = 96
	}
	label := n.Label
	if label == "" {
		label = strings.TrimSpace(n.Text + " " + n.Detail)
	}
	track := pal.text.Alpha(0.1)
	if pal.dark {
		track = ui.RGBA(255, 255, 255, 0.12)
	}
	var total float64
	shown := 0
	for _, seg := range n.Segments {
		if seg.Value > 0 {
			total += seg.Value
			shown++
		}
	}
	box := ui.Column(c.Key(n.ID)).Size(d, d).Shrink(0).Center().Label(label)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		w := d * 0.13
		radius := d/2 - w/2
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		v.ringPath(cx, cy, radius)
		p.StrokePath(&v.ring, w, track)
		if total <= 0 {
			return
		}
		col := func(seg protocol.Segment) ui.Color {
			return pluginTextColor(pal, &protocol.Node{Color: seg.Color})
		}
		if shown == 1 {
			// One slice is the whole ring; there are no ends to draw.
			for _, seg := range n.Segments {
				if seg.Value > 0 {
					p.StrokePath(&v.ring, w, col(seg))
				}
			}
			return
		}
		// Each slice is a sector of the ring, filled, so that its ends are
		// flat and a tiny slice is a sliver and not a dot as wide as the
		// ring. The slices part by a hairline of angle; one too thin to see
		// is widened to least, and the largest gives up what that takes, so
		// that the ring still adds up to a full turn.
		gap := pluginDonutGap / float64(radius)
		least := pluginDonutLeast / float64(radius)
		var largest float64
		for _, seg := range n.Segments {
			largest = max(largest, seg.Value)
		}
		var extra float64
		for _, seg := range n.Segments {
			if seg.Value > 0 {
				extra += max(0, least-(2*math.Pi*seg.Value/total-gap))
			}
		}
		at := 0.0
		taken := false
		for _, seg := range n.Segments {
			if seg.Value <= 0 {
				continue
			}
			sweep := 2*math.Pi*seg.Value/total - gap
			if !taken && seg.Value == largest {
				taken = true
				sweep = max(sweep-extra, least)
			} else {
				sweep = max(sweep, least)
			}
			v.sector(cx, cy, radius-w/2, radius+w/2, at+gap/2, at+gap/2+sweep)
			p.FillPath(&v.ring, col(seg))
			at += sweep + gap
		}
	})
	box.Children(func() {
		inner := d * 0.7
		if n.Text != "" {
			// The total keeps to the hole: it steps down from 20 points to
			// 13 until it fits, and ends in an ellipsis only then.
			room := d*0.74 - 8
			size := float32(20)
			for ; size > 13; size-- {
				if w, _ := c.MeasureText(0, ui.Span{Text: n.Text, Size: size, Weight: 600, Features: "tnum"}); w <= room {
					break
				}
			}
			ui.Text(c.Key("text"), n.Text).FontSize(size).FontWeight(600).FontFeatures("tnum").TextColor(pal.text).SingleLine().Ellipsis("…").MaxWidth(room)
		}

		if n.Detail != "" {
			ui.Text(c.Key("detail"), n.Detail).FontSize(11).TextColor(pal.textMuted).SingleLine().Ellipsis("…").MaxWidth(inner)
		}
	})
}

// pluginDonutGap and pluginDonutLeast are the part between two slices of a
// donut, and the least a slice shows, as lengths along the middle of the
// ring in DIPs.
const (
	pluginDonutGap   = 1.75
	pluginDonutLeast = 2
)

// sector sets v.ring to the part of a ring between the radii inner and
// outer, from the angle a0 to a1, in radians clockwise from twelve o'clock.
func (v *pluginView) sector(cx, cy, inner, outer float32, a0, a1 float64) {
	v.ring.Reset()
	x, y := arcPoint(cx, cy, outer, a0)
	v.ring.MoveTo(x, y)
	v.arc(cx, cy, outer, a0, a1)
	x, y = arcPoint(cx, cy, inner, a1)
	v.ring.LineTo(x, y)
	v.arc(cx, cy, inner, a1, a0)
	v.ring.Close()
}

// arcPoint is the point at the angle a on the circle of radius r.
func arcPoint(cx, cy, r float32, a float64) (x, y float32) {
	return cx + r*float32(math.Sin(a)), cy - r*float32(math.Cos(a))
}

// arc adds the arc of the circle from the angle a0 to a1, either way round,
// to v.ring in cubic pieces of at most a quarter turn. The path is already
// at the arc's start.
func (v *pluginView) arc(cx, cy, r float32, a0, a1 float64) {
	pieces := int(math.Ceil(math.Abs(a1-a0) / (math.Pi / 2)))
	if pieces < 1 {
		pieces = 1
	}
	step := (a1 - a0) / float64(pieces)
	k := float32(4.0 / 3.0 * math.Tan(step/4))
	for i := 0; i < pieces; i++ {
		from := a0 + float64(i)*step
		to := from + step
		x0, y0 := arcPoint(cx, cy, r, from)
		x1, y1 := arcPoint(cx, cy, r, to)
		// The tangent of a clockwise turn at the angle a is (cos a, sin a).
		v.ring.CubeTo(
			x0+k*r*float32(math.Cos(from)), y0+k*r*float32(math.Sin(from)),
			x1-k*r*float32(math.Cos(to)), y1-k*r*float32(math.Sin(to)),
			x1, y1)
	}
}

// ringPath sets v.ring to a circle that starts at twelve o'clock and runs
// clockwise, as a donut's slices are measured along it. Path.Circle starts
// at three o'clock, so the circle is built by hand.
func (v *pluginView) ringPath(cx, cy, r float32) {
	const k = 0.5522847498
	v.ring.Reset()
	v.ring.MoveTo(cx, cy-r)
	v.ring.CubeTo(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
	v.ring.CubeTo(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
	v.ring.CubeTo(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
	v.ring.CubeTo(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
	v.ring.Close()
}

// list draws a list part: its rows scroll in the pane, and keep where they
// were scrolled to by the list's id, and a click on a row sends a select
// that names it. In a pane that scrolls as one, the list shows all its
// rows at their height, and the pane scrolls past them.
func (v *pluginView) list(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	var box ui.Element
	if v.flow {
		box = ui.Column(c.Key(n.ID)).MinWidth(0).Label(n.ID)
	} else {
		st := v.scroll[n.ID]
		if st == nil {
			st = &ui.ScrollState{}
			v.scroll[n.ID] = st
		}
		box = ui.Scroll(c.Key(n.ID)).TrackScroll(st).Grow(1).MinHeight(60).MinWidth(0).Label(n.ID)
	}
	box.Children(func() {
		if n.Variant == protocol.VariantGrid {
			ui.Row(c.Key("tiles")).Wrap().MinWidth(0).Gap(pluginGridGap).AlignItems(ui.Start).Children(func() {
				for _, item := range n.Items {
					v.gridTile(c, pal, n, item, send)
				}
			})
			return
		}
		ui.Column(c.Key("rows")).MinWidth(0).Gap(2).Children(func() {
			for _, item := range n.Items {
				v.listRow(c, pal, n, item, send)
			}
		})
	})
}

// pluginGridGap is the space between a grid's tiles, both ways. A plugin
// that fills a row with tiles counts it between them.
const pluginGridGap = 12

// gridTile draws one item of a grid list: its picture, square and as wide
// as the list says, with the title and the subtitle under it.
func (v *pluginView) gridTile(c *ui.Context, pal *palette, n *protocol.Node, item protocol.Item, send func(pluginEvent)) {
	enabled := item.Enabled == nil || *item.Enabled
	w := float32(n.Width)
	if w <= 0 {
		w = 120
	}
	t := ui.Column(c.Key(item.ID)).Width(w).Gap(6).Padding(0).Radius(8).Label(item.Title).Shrink(0)
	shade := ui.Transparent
	switch {
	case !enabled:
	case t.Pressed():
		shade = ui.RGB(0, 0, 0).Alpha(0.18)
	case t.Hovered():
		shade = ui.RGB(255, 255, 255).Alpha(0.08)
	}
	t.Children(func() {
		pic := ui.Box(c.Key("frame")).Size(w, w).Radius(6).Clip().Shrink(0)
		pic.Children(func() {
			var bm *ui.Bitmap
			if item.Image != "" {
				bm = v.picture(c, pluginSource{url: item.Image, side: pluginTileSide(w)})
			}
			if bm != nil {
				ui.Image(c.Key("pic"), bm).Size(w, w).Fit(ui.Cover)
			} else {
				ui.Box(c.Key("pic")).Size(w, w).Background(pal.chip)
			}
			ui.Box(c.Key("shade")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Background(t.AnimateColor("shade", shade, row))
		})
		ui.Text(c.Key("title"), item.Title).FontSize(12.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Width(w)
		if item.Subtitle != "" {
			ui.Text(c.Key("sub"), item.Subtitle).FontSize(11.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Width(w)
		}
	})
	if !enabled {
		t.Opacity(0.45)
	} else if t.Clicked() {
		v.emit(send, n.ID, protocol.KindSelect, item.ID)
	}
}

// listRow draws one row of a list: its picture, title, subtitle and
// trailing text.
func (v *pluginView) listRow(c *ui.Context, pal *palette, n *protocol.Node, item protocol.Item, send func(pluginEvent)) {
	enabled := item.Enabled == nil || *item.Enabled
	h := float32(rowH)
	if item.Subtitle != "" {
		h = 46
	}
	r := ui.Row(c.Key(item.ID)).Height(h).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).Label(item.Title)
	bg := ui.Transparent
	switch {
	case !enabled:
	case r.Pressed():
		bg = pal.rowSelected
	case r.Hovered():
		bg = pal.rowHover
	}
	r.Background(r.AnimateColor("bg", bg, row))
	r.Children(func() {
		// The picture leads the row, inside it: each row's own "pic" key,
		// so that two rows with pictures do not share one.
		if item.Image != "" {
			if bm := v.picture(c, pluginSource{url: item.Image, side: pluginTileSide(28)}); bm != nil {
				ui.Image(c.Key("pic"), bm).Size(28, 28).Fit(ui.Cover).Radius(6).Shrink(0)
			} else {
				ui.Box(c.Key("pic")).Size(28, 28).Radius(6).Shrink(0).Background(pal.chip)
			}
		}
		if item.Leading != "" || item.Active {
			v.listLeading(c, pal, item)
		}
		titleColor, titleWeight := pal.text, 400
		if item.Active {
			titleColor, titleWeight = pal.accent, 600
		}
		ui.Column(c.Key("text")).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c.Key("title"), item.Title).FontSize(13).FontWeight(titleWeight).TextColor(titleColor).SingleLine().Ellipsis("…")
			if item.Subtitle != "" {
				ui.Text(c.Key("sub"), item.Subtitle).FontSize(11.5).TextColor(pal.textMuted).SingleLine().Ellipsis("…")
			}
		})
		if item.Trailing != "" {
			ui.Text(c, item.Trailing).FontSize(11.5).FontFeatures("tnum").TextColor(pal.textFaint).SingleLine().Shrink(0)
		}
	})
	if !enabled {
		r.Opacity(0.45)
	} else if r.Clicked() {
		v.emit(send, n.ID, protocol.KindSelect, item.ID)
	}
}

// listLeading draws the column before a row's picture: its leading text,
// such as a track's number, right-aligned in a fixed width so that the
// titles line up, or the speaker symbol in the accent for the row that is
// on.
func (v *pluginView) listLeading(c *ui.Context, pal *palette, item protocol.Item) {
	col := ui.Row(c.Key("leading")).Width(22).Shrink(0).AlignItems(ui.Center).Justify(ui.End)
	col.Children(func() {
		if item.Active {
			if sym, scale := v.symbol(c, "volume", 12, symbolMedium); sym != nil {
				drawSymbol(c, "symbol", sym, scale, pal.accent)
				return
			}
		}
		color := pal.textFaint
		if item.Active {
			color = pal.accent
		}
		ui.Text(c.Key("number"), item.Leading).FontSize(11.5).FontFeatures("tnum").TextColor(color).SingleLine()
	})
}

// searchField draws a search field, sends a change once the typing pauses,
// and a submit as Return comes.
func (v *pluginView) searchField(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	st := v.searches[n.ID]
	if st == nil {
		// A field that first shows takes the plugin's text, such as a
		// query saved before a restart.
		st = &pluginSearch{query: n.Text, sent: n.Text, given: n.Text}
		v.searches[n.ID] = st
	} else if n.Text != st.given {
		// The plugin sent another text. One that echoes what the field
		// last told it is not new, and must not undo the letters typed
		// since; any other replaces the field's text.
		st.given = n.Text
		if n.Text != st.sent {
			st.query, st.sent = n.Text, n.Text
			st.armed = false
		}
	}
	frame := ui.Row(c.Key(n.ID)).Cursor(ui.CursorText).MinWidth(0)
	var in ui.Element
	frame.Children(func() {
		controlIcon(c, searchIcon, 12, pal.textFaint, pal.dark).Shrink(0)
		in = ui.TextInputBase(c.Key("in"), &st.query).Grow(1).MinWidth(0).FontSize(13).Placeholder(n.Placeholder).Label(n.Placeholder)
	})
	v.bindFocus(in, n.ID)
	field(c, frame, in.Focused(), pal)
	in.OnChange(func() {
		// The change waits for the typing to pause. Ask for the frame
		// after this one, in which the wait is what the field shows, so
		// that the timer is set from a frame of its own.
		st.due = c.Now().Add(pluginSearchDelay)
		st.armed = true
		c.Invalidate()
	})
	in.OnSubmit(func() {
		st.armed = false
		st.sent = st.query
		v.emit(send, n.ID, protocol.KindSubmit, st.query)
	})
	if st.armed {
		if wait := st.due.Sub(c.Now()); wait > 0 {
			c.After(wait)
		} else {
			st.armed = false
			if st.query != st.sent {
				st.sent = st.query
				v.emit(send, n.ID, protocol.KindChange, st.query)
			}
		}
	}
}

// pluginSearchDelay is how long the typing in a search field pauses before
// the pane tells the plugin what it says, so that a long word is one
// change, not one a letter.
const pluginSearchDelay = 250 * time.Millisecond

// emptyState draws an empty state: its title, its detail, and its one
// action, centered as the Tasks pane's is.
func (v *pluginView) emptyState(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	ui.Column(c.Key(n.ID)).Grow(1).MinHeight(140).Center().Gap(6).Padding(24).Children(func() {
		ui.Text(c, n.Title).FontSize(17).FontWeight(600).TextColor(pal.text).TextAlign(ui.Center).Wrap()
		if n.Detail != "" {
			ui.Text(c, n.Detail).FontSize(13).TextColor(pal.textMuted).MaxWidth(380).TextAlign(ui.Center).LineHeight(1.45).Wrap()
		}
		if n.Action != "" {
			if button(c, n.ID+"-action", n.Action, "", true, pal) {
				v.emit(send, n.ID, protocol.KindPress, n.Action)
			}
		}
	})
}

// bindFocus ties the focus of e to the part id: the pane keeps the id the
// user gave the focus to, and gives it back to the part of that id when
// the part is drawn again under a new parent, or a new root, where MyGo
// sees a new control and the focus would be lost with the old one. An id
// this frame already bound, which a valid tree does not repeat, keeps its
// first part.
func (v *pluginView) bindFocus(e ui.Element, id string) {
	if _, ok := v.controls[id]; ok {
		return
	}
	v.controls[id] = e.ID()
	if last, ok := v.lastControls[id]; ok && last != e.ID() && v.focus == id {
		e.Focus()
	}
	e.FocusBind(&v.focus, id)
}

// emit sends an event, if the pane has somewhere to send it.
func (v *pluginView) emit(send func(pluginEvent), node, kind string, value any) {
	if send != nil {
		send(pluginEvent{Node: node, Kind: kind, Value: value})
	}
}

// pluginImageSource is the source of an image part.
func pluginImageSource(n *protocol.Node) pluginSource {
	src := pluginSource{url: n.URL, data: n.Data, tint: n.Tint}
	if px := 2 * max(n.Width, n.Height); px > 0 {
		src.side = pluginSideBucket(px, pluginMaxArtSide)
	}
	return src
}

// pluginSourceKey names a picture's source in the pane's cache.
func pluginSourceKey(src pluginSource) string {
	key := src.url + "\x00" + src.data
	if src.tint {
		key += "\x00tint"
	}
	return key
}

// picture returns the picture of src, starting its fetch the first time
// the tree asks for it, and nil until it arrives. A fetch that failed is
// tried again, a few times, after a pause.
func (v *pluginView) picture(c *ui.Context, src pluginSource) *ui.Bitmap {
	key := pluginSourceKey(src)
	now := c.Now()
	v.mu.Lock()
	pic := v.pictures[key]
	if pic == nil {
		pic = &pluginPicture{src: src}
		v.pictures[key] = pic
	}
	pic.used = v.frame
	var wait time.Duration
	switch {
	case pic.bm != nil || pic.loading || pic.tries >= pluginPictureTries:
	case now.Before(pic.retryAt):
		wait = pic.retryAt.Sub(now)
	default:
		pic.loading, pic.failed = true, false
		pic.tries++
		v.startPicture(key, pic, now)
	}
	bm := pic.bm
	v.mu.Unlock()
	if wait > 0 {
		// Ask for the frame in which the picture may be tried again.
		c.After(wait)
	}
	return bm
}

// startPicture loads a picture off the UI goroutine and puts it in the
// cache, if it is still the cache's entry when it arrives. It is called
// with v.mu held.
func (v *pluginView) startPicture(key string, pic *pluginPicture, started time.Time) {
	fetch, invalidate := v.fetch, v.invalidate
	go func() {
		bm, img, err := fetch(pic.src)
		var tint ui.Color
		tinted := err == nil && img != nil && pic.src.tint
		if tinted {
			// The color of a picture the pane is tinted from is read
			// here, off the UI goroutine, once for this picture.
			tint = pluginDominantColor(img)
		}
		v.mu.Lock()
		if v.pictures[key] == pic {
			pic.loading = false
			if err != nil {
				pic.failed = true
				pic.retryAt = started.Add(pluginPictureRetry)
			} else {
				pic.bm, pic.tint, pic.tinted = bm, tint, tinted
				pic.failed = false
			}
		}
		v.mu.Unlock()
		if invalidate != nil {
			invalidate()
		}
	}()
}

// prunePictures drops the pictures the tree no longer asks for, the
// longest unasked first, while the pane keeps more than its budget. A
// picture this draw asked for stays. One still on its way that nothing
// asks for goes too, and its result is dropped when it arrives.
func (v *pluginView) prunePictures() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for len(v.pictures) > pluginPictureBudget {
		oldestKey, oldest := "", uint64(0)
		for key, pic := range v.pictures {
			if pic.used == v.frame {
				continue
			}
			if oldestKey == "" || pic.used < oldest {
				oldestKey, oldest = key, pic.used
			}
		}
		if oldestKey == "" {
			break
		}
		delete(v.pictures, oldestKey)
	}
}

// pictureTint returns the color the picture was read for, once it is
// loaded, and whether the picture is still on its way: a picture the pane
// has not asked for yet, one that loads, and one that failed but will be
// tried again all have no color yet, which is not the same as none.
func (v *pluginView) pictureTint(src pluginSource) (tint ui.Color, ok, pending bool) {
	key := pluginSourceKey(src)
	v.mu.Lock()
	defer v.mu.Unlock()
	pic := v.pictures[key]
	switch {
	case pic == nil || pic.loading:
		return ui.Transparent, false, true
	case pic.tinted:
		return pic.tint, true, false
	case pic.bm == nil && pic.tries < pluginPictureTries:
		return ui.Transparent, false, true
	}
	return ui.Transparent, false, false
}

// pluginMaxImageBytes keeps a huge picture from being fetched whole, and
// pluginMaxImagePixels from being decoded whole: a small file can declare
// a picture of many gigabytes.
const (
	pluginMaxImageBytes  = 32 << 20
	pluginMaxImagePixels = 4096 * 4096
	// pluginMaxImageSide is the longest side that a decoded picture keeps
	// by default, and pluginMaxArtSide the longest an image part with a size
	// of its own may ask for. A cover of 200 points is sharp on a Retina
	// screen at 400 pixels, and a larger one only costs memory: about 50
	// tiles at 512 pixels are 50 MB.
	pluginMaxImageSide = 512
	pluginMaxArtSide   = 1024
)

// pluginSideBucket rounds a size in pixels up to 128, 256, 512 or 1024, at
// most max, so that a pane that resizes by a pixel asks for the same size.
func pluginSideBucket(px, max int) int {
	for _, b := range []int{128, 256, 512, 1024} {
		if px <= b || b >= max {
			return min(b, max)
		}
	}
	return max
}

// pluginTileSide is the longest side, in pixels, to keep a picture at that
// is drawn points wide: twice the points, in a bucket, at most
// pluginMaxImageSide.
func pluginTileSide(points float32) int {
	return pluginSideBucket(int(points*2+0.5), pluginMaxImageSide)
}

// pluginDownscale returns img with its longest side at most side (and at
// most pluginMaxImageSide; zero is the latter), in the same proportions,
// or img itself when it is small enough.
func pluginDownscale(img image.Image, side int) image.Image {
	if side <= 0 {
		side = pluginMaxImageSide
	}
	side = min(side, pluginMaxArtSide)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return img
	}
	if w >= h {
		h = max(1, h*side/w)
		w = side
	} else {
		w = max(1, w*side/h)
		h = side
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}

// pluginFetch loads a picture off the UI goroutine: a data: URI's bytes,
// or a URL's, decoded once, to an image for its colors and a bitmap of it
// for the pane, with its longest side at most pluginMaxImageSide. Its size is read from its header first, so a picture too
// large to decode is turned away before it takes the memory.
func pluginFetch(src pluginSource) (*ui.Bitmap, image.Image, error) {
	data, err := pluginPictureBytes(src)
	if err != nil {
		return nil, nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > pluginMaxImagePixels {
		return nil, nil, fmt.Errorf("plugin image: %dx%d is larger than %d pixels", cfg.Width, cfg.Height, pluginMaxImagePixels)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	img = pluginDownscale(img, src.side)
	return ui.NewBitmap(img), img, nil
}

// pluginPictureBytes is the bytes of a picture: a data: URI's, or a URL's
// over the network.
func pluginPictureBytes(src pluginSource) ([]byte, error) {
	if src.data != "" {
		return pluginDataBytes(src.data)
	}
	if strings.HasPrefix(src.url, "data:") {
		return pluginDataBytes(src.url)
	}
	if src.url == "" {
		return nil, errors.New("plugin image: no url or data")
	}
	req, err := http.NewRequest(http.MethodGet, src.url, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("plugin image: " + resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, pluginMaxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > pluginMaxImageBytes {
		return nil, errors.New("plugin image: larger than 32 MB")
	}
	return data, nil
}

// pluginDataBytes reads a data: URI, base64 or percent encoded.
func pluginDataBytes(s string) ([]byte, error) {
	comma := strings.Index(s, ",")
	if comma < 0 {
		return nil, errors.New("plugin image: a data: URI needs a comma")
	}
	meta, body := s[:comma], s[comma+1:]
	if strings.Contains(meta, ";base64") {
		return base64.StdEncoding.DecodeString(body)
	}
	plain, err := url.PathUnescape(body)
	if err != nil {
		return nil, err
	}
	return []byte(plain), nil
}

// pluginIcon is the app's icon of one of the protocol's names, and
// whether the set has one.
func pluginIcon(name string) (*ui.SVG, bool) {
	svg, ok := pluginIcons[name]
	return svg, ok
}

// pluginIcons maps the protocol's fixed icon set to the app's own.
var pluginIcons = map[string]*ui.SVG{
	"play":          icon(`<path d="M5.5 3.5v9l7-4.5z"/>`),
	"pause":         icon(`<path d="M5.5 3.5v9M10.5 3.5v9"/>`),
	"next":          icon(`<path d="M5 3.5l6 4.5-6 4.5M12 3.5v9"/>`),
	"previous":      icon(`<path d="M11 3.5l-6 4.5 6 4.5M4 3.5v9"/>`),
	"search":        glyph("f7-search"),
	"close":         glyph("f7-xmark"),
	"refresh":       glyph("f7-arrow-clockwise"),
	"plus":          glyph("f7-plus"),
	"chevron-right": glyph("f7-chevron-right"),
	"chevron-up":    glyph("f7-chevron-up"),
	"chevron-down":  glyph("f7-chevron-down"),
	"folder":        glyph("f7-folder"),
	"external":      glyph("f7-arrow-up-right-square"),
	"copy":          glyph("f7-doc-on-doc"),
	"link":          glyph("f7-link"),
	"gear":          glyph("f7-gear-alt"),
	"files":         glyph("f7-folder-fill"),
	"globe":         glyph("f7-globe"),
	"terminal":      glyph("meteor-terminal"),
	"image":         glyph("f7-photo"),
	"back":          glyph("f7-arrow-left"),
	"forward":       glyph("f7-arrow-right"),
	"preview":       glyph("f7-eye"),
	"source":        icon(`<path d="M6 4L2 8l4 4M10 4l4 4-4 4"/>`),
	"doc":           glyph("f7-doc"),
	"list":          glyph("f7-list-bullet"),
	"board":         glyph("f7-rectangle-split-3x1"),
	"flame":         icon(`<path d="M8 1.8c.4 2 2.6 3.2 2.6 5.6 0 .9-.3 1.5-.7 2 .1-.8-.3-1.6-.9-2-.3 1-1 1.5-1.4 2.2-.5.8-.3 1.6.2 2.2-1.6-.3-2.8-1.6-2.8-3.4 0-2 1.7-3 2.3-4.6z"/>`),
	"dot":           icon(`<circle cx="8" cy="8" r="3.5" fill="currentColor"/>`),
	"warning":       icon(`<path d="M8 2.5l6 10.5H2zM8 6.5v3M8 11.2v.1"/>`),
	"info":          icon(`<circle cx="8" cy="8" r="6"/><path d="M8 7.2v3.6M8 5.2v.1"/>`),

	// The SVG stand-ins of the icons the issue trackers use, drawn only
	// where the system has no symbol (see pluginSymbols).
	"circle":          icon(`<circle cx="8" cy="8" r="5.5"/>`),
	"circle-dashed":   icon(`<circle cx="8" cy="8" r="5.5" stroke-dasharray="2.1 2.1"/>`),
	"circle-half":     icon(`<circle cx="8" cy="8" r="5.5"/><path d="M8 4.5a3.5 3.5 0 0 0 0 7z" fill="currentColor"/>`),
	"check-circle":    icon(`<circle cx="8" cy="8" r="5.5"/><path d="M5.6 8.2l1.7 1.7 3.1-3.5"/>`),
	"xmark-circle":    icon(`<circle cx="8" cy="8" r="5.5"/><path d="M6 6l4 4M10 6l-4 4"/>`),
	"filter":          icon(`<circle cx="8" cy="8" r="5.5"/><path d="M5.2 6.3h5.6M6.3 8.2h3.4M7.2 10.1h1.6"/>`),
	"person":          icon(`<circle cx="8" cy="8" r="5.5"/><circle cx="8" cy="6.6" r="1.7"/><path d="M4.9 11.8c.7-1.4 1.8-2 3.1-2s2.4.6 3.1 2"/>`),
	"tag":             icon(`<path d="M2.5 8.2V3.5a1 1 0 0 1 1-1h4.7l5.3 5.3-5.2 5.2z"/><circle cx="5.6" cy="5.6" r=".7" fill="currentColor"/>`),
	"flag":            icon(`<path d="M4 14V2.5M4 3.2h8l-2 3 2 3H4"/>`),
	"pin":             icon(`<path d="M6 2.5h4l-.6 4 2.1 2.2H4.5l2.1-2.2zM8 8.7V14"/>`),
	"priority-urgent": icon(`<rect x="2.5" y="2.5" width="11" height="11" rx="2.5"/><path d="M8 5.2v3.8M8 11v.1"/>`),
	"priority-high":   icon(`<path d="M4 8l4-4 4 4M4 12.5l4-4 4 4"/>`),
	"priority-medium": icon(`<path d="M4 6.5h8M4 9.5h8"/>`),
	"priority-low":    icon(`<path d="M4 6l4 4 4-4"/>`),
	"milestone":       glyph("f7-suit-diamond"),
	"blocked":         icon(`<circle cx="8" cy="8" r="5.5"/><path d="M4.1 11.9l7.8-7.8"/>`),
	"blocking":        glyph("f7-arrow-turn-down-right"),
	"subissue":        glyph("f7-list-bullet-indent"),
}
