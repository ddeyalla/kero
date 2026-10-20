package main

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"

	"kero/locales"
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
			{ID: "s0", Name: "Backlog", Type: "backlog", Position: 0, Color: "#bec2c8"},
			{ID: "s1", Name: "Todo", Type: "unstarted", Position: 1, Color: "#e2e2e2"},
			{ID: "s2", Name: "In Progress", Type: "started", Position: 2, Color: "#f2c94c"},
			{ID: "s3", Name: "In Review", Type: "started", Position: 1002, Color: "#0f783c"},
			{ID: "s4", Name: "Done", Type: "completed", Position: 3, Color: "#5e6ad2"},
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
				Blocked: 1, Children: 2, UpdatedAt: time.Now().Add(-2 * time.Hour).UnixMilli()},
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
	a.lin.detail.Runs = []proto.TaskRun{{Agent: "claude", ID: "9f2a1c44-0d1e-4a1b-9c77-2f0a5b1e3d90", Session: "gone", Dir: "/repo",
		Started: time.Now().Add(-3 * time.Hour).UnixMilli(), Ended: time.Now().Add(-time.Hour).UnixMilli()}}
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	for _, want := range []string{"BRU-25", "9f2a1c44", "Summary", "Activity", "Resume", "Claude Code"} {
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
		if !tt.HasText(a.linearStatusLabel(nil)) {
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

// testDetail opens BRU-25 in the detail, as read from Linear, with the
// session working on it.
func testDetail(a *App) {
	is := &a.st.Linear.Issues[1]
	is.Session, is.SessionName, is.SessionStatus, is.Progress = "s1", "bru-25-rebuild", proto.Working, 60
	is.ResumeID = "9f2a1c44-0d1e-4a1b-9c77-2f0a5b1e3d90"
	is.Relations = []proto.LinearRelation{
		{Type: "blockedBy", Identifier: "BRU-24", Title: "M1.15 Run topology, gates, cancel and deploys", State: "In Progress"},
		{Type: "related", Identifier: "BRU-27", Title: "M1.19 Fault-injection suite and M1 exit", State: "Todo"},
	}
	a.lin.open = is.ID
	a.lin.detail = &proto.LinearIssueDetail{
		LinearIssue: *is,
		BranchName:  "dv/bru-25-m116-checks-critic-runner",
		Description: "**Prompt:** `Rig/build/tasks/M1/M1.16-checks.md`\n**Depends on:** M1.07, M1.12\n\n- The issue-code registry as data.\n- Checks run as leaf workflows.",
		Attachments: []proto.LinearAttachment{{ID: "a1", Title: "Task prompt", URL: "https://github.com/dv-brunoh/rig/blob/main/build/tasks/M1/M1.16.md", Subtitle: "github.com"}},
		Comments: []proto.LinearComment{{
			ID: "c1", Author: "Divyanshu DV", CreatedAt: time.Now().Add(-2 * time.Hour).UnixMilli(),
			Body: "M1.16 is done on `m1-foundation`.\n\n**Summary:** 171 issue codes registered.\n**Tests:** 158 files passed.",
		}},
	}
}

// The issue opens in a card of its own beside the board, which stays.
func TestLinearDetailIsAPane(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testLinear(t)
		testDetail(a)
		tt := ui.NewTester(a.view, 1280, 760)
		tt.SetDark(dark)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		for _, want := range []string{"In Progress", "BRU-24", "M1.16 Checks, critic runner and the verdict function", "Properties", "Task prompt", "Blocked by", "Activity"} {
			if !tt.HasText(want) && want != "Properties" {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		shot(t, tt, map[bool]string{false: "linear-detail-pane-light", true: "linear-detail-pane-dark"}[dark])
	}
	// Wide, the properties stand in a column of their own.
	a := testLinear(t)
	testDetail(a)
	a.lin.detailW = 800
	tt := ui.NewTester(a.view, 1680, 900)
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("Properties") {
		t.Errorf("a wide detail has no properties column; texts %q", tt.Texts())
	}
	shot(t, tt, "linear-detail-wide-dark")
}

// On a window too narrow for both, the issue takes the board's place,
// and leads back to it.
func TestLinearNarrowDetailTakesOver(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if tt.HasText("In Progress") && tt.HasText("BRU-33") {
		t.Errorf("the board shows beside a detail that has no room; texts %q", tt.Texts())
	}
	if !tt.HasText("M1.16 Checks, critic runner and the verdict function") {
		t.Fatalf("no detail; texts %q", tt.Texts())
	}
	shot(t, tt, "linear-detail-narrow-light")
	a.linearClose()
	tt.Frame()
	if !tt.HasText("BRU-33") {
		t.Errorf("closing the detail did not bring the board back; texts %q", tt.Texts())
	}
}

// With the sidebar hidden, the first card keeps the sidebar's button
// after the traffic lights, and the last the window's controls.
func TestLinearSidebarHiddenShot(t *testing.T) {
	a := testLinear(t)
	a.prefs.SidebarHidden = true
	testDetail(a)
	tt := ui.NewTester(a.view, 1280, 760)
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "linear-sidebar-hidden-dark")
}

// J and K step through the open issue's column, and Escape closes it.
func TestLinearKeysStepAndClose(t *testing.T) {
	a := testLinear(t)
	tt := ui.NewTester(a.view, 1400, 760)
	a.linearOpen("i3") // BRU-27, the first of Todo: newer than BRU-33
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if a.lin.open != "i4" {
		t.Fatalf("J: open %q, want i4", a.lin.open)
	}
	tt.Key(0, ui.KeyJ) // the end of the column
	tt.Frame()
	if a.lin.open != "i4" {
		t.Fatalf("J past the end: open %q", a.lin.open)
	}
	tt.Key(0, ui.KeyK)
	tt.Frame()
	if a.lin.open != "i3" {
		t.Fatalf("K: open %q, want i3", a.lin.open)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.lin.open != "" {
		t.Fatalf("Escape: open %q", a.lin.open)
	}
}

// While the search has the focus, the keys are the search's.
func TestLinearKeysYieldToSearch(t *testing.T) {
	a := testLinear(t)
	tt := ui.NewTester(a.view, 1400, 760)
	a.linearOpen("i3")
	tt.Frame()
	if err := tt.Click("Search issues"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.lin.open != "i3" {
		t.Fatalf("a key typed into the search moved or closed the detail: open %q", a.lin.open)
	}
}

// The model is worked out once for each state and filter, not each frame.
func TestLinearModelCached(t *testing.T) {
	a := testLinear(t)
	pal := paletteForDark(false)
	m1 := a.linearModel(pal)
	first := &m1.issues[0]
	if m2 := a.linearModel(pal); &m2.issues[0] != first {
		t.Fatal("the model was worked out again with nothing changed")
	}
	a.lin.milestone = "M2 Brands and intake"
	if m3 := a.linearModel(pal); len(m3.issues) != 1 {
		t.Fatalf("a changed filter kept the old model: %d issues", len(m3.issues))
	}
	a.lin.milestone = ""
	next := *a.st.Linear
	next.Issues = append([]proto.LinearIssue{}, next.Issues...)
	next.Issues[0].State, next.Issues[0].StateType = "Done", "completed"
	a.st.Linear = &next
	if m4 := a.linearModel(pal); len(m4.byState["Done"]) != 2 {
		t.Fatalf("a new state from the daemon kept the old model: %v", m4.byState["Done"])
	}
}

// Empty states are a rail, columns, or nothing, as the setting says.
func TestLinearEmptyColumnsSetting(t *testing.T) {
	for mode, want := range map[string][2]bool{"collapsed": {true, true}, "shown": {false, true}, "hidden": {false, false}} {
		a := testLinear(t)
		a.set.LinearEmptyColumns = mode
		tt := ui.NewTester(a.view, 1500, 720)
		tt.Frame()
		if got := tt.HasText("Hidden columns"); got != want[0] {
			t.Errorf("%s: rail %v, want %v", mode, got, want[0])
		}
		if got := tt.HasText("Backlog"); got != want[1] {
			t.Errorf("%s: Backlog shown %v, want %v", mode, got, want[1])
		}
		if mode == "collapsed" {
			shot(t, tt, "linear-board-rail-light")
		}
	}
}

// Compact cards leave out the chips and the time.
func TestLinearCompactCards(t *testing.T) {
	a := testLinear(t)
	a.set.LinearDensity = "compact"
	tt := ui.NewTester(a.view, 1280, 720)
	tt.Frame()
	if tt.HasText("M1 Foundation") || tt.HasText(locales.LinearUpdated("2h")) {
		t.Errorf("compact cards show chips or times; texts %q", tt.Texts())
	}
	if !tt.HasText("BRU-25") {
		t.Errorf("compact cards lost the identifier; texts %q", tt.Texts())
	}
	shot(t, tt, "linear-board-compact-light")
}

// The sidebar's row goes when the setting says so.
func TestLinearSidebarSetting(t *testing.T) {
	a := testApp(t)
	a.st.Linear = &proto.LinearState{Status: proto.LinearOK}
	a.set.LinearSidebar = false
	tt := ui.NewTester(a.view, 1100, 640)
	tt.Frame()
	if tt.HasText("Linear") {
		t.Errorf("the Linear row shows with the setting off; texts %q", tt.Texts())
	}
	a.set.LinearSidebar = true
	tt.Frame()
	if !tt.HasText("Linear") {
		t.Errorf("no Linear row; texts %q", tt.Texts())
	}
}

// A state takes the team's color, but for the gray ones, and one by its
// meaning from a daemon that sends none.
func TestLinearStateColors(t *testing.T) {
	pal := paletteForDark(false)
	if got := linearStateColor("started", "#0f783c", pal); got != ui.Hex("#0f783c") {
		t.Errorf("started: %v", got)
	}
	if got := linearStateColor("unstarted", "#e2e2e2", pal); got != pal.textFaint {
		t.Errorf("unstarted took the team's pale gray: %v", got)
	}
	if got := linearStateColor("completed", "", pal); got != ui.Hex("#5e6ad2") {
		t.Errorf("completed without a color: %v", got)
	}
	marks := linearMarks(testLinear(t).st.Linear.States, pal)
	if marks["In Review"].fill <= marks["In Progress"].fill {
		t.Errorf("In Review fills no more than In Progress: %v %v", marks["In Review"].fill, marks["In Progress"].fill)
	}
}

// The open issue is read again when the board says it changed, and not
// otherwise.
func TestLinearDetailRereadOnChange(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	req := a.lin.request
	a.linearFreshen()
	if a.lin.request != req {
		t.Fatal("an unchanged issue was read again")
	}
	a.st.Linear.Issues[1].UpdatedAt = time.Now().UnixMilli()
	a.linearFreshen()
	if a.lin.request == req {
		t.Fatal("a changed issue was not read again")
	}
}

func TestInitials(t *testing.T) {
	for name, want := range map[string]string{"Divyanshu DV": "DD", "linear": "L", "": "?", "Ada Byron King": "AB"} {
		if got := initials(name); got != want {
			t.Errorf("initials(%q) = %q, want %q", name, got, want)
		}
	}
}

// The list, grouped by state, in the dark ($KERO_SHOTS).
func TestLinearListDarkShot(t *testing.T) {
	a := testLinear(t)
	a.lin.view = "list"
	a.st.Linear.Issues[0].Session, a.st.Linear.Issues[0].SessionName, a.st.Linear.Issues[0].SessionStatus = "s1", "rig-builder", proto.Working
	tt := ui.NewTester(a.view, 1280, 720)
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("In Progress") || !tt.HasText("rig-builder") {
		t.Errorf("the list lost its groups or its session; texts %q", tt.Texts())
	}
	shot(t, tt, "linear-list-dark")
}

// A read again of the issue shown that fails keeps what shows, while a
// first read that fails says so.
func TestLinearFailedRereadKeepsDetail(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	a.lin.request++
	a.linearComplete("i2", a.lin.request, nil, errTest)
	if a.lin.err != "" || a.lin.detail == nil {
		t.Fatalf("a failed read again lost the detail: err %q", a.lin.err)
	}
	a.linearClose()
	a.lin.open, a.lin.request = "i1", a.lin.request+1
	a.linearComplete("i1", a.lin.request, nil, errTest)
	if a.lin.err == "" {
		t.Fatal("a failed first read said nothing")
	}
}

var errTest = errors.New("unreachable")

// A failed read again of the issue shown is not tried again at once:
// each try would be a request to Linear, which may be refusing them.
func TestLinearFailedRereadDoesNotRetryAtOnce(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	a.st.Linear.Issues[1].UpdatedAt = time.Now().UnixMilli()
	a.lin.request++
	a.linearComplete("i2", a.lin.request, nil, errTest)
	req := a.lin.request
	a.linearFreshen()
	if a.lin.request != req {
		t.Fatal("a failed read again was tried again at once")
	}
}

// A drag of the divider sizes the issue's card, and is kept as it ends;
// a click that does not move keeps the width as it was.
func TestLinearDividerDrag(t *testing.T) {
	a := testLinear(t)
	testDetail(a)
	tt := ui.NewTester(a.view, 1280, 760)
	tt.Frame()
	// The issue's card is the default width from the window's right,
	// after the gap around the cards; the divider is the gap before it.
	x, y := float32(1280-cardGap-linearDetailW-paneGap/2), float32(400)
	tt.Press(x, y)
	tt.Frame()
	tt.Release(x, y)
	tt.Frame()
	if a.lin.detailW != 0 {
		t.Fatalf("a click on the divider kept a width: %v", a.lin.detailW)
	}
	tt.Press(x, y)
	for i := 1; i <= 8; i++ {
		tt.Move(x-float32(i)*10, y)
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	tt.Release(x-80, y)
	tt.Frame()
	if a.lin.detailW < linearDetailW+60 || a.lin.detailW > linearDetailW+100 {
		t.Fatalf("dragged 80 left: width %v, want about %v", a.lin.detailW, linearDetailW+80)
	}
	if a.prefs.Linear.DetailWidth != a.lin.detailW {
		t.Errorf("the width was not kept as the drag ended: %v", a.prefs.Linear.DetailWidth)
	}
}

// Every conversation on the issue shows, the latest first: one still
// running opens its session, one that ended resumes, one of a harness
// without a session id shows but does not resume.
func TestLinearSessionsLog(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := testLinear(t)
		testDetail(a)
		now := time.Now()
		a.st.Linear.Issues[1].Session, a.st.Linear.Issues[1].SessionName = "s1", "demo"
		a.st.Linear.Issues[1].SessionStatus = proto.Working
		a.lin.detail.Runs = []proto.TaskRun{
			{Agent: "codex", ID: "019a7f3e-codex", Session: "s1", SessionName: "demo", Pane: "a", Dir: "/tmp", Via: "task", Started: now.Add(-10 * time.Minute).UnixMilli()},
			{Agent: "gemini", Session: "old", SessionName: "bru-25-try", Dir: "/tmp", Started: now.Add(-5 * time.Hour).UnixMilli(), Ended: now.Add(-4 * time.Hour).UnixMilli()},
			{Agent: "claude", ID: "9f2a1c44-0d1e", Session: "old", SessionName: "bru-25-first", Dir: "/repo/wt", Via: "branch", Started: now.Add(-26 * time.Hour).UnixMilli(), Ended: now.Add(-25 * time.Hour).UnixMilli()},
		}
		tt := ui.NewTester(a.view, 1280, 900)
		tt.SetDark(dark)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		for _, want := range []string{"Sessions", "Codex", "Gemini CLI", "Claude Code", "linked by the agent", "linked by branch", "Open", "Resume", "019a7f3e-codex"} {
			if !tt.HasText(want) {
				t.Errorf("dark=%v: no %q; texts %q", dark, want, tt.Texts())
			}
		}
		shot(t, tt, map[bool]string{false: "linear-sessions-light", true: "linear-sessions-dark"}[dark])
	}
	a := testLinear(t)
	p, ok := a.resumeParams(proto.TaskRun{Agent: "claude", ID: "9f2a1c44-0d1e", Session: "s1", Dir: "/repo/wt"}, "BRU-25")
	if !ok || p.Dir != "/repo/wt" || p.Task != "BRU-25" || p.Project != "p1" || len(p.Command) != 3 || p.Command[0] != "claude" || p.Command[2] != "9f2a1c44-0d1e" {
		t.Fatalf("resume params: %+v", p)
	}
	if _, ok := a.resumeParams(proto.TaskRun{Agent: "gemini", Session: "s1"}, "BRU-25"); ok {
		t.Fatal("a run with no session id resumed")
	}
}
