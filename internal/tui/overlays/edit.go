package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// EditField identifies which Jira field the user is editing. The root model
// uses this to translate the submitted string into the right wire shape and
// to apply the right optimistic update.
type EditField int

const (
	// EditSummary edits the issue's summary (title) — single-line text.
	EditSummary EditField = iota
	// EditPriority edits the issue's priority — Jira accepts the priority
	// name verbatim, e.g. "High", "Medium", "Low". An invalid name surfaces
	// as a 400 from the backend; the overlay does not pre-validate so users
	// running customised workflows can use whatever names their instance
	// has configured.
	EditPriority
	// EditLabels edits the issue's labels — comma-separated input that the
	// root model splits and trims into a []string.
	EditLabels
	// EditDueDate edits the issue's due date — accepts YYYY-MM-DD or empty
	// to clear. Jira validates server-side.
	EditDueDate
)

// FieldName returns the human-readable label shown in the overlay title.
func (f EditField) FieldName() string {
	switch f {
	case EditSummary:
		return "summary"
	case EditPriority:
		return "priority"
	case EditLabels:
		return "labels"
	case EditDueDate:
		return "due date"
	}
	return "field"
}

// EditSubmittedMsg is published when the user presses Enter on a non-empty
// (or, for clearable fields, possibly empty) value. The root model is
// responsible for translating Value into the right Jira wire shape.
type EditSubmittedMsg struct {
	IssueKey string
	Field    EditField
	Value    string
}

// Edit is the `T`/`P`/`L`/`D` overlay: a one-line text input bound to a
// single field of an issue. It is intentionally generic — keeping the
// translation logic outside the overlay means the overlay does not import
// the jira package.
type Edit struct {
	visible      bool
	issueKey     string
	field        EditField
	input        textinput.Model
	closeBinding key.Binding
}

// NewEdit constructs a hidden Edit overlay. closeKey is the key that hides
// the overlay (typically `esc`).
func NewEdit(closeKey key.Binding) Edit {
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 0
	in.Width = 60
	return Edit{
		input:        in,
		closeBinding: closeKey,
	}
}

// Visible reports whether the overlay is currently shown.
func (e Edit) Visible() bool { return e.visible }

// IssueKey returns the issue key the overlay was opened for.
func (e Edit) IssueKey() string { return e.issueKey }

// Field returns which field is being edited.
func (e Edit) Field() EditField { return e.field }

// Value returns the input's current value (mostly for tests).
func (e Edit) Value() string { return e.input.Value() }

// Show opens the overlay scoped to (issueKey, field) with current as the
// pre-filled value. The returned cmd starts the cursor blink.
func (e Edit) Show(issueKey string, field EditField, current string) (Edit, tea.Cmd) {
	e.issueKey = issueKey
	e.field = field
	e.visible = true
	e.input.SetValue(current)
	e.input.CursorEnd()
	cmd := e.input.Focus()
	return e, cmd
}

// Hide returns a copy of e with the overlay closed and state cleared.
func (e Edit) Hide() Edit {
	e.visible = false
	e.issueKey = ""
	e.input.SetValue("")
	e.input.Blur()
	return e
}

// Update consumes input while the overlay is visible. Enter submits, the
// close key cancels. The Tab key is swallowed so the user does not pop out
// of the overlay accidentally.
func (e Edit) Update(msg tea.Msg) (Edit, tea.Cmd) {
	if !e.visible {
		return e, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyEnter:
			value := strings.TrimSpace(e.input.Value())
			issueKey := e.issueKey
			field := e.field
			hidden := e.Hide()
			return hidden, func() tea.Msg {
				return EditSubmittedMsg{IssueKey: issueKey, Field: field, Value: value}
			}
		case tea.KeyTab, tea.KeyShiftTab:
			return e, nil
		}
		if key.Matches(k, e.closeBinding) {
			return e.Hide(), nil
		}
	}
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	return e, cmd
}

// View renders the overlay. Returns "" when hidden.
func (e Edit) View(s styles.Styles) string {
	if !e.visible {
		return ""
	}
	title := s.OverlayTitle.Render("Edit " + e.field.FieldName() + " · " + e.issueKey)
	hint := s.Muted.Render(
		"enter submit    " + e.closeBinding.Help().Key + " " + e.closeBinding.Help().Desc,
	)
	parts := []string{title, "", e.input.View(), "", hint}
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}
