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
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
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
	// No test draws a real symbol, unless it asks to.
	v.render = func(string, float64, float64, int) (*image.RGBA, bool) { return nil, false }
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
		sdk.Column("card",
			sdk.Segmented("range",
				protocol.Item{ID: "today", Title: "Today", Active: true},
				protocol.Item{ID: "yesterday", Title: "Yesterday"},
				protocol.Item{ID: "30d", Title: "30 Days"},
			),
			sdk.Row("cost",
				sdk.Donut("donut",
					protocol.Segment{ID: "claude", Label: "Claude", Value: 3, Color: protocol.ColorOrange},
					protocol.Segment{ID: "codex", Label: "Codex", Value: 1, Color: protocol.ColorBlue},
				).Center("$4.6K", "dollars").Width(96),
				sdk.Column("legend",
					sdk.Text("leg-dot", "").Icon("dot").Color(protocol.ColorOrange),
					sdk.Text("leg-name", "Claude").Mark("claude").Role(protocol.RoleStrong),
					sdk.Text("flame", "Limit in 1d 4h").Icon("flame").Color(protocol.ColorDanger).Role(protocol.RoleCaption),
				).Grow(1).Gap(6),
			).Gap(12),
			sdk.Progress("pace", 0.4).Marker(0.7).Color(protocol.ColorDanger).Label("Session"),
		).Surface(protocol.SurfaceCard).Padding(12).Gap(10),
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
		for _, text := range []string{
			"Today", "Yesterday", "30 Days", "$4.6K", "dollars", "Claude", "Limit in 1d 4h",
		} {
			if !h.tt.HasText(text) {
				t.Fatalf("dark %v: the tree does not show %q; it shows %q", dark, text, h.tt.Texts())
			}
		}
		for _, label := range []string{"Slider", "Progress", "files", "Search files", "Session", "$4.6K dollars", "Today"} {
			if _, ok := h.tt.Find(label); !ok {
				t.Fatalf("dark %v: no part is labeled %q", dark, label)
			}
		}
	}
}

// TestPluginMarksAreProgramIcons keeps the marks the protocol allows in
// step with the logos the app has.
func TestPluginMarksAreProgramIcons(t *testing.T) {
	for _, mark := range protocol.KnownMarks {
		if programIcons[mark] == nil {
			t.Errorf("mark %q has no program icon", mark)
		}
	}
	for name := range programIcons {
		if !slices.Contains(protocol.KnownMarks, name) {
			t.Errorf("program icon %q is not a known mark", name)
		}
	}
}

// TestPluginIconsAreDrawn gives every icon of the protocol an SF Symbol,
// and the icons of the usage parts an SVG to fall back on as well.
func TestPluginIconsAreDrawn(t *testing.T) {
	for _, name := range protocol.KnownIcons {
		if _, ok := pluginSymbols[name]; !ok {
			t.Errorf("icon %q has no SF Symbol", name)
		}
	}
	for _, name := range []string{"flame", "dot", "warning", "info", "chevron-up", "chevron-down"} {
		if _, ok := pluginIcons[name]; !ok {
			t.Errorf("icon %q has no SVG", name)
		}
	}
}

// TestPluginSegmentedChoosesAtOnce switches a segmented control on a
// click, sends a select with the item's id, and takes the plugin's choice
// back when it changes.
func TestPluginSegmentedChoosesAtOnce(t *testing.T) {
	var mu sync.Mutex
	var got []pluginEvent
	send := func(ev pluginEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}
	tree := func(active string) *protocol.Tree {
		items := make([]protocol.Item, 0, 3)
		for _, id := range []string{"today", "yesterday", "30d"} {
			items = append(items, protocol.Item{ID: id, Title: "item " + id, Active: id == active})
		}
		return pluginTree(sdk.Column("root", sdk.Segmented("range", items...)))
	}
	h := newPluginHarness(t, tree("today"), send)
	if st := h.view.segments["range"]; st == nil || st.index != 0 {
		t.Fatalf("segments = %v, want the first chosen", h.view.segments)
	}
	if err := h.tt.Click("item 30d"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	ok := hasPluginEvent(got, "range", protocol.KindSelect, "30d")
	mu.Unlock()
	if !ok {
		t.Fatalf("no select for the segment: %v", got)
	}
	if st := h.view.segments["range"]; st.index != 2 {
		t.Fatalf("index = %d after a click, want 2", st.index)
	}
	// The plugin echoes the choice, then chooses another.
	h.show(tree("30d"))
	if st := h.view.segments["range"]; st.index != 2 {
		t.Fatalf("index = %d after the echo, want 2", st.index)
	}
	h.show(tree("yesterday"))
	if st := h.view.segments["range"]; st.index != 1 {
		t.Fatalf("index = %d after the plugin chose, want 1", st.index)
	}
	h.show(pluginTree(sdk.Column("root", sdk.Text("t", "gone"))))
	if len(h.view.segments) != 0 {
		t.Fatalf("segments = %v, want the state of a removed control dropped", h.view.segments)
	}
}

// TestPluginColorRolesDraw maps every color role to a color of its own in
// both appearances.
func TestPluginColorRolesDraw(t *testing.T) {
	for _, pal := range []*palette{&lightPalette, &darkPalette} {
		seen := map[ui.Color]string{}
		for _, role := range protocol.KnownColorRoles {
			col := pluginTextColor(pal, &protocol.Node{Color: role})
			if col.A == 0 {
				t.Errorf("dark %v: color %q is transparent", pal.dark, role)
			}
			if role == protocol.ColorText || role == protocol.ColorMuted || role == protocol.ColorAccent || role == protocol.ColorBlue || role == protocol.ColorGray {
				continue
			}
			if other, dup := seen[col]; dup {
				t.Errorf("dark %v: colors %q and %q are alike", pal.dark, role, other)
			}
			seen[col] = role
		}
	}
}

// TestPluginPartShots writes shots of a usage-like pane, at 300 wide, in
// both appearances, into the directory PLUGIN_SHOT_DIR names.
func TestPluginPartShots(t *testing.T) {
	dir := os.Getenv("PLUGIN_SHOT_DIR")
	if dir == "" {
		t.Skip("PLUGIN_SHOT_DIR is not set")
	}
	for _, dark := range []bool{false, true} {
		h := newPluginHarness(t, pluginUsageShot(), nil)
		pal := lightPalette
		if dark {
			pal = darkPalette
		}
		// The pane draws on the card of the app's own background.
		h.tt = ui.NewTester(func(c *ui.Context) {
			ui.Column(c.Key("shot")).Fill().Background(pal.card).Children(func() {
				drawPluginTree(c, h.view, &pal, h.tree, nil)
			})
		}, 300, 560)
		h.frames()
		name := "plugin-parts-light.png"
		if dark {
			name = "plugin-parts-dark.png"
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pluginUsageShot is a pane in the look of the usage plugin's.
func pluginUsageShot() *protocol.Tree {
	legend := func(id, name string, color string, usd string, estimate bool) *sdk.Node {
		amount := []*sdk.Node{sdk.Text("leg-usd-"+id, usd)}
		if estimate {
			amount = append([]*sdk.Node{sdk.Text("leg-approx-"+id, "≈").Color(protocol.ColorMuted)}, amount...)
		}
		return sdk.Row("leg-"+id,
			sdk.Text("leg-dot-"+id, "").Icon("dot").Color(color),
			sdk.Text("leg-name-"+id, name).Grow(1).Lines(1),
			sdk.Row("leg-amount-"+id, amount...).Gap(0),
		).Gap(6).Align(protocol.AlignCenter)
	}
	window := func(id, label string, used, pace float64, tone, left, resets string) *sdk.Node {
		return sdk.Column("win-"+id,
			sdk.Row("win-head-"+id,
				sdk.Text("wlabel-"+id, label).Role(protocol.RoleStrong),
				sdk.Spacer("wspace-"+id),
				sdk.Text("pace-"+id, "Limit in 1d 4h").Icon("flame").Color(protocol.ColorDanger).Role(protocol.RoleCaption),
			),
			sdk.Progress("bar-"+id, used).Marker(pace).Color(tone).Label(label),
			sdk.Row("win-foot-"+id,
				sdk.Text("left-"+id, left).Role(protocol.RoleCaption).Color(protocol.ColorMuted).Grow(1),
				sdk.Text("reset-"+id, resets).Role(protocol.RoleCaption).Color(protocol.ColorMuted),
			),
		).Gap(4)
	}
	return pluginTree(sdk.Column("usage-root",
		sdk.Column("usage-cost",
			sdk.Segmented("usage-range",
				protocol.Item{ID: "today", Title: "Today", Active: true},
				protocol.Item{ID: "yesterday", Title: "Yesterday"},
				protocol.Item{ID: "30d", Title: "30 Days"},
			),
			sdk.Row("usage-cost-body",
				sdk.Donut("usage-donut",
					protocol.Segment{ID: "claude", Value: 3100, Color: protocol.ColorOrange},
					protocol.Segment{ID: "codex", Value: 1400, Color: protocol.ColorGreen},
					protocol.Segment{ID: "opencode", Value: 90, Color: protocol.ColorPurple},
					protocol.Segment{ID: "deepseek", Value: 4.2, Color: protocol.ColorBlue},
				).Center("$4.6K", "dollars").Width(96),
				sdk.Column("usage-legend",
					legend("claude", "Claude", protocol.ColorOrange, "$3,100.00", false),
					legend("codex", "Codex", protocol.ColorGreen, "$1,400.00", true),
					legend("opencode", "OpenCode Go", protocol.ColorPurple, "$90.00", false),
					legend("deepseek", "DeepSeek", protocol.ColorBlue, "$4.20", false),
				).Grow(1).Gap(6),
			).Gap(12),
			sdk.Text("usage-estimated", "Some amounts are estimates").Icon("info").Role(protocol.RoleCaption).Color(protocol.ColorMuted).Lines(1).Label("Includes estimates for unknown models: gpt-9"),
		).Surface(protocol.SurfaceCard).Padding(12).Gap(10),
		sdk.Column("sec-claude",
			sdk.Row("head-claude",
				sdk.Text("name-claude", "Claude").Mark("claude").Role(protocol.RoleStrong),
				sdk.Text("plan-claude", "Max").Role(protocol.RoleCaption).Color(protocol.ColorMuted),
				sdk.Spacer("head-space"),
				sdk.IconButton("toggle-claude", "chevron-down").Label("Show details"),
			).Gap(6),
			sdk.Column("card-claude",
				window("0", "Session", 0.62, 0.4, protocol.ColorDanger, "38% left", "Resets in 3h 12m"),
				window("1", "Weekly", 0.2, 0.3, protocol.ColorBlue, "80% left", "Resets in 3d 6h"),
			).Surface(protocol.SurfaceCard).Padding(12).Gap(12),
		).Gap(6),
		sdk.Row("usage-foot",
			sdk.Text("usage-updated", "Updated just now").Role(protocol.RoleCaption).Color(protocol.ColorMuted).Grow(1),
			sdk.IconButton("usage-refresh", "refresh").Label("Refresh"),
		),
	).Padding(12).Gap(12))
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

// TestPluginSearchFieldTakesThePluginsQuery shows the query a plugin sends
// when the field first shows and when the plugin changes it, and keeps
// the letters typed after a change whose echo comes back late.
func TestPluginSearchFieldTakesThePluginsQuery(t *testing.T) {
	field := func(query string) *protocol.Tree {
		return pluginTree(sdk.Column("root", sdk.SearchField("search", "Search files").Query(query)))
	}
	query := func(h *pluginHarness) string {
		st := h.view.searches["search"]
		if st == nil {
			t.Fatal("the view keeps no search field")
		}
		return st.query
	}

	saved := newPluginHarness(t, field("miles"), nil)
	if got := query(saved); got != "miles" {
		t.Fatalf("a field that first shows holds %q, want the plugin's saved query", got)
	}

	var mu sync.Mutex
	var got []pluginEvent
	send := func(ev pluginEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}
	h := newPluginHarness(t, field(""), send)
	if err := h.tt.Click("Search files"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("abc")
	h.tt.Advance(pluginSearchDelay + 50*time.Millisecond)
	mu.Lock()
	changed := hasPluginEvent(got, "search", protocol.KindChange, "abc")
	mu.Unlock()
	if !changed {
		t.Fatal("no change for the typed query")
	}
	h.tt.Type("d")
	h.show(field("abc"))
	if q := query(h); q != "abcd" {
		t.Fatalf("the plugin's echo of %q made the field %q, want the typed %q kept", "abc", q, "abcd")
	}
	h.show(field("abc"))
	if q := query(h); q != "abcd" {
		t.Fatalf("a render with the same query made the field %q", q)
	}
	h.show(field("coltrane"))
	if q := query(h); q != "coltrane" {
		t.Fatalf("the field holds %q, want the query the plugin changed it to", q)
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

// TestPluginFetchDownscalesLargePictures decodes a 640 pixel cover to at
// most 512 pixels, in proportion, and leaves a small picture as it is.
func TestPluginFetchDownscalesLargePictures(t *testing.T) {
	uri := func(w, h int) string {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+3] = 0xc0, 0xff
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	for _, tc := range []struct{ w, h, wantW, wantH int }{
		{640, 640, 512, 512},
		{640, 320, 512, 256},
		{300, 600, 256, 512},
		{512, 400, 512, 400},
		{200, 200, 200, 200},
	} {
		bm, img, err := pluginFetch(pluginSource{data: uri(tc.w, tc.h)})
		if err != nil {
			t.Fatal(err)
		}
		if w, h := bm.Size(); w != tc.wantW || h != tc.wantH {
			t.Errorf("a %dx%d picture is %dx%d, want %dx%d", tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
		if b := img.Bounds(); b.Dx() != tc.wantW || b.Dy() != tc.wantH {
			t.Errorf("a %dx%d picture samples at %v, want %dx%d", tc.w, tc.h, b, tc.wantW, tc.wantH)
		}
		// The colors survive the filter.
		if r, _, _, a := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA(); r>>8 < 0xb8 || a>>8 != 0xff {
			t.Errorf("a %dx%d picture lost its color: r=%#x a=%#x", tc.w, tc.h, r>>8, a>>8)
		}
	}
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
		if !waitPluginPicture(t, h.view, pluginSource{url: item.Image, side: pluginTileSide(28)}) {
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
			{url: "https://example.invalid/r1.png", side: pluginTileSide(28)},
			{url: "https://example.invalid/r2.png", side: pluginTileSide(28)},
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

// TestPluginSliderDragOffTheTrackCommitsOnce commits once, on release, a
// drag whose pointer strays off the thin track: each step of it is not a
// value the user settled on. A seek bar that committed every step flooded
// the player with seeks.
func TestPluginSliderDragOffTheTrackCommitsOnce(t *testing.T) {
	var got []pluginEvent
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.Slider("seek", 0.2, 0, 1))), func(ev pluginEvent) {
		got = append(got, ev)
	})
	r, ok := h.tt.Find("Slider")
	if !ok {
		t.Fatal("no slider")
	}
	y := r.Y + r.H/2
	h.tt.Press(r.X+r.W*0.2, y)
	h.tt.Frame()
	for i, f := range []float32{0.3, 0.4, 0.5, 0.6, 0.7} {
		// Below the track, as a hand drifts while it drags.
		h.tt.Move(r.X+r.W*f, y+r.H*float32(2+i))
		h.tt.Frame()
	}
	commits := 0
	for _, ev := range got {
		if ev.Node == "seek" && ev.Kind == protocol.KindCommit {
			commits++
		}
	}
	if commits != 0 {
		t.Fatalf("the drag committed %d times before the release: %v", commits, got)
	}
	h.tt.Release(r.X+r.W*0.7, y+r.H*6)
	h.tt.Frame()
	for _, ev := range got {
		if ev.Node == "seek" && ev.Kind == protocol.KindCommit {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("the release sent %d commits, want one: %v", commits, got)
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

// bandOf is the pixels of a frame inside r.
func bandOf(img *image.RGBA, r ui.Rect) []byte {
	var out []byte
	for y := int(r.Y); y < int(r.Y+r.H) && y < img.Bounds().Dy(); y++ {
		i := img.PixOffset(int(r.X), y)
		out = append(out, img.Pix[i:i+int(r.W)*4]...)
	}
	return out
}

// pluginScrollingTree is a pane that scrolls as one: a head, and a list of
// n rows under it.
func pluginScrollingTree(n int) *protocol.Tree {
	var items []protocol.Item
	for i := 1; i <= n; i++ {
		items = append(items, protocol.Item{ID: fmt.Sprintf("r%02d", i), Title: fmt.Sprintf("Row %02d", i)})
	}
	tree := sdk.Column("root",
		sdk.Text("head", "The head").Role("title"),
		sdk.List("files", items...).Grow(1),
	).Gap(8).Grow(1).ScrollingTree()
	return &tree
}

// TestPluginScrollsAsOne scrolls a pane taller than its content, which
// stays where it is, and a short pane, which scrolls its head out of view
// with its list, and keeps the list from scrolling on its own.
func TestPluginScrollsAsOne(t *testing.T) {
	draw := func(tree *protocol.Tree, w, h int) (*ui.Tester, *pluginView) {
		v := newPluginView()
		v.fetch = stubPluginFetch()
		pal := lightPalette
		tt := ui.NewTester(func(c *ui.Context) { drawPluginTree(c, v, &pal, tree, nil) }, w, h)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		return tt, v
	}

	// Room to spare: nothing scrolls.
	tt, v := draw(pluginScrollingTree(2), 400, 600)
	tt.Scroll(200, 300, 0, -300)
	tt.Frame()
	if st := v.scroll["root"]; st != nil && st.Y != 0 {
		t.Fatalf("a pane taller than its content scrolled to %v", st.Y)
	}
	if r, ok := tt.Find("The head"); !ok || r.Y < 0 {
		t.Fatalf("the head left the pane: %v %v", r, ok)
	}

	// A short pane: the head scrolls out, the list does not scroll alone.
	tt, v = draw(pluginScrollingTree(40), 400, 200)
	head, ok := tt.Find("The head")
	if !ok {
		t.Fatal("no head")
	}
	before := bandOf(tt.Image(), head)
	tt.Scroll(200, 100, 0, -300)
	tt.Frame()
	if v.scroll["root"] == nil || v.scroll["root"].Y == 0 {
		tt.Scroll(200, 100, 0, 300)
		tt.Frame()
	}
	st := v.scroll["root"]
	if st == nil || st.Y == 0 {
		t.Fatalf("the pane did not scroll: %+v", st)
	}
	// The tester's boxes do not move with a scroll, so the pixels tell: the
	// strip the head drew in is not what it was.
	if after := bandOf(tt.Image(), head); bytes.Equal(after, before) {
		t.Fatalf("the head's place did not change after the pane scrolled %v", st.Y)
	}
	if st.Y < float32(head.H) {
		t.Fatalf("the pane scrolled %v, less than the head's %v", st.Y, head.H)
	}
	if st := v.scroll["files"]; st != nil {
		t.Fatalf("the list scrolls on its own: %+v", st)
	}
	// All 40 rows are inside the one scroll: it runs on past their height.
	tt.Scroll(200, 100, 0, -100000)
	tt.Frame()
	tt.Scroll(200, 100, 0, 100000)
	tt.Frame()
	if v.scroll["root"].Y < 40*rowH-200 {
		t.Fatalf("the pane scrolls to %v, less than its 40 rows are tall", v.scroll["root"].Y)
	}
}

// TestPluginSymbolIcons draws a symbol in place of an icon's SVG, tinted
// with the part's color, once its fake renderer has drawn it, and keeps the
// SVG when the symbol is not there.
func TestPluginSymbolIcons(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]int{}
	v := newPluginView()
	v.fetch = stubPluginFetch()
	v.render = func(name string, size, weight float64, scale int) (*image.RGBA, bool) {
		mu.Lock()
		defer mu.Unlock()
		asked[name]++
		img := image.NewRGBA(image.Rect(0, 0, 8*scale, 8*scale))
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
		return img, true
	}
	h := &pluginHarness{view: v, pal: lightPalette,
		tree: pluginTree(sdk.Column("root", sdk.IconButton("play", "play").Primary().Size(44, 44)))}
	h.tt = ui.NewTester(func(c *ui.Context) { drawPluginTree(c, h.view, &h.pal, h.tree, nil) }, 900, 700)
	h.show(h.tree)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.show(h.tree)
		mu.Lock()
		n := asked["play.fill"]
		mu.Unlock()
		v := h.view
		v.mu.Lock()
		done := 0
		for _, s := range v.symbols {
			if s.bitmap != nil {
				done++
			}
		}
		v.mu.Unlock()
		if n > 0 && done > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	n := asked["play.fill"]
	mu.Unlock()
	if n != 1 {
		t.Fatalf("play.fill was drawn %d times, want once, cached after", n)
	}
	h.view.mu.Lock()
	defer h.view.mu.Unlock()
	for _, s := range h.view.symbols {
		if s.bitmap == nil {
			t.Fatal("the symbol has no bitmap")
		}
		if w, hh := s.bitmap.Size(); w != 16 || hh != 16 {
			t.Fatalf("the symbol is %dx%d pixels, want 16x16 for 8 points at 2x", w, hh)
		}
	}

	// A symbol the system lacks is tried once, and the SVG stays.
	h2 := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.IconButton("next", "next"))), nil)
	h2.view.render = func(string, float64, float64, int) (*image.RGBA, bool) { return nil, false }
	h2.show(h2.tree)
	time.Sleep(50 * time.Millisecond)
	h2.show(h2.tree)
	if _, found := h2.tt.Find("next"); !found {
		t.Fatal("the button is gone without its symbol")
	}
}

// TestTintSymbol colors the shape of a symbol's alpha, premultiplied.
func TestTintSymbol(t *testing.T) {
	mask := image.NewRGBA(image.Rect(0, 0, 2, 1))
	mask.Pix[3] = 0xff
	mask.Pix[7] = 0x80
	out := tintSymbol(mask, ui.RGB(200, 100, 50))
	if got := out.Pix[:4]; got[0] != 200 || got[1] != 100 || got[2] != 50 || got[3] != 0xff {
		t.Fatalf("opaque pixel %v", got)
	}
	if got := out.Pix[4:8]; got[0] != 100 || got[3] != 0x80 {
		t.Fatalf("half pixel %v, want premultiplied", got)
	}
}

// TestPluginSliderAndClockRunOnTheirOwn advances a slider's value and a
// clock text from their rate and instant, within their ranges.
func TestPluginSliderAndClockRunOnTheirOwn(t *testing.T) {
	at := time.UnixMilli(10_000)
	slider := &protocol.Node{Type: protocol.NodeSlider, Value: 30_000, Min: 0, Max: 200_000, Rate: 1000, At: at.UnixMilli()}
	if got := pluginSliderValue(slider, at.Add(2500*time.Millisecond)); got != 32_500 {
		t.Fatalf("the slider is at %v after 2.5s, want 32500", got)
	}
	if got := pluginSliderValue(slider, at.Add(time.Hour)); got != 200_000 {
		t.Fatalf("the slider is at %v after an hour, want its max", got)
	}
	slider.Rate = 0
	if got := pluginSliderValue(slider, at.Add(time.Hour)); got != 30_000 {
		t.Fatalf("a slider with no rate is at %v, want its value", got)
	}

	clock := &protocol.Node{Type: protocol.NodeText, Text: "1:05", Value: 65, Max: 70, Rate: 1, At: at.UnixMilli()}
	text, ok, next := pluginClockText(clock, at.Add(2250*time.Millisecond))
	if text != "1:07" || !ok || next < 750*time.Millisecond || next > 760*time.Millisecond {
		t.Fatalf("the clock reads %q ok=%v next=%v, want 1:07 and the next second in 750ms", text, ok, next)
	}
	text, ok, next = pluginClockText(clock, at.Add(time.Minute))
	if text != "1:10" || !ok || next != 0 {
		t.Fatalf("the clock reads %q ok=%v next=%v, want 1:10 capped and stopped", text, ok, next)
	}
	clock.Rate = 0
	if text, ok, _ := pluginClockText(clock, at); ok || text != "1:05" {
		t.Fatalf("a paused clock reads %q ok=%v, want its own text", text, ok)
	}
}

// TestPluginSliderRunsOnAndYieldsToTheUser moves a playing slider by the
// clock without a render, and keeps the user's value when they set one,
// until the plugin answers or the hold ends.
func TestPluginSliderRunsOnAndYieldsToTheUser(t *testing.T) {
	tree := func(value float64, at int64) *protocol.Tree {
		return pluginTree(sdk.Column("root",
			sdk.Slider("seek", value, 0, 100).Playing(1, at),
			sdk.Clock("elapsed", value, 100).Playing(1, at),
		))
	}
	h := newPluginHarness(t, tree(0, 0), nil)
	at := h.tt.Now().UnixMilli()
	h.tree = tree(10, at)
	h.tt.Frame()
	if got := h.view.sliders["seek"].value; got != 10 {
		t.Fatalf("the slider shows %v when sent, want 10", got)
	}
	h.tt.Advance(5 * time.Second)
	if got := h.view.sliders["seek"].value; math.Abs(got-15) > 0.3 {
		t.Fatalf("the slider shows %v after 5s with no render, want about 15", got)
	}
	if !h.tt.HasText("0:15") && !h.tt.HasText("0:14") {
		t.Fatalf("the clock does not count on: %q", h.tt.Texts())
	}
	// The user lets go of a value of their own: it stands, not the old run.
	st := h.view.sliders["seek"]
	st.value, st.holdUntil = 80, h.tt.Now().Add(pluginSliderHold)
	h.tt.Advance(time.Second)
	if st.value != 80 {
		t.Fatalf("the slider jumped to %v inside the hold, want 80", st.value)
	}
	// The plugin answers with a new instant: its value wins.
	h.tree = tree(80, h.tt.Now().UnixMilli())
	h.tt.Frame()
	if st.value != 80 || !st.holdUntil.IsZero() {
		t.Fatalf("after the answer the slider is %v hold %v, want 80 and no hold", st.value, st.holdUntil)
	}
	// No answer: after the hold, the run takes over from the plugin's value.
	st.value, st.holdUntil = 5, h.tt.Now().Add(pluginSliderHold)
	h.tt.Advance(pluginSliderHold + time.Second)
	if st.value < 80 {
		t.Fatalf("the slider stays at %v after the hold, want it to run on from 80", st.value)
	}
}

// TestPluginSymbolKeyHasNoColor draws a symbol once for any color, and
// keeps the last bitmap of an icon while another size is drawn.
func TestPluginSymbolKeyHasNoColor(t *testing.T) {
	var mu sync.Mutex
	asked := 0
	v := newPluginView()
	release := make(chan struct{})
	v.render = func(name string, size, weight float64, scale int) (*image.RGBA, bool) {
		mu.Lock()
		asked++
		mu.Unlock()
		if size == 32 {
			<-release
		}
		return image.NewRGBA(image.Rect(0, 0, 8*scale, 8*scale)), true
	}
	var bm, bm2 *ui.Bitmap
	tt := ui.NewTester(func(c *ui.Context) {
		v.frame++
		if bm == nil {
			bm, _ = v.symbol(c, "play", 16, symbolMedium)
		}
	}, 100, 100)
	tt.Frame()
	waitFor := func(cond func() bool) {
		t.Helper()
		for i := 0; i < 200 && !cond(); i++ {
			time.Sleep(10 * time.Millisecond)
		}
	}
	ready := func() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.lastSymbol["play.fill"].bitmap != nil }
	waitFor(ready)
	if !ready() {
		t.Fatal("the symbol did not arrive")
	}
	var got1, got2, other *ui.Bitmap
	var stretch float64
	tt2 := ui.NewTester(func(c *ui.Context) {
		v.frame++
		got1, _ = v.symbol(c, "play", 16, symbolMedium)
		got2, _ = v.symbol(c, "play", 16, symbolMedium)
		other, stretch = v.symbol(c, "play", 32, symbolMedium)
	}, 100, 100)
	tt2.Frame()
	_ = bm2
	if got1 == nil || got1 != got2 {
		t.Fatal("the same symbol is not served from the cache")
	}
	if stretch != 2 {
		t.Fatalf("the stand-in is stretched %v, want 2 for 16 points shown at 32", stretch)
	}
	if other == nil || other != got1 {
		t.Fatal("a size still being drawn does not stand in with the last bitmap")
	}
	waitFor(func() bool { mu.Lock(); defer mu.Unlock(); return asked >= 2 })
	close(release)
	mu.Lock()
	defer mu.Unlock()
	if asked != 2 {
		t.Fatalf("the renderer ran %d times, want 2 (one for each size, none for a color)", asked)
	}
	if key := (symbolKey{"play.fill", 16, symbolMedium, symbolScale}); v.symbols[key] == nil {
		t.Fatal("the symbol key has no entry; it must not depend on a color")
	}
}

// TestPluginProgressBarNeedsNoViewBuilds runs a playing bar for seconds and
// counts the builds of the view: its motion is repaints, not builds.
func TestPluginProgressBarNeedsNoViewBuilds(t *testing.T) {
	tree := pluginTree(sdk.Column("root", sdk.Progress("dl", 0.2).Playing(0.01, 0)))
	builds := 0
	v := newPluginView()
	v.fetch = stubPluginFetch()
	v.render = func(string, float64, float64, int) (*image.RGBA, bool) { return nil, false }
	pal := lightPalette
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		if tree.Root.Children[0].At == 0 {
			tree.Root.Children[0].At = c.Now().UnixMilli()
		}
		drawPluginTree(c, v, &pal, tree, nil)
	}, 400, 300)
	tt.Frame()
	tt.Frame()
	before := builds
	tt.Advance(5 * time.Second)
	if got := v.progress["dl"]; got < 0.24 {
		t.Fatalf("the bar shows %v after 5s at 0.01 a second, want it to have moved to about 0.25", got)
	}
	if builds-before > 3 {
		t.Fatalf("the view was built %d times in 5s, want repaints only", builds-before)
	}
}

// TestPluginPictureSizeIsNotInTheKey draws one URL at two sizes and a
// resized tile: one fetch, never another.
func TestPluginPictureSizeIsNotInTheKey(t *testing.T) {
	var mu sync.Mutex
	fetched := map[string]int{}
	inner := stubPluginFetch()
	v := newPluginView()
	v.fetch = func(src pluginSource) (*ui.Bitmap, image.Image, error) {
		mu.Lock()
		fetched[src.url]++
		mu.Unlock()
		return inner(src)
	}
	v.render = func(string, float64, float64, int) (*image.RGBA, bool) { return nil, false }
	pal := lightPalette
	tree := func(tile int) *protocol.Tree {
		return pluginTree(sdk.Column("root", sdk.List("lib",
			protocol.Item{ID: "a", Title: "A", Image: "https://example.invalid/a.png"}).Grid(tile)))
	}
	cur := tree(160)
	tt := ui.NewTester(func(c *ui.Context) { drawPluginTree(c, v, &pal, cur, nil) }, 900, 700)
	tt.Frame()
	for _, tile := range []int{160, 161, 163, 199, 120, 240} {
		cur = tree(tile)
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	// The same URL as a list row, at another size.
	cur = pluginTree(sdk.Column("root", sdk.List("lib",
		protocol.Item{ID: "a", Title: "A", Image: "https://example.invalid/a.png"})))
	tt.Frame()
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if n := fetched["https://example.invalid/a.png"]; n != 1 {
		t.Fatalf("the picture was fetched %d times across sizes, want once", n)
	}
	for px, want := range map[int]int{1: 128, 128: 128, 129: 256, 322: 512, 900: 512} {
		if got := pluginSideBucket(px, pluginMaxImageSide); got != want {
			t.Errorf("bucket(%d) = %d, want %d", px, got, want)
		}
	}
	if got := pluginSideBucket(900, pluginMaxArtSide); got != 1024 {
		t.Errorf("art bucket(900) = %d, want 1024", got)
	}
}

// TestPluginPictureBudgetCountsLoading prunes pictures nothing asks for,
// loaded or not, down to the budget.
func TestPluginPictureBudgetCountsLoading(t *testing.T) {
	v := newPluginView()
	for i := range 3 * pluginPictureBudget {
		v.pictures[fmt.Sprint(i)] = &pluginPicture{loading: i%2 == 0, used: uint64(i)}
	}
	v.frame = uint64(3*pluginPictureBudget) + 1
	v.prunePictures()
	if n := len(v.pictures); n > pluginPictureBudget {
		t.Fatalf("%d pictures kept, want at most %d", n, pluginPictureBudget)
	}
}

// TestPluginSliderNeedsNoViewBuilds runs a playing slider for seconds and
// counts the builds of the view: its motion is repaints, not builds.
func TestPluginSliderNeedsNoViewBuilds(t *testing.T) {
	tree := pluginTree(sdk.Column("root", sdk.Slider("seek", 10, 0, 100).Playing(1, 0)))
	builds := 0
	v := newPluginView()
	v.fetch = stubPluginFetch()
	v.render = func(string, float64, float64, int) (*image.RGBA, bool) { return nil, false }
	pal := lightPalette
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		if n := tree.Root.Children[0]; n.At == 0 {
			n.At = c.Now().UnixMilli()
		}
		drawPluginTree(c, v, &pal, tree, nil)
	}, 400, 300)
	tt.Frame()
	tt.Frame()
	before := builds
	tt.Advance(5 * time.Second)
	if got := v.sliders["seek"].value; math.Abs(got-15) > 0.5 {
		t.Fatalf("the slider is at %v after 5s from 10 at 1 a second, want about 15", got)
	}
	if builds-before > 3 {
		t.Fatalf("the view was built %d times in 5s, want repaints only", builds-before)
	}
}

// A list row with a leading text shows it before the title, and the row
// that is on still takes a press.
func TestPluginListRowsWithLeadingAndActive(t *testing.T) {
	items := []protocol.Item{
		{ID: "t-0", Title: "First song", Subtitle: "Ada", Trailing: "3:04", Leading: "1"},
		{ID: "t-1", Title: "Second song", Subtitle: "Ada", Trailing: "2:10", Leading: "2", Active: true},
	}
	var got []pluginEvent
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.List("tracks", items...)).Grow(1)), func(ev pluginEvent) {
		got = append(got, ev)
	})
	for _, text := range []string{"First song", "Second song", "3:04", "1", "2"} {
		if !h.tt.HasText(text) {
			t.Fatalf("the list does not show %q; it shows %q", text, h.tt.Texts())
		}
	}
	if err := h.tt.Click("Second song"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(got, "tracks", protocol.KindSelect, "t-1") {
		t.Fatalf("no select for the active row: %v", got)
	}
}

// A slider with a width of its own, such as a volume, keeps it, where one
// without grows to fill its row.
func TestPluginSliderKeepsItsOwnWidth(t *testing.T) {
	width := func(slider *sdk.Node) float32 {
		h := newPluginHarness(t, pluginTree(sdk.Row("root", slider).Gap(10)), func(pluginEvent) {})
		r, ok := h.tt.Find("Slider")
		if !ok {
			t.Fatal("no slider")
		}
		return r.W
	}
	if w := width(sdk.Slider("volume", 70, 0, 100).Size(80, 0)); w != 80 {
		t.Fatalf("a slider of 80 is %v wide", w)
	}
	if w := width(sdk.Slider("seek", 10, 0, 100)); w <= 120 {
		t.Fatalf("a slider with no width is %v wide, want it to grow", w)
	}
}

// An icon button that is active draws, and takes a press like any other.
func TestPluginActiveIconButtonPresses(t *testing.T) {
	var got []pluginEvent
	h := newPluginHarness(t, pluginTree(sdk.Row("root",
		sdk.IconButton("shuffle", "shuffle").Size(28, 28).Active(true),
		sdk.IconButton("repeat", "repeat").Size(28, 28),
	)), func(ev pluginEvent) { got = append(got, ev) })
	if err := h.tt.Click("shuffle"); err != nil {
		t.Fatal(err)
	}
	if !hasPluginEvent(got, "shuffle", protocol.KindPress, nil) {
		t.Fatalf("no press for the active button: %v", got)
	}
}

// An icon with no words is named by its label, and a long total in a donut
// shrinks to the hole and is not cut.
func TestPluginIconTextLabelAndDonutFit(t *testing.T) {
	h := newPluginHarness(t, pluginTree(sdk.Column("root",
		sdk.Text("flame", "").Icon("flame").Color(protocol.ColorDanger).Label("Limit in 1d 4h"),
		sdk.Donut("donut", protocol.Segment{ID: "a", Value: 1, Color: protocol.ColorOrange}).Center("$1,234.5K", "dollars").Width(96),
	)), nil)
	if _, ok := h.tt.Find("Limit in 1d 4h"); !ok {
		t.Error("the icon-only text has no label")
	}
	if !h.tt.HasText("$1,234.5K") {
		t.Errorf("the donut total is cut: the tree shows %q", h.tt.Texts())
	}
}

// A donut slice is a sector of the ring: it stays between its radii and
// inside its angles.
func TestPluginDonutSectorPath(t *testing.T) {
	var v pluginView
	v.sector(50, 50, 30, 40, 0, math.Pi/2)
	b := v.ring.Bounds()
	if b.X < 49.9 || b.X+b.W > 90.1 || b.Y < 9.9 || b.Y+b.H > 50.1 {
		t.Errorf("a quarter sector at the top right is %+v", b)
	}
}

// boardTree is a pane of the parts a board is made of: pressable cards in
// columns that scroll sideways, a menu and a markdown block.
func boardTree(active string) *protocol.Tree {
	card := func(id, title string) *sdk.Node {
		return sdk.Column(id,
			sdk.Text(id+"-id", id).Role(protocol.RoleCaption),
			sdk.Text(id+"-title", title).Role(protocol.RoleStrong),
		).Pressable().Label(title).Surface(protocol.SurfaceCard).Padding(10).Gap(4)
	}
	column := func(id, title string, cards ...*sdk.Node) *sdk.Node {
		return sdk.Column(id, append([]*sdk.Node{sdk.Text(id+"-head", title).Role(protocol.RoleStrong)}, cards...)...).Width(260).Gap(8)
	}
	return pluginTree(sdk.Column("root",
		sdk.Menu("who", "Assignee",
			protocol.Item{ID: "me", Title: "Me", Active: active == "me"},
			protocol.Item{ID: "anyone", Title: "Anyone", Active: active == "anyone"},
		).Icon("person"),
		sdk.Row("cols",
			column("todo", "Todo", card("BRU-1", "Fix the pane"), card("BRU-2", "Write the docs")),
			column("doing", "In Progress", card("BRU-3", "Ship the board")),
			column("done", "Done"),
			column("late", "Later"),
		).ScrollX().Gap(12).Padding(8).Size(0, 160),
		sdk.Markdown("body", "# Heading one\n\nSome **bold** words and a [link](https://example.com/page).\n\n- first item\n- second item\n"),
	))
}

// A pressable card sends a press that names it, and a menu sends a select
// with the item chosen.
func TestPluginPressableAndMenuSendEvents(t *testing.T) {
	var mu sync.Mutex
	var got []pluginEvent
	h := newPluginHarness(t, boardTree("me"), func(ev pluginEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})
	if err := h.tt.Click("Fix the pane"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	pressed := hasPluginEvent(got, "BRU-1", protocol.KindPress, nil)
	mu.Unlock()
	if !pressed {
		t.Fatalf("no press for the card: %v", got)
	}
	if _, ok := h.tt.Find("Fix the pane"); !ok {
		t.Fatal("the card is not labeled")
	}

	if err := h.tt.Click("Assignee"); err != nil {
		t.Fatal(err)
	}
	items := h.tt.Menu()
	if !slices.Contains(items, "Me") || !slices.Contains(items, "Anyone") {
		t.Fatalf("the menu shows %q, want Me and Anyone", items)
	}
	if err := h.tt.ChooseMenuItem("Anyone"); err != nil {
		t.Fatal(err)
	}
	h.frames()
	mu.Lock()
	chosen := hasPluginEvent(got, "who", protocol.KindSelect, "anyone")
	mu.Unlock()
	if !chosen {
		t.Fatalf("no select for the menu item: %v", got)
	}
}

// A row that scrolls sideways lets the columns keep their widths and
// scrolls to the ones past its edge; its position is kept by its id.
func TestPluginRowScrollsSideways(t *testing.T) {
	h := newPluginHarness(t, boardTree("me"), nil)
	st := h.view.scroll["cols"]
	if st == nil || st.MaxX <= 0 {
		t.Fatalf("scroll = %+v, want content wider than the row", st)
	}
	r, ok := h.tt.Find("Todo")
	if !ok {
		t.Fatal("no first column")
	}
	h.tt.Wheel(r.X+10, r.Y+10, 400, 0)
	h.frames()
	if st.X <= 0 {
		t.Fatalf("X = %v after a wheel, want the row scrolled", st.X)
	}
	moved := st.X
	h.show(boardTree("anyone"))
	if h.view.scroll["cols"] != st || st.X != moved {
		t.Fatalf("the row lost its place on a render: X = %v, want %v", st.X, moved)
	}
}

// A markdown part shows headings, text and lists, and a link opens in the
// browser through the pane when the plugin may open links, else it goes to
// the plugin as a press with the link as its value.
func TestPluginMarkdownRendersAndLinks(t *testing.T) {
	var mu sync.Mutex
	var got []pluginEvent
	h := newPluginHarness(t, boardTree("me"), func(ev pluginEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})
	for _, text := range []string{"Heading one", "bold", "link", "first item", "second item"} {
		if !h.tt.HasText(text) {
			t.Fatalf("the markdown does not show %q; it shows %q", text, h.tt.Texts())
		}
	}
	if _, ok := h.tt.Find("Formatted text"); !ok {
		t.Fatal("the markdown part has no label")
	}

	// Without the permission the click goes to the plugin.
	if err := h.tt.Click("link"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	sent := hasPluginEvent(got, "body", protocol.KindPress, "https://example.com/page")
	mu.Unlock()
	if !sent {
		t.Fatalf("no press carrying the link: %v", got)
	}

	// With it the pane opens the link and the plugin hears nothing.
	var opened []string
	h.view.openLink = func(link string) bool { opened = append(opened, link); return true }
	got = nil
	if err := h.tt.Click("link"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opened, []string{"https://example.com/page"}) {
		t.Fatalf("opened %v, want the link", opened)
	}
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("the plugin heard %v for a link the pane opened", got)
	}

	// A link that is not a web address is never opened by the pane.
	for _, link := range []string{"file:///etc/passwd", "javascript:alert(1)", "#section", "mailto:a@b.c", "https://", "https://user@example.com/x", "https://user:pw@example.com/"} {
		if pluginSafeLink(link) {
			t.Errorf("%q is a safe link", link)
		}
	}
	if !pluginSafeLink("http://example.com/x") {
		t.Error("an http link is not safe")
	}
	// A markdown part that goes away takes its options with it.
	h.show(pluginTree(sdk.Column("root", sdk.Text("t", "gone"))))
	if len(h.view.markdown) != 0 {
		t.Fatalf("markdown = %v, want a removed part's state dropped", h.view.markdown)
	}
}

// TestPluginBoardPartShots writes shots of a pressable card, a menu and a
// markdown block, in both appearances, into the directory
// PLUGIN_SHOT_DIR names, as linear-parts-<part>-<appearance>.png.
func TestPluginBoardPartShots(t *testing.T) {
	dir := os.Getenv("PLUGIN_SHOT_DIR")
	if dir == "" {
		t.Skip("PLUGIN_SHOT_DIR is not set")
	}
	write := func(name string, img image.Image) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		pal, suffix := lightPalette, "light"
		if dark {
			pal, suffix = darkPalette, "dark"
		}
		shoot := func(part string, w, h int, tree *protocol.Tree, act func(*pluginHarness)) {
			ph := newPluginHarness(t, tree, nil)
			ph.pal = pal
			ph.tt = ui.NewTester(func(c *ui.Context) {
				ui.Column(c.Key("shot")).Fill().Background(pal.card).Padding(12).Children(func() {
					drawPluginTree(c, ph.view, &pal, ph.tree, nil)
				})
			}, w, h)
			ph.tt.SetDark(dark)
			ph.frames()
			if act != nil {
				act(ph)
			}
			write("linear-parts-"+part+"-"+suffix+".png", ph.tt.Image())
		}
		// A pressable card, with the pointer over a second one.
		cards := pluginTree(sdk.Column("root",
			sdk.Column("c1", sdk.Text("c1-id", "BRU-12").Role(protocol.RoleCaption),
				sdk.Text("c1-title", "Show the board's cards in a pane").Role(protocol.RoleStrong),
				sdk.Text("c1-sub", "In Progress · High").Role(protocol.RoleCaption).Color(protocol.ColorMuted),
			).Pressable().Label("card one").Surface(protocol.SurfaceCard).Padding(12).Gap(4),
			sdk.Column("c2", sdk.Text("c2-id", "BRU-13").Role(protocol.RoleCaption),
				sdk.Text("c2-title", "Hovered card").Role(protocol.RoleStrong),
			).Pressable().Label("card two").Surface(protocol.SurfaceCard).Padding(12).Gap(4),
		).Gap(8))
		shoot("card", 320, 190, cards, func(ph *pluginHarness) {
			if r, ok := ph.tt.Find("card two"); ok {
				ph.tt.Move(r.X+r.W/2, r.Y+r.H/2)
				ph.frames()
			}
		})
		shoot("menu", 320, 190, boardTree("me"), func(ph *pluginHarness) {
			if err := ph.tt.Click("Assignee"); err == nil {
				ph.frames()
			}
		})
		shoot("markdown", 360, 260, pluginTree(sdk.Column("root", sdk.Markdown("body",
			"# Heading one\n\nSome **bold** words, `code` and a [link](https://example.com).\n\n## A list\n\n- first item\n- second item\n\n> A quote\n"))), nil)
	}
}

// --- the parts a plugin needs to match a built-in view: a header that stays,
// a menu on a right click, keys, chips, and sides that scroll alone ---

func pluginScrollTree(header, root *sdk.Node, keys ...string) *protocol.Tree {
	tree := root.ScrollingTreeUnder(header)
	tree.Keys = keys
	return &tree
}

func pluginManyRows(prefix string, n int) []*sdk.Node {
	var rows []*sdk.Node
	for i := 0; i < n; i++ {
		rows = append(rows, sdk.Text(fmt.Sprintf("%s%d", prefix, i), fmt.Sprintf("%s row %d", prefix, i)))
	}
	return rows
}

func TestPluginHeaderStaysWhenTheBodyScrolls(t *testing.T) {
	tree := pluginScrollTree(
		sdk.Row("head", sdk.Text("head-t", "Sticky header")).Padding(8),
		sdk.Column("root", pluginManyRows("a", 120)...),
	)
	if err := tree.ValidateTree(); err != nil {
		t.Fatal(err)
	}
	h := newPluginHarness(t, tree, nil)
	head, ok := h.tt.Find("Sticky header")
	if !ok {
		t.Fatal("the header is not drawn")
	}
	first, ok := h.tt.Find("a row 0")
	if !ok {
		t.Fatal("the first row is not drawn")
	}
	if first.Y <= head.Y {
		t.Fatalf("the body (%v) starts above the header (%v)", first.Y, head.Y)
	}
	far, _ := h.tt.Find("a row 40")
	h.tt.Scroll(300, 400, 0, 600)
	h.frames()
	again, ok := h.tt.Find("Sticky header")
	if !ok || again.Y != head.Y {
		t.Fatalf("the header moved with the scroll: %v then %v", head.Y, again.Y)
	}
	if moved, ok := h.tt.Find("a row 40"); !ok || moved.Y >= far.Y {
		t.Fatalf("the body did not scroll: %v then %v", far.Y, moved.Y)
	}
}

func TestPluginRightClickOpensTheContextMenu(t *testing.T) {
	var got []pluginEvent
	card := sdk.Column("card", sdk.Text("card-t", "BRU-1 Fix it")).Pressable().Label("card one").Surface(protocol.SurfaceCard).Padding(10).
		ContextMenu(
			sdk.MenuItem("open", "Open"),
			protocol.Item{ID: "ext", Title: "Open in Linear", Enabled: boolPtr(false)},
			sdk.Separator(),
			sdk.Submenu("move", "Move to",
				protocol.Item{ID: "mv:todo", Title: "Todo", Active: true, Enabled: boolPtr(false)},
				sdk.MenuItem("mv:done", "Done"),
			),
		)
	tree := pluginTree(sdk.Column("root", card))
	if err := tree.ValidateTree(); err != nil {
		t.Fatal(err)
	}
	h := newPluginHarness(t, tree, func(ev pluginEvent) { got = append(got, ev) })
	if err := h.tt.RightClick("card one"); err != nil {
		t.Fatal(err)
	}
	h.frames()
	menu := h.tt.Menu()
	if !slices.Contains(menu, "Open") || !slices.Contains(menu, "Move to") {
		t.Fatalf("menu = %v", menu)
	}
	if err := h.tt.ChooseMenuItem("Move to", "Done"); err != nil {
		t.Fatal(err)
	}
	h.frames()
	if len(got) != 1 || got[0] != (pluginEvent{Node: "card", Kind: protocol.KindContext, Value: "mv:done"}) {
		t.Fatalf("events = %+v, want one context event naming the card and the entry", got)
	}

	// An entry a plugin turned off sends nothing.
	got = nil
	if err := h.tt.RightClick("card one"); err != nil {
		t.Fatal(err)
	}
	h.frames()
	_ = h.tt.ChooseMenuItem("Open in Linear")
	h.frames()
	if len(got) != 0 {
		t.Fatalf("a disabled entry sent %+v", got)
	}
	h.tt.CloseMenu()

	// A plain press still presses.
	if err := h.tt.Click("card one"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != protocol.KindPress {
		t.Fatalf("a click sent %+v, want a press", got)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestPluginKeysAreHeardOnlyWhenAskedAndAllowed(t *testing.T) {
	var got []pluginEvent
	tree := pluginScrollTree(
		sdk.Row("head", sdk.SearchField("q", "Search issues")),
		sdk.Column("root", sdk.Text("t", "body")),
		"j", "escape",
	)
	h := newPluginHarness(t, tree, func(ev pluginEvent) { got = append(got, ev) })
	keys := func() []string {
		var out []string
		for _, ev := range got {
			if ev.Kind == protocol.KindKey {
				out = append(out, fmt.Sprint(ev.Value))
			}
		}
		return out
	}

	// The pane is not the focused one: no key is read.
	h.view.keysOn = false
	h.tt.Key(0, ui.KeyJ)
	h.frames()
	if len(keys()) != 0 {
		t.Fatalf("an unfocused pane heard %v", keys())
	}

	h.view.keysOn = true
	h.tt.Key(0, ui.KeyJ)
	h.frames()
	if !slices.Equal(keys(), []string{"j"}) {
		t.Fatalf("heard %v, want j", keys())
	}
	if got[0].Node != "keys" {
		t.Fatalf("the key came from %q", got[0].Node)
	}
	// A key the tree did not ask for stays with the app.
	got = nil
	h.tt.Key(0, ui.KeyK)
	h.tt.Key(0, ui.KeyDown)
	h.tt.Key(0, ui.KeyEnter)
	h.frames()
	if len(keys()) != 0 {
		t.Fatalf("heard %v, which the tree did not ask for", keys())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.frames()
	if !slices.Equal(keys(), []string{"escape"}) {
		t.Fatalf("heard %v, want escape", keys())
	}

	// A field that takes the typing keeps its letters.
	got = nil
	if err := h.tt.Click("Search issues"); err != nil {
		t.Fatal(err)
	}
	h.frames()
	h.tt.Type("j")
	h.tt.Key(0, ui.KeyJ)
	h.frames()
	if len(keys()) != 0 {
		t.Fatalf("a search field's J reached the plugin as a key: %v", keys())
	}
}

func TestPluginPaneKeysFollowTheFocusedPane(t *testing.T) {
	a := testApp(t)
	s := &proto.Session{ID: "s9", Panes: []proto.Pane{{ID: "term"}, {ID: "plug"}}, Focus: "term"}
	if a.pluginKeysOn(s, &s.Panes[1], true) {
		t.Fatal("a plugin pane next to the focused terminal heard keys")
	}
	if a.pluginKeysOn(s, &s.Panes[0], true) != true {
		t.Fatal("the focused pane hears keys")
	}
	s.Focus = "plug"
	if !a.pluginKeysOn(s, &s.Panes[1], true) || a.pluginKeysOn(s, &s.Panes[0], true) {
		t.Fatal("the keys follow the focus")
	}
	if a.pluginKeysOn(s, &s.Panes[1], false) {
		t.Fatal("a pane without the keyboard heard keys")
	}
	a.renaming = "s9"
	if a.pluginKeysOn(s, &s.Panes[1], true) {
		t.Fatal("a rename field takes the keys")
	}
}

func TestPluginCopyReachesTheNextFrame(t *testing.T) {
	a := &App{}
	a.pluginCopy("")
	if a.copyText != "" {
		t.Fatal("an empty text was queued")
	}
	a.pluginCopy("BRU-1")
	if a.copyText != "BRU-1" {
		t.Fatalf("copyText = %q", a.copyText)
	}
}

func TestPluginChipsHexAndSidesThatScroll(t *testing.T) {
	left := sdk.Column("main", append(pluginManyRows("m", 80),
		sdk.Text("chip", "bug").Chip("#E5484D").Lines(1),
		sdk.Text("chip2", "2").Icon("blocked").Chip(protocol.ChipNeutral),
		sdk.Text("hex", "In Progress").Icon("circle-half").Hex("#F2C94C"),
	)...).ScrollY().Grow(1)
	right := sdk.Column("side", pluginManyRows("s", 80)...).ScrollY().Width(240)
	tree := pluginTree(sdk.Row("root", left, right).Grow(1).Gap(10))
	if err := tree.ValidateTree(); err != nil {
		t.Fatal(err)
	}
	h := newPluginHarness(t, tree, nil)
	for _, dark := range []bool{false, true} {
		h.tt.SetDark(dark)
		if dark {
			h.pal = darkPalette
		}
		h.frames()
		if !h.tt.HasText("m row 0") || !h.tt.HasText("s row 0") {
			t.Fatalf("dark=%v: both sides draw", dark)
		}
	}
	m0, _ := h.tt.Find("m row 0")
	s0, _ := h.tt.Find("s row 0")
	if s0.X <= m0.X+m0.W {
		t.Fatalf("the sides stand side by side: %v and %v", m0, s0)
	}
	// Scrolling the right side leaves the left where it was.
	sFar, _ := h.tt.Find("s row 40")
	mFar, _ := h.tt.Find("m row 40")
	h.tt.Scroll(s0.X+10, s0.Y+10, 0, 500)
	h.frames()
	if m1, _ := h.tt.Find("m row 40"); m1.Y != mFar.Y {
		t.Fatalf("the left side moved with the right: %v then %v", mFar.Y, m1.Y)
	}
	if s1, ok := h.tt.Find("s row 40"); !ok || s1.Y >= sFar.Y {
		t.Fatalf("the right side did not scroll: %v then %v", sFar.Y, s1.Y)
	}
}

func TestPluginChipFillsItsWordsAndAColorTintsIt(t *testing.T) {
	// A chip is a soft fill under its words: the pixels under it differ
	// from the pane's, and a hex color shows in the text.
	shot := func(node *sdk.Node) *image.RGBA {
		h := newPluginHarness(t, pluginTree(sdk.Column("root", node).Padding(20)), nil)
		return h.tt.Image()
	}
	plain := shot(sdk.Text("t", "bug"))
	chip := shot(sdk.Text("t", "bug").Chip("#E5484D"))
	if reflect.DeepEqual(plain.Pix, chip.Pix) {
		t.Fatal("a chip draws like plain text")
	}
	// The chip's fill is red-ish at the left edge of its words.
	h := newPluginHarness(t, pluginTree(sdk.Column("root", sdk.Text("t", "bug").Chip("#E5484D")).Padding(20)), nil)
	r, ok := h.tt.Find("bug")
	if !ok {
		t.Fatal("no chip text")
	}
	img := h.tt.Image()
	px := img.RGBAAt(int(r.X)-4, int(r.Y+r.H/2))
	if px.R <= px.B {
		t.Fatalf("the chip's fill is %v, want it red-tinted", px)
	}
}

func TestPluginSelectedRowScrollsIntoViewOnce(t *testing.T) {
	rows := func(selected int) *protocol.Tree {
		var kids []*sdk.Node
		for i := 0; i < 100; i++ {
			kids = append(kids, sdk.Row(fmt.Sprintf("r%d", i), sdk.Text(fmt.Sprintf("r%d-t", i), fmt.Sprintf("issue %d", i))).
				Pressable().Label(fmt.Sprintf("row %d", i)).Active(i == selected).Padding(4))
		}
		return pluginTree(sdk.Column("root", kids...))
	}
	h := newPluginHarness(t, pluginScrollTree(sdk.Row("head", sdk.Text("ht", "head")), sdk.Column("root", pluginManyRows("q", 1)...)), nil)
	h.show(func() *protocol.Tree { t := rows(-1).Root; return &protocol.Tree{Root: t, Scroll: true} }())
	if r, _ := h.tt.Find("issue 70"); r.W == 0 {
		// Off screen, as the test needs it.
	} else {
		t.Fatalf("issue 70 is on screen at the start: %v", r)
	}
	h.show(func() *protocol.Tree { t := rows(70).Root; return &protocol.Tree{Root: t, Scroll: true} }())
	r, ok := h.tt.Find("issue 70")
	if !ok || r.W == 0 {
		t.Fatalf("the selected row did not scroll into view: %v", r)
	}
}

// TestLinearParityShots draws the trees the Linear plugin sends (written by
// its TestWriteParityTrees into LINEAR_TREE_DIR) in the tester, in both
// appearances, into PLUGIN_SHOT_DIR as linear-parity-<shot>-<appearance>.png:
// the list with a selected row and one under the pointer, the board with the
// chips on its cards, and the wide pane with the issue beside the issues.
func TestLinearParityShots(t *testing.T) {
	dir, trees := os.Getenv("PLUGIN_SHOT_DIR"), os.Getenv("LINEAR_TREE_DIR")
	if dir == "" || trees == "" {
		t.Skip("PLUGIN_SHOT_DIR and LINEAR_TREE_DIR are not both set")
	}
	load := func(name string) *protocol.Tree {
		raw, err := os.ReadFile(filepath.Join(trees, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var tree protocol.Tree
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatal(err)
		}
		if err := tree.ValidateTree(); err != nil {
			t.Fatal(err)
		}
		return &tree
	}
	for _, dark := range []bool{false, true} {
		pal, suffix := lightPalette, "light"
		if dark {
			pal, suffix = darkPalette, "dark"
		}
		shoot := func(name string, w, h int, act func(*pluginHarness)) {
			ph := newPluginHarness(t, load(name), nil)
			ph.pal = pal
			ph.view.keysOn = true
			ph.tt = ui.NewTester(func(c *ui.Context) {
				ui.Column(c.Key("shot")).Fill().Background(pal.card).Children(func() {
					drawPluginTree(c, ph.view, &pal, ph.tree, nil)
				})
			}, w, h)
			ph.tt.SetDark(dark)
			ph.frames()
			if act != nil {
				act(ph)
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, ph.tt.Image()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "linear-parity-"+name+"-"+suffix+".png"), buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		shoot("list", 560, 520, func(ph *pluginHarness) {
			// The pointer over a row that has the issue menu.
			if r, ok := ph.tt.Find("BRU-4 Issue number 4"); ok {
				ph.tt.Move(r.X+r.W/2, r.Y+r.H/2)
				ph.frames()
				if err := ph.tt.RightClick("BRU-4 Issue number 4"); err != nil {
					t.Fatal(err)
				}
				ph.frames()
				if menu := ph.tt.Menu(); !slices.Contains(menu, "Open Issue") || !slices.Contains(menu, "Move To") {
					t.Fatalf("the row's menu = %v", menu)
				}
				ph.tt.CloseMenu()
			}
		})
		shoot("board", 600, 560, nil)
		shoot("wide", 1000, 620, nil)
	}
}
