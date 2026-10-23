package main

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
)

// Legacy SVG handles identify controls at existing call sites. controlIcon
// renders those handles with native SF Symbols. Program logos stay artwork.
//
//go:embed icons/*.svg
var iconFiles embed.FS

// glyph returns an icon of the set, as "f7-gear-alt".
func glyph(name string) *ui.SVG {
	glyphMu.Lock()
	defer glyphMu.Unlock()
	if svg := glyphs[name]; svg != nil {
		return svg
	}
	b, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic("kero: no icon " + name)
	}
	svg := ui.MustParseSVG(b)
	glyphs[name] = svg
	if symbol := controlSymbolNames[name]; symbol != "" {
		controlSymbols[svg] = symbol
	}
	return svg
}

// controlSymbols keeps the native meaning of the existing control handles;
// logos deliberately have no entry and keep their SVG artwork.
var glyphMu sync.Mutex
var glyphs = map[string]*ui.SVG{}
var controlSymbols = map[*ui.SVG]string{}

var controlSymbolNames = map[string]string{
	"f7-sidebar-left": "sidebar.left", "f7-plus": "plus", "f7-chevron-right": "chevron.right",
	"f7-chevron-left": "chevron.left", "f7-chevron-up": "chevron.up", "f7-chevron-down": "chevron.down",
	"f7-folder": "folder", "f7-search": "magnifyingglass", "meteor-terminal": "terminal",
	"f7-return": "return", "f7-gear-alt": "gearshape", "f7-square-split-2x1": "rectangle.split.2x1",
	"f7-square-split-1x2": "rectangle.split.1x2", "f7-arrow-up-left-arrow-down-right": "arrow.up.left.and.arrow.down.right",
	"f7-arrow-down-right-arrow-up-left": "arrow.down.right.and.arrow.up.left", "f7-xmark": "xmark",
	"f7-arrow-right-arrow-left": "arrow.left.arrow.right", "f7-tray-fill": "tray.fill",
	"f7-arrow-clockwise": "arrow.clockwise", "f7-arrow-up-right-square": "arrow.up.right.square",
	"f7-doc-on-doc": "doc.on.doc", "f7-cube": "cube", "f7-doc-text": "doc.text",
	"f7-folder-fill": "folder.fill", "f7-globe": "globe", "f7-doc": "doc", "f7-photo": "photo",
	"f7-arrow-left": "arrow.left", "f7-arrow-right": "arrow.right", "f7-eye": "eye",
	"f7-chevron-left-slash-chevron-right": "chevron.left.forwardslash.chevron.right",
	"lucide-chevrons-up-down":             "chevron.up.chevron.down", "lucide-check": "checkmark",
	"f7-bell": "bell", "f7-keyboard": "keyboard", "f7-paintbrush": "paintbrush",
	"f7-slider-horizontal-3": "slider.horizontal.3", "f7-sparkles": "sparkles",
	"f7-arrow-turn-down-right": "arrow.turn.down.right", "f7-link": "link", "f7-list-bullet": "list.bullet",
	"f7-list-bullet-indent": "list.bullet.indent", "f7-rectangle-split-3x1": "rectangle.split.3x1",
	"f7-suit-diamond": "suit.diamond",
}

// controlIcon preserves element layout and modifiers while drawing the system's
// own symbol. An unknown symbol leaves an empty slot rather than a substitute.
func controlIcon(c *ui.Context, svg *ui.SVG, size float32, tint ui.Color, dark bool) ui.Element {
	glyphMu.Lock()
	name := controlSymbols[svg]
	glyphMu.Unlock()
	if name != "" {
		if bitmap := symbolImage(name, size, dark); bitmap != nil {
			return symbolElement(c, bitmap, size, tint).Shrink(0)
		}
		return ui.Box(c).Size(size, size).Shrink(0)
	}
	return ui.Icon(c, svg).Size(size, size).TextColor(tint)
}

// symbolMark draws an SF Symbol at size, in tint, as an image where the
// app draws an icon: a new icon of the app's is the system's own (see
// symbols_darwin.go). It draws nothing on a system without symbols, and
// where the system has no such symbol, which it reports.
func symbolMark(c *ui.Context, name string, size float32, tint ui.Color, dark bool) bool {
	b := symbolImage(name, size, dark)
	if b == nil {
		return false
	}
	symbolElement(c, b, size, tint).Shrink(0)
	return true
}

// The system shape is cached once as white, as plugin symbols are. Tint
// is applied while painting so a hover or ambient fade keeps that image.
func symbolElement(c *ui.Context, bitmap *ui.Bitmap, size float32, tint ui.Color) ui.Element {
	return ui.Box(c).Size(size, size).Draw(func(p *ui.Painter, r ui.Rect) {
		p.ImageRegion(bitmap, ui.Rect{}, r, ui.ImageOptions{Tint: tint})
	})
}

// icon draws an icon of kero's own, on 16, as a status glyph.
func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	sidebarIcon     = glyph("f7-sidebar-left")
	plusIcon        = glyph("f7-plus")
	chevronIcon     = glyph("f7-chevron-right")
	chevronUpIcon   = glyph("f7-chevron-up")
	chevronDownIcon = glyph("f7-chevron-down")
	folderIcon      = glyph("f7-folder")
	searchIcon      = glyph("f7-search")
	terminalIcon    = glyph("meteor-terminal")
	returnIcon      = glyph("f7-return")
	gearIcon        = glyph("f7-gear-alt")
	updateIcon      = icon(`<path d="M13 5a5.5 5.5 0 1 0 .3 5M13 2v3H10"/>`)
	splitRightIcon  = glyph("f7-square-split-2x1")
	splitDownIcon   = glyph("f7-square-split-1x2")
	zoomIcon        = glyph("f7-arrow-up-left-arrow-down-right")
	unzoomIcon      = glyph("f7-arrow-down-right-arrow-up-left")
	closeIcon       = glyph("f7-xmark")
	swapIcon        = glyph("f7-arrow-right-arrow-left")

	// The tray of the Linear page of Settings and the Tasks entry, and the
	// marks of what can be done with a link.
	trayIcon     = glyph("f7-tray-fill")
	refreshIcon  = glyph("f7-arrow-clockwise")
	externalIcon = glyph("f7-arrow-up-right-square")
	copyIcon     = glyph("f7-doc-on-doc")
	projectIcon  = glyph("f7-cube")
	docIcon      = glyph("f7-doc-text")
	backIcon     = glyph("f7-chevron-left")

	// The panes that are views: the project's files and a web page, and
	// what they show and do.
	filesIcon   = glyph("f7-folder-fill")
	globeIcon   = glyph("f7-globe")
	fileIcon    = glyph("f7-doc")
	imageIcon   = glyph("f7-photo")
	goBackIcon  = glyph("f7-arrow-left")
	goFwdIcon   = glyph("f7-arrow-right")
	previewIcon = glyph("f7-eye")
	sourceIcon  = glyph("f7-chevron-left-slash-chevron-right")
)

// programIcons are the logos of programs a pane may run, by the name kerod
// gives the foreground program, its agent or its script's runtime.
var programIcons = map[string]*ui.SVG{}

func init() {
	controlSymbols[updateIcon] = "arrow.clockwise"
	for name, logo := range map[string]string{
		"claude": "claude", "codex": "openai", "gemini": "googlegemini", "opencode": "opencode",
		"amp": "amp", "cursor-agent": "cursor", "copilot": "githubcopilot", "qwen": "qwen",
		"kimi": "kimi", "cline": "cline",
		"node": "nodedotjs", "bun": "bun", "deno": "deno", "python": "python", "python3": "python",
		"vim": "vim", "nvim": "neovim", "git": "git", "docker": "docker",
	} {
		programIcons[name] = glyph("si-" + logo)
	}
}

// programIcon is the logo of the pane's foreground program, as Claude's
// for Claude Code and Node.js's for a script node runs; the terminal's
// while the shell is in the foreground or the program has none.
func programIcon(p *proto.Pane) *ui.SVG {
	for _, name := range []string{p.Agent, p.Process, p.Runtime} {
		if i := programIcons[name]; i != nil {
			return i
		}
	}
	return terminalIcon
}
