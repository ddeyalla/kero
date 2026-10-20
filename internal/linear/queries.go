package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// issueFields are the fields of an issue the board shows. The board reads
// every issue of the team, so it asks for little: the detail view reads
// the rest, one issue at a time.
const issueFields = `
        id identifier title url
        createdAt updatedAt completedAt
        state { id name type position }
        project { id name }
        projectMilestone { id name }
        assignee { id name }
        labels { nodes { id name color } }
        relations(first: 10) { nodes { type relatedIssue { id identifier title state { id name type } } } }
        inverseRelations(first: 10) { nodes { type relatedIssue: issue { id identifier title state { id name type } } } }`

// boardQuery reads a team whole: its states, its projects with their
// milestones, and its issues.
const boardQuery = `query Board($team: String!) {
  team(id: $team) {
    id key name
    states(first: 250) { nodes { id name type position } }
  }
}`

const projectsQuery = `query Projects($team: String!, $after: String) {
  team(id: $team) {
    projects(first: 50, after: $after) {
      nodes { id name slugId status { type name } progress }
      pageInfo { hasNextPage endCursor }
    }
  }
}`
const milestonesQuery = `query Milestones($id: String!, $after: String) {
  project(id: $id) {
    projectMilestones(first: 100, after: $after) {
      nodes { id name status progress targetDate }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

const issuesQuery = `query Issues($team: String!, $after: String) {
  team(id: $team) {
    issues(first: 100, after: $after, orderBy: updatedAt) {
      nodes {` + issueFields + `}
      pageInfo { hasNextPage endCursor }
    }
  }
}`
const deltaQuery = `query Delta($team: String!, $since: DateTimeOrDuration!, $after: String) {
  team(id: $team) {
    issues(first: 100, after: $after, orderBy: updatedAt, filter: { updatedAt: { gte: $since } }) {
      nodes {` + issueFields + `}
      pageInfo { hasNextPage endCursor }
    }
  }
}`

// issueQuery reads one issue whole: its description, its relations, the
// files attached to it, and the comments on it.
const issueQuery = `query Issue($id: String!) {
  issue(id: $id) {
    id identifier title description url branchName priority
    createdAt updatedAt completedAt
    state { id name type position }
    project { id name }
    projectMilestone { id name }
    assignee { id name }
    creator { id name }
    labels { nodes { id name color } }
    attachments(first: 50) { nodes { id title url subtitle sourceType } pageInfo { hasNextPage endCursor } }
    comments(first: 50) { nodes { id body createdAt user { id name } } pageInfo { hasNextPage endCursor } }
    relations(first: 50) { nodes { type relatedIssue { id identifier title state { id name type } } } pageInfo { hasNextPage endCursor } }
    inverseRelations(first: 50) { nodes { type relatedIssue: issue { id identifier title state { id name type } } } pageInfo { hasNextPage endCursor } }
    children(first: 50) { nodes { id identifier title state { id name type } } pageInfo { hasNextPage endCursor } }
    parent { id identifier title }
  }
}`

// setStateMutation moves an issue to a workflow state.
const setStateMutation = `mutation SetState($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) { success issue { id identifier state { id name } } }
}`

// commentMutation writes a comment on an issue.
const commentMutation = `mutation Comment($id: String!, $body: String!) {
  commentCreate(input: { issueId: $id, body: $body }) { success comment { id createdAt } }
}`

// teamsQuery reads the teams of the workspace.
const teamsQuery = `query Teams {
  teams(first: 20) { nodes { id key name issueCount } }
}`

// Teams reads the teams of the workspace, which is how a client with no
// team named finds the one to read.
func (c *Client) Teams(ctx context.Context) ([]Team, error) {
	var res struct {
		Teams list[Team] `json:"teams"`
	}
	if err := c.Do(ctx, teamsQuery, nil, &res); err != nil {
		return nil, err
	}
	return res.Teams, nil
}

type connection[T any] struct {
	Nodes    []T `json:"nodes"`
	PageInfo struct {
		HasNext bool   `json:"hasNextPage"`
		Cursor  string `json:"endCursor"`
	} `json:"pageInfo"`
}

// pages fails closed on broken cursors instead of looping or silently
// publishing an incomplete board. Requests stay within the caller's deadline.
func pages[T any](ctx context.Context, read func(context.Context, string) (connection[T], error)) ([]T, error) {
	var all []T
	cursor := ""
	seen := map[string]bool{}
	for {
		page, err := read(ctx, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Nodes...)
		if !page.PageInfo.HasNext {
			return all, nil
		}
		cursor = page.PageInfo.Cursor
		if cursor == "" || seen[cursor] {
			return nil, fmt.Errorf("Linear returned an invalid pagination cursor")
		}
		seen[cursor] = true
	}
}

// Board reads every issue and project of the selected team. The watermark
// precedes the first request, so edits during pagination are read next time.
func (c *Client) Board(ctx context.Context, team string) (*Snapshot, error) {
	started := time.Now()
	var meta struct {
		Team *Team `json:"team"`
	}
	if err := c.Do(ctx, boardQuery, map[string]any{"team": team}, &meta); err != nil {
		return nil, err
	}
	if meta.Team == nil {
		return nil, fmt.Errorf("Linear team was not found")
	}
	projects, err := pages(ctx, func(ctx context.Context, after string) (connection[Project], error) {
		var res struct {
			Team struct {
				Projects connection[Project] `json:"projects"`
			} `json:"team"`
		}
		err := c.Do(ctx, projectsQuery, map[string]any{"team": team, "after": nullableCursor(after)}, &res)
		return res.Team.Projects, err
	})
	if err != nil {
		return nil, err
	}
	for n := range projects {
		milestones, err := pages(ctx, func(ctx context.Context, after string) (connection[Milestone], error) {
			var res struct {
				Project struct {
					Milestones connection[Milestone] `json:"projectMilestones"`
				} `json:"project"`
			}
			err := c.Do(ctx, milestonesQuery, map[string]any{"id": projects[n].ID, "after": nullableCursor(after)}, &res)
			return res.Project.Milestones, err
		})
		if err != nil {
			return nil, err
		}
		projects[n].Milestones = milestones
	}
	issues, err := c.readIssues(ctx, issuesQuery, map[string]any{"team": team})
	if err != nil {
		return nil, err
	}
	return &Snapshot{Team: *meta.Team, Projects: projects, Issues: issues, Read: started}, nil
}

func nullableCursor(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (c *Client) readIssues(ctx context.Context, query string, vars map[string]any) ([]Issue, error) {
	return pages(ctx, func(ctx context.Context, after string) (connection[Issue], error) {
		var res struct {
			Team struct {
				Issues connection[Issue] `json:"issues"`
			} `json:"team"`
		}
		vars["after"] = nullableCursor(after)
		err := c.Do(ctx, query, vars, &res)
		return res.Team.Issues, err
	})
}

func (c *Client) Delta(ctx context.Context, team string, since time.Time) ([]Issue, error) {
	return c.readIssues(ctx, deltaQuery, map[string]any{"team": team, "since": since.UTC().Format("2006-01-02T15:04:05.000Z")})
}

// Issue reads one issue whole.
func (c *Client) Issue(ctx context.Context, id string) (*Issue, error) {
	var res struct {
		Issue json.RawMessage `json:"issue"`
	}
	if err := c.Do(ctx, issueQuery, map[string]any{"id": id}, &res); err != nil {
		return nil, err
	}
	if len(res.Issue) == 0 || string(res.Issue) == "null" {
		return nil, fmt.Errorf("Linear: no issue %q", id)
	}
	var issue Issue
	if err := json.Unmarshal(res.Issue, &issue); err != nil {
		return nil, err
	}
	var detail struct {
		Comments    connection[Comment]    `json:"comments"`
		Attachments connection[Attachment] `json:"attachments"`
		Relations   connection[Relation]   `json:"relations"`
		Inverse     connection[Relation]   `json:"inverseRelations"`
		Children    connection[Issue]      `json:"children"`
	}
	if err := json.Unmarshal(res.Issue, &detail); err != nil {
		return nil, err
	}
	var err error
	issue.Comments, err = issueItems(ctx, c, id, "comments", "id body createdAt user { id name }", detail.Comments)
	if err != nil {
		return nil, err
	}
	issue.Attachments, err = issueItems(ctx, c, id, "attachments", "id title url subtitle sourceType", detail.Attachments)
	if err != nil {
		return nil, err
	}
	issue.Relations, err = issueItems(ctx, c, id, "relations", "type relatedIssue { id identifier title state { id name type } }", detail.Relations)
	if err != nil {
		return nil, err
	}
	issue.InverseRelations, err = issueItems(ctx, c, id, "inverseRelations", "type relatedIssue: issue { id identifier title state { id name type } }", detail.Inverse)
	if err != nil {
		return nil, err
	}
	issue.Children, err = issueItems(ctx, c, id, "children", "id identifier title state { id name type }", detail.Children)
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

// Field and selection are internal constants; IDs and cursors are variables.
func issueItems[T any](ctx context.Context, c *Client, id, field, selection string, first connection[T]) ([]T, error) {
	initial := true
	return pages(ctx, func(ctx context.Context, after string) (connection[T], error) {
		if initial {
			initial = false
			return first, nil
		}
		query := `query IssueItems($id: String!, $after: String) { issue(id: $id) { items: ` + field + `(first: 50, after: $after) { nodes { ` + selection + ` } pageInfo { hasNextPage endCursor } } } }`
		var res struct {
			Issue struct {
				Items connection[T] `json:"items"`
			} `json:"issue"`
		}
		err := c.Do(ctx, query, map[string]any{"id": id, "after": after}, &res)
		return res.Issue.Items, err
	})
}

// SetState moves an issue to a workflow state, by the state's id.
func (c *Client) SetState(ctx context.Context, issue, stateID string) error {
	var res struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	err := c.Do(ctx, setStateMutation, map[string]any{"id": issue, "stateId": stateID}, &res)
	if err != nil {
		return err
	}
	if !res.IssueUpdate.Success {
		return fmt.Errorf("Linear: the issue did not move to %q", stateID)
	}
	return nil
}

// Comment writes a comment on an issue.
func (c *Client) Comment(ctx context.Context, issue, body string) error {
	var res struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	err := c.Do(ctx, commentMutation, map[string]any{"id": issue, "body": body}, &res)
	if err != nil {
		return err
	}
	if !res.CommentCreate.Success {
		return fmt.Errorf("Linear: the comment was not written")
	}
	return nil
}
