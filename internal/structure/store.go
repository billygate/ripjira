package structure

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned by FindByID when no matching structure exists.
var ErrNotFound = errors.New("structure not found")

// Store loads structures from a directory of <PROJECT>.yml files. Each file
// is a YAML list of Structure values. Built-ins are always returned in front
// of user structures.
//
// Store is safe for concurrent use from a single TUI goroutine; callers
// share a Store across the app rather than reconstructing per call.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir. dir does not need to exist;
// missing files are treated as "no user structures", not an error.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir returns the underlying directory (for the watcher and "open in editor"
// affordances in the UI).
func (s *Store) Dir() string { return s.dir }

// Path returns the YAML path for the given project key.
func (s *Store) Path(projectKey string) string {
	return filepath.Join(s.dir, projectKey+".yml")
}

// Load returns built-ins first, then user structures from
// <dir>/<projectKey>.yml. Returns an error if any user structure fails
// validation or the file is malformed YAML.
func (s *Store) Load(projectKey string) ([]Structure, error) {
	out := Builtins(projectKey)

	body, err := os.ReadFile(s.Path(projectKey))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.Path(projectKey), err)
	}

	var user []Structure
	if err := yaml.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.Path(projectKey), err)
	}
	for i := range user {
		user[i].ProjectKey = projectKey
		if err := Validate(&user[i]); err != nil {
			return nil, fmt.Errorf("structure %q: %w", user[i].ID, err)
		}
	}
	out = append(out, user...)
	return out, nil
}

// FindByID returns the structure for the given (project, id) pair or
// ErrNotFound. Built-ins resolve without touching disk for that lookup.
func (s *Store) FindByID(projectKey, id string) (Structure, error) {
	all, err := s.Load(projectKey)
	if err != nil {
		return Structure{}, err
	}
	for i := range all {
		if all[i].ID == id {
			return all[i], nil
		}
	}
	return Structure{}, ErrNotFound
}
