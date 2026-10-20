package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// testFiles is the session of testApp with pane b showing the files of
// a small project.
func testFiles(t *testing.T) (*App, string) {
	t.Helper()
	a := testApp(t)
	dir := t.TempDir()
	write := func(name, text string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Shibu Code\n\nA terminal for coding agents. See [the guide](docs/guide.md).\n\n- Panes show anything\n- **Tasks** from Linear\n\n```go\nfunc main() {}\n```\n")
	write("docs/guide.md", "# Guide\n\nRead me.\n")
	write("main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	write("node_modules/left-pad/index.js", "module.exports = 1\n")
	write("data.bin", "\x00\x01\x02binary")
	img := image.NewRGBA(image.Rect(0, 0, 40, 24))
	for x := range 40 {
		for y := range 24 {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: 120, B: 200, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, img)
	f.Close()
	b := &a.st.Sessions[0].Panes[1]
	b.View, b.Shell, b.Dir = proto.PaneFiles, "", dir
	return a, dir
}

// The tree is the folder's, its folders first, without node_modules.
func TestFilesPaneTree(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a, _ := testFiles(t)
		tt := ui.NewTester(a.view, 1280, 720)
		tt.SetDark(dark)
		frames(tt)
		for _, want := range []string{"Files", "docs", "README.md", "main.go", "logo.png"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		if tt.HasText("node_modules") {
			t.Errorf("dark=%v: node_modules shows", dark)
		}
		shot(t, tt, map[bool]string{false: "files-tree-light", true: "files-tree-dark"}[dark])
	}
}

// Markdown reads as it renders, or as its text; its links to files of
// the folder read them in the pane, and none leads out of it.
func TestFilesPaneMarkdown(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a, dir := testFiles(t)
		a.st.Sessions[0].Layout.Ratio = 0.3
		tt := ui.NewTester(a.view, 1600, 820)
		tt.SetDark(dark)
		frames(tt)
		st := a.files["b"]
		a.openFile(st, filepath.Join(dir, "README.md"))
		frames(tt)
		for _, want := range []string{"Shibu Code", "Panes show anything", "README.md", "10 lines"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		if tt.HasText("# Shibu Code") {
			t.Errorf("dark=%v: Markdown showed as its text", dark)
		}
		shot(t, tt, map[bool]string{false: "files-markdown-light", true: "files-markdown-dark"}[dark])
	}
	a, dir := testFiles(t)
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	st := a.files["b"]
	a.openFile(st, filepath.Join(dir, "README.md"))
	st.source = true
	frames(tt)
	if !tt.HasText("# Shibu Code\n\nA terminal for coding agents. See [the guide](docs/guide.md).\n\n- Panes show anything\n- **Tasks** from Linear\n\n```go\nfunc main() {}\n```") {
		t.Errorf("Show Source did not show the text; texts %q", tt.Texts())
	}
	a.fileLink(st, dir, "../outside.md")
	a.fileLink(st, dir, "javascript:alert(1)")
	if st.open != filepath.Join(dir, "README.md") {
		t.Fatalf("a link led to %q", st.open)
	}
	a.fileLink(st, dir, "docs/guide.md#top")
	if st.open != filepath.Join(dir, "docs", "guide.md") {
		t.Fatalf("the guide's link opened %q", st.open)
	}
}

// Code shows with its line numbers; a binary file and an image say what
// they are.
func TestFilesPaneFiles(t *testing.T) {
	a, dir := testFiles(t)
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	st := a.files["b"]
	a.openFile(st, filepath.Join(dir, "main.go"))
	frames(tt)
	if !tt.HasText("1\n2\n3\n4\n5\n6\n7") || !tt.HasText("7 lines") {
		t.Errorf("no line numbers; texts %q", tt.Texts())
	}
	shot(t, tt, "files-code-light")
	a.openFile(st, filepath.Join(dir, "data.bin"))
	frames(tt)
	if !tt.HasText("Not a text file") {
		t.Errorf("a binary file showed; texts %q", tt.Texts())
	}
	a.openFile(st, filepath.Join(dir, "logo.png"))
	frames(tt)
	if st.file == nil || st.file.image == nil {
		t.Fatal("the image did not load")
	}
}

// On a narrow pane the file takes the tree's place, and Esc brings the
// tree back; the arrows and Return work the tree.
func TestFilesPaneNarrowAndKeys(t *testing.T) {
	a, dir := testFiles(t)
	tt := ui.NewTester(a.view, 1000, 720)
	frames(tt)
	st := a.files["b"]
	a.setFocus(a.session("s1"), "b")
	a.paneRef("b").Focus()
	frames(tt)
	// docs, data.bin, logo.png, main.go, README.md: by name, whatever
	// its case.
	for range 5 {
		tt.Key(0, ui.KeyDown)
		tt.Frame()
	}
	tt.Key(0, ui.KeyEnter)
	frames(tt)
	if st.open != filepath.Join(dir, "README.md") {
		t.Fatalf("Return opened %q, selected %q", st.open, st.selected)
	}
	if !tt.HasText("All Files") || tt.HasText("main.go") {
		t.Errorf("the narrow pane kept its tree; texts %q", tt.Texts())
	}
	shot(t, tt, "files-narrow-light")
	tt.Key(0, ui.KeyEscape)
	frames(tt)
	if st.open != "" || !tt.HasText("main.go") {
		t.Fatalf("Esc left the file open")
	}
}

// The file read is the pane's Target, and opens again from it.
func TestFilesPaneOpensItsTarget(t *testing.T) {
	a, dir := testFiles(t)
	a.st.Sessions[0].Panes[1].Target = filepath.Join(dir, "docs", "guide.md")
	tt := ui.NewTester(a.view, 1600, 820)
	frames(tt)
	st := a.files["b"]
	if st.open != filepath.Join(dir, "docs", "guide.md") || !st.expanded[filepath.Join(dir, "docs")] {
		t.Fatalf("the Target did not open: open %q", st.open)
	}
	if !tt.HasText("Guide") {
		t.Errorf("the guide does not show; texts %q", tt.Texts())
	}
}

func TestParseGitStatus(t *testing.T) {
	got := parseGitStatus("/repo", []byte(" M a.go\x00?? new.txt\x00R  b/new.go\x00b/old.go\x00A  c.go\x00 D gone.go\x00"))
	want := map[string]string{"/repo/a.go": " M", "/repo/new.txt": "??", "/repo/b/new.go": "R ", "/repo/c.go": "A ", "/repo/gone.go": " D"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if !dirChanged(got, "/repo/b") || dirChanged(got, "/repo/bb") {
		t.Error("a folder's changes")
	}
}
