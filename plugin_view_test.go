package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
)

// pluginHarness draws plugin trees in a tester window with the pane's
// state, as one pane does: tests swap the tree for the next render.
type pluginHarness struct {
	tt   *ui.Tester
	view *pluginView
	tree *protocol.Tree
	pal  palette
}

// newPluginHarness starts a pane showing tree. It stubs the picture
// loader, so that no test touches the network.
func newPluginHarness(t *testing.T, tree *protocol.Tree, send func(pluginEvent)) *pluginHarness {
	t.Helper()
	defaultPluginView = newPluginView()
	defaultPluginView.fetch = stubPluginFetch()
	h := &pluginHarness{view: defaultPluginView, tree: tree, pal: lightPalette}
	h.tt = ui.NewTester(func(c *ui.Context) {
		drawPluginTree(c, &h.pal, h.tree, send)
	}, 900, 700)
	h.frames()
	return h
}

// frames renders twice with time between, as pane tests do.
func (h *pluginHarness) frames() {
	h.tt.Frame()
	h.tt.Advance(time.Second)
	h.tt.Frame()
}

// show renders tree in the pane.
func (h *pluginHarness) show(tree *protocol.Tree) {
	h.tree = tree
	h.frames()
}

// stubPluginFetch returns a four-pixel bitmap, at once.
func stubPluginFetch() func(pluginSource) (*ui.Bitmap, image.Image, error) {
	return func(pluginSource) (*ui.Bitmap, image.Image, error) {
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for i := range img.Pix {
			img.Pix[i] = 0xc0
		}
		return ui.NewBitmap(img), img, nil
	}
}

// pluginEveryPart is a tree with one of each part of the first protocol.
func pluginEveryPart() *protocol.Tree {
	return pluginTree(sdk.Column("root",
		sdk.Text("title", "A plugin pane").Role("title"),
		sdk.Text("body", "It draws the parts."),
		sdk.Text("caption", "a caption").Role("caption").Color("muted"),
		sdk.Text("mono", "go test ./...").Role("mono"),
		sdk.Row("bar",
			sdk.Text("bar-label", "In a row"),
			sdk.Button("go", "Start").Primary(),
			sdk.IconButton("play-button", "play"),
			sdk.Spacer("bar-space"),
		).Gap(8).Padding(6),
		sdk.Image("picture", "data:image/png;base64,AAAA").Size(80, 60).Radius(5),
		sdk.Text("picture-note", "a picture").Role("caption"),
		sdk.Slider("volume", 0.5, 0, 1).Size(160, 0).Step(0.1),
		sdk.Progress("download", 0.25),
		sdk.List("files",
			protocol.Item{ID: "r1", Title: "Row one", Subtitle: "first", Trailing: "12 KB"},
			protocol.Item{ID: "r2", Title: "Row two"},
		),
		sdk.SearchField("search", "Search files"),
		sdk.EmptyState("empty", "Nothing here", "A detail line").Action("Create"),
		sdk.Divider("divider"),
	))
}

// pluginTree builds a tree from a plugin's parts.
func pluginTree(n *sdk.Node) *protocol.Tree {
	tree := n.Tree()
	return &tree
}

// TestPluginPartsDraw draws one of every part in both appearances: each
// shows its text.
func TestPluginPartsDraw(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h := newPluginHarness(t, pluginEveryPart(), nil)
		if dark {
			h.pal = darkPalette
			h.frames()
		}
		for _, text := range []string{
			"A plugin pane", "It draws the parts.", "a caption", "go test ./...",
			"In a row", "Start", "a picture", "Row one", "Row two", "Nothing here",
			"A detail line", "Create",
		} {
			if !h.tt.HasText(text) {
				t.Fatalf("dark %v: the tree does not show %q; it shows %q", dark, text, h.tt.Texts())
			}
		}
		for _, label := range []string{"volume", "files", "Search files"} {
			if _, ok := h.tt.Find(label); !ok {
				t.Fatalf("dark %v: no part is labeled %q", dark, label)
			}
		}
	}
}

// TestPluginPartEvents sends one event of every kind, each naming its part.
func TestPluginPartEvents(t *testing.T) {
	var mu sync.Mutex
	var got []pluginEvent
	send := func(ev pluginEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}
	events := func() []pluginEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]pluginEvent(nil), got...)
	}
	h := newPluginHarness(t, pluginEveryPart(), send)
	tt := h.tt

	if err := tt.Click("Start"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(events(), "go", protocol.KindPress) {
		t.Fatalf("no press for the button: %v", events())
	}

	if err := tt.Click("play"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(events(), "play-button", protocol.KindPress) {
		t.Fatalf("no press for the icon button: %v", events())
	}

	r, ok := tt.Find("volume")
	if !ok {
		t.Fatal("no slider")
	}
	y := r.Y + r.H/2
	tt.Press(r.X+r.W*0.2, y)
	tt.Move(r.X+r.W*0.8, y)
	tt.Move(r.X+r.W*0.85, y)
	tt.Release(r.X+r.W*0.85, y)
	tt.Frame()
	change, commit := -1, -1
	for i, ev := range events() {
		if ev.Node != "volume" {
			continue
		}
		if ev.Kind == protocol.KindChange && change < 0 {
			change = i
		}
		if ev.Kind == protocol.KindCommit {
			commit = i
		}
	}
	if change < 0 || commit < 0 {
		t.Fatalf("no change and commit for the slider: %v", events())
	}
	if commit < change {
		t.Fatalf("the slider commits before it changes: %v", events())
	}
	if v, ok := events()[commit].Value.(float64); !ok || v < 0.6 {
		t.Fatalf("the slider commits %v, want the value the user dragged to", events()[commit].Value)
	}

	if err := tt.Click("Row one"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(events(), "files", protocol.KindSelect, "r1") {
		t.Fatalf("no select for the list row: %v", events())
	}

	if err := tt.Click("Search files"); err != nil {
		t.Fatal(err)
	}
	tt.Type("abc")
	tt.Advance(pluginSearchDelay + 50*time.Millisecond)
	if !hasPluginEvent(events(), "search", protocol.KindChange, "abc") {
		t.Fatalf("no change for the search field: %v", events())
	}
	tt.Key(0, ui.KeyEnter)
	if !hasPluginEvent(events(), "search", protocol.KindSubmit, "abc") {
		t.Fatalf("no submit for the search field: %v", events())
	}

	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(events(), "empty", protocol.KindPress, "Create") {
		t.Fatalf("no press for the empty state's action: %v", events())
	}
}

// hasPluginEvent reports whether evs holds one of the node and kind, and
// of the value, when one is given.
func hasPluginEvent(evs []pluginEvent, node, kind string, value ...any) bool {
	for _, ev := range evs {
		if ev.Node != node || ev.Kind != kind {
			continue
		}
		if len(value) == 0 || ev.Value == value[0] {
			return true
		}
	}
	return false
}

// TestPluginFocusSurvivesRender keeps the focus on the part the user gave
// it to, across a render of a tree with the same ids.
func TestPluginFocusSurvivesRender(t *testing.T) {
	h := newPluginHarness(t, pluginEveryPart(), nil)
	tt := h.tt
	if err := tt.Click("Search files"); err != nil {
		t.Fatal(err)
	}
	tt.Type("abc")
	if h.view.focus != "search" {
		t.Fatalf("the view keeps %q as the focus, want the search field", h.view.focus)
	}
	if !tt.Focused("Search files") {
		t.Fatal("the search field is not focused")
	}
	h.show(pluginEveryPart())
	if !tt.Focused("Search files") {
		t.Fatal("the search field lost the focus across a render")
	}
	if h.view.focus != "search" {
		t.Fatalf("the view keeps %q as the focus across a render", h.view.focus)
	}
	tt.Type("d")
	if st := h.view.searches["search"]; st == nil || st.query != "abcd" {
		t.Fatalf("the search field holds %v after a render, want the typed text kept", st)
	}
}

// TestPluginScrollSurvivesRender keeps a list where the user scrolled it,
// across a render of a tree with the same ids.
func TestPluginScrollSurvivesRender(t *testing.T) {
	var items []protocol.Item
	for i := 1; i <= 30; i++ {
		title := fmt.Sprintf("Row %02d", i)
		items = append(items, protocol.Item{ID: fmt.Sprintf("r%02d", i), Title: title})
	}
	list := func() *protocol.Tree {
		return pluginTree(sdk.Column("root", sdk.Text("head", "Files").Role("title"), sdk.List("files", items...)).Grow(1))
	}
	h := newPluginHarness(t, list(), nil)
	r, ok := h.tt.Find("files")
	if !ok {
		t.Fatal("no list")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	h.tt.Scroll(x, y, 0, -300)
	h.tt.Frame()
	st := h.view.scroll["files"]
	if st == nil || st.Y == 0 {
		h.tt.Scroll(x, y, 0, 300)
		h.tt.Frame()
	}
	st = h.view.scroll["files"]
	if st == nil || st.Y == 0 {
		t.Fatalf("the list did not scroll: %+v", st)
	}
	scrolled := st.Y
	h.show(list())
	if st2 := h.view.scroll["files"]; st2 != st || st2.Y != scrolled {
		t.Fatalf("the list moved from %v to %v across a render", scrolled, st2.Y)
	}
}

// TestPluginImageFetchOffUIThreadAndCached fetches a picture without
// holding the frame, and once for as long as the tree asks for it.
func TestPluginImageFetchOffUIThreadAndCached(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	defaultPluginView = newPluginView()
	defaultPluginView.fetch = func(pluginSource) (*ui.Bitmap, image.Image, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for i := range img.Pix {
			img.Pix[i] = 0x40
		}
		return ui.NewBitmap(img), img, nil
	}
	tree := func() *protocol.Tree {
		return pluginTree(sdk.Column("root", sdk.Text("t", "Picture"), sdk.Image("pic", "https://example.invalid/a.png")))
	}
	pal := lightPalette
	tt := ui.NewTester(func(c *ui.Context) {
		drawPluginTree(c, &pal, tree(), nil)
	}, 500, 400)

	// A frame returns while the picture is still on its way: the fetch
	// does not run on the UI goroutine.
	done := make(chan struct{})
	go func() {
		tt.Frame()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a frame waited for the picture: the fetch ran on the UI goroutine")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the picture was not fetched")
	}
	tt.Frame()
	if got := calls.Load(); got != 1 {
		t.Fatalf("the picture was fetched %d times while loading, want once", got)
	}

	close(release)
	if !waitPluginPicture(t, defaultPluginView, pluginSource{url: "https://example.invalid/a.png"}) {
		t.Fatal("the picture did not arrive")
	}
	tt.Frame()

	// The plugin sends the same picture again, in a new tree: it is kept.
	tt.Frame()
	if got := calls.Load(); got != 1 {
		t.Fatalf("the picture was fetched %d times, want it cached", got)
	}
}

// waitPluginPicture waits for the picture of src to load, and reports
// whether it arrived before the test gave up.
func waitPluginPicture(t *testing.T, v *pluginView, src pluginSource) bool {
	t.Helper()
	key := src.url + "\x00" + src.data
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v.mu.Lock()
		pic := v.pictures[key]
		loaded := pic != nil && !pic.loading && pic.bm != nil
		v.mu.Unlock()
		if loaded {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// TestPluginProgressMovesFromRateAndAt runs a bar on its own between the
// plugin's renders, from its rate and the instant it sent.
func TestPluginProgressMovesFromRateAndAt(t *testing.T) {
	bar := func(rate float64, at int64) *protocol.Tree {
		return pluginTree(sdk.Column("root",
			sdk.Text("t", "Download"),
			sdk.Progress("dl", 0.2).Playing(rate, at),
		))
	}
	h := newPluginHarness(t, bar(0, 0), nil)
	at := h.tt.Now().UnixMilli()
	// Set the rate and the instant from the tester's clock, and render
	// that frame at once: h.frames would move the clock on.
	h.tree = bar(0.5, at)
	h.tt.Frame()
	if got := h.view.progress["dl"]; math.Abs(got-0.2) > 1e-9 {
		t.Fatalf("the bar shows %v at the instant it was sent, want 0.2", got)
	}
	h.tt.Advance(time.Second)
	h.tt.Frame()
	if got := h.view.progress["dl"]; math.Abs(got-0.7) > 1e-9 {
		t.Fatalf("the bar shows %v after a second at 0.5 a second, want 0.7", got)
	}
	h.tt.Advance(10 * time.Second)
	h.tt.Frame()
	if got := h.view.progress["dl"]; got != 1 {
		t.Fatalf("the bar shows %v after ten more seconds, want it clamped at 1", got)
	}
}

// TestPluginFetchDataURI reads a picture from a data: URI into the bitmap
// the pane shows and the image it samples colors from.
func TestPluginFetchDataURI(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 0xff, G: 0x40, B: 0x20, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	bm, sampled, err := pluginFetch(pluginSource{data: uri})
	if err != nil {
		t.Fatal(err)
	}
	if bm == nil || sampled == nil {
		t.Fatal("the data: URI did not decode to a picture")
	}
	if w, h := bm.Size(); w != 3 || h != 2 {
		t.Fatalf("the picture is %dx%d, want 3x2", w, h)
	}
	_ = sampled
}
