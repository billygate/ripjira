# In-app settings editor

Status: design, ready for plan
Date: 2026-05-07

## Problem

`config.yaml` is the only way to change UI preferences (theme, icons,
default grouping, auto-refresh interval, epic issue types). Users must
quit ripjira, edit the file by hand, and relaunch — even for
trivial changes like switching the theme.

## Goal

A modal overlay (`ctrl+,`) that lets the user edit the UI-side fields of
`config.yaml` from inside the app. Apply changes live where possible
(theme, icons, auto-refresh interval), persist to YAML on confirm,
revert on cancel.

## Non-goals

- Editing connection fields (`base_url`, `email`) or the API token. The
  account configuration stays in YAML / keyring.
- Editing `custom_fields`, `default_project`, structures, or favorites.
- Preserving comments or formatting in `config.yaml`. The current
  `config.Save` rewrites the file via `yaml.Marshal`; that behavior is
  inherited as-is.
- Live preview while scrolling enum values. Selection only takes effect
  on explicit confirm.
- A new top tab or full-screen settings view. Overlay is the only
  surface.

## Scope of editable fields

| Field                  | Type                  | Editor           |
|------------------------|-----------------------|------------------|
| `theme`                | enum (`validThemes`)  | ←/→ cycle        |
| `icons`                | enum (`unicode`/`ascii`) | ←/→ cycle    |
| `default_grouping`     | enum (`status`/`priority`/`epic`/`parent`) | ←/→ cycle |
| `auto_refresh_seconds` | int ≥ 0               | Enter → input mode, Enter commit, Esc revert field |
| `epic_issue_types`     | `[]string`            | Enter → sub-overlay |

All other `Config` fields round-trip unchanged through `yaml.Marshal`
because the overlay never touches them.

## UX

### Entry point

- Global hotkey `ctrl+,` (new binding `Settings` in `keymap.go`). Opens
  the overlay over any view. The existing `,` (Options: session
  grouping/sort) is unrelated and unchanged.

### Overlay layout

A single vertical list of `label : value` rows in the order above.
Cursor `▸` marks the current row.

```
  Settings

  ▸ Theme              tokyonight       ◂ ▸
    Icons              unicode
    Default grouping   status
    Auto refresh (s)   60
    Epic issue types   Epic, Epic Feature

  ↑/↓ row · ←/→ change · enter edit/open · ctrl+s save · esc cancel
```

- `↑`/`↓`: move cursor.
- `←`/`→`: cycle enum values for the row under cursor (theme, icons,
  default grouping). Wraps.
- `Enter`:
  - On `auto_refresh_seconds`: enter input mode (textinput). Enter
    again commits the parsed int into the draft (rejecting negative
    or non-numeric input with a row-level inline error). Esc exits
    input mode and reverts the field to the draft value before the
    edit started.
  - On `epic_issue_types`: open `EpicTypes` sub-overlay.
  - On enum rows: same as `→` (one cycle).
- `ctrl+s`: validate draft, emit `SettingsAppliedMsg{NewCfg}`, close.
  Enter is intentionally not bound to "save" because it is already
  overloaded for cycle/edit/open-sub-overlay; an explicit chord
  avoids ambiguity.
- `Esc`: emit `SettingsCancelledMsg{}`, close. Draft is discarded.

### Epic types sub-overlay

```
  Epic issue types

  ▸ Epic
    Epic Feature

  a add · d delete · e edit · enter back · esc cancel
```

- `a`: opens an inline textinput, Enter appends, Esc cancels.
- `d`: removes the row under cursor.
- `e`: opens textinput pre-filled with the row, Enter replaces, Esc
  cancels.
- `Enter` (when not in textinput): emits
  `EpicTypesAppliedMsg{Items}` and closes the sub-overlay; control
  returns to the settings overlay with the new list reflected in the
  draft.
- `Esc`: emits `EpicTypesCancelledMsg{}`; the settings draft for
  `epic_issue_types` is unchanged.

## Architecture

### File layout

New files:
- `internal/tui/overlays/settings.go` — `Settings` overlay model.
- `internal/tui/overlays/settings_test.go`
- `internal/tui/overlays/epic_types.go` — `EpicTypes` sub-overlay.
- `internal/tui/overlays/epic_types_test.go`

Edited files:
- `internal/tui/keymap.go` — add `Settings key.Binding`, wire into
  `DefaultKeymap()`, `All()`, `FullHelp()`.
- `internal/tui/messages.go` — add the four new message types.
- `internal/tui/app_struct.go` — add `settings overlays.Settings` and
  `epicTypes overlays.EpicTypes` (the sub-overlay) fields.
- `internal/tui/app_keymap.go` — route `ctrl+,` to open settings.
- `internal/tui/app_overlays.go` — visibility check, message
  forwarding, render order.
- `internal/tui/app.go` — handlers for `SettingsAppliedMsg`,
  `SettingsCancelledMsg`, `SettingsSaveErrorMsg`,
  `EpicTypesAppliedMsg`, `EpicTypesCancelledMsg`.
- `README.md` — keymap table entry for `ctrl+,`.

### Overlay model

```go
type Settings struct {
    visible      bool
    closeBinding key.Binding

    draft    config.Config // mutated as user edits
    cursor   int           // 0..4
    editing  bool          // textinput mode for auto_refresh_seconds
    input    textinput.Model
    rowError string        // inline validation message for current row
}

func NewSettings(closeKey key.Binding) Settings
func (Settings) Visible() bool
func (Settings) Show(current config.Config) Settings
func (Settings) Hide() Settings
func (Settings) Update(msg tea.Msg) (Settings, tea.Cmd)
func (Settings) View(s styles.Styles) string
func (Settings) Draft() config.Config
```

`EpicTypes` follows the same shape with its own `items []string`,
`cursor`, `editing`, `input`.

### Messages

```go
// in internal/tui/messages.go
type SettingsAppliedMsg struct{ NewCfg config.Config }
type SettingsCancelledMsg struct{}
type SettingsSaveErrorMsg struct {
    Draft config.Config
    Err   error
}
type EpicTypesAppliedMsg struct{ Items []string }
type EpicTypesCancelledMsg struct{}
```

### Data flow

1. User presses `ctrl+,` → app sets `m.settings = m.settings.Show(m.cfg)`.
2. User navigates / edits → all changes mutate `settings.draft` only.
3. User presses `ctrl+s` → overlay calls `settings.draft.Validate()`.
   On error, set `rowError` and stay open. On success, emit
   `SettingsAppliedMsg{NewCfg: draft}` and call `Hide()`.
4. App receives `SettingsAppliedMsg`:
   - Compute diff between `m.cfg` and `NewCfg`.
   - If `Theme` changed: rebuild `m.palette` via `themes.ByName(NewCfg.Theme)`
     (returns `(Palette, error)`; `Validate()` already enforced enum
     membership so the error path is unreachable), rebuild
     `m.styles = styles.New(palette, NewCfg.Icons)`. No further
     propagation required: panes and overlays consume `Styles` through
     their `View(s styles.Styles)` parameter on every paint, so the
     next render picks up the new palette automatically.
   - If `Icons` changed (and theme didn't): rebuild styles only.
   - If `AutoRefreshSeconds` changed: cancel the in-flight refresh
     timer, schedule a new tick at the new interval.
   - `DefaultGrouping` and `EpicIssueTypes` are stored in `m.cfg`
     only (do not retroactively re-group the current session).
   - Set `m.cfg = NewCfg`. Dispatch a `tea.Cmd` that calls
     `config.Save(m.cfgPath, &m.cfg)`. On error, returns
     `SettingsSaveErrorMsg{Draft: NewCfg, Err: err}`.
5. App receives `SettingsSaveErrorMsg`:
   - Push toast `"failed to save settings: <err>"`.
   - Re-open settings overlay with `Show(Draft)` so the user can fix
     and retry. `m.cfg` already reflects the live-applied values; the
     overlay re-seeds from the same draft.
6. App receives `SettingsCancelledMsg`: nothing to do (draft was never
   touched).
7. App receives `EpicTypesAppliedMsg{Items}`:
   - Forward into the open `Settings` overlay via a setter
     (`settings = settings.WithEpicTypes(Items)`), which writes
     `Items` into `settings.draft.EpicIssueTypes`. The overlay
     remains visible.
8. App receives `EpicTypesCancelledMsg`: same forwarding, but
   discarding — overlay stays visible, draft unchanged.

### Live application notes

- Theme/icons rebuild happens **inside the message handler**, before
  the YAML write. If the YAML write fails, the new theme is already
  applied; the user sees the toast and the re-opened overlay still
  shows the new draft, so the in-memory state and the visible state
  are consistent. This matches the chosen "save error keeps overlay
  open" semantics (clarifying question 8).
- `auto_refresh_seconds = 0` keeps the existing semantics in the
  refresh code path (no auto-refresh). The validator already enforces
  `>= 0`.

## Validation

`config.Config.Validate()` is reused unchanged. Specifically:
- `Theme`, `Icons`, `DefaultGrouping`: enum membership.
- `AutoRefreshSeconds >= 0`.
- `BaseURL`, `Email` are not editable here, so their existing values
  pass validation untouched.

`epic_issue_types` has no validator today; the editor accepts any
non-empty string for added items. Empty strings are rejected at the
input boundary in the sub-overlay.

## Error handling

- Invalid input in a row (e.g. `auto_refresh_seconds = -1` or
  non-numeric): inline `rowError` displayed under the row, `ctrl+s`
  short-circuits to a no-op until the row is fixed.
- `config.Save` failure: toast + re-open overlay with the draft
  preserved (see step 5 above).
- `themes.ByName` returning a missing palette is impossible because
  `Validate()` already enforces enum membership before we get here.
  No defensive code added.

## Testing

### Overlay unit tests

`internal/tui/overlays/settings_test.go`:
- Navigation: `↑`/`↓` move cursor and clamp at boundaries.
- Enum cycling: `→` from last value wraps to first; `←` from first
  wraps to last; covers theme / icons / default grouping rows.
- `auto_refresh_seconds`: Enter → editing mode; type "30" + Enter →
  draft has 30; "-1" + Enter → `rowError` set, draft unchanged; Esc
  in editing mode → exits without commit.
- `ctrl+s` with valid draft → returns `SettingsAppliedMsg` with the
  expected `NewCfg`.
- `ctrl+s` with invalid draft (only path: editing mode left in an
  invalid value) → no message, `rowError` shown.
- `Esc` (not in editing mode) → returns `SettingsCancelledMsg`.
- `Show(current)` re-seeds draft and resets cursor / editing state.

`internal/tui/overlays/epic_types_test.go`:
- `a` add flow: produces a new item; empty input rejected.
- `d` delete flow: removes correct row; cursor clamps.
- `e` edit flow: replaces row; Esc reverts.
- `Enter` returns `EpicTypesAppliedMsg{Items}`.
- `Esc` returns `EpicTypesCancelledMsg`.

### App-level integration

`internal/tui/app_test.go` (new test cases):
- `ctrl+,` opens overlay.
- `SettingsAppliedMsg` with new `Theme` rebuilds `m.palette` (assert
  against `themes.ByName`); `m.styles` is rebuilt; `m.cfg.Theme` is
  set; `config.Save` writes the file (use `t.TempDir()` for `cfgPath`
  and read the resulting YAML).
- `SettingsAppliedMsg` with new `AutoRefreshSeconds` reschedules the
  refresh tick (assert via the same generation-counter pattern other
  tests use, or by stubbing the refresh ticker).
- `SettingsSaveErrorMsg` re-opens the overlay with the draft and
  enqueues a toast. Use a read-only temp dir to force the write
  failure.
- `SettingsCancelledMsg` is a no-op (`m.cfg` unchanged, no file
  written).

### Golden frame

Add one frame in `internal/tui/integration_test.go` covering the
overlay open state to lock the layout.

## Hotkeys / docs

Update all three places per CLAUDE.md:
1. `internal/tui/keymap.go`: new `Settings` binding (`ctrl+,`,
   help text "settings"), included in `All()` and the
   "Overlays" column of `FullHelp()`.
2. `?` help overlay: automatic via `FullHelp()`. The existing test
   asserting every binding is documented should pass without
   modification.
3. `README.md`: add a row to the keymap table.

The bottom hint bar is intentionally not extended (CLAUDE.md
constraint).

## Migration / rollout

- No config schema change. Existing `config.yaml` files are
  unaffected.
- First save through the editor rewrites the file via
  `yaml.Marshal` and drops any user comments / blank lines. This is
  inherited behavior, called out in non-goals; no toast warning is
  added.
- No new state in `state.json`.
