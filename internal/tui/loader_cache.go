package tui

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/billygate/ripjira/internal/jira"
)

// DefaultDetailCacheSize and DefaultDetailCacheTTL bound the in-memory cache
// of per-issue detail loads. The TTL is intentionally short: Jira issues can
// change on the server at any time and we have no invalidation channel, so
// stale-by-up-to-a-minute is the trade for instant back-and-forth navigation
// between recently viewed issues.
const (
	DefaultDetailCacheSize = 64
	DefaultDetailCacheTTL  = 15 * time.Minute
	// DefaultPrefetchConcurrency is the number of in-flight prefetch
	// LoadIssue calls. Jira's per-tenant rate limits make 2 a safe default
	// — enough to overlap network latency, low enough to dodge 429s.
	DefaultPrefetchConcurrency = 2
)

// NewCachingLoader wraps inner with an LRU cache for LoadIssue / LoadComments
// / LoadTransitions. Mutations (DoTransition, AddComment, AssignIssue) drop
// the affected key so the next read re-fetches. List loads, user search, and
// project metadata are not cached — those have their own freshness story
// (auto-refresh tick for the list, debounced typing for user search).
func NewCachingLoader(inner AppLoader) AppLoader {
	return newCachingLoaderWithClock(inner, DefaultDetailCacheSize, DefaultDetailCacheTTL, time.Now)
}

func newCachingLoaderWithClock(inner AppLoader, size int, ttl time.Duration, now func() time.Time) *cachingLoader {
	return &cachingLoader{
		AppLoader:   inner,
		issues:      newLRU[jira.Issue](size, ttl, now),
		comments:    newLRU[[]jira.Comment](size, ttl, now),
		transitions: newLRU[[]jira.Transition](size, ttl, now),
	}
}

type cachingLoader struct {
	AppLoader
	issues      *lru[jira.Issue]
	comments    *lru[[]jira.Comment]
	transitions *lru[[]jira.Transition]

	progressMu     sync.Mutex
	progressDone   int
	progressTotal  int
	progressActive bool
}

func (l *cachingLoader) LoadIssue(ctx context.Context, key string) (jira.Issue, error) {
	if v, ok := l.issues.get(key); ok {
		return v, nil
	}
	v, err := l.AppLoader.LoadIssue(ctx, key)
	if err != nil {
		return v, err
	}
	l.issues.put(key, v)
	// The issue payload already carries comments — populate that cache too so
	// the detail pane's follow-up LoadComments is a hit instead of another
	// round-trip to the same endpoint.
	l.comments.put(key, v.Comments)
	return v, nil
}

func (l *cachingLoader) LoadComments(ctx context.Context, key string) ([]jira.Comment, error) {
	if v, ok := l.comments.get(key); ok {
		return v, nil
	}
	v, err := l.AppLoader.LoadComments(ctx, key)
	if err != nil {
		return v, err
	}
	l.comments.put(key, v)
	return v, nil
}

func (l *cachingLoader) LoadTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	if v, ok := l.transitions.get(key); ok {
		return v, nil
	}
	v, err := l.AppLoader.LoadTransitions(ctx, key)
	if err != nil {
		return v, err
	}
	l.transitions.put(key, v)
	return v, nil
}

func (l *cachingLoader) DoTransition(ctx context.Context, key, transitionID string) error {
	if err := l.AppLoader.DoTransition(ctx, key, transitionID); err != nil {
		return err
	}
	l.invalidate(key)
	return nil
}

func (l *cachingLoader) AddComment(ctx context.Context, key, body string) error {
	if err := l.AppLoader.AddComment(ctx, key, body); err != nil {
		return err
	}
	l.invalidate(key)
	return nil
}

func (l *cachingLoader) AssignIssue(ctx context.Context, key, accountID string) error {
	if err := l.AppLoader.AssignIssue(ctx, key, accountID); err != nil {
		return err
	}
	l.invalidate(key)
	return nil
}

func (l *cachingLoader) SetParent(ctx context.Context, key, parentKey string) error {
	if err := l.AppLoader.SetParent(ctx, key, parentKey); err != nil {
		return err
	}
	l.invalidate(key)
	return nil
}

func (l *cachingLoader) invalidate(key string) {
	l.issues.delete(key)
	l.comments.delete(key)
	l.transitions.delete(key)
}

// InvalidateAll drops every cached issue, comment list, and transitions list.
// The hard-refresh hotkey (`R`) calls this so the next detail load goes back
// to the network instead of serving stale data.
func (l *cachingLoader) InvalidateAll() {
	l.issues.clear()
	l.comments.clear()
	l.transitions.clear()
}

// cacheInvalidator is the optional interface the root model checks for when
// handling the hard-refresh hotkey. Loaders that don't cache can simply not
// implement it.
type cacheInvalidator interface {
	InvalidateAll()
}

// prefetcher is the optional interface the root model uses to warm the
// detail cache as soon as a fresh issue list arrives. Loaders that don't
// cache simply don't implement it and prefetching becomes a no-op.
type prefetcher interface {
	PrefetchIssues(ctx context.Context, keys []string)
}

// prefetchProgressReporter is the optional interface the top bar uses to
// render the warm-up indicator. Reported counts are advisory: callers
// should treat (0, 0, false) as "no prefetch running".
type prefetchProgressReporter interface {
	PrefetchProgress() (done, total int, active bool)
}

// PrefetchIssues warms the cache with up to issues-LRU-capacity entries by
// invoking LoadIssue (and therefore the inner network call) for each key
// that is not yet cached. Honours ctx cancellation between issues so a list
// refresh can supersede an in-flight warm-up. Errors are silently dropped —
// a failed prefetch just means the next user-visible read goes to the
// network like it would have anyway.
//
// Up to DefaultPrefetchConcurrency LoadIssue calls run in parallel; Jira is
// slow per request, so overlapping a couple of requests roughly halves the
// time-to-warm without spiking 429 rates.
func (l *cachingLoader) PrefetchIssues(ctx context.Context, keys []string) {
	capLimit := l.issues.cap
	total := len(keys)
	if capLimit > 0 && total > capLimit {
		total = capLimit
	}
	l.setProgress(0, total, true)
	defer l.setProgress(0, 0, false)

	// Pre-filter cached keys so the worker pool isn't fed no-ops.
	work := make([]string, 0, total)
	for _, k := range keys {
		if capLimit > 0 && len(work) >= capLimit {
			break
		}
		if l.issues.has(k) {
			continue
		}
		work = append(work, k)
	}

	jobs := make(chan string)
	var doneCount int
	var doneMu sync.Mutex
	var wg sync.WaitGroup
	workerCount := min(DefaultPrefetchConcurrency, len(work))
	for range workerCount {
		wg.Go(func() {
			for k := range jobs {
				if ctx.Err() != nil {
					return
				}
				if capLimit > 0 && l.issues.len() >= capLimit {
					return
				}
				if l.issues.has(k) {
					continue
				}
				if _, err := l.LoadIssue(ctx, k); err == nil {
					doneMu.Lock()
					doneCount++
					l.setProgress(doneCount, total, true)
					doneMu.Unlock()
				}
			}
		})
	}
	for _, k := range work {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- k:
		}
	}
	close(jobs)
	wg.Wait()
}

func (l *cachingLoader) setProgress(done, total int, active bool) {
	l.progressMu.Lock()
	l.progressDone = done
	l.progressTotal = total
	l.progressActive = active
	l.progressMu.Unlock()
}

// PrefetchProgress reports the warm-up state for the top-bar indicator. When
// active is false the done/total values should be ignored.
func (l *cachingLoader) PrefetchProgress() (done, total int, active bool) {
	l.progressMu.Lock()
	defer l.progressMu.Unlock()
	return l.progressDone, l.progressTotal, l.progressActive
}

// lru is a tiny mutex-guarded LRU cache with per-entry expiry. Capacity 0 or
// negative TTL disable storage / expiry respectively. Operations are O(1).
type lru[V any] struct {
	mu    sync.Mutex
	cap   int
	ttl   time.Duration
	ll    *list.List
	items map[string]*list.Element
	now   func() time.Time
}

type lruEntry[V any] struct {
	key string
	val V
	exp time.Time
}

func newLRU[V any](capacity int, ttl time.Duration, now func() time.Time) *lru[V] {
	if now == nil {
		now = time.Now
	}
	return &lru[V]{
		cap:   capacity,
		ttl:   ttl,
		ll:    list.New(),
		items: make(map[string]*list.Element),
		now:   now,
	}
}

func (c *lru[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	if c.cap <= 0 {
		return zero, false
	}
	el, ok := c.items[key]
	if !ok {
		return zero, false
	}
	e := el.Value.(*lruEntry[V])
	if c.ttl > 0 && c.now().After(e.exp) {
		c.ll.Remove(el)
		delete(c.items, key)
		return zero, false
	}
	c.ll.MoveToFront(el)
	return e.val, true
}

func (c *lru[V]) put(key string, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cap <= 0 {
		return
	}
	exp := time.Time{}
	if c.ttl > 0 {
		exp = c.now().Add(c.ttl)
	}
	if el, ok := c.items[key]; ok {
		e := el.Value.(*lruEntry[V])
		e.val = val
		e.exp = exp
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruEntry[V]{key: key, val: val, exp: exp})
	c.items[key] = el
	for c.ll.Len() > c.cap {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		c.ll.Remove(oldest)
		delete(c.items, oldest.Value.(*lruEntry[V]).key)
	}
}

func (c *lru[V]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

func (c *lru[V]) has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return false
	}
	if c.ttl > 0 && c.now().After(el.Value.(*lruEntry[V]).exp) {
		return false
	}
	return true
}

func (c *lru[V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.items = make(map[string]*list.Element)
}

func (c *lru[V]) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.Remove(el)
		delete(c.items, key)
	}
}
