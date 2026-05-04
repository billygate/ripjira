package tui

import "github.com/billygate/ripjira/internal/jira"

// epicsLoadedMsg is the result of an async SearchEpics dispatched while the
// epic picker is open. IssueKey is the originating issue so a stale result
// (the user already moved on) can be ignored.
type epicsLoadedMsg struct {
	IssueKey string
	Epics    []jira.Issue
	Err      error
}

// setParentDoneMsg signals completion of an in-flight SetParent. Err is
// non-nil only on failure; the optimistic local update is reverted then.
type setParentDoneMsg struct {
	IssueKey     string
	OldParentKey string
	OldParentSum string
	NewParentKey string
	Err          error
}

// SelectionChangedMsg is published by the app when the list pane's selection
// moves to a different issue. The app reacts by telling the detail pane to
// switch issues, which in turn cancels any in-flight loads and starts new
// ones. Carries the issue key so panes can filter stale follow-up messages.
type SelectionChangedMsg struct {
	Key string
}

// RefreshRequestedMsg is published when the user presses `r` (manual refresh).
// The app re-runs both the list and the open issue's detail loads.
type RefreshRequestedMsg struct{}

// structureChangedMsg fires when the YAML file for a project's structures
// changes on disk. The Update handler invalidates the loaded-structures cache
// and re-arms the watcher Cmd for the next event.
type structureChangedMsg struct{ Project string }

// BackgroundActivityMsg adjusts the count of in-flight background operations.
// The spinner in the top bar is shown when the count is positive. Panes emit
// +1 when they kick off a network request and -1 when it completes.
type BackgroundActivityMsg struct {
	Delta int
}
