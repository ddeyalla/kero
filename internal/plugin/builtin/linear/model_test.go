package linear

import (
	"reflect"
	"slices"
	"testing"

	"kero/internal/proto"
)

func TestSectionOf(t *testing.T) {
	cases := map[string]string{
		"started": secCurrent, "completed": secDone, "unstarted": secTodo,
		"backlog": secBacklog, "triage": secBacklog,
		"canceled": secCanceled, "duplicate": secCanceled,
		"": secTodo, "something new": secTodo,
	}
	for typ, want := range cases {
		if got := sectionOf(typ); got != want {
			t.Errorf("sectionOf(%q) = %q, want %q", typ, got, want)
		}
	}
}

func sectionKeys(secs []section) []string {
	var out []string
	for _, s := range secs {
		out = append(out, s.Key)
	}
	return out
}

func identsOf(list []proto.LinearIssue) []string {
	var out []string
	for _, i := range list {
		out = append(out, i.Identifier)
	}
	return out
}

func TestSectionsOrderTriageAndCanceled(t *testing.T) {
	s := snap(
		issue(1, "triage"), issue(2, "backlog"), issue(3, "unstarted"),
		issue(4, "started"), issue(5, "completed"), issue(6, "canceled"), issue(7, "duplicate"),
	)
	got := buildSections(s, s.Issues, focusInfo{}, false, false)
	if want := []string{secCurrent, secDone, secTodo, secBacklog}; !slices.Equal(sectionKeys(got), want) {
		t.Fatalf("sections %v, want %v (canceled is hidden)", sectionKeys(got), want)
	}
	backlog := got[3]
	if !slices.Equal(identsOf(backlog.Issues), []string{"BRU-1", "BRU-2"}) {
		t.Fatalf("triage joins the backlog, newest change first: %v", identsOf(backlog.Issues))
	}
	got = buildSections(s, s.Issues, focusInfo{}, false, true)
	if want := []string{secCurrent, secDone, secTodo, secBacklog, secCanceled}; !slices.Equal(sectionKeys(got), want) {
		t.Fatalf("with canceled shown: %v, want %v", sectionKeys(got), want)
	}
	if n := len(got[4].Issues); n != 2 {
		t.Fatalf("canceled holds the canceled and the duplicates, got %d", n)
	}
}

func TestCurrentOrderFollowsTheFocusedSession(t *testing.T) {
	s := snap(
		issue(1, "started"), // someone else's, changed last
		issue(2, "started", assignedTo("me", "Me")),
		issue(3, "started", withSession("s-other", "other")),
		issue(4, "started"),
		issue(5, "started", withSession("s-focus", "focus"), assignedTo("someone", "Some One")),
		issue(6, "started", assignedTo("me", "Me")),
	)
	order := func(focus focusInfo) []string {
		secs := buildSections(s, s.Issues, focus, false, false)
		return identsOf(secs[0].Issues)
	}
	// With no focus: live sessions, then mine, then the rest; each by update.
	if got, want := order(focusInfo{}), []string{"BRU-3", "BRU-5", "BRU-2", "BRU-6", "BRU-1", "BRU-4"}; !slices.Equal(got, want) {
		t.Fatalf("unfocused order %v, want %v", got, want)
	}
	// The focused session's issue goes first, found by its session...
	if got := order(focusInfo{Session: "s-focus"}); got[0] != "BRU-5" || got[1] != "BRU-3" {
		t.Fatalf("focused by session: %v", got)
	}
	// ...or by a task its panes are linked to.
	if got := order(focusInfo{Session: "s-x", Tasks: []string{"BRU-4"}}); got[0] != "BRU-4" {
		t.Fatalf("focused by task: %v", got)
	}
	if got := order(focusInfo{Session: "s-x", Tasks: []string{"id-1"}}); got[0] != "BRU-1" {
		t.Fatalf("focused by task id: %v", got)
	}
	// Other sections do not use the tiers: the latest change first.
	d := snap(issue(9, "completed"), issue(8, "completed", withSession("s", "n")))
	secs := buildSections(d, d.Issues, focusInfo{Session: "s"}, false, false)
	if got := identsOf(secs[0].Issues); got[0] != "BRU-8" {
		t.Fatalf("done order %v", got)
	}
}

func TestDoneKeepsTheLatestTwentyUntilAsked(t *testing.T) {
	var issues []proto.LinearIssue
	for i := 1; i <= 35; i++ {
		issues = append(issues, issue(i, "completed"))
	}
	s := snap(issues...)
	secs := buildSections(s, s.Issues, focusInfo{}, false, false)
	if len(secs[0].Issues) != 20 || secs[0].More != 15 || secs[0].Total != 35 {
		t.Fatalf("done shows %d, hides %d of %d", len(secs[0].Issues), secs[0].More, secs[0].Total)
	}
	if secs[0].Issues[0].Identifier != "BRU-1" || secs[0].Issues[19].Identifier != "BRU-20" {
		t.Fatalf("done is not the latest twenty: %v", identsOf(secs[0].Issues))
	}
	secs = buildSections(s, s.Issues, focusInfo{}, true, false)
	if len(secs[0].Issues) != 35 || secs[0].More != 0 {
		t.Fatalf("show more: %d shown, %d hidden", len(secs[0].Issues), secs[0].More)
	}
}

func TestFilters(t *testing.T) {
	s := snap(
		issue(1, "started", assignedTo("me", "Me"), inProject("Rig")),
		issue(2, "started", assignedTo("ann", "Ann Lee"), inProject("Web"), func(i *proto.LinearIssue) {
			i.Labels = []string{"bug"}
			i.Priority = 1
		}),
		issue(3, "unstarted", inProject("Rig"), func(i *proto.LinearIssue) { i.Title = "Fix the Parser" }),
	)
	cases := []struct {
		name  string
		f     filters
		query string
		scope string
		want  []string
	}{
		{"none", filters{}, "", "", []string{"BRU-1", "BRU-2", "BRU-3"}},
		{"me", filters{Assignee: assigneeMe}, "", "", []string{"BRU-1"}},
		{"person", filters{Assignee: "ann"}, "", "", []string{"BRU-2"}},
		{"project", filters{Project: "Rig"}, "", "", []string{"BRU-1", "BRU-3"}},
		{"scope", filters{}, "", "Web", []string{"BRU-2"}},
		{"label", filters{Label: "bug"}, "", "", []string{"BRU-2"}},
		{"priority", filters{Priority: "1"}, "", "", []string{"BRU-2"}},
		{"no priority", filters{Priority: "0"}, "", "", []string{"BRU-1", "BRU-3"}},
		{"query title", filters{}, "parser", "", []string{"BRU-3"}},
		{"query ident", filters{}, "bru-2", "", []string{"BRU-2"}},
		{"both", filters{Project: "Rig", Assignee: assigneeMe}, "", "", []string{"BRU-1"}},
	}
	for _, c := range cases {
		if got := identsOf(filterIssues(s, c.f, c.query, c.scope)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// A viewer the daemon did not tell does not empty the pane.
	s.Viewer = ""
	if got := filterIssues(s, filters{Assignee: assigneeMe}, "", ""); len(got) != 3 {
		t.Errorf("me without a viewer kept %d", len(got))
	}
}

func TestNestingGroupsAndCycles(t *testing.T) {
	s := snap(
		issue(1, "started", inProject("Rig")),
		issue(2, "started", inProject("Rig"), withParent("BRU-1")),
		issue(3, "started", inProject("Rig"), withParent("BRU-2")),
		issue(4, "started", inProject("Rig"), withParent("BRU-99")), // parent elsewhere
		issue(5, "started"),
		issue(6, "started", inProject("Web")),
	)
	secs := buildSections(s, s.Issues, focusInfo{}, false, false)
	groups := groupProjects(secs[0])
	var names []string
	for _, g := range groups {
		names = append(names, g.Name)
	}
	if !slices.Equal(names, []string{"Rig", "Web", noProject}) {
		t.Fatalf("groups %q: the Current section orders by best issue, no project last", names)
	}
	rig := groups[0]
	if rig.Count != 4 || len(rig.Roots) != 2 {
		t.Fatalf("rig: %d issues, %d roots", rig.Count, len(rig.Roots))
	}
	if root := rig.Roots[0]; root.Issue.Identifier != "BRU-1" || len(root.Kids) != 1 || len(root.Kids[0].Kids) != 1 || root.count() != 3 {
		t.Fatalf("BRU-1 should hold BRU-2 which holds BRU-3: %+v", root)
	}

	// Other sections order the groups by name.
	todo := snap(issue(1, "unstarted", inProject("Zed")), issue(2, "unstarted", inProject("alpha")), issue(3, "unstarted"))
	g := groupProjects(buildSections(todo, todo.Issues, focusInfo{}, false, false)[0])
	if g[0].Name != "alpha" || g[1].Name != "Zed" || g[2].Name != noProject {
		t.Fatalf("todo groups: %q %q %q", g[0].Name, g[1].Name, g[2].Name)
	}

	// Parents in a cycle do not vanish.
	cyc := nestIssues([]proto.LinearIssue{
		issue(1, "started", withParent("BRU-2")), issue(2, "started", withParent("BRU-1")),
		issue(3, "started", withParent("BRU-3")),
	})
	total := 0
	for _, r := range cyc {
		total += r.count()
	}
	if total != 3 {
		t.Fatalf("a cycle lost issues: %d of 3 reachable", total)
	}
}

func TestScopeProject(t *testing.T) {
	s := snap(issue(1, "started", inProject("Rig")))
	s.Projects = []proto.Project{{ID: "pr-a", Name: "app", LinearProject: "lp-rig"}, {ID: "pr-b", Name: "bare"}}
	s.Sessions = []sessionRef{
		{ID: "s1", Project: "pr-a", Panes: []string{"p-a"}},
		{ID: "s2", Project: "pr-b", Panes: []string{"p-b"}},
	}
	if name, linked, scoped := scopeProject(s, "p-a", scopeProjectName); name != "Rig" || !linked || !scoped {
		t.Fatalf("linked project: %q %v %v", name, linked, scoped)
	}
	if _, linked, scoped := scopeProject(s, "p-b", scopeProjectName); linked || !scoped {
		t.Fatalf("an unlinked project must ask: linked=%v scoped=%v", linked, scoped)
	}
	if _, linked, scoped := scopeProject(s, "p-b", scopeAll); !linked || scoped {
		t.Fatalf("scope all never asks")
	}
	if _, linked, scoped := scopeProject(s, "p-unknown", scopeProjectName); !linked || scoped {
		t.Fatalf("a pane without a session shows everything")
	}
}

func TestFingerprintIgnoresTicks(t *testing.T) {
	st := stateOfSnap(snap(issue(1, "started", withSession("s1", "work"))), session("s1", "pr-a", "p1"))
	a := fingerprint(st)
	st.Linear.Issues[0].Progress = 40
	st.Sessions[0].Activity, st.Sessions[0].Since = 123, 456
	st.Sessions[0].Panes[0].Activity, st.Sessions[0].Panes[0].Target = 999, `{"plugin":"linear"}`
	if fingerprint(st) != a {
		t.Fatal("a tick of a terminal, a progress and a pane target changed the fingerprint")
	}
	st.Linear.Issues[0].State = "In Review"
	if fingerprint(st) == a {
		t.Fatal("a change of state did not change the fingerprint")
	}
	st.Linear.Issues[0].State = "In Progress"
	st.Sessions[0].Panes[0].TaskID = "BRU-1"
	if fingerprint(st) == a {
		t.Fatal("a link to a task did not change the fingerprint")
	}
}

// TestFingerprintCoversEveryIssueField sets each field of an issue in turn:
// all of them but Progress, which ticks, must change the fingerprint, so that
// a field added to the issue cannot be left out of it.
func TestFingerprintCoversEveryIssueField(t *testing.T) {
	st := stateOfSnap(snap(issue(1, "started")))
	base := fingerprint(st)
	typ := reflect.TypeOf(proto.LinearIssue{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		st.Linear.Issues[0] = issue(1, "started")
		v := reflect.ValueOf(&st.Linear.Issues[0]).Elem().Field(i)
		switch v.Kind() {
		case reflect.String:
			v.SetString(v.String() + "x")
		case reflect.Int, reflect.Int64:
			v.SetInt(v.Int() + 7)
		case reflect.Slice:
			if f.Name == "Labels" {
				v.Set(reflect.ValueOf([]string{"bug"}))
			} else {
				v.Set(reflect.ValueOf([]proto.LinearRelation{{Type: "blocks", Identifier: "BRU-9"}}))
			}
		default:
			t.Fatalf("field %s: kind %s is not handled by the test", f.Name, v.Kind())
		}
		changed := fingerprint(st) != base
		if f.Name == "Progress" && changed {
			t.Errorf("Progress ticks and must not change the fingerprint")
		}
		if f.Name != "Progress" && !changed {
			t.Errorf("a change of Issue.%s does not change the fingerprint", f.Name)
		}
	}
	st.Linear.Issues[0] = issue(1, "started")
	st.Linear.LabelColors = map[string]string{"bug": "#E5484D"}
	if fingerprint(st) == base {
		t.Error("label colors do not change the fingerprint")
	}
}
