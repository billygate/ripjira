# ripjira — internal conventions

Architectural reference for contributors and AI assistants. The user-facing
docs live in `README.md`; the authoritative design spec is
`docs/superpowers/specs/2026-04-30-ripjira-design.md`.

## Product principles

ripjira is a modern client for Jira Cloud. Hold the bar on:

- **Speed.** Cold start to first list under a second when cache is warm.
  Network calls go through `internal/jira` only and are debounced or
  cancelled when the user moves on.
- **Non-blocking UI.** Every load runs as a `tea.Cmd`; the model never
  waits on a sync HTTP call. Detail loads, search, refresh, and assign
  picker all use the cancel-on-supersede pattern (see "Async-cancel
  pattern" below). Optimistic mutations apply locally first and revert
  on error rather than freeze the screen.
- **Rich previews where the terminal supports them.** Modern terminals
  (Kitty, iTerm2, WezTerm, Ghostty) can render images and small files
  inline via the Kitty graphics protocol or iTerm2 inline-image escapes.
  When adding attachments, comments-with-media, or file pickers, detect
  capabilities at startup and degrade gracefully to a placeholder line
  on terminals that don't support graphics. Never block rendering on
  the preview — fetch + decode happens off the main update loop.

## Layering

- `internal/jira/` — HTTP client, DTOs, domain types. The only package
  that talks to Jira. Domain types in `types.go` are what the rest of
  the app consumes; raw API DTOs never escape this package.
- `internal/config/` — YAML loader, `SecretStore` interface (keyring
  impl + env fallback + in-memory fake for tests).
- `internal/state/` — small JSON store at
  `$XDG_STATE_HOME/ripjira/state.json` for runtime memory the user
  shouldn't have to re-supply (last-selected create-wizard project,
  future cursors / view prefs). Atomic writes, mode 0600.
- `internal/tui/` — Bubble Tea models. Subpackages: `panes` (list +
  detail), `overlays` (transition, comment, assign, create wizard,
  help), `grouping` (list strategies), `themes`, `styles`.
- `cmd/ripjira/` — entry point, flag parsing, wiring.

UI code never imports `internal/jira` DTOs and never talks to HTTP
directly. It calls a `Client` interface (defined where the UI consumes
it) so tests can substitute a stub.

## Layout & navigation

The TUI is a two-pane layout (List + Detail) with a horizontal top
**tab strip** above it (`MY ISSUES` / `WATCHING` / `SEARCH`). There is
no left sidebar — view selection is keyboard-only:

- `]` / `[` cycle through tabs.
- `tab` / `shift+tab` cycle focus between List and Detail.
- `,` opens the **Options** overlay (grouping + within-group sort).
  Choices persist in `state.json` and override the YAML default on
  next launch.
- `q` and `ctrl+c` exit silently. `Esc` on the main view (no overlay,
  no editing input) arms a 3-second confirmation toast — a second
  `Esc` within the window quits, any other key cancels. Esc on an open
  overlay always closes the overlay first; the quit-arm only fires
  when there's nothing left to close.

## Theme / style discipline

- Hex literals belong **only** in `internal/tui/themes/*.go`. Every
  other package gets colours through a `Palette` interface or a
  pre-built `Styles` struct. The semantic getters (`Priority`,
  `Status`) keep callers from hard-coding "red means high".
- Adding a theme = one file in `internal/tui/themes/` plus a registry
  line. The `theme_test.go` registry test will fail if a name is
  registered that does not return all required colours.
- Adding a new semantic colour = extend the `Palette` interface and
  every existing palette at once. Don't reach for `lipgloss.Color("…")`
  in a view.

## Async-cancel pattern (detail loads, search, refresh)

Switching the selected issue (or typing in the assign picker) cancels
any work the previous selection started. The pattern:

1. Root model holds a `context.CancelFunc` for the in-flight load.
2. On selection change, call the stored cancel, then create a new
   `ctx, cancel := context.WithCancel(parent)` and store the new
   `cancel`.
3. Dispatch each data piece as its own `tea.Cmd`. The cmd captures the
   ctx by value; if it returns after cancellation, the resulting
   message is dropped by a generation counter on the model so stale
   results never overwrite fresh ones.
4. Errors from cancelled contexts are silently swallowed — they're not
   user-visible failures.

This applies anywhere a user action can supersede an earlier one:
detail tabs, debounced search inputs, manual refresh on top of an
auto-refresh tick.

## Optimistic mutations

Transitions and assignments update the local state first, then call
the API. On failure, revert and surface a toast. The toast queue
(`internal/tui/toast.go`) carries its own TTL so views don't have to
manage timers.

## Tests

- `internal/jira/`: `httptest.Server` for the HTTP layer, table-driven
  tests for ADF + DTO mapping, fixtures in `testdata/`. Coverage
  target ≥80% (`make cover`).
- TUI: `charmbracelet/x/exp/teatest` with golden frames. Stub the Jira
  client at the interface boundary, never via monkey-patching.
- `KeyringStore` real-backend test is gated by a build tag so CI
  without a keychain stays green; `FakeStore` and `EnvFallbackStore`
  cover the rest.

## Commands

```
make build    # ./bin/ripjira
make test     # full suite
make lint     # golangci-lint
make cover    # coverage report
```

## Publication hygiene

Before publishing or pushing a release tag, always delete completed
plans and one-off design specs from `docs/superpowers/plans/` and
`docs/superpowers/specs/`. The repo only keeps the living
architectural reference (`docs/superpowers/specs/2026-04-30-ripjira-design.md`);
dated implementation plans/specs for shipped features are removed —
their content is in git history and the code itself. Same goes for
any personal data (real emails, names tied to internal companies,
absolute home-directory paths) in test fixtures or docs: scrub
before tagging.
