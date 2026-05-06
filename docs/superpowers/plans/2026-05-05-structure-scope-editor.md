# Visual Structure Scope Editor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a structure-wide `Scope` filter and a visual TUI editor for it, AND-applied to every section.

**Architecture:** Reuse `SectionFilter`/`FilterClause`. Add a single `Scope` field to `Structure`, AND-match it in `apply.go`. New pure-UI overlay `ScopeEditor` in `internal/tui/overlays/`, opened from the Structures picker (`\` then `e`). UI ↔ structure conversion lives in `internal/tui/structureadapter`. Value suggestions come from currently-loaded issues via a new helper.

**Tech Stack:** Go, Bubble Tea, lipgloss, gopkg.in/yaml.v3, teatest.

**Spec:** `docs/superpowers/specs/2026-05-05-structure-scope-editor-design.md`

---

## File Map

- `internal/structure/types.go` — add `Scope SectionFilter` to `Structure`.
- `internal/structure/apply.go` — AND `structure.Scope` to each section.
- `internal/structure/types_test.go` — YAML round-trip with/without scope.
- `internal/structure/store_test.go` — save/load preserves Scope.
- `internal/structure/apply_test.go` — scope behavior table.
- `internal/tui/structureadapter/scope_rows.go` (new) — `ScopeRow` + conversion helpers.
- `internal/tui/structureadapter/scope_rows_test.go` (new) — round-trip table.
- `internal/tui/scope_values.go` (new) — `UniqueValues(issues, field)`.
- `internal/tui/scope_values_test.go` (new) — unit tests.
- `internal/tui/overlays/scope_editor.go` (new) — overlay (list + row-editor).
- `internal/tui/overlays/scope_editor_test.go` (new) — teatest golden frames.
- `internal/tui/overlays/structures.go` — emit `StructureEditScopeMsg` on `e`.
- `internal/tui/overlays/structures_test.go` — assertion for the new message.
- `internal/tui/app.go` — wire `StructureEditScopeMsg` and `ScopeSavedMsg`.
- `internal/tui/app_test.go` — integration: `e` opens editor, save persists.
- `README.md` — keymap addendum (overlay-local `e edit scope`).

---

### Task 1: Add `Scope` field to `Structure`

**Files:**
- Modify: `internal/structure/types.go`
- Test: `internal/structure/types_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/structure/types_test.go`:

```go
func TestStructure_YAMLRoundTrip_WithScope(t *testing.T) {
	in := Structure{
		ID:   "s1",
		Name: "n",
		Scope: SectionFilter{
			"labels": {In: []string{"Q12026", "Q22026"}},
		},
		Sections: []Section{{Title: "T"}},
	}
	out, err := yaml.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "scope:") {
		t.Fatalf("expected scope in YAML, got:\n%s", out)
	}
	var got Structure
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.Scope, in.Scope) {
		t.Fatalf("scope round-trip mismatch:\nwant %#v\ngot  %#v", in.Scope, got.Scope)
	}
}

func TestStructure_YAMLRoundTrip_EmptyScopeOmitted(t *testing.T) {
	in := Structure{ID: "s1", Name: "n", Sections: []Section{{Title: "T"}}}
	out, err := yaml.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "scope:") {
		t.Fatalf("expected scope omitted, got:\n%s", out)
	}
}
```

If `strings` / `reflect` imports not present, add them.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/structure/ -run YAMLRoundTrip_WithScope -v`
Expected: FAIL — field `Scope` undefined.

- [ ] **Step 3: Add Scope field to Structure**

Modify `internal/structure/types.go`, in the `Structure` struct, add `Scope` between `Sections` and `Source`:

```go
type Structure struct {
	ID         string        `json:"id" yaml:"id"`
	ProjectKey string        `json:"project_key" yaml:"project_key,omitempty"`
	Name       string        `json:"name" yaml:"name"`
	Sections   []Section     `json:"sections" yaml:"sections"`
	Scope      SectionFilter `json:"scope,omitempty" yaml:"scope,omitempty"`
	Source     string        `json:"source,omitempty" yaml:"source,omitempty"`
	CreatedAt  time.Time     `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt  time.Time     `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/structure/ -v`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/structure/types.go internal/structure/types_test.go
git commit -m "feat(structure): add Scope field to Structure (YAML round-trip)"
```

---

### Task 2: Apply Scope in `apply.go`

**Files:**
- Modify: `internal/structure/apply.go`
- Test: `internal/structure/apply_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/structure/apply_test.go`:

```go
func TestApply_ScopeAndsAcrossSections(t *testing.T) {
	issues := []Issue{
		fakeIssue{"labels": "Q12026", "status": "Open"},
		fakeIssue{"labels": "Other", "status": "Open"},
		fakeIssue{"labels": "Q22026", "status": "Done"},
	}
	s := &Structure{
		Scope: SectionFilter{"labels": {In: []string{"Q12026", "Q22026"}}},
		Sections: []Section{
			{Title: "Open", Filter: SectionFilter{"status": In("Open")}},
			{Title: "Done", Filter: SectionFilter{"status": In("Done")}},
		},
	}
	got := Apply(issues, s)
	if len(got) != 2 {
		t.Fatalf("want 2 sections, got %d", len(got))
	}
	if len(got[0].Issues) != 1 || got[0].Issues[0].Field("labels") != "Q12026" {
		t.Fatalf("Open section did not honor scope: %+v", got[0].Issues)
	}
	if len(got[1].Issues) != 1 || got[1].Issues[0].Field("labels") != "Q22026" {
		t.Fatalf("Done section did not honor scope: %+v", got[1].Issues)
	}
}

func TestApply_EmptyScopeIsNoop(t *testing.T) {
	issues := []Issue{
		fakeIssue{"status": "Open"},
		fakeIssue{"status": "Done"},
	}
	s := &Structure{
		Sections: []Section{
			{Title: "All", Filter: nil},
		},
	}
	got := Apply(issues, s)
	if len(got) != 1 || len(got[0].Issues) != 2 {
		t.Fatalf("empty scope altered output: %+v", got)
	}
}
```

If `fakeIssue` does not exist in this file, look at existing tests for the helper used (likely `mapIssue` or similar). Use whatever the existing tests use; if nothing matches, add at the bottom of the test file:

```go
type fakeIssue map[string]string

func (f fakeIssue) Field(name string) string { return f[name] }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/structure/ -run TestApply_Scope -v`
Expected: scope test fails (scope ignored), empty-scope test passes.

- [ ] **Step 3: AND scope into filterIssues**

Modify `internal/structure/apply.go`. Replace the `Apply` function:

```go
func Apply(issues []Issue, s *Structure) []AppliedSection {
	scoped := issues
	if len(s.Scope) > 0 {
		scoped = make([]Issue, 0, len(issues))
		for _, is := range issues {
			if matchesFilter(is, s.Scope) {
				scoped = append(scoped, is)
			}
		}
	}
	out := make([]AppliedSection, 0, len(s.Sections))
	for i := range s.Sections {
		sec := &s.Sections[i]
		matched := filterIssues(scoped, sec.Filter, sec.AnyOf)
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/structure/ -v`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/structure/apply.go internal/structure/apply_test.go
git commit -m "feat(structure): apply Scope as AND filter across all sections"
```

---

### Task 3: Store round-trip with Scope

**Files:**
- Test: `internal/structure/store_test.go`

- [ ] **Step 1: Add a save/load test**

Append to `internal/structure/store_test.go`:

```go
func TestStore_LoadPreservesScope(t *testing.T) {
	dir := t.TempDir()
	yamlBody := `- id: s1
  name: n
  sections:
    - title: T
  scope:
    labels: [Q12026, Q22026]
`
	if err := os.WriteFile(filepath.Join(dir, "ABC.yml"), []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	got, err := s.Load("ABC")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Skip past built-ins; user structure is the last one.
	user := got[len(got)-1]
	if user.ID != "s1" {
		t.Fatalf("want id s1, got %q", user.ID)
	}
	want := []string{"Q12026", "Q22026"}
	if !reflect.DeepEqual(user.Scope["labels"].In, want) {
		t.Fatalf("scope.labels.in: want %v, got %v", want, user.Scope["labels"].In)
	}
}
```

If imports `os`/`path/filepath`/`reflect` are missing, add them.

- [ ] **Step 2: Run**

Run: `go test ./internal/structure/ -run TestStore_LoadPreservesScope -v`
Expected: PASS (Task 1 already added `Scope` with yaml tag, no further code change needed).

- [ ] **Step 3: Commit**

```bash
git add internal/structure/store_test.go
git commit -m "test(structure): cover scope round-trip through Store.Load"
```

---

### Task 4: `ScopeRow` adapter (UI ↔ SectionFilter)

**Files:**
- Create: `internal/tui/structureadapter/scope_rows.go`
- Create: `internal/tui/structureadapter/scope_rows_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/structureadapter/scope_rows_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/structureadapter/ -run TestScopeRows_RoundTrip -v`
Expected: FAIL — `ScopeRow`/`OpIn` etc. undefined.

- [ ] **Step 3: Implement adapter**

Create `internal/tui/structureadapter/scope_rows.go`:

```go
package structureadapter

import "github.com/billygate/ripjira/internal/structure"

// ScopeOp is the operator chosen in the visual scope editor. One row maps
// 1:1 to a populated FilterClause predicate.
type ScopeOp string

const (
	OpIn       ScopeOp = "in"
	OpNot      ScopeOp = "not"
	OpRegex    ScopeOp = "regex"
	OpContains ScopeOp = "contains"
	OpExists   ScopeOp = "exists"
)

// ScopeRow is the editor's flat view of one field predicate. UI overlays
// operate on []ScopeRow so they don't need to import internal/structure.
type ScopeRow struct {
	Field  string
	Op     ScopeOp
	Values []string // for exists: ["yes"] or ["no"]; for regex/contains: [single]
}

// RowsFromFilter converts a SectionFilter to a deterministic slice of
// ScopeRows (one row per field). Field iteration order is by field name
// for stable rendering.
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

// FilterFromRows converts editor rows back to a SectionFilter. Empty rows
// (no values, or unknown op) are dropped.
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

// sortStrings sorts in place; tiny helper to avoid pulling sort into a
// hot file when the caller already has a small slice.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/structureadapter/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/structureadapter/scope_rows.go internal/tui/structureadapter/scope_rows_test.go
git commit -m "feat(tui): ScopeRow adapter — round-trip between UI rows and SectionFilter"
```

---

### Task 5: `UniqueValues` helper

**Files:**
- Create: `internal/tui/scope_values.go`
- Create: `internal/tui/scope_values_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/scope_values_test.go`:

```go
package tui

import (
	"reflect"
	"testing"

	"github.com/billygate/ripjira/internal/jira"
)

func TestUniqueValues(t *testing.T) {
	issues := []jira.Issue{
		{Key: "A-1", Labels: []string{"alpha", "beta"}, Status: jira.Status{Name: "Open"}, Priority: jira.Priority{Name: "High"}, Assignee: &jira.User{DisplayName: "Alice"}},
		{Key: "A-2", Labels: []string{"beta", "gamma"}, Status: jira.Status{Name: "Done"}, Priority: jira.Priority{Name: "Low"}},
		{Key: "A-3", Labels: nil, Status: jira.Status{Name: "Open"}, Priority: jira.Priority{Name: "High"}, Assignee: &jira.User{DisplayName: "Bob"}},
	}
	cases := []struct {
		field string
		want  []string
	}{
		{"labels", []string{"alpha", "beta", "gamma"}},
		{"status", []string{"Done", "Open"}},
		{"priority", []string{"High", "Low"}},
		{"assignee", []string{"Alice", "Bob"}},
		{"unknown", nil},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			got := UniqueValues(issues, tc.field)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("field %q: want %v, got %v", tc.field, tc.want, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestUniqueValues -v`
Expected: FAIL — `UniqueValues` undefined.

- [ ] **Step 3: Implement**

Create `internal/tui/scope_values.go`:

```go
package tui

import (
	"sort"

	"github.com/billygate/ripjira/internal/jira"
)

// UniqueValues returns a sorted, de-duplicated list of values seen for the
// given logical field across the in-memory issue set. Used by the scope
// editor to power autocomplete suggestions. Unknown fields return nil so
// callers fall back to free input.
func UniqueValues(issues []jira.Issue, field string) []string {
	seen := map[string]struct{}{}
	add := func(s string) {
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}
	for _, is := range issues {
		switch field {
		case "labels":
			for _, l := range is.Labels {
				add(l)
			}
		case "status":
			add(is.Status.Name)
		case "priority":
			add(is.Priority.Name)
		case "issuetype":
			add(is.Type.Name)
		case "assignee":
			if is.Assignee != nil {
				add(is.Assignee.DisplayName)
			}
		case "reporter":
			if is.Reporter != nil {
				add(is.Reporter.DisplayName)
			}
		case "project":
			if i := indexOfDash(is.Key); i > 0 {
				add(is.Key[:i])
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func indexOfDash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			return i
		}
	}
	return -1
}
```

If `jira.Issue` does not expose `Type` / `Reporter` exactly as written, drop those branches. Keep the test in lockstep — only test the fields the production type actually has.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run TestUniqueValues -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/scope_values.go internal/tui/scope_values_test.go
git commit -m "feat(tui): UniqueValues for scope-editor autocomplete suggestions"
```

---

### Task 6: ScopeEditor overlay — list state

**Files:**
- Create: `internal/tui/overlays/scope_editor.go`
- Create: `internal/tui/overlays/scope_editor_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/overlays/scope_editor_test.go`:

```go
package overlays

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"

	"github.com/billygate/ripjira/internal/tui/structureadapter"
	"github.com/billygate/ripjira/internal/tui/styles"
)

func testStyles() styles.Styles { return styles.New(styles.DefaultPalette()) }

func TestScopeEditor_OpensWithRowsAndShowsHint(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey)
	e = e.Show("My Structure", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"Q12026", "Q22026"}},
	}, nil)
	if !e.Visible() {
		t.Fatal("expected visible")
	}
	out := e.View(testStyles())
	if !strings.Contains(out, "labels") || !strings.Contains(out, "Q12026") {
		t.Fatalf("expected labels row in view:\n%s", out)
	}
	if !strings.Contains(out, "+ add row") {
		t.Fatalf("expected add-row affordance:\n%s", out)
	}
}

func TestScopeEditor_DeleteRow(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"x"}},
		{Field: "status", Op: structureadapter.OpNot, Values: []string{"Done"}},
	}, nil)
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got := e.Rows(); len(got) != 1 || got[0].Field != "status" {
		t.Fatalf("after delete cursor=0: want only status, got %#v", got)
	}
}

func TestScopeEditor_SaveEmitsMsg(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"x"}},
	}, nil)
	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	msg := cmd()
	saved, ok := msg.(ScopeSavedMsg)
	if !ok {
		t.Fatalf("want ScopeSavedMsg, got %T", msg)
	}
	if len(saved.Rows) != 1 || saved.Rows[0].Field != "labels" {
		t.Fatalf("unexpected rows: %#v", saved.Rows)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/overlays/ -run TestScopeEditor -v`
Expected: FAIL — type undefined.

- [ ] **Step 3: Implement list state**

Create `internal/tui/overlays/scope_editor.go`:

```go
package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/structureadapter"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// ScopeSavedMsg is emitted when the user accepts the editor with `s`.
// The carrying StructureID is set by the caller on Show so the app
// knows which structure to update.
type ScopeSavedMsg struct {
	StructureID string
	Rows        []structureadapter.ScopeRow
}

// ScopeValuesProvider supplies autocomplete suggestions for a field.
// Returning nil/empty means "no suggestions"; the row editor still
// accepts free input. Implemented by the app on top of UniqueValues.
type ScopeValuesProvider func(field string) []string

// ScopeEditor is the visual editor for a structure's Scope filter.
// It is pure-UI: it operates on []ScopeRow and has no awareness of
// SectionFilter.
type ScopeEditor struct {
	visible      bool
	structureID  string
	title        string
	rows         []structureadapter.ScopeRow
	cursor       int // 0..len(rows) — len(rows) is the "+ add row" affordance
	closeBinding key.Binding
	values       ScopeValuesProvider

	// Row-editor sub-state. Filled in Task 7.
	rowEdit *rowEditState
}

// NewScopeEditor returns a hidden editor. closeKey closes the overlay
// (or the row sub-editor when one is open).
func NewScopeEditor(closeKey key.Binding) ScopeEditor {
	return ScopeEditor{closeBinding: closeKey}
}

// Visible reports whether the overlay is shown.
func (e ScopeEditor) Visible() bool { return e.visible }

// Rows returns the current row set (post-edits, pre-save).
func (e ScopeEditor) Rows() []structureadapter.ScopeRow {
	return append([]structureadapter.ScopeRow(nil), e.rows...)
}

// Show opens the editor with the supplied rows. The provider is used
// for value autocomplete; pass nil for no suggestions. structureID is
// echoed back in ScopeSavedMsg so the caller knows which structure to
// persist.
func (e ScopeEditor) Show(title string, rows []structureadapter.ScopeRow, values ScopeValuesProvider) ScopeEditor {
	e.title = title
	e.rows = append([]structureadapter.ScopeRow(nil), rows...)
	e.cursor = 0
	e.visible = true
	e.values = values
	e.rowEdit = nil
	return e
}

// ShowWithID is Show + sets StructureID for ScopeSavedMsg routing.
func (e ScopeEditor) ShowWithID(id, title string, rows []structureadapter.ScopeRow, values ScopeValuesProvider) ScopeEditor {
	e = e.Show(title, rows, values)
	e.structureID = id
	return e
}

// Hide closes the overlay and clears state.
func (e ScopeEditor) Hide() ScopeEditor {
	return ScopeEditor{closeBinding: e.closeBinding}
}

// Update handles input while visible.
func (e ScopeEditor) Update(msg tea.Msg) (ScopeEditor, tea.Cmd) {
	if !e.visible {
		return e, nil
	}
	if e.rowEdit != nil {
		return e.updateRowEdit(msg)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	switch k.String() {
	case "j", "down":
		if e.cursor < len(e.rows) { // len = "+ add row" line; cursor may sit on it
			e.cursor++
		}
		return e, nil
	case "k", "up":
		if e.cursor > 0 {
			e.cursor--
		}
		return e, nil
	case "d":
		if e.cursor < len(e.rows) {
			e.rows = append(e.rows[:e.cursor], e.rows[e.cursor+1:]...)
			if e.cursor > 0 && e.cursor >= len(e.rows) {
				e.cursor = len(e.rows)
			}
		}
		return e, nil
	case "s":
		id := e.structureID
		rows := append([]structureadapter.ScopeRow(nil), e.rows...)
		hidden := e.Hide()
		return hidden, func() tea.Msg { return ScopeSavedMsg{StructureID: id, Rows: rows} }
	case "a":
		// Add row — opened in Task 7. Stub returns to caller unchanged.
		return e, nil
	case "enter":
		// Edit existing row or add — opened in Task 7. Stub for now.
		return e, nil
	}
	if key.Matches(k, e.closeBinding) {
		return e.Hide(), nil
	}
	return e, nil
}

// View renders the overlay.
func (e ScopeEditor) View(s styles.Styles) string {
	if !e.visible {
		return ""
	}
	if e.rowEdit != nil {
		return e.viewRowEdit(s)
	}
	title := s.OverlayTitle.Render("Scope: " + e.title)
	var lines []string
	for i, r := range e.rows {
		line := scopeRowLine(r)
		if i == e.cursor {
			line = "▶ " + line
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	addLine := "+ add row"
	if e.cursor == len(e.rows) {
		addLine = "▶ " + addLine
	} else {
		addLine = "  " + addLine
	}
	lines = append(lines, s.Muted.Render(addLine))
	hint := s.Muted.Render("enter edit · a add · d delete · s save · " +
		e.closeBinding.Help().Key + " cancel")
	parts := []string{title, ""}
	parts = append(parts, lines...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func scopeRowLine(r structureadapter.ScopeRow) string {
	values := strings.Join(r.Values, ", ")
	return r.Field + "  " + string(r.Op) + "  " + values
}

// rowEditState and the row-editor functions are stubbed in Task 7.
type rowEditState struct{}

func (e ScopeEditor) updateRowEdit(_ tea.Msg) (ScopeEditor, tea.Cmd) { return e, nil }
func (e ScopeEditor) viewRowEdit(_ styles.Styles) string             { return "" }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/overlays/ -run TestScopeEditor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overlays/scope_editor.go internal/tui/overlays/scope_editor_test.go
git commit -m "feat(tui): ScopeEditor overlay — list state with delete/save"
```

---

### Task 7: ScopeEditor — row-edit sub-state

**Files:**
- Modify: `internal/tui/overlays/scope_editor.go`
- Modify: `internal/tui/overlays/scope_editor_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/overlays/scope_editor_test.go`:

```go
func TestScopeEditor_AddRow_TypeFieldOpValues(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", nil, nil)
	// Move cursor to "+ add row" (it's at index 0 when no rows; pressing 'a' anywhere works).
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !e.InRowEdit() {
		t.Fatal("expected row-edit state after 'a'")
	}
	// Type field "labels".
	for _, r := range []rune("labels") {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Tab to operator step.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Operator already defaults to "in"; tab to values.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Type a chip "Q12026" + Enter, then "Q22026" + Enter.
	for _, r := range []rune("Q12026") {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range []rune("Q22026") {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Final Enter accepts the row.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.InRowEdit() {
		t.Fatal("expected row-edit closed after accept")
	}
	rows := e.Rows()
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.Field != "labels" || got.Op != structureadapter.OpIn ||
		!reflect.DeepEqual(got.Values, []string{"Q12026", "Q22026"}) {
		t.Fatalf("unexpected row: %#v", got)
	}
}

func TestScopeEditor_RowEdit_EscCancels(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", nil, nil)
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.InRowEdit() {
		t.Fatal("esc should cancel row edit")
	}
	if len(e.Rows()) != 0 {
		t.Fatal("cancel should not persist any row")
	}
}

func TestScopeEditor_DuplicateFieldRejected(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	e := NewScopeEditor(closeKey).Show("S", []structureadapter.ScopeRow{
		{Field: "labels", Op: structureadapter.OpIn, Values: []string{"x"}},
	}, nil)
	// Move to add-row line then add.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range []rune("labels") {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !e.RowEditHasError() {
		t.Fatal("expected duplicate-field error before tab succeeds")
	}
}
```

Add `reflect` import if not already present.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/overlays/ -run TestScopeEditor -v`
Expected: FAIL — `InRowEdit`, `RowEditHasError` undefined; `'a'` does not enter row-edit.

- [ ] **Step 3: Implement row-edit state**

Replace the stub bottom of `internal/tui/overlays/scope_editor.go` (everything from `type rowEditState struct{}` to end of file) with:

```go
// rowEditStep enumerates the focus position in the row sub-editor.
type rowEditStep int

const (
	stepField rowEditStep = iota
	stepOp
	stepValues
)

// rowEditState holds in-progress values for adding/editing a row.
type rowEditState struct {
	editingIndex int // -1 when adding a new row
	step         rowEditStep
	field        string
	op           structureadapter.ScopeOp
	chips        []string
	textInput    string // current chip-in-progress (in/not) or value (regex/contains)
	existsYes    bool
	errMsg       string
}

// InRowEdit reports whether the row sub-editor is open (used in tests).
func (e ScopeEditor) InRowEdit() bool { return e.rowEdit != nil }

// RowEditHasError reports whether the current row-edit has a validation
// error displayed (duplicate field, invalid regex, etc.).
func (e ScopeEditor) RowEditHasError() bool {
	return e.rowEdit != nil && e.rowEdit.errMsg != ""
}

func (e ScopeEditor) openRowEditForCursor() ScopeEditor {
	if e.cursor < len(e.rows) {
		r := e.rows[e.cursor]
		st := &rowEditState{
			editingIndex: e.cursor,
			step:         stepField,
			field:        r.Field,
			op:           r.Op,
		}
		hydrateValues(st, r)
		e.rowEdit = st
		return e
	}
	e.rowEdit = &rowEditState{editingIndex: -1, step: stepField, op: structureadapter.OpIn}
	return e
}

func hydrateValues(st *rowEditState, r structureadapter.ScopeRow) {
	switch r.Op {
	case structureadapter.OpIn, structureadapter.OpNot:
		st.chips = append([]string(nil), r.Values...)
	case structureadapter.OpRegex, structureadapter.OpContains:
		if len(r.Values) > 0 {
			st.textInput = r.Values[0]
		}
	case structureadapter.OpExists:
		st.existsYes = len(r.Values) > 0 && r.Values[0] == "yes"
	}
}

// Update list-state cases for 'a' and 'enter' need wiring; replace the
// `'a'` and `"enter"` branches in (e ScopeEditor) Update with the real
// behavior:
//
//   case "a":
//   	if e.cursor > len(e.rows) { e.cursor = len(e.rows) }
//   	return e.openRowEditForCursor(), nil
//   case "enter":
//   	return e.openRowEditForCursor(), nil
//
// (Make that change in the existing Update method, NOT here.)

func (e ScopeEditor) updateRowEdit(msg tea.Msg) (ScopeEditor, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	st := e.rowEdit
	switch k.String() {
	case "esc":
		e.rowEdit = nil
		return e, nil
	case "tab":
		if !e.advanceStep() {
			return e, nil
		}
		return e, nil
	case "shift+tab":
		if st.step > stepField {
			st.step--
		}
		return e, nil
	case "enter":
		return e.handleRowEditEnter()
	case "backspace":
		return e.handleRowEditBackspace(), nil
	}
	// Plain character keys.
	if k.Type == tea.KeyRunes {
		return e.handleRowEditRunes(k.Runes), nil
	}
	return e, nil
}

func (e ScopeEditor) advanceStep() bool {
	st := e.rowEdit
	switch st.step {
	case stepField:
		if st.field == "" {
			st.errMsg = "field required"
			return false
		}
		if e.fieldDuplicate(st.field, st.editingIndex) {
			st.errMsg = "another row already filters " + st.field
			return false
		}
		st.errMsg = ""
		st.step = stepOp
	case stepOp:
		st.errMsg = ""
		st.step = stepValues
	case stepValues:
		// no-op; Enter accepts.
	}
	return true
}

func (e ScopeEditor) fieldDuplicate(field string, ignoreIndex int) bool {
	for i, r := range e.rows {
		if i == ignoreIndex {
			continue
		}
		if r.Field == field {
			return true
		}
	}
	return false
}

func (e ScopeEditor) handleRowEditEnter() (ScopeEditor, tea.Cmd) {
	st := e.rowEdit
	if st.step != stepValues {
		// Treat Enter on earlier steps as Tab.
		if !e.advanceStep() {
			return e, nil
		}
		return e, nil
	}
	switch st.op {
	case structureadapter.OpIn, structureadapter.OpNot:
		if st.textInput != "" {
			st.chips = append(st.chips, st.textInput)
			st.textInput = ""
			return e, nil
		}
		if len(st.chips) == 0 {
			st.errMsg = "at least one value required"
			return e, nil
		}
	case structureadapter.OpRegex:
		if st.textInput == "" {
			st.errMsg = "regex required"
			return e, nil
		}
		if _, err := compileRegex(st.textInput); err != nil {
			st.errMsg = "invalid regex: " + err.Error()
			return e, nil
		}
	case structureadapter.OpContains:
		if st.textInput == "" {
			st.errMsg = "value required"
			return e, nil
		}
	}
	row := materializeRow(st)
	if st.editingIndex >= 0 && st.editingIndex < len(e.rows) {
		e.rows[st.editingIndex] = row
	} else {
		e.rows = append(e.rows, row)
	}
	e.rowEdit = nil
	return e, nil
}

func materializeRow(st *rowEditState) structureadapter.ScopeRow {
	switch st.op {
	case structureadapter.OpIn, structureadapter.OpNot:
		return structureadapter.ScopeRow{Field: st.field, Op: st.op, Values: append([]string(nil), st.chips...)}
	case structureadapter.OpRegex, structureadapter.OpContains:
		return structureadapter.ScopeRow{Field: st.field, Op: st.op, Values: []string{st.textInput}}
	case structureadapter.OpExists:
		v := "no"
		if st.existsYes {
			v = "yes"
		}
		return structureadapter.ScopeRow{Field: st.field, Op: st.op, Values: []string{v}}
	}
	return structureadapter.ScopeRow{Field: st.field, Op: st.op}
}

func (e ScopeEditor) handleRowEditBackspace() ScopeEditor {
	st := e.rowEdit
	switch st.step {
	case stepField:
		if n := len(st.field); n > 0 {
			st.field = st.field[:n-1]
		}
	case stepValues:
		switch st.op {
		case structureadapter.OpIn, structureadapter.OpNot:
			if st.textInput != "" {
				st.textInput = st.textInput[:len(st.textInput)-1]
			} else if n := len(st.chips); n > 0 {
				st.chips = st.chips[:n-1]
			}
		case structureadapter.OpRegex, structureadapter.OpContains:
			if n := len(st.textInput); n > 0 {
				st.textInput = st.textInput[:n-1]
			}
		}
	}
	return e
}

func (e ScopeEditor) handleRowEditRunes(rs []rune) ScopeEditor {
	st := e.rowEdit
	switch st.step {
	case stepField:
		st.field += string(rs)
		st.errMsg = ""
	case stepOp:
		switch string(rs) {
		case "h", "left":
			st.op = prevOp(st.op)
		case "l", "right":
			st.op = nextOp(st.op)
		}
	case stepValues:
		switch st.op {
		case structureadapter.OpIn, structureadapter.OpNot, structureadapter.OpRegex, structureadapter.OpContains:
			st.textInput += string(rs)
		case structureadapter.OpExists:
			switch string(rs) {
			case "y":
				st.existsYes = true
			case "n":
				st.existsYes = false
			case " ":
				st.existsYes = !st.existsYes
			}
		}
	}
	return e
}

var opCycle = []structureadapter.ScopeOp{
	structureadapter.OpIn, structureadapter.OpNot,
	structureadapter.OpContains, structureadapter.OpRegex,
	structureadapter.OpExists,
}

func nextOp(o structureadapter.ScopeOp) structureadapter.ScopeOp {
	for i, x := range opCycle {
		if x == o {
			return opCycle[(i+1)%len(opCycle)]
		}
	}
	return opCycle[0]
}

func prevOp(o structureadapter.ScopeOp) structureadapter.ScopeOp {
	for i, x := range opCycle {
		if x == o {
			return opCycle[(i-1+len(opCycle))%len(opCycle)]
		}
	}
	return opCycle[0]
}

// compileRegex is split out so tests can stub validation rules cheaply.
func compileRegex(pattern string) (any, error) {
	// Use the same regexp engine the structure package uses.
	return regexpCompile(pattern)
}

func (e ScopeEditor) viewRowEdit(s styles.Styles) string {
	st := e.rowEdit
	title := s.OverlayTitle.Render("Edit row")
	var lines []string
	mark := func(step rowEditStep, label string) string {
		if st.step == step {
			return s.OverlayTitle.Render("▶ " + label)
		}
		return s.Muted.Render("  " + label)
	}
	lines = append(lines, mark(stepField, "field: "+st.field))
	lines = append(lines, mark(stepOp, "operator: "+string(st.op)))
	switch st.op {
	case structureadapter.OpIn, structureadapter.OpNot:
		chipsLine := strings.Join(st.chips, ", ")
		if st.textInput != "" {
			if chipsLine != "" {
				chipsLine += ", "
			}
			chipsLine += st.textInput + "▏"
		} else if st.step == stepValues {
			chipsLine += "▏"
		}
		lines = append(lines, mark(stepValues, "values: "+chipsLine))
		if st.step == stepValues {
			lines = append(lines, s.Muted.Render("  enter add chip · backspace remove · enter accept"))
		}
	case structureadapter.OpRegex, structureadapter.OpContains:
		v := st.textInput
		if st.step == stepValues {
			v += "▏"
		}
		lines = append(lines, mark(stepValues, "value: "+v))
	case structureadapter.OpExists:
		v := "no"
		if st.existsYes {
			v = "yes"
		}
		lines = append(lines, mark(stepValues, "exists: "+v+"  (y/n/space)"))
	}
	if st.errMsg != "" {
		lines = append(lines, "", s.Muted.Render("error: "+st.errMsg))
	}
	hint := s.Muted.Render("tab/shift+tab navigate · enter accept · esc cancel")
	parts := []string{title, ""}
	parts = append(parts, lines...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
```

Now wire `'a'` and `"enter"` in the existing `Update` method (replace the two stub branches you wrote in Task 6):

```go
case "a":
	if e.cursor > len(e.rows) {
		e.cursor = len(e.rows)
	}
	return e.openRowEditForCursor(), nil
case "enter":
	return e.openRowEditForCursor(), nil
```

Add at the top of the file (with other imports):

```go
import "regexp"
```

And add a small helper near `compileRegex`:

```go
func regexpCompile(p string) (*regexp.Regexp, error) { return regexp.Compile(p) }
```

(`compileRegex` returns `any` to keep the type out of overlay's public API.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/overlays/ -run TestScopeEditor -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overlays/scope_editor.go internal/tui/overlays/scope_editor_test.go
git commit -m "feat(tui): ScopeEditor row sub-editor — field/op/values with validation"
```

---

### Task 8: Wire `e` from Structures overlay

**Files:**
- Modify: `internal/tui/overlays/structures.go`
- Modify: `internal/tui/overlays/structures_test.go`

- [ ] **Step 1: Write the failing test**

Open `internal/tui/overlays/structures_test.go` (create if missing). Append:

```go
func TestStructures_EmitsEditScopeOnE(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	p := NewStructures(closeKey).Show([]StructureEntry{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}, "b")
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("expected cmd from 'e'")
	}
	msg := cmd()
	got, ok := msg.(StructureEditScopeMsg)
	if !ok {
		t.Fatalf("want StructureEditScopeMsg, got %T", msg)
	}
	if got.ID != "b" {
		t.Fatalf("want ID b, got %q", got.ID)
	}
}

func TestStructures_EditScopeRejectsReadOnly(t *testing.T) {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	p := NewStructures(closeKey).Show([]StructureEntry{
		{ID: "a", Name: "A", ReadOnly: true},
	}, "a")
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("expected cmd from 'e' even on read-only (toast)")
	}
	if _, ok := cmd().(StructureEditScopeMsg); ok {
		t.Fatal("read-only structure should not emit StructureEditScopeMsg")
	}
}
```

If the test file doesn't exist, top of file:

```go
package overlays

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/overlays/ -run TestStructures_Emits -v`
Expected: FAIL — `StructureEditScopeMsg` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/overlays/structures.go`, near `StructureSelectedMsg`:

```go
// StructureEditScopeMsg fires when the user presses `e` on a writable
// structure entry. The root model opens the scope editor in response.
type StructureEditScopeMsg struct {
	ID string
}

// StructureReadOnlyMsg fires when the user presses `e` on a read-only
// entry — the root model surfaces a toast and does not open the editor.
type StructureReadOnlyMsg struct {
	ID string
}
```

In `Update`, after the `"enter"` case and before the `closeBinding` block, add:

```go
case "e":
	if len(p.entries) == 0 {
		return p, nil
	}
	cur := p.entries[p.cursor]
	if cur.ReadOnly {
		id := cur.ID
		return p, func() tea.Msg { return StructureReadOnlyMsg{ID: id} }
	}
	id := cur.ID
	hidden := p.Hide()
	return hidden, func() tea.Msg { return StructureEditScopeMsg{ID: id} }
```

Update the footer hint string in `View`:

```go
hint := s.Muted.Render("enter select · e edit scope · j/k navigate · " +
	p.closeBinding.Help().Key + " " + p.closeBinding.Help().Desc)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/overlays/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/overlays/structures.go internal/tui/overlays/structures_test.go
git commit -m "feat(tui): emit StructureEditScopeMsg on 'e' from structures picker"
```

---

### Task 9: App wiring — open editor and persist on save

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

- [ ] **Step 1: Locate hook points**

Find these landmarks:
- The struct that holds the model (search `m.structures` or `Structures` field). Add a sibling field `scopeEditor overlays.ScopeEditor`.
- `func New(...)` or `NewModel(...)` that constructs the model. Add `scopeEditor: overlays.NewScopeEditor(keymap.CloseOverlay)`.
- The top-level `Update(msg tea.Msg)`. Add a switch case for `overlays.StructureEditScopeMsg`, `overlays.StructureReadOnlyMsg`, `overlays.ScopeSavedMsg`. Forward `msg` to `m.scopeEditor` when it is visible (place this before other overlays consume the msg).
- The `View()` composition. Render `m.scopeEditor.View(m.styles)` on top of other overlays when visible.
- A function that persists structure changes to disk. If none exists, use `Store.Path(projectKey)` + a YAML write — see Task 10.

Run: `grep -n "structures \|m.structures\|overlays.Structures\b" internal/tui/app.go`

- [ ] **Step 2: Write the failing integration test**

Append to `internal/tui/app_test.go`:

```go
func TestApp_ScopeEditor_OpensFromStructuresPicker(t *testing.T) {
	m := newTestModelWithStructures(t, []structure.Structure{
		{ID: "u1", Name: "User One", ProjectKey: "ABC", Sections: []structure.Section{{Title: "All"}}},
	})
	// Open structures picker.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	// Press 'e' on the highlighted user structure (last entry; built-ins first).
	for m.structures.Cursor() < lastIndex(m) {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !m.scopeEditor.Visible() {
		t.Fatal("scope editor should be visible after 'e'")
	}
}

func TestApp_ScopeSaved_PersistsToStore(t *testing.T) {
	dir := t.TempDir()
	original := structure.Structure{
		ID: "u1", Name: "User One", ProjectKey: "ABC",
		Sections: []structure.Section{{Title: "All"}},
	}
	writeStructureFile(t, dir, "ABC", []structure.Structure{original})
	m := newTestModelWithStore(t, dir, "ABC", original.ID)
	saved := overlays.ScopeSavedMsg{
		StructureID: "u1",
		Rows: []structureadapter.ScopeRow{
			{Field: "labels", Op: structureadapter.OpIn, Values: []string{"Q12026"}},
		},
	}
	m, _ = m.Update(saved)
	store := structure.NewStore(dir)
	got, err := store.FindByID("ABC", "u1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Scope["labels"].In[0] != "Q12026" {
		t.Fatalf("scope not persisted: %#v", got.Scope)
	}
}
```

`newTestModelWithStructures`, `newTestModelWithStore`, `writeStructureFile`, `lastIndex` — write whatever helpers fit alongside existing test helpers in `app_test.go`. If equivalents already exist (look for `newTestModel`), prefer extending them. Imports: `structureadapter "github.com/billygate/ripjira/internal/tui/structureadapter"`.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/tui/ -run TestApp_Scope -v`
Expected: FAIL — handlers not yet wired.

- [ ] **Step 4: Implement wiring**

In the model struct, add field:

```go
scopeEditor overlays.ScopeEditor
```

In the constructor (after `m.structures = overlays.NewStructures(...)`):

```go
m.scopeEditor = overlays.NewScopeEditor(m.keymap.CloseOverlay)
```

In `Update`, add early forwarding when the overlay is visible (before global key handlers):

```go
if m.scopeEditor.Visible() {
	var cmd tea.Cmd
	m.scopeEditor, cmd = m.scopeEditor.Update(msg)
	return m, cmd
}
```

Add cases for the three messages. Place near the existing `overlays.StructureSelectedMsg` handler:

```go
case overlays.StructureEditScopeMsg:
	str, err := m.store.FindByID(m.projectKey, msg.ID)
	if err != nil {
		return m, m.toast.Push("structure not found")
	}
	if str.IsReadOnly() {
		return m, m.toast.Push("structure is read-only")
	}
	rows := structureadapter.RowsFromFilter(str.Scope)
	provider := func(field string) []string { return UniqueValues(m.allIssues(), field) }
	m.scopeEditor = m.scopeEditor.ShowWithID(str.ID, str.Name, rows, provider)
	return m, nil

case overlays.StructureReadOnlyMsg:
	return m, m.toast.Push("structure is read-only")

case overlays.ScopeSavedMsg:
	if err := m.applyScopeAndPersist(msg.StructureID, msg.Rows); err != nil {
		return m, m.toast.Push("save failed: " + err.Error())
	}
	return m, m.reapplyActiveStructure()
```

`m.allIssues()` — return whatever in-memory issue slice the model already has (search for a field like `m.issues`, `m.allIssues`, or `m.list.Items()` adapted to `[]jira.Issue`). If no single accessor exists, add one that returns the field used by `feedList`.

`m.toast.Push(...)` — use the existing toast helper. If the API differs, match the existing call sites for toast usage in the same file.

`m.reapplyActiveStructure()` — return `tea.Cmd` that re-runs the structure apply pipeline; reuse whatever exists for `StructureSelectedMsg` handling.

Add `applyScopeAndPersist` — see Task 10.

In `View`, render the overlay above existing overlays:

```go
if m.scopeEditor.Visible() {
	return overlayLayer(base, m.scopeEditor.View(m.styles))
}
```

(Use the same overlay-stacking helper used for `m.structures.View(...)`.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tui/ -run TestApp_Scope -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat(tui): wire ScopeEditor into app — open from picker, persist on save"
```

---

### Task 10: Persist Scope to YAML

**Files:**
- Modify: `internal/structure/store.go`
- Test: `internal/structure/store_test.go`
- Modify: `internal/tui/app.go` (uses Save)

- [ ] **Step 1: Write the failing test**

Append to `internal/structure/store_test.go`:

```go
func TestStore_SaveStructure_RoundTripsScope(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	in := Structure{
		ID: "u1", Name: "n", ProjectKey: "ABC",
		Sections: []Section{{Title: "T"}},
		Scope:    SectionFilter{"labels": {In: []string{"Q12026"}}},
	}
	if err := s.SaveStructure(&in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.FindByID("ABC", "u1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Scope["labels"].In[0] != "Q12026" {
		t.Fatalf("scope lost: %#v", got.Scope)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/structure/ -run TestStore_SaveStructure -v`
Expected: FAIL — `SaveStructure` undefined.

- [ ] **Step 3: Implement**

Append to `internal/structure/store.go`:

```go
// SaveStructure writes (or creates) the project YAML containing s. If
// the file already has structures, the matching ID is updated in place;
// otherwise s is appended. ID and ProjectKey must be non-empty.
func (s *Store) SaveStructure(in *Structure) error {
	if in.ID == "" || in.ProjectKey == "" {
		return fmt.Errorf("save structure: ID and ProjectKey required")
	}
	path := s.Path(in.ProjectKey)
	var existing []Structure
	if body, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(body, &existing); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated := false
	for i := range existing {
		if existing[i].ID == in.ID {
			existing[i] = *in
			updated = true
			break
		}
	}
	if !updated {
		existing = append(existing, *in)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	body, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Implement `applyScopeAndPersist` in app.go**

Add to `internal/tui/app.go`:

```go
func (m *Model) applyScopeAndPersist(id string, rows []structureadapter.ScopeRow) error {
	str, err := m.store.FindByID(m.projectKey, id)
	if err != nil {
		return err
	}
	str.Scope = structureadapter.FilterFromRows(rows)
	if err := m.store.SaveStructure(&str); err != nil {
		return err
	}
	if m.activeStructureID == id {
		m.activeStructure = str
	}
	return nil
}
```

If the model field for active structure has a different name (search `activeStructure`), match it.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/structure/store.go internal/structure/store_test.go internal/tui/app.go
git commit -m "feat(structure): SaveStructure with atomic write; persist scope edits"
```

---

### Task 11: README keymap update

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update keymap section**

Open `README.md`, find the keymap table. Add a row beneath the Structures-picker row:

```
| `\` | open structures picker |
| `e` (in picker) | edit scope of highlighted structure |
```

If the table format differs, mimic the existing entries.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: mention 'e edit scope' in structures picker"
```

---

### Task 12: Final integration sanity check

- [ ] **Step 1: Run full suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: PASS.

- [ ] **Step 3: Build**

Run: `make build`
Expected: PASS, binary at `./bin/ripjira`.

- [ ] **Step 4: Manual smoke test**

```
./bin/ripjira
```

Then in the TUI:
- Press `\` — structures picker opens.
- Navigate to a user (non-builtin) structure with `j/k`.
- Press `e` — scope editor opens.
- Press `a`, type `labels`, Tab, ensure `in` selected, Tab, type `Q12026`, Enter, type `Q22026`, Enter, Enter — row added.
- Press `s` — editor closes; list refreshes; only issues with one of those labels are visible.
- Re-open with `\` `e` — the row persists.
- `esc` `esc` — quit.
- Edit `~/.config/ripjira/structures/<PROJECT>.yml` — confirm `scope:` block is present in YAML.
