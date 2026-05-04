package overlays

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// LinkEntry is the read-only view of an existing issue link shown in the
// remove-link picker. Mirrors the structure the detail pane renders, kept
// here so the overlay package does not import internal/jira.
type LinkEntry struct {
	ID       string
	Relation string
	OtherKey string
	Summary  string
}

// LinkDeletedMsg is published when the user picks an entry to delete.
type LinkDeletedMsg struct {
	IssueKey string
	LinkID   string
	OtherKey string
}

// RemoveLink is the `-` overlay: a vertical picker of existing links on
// the current issue with j/k navigation and Enter to delete.
type RemoveLink struct {
	visible      bool
	issueKey     string
	entries      []LinkEntry
	cursor       int
	closeBinding key.Binding
}

// NewRemoveLink builds a hidden overlay.
func NewRemoveLink(closeKey key.Binding) RemoveLink {
	return RemoveLink{closeBinding: closeKey}
}

// Visible reports whether the overlay is shown.
func (r RemoveLink) Visible() bool { return r.visible }

// IssueKey returns the issue the overlay was opened for.
func (r RemoveLink) IssueKey() string { return r.issueKey }

// Cursor returns the highlighted index.
func (r RemoveLink) Cursor() int { return r.cursor }

// Show opens the picker scoped to issueKey and seeded with entries.
// Empty entries means "no links to remove" — the overlay still opens but
// shows a muted placeholder so the user gets feedback that `-` was seen.
func (r RemoveLink) Show(issueKey string, entries []LinkEntry) RemoveLink {
	r.issueKey = issueKey
	r.entries = append([]LinkEntry(nil), entries...)
	r.cursor = 0
	r.visible = true
	return r
}

// Hide returns a copy of r with state cleared.
func (r RemoveLink) Hide() RemoveLink {
	r.visible = false
	r.issueKey = ""
	r.entries = nil
	r.cursor = 0
	return r
}

// Update consumes input while visible.
func (r RemoveLink) Update(msg tea.Msg) (RemoveLink, tea.Cmd) {
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
			return LinkDeletedMsg{IssueKey: issueKey, LinkID: e.ID, OtherKey: e.OtherKey}
		}
	}
	if key.Matches(k, r.closeBinding) {
		return r.Hide(), nil
	}
	return r, nil
}

// View renders the picker.
func (r RemoveLink) View(s styles.Styles) string {
	if !r.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Remove link · " + r.issueKey)
	var rows []string
	if len(r.entries) == 0 {
		rows = append(rows, s.Muted.Render("(no links to remove)"))
	} else {
		for i, e := range r.entries {
			line := truncate(e.Relation, 16) + "  " + e.OtherKey + "  " +
				s.Muted.Render(truncate(e.Summary, 50))
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
