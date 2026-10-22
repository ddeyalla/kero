package main

import (
	"bytes"
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"kero/internal/proto"

	"kero/locales"
)

// The Files pane is the session's folder as a tree, and a file of it
// read: text with its line numbers, Markdown as it reads (or its text),
// an image. Nothing in it writes. Folders are read as they unfold, off
// the main thread, and read again, with what git says changed, every few
// seconds while the pane shows. The file read is the pane's Target, which
// kerod keeps, so that it opens again after a restart.

const (
	// maxTextFile is the most of a file read as text; maxImageFile of an
	// image.
	maxTextFile  = 2 << 20
	maxImageFile = 20 << 20
	// numberedLines is the most lines shown with their numbers; a longer
	// file shows in a text area that lays out only the lines in view.
	numberedLines = 5000
	// filesRefresh is how often the folders unfolded and the changes are
	// read again.
	filesRefresh = 5 * time.Second
	// filesSplitWidth is the narrowest pane that shows the tree beside the
	// file; a narrower one shows the file in the tree's place.
	filesSplitWidth = 720
	// filesIndexed is the most files Find looks through; filesFound the
	// most it shows. filesIndexAge is how old the files it looked through
	// may be before it looks again.
	filesIndexed  = 50000
	filesFound    = 100
	filesIndexAge = 30 * time.Second
)

// skippedDirs are folders the tree leaves out: what tools keep, not what
// one reads.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, ".DS_Store": true}

// unindexedDirs are folders Find leaves out besides: what builds and
// tools make, many files no one looks for.
var unindexedDirs = map[string]bool{".next": true, ".venv": true, "__pycache__": true, ".cache": true, ".turbo": true, "target": true}

// filesState is a Files pane's: what it has read and shows.
type filesState struct {
	id       string // the pane's
	root     string
	dirs     map[string]*dirListing
	expanded map[string]bool
	selected string // the row the keyboard is on
	open     string // the file read, "" none
	file     *fileContent
	source   bool              // Markdown as its text
	changes  map[string]string // path → git's two-letter status
	readAt   time.Time
	reading  bool
	sent     string // the Target kerod has
	md       markdown.Options
	buf      *ui.TextBuffer // a long file's text
	anchor   string         // the heading of the Markdown read to scroll to
	mdDir    string         // the folder md loads images from

	// Find: what is typed, the files it looks through, and those found.
	query     string
	finding   bool // the field has the keyboard
	find      bool // the field takes the keyboard
	index     []string
	indexAt   time.Time
	indexing  bool
	found     []string
	foundFor  string
	foundAt   time.Time // the index the files found were found in
	searching bool
	hit       int // the file found the keyboard is on
}

type dirListing struct {
	entries []fileEntry
	err     string
}

type fileEntry struct {
	name, path string
	dir        bool
}

// fileContent is a file read: its text, or an image, or why neither.
type fileContent struct {
	path   string
	text   string
	lines  int
	image  ui.ImageSource
	binary bool
	tooBig bool
	size   int64
	err    string
}

// background runs work off the main thread, and what it returns on it.
// Without a window, as in tests, both run at once.
func (a *App) background(work func() func()) {
	if a.win == nil {
		work()()
		return
	}
	go func() {
		done := work()
		a.update(done)
	}()
}

// filesStateOf is a Files pane's state, made as it first shows: open on
// its Target.
func (a *App) filesStateOf(s *proto.Session, p *proto.Pane) *filesState {
	if a.files == nil {
		a.files = map[string]*filesState{}
	}
	st := a.files[p.ID]
	if st != nil {
		return st
	}
	root := p.Dir
	if root == "" {
		root = s.WorkDir()
	}
	st = &filesState{id: p.ID, root: root, dirs: map[string]*dirListing{}, expanded: map[string]bool{root: true}, sent: p.Target}
	a.files[p.ID] = st
	// The folders open last time open again.
	for _, rel := range a.prefs.Files[p.ID] {
		if d := filepath.Join(root, filepath.FromSlash(rel)); within(root, d) {
			st.expanded[d] = true
		}
	}
	if t := p.Target; t != "" && within(root, t) {
		for d := filepath.Dir(t); within(root, d); d = filepath.Dir(d) {
			st.expanded[d] = true
			if d == root {
				break
			}
		}
		if fi, err := os.Stat(t); err == nil && !fi.IsDir() {
			a.openFile(st, t)
		} else if err == nil {
			st.expanded[t] = true
		}
	}
	return st
}

// within tells a path in dir, or dir itself.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readFolders reads again the folders unfolded, and what git says
// changed, unless it did lately.
func (a *App) readFolders(c *ui.Context, st *filesState) {
	if st.reading {
		return
	}
	if wait := filesRefresh - time.Since(st.readAt); !st.readAt.IsZero() && wait > 0 {
		c.After(wait)
		return
	}
	st.reading = true
	var dirs []string
	for d, on := range st.expanded {
		if on {
			dirs = append(dirs, d)
		}
	}
	root := st.root
	a.background(func() func() {
		listings := map[string]*dirListing{}
		for _, d := range dirs {
			listings[d] = listDir(d)
		}
		changes := gitChanges(root)
		return func() {
			for d, l := range listings {
				st.dirs[d] = l
			}
			st.changes = changes
			st.reading = false
			st.readAt = time.Now()
		}
	})
}

// listDir reads a folder: its folders first, then its files, each by
// name, without skippedDirs.
func listDir(dir string) *dirListing {
	des, err := os.ReadDir(dir)
	if err != nil {
		return &dirListing{err: err.Error()}
	}
	l := &dirListing{}
	for _, de := range des {
		if skippedDirs[de.Name()] {
			continue
		}
		isDir := de.IsDir()
		if de.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, de.Name())); err == nil {
				isDir = fi.IsDir()
			}
		}
		l.entries = append(l.entries, fileEntry{name: de.Name(), path: filepath.Join(dir, de.Name()), dir: isDir})
	}
	slices.SortFunc(l.entries, func(x, y fileEntry) int {
		if x.dir != y.dir {
			if x.dir {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(x.name), strings.ToLower(y.name))
	})
	return l
}

// gitChanges is what git says changed in the repository around dir, by
// absolute path: nil outside one, or without git.
func gitChanges(dir string) map[string]string {
	top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil
	}
	root := strings.TrimSpace(string(top))
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain=v1", "-z", "--untracked-files=normal").Output()
	if err != nil {
		return nil
	}
	return parseGitStatus(root, out)
}

// parseGitStatus reads `git status --porcelain=v1 -z`, whose paths are
// relative to the repository's root; a rename's old path follows it.
func parseGitStatus(root string, out []byte) map[string]string {
	changes := map[string]string{}
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		code, path := string(e[:2]), string(e[3:])
		changes[filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(path, "/")))] = code
		if code[0] == 'R' || code[0] == 'C' {
			i++
		}
	}
	return changes
}

// changeMark is how a changed file shows: a letter and its color.
func changeMark(pal *palette, code string) (string, ui.Color) {
	switch {
	case code == "??":
		return "U", pal.done
	case strings.Contains(code, "D"):
		return "D", pal.danger
	case strings.Contains(code, "A"):
		return "A", pal.done
	case strings.Contains(code, "R"):
		return "R", pal.accent
	}
	return "M", pal.attention
}

// dirChanged tells a folder holding a change.
func dirChanged(changes map[string]string, dir string) bool {
	prefix := dir + string(filepath.Separator)
	for p := range changes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// openFile reads a file into the pane, off the main thread.
func (a *App) openFile(st *filesState, path string) {
	st.open, st.selected, st.file, st.buf, st.anchor = path, path, nil, nil, ""
	a.background(func() func() {
		f := readFile(path)
		return func() {
			if st.open != path {
				return
			}
			st.file = f
			if f.lines > numberedLines {
				st.buf = ui.NewTextBuffer(f.text)
			}
		}
	})
}

// imageExts are the images the pane shows.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".svg": true}

func readFile(path string) *fileContent {
	f := &fileContent{path: path}
	fi, err := os.Stat(path)
	if err != nil {
		f.err = err.Error()
		return f
	}
	f.size = fi.Size()
	ext := strings.ToLower(filepath.Ext(path))
	if imageExts[ext] {
		if f.size > maxImageFile {
			f.tooBig = true
			return f
		}
		data, err := os.ReadFile(path)
		if err != nil {
			f.err = err.Error()
			return f
		}
		if ext == ".svg" {
			f.image, err = ui.ParseSVG(data)
		} else {
			f.image, err = ui.DecodeBitmap(data)
		}
		if err != nil {
			f.err = err.Error()
		}
		return f
	}
	if f.size > maxTextFile {
		f.tooBig = true
		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		f.err = err.Error()
		return f
	}
	head := data[:min(len(data), 8000)]
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(data) {
		f.binary = true
		return f
	}
	f.text = strings.ReplaceAll(string(data), "\r\n", "\n")
	f.lines = strings.Count(f.text, "\n")
	if !strings.HasSuffix(f.text, "\n") {
		f.lines++
	}
	return f
}

func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// treeRow is a row the tree shows: an entry, at its depth.
type treeRow struct {
	fileEntry
	depth int
}

// treeRows are the rows the tree shows, in order: the unfolded folders'
// entries under them.
func (st *filesState) treeRows() []treeRow {
	var rows []treeRow
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		l := st.dirs[dir]
		if l == nil {
			return
		}
		for _, e := range l.entries {
			rows = append(rows, treeRow{e, depth})
			if e.dir && st.expanded[e.path] {
				walk(e.path, depth+1)
			}
		}
	}
	walk(st.root, 0)
	return rows
}

// toggleDir unfolds a folder, reading it, or folds it.
func (a *App) toggleDir(st *filesState, dir string) {
	if st.expanded[dir] {
		delete(st.expanded, dir)
		a.keepExpanded(st)
		return
	}
	st.expanded[dir] = true
	a.keepExpanded(st)
	if st.dirs[dir] == nil {
		a.background(func() func() {
			l := listDir(dir)
			return func() { st.dirs[dir] = l }
		})
	}
}

// keepExpanded keeps the folders unfolded, which unfold again after a
// restart.
func (a *App) keepExpanded(st *filesState) {
	if st.id == "" {
		return
	}
	var rels []string
	for d, on := range st.expanded {
		if rel, err := filepath.Rel(st.root, d); on && err == nil && rel != "." {
			rels = append(rels, filepath.ToSlash(rel))
		}
	}
	slices.Sort(rels)
	if a.prefs.Files == nil {
		a.prefs.Files = map[string][]string{}
	}
	if len(rels) == 0 {
		delete(a.prefs.Files, st.id)
	} else {
		a.prefs.Files[st.id] = rels
	}
	a.savePrefs()
}

// reveal unfolds the folders down to a path, and puts the tree's row on
// it.
func (a *App) reveal(st *filesState, path string) {
	var dirs []string
	for d := filepath.Dir(path); within(st.root, d) && d != st.root; d = filepath.Dir(d) {
		dirs = append(dirs, d)
	}
	for _, d := range slices.Backward(dirs) {
		if !st.expanded[d] {
			a.toggleDir(st, d)
		}
	}
	st.selected = path
}

// focusedFiles is the Files pane the user is in, nil when in none.
func (a *App) focusedFiles(s *proto.Session) *filesState {
	if s == nil {
		return nil
	}
	p := s.PaneByID(a.focusOf(s))
	if p == nil || p.View != proto.PaneFiles {
		return nil
	}
	return a.filesStateOf(s, p)
}

// indexFiles looks through the folder's files for Find, off the main
// thread, unless it did lately.
func (a *App) indexFiles(st *filesState) {
	if st.indexing || st.index != nil && time.Since(st.indexAt) < filesIndexAge {
		return
	}
	st.indexing = true
	root := st.root
	a.background(func() func() {
		files := indexDir(root, filesIndexed)
		return func() {
			st.index, st.indexAt, st.indexing = files, time.Now(), false
		}
	})
}

// indexDir is the files under root, at most limit, without the folders
// the tree and Find leave out.
func indexDir(root string, limit int) []string {
	files := []string{}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && (skippedDirs[d.Name()] || unindexedDirs[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if skippedDirs[d.Name()] {
			return nil
		}
		files = append(files, path)
		if len(files) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return files
}

// findFiles are the files of the index the query matches, best first: by
// name before by path.
func findFiles(root string, index []string, query string, limit int) []string {
	type hit struct {
		path  string
		score int
	}
	var hits []hit
	for _, path := range index {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		score := fuzzy(query, rel)
		if n := fuzzy(query, filepath.Base(path)); n >= 0 {
			score = max(score, n+1000)
		}
		if score >= 0 {
			hits = append(hits, hit{path, score})
		}
	}
	slices.SortStableFunc(hits, func(x, y hit) int {
		return cmp.Or(cmp.Compare(y.score, x.score), cmp.Compare(len(x.path), len(y.path)))
	})
	found := make([]string, 0, min(len(hits), limit))
	for _, h := range hits[:min(len(hits), limit)] {
		found = append(found, h.path)
	}
	return found
}

func (a *App) filesPane(c *ui.Context, pal *palette, s *proto.Session, p *proto.Pane, body ui.Element) {
	st := a.filesStateOf(s, p)
	a.readFolders(c, st)
	// What the pane reads is its Target, which kerod keeps.
	if st.open != st.sent {
		st.sent = st.open
		a.call("pane.target", proto.TargetParams{Pane: p.ID, Target: st.open}, nil)
	}
	w := a.paneRef("card-" + p.ID).Bounds(c).W
	split := w == 0 || w >= filesSplitWidth
	a.filesKeys(c, st, body, split)
	switch {
	// Find, in a pane too narrow for the tree beside the file, shows the
	// tree in its place.
	case st.open == "" || !split && (st.find || st.finding || st.query != ""):
		a.filesTree(c, pal, st)
	case split:
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(260).Shrink(0).MinHeight(0).Children(func() { a.filesTree(c, pal, st) })
			ui.Box(c).Width(1).AlignSelf(ui.Stretch).Background(pal.divider)
			ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() { a.fileViewer(c, pal, st, false) })
		})
	default:
		a.fileViewer(c, pal, st, true)
	}
}

// filesKeys are the pane's keys, while it has the keyboard: the arrows
// move in the tree, unfold and fold; Return reads the file; Esc closes it.
func (a *App) filesKeys(c *ui.Context, st *filesState, body ui.Element, split bool) {
	// Find has its own keys.
	if st.finding || st.query != "" {
		return
	}
	if st.open != "" && !split {
		if body.Shortcut(0, ui.KeyEscape) {
			st.open, st.file, st.buf = "", nil, nil
		}
		return
	}
	rows := st.treeRows()
	if len(rows) == 0 {
		return
	}
	at := slices.IndexFunc(rows, func(r treeRow) bool { return r.path == st.selected })
	move := func(d int) {
		n := min(max(at+d, 0), len(rows)-1)
		if at < 0 {
			n = 0
		}
		st.selected = rows[n].path
	}
	if body.Shortcut(0, ui.KeyDown) {
		move(1)
	}
	if body.Shortcut(0, ui.KeyUp) {
		move(-1)
	}
	if at < 0 {
		return
	}
	r := rows[at]
	if body.Shortcut(0, ui.KeyRight) && r.dir && !st.expanded[r.path] {
		a.toggleDir(st, r.path)
	}
	if body.Shortcut(0, ui.KeyLeft) {
		if r.dir && st.expanded[r.path] {
			a.toggleDir(st, r.path)
		} else if parent := filepath.Dir(r.path); parent != st.root && within(st.root, parent) {
			st.selected = parent
		}
	}
	if body.Shortcut(0, ui.KeyEnter) {
		if r.dir {
			a.toggleDir(st, r.path)
		} else {
			a.openFile(st, r.path)
		}
	}
	if split && st.open != "" && body.Shortcut(0, ui.KeyEscape) {
		st.open, st.file, st.buf = "", nil, nil
	}
}

func (a *App) filesTree(c *ui.Context, pal *palette, st *filesState) {
	a.filesFind(c, pal, st)
	ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
	if st.query != "" {
		a.filesFound(c, pal, st)
		return
	}
	root := st.dirs[st.root]
	switch {
	case root == nil:
		working(c, pal, locales.FilesReading())
		return
	case root.err != "":
		paneNothing(c, pal, filesIcon, locales.FilesCantRead(), root.err)
		return
	case len(root.entries) == 0:
		paneNothing(c, pal, filesIcon, locales.FilesEmpty(), proto.ShortDir(st.root))
		return
	}
	ui.Scroll(c.Key("files-tree")).Grow(1).MinHeight(0).Padding(6, 6, 12, 6).Children(func() {
		for _, r := range st.treeRows() {
			a.treeRow(c, pal, st, r)
		}
	})
}

// filesFind is the tree's header, as high as the file's: a field that
// finds a file of the folder by its name or path, as typed loosely.
func (a *App) filesFind(c *ui.Context, pal *palette, st *filesState) {
	ui.Row(c.Key("files-find")).Height(36).Shrink(0).AlignItems(ui.Center).Gap(7).Padding(0, 8, 0, 12).Children(func() {
		ui.Icon(c, searchIcon).Size(13, 13).TextColor(pal.textFaint).Shrink(0)
		in := ui.TextInputBase(c.Key("find"), &st.query).Grow(1).MinWidth(0).FontSize(12.5).
			Placeholder(locales.FilesFind(filepath.Base(st.root))).Label(locales.FilesFind(filepath.Base(st.root))).
			Bind(a.paneRef("find-" + st.id))
		// Asked for, the field keeps asking until it has the keyboard.
		if st.find {
			if in.Focused() {
				st.find = false
			} else {
				a.paneRef("find-" + st.id).Focus()
				in.SetTextSelection(0, utf8.RuneCountInString(st.query))
			}
		}
		st.finding = in.Focused()
		if st.query != "" {
			a.indexFiles(st)
		}
		n := len(st.found)
		if st.finding && n > 0 {
			if in.Shortcut(0, ui.KeyDown) || in.Shortcut(ui.Ctrl, ui.KeyN) {
				st.hit = (st.hit + 1) % n
			}
			if in.Shortcut(0, ui.KeyUp) || in.Shortcut(ui.Ctrl, ui.KeyP) {
				st.hit = (st.hit + n - 1) % n
			}
		}
		if st.query == "" {
			st.found, st.foundFor, st.hit = nil, "", 0
		}
		if in.Submitted() && st.query != "" && st.hit < n {
			a.openFound(st, st.found[st.hit])
		}
		if st.finding && in.Shortcut(0, ui.KeyEscape) {
			if st.query != "" {
				st.query = ""
			} else {
				a.paneRef(st.id).Focus()
			}
		}
		if st.query != "" {
			iconButton(c, "find-clear", closeIcon, locales.FilesFindClear(), pal, func() {
				st.query = ""
				a.paneRef(st.id).Focus()
			})
		}
	})
}

// openFound reads a file found, which the tree then shows, and lets go
// of Find.
func (a *App) openFound(st *filesState, path string) {
	st.query = ""
	a.reveal(st, path)
	a.openFile(st, path)
	a.paneRef(st.id).Focus()
}

// filesFound are the files Find found, or why there are none.
func (a *App) filesFound(c *ui.Context, pal *palette, st *filesState) {
	if st.index == nil {
		working(c, pal, locales.FilesFinding())
		return
	}
	// Many files take a while to look through: off the main thread, what
	// was found last showing meanwhile.
	if (st.foundFor != st.query || !st.foundAt.Equal(st.indexAt)) && !st.searching {
		st.searching = true
		root, index, query, at := st.root, st.index, st.query, st.indexAt
		a.background(func() func() {
			found := findFiles(root, index, query, filesFound)
			return func() {
				st.searching = false
				if query != st.query {
					return
				}
				// Looked through again, the keyboard stays on its file.
				var on string
				if st.foundFor == query && st.hit < len(st.found) {
					on = st.found[st.hit]
				}
				st.found, st.foundFor, st.foundAt, st.hit = found, query, at, max(slices.Index(found, on), 0)
			}
		})
	}
	if st.foundFor == "" {
		return
	}
	if len(st.found) == 0 {
		paneNothing(c, pal, searchIcon, locales.FilesNoneFound(), st.query)
		return
	}
	ui.Scroll(c.Key("files-found")).Grow(1).MinHeight(0).Padding(6, 6, 12, 6).Children(func() {
		for i, path := range st.found {
			rel, _ := filepath.Rel(st.root, path)
			dir, name := filepath.Split(filepath.ToSlash(rel))
			row := ui.ButtonBase(c.Key("h-"+path)).Height(26).Shrink(0).AlignItems(ui.Center).Gap(6).
				Padding(0, 8, 0, 8).Radius(6).Label(rel)
			switch {
			case i == st.hit:
				row.Background(pal.accent.Alpha(0.14)).ScrollIntoView()
			case row.Hovered():
				row.Background(pal.rowHover)
			}
			if row.Clicked() {
				a.openFound(st, path)
			}
			row.DragDataFrom(func() transfer.Data { d, _ := transfer.FileData(path); return d })
			row.ContextMenu(func(m *ui.Menu) { a.fileMenu(m, st, path, false) })
			row.Children(func() {
				ui.Icon(c, entryIcon(fileEntry{name: name})).Size(14, 14).TextColor(pal.textMuted).Shrink(0)
				ui.Text(c, name).FontSize(12.5).TextColor(pal.text).SingleLine().Shrink(0).MaxWidthPercent(70).Ellipsis("…")
				ui.Text(c, strings.TrimSuffix(dir, "/")).FontSize(11.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			})
		}
	})
}

func (a *App) treeRow(c *ui.Context, pal *palette, st *filesState, r treeRow) {
	row := ui.ButtonBase(c.Key("f-"+r.path)).Height(26).Shrink(0).AlignItems(ui.Center).Gap(6).
		Padding(0, 8, 0, 6+float32(r.depth)*14).Radius(6).Label(r.name)
	switch {
	case r.path == st.open:
		row.Background(pal.accent.Alpha(0.14))
	case r.path == st.selected:
		row.Background(pal.rowHover)
	case row.Hovered():
		row.Background(pal.rowHover)
	}
	if r.path == st.selected {
		row.ScrollIntoView()
	}
	if row.Clicked() {
		st.selected = r.path
		if r.dir {
			a.toggleDir(st, r.path)
		} else {
			a.openFile(st, r.path)
		}
	}
	row.ContextMenu(func(m *ui.Menu) { a.fileMenu(m, st, r.path, r.dir) })
	// Dragged to a terminal, a row is its path typed; to another app, the
	// file.
	path := r.path
	row.DragDataFrom(func() transfer.Data { d, _ := transfer.FileData(path); return d })
	row.Children(func() {
		chev := ui.Box(c).Size(10, 10).Shrink(0).Center()
		if r.dir {
			chev.Children(func() {
				ic := ui.Icon(c, chevronIcon).Size(9, 9).TextColor(pal.textFaint)
				if st.expanded[r.path] {
					ic.Rotate(90)
				}
			})
		}
		ui.Icon(c, entryIcon(r.fileEntry)).Size(14, 14).TextColor(pal.textMuted).Shrink(0)
		name := ui.Text(c, r.name).FontSize(12.5).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if r.path == st.open {
			name.FontWeight(600)
		}
		if code, ok := st.changes[r.path]; ok && !r.dir {
			letter, col := changeMark(pal, code)
			name.TextColor(col)
			ui.Text(c, letter).FontSize(11).FontWeight(600).TextColor(col).Shrink(0)
		} else if r.dir && dirChanged(st.changes, r.path) {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.attention).Shrink(0).Margin(0, 3, 0, 0)
		}
	})
}

func entryIcon(e fileEntry) *ui.SVG {
	switch {
	case e.dir:
		return folderIcon
	case imageExts[strings.ToLower(filepath.Ext(e.name))]:
		return imageIcon
	case isMarkdown(e.name):
		return docIcon
	}
	return fileIcon
}

// fileMenu is a file's or a folder's: where else to open it, and its path.
func (a *App) fileMenu(m *ui.Menu, st *filesState, path string, dir bool) {
	if !dir && m.Item(locales.FilesOpenDefault()).Chosen() {
		openPath(path)
	}
	if editor := codeEditor(); editor != nil && m.Item(locales.FilesOpenEditor()).Chosen() {
		editor(path)
	}
	if m.Item(locales.CommonShowIn(fileManager())).Chosen() {
		revealPath(path)
	}
	m.Separator()
	if m.Item(locales.FilesCopyPath()).Chosen() {
		a.copyText = path
	}
	if rel, err := filepath.Rel(st.root, path); err == nil && m.Item(locales.FilesCopyRelative()).Chosen() {
		a.copyText = filepath.ToSlash(rel)
	}
}

// codeEditor opens a path in VS Code, nil when it has no way to.
func codeEditor() func(path string) {
	if bin, err := exec.LookPath("code"); err == nil {
		return func(path string) { exec.Command(bin, path).Start() }
	}
	if _, err := os.Stat("/Applications/Visual Studio Code.app"); err == nil {
		return func(path string) { exec.Command("/usr/bin/open", "-a", "Visual Studio Code", path).Start() }
	}
	return nil
}

// fileViewer is the file read: its header, then its text, Markdown or
// image. alone, it is in the tree's place, with the way back.
func (a *App) fileViewer(c *ui.Context, pal *palette, st *filesState, alone bool) {
	f := st.file
	md := isMarkdown(st.open)
	ui.Row(c.Key("file-hdr")).Height(36).Shrink(0).AlignItems(ui.Center).Gap(6).Padding(0, 8, 0, 10).Children(func() {
		if alone {
			iconButton(c, "files-back", backIcon, locales.FilesBack(), pal, func() { st.open, st.file, st.buf = "", nil, nil })
		}
		rel, err := filepath.Rel(st.root, st.open)
		if err != nil || !within(st.root, st.open) {
			rel = proto.ShortDir(st.open)
		}
		dir, name := filepath.Split(filepath.ToSlash(rel))
		ui.Row(c).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			if dir != "" {
				ui.Text(c, dir).FontSize(12.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Shrink(2).MinWidth(0)
			}
			ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(pal.text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(20)
		})
		if code, ok := st.changes[st.open]; ok {
			letter, col := changeMark(pal, code)
			ui.Text(c, letter).FontSize(11).FontWeight(600).TextColor(col).Shrink(0)
		}
		ui.Box(c).Grow(1)
		if f != nil && f.lines > 0 {
			ui.Text(c, locales.FilesLines(strconv.Itoa(f.lines))).FontSize(11.5).TextColor(pal.textFaint).FontFeatures("tnum").SingleLine().Shrink(0)
		}
		if md && f != nil && f.text != "" {
			if st.source {
				iconButton(c, "md-preview", previewIcon, locales.FilesPreview(), pal, func() { st.source = false })
			} else {
				iconButton(c, "md-source", sourceIcon, locales.FilesSource(), pal, func() { st.source = true })
			}
		}
		if editor := codeEditor(); editor != nil {
			path := st.open
			iconButton(c, "file-editor", externalIcon, locales.FilesOpenEditor(), pal, func() { editor(path) })
		}
		path := st.open
		iconButton(c, "file-copy", copyIcon, locales.FilesCopyPath(), pal, func() { a.copyText = path })
	})
	ui.Box(c).Height(1).Shrink(0).Background(pal.divider)
	switch {
	case f == nil:
		working(c, pal, locales.FilesReading())
	case f.err != "":
		paneNothing(c, pal, fileIcon, locales.FilesCantRead(), f.err)
	case f.tooBig:
		paneNothing(c, pal, fileIcon, locales.FilesTooBig(), humanSize(f.size))
	case f.binary:
		paneNothing(c, pal, fileIcon, locales.FilesBinary(), humanSize(f.size))
	case f.image != nil:
		ui.Scroll(c.Key("file-image")).Grow(1).MinHeight(0).Padding(16).Children(func() {
			ui.Image(c, f.image).MaxWidthPercent(100).Fit(ui.Contain)
		})
	case md && !st.source:
		a.markdownFile(c, st, f)
	case f.lines > numberedLines && st.buf != nil:
		ui.TextAreaBufferBase(c.Key("file-buf"), st.buf).ReadOnly(true).Font("monospace").FontSize(12).
			Grow(1).MinHeight(0).Padding(10, 12)
	default:
		numberedText(c, pal, f)
	}
}

// markdownFile is a Markdown file as it reads: its links to files of the
// folder read them in the pane, its links to the web open in the browser,
// and its images are the folder's own.
func (a *App) markdownFile(c *ui.Context, st *filesState, f *fileContent) {
	dir := filepath.Dir(f.path)
	if st.md.OnLink == nil || st.mdDir != dir {
		st.mdDir = dir
		st.md.OnLink = func(link string) { a.fileLink(st, dir, link) }
		load := markdown.FileImages(dir)
		root := st.root
		st.md.LoadImage = func(url string) (ui.ImageSource, error) {
			if p := strings.TrimPrefix(url, "file:"); filepath.IsAbs(p) && !within(root, p) ||
				!filepath.IsAbs(p) && !within(root, filepath.Join(dir, filepath.FromSlash(p))) {
				return nil, os.ErrNotExist
			}
			return load(url)
		}
	}
	// A link to a heading scrolls to it: the heading built finds itself.
	st.md.Block = nil
	asked := st.anchor
	if asked != "" {
		name := strings.ToLower(asked)
		view := a.paneRef("md-" + st.id).Bounds(c)
		st.md.Block = func(c *ui.Context, d *parser.Document, i int) bool {
			if d.Block(i).Kind == parser.Heading && markdown.Slug(parser.PlainText(d.Inlines(i))) == name {
				// As tall as the view, from just above the heading: shown,
				// the heading is at the top.
				ui.Box(c).Absolute().Top(-12).Left(0).Width(1).Height(max(view.H-24, 1)).PassThrough().ScrollIntoView()
				st.anchor = ""
			}
			return false
		}
	}
	ui.Scroll(c.Key("file-md")).Grow(1).MinHeight(0).Padding(18, 22, 28, 22).Bind(a.paneRef("md-" + st.id)).Children(func() {
		markdown.View(c, f.text, &st.md)
	})
	switch {
	case st.anchor != asked:
		// A link clicked as it built: the next frame scrolls.
		c.Invalidate()
	case asked != "":
		// A heading not there scrolls nowhere.
		st.anchor = ""
	}
}

// fileLink follows a Markdown file's link: a web address in the browser,
// a file of the folder in the pane, a heading of it there.
func (a *App) fileLink(st *filesState, dir, link string) {
	if linearSafeURL(link) {
		openURL(link)
		return
	}
	if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
		return
	}
	link, heading, _ := strings.Cut(link, "#")
	if link == "" {
		st.anchor = heading
		return
	}
	path := filepath.Join(dir, filepath.FromSlash(link))
	if !within(st.root, path) {
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	if fi.IsDir() {
		for d := path; within(st.root, d) && d != st.root; d = filepath.Dir(d) {
			if !st.expanded[d] {
				a.toggleDir(st, d)
			}
		}
		st.selected = path
		return
	}
	a.openFile(st, path)
	st.anchor = heading
}

// numberedText is a file's text beside its line numbers, which scroll
// with it.
func numberedText(c *ui.Context, pal *palette, f *fileContent) {
	var nums strings.Builder
	for i := 1; i <= f.lines; i++ {
		if i > 1 {
			nums.WriteByte('\n')
		}
		nums.WriteString(strconv.Itoa(i))
	}
	text := strings.TrimSuffix(f.text, "\n")
	ui.ScrollBoth(c.Key("file-text")).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Padding(10, 16, 16, 0).Gap(14).Children(func() {
			ui.Text(c, nums.String()).Font("monospace").FontSize(12).LineHeight(1.5).TextColor(pal.textFaint).
				TextAlign(ui.End).FontFeatures("tnum").Shrink(0).MinWidth(36).Unselectable()
			ui.Text(c, text).Font("monospace").FontSize(12).LineHeight(1.5).TextColor(pal.text).NoWrap().Selectable()
		})
	})
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

// paneNothing is a view with nothing to show: why, under its mark.
func paneNothing(c *ui.Context, pal *palette, svg *ui.SVG, title, detail string) {
	ui.Column(c).Fill().Center().Gap(6).Padding(24).Children(func() {
		ui.Box(c).Size(40, 40).Radius(11).Center().Background(pal.chip).TextColor(pal.textMuted).Margin(0, 0, 6, 0).Children(func() {
			ui.Icon(c, svg).Size(20, 20)
		})
		ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(pal.text).TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(12.5).TextColor(pal.textFaint).TextAlign(ui.Center).MaxWidth(360)
		}
	})
}
