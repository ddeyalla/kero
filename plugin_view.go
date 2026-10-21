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
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
)

// pluginEvent is what a part of the tree sends the plugin: the id of the
// part, the kind of event, and its value, if it has one.
type pluginEvent struct {
	Node  string
	Kind  string
	Value any
}

// pluginSource is where a picture comes from: a URL to fetch, or a data:
// URI to read.
type pluginSource struct {
	url  string
	data string
}

// pluginPicture is one picture of the tree, as the pane shows it: nil
// while it loads, or when it could not be loaded.
type pluginPicture struct {
	src     pluginSource
	loading bool
	bm      *ui.Bitmap
	failed  bool
}

// pluginSlider is a slider's value while the pane shows it: the value the
// user set, which the plugin's own comes back to.
type pluginSlider struct {
	value   float64
	seen    float64 // the value the plugin last sent
	known   bool    // seen is set
	pressed bool    // the pointer holds the knob
}

// pluginSearch is a search field's text, and the change it owes the
// plugin, which waits for the typing to pause.
type pluginSearch struct {
	query string
	sent  string    // the text the plugin last heard
	due   time.Time // when the change is due
	armed bool
}

// pluginView is what a plugin pane keeps between renders: the focus, the
// scrolling and the controls' values by part id, and the pictures, which it
// fetches and decodes off the UI goroutine. A pane keeps one pluginView.
type pluginView struct {
	mu       sync.Mutex
	focus    string
	scroll   map[string]*ui.ScrollState
	sliders  map[string]*pluginSlider
	searches map[string]*pluginSearch
	progress map[string]float64 // the value each bar shows this frame
	pictures map[string]*pluginPicture

	// fetch loads a picture, off the UI goroutine; tests replace it. The
	// default reads a data: URI, or fetches a URL.
	fetch func(pluginSource) (*ui.Bitmap, image.Image, error)
	// invalidate asks for a frame, once a picture arrives.
	invalidate func()

	ids map[string]bool // the parts drawn this frame
}

// drawPluginTree draws tree for one frame in the pane pal belongs to,
// sending what the user does to the plugin through send. A pane with one
// plugin uses this; a pane with several keeps a pluginView for each and
// calls its draw, so that their focus and pictures stay apart.
func drawPluginTree(c *ui.Context, pal *palette, tree *protocol.Tree, send func(pluginEvent)) {
	defaultPluginView.draw(c, pal, tree, send)
}

// defaultPluginView is the state of the one plugin pane drawPluginTree
// serves.
var defaultPluginView = newPluginView()

// newPluginView returns the state of a plugin pane, empty.
func newPluginView() *pluginView {
	return &pluginView{
		scroll:   map[string]*ui.ScrollState{},
		sliders:  map[string]*pluginSlider{},
		searches: map[string]*pluginSearch{},
		progress: map[string]float64{},
		pictures: map[string]*pluginPicture{},
		fetch:    pluginFetch,
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
	// The root's own key holds the pane's place: a tree of the same root
	// id keeps its controls, and their state, from one render to the next.
	ui.Column(c.Key("plugin-" + tree.Root.ID)).Fill().MinWidth(0).MinHeight(0).Children(func() {
		v.part(c, pal, tree.Root, true, send)
	})
	v.forget()
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
	for id := range v.progress {
		if !v.ids[id] {
			delete(v.progress, id)
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
		var e ui.Element
		if n.Type == protocol.NodeRow {
			e = ui.Row(c.Key(n.ID)).AlignItems(ui.Center)
		} else {
			e = ui.Column(c.Key(n.ID)).AlignItems(ui.Stretch)
		}
		e.Gap(float32(n.Gap)).Padding(float32(n.Padding)).MinWidth(0)
		pluginAlign(e, n.Align)
		if n.Grow > 0 {
			e.Grow(float32(n.Grow))
		}
		row := n.Type == protocol.NodeRow
		e.Children(func() {
			for _, child := range n.Children {
				v.part(c, pal, child, row, send)
			}
		})
	case protocol.NodeText:
		v.text(c, pal, n)
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

// pluginText is a role's size and weight, and the family it draws in, if
// not the system's.
func pluginText(role string) (size float32, weight int, family string) {
	switch role {
	case protocol.RoleTitle:
		return 17, 600, ""
	case protocol.RoleCaption:
		return 11.5, 400, ""
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
	switch n.Color {
	case protocol.ColorMuted:
		return pal.textMuted
	case protocol.ColorAccent:
		return pal.accent
	case protocol.ColorText:
		return pal.text
	}
	if n.Role == protocol.RoleCaption {
		return pal.textMuted
	}
	return pal.text
}

// text draws a text part: its role's size and weight, its color, and its
// lines, as many as it takes unless the plugin limits them.
func (v *pluginView) text(c *ui.Context, pal *palette, n *protocol.Node) {
	size, weight, family := pluginText(n.Role)
	e := ui.Text(c.Key(n.ID), n.Text).FontSize(size).FontWeight(weight).TextColor(pluginTextColor(pal, n)).MinWidth(0)
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

// image draws a picture part: the picture once it is loaded, at its own
// size, or the one the plugin asks for, rounded as it says.
func (v *pluginView) image(c *ui.Context, pal *palette, n *protocol.Node) {
	bm := v.picture(pluginSource{url: n.URL, data: n.Data})
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
		b := ui.ButtonBase(c.Key(n.ID)).Size(28, 26).Radius(6).Center().Shrink(0).Label(label).Tooltip(label)
		b.AlignSelf(ui.Start)
		bg := ui.Transparent
		switch {
		case b.Pressed():
			bg = pal.rowHover.Mix(pal.text, 0.06)
		case b.Hovered():
			bg = pal.rowHover
		}
		b.Background(b.AnimateColor("bg", bg, hover)).TextColor(pal.textMuted)
		b.Children(func() {
			if ok {
				ui.Icon(c, icon).Size(16, 16)
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
	b := ui.ButtonBase(c.Key(n.ID)).Height(30).Padding(0, 14).Gap(8).Radius(8).AlignItems(ui.Center).Shrink(0).Label(label)
	bg, fg := pal.control, pal.text
	if n.Primary {
		bg, fg = pal.accent, ui.Hex("#ffffff")
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
	face(b, pal, b.AnimateColor("bg", bg, press)).TextColor(fg)
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
	if !st.known || !st.pressed && st.seen != n.Value {
		st.value, st.seen, st.known = n.Value, n.Value, true
	}
	width := float32(n.Width)
	if width <= 0 {
		width = 150
	}
	s := slider(c, n.ID, &st.value, n.Min, n.Max, width, pal).Label(n.ID)
	if horizontal {
		s.Grow(1).MinWidth(120)
	} else {
		s.AlignSelf(ui.Stretch).MinWidth(120)
	}
	if n.Enabled != nil && !*n.Enabled {
		s.Disabled(true).Opacity(0.45)
	}
	pressed := s.Pressed()
	switch {
	case n.Enabled != nil && !*n.Enabled:
	case s.Changed():
		v.emit(send, n.ID, protocol.KindChange, st.value)
	}
	if st.pressed && !pressed {
		v.emit(send, n.ID, protocol.KindCommit, st.value)
	}
	st.pressed = pressed
}

// pluginProgressValue is the value a progress bar shows: the one the
// plugin sent, moved on by its rate from the instant it sent it, so that
// the bar runs on its own between renders.
func pluginProgressValue(n *protocol.Node, now time.Time) float64 {
	v := n.Value
	if n.Rate > 0 && n.At > 0 {
		v += n.Rate * float64(now.UnixMilli()-n.At) / 1000
	}
	return min(max(v, 0), 1)
}

// progressBar draws a progress part: a thin track, the accent up to its
// value, which the pane moves itself from the rate and the instant.
func (v *pluginView) progressBar(c *ui.Context, pal *palette, n *protocol.Node) {
	const h = 6
	value := pluginProgressValue(n, c.Now())
	v.progress[n.ID] = value
	track := pal.text.Alpha(0.12)
	if pal.dark {
		track = ui.RGBA(255, 255, 255, 0.16)
	}
	e := ui.Box(c.Key(n.ID)).Height(h).MinHeight(h).MinWidth(0).AlignSelf(ui.Stretch).Label(n.ID)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, track, h/2)
		if value > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*float32(value), h), H: r.H}, pal.accent, h/2)
		}
	})
}

// list draws a list part: its rows scroll in the pane, and keep where they
// were scrolled to by the list's id, and a click on a row sends a select
// that names it.
func (v *pluginView) list(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	st := v.scroll[n.ID]
	if st == nil {
		st = &ui.ScrollState{}
		v.scroll[n.ID] = st
	}
	box := ui.Scroll(c.Key(n.ID)).TrackScroll(st).Grow(1).MinHeight(60).MinWidth(0).Label(n.ID)
	box.Children(func() {
		ui.Column(c.Key("rows")).MinWidth(0).Gap(2).Children(func() {
			for _, item := range n.Items {
				v.listRow(c, pal, n, item, send)
			}
		})
	})
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
	if item.Image != "" {
		if bm := v.picture(pluginSource{url: item.Image}); bm != nil {
			ui.Image(c.Key("pic"), bm).Size(28, 28).Fit(ui.Cover).Radius(6).Shrink(0)
		} else {
			ui.Box(c.Key("pic")).Size(28, 28).Radius(6).Shrink(0).Background(pal.chip)
		}
	}
	r.Children(func() {
		ui.Column(c.Key("text")).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c.Key("title"), item.Title).FontSize(13).TextColor(pal.text).SingleLine().Ellipsis("…")
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

// searchField draws a search field, sends a change once the typing pauses,
// and a submit as Return comes.
func (v *pluginView) searchField(c *ui.Context, pal *palette, n *protocol.Node, send func(pluginEvent)) {
	st := v.searches[n.ID]
	if st == nil {
		st = &pluginSearch{}
		v.searches[n.ID] = st
	}
	frame := ui.Row(c.Key(n.ID)).Cursor(ui.CursorText).MinWidth(0)
	var in ui.Element
	frame.Children(func() {
		ui.Icon(c, searchIcon).Size(12, 12).TextColor(pal.textFaint).Shrink(0)
		in = ui.TextInputBase(c.Key("in"), &st.query).Grow(1).MinWidth(0).FontSize(13).Placeholder(n.Placeholder).Label(n.Placeholder)
	})
	field(c, frame, in.Focused(), pal)
	if in.Focused() {
		v.focus = n.ID
	}
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

// emit sends an event, if the pane has somewhere to send it.
func (v *pluginView) emit(send func(pluginEvent), node, kind string, value any) {
	if send != nil {
		send(pluginEvent{Node: node, Kind: kind, Value: value})
	}
}

// picture returns the picture of src, starting its fetch the first time
// the tree asks for it, and nil until it arrives.
func (v *pluginView) picture(src pluginSource) *ui.Bitmap {
	key := src.url + "\x00" + src.data
	v.mu.Lock()
	pic := v.pictures[key]
	if pic == nil {
		pic = &pluginPicture{src: src, loading: true}
		v.pictures[key] = pic
		fetch, invalidate := v.fetch, v.invalidate
		go func() {
			bm, _, err := fetch(src)
			v.mu.Lock()
			pic.bm, pic.failed, pic.loading = bm, err != nil, false
			v.mu.Unlock()
			if invalidate != nil {
				invalidate()
			}
		}()
	}
	bm := pic.bm
	v.mu.Unlock()
	return bm
}

// pluginMaxImageBytes keeps a huge picture from being fetched whole.
const pluginMaxImageBytes = 32 << 20

// pluginFetch loads a picture off the UI goroutine: a data: URI's bytes,
// or a URL's, decoded to a bitmap for the pane and to an image for its
// colors.
func pluginFetch(src pluginSource) (*ui.Bitmap, image.Image, error) {
	data, err := pluginPictureBytes(src)
	if err != nil {
		return nil, nil, err
	}
	bm, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil, nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// The picture shows; it just has no color the pane can sample.
		return bm, nil, nil
	}
	return bm, img, nil
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
	plain, err := url.QueryUnescape(body)
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
}
