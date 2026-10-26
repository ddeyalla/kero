//go:build !windows

package clilink

import (
	"os"
	"path/filepath"
)

func target(bin string) string {
	for _, name := range names {
		if t, err := os.Readlink(filepath.Join(bin, name)); err == nil {
			return t
		}
	}
	return ""
}

func link(exe, bin string) error {
	os.MkdirAll(bin, 0o755)
	for _, name := range names {
		link := filepath.Join(bin, name)
		if cur, err := os.Readlink(link); err == nil && cur == exe {
			continue
		}
		os.Remove(link)
		if err := os.Symlink(exe, link); err != nil {
			return err
		}
	}
	return nil
}
