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
)

// State is the on-disk shape. Future fields go here.
type State struct {
	LastProject string `json:"lastProject,omitempty"`
	Grouping    string `json:"grouping,omitempty"`
	Sort        string `json:"sort,omitempty"`
	SortDesc    *bool  `json:"sortDesc,omitempty"`
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

// Save writes s to path with mode 0600. Parent directories are created
// as needed. Writes are atomic (temp file + rename).
func Save(path string, s State) error {
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
