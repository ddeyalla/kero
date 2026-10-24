package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/actions"
	"kero/internal/plugin/protocol"
	"kero/internal/plugin/registry"
	"kero/internal/proto"
	"kero/locales"
)

// navTestApp is an app with two projects, three sessions and two panes in
// the first session, whose tab and pane titles differ.
func navTestApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.st.Sessions[0].Name = "api"
	a.st.Projects = append(a.st.Projects, proto.Project{ID: "p2", Name: "ship", Dir: "/tmp/ship"})
	a.st.Sessions = append(a.st.Sessions,
		proto.Session{ID: "s2", Project: "p1", Name: "web", Focus: "c",
			Panes:  []proto.Pane{{ID: "c", Shell: "bash", Dir: "/tmp", Status: proto.Idle, Progress: -1}},
			Layout: &proto.Layout{Pane: "c"}},
		proto.Session{ID: "s3", Project: "p2", Name: "docs", Focus: "d",
			Panes:  []proto.Pane{{ID: "d", Shell: "sh", Dir: "/tmp", Status: proto.Idle, Progress: -1}},
			Layout: &proto.Layout{Pane: "d"}},
	)
	a.selected = "s1"
	return a
}

func tabWidth(t *testing.T, tt *ui.Tester, label string) float32 {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no tab %q; texts %q", label, tt.Texts())
	}
	return r.W
}

func TestNavPlacementPreference(t *testing.T) {
	a := testApp(t)
	if got := a.navPlacement(); got != navLeft {
		t.Fatalf("a new app places navigation at %q", got)
	}
	a.prefs.NavPlacement = "sideways"
	if got := a.navPlacement(); got != navLeft {
		t.Fatalf("an unknown placement reads as %q", got)
	}
	a.prefs.NavPlacement = "top"
	a.savePrefs()
	a.flushPrefs()
	b := newApp()
	if got := b.navPlacement(); got != navTop {
		t.Fatalf("the saved placement reads as %q", got)
	}
	// A file with an unknown value, and one with none, read as the left
	// sidebar.
	if err := writePrefs(prefsPath(), prefs{NavPlacement: "diagonal"}); err != nil {
		t.Fatal(err)
	}
	if got := newApp().navPlacement(); got != navLeft {
		t.Fatalf("an unknown saved placement reads as %q", got)
	}
	if err := writePrefs(prefsPath(), prefs{}); err != nil {
		t.Fatal(err)
	}
	if got := newApp().navPlacement(); got != navLeft {
		t.Fatalf("a missing saved placement reads as %q", got)
	}
}

func TestNavPlacementKeepsPanesAndFocus(t *testing.T) {
	a := navTestApp(t)
	s := a.session("s1")
	a.setFocus(s, "b")
	a.focus["s1"] = "b"
	s.Focus = "b"
	s.Zoomed = "b"
	a.selected = "s1"
	term := &termEntry{}
	a.terms["b"] = term
	wantLayout := s.Layout.Clone()
	for _, p := range navPlacements {
		a.run("nav.placement."+string(p), nil)
		if got := a.navPlacement(); got != p {
			t.Fatalf("placement after the action = %q, want %q", got, p)
		}
		if a.selected != "s1" || a.focusOf(a.session("s1")) != "b" {
			t.Fatalf("%s: selected %q, focus %q", p, a.selected, a.focusOf(a.session("s1")))
		}
		s = a.session("s1")
		if got := s.Layout.Panes(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("%s: panes %v", p, got)
		}
		if !reflect.DeepEqual(s.Layout, wantLayout) {
			t.Fatalf("%s: the layout changed", p)
		}
		if s.Zoomed != "b" || a.terms["b"] != term {
			t.Fatalf("%s: zoom %q, terminal %p", p, s.Zoomed, a.terms["b"])
		}
		tt := ui.NewTester(a.view, 1280, 760)
		tt.Frame()
		// The zoomed pane keeps the window, as it did before the move.
		if !tt.HasText("zsh") || tt.HasText("fish") {
			t.Fatalf("%s: the zoomed pane is not the only one drawn: %q", p, tt.Texts())
		}
	}
}

func TestNavFourLayouts(t *testing.T) {
	a := navTestApp(t)
	for _, place := range []navPlacement{navLeft, navRight, navTop, navBottom} {
		a.setNavPlacement(place)
		tt := ui.NewTester(a.view, 1280, 760)
		tt.Frame()
		project, ok := tt.Find("demo")
		if !ok {
			t.Fatalf("%s: no project header; %q", place, tt.Texts())
		}
		fish, ok := tt.Find("fish")
		if !ok {
			t.Fatalf("%s: missing project or pane: %q", place, tt.Texts())
		}
		if place != navLeft && fish.X < systemBar.Left {
			// The lights sit over the panes in these layouts: the header's
			// title stays clear of them.
			t.Errorf("%s: the pane title is under the traffic lights: x %v", place, fish.X)
		}
		want := []string{locales.ActionNewSession(), locales.ActionGoToSession(), locales.ActionOpenProject()}
		if place == navTop || place == navBottom {
			// Project access lives in the overflow in the tabs.
			want = []string{locales.ActionNewSession(), locales.ActionGoToSession(), locales.NavOverflow()}
		}
		for _, label := range want {
			if !tt.HasText(label) {
				t.Fatalf("%s: %q is not reachable: %q", place, label, tt.Texts())
			}
		}
		if place == navTop || place == navBottom {
			if err := tt.Click(locales.NavOverflow()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			open := false
			for _, label := range tt.Menu() {
				open = open || label == locales.ActionOpenProject()
			}
			if !open {
				t.Fatalf("%s: the overflow has no project access: %q", place, tt.Menu())
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
		}
		shot(t, tt, "nav-"+string(place)+"-light")
		switch place {
		case navLeft:
			if project.X >= defaultSidebar {
				t.Errorf("left: the project is at %v, not in the sidebar", project.X)
			}
		case navRight:
			if project.X <= 1280-defaultSidebar {
				t.Errorf("right: the project is at %v, not in the right sidebar", project.X)
			}
		case navTop:
			if project.Y > 64 {
				t.Errorf("top: the project is at y %v, not in the top bar", project.Y)
			}
			if project.X < systemBar.Left {
				t.Errorf("top: the tabs are under the traffic lights: x %v", project.X)
			}
		case navBottom:
			if project.Y < 760-64 {
				t.Errorf("bottom: the project is at y %v, not in the bottom bar", project.Y)
			}
		}
	}
	// Dark, with the reduced motion preference: the same compositions.
	tt := ui.NewTester(a.view, 1280, 760)
	tt.SetDark(true)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	shot(t, tt, "nav-bottom-dark-reduced")
	a.setNavPlacement(navLeft)
	tt.Frame()
	shot(t, tt, "nav-left-dark")
}

func TestCollapsingTheActiveProjectKeepsItsPanes(t *testing.T) {
	a := navTestApp(t)
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 1280, 760)
	tt.Frame()
	if !tt.HasText("api") {
		t.Fatalf("the active session has no tab: %q", tt.Texts())
	}
	if err := tt.Click("demo"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.prefs.Collapsed["p1"] {
		t.Fatal("the project did not collapse")
	}
	if a.selected != "s1" {
		t.Fatalf("collapsing selected %q", a.selected)
	}
	if tt.HasText("api") {
		t.Fatalf("the collapsed project still shows its tab: %q", tt.Texts())
	}
	if !tt.HasText("fish") || !tt.HasText("zsh") {
		t.Fatalf("the active session's panes left the window: %q", tt.Texts())
	}
	// It stays collapsed over the next frames: collapsing is not undone by
	// the selection it keeps.
	for range 4 {
		tt.Frame()
	}
	if !a.prefs.Collapsed["p1"] || a.selected != "s1" {
		t.Fatalf("the collapse undid itself: %v, selected %q", a.prefs.Collapsed["p1"], a.selected)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	shot(t, tt, "nav-collapsed-active")
	// The overflow represents the folded project by its header, not by its
	// hidden sessions.
	if err := tt.Click(locales.NavOverflow()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range tt.Menu() {
		if label == "demo" {
			t.Fatalf("the overflow lists the folded project's sessions: %q", tt.Menu())
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	// A separate click, not a double click on the header.
	tt.Advance(500 * time.Millisecond)
	if err := tt.Click("demo"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prefs.Collapsed["p1"] || !tt.HasText("api") {
		t.Fatalf("expanding did not restore the tab: %q", tt.Texts())
	}
}

func TestTabsMinimumWidthAndReveal(t *testing.T) {
	a := testApp(t)
	a.st.Sessions[0].Name = "s00"
	for i := 1; i < 14; i++ {
		id := "s" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: "p1", Name: id, Focus: "pane-" + id,
			Panes:  []proto.Pane{{ID: "pane-" + id, Shell: "sh", Dir: "/tmp", Status: proto.Idle}},
			Layout: &proto.Layout{Pane: "pane-" + id},
		})
	}
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 760, 700)
	tt.Frame()
	if _, ok := tt.Find(locales.NavOverflow()); !ok {
		t.Fatalf("no overflow control: %q", tt.Texts())
	}
	// The tabs that fit share the row at the floor; the rest are clipped into
	// it, which is what the scroll and overflow exist for.
	for i := 0; i < 3; i++ {
		label := "s" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		if w := tabWidth(t, tt, locales.NavSessionTab(label)); w < navTabMin-0.5 {
			t.Fatalf("%s is %v wide, under the floor", label, w)
		}
	}
	if a.nav.scroll.MaxX <= 0 {
		t.Fatalf("the tabs do not scroll: maxX %v", a.nav.scroll.MaxX)
	}
	shot(t, tt, "nav-tabs-narrow")
	// The overflow lists the sessions by project; choosing one reveals its
	// tab.
	if err := tt.Click(locales.NavOverflow()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("demo", "s13"); err != nil {
		t.Fatalf("the overflow has no s13 under demo: %v, %q", err, tt.Menu())
	}
	tt.Frame()
	if a.selected != "s13" {
		t.Fatalf("the overflow selected %q", a.selected)
	}
	if a.nav.scroll.X <= 0 {
		t.Fatalf("the revealed tab did not scroll into view: %v", a.nav.scroll.X)
	}
	shot(t, tt, "nav-tabs-revealed")
}

// selectRevealing expands the group a session is drawn under, which is the
// synthetic Other group for a missing or empty project ID, and only then the
// group's own collapse key.
func TestSelectRevealingExpandsDisplayedGroup(t *testing.T) {
	a := navTestApp(t)
	add := func(id, project, name string) {
		t.Helper()
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: project, Name: name, Focus: "pane-" + id,
			Panes:  []proto.Pane{{ID: "pane-" + id, Shell: "sh", Dir: "/tmp", Status: proto.Idle}},
			Layout: &proto.Layout{Pane: "pane-" + id},
		})
	}
	add("orphan", "missing", "orphan")
	add("empty-project", "", "empty")

	inGroup := func(groupID, id string) bool {
		for _, g := range a.groups() {
			if g.project.ID != groupID {
				continue
			}
			for _, s := range g.sessions {
				if s.ID == id {
					return true
				}
			}
		}
		return false
	}
	if !inGroup("", "orphan") || !inGroup("", "empty-project") {
		t.Fatalf("the sessions with an unknown or empty project are not in Other: %+v", a.groups())
	}
	if !inGroup("p1", "s2") || !inGroup("p2", "s3") {
		t.Fatalf("the known projects do not own their sessions: %+v", a.groups())
	}

	for _, c := range []struct {
		name, id, project, group string
	}{
		{"unknown project", "orphan", "missing", ""},
		{"empty project", "empty-project", "", ""},
		{"known project", "s2", "p1", "p1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a.prefs.Collapsed = map[string]bool{c.group: true}
			a.selected, a.nav.reveal = "s1", ""
			a.selectRevealing(c.id)
			if a.selected != c.id {
				t.Fatalf("selected %q, want %q", a.selected, c.id)
			}
			if a.nav.reveal != c.id {
				t.Fatalf("asked to reveal %q, want %q", a.nav.reveal, c.id)
			}
			if a.prefs.Collapsed[c.group] {
				t.Fatalf("the %q group stayed collapsed", c.group)
			}
			if got := a.session(c.id).Project; got != c.project {
				t.Fatalf("the session's project changed from %q to %q", c.project, got)
			}
		})
	}
}

// Folding a project is a navigation choice, not a selection change: internal
// selection and startup reconciliation leave it folded, while an explicit
// reveal by the user opens it.
// An attention jump is a user reveal action even when the session is already
// selected and its project is open: its tab may sit past the right edge.
func TestAttentionJumpRevealsAlreadySelectedTab(t *testing.T) {
	a := navTestApp(t)
	for i := 0; i < 22; i++ {
		id := fmt.Sprintf("many-%02d", i)
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: "p1", Name: id, Focus: "pane-" + id,
			Panes:  []proto.Pane{{ID: "pane-" + id, Shell: "sh", Dir: "/tmp", Status: proto.Idle}},
			Layout: &proto.Layout{Pane: "pane-" + id},
		})
	}
	last := "many-21"
	pane := "pane-" + last
	a.setNavPlacement(navTop)
	a.show, a.selected, a.nav.reveal = "session", last, ""
	delete(a.prefs.Collapsed, "p1")

	tt := ui.NewTester(a.view, 760, 700)
	tt.Frame()
	if a.nav.scroll.MaxX <= 0 {
		t.Fatalf("the tabs do not overflow: maxX %v", a.nav.scroll.MaxX)
	}
	if a.nav.scroll.X != 0 {
		t.Fatalf("the row starts scrolled to %v", a.nav.scroll.X)
	}

	if !a.focusAttention(last, pane) {
		t.Fatal("the attention jump found no session")
	}
	if a.nav.reveal != last {
		t.Fatalf("attention did not request tab reveal for the selected session: got %q", a.nav.reveal)
	}
	if a.selected != last || a.show != "session" {
		t.Fatalf("the attention jump changed the page: selected %q, show %q", a.selected, a.show)
	}
	if got := a.focusOf(a.session(last)); got != pane {
		t.Fatalf("the attention jump focused %q, want %q", got, pane)
	}

	tt.Frame()
	if a.nav.reveal != "" {
		t.Fatalf("the reveal request stayed pending: %q", a.nav.reveal)
	}
	if a.nav.scroll.X <= 0 {
		t.Fatalf("the selected tab was not scrolled into view: %v", a.nav.scroll.X)
	}
	tab, ok := tt.Find(locales.NavSessionTab(last))
	if !ok || tab.W < navTabMin-0.5 || tab.X < 0 || tab.X+tab.W > 760 {
		t.Fatalf("the selected tab is not fully in view: %+v, found %v", tab, ok)
	}
	if got := a.focusOf(a.session(last)); got != pane {
		t.Fatalf("the frame changed the pane focus to %q, want %q", got, pane)
	}
}

func TestExplicitCollapseWaitsForReveal(t *testing.T) {
	a := navTestApp(t)
	a.selected = "s1"
	a.toggleProject("p1")
	if !a.prefs.Collapsed["p1"] || a.selected != "s1" {
		t.Fatalf("the fold changed selection: collapsed %v selected %q", a.prefs.Collapsed["p1"], a.selected)
	}
	a.selectSession("s1")
	a.reconcileSelection(false)
	if !a.prefs.Collapsed["p1"] {
		t.Fatal("an internal selection undid the fold")
	}
	a.selectRevealing("s2")
	if a.prefs.Collapsed["p1"] || a.selected != "s2" || a.nav.reveal != "s2" {
		t.Fatalf("the reveal did not open the project: %+v, selected %q", a.prefs.Collapsed, a.selected)
	}

	// Other is a group of its own, collapsed by the same empty ID.
	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: "orphan", Project: "missing", Name: "orphan", Focus: "orphan-pane",
		Panes:  []proto.Pane{{ID: "orphan-pane", Shell: "sh", Dir: "/tmp", Status: proto.Idle}},
		Layout: &proto.Layout{Pane: "orphan-pane"},
	})
	a.selected = "orphan"
	a.toggleProject("")
	a.selectSession("orphan")
	a.reconcileSelection(false)
	if !a.prefs.Collapsed[""] {
		t.Fatal("an internal selection undid the Other fold")
	}
	a.selectRevealing("orphan")
	if a.prefs.Collapsed[""] {
		t.Fatal("an explicit reveal did not open Other")
	}

	// An attention jump is another external selection: it must open Other
	// too, even when the orphan is already the selected session.
	a.prefs.Collapsed[""] = true
	a.selected, a.show = "orphan", "session"
	a.focusAttention("orphan", "")
	if a.prefs.Collapsed[""] {
		t.Fatal("an attention jump did not open Other")
	}
}

func TestTabsKeepPinsAndOtherLast(t *testing.T) {
	a := navTestApp(t)
	a.prefs.SessionPins = map[string]bool{"s2": true}
	a.st.Sessions = append(a.st.Sessions, proto.Session{
		ID: "s4", Project: "missing", Name: "orphan", Focus: "e",
		Panes:  []proto.Pane{{ID: "e", Shell: "sh", Dir: "/tmp", Status: proto.Idle}},
		Layout: &proto.Layout{Pane: "e"},
	})
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 1280, 700)
	tt.Frame()
	web, ok1 := tt.Find(locales.NavSessionTab("web"))
	api, ok2 := tt.Find(locales.NavSessionTab("api"))
	ship, ok3 := tt.Find("ship")
	other, ok4 := tt.Find(locales.SidebarOtherProject())
	if !ok1 || !ok2 || !ok3 || !ok4 {
		t.Fatalf("missing tabs or groups: %q", tt.Texts())
	}
	if web.X > api.X {
		t.Errorf("the pinned session is not first: web %v, api %v", web.X, api.X)
	}
	if other.X <= ship.X {
		t.Errorf("Other is not last: Other %v, ship %v", other.X, ship.X)
	}
}

func TestPluginShortcutsMoveIntoTheOverflow(t *testing.T) {
	a := navTestApp(t)
	a.plugins.known = []registry.Plugin{{Manifest: protocol.Manifest{
		ID: "fake", Name: "Fake", Version: "1.0.0", Run: []string{"fake"},
		Widgets: []protocol.Widget{{Name: "main", Title: "Main", Icon: "board"}},
	}, Builtin: true}}
	a.plugins.knownAt = true
	a.prefs.Pinned = []pinRef{{Plugin: "fake", Widget: "main"}}
	a.setNavPlacement(navTop)
	narrow := ui.NewTester(a.view, 720, 700)
	narrow.Frame()
	if _, ok := narrow.Find("Fake"); ok {
		t.Fatalf("the plugin shortcut did not make room: %q", narrow.Texts())
	}
	if err := narrow.Click(locales.NavOverflow()); err != nil {
		t.Fatal(err)
	}
	narrow.Frame()
	found := false
	for _, label := range narrow.Menu() {
		found = found || label == locales.PagePlugins()
	}
	if !found {
		t.Fatalf("the overflow has no labeled plugin shortcuts: %q", narrow.Menu())
	}
	narrow.Key(0, ui.KeyEscape)
	wide := ui.NewTester(a.view, 1600, 700)
	wide.Frame()
	if _, ok := wide.Find("Fake"); !ok {
		t.Fatalf("the plugin shortcut does not show when there is room: %q", wide.Texts())
	}
	if err := wide.Click(locales.NavOverflow()); err != nil {
		t.Fatal(err)
	}
	wide.Frame()
	for _, label := range wide.Menu() {
		if label == locales.PagePlugins() {
			t.Fatalf("the overflow duplicates a visible plugin shortcut: %q", wide.Menu())
		}
	}
}

func TestTabsWithNoSessions(t *testing.T) {
	a := testApp(t)
	a.st.Sessions, a.st.Projects, a.selected = nil, nil, ""
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 900, 600)
	tt.Frame()
	if _, ok := tt.Find(locales.NavOverflow()); !ok {
		t.Fatalf("the empty navigation has no controls: %q", tt.Texts())
	}
	shot(t, tt, "nav-empty-light")
}

func TestTabsWithManySessions(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("many-%02d", i)
		a.st.Sessions = append(a.st.Sessions, proto.Session{
			ID: id, Project: "p1", Name: id, Focus: "pane-" + id,
			Panes:  []proto.Pane{{ID: "pane-" + id, Shell: "sh", Dir: "/tmp", Status: proto.Idle}},
			Layout: &proto.Layout{Pane: "pane-" + id},
		})
	}
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	if a.nav.scroll.MaxX <= 0 {
		t.Fatalf("25 sessions do not overflow: maxX %v", a.nav.scroll.MaxX)
	}
	if _, ok := tt.Find(locales.NavOverflow()); !ok {
		t.Fatalf("25 sessions have no overflow: %q", tt.Texts())
	}
}

func TestTabsPreferredMaximum(t *testing.T) {
	a := navTestApp(t)
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 1400, 760)
	tt.Frame()
	for _, label := range []string{"api", "web", "docs"} {
		if w := tabWidth(t, tt, locales.NavSessionTab(label)); w > navTabMax+0.5 {
			t.Fatalf("%s is %v wide, over the preferred maximum", label, w)
		}
	}
}

func TestDraggingAProjectHeaderDoesNotCollapseIt(t *testing.T) {
	a := navTestApp(t)
	a.conn = connReady
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 1280, 700)
	tt.Frame()
	hdr, ok := tt.Find("ship")
	if !ok {
		t.Fatalf("no ship header: %q", tt.Texts())
	}
	demo, ok := tt.Find("demo")
	if !ok {
		t.Fatalf("no demo header: %q", tt.Texts())
	}
	tt.Press(hdr.X+hdr.W/2, hdr.Y+hdr.H/2)
	for i := 1; i <= 6; i++ {
		f := float32(i) / 6
		tt.Move(hdr.X+(demo.X-hdr.X)*f, hdr.Y)
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	tt.Release(demo.X+demo.W/2, demo.Y+demo.H/2)
	tt.Frame()
	if a.prefs.Collapsed["p2"] {
		t.Fatal("dragging the project header collapsed it")
	}
	if a.selected != "s1" {
		t.Fatalf("dragging the project header selected %q", a.selected)
	}
}

func TestTabMenuAndClose(t *testing.T) {
	a := navTestApp(t)
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 1280, 700)
	tt.Frame()
	if err := tt.RightClick(locales.NavSessionTab("web")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{locales.SessionRename(), locales.ActionCloseSession(), locales.SessionPin()} {
		found := false
		for _, label := range tt.Menu() {
			found = found || label == want
		}
		if !found {
			t.Fatalf("the tab menu has no %q: %q", want, tt.Menu())
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	// A double click starts the session's rename in its tab.
	tab, _ := tt.Find(locales.NavSessionTab("web"))
	x, y := tab.X+tab.W/2, tab.Y+tab.H/2
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if a.renaming != "s2" {
		t.Fatalf("double click renamed %q, not s2", a.renaming)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	a.selectSession("s1")
	tt.Frame()
	// The close button shows under the pointer and closes only its session.
	tt.Move(tab.X+tab.W/2, tab.Y+tab.H/2)
	tt.Frame()
	if err := tt.Click(locales.ActionCloseSession()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.session("s2") != nil {
		t.Fatalf("closing the tab left its session: %q", a.selected)
	}
	if a.selected != "s1" {
		t.Fatalf("closing another tab selected %q", a.selected)
	}
}

func TestDraggingATabDoesNotSelectIt(t *testing.T) {
	a := navTestApp(t)
	a.conn = connReady
	a.setNavPlacement(navTop)
	tt := ui.NewTester(a.view, 1280, 760)
	tt.Frame()
	tab, ok := tt.Find("web")
	if !ok {
		t.Fatalf("no web tab: %q", tt.Texts())
	}
	x, y := tab.X+tab.W/2, tab.Y+tab.H/2
	tt.Press(x, y)
	for i := 1; i <= 6; i++ {
		tt.Move(x-float32(i)*8, y)
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	tt.Release(x-48, y)
	tt.Frame()
	if a.selected != "s1" {
		t.Fatalf("dragging a tab selected %q", a.selected)
	}
}

// sidebarFooterBounds is the narrow sidebar's left and right edge in a
// window of windowW.
func sidebarFooterBounds(place navPlacement, windowW, sidebarW float32) (float32, float32) {
	if place == navRight {
		return windowW - sidebarW, windowW
	}
	return 0, sidebarW
}

// footerRects checks that the controls named are in the sidebar and in the
// order given, without overlap. It returns the rects for the caller's own
// assertions.
func footerRects(t *testing.T, tt *ui.Tester, place navPlacement, windowW, sidebarW float32, labels ...string) []ui.Rect {
	t.Helper()
	x0, x1 := sidebarFooterBounds(place, windowW, sidebarW)
	rects := make([]ui.Rect, 0, len(labels))
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s: the footer has no %q: %q", place, label, tt.Texts())
		}
		if r.W < 20 || r.X < x0 || r.X+r.W > x1 {
			t.Errorf("%s: %q is clipped or compressed: %+v, sidebar %v..%v", place, label, r, x0, x1)
		}
		if len(rects) > 0 && rects[len(rects)-1].X+rects[len(rects)-1].W > r.X+0.5 {
			t.Errorf("%s: %q overlaps the control before it: %+v then %+v", place, label, rects[len(rects)-1], r)
		}
		rects = append(rects, r)
	}
	return rects
}

func TestSidebarFooterAtMinimumWidth(t *testing.T) {
	for _, place := range []navPlacement{navLeft, navRight} {
		t.Run(string(place), func(t *testing.T) {
			a := navTestApp(t)
			a.conn = connReady
			a.prefs.SidebarWidth = minSidebar
			a.st.Sessions[0].Status = proto.Waiting
			a.st.Sessions[1].Status = proto.Working
			a.st.Sessions[2].Status, a.st.Sessions[2].Unread = proto.Idle, true
			a.setNavPlacement(place)
			tt := ui.NewTester(a.view, 560, 700)
			tt.Frame()

			footerRects(t, tt, place, 560, minSidebar,
				locales.UsageTitle(),
				a.tip(locales.SettingsTitle(), "settings.open"),
				locales.FooterNeedsYou(1),
				locales.FooterWorking(1),
				locales.FooterDone(1),
				locales.SidebarMore(),
			)

			// Project and search moved into the labelled overflow.
			if err := tt.Click(locales.SidebarMore()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			menu := tt.Menu()
			for _, want := range []string{locales.ActionOpenProject(), locales.ActionGoToSession()} {
				found := false
				for _, label := range menu {
					found = found || label == want
				}
				if !found {
					t.Fatalf("%s: the overflow has no %q: %q", place, want, menu)
				}
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()

			// The attention chip still navigates.
			a.selected = "s2"
			tt.Frame()
			if err := tt.Click(locales.FooterNeedsYou(1)); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.selected != "s1" {
				t.Fatalf("%s: the attention chip selected %q, want s1", place, a.selected)
			}
			shot(t, tt, "nav-sidebar-min-"+string(place))
		})
	}
}

func TestSidebarFooterOverflowKeepsHiddenStatus(t *testing.T) {
	a := navTestApp(t)
	a.conn = connConnecting
	a.prefs.SidebarWidth = minSidebar
	a.st.Sessions[0].Status = proto.Waiting
	a.st.Sessions[1].Status = proto.Working
	a.st.Sessions[2].Status, a.st.Sessions[2].Unread = proto.Idle, true
	a.setNavPlacement(navLeft)
	tt := ui.NewTester(a.view, 560, 700)
	tt.Frame()
	if _, ok := tt.Find(locales.FooterDone(1)); ok {
		t.Fatal("the done chip did not give room to the connection and the other statuses")
	}
	if err := tt.Click(locales.SidebarMore()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem(locales.FooterDone(1)); err != nil {
		t.Fatalf("the overflow has no done action: %v, %q", err, tt.Menu())
	}
	tt.Frame()
	if a.selected != "s3" {
		t.Fatalf("the overflow done action selected %q, want s3", a.selected)
	}
}

// Attention is required, so it is placed before the optional project, search,
// working and done controls when the footer must overflow. The optional
// controls that are displaced stay reachable in the labelled menu.
func TestSidebarFooterReservesAttentionFirst(t *testing.T) {
	for _, place := range []navPlacement{navLeft, navRight} {
		for _, width := range []float32{239, 240, 241, 260, 268} {
			for _, count := range []int{1, 10, 100} {
				for _, bad := range []bool{false, true} {
					name := fmt.Sprintf("%s/%g/%d/bad=%v", place, width, count, bad)
					t.Run(name, func(t *testing.T) {
						a := navTestApp(t)
						a.conn = connReady
						a.daemonApp = ""
						if bad {
							a.daemonApp = "outdated"
						}
						a.prefs.SidebarWidth = width
						a.st.Sessions = nil
						for i := 0; i < count; i++ {
							id := fmt.Sprintf("waiting-%03d", i)
							a.st.Sessions = append(a.st.Sessions, proto.Session{
								ID: id, Project: "p1", Name: id, Status: proto.Waiting, Progress: -1,
							})
						}
						a.st.Sessions = append(a.st.Sessions,
							proto.Session{ID: "worker", Project: "p1", Name: "worker", Status: proto.Working, Progress: -1},
							proto.Session{ID: "done", Project: "p1", Name: "done", Status: proto.Idle, Unread: true, Progress: -1},
						)
						a.selected = "waiting-000"
						a.setNavPlacement(place)
						tt := ui.NewTester(a.view, 800, 700)
						tt.Frame()

						labels := []string{
							locales.UsageTitle(),
							a.tip(locales.SettingsTitle(), "settings.open"),
						}
						if bad {
							labels = append(labels, locales.CommonRestartService())
						}
						labels = append(labels, locales.FooterNeedsYou(count))
						if _, ok := tt.Find(locales.SidebarMore()); ok {
							labels = append(labels, locales.SidebarMore())
						}
						footerRects(t, tt, place, 800, width, labels...)

						_, projectVisible := tt.Find(a.tip(locales.ActionOpenProject(), "project.open"))
						_, searchVisible := tt.Find(a.tip(locales.ActionGoToSession(), "palette.open"))
						_, overflowVisible := tt.Find(locales.SidebarMore())
						if !projectVisible || !searchVisible {
							if !overflowVisible {
								t.Fatalf("the displaced project/search actions have no overflow: %q", tt.Texts())
							}
							if err := tt.Click(locales.SidebarMore()); err != nil {
								t.Fatal(err)
							}
							tt.Frame()
							menu := tt.Menu()
							for _, want := range []struct {
								visible bool
								label   string
							}{
								{projectVisible, locales.ActionOpenProject()},
								{searchVisible, locales.ActionGoToSession()},
							} {
								if want.visible {
									continue
								}
								found := false
								for _, label := range menu {
									found = found || label == want.label
								}
								if !found {
									t.Fatalf("the overflow has no %q: %q", want.label, menu)
								}
							}
							tt.Key(0, ui.KeyEscape)
							tt.Frame()
						}
					})
				}
			}
		}
	}
}

func TestSidebarFooterConnectionAtMinimumWidth(t *testing.T) {
	for _, place := range []navPlacement{navLeft, navRight} {
		for _, c := range []struct {
			name, daemon, label string
			conn                connState
		}{
			{"stale", "outdated", locales.CommonRestartService(), connReady},
			{"connecting", "", locales.StatusConnecting(), connConnecting},
			{"disconnected", "", locales.StatusDisconnected(), connFailed},
		} {
			t.Run(string(place)+"/"+c.name, func(t *testing.T) {
				a := navTestApp(t)
				a.prefs.SidebarWidth = minSidebar
				a.conn, a.daemonApp = c.conn, c.daemon
				a.st.Sessions[0].Status = proto.Waiting
				a.setNavPlacement(place)
				tt := ui.NewTester(a.view, 560, 700)
				tt.Frame()
				footerRects(t, tt, place, 560, minSidebar,
					locales.UsageTitle(),
					a.tip(locales.SettingsTitle(), "settings.open"),
					c.label,
					locales.FooterNeedsYou(1),
					locales.SidebarMore(),
				)
			})
		}
	}
}

func TestNavPlacementActionsAndSettings(t *testing.T) {
	a := testApp(t)
	for _, act := range []string{"nav.placement.left", "nav.placement.right", "nav.placement.top", "nav.placement.bottom"} {
		a, ok := actionFor(act)
		if !ok {
			t.Fatalf("no action %q", act)
		}
		if a.Keys != "" {
			t.Fatalf("%s has a default shortcut %q", act, a.Keys)
		}
	}
	// The keyboard page lists the placement actions, so a user can bind one
	// without a default.
	bindable := false
	for _, e := range searchEntries(nil) {
		if strings.Contains(e.words, "nav.placement.top") {
			bindable = true
		}
	}
	if !bindable {
		t.Fatal("Settings › Keyboard does not list the placement actions")
	}
	a.run("nav.placement.right", nil)
	if a.navPlacement() != navRight {
		t.Fatalf("the action placed navigation at %q", a.navPlacement())
	}
	found := false
	for _, it := range a.paletteItems() {
		if it.title == locales.NavPlacementTopAction() {
			found = true
			it.run()
			break
		}
	}
	if !found || a.navPlacement() != navTop {
		t.Fatalf("the palette did not place navigation at the top: %v, %q", found, a.navPlacement())
	}
	// Settings carries the same four choices.
	tt := ui.NewTester(a.view, 1100, 700)
	a.openSettings("general")
	tt.Frame()
	if !tt.HasText(locales.SetNavigationPlacement()) {
		t.Fatalf("Settings has no placement row: %q", tt.Texts())
	}
	shot(t, tt, "nav-settings-light")
	if err := tt.Click(locales.NavPlacementBottom()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.navPlacement() != navBottom {
		t.Fatalf("Settings placed navigation at %q", a.navPlacement())
	}
}

func TestHidingNavigationKeepsItsPlacement(t *testing.T) {
	a := navTestApp(t)
	a.setNavPlacement(navBottom)
	tt := ui.NewTester(a.view, 1280, 760)
	tt.Frame()
	if _, ok := tt.Find("demo"); !ok {
		t.Fatalf("the bottom tabs are missing: %q", tt.Texts())
	}
	// Cmd+B on the window hides the current navigation surface.
	a.run("sidebar.toggle", nil)
	for range 4 {
		tt.Frame()
		tt.Advance(200 * time.Millisecond)
	}
	tt.Frame()
	if !a.prefs.SidebarHidden || a.navPlacement() != navBottom {
		t.Fatalf("hidden %v, placement %q", a.prefs.SidebarHidden, a.navPlacement())
	}
	if a.chrome.shown > 0.01 {
		t.Fatalf("the navigation is still %v shown", a.chrome.shown)
	}
	if _, ok := tt.Find("demo"); ok {
		t.Fatalf("the hidden tabs still show: %q", tt.Texts())
	}
	a.run("sidebar.toggle", nil)
	for range 4 {
		tt.Frame()
		tt.Advance(200 * time.Millisecond)
	}
	tt.Frame()
	if _, ok := tt.Find("demo"); !ok {
		t.Fatalf("the tabs did not come back: %q", tt.Texts())
	}

	// At the top, the hidden tabs leave the header's reveal button after the
	// traffic lights, and the title after it.
	b := navTestApp(t)
	b.setNavPlacement(navTop)
	top := ui.NewTester(b.view, 1280, 760)
	top.Frame()
	b.run("sidebar.toggle", nil)
	for range 4 {
		top.Frame()
		top.Advance(200 * time.Millisecond)
	}
	top.Frame()
	reveal, ok := top.Find(locales.NavShowTabs())
	if !ok {
		t.Fatalf("the hidden tabs have no reveal control: %q", top.Texts())
	}
	fish, ok := top.Find("fish")
	if !ok {
		t.Fatalf("the pane header left with the tabs: %q", top.Texts())
	}
	if fish.X < reveal.X+reveal.W {
		t.Errorf("the title draws over the reveal control: title %+v, reveal %+v", fish, reveal)
	}
}

// actionFor finds an action in the registry, as the menus and `kero do` do.
func actionFor(id string) (actions.Action, bool) {
	return actions.Find(id)
}

// fakeUsageApp is the fake plugin app with a Usage plugin: the transient
// popover opens it through the same host as a real pane.
func fakeUsageApp(t *testing.T) (*App, chan func()) {
	return fakeUsageAppMarked(t, false)
}

func fakeUsageAppMarked(t *testing.T, mark bool) (*App, chan func()) {
	t.Helper()
	a, posts := fakePluginApp(t, mark)
	a.plugins.known = []registry.Plugin{{Manifest: protocol.Manifest{
		ID: "usage", Name: "Usage", Version: "1.0.0", Run: []string{"fake"},
		Widgets: []protocol.Widget{{Name: "usage", Title: "Usage"}},
	}, Builtin: true}}
	a.prefs.NavPlacement = "top"
	return a, posts
}

func TestUsagePopoverHoverPinAndDismiss(t *testing.T) {
	a, posts := fakeUsageApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	pump(t, tt, posts, "the trigger", func() bool {
		_, ok := tt.Find(locales.UsageTitle())
		return ok
	})
	trig, _ := tt.Find(locales.UsageTitle())
	tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Advance(60 * time.Millisecond)
	pump(t, tt, posts, "the usage widget", func() bool {
		st := a.plugins.panes[navUsagePane]
		return a.navUsage.open && st != nil && st.tree != nil
	})
	if a.navUsage.pinned {
		t.Fatal("hover pinned the popover")
	}
	if a.navUsage.hadFocus {
		t.Fatal("hover took the keyboard focus")
	}
	shot(t, tt, "usage-popover-hover")

	// The pointer crosses from the icon into the card.
	inner, ok := tt.Find("state: visible:true")
	if !ok {
		t.Fatalf("the card has no plugin text: %q", tt.Texts())
	}
	tt.Move(inner.X+inner.W/2, inner.Y+inner.H/2)
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	if !a.navUsage.open {
		t.Fatal("the card closed as the pointer came to it")
	}

	// A click pins it.
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Frame()
	if !a.navUsage.open || !a.navUsage.pinned {
		t.Fatalf("the click did not pin it: open %v pinned %v", a.navUsage.open, a.navUsage.pinned)
	}
	tt.Move(20, 300)
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	if !a.navUsage.open {
		t.Fatal("the pointer leaving closed a pinned card")
	}
	// The trigger closes it again, and the surface stops.
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Frame()
	if a.navUsage.open || a.plugins.panes[navUsagePane] != nil {
		t.Fatal("the second click did not close the card and its surface")
	}
}

// Open in Pane is only offered when there is a selected session and a
// Usage widget for it to open.
func TestNavUsageOpenPaneOnlyWhenUsable(t *testing.T) {
	a, posts := fakeUsageApp(t)
	a.selected = ""
	a.st.Sessions = nil
	tt := ui.NewTester(a.view, 760, 600)
	tt.Frame()
	a.toggleNavUsage(true)
	pump(t, tt, posts, "the usage card without a session", func() bool {
		st := a.plugins.panes[navUsagePane]
		return a.navUsage.open && st != nil && st.tree != nil
	})
	if _, ok := tt.Find(locales.NavUsageOpenPane()); ok {
		t.Fatalf("Open in Pane is shown without a session: %q", tt.Texts())
	}
}

// A short window must not clip the Usage popover's footer. The card takes
// the room its top or bottom anchor leaves, so the 30-point Open in Pane
// control and the footer's bottom padding stay whole, while the plugin list
// keeps the remaining room and scrolls.
func TestNavUsageFooterFitsShortWindows(t *testing.T) {
	for _, height := range []float32{360, 500} {
		for _, place := range []navPlacement{navTop, navBottom, navLeft, navRight} {
			t.Run(string(place)+"/"+fmt.Sprint(height), func(t *testing.T) {
				t.Setenv(appFakeScroll, "1")
				a, posts := fakeUsageApp(t)
				a.setNavPlacement(place)
				tt := ui.NewTester(a.view, 760, int(height))
				tt.Frame()
				a.toggleNavUsage(true)
				pump(t, tt, posts, "the scrolling usage card", func() bool {
					st := a.plugins.panes[navUsagePane]
					return a.navUsage.open && st != nil && st.tree != nil
				})
				panel, ok := tt.Find(locales.NavUsagePopover())
				if !ok {
					t.Fatalf("no popover panel: %q", tt.Texts())
				}
				button, ok := tt.Find(locales.NavUsageOpenPane())
				if !ok {
					t.Fatalf("no Open in Pane button: %q", tt.Texts())
				}
				if panel.Y < 0 || panel.Y+panel.H > height+0.5 {
					t.Fatalf("the panel is outside the window: panel=%+v height=%v", panel, height)
				}
				if button.H < 29.5 {
					t.Fatalf("the footer button is clipped: panel=%+v button=%+v", panel, button)
				}
				if button.Y < panel.Y || button.Y+button.H > panel.Y+panel.H-9.5 {
					t.Fatalf("the footer button or its bottom padding is clipped: panel=%+v button=%+v", panel, button)
				}
				st := a.plugins.panes[navUsagePane]
				if st == nil || st.tree == nil || !st.tree.Scroll {
					t.Fatalf("the plugin content is not scrollable: %+v", st)
				}
				if !tt.HasText("row 00") {
					t.Fatalf("the scrolling content's first row is not shown: %q", tt.Texts())
				}
				tt.Scroll(panel.X+panel.W/2, panel.Y+120, 0, -300)
				tt.Frame()
				if !tt.HasText("row 20") && !tt.HasText("row 30") {
					t.Fatalf("the plugin content did not scroll: %q", tt.Texts())
				}
				shot(t, tt, "nav-usage-short-"+string(place)+"-"+fmt.Sprint(int(height)))
			})
		}
	}
}

// A hover-open card that the keyboard has entered stays open while the focus
// remains inside, even after the pointer leaves. A passive hover never gains
// that focus.
func TestHoverPopoverKeepsKeyboardFocus(t *testing.T) {
	a, posts := fakeUsageApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	pump(t, tt, posts, "the trigger", func() bool {
		_, ok := tt.Find(locales.UsageTitle())
		return ok
	})
	trig, _ := tt.Find(locales.UsageTitle())
	tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Advance(60 * time.Millisecond)
	pump(t, tt, posts, "the hover card", func() bool {
		return a.navUsage.open && !a.navUsage.hadFocus
	})
	panel, ok := tt.Find(locales.NavUsagePopover())
	if !ok {
		t.Fatalf("no popover panel: %q", tt.Texts())
	}
	tt.ClickAt(panel.X+35, panel.Y+15)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !a.navUsage.hadFocus {
		t.Fatal("the card did not take keyboard focus")
	}
	tt.Move(10, 650)
	for range 4 {
		tt.Frame()
		tt.Advance(150 * time.Millisecond)
	}
	tt.Frame()
	if !a.navUsage.open {
		t.Fatal("the focused card closed when the pointer left")
	}
	if a.navUsage.pinned {
		t.Fatal("keyboard focus pinned the card")
	}
}

// The popover keeps its own surface and contrast in the dark appearance.
// It is pinned first, so the theme's frames do not depend on a hover.
func TestUsagePopoverDark(t *testing.T) {
	a, posts := fakeUsageApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	pump(t, tt, posts, "the trigger", func() bool {
		_, ok := tt.Find(locales.UsageTitle())
		return ok
	})
	trig, _ := tt.Find(locales.UsageTitle())
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	pump(t, tt, posts, "the pinned card", func() bool {
		st := a.plugins.panes[navUsagePane]
		return a.navUsage.open && st != nil && st.tree != nil
	})
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if !a.navUsage.open {
		t.Fatal("the theme change closed the pinned card")
	}
	shot(t, tt, "usage-popover-dark")
}

func TestUsagePopoverOutsideClickAndEscape(t *testing.T) {
	a, posts := fakeUsageApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	pump(t, tt, posts, "the trigger", func() bool {
		_, ok := tt.Find(locales.UsageTitle())
		return ok
	})
	trig, _ := tt.Find(locales.UsageTitle())
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	pump(t, tt, posts, "the pinned card", func() bool { return a.navUsage.open && a.plugins.panes[navUsagePane] != nil })
	tt.ClickAt(300, 400)
	tt.Frame()
	if a.navUsage.open {
		t.Fatal("a click outside did not close the card")
	}
	// A click pins the card and gives it the keyboard; Escape then closes it
	// and returns to the icon.
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	pump(t, tt, posts, "the pinned card", func() bool { return a.navUsage.open })
	tt.Frame()
	if !a.navUsage.pinned || !a.navUsage.hadFocus {
		t.Fatalf("the click did not pin and focus the card: pinned %v focus %v", a.navUsage.pinned, a.navUsage.hadFocus)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.navUsage.open {
		t.Fatal("Escape did not close the card")
	}
	// A keyboard open of a closed card also works.
	a.toggleNavUsage(true)
	tt.Frame()
	if !a.navUsage.open || !a.navUsage.keyboard {
		t.Fatalf("the keyboard did not open the card: open %v keyboard %v", a.navUsage.open, a.navUsage.keyboard)
	}
	a.dismissNavUsage(false)
	tt.Frame()
}

func TestUsagePopoverCoexistsWithAUsagePane(t *testing.T) {
	a, posts := fakeUsageApp(t)
	target, err := protocol.EncodeTarget(protocol.Target{Plugin: "usage", Widget: "usage"})
	if err != nil {
		t.Fatal(err)
	}
	p := &a.st.Sessions[0].Panes[1]
	p.View, p.Target = proto.PanePlugins, target
	tt := ui.NewTester(a.view, 1100, 700)
	pump(t, tt, posts, "the usage pane", func() bool {
		st := a.plugins.panes["b"]
		return st != nil && st.tree != nil
	})
	trig, ok := tt.Find(locales.UsageTitle())
	if !ok {
		t.Fatalf("no usage trigger: %q", tt.Texts())
	}
	tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Advance(60 * time.Millisecond)
	pump(t, tt, posts, "the popover surface", func() bool {
		st := a.plugins.panes[navUsagePane]
		return st != nil && st.tree != nil
	})
	if a.plugins.panes["b"] == nil {
		t.Fatal("opening the popover closed the real Usage pane")
	}
	// The first click pins the hovered card; the second closes it.
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Frame()
	tt.ClickAt(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Frame()
	if a.plugins.panes[navUsagePane] != nil || a.plugins.panes["b"] == nil {
		t.Fatal("closing the popover took the real Usage pane with it")
	}
}

func TestUsagePopoverOpensInwardAndStaysInTheWindow(t *testing.T) {
	a, posts := fakeUsageApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	for _, place := range []navPlacement{navLeft, navRight, navTop, navBottom} {
		tt.Move(5, 5)
		tt.Frame()
		a.setNavPlacement(place)
		tt.Frame()
		trig, ok := tt.Find(locales.UsageTitle())
		if !ok {
			t.Fatalf("%s: no usage trigger: %q", place, tt.Texts())
		}
		tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
		tt.Advance(60 * time.Millisecond)
		pump(t, tt, posts, "the "+string(place)+" popover", func() bool {
			st := a.plugins.panes[navUsagePane]
			return st != nil && st.tree != nil
		})
		panel, ok := tt.Find(locales.NavUsagePopover())
		if !ok {
			t.Fatalf("%s: the popover has no bounds: %q", place, tt.Texts())
		}
		switch place {
		case navLeft:
			if panel.X < trig.X+trig.W {
				t.Errorf("left: the popover opens over its icon: %+v, icon %+v", panel, trig)
			}
		case navRight:
			if panel.X+panel.W > trig.X {
				t.Errorf("right: the popover opens over its icon: %+v, icon %+v", panel, trig)
			}
		case navTop:
			if panel.Y < trig.Y+trig.H {
				t.Errorf("top: the popover opens over its icon: %+v, icon %+v", panel, trig)
			}
		case navBottom:
			if panel.Y+panel.H > trig.Y {
				t.Errorf("bottom: the popover opens over its icon: %+v, icon %+v", panel, trig)
			}
		}
		if panel.X < 0 || panel.Y < 0 || panel.X+panel.W > 1100 || panel.Y+panel.H > 700 {
			t.Errorf("%s: the popover leaves the window: %+v", place, panel)
		}
		a.dismissNavUsage(false)
		tt.Frame()
	}
	// A short window bounds the card and leaves the plugin's list to scroll.
	tt.Move(5, 5)
	tt.Frame()
	a.setNavPlacement(navTop)
	tt.Frame()
	trig, _ := tt.Find(locales.UsageTitle())
	tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Advance(60 * time.Millisecond)
	pump(t, tt, posts, "the popover", func() bool { return a.navUsage.open })
	tt.SetSize(560, 360)
	tt.Frame()
	panel, ok := tt.Find(locales.NavUsagePopover())
	if !ok || panel.H > 360 || panel.X < 0 || panel.X+panel.W > 560 {
		t.Fatalf("the short window does not bound the popover: %+v, found %v", panel, ok)
	}
}

func TestTransientPluginStateStaysOutOfTheDaemon(t *testing.T) {
	a := &App{}
	st := a.pluginSurfaceState(navUsagePane, "usage", "usage")
	saved := 0
	a.plugins.saveTarget = func(pane, target string) { saved++ }
	a.pluginStateSaved("usage", navUsagePane, []byte(`{"range":"30d"}`))
	if saved != 0 {
		t.Fatalf("the transient surface wrote a daemon target %d times", saved)
	}
	if string(st.state) != `{"range":"30d"}` {
		t.Fatalf("the transient state did not stay in memory: %s", st.state)
	}
}

func TestUsagePopoverCrashesAndRestarts(t *testing.T) {
	a, posts := fakeUsageAppMarked(t, true)
	tt := ui.NewTester(a.view, 1100, 700)
	pump(t, tt, posts, "the trigger", func() bool {
		_, ok := tt.Find(locales.UsageTitle())
		return ok
	})
	trig, _ := tt.Find(locales.UsageTitle())
	tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Advance(60 * time.Millisecond)
	pump(t, tt, posts, "the crash", func() bool { return tt.HasText(locales.PluginCrashTitle("Usage")) })
	if _, ok := tt.Find(locales.PluginRestart()); !ok {
		t.Fatalf("the crash has no Restart: %q", tt.Texts())
	}
	if err := tt.Click(locales.PluginRestart()); err != nil {
		t.Fatal(err)
	}
	pump(t, tt, posts, "the restarted widget", func() bool {
		return tt.HasText("state: visible:true")
	})
}

func TestUsagePopoverMissingPlugin(t *testing.T) {
	a := testApp(t)
	a.prefs.NavPlacement = "top"
	a.plugins.knownAt = true
	a.plugins.known = nil
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	trig, ok := tt.Find(locales.UsageTitle())
	if !ok {
		t.Fatalf("no usage trigger: %q", tt.Texts())
	}
	tt.Move(trig.X+trig.W/2, trig.Y+trig.H/2)
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if !tt.HasText(locales.PluginUnknownTitle()) {
		t.Fatalf("a missing Usage plugin says nothing: %q", tt.Texts())
	}
}
