# Structures Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port pilot's "structures" feature to ripjira as a native, schema-compatible, file-backed view layer: a saved set of named *sections* per project, each with a filter, optional OR-alternatives, and group-by axes. Two built-ins (`default`, `inbox`) ship in code; user structures live as YAML on disk and hot-reload on change.

**Architecture:** New domain package `internal/structure/` owns the types, filter evaluator, validator, built-ins, and a file-backed store with `fsnotify` hot-reload. The TUI gains a new tab `STRUCTURES`, a picker overlay (`{` / `}` cycles within the tab), and a list-pane render mode that draws section headers above the existing grouping. No HTTP, no server. JSON tags on the types match pilot's `Structure`/`Section`/`FilterClause` byte-for-byte so external scripts can drop pilot's REST output into `~/.config/ripjira/structures/<PROJECT>.yml` for sync.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3` (already vendored), `github.com/fsnotify/fsnotify` (new dep), Bubble Tea, lipgloss.

---

## File Structure

**New package — `internal/structure/`:**
- `types.go` — `Structure`, `Section`, `SectionFilter`, `FilterClause`, `SortKey`, `SortField`, `SortDir` with JSON+YAML tags. Custom `UnmarshalJSON`/`MarshalJSON` for `FilterClause` (array shorthand vs object).
- `apply.go` — `Apply(issues, structure) []Section` and helpers (`matchesFilter`, `clauseMatches`, `readIssueField`, `splitFieldValue`).
- `validate.go` — `Validate(structure) error`; rejects empty clauses, unknown fields/sort keys, bad regexes, oversized order_by.
- `builtins.go` — `Default()`, `Inbox()`, `IsBuiltinID(id)`, `Builtins(projectKey) []Structure`.
- `store.go` — `Store` type: load/save YAML at `<configDir>/ripjira/structures/<PROJECT>.yml`, plus `Watch(ctx) <-chan Event` over `fsnotify`.
- `*_test.go` — table-driven tests for each.

**Modified files:**
- `go.mod` / `go.sum` — add `github.com/fsnotify/fsnotify`.
- `internal/state/state.go` — add `LastStructure map[string]string` (project → structure id).
- `internal/tui/panes/views.go` — add `ViewStructures` constant and `String()` case.
- `internal/tui/panes/list.go` — new render mode that draws `section.Title` headers above grouped issues; collapsible per-section.
- `internal/tui/app.go` — register tab, key handlers (`{`/`}` for cycle, `S` to open picker), wire structure store load + watcher cmd, persist last-selected.
- `internal/tui/keymap.go` / `keymap_layout.go` — bindings: `CyclePrevStructure` `[`-like, `CycleNextStructure` `]`-like, `OpenStructurePicker`.
- `internal/tui/overlays/structures.go` (new) — list picker (built-ins + user structures), Esc closes, Enter selects.
- `README.md` — short `STRUCTURES` section with example YAML and link to doc.
- `docs/superpowers/specs/2026-04-30-ripjira-design.md` — one paragraph documenting the new tab + storage location (canonical reference).

**Why this shape:** domain logic in `internal/structure/` is pure and testable without any TUI plumbing; UI imports only the package's public types. Same layering as `internal/jira` ↔ `internal/tui/panes`. JSON tags on every domain type let external sync scripts treat the YAML files as a transparent JSON-shaped store.

---

## Out of Scope (deferred)

These are deliberate cuts to keep the plan shippable. Each can be a follow-up plan:

- **In-app filter editor.** v1 is a viewer; users edit YAML by hand or sync from pilot. The Options overlay (`,`) gets a hint pointing at the YAML path, nothing more.
- **Server-side sync to pilot.** Documented as an external script in `docs/structures-sync.md` (Task 14); not built into the binary.
- **`order_by` runtime sort.** v1 honours `group_by` but uses ripjira's existing within-group sort. `order_by` is parsed/validated but ignored at render. (Avoids reshuffling `internal/tui/grouping/sort.go` in this plan.)
- **Custom field configuration.** Pilot's `teamField` from project config has no equivalent in ripjira. Built-ins fall back to the `teamField == ""` branch from pilot — Backlog becomes unreachable, Entry falls back to "no labels". Adding configurable team field is a separate feature.

---

## Task 1: Add fsnotify dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

```bash
cd /Users/7424d/Dev/ripjira
go get github.com/fsnotify/fsnotify@latest
go mod tidy
```

- [ ] **Step 2: Verify build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add fsnotify for structures hot-reload"
```

---

## Task 2: Domain types with JSON+YAML round-trip

**Files:**
- Create: `internal/structure/types.go`
- Create: `internal/structure/types_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/structure/types_test.go`:

```go
package structure

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFilterClause_JSONShorthand(t *testing.T) {
	// Array shorthand decodes to In.
	var c FilterClause
	if err := json.Unmarshal([]byte(`["High","Medium"]`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(c.In, []string{"High", "Medium"}) {
		t.Fatalf("want In=[High Medium], got %#v", c)
	}

	// Object form decodes every predicate.
	yes := true
	var d FilterClause
	in := []byte(`{"in":["a"],"not":["b"],"regex":"^x","contains":"y","exists":true}`)
	if err := json.Unmarshal(in, &d); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	want := FilterClause{In: []string{"a"}, Not: []string{"b"}, Regex: "^x", Contains: "y", Exists: &yes}
	if !reflect.DeepEqual(d.In, want.In) || !reflect.DeepEqual(d.Not, want.Not) ||
		d.Regex != want.Regex || d.Contains != want.Contains ||
		d.Exists == nil || *d.Exists != *want.Exists {
		t.Fatalf("object form mismatch: %#v", d)
	}

	// Marshal: only In set → array form.
	out, err := json.Marshal(FilterClause{In: []string{"X"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `["X"]` {
		t.Fatalf("want array shorthand, got %s", out)
	}

	// Marshal: mixed → object form.
	out, err = json.Marshal(FilterClause{In: []string{"X"}, Regex: "^x"})
	if err != nil {
		t.Fatalf("marshal mixed: %v", err)
	}
	if string(out) != `{"in":["X"],"regex":"^x"}` {
		t.Fatalf("want object form, got %s", out)
	}
}

func TestFilterClause_YAMLShorthand(t *testing.T) {
	// YAML uses the same logic via JSON conversion under the hood.
	src := []byte(`
title: T
filter:
  status: [Open, "In Progress"]
  assignee:
    exists: true
`)
	var s Section
	if err := yaml.Unmarshal(src, &s); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if got := s.Filter["status"].In; !reflect.DeepEqual(got, []string{"Open", "In Progress"}) {
		t.Fatalf("status In = %#v", got)
	}
	if e := s.Filter["assignee"].Exists; e == nil || !*e {
		t.Fatalf("assignee.exists should be true, got %#v", e)
	}
}
```

- [ ] **Step 2: Run, expect compile fail**

```bash
go test ./internal/structure/...
```

Expected: build error (`structure` package missing).

- [ ] **Step 3: Implement `types.go`**

Create `internal/structure/types.go`:

```go
// Package structure defines the saved view layer ("structures") that groups
// issues into named sections by filter + group_by. The package is pure: it
// does not import internal/jira or any TUI code, so the same logic can run
// from tests, scripts, or external sync tooling. JSON tags match pilot's
// REST DTOs byte-for-byte so external sync against pilot is a direct file
// drop.
package structure

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// FilterClause is the predicate set for a single field. Predicates AND
// together: a value satisfies the clause when every populated predicate
// accepts it. Wire format is either a JSON array (shorthand for In) or an
// object with any subset of {in, not, regex, contains, exists}.
type FilterClause struct {
	In       []string `json:"in,omitempty" yaml:"in,omitempty"`
	Not      []string `json:"not,omitempty" yaml:"not,omitempty"`
	Regex    string   `json:"regex,omitempty" yaml:"regex,omitempty"`
	Contains string   `json:"contains,omitempty" yaml:"contains,omitempty"`
	Exists   *bool    `json:"exists,omitempty" yaml:"exists,omitempty"`

	compiled *regexp.Regexp `json:"-" yaml:"-"`
}

// In is a convenience constructor.
func In(values ...string) FilterClause { return FilterClause{In: values} }

// IsEmpty reports whether the clause expresses no constraint.
func (c *FilterClause) IsEmpty() bool {
	return len(c.In) == 0 && len(c.Not) == 0 && c.Regex == "" && c.Contains == "" && c.Exists == nil
}

// UnmarshalJSON accepts a JSON array (In shorthand) or a JSON object.
func (c *FilterClause) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '[' {
		var values []string
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("filter clause array: %w", err)
		}
		*c = FilterClause{In: values}
		return nil
	}
	type raw FilterClause
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("filter clause object: %w", err)
	}
	*c = FilterClause(r)
	return nil
}

// MarshalJSON emits the array shorthand when only In is set, else an object.
// Value receiver so encoding/json calls it on map values (non-addressable).
//
//nolint:gocritic // value receiver intentional
func (c FilterClause) MarshalJSON() ([]byte, error) {
	onlyIn := len(c.Not) == 0 && c.Regex == "" && c.Contains == "" && c.Exists == nil
	if onlyIn {
		if c.In == nil {
			return json.Marshal([]string{})
		}
		return json.Marshal(c.In)
	}
	type wire struct {
		In       []string `json:"in,omitempty"`
		Not      []string `json:"not,omitempty"`
		Regex    string   `json:"regex,omitempty"`
		Contains string   `json:"contains,omitempty"`
		Exists   *bool    `json:"exists,omitempty"`
	}
	return json.Marshal(wire{In: c.In, Not: c.Not, Regex: c.Regex, Contains: c.Contains, Exists: c.Exists})
}

// SectionFilter maps field name → predicate. Across keys: AND.
type SectionFilter map[string]FilterClause

// Section is one block in a Structure.
type Section struct {
	Title   string          `json:"title" yaml:"title"`
	Filter  SectionFilter   `json:"filter,omitempty" yaml:"filter,omitempty"`
	AnyOf   []SectionFilter `json:"any_of,omitempty" yaml:"any_of,omitempty"`
	GroupBy []string        `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	OrderBy []SortKey       `json:"order_by,omitempty" yaml:"order_by,omitempty"`
}

// SortField is a whitelisted sort key for Section.OrderBy.
type SortField string

// SortDir is asc/desc.
type SortDir string

const (
	SortFieldPriority SortField = "priority"
	SortFieldUpdated  SortField = "updated"
	SortFieldStatus   SortField = "status"
	SortFieldProgress SortField = "progress"

	SortDirAsc  SortDir = "asc"
	SortDirDesc SortDir = "desc"

	MaxOrderByLen = 4
)

// SortKey is one tier of OrderBy.
type SortKey struct {
	Field SortField `json:"field" yaml:"field"`
	Dir   SortDir   `json:"dir" yaml:"dir"`
}

// Structure is a saved, project-scoped collection of sections.
//
// Source is set to "pilot" (or any non-empty value) by sync tooling to mark
// the structure read-only in the UI. Local user structures leave it empty.
type Structure struct {
	ID         string    `json:"id" yaml:"id"`
	ProjectKey string    `json:"project_key" yaml:"project_key,omitempty"`
	Name       string    `json:"name" yaml:"name"`
	Sections   []Section `json:"sections" yaml:"sections"`
	Source     string    `json:"source,omitempty" yaml:"source,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// IsReadOnly returns true when the structure originates from an external
// sync source (pilot, etc.) and should not be edited locally.
func (s *Structure) IsReadOnly() bool { return s.Source != "" }
```

- [ ] **Step 4: Run tests, expect pass**

```bash
go test ./internal/structure/... -run FilterClause -v
```

Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/structure/types.go internal/structure/types_test.go
git commit -m "feat(structure): add core domain types with pilot-compatible JSON"
```

---

## Task 3: Issue accessor abstraction

**Files:**
- Create: `internal/structure/issue.go`
- Create: `internal/structure/issue_test.go`

The evaluator needs to read named fields off issues without importing
`internal/jira` (keeps the package pure and testable). We define a small
interface: callers (UI) implement `Field(name string) string` over their own
issue type via a thin adapter.

- [ ] **Step 1: Write the failing test**

Create `internal/structure/issue_test.go`:

```go
package structure

import "testing"

type fakeIssue map[string]string

func (f fakeIssue) Field(name string) string { return f[name] }

func TestSplitFieldValue(t *testing.T) {
	cases := map[string][]string{
		"":               nil,
		"a":              {"a"},
		"a, b ,  c":      {"a", "b", "c"},
		"single, ,empty": {"single", "empty"},
	}
	for in, want := range cases {
		got := splitFieldValue(in)
		if len(got) != len(want) {
			t.Fatalf("%q → %#v, want %#v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q → %#v, want %#v", in, got, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/structure/... -run SplitFieldValue
```

Expected: build error (`splitFieldValue` undefined).

- [ ] **Step 3: Implement**

Create `internal/structure/issue.go`:

```go
package structure

import "strings"

// Issue is the minimal accessor the evaluator needs. UI code passes an
// adapter that maps logical field names ("status", "priority", "assignee",
// "labels", …) to their string representation. Multi-value fields (labels)
// are joined with ", " and split back out by splitFieldValue.
type Issue interface {
	Field(name string) string
}

// splitFieldValue splits comma-separated multi-value fields, trimming
// whitespace and dropping empty parts. Single-value fields produce a
// one-element slice; empty input returns nil.
func splitFieldValue(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/structure/... -run SplitFieldValue -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/structure/issue.go internal/structure/issue_test.go
git commit -m "feat(structure): add Issue accessor + multi-value splitter"
```

---

## Task 4: Filter evaluation

**Files:**
- Create: `internal/structure/apply.go`
- Create: `internal/structure/apply_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/structure/apply_test.go`:

```go
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
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/structure/... -run Apply
```

Expected: build error (`Apply`, `clauseMatches` undefined).

- [ ] **Step 3: Implement**

Create `internal/structure/apply.go`:

```go
package structure

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// AppliedSection is one section after filtering: title + the issues that
// matched, in input order. Callers feed AppliedSection.Issues into their
// existing grouping pipeline (per-section group_by handled by callers).
type AppliedSection struct {
	Title   string
	GroupBy []string
	OrderBy []SortKey
	Issues  []Issue
}

// Apply runs each section's filter+anyOf over issues and returns one
// AppliedSection per non-empty section, in declaration order. Empty
// sections (zero matches) are dropped.
func Apply(issues []Issue, s *Structure) []AppliedSection {
	out := make([]AppliedSection, 0, len(s.Sections))
	for i := range s.Sections {
		sec := &s.Sections[i]
		matched := filterIssues(issues, sec.Filter, sec.AnyOf)
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

func filterIssues(issues []Issue, filter SectionFilter, anyOf []SectionFilter) []Issue {
	if len(filter) == 0 && len(anyOf) == 0 {
		return issues
	}
	out := make([]Issue, 0, len(issues))
	for _, is := range issues {
		if !matchesFilter(is, filter) {
			continue
		}
		if !matchesAnyOf(is, anyOf) {
			continue
		}
		out = append(out, is)
	}
	return out
}

func matchesFilter(issue Issue, filter SectionFilter) bool {
	for field, clause := range filter {
		if !clauseMatches(issue.Field(field), &clause) {
			return false
		}
	}
	return true
}

func matchesAnyOf(issue Issue, anyOf []SectionFilter) bool {
	if len(anyOf) == 0 {
		return true
	}
	for _, alt := range anyOf {
		if matchesFilter(issue, alt) {
			return true
		}
	}
	return false
}

func clauseMatches(value string, c *FilterClause) bool {
	return existsMatches(value, c.Exists) &&
		(c.In == nil || valueIn(value, c.In)) &&
		(c.Not == nil || !valueIn(value, c.Not)) &&
		regexMatches(value, c) &&
		(c.Contains == "" || containsValue(value, c.Contains))
}

func existsMatches(value string, want *bool) bool {
	if want == nil {
		return true
	}
	return *want == (value != "")
}

func regexMatches(value string, c *FilterClause) bool {
	if c.Regex == "" {
		return true
	}
	re, err := c.matcher()
	if err != nil {
		return false
	}
	if value == "" {
		return re.MatchString("")
	}
	return slices.ContainsFunc(splitFieldValue(value), re.MatchString)
}

func valueIn(value string, allowed []string) bool {
	if value == "" {
		return slices.Contains(allowed, "")
	}
	for _, part := range splitFieldValue(value) {
		if slices.Contains(allowed, part) {
			return true
		}
	}
	return false
}

func containsValue(value, needle string) bool {
	if value == "" {
		return needle == ""
	}
	for _, part := range splitFieldValue(value) {
		if strings.Contains(part, needle) {
			return true
		}
	}
	return false
}

func (c *FilterClause) matcher() (*regexp.Regexp, error) {
	if c.compiled != nil {
		return c.compiled, nil
	}
	re, err := regexp.Compile(c.Regex)
	if err != nil {
		return nil, fmt.Errorf("compile regex %q: %w", c.Regex, err)
	}
	c.compiled = re
	return re, nil
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/structure/... -v
```

Expected: all `Apply` and `Clause` tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/structure/apply.go internal/structure/apply_test.go
git commit -m "feat(structure): filter + apply evaluator with AND/OR semantics"
```

---

## Task 5: Validation

**Files:**
- Create: `internal/structure/validate.go`
- Create: `internal/structure/validate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/structure/validate_test.go`:

```go
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
		"no name": {ID: "u1", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": In("Open")}}}},
		"no sections": {ID: "u1", Name: "n"},
		"empty section title": {ID: "u1", Name: "n", Sections: []Section{{Filter: SectionFilter{"status": In("Open")}}}},
		"empty clause": {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {}}}}},
		"unknown groupby": {ID: "u1", Name: "n", Sections: []Section{{Title: "T", GroupBy: []string{"weird"}, Filter: SectionFilter{"status": In("X")}}}},
		"bad regex": {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {Regex: "(["}}}}},
		"bad order field": {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {Exists: &yes}}, OrderBy: []SortKey{{Field: "weird", Dir: SortDirAsc}}}}},
		"bad order dir": {ID: "u1", Name: "n", Sections: []Section{{Title: "T", Filter: SectionFilter{"status": {Exists: &yes}}, OrderBy: []SortKey{{Field: SortFieldPriority, Dir: "sideways"}}}}},
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
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/structure/... -run Validate
```

Expected: build error.

- [ ] **Step 3: Implement**

Create `internal/structure/validate.go`:

```go
package structure

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// KnownFields is the whitelist of field names usable in filters and group_by.
// Adding a new field requires both a constant here and a corresponding case
// in the UI's Issue adapter (internal/tui adapter).
var KnownFields = []string{
	"status",
	"status_category",
	"priority",
	"issuetype",
	"assignee",
	"reporter",
	"parent_key",
	"labels",
	"project",
}

// Validate checks the structure for problems that would break evaluation or
// confuse a reader: empty sections, unknown fields, bad regexes, unsupported
// sort directions, etc.
func Validate(s *Structure) error {
	if s.Name == "" {
		return errors.New("structure: name is required")
	}
	if len(s.Sections) == 0 {
		return errors.New("structure: at least one section required")
	}
	for i := range s.Sections {
		if err := validateSection(&s.Sections[i]); err != nil {
			return fmt.Errorf("section %d (%q): %w", i, s.Sections[i].Title, err)
		}
	}
	return nil
}

func validateSection(sec *Section) error {
	if sec.Title == "" {
		return errors.New("title is required")
	}
	if len(sec.Filter) == 0 && len(sec.AnyOf) == 0 && len(sec.GroupBy) == 0 {
		return errors.New("section must have at least filter, any_of, or group_by")
	}
	if err := validateFilter(sec.Filter); err != nil {
		return fmt.Errorf("filter: %w", err)
	}
	for i, alt := range sec.AnyOf {
		if err := validateFilter(alt); err != nil {
			return fmt.Errorf("any_of[%d]: %w", i, err)
		}
	}
	for _, g := range sec.GroupBy {
		if !slices.Contains(KnownFields, g) {
			return fmt.Errorf("group_by: unknown field %q", g)
		}
	}
	if len(sec.OrderBy) > MaxOrderByLen {
		return fmt.Errorf("order_by: too many keys (max %d)", MaxOrderByLen)
	}
	for _, k := range sec.OrderBy {
		if err := validateSortKey(k); err != nil {
			return fmt.Errorf("order_by: %w", err)
		}
	}
	return nil
}

func validateFilter(f SectionFilter) error {
	for field, clause := range f {
		if !slices.Contains(KnownFields, field) {
			return fmt.Errorf("unknown field %q", field)
		}
		if clause.IsEmpty() {
			return fmt.Errorf("field %q: clause has no predicates", field)
		}
		if clause.Regex != "" {
			if _, err := regexp.Compile(clause.Regex); err != nil {
				return fmt.Errorf("field %q: bad regex: %w", field, err)
			}
		}
	}
	return nil
}

func validateSortKey(k SortKey) error {
	switch k.Field {
	case SortFieldPriority, SortFieldUpdated, SortFieldStatus, SortFieldProgress:
	default:
		return fmt.Errorf("unknown sort field %q", k.Field)
	}
	switch k.Dir {
	case SortDirAsc, SortDirDesc:
	default:
		return fmt.Errorf("bad sort direction %q", k.Dir)
	}
	return nil
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/structure/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/structure/validate.go internal/structure/validate_test.go
git commit -m "feat(structure): validation rules for sections, fields, sort keys"
```

---

## Task 6: Built-in structures

**Files:**
- Create: `internal/structure/builtins.go`
- Create: `internal/structure/builtins_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/structure/builtins_test.go`:

```go
package structure

import "testing"

func TestBuiltins_DefaultEvaluatesCleanly(t *testing.T) {
	def := Default("BIL")
	if err := Validate(&def); err != nil {
		t.Fatalf("default invalid: %v", err)
	}
	if def.ID != BuiltinDefaultID {
		t.Fatalf("id = %q, want %q", def.ID, BuiltinDefaultID)
	}

	in := Inbox("BIL")
	if err := Validate(&in); err != nil {
		t.Fatalf("inbox invalid: %v", err)
	}
	if in.ID != BuiltinInboxID {
		t.Fatalf("id = %q", in.ID)
	}

	if !IsBuiltinID(BuiltinDefaultID) || !IsBuiltinID(BuiltinInboxID) {
		t.Fatal("IsBuiltinID broken")
	}
	if IsBuiltinID("user-uuid") {
		t.Fatal("user id mistakenly built-in")
	}
}

func TestBuiltins_DefaultBuckets(t *testing.T) {
	def := Default("BIL")
	mk := func(labels string) fakeIssue { return fakeIssue{"labels": labels} }
	out := Apply([]Issue{mk("blocker"), mk("")}, &def)

	titles := make([]string, len(out))
	for i := range out {
		titles[i] = out[i].Title
	}
	// Without team field, only "Projects" (labels exist) and "Entry"
	// (labels missing) are reachable. "Backlog" must not appear.
	want := []string{"Projects", "Entry"}
	if len(titles) != len(want) {
		t.Fatalf("titles = %#v, want %#v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("titles[%d] = %q, want %q", i, titles[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/structure/... -run Builtins
```

- [ ] **Step 3: Implement**

Create `internal/structure/builtins.go`:

```go
package structure

// Built-in structure IDs are reserved sentinel strings; user structures get
// UUIDs (or any string starting with something else).
const (
	BuiltinDefaultID = "default"
	BuiltinInboxID   = "inbox"

	// sentinelNeverField is a synthetic field name used to make a section
	// unreachable. The Issue adapter returns "" for unknown fields, so a
	// clause requiring Exists:&true on this field never matches.
	sentinelNeverField = "__never"
)

//nolint:gochecknoglobals // module-private constant pointer targets
var (
	builtinTrue  = true
	builtinFalse = false

	defaultOrderBy = []SortKey{
		{Field: SortFieldPriority, Dir: SortDirDesc},
		{Field: SortFieldUpdated, Dir: SortDirDesc},
	}
)

// IsBuiltinID reports whether id refers to a system-provided structure.
func IsBuiltinID(id string) bool {
	return id == BuiltinDefaultID || id == BuiltinInboxID
}

// Builtins returns both system structures resolved for the project.
// The returned slice is freshly constructed; callers may mutate.
func Builtins(projectKey string) []Structure {
	return []Structure{Default(projectKey), Inbox(projectKey)}
}

// Default is "Projects/Backlog/Entry": labelled items vs unlabelled items.
// Without per-project team-field config, Backlog is unreachable (labels
// missing AND a sentinel field exists — which always evaluates to ""), and
// Entry catches the "no labels" bucket. Projects requires labels exist.
func Default(projectKey string) Structure {
	return Structure{
		ID:         BuiltinDefaultID,
		ProjectKey: projectKey,
		Name:       "Default",
		Sections: []Section{
			{
				Title:   "Projects",
				Filter:  SectionFilter{"labels": {Exists: &builtinTrue}},
				OrderBy: defaultOrderBy,
			},
			{
				Title: "Backlog",
				Filter: SectionFilter{
					"labels":           {Exists: &builtinFalse},
					sentinelNeverField: {Exists: &builtinTrue},
				},
				OrderBy: defaultOrderBy,
			},
			{
				Title:   "Entry",
				Filter:  SectionFilter{"labels": {Exists: &builtinFalse}},
				OrderBy: defaultOrderBy,
			},
		},
	}
}

// Inbox surfaces issues that are missing labels (incomplete metadata).
func Inbox(projectKey string) Structure {
	return Structure{
		ID:         BuiltinInboxID,
		ProjectKey: projectKey,
		Name:       "Inbox",
		Sections: []Section{{
			Title:   "Missing labels",
			AnyOf:   []SectionFilter{{"labels": {Exists: &builtinFalse}}},
			OrderBy: defaultOrderBy,
		}},
	}
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/structure/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/structure/builtins.go internal/structure/builtins_test.go
git commit -m "feat(structure): default + inbox built-ins"
```

---

## Task 7: YAML file store

**Files:**
- Create: `internal/structure/store.go`
- Create: `internal/structure/store_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/structure/store_test.go`:

```go
package structure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_LoadEmptyDirReturnsBuiltinsOnly(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	got, err := s.Load("BIL")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 builtins, got %d", len(got))
	}
	if got[0].ID != BuiltinDefaultID || got[1].ID != BuiltinInboxID {
		t.Fatalf("unexpected ids: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestStore_LoadParsesUserYAML(t *testing.T) {
	dir := t.TempDir()
	yamlBody := `
- id: my-team
  name: My team
  sections:
    - title: In progress
      filter:
        status: [Open, "In Progress"]
        assignee:
          exists: true
- id: synced
  name: Pilot synced
  source: pilot
  sections:
    - title: Foo
      filter:
        status: [Open]
`
	if err := os.WriteFile(filepath.Join(dir, "BIL.yml"), []byte(yamlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	got, err := s.Load("BIL")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// 2 builtins + 2 user
	if len(got) != 4 {
		t.Fatalf("want 4 structures, got %d: %#v", len(got), got)
	}
	if got[2].ID != "my-team" || got[2].IsReadOnly() {
		t.Errorf("user structure: %#v", got[2])
	}
	if !got[3].IsReadOnly() {
		t.Errorf("synced should be read-only")
	}
}

func TestStore_LoadRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BIL.yml"), []byte(`- id: bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if _, err := s.Load("BIL"); err == nil {
		t.Fatal("expected validation error for nameless structure")
	}
}

func TestStore_FindByID(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	got, err := s.FindByID("BIL", BuiltinDefaultID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != BuiltinDefaultID {
		t.Fatalf("got %q", got.ID)
	}
	if _, err := s.FindByID("BIL", "nope"); err == nil {
		t.Fatal("expected not found")
	}
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/structure/... -run Store
```

- [ ] **Step 3: Implement**

Create `internal/structure/store.go`:

```go
package structure

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned by FindByID when no matching structure exists.
var ErrNotFound = errors.New("structure not found")

// Store loads structures from a directory of <PROJECT>.yml files. Each file
// is a YAML list of Structure values. Built-ins are always returned in front
// of user structures.
//
// Store is safe for concurrent use from a single TUI goroutine; callers
// share a Store across the app rather than reconstructing per call.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir. dir does not need to exist;
// missing files are treated as "no user structures", not an error.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir returns the underlying directory (for the watcher and "open in editor"
// affordances in the UI).
func (s *Store) Dir() string { return s.dir }

// Path returns the YAML path for the given project key.
func (s *Store) Path(projectKey string) string {
	return filepath.Join(s.dir, projectKey+".yml")
}

// Load returns built-ins first, then user structures from
// <dir>/<projectKey>.yml. Returns an error if any user structure fails
// validation or the file is malformed YAML.
func (s *Store) Load(projectKey string) ([]Structure, error) {
	out := Builtins(projectKey)

	body, err := os.ReadFile(s.Path(projectKey))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.Path(projectKey), err)
	}

	var user []Structure
	if err := yaml.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.Path(projectKey), err)
	}
	for i := range user {
		user[i].ProjectKey = projectKey
		if err := Validate(&user[i]); err != nil {
			return nil, fmt.Errorf("structure %q: %w", user[i].ID, err)
		}
	}
	out = append(out, user...)
	return out, nil
}

// FindByID returns the structure for the given (project, id) pair or
// ErrNotFound. Built-ins resolve without touching disk for that lookup.
func (s *Store) FindByID(projectKey, id string) (Structure, error) {
	all, err := s.Load(projectKey)
	if err != nil {
		return Structure{}, err
	}
	for i := range all {
		if all[i].ID == id {
			return all[i], nil
		}
	}
	return Structure{}, ErrNotFound
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/structure/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/structure/store.go internal/structure/store_test.go
git commit -m "feat(structure): YAML file store with built-in fallback"
```

---

## Task 8: Hot-reload watcher

**Files:**
- Create: `internal/structure/watch.go`
- Create: `internal/structure/watch_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/structure/watch_test.go`:

```go
package structure

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch_FiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "BIL.yml")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	events, err := Watch(ctx, dir)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	// Give fsnotify a moment to install the watch.
	time.Sleep(100 * time.Millisecond)

	body := []byte("- id: u\n  name: U\n  sections:\n    - title: T\n      filter:\n        status: [Open]\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-events:
		if ev.ProjectKey != "BIL" {
			t.Fatalf("project = %q", ev.ProjectKey)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/structure/... -run Watch
```

- [ ] **Step 3: Implement**

Create `internal/structure/watch.go`:

```go
package structure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// Event signals that <dir>/<ProjectKey>.yml changed. Consumers should call
// Store.Load(ProjectKey) to refresh.
type Event struct {
	ProjectKey string
}

// Watch installs a directory watcher and emits Event values until ctx is
// cancelled. The directory is created if missing. The returned channel is
// closed when the watcher stops (ctx done or fatal error). Non-YAML files
// and files with no project-key basename are ignored.
//
// Bursty filesystem events (atomic-write rename + create) are not debounced
// here; the consumer is expected to be cheap (Store.Load reads one small
// file). Add coalescing in the consumer if needed.
func Watch(ctx context.Context, dir string) (<-chan Event, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fsnotify: %w", err)
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("watch %s: %w", dir, err)
	}
	out := make(chan Event, 8)
	go func() {
		defer close(out)
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if pk, ok := projectFromPath(ev.Name); ok {
					select {
					case out <- Event{ProjectKey: pk}:
					case <-ctx.Done():
						return
					}
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return out, nil
}

func projectFromPath(p string) (string, bool) {
	base := filepath.Base(p)
	if !strings.HasSuffix(base, ".yml") && !strings.HasSuffix(base, ".yaml") {
		return "", false
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" || strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/structure/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/structure/watch.go internal/structure/watch_test.go
git commit -m "feat(structure): fsnotify-based hot-reload watcher"
```

---

## Task 9: Persist last-selected structure per project

**Files:**
- Modify: `internal/state/state.go`
- Modify: `internal/state/state_test.go`

- [ ] **Step 1: Add field to State**

Add to the `State` struct in `internal/state/state.go` (after `CommentDrafts`):

```go
	// LastStructure maps project key → last-selected structure id for that
	// project. Persisted so opening the STRUCTURES tab next session restores
	// the previous view per project.
	LastStructure map[string]string `json:"lastStructure,omitempty"`
```

- [ ] **Step 2: Add test**

Add to `internal/state/state_test.go`:

```go
func TestState_LastStructureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := Mutate(path, func(s *State) {
		if s.LastStructure == nil {
			s.LastStructure = map[string]string{}
		}
		s.LastStructure["BIL"] = "my-team"
		s.LastStructure["OPS"] = "default"
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.LastStructure["BIL"] != "my-team" || got.LastStructure["OPS"] != "default" {
		t.Fatalf("round-trip lost data: %#v", got.LastStructure)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/state/... -v
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/state/state.go internal/state/state_test.go
git commit -m "feat(state): persist last-selected structure per project"
```

---

## Task 10: Issue adapter for the evaluator

**Files:**
- Create: `internal/tui/structureadapter/adapter.go`
- Create: `internal/tui/structureadapter/adapter_test.go`

The TUI must adapt `jira.Issue` (a struct with typed fields) to the pure
`structure.Issue` interface (string-keyed accessor). This adapter lives in
`internal/tui/` because it bridges the two layers.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/structureadapter/adapter_test.go`:

```go
package structureadapter

import (
	"testing"

	"github.com/billygate/ripjira/internal/jira"
)

func TestAdapter_FieldsResolveCorrectly(t *testing.T) {
	user := &jira.User{DisplayName: "Alice"}
	is := jira.Issue{
		Key:      "BIL-1",
		Status:   jira.Status{Name: "In Progress", Category: "indeterminate"},
		Priority: jira.Priority{Name: "High"},
		Type:     jira.IssueType{Name: "Bug"},
		Assignee: user,
		Reporter: user,
		Labels:   []string{"ui", "blocker"},
	}
	a := New(is)
	cases := map[string]string{
		"status":          "In Progress",
		"status_category": "indeterminate",
		"priority":        "High",
		"issuetype":       "Bug",
		"assignee":        "Alice",
		"reporter":        "Alice",
		"labels":          "ui, blocker",
		"unknown":         "",
	}
	for field, want := range cases {
		if got := a.Field(field); got != want {
			t.Errorf("Field(%q) = %q, want %q", field, got, want)
		}
	}
}

func TestAdapter_NilAssignee(t *testing.T) {
	a := New(jira.Issue{Key: "X-1"})
	if a.Field("assignee") != "" {
		t.Fatal("nil assignee should be empty")
	}
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/tui/structureadapter/...
```

- [ ] **Step 3: Implement**

Create `internal/tui/structureadapter/adapter.go`:

```go
// Package structureadapter wraps jira.Issue with the
// structure.Issue interface so the pure evaluator can read named fields.
package structureadapter

import (
	"strings"

	"github.com/billygate/ripjira/internal/jira"
)

// Adapter implements structure.Issue over jira.Issue.
type Adapter struct{ issue jira.Issue }

// New returns an Adapter for issue.
func New(issue jira.Issue) Adapter { return Adapter{issue: issue} }

// Issue returns the wrapped jira.Issue (for callers that need to render it
// after the structure evaluator picks it).
func (a Adapter) Issue() jira.Issue { return a.issue }

// Field implements structure.Issue.
func (a Adapter) Field(name string) string {
	switch name {
	case "status":
		return a.issue.Status.Name
	case "status_category":
		return a.issue.Status.Category
	case "priority":
		return a.issue.Priority.Name
	case "issuetype":
		return a.issue.Type.Name
	case "assignee":
		if a.issue.Assignee != nil {
			return a.issue.Assignee.DisplayName
		}
		return ""
	case "reporter":
		if a.issue.Reporter != nil {
			return a.issue.Reporter.DisplayName
		}
		return ""
	case "parent_key":
		// Parent is exposed via the Links/Subtasks shape; not first-class on
		// jira.Issue today. Return "" for now; extend when ParentKey is added.
		return ""
	case "labels":
		return strings.Join(a.issue.Labels, ", ")
	case "project":
		if i := strings.Index(a.issue.Key, "-"); i > 0 {
			return a.issue.Key[:i]
		}
		return ""
	}
	return ""
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test ./internal/tui/structureadapter/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/structureadapter/
git commit -m "feat(tui): structureadapter bridges jira.Issue → structure.Issue"
```

---

## Task 11: Structures view kind + tab cell

**Files:**
- Modify: `internal/tui/panes/views.go`

- [ ] **Step 1: Add the constant**

In `internal/tui/panes/views.go`, add `ViewStructures` to the `ViewKind` enum
(after `ViewSearch` to preserve the int values of existing constants — the
order in the tab strip is not the iota order).

```go
const (
	ViewMyTasks ViewKind = iota
	ViewWatching
	// ... existing entries ...
	ViewSprint
	ViewMentions
	ViewSearch
	ViewStructures
)
```

Add a `String()` case:

```go
	case ViewStructures:
		return "STRUCTURES"
```

- [ ] **Step 2: Update existing String() test if present**

Run:

```bash
go test ./internal/tui/panes/... -v
```

If `views_test.go` enumerates all ViewKind values, add `ViewStructures` to
the list there.

- [ ] **Step 3: Wire into the tab strip in `app.go`**

In `internal/tui/app.go` `renderTabs()`, add an entry to the `labels` map:

```go
		panes.ViewStructures: "STRUCTURES",
```

And include `panes.ViewStructures` in whichever ordered slice drives the tab
ordering (search for the slice that contains `ViewMyTasks`, `ViewWatching`,
`ViewSearch`).

- [ ] **Step 4: Build + commit (no behavior change yet)**

```bash
go build ./...
go test ./internal/tui/...
git add internal/tui/panes/views.go internal/tui/app.go
git commit -m "feat(tui): register STRUCTURES tab placeholder"
```

---

## Task 12: Wire Store + Watcher into the app

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `cmd/ripjira/main.go` (or wherever the Model is constructed; locate
  via `grep -n "panes.New\|tui.New\|app.New" cmd/`)

- [ ] **Step 1: Resolve the structures directory**

Add a small helper in `internal/structure/store.go` (skip if already in place
via Task 7):

```go
// DefaultDir returns the XDG-aware default location for structure files:
// $XDG_CONFIG_HOME/ripjira/structures or ~/.config/ripjira/structures.
func DefaultDir() (string, error) {
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
		return filepath.Join(cfg, "ripjira", "structures"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ripjira", "structures"), nil
}
```

Cover with a small test that asserts the XDG override wins.

- [ ] **Step 2: Hold a Store + watcher channel on the Model**

In `internal/tui/app.go`, on the `Model` struct add:

```go
	structures      *structure.Store
	structureCancel context.CancelFunc
	currentStructID map[string]string // project → selected id (mirror of state)
	loadedStructs   map[string][]structure.Structure // project → cached load
```

Initialise in the Model constructor (wherever existing fields are set):

```go
	dir, err := structure.DefaultDir()
	if err == nil {
		m.structures = structure.NewStore(dir)
	}
	m.currentStructID = map[string]string{}
	if st.LastStructure != nil {
		for k, v := range st.LastStructure {
			m.currentStructID[k] = v
		}
	}
	m.loadedStructs = map[string][]structure.Structure{}
```

- [ ] **Step 3: Start the watcher as a tea.Cmd**

Add a watcher message and command:

```go
type structureChangedMsg struct{ Project string }

func watchStructuresCmd(store *structure.Store) (tea.Cmd, context.CancelFunc) {
	if store == nil {
		return func() tea.Msg { return nil }, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := structure.Watch(ctx, store.Dir())
	if err != nil {
		cancel()
		return func() tea.Msg { return nil }, func() {}
	}
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return structureChangedMsg{Project: ev.ProjectKey}
	}, cancel
}
```

In `Init()` (or wherever the model dispatches startup commands):

```go
	cmd, cancel := watchStructuresCmd(m.structures)
	m.structureCancel = cancel
	cmds = append(cmds, cmd)
```

In `Update`, handle the message:

```go
case structureChangedMsg:
	delete(m.loadedStructs, msg.Project)
	// Re-arm the watcher: tea.Cmd consumes one message per call.
	cmd, _ := watchStructuresCmd(m.structures)
	return m, cmd
```

- [ ] **Step 4: Build, run, smoke**

```bash
go build ./... && go test ./internal/tui/... -count=1
```

Manual: launch ripjira, edit `~/.config/ripjira/structures/BIL.yml`, confirm
no panic and that `m.loadedStructs["BIL"]` is invalidated (add a debug log
or breakpoint if needed; remove before commit).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/structure/store.go
git commit -m "feat(tui): wire structure store and hot-reload watcher into app"
```

---

## Task 13: Apply structure to list pane and render section headers

**Files:**
- Modify: `internal/tui/panes/list.go`
- Modify: `internal/tui/app.go` (selection of which structure feeds the list)

This is the largest UI change. The list pane already groups issues; we add a
**section** layer above grouping. When the active tab is `STRUCTURES`, the
list:

1. Calls `structure.Apply(adapters, &activeStructure)` → `[]AppliedSection`.
2. For each section, runs the existing grouping strategy on `sec.Issues`.
3. Renders a styled section header (`sec.Title`, count) above the section's
   groups; sections with `Source != ""` get a ` 🔒 read-only` suffix (or a
   plain `[ro]` when emoji-free per CLAUDE.md — text-only).

- [ ] **Step 1: Define a list-pane mode**

In `internal/tui/panes/list.go`, near the existing `Pane` struct, add:

```go
// Sections, when non-empty, swaps the flat-grouping render for a sectioned
// render: each section is drawn with a header and the existing grouping is
// applied within it.
type Section struct {
	Title    string
	ReadOnly bool
	Issues   []jira.Issue
}

// SetSections enables sectioned mode. Pass nil to revert to flat grouping.
func (p *Pane) SetSections(sections []Section) {
	p.sections = sections
}
```

Add `sections []Section` to the `Pane` struct.

- [ ] **Step 2: Branch the renderer**

In the list-pane render method, before the existing grouping render:

```go
if len(p.sections) > 0 {
	return p.renderSectioned(width, height)
}
```

Add `renderSectioned`:

```go
func (p *Pane) renderSectioned(width, height int) string {
	var b strings.Builder
	for i := range p.sections {
		sec := &p.sections[i]
		header := fmt.Sprintf("▾ %s (%d)", sec.Title, len(sec.Issues))
		if sec.ReadOnly {
			header += "  [ro]"
		}
		b.WriteString(p.styles.SectionHeader.Render(header))
		b.WriteByte('\n')
		groups := p.strategy.Group(sec.Issues)
		for _, g := range groups {
			b.WriteString(p.renderGroup(g, width)) // existing helper
			b.WriteByte('\n')
		}
	}
	return b.String()
}
```

If `renderGroup` is not already a separate function in `list.go`, extract
the per-group portion of the existing flat render into one before adding
`renderSectioned` (smaller commit; pure refactor, do this as Step 2a).

- [ ] **Step 3: Add the SectionHeader style**

In `internal/tui/styles/styles.go` (or the equivalent struct construction):

```go
	SectionHeader: lipgloss.NewStyle().
		Bold(true).
		Foreground(palette.Accent()).
		Padding(0, 0, 0, 0).
		MarginTop(1),
```

- [ ] **Step 4: Apply structure when on the STRUCTURES tab**

In `internal/tui/app.go`, where `m.list.SetIssues(...)` is called, branch:

```go
if m.activeView == panes.ViewStructures {
	if structID, ok := m.currentStructID[m.project]; ok && structID != "" {
		structs, err := m.loadStructures(m.project)
		if err == nil {
			if st, ok := findByID(structs, structID); ok {
				adapters := make([]structure.Issue, len(issues))
				for i, is := range issues {
					adapters[i] = structureadapter.New(is)
				}
				applied := structure.Apply(adapters, &st)
				secs := make([]panes.Section, 0, len(applied))
				for _, a := range applied {
					realIssues := make([]jira.Issue, len(a.Issues))
					for i, x := range a.Issues {
						realIssues[i] = x.(structureadapter.Adapter).Issue()
					}
					secs = append(secs, panes.Section{
						Title:    a.Title,
						ReadOnly: st.IsReadOnly(),
						Issues:   realIssues,
					})
				}
				m.list.SetSections(secs)
				m.list.SetIssues(nil) // section render owns the data
				return
			}
		}
	}
	// Fallback: no selection / load error → behave like MY ISSUES.
}
m.list.SetSections(nil)
m.list.SetIssues(issues)
```

`loadStructures` is a small helper:

```go
func (m *Model) loadStructures(project string) ([]structure.Structure, error) {
	if v, ok := m.loadedStructs[project]; ok {
		return v, nil
	}
	if m.structures == nil {
		return structure.Builtins(project), nil
	}
	v, err := m.structures.Load(project)
	if err != nil {
		return nil, err
	}
	m.loadedStructs[project] = v
	return v, nil
}

func findByID(all []structure.Structure, id string) (structure.Structure, bool) {
	for i := range all {
		if all[i].ID == id {
			return all[i], true
		}
	}
	return structure.Structure{}, false
}
```

- [ ] **Step 5: Default selection on first entry to the tab**

When the user switches to `ViewStructures` and `m.currentStructID[m.project]`
is empty, set it to `BuiltinDefaultID` and persist:

```go
if _, ok := m.currentStructID[m.project]; !ok {
	m.currentStructID[m.project] = structure.BuiltinDefaultID
	go state.Mutate(statePath, func(s *state.State) {
		if s.LastStructure == nil {
			s.LastStructure = map[string]string{}
		}
		s.LastStructure[m.project] = structure.BuiltinDefaultID
	})
}
```

- [ ] **Step 6: Run tests and golden frames**

```bash
go test ./internal/tui/... -count=1
```

Expected: existing tests pass; if golden frames diff because of the new tab
cell, regenerate them under `-update` and inspect the diffs.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/panes/list.go internal/tui/app.go internal/tui/styles/
git commit -m "feat(tui): render structures as sectioned list"
```

---

## Task 14: Structure picker overlay

**Files:**
- Create: `internal/tui/overlays/structures.go`
- Create: `internal/tui/overlays/structures_test.go`
- Modify: `internal/tui/keymap.go`, `internal/tui/keymap_layout.go`
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Add keybindings**

In `internal/tui/keymap.go`, add:

```go
	OpenStructurePicker key.Binding
	NextStructure       key.Binding
	PrevStructure       key.Binding
```

Initialise:

```go
	OpenStructurePicker: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "structure picker")),
	NextStructure:       key.NewBinding(key.WithKeys("}"), key.WithHelp("}", "next structure")),
	PrevStructure:       key.NewBinding(key.WithKeys("{"), key.WithHelp("{", "prev structure")),
```

Wire help labels in `keymap_layout.go` if that file maintains a list.

- [ ] **Step 2: Implement the overlay**

Create `internal/tui/overlays/structures.go`:

```go
package overlays

import (
	"github.com/billygate/ripjira/internal/structure"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StructurePicker is a single-list overlay for choosing a Structure for the
// current project. Built-ins lead, user structures follow, read-only synced
// structures get a [ro] suffix.
type StructurePicker struct {
	items   []structure.Structure
	cursor  int
	visible bool
}

// NewStructurePicker constructs a picker over the given structures.
func NewStructurePicker(items []structure.Structure, currentID string) StructurePicker {
	p := StructurePicker{items: items}
	for i := range items {
		if items[i].ID == currentID {
			p.cursor = i
			break
		}
	}
	return p
}

// Show / Hide / Visible follow the conventions of the other overlays.
func (p *StructurePicker) Show()        { p.visible = true }
func (p *StructurePicker) Hide()        { p.visible = false }
func (p StructurePicker) Visible() bool { return p.visible }

// PickedMsg is dispatched when the user presses enter.
type PickedMsg struct{ ID string }

// Update handles arrow keys, enter (picks), and esc (cancels).
func (p StructurePicker) Update(msg tea.Msg) (StructurePicker, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(k, key.NewBinding(key.WithKeys("up", "k"))):
			if p.cursor > 0 {
				p.cursor--
			}
		case key.Matches(k, key.NewBinding(key.WithKeys("down", "j"))):
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case key.Matches(k, key.NewBinding(key.WithKeys("enter"))):
			id := p.items[p.cursor].ID
			p.visible = false
			return p, func() tea.Msg { return PickedMsg{ID: id} }
		case key.Matches(k, key.NewBinding(key.WithKeys("esc"))):
			p.visible = false
		}
	}
	return p, nil
}

// View renders the overlay.
func (p StructurePicker) View() string {
	if !p.visible {
		return ""
	}
	var b lipgloss.Style
	_ = b
	lines := make([]string, 0, len(p.items)+2)
	lines = append(lines, "Structures")
	for i, s := range p.items {
		marker := "  "
		if i == p.cursor {
			marker = "▸ "
		}
		suffix := ""
		if s.IsReadOnly() {
			suffix = "  [ro]"
		}
		lines = append(lines, marker+s.Name+suffix)
	}
	lines = append(lines, "↑/↓ navigate  ⏎ select  esc cancel")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
```

- [ ] **Step 3: Picker test**

Create `internal/tui/overlays/structures_test.go`:

```go
package overlays

import (
	"testing"

	"github.com/billygate/ripjira/internal/structure"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStructurePicker_EnterDispatchesPickedMsg(t *testing.T) {
	items := []structure.Structure{
		{ID: "default", Name: "Default"},
		{ID: "u1", Name: "Mine"},
	}
	p := NewStructurePicker(items, "default")
	p.Show()

	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 1 {
		t.Fatalf("cursor = %d", p.cursor)
	}
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a Cmd")
	}
	msg := cmd()
	picked, ok := msg.(PickedMsg)
	if !ok {
		t.Fatalf("got %T, want PickedMsg", msg)
	}
	if picked.ID != "u1" {
		t.Fatalf("picked %q", picked.ID)
	}
}

func TestStructurePicker_EscHidesWithoutDispatch(t *testing.T) {
	p := NewStructurePicker([]structure.Structure{{ID: "a", Name: "A"}}, "a")
	p.Show()
	p, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.visible {
		t.Fatal("should hide")
	}
	if cmd != nil && cmd() != nil {
		t.Fatal("should not dispatch")
	}
}
```

- [ ] **Step 4: Wire into Model**

In `internal/tui/app.go` add the picker field and handlers:

```go
	structPicker overlays.StructurePicker
```

Key handler (in the keypress switch):

```go
case key.Matches(msg, m.keymap.OpenStructurePicker):
	if m.activeView == panes.ViewStructures {
		all, err := m.loadStructures(m.project)
		if err == nil {
			m.structPicker = overlays.NewStructurePicker(all, m.currentStructID[m.project])
			m.structPicker.Show()
		}
	}
case key.Matches(msg, m.keymap.NextStructure):
	m.cycleStructure(+1)
case key.Matches(msg, m.keymap.PrevStructure):
	m.cycleStructure(-1)
```

`cycleStructure`:

```go
func (m *Model) cycleStructure(delta int) {
	if m.activeView != panes.ViewStructures {
		return
	}
	all, err := m.loadStructures(m.project)
	if err != nil || len(all) == 0 {
		return
	}
	cur := m.currentStructID[m.project]
	idx := 0
	for i := range all {
		if all[i].ID == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(all)) % len(all)
	m.currentStructID[m.project] = all[idx].ID
	go state.Mutate(m.statePath, func(s *state.State) {
		if s.LastStructure == nil {
			s.LastStructure = map[string]string{}
		}
		s.LastStructure[m.project] = all[idx].ID
	})
}
```

PickedMsg handler:

```go
case overlays.PickedMsg:
	m.currentStructID[m.project] = msg.ID
	go state.Mutate(m.statePath, func(s *state.State) {
		if s.LastStructure == nil {
			s.LastStructure = map[string]string{}
		}
		s.LastStructure[m.project] = msg.ID
	})
```

Forward picker updates inside the main `Update` switch:

```go
if m.structPicker.Visible() {
	var cmd tea.Cmd
	m.structPicker, cmd = m.structPicker.Update(msg)
	return m, cmd
}
```

Render in `View()`:

```go
if m.structPicker.Visible() {
	body = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, m.structPicker.View())
}
```

- [ ] **Step 5: Build, test, smoke**

```bash
go build ./... && go test ./internal/... -count=1
```

Manual: open ripjira, press `]` until `STRUCTURES` is active, press `S`,
arrow-pick `Inbox`, press Enter, observe section headers update.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/overlays/structures.go internal/tui/overlays/structures_test.go internal/tui/app.go internal/tui/keymap.go internal/tui/keymap_layout.go
git commit -m "feat(tui): structure picker overlay + cycle bindings"
```

---

## Task 15: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-04-30-ripjira-design.md`
- Create: `docs/structures.md`

- [ ] **Step 1: README — short STRUCTURES section**

Add a new top-level section under existing tab docs:

```markdown
### STRUCTURES tab

Save named "structures" — ordered sections of issues, each filtered and
grouped its own way. Two built-ins ship: `default` (Projects/Backlog/Entry
by labels) and `inbox` (issues missing labels). User structures live as
YAML at `~/.config/ripjira/structures/<PROJECT_KEY>.yml` and hot-reload
when the file changes. See `docs/structures.md` for the schema.

Bindings inside the tab: `S` opens the picker, `{` / `}` cycle.
```

- [ ] **Step 2: docs/structures.md**

Create `docs/structures.md` with: schema reference (annotated YAML
example), full FilterClause grammar (in/not/regex/contains/exists), the
list of `KnownFields`, the `[ro]` marker convention, and a copy-pasteable
`sync-from-pilot.sh` snippet (pure documentation; not shipped in the
binary). Use the YAML example from earlier in this conversation as the
canonical sample.

- [ ] **Step 3: Update the design spec**

Append one paragraph to
`docs/superpowers/specs/2026-04-30-ripjira-design.md` under the layout
section noting the `STRUCTURES` tab, the storage path, and that it is
file-driven with optional external sync.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/structures.md docs/superpowers/specs/2026-04-30-ripjira-design.md
git commit -m "docs: structures tab, schema, and sync recipe"
```

---

## Task 16: Plan cleanup

Per `CLAUDE.md`: "Before publishing or pushing a release tag, always delete
completed plans and one-off design specs from `docs/superpowers/plans/`."

- [ ] **Step 1: Delete this plan file**

```bash
git rm docs/superpowers/plans/2026-05-04-structures.md
git commit -m "docs: drop completed structures plan"
```

(Run only after Tasks 1–15 are merged and the feature ships. The design
spec absorbs the lasting reference; this plan does not.)

---

## Verification

Before declaring the feature done, run all of:

```bash
make lint       # golangci-lint
make test       # full suite, race detector
make cover      # coverage; structure package should be ≥80%
make build      # ./bin/ripjira
./bin/ripjira   # smoke: switch to STRUCTURES, S, edit YAML, observe reload
```

Manual scenarios to confirm:

1. **Empty config dir** → built-ins still work; STRUCTURES tab opens, default selected.
2. **Drop a hand-written YAML** → appears in picker within ~200ms of save.
3. **Bad YAML** → toast error, previous list unchanged, no crash.
4. **`source: pilot`** structure → `[ro]` suffix in picker and section headers, no editor affordance offered.
5. **Switch projects** → last-selected structure restored from `state.json`.
6. **Quit + relaunch** → STRUCTURES tab opens on the same structure.
