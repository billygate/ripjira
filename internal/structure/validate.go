package structure

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// KnownFields is the whitelist of field names usable in filters and group_by.
// Adding a new field requires both a constant here and a corresponding case
// in the UI's Issue adapter (internal/tui adapter).
var KnownFields = []string{
	"status",
	"status_category",
	"priority",
	"issuetype",
	"assignee",
	"reporter",
	"parent_key",
	"labels",
	"project",
}

// Validate checks the structure for problems that would break evaluation or
// confuse a reader: empty sections, unknown fields, bad regexes, unsupported
// sort directions, etc.
func Validate(s *Structure) error {
	if s.Name == "" {
		return errors.New("structure: name is required")
	}
	if len(s.Sections) == 0 {
		return errors.New("structure: at least one section required")
	}
	for i := range s.Sections {
		if err := validateSection(&s.Sections[i]); err != nil {
			return fmt.Errorf("section %d (%q): %w", i, s.Sections[i].Title, err)
		}
	}
	return nil
}

func validateSection(sec *Section) error {
	if sec.Title == "" {
		return errors.New("title is required")
	}
	if len(sec.Filter) == 0 && len(sec.AnyOf) == 0 && len(sec.GroupBy) == 0 {
		return errors.New("section must have at least filter, any_of, or group_by")
	}
	if err := validateFilter(sec.Filter); err != nil {
		return fmt.Errorf("filter: %w", err)
	}
	for i, alt := range sec.AnyOf {
		if err := validateFilter(alt); err != nil {
			return fmt.Errorf("any_of[%d]: %w", i, err)
		}
	}
	for _, g := range sec.GroupBy {
		if !slices.Contains(KnownFields, g) {
			return fmt.Errorf("group_by: unknown field %q", g)
		}
	}
	if len(sec.OrderBy) > MaxOrderByLen {
		return fmt.Errorf("order_by: too many keys (max %d)", MaxOrderByLen)
	}
	for _, k := range sec.OrderBy {
		if err := validateSortKey(k); err != nil {
			return fmt.Errorf("order_by: %w", err)
		}
	}
	return nil
}

func validateFilter(f SectionFilter) error {
	for field, clause := range f {
		if !slices.Contains(KnownFields, field) {
			return fmt.Errorf("unknown field %q", field)
		}
		if clause.IsEmpty() {
			return fmt.Errorf("field %q: clause has no predicates", field)
		}
		if clause.Regex != "" {
			if _, err := regexp.Compile(clause.Regex); err != nil {
				return fmt.Errorf("field %q: bad regex: %w", field, err)
			}
		}
	}
	return nil
}

func validateSortKey(k SortKey) error {
	switch k.Field {
	case SortFieldPriority, SortFieldUpdated, SortFieldStatus, SortFieldProgress:
	default:
		return fmt.Errorf("unknown sort field %q", k.Field)
	}
	switch k.Dir {
	case SortDirAsc, SortDirDesc:
	default:
		return fmt.Errorf("bad sort direction %q", k.Dir)
	}
	return nil
}
