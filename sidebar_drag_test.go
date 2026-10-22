package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
)

func dragSidebarItem(t *testing.T, tt *ui.Tester, source string, x, y float32) {
	t.Helper()
	from, ok := tt.Find(source)
	if !ok {
		t.Fatalf("missing drag source %s", source)
	}
	sx, sy := from.X+from.W/2, from.Y+from.H/2
	tt.Press(sx, sy)
	for i := 1; i <= 12; i++ {
		f := float32(i) / 12
		tt.Move(sx+(x-sx)*f, sy+(y-sy)*f)
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
}

func TestSidebarSessionDragKeepsSelectionAndLayout(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, reduced := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark-%t-reduced-%t", dark, reduced), func(t *testing.T) {
				a := sidebarTestApp(t)
				// A non-blue accent must not affect the placement cue.
				a.set.Accent = "plum"
				moves := sidebarRPC(t, a)
				tt := ui.NewTester(a.view, 1100, 760)
				tt.SetDark(dark)
				tt.SetPreferences(ui.Preferences{ReduceTransparency: reduced, ReduceMotion: reduced, TextScale: 1})
				tt.Frame()
				before, ok := tt.Find("s2")
				if !ok {
					t.Fatal("no drop target")
				}
				x, y := before.X+before.W/2, before.Y+before.H
				dragSidebarItem(t, tt, "s4", x, y)
				if a.selected != "s1" {
					t.Fatal("drag switched active terminal")
				}
				during, _ := tt.Find("s2")
				if during != before {
					t.Fatalf("placement cue shifted row: before %+v during %+v", before, during)
				}
				shot(t, tt, fmt.Sprintf("sidebar-drag-dark-%t-reduced-%t", dark, reduced))
				tt.Release(x, y)
				tt.Frame()
				wantSidebarMove(t, moves, proto.MoveParams{ID: "s4", Index: 2})
				if a.selected != "s1" {
					t.Fatal("drop switched active terminal")
				}
			})
		}
	}
}

func TestSidebarSessionDragEscapeAndInvalidTargets(t *testing.T) {
	for _, target := range []string{"s2", "other"} {
		t.Run(target, func(t *testing.T) {
			a := sidebarTestApp(t)
			a.setSessionPin("s2", true)
			moves := sidebarRPC(t, a)
			tt := ui.NewTester(a.view, 1100, 760)
			tt.Frame()
			to, ok := tt.Find(target)
			if !ok {
				t.Fatal("missing target")
			}
			x, y := to.X+to.W/2, to.Y+to.H/2
			dragSidebarItem(t, tt, "s4", x, y)
			tt.Release(x, y)
			tt.Frame()
			if a.selected != "s1" {
				t.Fatal("invalid drop changed selection")
			}
			select {
			case p := <-moves:
				t.Fatalf("invalid drop sent %+v", p)
			default:
			}
		})
	}
	a := sidebarTestApp(t)
	moves := sidebarRPC(t, a)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	to, _ := tt.Find("s2")
	x, y := to.X+to.W/2, to.Y+to.H/2
	dragSidebarItem(t, tt, "s4", x, y)
	tt.Key(0, ui.KeyEscape)
	tt.Release(x, y)
	tt.Frame()
	if a.selected != "s1" {
		t.Fatal("cancel changed selection")
	}
	select {
	case p := <-moves:
		t.Fatalf("canceled drag sent %+v", p)
	default:
	}
	// Ordinary clicks must still select after removing selection on press.
	if err := tt.Click("s3"); err != nil {
		t.Fatal(err)
	}
	if a.selected != "s3" {
		t.Fatal("click no longer selects")
	}
}

func TestSidebarProjectDragMovesWholeGroup(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprintf("after-%t", after), func(t *testing.T) {
			a := projectSidebarApp(t)
			moves := projectSidebarRPC(t, a)
			tt := ui.NewTester(a.view, 1100, 760)
			tt.Frame()
			label := "demo"
			if after {
				label = "s4"
			}
			to, ok := tt.Find(label)
			if !ok {
				t.Fatal("no project drop destination")
			}
			x, y := to.X+to.W/2, to.Y+to.H/2
			dragSidebarItem(t, tt, "third", x, y)
			if a.prefs.Collapsed["p3"] {
				t.Fatal("drag collapsed project")
			}
			if a.selected != "s1" {
				t.Fatal("project drag changed selected session")
			}
			shot(t, tt, fmt.Sprintf("sidebar-project-drag-after-%t", after))
			tt.Release(x, y)
			tt.Frame()
			index := 0
			if after {
				index = 1
			}
			wantProjectSidebarMove(t, moves, proto.MoveProjectParams{ID: "p3", Index: index})
			if a.prefs.Collapsed["p3"] {
				t.Fatal("drop collapsed project")
			}
		})
	}
}

func TestSessionMoveRejectsUnchangedVisibleOrder(t *testing.T) {
	a := sidebarTestApp(t)
	a.setSessionPin("s2", true)
	if _, ok := a.sessionMove("s1", "s3", false); ok {
		t.Fatal("before current next changes no visible order")
	}
	if _, ok := a.sessionMove("s3", "s1", true); ok {
		t.Fatal("after current previous changes no visible order")
	}
}
