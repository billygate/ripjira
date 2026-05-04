# ripjira

A keyboard-first TUI client for Jira Cloud, written in Go on top of
[Bubble Tea](https://github.com/charmbracelet/bubbletea). `ripjira`
shows the issues currently assigned to you, lets you triage them
(transition, comment, reassign, open in browser), and creates new
issues with a dynamic form built from Jira's createmeta — all without
leaving your terminal.

## Features

- Two-pane layout (List + Detail) with a horizontal top tab strip:
  `MY ISSUES` / `WATCHING` / `SEARCH`
- Inline JQL/text search that toggles between an editable input and a
  collapsed `🔍 query` header above the results
- Group + sort options overlay (`,`) — choices persist across launches
- Async loading with cancellation: switching issues, views, or search
  queries never leaves stale loaders behind
- Optimistic mutations: status transition, assign, and comment apply
  locally first and revert on error
- Dynamic create form driven by Jira's `createmeta` (text, ADF,
  options, users, dates, numbers); subtask mode included
- Inline image previews and HTML→Markdown rendering on terminals that
  support the Kitty graphics protocol or iTerm2 inline images;
  graceful fallback elsewhere
- Disk cache so the first frame paints instantly on launch
  (My Issues only — Watching/Search results are runtime-only)
- Five built-in themes: Tokyo Night, Catppuccin, Gruvbox, Nord, Rosé Pine
- First-run wizard that probes `/myself` before saving credentials

## Install

### Homebrew

```sh
brew tap billygate/tap
brew install ripjira
```

This installs both `ripjira` and the short alias `rj`.

### From source

Requires Go 1.26 or newer.

```sh
go install github.com/billygate/ripjira/cmd/ripjira@latest
```

Or build from a checkout:

```sh
git clone https://github.com/billygate/ripjira
cd ripjira
make build      # produces ./bin/ripjira
```

## First run

The first launch opens a wizard that captures everything `ripjira`
needs:

1. Jira base URL (e.g. `https://acme.atlassian.net`)
2. Account email
3. Atlassian API token — created at
   <https://id.atlassian.com/manage-profile/security/api-tokens>
4. Optional default project key

The wizard calls `/rest/api/3/myself` with the supplied credentials
before saving anything; if the call fails you stay on the token step
with the error inlined. On success:

- `~/.config/ripjira/config.yaml` is written with mode `0600`
- the API token is stored in the OS keychain (macOS Keychain, GNOME
  Keyring / libsecret, Windows Credential Manager) under service
  `ripjira`, account `<your-email>`

To reconfigure later:

```sh
ripjira login           # re-run the wizard, existing values pre-filled
ripjira login --reset   # delete the stored token first, then re-run
```

## Keymap

| Key                | Action                                |
| ------------------ | ------------------------------------- |
| `↑`/`k`, `↓`/`j`   | move up / down                        |
| `tab`, `shift+tab` | focus next / previous pane            |
| `}`, `{`           | next / previous top tab               |
| `]`, `[`           | next / previous sub-tab               |
| `g`, `G`           | jump to top / bottom                  |
| `space`            | collapse / expand current group       |
| `,`                | open Options (grouping + sort)        |
| `/`                | filter list / open Search tab         |
| `enter`            | open issue                            |
| `s`                | change status (transition picker)     |
| `a`                | assign to a user                      |
| `E`                | edit epic (set or detach parent)      |
| `c`                | add a comment                         |
| `n`                | create a new issue                    |
| `S`                | create a subtask of the current issue |
| `o`                | open current issue in your browser    |
| `r`                | force refresh                         |
| `\`                | pick a structure (Structures tab)     |
| `?`                | show full help overlay                |
| `esc`              | close overlay (or arm quit)           |
| `q`, `ctrl+c`      | quit                                  |

`esc` on the main view (no overlay, no editing input) arms a
3-second quit confirmation — a second `esc` within the window quits,
any other key cancels.

Hotkeys also fire on Cyrillic and Greek keyboard layouts, so you
don't need to switch input methods to drive the UI.

## Structures

The `STRUCTURES` tab groups issues into named **sections** defined per project.
Two built-ins ship in code (`default` and `inbox`); user-defined structures
live as YAML at `~/.config/ripjira/structures/<PROJECT>.yml` and hot-reload
on file change. Inside the tab `\` opens the picker, `}` / `{` cycle.

Minimal example:

```yaml
- id: my-team
  name: My team
  sections:
    - title: In progress
      filter:
        status: [Open, "In Progress"]
        assignee: { exists: true }
      group_by: [priority]
    - title: Blocked
      filter:
        labels: [blocker]
```

Filter clauses accept a shorthand array (`In`) or a `{in, not, regex,
contains, exists}` object. Across keys the predicates AND together; `any_of`
provides OR semantics. Built-in field names: `status`, `status_category`,
`priority`, `issuetype`, `assignee`, `reporter`, `parent_key`, `labels`,
`project`.

## Themes

Set `theme:` in `~/.config/ripjira/config.yaml` to any of:

- `tokyonight` (default)
- `catppuccin`
- `gruvbox`
- `nord`
- `rosepine`

Themes are resolved at startup; restart `ripjira` after editing the
config.

## Configuration reference

`~/.config/ripjira/config.yaml`:

```yaml
base_url: https://acme.atlassian.net
email: you@acme.com
default_project: PROJ          # optional; pre-selects in create overlay
default_grouping: status       # status | priority | epic_priority | parent
auto_refresh_seconds: 60       # 0 disables; otherwise silent list refresh
theme: tokyonight              # see Themes above
icons: unicode                 # unicode | ascii
epic_issue_types:              # issuetypes treated as epic-shaped
  - Epic
  - Epic Feature
```

The `parent` grouping buckets tasks under their parent epic — epics on
top, then one bucket per parent, then a trailing "No epic" bucket.
`epic_issue_types` controls which issuetype names count as epic-shaped
when listing pickable parents and rendering the epics-on-top section.

`XDG_CONFIG_HOME` is honoured if set. The file is rewritten with mode
`0600`; `ripjira` warns (but still loads) if it finds wider permissions.

Runtime UI state (last grouping/sort, last create-wizard project) is
kept in `$XDG_STATE_HOME/ripjira/state.json` so options survive
restarts without you re-supplying them.

## Troubleshooting

- Set `RIPJIRA_DEBUG=1` to write every HTTP request and response to
  `~/.cache/ripjira/debug.log`. The `Authorization` header is masked.
- If the OS keychain is unavailable (e.g. headless CI, locked
  session), set `RIPJIRA_TOKEN=<api-token>`. `ripjira` falls back to
  the env var when no keychain entry exists.
- "permissions wider than 0600" warning: `chmod 600
  ~/.config/ripjira/config.yaml`.
- Auth errors after a working session usually mean the token was
  rotated. Run `ripjira login --reset` to clear it and re-enter.
- Unknown create-form fields are skipped with a warning rather than
  blocking submit. Required fields you cannot fill from the TUI must
  be set after creation in the web UI.

## Development

```sh
make test    # full test suite
make lint    # golangci-lint
make cover   # coverage report; ≥80% target for internal/jira
make build   # produce ./bin/ripjira
```

The architecture is documented at
`docs/superpowers/specs/2026-04-30-ripjira-design.md`.

## License

[PolyForm Noncommercial 1.0.0](LICENSE) — free for personal,
research, educational, and noncommercial use. Commercial use
requires a separate license; open an issue to discuss.
