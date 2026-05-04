package panes_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	m.Run()
}

func newList(t *testing.T) panes.List {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	return panes.New(styles.New(p), grouping.ByStatus{}, 60, 20)
}

func sampleIssues() []jira.Issue {
	return []jira.Issue{
		{Key: "PROJ-1", Summary: "Fix login redirect on Safari",
			Status:   jira.Status{Name: "To Do", Category: "new"},
			Priority: jira.Priority{Name: "High"}},
		{Key: "PROJ-2", Summary: "Refactor auth middleware",
			Status:   jira.Status{Name: "In Progress", Category: "indeterminate"},
			Priority: jira.Priority{Name: "Medium"}},
		{Key: "PROJ-3", Summary: "Add metrics endpoint",
			Status:   jira.Status{Name: "To Do", Category: "new"},
			Priority: jira.Priority{Name: "Low"}},
	}
}

func TestNew_StartsEmpty(t *testing.T) {
	l := newList(t)
	if l.Selected() != nil {
		t.Errorf("empty list selected = %v, want nil", l.Selected())
	}
	if got := l.Strategy().Name(); got != "status" {
		t.Errorf("default strategy = %q, want status", got)
	}
}

func TestNew_NilStrategyDefaultsToByStatus(t *testing.T) {
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	l := panes.New(styles.New(p), nil, 60, 20)
	if l.Strategy().Name() != "status" {
		t.Errorf("nil strategy → %q, want status", l.Strategy().Name())
	}
}

func TestSetIssues_BuildsGroups(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())
	groups := l.Groups()
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0].Key != "To Do" || groups[1].Key != "In Progress" {
		t.Errorf("group order = %s,%s; want To Do,In Progress",
			groups[0].Key, groups[1].Key)
	}

	out := stripANSI(l.View())
	for _, want := range []string{"To Do", "(2)", "PROJ-1", "PROJ-3", "In Progress", "(1)", "PROJ-2"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n%s", want, out)
		}
	}
}

func TestNavigate_GroupHeaderThenIssue(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())

	// First selectable row is the first group header.
	if got := l.SelectedGroupKey(); got != "To Do" {
		t.Errorf("initial selection = %q, want group header To Do", got)
	}
	if l.Selected() != nil {
		t.Errorf("group header selected, but Selected() returned issue %v", l.Selected())
	}

	// Move down once → first issue in group.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got := l.Selected()
	if got == nil {
		t.Fatal("after j: Selected() = nil, want issue")
	}
	if got.Key != "PROJ-1" {
		t.Errorf("after j: selected = %q, want PROJ-1", got.Key)
	}
}

func TestSpace_TogglesGroupCollapse(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())

	// Cursor is on the first group header. Space → collapse.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !l.IsCollapsed("To Do") {
		t.Error("space did not collapse To Do group")
	}
	out := stripANSI(l.View())
	if strings.Contains(out, "PROJ-1") || strings.Contains(out, "PROJ-3") {
		t.Errorf("collapsed group still shows issues:\n%s", out)
	}
	if !strings.Contains(out, "▸ To Do") {
		t.Errorf("expected ▸ caret on collapsed group, got:\n%s", out)
	}

	// Space again → expand.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if l.IsCollapsed("To Do") {
		t.Error("second space did not re-expand")
	}
	out = stripANSI(l.View())
	if !strings.Contains(out, "PROJ-1") {
		t.Errorf("re-expanded group missing PROJ-1:\n%s", out)
	}
}

func TestG_Top_GG_Bottom(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())

	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if got := l.Selected(); got == nil || got.Key != "PROJ-2" {
		// In Progress group, sole issue, is at the bottom.
		t.Errorf("G: selected = %v, want PROJ-2", got)
	}
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if l.SelectedGroupKey() != "To Do" {
		t.Errorf("g: selected group = %q, want To Do", l.SelectedGroupKey())
	}
}

func TestSetStrategy_ClearsCollapseAndRegroups(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())

	// Collapse To Do, then switch strategy.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !l.IsCollapsed("To Do") {
		t.Fatal("precondition: To Do should be collapsed")
	}
	l.SetStrategy(grouping.ByPriority{})
	if l.IsCollapsed("To Do") {
		t.Error("collapse state survived strategy switch")
	}
	groups := l.Groups()
	wantOrder := []string{"High", "Medium", "Low"}
	for i, g := range groups {
		if g.Key != wantOrder[i] {
			t.Errorf("priority group[%d] = %q, want %q", i, g.Key, wantOrder[i])
		}
	}
}

func TestIssuesLoadedMsg_Replaces(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())

	newIssues := []jira.Issue{
		{Key: "X-9", Summary: "New thing",
			Status: jira.Status{Name: "Done", Category: "done"}},
	}
	l, _ = l.Update(panes.IssuesLoadedMsg{Issues: newIssues})

	out := stripANSI(l.View())
	if !strings.Contains(out, "X-9") {
		t.Errorf("after IssuesLoadedMsg view missing X-9:\n%s", out)
	}
	if strings.Contains(out, "PROJ-1") {
		t.Errorf("stale issue still present:\n%s", out)
	}
}

func TestRebuild_PreservesIssueSelection(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())
	// Move cursor to PROJ-1.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := l.Selected(); got == nil || got.Key != "PROJ-1" {
		t.Fatalf("precondition: selected = %v, want PROJ-1", got)
	}
	// Re-set with the same issues; selection should stick.
	l.SetIssues(sampleIssues())
	if got := l.Selected(); got == nil || got.Key != "PROJ-1" {
		t.Errorf("after rebuild: selected = %v, want PROJ-1", got)
	}
}

// teatest end-to-end — boot a wrapper model containing the List, navigate,
// and check the rendered frame reflects the selection.
type listWrap struct{ l panes.List }

func (w listWrap) Init() tea.Cmd { return w.l.Init() }
func (w listWrap) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	w.l, cmd = w.l.Update(msg)
	return w, cmd
}
func (w listWrap) View() string { return w.l.View() }

func TestTeatest_NavigationGolden(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())

	tm := teatest.NewTestModel(t, listWrap{l: l},
		teatest.WithInitialTermSize(60, 20))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("To Do")) && bytes.Contains(b, []byte("PROJ-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Move down twice: header → PROJ-1 → PROJ-3.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestList_NumberJumpsToNthIssue(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())
	// 1 → first visible issue (PROJ-1, skipping the "To Do" header).
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if got := l.Selected(); got == nil || got.Key != "PROJ-1" {
		t.Fatalf("after 1: selected = %v, want PROJ-1", got)
	}
	// 2 → second visible issue (PROJ-3).
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if got := l.Selected(); got == nil || got.Key != "PROJ-3" {
		t.Fatalf("after 2: selected = %v, want PROJ-3", got)
	}
	// 3 → third visible issue (PROJ-2 in In Progress group, header skipped).
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if got := l.Selected(); got == nil || got.Key != "PROJ-2" {
		t.Fatalf("after 3: selected = %v, want PROJ-2", got)
	}
	// 9 → out of range, selection unchanged.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	if got := l.Selected(); got == nil || got.Key != "PROJ-2" {
		t.Fatalf("after 9 (out of range): selected = %v, want PROJ-2", got)
	}
}

func TestList_NumberKeyInertWhileSearchEditing(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())
	l.SetSearchEditing("")
	// While editing, '2' should be typed into the input, not jump selection.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if got := l.Selected(); got != nil {
		t.Fatalf("number key during search edit moved selection to %v", got)
	}
}

func TestList_SearchEditingShowsInput(t *testing.T) {
	l := newList(t)
	l.SetSearchEditing("")
	out := stripANSI(l.View())
	if !strings.Contains(out, "Type JQL or text") {
		t.Fatalf("editing placeholder missing:\n%s", out)
	}
}

func TestList_SearchEnterEmitsSubmittedMsg(t *testing.T) {
	l := newList(t)
	l.SetSearchEditing("")
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")})
	l, cmd := l.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(panes.SearchSubmittedMsg)
	if !ok {
		t.Fatalf("type: %T", cmd())
	}
	if msg.Query != "foo" {
		t.Fatalf("query: %q", msg.Query)
	}
}

func TestList_SearchEscEmptyEmitsCancelled(t *testing.T) {
	l := newList(t)
	l.SetSearchEditing("")
	_, cmd := l.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(panes.SearchCancelledMsg); !ok {
		t.Fatalf("type: %T", cmd())
	}
}

func TestList_LocalFilterNarrowsRows(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())
	l.BeginLocalFilter()
	// Type "auth" — should match only PROJ-2 (Refactor auth middleware).
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("auth")})
	if l.LocalFilter() != "auth" {
		t.Fatalf("LocalFilter = %q", l.LocalFilter())
	}
	groups := l.Groups()
	count := 0
	for _, g := range groups {
		count += len(g.Issues)
	}
	if count != 1 {
		t.Fatalf("filtered issues = %d, want 1", count)
	}
	// Esc clears.
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if l.LocalFilter() != "" {
		t.Fatalf("LocalFilter not cleared: %q", l.LocalFilter())
	}
}

func TestList_LocalFilterCaseInsensitive(t *testing.T) {
	l := newList(t)
	l.SetIssues(sampleIssues())
	l.BeginLocalFilter()
	l, _ = l.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("SAFARI")})
	count := 0
	for _, g := range l.Groups() {
		count += len(g.Issues)
	}
	if count != 1 {
		t.Fatalf("case-insensitive match count = %d, want 1", count)
	}
}

func TestList_SearchCollapsedShowsQuery(t *testing.T) {
	l := newList(t)
	l.SetSearchCollapsed("hello")
	out := stripANSI(l.View())
	if !strings.Contains(out, "🔍 hello") {
		t.Fatalf("collapsed header missing:\n%s", out)
	}
}

func TestList_SetSortReordersWithinGroup(t *testing.T) {
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	s := styles.New(p)
	l := panes.New(s, grouping.ByStatus{}, 80, 24)
	l.SetIssues([]jira.Issue{
		{Key: "P-2", Summary: "b", Status: jira.Status{Name: "Open"}},
		{Key: "P-10", Summary: "c", Status: jira.Status{Name: "Open"}},
		{Key: "P-1", Summary: "a", Status: jira.Status{Name: "Open"}},
	})
	l.SetSort(grouping.ByKeySort{}, false)

	items := []string{}
	for _, raw := range l.Items() {
		if it, ok := raw.(panes.ListItemForTest); ok && it.Issue != nil {
			items = append(items, it.Issue.Key)
		}
	}
	want := []string{"P-1", "P-2", "P-10"}
	for i := range want {
		if i >= len(items) || items[i] != want[i] {
			t.Fatalf("after SetSort asc: got %v, want %v", items, want)
		}
	}
}

// stripANSI removes CSI escape sequences so assertions can match plain text.
func stripANSI(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	in := []byte(s)
	for i := 0; i < len(in); i++ {
		if in[i] == 0x1b && i+1 < len(in) && in[i+1] == '[' {
			j := i + 2
			for j < len(in) {
				c := in[j]
				if (c >= 0x40 && c <= 0x7e) || c == 'm' {
					j++
					break
				}
				j++
			}
			i = j - 1
			continue
		}
		out.WriteByte(in[i])
	}
	return out.String()
}
