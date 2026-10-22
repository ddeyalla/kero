package main

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// cost is what some work allocated on the heap, in bytes and in objects.
type cost struct{ bytes, objects float64 }

// echoFrameCosts returns what echoing a key costs a terminal pane, a call
// on average: the daemon's side, as the program's output reaches its
// terminal and the mirror makes a frame of it, and the app's, as its
// terminal takes the frame and the window builds and paints. Both
// terminals keep mb megabytes of scrollback, full.
func echoFrameCosts(t *testing.T, mb int) (daemon, app cost) {
	t.Helper()
	done := make(chan struct{})
	defer close(done)
	server, err := terminal.New(terminal.Options{Conn: benchmarkConn{done}, Scrollback: mb << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := terminal.New(terminal.Options{Conn: benchmarkConn{done}, ResizeByConn: true, Scrollback: mb << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server.Resize(120, 36)
	client.Resize(120, 36)
	var b strings.Builder
	for i := 0; b.Len() < 16<<20; i++ {
		fmt.Fprintf(&b, "\x1b[32m%6d\x1b[0m  the quick brown fox jumps over the lazy dog %x\r\n", i, i*7919)
	}
	server.Feed([]byte(b.String()))
	client.Feed([]byte(b.String()))
	mirror, err := server.NewMirror()
	if err != nil {
		t.Fatal(err)
	}
	defer mirror.Close()
	frame, _ := mirror.Frame()
	client.Feed(frame)
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, client).Fill() }, 1200, 720)
	tt.Frame()

	var m0, m1, m2 runtime.MemStats
	const warm, n = 5, 50
	runtime.GC()
	for i := range warm + n {
		runtime.ReadMemStats(&m0)
		server.Feed([]byte(fmt.Sprintf("\r\x1b[K> typed character %d", i)))
		frame, _ := mirror.Frame()
		runtime.ReadMemStats(&m1)
		client.Feed(frame)
		tt.Frame()
		runtime.ReadMemStats(&m2)
		if i < warm {
			continue
		}
		daemon.bytes += float64(m1.TotalAlloc - m0.TotalAlloc)
		daemon.objects += float64(m1.Mallocs - m0.Mallocs)
		app.bytes += float64(m2.TotalAlloc - m1.TotalAlloc)
		app.objects += float64(m2.Mallocs - m1.Mallocs)
	}
	daemon.bytes, daemon.objects = daemon.bytes/n, daemon.objects/n
	app.bytes, app.objects = app.bytes/n, app.objects/n
	return daemon, app
}

// A key's echo costs the same with 1 MB of scrollback as with 16 MB, on
// both sides, and stays within today's budget (docs/perf.md): the
// allocations of a frame are what the garbage collector of the app pays
// for every frame of a streaming pane.
func TestFrameAllocsFlatWithScrollback(t *testing.T) {
	smallDaemon, smallApp := echoFrameCosts(t, 1)
	daemon, app := echoFrameCosts(t, 16)
	t.Logf("daemon: 1 MB %.0f B in %.1f objects, 16 MB %.0f B in %.1f objects", smallDaemon.bytes, smallDaemon.objects, daemon.bytes, daemon.objects)
	t.Logf("app:    1 MB %.0f B in %.1f objects, 16 MB %.0f B in %.1f objects", smallApp.bytes, smallApp.objects, app.bytes, app.objects)
	for _, c := range []struct {
		side         string
		small, large cost
		budget       cost
	}{
		{"daemon", smallDaemon, daemon, cost{52 << 10, 1400}},
		{"app", smallApp, app, cost{20 << 10, 350}},
	} {
		if c.large.bytes > 1.5*c.small.bytes+4096 || c.large.objects > 1.2*c.small.objects+8 {
			t.Errorf("%s: a frame allocates with the scrollback: %.0f B in %.1f objects at 1 MB, %.0f B in %.1f objects at 16 MB",
				c.side, c.small.bytes, c.small.objects, c.large.bytes, c.large.objects)
		}
		// Today's cost and about half again: a change that makes frames
		// cost more moves the budget on purpose, in docs/perf.md too.
		if c.large.bytes > c.budget.bytes || c.large.objects > c.budget.objects {
			t.Errorf("%s: a frame allocates %.0f B in %.1f objects, over its budget of %.0f B in %.0f objects",
				c.side, c.large.bytes, c.large.objects, c.budget.bytes, c.budget.objects)
		}
	}
}
