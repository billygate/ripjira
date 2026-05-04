package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui/gfx"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/structureadapter"
	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// Focus identifies which pane has keyboard focus.
type Focus int

// Focus values for the two-pane body layout. FocusList is the issue list,
// FocusDetail is the right detail pane. Order matters: the integer values
// are not load-bearing in any caller, but they define the natural left→right
// tab cycle (see Model.cycleFocus). The top-tab strip is not focusable; view
// changes go through `]` / `[` keybinds.
const (
	FocusList Focus = iota
	FocusDetail
)

// previewState tracks the optional third pane that displays an inline
// image preview for an attachment of the currently selected issue. When
// Active, the layout cascades one slot to the left: the list pane is
// hidden, detail moves to the left half, and the preview occupies the
// right half. The right-arrow key opens it; the left-arrow key closes it.
type previewState struct {
	Active     bool
	Attachment jira.Attachment
}

// Model is the root Bubble Tea model for the ripjira TUI. Each Stage 2 task
// has filled in another piece (panes, cache, toasts, help); Stage 2's
// integration task wires them all together so a fresh launch reads the
// cache, paints the list, then refreshes from the network in the background.
type Model struct {
	keymap  Keymap
	palette themes.Palette
	styles  styles.Styles

	focus      Focus
	width      int
	height     int
	statusText string

	toasts     Toasts
	spinner    Spinner
	help       overlays.Help
	transition overlays.Transition
	comment    overlays.Comment
	assign     overlays.Assign
	create     overlays.Create
	options    overlays.Options
	edit       overlays.Edit
	favorites  overlays.Favorites
	link        overlays.Link
	linkRemove  overlays.RemoveLink
	worklog       overlays.Worklog
	worklogRemove overlays.RemoveWorklog
	description   overlays.Description
	priority      overlays.Priority
	epicPicker    overlays.Epic
	structPicker  overlays.Structures
	topGo         overlays.TopGo

	list   panes.List
	detail panes.Detail

	loader         AppLoader
	browser        BrowserOpener
	cachePath      string
	statePath      string
	accountID      string
	displayName    string
	defaultProject string
	epicTypes      []string
	customFields   map[string]string
	initialIssues  []jira.Issue

	assignDebounce    time.Duration
	assignDebounceSet bool

	autoRefresh time.Duration
	ticker      TickerFunc

	selectedKey string

	preview previewState

	view        panes.ViewKind
	searchQuery string

	// recentKeys is the bounded most-recently-viewed list (head = newest).
	// Loaded from state.json at startup, persisted on every push.
	recentKeys []string

	// pendingDeletedLink stores the just-removed link so handleLinkDeleteDone
	// can re-append it on failure. Cleared on each result.
	pendingDeletedLink    jira.IssueLink
	pendingDeletedWorklog jira.Worklog

	listToken  int
	listCancel context.CancelFunc

	prefetchCancel context.CancelFunc

	pendingQuitUntil time.Time

	structures      *structure.Store
	structureEvents <-chan structure.Event
	currentStructID map[string]string
	loadedStructs   map[string][]structure.Structure

	// lastSubView remembers the last sub-view chosen under each top tab so
	// `]`/`[` returns to the user's previous scope rather than always landing
	// on the first sub.
	lastSubView map[panes.TopTabKind]panes.ViewKind
}

// QuitArmed reports whether the user has pressed Esc once on the main view
// and the confirmation window has not yet elapsed.
func (m Model) QuitArmed() bool {
	return !m.pendingQuitUntil.IsZero() && time.Now().Before(m.pendingQuitUntil)
}

// canArmQuit reports whether an Esc keypress on the main view should arm
// the quit confirmation. False when any overlay is visible OR when the
// list pane is in search-editing mode (Esc must remain a cancel there).
func (m Model) canArmQuit() bool {
	if m.help.Visible() || m.transition.Visible() || m.comment.Visible() ||
		m.assign.Visible() || m.create.Visible() || m.options.Visible() ||
		m.edit.Visible() || m.favorites.Visible() || m.link.Visible() ||
		m.linkRemove.Visible() || m.worklog.Visible() || m.worklogRemove.Visible() ||
		m.description.Visible() || m.priority.Visible() ||
		m.epicPicker.Visible() || m.structPicker.Visible() || m.topGo.Visible() {
		return false
	}
	if m.list.SearchEditing() || m.list.LocalFilterEditing() {
		return false
	}
	return true
}

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

// New constructs a root Model bound to the given palette. Styles are derived
// from the palette so theme switching is a constructor-only concern.
func New(p themes.Palette, opts ...Option) Model {
	km := DefaultKeymap()
	st := styles.New(p)
	m := Model{
		keymap:     km,
		palette:    p,
		styles:     st,
		focus:      FocusList,
		toasts:     NewToasts(),
		spinner:    NewSpinner(),
		help:       overlays.NewHelp(buildHelpColumns(km), km.CloseOverlay),
		transition: overlays.NewTransition(km.CloseOverlay),
		comment:    overlays.NewComment(km.CloseOverlay),
		assign:     overlays.NewAssign(km.CloseOverlay, overlays.DefaultAssignDebounce),
		create:     overlays.NewCreate(km.CloseOverlay, ""),
		options:    overlays.NewOptions(km.CloseOverlay, "status", "priority", false),
		edit:       overlays.NewEdit(km.CloseOverlay),
		favorites:  overlays.NewFavorites(km.CloseOverlay),
		link:       overlays.NewLink(km.CloseOverlay),
		linkRemove:  overlays.NewRemoveLink(km.CloseOverlay),
		worklog:       overlays.NewWorklog(km.CloseOverlay),
		worklogRemove: overlays.NewRemoveWorklog(km.CloseOverlay),
		description:   overlays.NewDescription(km.CloseOverlay),
		priority:    overlays.NewPriority(km.CloseOverlay),
		epicPicker:   overlays.NewEpic(),
		structPicker: overlays.NewStructures(km.CloseOverlay),
		topGo:        overlays.NewTopGo(km.CloseOverlay),
		list:       panes.New(st, grouping.ByEpicAndPriority{}, 1, 1),
		detail:     panes.NewDetail(st, panesNoopLoader{}, 1, 1),
		browser:    OSOpener{},
	}
	for _, o := range opts {
		o(&m)
	}
	if m.assignDebounceSet {
		m.assign = overlays.NewAssign(km.CloseOverlay, m.assignDebounce)
	}
	if m.loader != nil {
		m.detail = panes.NewDetail(st, m.loader, 1, 1)
	}
	// initialIssues are placed via feedList in Init/handleListFetched once the
	// view is settled; here we just store them on m.list as the flat fallback.
	if len(m.initialIssues) > 0 {
		m.list.SetIssues(m.initialIssues)
	}
	m.currentStructID = map[string]string{}
	m.loadedStructs = map[string][]structure.Structure{}
	m.lastSubView = map[panes.TopTabKind]panes.ViewKind{}
	if m.statePath != "" {
		if st, err := state.Load(m.statePath); err == nil {
			if st.Grouping != "" {
				m.list.SetStrategy(grouping.ByName(st.Grouping, m.epicTypes))
			}
			sortName := st.Sort
			if sortName == "" {
				sortName = "priority"
			}
			desc := defaultDescFor(sortName)
			if st.SortDesc != nil {
				desc = *st.SortDesc
			}
			m.list.SetSort(grouping.SortByName(sortName), desc)
			m.recentKeys = append([]string(nil), st.RecentlyViewed...)
			for k, v := range st.LastStructure {
				m.currentStructID[k] = v
			}
			for k, v := range st.LastSubView {
				m.lastSubView[panes.TopTabKind(k)] = panes.ViewKind(v)
			}
			if st.LastView != nil {
				v := panes.ViewKind(*st.LastView)
				// Search is a transient mode, never restore as boot view.
				if v != panes.ViewSearch {
					m.view = v
				}
			}
		}
	}
	return m
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

// maxRecentKeys caps the recently-viewed list. Twenty is a comfortable
// number for daily use without making the JQL `key in (…)` query
// uncomfortably long.
const maxRecentKeys = 20

// pushRecent moves key to the front of m.recentKeys, dedups, and caps the
// list at maxRecentKeys. Persists asynchronously to state.json. No-op for
// empty keys.
func (m *Model) pushRecent(key string) {
	if key == "" {
		return
	}
	out := make([]string, 0, len(m.recentKeys)+1)
	out = append(out, key)
	for _, k := range m.recentKeys {
		if k == key {
			continue
		}
		out = append(out, k)
		if len(out) >= maxRecentKeys {
			break
		}
	}
	m.recentKeys = out
	if m.statePath == "" {
		return
	}
	path := m.statePath
	keys := append([]string(nil), out...)
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			s.RecentlyViewed = keys
		})
	}()
}

// recentJQL builds the JQL fed to the loader for ViewRecent. Returns ""
// when the recently-viewed list is empty so the loader can short-circuit
// without calling Search.
func (m Model) recentJQL() string {
	if len(m.recentKeys) == 0 {
		return ""
	}
	parts := make([]string, len(m.recentKeys))
	for i, k := range m.recentKeys {
		parts[i] = `"` + k + `"`
	}
	return "key in (" + strings.Join(parts, ", ") + ")"
}

// sprintJQL returns the JQL backing the SPRINT tab — the user's open
// issues in any active sprint they have access to. Empty when the user
// is not assigned to any project that uses sprints; the loader will then
// short-circuit instead of pinging Jira.
func (m Model) sprintJQL() string {
	return "assignee = currentUser() AND sprint in openSprints() AND resolution = Unresolved ORDER BY updated DESC"
}

// mentionsJQL returns the JQL backing the MENTIONS tab — issues whose
// comments mention the user. Jira does not expose a "mentioned me"
// predicate, so we narrow to the `comment` field (rather than the
// catch-all `text` field) and search for the display name. This matches
// rendered "@displayName" tokens that Jira's mention macro produces, but
// will also catch plain-text occurrences of the name. A perfect filter
// would require fetching every candidate's comment ADF and verifying a
// `mention` node with the user's accountId — left as future work.
//
// Empty when the display name is not yet known so the loader short-
// circuits.
func (m Model) mentionsJQL() string {
	if strings.TrimSpace(m.displayName) == "" {
		return ""
	}
	escaped := strings.ReplaceAll(m.displayName, `"`, `\"`)
	return `comment ~ "` + escaped + `" ORDER BY updated DESC`
}

// structuresJQL returns the JQL feeding the STRUCTURES tab. The default
// scope is "live work" — every unresolved issue plus anything touched in
// the last 30 days, freshest first. Structures' filters then partition the
// stream into named sections. Empty when no defaultProject is configured;
// loader short-circuits to no fetch.
func (m Model) structuresJQL() string {
	if m.defaultProject == "" {
		return ""
	}
	return `project = "` + m.defaultProject +
		`" AND (resolution = Unresolved OR updated >= -30d) ORDER BY updated DESC`
}

// reorderByRecent sorts issues into the order they appear in m.recentKeys.
// Issues whose keys are not in the recent list (defensive — shouldn't
// happen since we filtered by key in the JQL) are dropped.
func (m Model) reorderByRecent(issues []jira.Issue) []jira.Issue {
	if len(issues) == 0 || len(m.recentKeys) == 0 {
		return issues
	}
	byKey := make(map[string]jira.Issue, len(issues))
	for _, is := range issues {
		byKey[is.Key] = is
	}
	out := make([]jira.Issue, 0, len(issues))
	for _, k := range m.recentKeys {
		if is, ok := byKey[k]; ok {
			out = append(out, is)
		}
	}
	return out
}

// buildHelpColumns pairs FullHelp's binding columns with their titles so the
// help overlay can render labelled sections without duplicating the binding
// lists.
func buildHelpColumns(km Keymap) []overlays.HelpColumn {
	cols := km.FullHelp()
	titles := km.FullHelpTitles()
	out := make([]overlays.HelpColumn, len(cols))
	for i, c := range cols {
		title := ""
		if i < len(titles) {
			title = titles[i]
		}
		out[i] = overlays.HelpColumn{Title: title, Bindings: c}
	}
	return out
}

// Keymap returns the model's keymap. Used by the help overlay (later task).
func (m Model) Keymap() Keymap { return m.keymap }

// Focused returns which pane currently has keyboard focus.
func (m Model) Focused() Focus { return m.focus }

// SetStatus replaces the top-bar status text.
func (m *Model) SetStatus(s string) { m.statusText = s }

// Toasts returns the current toast queue (mostly for tests).
func (m Model) Toasts() Toasts { return m.toasts }

// Spinner returns the current spinner state (mostly for tests).
func (m Model) Spinner() Spinner { return m.spinner }

// HelpVisible reports whether the help overlay is currently shown.
func (m Model) HelpVisible() bool { return m.help.Visible() }

// TransitionVisible reports whether the transition overlay is currently shown.
func (m Model) TransitionVisible() bool { return m.transition.Visible() }

// CommentVisible reports whether the comment overlay is currently shown.
func (m Model) CommentVisible() bool { return m.comment.Visible() }

// CommentConfirming reports whether the comment overlay is in its
// "discard draft?" confirmation state.
func (m Model) CommentConfirming() bool { return m.comment.Confirming() }

// AssignVisible reports whether the assign overlay is currently shown.
func (m Model) AssignVisible() bool { return m.assign.Visible() }

// Assign returns the embedded assign overlay (mostly for tests).
func (m Model) Assign() overlays.Assign { return m.assign }

// CreateVisible reports whether the create overlay is currently shown.
func (m Model) CreateVisible() bool { return m.create.Visible() }

// OptionsVisible reports whether the options overlay is currently shown.
func (m Model) OptionsVisible() bool { return m.options.Visible() }

// Create returns the embedded create overlay (mostly for tests).
func (m Model) Create() overlays.Create { return m.create }

// List returns the embedded list pane (mostly for tests).
func (m Model) List() panes.List { return m.list }

// Detail returns the embedded detail pane (mostly for tests).
func (m Model) Detail() panes.Detail { return m.detail }

// WithToastClock returns a copy of m whose toast queue uses the given clock.
// Used by tests to simulate the passage of time without sleeping.
func (m Model) WithToastClock(now func() time.Time) Model {
	m.toasts = m.toasts.WithClock(now)
	return m
}

// listFetchedMsg carries the result of the background MyIssues call kicked
// off in Init. The app forwards the issues into the list pane and saves
// them to disk for the next start.
type listFetchedMsg struct {
	Token  int
	Issues []jira.Issue
	Err    error
}

// transitionDoneMsg carries the result of a DoTransition network call. On
// error the app reverts the optimistic status to PrevStatus and pushes an
// error toast.
type transitionDoneMsg struct {
	IssueKey   string
	PrevStatus jira.Status
	NewStatus  jira.Status
	Err        error
}

// commentDoneMsg carries the result of an AddComment network call. On
// success the body is appended to the detail pane and a toast is shown;
// on error a toast surfaces the failure.
type commentDoneMsg struct {
	IssueKey string
	Body     string
	Err      error
}

// assignDoneMsg carries the result of an AssignIssue network call. On
// error the optimistic assignee is rolled back to PrevAssignee and a toast
// surfaces the failure; on success a confirmation toast is shown.
type assignDoneMsg struct {
	IssueKey     string
	NewAssignee  jira.User
	PrevAssignee *jira.User
	Err          error
}

// autoRefreshTickMsg fires whenever the configured auto-refresh interval
// elapses. The handler dispatches a silent list refresh and re-arms the
// next tick. Carries no payload — the tick instant itself is irrelevant.
type autoRefreshTickMsg struct{}

// refreshListMsg is dispatched from Init (and from `r` keybinding) so the
// Update path — which can mutate model state — owns the bump of the list
// token and the cancel of any in-flight previous fetch.
type refreshListMsg struct{}

// cacheLoadedMsg carries the result of the synchronous cache load. The app
// emits this immediately so the list paints before the network responds.
type cacheLoadedMsg struct {
	Issues []jira.Issue
	Err    error
}

// accountIDFetchedMsg carries the result of the background Myself call
// kicked off in Init. The app saves the ID for cache scoping and then
// dispatches a cache-load command.
type accountIDFetchedMsg struct {
	AccountID   string
	DisplayName string
	Err         error
}

// Init implements tea.Model. When a loader is wired up, Init returns a
// batched command: fetch the account ID, and then (after ID arrives) read
// the on-disk cache and kick off a fresh MyIssues call.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if m.loader != nil {
		loader := m.loader
		cmds = append(cmds,
			func() tea.Msg { return BackgroundActivityMsg{Delta: 1} },
			func() tea.Msg {
				me, err := loader.GetMyself(context.Background())
				return accountIDFetchedMsg{AccountID: me.AccountID, DisplayName: me.DisplayName, Err: err}
			},
		)
		cmds = append(cmds, func() tea.Msg { return refreshListMsg{} })
	}
	if tick := m.scheduleAutoRefresh(); tick != nil {
		cmds = append(cmds, tick)
	}
	if cmd := m.watchStructuresNextCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// feedList routes issues into the list pane: in sectioned mode (active
// structure on the STRUCTURES tab) it builds sections via structure.Apply;
// otherwise it falls through to SetIssues. Always called before rendering
// so the two modes never coexist.
func (m *Model) feedList(issues []jira.Issue) {
	if m.view != panes.ViewStructures {
		m.list.SetSections(nil)
		m.list.SetIssues(issues)
		return
	}
	st, ok := m.activeStructure()
	if !ok {
		m.list.SetSections(nil)
		m.list.SetIssues(issues)
		return
	}
	adapters := make([]structure.Issue, len(issues))
	for i := range issues {
		adapters[i] = structureadapter.NewWithCustom(issues[i], m.customFields)
	}
	applied := structure.Apply(adapters, &st)
	secs := make([]panes.Section, 0, len(applied))
	for _, a := range applied {
		real := make([]jira.Issue, len(a.Issues))
		for i, x := range a.Issues {
			real[i] = x.(structureadapter.Adapter).Issue()
		}
		section := panes.Section{Title: a.Title, ReadOnly: st.IsReadOnly(), Issues: real}
		if len(a.GroupBy) > 0 {
			tree := structure.GroupTree(a.Issues, a.GroupBy, "", 0)
			section.Tree = treeToSectionNodes(tree)
		}
		secs = append(secs, section)
	}
	// Keep raw issues populated so cycling structures can re-apply without
	// re-fetching; sections take precedence in the list-pane rebuild.
	m.list.SetIssues(issues)
	m.list.SetSections(secs)
}

// treeToSectionNodes converts a structure.TreeNode forest (carrying the
// adapter-wrapped Issue interface) into the panes.SectionNode shape used by
// the list-pane renderer (which works with concrete jira.Issue values).
func treeToSectionNodes(nodes []structure.TreeNode) []panes.SectionNode {
	out := make([]panes.SectionNode, 0, len(nodes))
	for _, n := range nodes {
		pn := panes.SectionNode{
			Title: n.Title,
			Path:  n.Path,
			Depth: n.Depth,
		}
		if len(n.Children) > 0 {
			pn.Children = treeToSectionNodes(n.Children)
		} else {
			pn.Issues = make([]jira.Issue, 0, len(n.Issues))
			for _, x := range n.Issues {
				if a, ok := x.(structureadapter.Adapter); ok {
					pn.Issues = append(pn.Issues, a.Issue())
				}
			}
		}
		out = append(out, pn)
	}
	return out
}

// activeStructure resolves the currently-selected structure for the default
// project. Falls back to the Default built-in when no selection exists.
func (m *Model) activeStructure() (structure.Structure, bool) {
	pk := m.defaultProject
	if pk == "" {
		return structure.Structure{}, false
	}
	id := m.currentStructID[pk]
	if id == "" {
		id = structure.BuiltinDefaultID
	}
	all, err := m.loadStructuresFor(pk)
	if err != nil {
		return structure.Structure{}, false
	}
	for i := range all {
		if all[i].ID == id {
			return all[i], true
		}
	}
	if len(all) > 0 {
		return all[0], true
	}
	return structure.Structure{}, false
}

// editStructuresYAML suspends the TUI and runs the user's $EDITOR (fallback
// vim) on the structures YAML for the active project. Creates the file with
// a starter template if it doesn't exist. The fsnotify watcher picks up the
// change after exit; toast surfaces an error if the editor failed.
func (m Model) editStructuresYAML() (tea.Model, tea.Cmd) {
	if m.structures == nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: store unavailable", Level: ToastError}
		}
	}
	pk := m.defaultProject
	if pk == "" {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: set default project to edit YAML", Level: ToastInfo}
		}
	}
	path := m.structures.Path(pk)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: " + err.Error(), Level: ToastError}
		}
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		template := []byte(starterStructuresYAML(pk))
		if werr := os.WriteFile(path, template, 0o600); werr != nil {
			return m, func() tea.Msg {
				return ToastMsg{Text: "structures: " + werr.Error(), Level: ToastError}
			}
		}
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return ToastMsg{Text: "editor: " + err.Error(), Level: ToastError}
		}
		return ToastMsg{Text: "structures reloaded", Level: ToastInfo}
	})
}

// starterStructuresYAML returns a commented-out example users can edit when
// no file exists yet for project pk.
func starterStructuresYAML(pk string) string {
	return "# ripjira structures for " + pk + "\n" +
		"# Each entry is a structure shown in the STRUCTURES picker.\n" +
		"# Field whitelist: status, status_category, priority, issuetype,\n" +
		"# assignee, reporter, parent_key, labels, project.\n" +
		"#\n" +
		"# - id: my-team\n" +
		"#   name: My team\n" +
		"#   sections:\n" +
		"#     - title: In progress\n" +
		"#       filter:\n" +
		"#         status: [Open, \"In Progress\"]\n" +
		"#         assignee: { exists: true }\n" +
		"#       group_by: [priority]\n" +
		"#     - title: Blocked\n" +
		"#       filter:\n" +
		"#         labels: [blocker]\n"
}

// openTopGo pops the "Go to" overlay listing every top-level tab. The
// active top is pre-selected; Enter switches to its persisted last sub.
func (m Model) openTopGo() (tea.Model, tea.Cmd) {
	tops := panes.AllTopTabs()
	entries := make([]overlays.TopGoEntry, 0, len(tops))
	for _, t := range tops {
		entries = append(entries, overlays.TopGoEntry{Label: t.String(), ID: int(t)})
	}
	m.topGo = m.topGo.Show(entries, int(panes.TopGroup(m.view)))
	return m, nil
}

// openStructurePicker pops the picker overlay populated with the active
// project's built-ins + user structures. No-op when defaultProject is unset.
func (m Model) openStructurePicker() (tea.Model, tea.Cmd) {
	pk := m.defaultProject
	if pk == "" {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: set default project to use this picker", Level: ToastInfo}
		}
	}
	all, err := m.loadStructuresFor(pk)
	if err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: " + err.Error(), Level: ToastError}
		}
	}
	entries := make([]overlays.StructureEntry, 0, len(all))
	for _, s := range all {
		entries = append(entries, overlays.StructureEntry{
			ID: s.ID, Name: s.Name,
			ReadOnly: s.IsReadOnly(),
			Builtin:  structure.IsBuiltinID(s.ID),
		})
	}
	selID := m.currentStructID[pk]
	if selID == "" {
		selID = structure.BuiltinDefaultID
	}
	m.structPicker = m.structPicker.Show(entries, selID)
	return m, nil
}


// persistLastView writes the currently active ViewKind to state.json so
// the next session boots into the user's last view instead of MyTasks.
func (m *Model) persistLastView(v panes.ViewKind) {
	if m.statePath == "" {
		return
	}
	path := m.statePath
	id := int(v)
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			s.LastView = &id
		})
	}()
}

// persistLastSubView writes the active sub-view under top to state.json so
// the next session restores the user's scope. Async; no-op without a state
// path.
func (m *Model) persistLastSubView(top panes.TopTabKind, v panes.ViewKind) {
	if m.statePath == "" {
		return
	}
	path := m.statePath
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			if s.LastSubView == nil {
				s.LastSubView = map[int]int{}
			}
			s.LastSubView[int(top)] = int(v)
		})
	}()
}

// persistLastStructure writes the structure id for project to state.json
// asynchronously. No-op when state path is unset.
func (m *Model) persistLastStructure(project, id string) {
	if m.statePath == "" || project == "" {
		return
	}
	path := m.statePath
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			if s.LastStructure == nil {
				s.LastStructure = map[string]string{}
			}
			s.LastStructure[project] = id
		})
	}()
}

// loadStructuresFor returns built-ins + user structures for project, caching
// the result. The watcher invalidates the cache on file changes.
func (m *Model) loadStructuresFor(project string) ([]structure.Structure, error) {
	if v, ok := m.loadedStructs[project]; ok {
		return v, nil
	}
	if m.structures == nil {
		v := structure.Builtins(project)
		m.loadedStructs[project] = v
		return v, nil
	}
	v, err := m.structures.Load(project)
	if err != nil {
		return nil, err
	}
	m.loadedStructs[project] = v
	return v, nil
}

// watchStructuresNextCmd blocks for the next watcher event and translates it
// into structureChangedMsg. Re-armed by the Update handler after each event.
func (m Model) watchStructuresNextCmd() tea.Cmd {
	if m.structureEvents == nil {
		return nil
	}
	events := m.structureEvents
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return structureChangedMsg{Project: ev.ProjectKey}
	}
}

// dispatchListRefresh cancels any in-flight list fetch, bumps the generation
// token, and fires off a fresh MyIssues call. Returns the updated model and
// the command batch. Used by initial load, auto-refresh ticks, and the
// manual `r` keybinding. The Cmd is nil when no loader is wired.
func (m Model) dispatchListRefresh() (Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	if m.listCancel != nil {
		m.listCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.listCancel = cancel
	m.listToken++
	token := m.listToken
	loader := m.loader
	view := m.view
	query := m.searchQuery
	switch view {
	case panes.ViewRecent:
		query = m.recentJQL()
	case panes.ViewSprint:
		query = m.sprintJQL()
	case panes.ViewMentions:
		query = m.mentionsJQL()
	case panes.ViewStructures:
		query = m.structuresJQL()
	}
	cmd := tea.Batch(
		func() tea.Msg { return BackgroundActivityMsg{Delta: 1} },
		func() tea.Msg {
			is, err := loader.LoadIssues(ctx, view, query)
			return listFetchedMsg{Token: token, Issues: is, Err: err}
		},
	)
	return m, cmd
}

// scheduleAutoRefresh arms a one-shot tick for the configured interval. The
// tick handler re-arms after each fire, so a single Init call kicks off the
// whole loop. Returns nil when auto-refresh is disabled or no loader exists,
// so callers can safely append the result to a batch.
func (m Model) scheduleAutoRefresh() tea.Cmd {
	if m.autoRefresh <= 0 || m.loader == nil {
		return nil
	}
	tick := m.ticker
	if tick == nil {
		tick = tea.Tick
	}
	return tick(m.autoRefresh, func(time.Time) tea.Msg { return autoRefreshTickMsg{} })
}

// loadCacheCmd returns a command that reads the on-disk cache for the
// currently configured accountID. If no cache path or ID is set, or if
// initial issues were already provided, it returns nil.
func (m Model) loadCacheCmd() tea.Cmd {
	if m.cachePath == "" || m.accountID == "" || len(m.initialIssues) > 0 {
		return nil
	}
	path, account := m.cachePath, m.accountID
	return func() tea.Msg {
		issues, err := LoadCache(path, account)
		return cacheLoadedMsg{Issues: issues, Err: err}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case ToastMsg:
		var cmd tea.Cmd
		m.toasts, cmd = m.toasts.Push(msg.Text, msg.Level)
		return m, cmd
	case toastExpireMsg:
		m.toasts = m.toasts.Tick()
		return m, nil
	case prefetchTickMsg:
		if pf, ok := m.loader.(prefetchProgressReporter); ok {
			if _, _, active := pf.PrefetchProgress(); active {
				return m, prefetchTick()
			}
		}
		return m, nil
	case BackgroundActivityMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Adjust(msg.Delta)
		return m, cmd
	case structureChangedMsg:
		delete(m.loadedStructs, msg.Project)
		// Re-apply if we're currently viewing structures for that project.
		if m.view == panes.ViewStructures && m.defaultProject == msg.Project {
			m.feedList(m.list.Issues())
		}
		return m, m.watchStructuresNextCmd()
	case overlays.TopGoSelectedMsg:
		target := panes.TopTabKind(msg.ID)
		v, ok := m.lastSubView[target]
		if !ok {
			subs := panes.SubViews(target)
			if len(subs) > 0 {
				v = subs[0]
			}
		}
		return m.handleViewSelected(v)
	case overlays.StructureSelectedMsg:
		pk := m.defaultProject
		if pk != "" {
			m.currentStructID[pk] = msg.ID
			m.persistLastStructure(pk, msg.ID)
		}
		m.feedList(m.list.Issues())
		return m, nil
	case accountIDFetchedMsg:
		stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
		if msg.Err != nil {
			return m, stopSpinner
		}
		m.accountID = msg.AccountID
		m.displayName = msg.DisplayName
		m.create = m.create.SetCurrentUserAccountID(msg.AccountID)
		return m, tea.Batch(stopSpinner, m.loadCacheCmd())
	case autoRefreshTickMsg:
		m, refreshCmd := m.dispatchListRefresh()
		return m, tea.Batch(refreshCmd, m.scheduleAutoRefresh())
	case refreshListMsg:
		m, refreshCmd := m.dispatchListRefresh()
		return m, refreshCmd
	case cacheLoadedMsg:
		if msg.Err != nil || len(msg.Issues) == 0 {
			return m, nil
		}
		m.feedList(msg.Issues)
		prefetchCmd := m.startPrefetch(msg.Issues)
		return m, tea.Batch(prefetchCmd, m.syncDetailFromList())
	case listFetchedMsg:
		return m.handleListFetched(msg)
	case overlays.TransitionSelectedMsg:
		return m.handleTransitionSelected(msg)
	case transitionDoneMsg:
		return m.handleTransitionDone(msg)
	case overlays.CommentSubmittedMsg:
		return m.handleCommentSubmitted(msg)
	case commentDoneMsg:
		return m.handleCommentDone(msg)
	case overlays.EditSubmittedMsg:
		return m.handleEditSubmitted(msg)
	case editDoneMsg:
		return m.handleEditDone(msg)
	case overlays.FavoriteAppliedMsg:
		return m.handleFavoriteApplied(msg)
	case overlays.FavoriteSavedMsg:
		return m.handleFavoriteSaved(msg)
	case overlays.FavoriteDeletedMsg:
		return m.handleFavoriteDeleted(msg)
	case overlays.LinkSubmittedMsg:
		return m.handleLinkSubmitted(msg)
	case linkDoneMsg:
		return m.handleLinkDone(msg)
	case overlays.LinkDeletedMsg:
		return m.handleLinkDeletePicked(msg)
	case linkDeletedDoneMsg:
		return m.handleLinkDeleteDone(msg)
	case overlays.DescriptionSubmittedMsg:
		return m.handleDescriptionSubmitted(msg)
	case descriptionDoneMsg:
		return m.handleDescriptionDone(msg)
	case overlays.PrioritySelectedMsg:
		return m.handlePrioritySelected(msg)
	case epicsLoadedMsg:
		return m.handleEpicsLoaded(msg)
	case overlays.EpicCancelledMsg:
		m.epicPicker = m.epicPicker.Hide()
		return m, nil
	case overlays.EpicPickedMsg:
		return m.handleEpicPicked(msg)
	case setParentDoneMsg:
		return m.handleSetParentDone(msg)
	case overlays.WorklogDeletedMsg:
		return m.handleWorklogDeletePicked(msg)
	case worklogDeletedDoneMsg:
		return m.handleWorklogDeleteDone(msg)
	case watchDoneMsg:
		return m.handleWatchDone(msg)
	case overlays.WorklogSubmittedMsg:
		return m.handleWorklogSubmitted(msg)
	case worklogDoneMsg:
		return m.handleWorklogDone(msg)
	case overlays.AssignSearchRequestMsg:
		return m.handleAssignSearchRequest(msg)
	case overlays.AssignSelectedMsg:
		return m.handleAssignSelected(msg)
	case overlays.AssignResultsMsg:
		var cmd tea.Cmd
		m.assign, cmd = m.assign.Update(msg)
		return m, cmd
	case createProjectsLoadedMsg:
		if msg.Err != nil {
			toast := func() tea.Msg {
				return ToastMsg{Text: "Projects load failed: " + msg.Err.Error(), Level: ToastError}
			}
			return m, toast
		}
		preselect := m.defaultProject
		if m.statePath != "" {
			if st, err := state.Load(m.statePath); err == nil && st.LastProject != "" {
				preselect = st.LastProject
			}
		}
		c, cmd := m.create.Show(msg.Projects, preselect)
		m.create = c
		return m, cmd
	case createSubtaskProjectsLoadedMsg:
		if msg.Err != nil {
			toast := func() tea.Msg {
				return ToastMsg{Text: "Projects load failed: " + msg.Err.Error(), Level: ToastError}
			}
			return m, toast
		}
		c, cmd := m.create.ShowAsSubtask(msg.Parent, msg.Projects)
		m.create = c
		return m, cmd
	case overlays.CreateProjectChosenMsg:
		loader := m.loader
		pk := msg.ProjectKey
		return m, func() tea.Msg {
			types, err := loader.IssueTypesForProject(context.Background(), pk)
			return overlays.CreateIssueTypesMsg{ProjectKey: pk, IssueTypes: types, Err: err}
		}
	case overlays.CreateIssueTypesMsg:
		var cmd tea.Cmd
		m.create, cmd = m.create.Update(msg)
		return m, cmd
	case overlays.CreateTypeChosenMsg:
		loader := m.loader
		pk, typeID := msg.ProjectKey, msg.IssueType.ID
		return m, func() tea.Msg {
			meta, err := loader.CreateMeta(context.Background(), pk, typeID)
			return overlays.CreateMetaLoadedMsg{ProjectKey: pk, IssueTypeID: typeID, Meta: meta, Err: err}
		}
	case overlays.CreateMetaLoadedMsg:
		var cmd tea.Cmd
		m.create, cmd = m.create.Update(msg)
		return m, cmd
	case overlays.UserSearchRequestMsg:
		if m.loader == nil {
			return m, nil
		}
		fieldID, query, token := msg.FieldID, msg.Query, msg.Token
		loader := m.loader
		return m, func() tea.Msg {
			users, err := loader.SearchUsers(context.Background(), query)
			return overlays.UserSearchResultsMsg{
				FieldID: fieldID,
				Token:   token,
				Users:   users,
				Err:     err,
			}
		}
	case overlays.UserSearchResultsMsg:
		if m.create.Visible() {
			var cmd tea.Cmd
			m.create, cmd = m.create.Update(msg)
			return m, cmd
		}
		return m, nil
	case overlays.CreateSubmitRequestedMsg:
		loader := m.loader
		payload := msg.Payload
		pk, typeID := msg.ProjectKey, msg.IssueTypeID
		return m, func() tea.Msg {
			issue, err := loader.CreateIssue(context.Background(), payload)
			return overlays.CreateSubmitDoneMsg{ProjectKey: pk, IssueTypeID: typeID, Issue: issue, Err: err}
		}
	case overlays.CreateSubmitDoneMsg:
		parent := m.create.ParentKey() // capture BEFORE Update potentially resets it
		var cmd tea.Cmd
		m.create, cmd = m.create.Update(msg)
		if msg.Err == nil {
			if parent != "" && msg.Issue.Key != "" {
				sub := jira.SubtaskRef{
					Key:     msg.Issue.Key,
					Summary: msg.Issue.Summary,
					Status:  msg.Issue.Status,
				}
				m.detail.AppendSubtask(parent, sub)
			}
			if m.statePath != "" && msg.ProjectKey != "" {
				path := m.statePath
				pk := msg.ProjectKey
				go func() {
					_ = state.Mutate(path, func(s *state.State) { s.LastProject = pk })
				}()
			}
			var refresh tea.Cmd
			m, refresh = m.dispatchListRefresh()
			return m, tea.Batch(cmd, refresh)
		}
		return m, cmd
	case overlays.CreateCancelledMsg:
		return m, nil
	case overlays.OptionsAppliedMsg:
		m.list.SetStrategy(grouping.ByName(msg.Grouping, m.epicTypes))
		m.list.SetSort(grouping.SortByName(msg.Sort), msg.Desc)
		if m.statePath != "" {
			path := m.statePath
			grp, srt, desc := msg.Grouping, msg.Sort, msg.Desc
			go func() {
				_ = state.Mutate(path, func(s *state.State) {
					s.Grouping = grp
					s.Sort = srt
					d := desc
					s.SortDesc = &d
				})
			}()
		}
		return m, m.syncDetailFromList()
	case overlays.OptionsCancelledMsg:
		return m, nil
	case assignSearchDoneMsg:
		stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
		forward := func() tea.Msg { return msg.Result }
		return m, tea.Batch(stopSpinner, forward)
	case assignDoneMsg:
		return m.handleAssignDone(msg)
	case browserOpenedMsg:
		if msg.Err == nil {
			return m, nil
		}
		toast := func() tea.Msg {
			return ToastMsg{Text: "Open failed: " + msg.Err.Error(), Level: ToastError}
		}
		return m, toast
	case panes.IssueLoadedMsg, panes.CommentsLoadedMsg, panes.TransitionsLoadedMsg, panes.AttachmentPreviewMsg:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		if loaded, ok := msg.(panes.IssueLoadedMsg); ok && loaded.Err == nil && loaded.Key != "" {
			m.pushRecent(loaded.Key)
		}
		return m, cmd
	case panes.SearchSubmittedMsg:
		m.searchQuery = msg.Query
		m.list.SetSearchCollapsed(msg.Query)
		m, refreshCmd := m.dispatchListRefresh()
		return m, refreshCmd
	case panes.SearchCancelledMsg:
		// User pressed Esc on an empty input with no prior query. Revert
		// to My Tasks (we do not track view history).
		prev := panes.ViewMyTasks
		m.view = prev
		m.searchQuery = ""
		m.list.SetSearchInactive()
		m.focus = FocusList
		m, refreshCmd := m.dispatchListRefresh()
		return m, refreshCmd
	default:
		var spCmd tea.Cmd
		m.spinner, spCmd = m.spinner.Update(msg)
		if spCmd != nil {
			return m, spCmd
		}
	}
	return m, nil
}

func (m Model) handleResize(msg tea.WindowSizeMsg) Model {
	m.width = msg.Width
	m.height = msg.Height
	listW, detailW, _, contentH := m.paneDims()
	// Inner content area = pane height (contentH) minus 2 for the rounded
	// border minus 1 for the pane title. Width subtracts 2 for the border.
	innerW := func(w int) int { return max(w-2, 1) }
	innerH := max(contentH-3, 1)
	m.list.SetSize(innerW(listW), innerH)
	m.detail.SetSize(innerW(detailW), innerH)
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help.Visible() {
		var cmd tea.Cmd
		m.help, cmd = m.help.Update(msg)
		return m, cmd
	}
	if m.transition.Visible() {
		var cmd tea.Cmd
		m.transition, cmd = m.transition.Update(msg)
		return m, cmd
	}
	if m.comment.Visible() {
		// Snapshot the in-progress body and key BEFORE the overlay sees the
		// keypress: if this very keystroke closes the overlay we want to
		// know what was being typed.
		prevKey, prevBody := m.comment.IssueKey(), m.comment.Value()
		submitting := msg.String() == "ctrl+s"
		var cmd tea.Cmd
		m.comment, cmd = m.comment.Update(msg)
		// On any close that wasn't a submit, persist what the user had so
		// far so it comes back on next open. Submits clear via the success
		// branch in handleCommentDone.
		if !m.comment.Visible() && !submitting && prevBody != "" {
			m.saveDraft(prevKey, prevBody)
		}
		return m, cmd
	}
	if m.assign.Visible() {
		var cmd tea.Cmd
		m.assign, cmd = m.assign.Update(msg)
		return m, cmd
	}
	if m.create.Visible() {
		var cmd tea.Cmd
		m.create, cmd = m.create.Update(msg)
		return m, cmd
	}
	if m.options.Visible() {
		var cmd tea.Cmd
		m.options, cmd = m.options.Update(msg)
		return m, cmd
	}
	if m.edit.Visible() {
		var cmd tea.Cmd
		m.edit, cmd = m.edit.Update(msg)
		return m, cmd
	}
	if m.favorites.Visible() {
		var cmd tea.Cmd
		m.favorites, cmd = m.favorites.Update(msg)
		return m, cmd
	}
	if m.link.Visible() {
		var cmd tea.Cmd
		m.link, cmd = m.link.Update(msg)
		return m, cmd
	}
	if m.linkRemove.Visible() {
		var cmd tea.Cmd
		m.linkRemove, cmd = m.linkRemove.Update(msg)
		return m, cmd
	}
	if m.worklog.Visible() {
		var cmd tea.Cmd
		m.worklog, cmd = m.worklog.Update(msg)
		return m, cmd
	}
	if m.worklogRemove.Visible() {
		var cmd tea.Cmd
		m.worklogRemove, cmd = m.worklogRemove.Update(msg)
		return m, cmd
	}
	if m.description.Visible() {
		var cmd tea.Cmd
		m.description, cmd = m.description.Update(msg)
		return m, cmd
	}
	if m.priority.Visible() {
		var cmd tea.Cmd
		m.priority, cmd = m.priority.Update(msg)
		return m, cmd
	}
	if m.epicPicker.Visible() {
		var cmd tea.Cmd
		m.epicPicker, cmd = m.epicPicker.Update(msg)
		return m, cmd
	}
	if m.structPicker.Visible() {
		var cmd tea.Cmd
		m.structPicker, cmd = m.structPicker.Update(msg)
		return m, cmd
	}
	if m.topGo.Visible() {
		var cmd tea.Cmd
		m.topGo, cmd = m.topGo.Update(msg)
		return m, cmd
	}
	// While the list pane's search input is being edited, the input must
	// own the keypress — otherwise typing "n", "s", etc. would trigger
	// global hotkeys (open create, open status…) instead of going into
	// the textinput. The list's own Update handles Enter/Esc.
	if m.list.SearchEditing() || m.list.LocalFilterEditing() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	// Esc on a collapsed local filter clears it before any quit-arm logic
	// runs — feels like the natural undo.
	if msg.Type == tea.KeyEsc && m.list.LocalFilter() != "" {
		m.list.ClearLocalFilter()
		return m, nil
	}
	if msg.Type == tea.KeyEsc && m.canArmQuit() {
		if m.QuitArmed() {
			return m, tea.Quit
		}
		m.pendingQuitUntil = time.Now().Add(3 * time.Second)
		var toastCmd tea.Cmd
		m.toasts, toastCmd = m.toasts.Push(
			"Press Esc again to quit  (any other key cancels)",
			ToastInfo,
		)
		return m, toastCmd
	}
	// Any non-Esc key clears a pending arm; fall through to the regular
	// handler so the keypress still does its normal job.
	if msg.Type != tea.KeyEsc && m.QuitArmed() {
		m.pendingQuitUntil = time.Time{}
	}
	// Translate Cyrillic/Greek runes to their Latin physical-position
	// equivalents so global hotkeys ([, ], q, n, …) work regardless of
	// the active keyboard layout. Suppressed when an input is focused
	// (canArmQuit encodes that predicate already).
	if m.canArmQuit() {
		msg = translateLayout(msg)
	}
	switch {
	case key.Matches(msg, m.keymap.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keymap.Help):
		m.help = m.help.Show()
		return m, nil
	case key.Matches(msg, m.keymap.Status):
		return m.openTransitionOverlay()
	case key.Matches(msg, m.keymap.Comment):
		return m.openCommentOverlay()
	case key.Matches(msg, m.keymap.Assign):
		return m.openAssignOverlay()
	case key.Matches(msg, m.keymap.New):
		return m.openCreateOverlay()
	case key.Matches(msg, m.keymap.NewSubtask):
		return m.openCreateSubtaskOverlay()
	case key.Matches(msg, m.keymap.Browser):
		return m.openInBrowser()
	case key.Matches(msg, m.keymap.CopyKey):
		return m.copySelectedIssue(false)
	case key.Matches(msg, m.keymap.CopyURL):
		return m.copySelectedIssue(true)
	case key.Matches(msg, m.keymap.Refresh):
		return m.handleManualRefresh()
	case key.Matches(msg, m.keymap.CycleFocusForward):
		return m.cycleFocus(1), nil
	case key.Matches(msg, m.keymap.CycleFocusBackward):
		return m.cycleFocus(-1), nil
	case key.Matches(msg, m.keymap.NextTab):
		return m.handleViewSelected(m.nextView())
	case key.Matches(msg, m.keymap.PrevTab):
		return m.handleViewSelected(m.prevView())
	case key.Matches(msg, m.keymap.FocusLeft):
		mm, cmd := m.stepFocus(-1)
		return mm, cmd
	case key.Matches(msg, m.keymap.FocusRight):
		mm, cmd := m.stepFocus(1)
		return mm, cmd
	case key.Matches(msg, m.keymap.OpenSearch):
		return m.openSearch()
	case key.Matches(msg, m.keymap.OpenFilter):
		m.list.BeginLocalFilter()
		m.focus = FocusList
		return m, nil
	case key.Matches(msg, m.keymap.OpenFavorites):
		return m.openFavoritesOverlay()
	case key.Matches(msg, m.keymap.EditSummary):
		return m.openEditOverlay(overlays.EditSummary)
	case key.Matches(msg, m.keymap.EditPriority):
		return m.openPriorityPicker()
	case key.Matches(msg, m.keymap.EditLabels):
		return m.openEditOverlay(overlays.EditLabels)
	case key.Matches(msg, m.keymap.EditDueDate):
		return m.openEditOverlay(overlays.EditDueDate)
	case key.Matches(msg, m.keymap.EditDescription):
		return m.openDescriptionOverlay()
	case key.Matches(msg, m.keymap.EditEpic):
		return m.openEpicPicker()
	case key.Matches(msg, m.keymap.OpenTopGo):
		return m.openTopGo()
	case key.Matches(msg, m.keymap.OpenStructures):
		return m.openStructurePicker()
	case key.Matches(msg, m.keymap.EditStructures):
		return m.editStructuresYAML()
	case key.Matches(msg, m.keymap.NextSubView):
		return m.handleViewSelected(m.nextSubView())
	case key.Matches(msg, m.keymap.PrevSubView):
		return m.handleViewSelected(m.prevSubView())
	case key.Matches(msg, m.keymap.AddLink):
		return m.openLinkOverlay()
	case key.Matches(msg, m.keymap.RemoveLink):
		return m.openRemoveLinkOverlay()
	case key.Matches(msg, m.keymap.Watch):
		return m.dispatchWatch(true)
	case key.Matches(msg, m.keymap.Unwatch):
		return m.dispatchWatch(false)
	case key.Matches(msg, m.keymap.LogWork):
		return m.openWorklogOverlay()
	case key.Matches(msg, m.keymap.RemoveWorklog):
		return m.openRemoveWorklogOverlay()
	case key.Matches(msg, m.keymap.OpenOptions):
		cur := m.list.Strategy().Name()
		sortName := "priority"
		desc := false
		if s, d := m.list.Sort(); s != nil {
			sortName = s.Name()
			desc = d
		}
		m.options = m.options.Show(cur, sortName, desc)
		return m, nil
	}

	switch m.focus {
	case FocusList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		selCmd := m.syncDetailFromList()
		return m, tea.Batch(cmd, selCmd)
	case FocusDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleManualRefresh implements the `r` keybinding: drop the detail cache,
// re-fetch the issue list, AND re-run the open issue's detail loads. The
// auto-refresh path differs here — auto refresh is silent and never touches
// the detail pane, while a user-initiated refresh wants both panes back in
// sync with the server. Cache invalidation is part of the same step so a
// user pressing `r` after they know the server changed never gets a stale
// view.
func (m Model) handleManualRefresh() (tea.Model, tea.Cmd) {
	if inv, ok := m.loader.(cacheInvalidator); ok {
		inv.InvalidateAll()
	}
	m, refreshCmd := m.dispatchListRefresh()
	cmds := []tea.Cmd{refreshCmd}
	if issue := m.detail.Issue(); issue != nil {
		cmds = append(cmds, m.detail.SetIssue(issue))
	}
	return m, tea.Batch(cmds...)
}

// openTransitionOverlay opens the `s` overlay for the currently selected
// issue. When no issue is selected (group header or empty list) it is a
// no-op. Transitions come from whatever the detail pane has already loaded;
// if the load is still in flight the overlay opens with an empty list and
// shows "(no transitions available)".
func (m Model) openTransitionOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	m.transition = m.transition.Show(issue.Key, m.detail.Transitions())
	return m, nil
}

// openCommentOverlay opens the `c` overlay scoped to the currently selected
// issue, prefilled from any saved draft for that key. No-op when no issue
// is selected.
func (m Model) openCommentOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.comment, cmd = m.comment.Show(issue.Key)
	if draft := m.loadDraft(issue.Key); draft != "" {
		m.comment = m.comment.SetValue(draft)
	}
	return m, cmd
}

// loadDraft returns the saved comment-in-progress for issueKey, or "".
func (m Model) loadDraft(issueKey string) string {
	if m.statePath == "" || issueKey == "" {
		return ""
	}
	st, err := state.Load(m.statePath)
	if err != nil {
		return ""
	}
	return st.CommentDrafts[issueKey]
}

// saveDraft persists a comment-in-progress to state.json under issueKey.
// Empty bodies clear the draft. Write happens in a goroutine.
func (m Model) saveDraft(issueKey, body string) {
	if m.statePath == "" || issueKey == "" {
		return
	}
	path := m.statePath
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			if strings.TrimSpace(body) == "" {
				delete(s.CommentDrafts, issueKey)
				return
			}
			if s.CommentDrafts == nil {
				s.CommentDrafts = map[string]string{}
			}
			s.CommentDrafts[issueKey] = body
		})
	}()
}

// clearDraft drops the stored draft for issueKey.
func (m Model) clearDraft(issueKey string) { m.saveDraft(issueKey, "") }

// browserOpenedMsg carries the result of a BrowserOpener.Open call. It is
// only used to surface failures via toast — successful opens are silent so a
// noisy "opened" toast doesn't fire every time the user presses `o`.
type browserOpenedMsg struct {
	URL string
	Err error
}

// openInBrowser launches the currently selected issue's URL via the wired
// BrowserOpener. The call runs in a tea.Cmd so a slow `xdg-open` cannot stall
// the UI. When no issue is selected (group header or empty list) it is a
// no-op. When the issue has no URL (the loader populated it without a base
// URL) the call is also skipped silently.
func (m Model) openInBrowser() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil || issue.URL == "" || m.browser == nil {
		return m, nil
	}
	url := issue.URL
	opener := m.browser
	return m, func() tea.Msg {
		return browserOpenedMsg{URL: url, Err: opener.Open(url)}
	}
}

// watchDoneMsg / worklogDoneMsg / openWorklogOverlay / dispatchWatch live
// here as a small Phase-4 cluster — they share the loader/spinner/toast
// scaffolding but otherwise have no interaction with each other.

type watchDoneMsg struct {
	IssueKey string
	Watching bool // true if we tried to watch, false if unwatch
	Err      error
}

type worklogDoneMsg struct {
	IssueKey  string
	TimeSpent string
	Err       error
}

// dispatchWatch fires a watch/unwatch network call for the current issue.
// Watch uses an empty accountID (Jira's "self" semantics); unwatch
// requires the explicit accountID from the model.
func (m Model) dispatchWatch(watching bool) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil || m.loader == nil {
		return m, nil
	}
	loader := m.loader
	key := issue.Key
	accountID := m.accountID
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		var err error
		if watching {
			err = loader.AddWatcher(context.Background(), key, "")
		} else {
			err = loader.RemoveWatcher(context.Background(), key, accountID)
		}
		return watchDoneMsg{IssueKey: key, Watching: watching, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleWatchDone toasts success/failure of an Add/RemoveWatcher call.
func (m Model) handleWatchDone(msg watchDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	verb := "Watching"
	if !msg.Watching {
		verb = "Unwatched"
	}
	if msg.Err != nil {
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  verb + " failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{Text: verb + " " + msg.IssueKey, Level: ToastInfo}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// openRemoveWorklogOverlay opens the worklog-remove picker over the
// current issue's worklog list.
func (m Model) openRemoveWorklogOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	entries := make([]overlays.WorklogEntry, 0, len(issue.Worklogs))
	for _, w := range issue.Worklogs {
		author := ""
		if w.Author != nil {
			author = w.Author.DisplayName
		}
		when := ""
		if !w.Started.IsZero() {
			when = w.Started.Format("2006-01-02")
		}
		entries = append(entries, overlays.WorklogEntry{
			ID:        w.ID,
			TimeSpent: w.TimeSpent,
			Author:    author,
			When:      when,
		})
	}
	m.worklogRemove = m.worklogRemove.Show(issue.Key, entries)
	return m, nil
}

// worklogDeletedDoneMsg carries the result of a DeleteWorklog call.
type worklogDeletedDoneMsg struct {
	IssueKey  string
	WorklogID string
	Err       error
}

// handleWorklogDeletePicked dispatches DeleteWorklog with optimistic
// local removal. Stores a snapshot in the model so handleWorklogDeleteDone
// can revert on failure.
func (m Model) handleWorklogDeletePicked(msg overlays.WorklogDeletedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil || msg.WorklogID == "" {
		return m, nil
	}
	var prev jira.Worklog
	if issue := m.detail.Issue(); issue != nil && issue.Key == msg.IssueKey {
		for _, w := range issue.Worklogs {
			if w.ID == msg.WorklogID {
				prev = w
				break
			}
		}
	}
	m.detail.RemoveWorklogByID(msg.IssueKey, msg.WorklogID)
	m.pendingDeletedWorklog = prev

	loader := m.loader
	key, id := msg.IssueKey, msg.WorklogID
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.DeleteWorklog(context.Background(), key, id)
		return worklogDeletedDoneMsg{IssueKey: key, WorklogID: id, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleWorklogDeleteDone toasts and reverts on failure.
func (m Model) handleWorklogDeleteDone(msg worklogDeletedDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		if m.pendingDeletedWorklog.ID != "" {
			m.detail.AppendWorklog(msg.IssueKey, m.pendingDeletedWorklog)
		}
		m.pendingDeletedWorklog = jira.Worklog{}
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Remove worklog failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	m.pendingDeletedWorklog = jira.Worklog{}
	toast := func() tea.Msg {
		return ToastMsg{Text: "Worklog removed", Level: ToastInfo}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// openWorklogOverlay opens the log-work overlay for the selected issue.
func (m Model) openWorklogOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.worklog, cmd = m.worklog.Show(issue.Key)
	return m, cmd
}

// handleWorklogSubmitted dispatches the AddWorklog network call. There is
// nothing optimistic to update locally — the issue's worklog list is not
// surfaced in the detail pane in this MVP.
func (m Model) handleWorklogSubmitted(msg overlays.WorklogSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	key, ts, comment := msg.IssueKey, msg.TimeSpent, msg.Comment
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.AddWorklog(context.Background(), key, ts, comment)
		return worklogDoneMsg{IssueKey: key, TimeSpent: ts, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleWorklogDone toasts success/failure of an AddWorklog call.
func (m Model) handleWorklogDone(msg worklogDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Worklog failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Logged " + msg.TimeSpent + " on " + msg.IssueKey,
			Level: ToastInfo,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// openPriorityPicker opens the priority picker for the current issue
// with the cursor on the issue's current priority.
func (m Model) openPriorityPicker() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	m.priority = m.priority.Show(issue.Key, issue.Priority.Name)
	return m, nil
}

// handlePrioritySelected synthesises an EditSubmittedMsg for EditPriority
// so the existing optimistic-update + rollback logic handles the picker
// path uniformly with the text-input path.
func (m Model) handlePrioritySelected(msg overlays.PrioritySelectedMsg) (tea.Model, tea.Cmd) {
	return m.handleEditSubmitted(overlays.EditSubmittedMsg{
		IssueKey: msg.IssueKey,
		Field:    overlays.EditPriority,
		Value:    msg.Name,
	})
}

// openEpicPicker opens the epic-link picker for the current issue and
// dispatches a SearchEpics call to populate it. The project is derived
// from the issue key prefix (e.g. "BILLING-123" → "BILLING").
func (m Model) openEpicPicker() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	if m.loader == nil {
		return m, nil
	}
	m.epicPicker = m.epicPicker.Show(issue.Key, issue.ParentKey)
	return m, m.searchEpicsCmd(issue.Key)
}

// projectKeyOf returns the project portion of a Jira issue key
// ("BILLING-123" → "BILLING"). Returns the input unchanged when no
// hyphen is present.
func projectKeyOf(issueKey string) string {
	if i := strings.IndexByte(issueKey, '-'); i > 0 {
		return issueKey[:i]
	}
	return issueKey
}

// searchEpicsCmd builds the tea.Cmd that calls SearchEpics for the
// project the issue belongs to.
func (m Model) searchEpicsCmd(issueKey string) tea.Cmd {
	loader := m.loader
	project := projectKeyOf(issueKey)
	types := append([]string(nil), m.epicTypes...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		epics, err := loader.SearchEpics(ctx, project, types)
		return epicsLoadedMsg{IssueKey: issueKey, Epics: epics, Err: err}
	}
}

// handleEpicsLoaded consumes the SearchEpics result. Stale results (the
// user closed or moved on) are dropped.
func (m Model) handleEpicsLoaded(msg epicsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.epicPicker = m.epicPicker.Hide()
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Could not load epics: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, toast
	}
	if !m.epicPicker.Visible() || m.epicPicker.IssueKey() != msg.IssueKey {
		return m, nil
	}
	m.epicPicker = m.epicPicker.SetEpics(msg.Epics)
	return m, nil
}

// handleEpicPicked applies the picked epic optimistically and dispatches
// SetParent. On error the previous parent is restored via setParentDoneMsg.
func (m Model) handleEpicPicked(msg overlays.EpicPickedMsg) (tea.Model, tea.Cmd) {
	loaded := m.epicPicker.LoadedEpics()
	m.epicPicker = m.epicPicker.Hide()
	if m.loader == nil {
		return m, nil
	}

	var oldKey, oldSum string
	if issue := m.detail.Issue(); issue != nil && issue.Key == msg.IssueKey {
		oldKey = issue.ParentKey
		oldSum = issue.ParentSummary
	} else {
		for _, is := range m.list.Issues() {
			if is.Key == msg.IssueKey {
				oldKey = is.ParentKey
				oldSum = is.ParentSummary
				break
			}
		}
	}
	if oldKey == msg.ParentKey {
		return m, nil
	}

	newSum := ""
	for _, ep := range loaded {
		if ep.Key == msg.ParentKey {
			newSum = ep.Summary
			break
		}
	}

	m.list.UpdateIssueParent(msg.IssueKey, msg.ParentKey, newSum)
	m.detail.UpdateParent(msg.IssueKey, msg.ParentKey, newSum)

	loader := m.loader
	issueKey, parentKey := msg.IssueKey, msg.ParentKey
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := loader.SetParent(ctx, issueKey, parentKey)
		return setParentDoneMsg{
			IssueKey:     issueKey,
			OldParentKey: oldKey,
			OldParentSum: oldSum,
			NewParentKey: parentKey,
			Err:          err,
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleSetParentDone consumes the SetParent result, reverting the
// optimistic update on failure.
func (m Model) handleSetParentDone(msg setParentDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.list.UpdateIssueParent(msg.IssueKey, msg.OldParentKey, msg.OldParentSum)
		m.detail.UpdateParent(msg.IssueKey, msg.OldParentKey, msg.OldParentSum)
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Could not set epic: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	return m, stopSpinner
}

// openDescriptionOverlay opens the description-edit textarea, prefilled
// with the current markdown body.
func (m Model) openDescriptionOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.description, cmd = m.description.Show(issue.Key, issue.Description)
	return m, cmd
}

// descriptionDoneMsg carries the result of an UpdateDescription call.
type descriptionDoneMsg struct {
	IssueKey string
	NewBody  string
	PrevBody string
	Err      error
}

// handleDescriptionSubmitted dispatches UpdateDescription with optimistic
// local update of the displayed markdown body. On error the previous
// body is restored.
func (m Model) handleDescriptionSubmitted(msg overlays.DescriptionSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	prev := issue.Description
	if msg.Body == prev {
		return m, nil
	}
	m.detail.UpdateDescription(msg.IssueKey, msg.Body)

	loader := m.loader
	key, body := msg.IssueKey, msg.Body
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.UpdateDescription(context.Background(), key, body)
		return descriptionDoneMsg{IssueKey: key, NewBody: body, PrevBody: prev, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleDescriptionDone consumes the UpdateDescription result.
func (m Model) handleDescriptionDone(msg descriptionDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.detail.UpdateDescription(msg.IssueKey, msg.PrevBody)
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Edit description failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{Text: "Updated description", Level: ToastInfo}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// openRemoveLinkOverlay opens the remove-link picker over the current
// issue's links. No-op when no issue is selected; opens with an empty
// state when the issue has no links yet (the user gets visual feedback
// the keypress was seen).
func (m Model) openRemoveLinkOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	entries := make([]overlays.LinkEntry, 0, len(issue.Links))
	for _, l := range issue.Links {
		entries = append(entries, overlays.LinkEntry{
			ID:       l.ID,
			Relation: l.Relation,
			OtherKey: l.OtherKey,
			Summary:  l.Summary,
		})
	}
	m.linkRemove = m.linkRemove.Show(issue.Key, entries)
	return m, nil
}

// linkDeletedDoneMsg carries the result of a DeleteIssueLink call.
type linkDeletedDoneMsg struct {
	IssueKey string
	OtherKey string
	Err      error
}

// handleLinkDeletePicked dispatches DeleteIssueLink with optimistic local
// removal — the link disappears from the detail pane immediately. On
// failure it is restored from the prior snapshot.
func (m Model) handleLinkDeletePicked(msg overlays.LinkDeletedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil || msg.LinkID == "" {
		return m, nil
	}
	// Snapshot for revert.
	var prev jira.IssueLink
	if issue := m.detail.Issue(); issue != nil && issue.Key == msg.IssueKey {
		for _, l := range issue.Links {
			if l.ID == msg.LinkID {
				prev = l
				break
			}
		}
	}
	m.detail.RemoveLink(msg.IssueKey, msg.OtherKey)

	loader := m.loader
	linkID := msg.LinkID
	owning := msg.IssueKey
	other := msg.OtherKey
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.DeleteLink(context.Background(), linkID)
		if err != nil {
			// Reattach a copy for revert via the result handler.
			return linkDeletedDoneMsg{
				IssueKey: owning,
				OtherKey: other,
				Err:      err,
			}
		}
		_ = prev
		return linkDeletedDoneMsg{IssueKey: owning, OtherKey: other}
	}
	// We can't capture the closure-mutated `prev` without a state field;
	// stash it on the model. Errors revert by re-appending what we knew.
	m.pendingDeletedLink = prev
	return m, tea.Batch(startSpinner, call)
}

// handleLinkDeleteDone toasts success/failure of a link deletion. On
// failure, re-adds the optimistically-removed link.
func (m Model) handleLinkDeleteDone(msg linkDeletedDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		if m.pendingDeletedLink.OtherKey != "" {
			m.detail.AppendLink(msg.IssueKey, m.pendingDeletedLink)
		}
		m.pendingDeletedLink = jira.IssueLink{}
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Remove link failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	m.pendingDeletedLink = jira.IssueLink{}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Removed link to " + msg.OtherKey,
			Level: ToastInfo,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// openLinkOverlay opens the add-link overlay scoped to the currently
// selected issue. No-op when nothing is selected.
func (m Model) openLinkOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.link, cmd = m.link.Show(issue.Key)
	return m, cmd
}

// linkDoneMsg carries the result of a CreateIssueLink call back to the
// root model so it can render an info or error toast.
type linkDoneMsg struct {
	IssueKey  string
	Type      string
	TargetKey string
	Err       error
}

// handleLinkSubmitted dispatches the CreateIssueLink network call. On
// success the user gets an info toast and the detail pane gains a
// provisional link entry; on error a toast surfaces the message. The
// optimistic entry is partial — only OtherKey, TypeName and Outward are
// known locally. The next refresh fills in the missing summary/status.
func (m Model) handleLinkSubmitted(msg overlays.LinkSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	issue := m.detail.Issue()
	if issue != nil && issue.Key == msg.IssueKey {
		m.detail.AppendLink(msg.IssueKey, jira.IssueLink{
			Relation: strings.ToLower(msg.Type),
			TypeName: msg.Type,
			OtherKey: msg.TargetKey,
			Outward:  true,
		})
	}
	loader := m.loader
	typ, target := msg.Type, msg.TargetKey
	owning := msg.IssueKey
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.CreateLink(context.Background(), typ, owning, target)
		return linkDoneMsg{IssueKey: owning, Type: typ, TargetKey: target, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleLinkDone consumes the CreateIssueLink result. Removes the
// optimistic entry and surfaces an error toast on failure; on success
// shows a confirmation and triggers a detail refresh so the link gets
// its summary/status from Jira.
func (m Model) handleLinkDone(msg linkDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.detail.RemoveLink(msg.IssueKey, msg.TargetKey)
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Link failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Linked " + msg.IssueKey + " " + msg.Type + " " + msg.TargetKey,
			Level: ToastInfo,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// openFavoritesOverlay reads the saved favorites from disk and opens the
// picker. The current search query (when in Search view) is passed in so
// the overlay can offer save-mode. Disk read happens on demand here, not
// at startup, so the picker always shows fresh data when a save from
// another window has just landed.
func (m Model) openFavoritesOverlay() (tea.Model, tea.Cmd) {
	entries := m.loadFavoriteEntries()
	current := ""
	if m.view == panes.ViewSearch {
		current = m.searchQuery
	}
	m.favorites = m.favorites.Show(entries, current)
	return m, nil
}

// loadFavoriteEntries returns the saved favorites as overlay entries, or
// an empty slice when state is unavailable.
func (m Model) loadFavoriteEntries() []overlays.FavoriteEntry {
	if m.statePath == "" {
		return nil
	}
	st, err := state.Load(m.statePath)
	if err != nil {
		return nil
	}
	out := make([]overlays.FavoriteEntry, 0, len(st.Favorites))
	for _, f := range st.Favorites {
		out = append(out, overlays.FavoriteEntry{Name: f.Name, JQL: f.JQL})
	}
	return out
}

// handleFavoriteApplied switches to the Search view with the chosen JQL
// and dispatches a refresh. Reuses the same plumbing as the search input.
func (m Model) handleFavoriteApplied(msg overlays.FavoriteAppliedMsg) (tea.Model, tea.Cmd) {
	m.view = panes.ViewSearch
	m.searchQuery = msg.JQL
	m.list.SetSearchCollapsed(msg.JQL)
	m.list.SetStrategy(grouping.ByStatus{})
	m.detail.SetIssue(nil)
	m.selectedKey = ""
	m.list.Top()
	updated, cmd := m.dispatchListRefresh()
	return updated, cmd
}

// handleFavoriteSaved persists a new named favorite to state.json. The
// write happens in a goroutine via state.Mutate so the Update loop never
// blocks on disk I/O.
func (m Model) handleFavoriteSaved(msg overlays.FavoriteSavedMsg) (tea.Model, tea.Cmd) {
	if m.statePath == "" {
		return m, nil
	}
	path := m.statePath
	name, jql := msg.Name, msg.JQL
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			// Replace any existing entry with the same name so saving
			// twice with the same name updates rather than duplicates.
			for i := range s.Favorites {
				if s.Favorites[i].Name == name {
					s.Favorites[i].JQL = jql
					return
				}
			}
			s.Favorites = append(s.Favorites, state.Favorite{Name: name, JQL: jql})
		})
	}()
	toast := func() tea.Msg {
		return ToastMsg{Text: "Saved favorite: " + name, Level: ToastInfo}
	}
	return m, toast
}

// handleFavoriteDeleted persists the removal to state.json.
func (m Model) handleFavoriteDeleted(msg overlays.FavoriteDeletedMsg) (tea.Model, tea.Cmd) {
	if m.statePath == "" {
		return m, nil
	}
	path := m.statePath
	name := msg.Name
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			out := s.Favorites[:0]
			for _, f := range s.Favorites {
				if f.Name != name {
					out = append(out, f)
				}
			}
			s.Favorites = out
		})
	}()
	toast := func() tea.Msg {
		return ToastMsg{Text: "Deleted favorite: " + name, Level: ToastInfo}
	}
	return m, toast
}

// openEditOverlay opens the generic field-edit overlay scoped to the
// currently selected issue and field. No-op when nothing is selected. The
// pre-fill is the field's current display value so users can start from
// the existing text rather than retyping.
func (m Model) openEditOverlay(field overlays.EditField) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	current := ""
	switch field {
	case overlays.EditSummary:
		current = issue.Summary
	case overlays.EditPriority:
		current = issue.Priority.Name
	case overlays.EditLabels:
		current = strings.Join(issue.Labels, ", ")
	case overlays.EditDueDate:
		current = issue.DueDate
	}
	var cmd tea.Cmd
	m.edit, cmd = m.edit.Show(issue.Key, field, current)
	return m, cmd
}

// editDoneMsg carries the result of an UpdateFields call back to the root
// model so optimistic updates can be reverted on error.
type editDoneMsg struct {
	IssueKey string
	Field    overlays.EditField
	// Only the field matching the edit is meaningful — the rest are zero.
	PrevSummary  string
	PrevPriority jira.Priority
	PrevLabels   []string
	PrevDueDate  string
	Err          error
}

// handleEditSubmitted applies an optimistic update for the chosen field
// and dispatches the UpdateFields network call. Empty values are rejected
// for fields that cannot meaningfully be empty (summary, priority); for
// labels and due date an empty value is a deliberate "clear" operation.
func (m Model) handleEditSubmitted(msg overlays.EditSubmittedMsg) (tea.Model, tea.Cmd) {
	if msg.Value == "" && (msg.Field == overlays.EditSummary || msg.Field == overlays.EditPriority) {
		toast := func() tea.Msg {
			return ToastMsg{Text: "Empty value, edit cancelled", Level: ToastInfo}
		}
		return m, toast
	}
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }

	switch msg.Field {
	case overlays.EditSummary:
		prev := issue.Summary
		if msg.Value == prev {
			return m, nil
		}
		m.list.UpdateIssueSummary(msg.IssueKey, msg.Value)
		m.detail.UpdateSummary(msg.IssueKey, msg.Value)
		key := msg.IssueKey
		newVal := msg.Value
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"summary": newVal,
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditSummary, PrevSummary: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)

	case overlays.EditPriority:
		prev := issue.Priority
		if msg.Value == prev.Name {
			return m, nil
		}
		next := jira.Priority{Name: msg.Value}
		m.list.UpdateIssuePriority(msg.IssueKey, next)
		m.detail.UpdatePriority(msg.IssueKey, next)
		key := msg.IssueKey
		newName := msg.Value
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"priority": map[string]any{"name": newName},
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditPriority, PrevPriority: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)

	case overlays.EditLabels:
		prev := append([]string(nil), issue.Labels...)
		next := splitLabels(msg.Value)
		if labelsEqual(prev, next) {
			return m, nil
		}
		m.list.UpdateIssueLabels(msg.IssueKey, next)
		m.detail.UpdateLabels(msg.IssueKey, next)
		key := msg.IssueKey
		// Pass a fresh copy to the goroutine so the closure does not race
		// with the optimistic update we just queued on the main model.
		nextCopy := append([]string(nil), next...)
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"labels": nextCopy,
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditLabels, PrevLabels: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)

	case overlays.EditDueDate:
		prev := issue.DueDate
		if msg.Value == prev {
			return m, nil
		}
		m.list.UpdateIssueDueDate(msg.IssueKey, msg.Value)
		m.detail.UpdateDueDate(msg.IssueKey, msg.Value)
		key := msg.IssueKey
		newVal := msg.Value
		// Jira accepts "" via JSON null to clear the field; sending an empty
		// string would be rejected as an invalid date.
		var wire any = newVal
		if newVal == "" {
			wire = nil
		}
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"duedate": wire,
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditDueDate, PrevDueDate: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)
	}
	return m, nil
}

// splitLabels turns a comma-separated user input into the de-duplicated,
// trimmed slice Jira expects. Empty segments and pure-whitespace ones are
// dropped so users can type "a, b ,c," without surprises.
func splitLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for raw := range strings.SplitSeq(s, ",") {
		l := strings.TrimSpace(raw)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// labelsEqual reports whether two label slices contain the same elements
// in the same order.
func labelsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// handleEditDone consumes the UpdateFields result. On error the optimistic
// update is reverted and the user is told via toast.
func (m Model) handleEditDone(msg editDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err == nil {
		toast := func() tea.Msg {
			return ToastMsg{Text: "Updated " + msg.Field.FieldName(), Level: ToastInfo}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	switch msg.Field {
	case overlays.EditSummary:
		m.list.UpdateIssueSummary(msg.IssueKey, msg.PrevSummary)
		m.detail.UpdateSummary(msg.IssueKey, msg.PrevSummary)
	case overlays.EditPriority:
		m.list.UpdateIssuePriority(msg.IssueKey, msg.PrevPriority)
		m.detail.UpdatePriority(msg.IssueKey, msg.PrevPriority)
	case overlays.EditLabels:
		m.list.UpdateIssueLabels(msg.IssueKey, msg.PrevLabels)
		m.detail.UpdateLabels(msg.IssueKey, msg.PrevLabels)
	case overlays.EditDueDate:
		m.list.UpdateIssueDueDate(msg.IssueKey, msg.PrevDueDate)
		m.detail.UpdateDueDate(msg.IssueKey, msg.PrevDueDate)
	}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Edit " + msg.Field.FieldName() + " failed: " + msg.Err.Error(),
			Level: ToastError,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// copySelectedIssue copies either the issue key or its URL to the system
// clipboard via OSC 52 and posts a confirmation toast. No-op when nothing
// is selected, or when copying a URL that wasn't populated by the loader.
func (m Model) copySelectedIssue(asURL bool) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	text, label := issue.Key, "key"
	if asURL {
		if issue.URL == "" {
			return m, nil
		}
		text, label = issue.URL, "URL"
	}
	return m, func() tea.Msg {
		if err := copyToClipboard(nil, text); err != nil {
			return ToastMsg{Text: "Copy failed: " + err.Error(), Level: ToastError}
		}
		return ToastMsg{Text: "Copied " + label + ": " + issue.Key, Level: ToastInfo}
	}
}

// openAssignOverlay opens the `a` overlay scoped to the currently selected
// issue. When no issue is selected it is a no-op.
func (m Model) openAssignOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.assign, cmd = m.assign.Show(issue.Key, issue.Assignee)
	return m, cmd
}

// createProjectsLoadedMsg carries the result of the projects fetch
// triggered by openCreateOverlay. It is internal — never emitted by the
// overlay itself.
type createProjectsLoadedMsg struct {
	Projects []jira.Project
	Err      error
}

// openCreateOverlay starts the create wizard. The projects fetch is
// asynchronous; the overlay opens at Step 1 once the response arrives.
func (m Model) openCreateOverlay() (tea.Model, tea.Cmd) {
	loader := m.loader
	if loader == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		ps, err := loader.Projects(context.Background())
		return createProjectsLoadedMsg{Projects: ps, Err: err}
	}
}

// createSubtaskProjectsLoadedMsg is the result of the projects fetch
// triggered by openCreateSubtaskOverlay. It carries the parent issue so
// the subsequent ShowAsSubtask call has the right context.
type createSubtaskProjectsLoadedMsg struct {
	Parent   jira.Issue
	Projects []jira.Project
	Err      error
}

// openCreateSubtaskOverlay opens the create wizard in subtask mode for
// the issue currently displayed in the detail pane. No-op when the
// detail is empty or no loader is wired.
func (m Model) openCreateSubtaskOverlay() (tea.Model, tea.Cmd) {
	loader := m.loader
	parent := m.detail.Issue()
	if loader == nil || parent == nil {
		return m, nil
	}
	parentClone := *parent
	return m, func() tea.Msg {
		ps, err := loader.Projects(context.Background())
		return createSubtaskProjectsLoadedMsg{Parent: parentClone, Projects: ps, Err: err}
	}
}

// handleAssignSearchRequest dispatches a SearchUsers call for the query the
// overlay just debounced. Stale requests (the user kept typing) and
// requests arriving after the overlay closed are silently dropped.
func (m Model) handleAssignSearchRequest(msg overlays.AssignSearchRequestMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	if !m.assign.Visible() || msg.Token != m.assign.Token() {
		return m, nil
	}
	loader := m.loader
	query := msg.Query
	token := msg.Token
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		users, err := loader.SearchUsers(context.Background(), query)
		return assignSearchDoneMsg{
			Result: overlays.AssignResultsMsg{Query: query, Token: token, Users: users, Err: err},
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleAssignSelected applies an optimistic assignee change to the list +
// detail panes, then dispatches the AssignIssue network call. The spinner
// counter is bumped while the call is in flight.
func (m Model) handleAssignSelected(msg overlays.AssignSelectedMsg) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	next := msg.User
	m.list.UpdateIssueAssignee(msg.IssueKey, &next)
	m.detail.UpdateAssignee(msg.IssueKey, &next)

	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	issueKey := msg.IssueKey
	accountID := msg.User.AccountID
	prev := msg.PrevAssignee
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.AssignIssue(context.Background(), issueKey, accountID)
		return assignDoneMsg{
			IssueKey:     issueKey,
			NewAssignee:  next,
			PrevAssignee: prev,
			Err:          err,
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleAssignDone consumes the AssignIssue result. On error the assignee
// is rolled back; on success a confirmation toast is shown.
func (m Model) handleAssignDone(msg assignDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.list.UpdateIssueAssignee(msg.IssueKey, msg.PrevAssignee)
		m.detail.UpdateAssignee(msg.IssueKey, msg.PrevAssignee)
		toast := func() tea.Msg {
			return ToastMsg{Text: "Assign failed: " + msg.Err.Error(), Level: ToastError}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{Text: "Assigned to " + msg.NewAssignee.DisplayName, Level: ToastInfo}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// assignSearchDoneMsg wraps an AssignResultsMsg so the root model can
// observe completion (decrement spinner) before passing the underlying
// result through to the overlay.
type assignSearchDoneMsg struct {
	Result overlays.AssignResultsMsg
}

// handleCommentSubmitted dispatches the AddComment network call when the
// overlay confirms a draft. With no loader wired (e.g. tests asserting only
// overlay behaviour), the message is dropped on the floor.
func (m Model) handleCommentSubmitted(msg overlays.CommentSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	issueKey := msg.IssueKey
	body := msg.Body
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.AddComment(context.Background(), issueKey, body)
		return commentDoneMsg{IssueKey: issueKey, Body: body, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleCommentDone consumes the AddComment result. On success the body is
// appended to the detail pane (when the user has not navigated away) and an
// info toast is shown; on error the user is told via toast and nothing is
// appended.
func (m Model) handleCommentDone(msg commentDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		toast := func() tea.Msg {
			return ToastMsg{Text: "Comment failed: " + msg.Err.Error(), Level: ToastError}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	m.detail.AppendComment(msg.IssueKey, jira.Comment{
		Body:    msg.Body,
		Created: time.Now(),
	})
	m.clearDraft(msg.IssueKey)
	toast := func() tea.Msg { return ToastMsg{Text: "Comment added", Level: ToastInfo} }
	return m, tea.Batch(stopSpinner, toast)
}

// handleTransitionSelected applies an optimistic status change to the list
// and detail panes, then dispatches the DoTransition network call. The
// spinner counter is bumped while the call is in flight; the corresponding
// decrement happens in handleTransitionDone.
func (m Model) handleTransitionSelected(msg overlays.TransitionSelectedMsg) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	prev := issue.Status
	next := msg.Transition.To
	m.list.UpdateIssueStatus(msg.IssueKey, next)
	m.detail.UpdateStatus(msg.IssueKey, next)

	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	key := msg.IssueKey
	id := msg.Transition.ID
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.DoTransition(context.Background(), key, id)
		return transitionDoneMsg{
			IssueKey:   key,
			PrevStatus: prev,
			NewStatus:  next,
			Err:        err,
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleTransitionDone consumes the DoTransition result. On error the
// optimistic status is reverted and the user is told via toast; on success
// nothing else needs to happen because the optimistic state is now real.
func (m Model) handleTransitionDone(msg transitionDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err == nil {
		return m, stopSpinner
	}
	m.list.UpdateIssueStatus(msg.IssueKey, msg.PrevStatus)
	m.detail.UpdateStatus(msg.IssueKey, msg.PrevStatus)
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Transition failed: " + msg.Err.Error(),
			Level: ToastError,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// handleViewSelected swaps the active view, clears the detail pane, and
// either dispatches a list refresh (My Tasks / Watching / re-running an
// existing search) or opens the search input (Search with no prior query).
func (m Model) handleViewSelected(v panes.ViewKind) (tea.Model, tea.Cmd) {
	if v == m.view {
		return m, nil
	}
	if v != panes.ViewSearch {
		m.searchQuery = ""
	}
	m.view = v
	m.lastSubView[panes.TopGroup(v)] = v
	m.persistLastSubView(panes.TopGroup(v), v)
	m.persistLastView(v)
	switch v {
	case panes.ViewMyTasks:
		m.list.SetStrategy(grouping.ByEpicAndPriority{})
	case panes.ViewWatching, panes.ViewReported, panes.ViewSearch, panes.ViewSprint, panes.ViewMentions:
		m.list.SetStrategy(grouping.ByStatus{})
	case panes.ViewRecent:
		// Recent uses a "by-key" arrangement that mimics insertion order;
		// any grouping will fight that. ByStatus is a tolerable default —
		// the user can switch via the options overlay if they want.
		m.list.SetStrategy(grouping.ByStatus{})
	case panes.ViewStructures:
		// Sectioned mode draws its own headers via the chosen structure;
		// keep a tolerable default within-section grouping.
		m.list.SetStrategy(grouping.ByStatus{})
		if m.defaultProject != "" {
			if _, ok := m.currentStructID[m.defaultProject]; !ok {
				m.currentStructID[m.defaultProject] = structure.BuiltinDefaultID
				m.persistLastStructure(m.defaultProject, structure.BuiltinDefaultID)
			}
		}
	}
	m.detail.SetIssue(nil)
	m.selectedKey = ""
	m.list.Top()

	switch v {
	case panes.ViewSearch:
		if m.searchQuery == "" {
			m.list.SetSearchEditing("")
			m.focus = FocusList
			return m, nil
		}
		m.list.SetSearchCollapsed(m.searchQuery)
	default:
		m.list.SetSearchInactive()
	}
	m, refreshCmd := m.dispatchListRefresh()
	return m, refreshCmd
}

func (m Model) handleListFetched(msg listFetchedMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	// Drop stale results: a newer refresh has already been kicked off, so the
	// in-flight call we're hearing back from is no longer the source of truth.
	if msg.Token != 0 && msg.Token != m.listToken {
		return m, stopSpinner
	}
	if msg.Err != nil {
		toast := func() tea.Msg { return ToastMsg{Text: "Refresh failed: " + msg.Err.Error(), Level: ToastError} }
		return m, tea.Batch(stopSpinner, toast)
	}
	issues := msg.Issues
	if m.view == panes.ViewRecent {
		issues = m.reorderByRecent(issues)
	}
	m.feedList(issues)
	if m.view == panes.ViewMyTasks && m.cachePath != "" && m.accountID != "" {
		path, account := m.cachePath, m.accountID
		toCache := issues
		go func() { _ = SaveCache(path, account, toCache) }()
	}
	prefetchCmd := m.startPrefetch(issues)
	return m, tea.Batch(stopSpinner, prefetchCmd, m.syncDetailFromList())
}

// startPrefetch kicks off a background warm-up of the detail cache for the
// freshly-loaded list. Cancels any prior prefetch so a quick list refresh
// can't pile up overlapping warm-up loops. The spawned goroutine writes
// only into the cache (which has its own mutex); the returned tea.Cmd is a
// repaint tick that keeps the top-bar prefetch indicator updating while
// the warm-up runs. When the loader doesn't cache, returns nil.
func (m *Model) startPrefetch(issues []jira.Issue) tea.Cmd {
	pf, ok := m.loader.(prefetcher)
	if !ok || len(issues) == 0 {
		return nil
	}
	if m.prefetchCancel != nil {
		m.prefetchCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.prefetchCancel = cancel
	keys := make([]string, len(issues))
	for i, is := range issues {
		keys[i] = is.Key
	}
	go pf.PrefetchIssues(ctx, keys)
	return prefetchTick()
}

// prefetchTickMsg fires every prefetchTickInterval while a prefetch is
// running so the top bar can repaint with the new done/total counts. The
// handler stops the loop as soon as the loader reports active=false.
type prefetchTickMsg struct{}

const prefetchTickInterval = 250 * time.Millisecond

func prefetchTick() tea.Cmd {
	return tea.Tick(prefetchTickInterval, func(time.Time) tea.Msg {
		return prefetchTickMsg{}
	})
}

// syncDetailFromList mirrors the list's current selection into the detail
// pane. When the selection is unchanged it is a no-op; when it changes (or
// flips between issue and group header) the detail pane is told to load a
// new issue (or clear) and the resulting batch of load commands is
// returned.
func (m *Model) syncDetailFromList() tea.Cmd {
	cur := ""
	if sel := m.list.Selected(); sel != nil {
		cur = sel.Key
	}
	if cur == m.selectedKey {
		return nil
	}
	m.selectedKey = cur
	return m.detail.SetIssue(m.list.Selected())
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	topBar := m.renderTopBar()
	tabBar := m.renderTabBar()
	hintBar := m.renderHintBar()
	toasts := m.toasts.View(m.styles)

	listW, detailW, previewW, contentHeight := m.paneDims()
	var body string
	if m.preview.Active {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderDetailPane(detailW, contentHeight),
			m.renderPreviewPane(previewW, contentHeight),
		)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderListPane(listW, contentHeight),
			m.renderDetailPane(detailW, contentHeight),
		)
	}

	parts := []string{topBar, tabBar, body}
	if toasts != "" {
		parts = append(parts, toasts)
	}
	parts = append(parts, hintBar)
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if v := m.activeOverlay(); v != "" {
		return overlayCenter(frame, v)
	}
	return frame
}

// activeOverlay returns the visible overlay's rendered view, or "" when no
// overlay is showing. Order matches the input-routing precedence in
// handleKey so the same modal that captures keypresses is also the one
// rendered on top.
func (m Model) activeOverlay() string {
	if v := m.help.View(m.styles); v != "" {
		return v
	}
	if v := m.transition.View(m.styles); v != "" {
		return v
	}
	if v := m.comment.View(m.styles); v != "" {
		return v
	}
	if v := m.assign.View(m.styles); v != "" {
		return v
	}
	if v := m.create.View(m.styles); v != "" {
		return v
	}
	if v := m.options.View(m.styles); v != "" {
		return v
	}
	if v := m.edit.View(m.styles); v != "" {
		return v
	}
	if v := m.favorites.View(m.styles); v != "" {
		return v
	}
	if v := m.link.View(m.styles); v != "" {
		return v
	}
	if v := m.linkRemove.View(m.styles); v != "" {
		return v
	}
	if v := m.worklog.View(m.styles); v != "" {
		return v
	}
	if v := m.worklogRemove.View(m.styles); v != "" {
		return v
	}
	if v := m.description.View(m.styles); v != "" {
		return v
	}
	if v := m.priority.View(m.styles); v != "" {
		return v
	}
	if v := m.epicPicker.View(m.styles); v != "" {
		return v
	}
	if v := m.structPicker.View(m.styles); v != "" {
		return v
	}
	if v := m.topGo.View(m.styles); v != "" {
		return v
	}
	return ""
}

// overlayCenter places fg over the centre of bg. Both arguments may carry
// ANSI styling; cell positions are computed via lipgloss.Width / ansi helpers
// so wide runes and escape sequences don't shift the splice points.
func overlayCenter(bg, fg string) string {
	bgW := lipgloss.Width(bg)
	bgH := lipgloss.Height(bg)
	fgW := lipgloss.Width(fg)
	fgH := lipgloss.Height(fg)
	x := (bgW - fgW) / 2
	y := (bgH - fgH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return overlayCompose(bg, fg, x, y)
}

// overlayCompose splices fg onto bg starting at cell coordinate (x, y). For
// each row covered by fg the underlying bg row is cut at startX and endX;
// the gap between is replaced verbatim by the corresponding fg line. Lines
// outside fg's vertical range are left untouched.
func overlayCompose(bg, fg string, x, y int) string {
	if fg == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]
		fgW := lipgloss.Width(fgLine)
		left := ansi.Truncate(bgLine, x, "")
		if lw := lipgloss.Width(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ansi.TruncateLeft(bgLine, x+fgW, "")
		bgLines[row] = left + fgLine + right
	}
	return strings.Join(bgLines, "\n")
}

func (m Model) paneDims() (listW, detailW, previewW, contentHeight int) {
	topBar := m.renderTopBar()
	tabBar := m.renderTabBar()
	hintBar := m.renderHintBar()
	overhead := lipgloss.Height(topBar) + lipgloss.Height(tabBar) + lipgloss.Height(hintBar)
	if v := m.toasts.View(m.styles); v != "" {
		overhead += lipgloss.Height(v)
	}
	contentHeight = max(m.height-overhead, 3)
	if m.preview.Active {
		listW = 0
		detailW = m.width / 2
		previewW = m.width - detailW
		return
	}
	listW = m.width / 2
	detailW = m.width - listW
	return
}

func (m Model) renderTopBar() string {
	parts := []string{m.styles.TopBar.Render("~/RJ>"), m.renderTabs()}
	if sp := m.spinner.View(); sp != "" {
		parts = append(parts, m.styles.Accent.Render(sp))
	}
	if pi := m.renderPrefetchIndicator(); pi != "" {
		parts = append(parts, pi)
	}
	if m.statusText != "" {
		parts = append(parts, m.styles.Muted.Render(m.statusText))
	}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(parts, "  "))
}

// renderTabs returns just the horizontal tab cells, with the active view
// highlighted. Search is a transient mode (entered via `/`) rather than a
// tab; when active it appends a SEARCH cell so the user has a visual cue,
// but it is not part of the `[`/`]` cycle.
// renderTabs draws a single-row drill-down strip: the active top group's
// label, then a `›` separator, then the sub-views of that group as pills.
// Other top groups are reachable via `}`/`{`. When the active top has only
// one sub-view (Sprint / Structures / Search), only the top label is shown.
func (m Model) renderTabs() string {
	active := panes.TopGroup(m.view)
	topCell := m.styles.ActiveTab.Render(active.String()) +
		m.styles.Muted.Render(" [g]")
	subs := panes.SubViews(active)
	if len(subs) <= 1 {
		return topCell
	}
	subCells := make([]string, 0, len(subs))
	for _, v := range subs {
		label := panes.SubLabel(v)
		if v == m.view {
			subCells = append(subCells, m.styles.ActiveTab.Render(label))
		} else {
			subCells = append(subCells, m.styles.InactiveTab.Render(label))
		}
	}
	sep := m.styles.Muted.Render(" › ")
	return topCell + sep + lipgloss.JoinHorizontal(lipgloss.Top, subCells...)
}

// renderPrefetchIndicator returns a static "▒ caching N/M" chip when the
// detail cache is warming up, otherwise "". Static glyph (no animation) is
// the deliberate distinction from the spinner — the spinner means "the user
// is waiting on a network call", this indicator means "the user is free to
// keep working while we warm the cache in the background".
func (m Model) renderPrefetchIndicator() string {
	pf, ok := m.loader.(prefetchProgressReporter)
	if !ok {
		return ""
	}
	done, total, active := pf.PrefetchProgress()
	if !active || total == 0 {
		return ""
	}
	return m.styles.Muted.Render(fmt.Sprintf("▒ %d/%d", done, total))
}

func (m Model) renderHintBar() string {
	parts := []string{
		"↑/↓ nav",
		"}/{ top tab",
		"]/[ sub-tab",
		"? help",
	}
	return m.styles.HintBar.Width(m.width).Render(strings.Join(parts, "  "))
}

func (m Model) renderListPane(w, h int) string {
	border := m.styles.PaneBorder
	if m.focus == FocusList {
		border = m.styles.PaneBorderFocused
	}
	title := m.styles.PaneTitle.Render("Issues")
	body := m.list.View()
	if body == "" {
		body = m.styles.Muted.Render("Loading…")
	}
	content := title + "\n" + body
	return border.Width(max(w-2, 1)).Height(max(h-2, 1)).Render(content)
}

func (m Model) renderDetailPane(w, h int) string {
	border := m.styles.PaneBorder
	if m.focus == FocusDetail {
		border = m.styles.PaneBorderFocused
	}
	title := m.styles.PaneTitle.Render("Details")
	body := m.detail.View()
	content := title + "\n" + body
	return border.Width(max(w-2, 1)).Height(max(h-2, 1)).Render(content)
}

// renderPreviewPane draws the third pane that holds the inline image
// preview. The pane intentionally skips lipgloss border rendering around
// the body — the Kitty / iTerm2 graphics escape contains APC sequences
// lipgloss cannot measure, so wrapping it in a styled border distorts
// neighbouring content. We emit a minimal title row, the escape (or a
// placeholder while it loads), and a "←  back" hint.
func (m Model) renderPreviewPane(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	att := m.preview.Attachment
	title := m.styles.PaneTitle.Render("Preview: " + truncatePane(att.Filename, max(w-12, 4)))
	hint := m.styles.Muted.Render("←  back")
	innerW := max(w-2, 1)
	innerH := max(h-3, 1)
	preview := m.detail.AttachmentPreview(att.ID)
	var bodyBlock string
	if preview.Escape == "" {
		bodyBlock = lipgloss.NewStyle().
			Width(innerW).
			Height(innerH).
			Align(lipgloss.Center, lipgloss.Center).
			Render(m.styles.Muted.Render("Loading preview…"))
	} else {
		// Centre the image within the preview pane. The escape is a
		// zero-cell glyph from lipgloss's perspective, so we have to
		// pad with real spaces and newlines around it. Use the cell
		// footprint reported by gfx.Render to compute the slack on
		// each axis.
		padX := max((innerW-preview.Cols)/2, 0)
		padY := max((innerH-preview.Rows)/2, 0)
		var b strings.Builder
		for range padY {
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat(" ", padX))
		b.WriteString(preview.Escape)
		bodyBlock = lipgloss.NewStyle().Width(innerW).Height(innerH).Render(b.String())
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, bodyBlock, hint)
}

// clearGraphicsCmd returns a tea.Cmd that writes the host terminal's
// "delete all images" escape directly to stdout. We deliberately do this
// outside View() because lipgloss/bubbletea's ANSI parser has no concept
// of APC sequences — embedding the escape in the rendered frame caused
// the renderer to misalign rows and stutter on every keypress. A direct
// stdout write fires once at the moment the user leaves the preview pane
// and is invisible to bubbletea's diff renderer.
func clearGraphicsCmd() tea.Cmd {
	clr := gfx.ClearAll()
	if clr == "" {
		return nil
	}
	return func() tea.Msg {
		_, _ = os.Stdout.WriteString(clr)
		return nil
	}
}

// truncatePane abbreviates s with an ellipsis to fit n cells. Mirrors the
// pane-internal helper without coupling app.go to internal/tui/panes.
func truncatePane(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// renderTabBar returns the horizontal divider that sits below the combined
// program-name + tabs row. The tab cells themselves are rendered inline by
// renderTabs as part of renderTopBar.
func (m Model) renderTabBar() string {
	return m.styles.TabDivider.Render(strings.Repeat("─", m.width))
}

// cycleFocus advances the focus by step (positive = forward) between
// FocusList and FocusDetail.
func (m Model) cycleFocus(step int) Model {
	order := []Focus{FocusList, FocusDetail}
	idx := 0
	for i, f := range order {
		if f == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + step + len(order)) % len(order)
	m.focus = order[idx]
	return m
}

// stepFocus moves left (-1) or right (+1) one pane, clamping at the
// edges. The preview pane extends the cascade: from FocusDetail with an
// image attachment available, →  opens the preview pane (list collapses);
// from inside the preview pane, ←  closes it.
func (m Model) stepFocus(step int) (Model, tea.Cmd) {
	if m.preview.Active {
		if step < 0 {
			m.preview.Active = false
			m.preview.Attachment = jira.Attachment{}
			m.focus = FocusDetail
			return m, clearGraphicsCmd()
		}
		return m, nil
	}
	switch m.focus {
	case FocusList:
		if step > 0 {
			m.focus = FocusDetail
		}
	case FocusDetail:
		if step > 0 {
			if att := m.detail.FirstImageAttachment(); att != nil {
				m.preview.Active = true
				m.preview.Attachment = *att
				_, _, previewW, contentH := m.paneDims()
				// Reserve 80% of the available preview area so the
				// rendered image leaves a bit of breathing room on
				// every side; the remaining cells stay free for the
				// title row, the "← back" hint, and visual padding.
				cols := max((previewW-2)*4/5, 4)
				rows := max((contentH-3)*4/5, 4)
				return m, m.detail.LoadAttachmentPreview(*att, cols, rows)
			}
		} else {
			m.focus = FocusList
		}
	}
	return m, nil
}

// nextView returns the view immediately after m.view in the cyclic
// MyTasks → Watching → Search → MyTasks order.
// Search is intentionally excluded from the tab cycle — it is reachable
// only via the `/` hotkey and behaves as a transient mode rather than a
// tab.
func (m Model) nextView() panes.ViewKind {
	return m.cycleTopTab(+1)
}

// prevView is nextView's mirror.
func (m Model) prevView() panes.ViewKind {
	return m.cycleTopTab(-1)
}

// cycleTopTab advances the active top-level tab by step, returning the
// preferred sub-view of the new top: the persisted last sub-view if any,
// else the first sub.
func (m Model) cycleTopTab(step int) panes.ViewKind {
	tops := panes.AllTopTabs()
	cur := panes.TopGroup(m.view)
	idx := 0
	for i, t := range tops {
		if t == cur {
			idx = i
			break
		}
	}
	idx = (idx + step + len(tops)) % len(tops)
	target := tops[idx]
	if v, ok := m.lastSubView[target]; ok {
		return v
	}
	subs := panes.SubViews(target)
	if len(subs) == 0 {
		return panes.ViewMyTasks
	}
	return subs[0]
}

// nextSubView / prevSubView cycle within the active top tab's sub-views.
// Returns m.view unchanged when the top has a single sub.
func (m Model) nextSubView() panes.ViewKind { return m.cycleSubView(+1) }
func (m Model) prevSubView() panes.ViewKind { return m.cycleSubView(-1) }

func (m Model) cycleSubView(step int) panes.ViewKind {
	subs := panes.SubViews(panes.TopGroup(m.view))
	if len(subs) <= 1 {
		return m.view
	}
	idx := 0
	for i, v := range subs {
		if v == m.view {
			idx = i
			break
		}
	}
	return subs[(idx+step+len(subs))%len(subs)]
}

// openSearch flips the active view to Search, focuses the list pane, and
// puts its search header into editing mode pre-filled with the previous
// query (if any). Available from any focus, including FocusDetail.
func (m Model) openSearch() (tea.Model, tea.Cmd) {
	m.focus = FocusList
	m.view = panes.ViewSearch
	m.list.SetSearchEditing(m.searchQuery)
	return m, nil
}

// panesNoopLoader is the default Loader handed to the detail pane when no
// AppLoader was provided. Its methods return the zero value so the pane can
// render the "no issue selected" placeholder without panicking.
type panesNoopLoader struct{}

func (panesNoopLoader) LoadIssue(context.Context, string) (jira.Issue, error) {
	return jira.Issue{}, nil
}
func (panesNoopLoader) LoadComments(context.Context, string) ([]jira.Comment, error) {
	return nil, nil
}
func (panesNoopLoader) LoadTransitions(context.Context, string) ([]jira.Transition, error) {
	return nil, nil
}
func (panesNoopLoader) LoadAttachment(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}

// defaultDescFor returns the user-friendly default direction for a sort
// name when the user has not explicitly toggled it.
func defaultDescFor(sortName string) bool {
	switch sortName {
	case "updated", "created":
		return true
	default:
		return false
	}
}
