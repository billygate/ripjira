package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// PriorityNames is the canonical Jira priority list. Most instances use
// these five names; instances that customise priorities will need a
// different mechanism (a fetched list from /rest/api/3/priority — left
// as future work). Picking from this list is fine for MVP.
var PriorityNames = []string{"Highest", "High", "Medium", "Low", "Lowest"}

// PrioritySelectedMsg is published when the user picks a priority.
type PrioritySelectedMsg struct {
	IssueKey string
	Name     string
}

// Priority is the `P` overlay: a small vertical picker over canonical
// priority names. j/k navigate, Enter picks, Esc cancels.
type Priority struct {
	visible      bool
	issueKey     string
	cursor       int
	current      string
	closeBinding key.Binding
}

// NewPriority builds a hidden overlay.
func NewPriority(closeKey key.Binding) Priority {
	return Priority{closeBinding: closeKey}
}

// Visible reports whether the overlay is shown.
func (p Priority) Visible() bool { return p.visible }

// IssueKey returns the issue the overlay was opened for.
func (p Priority) IssueKey() string { return p.issueKey }

// Cursor returns the highlighted index.
func (p Priority) Cursor() int { return p.cursor }

// Show opens the picker, with the cursor positioned on `current` when it
// matches one of PriorityNames.
func (p Priority) Show(issueKey, current string) Priority {
	p.issueKey = issueKey
	p.current = current
	p.cursor = 0
	for i, name := range PriorityNames {
		if name == current {
			p.cursor = i
			break
		}
	}
	p.visible = true
	return p
}

// Hide returns a copy of p with state cleared.
func (p Priority) Hide() Priority {
	p.visible = false
	p.issueKey = ""
	p.current = ""
	p.cursor = 0
	return p
}

// Update consumes input while visible.
func (p Priority) Update(msg tea.Msg) (Priority, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch k.String() {
	case "j", "down":
		if p.cursor < len(PriorityNames)-1 {
			p.cursor++
		}
		return p, nil
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	case "enter":
		name := PriorityNames[p.cursor]
		issueKey := p.issueKey
		hidden := p.Hide()
		return hidden, func() tea.Msg {
			return PrioritySelectedMsg{IssueKey: issueKey, Name: name}
		}
	}
	if key.Matches(k, p.closeBinding) {
		return p.Hide(), nil
	}
	return p, nil
}

// View renders the picker.
func (p Priority) View(s styles.Styles) string {
	if !p.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Priority · " + p.issueKey)
	rows := make([]string, 0, len(PriorityNames))
	for i, name := range PriorityNames {
		marker := "  "
		if i == p.cursor {
			marker = "▶ "
		}
		row := marker + name
		if name == p.current {
			row += s.Muted.Render("  (current)")
		}
		rows = append(rows, row)
	}
	hint := s.Muted.Render("enter pick    " +
		p.closeBinding.Help().Key + " " + p.closeBinding.Help().Desc)
	parts := []string{title, ""}
	parts = append(parts, rows...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
