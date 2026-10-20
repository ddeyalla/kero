package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
)

// The icons are Framework7's, drawn after SF Symbols so that they feel at
// home on macOS, a few of other sets where it has none, and programs'
// logos from Simple Icons, as Iconify serves them: see icons/README.md.
//
//go:embed icons/*.svg
var iconFiles embed.FS

// glyph returns an icon of the set, as "f7-gear-alt".
func glyph(name string) *ui.SVG {
	b, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic("kero: no icon " + name)
	}
	return ui.MustParseSVG(b)
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

	// The Linear workspace's: the tray it is in the sidebar and Settings,
	// and the marks of what an issue has and what can be done with it.
	trayIcon      = glyph("f7-tray-fill")
	refreshIcon   = glyph("f7-arrow-clockwise")
	boardIcon     = glyph("f7-rectangle-split-3x1")
	listIcon      = glyph("f7-list-bullet")
	externalIcon  = glyph("f7-arrow-up-right-square")
	copyIcon      = glyph("f7-doc-on-doc")
	linkIcon      = glyph("f7-link")
	milestoneIcon = glyph("f7-suit-diamond")
	projectIcon   = glyph("f7-cube")
	attachIcon    = glyph("f7-paperclip")
	docIcon       = glyph("f7-doc-text")
	filterIcon    = glyph("f7-line-horizontal-3-decrease")
	blockingIcon  = glyph("f7-arrow-turn-down-right")
	subIssueIcon  = glyph("f7-list-bullet-indent")
	backIcon      = glyph("f7-chevron-left")

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
