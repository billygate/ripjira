# Boards view — design spec

Date: 2026-05-04
Status: approved, ready for planning

## Goal

Add a `BOARDS` top-level tab to the ripjira TUI that lets the user
browse the agile boards of the default project and work with one
board at a time — Scrum (active sprint + backlog) or Kanban
(columns) — without leaving the terminal.

## UX

### Tab strip

New top-level tab `BOARDS` joins the existing strip:

```
MY ISSUES | WATCHING | SEARCH | BOARDS
```

It cycles via `]`/`[` and is reachable from the `g` go-to overlay,
same as the others.

### Two states inside the tab

1. **Picker** — a list of boards from the default project. Shown on
   first entry, after `b`, or when no `LastBoardID` is stored.
   Columns: `Name`, `Type` (Scrum/Kanban), `Project`. Type-to-filter
   like the `g` overlay. `↑/↓` + `Enter` to open.
2. **Board view** — the open board. The board ID persists to
   `state.json` (`LastBoardID`), so re-entering the tab opens the
   same board. `b` returns to the picker to switch boards.

### Board view layout

Two-pane layout, same as the rest of the app: left = list/board,
right = detail. `tab` / `shift+tab` cycles focus.

- **Scrum board** — sub-tab strip `Active Sprint | Backlog` under
  the top-tabs, switched with `}`/`{` (same drill-down pattern
  Structures already uses). `Active Sprint` shows columns;
  `Backlog` shows a flat list ordered by Jira rank.
- **Kanban board** — no sub-tabs; columns shown directly.

A column = vertical list of cards. Card content: `KEY`, summary
(truncated to fit), priority dot, assignee initials. Columns and
their status mappings come from the board configuration endpoint.

### Narrow-terminal fallback

Below 100 columns the column layout collapses to a single-column
list grouped by status (column heading = group heading). `h`/`l`
become no-ops in this mode; everything else works as in the
regular list.

## Hotkeys

| Key       | Action                                                         |
|-----------|----------------------------------------------------------------|
| `]` / `[` | Cycle top-tabs (BOARDS included)                               |
| `}` / `{` | Cycle sub-tabs inside a Scrum board                            |
| `h` / `l` | Move column left / right (when list pane is focused)           |
| `j` / `k` | Move card down / up within the current column                  |
| `>` / `<` | Transition selected issue to the next / previous column        |
| `b`       | Return to the board picker                                     |
| `Enter`   | In picker — open the highlighted board                         |
| `Esc`     | Close picker if open; otherwise the standard armed-quit prompt |

`>`/`<` use the existing optimistic-transition machinery: the card
moves immediately, a `tea.Cmd` calls Jira, on error the move is
reverted and a toast is queued. Cancellation follows the same
generation-counter pattern as detail loads.

All three keymap surfaces must be updated together
(`internal/tui/keymap.go` struct + `DefaultKeymap` + `All` +
`FullHelp`, the `?` help overlay verification, the `README.md`
keymap table) — see the project's CLAUDE.md hotkey rule.

## Architecture

### `internal/jira/` additions

Domain types in `types.go`:

- `Board` — id, name, type (`scrum`/`kanban`), project key.
- `BoardColumn` — name, ordered list of statuses that map into it.
- `Sprint` — id, name, state, start/end dates.

New methods on the existing `Client`:

| Method                                   | Endpoint                                                        |
|------------------------------------------|-----------------------------------------------------------------|
| `ListBoards(ctx, projectKey)`            | `GET /rest/agile/1.0/board?projectKeyOrId=…`                    |
| `GetBoardConfig(ctx, boardID)`           | `GET /rest/agile/1.0/board/{id}/configuration`                  |
| `GetActiveSprint(ctx, boardID)`          | `GET /rest/agile/1.0/board/{id}/sprint?state=active` (first)    |
| `ListSprintIssues(ctx, sprintID)`        | `GET /rest/agile/1.0/sprint/{id}/issue`                         |
| `ListBoardBacklog(ctx, boardID)`         | `GET /rest/agile/1.0/board/{id}/backlog`                        |
| `ListBoardIssues(ctx, boardID)`          | `GET /rest/agile/1.0/board/{id}/issue` (Kanban)                 |

DTOs stay inside the package; only domain types cross the
boundary, per the project's layering rule.

### `internal/tui/panes/board/`

New sub-package:

- `picker.go` — board picker model (list + filter input).
- `view.go` — orchestrates Scrum vs Kanban, owns sub-tab state,
  column data, selection, and the focus contract with
  `panes/detail`.
- `column.go` — renders one column (lipgloss styles via the
  shared `Palette`/`Styles` — no hex literals here).
- `card.go` — renders one card.

UI talks to a `Client` interface defined in this sub-package,
not to `internal/jira` directly — same pattern the existing
panes use, so tests can stub it.

### `internal/tui/app.go`

Extend the `view` enum with `viewBoards` and route `]`/`[`,
`g`-overlay, and persistence through it. The detail pane is
reused as-is.

### `internal/state/`

Add `LastBoardID int` to the persisted state struct. A
per-project map is left out for now (YAGNI — there is one
default project today).

### Async, cancellation, cache

All loads follow the cancel-on-supersede pattern documented in
CLAUDE.md.

- Opening the picker dispatches `ListBoards` as a `tea.Cmd`;
  generation counter drops stale results.
- Picking a board dispatches the per-type loads in parallel:
  Scrum → `GetBoardConfig` + `GetActiveSprint` (then
  `ListSprintIssues` once the sprint is known); Kanban →
  `GetBoardConfig` + `ListBoardIssues`.
- Switching sub-tabs is instant from cache, with a refetch
  underneath; each sub-tab has its own cancel.
- `>`/`<` transition: optimistic local move + `tea.Cmd` to Jira;
  revert + toast on error.

`loader_cache` gains keys: `board:<id>:config`,
`board:<id>:sprint:<sprintID>:issues`, `board:<id>:backlog`,
`board:<id>:issues` (Kanban).

## Tests

- `internal/jira/agile_test.go` — `httptest.Server` for the six
  new endpoints; fixtures in `testdata/agile/`. Coverage ≥80%
  per `make cover`, in line with the rest of the package.
- `internal/tui/panes/board/*_test.go` — `teatest` golden frames
  for: picker, Scrum view (active sprint + backlog), Kanban view,
  narrow-terminal fallback, optimistic transition (`>`) success
  and revert paths. The Jira client is stubbed at the `Client`
  interface defined inside the board sub-package.

## Docs to update alongside the code

- `internal/tui/keymap.go` — keymap struct, `DefaultKeymap`,
  `All`, `FullHelp` (test asserts every binding is documented).
- `README.md` — keymap table; new `Boards` user-facing section.
- `docs/superpowers/specs/2026-04-30-ripjira-design.md` —
  add a Boards paragraph to the living architectural reference.

## Out of scope

- Drag-and-drop with the mouse.
- Editing the board configuration (column → status mapping).
- Multiple-project board picker.
- Sprint planning / sprint start / sprint complete actions.
- Inline media previews on cards (the existing image-rendering
  capability remains where it already is, in detail view).

These can come later as separate specs.
