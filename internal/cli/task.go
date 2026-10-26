package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"kero/internal/client"
	"kero/internal/proto"
)

// taskCmd is how an agent starts on a task of the user's tracker, which
// links its session to the issue, for any harness: a command it runs,
// not a hook it needs.
//
//	kero task start <issue-id> [--session S] [--keep-state]
//	kero task list [--all] [--json]
//	kero task                          the issue this session works on
func taskCmd(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "start":
		return taskStart(args)
	case "list", "ls":
		return taskList(args)
	case "", "show":
		return taskShow(args)
	}
	return usageErr("kero task start <issue-id> | list | show")
}

// taskStart links the session to the issue, moves the issue to the
// team's first started state unless it is under way or done, and prints
// the branch to work on.
func taskStart(args []string) error {
	f, err := parse(args, map[string]string{"session": "session", "s": "session"}, map[string]string{"keep-state": "keep"})
	if err != nil {
		return err
	}
	if len(f.pos) != 1 {
		return usageErr("kero task start <issue-id> [--session S] [--keep-state]")
	}
	sid := f.vals["session"]
	if sid == "" {
		sid = os.Getenv("KERO_SESSION")
	}
	if sid == "" {
		return usageErr("kero task start runs in a Shibu Code pane, or takes --session")
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	if err := c.Call("session.link", proto.LinkParams{Session: sid, Task: f.pos[0], Via: "task"}, nil); err != nil {
		return err
	}
	var d proto.LinearIssueDetail
	if err := c.Call("linear.issue", proto.LinearParams{Issue: f.pos[0]}, &d); err != nil {
		fmt.Printf("%s is linked to session %s\n", f.pos[0], sid)
		return nil
	}
	moved := ""
	if !f.set["keep"] && d.StateType != "started" && d.StateType != "completed" && d.StateType != "canceled" {
		var st proto.LinearState
		if c.Call("linear.state", nil, &st) == nil {
			if s, ok := firstStarted(st.States); ok && c.Call("linear.setState", proto.LinearParams{Issue: d.Identifier, State: s}, nil) == nil {
				moved = s
			}
		}
	}
	fmt.Printf("%s  %s\n", d.Identifier, d.Title)
	fmt.Printf("linked to session %s", sid)
	if moved != "" {
		fmt.Printf(", moved to %s", moved)
	} else if d.State != "" {
		fmt.Printf(", %s", d.State)
	}
	fmt.Println()
	if d.BranchName != "" {
		fmt.Printf("branch: %s\n", d.BranchName)
	}
	return nil
}

// firstStarted is the name of the team's first state of work under way:
// In Progress, as most teams call it.
func firstStarted(states []proto.LinearWorkflowState) (string, bool) {
	best := -1
	for i, s := range states {
		if s.Type == "started" && (best < 0 || s.Position < states[best].Position) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return states[best].Name, true
}

// taskList lists the issues open in the project's Linear project, the
// one the session's project is linked to, else in the whole team; with
// --all, the done ones too.
func taskList(args []string) error {
	f, err := parse(args, map[string]string{"project": "project", "p": "project"}, map[string]string{"all": "all", "json": "json"})
	if err != nil {
		return err
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	var st proto.State
	if err := c.Call("state", nil, &st); err != nil {
		return err
	}
	if st.Linear == nil || st.Linear.Status != proto.LinearOK && len(st.Linear.Issues) == 0 {
		return fmt.Errorf("the Linear workspace was not read")
	}
	project := f.vals["project"]
	if project == "" {
		project = sessionLinearProject(&st, os.Getenv("KERO_SESSION"))
	}
	issues := tasksOf(st.Linear, project, f.set["all"])
	if f.set["json"] {
		return jsonOut(issues)
	}
	if len(issues) == 0 {
		fmt.Println("no issues")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ISSUE\tSTATE\tTITLE")
	for _, i := range issues {
		fmt.Fprintf(w, "%s\t%s\t%s\n", i.Identifier, i.State, i.Title)
	}
	return w.Flush()
}

// sessionLinearProject is the name of the Linear project the session's
// project is linked to, "" none.
func sessionLinearProject(st *proto.State, sid string) string {
	for _, s := range st.Sessions {
		if s.ID != sid {
			continue
		}
		for _, p := range st.Projects {
			if p.ID == s.Project && p.LinearProject != "" {
				for _, lp := range st.Linear.Projects {
					if lp.ID == p.LinearProject {
						return lp.Name
					}
				}
			}
		}
	}
	return ""
}

// tasksOf are a Linear project's issues, by name, or the team's for "":
// the open ones, those under way first, in the order of their states.
func tasksOf(lin *proto.LinearState, project string, all bool) []proto.LinearIssue {
	pos := map[string]float64{}
	for _, s := range lin.States {
		pos[s.Name] = s.Position
	}
	weight := map[string]int{"started": 0, "unstarted": 1, "triage": 1, "backlog": 2, "completed": 3, "canceled": 4, "duplicate": 4}
	var out []proto.LinearIssue
	for _, i := range lin.Issues {
		if project != "" && !strings.EqualFold(i.Project, project) {
			continue
		}
		if !all && weight[i.StateType] >= 3 {
			continue
		}
		out = append(out, i)
	}
	slices.SortStableFunc(out, func(a, b proto.LinearIssue) int {
		if wa, wb := weight[a.StateType], weight[b.StateType]; wa != wb {
			return wa - wb
		}
		if pa, pb := pos[a.State], pos[b.State]; pa != pb {
			if pa < pb {
				return -1
			}
			return 1
		}
		return int(b.UpdatedAt - a.UpdatedAt)
	})
	return out
}

// taskShow prints the issue the session works on.
func taskShow(args []string) error {
	f, err := parse(args, map[string]string{"session": "session", "s": "session"}, nil)
	if err != nil {
		return err
	}
	sid := f.vals["session"]
	if sid == "" {
		sid = os.Getenv("KERO_SESSION")
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	var st proto.State
	if err := c.Call("state", nil, &st); err != nil {
		return err
	}
	if st.Linear != nil {
		for _, i := range st.Linear.Issues {
			if i.Session == sid && sid != "" {
				fmt.Printf("%s  %s  (%s)\n", i.Identifier, i.Title, i.State)
				return nil
			}
		}
	}
	fmt.Println("this session works on no issue: kero task start <issue-id>")
	return nil
}
