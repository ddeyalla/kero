//go:build !darwin

package main

import "image"

// renderSymbol has no SF Symbols off macOS: the parts draw their SVGs.
func renderSymbol(string, float64, float64, int) (*image.RGBA, bool) { return nil, false }
