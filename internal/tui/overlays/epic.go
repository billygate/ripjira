package overlays

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// EpicPickedMsg is published when the user picks an epic (or the
// "No epic" detach row). ParentKey is empty for detach.
type EpicPickedMsg struct {
	IssueKey  string
	ParentKey string
}

// EpicCancelledMsg is published when the user dismisses the picker.
type EpicCancelledMsg struct{}

// Epic is the epic-link picker overlay: a single-column list with a
// type-to-filter input. When the issue currently has a parent epic, a
// leading "⊘ No epic (detach)" row is rendered.
type Epic struct {
	visible       bool
	issueKey      string
	currentParent string
	epics         []jira.Issue
	filter        string
	cursor        int
}

// NewEpic builds a hidden Epic overlay.
func NewEpic() Epic { return Epic{} }

// Visible reports whether the overlay is currently shown.
func (e Epic) Visible() bool { return e.visible }

// IssueKey returns the issue the overlay was opened for.
func (e Epic) IssueKey() string { return e.issueKey }

// LoadedEpics returns the candidate epics the overlay knows about.
func (e Epic) LoadedEpics() []jira.Issue { return e.epics }

// Show opens the overlay for the given issue. currentParent is the
// issue's existing parent epic key (empty when none).
func (e Epic) Show(issueKey, currentParent string) Epic {
	e.visible = true
	e.issueKey = issueKey
	e.currentParent = currentParent
	e.filter = ""
	e.cursor = 0
	return e
}

// Hide returns a copy of e with the overlay closed and per-open state
// cleared. Loaded epics survive across Hide/Show.
func (e Epic) Hide() Epic {
	e.visible = false
	e.issueKey = ""
	e.currentParent = ""
	e.filter = ""
	e.cursor = 0
	return e
}

// SetEpics replaces the candidate list shown in the picker.
func (e Epic) SetEpics(epics []jira.Issue) Epic {
	e.epics = append([]jira.Issue(nil), epics...)
	if e.cursor >= e.rowCount() {
		e.cursor = 0
	}
	return e
}

// hasDetach reports whether the leading detach row should be rendered.
func (e Epic) hasDetach() bool { return e.currentParent != "" }

// filtered returns the epics matching the current filter substring.
func (e Epic) filtered() []jira.Issue {
	if e.filter == "" {
		return e.epics
	}
	needle := strings.ToLower(e.filter)
	out := make([]jira.Issue, 0, len(e.epics))
	for _, ep := range e.epics {
		hay := strings.ToLower(ep.Key + " " + ep.Summary)
		if strings.Contains(hay, needle) {
			out = append(out, ep)
		}
	}
	return out
}

// rowCount returns the number of selectable rows (detach + filtered).
func (e Epic) rowCount() int {
	n := len(e.filtered())
	if e.hasDetach() {
		n++
	}
	return n
}

// Update consumes input while visible.
func (e Epic) Update(msg tea.Msg) (Epic, tea.Cmd) {
	if !e.visible {
		return e, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	switch k.Type {
	case tea.KeyEsc:
		hidden := e.Hide()
		return hidden, func() tea.Msg { return EpicCancelledMsg{} }
	case tea.KeyEnter:
		issueKey := e.issueKey
		parent, ok := e.selectedParent()
		if !ok {
			return e, nil
		}
		hidden := e.Hide()
		return hidden, func() tea.Msg {
			return EpicPickedMsg{IssueKey: issueKey, ParentKey: parent}
		}
	case tea.KeyUp:
		if e.cursor > 0 {
			e.cursor--
		}
		return e, nil
	case tea.KeyDown:
		if e.cursor < e.rowCount()-1 {
			e.cursor++
		}
		return e, nil
	case tea.KeyBackspace:
		if r := []rune(e.filter); len(r) > 0 {
			e.filter = string(r[:len(r)-1])
			e.cursor = 0
		}
		return e, nil
	case tea.KeyRunes:
		e.filter += string(k.Runes)
		e.cursor = 0
		return e, nil
	}
	return e, nil
}

// selectedParent returns the parent key for the current cursor position.
// ok=false when there are no rows.
func (e Epic) selectedParent() (string, bool) {
	idx := e.cursor
	if e.hasDetach() {
		if idx == 0 {
			return "", true
		}
		idx--
	}
	rows := e.filtered()
	if idx < 0 || idx >= len(rows) {
		if !e.hasDetach() && len(rows) == 0 {
			return "", false
		}
		return "", false
	}
	return rows[idx].Key, true
}

// View renders the picker. Returns "" when hidden.
func (e Epic) View(s styles.Styles) string {
	if !e.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Epic · " + e.issueKey)
	filterLine := s.Muted.Render("filter: ") + e.filter

	rows := make([]string, 0, e.rowCount())
	row := func(i int, label string) string {
		if i == e.cursor {
			return s.ListItemSelected.Render(label)
		}
		return s.ListItem.Render(label)
	}
	idx := 0
	if e.hasDetach() {
		rows = append(rows, row(idx, "⊘ No epic (detach)"))
		idx++
	}
	for _, ep := range e.filtered() {
		label := ep.Key + "  " + ep.Summary
		if ep.Key == e.currentParent {
			label += s.Muted.Render("  (current)")
		}
		rows = append(rows, row(idx, label))
		idx++
	}
	body := s.Muted.Render("(no matches)")
	if len(rows) > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}
	hint := s.Muted.Render("enter pick    esc cancel")
	parts := []string{title, "", filterLine, "", body, "", hint}
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
