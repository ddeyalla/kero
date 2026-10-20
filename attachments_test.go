package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/transfer"
)

func TestAttachmentPaths(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "it's a screenshot.png")
	if err := os.WriteFile(p, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	d, _ := transfer.FileData(p)
	text, err := attachmentText(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && text != "'"+strings.ReplaceAll(p, "'", "'\\''")+"' " {
		t.Fatalf("unsafe path: %q", text)
	}
	if _, err := attachmentText(transfer.TextData("hello"), dir); err == nil {
		t.Fatal("text treated as attachment")
	}
}

func TestScreenshotSavedPrivately(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	dir := t.TempDir()
	d := transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, b.Bytes())))
	text, err := attachmentText(d, dir)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "attachments", "*.png"))
	if len(files) != 1 || !strings.Contains(text, files[0]) {
		t.Fatalf("missing screenshot: %q, %v", text, files)
	}
	st, _ := os.Stat(files[0])
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", st.Mode())
	}
	got, _ := os.ReadFile(files[0])
	if !bytes.Equal(got, b.Bytes()) {
		t.Fatal("image changed")
	}
	bad := transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, []byte("bad"))))
	if _, err := attachmentText(bad, dir); err == nil {
		t.Fatal("accepted invalid PNG")
	}
}
