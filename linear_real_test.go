package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/linear"
	"kero/internal/proto"
)

// The real workspace, rendered, for a look. It needs a key, and skips
// without one: LINEAR_API_KEY, or the key the app keeps.
// projectName is the issue's project's name.
func projectName(i *linear.Issue) string {
	if i.Project == nil {
		return ""
	}
	return i.Project.Name
}

// milestoneName is the issue's milestone's name.
func milestoneName(i *linear.Issue) string {
	if i.Milestone == nil {
		return ""
	}
	return i.Milestone.Name
}

func TestLinearRealShot(t *testing.T) {
	if os.Getenv("SHIBU_LINEAR_LIVE_TEST") != "1" {
		t.Skip("opt-in read-only Linear integration test")
	}
	key := linear.Key()
	if key == "" {
		t.Skip("no Linear key")
	}
	c := linear.New(key)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	team := os.Getenv("LINEAR_TEAM")
	if team == "" {
		teams, err := c.Teams(ctx)
		if err != nil || len(teams) == 0 {
			t.Fatal("could not read a Linear team")
		}
		team = teams[0].Key
	}
	snap, err := c.Board(ctx, team)
	if err != nil {
		t.Fatalf("the workspace was not read: %v", err)
	}
	a := testApp(t)
	a.show = "linear"
	a.selected = ""
	lin := &proto.LinearState{Status: proto.LinearOK, Team: snap.Team.Key, TeamName: snap.Team.Name}
	for _, s := range snap.Team.States {
		lin.States = append(lin.States, proto.LinearWorkflowState{ID: s.ID, Name: s.Name, Type: s.Type, Position: s.Position})
	}
	for _, p := range snap.Projects {
		lp := proto.LinearProject{ID: p.ID, Name: p.Name, Status: p.Status.Type, Progress: p.Progress}
		for _, m := range p.Milestones {
			lp.Milestones = append(lp.Milestones, proto.LinearMilestone{ID: m.ID, Name: m.Name, Status: m.Status, Progress: m.Progress})
		}
		lin.Projects = append(lin.Projects, lp)
	}

	for _, i := range snap.Issues {
		li := proto.LinearIssue{
			ID: i.ID, Identifier: i.Identifier, Title: i.Title, URL: i.URL,
			State: i.State.Name, StateType: i.State.Type,
			CreatedAt: i.CreatedAt.UnixMilli(), UpdatedAt: i.UpdatedAt.UnixMilli(),
		}
		if i.Project != nil {
			li.Project = i.Project.Name
		}
		if i.Milestone != nil {
			li.Milestone = i.Milestone.Name
		}
		for _, r := range i.Relations {
			switch r.Type {
			case "blockedBy":
				li.Blocked++
			case "blocks":
				li.Blocking++
			}
		}
		lin.Issues = append(lin.Issues, li)
	}
	a.st.Linear = lin
	// The issues of the project, as the user would filter to them.
	if len(snap.Projects) > 0 {
		a.lin.project = snap.Projects[0].Name
	}
	tt := ui.NewTester(a.view, 1400, 860)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	shot(t, tt, "linear-real-board")
	// And one of them, open.
	if len(snap.Issues) > 0 {
		for _, i := range snap.Issues {
			if i.Identifier != liveLinearIssue(snap) {
				continue
			}
			d, err := c.Issue(ctx, i.Identifier)
			if err != nil {
				t.Fatal(err)
			}
			detail := &proto.LinearIssueDetail{
				LinearIssue: proto.LinearIssue{
					ID: d.ID, Identifier: d.Identifier, Title: d.Title, URL: d.URL,
					State: d.State.Name, StateType: d.State.Type,
					Project:   projectName(d),
					Milestone: milestoneName(d),
				},
				Description: d.Description,
			}
			for _, cm := range d.Comments {
				detail.Comments = append(detail.Comments, proto.LinearComment{
					ID: cm.ID, Body: cm.Body, Author: linearCommentAuthor(cm), CreatedAt: cm.CreatedAt.UnixMilli(),
				})
			}
			for _, at := range d.Attachments {
				detail.Attachments = append(detail.Attachments, proto.LinearAttachment{ID: at.ID, Title: at.Title, URL: at.URL})
			}
			for _, r := range d.Relations {
				detail.Relations = append(detail.Relations, proto.LinearRelation{
					Type: r.Type, Identifier: r.Issue.Identifier, Title: r.Issue.Title, State: r.Issue.State.Name,
				})
			}
			a.lin.open = d.ID
			a.lin.detail = detail
			break
		}
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		shot(t, tt, "linear-real-detail")
	}
	if os.Getenv("KERO_SHOTS") == "" {
		t.Log("no $KERO_SHOTS: the pictures were not written")
	}
}

func linearCommentAuthor(cm linear.Comment) string {
	if cm.User != nil {
		return cm.User.Name
	}
	return "Former user"
}

func liveLinearIssue(snap *linear.Snapshot) string {
	if id := os.Getenv("SHIBU_LINEAR_TEST_ISSUE"); id != "" {
		return id
	}
	if len(snap.Issues) > 0 {
		return snap.Issues[0].Identifier
	}
	return ""
}
