// Package linear is a client for Linear's GraphQL API: the projects,
// milestones and issues of a workspace, and the comments on them.
//
// Linear has one GraphQL endpoint and no REST API. Everything is a query
// or a mutation posted to it, with the workspace's API key in the
// Authorization header.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Endpoint is Linear's GraphQL API.
const Endpoint = "https://api.linear.app/graphql"

// Statuses of a client: what the workspace says of it.
const (
	StatusOK          = "ok"
	StatusNoToken     = "no_token"
	StatusAuthFailed  = "auth_failed"
	StatusRateLimited = "rate_limited"
	StatusUnreachable = "unreachable"
	StatusDisabled    = "disabled"
)

// list is a GraphQL connection: the items under "nodes". Linear puts
// every collection in one, and the client reads them as the plain lists
// they are.
type list[T any] []T

func (l *list[T]) UnmarshalJSON(b []byte) error {
	var c struct {
		Nodes []T `json:"nodes"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	*l = c.Nodes
	return nil
}

// priority is an issue's priority: 0 none, 1 urgent, 2 high, 3 medium,
// 4 low. The API sends it as a number, but a workspace where it is an
// enum sends a string; both are read.
type priority int

func (p *priority) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*p = priority(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		*p = 0
		return nil
	}
	*p = priority(n)
	return nil
}

// State is a workflow state of a team: Todo, In Progress, Done and the
// rest. Type is what the state means, not what it is called: a team's
// states are named, and renamed, but their types do not change.
type State struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Position float64 `json:"position"`
}

// User is a person of the workspace.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Label is a label on an issue.
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Milestone is a project milestone: a phase of a project, as M1
// Foundation is a phase of Rig. Its Status is a milestone's own —
// unstarted, next, done — and not a workflow state.
type Milestone struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	TargetDate string  `json:"targetDate"`
}

// ProjectStatus is where a project stands: backlog, planned, started,
// paused, completed or canceled.
type ProjectStatus struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// Project is a project of the workspace.
type Project struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	SlugID     string          `json:"slugId"`
	Status     ProjectStatus   `json:"status"`
	Progress   float64         `json:"progress"`
	Milestones list[Milestone] `json:"projectMilestones"`
}

// Relation is a relation between two issues: one blocks the other, or
// they are related, or one duplicates the other.
type Relation struct {
	Type  string `json:"type"`
	Issue Issue  `json:"relatedIssue"`
}

// Attachment is a file or a link on an issue: a task prompt, a document.
type Attachment struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Subtitle string `json:"subtitle"`
	Source   string `json:"sourceType"`
}

// Comment is a comment on an issue: what an agent wrote when it finished,
// or what a person wrote.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	User      *User     `json:"user"`
}

// Issue is an issue of a team.
type Issue struct {
	ID               string           `json:"id"`
	Identifier       string           `json:"identifier"`
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	URL              string           `json:"url"`
	BranchName       string           `json:"branchName"`
	Priority         priority         `json:"priority"`
	State            State            `json:"state"`
	Project          *Project         `json:"project"`
	Milestone        *Milestone       `json:"projectMilestone"`
	Assignee         *User            `json:"assignee"`
	Creator          *User            `json:"creator"`
	Labels           list[Label]      `json:"labels"`
	Relations        list[Relation]   `json:"relations"`
	InverseRelations list[Relation]   `json:"inverseRelations"`
	Attachments      list[Attachment] `json:"attachments"`
	Comments         list[Comment]    `json:"comments"`
	Children         list[Issue]      `json:"children"`
	Parent           *Issue           `json:"parent"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	CompletedAt      time.Time        `json:"completedAt"`
}

// Team is a team of the workspace, with the states its issues are in.
type Team struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Name   string      `json:"name"`
	States list[State] `json:"states"`
}

// Snapshot is the workspace as it was when it was read: the team, its
// projects, and the issues of every project.
type Snapshot struct {
	Team     Team          `json:"team"`
	Projects list[Project] `json:"projects"`
	Issues   list[Issue]   `json:"issues"`
	Read     time.Time     `json:"read"`
}

// Client reads and writes a Linear workspace.
type Client struct {
	key      string
	endpoint string
	http     *http.Client
}

// New returns a client for the workspace the key opens.
func New(key string) *Client {
	return &Client{
		key:      strings.TrimSpace(key),
		endpoint: Endpoint,
		http:     &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

// WithEndpoint returns the client talking to another endpoint, which the
// tests use.
func (c *Client) WithEndpoint(endpoint string) *Client {
	c.endpoint = endpoint
	return c
}

// ErrNoToken says the client has no key, and so cannot read the
// workspace.
var ErrNoToken = fmt.Errorf("no Linear API key")

// apiError is a failure of a request, with what Linear said of it.
type apiError struct {
	code    string
	message string
	retryAt time.Time
}

func (e apiError) Error() string { return e.message }

// Code is Linear's code for the failure: AUTHENTICATION_ERROR,
// RATELIMITED, and so on.
func (e apiError) Code() string { return e.code }

// RetryAt is the earliest safe retry after Linear exhausts a rate budget.
func RetryAt(err error) time.Time {
	var e apiError
	if errors.As(err, &e) {
		return e.retryAt
	}
	return time.Time{}
}

// Do runs one GraphQL request, and puts its "data" in out.
func (c *Client) Do(ctx context.Context, query string, vars map[string]any, out any) error {
	if c.key == "" {
		return ErrNoToken
	}
	if len(c.key) > 8192 || strings.ContainsAny(c.key, "\x00\r\n") {
		return errors.New("invalid Linear API key format")
	}
	body := map[string]any{"query": query}
	if vars != nil {
		body["variables"] = vars
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return apiError{code: "UNREACHABLE", message: err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 8<<20 {
		return fmt.Errorf("Linear response exceeds 8 MiB")
	}
	retryAt := time.Now().Add(time.Minute)
	for _, resource := range []string{"Requests", "Complexity"} {
		if ms, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-"+resource+"-Reset"), 10, 64); err == nil {
			if reset := time.UnixMilli(ms); reset.After(retryAt) {
				retryAt = reset
			}
		}
	}
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		if reset := time.Now().Add(time.Duration(seconds) * time.Second); reset.After(retryAt) {
			retryAt = reset
		}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return apiError{code: "AUTHENTICATION_ERROR", message: "Linear refused the API key"}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return apiError{code: "RATELIMITED", message: "Linear rate limit reached", retryAt: retryAt}
	}
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("Linear returned an invalid response (HTTP %d)", resp.StatusCode)
	}
	if len(res.Errors) > 0 {
		// A rate limit arrives as HTTP 400 with the code in the body.
		e := res.Errors[0]
		code := e.Extensions.Code
		switch code {
		case "AUTHENTICATION_ERROR", "AUTHENTICATION_REQUIRED", "RATELIMITED", "INVALID_INPUT", "INPUT_ERROR", "FORBIDDEN", "INTERNAL_SERVER_ERROR", "GRAPHQL_VALIDATION_FAILED":
		default:
			code = fmt.Sprintf("HTTP_%d", resp.StatusCode)
		}
		// Do not publish remote response bodies or echoed credentials into
		// the daemon log, state.json, terminal or GUI.
		return apiError{code: code, message: "Linear request failed (" + code + ")", retryAt: retryAt}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return apiError{code: fmt.Sprintf("HTTP_%d", resp.StatusCode), message: "Linear: " + resp.Status}
	}
	if out == nil {
		return nil
	}
	if len(res.Data) == 0 || string(res.Data) == "null" {
		return fmt.Errorf("Linear returned no data")
	}
	if err := json.Unmarshal(res.Data, out); err != nil {
		return errors.New("Linear response could not be decoded")
	}
	return nil
}

// Status is what a failure means for the workspace: whether to try again,
// to say the key is wrong, or to wait.
func Status(err error) string {
	switch {
	case err == nil:
		return StatusOK
	case errors.Is(err, ErrNoToken):
		return StatusNoToken
	}
	var e apiError
	if errors.As(err, &e) {
		switch e.code {
		case "AUTHENTICATION_ERROR", "AUTHENTICATION_REQUIRED":
			return StatusAuthFailed
		case "RATELIMITED":
			return StatusRateLimited
		case "UNREACHABLE", "HTTP_502", "HTTP_503", "HTTP_504":
			return StatusUnreachable
		}
	}
	return StatusUnreachable
}
