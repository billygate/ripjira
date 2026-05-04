// Package structureadapter wraps jira.Issue with the
// structure.Issue interface so the pure evaluator can read named fields.
package structureadapter

import (
	"strings"

	"github.com/billygate/ripjira/internal/jira"
)

// Adapter implements structure.Issue over jira.Issue.
type Adapter struct{ issue jira.Issue }

// New returns an Adapter for issue.
func New(issue jira.Issue) Adapter { return Adapter{issue: issue} }

// Issue returns the wrapped jira.Issue (for callers that need to render it
// after the structure evaluator picks it).
func (a Adapter) Issue() jira.Issue { return a.issue }

// Field implements structure.Issue.
func (a Adapter) Field(name string) string {
	switch name {
	case "status":
		return a.issue.Status.Name
	case "status_category":
		return a.issue.Status.Category
	case "priority":
		return a.issue.Priority.Name
	case "issuetype":
		return a.issue.Type.Name
	case "assignee":
		if a.issue.Assignee != nil {
			return a.issue.Assignee.DisplayName
		}
		return ""
	case "reporter":
		if a.issue.Reporter != nil {
			return a.issue.Reporter.DisplayName
		}
		return ""
	case "parent_key":
		return a.issue.ParentKey
	case "labels":
		return strings.Join(a.issue.Labels, ", ")
	case "project":
		if i := strings.Index(a.issue.Key, "-"); i > 0 {
			return a.issue.Key[:i]
		}
		return ""
	}
	return ""
}
