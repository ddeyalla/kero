package cli

import (
	"regexp"
	"strings"
	"testing"

	"kero/internal/proto"
)

func taskState() *proto.State {
	return &proto.State{
		Projects: []proto.Project{{ID: "p1", Name: "shibu", LinearProject: "lp1"}, {ID: "p2", Name: "other"}},
		Sessions: []proto.Session{
			{ID: "s1", Project: "p1", Panes: []proto.Pane{{ID: "a"}}},
			{ID: "s2", Project: "p2", Panes: []proto.Pane{{ID: "b", TaskID: "uuid-27"}}},
		},
		Linear: &proto.LinearState{
			Team:     "BRU",
			States:   []proto.LinearWorkflowState{{Name: "Todo", Type: "unstarted", Position: 1}, {Name: "In Review", Type: "started", Position: 3}, {Name: "In Progress", Type: "started", Position: 2}, {Name: "Done", Type: "completed", Position: 4}},
			Projects: []proto.LinearProject{{ID: "lp1", Name: "Shibu Code"}},
			Issues: []proto.LinearIssue{
				{ID: "uuid-25", Identifier: "BRU-25", Title: "Todo one", State: "Todo", StateType: "unstarted", Project: "Shibu Code"},
				{ID: "uuid-26", Identifier: "BRU-26", Title: "Done one", State: "Done", StateType: "completed", Project: "Shibu Code"},
				{ID: "uuid-27", Identifier: "BRU-27", Title: "Review one", State: "In Review", StateType: "started", Project: "Shibu Code"},
				{ID: "uuid-28", Identifier: "BRU-28", Title: "Elsewhere", State: "In Progress", StateType: "started", Project: "Rig"},
			},
		},
	}
}

// An agent in a session of a linked project is told where its tasks are
// and how to start on one; in a linked session, which issue it is on.
func TestTaskContext(t *testing.T) {
	st := taskState()
	got := taskContext(st, st.Sessions[0])
	for _, want := range []string{`the Linear project "Shibu Code"`, "kero task start <issue-id> --session s1", "BRU-<number>", "ask the user", "don't create one"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	// No real-looking issue an agent could run the commands on as they
	// stand.
	if regexp.MustCompile(`BRU-[0-9]`).MatchString(got) {
		t.Errorf("an issue id in the instructions: %q", got)
	}
	if regexp.MustCompile(`task start [A-Z]+-[0-9]|set-state [A-Z]+-[0-9]|--id [A-Z]+-[0-9]`).MatchString(skillGuide) {
		t.Error("the skill's commands name a real-looking issue")
	}
	got = taskContext(st, st.Sessions[1])
	if !strings.Contains(got, "BRU-27") || !strings.Contains(got, "leave the link alone") || strings.Contains(got, "task start") {
		t.Errorf("linked session: %q", got)
	}
	st.Linear = nil
	if got := taskContext(st, st.Sessions[0]); got != "" {
		t.Errorf("no workspace, yet %q", got)
	}
}

func TestTaskListAndFirstStarted(t *testing.T) {
	st := taskState()
	if got := sessionLinearProject(st, "s1"); got != "Shibu Code" {
		t.Fatalf("project: %q", got)
	}
	var ids []string
	for _, i := range tasksOf(st.Linear, "Shibu Code", false) {
		ids = append(ids, i.Identifier)
	}
	if strings.Join(ids, " ") != "BRU-27 BRU-25" {
		t.Fatalf("open issues of the project, under way first: %v", ids)
	}
	if n := len(tasksOf(st.Linear, "", true)); n != 4 {
		t.Fatalf("all: %d", n)
	}
	if s, ok := firstStarted(st.Linear.States); !ok || s != "In Progress" {
		t.Fatalf("first started: %q", s)
	}
}
