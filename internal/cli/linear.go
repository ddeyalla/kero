package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"kero/internal/client"
	"kero/internal/linear"
	"kero/internal/proto"
)

// linearCmd is the kero linear commands: the workspace the app shows,
// and the issue a session works on.
//
//	kero linear status                 what the daemon knows of the workspace
//	kero linear board [--project P]    the issues, as a table
//	kero linear issue BRU-25           one issue, with its comments
//	kero linear set-state BRU-25 Done  move an issue
//	kero linear comment BRU-25 "text"  comment on an issue
//	kero linear auth                   keep the workspace's key
func linearCmd(args []string) error {
	if len(args) == 0 {
		return usageErr("kero linear status|board|issue|set-state|comment|auth")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "status":
		return linearStatus()
	case "board":
		return linearBoard(rest)
	case "issue":
		return linearIssue(rest)
	case "set-state":
		return linearWrite(rest, "set-state")
	case "comment":
		return linearWrite(rest, "comment")
	case "auth":
		return linearAuth(rest)
	}
	return usageErr("kero linear status|board|issue|set-state|comment|auth")
}

// linearStatus says what the daemon knows of the workspace: whether it
// read it, and when.
func linearStatus() error {
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	var st proto.LinearState
	if err := c.Call("linear.state", nil, &st); err != nil {
		return err
	}
	fmt.Printf("workspace: %s\n", workspaceName(&st))
	switch st.Status {
	case proto.LinearOK:
		fmt.Printf("read at:   %s\n", time.UnixMilli(st.FetchedAt).Format("Mon Jan 2 15:04"))
		fmt.Printf("projects:  %d\n", len(st.Projects))
		fmt.Printf("issues:    %d\n", len(st.Issues))
		if st.Message != "" {
			fmt.Printf("note:      %s\n", st.Message)
		}
	case proto.LinearNoToken:
		fmt.Println("no API key: set one with `kero linear auth --stdin`, or in LINEAR_API_KEY")
	case proto.LinearAuthFailed:
		fmt.Println("the API key was refused by Linear")
	default:
		fmt.Printf("unread:    %s\n", st.Message)
	}
	return nil
}

func workspaceName(st *proto.LinearState) string {
	if st.TeamName != "" {
		return st.TeamName
	}
	if st.Team != "" {
		return st.Team
	}
	return "not read"
}

// linearBoard lists the issues of the workspace, as a table.
func linearBoard(args []string) error {
	f, err := parse(args, map[string]string{"project": "project", "milestone": "milestone", "state": "state"}, map[string]string{"json": "json"})
	if err != nil {
		return err
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	var st proto.LinearState
	if err := c.Call("linear.state", nil, &st); err != nil {
		return err
	}
	if st.Status != proto.LinearOK {
		return fmt.Errorf("the workspace was not read: %s", st.Status)
	}
	issues := filterIssues(st.Issues, f.vals["project"], f.vals["milestone"], f.vals["state"])
	if f.set["json"] {
		return jsonOut(issues)
	}
	if len(issues) == 0 {
		fmt.Println("no issues")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ISSUE\tSTATE\tTASK\tMILESTONE\tTITLE")
	for _, i := range issues {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", i.Identifier, i.State, i.Project, i.Milestone, i.Title)
	}
	return w.Flush()
}

// filterIssues keeps the issues that match what was asked for: a project,
// a milestone, a state, or nothing at all.
func filterIssues(issues []proto.LinearIssue, project, milestone, state string) []proto.LinearIssue {
	out := make([]proto.LinearIssue, 0, len(issues))
	for _, i := range issues {
		if project != "" && !strings.EqualFold(i.Project, project) {
			continue
		}
		if milestone != "" && !strings.EqualFold(i.Milestone, milestone) {
			continue
		}
		if state != "" && !strings.EqualFold(i.State, state) {
			continue
		}
		out = append(out, i)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].Identifier < out[j].Identifier
	})
	return out
}

// linearIssue shows one issue, with its comments.
func linearIssue(args []string) error {
	f, err := parse(args, nil, map[string]string{"json": "json"})
	if err != nil {
		return err
	}
	if len(f.pos) < 1 {
		return usageErr("kero linear issue <BRU-25>")
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	var d proto.LinearIssueDetail
	if err := c.Call("linear.issue", proto.LinearParams{Issue: f.pos[0]}, &d); err != nil {
		return err
	}
	if f.set["json"] {
		return jsonOut(d)
	}
	fmt.Printf("%s  %s\n", d.Identifier, d.Title)
	fmt.Printf("state: %s", d.State)
	if d.Project != "" {
		fmt.Printf("   project: %s", d.Project)
	}
	if d.Milestone != "" {
		fmt.Printf("   milestone: %s", d.Milestone)
	}
	if d.Session != "" {
		fmt.Printf("   session: %s", d.Session)
	}
	if d.ResumeID != "" {
		fmt.Printf("   resume: %s", d.ResumeID)
	}
	fmt.Println()
	if d.Description != "" {
		fmt.Printf("\n%s\n", strings.TrimSpace(d.Description))
	}
	if len(d.Comments) > 0 {
		fmt.Println("\ncomments:")
		for _, cm := range d.Comments {
			when := time.UnixMilli(cm.CreatedAt).Format("Mon Jan 2 15:04")
			fmt.Printf("\n  %s · %s\n", cm.Author, when)
			for _, line := range strings.Split(strings.TrimSpace(cm.Body), "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
	}
	return nil
}

// linearWrite moves an issue, or comments on it.
func linearWrite(args []string, what string) error {
	if len(args) < 2 {
		return usageErr("kero linear %s <BRU-25> <text>", what)
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	p := proto.LinearParams{Issue: args[0]}
	if what == "set-state" {
		p.State = args[1]
	} else {
		p.Body = strings.Join(args[1:], " ")
	}
	method := "linear.comment"
	if what == "set-state" {
		method = "linear.setState"
	}
	if err := c.Call(method, p, nil); err != nil {
		return err
	}
	if what == "set-state" {
		fmt.Printf("%s is now %s\n", args[0], args[1])
	} else {
		fmt.Printf("commented on %s\n", args[0])
	}
	return nil
}

// linearAuth keeps the workspace's key, in the app's own private data
// directory, where the daemon reads it.
func linearAuth(args []string) error {
	if len(args) == 0 {
		fmt.Printf("key: %s\n", maskKey(linear.Key()))
		fmt.Println("save from stdin with `kero linear auth --stdin`, remove with `--remove`")
		return nil
	}
	if len(args) != 1 || (args[0] != "--stdin" && args[0] != "--remove") {
		return usageErr("kero linear auth --stdin|--remove (keys are never accepted in process arguments)")
	}
	key := ""
	if args[0] == "--stdin" {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 8194))
		if err != nil {
			return err
		}
		key = strings.TrimSpace(string(b))
		if key == "" || len(key) > 8192 {
			return fmt.Errorf("provide a nonempty Linear key of at most 8192 bytes")
		}
	}
	if err := linear.SaveKey(key); err != nil {
		return err
	}
	if key == "" {
		fmt.Println("the key was removed")
		return nil
	}
	fmt.Println("the key was kept")
	return nil
}

// linkCmd binds a session to a Linear issue, or unbinds it.
//
//	kero link <session> <BRU-25>   the session works on the issue
//	kero link <session>             the session works on no issue
func linkCmd(args []string) error {
	if len(args) < 1 {
		return usageErr("kero link <session> [BRU-25]")
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	task := ""
	if len(args) > 1 {
		task = strings.Join(args[1:], " ")
	}
	if err := c.Call("session.link", proto.LinkParams{Session: args[0], Task: task}, nil); err != nil {
		return err
	}
	if task == "" {
		fmt.Printf("%s is linked to no task\n", args[0])
		return nil
	}
	fmt.Printf("%s is linked to %s\n", args[0], task)
	return nil
}

// maskKey shows a key as what it is not: the first few characters and
// the last, and nothing else.
func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(none)"
	}
	if len(key) <= 12 {
		return strings.Repeat("•", len(key))
	}
	return "(configured)"
}

// jsonOut prints what a command read, as JSON.
func jsonOut(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
