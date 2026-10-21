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

// A Tasks pane whose workspace Linear did not answer offers to read it
// again, never to clear filters: those it would clear are the Linear
// page's, not the pane's.
func TestTasksNotReadKeepsLinearFilters(t *testing.T) {
	a := testTasks(t)
	a.st.Linear = &proto.LinearState{Status: proto.LinearUnreachable}
	a.lin.project, a.lin.query = "Rig", "gates"
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if !tt.HasText(locales.LinearNotRead()) {
		t.Fatalf("no not-read state; texts %q", tt.Texts())
	}
	if tt.HasText(locales.LinearClearFilters()) {
		t.Error("the not-read state offers to clear filters")
	}
	if !tt.HasText(locales.LinearStatusUnreachable()) || tt.HasText(locales.LinearNotReadDetail()) {
		t.Errorf("the not-read state does not say Linear was out of reach; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.LinearRefresh()); err != nil {
		t.Fatalf("no refresh in the not-read state: %v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	if a.lin.project != "Rig" || a.lin.query != "gates" {
		t.Errorf("the Linear page's filters changed: project %q, query %q", a.lin.project, a.lin.query)
	}
}

// A Tasks pane linked to a Linear project with no issues offers to read
// it again: the link's project is not a filter to clear.
func TestTasksLinkedNoIssuesRefreshes(t *testing.T) {
	a := testTasks(t)
	a.st.Linear.Issues = nil
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if !tt.HasText(locales.LinearNone()) {
		t.Fatalf("no empty state; texts %q", tt.Texts())
	}
	if tt.HasText(locales.LinearClearFilters()) {
		t.Error("the linked project counts as a filter to clear")
	}
	if !tt.HasText(locales.LinearRefresh()) {
		t.Errorf("no refresh in the empty state; texts %q", tt.Texts())
	}
	if tt.HasText(locales.LinearNoneFiltered()) {
		t.Error("the empty state says a filter hides the issues")
	}
	// A search the user typed is a filter, and Clear Filters clears it
	// and leaves the link.
	a.tasksState("b").query = "nothing like it"
	a.st.Linear.Issues = []proto.LinearIssue{{ID: "i1", Identifier: "BRU-1", Title: "One", State: "Todo", StateType: "unstarted", Project: "Rig"}}
	frames(tt)
	if err := tt.Click(locales.LinearClearFilters()); err != nil {
		t.Fatalf("no clear-filters for a search: %v; texts %q", err, tt.Texts())
	}
	frames(tt)
	if st := a.tasksState("b"); st.query != "" || st.project != "Rig" {
		t.Errorf("after Clear Filters: query %q, project %q; want none, Rig", st.query, st.project)
	}
}

// A workspace with the integration turned off sends the user to Settings,
// where it is turned on.
func TestLinearDisabledOpensSettings(t *testing.T) {
	a := testLinear(t)
	a.st.Linear = &proto.LinearState{Status: proto.LinearDisabled}
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if err := tt.Click(locales.CommonOpenSettings()); err != nil {
		t.Fatalf("no Open Settings for a disabled workspace: %v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	if !a.ss.open || a.ss.page != "linear" {
		t.Errorf("Settings opened at %q, open %v; want linear", a.ss.page, a.ss.open)
	}
}

// A Tasks pane whose issues are all done, and so hidden, says so, and
// offers to show them rather than to read Linear again.
func TestTasksAllDoneShowsDone(t *testing.T) {
	a := testTasks(t)
	a.st.Linear.Issues = []proto.LinearIssue{{ID: "i1", Identifier: "BRU-1", Title: "Shipped", State: "Done", StateType: "completed", Project: "Rig"}}
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if !tt.HasText(locales.TasksAllDone()) {
		t.Fatalf("the empty state does not say the issues are done; texts %q", tt.Texts())
	}
	if tt.HasText(locales.LinearRefresh()) {
		t.Error("the empty state offers a refresh for issues that are only hidden")
	}
	if err := tt.Click(locales.TasksShowDone()); err != nil {
		t.Fatalf("no Show Done: %v", err)
	}
	frames(tt)
	if a.tasksState("b").hideDone || !tt.HasText("Shipped") {
		t.Errorf("Show Done did not show the done issue; texts %q", tt.Texts())
	}
}

// A board of a workspace with the integration off says so, not that it
// has no issues.
func TestLinearDisabledSaysSo(t *testing.T) {
	a := testLinear(t)
	a.st.Linear = &proto.LinearState{Status: proto.LinearDisabled}
	tt := ui.NewTester(a.view, 1000, 700)
	frames(tt)
	if !tt.HasText(locales.LinearStatusDisabled()) {
		t.Errorf("no word that Linear is off; texts %q", tt.Texts())
	}
}
