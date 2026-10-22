package main

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// A first run shows the reveal over the shell once the state is in, and
// it is gone by the time the timeline ends. Starting it writes app.json,
// so a crash during it does not show it again.
func TestFirstRunIntro(t *testing.T) {
	// The control: a frame of a later launch writes none, so the write
	// below is the reveal's.
	later := testApp(t)
	ui.NewTester(later.view, 900, 700).Frame()
	if _, err := os.Stat(prefsPath()); !os.IsNotExist(err) {
		t.Fatalf("a frame without the reveal wrote app.json: %v", err)
	}

	a := testApp(t)
	a.intro.first = true
	if _, err := os.Stat(prefsPath()); !os.IsNotExist(err) {
		t.Fatalf("app.json is there before the reveal: %v", err)
	}
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	if !a.intro.started {
		t.Fatal("the first run did not start the reveal")
	}
	if _, err := os.Stat(prefsPath()); err != nil {
		t.Errorf("starting the reveal did not write app.json: %v", err)
	}
	if _, ok := tt.Find(locales.IntroLabel()); !ok {
		t.Fatalf("no reveal over the shell; texts %q", tt.Texts())
	}
	shot(t, tt, "first-run-intro")

	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	shot(t, tt, "first-run-intro-300")
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find(locales.IntroLabel()); !ok {
		t.Error("the reveal left before its time")
	}
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	if !a.intro.done {
		t.Error("the reveal did not end by 900 ms")
	}
	if _, ok := tt.Find(locales.IntroLabel()); ok {
		t.Errorf("the reveal stayed after its time; texts %q", tt.Texts())
	}
}

// Any input in the window puts the reveal away at once, so the shell is
// usable: a key, a click or a scroll.
func TestIntroCancelledByInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(tt *ui.Tester)
	}{
		{"key", func(tt *ui.Tester) { tt.Key(0, ui.KeyEscape) }},
		{"click", func(tt *ui.Tester) { tt.ClickAt(600, 350) }},
		{"scroll", func(tt *ui.Tester) { tt.Scroll(600, 350, 0, 40) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			a.intro.first = true
			tt := ui.NewTester(a.view, 900, 700)
			tt.Frame()
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
			if !a.intro.started || a.intro.done {
				t.Fatalf("the reveal is not running: started %v, done %v", a.intro.started, a.intro.done)
			}
			tc.do(tt)
			tt.Frame()
			if !a.intro.done {
				t.Errorf("a %s did not cancel the reveal", tc.name)
			}
			if _, ok := tt.Find(locales.IntroLabel()); ok {
				t.Errorf("the reveal stayed after a %s", tc.name)
			}
		})
	}
}

// cardProbe is a point in the window the shell's card covers once it
// shows, clear of the spinners at the cards' centres, and what it shows
// there.
func cardProbe(tt *ui.Tester) color.RGBA { return tt.Image().RGBAAt(840, 350) }

func rgba(c ui.Color) color.RGBA {
	return color.RGBA{c.R, c.G, c.B, 255}
}

// unloaded makes a's state not come yet, as a real start's: it returns
// the state, for setState to bring.
func unloaded(a *App) proto.State {
	st := a.st
	a.st, a.loaded, a.selected = proto.State{}, false, ""
	return st
}

// The reveal covers the window from its first frame, while kerod starts:
// the shell does not show before the state, nor on the frame it comes,
// and comes up under the overlay after.
func TestIntroWaitsForState(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	st := unloaded(a)
	card := rgba(lightPalette.card)
	tt := ui.NewTester(a.view, 900, 700)
	for range 4 {
		tt.Frame()
		if a.intro.started {
			t.Fatal("the reveal's timeline started before the state came")
		}
		if _, ok := tt.Find(locales.IntroLabel()); !ok {
			t.Fatalf("no reveal while the state is on its way; texts %q", tt.Texts())
		}
		if got := cardProbe(tt); got == card {
			t.Fatalf("the shell's card shows before the state: %v", got)
		}
		tt.Advance(100 * time.Millisecond)
	}
	shot(t, tt, "first-run-waiting")

	a.setState(st)
	tt.Frame()
	if !a.intro.started {
		t.Fatal("the state came and the reveal did not start")
	}
	if got := cardProbe(tt); got == card {
		t.Errorf("the shell showed whole on the frame the state came: %v", got)
	}
	tt.Advance(introTotal)
	tt.Frame()
	tt.Frame()
	if !a.intro.done {
		t.Error("the reveal did not end")
	}
	if got := cardProbe(tt); got != card {
		t.Errorf("the shell's card is not there after the reveal: %v, want %v", got, card)
	}
}

// A state slower than introWait skips the reveal: the shell fades in
// plainly, as it is, and a state coming later starts nothing.
func TestIntroGivesUpWaiting(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	st := unloaded(a)
	card := rgba(lightPalette.card)
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	tt.Advance(introWait / 2)
	tt.Frame()
	if a.intro.done {
		t.Fatal("the reveal gave up before its time")
	}
	tt.Advance(introWait / 2)
	tt.Frame()
	if !a.intro.done || a.intro.started {
		t.Fatalf("the reveal did not give up: done %v, started %v", a.intro.done, a.intro.started)
	}
	if _, ok := tt.Find(locales.IntroLabel()); ok {
		t.Error("the overlay stayed after the reveal gave up")
	}
	tt.Advance(introFade)
	tt.Frame()
	if got := cardProbe(tt); got != card {
		t.Errorf("the shell did not fade in after the wait: %v, want %v", got, card)
	}
	a.setState(st)
	tt.Frame()
	if a.intro.started {
		t.Error("a late state started the reveal")
	}
}

// The reveal never covers what kerod's failure says.
func TestIntroSkipsConnectionError(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	unloaded(a)
	a.conn, a.connErr = connFailed, "no kerod"
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	if !a.intro.done {
		t.Error("the reveal waits over a connection failure")
	}
	if _, ok := tt.Find(locales.IntroLabel()); ok {
		t.Error("the overlay covers the connection failure")
	}
	tt.Advance(introFade)
	tt.Frame()
	if !tt.HasText(locales.ConnCantReach()) {
		t.Errorf("no connection failure; texts %q", tt.Texts())
	}
}

// With Reduce Motion the reveal only fades: the shell and the rows come in
// where they are, and it is over in introFade.
func TestIntroReduceMotion(t *testing.T) {
	rowY := func(reduce bool) (mid, end float32) {
		a := testApp(t)
		a.intro.first = true
		tt := ui.NewTester(a.view, 900, 700)
		tt.SetPreferences(ui.Preferences{ReduceMotion: reduce, TextScale: 1})
		tt.Frame()
		tt.Advance(introFade / 3)
		tt.Frame()
		r, ok := tt.Find("demo")
		if !ok {
			t.Fatalf("no session row; texts %q", tt.Texts())
		}
		mid = r.Y
		if reduce && !a.intro.started {
			t.Fatal("the reveal did not start")
		}
		tt.Advance(introTotal)
		tt.Frame()
		tt.Frame()
		r, _ = tt.Find("demo")
		return mid, r.Y
	}
	// The control: with motion, the row is still on its way up.
	if mid, end := rowY(false); mid == end {
		t.Fatalf("the row does not move with motion either (%v); the probe sees no translation", mid)
	}
	if mid, end := rowY(true); mid != end {
		t.Errorf("the row moved with Reduce Motion: %v, then %v", mid, end)
	}
}

// recConn is a terminal's connection that records what the terminal
// writes, as typed keys.
type recConn struct {
	idleConn
	mu  sync.Mutex
	got strings.Builder
}

func (c *recConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got.Write(b)
	return len(b), nil
}

func (c *recConn) typed() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got.String()
}

// waitTyped is what the terminal was sent once it holds want, or after a
// short wait. The terminal writes to its connection on its own goroutine,
// so a check right after a frame can come first.
func (c *recConn) waitTyped(want string) string {
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(c.typed(), want) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return c.typed()
}

// The reveal holds the keyboard through the real start, where the state
// selects a session and focuses its pane: a key typed during it cancels
// it and reaches no terminal, and the pane has the keyboard after.
func TestIntroHoldsKeyboard(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	conn, st := introTerm(t, a)
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	a.setState(st)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if !a.intro.started || a.intro.done {
		t.Fatalf("the reveal is not running: started %v, done %v", a.intro.started, a.intro.done)
	}
	tt.TypeKey(0, ui.KeyX, "x")
	tt.Frame()
	tt.Frame()
	if !a.intro.done {
		t.Error("a typed key did not cancel the reveal")
	}
	if got := conn.typed(); got != "" {
		t.Errorf("the terminal got %q from the key that cancelled the reveal", got)
	}
	if !tt.Focused("Terminal") {
		t.Error("the pane does not have the keyboard after the reveal")
	}
	tt.TypeKey(0, ui.KeyY, "y")
	tt.Frame()
	if got := conn.waitTyped("y"); !strings.Contains(got, "y") {
		t.Errorf("the terminal got %q after the reveal, want the y typed", got)
	}
}

// introTerm gives a's pane a a terminal that records what it is sent, and
// makes a's state come as a real start's does.
func introTerm(t *testing.T, a *App) (*recConn, proto.State) {
	t.Helper()
	if testing.Short() {
		t.Skip("downloads libghostty-vt")
	}
	if err := terminal.Load(); err != nil {
		t.Fatal(err)
	}
	conn := &recConn{idleConn: idleConn{make(chan struct{})}}
	term, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	a.terms["a"] = &termEntry{term: term}
	return conn, unloaded(a)
}

// The reveal ending on its own gives the keyboard to the pane.
func TestIntroEndsWithPaneFocused(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	conn, st := introTerm(t, a)
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	a.setState(st)
	tt.Frame()
	tt.Advance(introTotal + 100*time.Millisecond)
	tt.Frame()
	if !a.intro.done {
		t.Fatal("the reveal did not end")
	}
	if !tt.Focused("Terminal") {
		t.Error("the pane does not have the keyboard after the reveal")
	}
	tt.TypeKey(0, ui.KeyY, "y")
	tt.Frame()
	if got := conn.waitTyped("y"); !strings.Contains(got, "y") {
		t.Errorf("the terminal got %q after the reveal, want the y typed", got)
	}
}

// A dialog opened during the reveal ends it and keeps the keyboard: the
// palette's field takes what is typed, and the pane nothing.
func TestIntroGivesWayToPalette(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	conn, st := introTerm(t, a)
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	a.setState(st)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	a.run("palette.open", nil)
	tt.Frame()
	tt.Frame()
	if !a.intro.done {
		t.Error("the reveal stayed over the palette")
	}
	tt.Type("de")
	tt.Frame()
	if !a.pal.open || a.pal.query != "de" {
		t.Errorf("the palette did not take the typing: open %v, query %q", a.pal.open, a.pal.query)
	}
	if got := conn.typed(); got != "" {
		t.Errorf("the terminal got %q typed into the palette", got)
	}
}

// The first run is the absence of app.json: once the app has one, a later
// launch has no reveal.
func TestIntroIsFirstRunOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KERO_HOME", home)
	t.Setenv("KERO_CONFIG", filepath.Join(home, "config"))
	a := newApp()
	if !a.intro.first {
		t.Fatal("a data directory with no app.json is not a first run")
	}
	a.savePrefs()
	if b := newApp(); b.intro.first {
		t.Error("a second launch with app.json is a first run")
	}
}
