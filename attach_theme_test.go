package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"kero/internal/proto"

	"github.com/egoist/mygo/ui"
)

// attachFake is a daemon socket that holds the attach handshake until the
// test releases it. It reports when the terminal takes its first resize.
type attachFake struct {
	release     chan struct{}
	accepted    chan struct{}
	welcomed    chan struct{}
	resized     chan struct{}
	releaseOnce sync.Once
	once        sync.Once
}

func startAttachFake(t *testing.T) *attachFake {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "attach-fake-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "d.sock")
	t.Setenv("KERO_SOCKET", socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	f := &attachFake{
		release:  make(chan struct{}),
		accepted: make(chan struct{}),
		welcomed: make(chan struct{}),
		resized:  make(chan struct{}),
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var hello proto.Hello
		if json.NewDecoder(conn).Decode(&hello) != nil {
			return
		}
		close(f.accepted)
		<-f.release
		if json.NewEncoder(conn).Encode(proto.Welcome{Version: proto.Version, Cols: 80, Rows: 24}) != nil {
			return
		}
		close(f.welcomed)
		for {
			kind, _, err := proto.ReadFrame(conn)
			if err != nil {
				return
			}
			if kind == proto.FrameResize {
				f.once.Do(func() { close(f.resized) })
			}
		}
	}()
	t.Cleanup(func() {
		f.once.Do(func() { close(f.resized) })
		f.releaseOnce.Do(func() { close(f.release) })
		ln.Close()
	})
	return f
}

// A slow attach must not read the ambient fade from its worker. The old
// path called termOptions in the worker after the handshake, racing the
// frame thread as the fade moved. The queued options are now a UI snapshot,
// while the published terminal reconciles the current shade on the main
// thread.
func TestSlowAttachDoesNotReadAmbientFromWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("loads libghostty-vt")
	}
	f := startAttachFake(t)
	a := testApp(t)
	a.conn = connReady
	a.ensureTerm("a")
	<-f.accepted

	var got *palette
	tt := ui.NewTester(func(c *ui.Context) {
		got = a.paletteFor(c)
		runAmbientLater(a)
	}, 200, 100)
	a.lendAmbient("p1", "fake", true, tintedView(ui.RGB(200, 40, 40)))
	tt.Frame()
	tt.Advance(ambientFade / 2)
	tt.Frame()
	if got.card == a.accentPal(false).withAmbient(ui.RGB(200, 40, 40), 1).card {
		t.Fatal("the fade had already settled")
	}

	f.releaseOnce.Do(func() { close(f.release) })
	<-f.welcomed
	for range 50 {
		// Keep the frame thread writing the fade while the worker builds
		// from its snapshot. The race detector catches the old worker's
		// termOptions read here.
		tt.Advance(time.Millisecond)
		tt.Frame()
		runtime.Gosched()
	}
	if e := a.terms["a"]; e == nil {
		t.Fatal("attach entry disappeared")
	}
}
