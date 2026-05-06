package panes_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// stubLoader implements panes.Loader with channel-controlled returns so tests
// can pause individual loads, observe the contexts passed in, and verify
// cancellation behaviour.
type stubLoader struct {
	mu       sync.Mutex
	contexts []context.Context

	issueResp       map[string]chan stubIssueResult
	commentsResp    map[string]chan stubCommentsResult
	transitionsResp map[string]chan stubTransitionsResult
}

type stubIssueResult struct {
	issue jira.Issue
	err   error
}
type stubCommentsResult struct {
	comments []jira.Comment
	err      error
}
type stubTransitionsResult struct {
	transitions []jira.Transition
	err         error
}

func newStubLoader() *stubLoader {
	return &stubLoader{
		issueResp:       map[string]chan stubIssueResult{},
		commentsResp:    map[string]chan stubCommentsResult{},
		transitionsResp: map[string]chan stubTransitionsResult{},
	}
}

func (s *stubLoader) issueChan(key string) chan stubIssueResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.issueResp[key]; !ok {
		s.issueResp[key] = make(chan stubIssueResult, 1)
	}
	return s.issueResp[key]
}

func (s *stubLoader) commentsChan(key string) chan stubCommentsResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.commentsResp[key]; !ok {
		s.commentsResp[key] = make(chan stubCommentsResult, 1)
	}
	return s.commentsResp[key]
}

func (s *stubLoader) transitionsChan(key string) chan stubTransitionsResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.transitionsResp[key]; !ok {
		s.transitionsResp[key] = make(chan stubTransitionsResult, 1)
	}
	return s.transitionsResp[key]
}

func (s *stubLoader) recordCtx(ctx context.Context) {
	s.mu.Lock()
	s.contexts = append(s.contexts, ctx)
	s.mu.Unlock()
}

func (s *stubLoader) LoadIssue(ctx context.Context, key string) (jira.Issue, error) {
	s.recordCtx(ctx)
	ch := s.issueChan(key)
	select {
	case r := <-ch:
		return r.issue, r.err
	case <-ctx.Done():
		return jira.Issue{}, ctx.Err()
	}
}

func (s *stubLoader) LoadComments(ctx context.Context, key string) ([]jira.Comment, error) {
	s.recordCtx(ctx)
	ch := s.commentsChan(key)
	select {
	case r := <-ch:
		return r.comments, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *stubLoader) LoadTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	s.recordCtx(ctx)
	ch := s.transitionsChan(key)
	select {
	case r := <-ch:
		return r.transitions, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *stubLoader) LoadAttachment(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}

func newDetail(t *testing.T, loader panes.Loader) panes.Detail {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	return panes.NewDetail(styles.New(p), loader, 80, 30)
}

func sampleHeader() *jira.Issue {
	return &jira.Issue{
		Key:      "PROJ-7",
		Summary:  "Header summary from list",
		Status:   jira.Status{Name: "In Progress", Category: "indeterminate"},
		Priority: jira.Priority{Name: "High"},
		Assignee: &jira.User{DisplayName: "Alice"},
	}
}

func TestNewDetail_NoIssueShowsPlaceholder(t *testing.T) {
	d := newDetail(t, newStubLoader())
	if d.Issue() != nil {
		t.Errorf("Issue() = %v, want nil", d.Issue())
	}
	out := stripANSI(d.View())
	if !strings.Contains(out, "No issue selected.") {
		t.Errorf("view missing placeholder:\n%s", out)
	}
}

func TestSetIssue_RendersHeaderAndSkeletons(t *testing.T) {
	d := newDetail(t, newStubLoader())
	cmd := d.SetIssue(sampleHeader())
	if cmd == nil {
		t.Fatal("SetIssue returned nil cmd, want batch of three loads")
	}
	if d.Issue() == nil || d.Issue().Key != "PROJ-7" {
		t.Errorf("Issue() = %v, want PROJ-7", d.Issue())
	}
	out := stripANSI(d.View())
	for _, want := range []string{
		"PROJ-7", "Header summary from list", "In Progress", "Alice",
		"── Description ──", "Loading description",
		"── Comments (0) ──", "Loading comments",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n%s", want, out)
		}
	}
}

func TestUpdate_AppliesLoadedSections(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(sampleHeader())
	tok := d.Token()

	full := jira.Issue{
		Key:         "PROJ-7",
		Summary:     "Full summary",
		Description: "A full plain-text description.",
		Status:      jira.Status{Name: "In Progress", Category: "indeterminate"},
		Priority:    jira.Priority{Name: "High"},
	}
	d, _ = d.Update(panes.IssueLoadedMsg{Key: "PROJ-7", Token: tok, Issue: full})

	created, _ := time.Parse(time.RFC3339, "2026-04-29T12:00:00Z")
	d, _ = d.Update(panes.CommentsLoadedMsg{Key: "PROJ-7", Token: tok, Comments: []jira.Comment{
		{Author: jira.User{DisplayName: "Bob"}, Body: "First comment", Created: created},
		{Author: jira.User{DisplayName: "Carol"}, Body: "Second comment", Created: created},
	}})

	d, _ = d.Update(panes.TransitionsLoadedMsg{Key: "PROJ-7", Token: tok, Transitions: []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress"}},
		{ID: "21", Name: "Resolve", To: jira.Status{Name: "Done"}},
	}})

	out := stripANSI(d.View())
	for _, want := range []string{
		"A full plain-text description.",
		"── Comments (2) ──",
		"Bob", "First comment", "Carol", "Second comment",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n%s", want, out)
		}
	}
	// Transitions are loaded silently for the status overlay, so they must
	// NOT appear in the detail pane's rendered output.
	for _, unwanted := range []string{
		"Loading description", "Loading comments", "Loading transitions",
		"→ Start", "→ Resolve", "Activity",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("view still contains %q after load:\n%s", unwanted, out)
		}
	}
	if got := len(d.Transitions()); got != 2 {
		t.Errorf("Transitions() len=%d, want 2 (still tracked for overlay)", got)
	}
}

func TestUpdate_ErrorShownPerSection(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(sampleHeader())
	tok := d.Token()

	d, _ = d.Update(panes.IssueLoadedMsg{Key: "PROJ-7", Token: tok, Err: errors.New("boom-issue")})
	d, _ = d.Update(panes.CommentsLoadedMsg{Key: "PROJ-7", Token: tok, Err: errors.New("boom-comments")})
	d, _ = d.Update(panes.TransitionsLoadedMsg{Key: "PROJ-7", Token: tok, Err: errors.New("boom-transitions")})

	out := stripANSI(d.View())
	// Transition errors are not rendered here — the overlay surfaces them
	// (or falls back to "(no transitions available)") when the user opens it.
	for _, want := range []string{"boom-issue", "boom-comments"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing error %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "boom-transitions") {
		t.Errorf("transition error must not render in detail pane:\n%s", out)
	}
}

func TestUpdate_StaleMessageIgnored(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(sampleHeader())
	staleToken := d.Token()

	other := &jira.Issue{Key: "PROJ-99", Summary: "different"}
	d.SetIssue(other)

	// Deliver a message bound to the stale token + old key.
	d, _ = d.Update(panes.IssueLoadedMsg{
		Key:   "PROJ-7",
		Token: staleToken,
		Issue: jira.Issue{Key: "PROJ-7", Description: "STALE-DATA"},
	})
	out := stripANSI(d.View())
	if strings.Contains(out, "STALE-DATA") {
		t.Errorf("stale token leaked into view:\n%s", out)
	}
	if !strings.Contains(out, "Loading description") {
		t.Errorf("expected description still loading for new issue:\n%s", out)
	}

	// Also: a message with the right token but wrong key is ignored.
	d, _ = d.Update(panes.IssueLoadedMsg{
		Key:   "PROJ-7",
		Token: d.Token(),
		Issue: jira.Issue{Key: "PROJ-7", Description: "STALE-KEY"},
	})
	out = stripANSI(d.View())
	if strings.Contains(out, "STALE-KEY") {
		t.Errorf("wrong-key message leaked into view:\n%s", out)
	}
}

func TestSetIssue_CancelsPriorContext(t *testing.T) {
	loader := newStubLoader()
	d := newDetail(t, loader)
	cmdA := d.SetIssue(sampleHeader())

	// Run the batch's commands as the bubbletea runtime would.
	results := runBatch(t, cmdA)

	// Switch issue — must cancel A's contexts.
	other := &jira.Issue{Key: "PROJ-99", Summary: "different"}
	d.SetIssue(other)

	// The three blocked goroutines should now return ctx.Canceled.
	for i := range 3 {
		select {
		case msg := <-results:
			if !cmdReturnedCancellation(msg) {
				t.Errorf("expected cancellation message, got %#v", msg)
			}
		case <-time.After(time.Second):
			t.Fatalf("load %d did not return after SetIssue cancel", i)
		}
	}

	// And the recorded contexts for the first issue are all Done.
	loader.mu.Lock()
	ctxs := append([]context.Context(nil), loader.contexts[:3]...)
	loader.mu.Unlock()
	for i, ctx := range ctxs {
		select {
		case <-ctx.Done():
		default:
			t.Errorf("context %d not cancelled", i)
		}
	}
}

func TestSetIssue_NilClearsState(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(sampleHeader())
	cmd := d.SetIssue(nil)
	if cmd != nil {
		t.Errorf("SetIssue(nil) returned cmd %v, want nil", cmd)
	}
	if d.Issue() != nil {
		t.Errorf("Issue() = %v after nil set, want nil", d.Issue())
	}
	if !strings.Contains(stripANSI(d.View()), "No issue selected.") {
		t.Errorf("view missing placeholder after clear:\n%s", d.View())
	}
}

// setIssueMsg is the wrapper-level command sent by teatest to ask the Detail
// pane to switch issues. The wrapper's Update calls SetIssue and bubbles up
// the resulting load batch.
type setIssueMsg struct{ issue *jira.Issue }

// detailWrap embeds a panes.Detail so it can be driven by teatest. The
// wrapper is the simplest tea.Model that can route into Detail.Update + the
// custom setIssueMsg trigger.
type detailWrap struct{ d panes.Detail }

func (w detailWrap) Init() tea.Cmd { return w.d.Init() }
func (w detailWrap) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(setIssueMsg); ok {
		cmd := w.d.SetIssue(m.issue)
		return w, cmd
	}
	var cmd tea.Cmd
	w.d, cmd = w.d.Update(msg)
	return w, cmd
}
func (w detailWrap) View() string { return w.d.View() }

func TestTeatest_AsyncLoadAndStaleCancellation(t *testing.T) {
	loader := newStubLoader()
	d := newDetail(t, loader)
	tm := teatest.NewTestModel(t, detailWrap{d: d},
		teatest.WithInitialTermSize(80, 30))

	first := sampleHeader()
	tm.Send(setIssueMsg{issue: first})

	// Skeleton frame appears before any load resolves.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Loading description"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(10*time.Millisecond))

	// Resolve all three loads with fresh data.
	loader.issueChan("PROJ-7") <- stubIssueResult{issue: jira.Issue{
		Key:         "PROJ-7",
		Description: "Real description loaded async",
		Status:      jira.Status{Name: "In Progress"},
	}}
	loader.commentsChan("PROJ-7") <- stubCommentsResult{}
	loader.transitionsChan("PROJ-7") <- stubTransitionsResult{transitions: []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress"}},
	}}

	// The detail pane no longer renders transitions ("Activity" section was
	// removed in favour of the status overlay), so the freshness signal is
	// just the description landing.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Real description loaded async"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(10*time.Millisecond))

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestDetail_NumberJumpsToNthComment(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(sampleHeader())
	tok := d.Token()

	// Resolve issue + comments + transitions so the comments section renders.
	d, _ = d.Update(panes.IssueLoadedMsg{Key: "PROJ-7", Token: tok, Issue: jira.Issue{
		Key: "PROJ-7", Description: strings.Repeat("padding line\n\n", 50),
	}})
	d, _ = d.Update(panes.CommentsLoadedMsg{Key: "PROJ-7", Token: tok, Comments: []jira.Comment{
		{Author: jira.User{DisplayName: "Bob"}, Body: "FIRST_COMMENT_BODY"},
		{Author: jira.User{DisplayName: "Carol"}, Body: "SECOND_COMMENT_BODY"},
		{Author: jira.User{DisplayName: "Dave"}, Body: "THIRD_COMMENT_BODY"},
	}})
	d, _ = d.Update(panes.TransitionsLoadedMsg{Key: "PROJ-7", Token: tok})

	// Number key 2 should scroll the viewport to the second comment.
	before := stripANSI(d.View())
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	after := stripANSI(d.View())
	if before == after {
		t.Fatalf("viewport did not scroll on '2':\n%s", after)
	}
	if !strings.Contains(after, "SECOND_COMMENT_BODY") {
		t.Fatalf("second comment not visible after jump:\n%s", after)
	}

	// Out-of-range number should be a no-op (does not panic, view unchanged).
	snapshot := d.View()
	d, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	if d.View() != snapshot {
		t.Errorf("out-of-range number changed view")
	}
}

// runBatch spawns each leaf cmd of a tea.Batch into its own goroutine,
// streaming the produced messages into the returned channel. Mirrors what
// the bubbletea runtime does for tea.Batch.
func runBatch(t *testing.T, cmd tea.Cmd) chan tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, 16)
	if cmd == nil {
		close(out)
		return out
	}
	msg := cmd()
	bm, ok := msg.(tea.BatchMsg)
	if !ok {
		// Single command — just deliver.
		out <- msg
		return out
	}
	for _, c := range bm {
		go func() { out <- c() }()
	}
	return out
}

// cmdReturnedCancellation reports whether a load message's Err is the
// context.Canceled error.
func cmdReturnedCancellation(msg tea.Msg) bool {
	switch m := msg.(type) {
	case panes.IssueLoadedMsg:
		return errors.Is(m.Err, context.Canceled)
	case panes.CommentsLoadedMsg:
		return errors.Is(m.Err, context.Canceled)
	case panes.TransitionsLoadedMsg:
		return errors.Is(m.Err, context.Canceled)
	}
	return false
}

func TestDetail_RendersSubtasksSection(t *testing.T) {
	d := newDetail(t, nil)
	issue := jira.Issue{
		Key:     "PROJ-100",
		Summary: "Parent issue",
		Status:  jira.Status{Name: "In Progress", Category: "indeterminate"},
		Subtasks: []jira.SubtaskRef{
			{Key: "PROJ-200", Summary: "First child", Status: jira.Status{Name: "To Do", Category: "new"}},
			{Key: "PROJ-201", Summary: "Second child", Status: jira.Status{Name: "Done", Category: "done"}},
		},
	}
	d.SetIssue(&issue)
	out := stripANSI(d.View())
	if !strings.Contains(out, "Subtasks (2)") {
		t.Errorf("View missing Subtasks header; got:\n%s", out)
	}
	if !strings.Contains(out, "PROJ-200") || !strings.Contains(out, "First child") {
		t.Errorf("View missing subtask 1; got:\n%s", out)
	}
	if !strings.Contains(out, "PROJ-201") || !strings.Contains(out, "Second child") {
		t.Errorf("View missing subtask 2; got:\n%s", out)
	}
}

func TestDetail_RendersSubtasksPlaceholderWhenEmpty(t *testing.T) {
	d := newDetail(t, nil)
	issue := jira.Issue{
		Key:     "PROJ-100",
		Summary: "Parent",
		Status:  jira.Status{Name: "To Do", Category: "new"},
	}
	d.SetIssue(&issue)
	out := stripANSI(d.View())
	if !strings.Contains(out, "Subtasks (0)") {
		t.Errorf("View missing Subtasks (0) header; got:\n%s", out)
	}
	if !strings.Contains(out, "press S to create one") {
		t.Errorf("View missing empty-state hint; got:\n%s", out)
	}
}

func TestDetail_AppendSubtaskAddsRow(t *testing.T) {
	d := newDetail(t, nil)
	parent := jira.Issue{Key: "PROJ-100", Summary: "Parent"}
	d.SetIssue(&parent)
	if ok := d.AppendSubtask("PROJ-100", jira.SubtaskRef{
		Key:     "PROJ-200",
		Summary: "added",
		Status:  jira.Status{Name: "To Do"},
	}); !ok {
		t.Fatal("AppendSubtask returned false")
	}
	out := stripANSI(d.View())
	if !strings.Contains(out, "PROJ-200") || !strings.Contains(out, "added") {
		t.Errorf("subtask row missing; got:\n%s", out)
	}
}

func TestDetail_AppendSubtaskRejectsKeyMismatch(t *testing.T) {
	d := newDetail(t, nil)
	d.SetIssue(&jira.Issue{Key: "PROJ-100"})
	if ok := d.AppendSubtask("PROJ-999", jira.SubtaskRef{Key: "X-1"}); ok {
		t.Fatal("AppendSubtask should have rejected mismatched key")
	}
}

func TestDetail_RendersEpicWithSummary(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(&jira.Issue{
		Key:           "BILLING-1",
		Summary:       "task",
		ParentKey:     "BILLING-100",
		ParentSummary: "Setup deploy",
	})
	view := stripANSI(d.View())
	if !strings.Contains(view, "BILLING-100") || !strings.Contains(view, "Setup deploy") {
		t.Fatalf("expected epic key + summary, got:\n%s", view)
	}
	if !strings.Contains(view, "Epic") {
		t.Fatalf("expected Epic label, got:\n%s", view)
	}
}

func TestDetail_RendersEpicKeyOnlyWhenSummaryEmpty(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(&jira.Issue{Key: "BILLING-1", ParentKey: "BILLING-200"})
	view := stripANSI(d.View())
	if !strings.Contains(view, "BILLING-200") {
		t.Fatalf("expected key, got:\n%s", view)
	}
	if strings.Contains(view, "Setup deploy") {
		t.Fatalf("should not invent a summary, got:\n%s", view)
	}
}

func TestDetail_NoEpicRowWhenAbsent(t *testing.T) {
	d := newDetail(t, newStubLoader())
	d.SetIssue(&jira.Issue{Key: "BILLING-1", ParentKey: ""})
	view := stripANSI(d.View())
	if strings.Contains(view, "Epic") {
		t.Fatalf("did not expect Epic row, got:\n%s", view)
	}
}

func TestDetail_ReloadKeepsContentAndBumpsToken(t *testing.T) {
	loader := newStubLoader()
	d := newDetail(t, loader)
	d.SetIssue(sampleHeader())
	tok1 := d.Token()
	d, _ = d.Update(panes.IssueLoadedMsg{Key: "PROJ-7", Token: tok1, Issue: jira.Issue{
		Key:     "PROJ-7",
		Summary: "Full summary",
	}})
	d, _ = d.Update(panes.CommentsLoadedMsg{Key: "PROJ-7", Token: tok1, Comments: []jira.Comment{
		{Author: jira.User{DisplayName: "Bob"}, Body: "First comment"},
	}})
	d, _ = d.Update(panes.TransitionsLoadedMsg{Key: "PROJ-7", Token: tok1, Transitions: []jira.Transition{
		{ID: "11", Name: "Start", To: jira.Status{Name: "In Progress"}},
	}})

	cmd := d.Reload()
	if cmd == nil {
		t.Fatal("Reload returned nil cmd")
	}
	if got := d.Token(); got == tok1 {
		t.Errorf("token did not bump after Reload (still %d)", got)
	}
	if len(d.Transitions()) != 1 {
		t.Errorf("Reload must not clear current transitions; got %d", len(d.Transitions()))
	}
	out := stripANSI(d.View())
	if !strings.Contains(out, "Bob") || !strings.Contains(out, "First comment") {
		t.Errorf("Reload should preserve current view until fresh data arrives:\n%s", out)
	}
}

func TestDetail_ReloadNoIssueIsNoop(t *testing.T) {
	d := newDetail(t, newStubLoader())
	if cmd := d.Reload(); cmd != nil {
		t.Errorf("Reload with no issue should return nil cmd, got %v", cmd)
	}
}
