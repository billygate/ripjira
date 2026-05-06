package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/structure"
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

	toasts        Toasts
	spinner       Spinner
	help          overlays.Help
	transition    overlays.Transition
	comment       overlays.Comment
	assign        overlays.Assign
	create        overlays.Create
	options       overlays.Options
	edit          overlays.Edit
	favorites     overlays.Favorites
	link          overlays.Link
	linkRemove    overlays.RemoveLink
	worklog       overlays.Worklog
	worklogRemove overlays.RemoveWorklog
	description   overlays.Description
	priority      overlays.Priority
	epicPicker    overlays.Epic
	structPicker  overlays.Structures
	scopeEditor   overlays.ScopeEditor
	topGo         overlays.TopGo
	created       overlays.Created

	// createdPending holds the freshly-created issue while the wizard is in
	// Step 4 (link to epic). On EpicPicked / EpicCancelled it is consumed:
	// the post-create popup is shown and this is reset to the zero value.
	createdPending jira.Issue

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

	// recentlyCreated keeps a freshly-created issue alive in the visible list
	// until Jira's eventually-consistent search index returns it for the
	// active view's JQL. Cleared in handleListFetched once the server's
	// response includes the key.
	recentlyCreated jira.Issue

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
		keymap:        km,
		palette:       p,
		styles:        st,
		focus:         FocusList,
		toasts:        NewToasts(),
		spinner:       NewSpinner(),
		help:          overlays.NewHelp(buildHelpColumns(km), km.CloseOverlay),
		transition:    overlays.NewTransition(km.CloseOverlay),
		comment:       overlays.NewComment(km.CloseOverlay),
		assign:        overlays.NewAssign(km.CloseOverlay, overlays.DefaultAssignDebounce),
		create:        overlays.NewCreate(km.CloseOverlay, ""),
		options:       overlays.NewOptions(km.CloseOverlay, "status", "priority", false),
		edit:          overlays.NewEdit(km.CloseOverlay),
		favorites:     overlays.NewFavorites(km.CloseOverlay),
		link:          overlays.NewLink(km.CloseOverlay),
		linkRemove:    overlays.NewRemoveLink(km.CloseOverlay),
		worklog:       overlays.NewWorklog(km.CloseOverlay),
		worklogRemove: overlays.NewRemoveWorklog(km.CloseOverlay),
		description:   overlays.NewDescription(km.CloseOverlay),
		priority:      overlays.NewPriority(km.CloseOverlay),
		epicPicker:    overlays.NewEpic(),
		structPicker:  overlays.NewStructures(km.CloseOverlay),
		scopeEditor:   overlays.NewScopeEditor(km.CloseOverlay),
		topGo:         overlays.NewTopGo(km.CloseOverlay),
		created:       overlays.NewCreated(km.CopyKey, km.CopyURL, km.Browser, km.CloseOverlay),
		list:          panes.New(st, grouping.ByStatus{}, 1, 1),
		detail:        panes.NewDetail(st, panesNoopLoader{}, 1, 1),
		browser:       OSOpener{},
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
		raw := make([]jira.Issue, len(a.Issues))
		for i, x := range a.Issues {
			raw[i] = x.(structureadapter.Adapter).Issue()
		}
		section := panes.Section{Title: a.Title, ReadOnly: st.IsReadOnly(), Issues: raw}
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
	case overlays.StructureEditScopeMsg:
		return m.handleEditScope(msg.ID)
	case overlays.StructureReadOnlyMsg:
		return m, func() tea.Msg {
			return ToastMsg{Text: "structure is read-only", Level: ToastInfo}
		}
	case overlays.ScopeSavedMsg:
		return m.handleScopeSaved(msg)
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
		if m.createdPending.Key != "" {
			issue := m.createdPending
			m.createdPending = jira.Issue{}
			m.created = m.created.Show(issue)
		}
		return m, nil
	case overlays.EpicPickedMsg:
		pendingMatches := m.createdPending.Key != "" && m.createdPending.Key == msg.IssueKey
		mm, cmd := m.handleEpicPicked(msg)
		m = mm.(Model)
		if pendingMatches {
			issue := m.createdPending
			m.createdPending = jira.Issue{}
			m.created = m.created.Show(issue)
		}
		return m, cmd
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
		typeName := m.create.SelectedIssueType().Name
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
			if msg.Issue.Key != "" && parent == "" {
				m.list.PrependIssue(msg.Issue)
				m.recentlyCreated = msg.Issue
			}
			var refresh tea.Cmd
			m, refresh = m.dispatchListRefresh()
			if msg.Issue.Key != "" {
				if m.shouldOfferEpicLink(parent, typeName) {
					m.createdPending = msg.Issue
					m.epicPicker = m.epicPicker.Show(msg.Issue.Key, "")
					return m, tea.Batch(cmd, refresh, m.searchEpicsCmd(msg.Issue.Key))
				}
				m.created = m.created.Show(msg.Issue)
			}
			return m, tea.Batch(cmd, refresh)
		}
		return m, cmd
	case overlays.CreatedDismissedMsg:
		if msg.Key != "" {
			m.list.SelectByKey(msg.Key)
		}
		return m, m.syncDetailFromList()
	case overlays.CreatedCopyRequestedMsg:
		text, label := msg.Text, msg.Label
		return m, func() tea.Msg {
			if err := copyToClipboard(nil, text); err != nil {
				return ToastMsg{Text: "Copy failed: " + err.Error(), Level: ToastError}
			}
			return ToastMsg{Text: "Copied " + label + ": " + text, Level: ToastInfo}
		}
	case overlays.CreatedOpenRequestedMsg:
		if m.browser == nil || msg.URL == "" {
			return m, nil
		}
		url := msg.URL
		opener := m.browser
		return m, func() tea.Msg {
			return browserOpenedMsg{URL: url, Err: opener.Open(url)}
		}
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

// worklogDeletedDoneMsg carries the result of a DeleteWorklog call.
type worklogDeletedDoneMsg struct {
	IssueKey  string
	WorklogID string
	Err       error
}

// shouldOfferEpicLink reports whether the create wizard should advance to
// Step 4 (link to epic) for the just-created issue. Skipped when:
//   - subtask mode (parent is set; subtasks inherit their parent's epic);
//   - the issue type itself is an Epic (linking an epic to an epic is not
//     a meaningful flow in this client).
func (m Model) shouldOfferEpicLink(parentKey, typeName string) bool {
	if parentKey != "" {
		return false
	}
	for _, t := range m.epicTypes {
		if strings.EqualFold(t, typeName) {
			return false
		}
	}
	return true
}

// descriptionDoneMsg carries the result of an UpdateDescription call.
type descriptionDoneMsg struct {
	IssueKey string
	NewBody  string
	PrevBody string
	Err      error
}

// linkDeletedDoneMsg carries the result of a DeleteIssueLink call.
type linkDeletedDoneMsg struct {
	IssueKey string
	OtherKey string
	Err      error
}

// linkDoneMsg carries the result of a CreateIssueLink call back to the
// root model so it can render an info or error toast.
type linkDoneMsg struct {
	IssueKey  string
	Type      string
	TargetKey string
	Err       error
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

// createProjectsLoadedMsg carries the result of the projects fetch
// triggered by openCreateOverlay. It is internal — never emitted by the
// overlay itself.
type createProjectsLoadedMsg struct {
	Projects []jira.Project
	Err      error
}

// createSubtaskProjectsLoadedMsg is the result of the projects fetch
// triggered by openCreateSubtaskOverlay. It carries the parent issue so
// the subsequent ShowAsSubtask call has the right context.
type createSubtaskProjectsLoadedMsg struct {
	Parent   jira.Issue
	Projects []jira.Project
	Err      error
}

// assignSearchDoneMsg wraps an AssignResultsMsg so the root model can
// observe completion (decrement spinner) before passing the underlying
// result through to the overlay.
type assignSearchDoneMsg struct {
	Result overlays.AssignResultsMsg
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
		m.list.SetStrategy(grouping.ByParent{EpicTypes: m.epicTypes})
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
	if m.recentlyCreated.Key != "" {
		found := false
		for i := range issues {
			if issues[i].Key == m.recentlyCreated.Key {
				found = true
				break
			}
		}
		if !found {
			issues = append([]jira.Issue{m.recentlyCreated}, issues...)
		} else {
			m.recentlyCreated = jira.Issue{}
		}
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

// issueKeyInGroupRe extracts a Jira-style key (e.g. BILLING-10118) from a
// group header label so cursor-on-epic-header can preview the epic itself.
var issueKeyInGroupRe = regexp.MustCompile(`[A-Z][A-Z0-9_]*-\d+`)

// syncDetailFromList mirrors the list's current selection into the detail
// pane. When the selection is unchanged it is a no-op; when it changes (or
// flips between issue and group header) the detail pane is told to load a
// new issue (or clear) and the resulting batch of load commands is
// returned. When the cursor sits on a group header whose key looks like a
// Jira issue key (epic / parent grouping), the matching issue is loaded so
// the right pane previews the epic.
func (m *Model) syncDetailFromList() tea.Cmd {
	cur := ""
	var target *jira.Issue
	if sel := m.list.Selected(); sel != nil {
		cur = sel.Key
		target = sel
	} else if gk := m.list.SelectedGroupKey(); gk != "" {
		if k := issueKeyInGroupRe.FindString(gk); k != "" {
			cur = k
			target = &jira.Issue{Key: k}
		}
	}
	if cur == m.selectedKey {
		return nil
	}
	m.selectedKey = cur
	return m.detail.SetIssue(target)
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

// handleEditScope opens the visual scope editor for the structure with the
// given id. Falls back to a toast on read-only structures or load errors.
func (m Model) handleEditScope(id string) (tea.Model, tea.Cmd) {
	pk := m.defaultProject
	if pk == "" || m.structures == nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: store unavailable", Level: ToastError}
		}
	}
	str, err := m.structures.FindByID(pk, id)
	if err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: " + err.Error(), Level: ToastError}
		}
	}
	if str.IsReadOnly() {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structure is read-only", Level: ToastInfo}
		}
	}
	rows := structureadapter.RowsFromFilter(str.Scope)
	issues := m.list.Issues()
	provider := func(field string) []string { return UniqueValues(issues, field) }
	m.scopeEditor = m.scopeEditor.ShowWithID(str.ID, str.Name, rows, provider)
	return m, nil
}

// handleScopeSaved persists the new scope to disk via the structure store
// and re-applies the active structure so the list reflects the change.
func (m Model) handleScopeSaved(msg overlays.ScopeSavedMsg) (tea.Model, tea.Cmd) {
	pk := m.defaultProject
	if pk == "" || m.structures == nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: store unavailable", Level: ToastError}
		}
	}
	str, err := m.structures.FindByID(pk, msg.StructureID)
	if err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "scope save: " + err.Error(), Level: ToastError}
		}
	}
	str.Scope = structureadapter.FilterFromRows(msg.Rows)
	if err := m.structures.SaveStructure(&str); err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "scope save: " + err.Error(), Level: ToastError}
		}
	}
	delete(m.loadedStructs, pk)
	m.feedList(m.list.Issues())
	return m, func() tea.Msg {
		return ToastMsg{Text: "scope saved", Level: ToastInfo}
	}
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
