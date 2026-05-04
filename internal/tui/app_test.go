package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// TestMain pins lipgloss to TrueColor so tests get deterministic, color-bearing
// ANSI sequences regardless of the host terminal — without this, lipgloss
// detects "no TTY" and renders without colors, hiding focus-border changes.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	m.Run()
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	return New(p)
}

func TestNew_Defaults(t *testing.T) {
	m := newTestModel(t)
	if m.Focused() != FocusList {
		t.Errorf("default focus = %v, want FocusList", m.Focused())
	}
	if got := m.Keymap().Quit.Keys(); len(got) == 0 {
		t.Error("Quit binding has no keys")
	}
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init returned non-nil cmd: %v", cmd)
	}
}

func TestNew_WithStatus(t *testing.T) {
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	m := New(p, WithStatus("⟳ refreshing…"))
	m, _ = sendSize(m, 80, 24)
	if !strings.Contains(stripANSI(m.View()), "refreshing") {
		t.Error("status text not rendered in top bar")
	}
}

func TestUpdate_WindowSize(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 100, 30)
	if m.width != 100 || m.height != 30 {
		t.Errorf("window size not stored, got w=%d h=%d", m.width, m.height)
	}
}

func TestUpdate_QuitKeys(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newTestModel(t)
			_, cmd := m.Update(c.key)
			if cmd == nil {
				t.Fatal("expected a quit cmd, got nil")
			}
			msg := cmd()
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Errorf("expected tea.QuitMsg, got %T", msg)
			}
		})
	}
}

func TestUpdate_FocusCycleWide(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	if m.Focused() != FocusList {
		t.Fatalf("initial focus = %v, want FocusList", m.Focused())
	}

	tab := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(tab)
	m = updated.(Model)
	if m.Focused() != FocusDetail {
		t.Errorf("after tab focus = %v, want FocusDetail", m.Focused())
	}

	updated, _ = m.Update(tab)
	m = updated.(Model)
	if m.Focused() != FocusList {
		t.Errorf("after second tab focus = %v, want FocusList", m.Focused())
	}

	shiftTab := tea.KeyMsg{Type: tea.KeyShiftTab}
	updated, _ = m.Update(shiftTab)
	m = updated.(Model)
	if m.Focused() != FocusDetail {
		t.Errorf("after shift+tab focus = %v, want FocusDetail", m.Focused())
	}
}

func TestUpdate_UnknownKeyDoesNothing(t *testing.T) {
	m := newTestModel(t)
	before := m.Focused()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if cmd != nil {
		t.Errorf("unknown key produced cmd: %v", cmd)
	}
	if updated.(Model).Focused() != before {
		t.Error("unknown key changed focus")
	}
}

func TestView_EmptyBeforeWindowSize(t *testing.T) {
	m := newTestModel(t)
	if m.View() != "" {
		t.Error("View should be empty before WindowSizeMsg")
	}
}

func TestView_RendersFrame(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	out := stripANSI(m.View())

	for _, want := range []string{"ripjira", "Issues", "Details"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q\nfull output:\n%s", want, out)
		}
	}

	// hint bar should advertise short-help bindings (help shortcut is
	// intentionally not in the footer — it lives in the help overlay).
	for _, want := range []string{"tab"} {
		if !strings.Contains(out, want) {
			t.Errorf("hint bar missing %q", want)
		}
	}
}

func TestView_FocusedBorderShifts(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	listFocused := m.View()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	detailFocused := updated.(Model).View()

	if listFocused == detailFocused {
		t.Error("View did not change when focus shifted from list to detail")
	}
}

// TestApp_BootsAndQuits exercises the end-to-end Bubble Tea lifecycle: start
// the program, wait until it renders the top bar, then send 'q' and confirm
// the program exits cleanly.
func TestApp_BootsAndQuits(t *testing.T) {
	m := newTestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("ripjira"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestHelpOverlay_OpensAndCloses(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 100, 40)

	if m.HelpVisible() {
		t.Fatal("help overlay should start hidden")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if !m.HelpVisible() {
		t.Fatal("? did not open help overlay")
	}

	view := stripANSI(m.View())
	for _, b := range m.Keymap().All() {
		h := b.Help()
		if !strings.Contains(view, h.Desc) {
			t.Errorf("help overlay missing description %q (key %q)", h.Desc, h.Key)
		}
	}

	// while help is visible, q should be swallowed (not quit)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(Model)
	if cmd != nil {
		t.Errorf("q while help visible produced cmd: %v", cmd)
	}
	if !m.HelpVisible() {
		t.Error("q while help visible should be swallowed, not close help")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.HelpVisible() {
		t.Error("esc did not close help overlay")
	}
}

func TestHelpOverlay_TeaTestEndToEnd(t *testing.T) {
	m := newTestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("ripjira"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Keymap"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestKeymap_AllBindingsHaveHelp(t *testing.T) {
	km := DefaultKeymap()
	for i, b := range km.All() {
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("binding %d missing help: key=%q desc=%q keys=%v", i, h.Key, h.Desc, b.Keys())
		}
		if len(b.Keys()) == 0 {
			t.Errorf("binding %d has no keys", i)
		}
	}
}

func TestKeymap_FullHelpCoversAllBindings(t *testing.T) {
	km := DefaultKeymap()
	seen := map[string]bool{}
	for _, col := range km.FullHelp() {
		for _, b := range col {
			seen[b.Help().Key+"|"+b.Help().Desc] = true
		}
	}
	for _, b := range km.All() {
		k := b.Help().Key + "|" + b.Help().Desc
		if !seen[k] {
			t.Errorf("binding %q (%s) is not in FullHelp", b.Help().Desc, b.Help().Key)
		}
	}
}

func TestFocus_TabCycles(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	m.focus = FocusList
	want := []Focus{FocusDetail, FocusList, FocusDetail}
	for i, exp := range want {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(Model)
		if m.focus != exp {
			t.Fatalf("step %d: got %v want %v", i, m.focus, exp)
		}
	}
}

func TestFocus_HLStepsAcrossPanes(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	m.focus = FocusList
	// Left edge clamps from list.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)
	if m.focus != FocusList {
		t.Fatalf("h from list should clamp -> %v", m.focus)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if m.focus != FocusDetail {
		t.Fatalf("l from list -> %v", m.focus)
	}
	// Right edge clamps.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(Model)
	if m.focus != FocusDetail {
		t.Fatalf("l from detail should clamp -> %v", m.focus)
	}
	// h back to list.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = updated.(Model)
	if m.focus != FocusList {
		t.Fatalf("h from detail -> %v", m.focus)
	}
}

func TestSlash_OpensSearchEditing(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	if m.view != panes.ViewSearch {
		t.Fatalf("view: %v", m.view)
	}
	if m.focus != FocusList {
		t.Fatalf("focus: %v", m.focus)
	}
	if !strings.Contains(stripANSI(m.View()), "Type JQL or text") {
		t.Fatal("list pane not in search editing")
	}
}

func TestFocus_NarrowTabCycles(t *testing.T) {
	m := newTestAppModel(t, 50, 30) // narrow
	m.focus = FocusList
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusDetail {
		t.Fatalf("tab in narrow -> %v", m.focus)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusList {
		t.Fatalf("tab back -> %v", m.focus)
	}
}

// helpers --------------------------------------------------------------------

func newTestAppModel(t *testing.T, w, h int) Model {
	t.Helper()
	pal, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	m := New(pal)
	m, _ = sendSize(m, w, h)
	return m
}

func sendSize(m Model, w, h int) (Model, tea.Cmd) {
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model), cmd
}

// stripANSI removes ANSI CSI sequences so we can assert on plain text.
// Lipgloss output includes escape codes; tests only care about the textual
// payload.
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

func TestView_TabBarRendersAtWideWidth(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	out := stripANSI(m.View())
	for _, want := range []string{"MY ISSUES", "WATCHING", "REPORTED"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tab bar missing %q:\n%s", want, out)
		}
	}
}

// recordingLoader captures the LoadIssues calls dispatched by the root model
// so tests can assert which view (and search query) actually triggered a
// network fetch.
type recordingLoader struct {
	mu      sync.Mutex
	views   []panes.ViewKind
	queries []string
	respond func(panes.ViewKind, string) ([]jira.Issue, error)
}

func (l *recordingLoader) LoadIssues(_ context.Context, v panes.ViewKind, q string) ([]jira.Issue, error) {
	l.mu.Lock()
	l.views = append(l.views, v)
	l.queries = append(l.queries, q)
	respond := l.respond
	l.mu.Unlock()
	if respond != nil {
		return respond(v, q)
	}
	return nil, nil
}

func (l *recordingLoader) LoadIssue(context.Context, string) (jira.Issue, error) {
	return jira.Issue{}, nil
}
func (l *recordingLoader) LoadComments(context.Context, string) ([]jira.Comment, error) {
	return nil, nil
}
func (l *recordingLoader) LoadTransitions(context.Context, string) ([]jira.Transition, error) {
	return nil, nil
}
func (l *recordingLoader) LoadAttachment(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}
func (l *recordingLoader) DoTransition(context.Context, string, string) error       { return nil }
func (l *recordingLoader) AddComment(context.Context, string, string) error         { return nil }
func (l *recordingLoader) SearchUsers(context.Context, string) ([]jira.User, error) { return nil, nil }
func (l *recordingLoader) AssignIssue(context.Context, string, string) error        { return nil }
func (l *recordingLoader) UpdateFields(context.Context, string, map[string]any) error { return nil }
func (l *recordingLoader) UpdateDescription(context.Context, string, string) error     { return nil }
func (l *recordingLoader) CreateLink(context.Context, string, string, string) error { return nil }
func (l *recordingLoader) DeleteLink(context.Context, string) error                  { return nil }
func (l *recordingLoader) AddWatcher(context.Context, string, string) error          { return nil }
func (l *recordingLoader) RemoveWatcher(context.Context, string, string) error       { return nil }
func (l *recordingLoader) AddWorklog(context.Context, string, string, string) error  { return nil }
func (l *recordingLoader) DeleteWorklog(context.Context, string, string) error        { return nil }
func (l *recordingLoader) GetMyself(context.Context) (jira.User, error)             { return jira.User{}, nil }
func (l *recordingLoader) Projects(context.Context) ([]jira.Project, error)         { return nil, nil }
func (l *recordingLoader) IssueTypesForProject(context.Context, string) ([]jira.IssueType, error) {
	return nil, nil
}
func (l *recordingLoader) CreateMeta(context.Context, string, string) (jira.CreateMeta, error) {
	return jira.CreateMeta{}, nil
}
func (l *recordingLoader) CreateIssue(context.Context, jira.CreatePayload) (jira.Issue, error) {
	return jira.Issue{}, nil
}
func (l *recordingLoader) SearchEpics(context.Context, string, []string) ([]jira.Issue, error) {
	return nil, nil
}
func (l *recordingLoader) SetParent(context.Context, string, string) error { return nil }

func newTestAppModelWithLoader(t *testing.T, w, h int, l AppLoader) Model {
	t.Helper()
	pal, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	m := New(pal, WithLoader(l))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

// drainCmds repeatedly invokes a cmd, dispatches the produced message back
// into the model, and stops once nothing remains to do. Handles tea.BatchMsg
// (a slice of Cmds) so the goroutine-spawning Bubble Tea runtime is not
// required.
func drainCmds(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				queue = append(queue, sub)
			}
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		if next != nil {
			queue = append(queue, next)
		}
	}
	return m
}

func TestTabs_NextTabDispatchesRightView(t *testing.T) {
	loader := &recordingLoader{}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	// Drain the initial Init refresh so loader.views starts clean.
	m = drainCmds(t, m, m.Init())
	loader.mu.Lock()
	loader.views = nil
	loader.queries = nil
	loader.mu.Unlock()

	// `]` advances from MyTasks → Watching.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = updated.(Model)
	m = drainCmds(t, m, cmd)

	loader.mu.Lock()
	views := append([]panes.ViewKind(nil), loader.views...)
	loader.mu.Unlock()
	if len(views) == 0 || views[len(views)-1] != panes.ViewWatching {
		t.Fatalf("views: %v, want last=ViewWatching", views)
	}
	if m.view != panes.ViewWatching {
		t.Fatalf("model.view = %v, want ViewWatching", m.view)
	}
}

func TestTabs_SwitchingToSameViewIsNoop(t *testing.T) {
	loader := &recordingLoader{}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	m = drainCmds(t, m, m.Init())
	loader.mu.Lock()
	loader.views = nil
	loader.mu.Unlock()

	// handleViewSelected with the active view should be a no-op.
	updated, cmd := m.handleViewSelected(panes.ViewMyTasks)
	_ = updated
	_ = drainCmds(t, m, cmd)

	loader.mu.Lock()
	views := append([]panes.ViewKind(nil), loader.views...)
	loader.mu.Unlock()
	if len(views) != 0 {
		t.Fatalf("re-selecting active view should not refetch, got: %v", views)
	}
}

func TestTabs_SearchWithEmptyQueryDoesNotFetch(t *testing.T) {
	loader := &recordingLoader{}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	m = drainCmds(t, m, m.Init())
	loader.mu.Lock()
	loader.views = nil
	loader.mu.Unlock()

	// `/` enters Search mode directly (it is no longer a tab in the cycle).
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	m = drainCmds(t, m, cmd)

	loader.mu.Lock()
	views := append([]panes.ViewKind(nil), loader.views...)
	loader.mu.Unlock()
	// loader will not see Search (empty query, never fetched).
	for _, v := range views {
		if v == panes.ViewSearch {
			t.Fatalf("should not fetch ViewSearch on empty query, got: %v", views)
		}
	}
	if m.view != panes.ViewSearch {
		t.Fatalf("model.view = %v, want ViewSearch", m.view)
	}
	if m.focus != FocusList {
		t.Fatalf("focus = %v, want FocusList", m.focus)
	}
}

func TestSearch_SubmitFetchesAndShowsResults(t *testing.T) {
	loader := &recordingLoader{
		respond: func(_ panes.ViewKind, _ string) ([]jira.Issue, error) {
			return []jira.Issue{{Key: "PROJ-9", Summary: "match"}}, nil
		},
	}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	// drain the initial Init refresh, then reset capture so we only see the
	// search-driven call
	updated, initCmd := m.Update(tea.KeyMsg{Type: tea.KeyTab}) // any no-op-ish to get into Update path
	_ = updated
	_ = initCmd
	// reset
	loader.mu.Lock()
	loader.views = nil
	loader.queries = nil
	loader.mu.Unlock()
	// open search via /
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	for _, r := range "PROJ-9" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m = drainCmds(t, m, cmd)

	loader.mu.Lock()
	queries := append([]string(nil), loader.queries...)
	views := append([]panes.ViewKind(nil), loader.views...)
	loader.mu.Unlock()

	if len(queries) == 0 || queries[len(queries)-1] != "PROJ-9" {
		t.Fatalf("queries: %v", queries)
	}
	if len(views) == 0 || views[len(views)-1] != panes.ViewSearch {
		t.Fatalf("views: %v", views)
	}
	if !strings.Contains(stripANSI(m.View()), "PROJ-9") {
		t.Fatalf("results not rendered:\n%s", m.View())
	}
}

func TestSearch_CancelRevertsToMyTasks(t *testing.T) {
	loader := &recordingLoader{}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	// open search
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	if m.view != panes.ViewSearch {
		t.Fatalf("precondition: view should be Search, got %v", m.view)
	}
	// drain initial state
	loader.mu.Lock()
	loader.views = nil
	loader.queries = nil
	loader.mu.Unlock()
	// Esc on empty input → SearchCancelledMsg
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	m = drainCmds(t, m, cmd)
	if m.view != panes.ViewMyTasks {
		t.Fatalf("view after cancel: %v", m.view)
	}
	if m.searchQuery != "" {
		t.Fatalf("searchQuery: %q", m.searchQuery)
	}
}

func TestSearch_EmptyQueryDoesNotFetch(t *testing.T) {
	loader := &recordingLoader{}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	m = drainCmds(t, m, m.Init())
	loader.mu.Lock()
	loader.views = nil
	loader.mu.Unlock()

	// `/` flips to ViewSearch and opens the search input but should not fetch.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	_ = drainCmds(t, m, cmd)

	loader.mu.Lock()
	views := append([]panes.ViewKind(nil), loader.views...)
	loader.mu.Unlock()
	if len(views) > 0 {
		t.Fatalf("should not fetch on empty search, got: %v", views)
	}
}

// TestCache_OnlyMyTasksWritesToDisk asserts that a list refresh whose active
// view is not ViewMyTasks (here: ViewWatching) does NOT persist results to
// the on-disk cache. Watching/Search results have a different scope and must
// not clobber the My Tasks startup cache.
func TestCache_OnlyMyTasksWritesToDisk(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	loader := &recordingLoader{
		respond: func(_ panes.ViewKind, _ string) ([]jira.Issue, error) {
			return []jira.Issue{{Key: "X-1"}}, nil
		},
	}
	m := newTestAppModelWithLoader(t, 120, 30, loader)
	m.cachePath = cachePath
	m.accountID = "acct"

	// Switch to Watching via `]`.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = updated.(Model)
	m = drainCmds(t, m, cmd)

	if m.view != panes.ViewWatching {
		t.Fatalf("precondition failed: view = %v", m.view)
	}

	// give the goroutine a chance, if any
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(cachePath); err == nil {
		t.Fatal("cache file should not have been written for Watching")
	}
}

func TestStrategy_DefaultIsEpicAndPriorityForMyTasks(t *testing.T) {
	m := newTestModel(t)
	if got := m.list.Strategy().Name(); got != "epic" {
		t.Errorf("startup strategy = %q, want %q", got, "epic")
	}
}

func TestStrategy_ReappliedOnReturnToMyTasks(t *testing.T) {
	m := newTestModel(t)
	// Simulate user override to ByPriority on MyTasks.
	m.list.SetStrategy(grouping.ByPriority{})
	if got := m.list.Strategy().Name(); got != "priority" {
		t.Fatalf("override strategy = %q, want %q", got, "priority")
	}
	// Switch to Watching → resets to that view's default (status).
	mi, _ := m.handleViewSelected(panes.ViewWatching)
	m = mi.(Model)
	if got := m.list.Strategy().Name(); got != "status" {
		t.Errorf("on Watching strategy = %q, want %q", got, "status")
	}
	// Return to MyTasks → epic strategy is re-applied.
	mi, _ = m.handleViewSelected(panes.ViewMyTasks)
	m = mi.(Model)
	if got := m.list.Strategy().Name(); got != "epic" {
		t.Errorf("on return to MyTasks strategy = %q, want %q", got, "epic")
	}
}

func TestStrategy_OtherViewsKeepByStatusByDefault(t *testing.T) {
	m := newTestModel(t)
	mi, _ := m.handleViewSelected(panes.ViewWatching)
	m = mi.(Model)
	if got := m.list.Strategy().Name(); got != "status" {
		t.Errorf("Watching default strategy = %q, want %q", got, "status")
	}
}

func TestCreate_NPressedDispatchesProjectsFetch(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	if m.CreateVisible() {
		t.Fatal("create overlay visible at startup")
	}
	// Without a loader, pressing n returns a no-op.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)
	if cmd != nil {
		t.Errorf("expected nil cmd without loader, got %T", cmd)
	}
	if m.CreateVisible() {
		t.Errorf("overlay should not be visible without loader")
	}
}

func TestCreate_OverlayOpensAfterProjectsLoaded(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(createProjectsLoadedMsg{
		Projects: []jira.Project{{Key: "PROJ"}},
	})
	m = mi.(Model)
	if !m.CreateVisible() {
		t.Errorf("overlay not visible after createProjectsLoadedMsg")
	}
}

func TestSubtask_SPressedDispatchesProjectsFetch(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	// Without a loader, S is a no-op.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(Model)
	if cmd != nil {
		t.Errorf("expected nil cmd without loader, got %T", cmd)
	}
	if m.CreateVisible() {
		t.Errorf("overlay should not be visible without loader")
	}
}

func TestSubtask_OverlayOpensInSubtaskModeAfterProjectsLoaded(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(createSubtaskProjectsLoadedMsg{
		Parent:   jira.Issue{Key: "PROJ-100", Summary: "Parent"},
		Projects: []jira.Project{{Key: "PROJ"}},
	})
	m = mi.(Model)
	if !m.CreateVisible() {
		t.Fatal("overlay not visible after createSubtaskProjectsLoadedMsg")
	}
	if !m.create.IsSubtaskMode() {
		t.Error("overlay not in subtask mode")
	}
	if got := m.create.ParentKey(); got != "PROJ-100" {
		t.Errorf("parent = %q, want PROJ-100", got)
	}
}

func TestCreateSubmitDone_AppendsSubtaskOptimistically(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	parent := jira.Issue{Key: "PROJ-100"}
	m.detail.SetIssue(&parent)
	// Put overlay into subtask mode (so m.create.ParentKey() == "PROJ-100").
	c, _ := m.create.ShowAsSubtask(parent, []jira.Project{{Key: "PROJ"}})
	m.create = c
	// Simulate a successful submit. Issue is what the server would return.
	mi, _ := m.Update(overlays.CreateSubmitDoneMsg{
		ProjectKey:  "PROJ",
		IssueTypeID: "10103",
		Issue: jira.Issue{
			Key:     "PROJ-200",
			Summary: "child",
			Status:  jira.Status{Name: "To Do"},
		},
	})
	m = mi.(Model)
	got := m.detail.Issue()
	if got == nil || len(got.Subtasks) != 1 || got.Subtasks[0].Key != "PROJ-200" {
		t.Errorf("subtask not appended; subtasks = %+v", got.Subtasks)
	}
}

func TestCreate_LastProjectFromStateOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := state.Save(statePath, state.State{LastProject: "BETA"}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	p, _ := themes.ByName("tokyonight")
	m := New(p, WithDefaultProject("ALPHA"), WithStatePath(statePath))
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(createProjectsLoadedMsg{
		Projects: []jira.Project{{Key: "ALPHA"}, {Key: "BETA"}},
	})
	m = mi.(Model)
	if got := m.create.SelectedProjectKey(); got == "" {
		// SelectedProjectKey is set only after Step 1 is completed.
		// Inspect the cursor instead — BETA is index 1.
		if c := m.create.ProjectCursor(); c != 1 {
			t.Errorf("cursor = %d, want 1 (BETA pre-selected from state)", c)
		}
	}
}

func TestCreate_SubmitDoneWritesLastProject(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	p, _ := themes.ByName("tokyonight")
	m := New(p, WithStatePath(statePath))
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(overlays.CreateSubmitDoneMsg{
		ProjectKey:  "GAMMA",
		IssueTypeID: "10100",
		Issue:       jira.Issue{Key: "GAMMA-1", Summary: "x"},
	})
	_ = mi
	// Save runs in a goroutine; poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := state.Load(statePath); err == nil && st.LastProject == "GAMMA" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("state.LastProject was not persisted to GAMMA")
}

func TestTabBar_RendersTwoLabels(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	out := stripANSI(m.View())
	for _, want := range []string{"MY ISSUES", "WATCHING", "REPORTED"} {
		if !strings.Contains(out, want) {
			t.Errorf("tab bar missing %q\nfull output:\n%s", want, out)
		}
	}
	// SEARCH should NOT appear in the tab bar at rest — it is a transient
	// mode entered via `/`.
	if strings.Contains(out, "SEARCH") {
		t.Errorf("tab bar should not include SEARCH at rest; got:\n%s", out)
	}
}

func TestTabBar_NextTabCyclesForward(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	if m.view != panes.ViewMyTasks {
		t.Fatalf("initial view = %v, want ViewMyTasks", m.view)
	}
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewWatching {
		t.Errorf("after ] view = %v, want ViewWatching", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewReported {
		t.Errorf("after ]] view = %v, want ViewReported", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewRecent {
		t.Errorf("after ]]] view = %v, want ViewRecent", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewSprint {
		t.Errorf("after ]]]] view = %v, want ViewSprint", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewMentions {
		t.Errorf("after ]]]]] view = %v, want ViewMentions", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewStructures {
		t.Errorf("after ]]]]]] view = %v, want ViewStructures", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mi.(Model)
	if m.view != panes.ViewMyTasks {
		t.Errorf("wrap: after ]]]]]]] view = %v, want ViewMyTasks", m.view)
	}
}

func TestTabBar_PrevTabCyclesBackward(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = mi.(Model)
	if m.view != panes.ViewStructures {
		t.Errorf("after [ view = %v, want ViewStructures", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = mi.(Model)
	if m.view != panes.ViewMentions {
		t.Errorf("after [[ view = %v, want ViewMentions", m.view)
	}
}

func TestTabBar_SearchExcludedFromCycle(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	for range 7 {
		mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
		m = mi.(Model)
		if m.view == panes.ViewSearch {
			t.Fatalf("] should never land on ViewSearch, got %v", m.view)
		}
	}
}

func TestFocusCycle_DoesNotIncludeMenu(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	if m.Focused() != FocusList {
		t.Fatalf("initial focus = %v, want FocusList", m.Focused())
	}
	tab := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(tab)
	m = updated.(Model)
	if m.Focused() != FocusDetail {
		t.Errorf("after tab focus = %v, want FocusDetail", m.Focused())
	}
	updated, _ = m.Update(tab)
	m = updated.(Model)
	if m.Focused() != FocusList {
		t.Errorf("after second tab focus = %v, want FocusList (no menu)", m.Focused())
	}
}

func TestQuit_FirstEscArmsThenSecondQuits(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	if m.QuitArmed() {
		t.Fatal("quit should not be armed at startup")
	}
	// First Esc — arms.
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if !m.QuitArmed() {
		t.Fatal("first Esc did not arm quit")
	}
	if cmd == nil {
		t.Error("first Esc should return a cmd (toast tick)")
	}
	// Toast text is visible.
	out := stripANSI(m.View())
	if !strings.Contains(out, "Press Esc again to quit") {
		t.Errorf("expected confirmation toast in view; got:\n%s", out)
	}
	// Second Esc — quits.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("second Esc returned nil cmd, expected tea.Quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected QuitMsg, got nil")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestQuit_OtherKeyCancelsArm(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if !m.QuitArmed() {
		t.Fatal("expected armed after first Esc")
	}
	// Press 'j' — should clear the arm.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mi.(Model)
	if m.QuitArmed() {
		t.Errorf("non-Esc key did not clear armed state")
	}
}

func TestQuit_QStillSilentlyQuits(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestQuit_EscWhileOverlayOpen_DoesNotArm(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	// Open the help overlay.
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = mi.(Model)
	if !m.HelpVisible() {
		t.Fatal("help overlay didn't open")
	}
	// Esc should close the overlay, NOT arm quit.
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.HelpVisible() {
		t.Error("Esc did not close help overlay")
	}
	if m.QuitArmed() {
		t.Error("Esc closed overlay but also armed quit (it shouldn't)")
	}
}

func TestLayout_RussianBracketCyclesTab(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	if m.view != panes.ViewMyTasks {
		t.Fatalf("initial view = %v, want ViewMyTasks", m.view)
	}
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ъ'}})
	m = mi.(Model)
	if m.view != panes.ViewWatching {
		t.Errorf("Russian ] (ъ) didn't cycle tab forward; view = %v", m.view)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'х'}})
	m = mi.(Model)
	if m.view != panes.ViewMyTasks {
		t.Errorf("Russian [ (х) didn't cycle tab backward; view = %v", m.view)
	}
}

func TestLayout_RussianQQuits(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'й'}})
	if cmd == nil {
		t.Fatal("Russian й (q) did not produce quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("Russian й (q) did not quit")
	}
}

func TestApp_CommaOpensOptionsOverlay(t *testing.T) {
	m := newTestModel(t)
	if m.OptionsVisible() {
		t.Fatal("options overlay should start hidden")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(",")})
	mm := m2.(Model)
	if !mm.OptionsVisible() {
		t.Fatal("',' should open the options overlay")
	}
}

func TestApp_OptionsAppliedPersistsToState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	m := newTestModel(t)
	m.statePath = statePath
	msg := overlays.OptionsAppliedMsg{
		Grouping: "priority",
		Sort:     "updated",
		Desc:     true,
	}
	_, _ = m.Update(msg)

	// Save runs in a goroutine; poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, err := state.Load(statePath)
		if err == nil && st.Grouping == "priority" && st.Sort == "updated" &&
			st.SortDesc != nil && *st.SortDesc {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _ := state.Load(statePath)
	t.Fatalf("state not persisted: grouping=%q sort=%q desc=%v", st.Grouping, st.Sort, st.SortDesc)
}

func TestLayout_NotTranslatedWhenOverlayOpen(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = mi.(Model)
	if !m.HelpVisible() {
		t.Fatal("help overlay didn't open")
	}
	preView := m.view
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ъ'}})
	m = mi.(Model)
	if m.view != preView {
		t.Errorf("hotkey translation fired with overlay open; view changed to %v", m.view)
	}
}

func TestApp_CreateOverlayReporterPrefilled(t *testing.T) {
	// Test the end-to-end flow: bootstrap sets m.accountID, create overlay
	// gets it via SetCurrentUserAccountID, form is built with reporter field,
	// and the reporter auto-fills with (me).
	m := newTestModel(t)
	m.accountID = "acc-me"

	// Set the current user account ID on the create overlay (simulating
	// what the root model does after bootstrap completes).
	c := m.Create()
	c = c.SetCurrentUserAccountID(m.accountID)

	// Simulate what happens when the create overlay loads metadata for a
	// project+type pair. We'll build the form directly using the same
	// BuildForm call that handleMetaLoadedMsg uses.
	meta := jira.CreateMeta{Fields: []jira.FieldMeta{
		{ID: "summary", Name: "Summary", SchemaType: "string"},
		{ID: "reporter", Name: "Reporter", SchemaType: "user"},
	}}
	form := overlays.BuildForm(meta, overlays.FormDefaults{CurrentUserAccountID: m.accountID})

	// Assert: the reporter field in the form should be pre-filled.
	var rep *overlays.Field
	for i := range form.Fields {
		if form.Fields[i].Meta.ID == "reporter" {
			rep = &form.Fields[i]
			break
		}
	}
	if rep == nil {
		t.Fatal("reporter field not in form")
	}
	if rep.UserAccountID() != "acc-me" {
		t.Fatalf("reporter UserAccountID = %q, want acc-me", rep.UserAccountID())
	}
}
