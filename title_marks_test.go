package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
)

// A session's title mark follows its status and its task's type: what
// needs the user first, then a bug, then work under way, then work ended
// and seen; the rest have none.
func TestTitleMark(t *testing.T) {
	lin := &proto.LinearState{Issues: []proto.LinearIssue{
		{Identifier: "BRU-1", Labels: []string{"Feature", "Bug"}},
		{Identifier: "BRU-2", Labels: []string{"bug"}},
		{Identifier: "BRU-3", Labels: []string{"Feature", "debugging"}},
	}}
	done := &proto.Attention{State: proto.AttentionDone}
	idle := &proto.Attention{State: proto.AttentionIdle}
	for _, c := range []struct {
		name string
		s    proto.Session
		want string
	}{
		{"waiting", proto.Session{Pane: proto.Pane{Status: proto.Waiting, Unread: true}}, markNeedsYou},
		{"waiting on a bug", proto.Session{Pane: proto.Pane{Status: proto.Waiting, TaskID: "BRU-1"}}, markNeedsYou},
		{"working", proto.Session{Pane: proto.Pane{Status: proto.Working}}, markWorking},
		{"working on a bug", proto.Session{Pane: proto.Pane{Status: proto.Working, TaskID: "BRU-1"}}, markBug},
		{"a bug at rest, lowercase", proto.Session{Pane: proto.Pane{Status: proto.Idle, TaskID: "bru-2"}}, markBug},
		{"a task that is no bug", proto.Session{Pane: proto.Pane{Status: proto.Working, TaskID: "BRU-3"}}, markWorking},
		{"a task the workspace has not", proto.Session{Pane: proto.Pane{Status: proto.Working, TaskID: "BRU-9"}}, markWorking},
		{"done and seen, folded", proto.Session{Pane: proto.Pane{Status: proto.Idle}, Attention: done}, markDone},
		{"done and seen, on screen", proto.Session{Pane: proto.Pane{Status: proto.Idle, Created: 1, Since: 2}, Attention: idle}, markDone},
		{"done and seen, reported", proto.Session{Pane: proto.Pane{Status: proto.Idle, Message: "Tests pass"}}, markDone},
		{"done, unseen", proto.Session{Pane: proto.Pane{Status: proto.Idle, Unread: true, Created: 1, Since: 2}}, ""},
		{"a shell never at work", proto.Session{Pane: proto.Pane{Status: proto.Idle, Created: 1, Since: 1}, Attention: idle}, ""},
		{"failed", proto.Session{Pane: proto.Pane{Status: proto.Failed, Created: 1, Since: 2}}, ""},
		{"exited", proto.Session{Pane: proto.Pane{Status: proto.Exited, Unread: true}}, ""},
	} {
		if got := titleMark(&c.s, lin); got != c.want {
			t.Errorf("%s: mark %q, want %q", c.name, got, c.want)
		}
	}
	// Without a workspace, a task is no bug.
	s := proto.Session{Pane: proto.Pane{Status: proto.Working, TaskID: "BRU-1"}}
	if got := titleMark(&s, nil); got != markWorking {
		t.Errorf("no workspace: mark %q, want %q", got, markWorking)
	}
}

// Every mark of the set is a symbol the system has: a name it does not
// know would draw nothing, and say nothing of it.
func TestTitleMarkSymbols(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("SF Symbols are macOS's")
	}
	for _, m := range []string{markNeedsYou, markBug, markWorking, markDone} {
		if symbolImage(m, titleMarkSize, ui.Hex("#6e6e78"), false) == nil {
			t.Errorf("no SF Symbol %q", m)
		}
		if titleMarkLabel(m) == "" {
			t.Errorf("mark %q says nothing", m)
		}
	}
}

// sidebarApp is the app with three projects, one of them renamed with an
// emoji, and a session in each state a title mark tells, beside a long
// title and a short one.
func sidebarApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.conn = connReady
	now := time.Now()
	ms := func(ago time.Duration) int64 { return now.Add(-ago).UnixMilli() }
	created := ms(time.Hour)
	a.st.Projects = []proto.Project{
		{ID: "p1", Name: "🐛 kero-deyalla", Dir: "/tmp/kero-deyalla"},
		{ID: "p2", Name: "website", Dir: "/tmp/website"},
		{ID: "p3", Name: "infra", Dir: "/tmp/infra"},
	}
	a.st.Sessions = nil
	a.st.Linear = &proto.LinearState{Status: proto.LinearOK, Issues: []proto.LinearIssue{
		{Identifier: "BRU-176", Labels: []string{"Bug"}},
	}}
	pane := func(st proto.Status, unread bool, since int64) proto.Pane {
		return proto.Pane{ID: "a", Shell: "zsh", Dir: "/tmp", Status: st, Unread: unread, Progress: -1, Created: created, Since: since}
	}
	add := func(project, id, name string, p proto.Pane) {
		s := proto.Session{ID: id, Project: project, Name: name, Pane: p, Panes: []proto.Pane{p}, Focus: "a"}
		a.st.Sessions = append(a.st.Sessions, s)
	}
	add("p1", "s1", "asks a question", pane(proto.Waiting, true, ms(time.Minute)))
	bug := pane(proto.Working, false, ms(2*time.Minute))
	bug.TaskID = "BRU-176"
	add("p1", "s2", "sidebar spacing", bug)
	add("p1", "s3", "builds", pane(proto.Working, false, ms(4*time.Minute)))
	add("p2", "s4", "landing page", pane(proto.Idle, false, ms(6*time.Minute)))
	add("p2", "s5", "turn ended", pane(proto.Idle, true, ms(7*time.Minute)))
	add("p2", "s6", "shell", pane(proto.Idle, false, created))
	add("p3", "s7", "a very long title that runs on past the edge of the sidebar and is cut short", pane(proto.Working, false, ms(3*time.Minute)))
	add("p3", "s8", "short", pane(proto.Working, false, ms(5*time.Minute)))
	a.selected = "s6"
	return a
}

// markInk counts the pixels of a title's mark: those of the slot left of
// the title that are not the row's own color.
func markInk(tt *ui.Tester, title ui.Rect) int {
	img := tt.Image()
	x0, y0 := int(title.X)-6-titleMarkSize, int(title.Y+title.H/2)-titleMarkSize/2
	bg := img.RGBAAt(x0-3, y0)
	n := 0
	for y := y0; y < y0+titleMarkSize; y++ {
		for x := x0; x < x0+titleMarkSize; x++ {
			c := img.RGBAAt(x, y)
			d := abs(float32(c.R)-float32(bg.R)) + abs(float32(c.G)-float32(bg.G)) + abs(float32(c.B)-float32(bg.B))
			if d > 40 {
				n++
			}
		}
	}
	return n
}

// The sidebar draws each session's mark at the head of its title, and
// none where the session has none; a long title is cut short, and the
// mark, the time and the status glyph keep their places.
func TestSidebarTitleMarks(t *testing.T) {
	a := sidebarApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	frames(tt)
	shot(t, tt, "sidebar-title-marks")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "sidebar-title-marks-dark")
	tt.SetDark(false)
	tt.Frame()

	find := func(s string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no %q; texts %q", s, tt.Texts())
		}
		return r
	}
	if runtime.GOOS == "darwin" {
		for _, name := range []string{"asks a question", "sidebar spacing", "builds", "landing page"} {
			if markInk(tt, find(name)) == 0 {
				t.Errorf("row %q shows no mark", name)
			}
		}
	}
	for _, name := range []string{"turn ended", "shell"} {
		if n := markInk(tt, find(name)); n != 0 {
			t.Errorf("row %q shows a mark of %d pixels, want none", name, n)
		}
	}
	if _, ok := tt.Find(locales.StatusBugWork()); !ok {
		t.Errorf("no mark says %q; texts %q", locales.StatusBugWork(), tt.Texts())
	}

	// Every title starts at one edge, marked or not.
	if x, y := find("shell").X, find("builds").X; x != y {
		t.Errorf("titles start at %v and %v", x, y)
	}
	// The long title gives way: it starts where the short one does, ends
	// before its time, and the times and status glyphs line up.
	long := find("a very long title that runs on past the edge of the sidebar and is cut short")
	short := find("short")
	if long.X != short.X {
		t.Errorf("the long title starts at %v, the short one at %v", long.X, short.X)
	}
	longAgo, shortAgo := find(locales.AgoMinutes(3)), find(locales.AgoMinutes(5))
	if long.X+long.W > longAgo.X {
		t.Errorf("the long title runs to %v, over its time at %v", long.X+long.W, longAgo.X)
	}
	if l, s := longAgo.X+longAgo.W, shortAgo.X+shortAgo.W; l != s {
		t.Errorf("the times end at %v and %v", l, s)
	}
	if runtime.GOOS == "darwin" && markInk(tt, long) == 0 {
		t.Error("the long title lost its mark")
	}

	// A header is close to its rows and far from the project above: the
	// gap above a header's name is wider than the one under it.
	web, landing, builds := find("website"), find("landing page"), find("builds")
	above := web.Y - (builds.Y + builds.H)
	below := landing.Y - (web.Y + web.H)
	if above <= below+8 {
		t.Errorf("a header is %v below the project above and %v above its rows: it does not read as their heading", above, below)
	}
}

// A project renamed with an emoji shows it whole, in its color: the
// sidebar draws the emoji, not a box where a glyph is missing.
func TestProjectTitleEmoji(t *testing.T) {
	a := sidebarApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	frames(tt)
	r, ok := tt.Find("🐛 kero-deyalla")
	if !ok {
		t.Fatalf("no emoji title; texts %q", tt.Texts())
	}
	shot(t, tt, "sidebar-emoji-project")
	img, colored := tt.Image(), 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X)+18; x++ {
			c := img.RGBAAt(x, y)
			hi := max(c.R, c.G, c.B)
			lo := min(c.R, c.G, c.B)
			if int(hi)-int(lo) > 60 {
				colored++
			}
		}
	}
	if colored == 0 {
		t.Error("the emoji drew without color: a missing glyph, not the bug")
	}
	// Cut short, the title ends in a whole grapheme and the ellipsis.
	b := testApp(t)
	b.st.Projects[0].Name = "🐛 a project title long enough to be cut short in the sidebar 🐛"
	tn := ui.NewTester(b.view, 1100, 400)
	frames(tn)
	shot(t, tn, "sidebar-emoji-project-cut")
	if _, ok := tn.Find(b.st.Projects[0].Name); !ok {
		t.Fatalf("no long emoji title; texts %q", tn.Texts())
	}
}

// A header keeps its parts where they are as the pointer comes over it:
// the add button shows in a place kept for it, and "needs you" before it
// does not move.
func TestProjectHeaderSteadyOnHover(t *testing.T) {
	a := sidebarApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	frames(tt)
	needs := locales.SidebarNeedsYou(1)
	before, ok := tt.Find(needs)
	if !ok {
		t.Fatalf("no %q; texts %q", needs, tt.Texts())
	}
	hdr, _ := tt.Find("🐛 kero-deyalla")
	tt.Move(hdr.X+hdr.W/2, hdr.Y+hdr.H/2)
	frames(tt)
	if _, ok := tt.Find(locales.NewSessionIn("🐛 kero-deyalla")); !ok {
		t.Fatalf("no add button on hover; texts %q", tt.Texts())
	}
	shot(t, tt, "sidebar-header-hover")
	after, _ := tt.Find(needs)
	if after.X != before.X || after.Y != before.Y {
		t.Errorf("%q moved on hover, from %v to %v", needs, before, after)
	}
}
