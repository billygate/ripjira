package grouping_test

import (
	"reflect"
	"testing"

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
		"epic":     "parent",
		"EPIC":     "parent",
		"parent":   "parent",
		"":         "status",
		"unknown":  "status",
	}
	for in, want := range cases {
		if got := grouping.ByName(in, nil).Name(); got != want {
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

// Compile-time assertion that both impls satisfy Strategy.
var (
	_ grouping.Strategy = grouping.ByStatus{}
	_ grouping.Strategy = grouping.ByPriority{}
	_ grouping.Strategy = grouping.ByParent{}
)

func TestByParent_BucketsAndOrder(t *testing.T) {
	mk := func(key, typ, parent, parentSum, prio string) jira.Issue {
		return jira.Issue{
			Key:           key,
			Type:          jira.IssueType{Name: typ},
			Priority:      jira.Priority{Name: prio},
			ParentKey:     parent,
			ParentSummary: parentSum,
		}
	}
	issues := []jira.Issue{
		mk("BIL-2", "Task", "BIL-100", "Setup deploy", "Low"),
		mk("BIL-100", "Epic Feature", "", "", "Medium"),
		mk("BIL-3", "Task", "BIL-200", "", "High"),
		mk("BIL-4", "Task", "", "", "Medium"),
		mk("BIL-1", "Task", "BIL-100", "Setup deploy", "High"),
	}
	got := grouping.ByParent{EpicTypes: []string{"Epic", "Epic Feature"}}.Group(issues)
	keys := make([]string, len(got))
	for i, g := range got {
		keys[i] = g.Key
	}
	want := []string{
		"Epics",
		"BIL-100  Setup deploy",
		"BIL-200",
		"No epic",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("group keys = %#v\nwant %#v", keys, want)
	}
	bucket := got[1].Issues
	if len(bucket) != 2 || bucket[0].Key != "BIL-1" || bucket[1].Key != "BIL-2" {
		t.Fatalf("BIL-100 bucket order: %#v", bucket)
	}
}

func TestByName_ParentReturnsByParent(t *testing.T) {
	s := grouping.ByName("parent", []string{"Epic", "Epic Feature"})
	bp, ok := s.(grouping.ByParent)
	if !ok {
		t.Fatalf("got %T, want ByParent", s)
	}
	if !reflect.DeepEqual(bp.EpicTypes, []string{"Epic", "Epic Feature"}) {
		t.Fatalf("EpicTypes not propagated: %#v", bp.EpicTypes)
	}
}

func TestByName_EpicAliasMapsToParent(t *testing.T) {
	if _, ok := grouping.ByName("epic", nil).(grouping.ByParent); !ok {
		t.Fatalf("legacy 'epic' name should now map to ByParent")
	}
}

func TestByName_StatusFallback(t *testing.T) {
	if _, ok := grouping.ByName("", nil).(grouping.ByStatus); !ok {
		t.Fatalf("empty name should fall back to ByStatus")
	}
}
