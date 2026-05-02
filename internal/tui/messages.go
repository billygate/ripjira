package tui

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

// BackgroundActivityMsg adjusts the count of in-flight background operations.
// The spinner in the top bar is shown when the count is positive. Panes emit
// +1 when they kick off a network request and -1 when it completes.
type BackgroundActivityMsg struct {
	Delta int
}
