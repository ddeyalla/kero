package clilink

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// command is a name's path in bin, as link makes it.
func command(bin, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(bin, name)
}

// Link makes every command run the app, and Target names it.
func TestLinkMakesEveryCommand(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "app")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("app"), 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	exe = real
	bin := t.TempDir()
	if err := Link(exe, bin); err != nil {
		t.Fatal(err)
	}
	if got := Target(bin); got != exe {
		t.Errorf("target %q, want %q", got, exe)
	}
	for _, name := range names {
		st, err := os.Stat(command(bin, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if st.Size() == 0 {
			t.Errorf("%s: empty", name)
		}
	}
	// The commands are shibu, and kero for what already runs it.
	if len(names) != 2 || names[0] != "shibu" || names[1] != "kero" {
		t.Errorf("names %q", names)
	}
	// Again: nothing to do.
	if err := Link(exe, bin); err != nil {
		t.Fatal(err)
	}
	if got := Target(bin); got != exe {
		t.Errorf("target after a second link %q", got)
	}
}
