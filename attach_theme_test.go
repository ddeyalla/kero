package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"kero/internal/client"
	"kero/internal/proto"
)

// attachFake is a daemon socket that holds the attach handshake until the
// test releases it. It reports when the terminal takes its first resize.
type attachFake struct {
	release     chan struct{}
	accepted    chan struct{}
	welcomed    chan struct{}
	resized     chan struct{}
	themes      chan proto.Theme
	closed      chan struct{}
	releaseOnce sync.Once
	resizeOnce  sync.Once
	closeOnce   sync.Once
	mu          sync.Mutex
	conn        net.Conn
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
		themes:   make(chan proto.Theme, 1),
		closed:   make(chan struct{}),
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		defer func() {
			conn.Close()
			f.mu.Lock()
			f.conn = nil
			f.mu.Unlock()
			f.closeOnce.Do(func() { close(f.closed) })
		}()
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
			kind, payload, err := proto.ReadFrame(conn)
			if err != nil {
				return
			}
			switch kind {
			case proto.FrameResize:
				f.resizeOnce.Do(func() { close(f.resized) })
			case proto.FrameTheme:
				if theme, ok := proto.ParseTheme(payload); ok {
					select {
					case f.themes <- theme:
					default:
					}
				}
			}
		}
	}()
	t.Cleanup(func() {
		f.mu.Lock()
		if f.conn != nil {
			f.conn.Close()
		}
		f.mu.Unlock()
		f.closeOnce.Do(func() { close(f.closed) })
		f.releaseOnce.Do(func() { close(f.release) })
		ln.Close()
	})
	return f
}

func (f *attachFake) stream(t *testing.T) *client.Stream {
	t.Helper()
	type result struct {
		stream *client.Stream
		err    error
	}
	done := make(chan result, 1)
	go func() {
		s, err := client.Attach("a")
		done <- result{s, err}
	}()
	<-f.accepted
	f.releaseOnce.Do(func() { close(f.release) })
	<-f.welcomed
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	return r.stream
}

func colorBytes(c ui.Color) [4]byte { return [4]byte{c.R, c.G, c.B, c.A} }

// publishTerm installs the terminal on the UI owner and reconciles the
// current theme, font and settings. Draw the published terminal to make
// MyGo report its applied theme to the stream.
func TestPublishTermReconcilesLatestThemeAndDropsStale(t *testing.T) {
	if testing.Short() {
		t.Skip("loads libghostty-vt")
	}
	f := startAttachFake(t)
	a := testApp(t)
	a.conn = connReady
	stream := f.stream(t)
	g := &gated{Stream: stream, ready: make(chan struct{}), hold: &a.sizeHold}
	o := a.termOptions("a", g)
	o.ResizeByConn = true
	term, err := terminal.New(o)
	if err != nil {
		stream.Close()
		t.Fatal(err)
	}
	close(g.ready)
	e := &termEntry{}
	a.terms["a"] = e

	var got *palette
	tt := ui.NewTester(func(c *ui.Context) {
		got = a.paletteFor(c)
		runAmbientLater(a)
	}, 200, 100)
	red := ui.RGB(200, 40, 40)
	a.lendAmbient("p1", "fake", true, tintedView(red))
	tt.Frame()
	tt.Advance(ambientFade / 2)
	tt.Frame()
	if got.card == a.accentPal(false).withAmbient(red, 1).card {
		t.Fatal("the fade had already settled")
	}

	a.publishTerm("a", e, term, stream)
	vt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term) }, 200, 100)
	vt.Frame()
	select {
	case theme := <-f.themes:
		want, _ := a.termThemes()
		if theme.Cursor != colorBytes(want.Cursor) {
			t.Fatalf("published cursor = %v, want the current fade %v", theme.Cursor, colorBytes(want.Cursor))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the published terminal reported no theme")
	}

	// A fade that settled while the terminal had no e.term is reconciled
	// too, rather than keeping the pre-attach snapshot.
	tt.Advance(ambientFade * 2)
	tt.Frame()
	a.publishTerm("a", e, term, stream)
	vt.Frame()
	select {
	case theme := <-f.themes:
		want, _ := a.termThemes()
		if theme.Cursor != colorBytes(want.Cursor) {
			t.Fatalf("settled cursor = %v, want the settled fade %v", theme.Cursor, colorBytes(want.Cursor))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the settled terminal reported no theme")
	}

	// A custom pair is also reconciled at publication, rather than keeping
	// the snapshot taken before the attach.
	a.set.LightTheme, a.set.DarkTheme = "Nord", "Nord"
	a.publishTerm("a", e, term, stream)
	vt.Frame()
	select {
	case theme := <-f.themes:
		want, err := terminal.GhosttyTheme("Nord")
		if err != nil {
			t.Fatal(err)
		}
		if theme.Cursor != colorBytes(want.Cursor) {
			t.Fatalf("custom cursor = %v, want %v", theme.Cursor, colorBytes(want.Cursor))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the custom terminal reported no theme")
	}

	// A replacement that won the entry while the attach was in flight is
	// rejected, and both the terminal and its stream are closed.
	other := &termEntry{}
	a.terms["a"] = other
	a.publishTerm("a", e, term, stream)
	select {
	case <-f.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the rejected attach kept its stream")
	}
	if a.terms["a"] != other {
		t.Fatal("the rejected attach replaced the current entry")
	}
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
	oldNew := newTerminal
	created := make(chan *terminal.Terminal, 1)
	newTerminal = func(o terminal.Options) (*terminal.Terminal, error) {
		term, err := terminal.New(o)
		if err == nil {
			created <- term
		}
		return term, err
	}
	defer func() { newTerminal = oldNew }()
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
	deadline := time.After(5 * time.Second)
	for {
		select {
		case term := <-created:
			term.Close()
			select {
			case <-f.closed:
			case <-time.After(5 * time.Second):
				t.Fatal("the completed attach did not close its stream")
			}
			if e := a.terms["a"]; e == nil {
				t.Fatal("attach entry disappeared")
			}
			return
		case <-deadline:
			t.Fatal("the slow attach did not complete")
		default:
			// Keep the frame thread writing the fade while the worker
			// reads its snapshot. The race detector catches the old
			// worker's termOptions read here.
			tt.Advance(time.Millisecond)
			tt.Frame()
		}
	}
}
