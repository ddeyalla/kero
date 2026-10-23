//go:build !darwin

package main

import "github.com/egoist/mygo/ui"

// symbolImage returns no template on systems without SF Symbols. macOS
// caches a white shape by name, size and appearance; callers apply tint
// while painting, so tint is not an argument or part of the cache key.
func symbolImage(name string, points float32, dark bool) *ui.Bitmap {
	return nil
}
