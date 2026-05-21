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
	LastProject    string            `json:"lastProject,omitempty"`
	Grouping       string            `json:"grouping,omitempty"`
	Sort           string            `json:"sort,omitempty"`
	SortDesc       *bool             `json:"sortDesc,omitempty"`
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

	// LastView is the ViewKind active at exit. The next session boots into
	// it instead of the zero-value ViewMyTasks.
	LastView *int `json:"lastView,omitempty"`

	// EditorAdviceShown is flipped to true the first time the app boots
	// after this feature ships. Used to gate the one-shot first-launch
	// editor-availability tip.
	EditorAdviceShown bool `json:"editorAdviceShown,omitempty"`

	// CreateUsage tracks how often each project / issue-type / option has
	// been chosen in the create wizard so frequently-used entries bubble
	// to the top of pickers and become the cursor default on next open.
	CreateUsage *CreateUsage `json:"createUsage,omitempty"`
}

// CreateUsage holds per-user usage counters for the create wizard. All
// maps are lazily allocated by Bump; callers should treat a nil receiver
// as "no history" (Count returns 0).
type CreateUsage struct {
	// Projects maps project key → use count.
	Projects map[string]int `json:"projects,omitempty"`
	// IssueTypes maps project key → (issue type id → use count). Issue
	// types are project-scoped: BILLING.Task and OPS.Task accumulate
	// independently because their meaning may differ.
	IssueTypes map[string]map[string]int `json:"issueTypes,omitempty"`
	// Options maps (project, type, field) composite key → (option id →
	// use count). The composite is built by optionUsageKey so callers
	// don't have to remember the separator shape.
	Options map[string]map[string]int `json:"options,omitempty"`
}

// optionUsageKey builds the composite key used by CreateUsage.Options.
// The NUL separator avoids collisions between identifier characters that
// would otherwise look the same when concatenated naively.
func optionUsageKey(projectKey, issueTypeID, fieldID string) string {
	return projectKey + "\x00" + issueTypeID + "\x00" + fieldID
}

// BumpProject increments the usage counter for project. Allocates the
// underlying map on first call.
func (u *CreateUsage) BumpProject(key string) {
	if u == nil || key == "" {
		return
	}
	if u.Projects == nil {
		u.Projects = map[string]int{}
	}
	u.Projects[key]++
}

// BumpIssueType increments the usage counter for (project, type).
func (u *CreateUsage) BumpIssueType(projectKey, typeID string) {
	if u == nil || projectKey == "" || typeID == "" {
		return
	}
	if u.IssueTypes == nil {
		u.IssueTypes = map[string]map[string]int{}
	}
	if u.IssueTypes[projectKey] == nil {
		u.IssueTypes[projectKey] = map[string]int{}
	}
	u.IssueTypes[projectKey][typeID]++
}

// BumpOption increments the usage counter for one option in one field.
func (u *CreateUsage) BumpOption(projectKey, typeID, fieldID, optionID string) {
	if u == nil || projectKey == "" || typeID == "" || fieldID == "" || optionID == "" {
		return
	}
	k := optionUsageKey(projectKey, typeID, fieldID)
	if u.Options == nil {
		u.Options = map[string]map[string]int{}
	}
	if u.Options[k] == nil {
		u.Options[k] = map[string]int{}
	}
	u.Options[k][optionID]++
}

// ProjectCount returns the usage count for the given project key, or 0
// when no history exists (including when u is nil).
func (u *CreateUsage) ProjectCount(key string) int {
	if u == nil {
		return 0
	}
	return u.Projects[key]
}

// IssueTypeCount returns the usage count for (project, type).
func (u *CreateUsage) IssueTypeCount(projectKey, typeID string) int {
	if u == nil || u.IssueTypes == nil {
		return 0
	}
	return u.IssueTypes[projectKey][typeID]
}

// OptionCounts returns the option-id → count map for (project, type,
// field), or nil when no history exists. Callers should treat nil as
// "no usage data" and not mutate the returned map.
func (u *CreateUsage) OptionCounts(projectKey, typeID, fieldID string) map[string]int {
	if u == nil || u.Options == nil {
		return nil
	}
	return u.Options[optionUsageKey(projectKey, typeID, fieldID)]
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
