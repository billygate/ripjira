package structure

import "testing"

func TestClauseMatches(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name   string
		value  string
		clause FilterClause
		want   bool
	}{
		{"in match", "High", FilterClause{In: []string{"High", "Med"}}, true},
		{"in miss", "Low", FilterClause{In: []string{"High"}}, false},
		{"not match", "Low", FilterClause{Not: []string{"Low"}}, false},
		{"not miss", "High", FilterClause{Not: []string{"Low"}}, true},
		{"regex match", "BIL-42", FilterClause{Regex: `^BIL-\d+$`}, true},
		{"regex miss", "ACME", FilterClause{Regex: `^BIL-`}, false},
		{"contains match", "long bug title", FilterClause{Contains: "bug"}, true},
		{"contains miss", "title", FilterClause{Contains: "bug"}, false},
		{"exists yes", "x", FilterClause{Exists: &yes}, true},
		{"exists no on empty", "", FilterClause{Exists: &yes}, false},
		{"exists false on empty", "", FilterClause{Exists: &no}, true},
		{"empty value matches in:[\"\"]", "", FilterClause{In: []string{""}}, true},
		{"multivalue any-match", "bug, ui, blocker", FilterClause{In: []string{"blocker"}}, true},
	}
	for _, tc := range cases {
		got := clauseMatches(tc.value, &tc.clause)
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestApply_FiltersAndAnyOf(t *testing.T) {
	mk := func(status, prio, labels string) fakeIssue {
		return fakeIssue{"status": status, "priority": prio, "labels": labels}
	}
	issues := []Issue{
		mk("Open", "High", "bug"),
		mk("Open", "Low", "feature"),
		mk("Done", "High", "bug"),
	}
	s := Structure{
		Sections: []Section{{
			Title:  "Open high or any bug",
			Filter: SectionFilter{"status": In("Open")},
			AnyOf: []SectionFilter{
				{"priority": In("High")},
				{"labels": In("bug")},
			},
		}},
	}
	out := Apply(issues, &s)
	if len(out) != 1 || len(out[0].Issues) != 1 {
		t.Fatalf("expected 1 section with 1 issue, got %#v", out)
	}
	if out[0].Issues[0].Field("priority") != "High" {
		t.Fatalf("wrong issue: %#v", out[0].Issues[0])
	}
}

func TestApply_DropsEmptySections(t *testing.T) {
	s := Structure{Sections: []Section{
		{Title: "A", Filter: SectionFilter{"status": In("Nope")}},
		{Title: "B"},
	}}
	out := Apply([]Issue{fakeIssue{"status": "Open"}}, &s)
	if len(out) != 1 || out[0].Title != "B" {
		t.Fatalf("expected only section B, got %#v", out)
	}
}
