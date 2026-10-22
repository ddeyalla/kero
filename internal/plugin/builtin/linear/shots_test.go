package linear

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kero/internal/proto"
)

// TestWriteParityTrees writes the trees the parity shots draw, as JSON, into
// the directory LINEAR_TREE_DIR names. The app's tests read them and draw
// them in the tester, so that a shot shows what the plugin really sends.
func TestWriteParityTrees(t *testing.T) {
	dir := os.Getenv("LINEAR_TREE_DIR")
	if dir == "" {
		t.Skip("LINEAR_TREE_DIR is not set")
	}
	s := snap(
		issue(1, "started", assignedTo("me", "Me Myself"), inProject("Rig"), withSession("s1", "bru-1"), func(i *proto.LinearIssue) {
			i.Priority = 2
			i.Blocked, i.Blocking, i.Children = 1, 2, 3
			i.Milestone = "M1 Foundation"
			i.Labels = []string{"bug", "ui", "web"}
		}),
		issue(2, "started", inProject("Rig"), func(i *proto.LinearIssue) { i.Priority = 1; i.Labels = []string{"design"} }),
		issue(3, "completed", inProject("Rig")),
		issue(4, "unstarted", inProject("Web"), func(i *proto.LinearIssue) { i.Priority = 3; i.Milestone = "M2 Launch" }),
		issue(5, "backlog", inProject("Web"), func(i *proto.LinearIssue) { i.Labels = []string{"bug"} }),
	)
	for i := range s.States {
		s.States[i].Color = map[string]string{
			"Triage": "#B7B8BC", "Backlog": "#BEC2C8", "Todo": "#E2E2E2", "In Progress": "#F2C94C",
			"In Review": "#4CB782", "Done": "#5E6AD2", "Canceled": "#95A2B3",
		}[s.States[i].Name]
	}
	colors := map[string]string{"bug": "#E5484D", "ui": "#2F6FED", "design": "#8B5CF6", "web": "#10A37F"}
	write := func(name string, in viewInput) {
		in.labelColors = colors
		tree := buildTree(in)
		if err := tree.ValidateTree(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := json.MarshalIndent(tree, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	list := input(s)
	list.sel = "id-2"
	list.width = 520
	write("list", list)

	board := input(s)
	board.st.Mode = modeBoard

	board.sel = "id-1"
	board.width = 560
	write("board", board)

	wide := input(s)
	wide.st.Open = "id-1"
	wide.width = 1000
	d := fullDetail()
	d.LinearIssue = s.Issues[0]
	d.LabelColors = []proto.LinearLabel{{Name: "bug", Color: "#E5484D"}, {Name: "ui", Color: "#2F6FED"}, {Name: "web", Color: "#10A37F"}}
	wide.detail = d
	write("wide", wide)
}
