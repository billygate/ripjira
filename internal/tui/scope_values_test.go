package tui

import (
	"reflect"
	"testing"

	"github.com/billygate/ripjira/internal/jira"
)

func TestUniqueValues(t *testing.T) {
	issues := []jira.Issue{
		{Key: "A-1", Labels: []string{"alpha", "beta"}, Status: jira.Status{Name: "Open"}, Priority: jira.Priority{Name: "High"}, Assignee: &jira.User{DisplayName: "Alice"}},
		{Key: "A-2", Labels: []string{"beta", "gamma"}, Status: jira.Status{Name: "Done"}, Priority: jira.Priority{Name: "Low"}},
		{Key: "A-3", Labels: nil, Status: jira.Status{Name: "Open"}, Priority: jira.Priority{Name: "High"}, Assignee: &jira.User{DisplayName: "Bob"}},
	}
	cases := []struct {
		field string
		want  []string
	}{
		{"labels", []string{"alpha", "beta", "gamma"}},
		{"status", []string{"Done", "Open"}},
		{"priority", []string{"High", "Low"}},
		{"assignee", []string{"Alice", "Bob"}},
		{"unknown", nil},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			got := UniqueValues(issues, tc.field)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("field %q: want %v, got %v", tc.field, tc.want, got)
			}
		})
	}
}
