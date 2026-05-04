package structure

import "testing"

func TestValidate_OK(t *testing.T) {
	yes := true
	s := Structure{ID: "u1", Name: "x", Sections: []Section{{
		Title:   "T",
		Filter:  SectionFilter{"status": In("Open")},
		AnyOf:   []SectionFilter{{"labels": {Exists: &yes}}},
		GroupBy: []string{"priority"},
		OrderBy: []SortKey{{Field: SortFieldPriority, Dir: SortDirDesc}},
	}}}
	if err := Validate(&s); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	yes := true
	cases := map[string]Structure{
		"no name":             {ID: "u1", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": In("Open")}}}},
		"no sections":         {ID: "u1", Name: "n"},
		"empty section title": {ID: "u1", Name: "n", Sections: []Section{{Filter: SectionFilter{"status": In("Open")}}}},
		"empty clause":        {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {}}}}},
		"unknown groupby":     {ID: "u1", Name: "n", Sections: []Section{{Title: "T", GroupBy: []string{"weird"}, Filter: SectionFilter{"status": In("X")}}}},
		"bad regex":           {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {Regex: "(["}}}}},
		"bad order field":     {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {Exists: &yes}}, OrderBy: []SortKey{{Field: "weird", Dir: SortDirAsc}}}}},
		"bad order dir":       {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {Exists: &yes}}, OrderBy: []SortKey{{Field: SortFieldPriority, Dir: "sideways"}}}}},
	}
	for name, s := range cases {
		s := s
		t.Run(name, func(t *testing.T) {
			if err := Validate(&s); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}
