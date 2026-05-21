package tui

import (
	"sort"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/tui/overlays"
)

// collectOptionUsage extracts every (fieldID → optionID → count) entry
// from u that pertains to (projectKey, issueTypeID). The result is the
// shape FormDefaults.OptionUsage expects; nil when no history exists.
// Iterating once on the state map is cheap and avoids embedding the
// composite-key shape in the overlay package.
func collectOptionUsage(u *state.CreateUsage, projectKey, issueTypeID string) map[string]map[string]int {
	if u == nil || len(u.Options) == 0 || projectKey == "" || issueTypeID == "" {
		return nil
	}
	prefix := projectKey + "\x00" + issueTypeID + "\x00"
	out := map[string]map[string]int{}
	for k, counts := range u.Options {
		if len(k) <= len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		fieldID := k[len(prefix):]
		// Counts are stored by ID; the field uses option IDs directly when
		// matching schema rows, so we forward the map as-is.
		out[fieldID] = counts
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sortProjectsByUsage stable-sorts in place so high-count projects come
// first, falling back to the original schema order within a count tier.
// Empty u or empty Projects map is a no-op.
func sortProjectsByUsage(projects []jira.Project, u *state.CreateUsage) {
	if u == nil || len(u.Projects) == 0 || len(projects) < 2 {
		return
	}
	counts := u.Projects
	sort.SliceStable(projects, func(i, j int) bool {
		return counts[projects[i].Key] > counts[projects[j].Key]
	})
}

// mostUsedIssueTypeID returns the issue-type id with the highest usage
// count for projectKey, or "" when no history exists. Used as a hint to
// CreateIssueTypesMsg so the overlay's cursor lands on the user's habit
// instead of the static "Task" fallback.
func mostUsedIssueTypeID(u *state.CreateUsage, projectKey string) string {
	if u == nil || projectKey == "" {
		return ""
	}
	counts := u.IssueTypes[projectKey]
	if len(counts) == 0 {
		return ""
	}
	var bestID string
	bestCount := 0
	for id, c := range counts {
		if c > bestCount {
			bestID, bestCount = id, c
		}
	}
	return bestID
}

// sortIssueTypesByUsage stable-sorts in place so high-count types for
// projectKey come first.
func sortIssueTypesByUsage(types []jira.IssueType, u *state.CreateUsage, projectKey string) {
	if u == nil || projectKey == "" || len(types) < 2 {
		return
	}
	counts := u.IssueTypes[projectKey]
	if len(counts) == 0 {
		return
	}
	sort.SliceStable(types, func(i, j int) bool {
		return counts[types[i].ID] > counts[types[j].ID]
	})
}

// recordCreateUsage walks the just-submitted form and bumps the per-user
// counters: one for the project, one for the (project, type) pair, and
// one for every option ID committed in an Option/MultiOption field. The
// caller is expected to schedule a persistAsync immediately after so the
// in-memory mutations land on disk.
func recordCreateUsage(u *state.CreateUsage, projectKey, issueTypeID string, fields []overlays.Field) {
	if u == nil {
		return
	}
	u.BumpProject(projectKey)
	u.BumpIssueType(projectKey, issueTypeID)
	for _, f := range fields {
		switch f.Kind {
		case overlays.FieldKindOption:
			id := f.Value()
			if id != "" {
				u.BumpOption(projectKey, issueTypeID, f.Meta.ID, id)
			}
		case overlays.FieldKindMultiOption:
			for _, opt := range f.Meta.AllowedValues {
				if f.IsMultiSelected(opt.ID) {
					u.BumpOption(projectKey, issueTypeID, f.Meta.ID, opt.ID)
				}
			}
		}
	}
}
