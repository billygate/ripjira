// Package tui contains the Bubble Tea TUI for ripjira.
//
// The disk cache lives here (rather than in internal/jira) because it is a
// presentation-layer concern: the TUI uses it to render an issue list at
// startup before the network refresh completes. Cache contents are bound to
// the user's accountID so a different login invalidates the previous cache.
package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/billygate/ripjira/internal/jira"
)

// CacheVersion is the on-disk schema version. Bump on incompatible changes.
const CacheVersion = 1

// Sentinel errors returned by LoadCache. Callers treat all three as "no
// usable cache, fall back to network" but may distinguish them for logging.
var (
	// ErrCacheMissing is returned when the cache file does not exist.
	ErrCacheMissing = errors.New("cache: file missing")
	// ErrCacheVersion is returned when the on-disk version does not match
	// CacheVersion.
	ErrCacheVersion = errors.New("cache: version mismatch")
	// ErrCacheAccountMismatch is returned when the cache is bound to a
	// different account than the one requested.
	ErrCacheAccountMismatch = errors.New("cache: account mismatch")
)

// cacheFile is the on-disk JSON schema. Issue uses encoding/json's default
// marshaling for time.Time and pointer fields, which round-trips cleanly.
type cacheFile struct {
	Version   int          `json:"version"`
	AccountID string       `json:"account_id"`
	Issues    []jira.Issue `json:"issues"`
}

// DefaultCachePath returns the XDG-aware location of the issues cache file:
// $XDG_CACHE_HOME/ripjira/issues.json, falling back to ~/.cache/ripjira/issues.json.
func DefaultCachePath() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ripjira", "issues.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "ripjira", "issues.json"), nil
}

// LoadCache reads the issue cache at path and returns its contents iff it is
// valid for the given accountID. A missing file, version mismatch, or account
// mismatch returns one of the sentinel errors above; callers should treat
// these as "no cache" rather than fatal.
func LoadCache(path, accountID string) ([]jira.Issue, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is configured by the app
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrCacheMissing
		}
		return nil, err
	}
	var f cacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("cache: parse: %w", err)
	}
	if f.Version != CacheVersion {
		return nil, ErrCacheVersion
	}
	if f.AccountID != accountID {
		return nil, ErrCacheAccountMismatch
	}
	return f.Issues, nil
}

// SaveCache atomically writes the issue list to path, binding it to accountID.
// The implementation writes to a sibling tmp file first and then renames; on
// POSIX filesystems rename is atomic, so a crash mid-write cannot leave a
// half-written issues.json.
func SaveCache(path, accountID string, issues []jira.Issue) error {
	if issues == nil {
		issues = []jira.Issue{}
	}
	f := cacheFile{
		Version:   CacheVersion,
		AccountID: accountID,
		Issues:    issues,
	}
	data, err := json.Marshal(&f)
	if err != nil {
		return fmt.Errorf("cache: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cache: mkdir: %w", err)
	}
	return atomicWrite(path, data, 0o600)
}

// atomicWrite writes data to path by first writing to a tmp file in the same
// directory and then renaming. On any failure, the tmp file is removed and the
// destination is left untouched.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".issues-*.json.tmp")
	if err != nil {
		return fmt.Errorf("cache: create tmp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("cache: write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("cache: sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("cache: close tmp: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("cache: chmod tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("cache: rename: %w", err)
	}
	return nil
}
