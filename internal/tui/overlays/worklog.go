package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// WorklogSubmittedMsg is published when the user confirms a worklog via
// Ctrl+S. The root model dispatches the AddWorklog network call.
type WorklogSubmittedMsg struct {
	IssueKey  string
	TimeSpent string
	Comment   string
}

// Worklog is the `t` overlay: a one-line text input for the time spent
// (Jira format, e.g. "1h 30m") plus a multi-line textarea for an
// optional comment. Tab cycles between the two; Ctrl+S submits.
type Worklog struct {
	visible      bool
	issueKey     string
	timeInput    textinput.Model
	commentArea  textarea.Model
	focusOnTime  bool
	closeBinding key.Binding
}

// NewWorklog builds a hidden Worklog overlay. closeKey hides the overlay
// (typically Esc).
func NewWorklog(closeKey key.Binding) Worklog {
	t := textinput.New()
	t.Prompt = "time> "
	t.Placeholder = "1h 30m"
	t.CharLimit = 30
	t.Width = 30

	a := textarea.New()
	a.Placeholder = "Comment (optional)…"
	a.Prompt = ""
	a.ShowLineNumbers = false
	a.SetWidth(60)
	a.SetHeight(4)

	return Worklog{
		timeInput:    t,
		commentArea:  a,
		focusOnTime:  true,
		closeBinding: closeKey,
	}
}

// Visible reports whether the overlay is currently shown.
func (w Worklog) Visible() bool { return w.visible }

// IssueKey returns the issue key the overlay was opened for.
func (w Worklog) IssueKey() string { return w.issueKey }

// TimeSpent returns the current time-input value (mostly for tests).
func (w Worklog) TimeSpent() string { return w.timeInput.Value() }

// Show binds w to issueKey with empty inputs and time-input focus.
func (w Worklog) Show(issueKey string) (Worklog, tea.Cmd) {
	w.issueKey = issueKey
	w.visible = true
	w.timeInput.SetValue("")
	w.commentArea.Reset()
	w.focusOnTime = true
	w.commentArea.Blur()
	return w, w.timeInput.Focus()
}

// Hide returns a copy of w with the overlay closed and state cleared.
func (w Worklog) Hide() Worklog {
	w.visible = false
	w.issueKey = ""
	w.timeInput.SetValue("")
	w.commentArea.Reset()
	w.timeInput.Blur()
	w.commentArea.Blur()
	return w
}

// Update consumes input while the overlay is visible. Tab/Shift+Tab
// switches focus; Ctrl+S submits when timeSpent is non-empty.
func (w Worklog) Update(msg tea.Msg) (Worklog, tea.Cmd) {
	if !w.visible {
		return w, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+s":
			ts := strings.TrimSpace(w.timeInput.Value())
			if ts == "" {
				return w, nil
			}
			cm := strings.TrimSpace(w.commentArea.Value())
			issueKey := w.issueKey
			hidden := w.Hide()
			return hidden, func() tea.Msg {
				return WorklogSubmittedMsg{IssueKey: issueKey, TimeSpent: ts, Comment: cm}
			}
		case "tab", "shift+tab":
			w.focusOnTime = !w.focusOnTime
			if w.focusOnTime {
				w.commentArea.Blur()
				return w, w.timeInput.Focus()
			}
			w.timeInput.Blur()
			return w, w.commentArea.Focus()
		}
		if key.Matches(k, w.closeBinding) {
			return w.Hide(), nil
		}
	}
	if w.focusOnTime {
		var cmd tea.Cmd
		w.timeInput, cmd = w.timeInput.Update(msg)
		return w, cmd
	}
	var cmd tea.Cmd
	w.commentArea, cmd = w.commentArea.Update(msg)
	return w, cmd
}

// View renders the overlay.
func (w Worklog) View(s styles.Styles) string {
	if !w.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Log work · " + w.issueKey)
	hint := s.Muted.Render(
		"ctrl+s submit    tab switch field    " +
			w.closeBinding.Help().Key + " " + w.closeBinding.Help().Desc +
			"    format: 1h 30m / 2d / 45m",
	)
	parts := []string{title, "", w.timeInput.View(), "", w.commentArea.View(), "", hint}
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
