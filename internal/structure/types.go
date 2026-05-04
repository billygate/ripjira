// Package structure defines the saved view layer ("structures") that groups
// issues into named sections by filter + group_by. The package is pure: it
// does not import internal/jira or any TUI code, so the same logic can run
// from tests, scripts, or external sync tooling. JSON tags match pilot's
// REST DTOs byte-for-byte so external sync against pilot is a direct file
// drop.
package structure

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// FilterClause is the predicate set for a single field. Predicates AND
// together: a value satisfies the clause when every populated predicate
// accepts it. Wire format is either a JSON array (shorthand for In) or an
// object with any subset of {in, not, regex, contains, exists}.
type FilterClause struct {
	In       []string `json:"in,omitempty" yaml:"in,omitempty"`
	Not      []string `json:"not,omitempty" yaml:"not,omitempty"`
	Regex    string   `json:"regex,omitempty" yaml:"regex,omitempty"`
	Contains string   `json:"contains,omitempty" yaml:"contains,omitempty"`
	Exists   *bool    `json:"exists,omitempty" yaml:"exists,omitempty"`

	compiled *regexp.Regexp `json:"-" yaml:"-"`
}

// In is a convenience constructor.
func In(values ...string) FilterClause { return FilterClause{In: values} }

// IsEmpty reports whether the clause expresses no constraint.
func (c *FilterClause) IsEmpty() bool {
	return len(c.In) == 0 && len(c.Not) == 0 && c.Regex == "" && c.Contains == "" && c.Exists == nil
}

// UnmarshalJSON accepts a JSON array (In shorthand) or a JSON object.
func (c *FilterClause) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '[' {
		var values []string
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("filter clause array: %w", err)
		}
		*c = FilterClause{In: values}
		return nil
	}
	type raw FilterClause
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("filter clause object: %w", err)
	}
	*c = FilterClause(r)
	return nil
}

// MarshalJSON emits the array shorthand when only In is set, else an object.
//
//nolint:gocritic // value receiver intentional
func (c FilterClause) MarshalJSON() ([]byte, error) {
	onlyIn := len(c.Not) == 0 && c.Regex == "" && c.Contains == "" && c.Exists == nil
	if onlyIn {
		if c.In == nil {
			return json.Marshal([]string{})
		}
		return json.Marshal(c.In)
	}
	type wire struct {
		In       []string `json:"in,omitempty"`
		Not      []string `json:"not,omitempty"`
		Regex    string   `json:"regex,omitempty"`
		Contains string   `json:"contains,omitempty"`
		Exists   *bool    `json:"exists,omitempty"`
	}
	return json.Marshal(wire{In: c.In, Not: c.Not, Regex: c.Regex, Contains: c.Contains, Exists: c.Exists})
}

// UnmarshalYAML accepts a YAML sequence (In shorthand) or a YAML mapping.
func (c *FilterClause) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.SequenceNode {
		var values []string
		if err := node.Decode(&values); err != nil {
			return fmt.Errorf("filter clause sequence: %w", err)
		}
		*c = FilterClause{In: values}
		return nil
	}
	type raw FilterClause
	var r raw
	if err := node.Decode(&r); err != nil {
		return fmt.Errorf("filter clause mapping: %w", err)
	}
	*c = FilterClause(r)
	return nil
}

// SectionFilter maps field name → predicate. Across keys: AND.
type SectionFilter map[string]FilterClause

// Section is one block in a Structure.
type Section struct {
	Title   string          `json:"title" yaml:"title"`
	Filter  SectionFilter   `json:"filter,omitempty" yaml:"filter,omitempty"`
	AnyOf   []SectionFilter `json:"any_of,omitempty" yaml:"any_of,omitempty"`
	GroupBy []string        `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	OrderBy []SortKey       `json:"order_by,omitempty" yaml:"order_by,omitempty"`
}

// SortField is a whitelisted sort key for Section.OrderBy.
type SortField string

// SortDir is asc/desc.
type SortDir string

const (
	SortFieldPriority SortField = "priority"
	SortFieldUpdated  SortField = "updated"
	SortFieldStatus   SortField = "status"
	SortFieldProgress SortField = "progress"

	SortDirAsc  SortDir = "asc"
	SortDirDesc SortDir = "desc"

	MaxOrderByLen = 4
)

// SortKey is one tier of OrderBy.
type SortKey struct {
	Field SortField `json:"field" yaml:"field"`
	Dir   SortDir   `json:"dir" yaml:"dir"`
}

// Structure is a saved, project-scoped collection of sections.
//
// Source is set to "pilot" (or any non-empty value) by sync tooling to mark
// the structure read-only in the UI. Local user structures leave it empty.
type Structure struct {
	ID         string    `json:"id" yaml:"id"`
	ProjectKey string    `json:"project_key" yaml:"project_key,omitempty"`
	Name       string    `json:"name" yaml:"name"`
	Sections   []Section `json:"sections" yaml:"sections"`
	Source     string    `json:"source,omitempty" yaml:"source,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// IsReadOnly returns true when the structure originates from an external
// sync source (pilot, etc.) and should not be edited locally.
func (s *Structure) IsReadOnly() bool { return s.Source != "" }
