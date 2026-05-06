package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/structure"
)

// TickerFunc schedules a one-shot tea.Cmd that fires after d and turns the
// fire instant into a tea.Msg via fn. The default is tea.Tick; tests pass a
// no-op so they can drive ticks deterministically by sending the message.
type TickerFunc func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd

// Option configures a Model at construction time.
type Option func(*Model)

// WithStatus sets initial top-bar status text (e.g. "⟳ refreshing…").
func WithStatus(s string) Option {
	return func(m *Model) { m.statusText = s }
}

// WithLoader attaches a data source. Without one the TUI still renders, but
// list refresh and detail-pane loads are no-ops — useful for the empty
// skeleton tests in app_test.go.
func WithLoader(l AppLoader) Option {
	return func(m *Model) { m.loader = l }
}

// WithCachePath wires the disk cache for instant startup. When set together
// with WithAccountID, Init dispatches a cache-load command before kicking
// off the network refresh.
func WithCachePath(p string) Option {
	return func(m *Model) { m.cachePath = p }
}

// WithStatePath wires the persistent state file (last-selected project,
// future cursors). Empty path disables persistence.
func WithStatePath(p string) Option {
	return func(m *Model) { m.statePath = p }
}

// WithAccountID binds the cache to a specific Jira account so a different
// login invalidates whatever lived on disk.
func WithAccountID(a string) Option {
	return func(m *Model) { m.accountID = a }
}

// WithDefaultProject pre-selects this project key in the create wizard.
func WithDefaultProject(s string) Option {
	return func(m *Model) { m.defaultProject = s }
}

// WithEpicTypes wires the configured epic-issue-type names so the "parent"
// grouping strategy can distinguish epic rows from their children.
func WithEpicTypes(types []string) Option {
	return func(m *Model) { m.epicTypes = append([]string(nil), types...) }
}

// WithCustomFields wires the user-friendly custom-field name → Jira API id
// mapping. The structure adapter uses it so YAML can reference custom
// fields by short names instead of `customfield_<id>`.
func WithCustomFields(m map[string]string) Option {
	return func(mod *Model) {
		if len(m) == 0 {
			return
		}
		mod.customFields = make(map[string]string, len(m))
		for k, v := range m {
			mod.customFields[k] = v
		}
	}
}

// WithInitialIssues seeds the list with a synchronous snapshot — used by
// the CLI's pre-render of the cache so the list shows up before Bubble Tea
// even starts dispatching commands.
func WithInitialIssues(issues []jira.Issue) Option {
	return func(m *Model) { m.initialIssues = issues }
}

// WithAssignDebounce overrides the assign overlay's typing-pause window.
// Tests pass 0 so the debounced search dispatches synchronously instead of
// after a 250ms wait.
func WithAssignDebounce(d time.Duration) Option {
	return func(m *Model) { m.assignDebounce = d; m.assignDebounceSet = true }
}

// WithAutoRefresh enables the silent background list refresh after every d.
// When d <= 0 (the default) no auto-refresh is scheduled. The ticker only
// re-fetches the list — the open detail pane is never disturbed.
func WithAutoRefresh(d time.Duration) Option {
	return func(m *Model) { m.autoRefresh = d }
}

// WithAutoRefreshTicker overrides the scheduling primitive used by the auto
// refresh loop. Tests pass a no-op (or counting) ticker so they can drive
// ticks deterministically by sending the tick message themselves.
func WithAutoRefreshTicker(t TickerFunc) Option {
	return func(m *Model) { m.ticker = t }
}

// WithBrowserOpener overrides the BrowserOpener used by the `o` keybinding.
// Production wiring uses OSOpener; tests substitute a fake that records the
// requested URL instead of spawning a real browser.
func WithBrowserOpener(b BrowserOpener) Option {
	return func(m *Model) { m.browser = b }
}

// WithStructures wires a Store and a hot-reload watcher into the model. Call
// from cmd/ripjira; tests skip this option so they don't spawn an fsnotify
// goroutine. ctx scopes the watcher; cancel it on app shutdown.
func WithStructures(ctx context.Context, store *structure.Store) Option {
	return func(m *Model) {
		if store == nil {
			return
		}
		m.structures = store
		if events, err := structure.Watch(ctx, store.Dir()); err == nil {
			m.structureEvents = events
		}
	}
}
