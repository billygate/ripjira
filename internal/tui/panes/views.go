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
