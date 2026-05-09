package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/structureadapter"
)

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

// listFetchedMsg carries the result of the background MyIssues call kicked
// off in Init. The app forwards the issues into the list pane and saves
// them to disk for the next start.
type listFetchedMsg struct {
	Token  int
	Issues []jira.Issue
	Err    error
}

// autoRefreshTickMsg fires whenever the configured auto-refresh interval
// elapses. The handler dispatches a silent list refresh and re-arms the
// next tick. Carries no payload — the tick instant itself is irrelevant.
type autoRefreshTickMsg struct{}

// refreshListMsg is dispatched from Init (and from `r` keybinding) so the
// Update path — which can mutate model state — owns the bump of the list
// token and the cancel of any in-flight previous fetch.
type refreshListMsg struct{}

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
	m.focus = FocusList
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
	// Wait for any prior prefetch goroutine to actually exit before
	// starting a new one. Bounded by the cancellation latency of the
	// in-flight HTTP call, so a tea.Quit can rely on the previous handle
	// being closed.
	if m.prefetchDone != nil {
		<-m.prefetchDone
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.prefetchCancel = cancel
	keys := make([]string, len(issues))
	for i, is := range issues {
		keys[i] = is.Key
	}
	done := make(chan struct{})
	m.prefetchDone = done
	go func() {
		defer close(done)
		pf.PrefetchIssues(ctx, keys)
	}()
	return prefetchTick()
}

// prefetchTickMsg fires every prefetchTickInterval while a prefetch is
// running so the top bar can repaint with the new done/total counts. The
// handler stops the loop as soon as the loader reports active=false.
type prefetchTickMsg struct{}

func prefetchTick() tea.Cmd {
	return tea.Tick(prefetchTickInterval, func(time.Time) tea.Msg {
		return prefetchTickMsg{}
	})
}

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
