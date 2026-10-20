package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/transfer"

	"kero/internal/proto"
)

func hasAttachment(d transfer.Data) bool {
	f := d.Formats()
	if slices.Contains(f, transfer.FileList) || slices.Contains(f, transfer.PNG) {
		return true
	}
	if slices.Contains(f, transfer.URIList) {
		paths, _ := d.Files()
		return len(paths) > 0
	}
	return false
}

// attachmentText returns paths for the local program to read. It never sends
// a newline or submits a command. Screenshots stay available after paste.
func attachmentText(d transfer.Data, dir string) (string, error) {
	paths, err := d.Files()
	if err != nil && !errors.Is(err, transfer.ErrFormat) {
		return "", errors.New("Could not read copied files")
	}
	if len(paths) == 0 {
		b, err := d.Read(transfer.PNG)
		if err != nil {
			return "", errors.New("No local file or image to paste")
		}
		if len(b) > 64<<20 {
			return "", errors.New("Image is larger than 64 MB; save it and paste its path")
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
			return "", errors.New("Could not read clipboard image")
		}
		folder := filepath.Join(dir, "attachments")
		if err := os.MkdirAll(folder, 0700); err != nil {
			return "", errors.New("Could not create attachments folder")
		}
		f, err := os.CreateTemp(folder, "screenshot-*.png")
		if err != nil {
			return "", errors.New("Could not save clipboard image")
		}
		_, writeErr := f.Write(b)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			os.Remove(f.Name())
			return "", errors.New("Could not save clipboard image")
		}
		paths = []string{f.Name()}
	}
	var quoted []string
	for _, p := range paths {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n\x1b") {
			return "", errors.New("File path contains unsupported characters")
		}
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("File is unavailable: %s", filepath.Base(p))
		}
		if runtime.GOOS == "windows" {
			quoted = append(quoted, "'"+strings.ReplaceAll(p, "'", "''")+"'")
		} else {
			quoted = append(quoted, "'"+strings.ReplaceAll(p, "'", "'\\''")+"'")
		}
	}
	return strings.Join(quoted, " ") + " ", nil
}

func (a *App) pasteAttachment(id string, d transfer.Data) bool {
	if !hasAttachment(d) {
		return false
	}
	entry := a.terms[id]
	if entry == nil || entry.term == nil {
		return false
	}
	dir := proto.Dir()
	go func() {
		text, err := attachmentText(d, dir)
		a.update(func() {
			if err != nil {
				a.toast(err.Error())
				return
			}
			// Do not paste into a replacement session after a reconnect.
			if a.terms[id] != entry {
				a.toast("Pane reconnected; paste the attachment again")
				return
			}
			entry.term.Paste(text)
		})
	}()
	return true
}
