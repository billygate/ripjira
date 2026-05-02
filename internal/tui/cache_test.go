package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/billygate/ripjira/internal/jira"
)

func sampleIssues() []jira.Issue {
	return []jira.Issue{
		{
			Key:     "PROJ-1",
			Summary: "first",
			Status:  jira.Status{ID: "1", Name: "To Do", Category: "new"},
			Updated: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
			URL:     "https://example.atlassian.net/browse/PROJ-1",
		},
		{
			Key:     "PROJ-2",
			Summary: "second",
			Status:  jira.Status{ID: "2", Name: "In Progress", Category: "indeterminate"},
			Updated: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			URL:     "https://example.atlassian.net/browse/PROJ-2",
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")
	want := sampleIssues()

	if err := SaveCache(path, "acct-1", want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	got, err := LoadCache(path, "acct-1")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Key != want[i].Key {
			t.Errorf("issues[%d].Key: got %q, want %q", i, got[i].Key, want[i].Key)
		}
		if got[i].Summary != want[i].Summary {
			t.Errorf("issues[%d].Summary: got %q, want %q", i, got[i].Summary, want[i].Summary)
		}
		if got[i].Status != want[i].Status {
			t.Errorf("issues[%d].Status: got %+v, want %+v", i, got[i].Status, want[i].Status)
		}
		if !got[i].Updated.Equal(want[i].Updated) {
			t.Errorf("issues[%d].Updated: got %v, want %v", i, got[i].Updated, want[i].Updated)
		}
	}
}

// TestSaveCacheReplacesPriorContents documents the "reconcile by full
// replacement" contract: a subsequent SaveCache fully overwrites the prior
// payload — no merging, no appending. App startup relies on this so the
// background refresh can simply hand the cache the latest server list.
func TestSaveCacheReplacesPriorContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")

	first := sampleIssues()
	if err := SaveCache(path, "acct-1", first); err != nil {
		t.Fatalf("SaveCache first: %v", err)
	}

	replacement := []jira.Issue{
		{
			Key:     "OTHER-9",
			Summary: "fresh from refresh",
			Status:  jira.Status{ID: "3", Name: "Done", Category: "done"},
			Updated: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	if err := SaveCache(path, "acct-1", replacement); err != nil {
		t.Fatalf("SaveCache replacement: %v", err)
	}

	got, err := LoadCache(path, "acct-1")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1 (full replacement)", len(got))
	}
	if got[0].Key != "OTHER-9" {
		t.Errorf("Key: got %q, want OTHER-9", got[0].Key)
	}
}

// TestSaveCacheNilIssues confirms a nil slice is persisted as an empty list,
// so a fresh refresh that returns no issues clears the cache rather than
// leaving a stale one.
func TestSaveCacheNilIssues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")
	if err := SaveCache(path, "acct-1", nil); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, err := LoadCache(path, "acct-1")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len: got %d, want 0", len(got))
	}
}

func TestSaveCacheFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")
	if err := SaveCache(path, "acct-1", sampleIssues()); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm: got %o, want 0600", perm)
	}
}

func TestLoadCacheMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	_, err := LoadCache(path, "acct-1")
	if !errors.Is(err, ErrCacheMissing) {
		t.Errorf("err: got %v, want ErrCacheMissing", err)
	}
}

func TestLoadCacheAccountMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")
	if err := SaveCache(path, "acct-1", sampleIssues()); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	_, err := LoadCache(path, "acct-2")
	if !errors.Is(err, ErrCacheAccountMismatch) {
		t.Errorf("err: got %v, want ErrCacheAccountMismatch", err)
	}
}

func TestLoadCacheVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")
	bogus := struct {
		Version   int          `json:"version"`
		AccountID string       `json:"account_id"`
		Issues    []jira.Issue `json:"issues"`
	}{Version: CacheVersion + 99, AccountID: "acct-1"}
	data, err := json.Marshal(bogus)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = LoadCache(path, "acct-1")
	if !errors.Is(err, ErrCacheVersion) {
		t.Errorf("err: got %v, want ErrCacheVersion", err)
	}
}

func TestLoadCacheCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadCache(path, "acct-1")
	if err == nil {
		t.Fatal("err: got nil, want parse error")
	}
	if errors.Is(err, ErrCacheMissing) || errors.Is(err, ErrCacheVersion) || errors.Is(err, ErrCacheAccountMismatch) {
		t.Errorf("err: got sentinel %v, want generic parse error", err)
	}
}

// TestAtomicWriteSurvivesFailure verifies that when a write fails partway
// through, the destination file is left in its previous good state and no
// stray .tmp files remain in the cache directory.
func TestAtomicWriteSurvivesFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.json")

	// Seed a valid prior cache.
	if err := SaveCache(path, "acct-1", sampleIssues()); err != nil {
		t.Fatalf("SaveCache initial: %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Force the rename target to be an unrenameable destination by replacing
	// it with a directory of the same name. Rename onto an existing
	// non-empty directory fails on POSIX, so atomicWrite must surface an
	// error and clean up its tmp file without touching the directory.
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove seed: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir blocker: %v", err)
	}
	// Drop a sentinel file inside the blocker directory so a successful
	// rename (which would discard the directory) is detectable.
	sentinel := filepath.Join(path, "blocker.txt")
	if err := os.WriteFile(sentinel, original, 0o600); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}

	err = SaveCache(path, "acct-1", sampleIssues())
	if err == nil {
		t.Fatal("SaveCache: got nil error, want failure due to dir blocker")
	}

	// The blocker directory and its sentinel must still be present —
	// proving the destination was not partially overwritten.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after failed write: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("path is no longer a directory; atomic write clobbered the destination")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel missing after failed write: %v", err)
	}

	// No tmp files should be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == "issues.json" {
			continue
		}
		if strings.Contains(name, ".tmp") {
			t.Errorf("orphan tmp file left behind: %s", name)
		}
	}
}

func TestDefaultCachePathRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-fixture")
	got, err := DefaultCachePath()
	if err != nil {
		t.Fatalf("DefaultCachePath: %v", err)
	}
	want := filepath.Join("/tmp/xdg-fixture", "ripjira", "issues.json")
	if got != want {
		t.Errorf("path: got %q, want %q", got, want)
	}
}

func TestDefaultCachePathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	got, err := DefaultCachePath()
	if err != nil {
		t.Fatalf("DefaultCachePath: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".cache", "ripjira", "issues.json")) {
		t.Errorf("path: got %q, want suffix .cache/ripjira/issues.json", got)
	}
}
