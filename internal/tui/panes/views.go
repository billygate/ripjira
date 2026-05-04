package panes

// ViewKind identifies which list of issues is shown in the centre pane.
// The root model holds the active view; the menu pane proposes changes.
// ViewKind lives in `panes` because the menu owns the items semantically
// and because `tui` imports `panes` (not the other way), so locating it
// here avoids an import cycle.
type ViewKind int

// View kinds in the order they appear in the menu pane.
const (
	ViewMyTasks ViewKind = iota
	ViewWatching
	ViewReported
	ViewRecent
	ViewSprint
	ViewMentions
	ViewSearch
	ViewStructures
)

// String returns a stable identifier used in tests, debug logging and
// the menu pane's row label.
func (v ViewKind) String() string {
	switch v {
	case ViewMyTasks:
		return "My Tasks"
	case ViewWatching:
		return "Watching"
	case ViewReported:
		return "Reported"
	case ViewRecent:
		return "Recent"
	case ViewSprint:
		return "Sprint"
	case ViewMentions:
		return "Mentions"
	case ViewSearch:
		return "Search"
	case ViewStructures:
		return "Structures"
	}
	return "?"
}

// TopTabKind groups related ViewKinds under a single top-level tab. The tab
// strip renders one cell per TopTabKind; sub-views become a second row.
type TopTabKind int

// Top-level tabs in display order.
const (
	TopMyIssues TopTabKind = iota
	TopSprint
	TopStructures
	TopSearch
)

// String returns the human label used in the tab strip.
func (t TopTabKind) String() string {
	switch t {
	case TopMyIssues:
		return "MY ISSUES"
	case TopSprint:
		return "SPRINT"
	case TopStructures:
		return "STRUCTURES"
	case TopSearch:
		return "SEARCH"
	}
	return "?"
}

// TopGroup returns the top-level tab a ViewKind belongs to.
func TopGroup(v ViewKind) TopTabKind {
	switch v {
	case ViewSprint:
		return TopSprint
	case ViewStructures:
		return TopStructures
	case ViewSearch:
		return TopSearch
	default:
		// MyTasks, Watching, Reported, Recent, Mentions all live under MY ISSUES.
		return TopMyIssues
	}
}

// SubViews returns the ordered ViewKinds shown as sub-tabs under a top tab.
// Tabs with a single sub get no second row; the slice still has one element
// for callers that need to look up the active sub.
func SubViews(t TopTabKind) []ViewKind {
	switch t {
	case TopMyIssues:
		return []ViewKind{ViewMyTasks, ViewWatching, ViewReported, ViewRecent, ViewMentions}
	case TopSprint:
		return []ViewKind{ViewSprint}
	case TopStructures:
		return []ViewKind{ViewStructures}
	case TopSearch:
		return []ViewKind{ViewSearch}
	}
	return nil
}

// SubLabel returns the short label used in the sub-tab row. Distinct from
// String() so the sub row reads as scope ("ASSIGNED" not "MY TASKS") rather
// than restating the top tab.
func SubLabel(v ViewKind) string {
	switch v {
	case ViewMyTasks:
		return "ASSIGNED"
	case ViewWatching:
		return "WATCHING"
	case ViewReported:
		return "REPORTED"
	case ViewRecent:
		return "RECENT"
	case ViewMentions:
		return "MENTIONS"
	case ViewSprint:
		return "SPRINT"
	case ViewStructures:
		return "STRUCTURES"
	case ViewSearch:
		return "SEARCH"
	}
	return "?"
}

// AllTopTabs returns the top tabs in display order.
func AllTopTabs() []TopTabKind {
	return []TopTabKind{TopMyIssues, TopSprint, TopStructures, TopSearch}
}
