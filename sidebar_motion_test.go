package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// Reduce Motion lands even a long project's collapse in one frame. The
// selected row stays in place, and opening again preserves session order.
func TestLargeProjectCollapseReduceMotion(t *testing.T) {
	a := testApp(t)
	for i := 1; i < 100; i++ {
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: fmt.Sprintf("s%d", i+1), Project: "p1", Name: fmt.Sprintf("session-%03d", i),
			Panes: []proto.Pane{{ID: fmt.Sprintf("p%d", i), Status: proto.Idle, Progress: -1}},
		})
	}
	tt := ui.NewTester(a.view, 900, 700)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	frames(tt)
	a.toggleProject("p1")
	tt.Frame()
	if tt.HasText("session-001") || !tt.HasText("demo") {
		t.Fatal("collapse did not snap to the selected row")
	}
	if tt.FramePending() {
		t.Fatal("Reduce Motion collapse scheduled another frame")
	}
	a.toggleProject("p1")
	tt.Frame()
	if !tt.HasText("session-001") || tt.FramePending() {
		t.Fatal("Reduce Motion expansion did not snap to its final rows")
	}
	first, ok1 := tt.Find("session-001")
	second, ok2 := tt.Find("session-002")
	if !ok1 || !ok2 || first.Y >= second.Y {
		t.Fatal("expansion did not preserve session order")
	}
}

// The tester cost includes layout and drawing through the software
// renderer. Each operation advances the collapse and its scheduled
// frames, so ns/op is per toggle, not a native GPU frame measurement.
func BenchmarkSidebar100SessionCollapse(b *testing.B) {
	home := b.TempDir()
	b.Setenv("KERO_HOME", home)
	b.Setenv("KERO_CONFIG", filepath.Join(home, "config"))
	a := newApp()
	a.loaded, a.selected, a.intro.first = true, "s0", false
	a.st.Projects = []proto.Project{{ID: "p1", Name: "demo", Dir: "/tmp"}}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("s%d", i)
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: "p1", Name: id,
			Panes: []proto.Pane{{ID: "p" + id, Status: proto.Idle, Progress: -1}},
		})
	}
	tt := ui.NewTester(a.view, 900, 700)
	frames(tt)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.prefs.Collapsed["p1"] = !a.prefs.Collapsed["p1"]
		tt.Advance(time.Second / 60)
		tt.Frame()
	}
}
