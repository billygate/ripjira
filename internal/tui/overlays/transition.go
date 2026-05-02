package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// TransitionSelectedMsg is published when the user picks a transition from
// the overlay. The root model handles it by applying an optimistic status
// change and dispatching the network call.
type TransitionSelectedMsg struct {
	IssueKey   string
	Transition jira.Transition
}

// Transition is the `s` overlay listing the workflow transitions available
// for the currently selected issue. A nil/empty transition list still opens
// the overlay so the user gets explicit feedback ("no transitions") instead
// of a silent keypress.
type Transition struct {
	visible      bool
	issueKey     string
	transitions  []jira.Transition
	cursor       int
	closeBinding key.Binding
}

// NewTransition constructs a hidden Transition overlay. The close binding is
// the key that hides the overlay (typically `esc`).
func NewTransition(closeKey key.Binding) Transition {
	return Transition{closeBinding: closeKey}
}

// Visible reports whether the overlay is currently shown.
func (t Transition) Visible() bool { return t.visible }

// Cursor returns the index of the currently highlighted transition.
func (t Transition) Cursor() int { return t.cursor }

// IssueKey returns the issue key the overlay was opened for.
func (t Transition) IssueKey() string { return t.issueKey }

// Transitions returns the list the overlay is currently displaying.
func (t Transition) Transitions() []jira.Transition { return t.transitions }

// Show returns a copy of t bound to the given issue key + transitions, with
// cursor reset to the first row.
func (t Transition) Show(issueKey string, transitions []jira.Transition) Transition {
	t.visible = true
	t.issueKey = issueKey
	t.transitions = append([]jira.Transition(nil), transitions...)
	t.cursor = 0
	return t
}

// Hide returns a copy of t with the overlay closed and state cleared.
func (t Transition) Hide() Transition {
	t.visible = false
	t.issueKey = ""
	t.transitions = nil
	t.cursor = 0
	return t
}

// Update consumes key events while the overlay is visible. Up/Down move the
// cursor, Enter publishes a TransitionSelectedMsg via the returned cmd, the
// configured close key hides the overlay, and any other key is swallowed.
func (t Transition) Update(msg tea.Msg) (Transition, tea.Cmd) {
	if !t.visible {
		return t, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return t, nil
	}
	if key.Matches(k, t.closeBinding) {
		return t.Hide(), nil
	}
	switch k.String() {
	case "up", "k":
		if t.cursor > 0 {
			t.cursor--
		}
		return t, nil
	case "down", "j":
		if t.cursor < len(t.transitions)-1 {
			t.cursor++
		}
		return t, nil
	case "enter":
		if len(t.transitions) == 0 {
			return t, nil
		}
		sel := t.transitions[t.cursor]
		issueKey := t.issueKey
		hidden := t.Hide()
		return hidden, func() tea.Msg {
			return TransitionSelectedMsg{IssueKey: issueKey, Transition: sel}
		}
	}
	return t, nil
}

// View renders the overlay. Returns "" when hidden so the caller can skip
// layout work.
func (t Transition) View(s styles.Styles) string {
	if !t.visible {
		return ""
	}
	titleText := "Transition issue"
	if t.issueKey != "" {
		titleText = "Transition " + t.issueKey
	}
	title := s.OverlayTitle.Render(titleText)

	rows := make([]string, 0, len(t.transitions)+1)
	if len(t.transitions) == 0 {
		rows = append(rows, s.Muted.Render("(no transitions available)"))
	} else {
		for i, tr := range t.transitions {
			line := "→ " + tr.Name
			if tr.To.Name != "" {
				line += "  (" + tr.To.Name + ")"
			}
			if i == t.cursor {
				line = s.ListItemSelected.Render(line)
			} else {
				line = s.ListItem.Render(line)
			}
			rows = append(rows, line)
		}
	}

	hint := s.Muted.Render(
		"enter select    " + t.closeBinding.Help().Key + " " + t.closeBinding.Help().Desc,
	)
	parts := append([]string{title, ""}, rows...)
	parts = append(parts, "", hint)
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}
