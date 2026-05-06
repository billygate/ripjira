// Package panes contains the read-only TUI panes (issue list, detail view).
// The List pane wraps bubbles/list with a custom delegate that knows how to
// render group-header rows and issue rows, plus collapse/expand state per
// group key.
package panes

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// digitJumpTimeout is how long the pane waits for a second digit before
// committing a buffered single-digit jump on lists with ≥10 issues.
const digitJumpTimeout = 700 * time.Millisecond

// digitJumpTimeoutMsg is dispatched after digitJumpTimeout to commit a
// pending one-digit jump when the user didn't follow up with a second digit.
type digitJumpTimeoutMsg struct{ gen int }

// searchInputState tracks whether the search header is hidden, editable,
// or collapsed to a one-line summary. Only the Search view ever moves it
// out of searchInactive.
type searchInputState int

const (
	searchInactive searchInputState = iota
	searchEditing
	searchCollapsed
)

// SearchSubmittedMsg is emitted when the user presses Enter in the
// search input with a non-empty value.
type SearchSubmittedMsg struct{ Query string }

// SearchCancelledMsg is emitted on Esc in an empty input with no prior
// query — the root model is expected to revert the active view.
type SearchCancelledMsg struct{}

// IssuesLoadedMsg is dispatched by app code when a fresh issue list has
// arrived from the Jira client; the List pane swaps in the new data.
type IssuesLoadedMsg struct {
	Issues []jira.Issue
}

// listItem is the union row type held by the underlying bubbles/list. It is
// either a group header (Issue == nil) or an issue row.
type listItem struct {
	GroupKey  string
	Issue     *jira.Issue
	Count     int
	Collapsed bool
	Number    int
	NumWidth  int

	// Section is true for section header rows (Structures view). Issues and
	// groups within the section follow until the next section header.
	Section         bool
	SectionTitle    string
	SectionReadOnly bool

	// Depth is the indent level for tree-grouped rows (0 = top group, etc.).
	// GroupPath uniquely identifies a tree node so collapse state survives
	// rebuilds even when titles change. For non-tree group rows GroupPath
	// equals GroupKey.
	Depth     int
	GroupPath string
}

func (l listItem) isSection() bool { return l.Section }
func (l listItem) isGroup() bool   { return !l.Section && l.Issue == nil }

// FilterValue implements list.Item for fuzzy filtering. Group rows match by
// their key; issue rows match by `KEY summary`.
func (l listItem) FilterValue() string {
	if l.isGroup() {
		return l.GroupKey
	}
	return l.Issue.Key + " " + l.Issue.Summary
}

// List is the issue-list pane. It owns the source []jira.Issue, the active
// grouping.Strategy, and per-key collapse state, and projects all of that
// into a flat sequence of bubbles/list items.
type List struct {
	styles    styles.Styles
	strategy  grouping.Strategy
	sort      grouping.Sort
	sortDesc  bool
	issues    []jira.Issue
	groups    []grouping.Group
	collapsed map[string]bool
	list      list.Model
	width     int
	height    int

	searchState searchInputState
	searchQuery string
	input       textinput.Model

	// localFilter narrows the visible rows by case-insensitive substring
	// match on `KEY summary` without hitting the network. localFilterEditing
	// distinguishes "actively typing the filter" from "filter is applied
	// and the input is blurred".
	localFilter        string
	localFilterEditing bool

	pendingDigit int
	pendingGen   int

	sections []Section
}

// Section is one block in the sectioned-list render mode. When SetSections is
// called with a non-empty slice the list renders section headers above the
// grouped rows; passing nil reverts to flat grouping.
//
// When Tree is empty, Issues feed through the current grouping strategy as a
// flat single-level grouping. When Tree is set, it overrides Issues and the
// list walks the tree, emitting one group-header row per node and indenting
// by node depth.
type Section struct {
	Title    string
	ReadOnly bool
	Issues   []jira.Issue
	Tree     []SectionNode
}

// SectionNode is one level in a Section's multi-level grouping tree. Path
// is a unique string used as the collapse-state key. Leaf nodes (Children
// == nil) carry the actual issues; interior nodes carry only Children.
type SectionNode struct {
	Title    string
	Path     string
	Depth    int
	Children []SectionNode
	Issues   []jira.Issue
}

// SetSections enables sectioned mode. Pass nil (or empty) to revert.
func (m *List) SetSections(sections []Section) {
	m.sections = sections
	m.rebuild()
}

// New constructs an empty List bound to the given styles and strategy.
// A nil strategy defaults to ByStatus.
func New(s styles.Styles, strategy grouping.Strategy, width, height int) List {
	if strategy == nil {
		strategy = grouping.ByStatus{}
	}
	d := delegate{styles: s}
	l := list.New(nil, d, width, height)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(false)
	// Surrender keys the app owns: q/esc/?, ←/→/h/l (focus toggling lives
	// in the root model).
	disable := []*key.Binding{
		&l.KeyMap.Quit, &l.KeyMap.ShowFullHelp, &l.KeyMap.CloseFullHelp,
		&l.KeyMap.PrevPage, &l.KeyMap.NextPage, &l.KeyMap.ClearFilter,
	}
	for _, b := range disable {
		b.SetEnabled(false)
	}
	return List{
		styles:    s,
		strategy:  strategy,
		collapsed: map[string]bool{},
		list:      l,
		width:     width,
		height:    height,
	}
}

// Strategy returns the active grouping strategy.
func (m List) Strategy() grouping.Strategy { return m.strategy }

// Sort returns the active within-group sort and direction. A nil Sort
// means no extra reordering is performed beyond the strategy default.
func (m List) Sort() (grouping.Sort, bool) { return m.sort, m.sortDesc }

// SetSort assigns the within-group sort and direction, then rebuilds.
func (m *List) SetSort(s grouping.Sort, desc bool) {
	m.sort = s
	m.sortDesc = desc
	m.rebuild()
}

// Items returns a snapshot of the underlying bubbles/list items. Exposed
// for tests that assert on the rendered ordering.
func (m List) Items() []list.Item {
	return m.list.Items()
}

// SetStrategy switches the grouping strategy, dropping collapse state because
// group keys no longer line up.
func (m *List) SetStrategy(s grouping.Strategy) {
	if s == nil {
		return
	}
	m.strategy = s
	m.collapsed = map[string]bool{}
	m.rebuild()
}

// SetIssues replaces the source data and rebuilds visible items.
func (m *List) SetIssues(issues []jira.Issue) {
	m.issues = issues
	m.rebuild()
}

// UpdateIssueStatus replaces the Status of the issue with the given key in
// the source list and rebuilds the view. Returns true when the key was
// found. The current selection is preserved across the rebuild.
func (m *List) UpdateIssueStatus(key string, status jira.Status) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			m.issues[i].Status = status
			m.rebuild()
			return true
		}
	}
	return false
}

// UpdateIssueSummary replaces the Summary of the issue with the given key
// and rebuilds the view. Returns true when the key was found.
func (m *List) UpdateIssueSummary(key, summary string) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			m.issues[i].Summary = summary
			m.rebuild()
			return true
		}
	}
	return false
}

// UpdateIssuePriority replaces the Priority of the issue with the given key
// and rebuilds the view. Returns true when the key was found.
func (m *List) UpdateIssuePriority(key string, priority jira.Priority) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			m.issues[i].Priority = priority
			m.rebuild()
			return true
		}
	}
	return false
}

// UpdateIssueLabels replaces the Labels slice of the issue with the given
// key and rebuilds the view. Returns true when the key was found.
func (m *List) UpdateIssueLabels(key string, labels []string) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			m.issues[i].Labels = append([]string(nil), labels...)
			m.rebuild()
			return true
		}
	}
	return false
}

// UpdateIssueDueDate replaces the DueDate of the issue with the given key
// and rebuilds the view. Returns true when the key was found.
func (m *List) UpdateIssueDueDate(key, dueDate string) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			m.issues[i].DueDate = dueDate
			m.rebuild()
			return true
		}
	}
	return false
}

// UpdateIssueParent replaces the parent epic key/summary of the issue with
// the given key and rebuilds the view. Used by optimistic epic-link edits.
// Returns true when the key was found.
func (m *List) UpdateIssueParent(key, parentKey, parentSummary string) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			m.issues[i].ParentKey = parentKey
			m.issues[i].ParentSummary = parentSummary
			m.rebuild()
			return true
		}
	}
	return false
}

// UpdateIssueAssignee replaces the Assignee of the issue with the given key
// in the source list and rebuilds the view. Returns true when the key was
// found. Used by optimistic assignee updates from the assign overlay.
func (m *List) UpdateIssueAssignee(key string, assignee *jira.User) bool {
	for i := range m.issues {
		if m.issues[i].Key == key {
			if assignee == nil {
				m.issues[i].Assignee = nil
			} else {
				clone := *assignee
				m.issues[i].Assignee = &clone
			}
			m.rebuild()
			return true
		}
	}
	return false
}

// Issues returns the source list (unmutated).
func (m List) Issues() []jira.Issue { return m.issues }

// Groups returns the strategy's bucketed view of the current source list.
// Useful for tests and external rendering.
func (m List) Groups() []grouping.Group { return m.groups }

// Selected returns the issue under the cursor or nil if a group header is
// selected (or the list is empty).
func (m List) Selected() *jira.Issue {
	it, ok := m.list.SelectedItem().(listItem)
	if !ok || it.isGroup() {
		return nil
	}
	return it.Issue
}

// SelectedGroupKey returns the group key under the cursor when a header is
// selected, "" otherwise.
func (m List) SelectedGroupKey() string {
	it, ok := m.list.SelectedItem().(listItem)
	if !ok || !it.isGroup() {
		return ""
	}
	return it.GroupKey
}

// IsCollapsed reports whether the named group is currently collapsed.
func (m List) IsCollapsed(key string) bool { return m.collapsed[key] }

// ToggleSelectedGroup flips collapse state of the group under the cursor.
// No-op when the cursor is on an issue row.
func (m *List) ToggleSelectedGroup() {
	gk := m.SelectedGroupKey()
	if gk == "" {
		return
	}
	m.collapsed[gk] = !m.collapsed[gk]
	m.rebuild()
}

// SetSize forwards width/height to the underlying list.
func (m *List) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.list.SetSize(w, h)
}

// SearchEditing reports whether the search input is currently being
// edited (Step 1 of the search flow).
func (m List) SearchEditing() bool { return m.searchState == searchEditing }

// SetSearchInactive returns the pane to its plain list rendering.
func (m *List) SetSearchInactive() {
	m.searchState = searchInactive
	m.searchQuery = ""
	m.input.Blur()
	m.list.SetHeight(m.height)
}

// SetSearchEditing puts the pane into the textinput state. prefill is the
// initial value (use "" for a fresh search). The input takes focus.
func (m *List) SetSearchEditing(prefill string) {
	m.input = textinput.New()
	m.input.Placeholder = "Type JQL or text, Enter to search · Esc to cancel"
	m.input.Prompt = "> "
	m.input.SetValue(prefill)
	m.input.Focus()
	m.searchState = searchEditing
	m.list.SetHeight(maxInt(0, m.height-1))
}

// LocalFilter returns the active local-filter string ("" when not filtering).
func (m List) LocalFilter() string { return m.localFilter }

// LocalFilterEditing reports whether the user is currently typing into the
// local-filter input.
func (m List) LocalFilterEditing() bool { return m.localFilterEditing }

// BeginLocalFilter focuses the textinput in local-filter mode. While the
// user types, every keystroke updates the filter and re-renders. Esc clears
// the filter; Enter commits it (input blurs, filter remains applied).
func (m *List) BeginLocalFilter() {
	m.input = textinput.New()
	m.input.Placeholder = "filter by key or summary · Esc to clear"
	m.input.Prompt = "▽ "
	m.input.SetValue(m.localFilter)
	m.input.CursorEnd()
	m.input.Focus()
	m.localFilterEditing = true
	m.list.SetHeight(maxInt(0, m.height-1))
}

// ClearLocalFilter drops the filter and exits the editing mode.
func (m *List) ClearLocalFilter() {
	m.localFilter = ""
	m.localFilterEditing = false
	m.input.Blur()
	m.list.SetHeight(m.height)
	m.rebuild()
}

// SetSearchCollapsed shows the 🔍 query header above the results.
func (m *List) SetSearchCollapsed(query string) {
	m.searchState = searchCollapsed
	m.searchQuery = query
	m.input.Blur()
	m.list.SetHeight(maxInt(0, m.height-1))
}

// visibleIssueCount returns the number of issue rows currently visible
// (excluding group headers and rows in collapsed groups).
func (m List) visibleIssueCount() int {
	n := 0
	for _, raw := range m.list.Items() {
		if row, ok := raw.(listItem); ok && !row.isGroup() {
			n++
		}
	}
	return n
}

// JumpToIssue moves the cursor to the n-th visible issue row (1-based),
// skipping group headers. Out-of-range n is a no-op.
func (m *List) JumpToIssue(n int) {
	if n < 1 {
		return
	}
	count := 0
	for idx, raw := range m.list.Items() {
		row, ok := raw.(listItem)
		if !ok || row.isGroup() {
			continue
		}
		count++
		if count == n {
			m.list.Select(idx)
			return
		}
	}
}

// SelectByKey moves the cursor to the row whose issue key equals key. Returns
// true when the issue was found in the visible items, false otherwise (in
// which case the cursor is left unchanged).
func (m *List) SelectByKey(key string) bool {
	if key == "" {
		return false
	}
	for idx, raw := range m.list.Items() {
		row, ok := raw.(listItem)
		if !ok || row.isGroup() {
			continue
		}
		if row.Issue.Key == key {
			m.list.Select(idx)
			return true
		}
	}
	return false
}

// Top moves selection to the first item.
func (m *List) Top() { m.list.Select(0) }

// Bottom moves selection to the last item.
func (m *List) Bottom() {
	n := len(m.list.Items())
	if n > 0 {
		m.list.Select(n - 1)
	}
}

// Init implements tea.Model.
func (m List) Init() tea.Cmd { return nil }

// Update routes messages to the wrapped bubbles/list. IssuesLoadedMsg is
// intercepted to swap source data; space toggles the current group.
func (m List) Update(msg tea.Msg) (List, tea.Cmd) {
	switch msg := msg.(type) {
	case IssuesLoadedMsg:
		m.issues = msg.Issues
		m.rebuild()
		return m, nil
	case digitJumpTimeoutMsg:
		if msg.gen == m.pendingGen && m.pendingDigit > 0 {
			d := m.pendingDigit
			m.pendingDigit = 0
			m.JumpToIssue(d)
		}
		return m, nil
	case tea.KeyMsg:
		if m.localFilterEditing {
			switch msg.Type {
			case tea.KeyEnter:
				m.localFilterEditing = false
				m.input.Blur()
				m.list.SetHeight(m.height)
				return m, nil
			case tea.KeyEsc:
				m.ClearLocalFilter()
				return m, nil
			case tea.KeyTab, tea.KeyShiftTab:
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.localFilter = m.input.Value()
			m.rebuild()
			return m, cmd
		}
		if m.searchState == searchEditing {
			switch msg.Type {
			case tea.KeyEnter:
				q := strings.TrimSpace(m.input.Value())
				if q == "" {
					return m, nil
				}
				m.searchState = searchCollapsed
				m.searchQuery = q
				m.input.Blur()
				return m, func() tea.Msg { return SearchSubmittedMsg{Query: q} }
			case tea.KeyEsc:
				if m.searchQuery == "" {
					return m, func() tea.Msg { return SearchCancelledMsg{} }
				}
				m.searchState = searchCollapsed
				m.input.Blur()
				return m, nil
			case tea.KeyTab, tea.KeyShiftTab:
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		s := msg.String()
		if len(s) == 1 && s[0] >= '0' && s[0] <= '9' {
			d := int(s[0] - '0')
			n := m.visibleIssueCount()
			if m.pendingDigit > 0 {
				combined := m.pendingDigit*10 + d
				m.pendingDigit = 0
				m.pendingGen++
				m.JumpToIssue(combined)
				return m, nil
			}
			if d == 0 {
				return m, nil
			}
			if n < 10 || d*10 > n {
				m.JumpToIssue(d)
				return m, nil
			}
			m.pendingDigit = d
			m.pendingGen++
			gen := m.pendingGen
			return m, tea.Tick(digitJumpTimeout, func(time.Time) tea.Msg {
				return digitJumpTimeoutMsg{gen: gen}
			})
		}
		if m.pendingDigit > 0 {
			m.pendingDigit = 0
			m.pendingGen++
		}
		switch s {
		case " ":
			m.ToggleSelectedGroup()
			return m, nil
		case "g":
			m.Top()
			return m, nil
		case "G":
			m.Bottom()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the list, optionally prefixed with a search or filter
// header. The local filter takes precedence — search and filter cannot be
// active simultaneously, but if both states are set the in-progress filter
// is what the user is interacting with.
func (m List) View() string {
	if m.localFilterEditing {
		return lipgloss.JoinVertical(lipgloss.Left, m.input.View(), m.list.View())
	}
	if m.localFilter != "" {
		header := m.styles.GroupHeader.Render("▽ " + m.localFilter)
		return lipgloss.JoinVertical(lipgloss.Left, header, m.list.View())
	}
	switch m.searchState {
	case searchEditing:
		return lipgloss.JoinVertical(lipgloss.Left, m.input.View(), m.list.View())
	case searchCollapsed:
		header := m.styles.GroupHeader.Render("🔍 " + m.searchQuery)
		return lipgloss.JoinVertical(lipgloss.Left, header, m.list.View())
	default:
		return m.list.View()
	}
}

// intPow10 returns 10**n for small non-negative n.
func intPow10(n int) int {
	r := 1
	for range n {
		r *= 10
	}
	return r
}

// maxInt returns the larger of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// rebuild projects the current []issues + strategy + collapse map into the
// flat list of bubbles/list items.
// appendTreeRows walks a SectionNode tree and emits flat list items in
// display order. Default-collapses any path it has not seen before. Returns
// the appended slice and the total number of leaf issues across the tree
// (used for the parent section's badge count).
func (m *List) appendTreeRows(items []list.Item, nodes []SectionNode, num *int) ([]list.Item, int) {
	total := 0
	for _, n := range nodes {
		if _, seen := m.collapsed[n.Path]; !seen {
			m.collapsed[n.Path] = true
		}
		issuesUnder := countIssuesIn(n)
		total += issuesUnder
		collapsed := m.collapsed[n.Path]
		items = append(items, listItem{
			GroupKey:  n.Path,
			GroupPath: n.Path,
			Count:     issuesUnder,
			Collapsed: collapsed,
			Depth:     n.Depth,
		})
		if collapsed {
			continue
		}
		if len(n.Children) > 0 {
			var sub int
			items, sub = m.appendTreeRows(items, n.Children, num)
			_ = sub // already counted via issuesUnder
		} else {
			for i := range n.Issues {
				is := n.Issues[i]
				*num++
				items = append(items, listItem{
					Issue:    &is,
					Number:   *num,
					NumWidth: 2,
					Depth:    n.Depth + 1,
				})
			}
		}
	}
	return items, total
}

// countIssuesIn counts leaf issues across the SectionNode subtree.
func countIssuesIn(n SectionNode) int {
	if len(n.Children) == 0 {
		return len(n.Issues)
	}
	total := 0
	for _, c := range n.Children {
		total += countIssuesIn(c)
	}
	return total
}

// applyLocalFilter returns issues filtered by the case-insensitive substring
// in m.localFilter, or the input slice unchanged when the filter is empty.
func (m *List) applyLocalFilter(in []jira.Issue) []jira.Issue {
	if m.localFilter == "" {
		return in
	}
	needle := strings.ToLower(m.localFilter)
	out := make([]jira.Issue, 0, len(in))
	for i := range in {
		hay := strings.ToLower(in[i].Key + " " + in[i].Summary)
		if strings.Contains(hay, needle) {
			out = append(out, in[i])
		}
	}
	return out
}

func (m *List) rebuild() {
	prevSelKey, prevSelGroup := "", ""
	if it, ok := m.list.SelectedItem().(listItem); ok {
		switch {
		case it.isSection():
			// Section rows have no stable key beyond title; let the rebuild
			// pick a fresh selection rather than chase the moving header.
		case it.isGroup():
			prevSelGroup = it.GroupKey
		default:
			prevSelKey = it.Issue.Key
		}
	}

	items := []list.Item{}
	num := 0
	if len(m.sections) > 0 {
		// Sectioned mode: emit a section header, then run grouping over each
		// section's issues. Numbering continues across sections.
		var sectionGroups []grouping.Group
		for _, sec := range m.sections {
			sectionItems := []list.Item{}
			sectionCount := 0
			if len(sec.Tree) > 0 {
				sectionItems, sectionCount = m.appendTreeRows(sectionItems, sec.Tree, &num)
			}
			count := sectionCount
			if len(sec.Tree) == 0 {
				count = len(sec.Issues)
			}
			items = append(items, listItem{
				Section:         true,
				SectionTitle:    sec.Title,
				SectionReadOnly: sec.ReadOnly,
				Count:           count,
			})
			items = append(items, sectionItems...)
			if len(sec.Tree) > 0 {
				continue
			}
			groups := m.strategy.Group(m.applyLocalFilter(sec.Issues))
			grouping.ApplySort(groups, m.sort, m.sortDesc)
			sectionGroups = append(sectionGroups, groups...)
			for _, g := range groups {
				// Sections default to collapsed: structures usually pack many
				// groups, so showing them folded gives an overview first.
				if _, seen := m.collapsed[g.Key]; !seen {
					m.collapsed[g.Key] = true
				}
				collapsed := m.collapsed[g.Key]
				items = append(items, listItem{GroupKey: g.Key, Count: len(g.Issues), Collapsed: collapsed})
				if collapsed {
					continue
				}
				for i := range g.Issues {
					is := g.Issues[i]
					num++
					items = append(items, listItem{Issue: &is, Number: num, NumWidth: 2})
				}
			}
		}
		m.groups = sectionGroups
		m.list.SetItems(items)
	} else {
		src := m.applyLocalFilter(m.issues)
		m.groups = m.strategy.Group(src)
		grouping.ApplySort(m.groups, m.sort, m.sortDesc)
		visibleIssues := 0
		for _, g := range m.groups {
			if !m.collapsed[g.Key] {
				visibleIssues += len(g.Issues)
			}
		}
		numWidth := 1
		if visibleIssues >= 10 {
			numWidth = 2
		}
		items = make([]list.Item, 0, len(m.issues)+len(m.groups))
		for _, g := range m.groups {
			collapsed := m.collapsed[g.Key]
			items = append(items, listItem{
				GroupKey:  g.Key,
				Count:     len(g.Issues),
				Collapsed: collapsed,
			})
			if collapsed {
				continue
			}
			for i := range g.Issues {
				is := g.Issues[i]
				num++
				items = append(items, listItem{Issue: &is, Number: num, NumWidth: numWidth})
			}
		}
		m.list.SetItems(items)
	}

	if prevSelKey != "" || prevSelGroup != "" {
		for idx, raw := range items {
			it := raw.(listItem)
			if prevSelGroup != "" && it.isGroup() && it.GroupKey == prevSelGroup {
				m.list.Select(idx)
				return
			}
			if prevSelKey != "" && !it.isGroup() && !it.isSection() && it.Issue.Key == prevSelKey {
				m.list.Select(idx)
				return
			}
		}
	}
}

// delegate is the bubbles/list ItemDelegate for List. It distinguishes group
// headers from issue rows and renders each through the shared Styles.
type delegate struct {
	styles styles.Styles
}

func (d delegate) Height() int { return 1 }

func (d delegate) Spacing() int { return 0 }

func (d delegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d delegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(listItem)
	if !ok {
		return
	}
	selected := index == m.Index()
	if it.isSection() {
		title := fmt.Sprintf("▼ %s (%d)", it.SectionTitle, it.Count)
		if it.SectionReadOnly {
			title += "  [ro]"
		}
		if budget := m.Width() - 1; budget > 4 && lipgloss.Width(title) > budget {
			title = truncate(title, budget)
		}
		styled := d.styles.SectionHeader.Render(title)
		if selected {
			styled = d.styles.ListItemSelected.Render(title)
		}
		_, _ = fmt.Fprint(w, styled)
		return
	}
	if it.isGroup() {
		caret := "▾"
		if it.Collapsed {
			caret = "▸"
		}
		indent := strings.Repeat("  ", it.Depth)
		line := fmt.Sprintf("%s%s %s  (%d)", indent, caret, it.GroupKey, it.Count)
		// Group label may be a parent epic line "KEY  Long summary" — clamp.
		if budget := m.Width() - 1; budget > 4 && lipgloss.Width(line) > budget {
			line = truncate(line, budget)
		}
		styled := d.styles.GroupHeader.Render(line)
		if selected {
			styled = d.styles.ListItemSelected.Render(line)
		}
		_, _ = fmt.Fprint(w, styled)
		return
	}
	icon := grouping.PriorityIcon(it.Issue.Priority.Name)
	numW := max(it.NumWidth, 1)
	numStr := ""
	if it.Number > 0 && it.Number <= intPow10(numW)-1 {
		numStr = fmt.Sprintf("%*d", numW, it.Number)
	} else {
		numStr = strings.Repeat(" ", numW)
	}
	prefix := fmt.Sprintf("%s  %s %-10s %s ", strings.Repeat("  ", it.Depth), numStr, it.Issue.Key, icon)
	// Pane width minus the prefix; clamp to a sane minimum so very narrow
	// terminals still render *something* readable rather than just an
	// ellipsis.
	budget := max(m.Width()-lipgloss.Width(prefix)-1, 8)
	line := prefix + truncate(it.Issue.Summary, budget)
	styled := d.styles.ListItem.Render(line)
	if selected {
		styled = d.styles.ListItemSelected.Render(line)
	}
	_, _ = fmt.Fprint(w, styled)
}

// ListItemForTest is an alias used by external test files that need to
// inspect rendered rows.
type ListItemForTest = listItem

// truncate shortens s to at most n display runes, ending with an ellipsis
// when truncation occurred.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
