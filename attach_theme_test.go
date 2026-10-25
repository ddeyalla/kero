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

// Changing the scrollback while the attach handshake is blocked must
// invalidate that pending entry. The replacement attach uses the new
// limit; the old terminal is built with its old snapshot and its stale
// publication is rejected with both resources closed.
func TestPendingAttachReplacesScrollbackChange(t *testing.T) {
	if testing.Short() {
		t.Skip("loads libghostty-vt")
	}
	oldNew := newTerminal
	defer func() { newTerminal = oldNew }()
	type built struct {
		opts   terminal.Options
		term   *terminal.Terminal
		stream *client.Stream
		ready  <-chan struct{}
		err    error
	}
	builtCh := make(chan built, 2)
	newTerminal = func(o terminal.Options) (*terminal.Terminal, error) {
		term, err := terminal.New(o)
		g := o.Conn.(*gated)
		builtCh <- built{o, term, g.Stream, g.ready, err}
		return term, err
	}

	first := startAttachFake(t)
	a := testApp(t)
	a.conn = connReady
	a.ensureTerm("a")
	<-first.accepted
	oldEntry := a.terms["a"]
	if oldEntry == nil {
		t.Fatal("the first attach did not start")
	}

	oldLimit := a.set.ScrollbackBytes()
	next := a.set
	next.Scrollback = 8
	a.apply(next, false)
	if a.terms["a"] != nil {
		t.Fatal("the pending attach survived a scrollback change")
	}

	second := startAttachFake(t)
	a.ensureTerm("a")
	<-second.accepted

	first.releaseOnce.Do(func() { close(first.release) })
	second.releaseOnce.Do(func() { close(second.release) })
	<-first.welcomed
	<-second.welcomed

	var oldTerm, newTerm *terminal.Terminal
	var oldStream, newStream *client.Stream
	var gotOld, gotNew bool
	for range 2 {
		select {
		case b := <-builtCh:
			if b.err != nil {
				t.Fatal(b.err)
			}
			select {
			case <-b.ready:
			case <-time.After(5 * time.Second):
				t.Fatal("an attach did not finish its terminal setup")
			}
			switch b.opts.Scrollback {
			case oldLimit:
				gotOld = true
				oldTerm, oldStream = b.term, b.stream
			case 8 << 20:
				gotNew = true
				newTerm, newStream = b.term, b.stream
			default:
				t.Fatalf("built with scrollback %d, want %d or %d", b.opts.Scrollback, oldLimit, 8<<20)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("an attach did not build its terminal")
		}
	}
	if !gotOld || !gotNew {
		t.Fatalf("built old=%v new=%v, want both", gotOld, gotNew)
	}
	if oldTerm == nil || newTerm == nil || oldStream == nil || newStream == nil {
		t.Fatal("a built terminal is missing")
	}
	newEntry := a.terms["a"]
	if newEntry == nil || newEntry == oldEntry {
		t.Fatal("the replacement attach did not replace the pending entry")
	}
	a.publishTerm("a", oldEntry, oldTerm, oldStream)
	if a.terms["a"] != newEntry {
		t.Fatal("the stale attach replaced the current entry")
	}
	select {
	case <-first.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the stale attach did not close its stream")
	}
	a.publishTerm("a", newEntry, newTerm, newStream)
	if newEntry.term != newTerm || newEntry.stream != newStream {
		t.Fatal("the replacement attach did not publish")
	}
	a.dropTerm("a")
	select {
	case <-second.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the replacement attach did not close its stream")
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
