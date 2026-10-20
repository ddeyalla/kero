//go:build !darwin

package main

import "github.com/egoist/mygo"

// setCompactTitleBar and followFullScreen are macOS's: elsewhere Kero
// draws its own window controls (ownControls).
func setCompactTitleBar(*mygo.Window) {}
func followFullScreen(*mygo.Window)   {}
