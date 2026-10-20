package daemon

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonRejectsInheritedSocket(t *testing.T) {
	if os.Getenv("SHIBU_TEST_SOCKET_CHILD") == "1" {
		err := Run()
		if err == nil || !strings.Contains(err.Error(), "socket belongs to a running daemon") {
			t.Fatalf("expected live socket refusal, got %v", err)
		}
		return
	}
	path := testSocketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonRejectsInheritedSocket$")
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		switch key {
		case "KERO_HOME", "KERO_CONFIG", "KERO_SOCKET", "KERO_SESSION", "KERO_PANE", "SHIBU_TEST_SOCKET_CHILD":
			continue
		}
		cmd.Env = append(cmd.Env, variable)
	}
	cmd.Env = append(cmd.Env, "SHIBU_TEST_SOCKET_CHILD=1", "KERO_HOME="+home, "KERO_SOCKET="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("daemon did not refuse inherited socket: %v\n%s", err, output)
	}
	assertSocketAlive(t, ln, path)
	if _, err := os.Stat(filepath.Join(home, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("daemon proceeded to session persistence: %v", err)
	}
}

func testSocketPath(t *testing.T) string {
	t.Helper()
	// macOS limits socket paths to 104 bytes; nested t.TempDir paths exceed it.
	dir, err := os.MkdirTemp("", "ks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func assertSocketAlive(t *testing.T, ln net.Listener, path string) {
	t.Helper()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			io.WriteString(c, "original")
			c.Close()
		}
	}()
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	b, err := io.ReadAll(c)
	if err != nil || string(b) != "original" {
		t.Fatalf("original listener unreachable: %q, %v", b, err)
	}
}

func TestSocketPreservesLegacyLiveDaemon(t *testing.T) {
	path := testSocketPath(t)
	ln, err := net.Listen("unix", path) // A daemon predating socket-level locks.
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if other, err := listenSocket(path); err == nil {
		other.Close()
		t.Fatal("replaced a live legacy socket")
	}
	assertSocketAlive(t, ln, path)
}

func TestSocketRejectsConcurrentDaemon(t *testing.T) {
	path := testSocketPath(t)
	ln, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if other, err := listenSocket(path); err == nil {
		other.Close()
		t.Fatal("second listener acquired the same socket")
	}
	assertSocketAlive(t, ln, path)
}

func TestSocketRecoversStaleAddress(t *testing.T) {
	path := testSocketPath(t)
	old, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	old.SetUnlinkOnClose(false)
	old.Close()
	ln, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	assertSocketAlive(t, ln, path)
	ln.Close()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket not removed on close: %v", err)
	}
	// Closing twice must not unlock a later listener or delete its address.
	next, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	ln.Close()
	assertSocketAlive(t, next, path)
}

func TestSocketPreservesRegularFileAndSymlink(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "symlink"}[symlink], func(t *testing.T) {
			path := testSocketPath(t)
			if symlink {
				if err := os.Symlink("missing-target", path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if ln, err := listenSocket(path); err == nil {
				ln.Close()
				t.Fatal("replaced a non-socket")
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("original path changed: %v", err)
			}
		})
	}
}

func TestSocketClosePreservesReplacement(t *testing.T) {
	path := testSocketPath(t)
	ln, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	ln.Close()
	assertSocketAlive(t, replacement, path)
}
