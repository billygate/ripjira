package structureadapter

import (
	"testing"

	"github.com/billygate/ripjira/internal/jira"
)

func TestAdapter_FieldsResolveCorrectly(t *testing.T) {
	user := &jira.User{DisplayName: "Alice"}
	is := jira.Issue{
		Key:           "BIL-1",
		Status:        jira.Status{Name: "In Progress", Category: "indeterminate"},
		Priority:      jira.Priority{Name: "High"},
		Type:          jira.IssueType{Name: "Bug"},
		Assignee:      user,
		Reporter:      user,
		Labels:        []string{"ui", "blocker"},
		ParentKey:     "BIL-100",
		ParentSummary: "Epic",
	}
	a := New(is)
	cases := map[string]string{
		"status":          "In Progress",
		"status_category": "indeterminate",
		"priority":        "High",
		"issuetype":       "Bug",
		"assignee":        "Alice",
		"reporter":        "Alice",
		"labels":          "ui, blocker",
		"parent_key":      "BIL-100",
		"project":         "BIL",
		"unknown":         "",
	}
	for field, want := range cases {
		if got := a.Field(field); got != want {
			t.Errorf("Field(%q) = %q, want %q", field, got, want)
		}
	}
}

func TestAdapter_NilAssignee(t *testing.T) {
	a := New(jira.Issue{Key: "X-1"})
	if a.Field("assignee") != "" {
		t.Fatal("nil assignee should be empty")
	}
}
