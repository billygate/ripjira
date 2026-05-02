package grouping

import (
	"strings"
	"unicode"

	"github.com/billygate/ripjira/internal/jira"
)

// Sort is the within-group ordering applied after a Strategy has bucketed
// issues. Less is defined in ascending terms; the caller flips the
// comparator to render the "desc" direction.
type Sort interface {
	Name() string
	Less(a, b jira.Issue) bool
}

// SortByName resolves a user-facing name into a concrete Sort. Empty or
// unknown names fall back to ByPrioritySort, matching the historical
// list ordering.
func SortByName(name string) Sort {
	switch name {
	case "updated":
		return ByUpdatedSort{}
	case "created":
		return ByCreatedSort{}
	case "key":
		return ByKeySort{}
	case "summary":
		return BySummarySort{}
	case "priority", "":
		return ByPrioritySort{}
	}
	return ByPrioritySort{}
}

// ByPrioritySort orders by priority rank ascending (Highest=0, Lowest=4),
// so ascending order reads as "important first".
type ByPrioritySort struct{}

func (ByPrioritySort) Name() string { return "priority" }
func (ByPrioritySort) Less(a, b jira.Issue) bool {
	return priorityRank(a.Priority.Name) < priorityRank(b.Priority.Name)
}

type ByUpdatedSort struct{}

func (ByUpdatedSort) Name() string { return "updated" }
func (ByUpdatedSort) Less(a, b jira.Issue) bool {
	return a.Updated.Before(b.Updated)
}

type ByCreatedSort struct{}

func (ByCreatedSort) Name() string { return "created" }
func (ByCreatedSort) Less(a, b jira.Issue) bool {
	return a.Created.Before(b.Created)
}

type ByKeySort struct{}

func (ByKeySort) Name() string { return "key" }
func (ByKeySort) Less(a, b jira.Issue) bool {
	return naturalLess(a.Key, b.Key)
}

type BySummarySort struct{}

func (BySummarySort) Name() string { return "summary" }
func (BySummarySort) Less(a, b jira.Issue) bool {
	aL := strings.ToLower(a.Summary)
	bL := strings.ToLower(b.Summary)
	if aL != bL {
		return aL < bL
	}
	return a.Key < b.Key
}

// naturalLess compares "PROJ-2" vs "PROJ-10" so the numeric tail orders
// numerically. Falls back to plain lexical compare when no numeric tail
// or when prefixes differ.
func naturalLess(a, b string) bool {
	pa, na, okA := splitKey(a)
	pb, nb, okB := splitKey(b)
	if okA && okB && pa == pb {
		return na < nb
	}
	return a < b
}

func splitKey(k string) (prefix string, n int, ok bool) {
	for i := len(k) - 1; i >= 0; i-- {
		if !unicode.IsDigit(rune(k[i])) {
			if i == len(k)-1 {
				return k, 0, false
			}
			num := 0
			for _, c := range k[i+1:] {
				num = num*10 + int(c-'0')
			}
			return k[:i+1], num, true
		}
	}
	return "", 0, false
}
