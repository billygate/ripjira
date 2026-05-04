package structure

// Built-in structure IDs are reserved sentinel strings; user structures get
// UUIDs (or any string starting with something else).
const (
	BuiltinDefaultID = "default"
	BuiltinInboxID   = "inbox"

	// sentinelNeverField is a synthetic field name used to make a section
	// unreachable. The Issue adapter returns "" for unknown fields, so a
	// clause requiring Exists:&true on this field never matches.
	sentinelNeverField = "__never"
)

//nolint:gochecknoglobals // module-private constant pointer targets
var (
	builtinTrue  = true
	builtinFalse = false

	defaultOrderBy = []SortKey{
		{Field: SortFieldPriority, Dir: SortDirDesc},
		{Field: SortFieldUpdated, Dir: SortDirDesc},
	}
)

// IsBuiltinID reports whether id refers to a system-provided structure.
func IsBuiltinID(id string) bool {
	return id == BuiltinDefaultID || id == BuiltinInboxID
}

// Builtins returns both system structures resolved for the project.
// The returned slice is freshly constructed; callers may mutate.
func Builtins(projectKey string) []Structure {
	return []Structure{Default(projectKey), Inbox(projectKey)}
}

// Default is "Projects/Backlog/Entry": labelled items vs unlabelled items.
// Without per-project team-field config, Backlog is unreachable (labels
// missing AND a sentinel field exists — which always evaluates to ""), and
// Entry catches the "no labels" bucket. Projects requires labels exist.
func Default(projectKey string) Structure {
	return Structure{
		ID:         BuiltinDefaultID,
		ProjectKey: projectKey,
		Name:       "Default",
		Sections: []Section{
			{
				Title:   "Projects",
				Filter:  SectionFilter{"labels": {Exists: &builtinTrue}},
				OrderBy: defaultOrderBy,
			},
			{
				Title: "Backlog",
				Filter: SectionFilter{
					"labels":           {Exists: &builtinFalse},
					sentinelNeverField: {Exists: &builtinTrue},
				},
				OrderBy: defaultOrderBy,
			},
			{
				Title:   "Entry",
				Filter:  SectionFilter{"labels": {Exists: &builtinFalse}},
				OrderBy: defaultOrderBy,
			},
		},
	}
}

// Inbox surfaces issues that are missing labels (incomplete metadata).
func Inbox(projectKey string) Structure {
	return Structure{
		ID:         BuiltinInboxID,
		ProjectKey: projectKey,
		Name:       "Inbox",
		Sections: []Section{{
			Title:   "Missing labels",
			AnyOf:   []SectionFilter{{"labels": {Exists: &builtinFalse}}},
			OrderBy: defaultOrderBy,
		}},
	}
}
