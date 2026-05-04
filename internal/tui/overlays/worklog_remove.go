package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// WorklogEntry is the read-only view of a worklog shown in the picker.
type WorklogEntry struct {
	ID        string
	TimeSpent string
	Author    string
	When      string // formatted "2006-01-02"
}

// WorklogDeletedMsg is published when the user picks an entry to delete.
type WorklogDeletedMsg struct {
	IssueKey  string
	WorklogID string
}

// RemoveWorklog is the `T` overlay: a vertical picker over the current
// issue's worklogs. j/k navigate, Enter deletes, Esc cancels.
type RemoveWorklog struct {
	visible      bool
	issueKey     string
	entries      []WorklogEntry
	cursor       int
	closeBinding key.Binding
}

// NewRemoveWorklog builds a hidden overlay.
func NewRemoveWorklog(closeKey key.Binding) RemoveWorklog {
	return RemoveWorklog{closeBinding: closeKey}
}

// Visible reports whether the overlay is shown.
func (r RemoveWorklog) Visible() bool { return r.visible }

// IssueKey returns the issue the overlay was opened for.
func (r RemoveWorklog) IssueKey() string { return r.issueKey }

// Cursor returns the highlighted index.
func (r RemoveWorklog) Cursor() int { return r.cursor }

// Show opens the picker scoped to issueKey, seeded with entries.
func (r RemoveWorklog) Show(issueKey string, entries []WorklogEntry) RemoveWorklog {
	r.issueKey = issueKey
	r.entries = append([]WorklogEntry(nil), entries...)
	r.cursor = 0
	r.visible = true
	return r
}

// Hide returns a copy of r with state cleared.
func (r RemoveWorklog) Hide() RemoveWorklog {
	r.visible = false
	r.issueKey = ""
	r.entries = nil
	r.cursor = 0
	return r
}

// Update consumes input while visible.
func (r RemoveWorklog) Update(msg tea.Msg) (RemoveWorklog, tea.Cmd) {
	if !r.visible {
		return r, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return r, nil
	}
	switch k.String() {
	case "j", "down":
		if len(r.entries) > 0 && r.cursor < len(r.entries)-1 {
			r.cursor++
		}
		return r, nil
	case "k", "up":
		if r.cursor > 0 {
			r.cursor--
		}
		return r, nil
	case "enter":
		if len(r.entries) == 0 {
			return r.Hide(), nil
		}
		e := r.entries[r.cursor]
		issueKey := r.issueKey
		hidden := r.Hide()
		return hidden, func() tea.Msg {
			return WorklogDeletedMsg{IssueKey: issueKey, WorklogID: e.ID}
		}
	}
	if key.Matches(k, r.closeBinding) {
		return r.Hide(), nil
	}
	return r, nil
}

// View renders the picker.
func (r RemoveWorklog) View(s styles.Styles) string {
	if !r.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Remove worklog · " + r.issueKey)
	var rows []string
	if len(r.entries) == 0 {
		rows = append(rows, s.Muted.Render("(no worklogs to remove)"))
	} else {
		for i, e := range r.entries {
			line := truncate(e.TimeSpent, 10) + "  " + truncate(e.When, 12) +
				"  " + s.Muted.Render(truncate(e.Author, 40))
			if i == r.cursor {
				line = "▶ " + line
			} else {
				line = "  " + line
			}
			rows = append(rows, line)
		}
	}
	hint := s.Muted.Render("enter delete    " +
		r.closeBinding.Help().Key + " " + r.closeBinding.Help().Desc)
	parts := []string{title, ""}
	parts = append(parts, rows...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
