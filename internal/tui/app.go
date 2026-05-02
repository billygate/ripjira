package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/tui/gfx"
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

	toasts     Toasts
	spinner    Spinner
	help       overlays.Help
	transition overlays.Transition
	comment    overlays.Comment
	assign     overlays.Assign
	create     overlays.Create
	options    overlays.Options

	list   panes.List
	detail panes.Detail

	loader         AppLoader
	browser        BrowserOpener
	cachePath      string
	statePath      string
	accountID      string
	defaultProject string
	initialIssues  []jira.Issue

	assignDebounce    time.Duration
	assignDebounceSet bool

	autoRefresh time.Duration
	ticker      TickerFunc

	selectedKey string

	preview previewState

	view        panes.ViewKind
	searchQuery string

	listToken  int
	listCancel context.CancelFunc

	prefetchCancel context.CancelFunc

	pendingQuitUntil time.Time
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
		m.assign.Visible() || m.create.Visible() || m.options.Visible() {
		return false
	}
	if m.list.SearchEditing() {
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
	if len(m.initialIssues) > 0 {
		m.list.SetIssues(m.initialIssues)
	}
	if m.statePath != "" {
		if st, err := state.Load(m.statePath); err == nil {
			if st.Grouping != "" {
				m.list.SetStrategy(grouping.ByName(st.Grouping))
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
		}
	}
	return m
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
	AccountID string
	Err       error
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
				return accountIDFetchedMsg{AccountID: me.AccountID, Err: err}
			},
		)
		cmds = append(cmds, func() tea.Msg { return refreshListMsg{} })
	}
	if tick := m.scheduleAutoRefresh(); tick != nil {
		cmds = append(cmds, tick)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
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
	case accountIDFetchedMsg:
		stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
		if msg.Err != nil {
			return m, stopSpinner
		}
		m.accountID = msg.AccountID
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
		m.list.SetIssues(msg.Issues)
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
					st, _ := state.Load(path)
					st.LastProject = pk
					_ = state.Save(path, st)
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
		m.list.SetStrategy(grouping.ByName(msg.Grouping))
		m.list.SetSort(grouping.SortByName(msg.Sort), msg.Desc)
		if m.statePath != "" {
			st, _ := state.Load(m.statePath)
			st.Grouping = msg.Grouping
			st.Sort = msg.Sort
			d := msg.Desc
			st.SortDesc = &d
			_ = state.Save(m.statePath, st)
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
		var cmd tea.Cmd
		m.comment, cmd = m.comment.Update(msg)
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
	// While the list pane's search input is being edited, the input must
	// own the keypress — otherwise typing "n", "s", etc. would trigger
	// global hotkeys (open create, open status…) instead of going into
	// the textinput. The list's own Update handles Enter/Esc.
	if m.list.SearchEditing() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
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
// issue. When no issue is selected it is a no-op.
func (m Model) openCommentOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.comment, cmd = m.comment.Show(issue.Key)
	return m, cmd
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
	switch v {
	case panes.ViewMyTasks:
		m.list.SetStrategy(grouping.ByEpicAndPriority{})
	case panes.ViewWatching, panes.ViewSearch:
		m.list.SetStrategy(grouping.ByStatus{})
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
	m.list.SetIssues(msg.Issues)
	if m.view == panes.ViewMyTasks && m.cachePath != "" && m.accountID != "" {
		path, account, issues := m.cachePath, m.accountID, msg.Issues
		go func() { _ = SaveCache(path, account, issues) }()
	}
	prefetchCmd := m.startPrefetch(msg.Issues)
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
	parts := []string{m.styles.TopBar.Render("~/ripjira>"), m.renderTabs()}
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
func (m Model) renderTabs() string {
	items := []panes.ViewKind{panes.ViewMyTasks, panes.ViewWatching}
	labels := map[panes.ViewKind]string{
		panes.ViewMyTasks:  "MY ISSUES",
		panes.ViewWatching: "WATCHING",
		panes.ViewSearch:   "SEARCH",
	}
	cells := make([]string, 0, len(items)+1)
	for _, v := range items {
		label := labels[v]
		if v == m.view {
			cells = append(cells, m.styles.ActiveTab.Render(label))
		} else {
			cells = append(cells, m.styles.InactiveTab.Render(label))
		}
	}
	if m.view == panes.ViewSearch {
		cells = append(cells, m.styles.ActiveTab.Render(labels[panes.ViewSearch]))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
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
	bindings := m.keymap.ShortHelp()
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, h.Key+" "+h.Desc)
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
	items := []panes.ViewKind{panes.ViewMyTasks, panes.ViewWatching}
	for i, v := range items {
		if v == m.view {
			return items[(i+1)%len(items)]
		}
	}
	return panes.ViewMyTasks
}

// prevView is nextView's mirror.
func (m Model) prevView() panes.ViewKind {
	items := []panes.ViewKind{panes.ViewMyTasks, panes.ViewWatching}
	for i, v := range items {
		if v == m.view {
			return items[(i-1+len(items))%len(items)]
		}
	}
	return panes.ViewMyTasks
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
