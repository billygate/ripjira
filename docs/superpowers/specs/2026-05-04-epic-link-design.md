# Epic Link & Group-by-Parent — Design Spec

**Date:** 2026-05-04
**Status:** draft, awaiting review

## Goal

Let a user in ripjira (a) see which "epic" each task belongs to, (b) group
the issue list by epic, and (c) attach a task to an epic (or detach it)
without leaving the TUI. The user's Jira (Atlassian Cloud, classic
project `BILLING`) uses the standard `parent` link with a custom issuetype
called `Epic Feature` instead of the built-in `Epic` — verified live: a
sample task has `fields.parent.key = "BILLING-10319"` whose `issuetype.name`
is `Epic Feature`, and `editmeta` allows `set` on the standard `parent`
field (`schema.system = "parent"`).

This is the simplest of the three variants: standard `parent` field,
no custom-field gymnastics, no per-project field-name configuration beyond
naming which issuetypes count as "epic-shaped".

## Out of Scope

- Story points, sprint, components, fix versions — separate plan.
- Story-of-an-epic hierarchy navigation (jumping into an epic's children
  view). v1 only renders the link in the existing list; deep navigation is
  follow-up.
- Migrating any consumer of the legacy `customfield_10014` (Epic Link). We
  use `parent` exclusively — it's the modern path and works on classic and
  team-managed projects in this Cloud instance.
- A bulk "move N tasks to epic" operation. v1 acts on the focused issue.

## Configuration

Add one new key to `internal/config/config.go`'s `Config` struct:

```yaml
# ~/.config/ripjira/config.yaml
epic_issue_types: [Epic, Epic Feature]   # case-insensitive, optional
```

When unset, defaults to `["Epic", "Epic Feature"]` so out-of-the-box ripjira
recognises both standard Atlassian epics and the BILLING-style "Epic
Feature" type. Users with other names (e.g. `Theme`, `Initiative`) override.
Comparison is case-insensitive against `Issue.Type.Name`.

## Domain changes

### `internal/jira/types.go` — extend `Issue`

```go
type Issue struct {
    // ... existing fields ...
    ParentKey     string // empty when no parent set
    ParentSummary string // populated when the parent issue is also in the
                         // current result set; otherwise empty
}
```

`ParentSummary` is a convenience: when the parent epic happens to be in the
fetched list (often the case on the `MY ISSUES` tab where epics + their
tasks coexist), the mapper resolves it in a second pass. When the parent
is not in the list, `ParentSummary == ""` and the UI falls back to
rendering just `ParentKey`. No extra HTTP fetch.

### `internal/jira/issues.go` — mapper

In `(d issueDTO) toDomain`:

1. Read `d.Fields.Parent.Key` into `Issue.ParentKey` if non-empty.
2. After the full slice is mapped (caller-side or in a new `linkParents`
   helper), do a second pass: build `map[key]summary` from the slice and
   populate `ParentSummary` where matches exist.

Add `parent` to the DTO struct:

```go
Parent *struct {
    Key    string `json:"key"`
    Fields struct {
        Summary   string `json:"summary"`
        IssueType struct {
            Name string `json:"name"`
        } `json:"issuetype"`
    } `json:"fields"`
} `json:"parent,omitempty"`
```

The DTO carries `Parent.Fields.Summary` directly when Atlassian populates it
(it usually does on `search/jql`), letting `ParentSummary` come out of the
single fetch even when the epic itself isn't in the list. The second-pass
fill is a safety net for callers that requested only `key,summary,parent`.

### `internal/jira/issues.go` — `SetParent` method

```go
// SetParent attaches issue key to parentKey via PUT /issue/{key} on the
// standard parent field. Pass parentKey == "" to detach.
func (c *Client) SetParent(ctx context.Context, key, parentKey string) error {
    var parent any
    if parentKey == "" {
        parent = nil // Jira interprets null as "remove parent"
    } else {
        parent = map[string]string{"key": parentKey}
    }
    return c.UpdateIssue(ctx, key, map[string]any{"parent": parent})
}
```

### `internal/jira/issues.go` — `SearchEpics`

```go
// SearchEpics returns issues in projectKey whose issuetype name matches any
// of epicTypes (case-insensitive), ordered by updated DESC. Used by the
// epic picker overlay; result is capped at 200 to keep the picker snappy.
func (c *Client) SearchEpics(ctx context.Context, projectKey string, epicTypes []string) ([]Issue, error)
```

JQL is built as `project = "PROJ" AND issuetype in ("Epic","Epic Feature") ORDER BY updated DESC`,
with names quoted/escaped properly. Reuses the existing search code path,
so async-cancel and pagination already work for free.

## UI changes

### Keybinding `E`

Add to `internal/tui/keymap.go`:

```go
EditEpic: key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "set epic")),
```

`E` is currently free (we use `e` for nothing as well, but uppercase reads
as "edit epic" parallel to `T` (title), `M` (description), `P` (priority),
`L` (labels), `D` (due date)).

### `EpicPicker` overlay

New file `internal/tui/overlays/epic.go`. Modeled on `priority.go` and the
upcoming `structures.go`: a single-column list with type-to-filter,
arrow-keys, Enter to pick, Esc to cancel. Adds one extra leading row —
"⊘ No epic (detach)" — when the focused issue currently has a parent.

States:

| State | Behavior |
|---|---|
| List loading | spinner + placeholder; cancellable via Esc |
| Empty list | "No epics in {PROJECT}" + Esc to dismiss |
| Loaded | navigable list, "/" focuses fuzzy-filter |
| Picked | dispatches `EpicPickedMsg{Key: "BILLING-10319"}` (or `Key: ""` for detach) |

Loaded state shows `KEY  Summary` per row; type-to-filter matches against
the concatenation.

### Optimistic-mutation flow

In `app.go`'s `EditEpic` handler:

1. Snapshot the focused issue's `ParentKey` + `ParentSummary`.
2. Open the picker; concurrently dispatch `SearchEpics` as a `tea.Cmd`,
   results land in the picker via a typed message.
3. On `EpicPickedMsg`:
   - Locally set `ParentKey`/`ParentSummary` to the chosen value (resolve
     summary from the picker's loaded slice).
   - Dispatch `SetParent` as a `tea.Cmd`.
   - On error: revert to snapshot, raise a toast `"Could not set parent: …"`.
   - On success: refresh nothing — the optimistic update already shows the
     new state; the next list refresh re-confirms.

This matches the assign/transition flows.

### Detail pane

Render parent above the labels/priority block:

```
Epic     BILLING-10319  Забрать поддержку, настроить деплой …
```

When `ParentSummary` is empty, render only the key. When `ParentKey` is
empty, render `Epic     —`.

### List grouping — `ByParent`

New strategy in `internal/tui/grouping/grouping.go`:

```go
type ByParent struct {
    EpicTypes []string  // populated from config
}
```

`Group(issues)`:

1. Issues whose `Type.Name` matches any `EpicTypes` entry → bucket
   `Key: "Epics"` at the top, sorted by priority desc then updated desc.
2. Other issues bucketed by `ParentKey`. Bucket key format is
   `KEY  Summary` when summary is known, else `KEY`.
3. Issues with empty `ParentKey` → bucket `Key: "No epic"` at the bottom.
4. Buckets between "Epics" and "No epic" are sorted alphabetically by key.

Inside each non-Epic bucket, sort by priority desc, then updated desc, same
as `ByEpicAndPriority`.

Register in `ByName(name)`:

```go
case "parent":
    return ByParent{EpicTypes: cfg.EpicIssueTypes}
```

`grouping.ByName` currently does not take config. Either:

- (chosen) Pass an `Options` struct through `ByName(name, opts)` —
  refactor at call sites in `app.go` (two places).
- Plumb config into `grouping` package via a singleton (rejected;
  globals).

### Options overlay

Add `parent` to the picker rows in `internal/tui/overlays/options.go` so the
new strategy appears alongside `status`, `priority`, `epic`. Label: "By
parent (epic)".

### Help overlay

Add `EditEpic` to the editing column in `Keymap.FullHelp()`.

## Tests

- `internal/jira/issues_test.go`: golden-file test for an issue with a
  parent (use `testdata/issue_with_parent.json`); verify `ParentKey` and
  `ParentSummary` populate correctly. New table case for the post-pass
  cross-reference.
- `internal/jira/client_test.go`: `httptest.Server` test for `SetParent`
  asserting the PUT body shape (`{"fields":{"parent":{"key":"X-1"}}}` and
  the detach case `{"fields":{"parent":null}}`).
- `internal/jira/client_test.go`: `SearchEpics` test verifying the JQL is
  built with quoting + the issuetype list passed through.
- `internal/tui/grouping/grouping_test.go`: `ByParent` table-driven —
  epics-on-top ordering, parent-keyed buckets, "No epic" tail, summary
  fallback when known.
- `internal/tui/overlays/epic_test.go`: type-to-filter, Enter dispatches
  `EpicPickedMsg`, "No epic" leading row only when current parent is set,
  Esc cancels.
- `internal/tui/integration_test.go` (teatest): full flow on a stubbed
  client — focus an issue, press `E`, pick an epic, confirm the detail
  pane and group-header reflect the change immediately, and that the
  client recorded one `SetParent` call.

## Migration / compatibility

- Existing `state.json`, config files, and golden frames need no
  migration. The new config key is optional; absence keeps prior behaviour
  (the new `ByParent` strategy isn't reachable until the user opts in via
  Options).
- Golden frames for the detail pane and list pane will diff because of the
  new "Epic" row and bucket headers. Regenerate under `-update` and review.

## Open questions

None blocking. Two minor follow-ups documented in `docs/follow-ups.md` (or
the project tracker) after merge:

1. Whether `customfield_10014` (legacy Epic Link) needs a fallback for
   pre-modern projects we don't currently use. Skip until a real user hits it.
2. Lazy-fetch of epic summary when not in the result set, with a small
   LRU. Probably wanted once the list grows; not in v1.
