package tui_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// stubLoader is the AppLoader used by the end-to-end teatest. Each method
// drains a per-key channel (or a default channel for LoadIssues) so the test
// can pace exactly when each load resolves and observe what the app does
// with skeleton placeholders along the way.
type stubLoader struct {
	mu sync.Mutex

	listCh chan listResult

	issueCh        map[string]chan issueResult
	commentsCh     map[string]chan commentsResult
	transitionsCh  map[string]chan transitionsResult
	doTransitionCh map[string]chan error
	addCommentCh   map[string]chan error
	addCommentLog  []addCommentCall

	searchUsersCh  map[string]chan searchUsersResult
	searchUsersLog []string
	assignCh       map[string]chan error
	assignLog      []assignCall
}

type assignCall struct {
	key       string
	accountID string
}

type searchUsersResult struct {
	users []jira.User
	err   error
}

type addCommentCall struct {
	key  string
	body string
}

type listResult struct {
	issues []jira.Issue
	err    error
}
type issueResult struct {
	issue jira.Issue
	err   error
}
type commentsResult struct {
	comments []jira.Comment
	err      error
}
type transitionsResult struct {
	transitions []jira.Transition
	err         error
}

func newStubLoader() *stubLoader {
	return &stubLoader{
		listCh:         make(chan listResult, 1),
		issueCh:        map[string]chan issueResult{},
		commentsCh:     map[string]chan commentsResult{},
		transitionsCh:  map[string]chan transitionsResult{},
		doTransitionCh: map[string]chan error{},
		addCommentCh:   map[string]chan error{},
		searchUsersCh:  map[string]chan searchUsersResult{},
		assignCh:       map[string]chan error{},
	}
}

func (s *stubLoader) issue(key string) chan issueResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.issueCh[key]; !ok {
		s.issueCh[key] = make(chan issueResult, 1)
	}
	return s.issueCh[key]
}
func (s *stubLoader) comments(key string) chan commentsResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.commentsCh[key]; !ok {
		s.commentsCh[key] = make(chan commentsResult, 1)
	}
	return s.commentsCh[key]
}
func (s *stubLoader) transitions(key string) chan transitionsResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.transitionsCh[key]; !ok {
		s.transitionsCh[key] = make(chan transitionsResult, 1)
	}
	return s.transitionsCh[key]
}

func (s *stubLoader) LoadIssues(ctx context.Context, _ panes.ViewKind, _ string) ([]jira.Issue, error) {
	select {
	case r := <-s.listCh:
		return r.issues, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *stubLoader) LoadIssue(ctx context.Context, key string) (jira.Issue, error) {
	select {
	case r := <-s.issue(key):
		return r.issue, r.err
	case <-ctx.Done():
		return jira.Issue{}, ctx.Err()
	}
}
func (s *stubLoader) LoadComments(ctx context.Context, key string) ([]jira.Comment, error) {
	select {
	case r := <-s.comments(key):
		return r.comments, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *stubLoader) LoadTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	select {
	case r := <-s.transitions(key):
		return r.transitions, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *stubLoader) LoadAttachment(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}

func (s *stubLoader) doTransition(transitionID string) chan error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doTransitionCh[transitionID]; !ok {
		s.doTransitionCh[transitionID] = make(chan error, 1)
	}
	return s.doTransitionCh[transitionID]
}

func (s *stubLoader) DoTransition(ctx context.Context, _, transitionID string) error {
	select {
	case err := <-s.doTransition(transitionID):
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *stubLoader) addComment(key string) chan error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.addCommentCh[key]; !ok {
		s.addCommentCh[key] = make(chan error, 1)
	}
	return s.addCommentCh[key]
}

func (s *stubLoader) AddComment(ctx context.Context, key, body string) error {
	s.mu.Lock()
	s.addCommentLog = append(s.addCommentLog, addCommentCall{key: key, body: body})
	s.mu.Unlock()
	select {
	case err := <-s.addComment(key):
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *stubLoader) searchUsers(query string) chan searchUsersResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.searchUsersCh[query]; !ok {
		s.searchUsersCh[query] = make(chan searchUsersResult, 1)
	}
	return s.searchUsersCh[query]
}

func (s *stubLoader) SearchUsers(ctx context.Context, query string) ([]jira.User, error) {
	s.mu.Lock()
	s.searchUsersLog = append(s.searchUsersLog, query)
	s.mu.Unlock()
	select {
	case r := <-s.searchUsers(query):
		return r.users, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *stubLoader) assign(key string) chan error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assignCh[key]; !ok {
		s.assignCh[key] = make(chan error, 1)
	}
	return s.assignCh[key]
}

func (s *stubLoader) AssignIssue(ctx context.Context, key, accountID string) error {
	s.mu.Lock()
	s.assignLog = append(s.assignLog, assignCall{key: key, accountID: accountID})
	s.mu.Unlock()
	select {
	case err := <-s.assign(key):
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *stubLoader) UpdateFields(context.Context, string, map[string]any) error {
	return nil
}

func (s *stubLoader) UpdateDescription(context.Context, string, string) error {
	return nil
}

func (s *stubLoader) CreateLink(context.Context, string, string, string) error {
	return nil
}

func (s *stubLoader) DeleteLink(context.Context, string) error { return nil }

func (s *stubLoader) AddWatcher(context.Context, string, string) error    { return nil }
func (s *stubLoader) RemoveWatcher(context.Context, string, string) error { return nil }
func (s *stubLoader) AddWorklog(context.Context, string, string, string) error {
	return nil
}

func (s *stubLoader) DeleteWorklog(context.Context, string, string) error {
	return nil
}

func (s *stubLoader) GetMyself(_ context.Context) (jira.User, error) {
	return jira.User{}, nil
}

func (s *stubLoader) Projects(_ context.Context) ([]jira.Project, error) { return nil, nil }
func (s *stubLoader) IssueTypesForProject(_ context.Context, _ string) ([]jira.IssueType, error) {
	return nil, nil
}
func (s *stubLoader) CreateMeta(_ context.Context, _, _ string) (jira.CreateMeta, error) {
	return jira.CreateMeta{}, nil
}
func (s *stubLoader) CreateIssue(_ context.Context, _ jira.CreatePayload) (jira.Issue, error) {
	return jira.Issue{}, nil
}
func (s *stubLoader) SearchEpics(_ context.Context, _ string, _ []string) ([]jira.Issue, error) {
	return nil, nil
}
func (s *stubLoader) SetParent(_ context.Context, _, _ string) error { return nil }

// TestStage2_EndToEnd is the integration check called out by Task 17 of the
// plan: launch the app with a stub client, verify the list renders, navigate
// to an issue, and watch the detail load. This proves the whole Stage 2
// pipeline (loader → app → list pane → detail pane) is correctly wired.
func TestStage2_EndToEnd(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	model := tui.New(palette, tui.WithLoader(loader))
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(120, 40))

	// Initial frame: the chrome should be there even before any data arrives.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Issues")) && bytes.Contains(b, []byte("Details"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Resolve the background MyIssues call with two issues.
	loader.listCh <- listResult{issues: []jira.Issue{
		{Key: "PROJ-1", Summary: "First issue",
			Status:   jira.Status{Name: "To Do", Category: "new"},
			Priority: jira.Priority{Name: "High"}},
		{Key: "PROJ-2", Summary: "Second issue",
			Status:   jira.Status{Name: "To Do", Category: "new"},
			Priority: jira.Priority{Name: "Medium"}},
	}}

	// The list pane should pick up both keys.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("PROJ-1")) && bytes.Contains(b, []byte("PROJ-2"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Navigate from the To Do group header onto PROJ-1 (one ↓), which fires
	// the detail load batch for that key.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Loading description"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Resolve the three detail loads.
	loader.issue("PROJ-1") <- issueResult{issue: jira.Issue{
		Key:         "PROJ-1",
		Summary:     "First issue",
		Description: "Real description for PROJ-1",
		Status:      jira.Status{Name: "To Do", Category: "new"},
		Priority:    jira.Priority{Name: "High"},
	}}
	loader.comments("PROJ-1") <- commentsResult{}
	loader.transitions("PROJ-1") <- transitionsResult{transitions: []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress"}},
	}}

	// Transitions are no longer rendered in the detail pane (the status
	// overlay owns that UI), so the freshness signal is just the description.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Real description for PROJ-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestStage2_CachePaintsBeforeNetwork verifies the spec's "instant startup"
// claim: a populated cache renders before the network fetch resolves.
func TestStage2_CachePaintsBeforeNetwork(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}

	cached := []jira.Issue{
		{Key: "OLD-1", Summary: "Cached issue",
			Status: jira.Status{Name: "To Do", Category: "new"}},
	}
	model := tui.New(palette,
		tui.WithLoader(loader),
		tui.WithInitialIssues(cached),
	)
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(120, 40))

	// Cached issue is in the first frame, before listCh has been touched.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("OLD-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Now resolve the refresh — it replaces the cached row.
	loader.listCh <- listResult{issues: []jira.Issue{
		{Key: "NEW-1", Summary: "Fresh issue",
			Status: jira.Status{Name: "To Do", Category: "new"}},
	}}

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("NEW-1")) && !bytes.Contains(b, []byte("OLD-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestStage2_ListFetchErrorShowsToast verifies that a failed initial load
// surfaces as a toast error rather than crashing the program.
func TestStage2_ListFetchErrorShowsToast(t *testing.T) {
	loader := newStubLoader()
	palette, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatal(err)
	}
	model := tui.New(palette, tui.WithLoader(loader))
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(120, 40))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Issues"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	loader.listCh <- listResult{err: errFetch}

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Refresh failed"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

var errFetch = stubErr("network down")

type stubErr string

func (s stubErr) Error() string { return string(s) }
