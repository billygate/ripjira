// Package state persists per-user runtime state (last-selected project,
// future cursors / view preferences). Stored at
// $XDG_STATE_HOME/ripjira/state.json (falling back to
// ~/.local/state/ripjira/state.json). The file is mode 0600.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// mu serializes Load/Save for a given process so concurrent Mutate calls
// (e.g. one tea.Cmd persisting LastProject while another persists grouping)
// don't race or clobber each other's writes.
var mu sync.Mutex

// State is the on-disk shape. Future fields go here.
type State struct {
	LastProject string     `json:"lastProject,omitempty"`
	Grouping    string     `json:"grouping,omitempty"`
	Sort        string     `json:"sort,omitempty"`
	SortDesc    *bool      `json:"sortDesc,omitempty"`
	Favorites      []Favorite        `json:"favorites,omitempty"`
	RecentlyViewed []string          `json:"recentlyViewed,omitempty"`
	CommentDrafts  map[string]string `json:"commentDrafts,omitempty"`

	// LastStructure maps project key → last-selected structure id for that
	// project. Persisted so opening the STRUCTURES tab next session restores
	// the previous view per project.
	LastStructure map[string]string `json:"lastStructure,omitempty"`

	// LastSubView remembers the last sub-view (ViewKind as int) chosen under
	// each top-tab so `}`/`{` returns to the user's previous scope rather
	// than always landing on the first sub-tab. Keys are TopTabKind ints.
	LastSubView map[int]int `json:"lastSubView,omitempty"`
}

// Favorite is a named JQL query the user has saved for re-use. Names are
// shown in the favorites picker; JQL is sent to Jira verbatim.
type Favorite struct {
	Name string `json:"name"`
	JQL  string `json:"jql"`
}

// DefaultPath returns the XDG-aware default location of the state file.
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ripjira", "state.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ripjira", "state.json"), nil
}

// Load reads and returns the State at path. A missing file returns a
// zero-value State and a nil error (first run is not an error).
func Load(path string) (State, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked(path)
}

// Save writes s to path with mode 0600. Parent directories are created
// as needed. Writes are atomic (temp file + rename).
func Save(path string, s State) error {
	mu.Lock()
	defer mu.Unlock()
	return saveLocked(path, s)
}

// Mutate loads the state at path, applies fn, and writes it back atomically
// under a process-wide lock. Safe to call from a tea.Cmd goroutine.
func Mutate(path string, fn func(*State)) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := loadLocked(path)
	if err != nil {
		return err
	}
	fn(&s)
	return saveLocked(path, s)
}

func loadLocked(path string) (State, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is configured by the app
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("state: parse: %w", err)
	}
	return s, nil
}

func saveLocked(path string, s State) error {
	data, err := json.Marshal(&s)
	if err != nil {
		return fmt.Errorf("state: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("state: mkdir: %w", err)
	}
	return atomicWrite(path, data, 0o600)
}

// atomicWrite writes data to path by first writing to a tmp file in the same
// directory and then renaming. On any failure, the tmp file is removed and the
// destination is left untouched.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("state: create tmp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("state: write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("state: sync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("state: close tmp: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("state: chmod tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("state: rename: %w", err)
	}
	return nil
}
