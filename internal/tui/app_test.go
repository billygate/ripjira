package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
	structureadapter "github.com/billygate/ripjira/internal/tui/structureadapter"
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

	for _, want := range []string{"RJ", "Issues", "Details"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q\nfull output:\n%s", want, out)
		}
	}

	// hint bar should advertise the tab navigation hints.
	for _, want := range []string{"top tab", "sub-tab"} {
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
		return bytes.Contains(out, []byte("RJ"))
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
		return bytes.Contains(out, []byte("RJ"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("edit summary+body")) || bytes.Contains(out, []byte("Keymap"))
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
func (l *recordingLoader) DoTransition(context.Context, string, string) error         { return nil }
func (l *recordingLoader) AddComment(context.Context, string, string) error           { return nil }
func (l *recordingLoader) SearchUsers(context.Context, string) ([]jira.User, error)   { return nil, nil }
func (l *recordingLoader) AssignIssue(context.Context, string, string) error          { return nil }
func (l *recordingLoader) UpdateFields(context.Context, string, map[string]any) error { return nil }
func (l *recordingLoader) UpdateDescription(context.Context, string, string) error    { return nil }
func (l *recordingLoader) CreateLink(context.Context, string, string, string) error   { return nil }
func (l *recordingLoader) DeleteLink(context.Context, string) error                   { return nil }
func (l *recordingLoader) AddWatcher(context.Context, string, string) error           { return nil }
func (l *recordingLoader) RemoveWatcher(context.Context, string, string) error        { return nil }
func (l *recordingLoader) AddWorklog(context.Context, string, string, string) error   { return nil }
func (l *recordingLoader) DeleteWorklog(context.Context, string, string) error        { return nil }
func (l *recordingLoader) GetMyself(context.Context) (jira.User, error)               { return jira.User{}, nil }
func (l *recordingLoader) Projects(context.Context) ([]jira.Project, error)           { return nil, nil }
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

func TestStrategy_DefaultIsParentForMyTasks(t *testing.T) {
	m := newTestModel(t)
	mi, _ := m.handleViewSelected(panes.ViewMyTasks)
	// view stays the same on no-op call; force a switch to invalidate cache.
	mi, _ = mi.(Model).handleViewSelected(panes.ViewWatching)
	mi, _ = mi.(Model).handleViewSelected(panes.ViewMyTasks)
	m = mi.(Model)
	if got := m.list.Strategy().Name(); got != "parent" {
		t.Errorf("MyTasks strategy = %q, want %q", got, "parent")
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
	if got := m.list.Strategy().Name(); got != "parent" {
		t.Errorf("on return to MyTasks strategy = %q, want %q", got, "parent")
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

func TestCreateSubmitDone_OffersEpicLinkForRegularIssue(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(overlays.CreateSubmitDoneMsg{
		ProjectKey:  "PROJ",
		IssueTypeID: "10100",
		Issue:       jira.Issue{Key: "PROJ-200", URL: "https://j/PROJ-200"},
	})
	m = mi.(Model)
	if !m.epicPicker.Visible() {
		t.Fatal("epic picker should be visible after regular create")
	}
	if m.created.Visible() {
		t.Error("created popup must not be shown until epic step resolves")
	}
	if m.createdPending.Key != "PROJ-200" {
		t.Errorf("createdPending = %q, want PROJ-200", m.createdPending.Key)
	}
}

func TestCreateSubmitDone_SkipsEpicLinkForSubtask(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	parent := jira.Issue{Key: "PROJ-100"}
	c, _ := m.create.ShowAsSubtask(parent, []jira.Project{{Key: "PROJ"}})
	m.create = c
	mi, _ := m.Update(overlays.CreateSubmitDoneMsg{
		ProjectKey:  "PROJ",
		IssueTypeID: "10103",
		Issue:       jira.Issue{Key: "PROJ-201", URL: "https://j/PROJ-201"},
	})
	m = mi.(Model)
	if m.epicPicker.Visible() {
		t.Error("subtask creation must not open epic picker")
	}
	if !m.created.Visible() {
		t.Error("subtask creation should show created popup directly")
	}
}

// TestUpdate_RoutesDigitJumpTimeoutToList covers the regression where the
// list pane scheduled a digitJumpTimeout via tea.Tick but the root Update
// had no case for it — the message landed in the spinner default and was
// silently eaten, so single-digit jumps on lists with ≥10 issues never
// committed even after the timer.
func TestUpdate_RoutesDigitJumpTimeoutToList(t *testing.T) {
	m := newTestAppModel(t, 120, 30)
	issues := make([]jira.Issue, 0, 12)
	for i := 1; i <= 12; i++ {
		issues = append(issues, jira.Issue{
			Key: fmt.Sprintf("PROJ-%d", i), Summary: fmt.Sprintf("row %d", i),
			Status: jira.Status{Name: "To Do", Category: "new"},
		})
	}
	m.list.SetIssues(issues)
	// Send '1' through the root model so the list buffers it as pending and
	// returns the tea.Tick cmd. We don't sleep — we synthesise the timeout
	// message ourselves to assert the routing wires it back to list.Update.
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = mi.(Model)
	if got := m.list.Selected(); got != nil && got.Key == "PROJ-1" {
		t.Fatalf("digit jump committed without timeout — pending wait skipped")
	}
	mi, _ = m.Update(panes.DigitJumpTimeoutMsg{Gen: m.list.PendingDigitGen()})
	m = mi.(Model)
	if got := m.list.Selected(); got == nil || got.Key != "PROJ-1" {
		t.Fatalf("after timeout: selected = %v, want PROJ-1", got)
	}
}

func TestCreatedDismissed_SelectsIssueWhenInList(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	m.list.SetIssues([]jira.Issue{
		{Key: "PROJ-1", Summary: "first"},
		{Key: "PROJ-2", Summary: "second"},
	})
	mi, _ := m.Update(overlays.CreatedDismissedMsg{Key: "PROJ-2"})
	m = mi.(Model)
	sel := m.list.Selected()
	if sel == nil || sel.Key != "PROJ-2" {
		t.Errorf("after dismiss selected = %+v, want PROJ-2", sel)
	}
}

func TestTransitionDone_ReloadsDetailWhenIssueMatches(t *testing.T) {
	m := newTestAppModelWithLoader(t, 120, 30, &recordingLoader{})
	_ = m.detail.SetIssue(&jira.Issue{Key: "PROJ-7"})
	tok := m.detail.Token()

	updated, _ := m.Update(transitionDoneMsg{
		IssueKey:  "PROJ-7",
		NewStatus: jira.Status{Name: "Done"},
	})
	m = updated.(Model)
	if m.detail.Token() <= tok {
		t.Fatalf("transition success must Reload detail (token %d -> %d)", tok, m.detail.Token())
	}
}

func TestTransitionDone_NoReloadWhenIssueDiffers(t *testing.T) {
	m := newTestAppModelWithLoader(t, 120, 30, &recordingLoader{})
	_ = m.detail.SetIssue(&jira.Issue{Key: "PROJ-7"})
	tok := m.detail.Token()

	updated, _ := m.Update(transitionDoneMsg{
		IssueKey:  "OTHER-1",
		NewStatus: jira.Status{Name: "Done"},
	})
	m = updated.(Model)
	if m.detail.Token() != tok {
		t.Errorf("non-matching issue must not bump token (got %d, want %d)", m.detail.Token(), tok)
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
	// Drill-down: only the active top group (MY ISSUES) and its subs are
	// rendered; other top groups are reachable via }/{ but not on screen.
	for _, want := range []string{"MY ISSUES", "ASSIGNED", "WATCHING", "REPORTED", "RECENT", "MENTIONS"} {
		if !strings.Contains(out, want) {
			t.Errorf("tab bar missing %q\nfull output:\n%s", want, out)
		}
	}
}

// `]` cycles SUB-views within the active top tab (MY ISSUES has five subs).
func TestTabBar_NextSubCyclesForward(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	if m.view != panes.ViewMyTasks {
		t.Fatalf("initial view = %v, want ViewMyTasks", m.view)
	}
	want := []panes.ViewKind{
		panes.ViewWatching, panes.ViewReported, panes.ViewRecent,
		panes.ViewMentions, panes.ViewMyTasks,
	}
	for i, w := range want {
		mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
		m = mi.(Model)
		if m.view != w {
			t.Errorf("after %d × ] view = %v, want %v", i+1, m.view, w)
		}
	}
}

// `}` cycles TOP tabs (MY → SPRINT → STRUCTURES → SEARCH → MY).
func TestTabBar_NextTopCyclesForward(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'}'}})
	m = mi.(Model)
	if g := panes.TopGroup(m.view); g != panes.TopSprint {
		t.Errorf("after } topGroup = %v, want TopSprint", g)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'}'}})
	m = mi.(Model)
	if g := panes.TopGroup(m.view); g != panes.TopStructures {
		t.Errorf("after }} topGroup = %v, want TopStructures", g)
	}
}

// `{` cycles TOP tabs backward.
func TestTabBar_PrevTopCyclesBackward(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{'}})
	m = mi.(Model)
	if g := panes.TopGroup(m.view); g != panes.TopSearch {
		t.Errorf("after { topGroup = %v, want TopSearch", g)
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

func TestApp_ScopeEditor_OpensOnEditScopeMsg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ABC.yml"), []byte(`- id: u1
  name: User One
  sections:
    - title: All
      filter:
        status: [Open]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	pal, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	store := structure.NewStore(dir)
	m := New(pal,
		WithStructures(context.Background(), store),
		WithDefaultProject("ABC"),
	)
	m2, _ := m.Update(overlays.StructureEditScopeMsg{ID: "u1"})
	m = m2.(Model)
	if !m.scopeEditor.Visible() {
		t.Fatal("scope editor should be visible after StructureEditScopeMsg")
	}
}

func TestApp_ScopeSaved_PersistsToStore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ABC.yml"), []byte(`- id: u1
  name: User One
  sections:
    - title: All
      filter:
        status: [Open]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	pal, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	store := structure.NewStore(dir)
	m := New(pal,
		WithStructures(context.Background(), store),
		WithDefaultProject("ABC"),
	)
	saved := overlays.ScopeSavedMsg{
		StructureID: "u1",
		Rows: []structureadapter.ScopeRow{
			{Field: "labels", Op: structureadapter.OpIn, Values: []string{"Q12026"}},
		},
	}
	m.Update(saved)
	got, err := store.FindByID("ABC", "u1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(got.Scope) == 0 || len(got.Scope["labels"].In) == 0 || got.Scope["labels"].In[0] != "Q12026" {
		t.Fatalf("scope not persisted: %#v", got.Scope)
	}
}

func TestModelStoresConfig(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
		EpicIssueTypes:     []string{"Epic"},
	}
	m := New(themes.TokyoNight(),
		WithConfig(cfg),
		WithConfigPath("/tmp/ripjira-test.yaml"),
	)
	if got := m.Config(); got.Theme != cfg.Theme {
		t.Fatalf("Config().Theme = %q, want %q", got.Theme, cfg.Theme)
	}
	if got := m.ConfigPath(); got != "/tmp/ripjira-test.yaml" {
		t.Fatalf("ConfigPath = %q, want /tmp/ripjira-test.yaml", got)
	}
}

func TestSettingsOverlay_RendersFrame(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
		EpicIssueTypes:     []string{"Epic", "Epic Feature"},
	}
	p, _ := themes.ByName("tokyonight")
	m := New(p, WithConfig(cfg))
	m, _ = sendSize(m, 100, 40)
	m.settings = m.settings.Show(cfg)

	view := stripANSI(m.View())
	for _, want := range []string{
		"Settings",
		"Theme",
		"Icons",
		"Default grouping",
		"Auto refresh (s)",
		"Epic issue types",
		"tokyonight",
		"unicode",
		"status",
		"60",
		"Epic, Epic Feature",
		"ctrl+s save",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("settings overlay frame missing %q", want)
		}
	}
}

func TestSettingsOverlayShows(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
	}
	m := New(themes.TokyoNight(), WithConfig(cfg))
	m.settings = m.settings.Show(cfg)
	if !m.settings.Visible() {
		t.Fatal("settings should be visible after Show()")
	}
}

func TestIssueKeyInGroupRe(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"BILLING-10118", "BILLING-10118"},
		{"BILLING-10118  Back support Q2 2026", "BILLING-10118"},
		{"parent_key: BILLING-42", "BILLING-42"},
		{"No epic", ""},
		{"Epics", ""},
		{"PROJ_X-7  thing", "PROJ_X-7"},
	}
	for _, tc := range cases {
		if got := issueKeyInGroupRe.FindString(tc.in); got != tc.want {
			t.Errorf("FindString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSettingsAppliedRebuildsTheme(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
		EpicIssueTypes:     []string{"Epic"},
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	m := New(themes.TokyoNight(), WithConfig(cfg), WithConfigPath(cfgPath))
	newCfg := cfg
	newCfg.Theme = config.ThemeNord
	newCfg.AutoRefreshSeconds = 30
	newCfg.EpicIssueTypes = []string{"Initiative"}

	updated, cmd := m.Update(overlays.SettingsAppliedMsg{NewCfg: newCfg})
	mm := updated.(Model)

	if mm.cfg.Theme != config.ThemeNord {
		t.Fatalf("cfg.Theme = %q, want nord", mm.cfg.Theme)
	}
	nord := themes.Nord()
	if mm.palette.Name() != nord.Name() {
		t.Fatalf("palette = %q, want %q", mm.palette.Name(), nord.Name())
	}
	if mm.cfg.AutoRefreshSeconds != 30 {
		t.Fatalf("cfg.AutoRefreshSeconds = %d, want 30", mm.cfg.AutoRefreshSeconds)
	}
	if !reflect.DeepEqual(mm.epicTypes, []string{"Initiative"}) {
		t.Fatalf("epicTypes = %v, want [Initiative]", mm.epicTypes)
	}
	if cmd == nil {
		t.Fatal("expected save cmd, got nil")
	}
	msg := cmd()
	// The save cmd may be batched with a refresh tick; pick out the save outcome.
	// tea.Batch returns a tea.Msg of type tea.BatchMsg. Iterate any sequence.
	var saveErr error
	switch v := msg.(type) {
	case SettingsSaveErrorMsg:
		saveErr = v.Err
	case nil:
		// success
	case tea.BatchMsg:
		// Batch returns []tea.Cmd; execute each and look for save error.
		for _, c := range v {
			if c == nil {
				continue
			}
			if e, ok := c().(SettingsSaveErrorMsg); ok {
				saveErr = e.Err
			}
		}
	}
	if saveErr != nil {
		t.Fatalf("save returned error: %v", saveErr)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "nord") {
		t.Fatalf("config.yaml does not contain new theme; got:\n%s", raw)
	}
}

func TestSettingsSaveErrorReopensOverlay(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
		EpicIssueTypes:     []string{"Epic"},
	}
	// Build a path where the parent does not exist AND cannot be created
	// (creating a directory under a regular file fails).
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(blocker, "config.yaml")

	m := New(themes.TokyoNight(), WithConfig(cfg), WithConfigPath(badPath))
	newCfg := cfg
	newCfg.Theme = config.ThemeNord

	updated, cmd := m.Update(overlays.SettingsAppliedMsg{NewCfg: newCfg})
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	// Resolve cmd into a SettingsSaveErrorMsg. The cmd may be a single
	// function or a tea.BatchMsg of cmds.
	msg := cmd()
	var errMsg SettingsSaveErrorMsg
	var found bool
	switch v := msg.(type) {
	case SettingsSaveErrorMsg:
		errMsg, found = v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if e, ok := c().(SettingsSaveErrorMsg); ok {
				errMsg, found = e, true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected SettingsSaveErrorMsg in cmd output, got %T", msg)
	}

	// Now feed the error back into the model.
	mm := updated.(Model)
	afterErr, toastCmd := mm.Update(errMsg)
	am := afterErr.(Model)
	if !am.settings.Visible() {
		t.Fatal("settings overlay should be re-opened on save error")
	}
	if am.settings.Draft().Theme != config.ThemeNord {
		t.Fatal("draft should preserve user's pending theme change")
	}
	if toastCmd == nil {
		t.Fatal("save error handler must return a toast cmd")
	}
	tm, ok := toastCmd().(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %T", toastCmd())
	}
	if tm.Level != ToastError {
		t.Fatalf("toast level = %v, want ToastError", tm.Level)
	}
}

// TestSettingsAppliedDropsAllEpicTypes verifies that deleting every epic
// issue type via the sub-overlay propagates into m.epicTypes, not just into
// m.cfg, so the parent-grouping strategy reflects the change immediately.
func TestSettingsAppliedDropsAllEpicTypes(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
		EpicIssueTypes:     []string{"Epic", "Epic Feature"},
	}
	m := New(themes.TokyoNight(), WithConfig(cfg))
	if got := len(m.epicTypes); got == 0 {
		t.Fatalf("seed: m.epicTypes empty, want non-empty")
	}
	newCfg := cfg
	newCfg.EpicIssueTypes = nil
	updated, _ := m.Update(overlays.SettingsAppliedMsg{NewCfg: newCfg})
	mm := updated.(Model)
	if len(mm.epicTypes) != 0 {
		t.Fatalf("m.epicTypes = %v, want empty after deleting all entries", mm.epicTypes)
	}
}

func TestSettingsCancelledIsNoop(t *testing.T) {
	cfg := config.Config{
		BaseURL:            "https://x.atlassian.net",
		Email:              "a@b.c",
		Theme:              config.ThemeTokyoNight,
		Icons:              config.IconsUnicode,
		DefaultGrouping:    config.GroupingStatus,
		AutoRefreshSeconds: 60,
	}
	m := New(themes.TokyoNight(), WithConfig(cfg))
	updated, cmd := m.Update(overlays.SettingsCancelledMsg{})
	mm := updated.(Model)
	if mm.cfg.Theme != cfg.Theme {
		t.Fatal("Theme should be unchanged")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd())
	}
}

func TestKeymap_CtrlE_DispatchesEditorOpen_OnIssueScreen(t *testing.T) {
	m := newTestAppModel(t, 120, 30)

	m.detail.SetIssue(&jira.Issue{
		Key:         "ABC-1",
		Summary:     "Title",
		Description: "Body markdown",
	})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a tea.Cmd from ctrl+e dispatch")
	}
	if m.editorToken == 0 {
		t.Fatalf("editorToken not advanced after ctrl+e")
	}
}
