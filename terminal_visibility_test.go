package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
)

// Visited terminals keep receiving output, but only the pane composition
// owns whether they may ask the window for frames.
func TestTerminalVisibilityFollowsPanes(t *testing.T) {
	if testing.Short() {
		t.Skip("loads libghostty-vt")
	}
	if err := terminal.Load(); err != nil {
		t.Fatal(err)
	}
	a := testApp(t)
	a.st.Sessions = append(a.st.Sessions, proto.Session{ID: "s2", Project: "p1", Name: "other", Panes: []proto.Pane{{ID: "c", Shell: "sh"}, {ID: "d", Shell: "sh"}, {ID: "e", Shell: "sh"}}})
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		term, err := terminal.New(terminal.Options{Conn: idleConn{make(chan struct{})}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { term.Close() })
		term.SetVisible(false)
		term.Feed([]byte("\x1b[?25l"))
		a.terms[id] = &termEntry{term: term}
	}
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1100, 640)
	tt.SetPreferences(ui.Preferences{ReduceTransparency: true})
	frames(tt)
	a.selected = "s2"
	frames(tt)
	a.selected = "s1"
	a.st.Sessions[0].Zoomed = "a"
	frames(tt)
	quiet := func(ids []string) {
		t.Helper()
		if tt.FramePending() {
			t.Fatal("window already has pending frame before hidden output")
		}
		before := builds
		for range 20 {
			for _, id := range ids {
				a.terms[id].term.Feed([]byte("hidden latest\r\n"))
			}
			if tt.FramePending() {
				tt.Frame()
			}
		}
		t.Logf("%d hidden attached terminals, 20 output cycles: %d window builds", len(ids), builds-before)
		if builds != before {
			t.Fatalf("hidden output built %d frames", builds-before)
		}
	}
	quiet([]string{"b", "c", "d", "e"})
	a.openSettings("general")
	frames(tt)
	for _, id := range []string{"b", "c", "d", "e"} {
		if a.terms[id].shown {
			t.Fatalf("settings made omitted terminal %s visible", id)
		}
		a.terms[id].term.Feed([]byte("hidden under settings\r\n"))
	}
	if !a.terms["a"].shown {
		t.Fatal("settings hid composed terminal")
	}
	a.terms["a"].term.Feed([]byte("visible under settings\r\n"))
	if !tt.FramePending() {
		t.Fatal("settings suppressed composed terminal output")
	}
	tt.Frame()
	a.ss.open = false
	a.st.Sessions[0].Zoomed = ""
	frames(tt)
	if !a.terms["b"].shown || !strings.Contains(a.terms["b"].term.Text(), "hidden latest") {
		t.Fatal("unzoom did not restore current sibling")
	}
	a.terms["b"].term.Feed([]byte("visible sibling\r\n"))
	if !tt.FramePending() {
		t.Fatal("revealed sibling did not invalidate")
	}
	frames(tt)
	a.selected = "s2"
	frames(tt)
	quiet([]string{"a", "b"})
	// Attachment may finish after its session left the composition.
	late, err := terminal.New(terminal.Options{Conn: idleConn{make(chan struct{})}})
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	late.SetVisible(false)
	a.terms["a"].term.Close()
	a.terms["a"] = &termEntry{term: late}
	tt.Frame()
	quiet([]string{"a", "b"})
	a.selected = "s1"
	frames(tt)
	if !a.terms["a"].shown || !strings.Contains(late.Text(), "hidden latest") {
		t.Fatal("late attachment lost content on reveal")
	}
	// Keeping a terminal entry while replacing its pane with a built-in view
	// must also hide it; the pane ID alone cannot imply visibility.
	a.st.Sessions[0].Panes[0].View = proto.PaneFiles
	frames(tt)
	quiet([]string{"a"})

}
