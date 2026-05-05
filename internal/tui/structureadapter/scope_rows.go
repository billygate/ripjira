package structureadapter

import "github.com/billygate/ripjira/internal/structure"

// ScopeOp is the operator chosen in the visual scope editor.
type ScopeOp string

const (
	OpIn       ScopeOp = "in"
	OpNot      ScopeOp = "not"
	OpRegex    ScopeOp = "regex"
	OpContains ScopeOp = "contains"
	OpExists   ScopeOp = "exists"
)

// ScopeRow is the editor's flat view of one field predicate.
type ScopeRow struct {
	Field  string
	Op     ScopeOp
	Values []string
}

// RowsFromFilter converts a SectionFilter to a deterministic slice of
// ScopeRows, sorted by field name for stable rendering.
func RowsFromFilter(f structure.SectionFilter) []ScopeRow {
	if len(f) == 0 {
		return nil
	}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([]ScopeRow, 0, len(keys))
	for _, k := range keys {
		c := f[k]
		row, ok := rowFromClause(k, c)
		if !ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

// FilterFromRows converts editor rows back to a SectionFilter. Empty
// rows are dropped.
func FilterFromRows(rows []ScopeRow) structure.SectionFilter {
	if len(rows) == 0 {
		return nil
	}
	out := make(structure.SectionFilter, len(rows))
	for _, r := range rows {
		clause, ok := clauseFromRow(r)
		if !ok {
			continue
		}
		out[r.Field] = clause
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func rowFromClause(field string, c structure.FilterClause) (ScopeRow, bool) {
	switch {
	case len(c.In) > 0:
		return ScopeRow{Field: field, Op: OpIn, Values: append([]string(nil), c.In...)}, true
	case len(c.Not) > 0:
		return ScopeRow{Field: field, Op: OpNot, Values: append([]string(nil), c.Not...)}, true
	case c.Regex != "":
		return ScopeRow{Field: field, Op: OpRegex, Values: []string{c.Regex}}, true
	case c.Contains != "":
		return ScopeRow{Field: field, Op: OpContains, Values: []string{c.Contains}}, true
	case c.Exists != nil:
		v := "no"
		if *c.Exists {
			v = "yes"
		}
		return ScopeRow{Field: field, Op: OpExists, Values: []string{v}}, true
	}
	return ScopeRow{}, false
}

func clauseFromRow(r ScopeRow) (structure.FilterClause, bool) {
	if r.Field == "" {
		return structure.FilterClause{}, false
	}
	switch r.Op {
	case OpIn:
		if len(r.Values) == 0 {
			return structure.FilterClause{}, false
		}
		return structure.FilterClause{In: append([]string(nil), r.Values...)}, true
	case OpNot:
		if len(r.Values) == 0 {
			return structure.FilterClause{}, false
		}
		return structure.FilterClause{Not: append([]string(nil), r.Values...)}, true
	case OpRegex:
		if len(r.Values) == 0 || r.Values[0] == "" {
			return structure.FilterClause{}, false
		}
		return structure.FilterClause{Regex: r.Values[0]}, true
	case OpContains:
		if len(r.Values) == 0 || r.Values[0] == "" {
			return structure.FilterClause{}, false
		}
		return structure.FilterClause{Contains: r.Values[0]}, true
	case OpExists:
		if len(r.Values) == 0 {
			return structure.FilterClause{}, false
		}
		yes := r.Values[0] == "yes"
		return structure.FilterClause{Exists: &yes}, true
	}
	return structure.FilterClause{}, false
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}
