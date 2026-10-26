//go:build !windows

package clilink

import (
	"os"
	"path/filepath"
	"testing"
)

// real is a path with its links resolved, as Link keeps it.
func real(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The commands are links to the app, and an install that has kero gains
// shibu beside it.
func TestLinkKeepsKeroAndAddsShibu(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "app")
	if err := os.WriteFile(exe, []byte("app"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe = real(t, exe)
	bin := t.TempDir()
	// A command installed before shibu: alone, the link kero.
	if err := os.Symlink(exe, filepath.Join(bin, "kero")); err != nil {
		t.Fatal(err)
	}
	if err := Link(exe, bin); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		got, err := os.Readlink(filepath.Join(bin, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != exe {
			t.Errorf("%s runs %q, want %q", name, got, exe)
		}
	}
}

// A link to another executable is replaced.
func TestLinkReplacesAnotherTarget(t *testing.T) {
	old := filepath.Join(t.TempDir(), "old")
	if err := os.WriteFile(old, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "app")
	if err := os.WriteFile(exe, []byte("app"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe = real(t, exe)
	bin := t.TempDir()
	if err := os.Symlink(old, filepath.Join(bin, "shibu")); err != nil {
		t.Fatal(err)
	}
	if err := Link(exe, bin); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if got, err := os.Readlink(filepath.Join(bin, name)); err != nil || got != exe {
			t.Errorf("%s runs %q (%v), want %q", name, got, err, exe)
		}
	}
}

// A command that starts kerod links the commands to the executable it runs,
// not to itself: a link to a link would be a loop.
func TestLinkResolvesItsOwnCommand(t *testing.T) {
	app := filepath.Join(t.TempDir(), "app")
	if err := os.WriteFile(app, []byte("app"), 0o755); err != nil {
		t.Fatal(err)
	}
	app = real(t, app)
	bin := t.TempDir()
	if err := os.Symlink(app, filepath.Join(bin, "kero")); err != nil {
		t.Fatal(err)
	}
	if err := Link(filepath.Join(bin, "kero"), bin); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		got, err := os.Readlink(filepath.Join(bin, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != app {
			t.Errorf("%s runs %q, want %q", name, got, app)
		}
	}
}
