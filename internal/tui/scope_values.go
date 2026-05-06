package tui

import (
	"sort"

	"github.com/billygate/ripjira/internal/jira"
)

// UniqueValues returns a sorted, de-duplicated list of values seen for
// the given logical field across the in-memory issue set. Used by the
// scope editor for autocomplete. Unknown fields return nil.
func UniqueValues(issues []jira.Issue, field string) []string {
	seen := map[string]struct{}{}
	add := func(s string) {
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}
	for _, is := range issues {
		switch field {
		case "labels":
			for _, l := range is.Labels {
				add(l)
			}
		case "status":
			add(is.Status.Name)
		case "priority":
			add(is.Priority.Name)
		case "issuetype":
			add(is.Type.Name)
		case "assignee":
			if is.Assignee != nil {
				add(is.Assignee.DisplayName)
			}
		case "reporter":
			if is.Reporter != nil {
				add(is.Reporter.DisplayName)
			}
		case "project":
			if i := indexOfDash(is.Key); i > 0 {
				add(is.Key[:i])
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func indexOfDash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			return i
		}
	}
	return -1
}
