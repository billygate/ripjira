package tui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// refreshLoader is a minimal AppLoader that counts list/detail loads and
// returns canned data so refresh tests can assert on dispatch counts without
// the full integration_test stub machinery.
type refreshLoader struct {
	listCalls        int32
	issueCalls       int32
	commentsCalls    int32
	transitionsCalls int32

	issues []jira.Issue
	issue  jira.Issue
}

func (l *refreshLoader) LoadIssues(_ context.Context, _ panes.ViewKind, _ string) ([]jira.Issue, error) {
	atomic.AddInt32(&l.listCalls, 1)
	return l.issues, nil
}
func (l *refreshLoader) LoadIssue(_ context.Context, _ string) (jira.Issue, error) {
	atomic.AddInt32(&l.issueCalls, 1)
	return l.issue, nil
}
func (l *refreshLoader) LoadComments(_ context.Context, _ string) ([]jira.Comment, error) {
	atomic.AddInt32(&l.commentsCalls, 1)
	return nil, nil
}
func (l *refreshLoader) LoadTransitions(_ context.Context, _ string) ([]jira.Transition, error) {
	atomic.AddInt32(&l.transitionsCalls, 1)
	return nil, nil
}
func (l *refreshLoader) LoadAttachment(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (l *refreshLoader) DoTransition(_ context.Context, _, _ string) error { return nil }
func (l *refreshLoader) AddComment(_ context.Context, _, _ string) error   { return nil }
func (l *refreshLoader) SearchUsers(_ context.Context, _ string) ([]jira.User, error) {
	return nil, nil
}
func (l *refreshLoader) AssignIssue(_ context.Context, _, _ string) error   { return nil }
func (l *refreshLoader) GetMyself(_ context.Context) (jira.User, error)     { return jira.User{}, nil }
func (l *refreshLoader) Projects(_ context.Context) ([]jira.Project, error) { return nil, nil }
func (l *refreshLoader) IssueTypesForProject(_ context.Context, _ string) ([]jira.IssueType, error) {
	return nil, nil
}
func (l *refreshLoader) CreateMeta(_ context.Context, _, _ string) (jira.CreateMeta, error) {
	return jira.CreateMeta{}, nil
}
func (l *refreshLoader) CreateIssue(_ context.Context, _ jira.CreatePayload) (jira.Issue, error) {
	return jira.Issue{}, nil
}

func newRefreshModel(t *testing.T, opts ...Option) (Model, *refreshLoader) {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	loader := &refreshLoader{
		issues: []jira.Issue{{
			Key:      "PROJ-1",
			Summary:  "Refresh subject",
			Status:   jira.Status{Name: "To Do", Category: "new"},
			Priority: jira.Priority{Name: "High"},
		}},
		issue: jira.Issue{
			Key:         "PROJ-1",
			Summary:     "Refresh subject",
			Description: "body",
			Status:      jira.Status{Name: "To Do", Category: "new"},
			Priority:    jira.Priority{Name: "High"},
		},
	}
	allOpts := append([]Option{WithLoader(loader)}, opts...)
	return New(p, allOpts...), loader
}

// drainCmd repeatedly invokes cmd, feeding each produced msg back into
// m.Update, so a tea.Batch can be unwound synchronously inside a test. Stops
// when cmd returns nil. Skips any tea.Tick scheduling messages that wrap a
// real timer — those would block the test if executed.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch msg.(type) {
		case nil:
			continue
		case panes.IssueLoadedMsg, panes.CommentsLoadedMsg, panes.TransitionsLoadedMsg:
			// Detail-load completions always go through the root Update so
			// the detail pane sees them and clears the loading state.
		}
		// tea.Batch returns a BatchMsg whose elements are themselves Cmds.
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

// TestManualRefresh_RefreshesListAndDetail verifies that pressing `r` dispatches
// a list refresh AND re-runs the open issue's detail loads.
func TestManualRefresh_RefreshesListAndDetail(t *testing.T) {
	m, loader := newRefreshModel(t)
	m, _ = sendSize(m, 120, 40)
	m = drainCmd(t, m, m.Init())
	if got := atomic.LoadInt32(&loader.listCalls); got != 1 {
		t.Fatalf("Init list calls = %d, want 1", got)
	}

	// Simulate user navigating to the issue so the detail pane has an
	// issue to re-fetch on `r`.
	cmd := m.detail.SetIssue(&loader.issue)
	m = drainCmd(t, m, cmd)
	baselineIssue := atomic.LoadInt32(&loader.issueCalls)
	baselineComments := atomic.LoadInt32(&loader.commentsCalls)
	baselineTrans := atomic.LoadInt32(&loader.transitionsCalls)
	if baselineIssue == 0 {
		t.Fatalf("detail SetIssue did not trigger any LoadIssue calls (baseline=%d)", baselineIssue)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = drainCmd(t, updated.(Model), cmd)

	if got := atomic.LoadInt32(&loader.listCalls); got != 2 {
		t.Errorf("after `r`, list calls = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&loader.issueCalls); got != baselineIssue+1 {
		t.Errorf("after `r`, issue calls = %d, want %d", got, baselineIssue+1)
	}
	if got := atomic.LoadInt32(&loader.commentsCalls); got != baselineComments+1 {
		t.Errorf("after `r`, comment calls = %d, want %d", got, baselineComments+1)
	}
	if got := atomic.LoadInt32(&loader.transitionsCalls); got != baselineTrans+1 {
		t.Errorf("after `r`, transition calls = %d, want %d", got, baselineTrans+1)
	}
}

// TestManualRefresh_NoDetailLoadWhenNoSelection verifies `r` only fires the
// list refresh when no issue is open in the detail pane.
func TestManualRefresh_NoDetailLoadWhenNoSelection(t *testing.T) {
	m, loader := newRefreshModel(t)
	m, _ = sendSize(m, 120, 40)
	m = drainCmd(t, m, m.Init())
	if got := atomic.LoadInt32(&loader.issueCalls); got != 0 {
		t.Fatalf("baseline issue calls = %d, want 0", got)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = drainCmd(t, updated.(Model), cmd)

	if got := atomic.LoadInt32(&loader.listCalls); got != 2 {
		t.Errorf("after `r` with no detail, list calls = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&loader.issueCalls); got != 0 {
		t.Errorf("after `r` with no detail, issue calls = %d, want 0", got)
	}
}

// TestAutoRefresh_TickerArmedOnInit verifies that when AutoRefresh > 0, Init
// arms exactly one tick via the supplied ticker function.
func TestAutoRefresh_TickerArmedOnInit(t *testing.T) {
	armed := 0
	var lastDuration time.Duration
	ticker := func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		armed++
		lastDuration = d
		return nil
	}
	m, _ := newRefreshModel(t,
		WithAutoRefresh(45*time.Second),
		WithAutoRefreshTicker(ticker),
	)
	_ = drainCmd(t, m, m.Init())
	if armed != 1 {
		t.Errorf("ticker armed %d times, want 1", armed)
	}
	if lastDuration != 45*time.Second {
		t.Errorf("ticker duration = %v, want 45s", lastDuration)
	}
}

// TestAutoRefresh_TickRefreshesListSilently verifies that an auto-refresh
// tick triggers a list refresh, re-arms the ticker, and does NOT touch the
// detail pane even when an issue is open.
func TestAutoRefresh_TickRefreshesListSilently(t *testing.T) {
	armed := 0
	ticker := func(_ time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		armed++
		return nil
	}
	m, loader := newRefreshModel(t,
		WithAutoRefresh(time.Second),
		WithAutoRefreshTicker(ticker),
	)
	m, _ = sendSize(m, 120, 40)
	m = drainCmd(t, m, m.Init())
	if armed != 1 {
		t.Fatalf("init armed = %d, want 1", armed)
	}
	if got := atomic.LoadInt32(&loader.listCalls); got != 1 {
		t.Fatalf("baseline list calls = %d, want 1", got)
	}

	// Simulate an open detail pane and capture baselines for issue loads.
	m = drainCmd(t, m, m.detail.SetIssue(&loader.issue))
	baselineIssue := atomic.LoadInt32(&loader.issueCalls)
	baselineComments := atomic.LoadInt32(&loader.commentsCalls)
	baselineTrans := atomic.LoadInt32(&loader.transitionsCalls)

	// Drive a tick directly. This is what the injected ticker would have
	// produced after its delay; bypassing the timer keeps the test fast.
	updated, cmd := m.Update(autoRefreshTickMsg{})
	m = drainCmd(t, updated.(Model), cmd)

	if got := atomic.LoadInt32(&loader.listCalls); got != 2 {
		t.Errorf("after tick, list calls = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&loader.issueCalls); got != baselineIssue {
		t.Errorf("auto-refresh tick triggered LoadIssue: %d → %d", baselineIssue, got)
	}
	if got := atomic.LoadInt32(&loader.commentsCalls); got != baselineComments {
		t.Errorf("auto-refresh tick triggered LoadComments: %d → %d", baselineComments, got)
	}
	if got := atomic.LoadInt32(&loader.transitionsCalls); got != baselineTrans {
		t.Errorf("auto-refresh tick triggered LoadTransitions: %d → %d", baselineTrans, got)
	}
	if armed != 2 {
		t.Errorf("ticker re-armed = %d, want 2 (Init + after tick)", armed)
	}
}

// TestAutoRefresh_DisabledByDefault verifies the ticker is never invoked when
// AutoRefresh is left at zero.
func TestAutoRefresh_DisabledByDefault(t *testing.T) {
	armed := 0
	ticker := func(_ time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		armed++
		return nil
	}
	m, _ := newRefreshModel(t, WithAutoRefreshTicker(ticker))
	_ = drainCmd(t, m, m.Init())
	if armed != 0 {
		t.Errorf("ticker armed without AutoRefresh: %d, want 0", armed)
	}
}

// TestAutoRefresh_DefaultTickerIsTeaTick exercises the default ticker path:
// constructing a Model with AutoRefresh > 0 but no explicit ticker should
// yield a non-nil scheduling cmd from Init. We don't run the timer to
// completion (that would block the test); we just confirm the cmd exists.
func TestAutoRefresh_DefaultTickerIsTeaTick(t *testing.T) {
	m, _ := newRefreshModel(t, WithAutoRefresh(50*time.Millisecond))
	if cmd := m.scheduleAutoRefresh(); cmd == nil {
		t.Fatal("scheduleAutoRefresh returned nil with AutoRefresh > 0")
	}
}
