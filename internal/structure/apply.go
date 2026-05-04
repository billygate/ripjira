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

// TreeNode is one node in a multi-level group_by tree. Leaf nodes
// (Children == nil) carry the matched issues; interior nodes carry only the
// nested children. Paths are unique strings (joined with "/") that the UI
// uses as stable keys for collapse state.
type TreeNode struct {
	Title    string
	Path     string
	Depth    int
	Children []TreeNode
	Issues   []Issue
}

// GroupTree recursively buckets issues by groupBy levels. Returns one root
// per distinct value at the first level; each sub-level recurses. When
// groupBy is empty, returns a single anonymous node containing every issue.
func GroupTree(issues []Issue, groupBy []string, parentPath string, depth int) []TreeNode {
	if len(groupBy) == 0 {
		return []TreeNode{{
			Path:   parentPath,
			Depth:  depth,
			Issues: issues,
		}}
	}
	field := groupBy[0]
	keys := []string{}
	buckets := map[string][]Issue{}
	for _, is := range issues {
		v := is.Field(field)
		if v == "" {
			v = "(none)"
		}
		if _, ok := buckets[v]; !ok {
			keys = append(keys, v)
		}
		buckets[v] = append(buckets[v], is)
	}
	out := make([]TreeNode, 0, len(keys))
	for _, k := range keys {
		path := k
		if parentPath != "" {
			path = parentPath + "/" + k
		}
		title := field + ": " + k
		children := GroupTree(buckets[k], groupBy[1:], path, depth+1)
		// Last-level descendants store issues directly; pass them through.
		out = append(out, TreeNode{
			Title:    title,
			Path:     path,
			Depth:    depth,
			Children: children,
		})
	}
	return out
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
