// Package clilink puts the shibu and kero commands in a directory: links
// to the app's executable, which is the CLI too. kero stays, so that what
// already runs it keeps working.
package clilink

import "path/filepath"

// names are the commands, in the order the app teaches them: its own, then
// the one scripts and agents already run.
var names = []string{"shibu", "kero"}

// Target returns the app's executable that the commands in bin run, ""
// when there is none.
func Target(bin string) string { return target(bin) }

// Link makes bin's commands run exe, unless they do already. It resolves
// exe's own links first, so that a command that started kerod links the
// commands to the app, not to itself.
func Link(exe, bin string) error {
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return link(exe, bin)
}
