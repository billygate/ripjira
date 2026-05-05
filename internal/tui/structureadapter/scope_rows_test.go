package structureadapter

import (
	"reflect"
	"testing"

	"github.com/billygate/ripjira/internal/structure"
)

func TestScopeRows_RoundTrip(t *testing.T) {
	yes := true
	cases := []struct {
		name   string
		filter structure.SectionFilter
		rows   []ScopeRow
	}{
		{
			name:   "empty",
			filter: structure.SectionFilter{},
			rows:   nil,
		},
		{
			name:   "in",
			filter: structure.SectionFilter{"labels": {In: []string{"Q12026", "Q22026"}}},
			rows:   []ScopeRow{{Field: "labels", Op: OpIn, Values: []string{"Q12026", "Q22026"}}},
		},
		{
			name:   "not",
			filter: structure.SectionFilter{"status": {Not: []string{"Done"}}},
			rows:   []ScopeRow{{Field: "status", Op: OpNot, Values: []string{"Done"}}},
		},
		{
			name:   "regex",
			filter: structure.SectionFilter{"key": {Regex: "^BIL-"}},
			rows:   []ScopeRow{{Field: "key", Op: OpRegex, Values: []string{"^BIL-"}}},
		},
		{
			name:   "contains",
			filter: structure.SectionFilter{"summary": {Contains: "bug"}},
			rows:   []ScopeRow{{Field: "summary", Op: OpContains, Values: []string{"bug"}}},
		},
		{
			name:   "exists yes",
			filter: structure.SectionFilter{"assignee": {Exists: &yes}},
			rows:   []ScopeRow{{Field: "assignee", Op: OpExists, Values: []string{"yes"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/from-filter", func(t *testing.T) {
			got := RowsFromFilter(tc.filter)
			if !reflect.DeepEqual(got, tc.rows) {
				t.Fatalf("rows: want %#v, got %#v", tc.rows, got)
			}
		})
		t.Run(tc.name+"/to-filter", func(t *testing.T) {
			got := FilterFromRows(tc.rows)
			if len(tc.filter) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.filter) {
				t.Fatalf("filter: want %#v, got %#v", tc.filter, got)
			}
		})
	}
}
