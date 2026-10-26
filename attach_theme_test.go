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
	"kero/internal/plugin/protocol"
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

func sameTheme(got proto.Theme, want *terminal.Theme) bool {
	if got.Foreground != colorBytes(want.Foreground) || got.Background != colorBytes(want.Background) ||
		got.Cursor != colorBytes(want.Cursor) {
		return false
	}
	for i := range want.Palette {
		if got.Palette[i] != colorBytes(want.Palette[i]) {
			return false
		}
	}
	return true
}

// publishTerm installs the terminal on the UI owner and reconciles the
// current theme, font and settings. Draw the published terminal to make
// MyGo report its applied theme to the stream.
func TestPublishTermReconcilesLatestThemeAndDropsStale(t *testing.T) {
	if testing.Short() {
		t.Skip("loads libghostty-vt")
	}
	type fixture struct {
		app    *App
		fake   *attachFake
		term   *terminal.Terminal
		stream *client.Stream
		entry  *termEntry
		old    *terminal.Theme
	}
	newFixture := func(t *testing.T) fixture {
		t.Helper()
		f := startAttachFake(t)
		a := testApp(t)
		a.plugins.known = ambientApp(protocol.PermAmbient).plugins.known
		a.plugins.knownAt = true
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
		t.Cleanup(func() {
			if e.term != nil {
				a.dropTerm("a")
				return
			}
			term.Close()
			stream.Close()
		})
		return fixture{a, f, term, stream, e, o.Theme}
	}

	red := ui.RGB(200, 40, 40)
	cases := []struct {
		name    string
		prepare func(t *testing.T, a *App, tt *ui.Tester, got **palette) *terminal.Theme
	}{
		{
			name: "mid fade",
			prepare: func(t *testing.T, a *App, tt *ui.Tester, got **palette) *terminal.Theme {
				base := *got
				a.lendAmbient("p1", "fake", true, tintedView(red))
				if color := a.ambientColor(); color != red {
					t.Fatalf("ambient color = %v, want %v", color, red)
				}
				tt.Frame()
				tt.Advance(ambientFade / 2)
				tt.Frame()
				if s := a.ambient.strength; s <= 0 || s >= 1 {
					t.Fatalf("mid-fade strength = %v, want between 0 and 1", s)
				}
				if (*got).card == base.card || (*got).card == base.withAmbient(red, 1).card {
					t.Fatal("the mid-fade card matched the base or the settled palette")
				}
				want, _ := a.termThemes()
				return want
			},
		},
		{
			name: "settled fade",
			prepare: func(t *testing.T, a *App, tt *ui.Tester, _ **palette) *terminal.Theme {
				a.lendAmbient("p1", "fake", true, tintedView(red))
				if color := a.ambientColor(); color != red {
					t.Fatalf("ambient color = %v, want %v", color, red)
				}
				tt.Frame()
				tt.Advance(ambientFade * 2)
				tt.Frame()
				tt.Frame()
				if s := a.ambient.strength; s != 1 {
					t.Fatalf("settled strength = %v, want 1", s)
				}
				want, _ := a.termThemes()
				return want
			},
		},
		{
			name: "custom theme",
			prepare: func(t *testing.T, a *App, _ *ui.Tester, _ **palette) *terminal.Theme {
				a.set.LightTheme, a.set.DarkTheme = "Nord", "Nord"
				want, err := terminal.GhosttyTheme("Nord")
				if err != nil {
					t.Fatal(err)
				}
				return want
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t)
			var got *palette
			tt := ui.NewTester(func(c *ui.Context) {
				got = fx.app.paletteFor(c)
				runAmbientLater(fx.app)
			}, 200, 100)
			tt.Frame()
			if fx.entry.term != nil {
				t.Fatal("the fixture terminal was installed before publication")
			}
			want := tc.prepare(t, fx.app, tt, &got)
			if fx.entry.term != nil {
				t.Fatal("the terminal was installed while the fade or theme changed")
			}
			if fx.app.terms["a"] != fx.entry {
				t.Fatal("the fixture entry left the table before publication")
			}
			if want.Cursor == fx.old.Cursor {
				t.Fatal("the fixture old snapshot already has the expected cursor")
			}

			fx.app.publishTerm("a", fx.entry, fx.term, fx.stream)
			if fx.entry.term != fx.term || fx.entry.stream != fx.stream {
				t.Fatal("the fixture terminal did not publish")
			}
			vt := ui.NewTester(func(c *ui.Context) { terminal.View(c, fx.term) }, 200, 100)
			vt.Frame()
			select {
			case theme := <-fx.fake.themes:
				if !sameTheme(theme, want) {
					t.Fatalf("published theme = %v, want the current theme %v", theme, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the published terminal reported no theme")
			}
		})
	}

	t.Run("stale replacement", func(t *testing.T) {
		fx := newFixture(t)
		other := &termEntry{}
		fx.app.terms["a"] = other
		fx.app.publishTerm("a", fx.entry, fx.term, fx.stream)
		select {
		case <-fx.fake.closed:
		case <-time.After(5 * time.Second):
			t.Fatal("the rejected attach kept its stream")
		}
		if fx.app.terms["a"] != other {
			t.Fatal("the rejected attach replaced the current entry")
		}
		if fx.entry.term != nil {
			t.Fatal("the rejected attach installed its terminal")
		}
	})
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
			t.Cleanup(func() {
				b.term.Close()
				b.stream.Close()
			})
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
	type built struct {
		term   *terminal.Terminal
		stream *client.Stream
		ready  <-chan struct{}
		err    error
	}
	created := make(chan built, 1)
	newTerminal = func(o terminal.Options) (*terminal.Terminal, error) {
		term, err := terminal.New(o)
		g := o.Conn.(*gated)
		created <- built{term, g.Stream, g.ready, err}
		return term, err
	}
	defer func() { newTerminal = oldNew }()
	a := testApp(t)
	a.plugins.known = ambientApp(protocol.PermAmbient).plugins.known
	a.plugins.knownAt = true
	a.conn = connReady
	a.ensureTerm("a")
	<-f.accepted

	var got *palette
	tt := ui.NewTester(func(c *ui.Context) {
		got = a.paletteFor(c)
		runAmbientLater(a)
	}, 200, 100)
	tt.Frame()
	red := ui.RGB(200, 40, 40)
	a.lendAmbient("p1", "fake", true, tintedView(red))
	if color := a.ambientColor(); color != red {
		t.Fatalf("ambient color = %v, want %v", color, red)
	}
	tt.Frame()
	tt.Advance(ambientFade / 2)
	tt.Frame()
	if s := a.ambient.strength; s <= 0 || s >= 1 {
		t.Fatalf("mid-fade strength = %v, want between 0 and 1", s)
	}
	if got.card == a.accentPal(false).withAmbient(red, 1).card {
		t.Fatal("the fade had already settled")
	}

	f.releaseOnce.Do(func() { close(f.release) })
	<-f.welcomed
	deadline := time.After(5 * time.Second)
	for {
		select {
		case b := <-created:
			if b.err != nil {
				t.Fatal(b.err)
			}
			t.Cleanup(func() { b.term.Close() })
			select {
			case <-b.ready:
			case <-time.After(5 * time.Second):
				t.Fatal("the slow attach did not finish its terminal setup")
			}
			e := a.terms["a"]
			if e == nil {
				t.Fatal("attach entry disappeared")
			}
			a.publishTerm("a", e, b.term, b.stream)
			if e.term != b.term || e.stream != b.stream {
				t.Fatal("the slow attach did not publish")
			}
			a.dropTerm("a")
			select {
			case <-f.closed:
			case <-time.After(5 * time.Second):
				t.Fatal("the completed attach did not close its stream")
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
