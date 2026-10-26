package clilink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// On Windows the commands are copies of the app's executable that run in
// the console: the app's runs without one (the GUI subsystem), so shells
// would neither wait for it nor show what it prints. kero.target names the
// executable they are copies of, as it was then: its name stays, so that
// an installed kero keeps updating.

// targetFile is where the executable's stamp stays, as kero installed it.
const targetFile = "kero.target"

func target(bin string) string {
	b, err := os.ReadFile(filepath.Join(bin, targetFile))
	if err != nil {
		return ""
	}
	exe, _, _ := strings.Cut(string(b), "\n")
	return exe
}

// commands returns the commands' paths, in the order of their names.
func commands(bin string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = filepath.Join(bin, name+".exe")
	}
	return out
}

// installed is the command whose executable copy link keeps: kero's, the
// one kerod has kept since before shibu, so that an installed command
// updates in place.
const installed = "kero"

// stamp tells an executable's version: its path, size and time.
func stamp(exe string) (string, error) {
	st, err := os.Stat(exe)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n%d\n%d\n", exe, st.Size(), st.ModTime().UnixNano()), nil
}

func link(exe, bin string) error {
	if strings.EqualFold(filepath.Dir(exe), filepath.Clean(bin)) {
		return nil // the command itself, which started kerod
	}
	want, err := stamp(exe)
	if err != nil {
		return err
	}
	cmd := filepath.Join(bin, installed+".exe")
	if have, err := os.ReadFile(filepath.Join(bin, targetFile)); err == nil && string(have) == want {
		if all(commands(bin)) {
			return nil
		}
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if b, err = consoleExecutable(b); err != nil {
		return err
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	tmp := cmd + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	// A running executable can't be replaced, but it can be moved: the
	// last one goes aside, to be removed once nothing runs it.
	olds, _ := filepath.Glob(filepath.Join(bin, "*.exe.old*"))
	for _, o := range olds {
		os.Remove(o)
	}
	if _, err := os.Stat(cmd); err == nil {
		if err := os.Rename(cmd, fmt.Sprintf("%s.old%d", cmd, os.Getpid())); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, cmd); err != nil {
		return err
	}
	if err := alias(bin); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bin, targetFile), []byte(want), 0o644)
}

// all tells whether every command is there.
func all(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// alias links each of the other commands to the installed one: a hard link,
// which is one file on disk, or a copy where the file system has no links.
// It runs after the installed command is replaced, so that a link never
// holds the one it replaced.
func alias(bin string) error {
	cmd := filepath.Join(bin, installed+".exe")
	for _, name := range names {
		if name == installed {
			continue
		}
		link := filepath.Join(bin, name+".exe")
		os.Remove(link)
		if os.Link(cmd, link) == nil {
			continue
		}
		b, err := os.ReadFile(cmd)
		if err != nil {
			return err
		}
		if err := os.WriteFile(link, b, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// consoleExecutable turns a GUI executable into a console one: its
// subsystem, and without its signature, which the change breaks.
func consoleExecutable(b []byte) ([]byte, error) {
	bad := errors.New("clilink: not a Windows executable")
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return nil, bad
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	opt := pe + 24 // the optional header, after the signature and file header
	if pe < 0 || opt+128 > len(b) || !bytes.Equal(b[pe:pe+4], []byte("PE\x00\x00")) {
		return nil, bad
	}
	var dirs int // the data directories
	switch binary.LittleEndian.Uint16(b[opt:]) {
	case 0x10b: // PE32
		dirs = opt + 96
	case 0x20b: // PE32+
		dirs = opt + 112
	default:
		return nil, bad
	}
	const guiSubsystem, consoleSubsystem = 2, 3
	if binary.LittleEndian.Uint16(b[opt+68:]) == guiSubsystem {
		binary.LittleEndian.PutUint16(b[opt+68:], consoleSubsystem)
	}
	// The certificate table, the fifth directory: a file offset, at the
	// end of the file.
	sec := dirs + 4*8
	if sec+8 <= len(b) {
		off := int(binary.LittleEndian.Uint32(b[sec:]))
		size := int(binary.LittleEndian.Uint32(b[sec+4:]))
		if off > 0 && size > 0 && off+size == len(b) {
			b = b[:off]
			binary.LittleEndian.PutUint64(b[sec:], 0)
		}
	}
	return b, nil
}
