package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

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

// The Tasks pane's empty state says what to do: open Settings for the key.
func TestTasksEmptyStateAction(t *testing.T) {
	a := testTasks(t)
	a.st.Linear = nil
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if !tt.HasText(locales.LinearNotRead()) {
		t.Fatalf("no not-read state; texts %q", tt.Texts())
	}
	shot(t, tt, "tasks-empty")
	if err := tt.Click(locales.CommonOpenSettings()); err != nil {
		t.Fatalf("no action in the empty state: %v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	if !a.ss.open || a.ss.page != "linear" {
		t.Errorf("Settings opened at %q, open %v; want linear", a.ss.page, a.ss.open)
	}
}

// A workspace that is read but filtered to nothing offers to clear what
// is filtered, rather than to read it again.
func TestLinearEmptyStateClearsFilters(t *testing.T) {
	a := testLinear(t)
	a.st.Linear = &proto.LinearState{Status: proto.LinearOK, Projects: a.st.Linear.Projects}
	a.lin.project = "Rig"
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if err := tt.Click(locales.LinearClearFilters()); err != nil {
		t.Fatalf("no clear-filters action: %v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	if a.lin.project != "" {
		t.Errorf("the filters stayed: project %q", a.lin.project)
	}
}
