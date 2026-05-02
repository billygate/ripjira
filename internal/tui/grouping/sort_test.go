package grouping

import (
	"testing"
	"time"

	"github.com/billygate/ripjira/internal/jira"
)

func mkIssue(key, summary string, prioName string, created, updated time.Time) jira.Issue {
	return jira.Issue{
		Key:      key,
		Summary:  summary,
		Priority: jira.Priority{Name: prioName},
		Created:  created,
		Updated:  updated,
	}
}

func TestSortByName_FallbackPriority(t *testing.T) {
	if got := SortByName("nope").Name(); got != "priority" {
		t.Fatalf("SortByName(unknown) = %q, want priority", got)
	}
}

func TestSortByName_Recognised(t *testing.T) {
	cases := []string{"priority", "updated", "created", "key", "summary"}
	for _, name := range cases {
		if got := SortByName(name).Name(); got != name {
			t.Fatalf("SortByName(%q).Name() = %q", name, got)
		}
	}
}

func TestByPrioritySort_AscendingIsHighestFirst(t *testing.T) {
	a := mkIssue("A-1", "a", "Highest", time.Time{}, time.Time{})
	b := mkIssue("A-2", "b", "Lowest", time.Time{}, time.Time{})
	if !(ByPrioritySort{}).Less(a, b) {
		t.Fatalf("Less(Highest, Lowest) = false, want true")
	}
	if (ByPrioritySort{}).Less(b, a) {
		t.Fatalf("Less(Lowest, Highest) = true, want false")
	}
}

func TestByUpdatedSort_AscendingIsOldestFirst(t *testing.T) {
	older := mkIssue("A-1", "x", "Medium", time.Time{}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := mkIssue("A-2", "y", "Medium", time.Time{}, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if !(ByUpdatedSort{}).Less(older, newer) {
		t.Fatalf("Less(older, newer) = false, want true")
	}
}

func TestByCreatedSort_AscendingIsOldestFirst(t *testing.T) {
	older := mkIssue("A-1", "x", "Medium", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	newer := mkIssue("A-2", "y", "Medium", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if !(ByCreatedSort{}).Less(older, newer) {
		t.Fatalf("Less(older, newer) = false, want true")
	}
}

func TestByKeySort_NaturalOrder(t *testing.T) {
	a := mkIssue("PROJ-2", "x", "Medium", time.Time{}, time.Time{})
	b := mkIssue("PROJ-10", "y", "Medium", time.Time{}, time.Time{})
	if !(ByKeySort{}).Less(a, b) {
		t.Fatalf("Less(PROJ-2, PROJ-10) = false, want true (natural sort)")
	}
}

func TestBySummarySort_CaseInsensitive(t *testing.T) {
	a := mkIssue("X-1", "alpha", "Medium", time.Time{}, time.Time{})
	b := mkIssue("X-2", "Beta", "Medium", time.Time{}, time.Time{})
	if !(BySummarySort{}).Less(a, b) {
		t.Fatalf("Less(alpha, Beta) = false, want true")
	}
}

func TestApplySort_AscReordersWithinGroup(t *testing.T) {
	g := []Group{{
		Key: "Status A",
		Issues: []jira.Issue{
			mkIssue("X-2", "z", "Medium", time.Time{}, time.Time{}),
			mkIssue("X-10", "a", "Medium", time.Time{}, time.Time{}),
			mkIssue("X-1", "m", "Medium", time.Time{}, time.Time{}),
		},
	}}
	ApplySort(g, ByKeySort{}, false)
	got := []string{g[0].Issues[0].Key, g[0].Issues[1].Key, g[0].Issues[2].Key}
	want := []string{"X-1", "X-2", "X-10"}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ApplySort asc: got %v, want %v", got, want)
		}
	}
}

func TestApplySort_DescReversesOrder(t *testing.T) {
	g := []Group{{
		Key: "Status A",
		Issues: []jira.Issue{
			mkIssue("X-1", "a", "Medium", time.Time{}, time.Time{}),
			mkIssue("X-2", "b", "Medium", time.Time{}, time.Time{}),
			mkIssue("X-10", "c", "Medium", time.Time{}, time.Time{}),
		},
	}}
	ApplySort(g, ByKeySort{}, true)
	got := []string{g[0].Issues[0].Key, g[0].Issues[1].Key, g[0].Issues[2].Key}
	want := []string{"X-10", "X-2", "X-1"}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ApplySort desc: got %v, want %v", got, want)
		}
	}
}

func TestApplySort_NilSortIsNoOp(t *testing.T) {
	g := []Group{{
		Key:    "g",
		Issues: []jira.Issue{mkIssue("Z-2", "a", "Medium", time.Time{}, time.Time{}), mkIssue("Z-1", "b", "Medium", time.Time{}, time.Time{})},
	}}
	ApplySort(g, nil, false)
	if g[0].Issues[0].Key != "Z-2" {
		t.Fatalf("nil Sort should be a no-op, got %q first", g[0].Issues[0].Key)
	}
}
