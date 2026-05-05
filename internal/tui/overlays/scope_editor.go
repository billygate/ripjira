package overlays

import (
	"regexp"
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
		e.rowEdit = &rowEditState{editingIndex: -1, step: stepField, op: structureadapter.OpIn}
		return e, nil
	case "enter":
		return e.openRowEditForCursor(), nil
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

// rowEditStep enumerates the focus position in the row sub-editor.
type rowEditStep int

const (
	stepField  rowEditStep = iota
	stepOp
	stepValues
)

type rowEditState struct {
	editingIndex int
	step         rowEditStep
	field        string
	op           structureadapter.ScopeOp
	chips        []string
	textInput    string
	existsYes    bool
	errMsg       string
}

func (e ScopeEditor) InRowEdit() bool { return e.rowEdit != nil }

func (e ScopeEditor) RowEditHasError() bool {
	return e.rowEdit != nil && e.rowEdit.errMsg != ""
}

func (e ScopeEditor) openRowEditForCursor() ScopeEditor {
	if e.cursor < len(e.rows) {
		r := e.rows[e.cursor]
		st := &rowEditState{
			editingIndex: e.cursor,
			step:         stepField,
			field:        r.Field,
			op:           r.Op,
		}
		hydrateValues(st, r)
		e.rowEdit = st
		return e
	}
	e.rowEdit = &rowEditState{editingIndex: -1, step: stepField, op: structureadapter.OpIn}
	return e
}

func hydrateValues(st *rowEditState, r structureadapter.ScopeRow) {
	switch r.Op {
	case structureadapter.OpIn, structureadapter.OpNot:
		st.chips = append([]string(nil), r.Values...)
	case structureadapter.OpRegex, structureadapter.OpContains:
		if len(r.Values) > 0 {
			st.textInput = r.Values[0]
		}
	case structureadapter.OpExists:
		st.existsYes = len(r.Values) > 0 && r.Values[0] == "yes"
	}
}

func (e ScopeEditor) updateRowEdit(msg tea.Msg) (ScopeEditor, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	st := e.rowEdit
	switch k.String() {
	case "esc":
		e.rowEdit = nil
		return e, nil
	case "tab":
		e.advanceStep()
		return e, nil
	case "shift+tab":
		if st.step > stepField {
			st.step--
		}
		return e, nil
	case "enter":
		return e.handleRowEditEnter()
	case "backspace":
		return e.handleRowEditBackspace(), nil
	}
	if k.Type == tea.KeyRunes {
		return e.handleRowEditRunes(k.Runes), nil
	}
	return e, nil
}

func (e ScopeEditor) advanceStep() bool {
	st := e.rowEdit
	switch st.step {
	case stepField:
		if st.field == "" {
			st.errMsg = "field required"
			return false
		}
		if e.fieldDuplicate(st.field, st.editingIndex) {
			st.errMsg = "another row already filters " + st.field
			return false
		}
		st.errMsg = ""
		st.step = stepOp
	case stepOp:
		st.errMsg = ""
		st.step = stepValues
	case stepValues:
	}
	return true
}

func (e ScopeEditor) fieldDuplicate(field string, ignoreIndex int) bool {
	for i, r := range e.rows {
		if i == ignoreIndex {
			continue
		}
		if r.Field == field {
			return true
		}
	}
	return false
}

func (e ScopeEditor) handleRowEditEnter() (ScopeEditor, tea.Cmd) {
	st := e.rowEdit
	if st.step != stepValues {
		e.advanceStep()
		return e, nil
	}
	switch st.op {
	case structureadapter.OpIn, structureadapter.OpNot:
		if st.textInput != "" {
			st.chips = append(st.chips, st.textInput)
			st.textInput = ""
			return e, nil
		}
		if len(st.chips) == 0 {
			st.errMsg = "at least one value required"
			return e, nil
		}
	case structureadapter.OpRegex:
		if st.textInput == "" {
			st.errMsg = "regex required"
			return e, nil
		}
		if _, err := regexp.Compile(st.textInput); err != nil {
			st.errMsg = "invalid regex: " + err.Error()
			return e, nil
		}
	case structureadapter.OpContains:
		if st.textInput == "" {
			st.errMsg = "value required"
			return e, nil
		}
	}
	row := materializeRow(st)
	if st.editingIndex >= 0 && st.editingIndex < len(e.rows) {
		e.rows[st.editingIndex] = row
	} else {
		e.rows = append(e.rows, row)
	}
	e.rowEdit = nil
	return e, nil
}

func materializeRow(st *rowEditState) structureadapter.ScopeRow {
	switch st.op {
	case structureadapter.OpIn, structureadapter.OpNot:
		return structureadapter.ScopeRow{Field: st.field, Op: st.op, Values: append([]string(nil), st.chips...)}
	case structureadapter.OpRegex, structureadapter.OpContains:
		return structureadapter.ScopeRow{Field: st.field, Op: st.op, Values: []string{st.textInput}}
	case structureadapter.OpExists:
		v := "no"
		if st.existsYes {
			v = "yes"
		}
		return structureadapter.ScopeRow{Field: st.field, Op: st.op, Values: []string{v}}
	}
	return structureadapter.ScopeRow{Field: st.field, Op: st.op}
}

func (e ScopeEditor) handleRowEditBackspace() ScopeEditor {
	st := e.rowEdit
	switch st.step {
	case stepField:
		if n := len(st.field); n > 0 {
			st.field = st.field[:n-1]
		}
	case stepValues:
		switch st.op {
		case structureadapter.OpIn, structureadapter.OpNot:
			if st.textInput != "" {
				st.textInput = st.textInput[:len(st.textInput)-1]
			} else if n := len(st.chips); n > 0 {
				st.chips = st.chips[:n-1]
			}
		case structureadapter.OpRegex, structureadapter.OpContains:
			if n := len(st.textInput); n > 0 {
				st.textInput = st.textInput[:n-1]
			}
		}
	}
	return e
}

func (e ScopeEditor) handleRowEditRunes(rs []rune) ScopeEditor {
	st := e.rowEdit
	switch st.step {
	case stepField:
		st.field += string(rs)
		st.errMsg = ""
	case stepOp:
		switch string(rs) {
		case "h":
			st.op = prevOp(st.op)
		case "l":
			st.op = nextOp(st.op)
		}
	case stepValues:
		switch st.op {
		case structureadapter.OpIn, structureadapter.OpNot, structureadapter.OpRegex, structureadapter.OpContains:
			st.textInput += string(rs)
		case structureadapter.OpExists:
			switch string(rs) {
			case "y":
				st.existsYes = true
			case "n":
				st.existsYes = false
			case " ":
				st.existsYes = !st.existsYes
			}
		}
	}
	return e
}

var opCycle = []structureadapter.ScopeOp{
	structureadapter.OpIn, structureadapter.OpNot,
	structureadapter.OpContains, structureadapter.OpRegex,
	structureadapter.OpExists,
}

func nextOp(o structureadapter.ScopeOp) structureadapter.ScopeOp {
	for i, x := range opCycle {
		if x == o {
			return opCycle[(i+1)%len(opCycle)]
		}
	}
	return opCycle[0]
}

func prevOp(o structureadapter.ScopeOp) structureadapter.ScopeOp {
	for i, x := range opCycle {
		if x == o {
			return opCycle[(i-1+len(opCycle))%len(opCycle)]
		}
	}
	return opCycle[0]
}

func (e ScopeEditor) viewRowEdit(s styles.Styles) string {
	st := e.rowEdit
	title := s.OverlayTitle.Render("Edit row")
	var lines []string
	mark := func(step rowEditStep, label string) string {
		if st.step == step {
			return s.OverlayTitle.Render("▶ " + label)
		}
		return s.Muted.Render("  " + label)
	}
	lines = append(lines, mark(stepField, "field: "+st.field))
	lines = append(lines, mark(stepOp, "operator: "+string(st.op)))
	switch st.op {
	case structureadapter.OpIn, structureadapter.OpNot:
		chipsLine := strings.Join(st.chips, ", ")
		if st.textInput != "" {
			if chipsLine != "" {
				chipsLine += ", "
			}
			chipsLine += st.textInput + "▏"
		} else if st.step == stepValues {
			chipsLine += "▏"
		}
		lines = append(lines, mark(stepValues, "values: "+chipsLine))
		if st.step == stepValues {
			lines = append(lines, s.Muted.Render("  enter add chip · backspace remove · enter accept"))
		}
	case structureadapter.OpRegex, structureadapter.OpContains:
		v := st.textInput
		if st.step == stepValues {
			v += "▏"
		}
		lines = append(lines, mark(stepValues, "value: "+v))
	case structureadapter.OpExists:
		v := "no"
		if st.existsYes {
			v = "yes"
		}
		lines = append(lines, mark(stepValues, "exists: "+v+"  (y/n/space)"))
	}
	if st.errMsg != "" {
		lines = append(lines, "", s.Muted.Render("error: "+st.errMsg))
	}
	hint := s.Muted.Render("tab/shift+tab navigate · enter accept · esc cancel")
	parts := []string{title, ""}
	parts = append(parts, lines...)
	parts = append(parts, "", hint)
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
