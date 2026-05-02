package grouping_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/grouping"
)

func issue(key, statusName, statusCat, priority string) jira.Issue {
	return jira.Issue{
		Key:      key,
		Status:   jira.Status{Name: statusName, Category: statusCat},
		Priority: jira.Priority{Name: priority},
	}
}

func TestByStatus_Name(t *testing.T) {
	if got := (grouping.ByStatus{}).Name(); got != "status" {
		t.Errorf("ByStatus.Name = %q, want %q", got, "status")
	}
}

func TestByPriority_Name(t *testing.T) {
	if got := (grouping.ByPriority{}).Name(); got != "priority" {
		t.Errorf("ByPriority.Name = %q, want %q", got, "priority")
	}
}

func TestByStatus_GroupsByCategoryOrder(t *testing.T) {
	issues := []jira.Issue{
		issue("A-1", "Done", "done", "High"),
		issue("A-2", "In Progress", "indeterminate", "High"),
		issue("A-3", "To Do", "new", "Medium"),
		issue("A-4", "In Progress", "indeterminate", "Low"),
		issue("A-5", "To Do", "new", "Low"),
	}
	groups := grouping.ByStatus{}.Group(issues)

	wantKeys := []string{"To Do", "In Progress", "Done"}
	gotKeys := make([]string, len(groups))
	for i, g := range groups {
		gotKeys[i] = g.Key
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("group keys = %v, want %v", gotKeys, wantKeys)
	}

	if len(groups[0].Issues) != 2 || len(groups[1].Issues) != 2 || len(groups[2].Issues) != 1 {
		t.Errorf("group sizes mismatch: %d/%d/%d",
			len(groups[0].Issues), len(groups[1].Issues), len(groups[2].Issues))
	}

	// Within a group, original order is preserved.
	if groups[0].Issues[0].Key != "A-3" || groups[0].Issues[1].Key != "A-5" {
		t.Errorf("To Do order = %s,%s; want A-3,A-5",
			groups[0].Issues[0].Key, groups[0].Issues[1].Key)
	}
}

func TestByStatus_UnknownCategory(t *testing.T) {
	issues := []jira.Issue{
		issue("A-1", "Pending", "weird", "Low"),
		issue("A-2", "To Do", "new", "Low"),
	}
	groups := grouping.ByStatus{}.Group(issues)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	// known category sorts before unknown
	if groups[0].Key != "To Do" || groups[1].Key != "Pending" {
		t.Errorf("order = %s,%s; want To Do,Pending", groups[0].Key, groups[1].Key)
	}
}

func TestByStatus_EmptyName(t *testing.T) {
	issues := []jira.Issue{{Key: "X-1"}}
	groups := grouping.ByStatus{}.Group(issues)
	if len(groups) != 1 || groups[0].Key != "Unknown" {
		t.Errorf("empty status name not bucketed under Unknown: %+v", groups)
	}
}

func TestByStatus_Empty(t *testing.T) {
	if g := (grouping.ByStatus{}).Group(nil); len(g) != 0 {
		t.Errorf("nil input → %d groups, want 0", len(g))
	}
}

func TestByPriority_GroupsInSpecOrder(t *testing.T) {
	issues := []jira.Issue{
		issue("P-1", "X", "new", "Low"),
		issue("P-2", "X", "new", "Highest"),
		issue("P-3", "X", "new", "High"),
		issue("P-4", "X", "new", "Lowest"),
		issue("P-5", "X", "new", "Medium"),
		issue("P-6", "X", "new", "High"),
	}
	groups := grouping.ByPriority{}.Group(issues)
	wantKeys := []string{"Highest", "High", "Medium", "Low", "Lowest"}
	gotKeys := make([]string, len(groups))
	for i, g := range groups {
		gotKeys[i] = g.Key
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("priority order = %v, want %v", gotKeys, wantKeys)
	}
	for _, g := range groups {
		if g.Key == "High" && len(g.Issues) != 2 {
			t.Errorf("High bucket size = %d, want 2", len(g.Issues))
		}
	}
}

func TestByPriority_UnknownLast(t *testing.T) {
	issues := []jira.Issue{
		issue("P-1", "X", "new", "Whatever"),
		issue("P-2", "X", "new", "High"),
	}
	groups := grouping.ByPriority{}.Group(issues)
	if len(groups) != 2 || groups[0].Key != "High" || groups[1].Key != "Whatever" {
		t.Errorf("unexpected order: %+v", groups)
	}
}

func TestByPriority_EmptyName(t *testing.T) {
	groups := grouping.ByPriority{}.Group([]jira.Issue{{Key: "X"}})
	if len(groups) != 1 || groups[0].Key != "Unknown" {
		t.Errorf("empty priority not bucketed under Unknown: %+v", groups)
	}
}

func TestByName(t *testing.T) {
	cases := map[string]string{
		"status":   "status",
		"STATUS":   "status",
		"priority": "priority",
		"epic":     "epic",
		"EPIC":     "epic",
		"":         "status",
		"unknown":  "status",
	}
	for in, want := range cases {
		if got := grouping.ByName(in).Name(); got != want {
			t.Errorf("ByName(%q).Name() = %q, want %q", in, got, want)
		}
	}
}

func TestPriorityIcon(t *testing.T) {
	cases := map[string]string{
		"Highest": "🔥",
		"high":    "●",
		"MEDIUM":  "◐",
		"Low":     "◌",
		"Lowest":  "·",
		"":        " ",
		"weird":   " ",
	}
	for in, want := range cases {
		if got := grouping.PriorityIcon(in); got != want {
			t.Errorf("PriorityIcon(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestByEpicAndPriority_Name(t *testing.T) {
	if got := (grouping.ByEpicAndPriority{}).Name(); got != "epic" {
		t.Errorf("ByEpicAndPriority.Name = %q, want %q", got, "epic")
	}
}

func issueWithType(key, typeName, priority string) jira.Issue {
	return jira.Issue{
		Key:      key,
		Type:     jira.IssueType{Name: typeName},
		Priority: jira.Priority{Name: priority},
	}
}

func TestByEpicAndPriority_TwoBucketsInOrder(t *testing.T) {
	issues := []jira.Issue{
		issueWithType("E-1", "Epic", "Medium"),
		issueWithType("T-1", "Task", "Low"),
		issueWithType("E-2", "EPIC", "Highest"),
		issueWithType("T-2", "Bug", "High"),
		issueWithType("T-3", "Story", "Lowest"),
	}
	groups := grouping.ByEpicAndPriority{}.Group(issues)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Key != "Epics" || groups[1].Key != "Tasks" {
		t.Errorf("group keys = %s,%s; want Epics,Tasks", groups[0].Key, groups[1].Key)
	}
	wantEpicOrder := []string{"E-2", "E-1"} // Highest before Medium
	for i, want := range wantEpicOrder {
		if groups[0].Issues[i].Key != want {
			t.Errorf("epics[%d] = %s, want %s", i, groups[0].Issues[i].Key, want)
		}
	}
	wantTaskOrder := []string{"T-2", "T-1", "T-3"} // High, Low, Lowest
	for i, want := range wantTaskOrder {
		if groups[1].Issues[i].Key != want {
			t.Errorf("tasks[%d] = %s, want %s", i, groups[1].Issues[i].Key, want)
		}
	}
}

func TestByEpicAndPriority_OmitsEmptyBuckets(t *testing.T) {
	onlyTasks := []jira.Issue{
		issueWithType("T-1", "Task", "High"),
		issueWithType("T-2", "Bug", "Low"),
	}
	groups := grouping.ByEpicAndPriority{}.Group(onlyTasks)
	if len(groups) != 1 || groups[0].Key != "Tasks" {
		t.Fatalf("expected only Tasks bucket, got %+v", groups)
	}

	onlyEpics := []jira.Issue{
		issueWithType("E-1", "Epic", "High"),
	}
	groups = grouping.ByEpicAndPriority{}.Group(onlyEpics)
	if len(groups) != 1 || groups[0].Key != "Epics" {
		t.Fatalf("expected only Epics bucket, got %+v", groups)
	}
}

func TestByEpicAndPriority_TieBreakByUpdatedDesc(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	issues := []jira.Issue{
		{Key: "T-old", Type: jira.IssueType{Name: "Task"}, Priority: jira.Priority{Name: "High"}, Updated: older},
		{Key: "T-new", Type: jira.IssueType{Name: "Task"}, Priority: jira.Priority{Name: "High"}, Updated: newer},
	}
	groups := grouping.ByEpicAndPriority{}.Group(issues)
	if groups[0].Issues[0].Key != "T-new" {
		t.Errorf("tie break: first = %s, want T-new", groups[0].Issues[0].Key)
	}
}

func TestByEpicAndPriority_Empty(t *testing.T) {
	if g := (grouping.ByEpicAndPriority{}).Group(nil); len(g) != 0 {
		t.Errorf("nil input → %d groups, want 0", len(g))
	}
}

// Compile-time assertion that both impls satisfy Strategy.
var (
	_ grouping.Strategy = grouping.ByStatus{}
	_ grouping.Strategy = grouping.ByPriority{}
	_ grouping.Strategy = grouping.ByEpicAndPriority{}
)
