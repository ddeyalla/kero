package daemon

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
)

// allocated returns what each call of f allocates on the heap, in bytes
// and in objects, over n calls. testing.AllocsPerRun counts objects alone,
// and a copy of the whole scrollback is still one object.
func allocated(n int, f func()) (bytes, objects float64) {
	f() // what a first call sets up
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range n {
		f()
	}
	runtime.ReadMemStats(&after)
	return float64(after.TotalAlloc-before.TotalAlloc) / float64(n), float64(after.Mallocs-before.Mallocs) / float64(n)
}

// scrollbackPane is a pane whose terminal keeps mb megabytes of
// scrollback, full, with an agent's screen at its end.
func scrollbackPane(t *testing.T, mb int) *pane {
	t.Helper()
	s := testPane(t)
	s.term.Close()
	term, err := terminal.New(terminal.Options{Conn: &headless{s: s, closed: make(chan struct{})}, Scrollback: mb << 20})
	if err != nil {
		t.Fatal(err)
	}
	s.term = term
	s.term.Resize(s.info.Cols, s.info.Rows)
	fill(s)
	s.show(strings.Repeat("a line of an agent's screen\n", s.info.Rows-1) + "> ")
	return s
}

// What status detection reads of the screen, several times a second for
// every agent pane, does not grow with the scrollback: reading 1 MB of it
// and 16 MB allocate the same. The whole text as it was read before
// (oldScreen) does grow, which shows the test can tell.
func TestScreenAllocsFlatWithScrollback(t *testing.T) {
	t.Setenv("KERO_CONFIG", filepath.Join(t.TempDir(), "config"))
	type cost struct{ bytes, objects float64 }
	measure := func(mb int) (screen, whole cost) {
		s := scrollbackPane(t, mb)
		s.mu.Lock()
		defer s.mu.Unlock()
		screen.bytes, screen.objects = allocated(50, func() { s.screen() })
		whole.bytes, whole.objects = allocated(3, func() { oldScreen(s) })
		return screen, whole
	}
	small, smallWhole := measure(1)
	large, largeWhole := measure(16)
	t.Logf("screen(): 1 MB %.0f B in %.1f objects, 16 MB %.0f B in %.1f objects", small.bytes, small.objects, large.bytes, large.objects)
	t.Logf("whole text: 1 MB %.0f B, 16 MB %.0f B", smallWhole.bytes, largeWhole.bytes)
	if largeWhole.bytes < 4*smallWhole.bytes {
		t.Fatalf("the whole text did not grow with the scrollback (%.0f B, then %.0f B): the test cannot tell", smallWhole.bytes, largeWhole.bytes)
	}
	if large.bytes > 1.5*small.bytes+4096 || large.objects > small.objects+4 {
		t.Errorf("screen() allocates with the scrollback: %.0f B in %.1f objects at 1 MB, %.0f B in %.1f objects at 16 MB",
			small.bytes, small.objects, large.bytes, large.objects)
	}
	// The budget: a screen's text and little more.
	if limit := 16.0 * 1024; large.bytes > limit {
		t.Errorf("screen() allocates %.0f B a call, over its budget of %.0f B", large.bytes, limit)
	}
}
