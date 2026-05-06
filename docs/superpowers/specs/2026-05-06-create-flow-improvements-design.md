# Create-flow improvements — design

Branch: `feat/create-flow-improvements`

Bundle of four focused changes to the issue-creation experience:

1. **Post-create popup** — confirm the new key, allow copy / open.
2. **Priority (single-select) marker** — visibly mark the chosen option.
3. **Form ordering** — Summary + Description first, the rest after.
4. **Step 4: Epic link** — optional epic-link step after the form.

Each item is independently shippable; they are bundled here because they
all touch the create flow and can share a single PR / round of manual QA.

---

## 1. Post-create popup

**Problem.** After `n` succeeds, the wizard closes silently. The user has
no visible confirmation of the new issue's key, no way to copy it, and no
way to open it.

**UX.** A persistent centered overlay appears on `CreateSubmitDoneMsg`
with `Err == nil`:

```
┌───────────────────────────────┐
│ Created PROJECT-123           │
│                               │
│ y copy key  Y copy URL        │
│ o open      esc/enter close   │
└───────────────────────────────┘
```

Keys handled by the overlay (and only by it while it's visible):

| Key       | Action                                                |
| --------- | ----------------------------------------------------- |
| `y`       | OSC-52 copy key → toast "Copied key: …", stays open   |
| `Y`       | OSC-52 copy URL → toast "Copied URL: …", stays open   |
| `o`       | open URL in browser, then close                       |
| `Esc`     | close                                                 |
| `Enter`   | close                                                 |

Any other key is swallowed. The bottom hint bar stays unchanged.

**Side-effect on close.** If the post-create list refresh has populated
the new issue into the currently visible list, select it. Otherwise leave
the list selection alone — the overlay closing is purely informational.

**Implementation.**

- New file `internal/tui/overlays/created.go` with type `Created` exposing
  `Show(issue jira.Issue) Created`, `Hide() Created`, `Visible() bool`,
  `Update(tea.Msg) (Created, tea.Cmd)`, `View(styles.Styles) string`.
  No new keymap entries — reuses `CopyKey` / `CopyURL` / `Browser` /
  `CloseOverlay` from the root keymap (passed in at construction time
  similar to `Create.closeBinding`).
- The overlay emits `CreatedDismissedMsg{Key string}` on close so the
  root model can attempt `m.list.SelectByKey(key)` (add this method to
  the list pane if missing — current API has cursor-by-index only).
- `app.go` `CreateSubmitDoneMsg` branch: on success call
  `m.created, cmd = m.created.Show(msg.Issue)` instead of (just)
  closing silently. Existing `dispatchListRefresh` is preserved.
- Update routing: while `m.created.Visible()`, all key messages route
  to it before reaching the list/detail panes.

**Tests.**

- `overlays/created_test.go`: visible after `Show`; `y`/`Y` produce
  expected payloads + keep visible; `o` emits browser-open intent and
  closes; `Esc`/`Enter` close; `j` is swallowed (no close, no
  side-effect).
- `app_test.go`: after a successful `CreateSubmitDoneMsg`, overlay is
  visible; on `Esc`, if the issue key is in the list, list cursor
  lands on it; if not, list cursor unchanged.

---

## 2. Priority single-select marker

**Problem.** `internal/tui/overlays/fields.go::viewOptions` renders
single-select (`FieldKindOption`, e.g. Priority) without any per-row
selection marker. The cursor row is highlighted only when the field
is focused, so:

- Unfocused: nothing tells you which value is current.
- Focused: highlight conflates "cursor position" with "current value"
  while the user is moving through the list.

**Fix.** Add a marker for single-select that always identifies the
chosen value, independent of focus:

```
  ● High
  ○ Medium
  ○ Low
```

The cursor row continues to use `ListItemSelected` styling on top of
the marker.

**Implementation.**

- In `viewOptions`, when `multi == false`, set `marker` to `"● "` for
  the row whose option ID equals the current value (`f.cursor` for
  single-select tracks the value), `"○ "` otherwise.
- Keep `[x] ` / `[ ] ` for multi-select unchanged.

**Tests.**

- `overlays/fields_test.go`: single-select with cursor on row 1
  renders `●` on row 1, `○` on rows 0 and 2. Focused vs unfocused
  both show the marker; only focused shows the cursor highlight.

---

## 3. Form ordering — Summary + Description first

**Problem.** `BuildForm` walks `meta.Fields` in createmeta order, which
in practice is alphabetical-ish and varies per project. Users want to
type the meaningful content (summary, description) immediately and only
then move through pickers (priority, assignee, labels, …).

**Fix.** After classifying fields, reorder the resulting `[]Field`
deterministically:

1. `summary` (always first if present)
2. `description` (next if present)
3. Remaining fields **in their original createmeta order**.

This is purely a presentation reorder; the payload submitted to Jira
is unaffected (it's keyed by field ID).

**Implementation.**

- Small helper in `fields.go`: `reorderFields([]Field) []Field` that
  pulls `summary` and `description` to the front by `Meta.ID`.
  Anything else stays put.
- Call it inside `BuildForm` before computing the initial focus.
- Initial focus stays "first field" → now naturally lands on summary.

**Tests.**

- `overlays/fields_test.go`: `BuildForm` with createmeta containing
  `[priority, summary, assignee, description]` returns Fields in
  order `[summary, description, priority, assignee]`. With neither
  summary nor description present, original order is preserved.

---

## 4. Step 4: Epic link

**Problem.** Today the wizard ends at Step 3 (form). Linking the new
issue to an epic is a separate post-create action. Users want to do
both in one flow.

**UX.** After Step 3 submit succeeds, before the post-create popup,
the wizard advances to Step 4 instead of closing:

```
┌─ Link to epic? ────────────────┐
│ › Filter epics…                │
│                                │
│   ⊘ No epic (skip)             │
│   PROJ-1   Q2 platform work    │
│   PROJ-7   Migration cleanup   │
│   …                            │
└────────────────────────────────┘
```

Reuses the existing `Epic` overlay shape: filterable epic picker, with
a leading `⊘` row. Pressing Enter on `⊘` (or `Esc` on the step) skips
linking and proceeds to the post-create popup. Pressing Enter on an
epic dispatches `CreateIssueLink` (or the `parent`/`customfield_10014`
update API equivalent already used by the existing Epic overlay), then
proceeds.

**Skip rules.** Step 4 is automatically skipped when:

- The created issue is itself an Epic (`selectedType.Name == "Epic"`).
- The wizard was opened via `ShowAsSubtask` (subtasks inherit the
  parent's epic; manual relink isn't useful here).
- The project has no epics — show `⊘` only and auto-advance on Enter.

**Implementation.**

- Add `createStepEpic` to the `createMode` enum, advance into it from
  Step 3 on `CreateSubmitDoneMsg{Err:nil}` (instead of closing) when
  the skip-rules don't apply. The created issue is held on the Create
  overlay until Step 4 resolves.
- Reuse `Epic` overlay logic: extract the existing pick + filter
  rendering into a shared helper if straightforward, otherwise embed
  an `Epic` instance inside `Create` and forward Update/View while in
  `createStepEpic`. The simpler embedding is preferred for v1.
- Epics list is fetched lazily on entering Step 4 via a new message
  (`createEpicsLoadedMsg`); show `Loading…` until it arrives. Cancel
  on supersede via the same context pattern as detail loads.
- On pick: dispatch the link request as a `tea.Cmd`; on Done, advance
  to the post-create popup. On link error, show a toast but still
  proceed to the popup (don't block the user from seeing/copying the
  key just because the link failed).

**Tests.**

- `overlays/create_test.go`: success on Step 3 → Step 4 visible (when
  skip rules don't apply); pressing Enter on `⊘` skips and emits
  popup; pressing Enter on an epic emits link request, then popup
  on link Done.
- Skip-rule cases: Epic issue type → goes straight to popup; subtask
  mode → goes straight to popup.

---

## Out of scope

- Inline epic creation from Step 4 (always pick existing).
- Linking issue types other than Epic (no-op for now).
- Reordering form fields beyond summary + description.
- Persistence of last-used issue type per project (separate feature).

## Test / lint / publication hygiene

- `make test` and `make lint` clean before PR.
- This spec file is removed before tagging the next release per
  CLAUDE.md publication hygiene; its content lives in commit history
  and the implemented code.
