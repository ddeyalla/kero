package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// server is a Linear that answers with what the test gives it.
func server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("the request carried no Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestDoSendsTheKey(t *testing.T) {
	s := server(t, 200, `{"data":{"viewer":{"name":"dv"}}}`)
	c := New("lin_api_test").WithEndpoint(s.URL)
	var out struct {
		Viewer struct {
			Name string `json:"name"`
		} `json:"viewer"`
	}
	if err := c.Do(context.Background(), `query { viewer { name } }`, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Viewer.Name != "dv" {
		t.Fatalf("got %q", out.Viewer.Name)
	}
}

func TestDoWithoutAKey(t *testing.T) {
	c := New("  ")
	err := c.Do(context.Background(), `query { viewer { name } }`, nil, nil)
	if err != ErrNoToken {
		t.Fatalf("got %v", err)
	}
	if Status(err) != StatusNoToken {
		t.Fatalf("status %q", Status(err))
	}
}

// A rate limit arrives as HTTP 400 with the code in the body, not a 429.
func TestRateLimitIsHTTP400(t *testing.T) {
	body := `{"errors":[{"message":"too many","extensions":{"code":"RATELIMITED"}}]}`
	s := server(t, 400, body)
	c := New("k").WithEndpoint(s.URL)
	err := c.Do(context.Background(), `query { x }`, nil, nil)
	if err == nil {
		t.Fatal("no error")
	}
	if Status(err) != StatusRateLimited {
		t.Fatalf("status %q for %v", Status(err), err)
	}
}

func TestAuthFailure(t *testing.T) {
	s := server(t, 200, `{"errors":[{"message":"bad key","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`)
	c := New("k").WithEndpoint(s.URL)
	err := c.Do(context.Background(), `query { x }`, nil, nil)
	if Status(err) != StatusAuthFailed {
		t.Fatalf("status %q for %v", Status(err), err)
	}
}

func TestBoardReadsTheTeam(t *testing.T) {
	body := `{"data":{
      "team":{"id":"t1","key":"BRU","name":"Brunoh",
        "states":{"nodes":[{"id":"s1","name":"Todo","type":"unstarted","position":1}]},
        "issues":{"nodes":[{"id":"i1","identifier":"BRU-25","title":"M1.16 Checks",
          "state":{"id":"s2","name":"In Progress","type":"started","position":2},
          "project":{"id":"p1","name":"Rig"},
          "projectMilestone":{"id":"m1","name":"M1 Foundation"},
          "labels":{"nodes":[{"id":"l1","name":"Feature","color":"#BB87FC"}]},
          "relations":{"nodes":[{"type":"blocks","relatedIssue":{"identifier":"BRU-27","title":"M1.19","state":{"id":"s1","name":"Todo","type":"unstarted"}}}]},
          "updatedAt":"2026-10-20T12:34:22.603Z"}]}},
      "projects":{"nodes":[{"id":"p1","name":"Rig","slugId":"5010238d3b20","status":{"type":"backlog","name":"Backlog"},"progress":0.26,
        "projectMilestones":{"nodes":[{"id":"m1","name":"M1 Foundation","status":"next","progress":0.81}]}}]}}}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var fixture map[string]any
		json.Unmarshal([]byte(body), &fixture)
		data := fixture["data"].(map[string]any)
		switch {
		case strings.Contains(req.Query, "query Projects"):
			data["team"].(map[string]any)["projects"] = data["projects"]
		case strings.Contains(req.Query, "query Milestones"):
			data["project"] = data["projects"].(map[string]any)["nodes"].([]any)[0]
		}
		json.NewEncoder(w).Encode(fixture)
	}))
	defer s.Close()
	c := New("k").WithEndpoint(s.URL)
	snap, err := c.Board(context.Background(), "BRU")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Team.Key != "BRU" || len(snap.Team.States) != 1 {
		t.Fatalf("team %+v", snap.Team)
	}
	if len(snap.Projects) != 1 || len(snap.Projects[0].Milestones) != 1 {
		t.Fatalf("projects %+v", snap.Projects)
	}
	if snap.Projects[0].Milestones[0].Name != "M1 Foundation" {
		t.Fatalf("milestone %+v", snap.Projects[0].Milestones[0])
	}
	if len(snap.Issues) != 1 || snap.Issues[0].Identifier != "BRU-25" {
		t.Fatalf("issues %+v", snap.Issues)
	}
	if snap.Issues[0].State.Type != "started" {
		t.Fatalf("state %+v", snap.Issues[0].State)
	}
	// The times are read, not left as strings.
	if snap.Issues[0].UpdatedAt.IsZero() {
		t.Fatal("updatedAt was not parsed")
	}
}

func TestIssueReadsTheComments(t *testing.T) {
	body := `{"data":{"issue":{"id":"i1","identifier":"BRU-25","title":"M1.16 Checks",
      "description":"**Prompt:** Rig/build/tasks/M1/M1.16.md",
      "state":{"id":"s2","name":"Done","type":"completed"},
      "comments":{"nodes":[{"id":"c1","body":"done on m1-foundation.","createdAt":"2026-10-20T12:34:15.947Z","user":{"id":"u1","name":"Divyanshu DV"}}]},
      "attachments":{"nodes":[{"id":"a1","title":"Task prompt","url":"https://github.com/x/y/blob/main/build/tasks/M1/M1.16.md"}]},
      "relations":{"nodes":[{"type":"blocks","relatedIssue":{"identifier":"BRU-27","title":"M1.19","state":{"id":"s1","name":"Todo","type":"unstarted"}}}]}}}}`
	s := server(t, 200, body)
	c := New("k").WithEndpoint(s.URL)
	iss, err := c.Issue(context.Background(), "BRU-25")
	if err != nil {
		t.Fatal(err)
	}
	if len(iss.Comments) != 1 || iss.Comments[0].User == nil || iss.Comments[0].User.Name != "Divyanshu DV" {
		t.Fatalf("comments %+v", iss.Comments)
	}
	if iss.Comments[0].CreatedAt.IsZero() {
		t.Fatal("the comment's time was not parsed")
	}
	if len(iss.Attachments) != 1 || iss.Attachments[0].Title != "Task prompt" {
		t.Fatalf("attachments %+v", iss.Attachments)
	}
	if len(iss.Relations) != 1 || iss.Relations[0].Type != "blocks" {
		t.Fatalf("relations %+v", iss.Relations)
	}
	if iss.Relations[0].Issue.Identifier != "BRU-27" {
		t.Fatal("related issue was lost in JSON decoding")
	}
	if !strings.Contains(iss.Description, "build/tasks/M1/M1.16.md") {
		t.Fatalf("description %q", iss.Description)
	}
}

func TestSetStateAndComment(t *testing.T) {
	var seen []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		q, _ := body["query"].(string)
		seen = append(seen, q)
		if strings.Contains(q, "issueUpdate") {
			w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
			return
		}
		w.Write([]byte(`{"data":{"commentCreate":{"success":true}}}`))
	}))
	defer s.Close()
	c := New("k").WithEndpoint(s.URL)
	if err := c.SetState(context.Background(), "BRU-25", "s2"); err != nil {
		t.Fatal(err)
	}
	if err := c.Comment(context.Background(), "BRU-25", "done"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !strings.Contains(seen[0], "issueUpdate") || !strings.Contains(seen[1], "commentCreate") {
		t.Fatalf("sent %v", seen)
	}
}

func TestDeltaAsksForWhatChanged(t *testing.T) {
	var vars map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		vars, _ = body["variables"].(map[string]any)
		w.Write([]byte(`{"data":{"team":{"issues":{"nodes":[{"identifier":"BRU-25"}]}}}}`))
	}))
	defer s.Close()
	c := New("k").WithEndpoint(s.URL)
	since := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	issues, err := c.Delta(context.Background(), "BRU", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues", len(issues))
	}
	if vars["since"] != "2026-10-20T00:00:00.000Z" {
		t.Fatalf("since %v", vars["since"])
	}
}

func TestBoardPaginatesBeyondOneHundred(t *testing.T) {
	var cursors []any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string         `json:"query"`
			Vars  map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "query Board"):
			fmt.Fprint(w, `{"data":{"team":{"id":"t","key":"BRU","states":{"nodes":[{"id":"s","position":1.5}]}}}}`)
		case strings.Contains(req.Query, "query Projects"):
			fmt.Fprint(w, `{"data":{"team":{"projects":{"nodes":[]}}}}`)
		default:
			cursors = append(cursors, req.Vars["after"])
			nodes := make([]map[string]any, 0)
			start, end := 0, 100
			if req.Vars["after"] != nil {
				start, end = 100, 148
			}
			for n := start; n < end; n++ {
				nodes = append(nodes, map[string]any{"id": fmt.Sprint(n), "identifier": fmt.Sprintf("BRU-%d", n)})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"team": map[string]any{"issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": end == 100, "endCursor": "next"}}}}})
		}
	}))
	defer s.Close()
	snap, err := New("fixture").WithEndpoint(s.URL).Board(context.Background(), "BRU")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Issues) != 148 || len(cursors) != 2 || cursors[1] != "next" {
		t.Fatalf("lost paginated issues: %d, cursors %v", len(snap.Issues), cursors)
	}
	if snap.Team.States[0].Position != 1.5 {
		t.Fatal("fractional state ordering lost")
	}
}

func TestDeltaRejectsRepeatedCursor(t *testing.T) {
	s := server(t, 200, `{"data":{"team":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"repeat"}}}}}`)
	_, err := New("fixture").WithEndpoint(s.URL).Delta(context.Background(), "BRU", time.Now())
	if err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("bad cursor accepted: %v", err)
	}
}

func TestErrorsNeverEchoResponseOrCredential(t *testing.T) {
	for _, body := range []string{"credential-that-must-stay-private", `{"errors":[{"message":"credential-that-must-stay-private","extensions":{"code":"credential-that-must-stay-private"}}]}`} {
		s := server(t, 500, body)
		err := New("credential-that-must-stay-private").WithEndpoint(s.URL).Do(context.Background(), "query { x }", nil, nil)
		if err == nil || strings.Contains(err.Error(), "credential-that-must-stay-private") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}

func TestRateLimitCarriesResetTime(t *testing.T) {
	reset := time.Now().Add(3 * time.Minute).Truncate(time.Millisecond)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Requests-Reset", fmt.Sprint(reset.UnixMilli()))
		w.WriteHeader(429)
		fmt.Fprint(w, "rate limit")
	}))
	defer s.Close()
	err := New("fixture").WithEndpoint(s.URL).Do(context.Background(), "query{x}", nil, nil)
	if Status(err) != StatusRateLimited || !RetryAt(err).Equal(reset) {
		t.Fatalf("rate reset not honored: %v %v", err, RetryAt(err))
	}
}

func TestCredentialRotationPermissionsRemovalAndMalformedPreservation(t *testing.T) {
	t.Setenv("KERO_HOME", t.TempDir())
	t.Setenv("LINEAR_API_KEY", "")
	if err := SaveKey("first-fixture"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(credsFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if Key() != "first-fixture" {
		t.Fatal("key not read")
	}
	if err := SaveKey("second-fixture"); err != nil {
		t.Fatal(err)
	}
	if Key() != "second-fixture" {
		t.Fatal("rotated key not read")
	}
	if err := SaveKey(""); err != nil {
		t.Fatal(err)
	}
	if Key() != "" {
		t.Fatal("key not removed")
	}
	if err := SaveKey("bad\nkey"); err == nil {
		t.Fatal("newline accepted")
	}
	if err := os.WriteFile(credsFile(), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveKey("replacement"); err == nil {
		t.Fatal("corrupt credentials overwritten")
	}
	b, _ := os.ReadFile(filepath.Clean(credsFile()))
	if string(b) != "corrupt" {
		t.Fatal("existing credentials were changed")
	}
}

func TestIssuePaginatesActivity(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Query string         `json:"query"`
			Vars  map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Query, "query IssueItems") {
			if req.Vars["after"] != "next-comments" {
				t.Error("missing comment cursor")
			}
			fmt.Fprint(w, `{"data":{"issue":{"items":{"nodes":[{"id":"second","body":"later"}],"pageInfo":{"hasNextPage":false}}}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"issue":{"id":"i","identifier":"TEAM-1","comments":{"nodes":[{"id":"first","body":"earlier"}],"pageInfo":{"hasNextPage":true,"endCursor":"next-comments"}}}}}`)
	}))
	defer s.Close()
	issue, err := New("fixture").WithEndpoint(s.URL).Issue(context.Background(), "TEAM-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Comments) != 2 || issue.Comments[1].ID != "second" || calls != 2 {
		t.Fatal("activity was truncated")
	}
}

func TestClientNeverForwardsKeysOnRedirect(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	if err := New("fixture").WithEndpoint(origin.URL).Do(context.Background(), "query{x}", nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if forwarded {
		t.Fatal("redirect target received a request")
	}
}
