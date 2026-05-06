package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/state"
)

// persistAsync runs state.Mutate on a background goroutine and returns a
// tea.Cmd that emits a ToastError on failure. label appears in the toast
// text so users can tell which write failed (e.g. "favorite", "options").
// A nil Cmd is returned when path is empty so callers can chain
// unconditionally.
func persistAsync(path, label string, fn func(*state.State)) tea.Cmd {
	if path == "" {
		return nil
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- state.Mutate(path, fn)
	}()
	return func() tea.Msg {
		if err := <-errCh; err != nil {
			return ToastMsg{
				Text:  "Persist " + label + " failed: " + err.Error(),
				Level: ToastError,
			}
		}
		return nil
	}
}
