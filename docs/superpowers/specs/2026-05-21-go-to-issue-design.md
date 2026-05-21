# Go to issue — design

Open and edit a Jira issue by key from anywhere in the TUI, and let the
post-create popup jump straight into the new issue instead of only
copying its key or opening the browser.

## Why

Today the detail panel (with status transitions, assignment, comment,
field edits, etc.) is reachable **only** if the issue is already in the
list of the active sub-view. The user has to know that the issue is in
WATCHING / REPORTED / RECENT / SEARCH and switch to the right tab. The
post-create popup just shows the new key with `y` / `Y` / `o` (copy
key / copy URL / browser) — the only way to start editing the freshly
created issue is to remember the key and find it in a list.

This change adds one universal entry point ("open by key") and reuses
it for the post-create flow, so creating an issue and continuing to
write/edit it is a single keystroke away.

## Scope

- Add a key-input overlay (`o`) that loads any issue by key and lands
  the user on the loaded issue inside the existing detail panel.
- Rebind `o` (currently "open in browser") → `O`. Update the popup
  bindings the same way: `o` = open in app, `O` = open in browser.
- Reuse `recentKeys` + the existing `ViewRecent` sub-view as the
  destination. No new top tab, no new view kind.

Out of scope:
- A persistent "history" tab beyond the existing `recentKeys` bound.
- Bulk-open or paste-multiple-keys.
- Cross-instance keys (multi-Jira-cloud).

## UX

### 1. Go to issue overlay

- Bound to `o` on the main view (any tab, no other overlay open).
- Centered box, single text input, the same `OverlayBorder` style as
  the Created popup.
- Layout:
  ```
  ┌─ Go to issue ───────────────┐
  │  ABC-123_                   │
  │  enter open · esc cancel    │
  └─────────────────────────────┘
  ```
- Input is normalised on submit:
  - Trim whitespace and surrounding angle brackets / quotes.
  - Uppercase the project prefix.
  - Validate against `^[A-Z][A-Z0-9_]+-[0-9]+$`. Invalid → keep the
    overlay open, surface a toast "invalid issue key".
- Enter:
  - emits `GoToIssueMsg{Key: ...}`,
  - the root model calls a new `Model.openIssueByKey(key string)`
    helper (see "Implementation" below),
  - the overlay closes.
- Esc closes without emitting.

### 2. Post-create popup

The existing `Created` overlay (`internal/tui/overlays/created.go`)
gets one new action and one renamed action:

| key | before              | after                          |
| --- | ------------------- | ------------------------------ |
| `y` | copy key            | (unchanged)                    |
| `Y` | copy URL            | (unchanged)                    |
| `o` | open browser        | **open in app** (jump to it)   |
| `O` | (none)              | **open browser**               |
| esc | close               | (unchanged)                    |
| ⏎   | close               | (unchanged)                    |

`o` emits a new `CreatedOpenInAppMsg{Key string}` and dismisses the
overlay; the root model handles it through the same
`Model.openIssueByKey` helper as the overlay.

The hint line in `Created.View` is updated:
```
y copy key   Y copy URL   o open   O browser   esc/enter close
```

### 3. Global o → O rebind

`Keymap.Browser` moves from `o` to `O`. The hint bar at the bottom
shows movement / tab navigation / `?` only (CLAUDE.md), so it doesn't
need an edit. The help overlay updates automatically because it reads
`Keymap.FullHelp()`. The README Keymap table is updated.

## Architecture

### New module: `internal/tui/overlays/goto.go`

Small overlay analogous to `Created` and the search/comment input
overlays:

```go
type Goto struct {
    visible bool
    input   textinput.Model
    keymap  GotoKeymap   // submit / cancel bindings (uses existing Open / CloseOverlay)
}

type GoToIssueMsg struct{ Key string }

func NewGoto(open, closeOverlay key.Binding) Goto
func (g Goto) Visible() bool
func (g Goto) Show() Goto
func (g Goto) Hide() Goto
func (g Goto) Update(msg tea.Msg) (Goto, tea.Cmd)
func (g Goto) View(s styles.Styles) string
```

The overlay does **not** touch the network. It only validates format
and emits `GoToIssueMsg`. Loading and tab-switching is the root model's
responsibility — this keeps the overlay testable in isolation and
matches how the other overlays are structured (`Created`, `Comment`,
`Transition`).

### New message and helper on `Model`

```go
type CreatedOpenInAppMsg struct{ Key string }

// openIssueByKey is the single entry point used by both the goto
// overlay and the post-create popup. It:
//   1. pushes key to recentKeys (head, dedup, cap),
//   2. switches the active top tab to TopMyIssues and sub-view to
//      ViewRecent (saving lastSubView so `]`/`[` still work),
//   3. selects the row by key once the recent list has reloaded,
//   4. detail-loads on selection via the existing async-cancel path.
//
// All steps already exist as primitives; this helper composes them.
func (m Model) openIssueByKey(key string) (Model, tea.Cmd)
```

`recentKeys` push and the Recent sub-view re-fetch already exist
(see `app.go` Model fields and `app_list.go`); the helper just wires
the existing primitives.

### Wiring

- `app_struct.go`: add field `goto overlays.Goto`.
- `app.go` constructor (`New`): initialise `goto` and the new keymap
  entry `GoToIssue: key.NewBinding(key.WithKeys("o"), …)`.
- `keymap.go`: add `GoToIssue` to the `Keymap` struct, `DefaultKeymap`,
  `All()`, `FullHelp()`. Change `Browser` keys from `"o"` to `"O"`.
- `app_overlays.go`: route key events to `goto` when it is visible;
  add an `IsAnyOverlayVisible()` clause for it; render in `View`
  z-order alongside other overlays (above tabs, below toasts — same
  as `Created`).
- `app.go` `Update`: handle `overlays.GoToIssueMsg` and
  `overlays.CreatedOpenInAppMsg` by calling `openIssueByKey`.
- `overlays/created.go`: add `openInApp` key binding, change `browser`
  binding key from `"o"` to `"O"`, add the new message,
  update `View()` hints.

### Failure modes

- **Invalid key format** → toast "invalid issue key"; overlay stays
  open.
- **Issue not found / 403** → toast surfaces the loader error (already
  the case for any detail load). The key still gets pushed into
  recents so the user can retry or remove it; this matches existing
  Recent-list behaviour for stale keys.
- **Network failure** → same as any other detail load — toast with
  retry advice.

## Tests

- `overlays/goto_test.go`
  - Hidden Goto is a no-op on key messages.
  - Submitting `abc-123` (lowercase) emits `GoToIssueMsg{Key:"ABC-123"}`.
  - Submitting `not-a-key` does not emit; surfaces an "invalid issue
    key" message (toast emitted by overlay or via a returned cmd —
    decide during implementation; test asserts the observable outcome).
  - Esc hides without emitting.
- `overlays/created_test.go`
  - Pressing `o` emits `CreatedOpenInAppMsg{Key}` and dismisses.
  - Pressing `O` emits `CreatedOpenRequestedMsg{URL}` and dismisses.
  - View string contains both `o open` and `O browser` hints.
- `keymap_test.go` (existing) — verifies every binding has help text;
  no change needed beyond adding the new entry to `All()` /
  `FullHelp()`.
- `app_test.go` / `integration_test.go`
  - `o` from main view → Goto visible → submit `ABC-123` → active
    sub-view is `ViewRecent`, `selectedKey == "ABC-123"`, detail load
    cmd dispatched.
  - After create flow: `Created.Show(issue)` is visible → press `o` →
    overlay dismissed → same destination state as above.
  - `O` from main view still calls `BrowserOpener` with the current
    selection's URL (regression).
- README Keymap table updated; help-overlay test (
  `app_test.go::TestHelpOverlay…` if present) asserts the new row.

## Open questions

- None — go-by-key destination and key bindings settled with the user
  (overlay `o`, browser `O`, post-create popup `o`=in-app /
  `O`=browser, destination = `ViewRecent` sub-view under MY ISSUES).

## Files touched

| path | change |
| ---- | ------ |
| `internal/tui/overlays/goto.go` | new |
| `internal/tui/overlays/goto_test.go` | new |
| `internal/tui/overlays/created.go` | new `o` action, rename browser → `O`, hint line |
| `internal/tui/overlays/created_test.go` | cover `o` / `O` split |
| `internal/tui/keymap.go` | `GoToIssue` binding; `Browser` keys `o` → `O`; `All()` / `FullHelp()` |
| `internal/tui/app_struct.go` | `goto overlays.Goto` field |
| `internal/tui/app.go` | construct overlay, dispatch helper, handle messages |
| `internal/tui/app_overlays.go` | route + render |
| `internal/tui/app_test.go` | overlay routing, post-create handoff |
| `internal/tui/integration_test.go` | end-to-end go-to-issue scenario |
| `README.md` | Keymap table: add `o`, update `O` |
