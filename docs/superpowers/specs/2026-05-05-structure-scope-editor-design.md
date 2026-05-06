# Visual scope editor for structures

Status: design, ready for plan
Date: 2026-05-05

## Problem

Structures group issues into named sections by per-field filters. Today the
only way to constrain *which issues a structure considers at all* is to edit
each section's filter, or to live with whatever the project-wide query
returns. A common need is "show me only issues with `labels in
[Q12026, Q22026]`" across the entire structure — without rewriting every
section.

## Goal

A visual, in-app editor that lets the user attach a structure-wide filter
("scope") that AND-applies to every section. No JQL, no YAML editing.
Local-only — no sync with external systems.

## Non-goals

- AnyOf (cross-field OR) at scope level.
- Editing the set of sections, group_by, or order_by.
- Creating, renaming, or deleting structures themselves.
- Live preview while editing.
- API fallback for value pickers (labels/components/users endpoints).

## Data model

Add one field to `internal/structure/Structure`:

```go
type Structure struct {
    // ...existing fields...
    Scope SectionFilter `json:"scope,omitempty" yaml:"scope,omitempty"`
}
```

Reuse `SectionFilter` and `FilterClause` as-is. No new types.

`apply.go` change: when filtering candidates for each section, additionally
require `matchesFilter(issue, structure.Scope)`. Empty `Scope` ⇒ no-op,
existing behavior preserved. Scope is AND-ed *outside* of section
`Filter`/`AnyOf`, i.e. the issue must match scope **and** the section's
own predicate.

YAML round-trip: empty scope omitted via `omitempty`. Existing structure
files load unchanged.

## UI

### Entry points

- The Structures overlay opens with `\` (existing `OpenStructures`).
- Inside the overlay, pressing `e` on the highlighted entry opens the
  scope editor for that structure.
- The existing top-level `e` binding (`EditStructures` → opens YAML in
  `$EDITOR`) is **left untouched** in this change. The visual scope
  editor is only reachable through the structures picker. A future
  change can repurpose top-level `e` once the visual editor covers
  more of the YAML.
- Read-only structures (`Source != ""`) cannot be edited: `e` on the
  overlay emits a toast "structure is read-only" and does not open the
  editor. (Pilot sync is not implemented today, but the gate is cheap
  and future-proof.)

### Layout

New overlay `ScopeEditor` in `internal/tui/overlays/scope_editor.go`.
Centered, takes most of the screen. Two states:

**List state** — rows of the scope filter:

```
┌─ Scope: <structure name> ────────────────────────────┐
│  labels       in       Q12026, Q22026                │
│▶ status       not       Done, Closed                  │
│  assignee     exists    yes                           │
│  + add row                                            │
└──────────────────────────────────────────────────────┘
 enter edit · a add · d delete · s save · esc cancel
```

Bindings:
- `j/k` / arrows — move cursor.
- `Enter` on a row — open row editor for that row.
- `Enter` on `+ add row` (or `a` anywhere) — open row editor for a new row.
- `d` — delete highlighted row.
- `s` — save and close. Emits `ScopeSavedMsg{Rows: []ScopeRow}`.
- `Esc` — cancel and discard pending edits. (No dirty-confirm in v1;
  `s` to save is explicit, and the editor only mutates the in-memory
  row list — discarding is cheap.)

**Row-editor state** — modal sub-overlay over the list, three steps via
`Tab`/`Shift+Tab`:

1. **Field** — text input with autocomplete. Suggestions: `labels`,
   `status`, `priority`, `assignee`, `reporter`, `components`,
   `fixVersions`, `issuetype`, `project`. Free input allowed for custom
   fields. Tab accepts highlighted suggestion.

2. **Operator** — radio: `in / not / contains / regex / exists`.
   `h/l` or arrows to switch.

3. **Values** — depends on operator:
   - `in` / `not`: chip multi-select. Suggestions come from
     `cache.UniqueValues(field)` (see below). Tab accepts a suggestion;
     Enter adds the typed text as a chip; Backspace on empty input
     removes the last chip.
   - `contains` / `regex`: single text input.
   - `exists`: yes/no toggle (`y`/`n` or `space`).

`Enter` on the last step accepts the row and returns to list state.
`Esc` cancels the row edit.

### UI-pure boundary

The overlay imports neither `internal/structure` nor `internal/jira`. It
operates on a flat `[]ScopeRow{Field, Op string; Values []string}` and
emits `ScopeSavedMsg{Rows []ScopeRow}`. Conversion to/from
`SectionFilter` lives in `internal/tui/structureadapter`.

## Layers and wiring

- `internal/structure/`
  - `types.go` — add `Scope SectionFilter` to `Structure`.
  - `apply.go` — extend section iteration to AND-match against
    `structure.Scope` before per-section predicates.
  - `store.go` — no change beyond YAML round-trip (covered by tests).

- `internal/tui/overlays/scope_editor.go` — new overlay, list + row-editor
  states, `ScopeRow` type, `ScopeSavedMsg`. Pure-UI.

- `internal/tui/structureadapter/` — add helpers
  `RowsFromFilter(SectionFilter) []ScopeRow` and
  `FilterFromRows([]ScopeRow) SectionFilter`. Round-trip tested.

- `internal/tui/cache.go` — add `UniqueValues(field string) []string`
  computed from currently loaded issues. Initial supported fields:
  labels, components, fixVersions, status, priority, assignee, reporter,
  issuetype, project. Unknown field ⇒ empty slice (free input still
  works). No HTTP.

- `internal/tui/keymap.go` — no new top-level binding; the `e` inside
  the Structures overlay is local to that overlay (handled in
  `overlays/structures.go`). Updates limited to the overlay's footer
  hint string.

- `internal/tui/overlays/structures.go` — handle `e` on the highlighted
  entry: emit a new `StructureEditScopeMsg{ID string}`. Update footer
  hint to include `e edit scope`.

- `internal/tui/app.go` — wiring:
  - On `StructureEditScopeMsg`: look up structure by ID, reject with
    toast if `IsReadOnly()`, otherwise build `[]ScopeRow` from
    structure's `Scope` and open `ScopeEditor` overlay.
  - On `ScopeSavedMsg`: convert rows → `SectionFilter`, mutate the
    target structure's `Scope`, persist via store, re-run apply, redraw.

## Edge cases

- **Empty scope after edit** — saving an empty row list clears
  `Structure.Scope` (sets to `nil`), which round-trips to omitted YAML.
- **Duplicate field rows** (e.g. two `labels` rows) — list editor
  prevents adding a second row for an existing field; editing the
  existing row is the intended path. Validation surfaced as inline
  error in row editor on field step.
- **Invalid regex** — row editor validates on Enter and surfaces an
  inline error; row cannot be saved until fixed.
- **Cache empty for a field** — value picker shows no suggestions,
  free input still works.
- **Cancel** — `Esc` discards pending edits without confirmation.
- **Large value lists** — chip multi-select scrolls horizontally
  inside its line; suggestion popup caps at 10 visible items with
  scroll.

## Tests

- `internal/structure/apply_test.go` — table-driven: empty scope (no-op),
  scope matches everything (no-op), scope filters out section, scope +
  section AnyOf, scope + section Filter both required.
- `internal/structure/types_test.go` — YAML round-trip with and without
  `scope` (including `omitempty` behavior).
- `internal/structure/store_test.go` — save → load preserves `Scope`.
- `internal/tui/overlays/scope_editor_test.go` — teatest golden frames:
  empty scope opens with `+ add row` highlighted; add row
  `labels in Q12026,Q22026`; edit existing row; delete row; save emits
  `ScopeSavedMsg`; cancel-with-dirty arms confirm; second-Esc discards.
- `internal/tui/structureadapter/adapter_test.go` — `ScopeRow ↔
  SectionFilter` round-trip across all operators including `exists`,
  `regex`, `contains`.
- `internal/tui/cache_test.go` — `UniqueValues` for labels, components,
  status, assignee from a fixture issue set; unknown field → empty.
- `internal/tui/app_test.go` — `e` opens editor for active structure;
  `e` on read-only structure shows toast and does not open editor;
  `ScopeSavedMsg` mutates active structure and triggers re-apply.

## Coverage of the user's example

`labels in [Q12026, Q22026]`:
1. `S` open structures, highlight target structure, `e`.
2. `a` add row → field `labels` → operator `in` → values
   `Q12026`, `Q22026` → Enter.
3. `s` save. Structure now applies with that scope; every section
   shows only issues matching at least one of those labels.

## Out of scope (for later)

- Cross-field OR (scope-level AnyOf) — separate design when needed.
- API-backed value pickers (labels/components/users endpoints) — same
  UI surface, behind a feature flag.
- Live preview of the result while editing.
- Structure CRUD (create/rename/delete from TUI).
- Importing/exporting scopes between structures.
