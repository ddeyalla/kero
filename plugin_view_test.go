package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
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
	v := newPluginView()
	v.fetch = stubPluginFetch()
	h := &pluginHarness{view: v, tree: tree, pal: lightPalette}
	h.tt = ui.NewTester(func(c *ui.Context) {
		drawPluginTree(c, h.view, &h.pal, h.tree, send)
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
		for _, label := range []string{"Slider", "Progress", "files", "Search files"} {
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

	r, ok := tt.Find("Slider")
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
	v := newPluginView()
	v.fetch = func(pluginSource) (*ui.Bitmap, image.Image, error) {
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
		drawPluginTree(c, v, &pal, tree(), nil)
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
	if !waitPluginPicture(t, v, pluginSource{url: "https://example.invalid/a.png"}) {
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
	key := pluginSourceKey(src)
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
	// From here the clock moves without a test Frame: the bar must ask
	// for the frames it needs itself, or a quiet pane would show it
	// standing still until the plugin renders again.
	h.tt.Advance(300 * time.Millisecond)
	if got := h.view.progress["dl"]; math.Abs(got-0.35) > 1e-9 {
		t.Fatalf("the bar shows %v after 0.3s at 0.5 a second, want 0.35", got)
	}
	h.tt.Advance(700 * time.Millisecond)
	if got := h.view.progress["dl"]; math.Abs(got-0.7) > 1e-9 {
		t.Fatalf("the bar shows %v after a second at 0.5 a second, want 0.7", got)
	}
	h.tt.Advance(10 * time.Second)
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

// TestPluginViewsStayApart keeps two panes with a plugin each apart: a
// scroll and a focus in one are not the other's.
func TestPluginViewsStayApart(t *testing.T) {
	var items []protocol.Item
	for i := 1; i <= 30; i++ {
		items = append(items, protocol.Item{ID: fmt.Sprintf("r%02d", i), Title: fmt.Sprintf("Row %02d", i)})
	}
	tree := func() *protocol.Tree {
		return pluginTree(sdk.Column("root",
			sdk.List("files", items...),
			sdk.SearchField("search", "Search files"),
		).Grow(1))
	}
	a := newPluginHarness(t, tree(), nil)
	b := newPluginHarness(t, tree(), nil)

	r, ok := a.tt.Find("files")
	if !ok {
		t.Fatal("the first pane has no list")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	a.tt.Scroll(x, y, 0, -300)
	a.tt.Frame()
	if st := a.view.scroll["files"]; st == nil || st.Y == 0 {
		a.tt.Scroll(x, y, 0, 300)
		a.tt.Frame()
	}
	if st := a.view.scroll["files"]; st == nil || st.Y == 0 {
		t.Fatalf("the first pane's list did not scroll: %+v", st)
	}
	if st := b.view.scroll["files"]; st != nil && st.Y != 0 {
		t.Fatalf("a scroll in one pane moved the other pane's list to %v", st.Y)
	}

	if err := a.tt.Click("Search files"); err != nil {
		t.Fatal(err)
	}
	a.tt.Type("abc")
	if a.view.focus != "search" {
		t.Fatalf("the first pane keeps %q as the focus, want its search field", a.view.focus)
	}
	if b.view.focus != "" {
		t.Fatalf("the second pane took the first pane's focus %q", b.view.focus)
	}
}

// pluginPictureColumn is a column of count pictures, from the first URL
// on: each picture a source of its own.
func pluginPictureColumn(first, count int) *protocol.Tree {
	var nodes []*sdk.Node
	for i := first; i < first+count; i++ {
		nodes = append(nodes, sdk.Image(fmt.Sprintf("pic-%02d", i), fmt.Sprintf("https://example.invalid/%d.png", i)).Size(8, 8))
	}
	return pluginTree(sdk.Column("root", nodes...))
}

// pluginPicturesLoaded reports how many pictures the pane keeps, and how
// many of them have arrived.
func pluginPicturesLoaded(v *pluginView) (kept, loaded int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, pic := range v.pictures {
		if pic.bm != nil {
			loaded++
		}
	}
	return len(v.pictures), loaded
}

// TestPluginPictureCacheKeepsShownPictures keeps every picture the tree
// shows, past the budget, and fetches each once however many draws ask for
// it: the budget drops only the pictures the tree no longer shows.
func TestPluginPictureCacheKeepsShownPictures(t *testing.T) {
	var calls atomic.Int32
	shown := pluginPictureBudget + 12
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.Text("t", "Pictures"))), nil)
	h.view.fetch = func(pluginSource) (*ui.Bitmap, image.Image, error) {
		calls.Add(1)
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		return ui.NewBitmap(img), img, nil
	}
	h.show(pluginPictureColumn(0, shown))
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.tt.Frame()
		if _, loaded := pluginPicturesLoaded(h.view); loaded == shown {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pictures did not all arrive: %d fetches", calls.Load())
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 40; i++ {
		h.tt.Frame()
		h.tt.Advance(100 * time.Millisecond)
	}
	if got := calls.Load(); got != int32(shown) {
		t.Fatalf("%d pictures shown for 40 draws were fetched %d times, want once each", shown, got)
	}
	if kept, _ := pluginPicturesLoaded(h.view); kept != shown {
		t.Fatalf("the pane keeps %d pictures, want the %d it shows", kept, shown)
	}

	// A tree of a few other pictures: the ones it no longer shows go,
	// down to the budget.
	h.show(pluginPictureColumn(1000, 4))
	h.frames()
	if kept, _ := pluginPicturesLoaded(h.view); kept > pluginPictureBudget {
		t.Fatalf("the pane keeps %d pictures, want at most %d", kept, pluginPictureBudget)
	}
	v := h.view
	v.mu.Lock()
	for i := 1000; i < 1004; i++ {
		if v.pictures[pluginSourceKey(pluginSource{url: fmt.Sprintf("https://example.invalid/%d.png", i)})] == nil {
			v.mu.Unlock()
			t.Fatalf("the pane dropped picture %d, which the tree shows", i)
		}
	}
	v.mu.Unlock()
}

// TestPluginListRowsWithPictures draws a list whose rows each have a
// picture: each row holds its own, and they all arrive.
func TestPluginListRowsWithPictures(t *testing.T) {
	var items []protocol.Item
	for i := 1; i <= 5; i++ {
		items = append(items, protocol.Item{
			ID:       fmt.Sprintf("r%d", i),
			Title:    fmt.Sprintf("Track %d", i),
			Subtitle: "An artist",
			Image:    fmt.Sprintf("https://example.invalid/art-%d.png", i),
		})
	}
	var got []pluginEvent
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.List("tracks", items...)).Grow(1)), func(ev pluginEvent) {
		got = append(got, ev)
	})
	for _, item := range items {
		if !waitPluginPicture(t, h.view, pluginSource{url: item.Image}) {
			t.Fatalf("the picture of %s did not arrive", item.ID)
		}
	}
	h.frames()
	for _, item := range items {
		if !h.tt.HasText(item.Title) {
			t.Fatalf("the list does not show %q; it shows %q", item.Title, h.tt.Texts())
		}
	}
	if err := h.tt.Click("Track 3"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(got, "tracks", protocol.KindSelect, "r3") {
		t.Fatalf("no select for the third row: %v", got)
	}
}

// TestPluginPictureRetriesAfterFailure tries a picture again, after a
// pause, when its first fetch failed: one network blip is not a session of
// placeholders.
func TestPluginPictureRetriesAfterFailure(t *testing.T) {
	var calls atomic.Int32
	v := newPluginView()
	v.fetch = func(pluginSource) (*ui.Bitmap, image.Image, error) {
		if calls.Add(1) == 1 {
			return nil, nil, errors.New("offline")
		}
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		return ui.NewBitmap(img), img, nil
	}
	src := pluginSource{url: "https://example.invalid/retry.png"}
	tree := pluginTree(sdk.Column("root", sdk.Image("pic", src.url)))
	pal := lightPalette
	tt := ui.NewTester(func(c *ui.Context) {
		drawPluginTree(c, v, &pal, tree, nil)
	}, 400, 300)
	key := pluginSourceKey(src)
	picture := func() *pluginPicture {
		v.mu.Lock()
		defer v.mu.Unlock()
		return v.pictures[key]
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pic := picture()
		if pic != nil && pic.failed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first fetch did not fail")
		}
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("the picture was fetched %d times, want once before the retry", got)
	}
	// The pane asks for the frame of the retry itself; Advance alone must
	// reach it.
	tt.Advance(pluginPictureRetry + 200*time.Millisecond)
	for {
		pic := picture()
		if pic != nil && pic.bm != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the picture was not tried again after its pause")
		}
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("the picture was fetched %d times, want the retry to be the second", got)
	}
}

// pluginEveryPartJSON is a tree with one of each part, as a plugin sends
// it over the wire.
const pluginEveryPartJSON = `{"root": {"type": "column", "id": "root", "children": [
	{"type": "text", "id": "title", "text": "A plugin pane", "role": "title"},
	{"type": "text", "id": "caption", "text": "a caption", "role": "caption", "color": "muted", "lines": 1},
	{"type": "text", "id": "mono", "text": "go test ./...", "role": "mono"},
	{"type": "text", "id": "marquee", "text": "A title that scrolls", "marquee": true},
	{"type": "row", "id": "bar", "gap": 8, "padding": 6, "align": "spaceBetween", "children": [
		{"type": "button", "id": "go", "label": "Start", "primary": true},
		{"type": "iconButton", "id": "play", "icon": "play"},
		{"type": "button", "id": "off", "label": "Stop", "enabled": false},
		{"type": "spacer", "id": "bar-space"}
	]},
	{"type": "image", "id": "art", "url": "https://example.invalid/art.png", "width": 80, "height": 60, "radius": 5, "tint": true},
	{"type": "slider", "id": "volume", "label": "Volume", "value": 0.5, "min": 0, "max": 1, "step": 0.1},
	{"type": "progress", "id": "track", "value": 0.25, "rate": 0.01, "at": 1},
	{"type": "list", "id": "files", "items": [
		{"id": "r1", "title": "Row one", "subtitle": "first", "trailing": "12 KB", "image": "https://example.invalid/r1.png"},
		{"id": "r2", "title": "Row two", "image": "https://example.invalid/r2.png"}
	]},
	{"type": "searchField", "id": "search", "placeholder": "Search files"},
	{"type": "emptyState", "id": "empty", "title": "Nothing here", "detail": "A detail line", "action": "Create"},
	{"type": "divider", "id": "divider"}
]}}`

// TestPluginPartsDrawFromJSON decodes a tree as the protocol does, checks
// it, and draws every part of it.
func TestPluginPartsDrawFromJSON(t *testing.T) {
	var tree protocol.Tree
	if err := json.Unmarshal([]byte(pluginEveryPartJSON), &tree); err != nil {
		t.Fatal(err)
	}
	if err := tree.ValidateTree(); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		h := newPluginHarness(t, &tree, nil)
		if dark {
			h.pal = darkPalette
			h.frames()
		}
		h.frames()
		for _, text := range []string{
			"A plugin pane", "a caption", "go test ./...", "A title that scrolls", "Start", "Stop",
			"Row one", "first", "12 KB", "Row two", "Nothing here", "A detail line", "Create",
		} {
			if !h.tt.HasText(text) {
				t.Fatalf("dark %v: the tree does not show %q; it shows %q", dark, text, h.tt.Texts())
			}
		}
		for _, label := range []string{"Volume", "Progress", "play", "files", "Search files"} {
			if _, ok := h.tt.Find(label); !ok {
				t.Fatalf("dark %v: no part is labeled %q", dark, label)
			}
		}
		for _, src := range []pluginSource{
			{url: "https://example.invalid/art.png", tint: true},
			{url: "https://example.invalid/r1.png"},
			{url: "https://example.invalid/r2.png"},
		} {
			if !waitPluginPicture(t, h.view, src) {
				t.Fatalf("dark %v: the picture %s did not arrive", dark, src.url)
			}
		}
	}
}

// TestPluginPartLabels names a slider and a progress bar to assistive
// technology by the label the plugin gives them, or by their kind, not by
// their ids.
func TestPluginPartLabels(t *testing.T) {
	slider := sdk.Slider("volume", 0.5, 0, 1).Build()
	slider.Label = "Volume"
	progress := sdk.Progress("track", 0.5).Build()
	tree := &protocol.Tree{Root: &protocol.Node{Type: protocol.NodeColumn, ID: "root", Children: []*protocol.Node{
		slider, progress,
		sdk.Slider("balance", 0.5, 0, 1).Build(),
	}}}
	h := newPluginHarness(t, tree, nil)
	for _, label := range []string{"Volume", "Progress", "Slider"} {
		if _, ok := h.tt.Find(label); !ok {
			t.Fatalf("no part is labeled %q", label)
		}
	}
	for _, id := range []string{"volume", "track", "balance"} {
		if _, ok := h.tt.Find(id); ok {
			t.Fatalf("a part is labeled by its id %q", id)
		}
	}
}

// TestPluginFocusSurvivesReparent gives the focus back to the part the
// user gave it to when the part moves to another parent, or the tree to a
// new root id: MyGo sees a new control, but the part id is the same.
func TestPluginFocusSurvivesReparent(t *testing.T) {
	flat := pluginTree(sdk.Column("root", sdk.SearchField("search", "Search files")))
	nested := pluginTree(sdk.Column("root", sdk.Row("bar", sdk.SearchField("search", "Search files"))))
	compact := pluginTree(sdk.Column("compact", sdk.SearchField("search", "Search files")))
	h := newPluginHarness(t, flat, nil)
	if err := h.tt.Click("Search files"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("abc")
	if !h.tt.Focused("Search files") || h.view.focus != "search" {
		t.Fatalf("the search field is not focused: view focus %q", h.view.focus)
	}
	for _, step := range []struct {
		name string
		tree *protocol.Tree
	}{
		{"a new parent", nested},
		{"a new root id", compact},
		{"the first tree again", flat},
	} {
		h.show(step.tree)
		if !h.tt.Focused("Search files") {
			t.Fatalf("after %s the search field lost the focus", step.name)
		}
		if h.view.focus != "search" {
			t.Fatalf("after %s the view keeps %q as the focus", step.name, h.view.focus)
		}
	}
	h.tt.Type("d")
	if st := h.view.searches["search"]; st == nil || st.query != "abcd" {
		t.Fatalf("the search field holds %+v, want the typed text kept", st)
	}

	// A part that goes takes its focus with it: when it comes back, the
	// focus is not taken back from where the user has moved it since.
	h.show(pluginTree(sdk.Column("root", sdk.Text("t", "Nothing to search"))))
	if h.view.focus != "" {
		t.Fatalf("the view keeps %q as the focus of a part that went", h.view.focus)
	}
	h.show(flat)
	if h.tt.Focused("Search files") {
		t.Fatal("the search field took the focus back when it came back")
	}
}

// TestPluginScrollSurvivesReparent keeps a list where the user scrolled
// it when it moves to another parent.
func TestPluginScrollSurvivesReparent(t *testing.T) {
	var items []protocol.Item
	for i := 1; i <= 30; i++ {
		items = append(items, protocol.Item{ID: fmt.Sprintf("r%02d", i), Title: fmt.Sprintf("Row %02d", i)})
	}
	flat := pluginTree(sdk.Column("root", sdk.List("files", items...)).Grow(1))
	nested := pluginTree(sdk.Column("root", sdk.Column("box", sdk.List("files", items...)).Grow(1)).Grow(1))
	h := newPluginHarness(t, flat, nil)
	r, ok := h.tt.Find("files")
	if !ok {
		t.Fatal("no list")
	}
	h.tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, -300)
	h.tt.Frame()
	if st := h.view.scroll["files"]; st == nil || st.Y == 0 {
		h.tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 300)
		h.tt.Frame()
	}
	st := h.view.scroll["files"]
	if st == nil || st.Y == 0 {
		t.Fatalf("the list did not scroll: %+v", st)
	}
	scrolled := st.Y
	first, _ := h.tt.Find("Row 01")
	h.show(nested)
	if st2 := h.view.scroll["files"]; st2 != st || st2.Y != scrolled {
		t.Fatalf("the list moved from %v to %+v under a new parent", scrolled, st2)
	}
	if again, ok := h.tt.Find("Row 01"); ok && again.Y != first.Y {
		t.Fatalf("the first row moved from %v to %v under a new parent", first.Y, again.Y)
	}
}

// TestPluginSliderKeyboardCommits commits a value the arrows set, as the
// pointer's release does, and moves by the plugin's step.
func TestPluginSliderKeyboardCommits(t *testing.T) {
	var got []pluginEvent
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.Slider("volume", 0.5, 0, 1).Step(0.1))), func(ev pluginEvent) {
		got = append(got, ev)
	})
	r, ok := h.tt.Find("Slider")
	if !ok {
		t.Fatal("no slider")
	}
	// The pointer gives it the focus, at the value it already has.
	h.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Frame()
	got = nil
	h.tt.Key(0, ui.KeyRight)
	h.tt.Frame()
	change, commit := -1, -1
	for i, ev := range got {
		switch {
		case ev.Node != "volume":
		case ev.Kind == protocol.KindChange && change < 0:
			change = i
		case ev.Kind == protocol.KindCommit:
			commit = i
		}
	}
	if change < 0 || commit < 0 || commit < change {
		t.Fatalf("an arrow sent %v, want a change, then a commit", got)
	}
	v, ok := got[commit].Value.(float64)
	if !ok || math.Abs(v-0.6) > 1e-6 {
		t.Fatalf("the arrow committed %v, want one step of 0.1 up from 0.5", got[commit].Value)
	}
	commits := 0
	for _, ev := range got {
		if ev.Kind == protocol.KindCommit {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("one arrow sent %d commits: %v", commits, got)
	}
}

// TestPluginFetchRefusesHugePicture turns away a small file that declares
// a picture too large to decode, before it decodes it.
func TestPluginFetchRefusesHugePicture(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	// The header chunk follows the 8-byte signature: its length and type,
	// then the width and the height, then its checksum.
	binary.BigEndian.PutUint32(data[16:], 100000)
	binary.BigEndian.PutUint32(data[20:], 100000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 100000 {
		t.Fatalf("the test picture does not declare its size: %+v %v", cfg, err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	if bm, img, err := pluginFetch(pluginSource{data: uri}); err == nil || bm != nil || img != nil {
		t.Fatalf("a picture of 100000x100000 was decoded: %v", err)
	}
}
