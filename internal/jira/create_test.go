package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestProjects_SinglePage(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "projects.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	ps, err := c.Projects(context.Background())
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if gotPath != "/rest/api/3/project/search" {
		t.Fatalf("path: got %q", gotPath)
	}
	if len(ps) != 2 {
		t.Fatalf("count: %d", len(ps))
	}
	want := []Project{
		{ID: "10000", Key: "PROJ", Name: "Sample Project"},
		{ID: "10001", Key: "ACME", Name: "Acme Corp"},
	}
	if !reflect.DeepEqual(ps, want) {
		t.Fatalf("projects:\n got %+v\nwant %+v", ps, want)
	}
}

func TestProjects_FollowsPagination(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 0:
			if r.URL.Query().Get("startAt") != "0" {
				t.Errorf("first call startAt: got %q", r.URL.Query().Get("startAt"))
			}
			_, _ = w.Write(readFixture(t, "projects_page1.json"))
		case 1:
			if r.URL.Query().Get("startAt") != "2" {
				t.Errorf("second call startAt: got %q", r.URL.Query().Get("startAt"))
			}
			_, _ = w.Write(readFixture(t, "projects_page2.json"))
		default:
			t.Errorf("unexpected extra call %d", calls)
		}
		calls++
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	ps, err := c.Projects(context.Background())
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 page fetches, got %d", calls)
	}
	if len(ps) != 3 {
		t.Fatalf("count: %d", len(ps))
	}
	if ps[2].Key != "CCC" {
		t.Fatalf("third project: %+v", ps[2])
	}
}

func TestCreateMeta_ParsesTypedFields(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "createmeta.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	meta, err := c.CreateMeta(context.Background(), "PROJ", "10100")
	if err != nil {
		t.Fatalf("CreateMeta: %v", err)
	}
	if gotPath != "/rest/api/3/issue/createmeta/PROJ/issuetypes/10100" {
		t.Fatalf("path: got %q", gotPath)
	}

	byID := map[string]FieldMeta{}
	for _, f := range meta.Fields {
		byID[f.ID] = f
	}

	cases := []struct {
		id          string
		name        string
		required    bool
		schemaType  string
		schemaItems string
		options     []FieldOption
	}{
		{"summary", "Summary", true, "string", "", nil},
		{"description", "Description", false, "string", "", nil},
		{"priority", "Priority", false, "priority", "", []FieldOption{
			{ID: "1", Name: "Highest"},
			{ID: "2", Name: "High"},
			{ID: "3", Name: "Medium"},
		}},
		{"issuetype", "Issue Type", true, "issuetype", "", []FieldOption{
			{ID: "10100", Name: "Task"},
			{ID: "10101", Name: "Bug"},
		}},
		{"assignee", "Assignee", false, "user", "", nil},
		{"labels", "Labels", false, "array", "string", nil},
		{"customfield_10010", "Sprint Color", false, "option", "", []FieldOption{
			{ID: "10300", Name: "Red"},
			{ID: "10301", Name: "Green"},
			{ID: "10302", Name: "Blue"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, ok := byID[tc.id]
			if !ok {
				t.Fatalf("field %q missing", tc.id)
			}
			if got.Name != tc.name {
				t.Errorf("name: got %q, want %q", got.Name, tc.name)
			}
			if got.Required != tc.required {
				t.Errorf("required: got %v, want %v", got.Required, tc.required)
			}
			if got.SchemaType != tc.schemaType {
				t.Errorf("schemaType: got %q, want %q", got.SchemaType, tc.schemaType)
			}
			if got.SchemaItems != tc.schemaItems {
				t.Errorf("schemaItems: got %q, want %q", got.SchemaItems, tc.schemaItems)
			}
			if !reflect.DeepEqual(got.AllowedValues, tc.options) {
				t.Errorf("allowedValues:\n got %+v\nwant %+v", got.AllowedValues, tc.options)
			}
		})
	}
}

func TestCreateMeta_RequiresProjectAndType(t *testing.T) {
	c := &Client{}
	if _, err := c.CreateMeta(context.Background(), "", "10100"); err == nil {
		t.Fatal("expected error for empty projectKey")
	}
	if _, err := c.CreateMeta(context.Background(), "PROJ", ""); err == nil {
		t.Fatal("expected error for empty issueTypeID")
	}
}

func TestCreateIssue_RoundTrip(t *testing.T) {
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(readFixture(t, "create_response.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	got, err := c.CreateIssue(context.Background(), CreatePayload{
		ProjectKey:  "PROJ",
		IssueTypeID: "10100",
		Summary:     "Make it work",
		Description: "First paragraph.\n\nSecond paragraph.",
		Priority:    "2",
		Assignee:    "abc-123",
		Labels:      []string{"backend", "urgent"},
		Fields: map[string]any{
			"customfield_10010": map[string]string{"id": "10300"},
		},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method: %q", gotMethod)
	}
	if gotPath != "/rest/api/3/issue" {
		t.Fatalf("path: %q", gotPath)
	}

	fields, ok := gotBody["fields"].(map[string]any)
	if !ok {
		t.Fatalf("body missing fields object: %+v", gotBody)
	}
	if proj, _ := fields["project"].(map[string]any); proj["key"] != "PROJ" {
		t.Errorf("project.key: %v", proj["key"])
	}
	if it, _ := fields["issuetype"].(map[string]any); it["id"] != "10100" {
		t.Errorf("issuetype.id: %v", it["id"])
	}
	if fields["summary"] != "Make it work" {
		t.Errorf("summary: %v", fields["summary"])
	}
	if pr, _ := fields["priority"].(map[string]any); pr["id"] != "2" {
		t.Errorf("priority.id: %v", pr["id"])
	}
	if as, _ := fields["assignee"].(map[string]any); as["accountId"] != "abc-123" {
		t.Errorf("assignee.accountId: %v", as["accountId"])
	}
	labels, _ := fields["labels"].([]any)
	if len(labels) != 2 || labels[0] != "backend" || labels[1] != "urgent" {
		t.Errorf("labels: %v", labels)
	}
	if cf, _ := fields["customfield_10010"].(map[string]any); cf["id"] != "10300" {
		t.Errorf("customfield: %v", cf)
	}

	desc, ok := fields["description"].(map[string]any)
	if !ok {
		t.Fatalf("description missing or not object: %+v", fields["description"])
	}
	if desc["type"] != "doc" {
		t.Errorf("description.type: %v", desc["type"])
	}
	if v, _ := desc["version"].(float64); v != 1 {
		t.Errorf("description.version: %v", desc["version"])
	}
	content, _ := desc["content"].([]any)
	if len(content) != 2 {
		t.Errorf("description paragraphs: got %d, want 2", len(content))
	}

	if got.Key != "PROJ-203" {
		t.Errorf("returned key: %q", got.Key)
	}
	if got.Summary != "Make it work" {
		t.Errorf("returned summary: %q", got.Summary)
	}
	if got.Type.ID != "10100" {
		t.Errorf("returned type.ID: %q", got.Type.ID)
	}
	if !strings.HasSuffix(got.URL, "/browse/PROJ-203") {
		t.Errorf("returned URL: %q", got.URL)
	}
}

func TestCreateIssue_OmitsOptionalFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(readFixture(t, "create_response.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if _, err := c.CreateIssue(context.Background(), CreatePayload{
		ProjectKey:  "PROJ",
		IssueTypeID: "10100",
		Summary:     "Minimal",
	}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	for _, k := range []string{"description", "priority", "assignee", "labels"} {
		if _, ok := fields[k]; ok {
			t.Errorf("field %q should be omitted, got %v", k, fields[k])
		}
	}
}

func TestCreateIssue_Validation(t *testing.T) {
	c := &Client{}
	cases := []struct {
		name string
		p    CreatePayload
	}{
		{"missing project", CreatePayload{IssueTypeID: "10100", Summary: "x"}},
		{"missing type", CreatePayload{ProjectKey: "PROJ", Summary: "x"}},
		{"missing summary", CreatePayload{ProjectKey: "PROJ", IssueTypeID: "10100"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.CreateIssue(context.Background(), tc.p); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestIssueTypesForProject_HitsCreateMetaAndMaps(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "issuetypes.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	types, err := c.IssueTypesForProject(context.Background(), "PROJ")
	if err != nil {
		t.Fatalf("IssueTypesForProject: %v", err)
	}
	if gotPath != "/rest/api/3/issue/createmeta/PROJ/issuetypes" {
		t.Fatalf("path: got %q", gotPath)
	}
	if len(types) != 5 {
		t.Fatalf("count: got %d, want 5", len(types))
	}
	if types[0].ID != "10100" || types[0].Name != "Task" || types[0].Subtask {
		t.Errorf("types[0]: %+v", types[0])
	}
	if types[3].Name != "Sub-task" || !types[3].Subtask {
		t.Errorf("types[3]: %+v", types[3])
	}
}

func TestIssueTypesForProject_RequiresProjectKey(t *testing.T) {
	c := &Client{}
	if _, err := c.IssueTypesForProject(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty project key")
	}
}

func TestCreateIssue_SurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"summary":"is required"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	_, err := c.CreateIssue(context.Background(), CreatePayload{
		ProjectKey:  "PROJ",
		IssueTypeID: "10100",
		Summary:     "x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status: %d", httpErr.StatusCode)
	}
	if !strings.Contains(string(httpErr.Body), "is required") {
		t.Errorf("body: %s", httpErr.Body)
	}
}

func TestCreateIssue_EmitsParentWhenParentKeySet(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "create_response.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	_, err := c.CreateIssue(context.Background(), CreatePayload{
		ProjectKey:  "PROJ",
		IssueTypeID: "10103",
		Summary:     "Sub-task summary",
		ParentKey:   "PROJ-100",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	var payload struct {
		Fields struct {
			Parent struct {
				Key string `json:"key"`
			} `json:"parent"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if payload.Fields.Parent.Key != "PROJ-100" {
		t.Errorf("parent.key: got %q, want %q", payload.Fields.Parent.Key, "PROJ-100")
	}
}

func TestCreateIssue_OmitsParentWhenEmpty(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "create_response.json"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	_, err := c.CreateIssue(context.Background(), CreatePayload{
		ProjectKey:  "PROJ",
		IssueTypeID: "10100",
		Summary:     "Plain task",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if strings.Contains(string(gotBody), `"parent"`) {
		t.Errorf("body contained parent field unexpectedly: %s", gotBody)
	}
}
