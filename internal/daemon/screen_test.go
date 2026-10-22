package daemon

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"

	"kero/internal/detect"
	"kero/internal/proto"

	"github.com/egoist/mygo/plugins/terminal"
)

// oldScreen is screen as it was before it read the last rows alone: the
// last rows of the whole text. It stays as the oracle the new one must
// match byte for byte, for status detection to see the same.
func oldScreen(s *pane) string {
	text := strings.TrimRight(s.term.Text(), "\n")
	lines := strings.Split(text, "\n")
	return strings.Join(lines[max(0, len(lines)-s.info.Rows):], "\n") + "\n"
}

// oldReadText is readText as it was before it read the last rows alone.
func oldReadText(s *pane, scrollback bool, lines int) string {
	all := strings.Split(strings.TrimRight(s.term.Text(), "\n "), "\n")
	if !scrollback && lines <= 0 {
		lines = s.info.Rows
	}
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	for i, l := range all {
		all[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(all, "\n") + "\n"
}

// sameText checks that the screen and what kero read reads are what they
// were when they read the whole text.
func sameText(t *testing.T, s *pane, label string) {
	t.Helper()
	s.mu.Lock()
	got, want := s.screen(), oldScreen(s)
	s.mu.Unlock()
	if got != want {
		t.Fatalf("%s: screen\n%q, want\n%q", label, got, want)
	}
	for _, c := range []struct {
		scrollback bool
		lines      int
	}{{false, 0}, {false, 5}, {true, 0}, {true, 7}, {true, 100000}} {
		s.mu.Lock()
		want := oldReadText(s, c.scrollback, c.lines)
		s.mu.Unlock()
		if got := s.readText(c.scrollback, c.lines); got != want {
			t.Fatalf("%s: read %+v\n%q, want\n%q", label, c, got, want)
		}
	}
}

// fill writes output past a scrollback of 4 MB, the default.
func fill(s *pane) {
	var b strings.Builder
	for i := 0; b.Len() < 16<<20; i++ {
		fmt.Fprintf(&b, "%6d  the quick brown fox jumps over the lazy dog %x\r\n", i, i*7919)
	}
	s.term.Feed([]byte(b.String()))
}

// The screen read from the last rows is the one read from the whole text:
// with a full scrollback, blank rows at the bottom, soft-wrapped lines, a
// text shorter than the screen and a full-screen program's screen.
func TestScreenFromLastRows(t *testing.T) {
	s := testPane(t)
	sameText(t, s, "empty")
	s.term.Feed([]byte("$ ls\r\na  b  c\r\n$ "))
	sameText(t, s, "shorter than the screen")
	fill(s)
	sameText(t, s, "a full scrollback")
	s.show(claudeScreen(t, "working"))
	sameText(t, s, "an agent over a full scrollback")
	// After clear, the screen is blank but for its first rows: its last
	// rows go up into the scrollback.
	s.term.Feed([]byte("\x1b[H\x1b[2J$ clear\r\n$ "))
	sameText(t, s, "blank rows at the bottom")
	s.term.Feed([]byte("\x1b[H\x1b[2J"))
	sameText(t, s, "a blank screen")
	s.term.Feed([]byte("\x1b[H\x1b[2J\x1b[3J$ "))
	sameText(t, s, "no scrollback")
	long := strings.Repeat("0123456789", 30)
	for i := range 60 {
		s.term.Feed([]byte(fmt.Sprintf("%d %s\r\n", i, long[:i*5])))
		sameText(t, s, fmt.Sprintf("soft-wrapped lines, %d", i))
	}
	s.term.Feed([]byte(strings.Repeat("世界", 400) + "\r\n\r\n\r\n"))
	sameText(t, s, "wide characters wrapped")
	// Lines that wrap over many rows each: twice the screen's rows hold
	// fewer lines than it has rows, and more rows are read.
	s.term.Feed([]byte(strings.Repeat(long+"\r\n", 40)))
	s.mu.Lock()
	text, all := s.term.TailText(2 * s.info.Rows)
	s.mu.Unlock()
	if all || strings.Count(text, "\n") >= s.info.Rows {
		t.Fatalf("twice the screen's rows hold %d lines", strings.Count(text, "\n")+1)
	}
	sameText(t, s, "lines of many rows")
	s.term.Feed([]byte("\x1b[?1049h\x1b[H\x1b[2Jfull screen\x1b[10;5Hhere"))
	sameText(t, s, "the alternate screen")
	s.term.Feed([]byte("\x1b[?1049l"))
	sameText(t, s, "back from the alternate screen")
}

// Random output of short, long, wide and blank lines, cleared now and
// then, at sizes small and large, reads the same as from the whole text.
func TestScreenFromLastRowsRandom(t *testing.T) {
	s := testPane(t)
	r := rand.New(rand.NewPCG(1, 2))
	pieces := []string{"", " ", "x", "a short line", strings.Repeat("long ", 50), strings.Repeat("界", 70), "   trailing   "}
	sizes := [][2]int{{96, 28}, {20, 5}, {7, 3}, {200, 60}, {40, 1}}
	for i := range 400 {
		if i%40 == 0 {
			size := sizes[r.IntN(len(sizes))]
			s.mu.Lock()
			s.info.Cols, s.info.Rows = size[0], size[1]
			s.term.Resize(size[0], size[1])
			s.mu.Unlock()
		}
		var b strings.Builder
		for range r.IntN(30) {
			b.WriteString(pieces[r.IntN(len(pieces))])
			b.WriteString("\r\n")
		}
		switch r.IntN(20) {
		case 0:
			b.WriteString("\x1b[H\x1b[2J")
		case 1:
			b.WriteString("\x1b[3J")
		case 2:
			b.WriteString(fmt.Sprintf("\x1b[%d;1H", r.IntN(10)+1))
		}
		b.WriteString(pieces[r.IntN(len(pieces))])
		s.term.Feed([]byte(b.String()))
		sameText(t, s, fmt.Sprintf("step %d", i))
	}
}

// What status detection sees of recorded agent screens, over a full
// scrollback, is what it saw, and tells what it told.
func TestScreenDetectsTheSame(t *testing.T) {
	s := testPane(t)
	fill(s)
	for name, want := range map[string]detect.State{"idle": detect.Idle, "working": detect.Working, "blocked": detect.Blocked} {
		s.show(claudeScreen(t, name))
		s.mu.Lock()
		got, old := s.screen(), oldScreen(s)
		s.mu.Unlock()
		if got != old {
			t.Fatalf("%s: screen\n%q, want\n%q", name, got, old)
		}
		in := detect.Input{Screen: got, Title: "✳ Claude Code"}
		r := detect.Detect("claude", in)
		if r != detect.Detect("claude", detect.Input{Screen: old, Title: in.Title}) || r.State != want {
			t.Errorf("%s: detected %+v, want %s", name, r, want)
		}
	}
}

// BenchmarkScreen measures reading the screen of a pane with a full
// scrollback, as status detection does several times a second: from its
// last rows, and from its whole text as before, with the default limit of
// 4 MB and with 64 MB.
func BenchmarkScreen(b *testing.B) {
	b.Setenv("KERO_CONFIG", filepath.Join(b.TempDir(), "config"))
	d := &Daemon{changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	d.viewed.Store(new(string))
	for _, mb := range []int{4, 64} {
		s, err := newPane(d, "s", proto.Pane{ID: "p", Status: proto.Idle, Cols: 96, Rows: 28})
		if err != nil {
			b.Fatal(err)
		}
		s.term.Close()
		s.term, err = terminal.New(terminal.Options{Conn: &headless{s: s, closed: make(chan struct{})}, Scrollback: mb << 20})
		if err != nil {
			b.Fatal(err)
		}
		s.term.Resize(96, 28)
		fill(s)
		s.show(strings.Repeat("a line of an agent's screen\n", 27) + "> ")
		s.mu.Lock()
		b.Run(fmt.Sprintf("%dMB/rows", mb), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				s.screen()
			}
		})
		b.Run(fmt.Sprintf("%dMB/text", mb), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				oldScreen(s)
			}
		})
		s.mu.Unlock()
		s.term.Close()
	}
}
