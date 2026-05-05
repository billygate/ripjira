package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/structureadapter"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// ScopeSavedMsg is emitted when the user accepts the editor with `s`.
type ScopeSavedMsg struct {
	StructureID string
	Rows        []structureadapter.ScopeRow
}

// ScopeValuesProvider supplies autocomplete suggestions for a field.
type ScopeValuesProvider func(field string) []string

// ScopeEditor is the visual editor for a structure's Scope filter.
type ScopeEditor struct {
	visible      bool
	structureID  string
	title        string
	rows         []structureadapter.ScopeRow
	cursor       int
	closeBinding key.Binding
	values       ScopeValuesProvider

	rowEdit *rowEditState
}

func NewScopeEditor(closeKey key.Binding) ScopeEditor {
	return ScopeEditor{closeBinding: closeKey}
}

func (e ScopeEditor) Visible() bool { return e.visible }

func (e ScopeEditor) Rows() []structureadapter.ScopeRow {
	return append([]structureadapter.ScopeRow(nil), e.rows...)
}

func (e ScopeEditor) Show(title string, rows []structureadapter.ScopeRow, values ScopeValuesProvider) ScopeEditor {
	e.title = title
	e.rows = append([]structureadapter.ScopeRow(nil), rows...)
	e.cursor = 0
	e.visible = true
	e.values = values
	e.rowEdit = nil
	return e
}

func (e ScopeEditor) ShowWithID(id, title string, rows []structureadapter.ScopeRow, values ScopeValuesProvider) ScopeEditor {
	e = e.Show(title, rows, values)
	e.structureID = id
	return e
}

func (e ScopeEditor) Hide() ScopeEditor {
	return ScopeEditor{closeBinding: e.closeBinding}
}

func (e ScopeEditor) Update(msg tea.Msg) (ScopeEditor, tea.Cmd) {
	if !e.visible {
		return e, nil
	}
	if e.rowEdit != nil {
		return e.updateRowEdit(msg)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	switch k.String() {
	case "j", "down":
		if e.cursor < len(e.rows) {
			e.cursor++
		}
		return e, nil
	case "k", "up":
		if e.cursor > 0 {
			e.cursor--
		}
		return e, nil
	case "d":
		if e.cursor < len(e.rows) {
			e.rows = append(e.rows[:e.cursor], e.rows[e.cursor+1:]...)
			if e.cursor > 0 && e.cursor >= len(e.rows) {
				e.cursor = len(e.rows)
			}
		}
		return e, nil
	case "s":
		id := e.structureID
		rows := append([]structureadapter.ScopeRow(nil), e.rows...)
		hidden := e.Hide()
		return hidden, func() tea.Msg { return ScopeSavedMsg{StructureID: id, Rows: rows} }
	case "a":
		return e, nil
	case "enter":
		return e, nil
	}
	if key.Matches(k, e.closeBinding) {
		return e.Hide(), nil
	}
	return e, nil
}

func (e ScopeEditor) View(s styles.Styles) string {
	if !e.visible {
		return ""
	}
	if e.rowEdit != nil {
		return e.viewRowEdit(s)
	}
	title := s.OverlayTitle.Render("Scope: " + e.title)
	var lines []string
	for i, r := range e.rows {
		line := scopeRowLine(r)
		if i == e.cursor {
			line = "▶ " + line
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	addLine := "+ add row"
	if e.cursor == len(e.rows) {
		addLine = "▶ " + addLine
	} else {
		addLine = "  " + addLine
	}
	lines = append(lines, s.Muted.Render(addLine))
	hint := s.Muted.Render("enter edit · a add · d delete · s save · " +
		e.closeBinding.Help().Key + " cancel")
	parts := []string{title, ""}
	parts = append(parts, lines...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func scopeRowLine(r structureadapter.ScopeRow) string {
	values := strings.Join(r.Values, ", ")
	return r.Field + "  " + string(r.Op) + "  " + values
}

// rowEditState and methods below are filled in Task 7 — stubs for Task 6.
type rowEditState struct{}

func (e ScopeEditor) updateRowEdit(_ tea.Msg) (ScopeEditor, tea.Cmd) { return e, nil }
func (e ScopeEditor) viewRowEdit(_ styles.Styles) string             { return "" }
