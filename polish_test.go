package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// A project's hover chrome stays for a moment after the pointer leaves it,
// so that moving diagonally off the header does not flicker the chevron
// and the add button away.
func TestProjectHoverExitDelay(t *testing.T) {
	a := testApp(t)
	a.st.Projects[0].Name = "proj"
	tt := ui.NewTester(a.view, 900, 700)
	frames(tt)

	hdr, ok := tt.Find("proj")
	if !ok {
		t.Fatalf("no project header; texts %q", tt.Texts())
	}
	tt.Move(hdr.X+hdr.W/2, hdr.Y+hdr.H/2)
	frames(tt)
	if _, ok := tt.Find(locales.NewSessionIn("proj")); !ok {
		t.Fatalf("no add button while the header is hovered; texts %q", tt.Texts())
	}

	tt.Move(600, 680)
	tt.Advance(40 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find(locales.NewSessionIn("proj")); !ok {
		t.Errorf("the add button left before the hover delay; seen %v, until %v, now %v; texts %q", a.hoverSeen, a.hoverUntil, time.Now(), tt.Texts())
	}
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find(locales.NewSessionIn("proj")); ok {
		t.Error("the add button stayed after the hover delay")
	}
}
