package structure

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// AppliedSection is one section after filtering: title + the issues that
// matched, in input order. Callers feed AppliedSection.Issues into their
// existing grouping pipeline (per-section group_by handled by callers).
type AppliedSection struct {
	Title   string
	GroupBy []string
	OrderBy []SortKey
	Issues  []Issue
}

// Apply runs each section's filter+anyOf over issues and returns one
// AppliedSection per non-empty section, in declaration order. Empty
// sections (zero matches) are dropped.
func Apply(issues []Issue, s *Structure) []AppliedSection {
	out := make([]AppliedSection, 0, len(s.Sections))
	for i := range s.Sections {
		sec := &s.Sections[i]
		matched := filterIssues(issues, sec.Filter, sec.AnyOf)
		if len(matched) == 0 {
			continue
		}
		out = append(out, AppliedSection{
			Title:   sec.Title,
			GroupBy: sec.GroupBy,
			OrderBy: sec.OrderBy,
			Issues:  matched,
		})
	}
	return out
}

func filterIssues(issues []Issue, filter SectionFilter, anyOf []SectionFilter) []Issue {
	if len(filter) == 0 && len(anyOf) == 0 {
		return issues
	}
	out := make([]Issue, 0, len(issues))
	for _, is := range issues {
		if !matchesFilter(is, filter) {
			continue
		}
		if !matchesAnyOf(is, anyOf) {
			continue
		}
		out = append(out, is)
	}
	return out
}

func matchesFilter(issue Issue, filter SectionFilter) bool {
	for field, clause := range filter {
		if !clauseMatches(issue.Field(field), &clause) {
			return false
		}
	}
	return true
}

func matchesAnyOf(issue Issue, anyOf []SectionFilter) bool {
	if len(anyOf) == 0 {
		return true
	}
	for _, alt := range anyOf {
		if matchesFilter(issue, alt) {
			return true
		}
	}
	return false
}

func clauseMatches(value string, c *FilterClause) bool {
	return existsMatches(value, c.Exists) &&
		(c.In == nil || valueIn(value, c.In)) &&
		(c.Not == nil || !valueIn(value, c.Not)) &&
		regexMatches(value, c) &&
		(c.Contains == "" || containsValue(value, c.Contains))
}

func existsMatches(value string, want *bool) bool {
	if want == nil {
		return true
	}
	return *want == (value != "")
}

func regexMatches(value string, c *FilterClause) bool {
	if c.Regex == "" {
		return true
	}
	re, err := c.matcher()
	if err != nil {
		return false
	}
	if value == "" {
		return re.MatchString("")
	}
	return slices.ContainsFunc(splitFieldValue(value), re.MatchString)
}

func valueIn(value string, allowed []string) bool {
	if value == "" {
		return slices.Contains(allowed, "")
	}
	for _, part := range splitFieldValue(value) {
		if slices.Contains(allowed, part) {
			return true
		}
	}
	return false
}

func containsValue(value, needle string) bool {
	if value == "" {
		return needle == ""
	}
	for _, part := range splitFieldValue(value) {
		if strings.Contains(part, needle) {
			return true
		}
	}
	return false
}

func (c *FilterClause) matcher() (*regexp.Regexp, error) {
	if c.compiled != nil {
		return c.compiled, nil
	}
	re, err := regexp.Compile(c.Regex)
	if err != nil {
		return nil, fmt.Errorf("compile regex %q: %w", c.Regex, err)
	}
	c.compiled = re
	return re, nil
}
