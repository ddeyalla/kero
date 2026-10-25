package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// sidebarRevealApp has a long, populated project and one session in a second
// project, so a row can be offscreen and a group can be folded.
func sidebarRevealApp(t *testing.T) *App {
	t.Helper()
	a := navTestApp(t)
	a.show = "session"
	a.selected = "s1"
	a.prefs.SidebarHidden = false
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("reveal-%03d", i)
		panes := []proto.Pane{{ID: "pane-" + id, Shell: "sh", Dir: "/tmp", Status: proto.Idle}}
		layout := &proto.Layout{Pane: "pane-" + id}
		if i == 119 {
			// The selected target has two panes, so its pane header does not
			// repeat the session title; Find then measures the sidebar row.
			panes = append(panes, proto.Pane{ID: "pane-" + id + "-b", Shell: "sh", Dir: "/tmp", Status: proto.Idle})
			layout = &proto.Layout{Row: true, Ratio: 0.5, A: &proto.Layout{Pane: "pane-" + id}, B: &proto.Layout{Pane: "pane-" + id + "-b"}}
		}
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: "p1", Name: id, Focus: "pane-" + id,
			Panes: panes, Layout: layout,
		})
	}
	a.selected = "s1"
	return a
}

func assertSidebarRowVisible(t *testing.T, a *App, r ui.Rect, width float32) {
	t.Helper()
	left, right := float32(0), width
	if a.navPlacement() == navRight {
		left = width - a.chrome.sideW
	}
	if r.X < left || r.X+r.W > right || r.Y < 0 || r.Y+r.H > 700 {
		t.Fatalf("row %+v is outside the %s sidebar %v..%v at 700 high", r, a.navPlacement(), left, right)
	}
}

func TestSidebarExplicitRevealScrollsRowIntoView(t *testing.T) {
	for _, place := range []navPlacement{navLeft, navRight} {
		t.Run(string(place), func(t *testing.T) {
			a := sidebarRevealApp(t)
			a.setNavPlacement(place)
			tt := ui.NewTester(a.view, 900, 700)
			tt.Frame()
			if a.sidebarScroll.MaxY <= 0 || a.sidebarScroll.Y != 0 {
				t.Fatalf("the sidebar does not start at the top: %+v", a.sidebarScroll)
			}
			last := "reveal-119"
			if _, ok := tt.Find(last); !ok {
				t.Fatalf("the last row is not built: %v", tt.Texts())
			}
			a.selectRevealing(last)
			tt.Frame()
			if a.nav.reveal != "" {
				t.Fatalf("the reveal stayed pending: %q", a.nav.reveal)
			}
			if a.sidebarScroll.Y <= 0 {
				t.Fatalf("the sidebar did not scroll to the revealed row: %+v", a.sidebarScroll)
			}
			r, ok := tt.Find(last)
			if !ok {
				t.Fatalf("the revealed row is not in the frame")
			}
			assertSidebarRowVisible(t, a, r, 900)
		})
	}
}

func TestSidebarRevealUnfoldsAndDoesNotYankOrdinarySelection(t *testing.T) {
	a := sidebarRevealApp(t)
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	last := "reveal-119"

	// An ordinary selection changes the session but neither unfolds nor
	// scrolls the sidebar.
	a.prefs.Collapsed["p1"] = true
	a.selectSession(last)
	tt.Frame()
	if !a.prefs.Collapsed["p1"] {
		t.Fatal("ordinary selection unfolded the project")
	}
	if a.sidebarScroll.Y != 0 {
		t.Fatalf("ordinary selection yanked the sidebar: %+v", a.sidebarScroll)
	}

	// An explicit reveal of the already-selected, folded row opens the group
	// and brings the row into view.
	a.selectRevealing(last)
	tt.Frame()
	if a.prefs.Collapsed["p1"] {
		t.Fatal("explicit reveal left the project folded")
	}
	if a.sidebarScroll.Y <= 0 {
		t.Fatalf("explicit reveal did not scroll: %+v", a.sidebarScroll)
	}
	r, ok := tt.Find(last)
	if !ok {
		t.Fatal("the explicitly revealed row is missing")
	}
	assertSidebarRowVisible(t, a, r, 900)
}

func TestSidebarRevealAlreadySelectedOffscreen(t *testing.T) {
	a := sidebarRevealApp(t)
	last := "reveal-119"
	// Select without a reveal first, as a plain click or an internal state
	// update can. It leaves the row offscreen.
	a.selectSession(last)
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	if a.sidebarScroll.Y != 0 {
		t.Fatalf("plain selection scrolled: %+v", a.sidebarScroll)
	}
	// The same selected row now receives an explicit reveal request.
	a.selectRevealing(last)
	tt.Frame()
	if a.nav.reveal != "" || a.sidebarScroll.Y <= 0 {
		t.Fatalf("already-selected row was not revealed: reveal=%q scroll=%+v", a.nav.reveal, a.sidebarScroll)
	}
	r, ok := tt.Find(last)
	if !ok {
		t.Fatal("the already-selected revealed row is missing")
	}
	assertSidebarRowVisible(t, a, r, 900)
}
