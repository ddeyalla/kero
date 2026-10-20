package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// testLinear is the app with a workspace of a few issues, one of them
// worked on by a session, and no kerod.
func testLinear(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.show = "linear"
	a.selected = ""
	a.st.Linear = &proto.LinearState{
		Status:    proto.LinearOK,
		Team:      "BRU",
		TeamName:  "Brunoh",
		FetchedAt: time.Now().UnixMilli(),
		States: []proto.LinearWorkflowState{
			{ID: "s0", Name: "Backlog", Type: "backlog", Position: 0},
			{ID: "s1", Name: "Todo", Type: "unstarted", Position: 1},
			{ID: "s2", Name: "In Progress", Type: "started", Position: 2},
			{ID: "s3", Name: "In Review", Type: "started", Position: 1002},
			{ID: "s4", Name: "Done", Type: "completed", Position: 3},
		},
		Projects: []proto.LinearProject{{
			ID: "p1", Name: "Rig", Status: "backlog", Progress: 0.26,
			Milestones: []proto.LinearMilestone{
				{ID: "m1", Name: "M1 Foundation", Status: "next", Progress: 0.82},
				{ID: "m2", Name: "M2 Brands and intake", Status: "unstarted"},
			},
		}},
		Issues: []proto.LinearIssue{
			{ID: "i1", Identifier: "BRU-24", Title: "M1.15 Run topology, gates, cancel and deploys",
				State: "In Progress", StateType: "started", Project: "Rig", Milestone: "M1 Foundation",
				Blocking: 2, UpdatedAt: time.Now().Add(-3 * time.Hour).UnixMilli()},
			{ID: "i2", Identifier: "BRU-25", Title: "M1.16 Checks, critic runner and the verdict function",
				State: "Done", StateType: "completed", Project: "Rig", Milestone: "M1 Foundation",
				Blocked: 1, UpdatedAt: time.Now().Add(-2 * time.Hour).UnixMilli()},
			{ID: "i3", Identifier: "BRU-27", Title: "M1.19 Fault-injection suite and M1 exit",
				State: "Todo", StateType: "unstarted", Project: "Rig", Milestone: "M1 Foundation",
				UpdatedAt: time.Now().Add(-30 * time.Minute).UnixMilli()},
			{ID: "i4", Identifier: "BRU-33", Title: "M2.01 Web app shell",
				State: "Todo", StateType: "unstarted", Project: "Rig", Milestone: "M2 Brands and intake",
				UpdatedAt: time.Now().Add(-time.Hour).UnixMilli()},
		},
	}
	return a
}

// The board, with the sidebar and the workspace, for a look ($KERO_SHOTS).
func TestLinearBoardShot(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testLinear(t)
		tt := ui.NewTester(a.view, 1280, 720)
		tt.SetDark(dark)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		shot(t, tt, map[bool]string{false: "linear-board-light", true: "linear-board-dark"}[dark])
	}
}

// The list, for a look ($KERO_SHOTS).
func TestLinearListShot(t *testing.T) {
	a := testLinear(t)
	a.lin.view = "list"
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "linear-list-light")
}

// The board shows the issues of the workspace, by their state.
func TestLinearBoardGroupsByState(t *testing.T) {
	a := testLinear(t)
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	for _, want := range []string{"BRU-24", "BRU-25", "BRU-27", "M1 Foundation", "In Progress", "Done", "Todo"} {
		if !tt.HasText(want) {
			t.Errorf("the board does not show %q; texts %q", want, tt.Texts())
		}
	}
}

// The filters narrow the issues.
func TestLinearFilters(t *testing.T) {
	a := testLinear(t)
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	a.lin.project = "Rig"
	a.lin.milestone = "M2 Brands and intake"
	tt.Frame()
	if tt.HasText("BRU-24") {
		t.Error("the milestone filter did not narrow the issues")
	}
	if !tt.HasText("BRU-33") {
		t.Errorf("the filtered issue is gone; texts %q", tt.Texts())
	}
	a.lin.milestone = ""
	a.lin.query = "BRU-25"
	tt.Frame()
	if tt.HasText("BRU-24") {
		t.Error("the search did not narrow the issues")
	}
	if !tt.HasText("BRU-25") {
		t.Errorf("the searched issue is gone; texts %q", tt.Texts())
	}
}

// The issue open beside the board shows its comments and its session.
func TestLinearDetail(t *testing.T) {
	a := testLinear(t)
	a.st.Linear.Issues[1].Session = "s1"
	a.st.Linear.Issues[1].SessionName = "bru-25-rebuild"
	a.st.Linear.Issues[1].ResumeID = "9f2a1c44-0d1e-4a1b-9c77-2f0a5b1e3d90"
	a.lin.open = "i2"
	a.lin.detail = &proto.LinearIssueDetail{
		LinearIssue: a.st.Linear.Issues[1],
		Description: "**Prompt:** `Rig/build/tasks/M1/M1.16-checks.md`\n\nThe issue-code registry as data.",
		Comments: []proto.LinearComment{{
			ID: "c1", Author: "Divyanshu DV", CreatedAt: time.Now().Add(-2 * time.Hour).UnixMilli(),
			Body: "M1.16 is done on `m1-foundation`.\n\n**Summary:** 171 issue codes registered.",
		}},
	}
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	for _, want := range []string{"BRU-25", "bru-25-rebuild", "9f2a1c44", "Summary", "Activity", "Resume"} {
		if !tt.HasText(want) {
			t.Errorf("the detail does not show %q; texts %q", want, tt.Texts())
		}
	}
	shot(t, tt, "linear-detail-light")
}

// The sidebar's row opens the workspace, and the count is the issues a
// session works on.
func TestLinearNav(t *testing.T) {
	a := testLinear(t)
	a.st.Linear.Issues[0].Session = "s1"
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	if !tt.HasText("Linear") {
		t.Fatalf("the sidebar has no Linear row; texts %q", tt.Texts())
	}
	if err := tt.Click("Linear"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.show != "linear" {
		t.Errorf("the workspace did not open: show=%q", a.show)
	}
}

func TestLinearModeSurvivesDaemonStateUpdates(t *testing.T) {
	a := testLinear(t)
	a.reconcileSelection(false)
	if a.show != "linear" {
		t.Fatal("a daemon broadcast navigated away from Linear")
	}
}

func TestLinearStaleDetailResponseIgnored(t *testing.T) {
	a := testLinear(t)
	a.lin.open = "second"
	a.lin.request = 2
	a.lin.loading = true
	a.linearComplete("first", 1, &proto.LinearIssueDetail{LinearIssue: proto.LinearIssue{ID: "first"}}, nil)
	if a.lin.detail != nil || !a.lin.loading {
		t.Fatal("stale response replaced current issue")
	}
	a.linearComplete("second", 2, &proto.LinearIssueDetail{LinearIssue: proto.LinearIssue{ID: "second"}}, nil)
	if a.lin.detail == nil || a.lin.detail.ID != "second" || a.lin.loading {
		t.Fatal("current response missing")
	}
}

func TestLinearUnavailableHasRetryInsteadOfEndlessSpinner(t *testing.T) {
	a := testLinear(t)
	a.linearOpen("i2") // no daemon in this test
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if a.lin.loading || !tt.HasText("Issue unavailable") || !tt.HasText("Retry") {
		t.Fatalf("missing failure state: %q", tt.Texts())
	}
}

func TestLinearCredentialsAndOfflineStates(t *testing.T) {
	for _, status := range []string{proto.LinearNoToken, proto.LinearAuthFailed, proto.LinearUnreachable, proto.LinearRateLimited} {
		a := testLinear(t)
		a.st.Linear.Status = status
		a.st.Linear.Issues = nil
		tt := ui.NewTester(a.view, 900, 640)
		tt.Frame()
		if !tt.HasText(a.linearStatusLabel()) {
			t.Fatalf("status %s not rendered", status)
		}
	}
	a := testLinear(t)
	a.st.Linear.Status = proto.LinearUnreachable
	tt := ui.NewTester(a.view, 900, 640)
	tt.Frame()
	if !tt.HasText("BRU-24") {
		t.Fatal("offline cache disappeared")
	}
}

func TestLinearRemovedFiltersFallBack(t *testing.T) {
	a := testLinear(t)
	a.lin.project = "deleted"
	a.lin.milestone = "gone"
	a.linearValidateFilters()
	if a.lin.project != "" || a.lin.milestone != "" || a.lin.filterNotice == "" {
		t.Fatal("invalid filters were silently retained")
	}
}

func TestLinearRemoteLinksCannotOpenFilesOrCommands(t *testing.T) {
	for _, link := range []string{"file:///etc/passwd", "javascript:alert(1)", "vscode://file/a", "/tmp/private", "https://user:password@example.test"} {
		if linearSafeURL(link) {
			t.Fatalf("unsafe remote link accepted: %s", link)
		}
	}
	if !linearSafeURL("https://linear.app/team/issue/BRU-25") {
		t.Fatal("web link refused")
	}
}

func TestLinearMenuRegistersShortcutAndRefresh(t *testing.T) {
	a := testLinear(t)
	var found, refresh bool
	var walk func([]*mygo.MenuItem)
	walk = func(items []*mygo.MenuItem) {
		for _, it := range items {
			if it.Label == "Open Linear" {
				found = it.Accelerator == "CmdOrCtrl+Shift+L"
			}
			if it.Label == "Refresh Linear" {
				refresh = true
			}
			walk(it.Submenu)
		}
	}
	walk(a.viewMenuItems())
	if !found || !refresh {
		t.Fatal("Linear actions missing from native menu/shortcut dispatch")
	}
}
