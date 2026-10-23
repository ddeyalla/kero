//go:build !darwin

package main

import "github.com/egoist/mygo/ui"

// symbolImage is no symbol on a system that has none: SF Symbols are
// macOS's. A pane that shows one shows nothing there, as a view whose
// target this system cannot draw does.
func symbolImage(name string, points float32, dark bool) *ui.Bitmap {
	return nil
}
