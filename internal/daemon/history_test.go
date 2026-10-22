package daemon

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
)

// historyTerm is a terminal of cols by rows that a history is fed to.
func historyTerm(t testing.TB, cols, rows int) *terminal.Terminal {
	t.Helper()
	done := make(chan struct{})
	term, err := terminal.New(terminal.Options{Conn: quiet{done}, Scrollback: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(done); term.Close() })
	term.Resize(cols, rows)
	return term
}

// lastRows is the text of a terminal's last n rows with something on them.
func lastRows(term *terminal.Terminal, n int) string {
	lines := strings.Split(strings.TrimRight(term.Text(), "\n "), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// busyHistory is the history of a terminal that showed lines lines of
// colored, some of them soft-wrapped, output.
func busyHistory(t testing.TB, cols, rows, lines int) []byte {
	src := historyTerm(t, cols, rows)
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "\x1b[3%dmline %d\x1b[0m %s\r\n", i%8, i, strings.Repeat("x", i%(2*cols)))
	}
	src.Feed([]byte(b.String()))
	return src.History()
}

// The tail of a history restores the same last screen as the whole of
// it, cut where a row ended, and a history that fits is kept whole.
func TestHistoryTailKeepsLastScreen(t *testing.T) {
	const cols, rows = 80, 24
	full := busyHistory(t, cols, rows, 20000)
	tail := historyTail(full, 64<<10)
	if len(tail) > 64<<10 || len(tail) == 0 {
		t.Fatalf("the tail of %d bytes is %d", len(full), len(tail))
	}
	if !bytes.HasSuffix(full, tail) {
		t.Fatal("the tail is not the end of the history")
	}
	if cut := full[len(full)-len(tail)-2 : len(full)-len(tail)]; string(cut) != "\r\n" {
		t.Fatalf("cut after %q, not where a row ended", cut)
	}
	a, b := historyTerm(t, cols, rows), historyTerm(t, cols, rows)
	a.Feed(full)
	b.Feed(tail)
	if got, want := lastRows(b, rows), lastRows(a, rows); got != want {
		t.Fatalf("the tail restores\n%s\nthe whole\n%s", got, want)
	}
	if small := busyHistory(t, cols, rows, 10); !bytes.Equal(historyTail(small, 64<<10), small) {
		t.Fatal("a history that fits was cut")
	}
	// One long wrapped line has nowhere to cut: kept whole.
	long := bytes.Repeat([]byte("y"), 1000)
	if got := historyTail(long, 100); !bytes.Equal(got, long) {
		t.Fatalf("a line with no row end was cut to %d bytes", len(got))
	}
}

// A short history file restores, as an empty one does: the pane comes
// back with what the file had, or with nothing, under a new shell.
func TestRestoreShortAndEmptyHistory(t *testing.T) {
	for name, h := range map[string][]byte{
		"short": historyTail(busyHistory(t, 80, 24, 5000), 4<<10),
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			d := linearTestDaemon(t)
			os.MkdirAll(filepath.Dir(d.historyPath("t1")), 0o755)
			if err := os.WriteFile(d.historyPath("t1"), h, 0o600); err != nil {
				t.Fatal(err)
			}
			p := d.restorePane("s1", savedPane{ID: "t1", Dir: t.TempDir(), Cols: 80, Rows: 24, Created: 1}, "Mon Jan 2 15:04", 1)
			if p == nil {
				t.Fatal("the pane did not restore")
			}
			t.Cleanup(func() { p.close() })
			p.mu.Lock()
			text := p.term.Text()
			p.mu.Unlock()
			restored := strings.Contains(text, "restored")
			if name == "short" && (!restored || !strings.Contains(text, "line 4999")) {
				t.Fatalf("the short history did not restore its last line:\n%s", text)
			}
			if name == "empty" && restored {
				t.Fatalf("an empty history restored a mark:\n%s", text)
			}
		})
	}
}

// What a 10 s save costs a pane with 4 MB of history, whole and as its
// tail: History serializes the whole scrollback either way; the tail
// cuts what goes to disk.
func BenchmarkSaveHistories(b *testing.B) {
	full := busyHistory(b, 120, 36, 60000)
	for len(full) < 4<<20 {
		full = append(full, full...)
	}
	full = full[len(full)-4<<20:]
	dir := b.TempDir()
	for _, c := range []struct {
		name string
		h    func() []byte
	}{
		{"whole", func() []byte { return full }},
		{"tail", func() []byte { return historyTail(full, historyTailBytes) }},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				h := c.h()
				writeAtomic(filepath.Join(dir, "p.vt"), h)
				b.SetBytes(int64(len(h)))
			}
		})
	}
}

// What reading a 4 MB pane's history costs, whose whole a tail still
// serializes first (terminal.History).
func BenchmarkPaneHistory(b *testing.B) {
	done := make(chan struct{})
	term, err := terminal.New(terminal.Options{Conn: quiet{done}, Scrollback: 4 << 20})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { close(done); term.Close() })
	term.Resize(120, 36)
	for i := range 80000 {
		term.Feed(fmt.Appendf(nil, "\x1b[3%dmline %d\x1b[0m %s\r\n", i%8, i, strings.Repeat("x", i%100)))
	}
	b.ResetTimer()
	for b.Loop() {
		b.SetBytes(int64(len(term.History())))
	}
}
