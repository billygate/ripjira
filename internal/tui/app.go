package tui

import (
	"context"
	"regexp"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui/editor"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
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
	settings      overlays.Settings
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

	cfg     config.Config
	cfgPath string

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

	// editorToken is incremented every time we dispatch an external editor
	// flow. Stale ClosedMsg results (token mismatch) are dropped to keep
	// rapid ctrl+e presses from clobbering the latest issue.
	editorToken int

	// recentlyCreated keeps a freshly-created issue alive in the visible list
	// until Jira's eventually-consistent search index returns it for the
	// active view's JQL. Cleared in handleListFetched once the server's
	// response includes the key.
	recentlyCreated jira.Issue

	prefetchCancel context.CancelFunc
	prefetchDone   chan struct{}

	pendingQuitUntil time.Time

	structures      *structure.Store
	structureEvents <-chan structure.Event
	currentStructID map[string]string
	loadedStructs   map[string][]structure.Structure

	// lastSubView remembers the last sub-view chosen under each top tab so
	// `]`/`[` returns to the user's previous scope rather than always landing
	// on the first sub.
	lastSubView map[panes.TopTabKind]panes.ViewKind

	// chromeHeights caches the rendered height of the topBar / tabBar /
	// hintBar so paneDims doesn't re-render them on every View() call.
	// Filled lazily by paneDims and invalidated on width change.
	chromeHeights chromeHeightCache

	// commentDrafts mirrors state.CommentDrafts loaded once at startup so
	// loadDraft is a map lookup, not sync disk I/O on the main goroutine.
	// saveDraft updates this map and persists to disk in the background.
	commentDrafts map[string]string

	// favoritesCache mirrors state.Favorites loaded once at startup. The
	// favorites overlay reads from here; favorite-write handlers update
	// it alongside firing the background state.Mutate.
	favoritesCache []state.Favorite

	// lastProject is the persisted last-used create-wizard project key.
	// Loaded once at startup; written via state.Mutate after every
	// successful create.
	lastProject string
}

// chromeHeightCache memoises the per-frame heights of the three single-line
// chrome bars. Toast height stays out — it's volatile (TTL-based) and cheap
// to query directly.
type chromeHeightCache struct {
	valid   bool
	width   int
	topBar  int
	tabBar  int
	hintBar int
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
		settings:      overlays.NewSettings(km.CloseOverlay),
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
	m.commentDrafts = map[string]string{}
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
			m.favoritesCache = append([]state.Favorite(nil), st.Favorites...)
			m.lastProject = st.LastProject
			for k, v := range st.CommentDrafts {
				m.commentDrafts[k] = v
			}
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

// maxRecentKeys caps the recently-viewed list. Twenty is a comfortable
// number for daily use without making the JQL `key in (…)` query
// uncomfortably long.
const maxRecentKeys = 20

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

// Config returns the in-memory configuration. Mutations should go through
// the Settings overlay flow which keeps cfg, palette, styles, and the
// auto-refresh timer in sync.
func (m Model) Config() config.Config { return m.cfg }

// ConfigPath returns the on-disk path of config.yaml; the empty string
// disables persistence (useful in tests).
func (m Model) ConfigPath() string { return m.cfgPath }

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
	case editor.ClosedMsg:
		return m.handleEditorClosed(msg)
	case externalEditorDoneMsg:
		return m.handleExternalEditorDone(msg)
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
		if m.lastProject != "" {
			preselect = m.lastProject
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
			var persistLast tea.Cmd
			if msg.ProjectKey != "" {
				m.lastProject = msg.ProjectKey
				pk := msg.ProjectKey
				persistLast = persistAsync(m.statePath, "last project", func(s *state.State) {
					s.LastProject = pk
				})
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
					return m, tea.Batch(cmd, refresh, persistLast, m.searchEpicsCmd(msg.Issue.Key))
				}
				m.created = m.created.Show(msg.Issue)
			}
			return m, tea.Batch(cmd, refresh, persistLast)
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
	case overlays.SettingsAppliedMsg:
		return m.handleSettingsApplied(msg)
	case overlays.SettingsCancelledMsg:
		return m, nil
	case overlays.EpicTypesAppliedMsg:
		m.settings = m.settings.WithEpicTypes(msg.Items)
		return m, nil
	case overlays.EpicTypesCancelledMsg:
		m.settings = m.settings.CloseEpicTypes()
		return m, nil
	case SettingsSaveErrorMsg:
		return m.handleSettingsSaveError(msg)
	case overlays.OptionsAppliedMsg:
		m.list.SetStrategy(grouping.ByName(msg.Grouping, m.epicTypes))
		m.list.SetSort(grouping.SortByName(msg.Sort), msg.Desc)
		grp, srt, desc := msg.Grouping, msg.Sort, msg.Desc
		persist := persistAsync(m.statePath, "options", func(s *state.State) {
			s.Grouping = grp
			s.Sort = srt
			d := desc
			s.SortDesc = &d
		})
		return m, tea.Batch(persist, m.syncDetailFromList())
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
	case panes.DigitJumpTimeoutMsg:
		// Forward the digit-jump timeout from the list pane back to it.
		// Without this route the message lands in the default branch and
		// gets eaten by the spinner, so single-digit jumps never fire.
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
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

// assignSearchDoneMsg wraps an AssignResultsMsg so the root model can
// observe completion (decrement spinner) before passing the underlying
// result through to the overlay.
type assignSearchDoneMsg struct {
	Result overlays.AssignResultsMsg
}

const prefetchTickInterval = 250 * time.Millisecond

// issueKeyInGroupRe extracts a Jira-style key (e.g. BILLING-10118) from a
// group header label so cursor-on-epic-header can preview the epic itself.
var issueKeyInGroupRe = regexp.MustCompile(`[A-Z][A-Z0-9_]*-\d+`)

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
