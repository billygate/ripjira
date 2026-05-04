// Package grouping implements list-grouping strategies for the issue list:
// by status (default), by priority, and any future axis. UI code asks the
// strategy to bucket a flat []jira.Issue into ordered groups; the list pane
// renders those groups with collapsible headers.
package grouping

import (
	"sort"
	"strings"

	"github.com/billygate/ripjira/internal/jira"
)

// Group is one bucket of issues sharing a label (status name, priority, …).
type Group struct {
	Key    string
	Issues []jira.Issue
}

// Strategy partitions a flat issue list into ordered groups.
type Strategy interface {
	// Name returns the canonical name ("status", "priority", …) used to look
	// the strategy up by config or hot-key.
	Name() string

	// Group buckets issues into ordered groups. Implementations are stable:
	// the same input yields the same output order.
	Group([]jira.Issue) []Group
}

// ByStatus groups by Status.Name. Groups are ordered by status category
// (new → indeterminate → done) so the most actionable work appears first;
// ties break alphabetically.
type ByStatus struct{}

// Name implements Strategy.
func (ByStatus) Name() string { return "status" }

var statusCategoryOrder = map[string]int{
	"new":           0,
	"indeterminate": 1,
	"done":          2,
}

// Group implements Strategy.
func (ByStatus) Group(issues []jira.Issue) []Group {
	type bucketKey struct {
		name     string
		category string
	}
	buckets := map[bucketKey][]jira.Issue{}
	order := []bucketKey{}
	for _, is := range issues {
		k := bucketKey{name: is.Status.Name, category: is.Status.Category}
		if k.name == "" {
			k.name = "Unknown"
		}
		if _, seen := buckets[k]; !seen {
			order = append(order, k)
		}
		buckets[k] = append(buckets[k], is)
	}
	sort.SliceStable(order, func(i, j int) bool {
		ci, oki := statusCategoryOrder[order[i].category]
		cj, okj := statusCategoryOrder[order[j].category]
		if oki && okj && ci != cj {
			return ci < cj
		}
		if oki != okj {
			return oki
		}
		return order[i].name < order[j].name
	})
	out := make([]Group, len(order))
	for i, k := range order {
		out[i] = Group{Key: k.name, Issues: buckets[k]}
	}
	return out
}

// ByPriority groups by Priority.Name in spec-defined order
// (Highest → High → Medium → Low → Lowest); unknown names sort last
// alphabetically.
type ByPriority struct{}

// Name implements Strategy.
func (ByPriority) Name() string { return "priority" }

var priorityOrder = map[string]int{
	"highest": 0,
	"high":    1,
	"medium":  2,
	"low":     3,
	"lowest":  4,
}

// Group implements Strategy.
func (ByPriority) Group(issues []jira.Issue) []Group {
	buckets := map[string][]jira.Issue{}
	order := []string{}
	for _, is := range issues {
		name := is.Priority.Name
		if name == "" {
			name = "Unknown"
		}
		if _, seen := buckets[name]; !seen {
			order = append(order, name)
		}
		buckets[name] = append(buckets[name], is)
	}
	sort.SliceStable(order, func(i, j int) bool {
		oi, oki := priorityOrder[strings.ToLower(order[i])]
		oj, okj := priorityOrder[strings.ToLower(order[j])]
		if oki && okj {
			return oi < oj
		}
		if oki != okj {
			return oki
		}
		return order[i] < order[j]
	})
	out := make([]Group, len(order))
	for i, k := range order {
		out[i] = Group{Key: k, Issues: buckets[k]}
	}
	return out
}

// sortByPriorityDesc orders issues by priority rank (Highest → Lowest →
// Unknown); ties break by Updated DESC.
func sortByPriorityDesc(issues []jira.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		ri := priorityRank(issues[i].Priority.Name)
		rj := priorityRank(issues[j].Priority.Name)
		if ri != rj {
			return ri < rj
		}
		return issues[i].Updated.After(issues[j].Updated)
	})
}

// priorityRank returns the index of name in priorityOrder, or
// len(priorityOrder) for unknown values (so they sort last).
func priorityRank(name string) int {
	if r, ok := priorityOrder[strings.ToLower(name)]; ok {
		return r
	}
	return len(priorityOrder)
}

// ByParent groups issues into an "Epics" zone (issues whose Type.Name
// matches one of EpicTypes, case-insensitive), then one bucket per
// non-empty ParentKey sorted alphabetically by key, then a "No epic"
// trailing bucket. Within each bucket: priority desc, then updated desc.
type ByParent struct {
	EpicTypes []string
}

// Name implements Strategy.
func (ByParent) Name() string { return "parent" }

// Group implements Strategy.
func (b ByParent) Group(issues []jira.Issue) []Group {
	isEpic := map[string]bool{}
	for _, t := range b.EpicTypes {
		isEpic[strings.ToLower(t)] = true
	}

	var epics, orphans []jira.Issue
	parentKeys := []string{}
	parentBuckets := map[string][]jira.Issue{}

	for _, is := range issues {
		if isEpic[strings.ToLower(is.Type.Name)] {
			epics = append(epics, is)
			continue
		}
		if is.ParentKey == "" {
			orphans = append(orphans, is)
			continue
		}
		if _, seen := parentBuckets[is.ParentKey]; !seen {
			parentKeys = append(parentKeys, is.ParentKey)
		}
		parentBuckets[is.ParentKey] = append(parentBuckets[is.ParentKey], is)
	}

	sort.Strings(parentKeys)
	sortByPriorityDesc(epics)
	sortByPriorityDesc(orphans)
	for k, v := range parentBuckets {
		sortByPriorityDesc(v)
		parentBuckets[k] = v
	}

	out := make([]Group, 0, 2+len(parentKeys))
	if len(epics) > 0 {
		out = append(out, Group{Key: "Epics", Issues: epics})
	}
	for _, k := range parentKeys {
		bucket := parentBuckets[k]
		label := k
		for _, is := range bucket {
			if is.ParentSummary != "" {
				label = k + "  " + is.ParentSummary
				break
			}
		}
		out = append(out, Group{Key: label, Issues: bucket})
	}
	if len(orphans) > 0 {
		out = append(out, Group{Key: "No epic", Issues: orphans})
	}
	return out
}

// ByName returns the strategy registered under name (case-insensitive).
// Empty or unknown names fall back to ByStatus, matching the config default.
// epicTypes is used by the "parent" strategy to distinguish epic rows from
// child issues; other strategies ignore it.
func ByName(name string, epicTypes []string) Strategy {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "priority":
		return ByPriority{}
	case "parent", "epic":
		// Legacy "epic" config value mapped to the same parent-aware strategy.
		return ByParent{EpicTypes: epicTypes}
	}
	return ByStatus{}
}

// PriorityIcon maps a Jira priority name to its Unicode icon (per spec §4).
// Unknown priorities yield a single space so the column still aligns.
func PriorityIcon(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "highest":
		return "🔥"
	case "high":
		return "●"
	case "medium":
		return "◐"
	case "low":
		return "◌"
	case "lowest":
		return "·"
	}
	return " "
}

// ApplySort reorders the issues inside each Group in place using s. When
// desc is true the comparator is reversed, producing a "biggest/newest/Z
// first" reading. A nil Sort is a no-op so legacy callers that haven't
// adopted the Sort axis keep their strategy-defined order.
func ApplySort(groups []Group, s Sort, desc bool) {
	if s == nil {
		return
	}
	for gi := range groups {
		items := groups[gi].Issues
		sort.SliceStable(items, func(i, j int) bool {
			if desc {
				return s.Less(items[j], items[i])
			}
			return s.Less(items[i], items[j])
		})
	}
}
