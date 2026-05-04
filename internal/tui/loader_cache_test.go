package tui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// countingLoader is a minimal AppLoader stub that records how many times each
// detail-load method is hit. Only the three caching paths (LoadIssue,
// LoadComments, LoadTransitions) and the three mutating paths (DoTransition,
// AddComment, AssignIssue) need real behaviour for these tests; the rest are
// stubbed to satisfy the interface.
type countingLoader struct {
	issueCalls    int32
	commentCalls  int32
	transCalls    int32
	issueOut      jira.Issue
	commentsOut   []jira.Comment
	transitionOut []jira.Transition
}

func (l *countingLoader) LoadIssues(context.Context, panes.ViewKind, string) ([]jira.Issue, error) {
	return nil, nil
}
func (l *countingLoader) LoadIssue(_ context.Context, _ string) (jira.Issue, error) {
	atomic.AddInt32(&l.issueCalls, 1)
	return l.issueOut, nil
}
func (l *countingLoader) LoadComments(_ context.Context, _ string) ([]jira.Comment, error) {
	atomic.AddInt32(&l.commentCalls, 1)
	return l.commentsOut, nil
}
func (l *countingLoader) LoadTransitions(_ context.Context, _ string) ([]jira.Transition, error) {
	atomic.AddInt32(&l.transCalls, 1)
	return l.transitionOut, nil
}
func (l *countingLoader) LoadAttachment(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
}
func (l *countingLoader) DoTransition(context.Context, string, string) error { return nil }
func (l *countingLoader) AddComment(context.Context, string, string) error   { return nil }
func (l *countingLoader) SearchUsers(context.Context, string) ([]jira.User, error) {
	return nil, nil
}
func (l *countingLoader) AssignIssue(context.Context, string, string) error { return nil }
func (l *countingLoader) UpdateFields(context.Context, string, map[string]any) error {
	return nil
}
func (l *countingLoader) CreateLink(context.Context, string, string, string) error {
	return nil
}
func (l *countingLoader) AddWatcher(context.Context, string, string) error    { return nil }
func (l *countingLoader) RemoveWatcher(context.Context, string, string) error { return nil }
func (l *countingLoader) AddWorklog(context.Context, string, string, string) error {
	return nil
}
func (l *countingLoader) GetMyself(context.Context) (jira.User, error) { return jira.User{}, nil }
func (l *countingLoader) Projects(context.Context) ([]jira.Project, error)  { return nil, nil }
func (l *countingLoader) IssueTypesForProject(context.Context, string) ([]jira.IssueType, error) {
	return nil, nil
}
func (l *countingLoader) CreateMeta(context.Context, string, string) (jira.CreateMeta, error) {
	return jira.CreateMeta{}, nil
}
func (l *countingLoader) CreateIssue(context.Context, jira.CreatePayload) (jira.Issue, error) {
	return jira.Issue{}, nil
}

func TestCachingLoader_HitsAndInvalidation(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	inner := &countingLoader{
		issueOut:      jira.Issue{Key: "PROJ-1", Comments: []jira.Comment{{ID: "c1"}}},
		transitionOut: []jira.Transition{{ID: "t1"}},
	}
	cl := newCachingLoaderWithClock(inner, 8, time.Minute, clock)
	ctx := context.Background()

	// First load populates the cache.
	if _, err := cl.LoadIssue(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadIssue: %v", err)
	}
	// Second load is a cache hit; counter must not advance.
	if _, err := cl.LoadIssue(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadIssue 2: %v", err)
	}
	if got := atomic.LoadInt32(&inner.issueCalls); got != 1 {
		t.Fatalf("issueCalls=%d, want 1", got)
	}

	// LoadIssue piggy-backed comments into the cache, so LoadComments is a
	// hit without ever calling the inner loader.
	if _, err := cl.LoadComments(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadComments: %v", err)
	}
	if got := atomic.LoadInt32(&inner.commentCalls); got != 0 {
		t.Fatalf("commentCalls=%d, want 0 after issue prefill", got)
	}

	// Transitions: miss, then hit.
	if _, err := cl.LoadTransitions(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadTransitions: %v", err)
	}
	if _, err := cl.LoadTransitions(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadTransitions 2: %v", err)
	}
	if got := atomic.LoadInt32(&inner.transCalls); got != 1 {
		t.Fatalf("transCalls=%d, want 1", got)
	}

	// Mutation invalidates everything for that key.
	if err := cl.DoTransition(ctx, "PROJ-1", "31"); err != nil {
		t.Fatalf("DoTransition: %v", err)
	}
	if _, err := cl.LoadIssue(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadIssue post-mutation: %v", err)
	}
	if _, err := cl.LoadTransitions(ctx, "PROJ-1"); err != nil {
		t.Fatalf("LoadTransitions post-mutation: %v", err)
	}
	if got := atomic.LoadInt32(&inner.issueCalls); got != 2 {
		t.Fatalf("issueCalls after invalidate=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&inner.transCalls); got != 2 {
		t.Fatalf("transCalls after invalidate=%d, want 2", got)
	}
}

func TestCachingLoader_TTLExpiry(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	inner := &countingLoader{issueOut: jira.Issue{Key: "PROJ-2"}}
	cl := newCachingLoaderWithClock(inner, 4, time.Minute, clock)
	ctx := context.Background()

	if _, err := cl.LoadIssue(ctx, "PROJ-2"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second) // past TTL
	if _, err := cl.LoadIssue(ctx, "PROJ-2"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.issueCalls); got != 2 {
		t.Fatalf("issueCalls=%d, want 2 after TTL expiry", got)
	}
}

func TestCachingLoader_InvalidateAll(t *testing.T) {
	inner := &countingLoader{
		issueOut:      jira.Issue{Key: "PROJ-3"},
		transitionOut: []jira.Transition{{ID: "t1"}},
	}
	cl := newCachingLoaderWithClock(inner, 4, time.Minute, time.Now)
	ctx := context.Background()

	_, _ = cl.LoadIssue(ctx, "PROJ-3")
	_, _ = cl.LoadTransitions(ctx, "PROJ-3")

	cl.InvalidateAll()

	_, _ = cl.LoadIssue(ctx, "PROJ-3")
	_, _ = cl.LoadTransitions(ctx, "PROJ-3")

	if got := atomic.LoadInt32(&inner.issueCalls); got != 2 {
		t.Fatalf("issueCalls=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&inner.transCalls); got != 2 {
		t.Fatalf("transCalls=%d, want 2", got)
	}
}

func TestCachingLoader_PrefetchPopulatesCache(t *testing.T) {
	inner := &countingLoader{issueOut: jira.Issue{Key: "X"}}
	cl := newCachingLoaderWithClock(inner, 4, time.Minute, time.Now)

	cl.PrefetchIssues(context.Background(), []string{"A", "B", "C"})

	if got := atomic.LoadInt32(&inner.issueCalls); got != 3 {
		t.Fatalf("issueCalls after prefetch=%d, want 3", got)
	}
	// Subsequent reads must be hits, not new fetches.
	for _, k := range []string{"A", "B", "C"} {
		if _, err := cl.LoadIssue(context.Background(), k); err != nil {
			t.Fatalf("LoadIssue %s: %v", k, err)
		}
	}
	if got := atomic.LoadInt32(&inner.issueCalls); got != 3 {
		t.Fatalf("issueCalls after reads=%d, want still 3", got)
	}
}

func TestCachingLoader_PrefetchStopsAtCap(t *testing.T) {
	inner := &countingLoader{}
	cl := newCachingLoaderWithClock(inner, 2, time.Minute, time.Now)

	cl.PrefetchIssues(context.Background(), []string{"A", "B", "C", "D"})

	if got := atomic.LoadInt32(&inner.issueCalls); got != 2 {
		t.Fatalf("issueCalls=%d, want 2 (cap)", got)
	}
}

func TestCachingLoader_PrefetchHonoursContextCancel(t *testing.T) {
	inner := &countingLoader{}
	cl := newCachingLoaderWithClock(inner, 64, time.Minute, time.Now)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before any work happens
	cl.PrefetchIssues(ctx, []string{"A", "B", "C"})

	if got := atomic.LoadInt32(&inner.issueCalls); got != 0 {
		t.Fatalf("issueCalls=%d, want 0 after pre-cancel", got)
	}
}

func TestCachingLoader_LRUEviction(t *testing.T) {
	inner := &countingLoader{}
	cl := newCachingLoaderWithClock(inner, 2, time.Minute, time.Now)
	ctx := context.Background()

	for _, k := range []string{"A", "B", "C"} {
		inner.issueOut = jira.Issue{Key: k}
		if _, err := cl.LoadIssue(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	// "A" was the LRU when "C" arrived, so it should have been evicted —
	// reading it again forces a fresh inner call.
	inner.issueOut = jira.Issue{Key: "A"}
	if _, err := cl.LoadIssue(ctx, "A"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&inner.issueCalls); got != 4 {
		t.Fatalf("issueCalls=%d, want 4 (A,B,C,A)", got)
	}
}
