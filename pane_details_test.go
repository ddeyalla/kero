package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// In the chooser, Shift opens the pane below rather than beside: the
// letter with Shift, Return with Shift, or a click with Shift.
func TestChooserShiftOpensBelow(t *testing.T) {
	// Pane a has no room for two side by side, and room for two down.
	open := func(t *testing.T, pick func(tt *ui.Tester)) string {
		a := testApp(t)
		a.setFocus(a.session("s1"), "a")
		tt := ui.NewTester(a.view, 1000, 700)
		frames(tt)
		a.run("pane.new", nil)
		frames(tt)
		if !tt.HasText("Hold to open it below") {
			t.Fatalf("the chooser says nothing of Shift; texts %q", tt.Texts())
		}
		pick(tt)
		tt.Frame()
		if a.chooser.open {
			t.Fatal("the chooser stayed open")
		}
		if a.pendingToast != "" || tt.HasText("Not enough room for another pane here") {
			return "Not enough room for another pane here"
		}
		return ""
	}
	if got := open(t, func(tt *ui.Tester) { tt.Key(0, ui.KeyE) }); got != "Not enough room for another pane here" {
		t.Fatalf("E opened beside: want the no-room toast, got %q", got)
	}
	if got := open(t, func(tt *ui.Tester) { tt.Key(ui.Shift, ui.KeyE) }); got != "" {
		t.Fatalf("Shift-E did not open below: %q", got)
	}
	if got := open(t, func(tt *ui.Tester) { tt.Key(ui.Shift, ui.KeyEnter) }); got != "" {
		t.Fatalf("Shift-Return did not open below: %q", got)
	}
	if got := open(t, func(tt *ui.Tester) { tt.ClickWith(ui.Shift, "Files") }); got != "" {
		t.Fatalf("a Shift-click did not open below: %q", got)
	}
	// Held, Shift says so.
	a := testApp(t)
	tt := ui.NewTester(a.view, 1280, 720)
	a.run("pane.new", nil)
	frames(tt)
	tt.HoldModifiers(ui.Shift)
	frames(tt)
	if !tt.HasText("Open below this pane") {
		t.Errorf("Shift held, the title did not change; texts %q", tt.Texts())
	}
	shot(t, tt, "pane-chooser-below-light")
}

// In a Browser pane, the text's size is the page's, Command-R reloads,
// and Command-L selects the address.
func TestBrowserKeys(t *testing.T) {
	a := testBrowser(t)
	s := a.session("s1")
	b := &s.Panes[1]
	b.Target = "localhost:5173"
	a.setFocus(s, "b")
	tt := ui.NewTester(a.view, 1280, 720)
	frames(tt)
	st := a.browsers["b"]
	font := a.set.FontSize
	a.run("text.bigger", nil)
	a.run("text.bigger", nil)
	if st.zoom != 1.25 || a.set.FontSize != font {
		t.Fatalf("bigger twice: page %v, terminal font %v (was %v)", st.zoom, a.set.FontSize, font)
	}
	frames(tt)
	if !tt.HasText("125%") {
		t.Errorf("the address shows no zoom; texts %q", tt.Texts())
	}
	shot(t, tt, "browser-zoom-light")
	a.run("text.smaller", nil)
	if st.zoom != 1.15 {
		t.Fatalf("smaller: %v", st.zoom)
	}
	for range 20 {
		a.run("text.smaller", nil)
	}
	if st.zoom != 0.5 {
		t.Fatalf("smallest: %v", st.zoom)
	}
	if err := tt.Click("Actual Size"); err != nil {
		t.Fatal(err)
	}
	frames(tt)
	if st.zoom != 1 || tt.HasText("100%") {
		t.Fatalf("the chip did not make the page its own size: %v", st.zoom)
	}
	a.run("text.bigger", nil)
	a.run("text.reset", nil)
	if st.zoom != 1 {
		t.Fatalf("Actual Size: %v", st.zoom)
	}
	// Command-R is the page's here, not Rename Session.
	a.run("session.rename", nil)
	frames(tt)
	if a.renaming != "" {
		t.Fatal("Command-R in a Browser pane renamed the session")
	}
	// Command-L, from the terminal beside, goes to the page's address.
	a.setFocus(s, "a")
	a.paneRef("a").Focus()
	frames(tt)
	a.run("browser.location", nil)
	frames(tt)
	if a.focusOf(s) != "b" || !st.editing {
		t.Fatalf("Command-L: focus %q, address editing %v", a.focusOf(s), st.editing)
	}
	// Elsewhere, the text is the terminal's again.
	a.setFocus(s, "a")
	a.run("text.bigger", nil)
	if a.set.FontSize == font {
		t.Fatal("Bigger in a terminal did nothing")
	}
}

// The tree's header is as high as the file's, with Find in it.
func TestFilesTreeHeader(t *testing.T) {
	a, dir := testFiles(t)
	a.st.Sessions[0].Layout.Ratio = 0.3
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	a.openFile(a.files["b"], filepath.Join(dir, "main.go"))
	frames(tt)
	find, ok1 := tt.Find("Find a file in " + filepath.Base(dir))
	file, ok2 := tt.Find("7 lines")
	if !ok1 || !ok2 {
		t.Fatalf("no header; texts %q", tt.Texts())
	}
	if d := find.Y + find.H/2 - (file.Y + file.H/2); d > 2 || d < -2 {
		t.Errorf("the tree's header is %v off the file's", d)
	}
	shot(t, tt, "files-header-light")
}

// Find looks through the folder's files, by name before path; Return
// reads the first, which the tree shows.
func TestFilesFind(t *testing.T) {
	a, dir := testFiles(t)
	s := a.session("s1")
	a.setFocus(s, "b")
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	st := a.files["b"]
	a.run("find.open", nil)
	frames(tt)
	if !st.finding {
		t.Fatal("Find did not go to the field")
	}
	tt.Type("gui")
	frames(tt)
	if !tt.HasText("guide.md") || tt.HasText("main.go") {
		t.Fatalf("found the wrong files; texts %q", tt.Texts())
	}
	shot(t, tt, "files-find-light")
	tt.Key(0, ui.KeyEnter)
	frames(tt)
	guide := filepath.Join(dir, "docs", "guide.md")
	if st.open != guide || st.query != "" || !st.expanded[filepath.Join(dir, "docs")] || st.selected != guide {
		t.Fatalf("Return read %q, query %q", st.open, st.query)
	}
	// Return with nothing typed reads nothing found before.
	a.openFile(st, filepath.Join(dir, "main.go"))
	a.run("find.open", nil)
	frames(tt)
	tt.Key(0, ui.KeyEnter)
	frames(tt)
	if st.open != filepath.Join(dir, "main.go") {
		t.Fatalf("Return in an empty field read %q", st.open)
	}
	// Esc clears what was typed.
	a.run("find.open", nil)
	frames(tt)
	tt.Type("zzzz")
	frames(tt)
	if !tt.HasText("No files match") {
		t.Errorf("no word of nothing found; texts %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	frames(tt)
	if st.query != "" {
		t.Fatalf("Esc kept %q", st.query)
	}
}

func TestFindFiles(t *testing.T) {
	root := "/r"
	index := []string{"/r/internal/settings/config.go", "/r/cmd/conf/main.go", "/r/config.go", "/r/docs/configuration.md"}
	got := findFiles(root, index, "config", 10)
	if len(got) < 3 || got[0] != "/r/config.go" || got[1] != "/r/internal/settings/config.go" {
		t.Fatalf("got %q", got)
	}
	if got := findFiles(root, index, "conf main", 10); len(got) != 1 || got[0] != "/r/cmd/conf/main.go" {
		t.Fatalf("by path: %q", got)
	}
	dir := t.TempDir()
	for _, name := range []string{"a.go", "node_modules/x/i.js", ".git/HEAD", "target/debug/b", "src/c.go"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, nil, 0o644)
	}
	files := indexDir(dir, 100)
	if len(files) != 2 {
		t.Fatalf("indexed %q", files)
	}
	if len(indexDir(dir, 1)) != 1 {
		t.Fatal("the index passed its limit")
	}
}

// The folders unfolded unfold again after a restart, until the pane
// closes.
func TestFilesRememberFolders(t *testing.T) {
	a, dir := testFiles(t)
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	a.toggleDir(a.files["b"], filepath.Join(dir, "docs"))
	if got := a.prefs.Files["b"]; len(got) != 1 || got[0] != "docs" {
		t.Fatalf("kept %q", got)
	}
	a.flushPrefs()
	again := newApp()
	again.st = a.st
	st := again.filesStateOf(again.session("s1"), &again.st.Sessions[0].Panes[1])
	if !st.expanded[filepath.Join(dir, "docs")] {
		t.Fatal("docs did not unfold again")
	}
	again.loaded = true
	again.st.Sessions[0].Panes = again.st.Sessions[0].Panes[:1]
	again.sweepViews()
	again.flushPrefs()
	if len(newApp().prefs.Files) != 0 {
		t.Fatal("a closed pane's folders stayed")
	}
}

// A link to a heading scrolls to it, in the file and from another.
func TestMarkdownHeadingLinks(t *testing.T) {
	a, dir := testFiles(t)
	long := "# Top\n\n[Jump to usage](#usage)\n\n" + strings.Repeat("Words.\n\n", 120) + "## Usage\n\nHow to use it.\n\n" + strings.Repeat("More.\n\n", 80)
	os.WriteFile(filepath.Join(dir, "LONG.md"), []byte(long), 0o644)
	os.WriteFile(filepath.Join(dir, "docs", "guide.md"), []byte("# Guide\n\n[Long usage](../LONG.md#usage)\n"), 0o644)
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	st := a.files["b"]
	a.openFile(st, filepath.Join(dir, "LONG.md"))
	frames(tt)
	// A link is a run of its paragraph's text: a click on its start.
	clickLink := func(text string) {
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("no link %q", text)
		}
		tt.ClickAt(r.X+8, r.Y+r.H/2)
		frames(tt)
	}
	clickLink("Jump to usage")
	if r, ok := tt.Find("How to use it."); !ok || r.Y > 300 {
		t.Fatalf("the heading is not at the top: %v", r)
	}
	shot(t, tt, "files-heading-link-light")
	a.openFile(st, filepath.Join(dir, "docs", "guide.md"))
	frames(tt)
	clickLink("Long usage")
	if st.open != filepath.Join(dir, "LONG.md") {
		t.Fatalf("the link opened %q", st.open)
	}
	if r, ok := tt.Find("How to use it."); !ok || r.Y > 300 {
		t.Fatalf("the other file's heading is not at the top: %v", r)
	}
}

// A new pane grows from its edge and fades in; one closing gives its
// room back before it goes.
func TestPanesGlideInAndOut(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1600, 800)
	frames(tt)
	s := a.session("s1")
	ns := *s
	ns.Panes = append(slices.Clone(s.Panes), proto.Pane{ID: "c", Shell: "zsh", Dir: "/tmp", Status: proto.Idle, Progress: -1})
	ns.Layout = s.Layout.Clone()
	ns.Layout.B = &proto.Layout{Row: false, Ratio: 0.5, A: ns.Layout.B, B: &proto.Layout{Pane: "c"}}
	ns.Focus = "c"
	a.focusNew(ns)
	if !a.growing["c"] || a.opening["c"] != 1 {
		t.Fatal("the new pane does not grow in")
	}
	tt.Frame()
	tt.Advance(40 * time.Millisecond)
	tt.Frame()
	if h := a.cardSize["c"].H; h <= 0 || h >= a.cardSize["b"].H {
		t.Errorf("mid-way, the new pane is %v high beside %v", h, a.cardSize["b"].H)
	}
	tt.Advance(time.Second)
	frames(tt)
	if a.growing["c"] {
		t.Fatal("the pane kept growing")
	}
	if d := a.cardSize["c"].H - a.cardSize["b"].H; d > 2 || d < -2 {
		t.Errorf("settled, the panes are %v and %v high", a.cardSize["b"].H, a.cardSize["c"].H)
	}
	// Closing: the pane shrinks away.
	a.closing = map[string]bool{"c": true}
	tt.Frame()
	tt.Advance(time.Second)
	frames(tt)
	if h := a.cardSize["c"].H; h > 1 {
		t.Errorf("the closing pane is still %v high", h)
	}
}

func BenchmarkFindFiles(b *testing.B) {
	index := make([]string, 0, filesIndexed)
	for i := range filesIndexed {
		index = append(index, "/repo/src/module"+strconv.Itoa(i%300)+"/sub/component_"+strconv.Itoa(i)+".tsx")
	}
	for b.Loop() {
		findFiles("/repo", index, "compbtn", filesFound)
	}
}
