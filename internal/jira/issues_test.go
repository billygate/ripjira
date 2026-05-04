package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readFixture loads testdata/<name> as []byte.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestMyself_ReturnsUserAndCachesAccountID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "myself.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	u, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if gotPath != "/rest/api/3/myself" {
		t.Fatalf("path: got %q", gotPath)
	}
	if u.AccountID != "5b10ac8d82e05b22cc7d4ef5" {
		t.Fatalf("AccountID: got %q", u.AccountID)
	}
	if u.DisplayName != "Sample User" {
		t.Fatalf("DisplayName: got %q", u.DisplayName)
	}
	if u.Email != "sample.user@example.com" {
		t.Fatalf("Email: got %q", u.Email)
	}
	// Account ID is cached on the client.
	if c.AccountID() != u.AccountID {
		t.Fatalf("client AccountID cache: got %q, want %q", c.AccountID(), u.AccountID)
	}
}

func TestMyIssues_HitsSearchJQLAndMaps(t *testing.T) {
	var (
		gotPath   string
		gotJQL    string
		gotFields string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotJQL = r.URL.Query().Get("jql")
		gotFields = r.URL.Query().Get("fields")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "search.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	issues, err := c.MyIssues(context.Background())
	if err != nil {
		t.Fatalf("MyIssues: %v", err)
	}
	if gotPath != "/rest/api/3/search/jql" {
		t.Fatalf("path: got %q", gotPath)
	}
	wantJQL := "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"
	if gotJQL != wantJQL {
		t.Fatalf("jql: got %q, want %q", gotJQL, wantJQL)
	}
	if gotFields == "" {
		t.Fatalf("fields param missing — new /search/jql returns empty fields by default")
	}
	if len(issues) != 2 {
		t.Fatalf("issues count: got %d", len(issues))
	}

	a := issues[0]
	if a.Key != "PROJ-142" || a.Summary != "Fix login redirect on Safari" {
		t.Fatalf("issue[0]: %+v", a)
	}
	if a.Status.ID != "10001" || a.Status.Name != "In Progress" || a.Status.Category != "indeterminate" {
		t.Fatalf("status: %+v", a.Status)
	}
	if a.Priority.ID != "2" || a.Priority.Name != "High" {
		t.Fatalf("priority: %+v", a.Priority)
	}
	if a.Type.ID != "10100" || a.Type.Name != "Task" || a.Type.Subtask {
		t.Fatalf("type: %+v", a.Type)
	}
	if a.Assignee == nil || a.Assignee.AccountID != "5b10ac8d82e05b22cc7d4ef5" {
		t.Fatalf("assignee: %+v", a.Assignee)
	}
	if a.Reporter == nil || a.Reporter.DisplayName != "Reporter Person" {
		t.Fatalf("reporter: %+v", a.Reporter)
	}
	wantUpdated := time.Date(2026, 4, 30, 12, 34, 56, 0, time.UTC)
	if !a.Updated.Equal(wantUpdated) {
		t.Fatalf("updated: got %v, want %v", a.Updated, wantUpdated)
	}
	wantURL := strings.TrimRight(srv.URL, "/") + "/browse/PROJ-142"
	if a.URL != wantURL {
		t.Fatalf("URL: got %q, want %q", a.URL, wantURL)
	}

	b := issues[1]
	if b.Key != "PROJ-118" {
		t.Fatalf("issue[1]: %+v", b)
	}
	if b.Assignee != nil {
		t.Fatalf("assignee should be nil: %+v", b.Assignee)
	}
	if b.Reporter != nil {
		t.Fatalf("reporter should be nil: %+v", b.Reporter)
	}
	// priority is null in fixture; should map to zero-value Priority{}.
	if b.Priority.ID != "" || b.Priority.Name != "" {
		t.Fatalf("priority should be empty: %+v", b.Priority)
	}
}

func TestGetIssue_FetchesAndStripsRenderedDescription(t *testing.T) {
	var (
		gotPath   string
		gotFields string
		gotExpand string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotFields = r.URL.Query().Get("fields")
		gotExpand = r.URL.Query().Get("expand")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "issue.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	is, err := c.GetIssue(context.Background(), "PROJ-142")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotPath != "/rest/api/3/issue/PROJ-142" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotFields != "*all" {
		t.Fatalf("fields: got %q", gotFields)
	}
	if gotExpand != "renderedFields" {
		t.Fatalf("expand: got %q", gotExpand)
	}
	if is.Key != "PROJ-142" {
		t.Fatalf("key: %q", is.Key)
	}
	wantDesc := "When user logs in via Safari 17,  \nthe redirect to /dashboard fails.\n\nSteps to reproduce: open & close."
	if is.Description != wantDesc {
		t.Fatalf("description:\n got %q\nwant %q", is.Description, wantDesc)
	}
	if len(is.Comments) != 1 {
		t.Fatalf("comments: got %d", len(is.Comments))
	}
	if got := is.Comments[0].Body; got != "I see the same issue." {
		t.Fatalf("comment body: %q", got)
	}
	if is.Comments[0].Author.DisplayName != "Reporter Person" {
		t.Fatalf("comment author: %+v", is.Comments[0].Author)
	}
	wantCommentCreated := time.Date(2026, 4, 30, 13, 0, 0, 0, time.UTC)
	if !is.Comments[0].Created.Equal(wantCommentCreated) {
		t.Fatalf("comment created: got %v, want %v", is.Comments[0].Created, wantCommentCreated)
	}
	if len(is.Subtasks) != 2 {
		t.Fatalf("subtasks count: got %d, want 2", len(is.Subtasks))
	}
	s0 := is.Subtasks[0]
	if s0.Key != "PROJ-200" || s0.Summary != "Investigate Safari user agent" {
		t.Errorf("subtasks[0]: %+v", s0)
	}
	if s0.Status.ID != "10001" || s0.Status.Name != "In Progress" || s0.Status.Category != "indeterminate" {
		t.Errorf("subtasks[0].Status: %+v", s0.Status)
	}
	s1 := is.Subtasks[1]
	if s1.Key != "PROJ-201" || s1.Summary != "Patch redirect logic" {
		t.Errorf("subtasks[1]: %+v", s1)
	}
	if s1.Status.Name != "To Do" {
		t.Errorf("subtasks[1].Status.Name: %q", s1.Status.Name)
	}
}

func TestGetTransitions(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "transitions.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	ts, err := c.GetTransitions(context.Background(), "PROJ-142")
	if err != nil {
		t.Fatalf("GetTransitions: %v", err)
	}
	if gotPath != "/rest/api/3/issue/PROJ-142/transitions" {
		t.Fatalf("path: got %q", gotPath)
	}
	if len(ts) != 2 {
		t.Fatalf("count: %d", len(ts))
	}
	if ts[0].ID != "11" || ts[0].Name != "Start Progress" || ts[0].To.ID != "10001" {
		t.Fatalf("first transition: %+v", ts[0])
	}
	if ts[1].To.Category != "done" {
		t.Fatalf("second transition.To.Category: %q", ts[1].To.Category)
	}
}

func TestDoTransition(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.DoTransition(context.Background(), "PROJ-142", "11"); err != nil {
		t.Fatalf("DoTransition: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method: %q", gotMethod)
	}
	if gotPath != "/rest/api/3/issue/PROJ-142/transitions" {
		t.Fatalf("path: %q", gotPath)
	}
	tr, ok := gotBody["transition"].(map[string]any)
	if !ok {
		t.Fatalf("body missing transition object: %+v", gotBody)
	}
	if tr["id"] != "11" {
		t.Fatalf("transition.id: %v", tr["id"])
	}
}

func TestUpdateIssue(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	err := c.UpdateIssue(context.Background(), "PROJ-1", map[string]any{
		"summary":  "New title",
		"priority": map[string]any{"name": "High"},
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method: %q", gotMethod)
	}
	if gotPath != "/rest/api/3/issue/PROJ-1" {
		t.Fatalf("path: %q", gotPath)
	}
	fields, ok := gotBody["fields"].(map[string]any)
	if !ok {
		t.Fatalf("body missing fields object: %+v", gotBody)
	}
	if fields["summary"] != "New title" {
		t.Fatalf("summary: %v", fields["summary"])
	}
	pr, ok := fields["priority"].(map[string]any)
	if !ok || pr["name"] != "High" {
		t.Fatalf("priority: %v", fields["priority"])
	}
}

func TestCreateIssueLink(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.CreateIssueLink(context.Background(), "Blocks", "PROJ-1", "PROJ-2"); err != nil {
		t.Fatalf("CreateIssueLink: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/rest/api/3/issueLink" {
		t.Fatalf("method/path: %s %s", gotMethod, gotPath)
	}
	typ, ok := gotBody["type"].(map[string]any)
	if !ok || typ["name"] != "Blocks" {
		t.Fatalf("type: %v", gotBody["type"])
	}
	in, ok := gotBody["inwardIssue"].(map[string]any)
	if !ok || in["key"] != "PROJ-1" {
		t.Fatalf("inwardIssue: %v", gotBody["inwardIssue"])
	}
	out, ok := gotBody["outwardIssue"].(map[string]any)
	if !ok || out["key"] != "PROJ-2" {
		t.Fatalf("outwardIssue: %v", gotBody["outwardIssue"])
	}
}

func TestAddWorklog(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.AddWorklog(context.Background(), "PROJ-7", "1h 30m", "fixed it"); err != nil {
		t.Fatalf("AddWorklog: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/rest/api/3/issue/PROJ-7/worklog" {
		t.Fatalf("method/path: %s %s", gotMethod, gotPath)
	}
	if gotBody["timeSpent"] != "1h 30m" {
		t.Fatalf("timeSpent: %v", gotBody["timeSpent"])
	}
	if _, hasComment := gotBody["comment"]; !hasComment {
		t.Fatalf("comment ADF not in body: %+v", gotBody)
	}
}

func TestAddWatcher_NoBodyMeansSelf(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = strings.TrimSpace(string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.AddWatcher(context.Background(), "PROJ-7", ""); err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/rest/api/3/issue/PROJ-7/watchers" {
		t.Fatalf("method/path: %s %s", gotMethod, gotPath)
	}
	// Empty accountID → no body (Jira's "self" semantics).
	if gotBody != "" {
		t.Fatalf("body for self-watch should be empty, got %q", gotBody)
	}
}

func TestRemoveWatcher(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotQuery  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.RemoveWatcher(context.Background(), "PROJ-7", "abc-123"); err != nil {
		t.Fatalf("RemoveWatcher: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method: %s", gotMethod)
	}
	if gotPath != "/rest/api/3/issue/PROJ-7/watchers" {
		t.Fatalf("path: %s", gotPath)
	}
	if !strings.Contains(gotQuery, "accountId=abc-123") {
		t.Fatalf("query: %s", gotQuery)
	}
}

func TestDeleteIssueLink(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.DeleteIssueLink(context.Background(), "10001"); err != nil {
		t.Fatalf("DeleteIssueLink: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/rest/api/3/issueLink/10001" {
		t.Fatalf("method/path: %s %s", gotMethod, gotPath)
	}
}

func TestUpdateDescription_SendsADF(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.UpdateDescription(context.Background(), "PROJ-1", "Hello world"); err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	desc, _ := fields["description"].(map[string]any)
	if desc["type"] != "doc" || desc["version"].(float64) != 1 {
		t.Fatalf("description not ADF: %+v", desc)
	}
}

func TestUpdateIssue_RejectsEmptyKeyOrFields(t *testing.T) {
	c, err := NewClient("https://x.atlassian.net", "a@b.com", "tok")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.UpdateIssue(context.Background(), "", map[string]any{"summary": "x"}); err == nil {
		t.Fatal("empty key should error")
	}
	if err := c.UpdateIssue(context.Background(), "PROJ-1", nil); err == nil {
		t.Fatal("nil fields should error")
	}
}

func TestAssignIssue(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.AssignIssue(context.Background(), "PROJ-142", "abc-123"); err != nil {
		t.Fatalf("AssignIssue: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method: %q", gotMethod)
	}
	if gotPath != "/rest/api/3/issue/PROJ-142/assignee" {
		t.Fatalf("path: %q", gotPath)
	}
	if gotBody["accountId"] != "abc-123" {
		t.Fatalf("body accountId: %v", gotBody["accountId"])
	}
}

func TestAssignIssue_Unassign(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.AssignIssue(context.Background(), "PROJ-142", ""); err != nil {
		t.Fatalf("AssignIssue: %v", err)
	}
	// Empty accountID unassigns the issue per Jira docs (accountId: null).
	v, ok := gotBody["accountId"]
	if !ok {
		t.Fatalf("accountId field missing: %+v", gotBody)
	}
	if v != nil {
		t.Fatalf("expected accountId=null, got %v", v)
	}
}

func TestSearchUsers(t *testing.T) {
	var (
		gotPath  string
		gotQuery string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "users.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	users, err := c.SearchUsers(context.Background(), "an")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if gotPath != "/rest/api/3/user/search" {
		t.Fatalf("path: %q", gotPath)
	}
	if gotQuery != "an" {
		t.Fatalf("query: %q", gotQuery)
	}
	if len(users) != 2 {
		t.Fatalf("count: %d", len(users))
	}
	if users[0].DisplayName != "Sample User" {
		t.Fatalf("first user: %+v", users[0])
	}
}

func TestStripHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"single paragraph", "<p>hello world</p>", "hello world"},
		{
			"two paragraphs",
			"<p>first</p><p>second</p>",
			"first\n\nsecond",
		},
		{
			"br as line break",
			"<p>line1<br/>line2</p>",
			"line1\nline2",
		},
		{
			"br variants",
			"a<br>b<br />c<BR/>d",
			"a\nb\nc\nd",
		},
		{
			"entity decode",
			"<p>open &amp; close &lt;tag&gt; &quot;q&quot; &#39;s&#39; &nbsp;x</p>",
			"open & close <tag> \"q\" 's'  x",
		},
		{
			"nested tags stripped",
			"<p>hello <strong>bold <em>and italic</em></strong> world</p>",
			"hello bold and italic world",
		},
		{
			"list items",
			"<ul><li>one</li><li>two</li></ul>",
			"one\ntwo",
		},
		{
			"divs",
			"<div>a</div><div>b</div>",
			"a\nb",
		},
		{
			"trims leading/trailing whitespace and collapses extras",
			"  \n<p>hi</p>\n  \n  ",
			"hi",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripHTML(tc.in)
			if got != tc.want {
				t.Fatalf("stripHTML(%q):\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseJiraTime(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"2026-04-30T12:34:56.000+0000", time.Date(2026, 4, 30, 12, 34, 56, 0, time.UTC), false},
		{"2026-04-30T12:34:56.000Z", time.Date(2026, 4, 30, 12, 34, 56, 0, time.UTC), false},
		{"2026-04-30T12:34:56+02:00", time.Date(2026, 4, 30, 10, 34, 56, 0, time.UTC), false},
		{"", time.Time{}, false},
		{"not a date", time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseJiraTime(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if !got.UTC().Equal(tc.want.UTC()) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMyIssues_FollowsNextPageToken(t *testing.T) {
	calls := 0
	var gotTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokens = append(gotTokens, r.URL.Query().Get("nextPageToken"))
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`{"issues":[{"key":"P-1","fields":{"summary":"a"}}],"nextPageToken":"tok2","isLast":false}`))
		case 2:
			_, _ = w.Write([]byte(`{"issues":[{"key":"P-2","fields":{"summary":"b"}}],"isLast":true}`))
		default:
			t.Fatalf("unexpected extra call: %d", calls)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	issues, err := c.MyIssues(context.Background())
	if err != nil {
		t.Fatalf("MyIssues: %v", err)
	}
	if len(issues) != 2 || issues[0].Key != "P-1" || issues[1].Key != "P-2" {
		t.Fatalf("issues: %+v", issues)
	}
	if calls != 2 {
		t.Fatalf("calls: got %d, want 2", calls)
	}
	if gotTokens[0] != "" || gotTokens[1] != "tok2" {
		t.Fatalf("page tokens: %v", gotTokens)
	}
}

func TestMyIssues_PropagatesContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.MyIssues(ctx)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MyIssues did not return after context cancel")
	}
}

func TestSearch_PassesArbitraryJQL(t *testing.T) {
	var gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJQL = r.URL.Query().Get("jql")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[],"isLast":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if _, err := c.Search(context.Background(), `project = ABC AND text ~ "foo"`); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotJQL != `project = ABC AND text ~ "foo"` {
		t.Fatalf("jql: got %q", gotJQL)
	}
}

func TestWatchingIssues_HitsSearchJQLWithWatcherJQL(t *testing.T) {
	var gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJQL = r.URL.Query().Get("jql")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[],"isLast":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if _, err := c.WatchingIssues(context.Background()); err != nil {
		t.Fatalf("WatchingIssues: %v", err)
	}
	want := "watcher = currentUser() AND resolution = Unresolved ORDER BY updated DESC"
	if gotJQL != want {
		t.Fatalf("jql: got %q, want %q", gotJQL, want)
	}
}

func TestIssueCreatedParsed(t *testing.T) {
	raw := []byte(`{
		"key": "PROJ-1",
		"fields": {
			"summary": "x",
			"created": "2026-01-15T10:00:00.000+0000",
			"updated": "2026-01-16T11:00:00.000+0000"
		}
	}`)
	var dto issueDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := newTestClient(t, srv, "a@b.com", "tok")
	iss := c.dtoToIssue(dto)
	want := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	if !iss.Created.Equal(want) {
		t.Fatalf("Created = %v, want %v", iss.Created, want)
	}
}

// Sanity check that the URL on issues uses the client's BaseURL host.
func TestIssueURL_UsesBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(readFixture(t, "search.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	issues, err := c.MyIssues(context.Background())
	if err != nil {
		t.Fatalf("MyIssues: %v", err)
	}
	u, err := url.Parse(issues[0].URL)
	if err != nil {
		t.Fatalf("issue URL parse: %v", err)
	}
	if u.Host != c.BaseURL().Host {
		t.Fatalf("issue URL host: got %q, want %q", u.Host, c.BaseURL().Host)
	}
	if !strings.HasSuffix(u.Path, "/browse/PROJ-142") {
		t.Fatalf("issue URL path: %q", u.Path)
	}
}
