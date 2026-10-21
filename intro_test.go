package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// A first run shows the reveal over the shell once the state is in, and
// it is gone by the time the timeline ends.
func TestFirstRunIntro(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	if !a.intro.started {
		t.Fatal("the first run did not start the reveal")
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

// Any input puts the reveal away at once, so the shell is usable.
func TestIntroCancelledByInput(t *testing.T) {
	a := testApp(t)
	a.intro.first = true
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if !a.intro.started || a.intro.done {
		t.Fatalf("the reveal is not running: started %v, done %v", a.intro.started, a.intro.done)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !a.intro.done {
		t.Error("a key did not cancel the reveal")
	}
	if _, ok := tt.Find(locales.IntroLabel()); ok {
		t.Error("the reveal stayed after a key")
	}
}

// The reveal waits for the first state: it covers the real shell, not a
// window that is still connecting.
func TestIntroWaitsForState(t *testing.T) {
	a := testApp(t)
	a.intro.first, a.loaded = true, false
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	if a.intro.started {
		t.Error("the reveal started before the state came")
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
