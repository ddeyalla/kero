package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// The icons are Framework7's, drawn after SF Symbols so that they feel at
// home on macOS, and one of Ionicons', as Iconify serves them: see
// icons/README.md.
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
	terminalIcon    = glyph("ion-terminal-outline")
	returnIcon      = glyph("f7-return")
	gearIcon        = glyph("f7-gear-alt")
	updateIcon      = icon(`<path d="M13 5a5.5 5.5 0 1 0 .3 5M13 2v3H10"/>`)
	splitRightIcon  = glyph("f7-square-split-2x1")
	splitDownIcon   = glyph("f7-square-split-1x2")
	zoomIcon        = glyph("f7-arrow-up-left-arrow-down-right")
	unzoomIcon      = glyph("f7-arrow-down-right-arrow-up-left")
	closeIcon       = glyph("f7-xmark")
	swapIcon        = glyph("f7-arrow-right-arrow-left")
)
