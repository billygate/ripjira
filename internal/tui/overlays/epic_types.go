package overlays

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// EpicTypesAppliedMsg is emitted on Enter (outside input mode) carrying the
// final list of issue types.
type EpicTypesAppliedMsg struct {
	Items []string
}

// EpicTypesCancelledMsg is emitted on Esc (outside input mode). The
// Settings draft is left unchanged.
type EpicTypesCancelledMsg struct{}

type epicEditMode int

const (
	epicEditNone epicEditMode = iota
	epicEditAdd
	epicEditReplace
)

// EpicTypes is the sub-overlay opened from the Settings overlay for
// editing the epic_issue_types list. It is a list editor with `a` add,
// `d` delete, `e` edit, Enter apply, Esc cancel. While editing, a single
// textinput captures the new/replacement value.
type EpicTypes struct {
	visible      bool
	closeBinding key.Binding

	items  []string
	cursor int
	mode   epicEditMode
	input  textinput.Model
}

// NewEpicTypes builds a hidden overlay. closeKey is what the parent
// considers "Esc" — used only for type discipline; the overlay also
// special-cases tea.KeyEsc directly.
func NewEpicTypes(closeKey key.Binding) EpicTypes {
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 60
	in.Width = 40
	return EpicTypes{closeBinding: closeKey, input: in}
}

func (o EpicTypes) Visible() bool   { return o.visible }
func (o EpicTypes) Items() []string { return append([]string(nil), o.items...) }
func (o EpicTypes) Cursor() int     { return o.cursor }
func (o EpicTypes) Editing() bool   { return o.mode != epicEditNone }

// SetInputForTest is a test helper that forces the textinput value.
// Production code should not call this.
func (o *EpicTypes) SetInputForTest(v string) { o.input.SetValue(v) }

func (o EpicTypes) Show(items []string) EpicTypes {
	o.visible = true
	o.items = append([]string(nil), items...)
	o.cursor = 0
	o.mode = epicEditNone
	o.input.Reset()
	o.input.Blur()
	return o
}

func (o EpicTypes) Hide() EpicTypes {
	o.visible = false
	o.mode = epicEditNone
	o.input.Reset()
	o.input.Blur()
	return o
}

// Update processes a key event. While in input mode, Enter commits and
// Esc cancels the input (without closing the overlay). Outside input
// mode, Enter applies and Esc cancels the entire overlay.
func (o EpicTypes) Update(msg tea.Msg) (EpicTypes, tea.Cmd) {
	if !o.visible {
		return o, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}
	if o.mode != epicEditNone {
		return o.updateEditing(k)
	}
	return o.updateBrowsing(k)
}

func (o EpicTypes) updateBrowsing(k tea.KeyMsg) (EpicTypes, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		cancelled := EpicTypesCancelledMsg{}
		return o.Hide(), func() tea.Msg { return cancelled }
	case tea.KeyEnter:
		items := append([]string(nil), o.items...)
		applied := EpicTypesAppliedMsg{Items: items}
		return o.Hide(), func() tea.Msg { return applied }
	case tea.KeyDown:
		if o.cursor < len(o.items)-1 {
			o.cursor++
		}
		return o, nil
	case tea.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return o, nil
	}
	switch string(k.Runes) {
	case "j":
		if o.cursor < len(o.items)-1 {
			o.cursor++
		}
	case "k":
		if o.cursor > 0 {
			o.cursor--
		}
	case "a":
		o.mode = epicEditAdd
		o.input.Reset()
		o.input.Focus()
	case "d":
		if len(o.items) > 0 {
			o.items = append(o.items[:o.cursor], o.items[o.cursor+1:]...)
			if o.cursor >= len(o.items) && o.cursor > 0 {
				o.cursor--
			}
		}
	case "e":
		if len(o.items) > 0 {
			o.mode = epicEditReplace
			o.input.SetValue(o.items[o.cursor])
			o.input.CursorEnd()
			o.input.Focus()
		}
	}
	return o, nil
}

func (o EpicTypes) updateEditing(k tea.KeyMsg) (EpicTypes, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		o.mode = epicEditNone
		o.input.Reset()
		o.input.Blur()
		return o, nil
	case tea.KeyEnter:
		val := strings.TrimSpace(o.input.Value())
		if val == "" {
			return o, nil // reject empty, keep input mode open
		}
		switch o.mode {
		case epicEditAdd:
			o.items = append(o.items, val)
			o.cursor = len(o.items) - 1
		case epicEditReplace:
			o.items[o.cursor] = val
		}
		o.mode = epicEditNone
		o.input.Reset()
		o.input.Blur()
		return o, nil
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(k)
	return o, cmd
}

// View renders the overlay. Returns "" when hidden.
func (o EpicTypes) View(s styles.Styles) string {
	if !o.visible {
		return ""
	}
	rows := []string{s.OverlayTitle.Render("Epic issue types"), ""}
	for i, it := range o.items {
		line := "  " + it
		if i == o.cursor {
			line = "▸ " + it
			line = s.ListItemSelected.Render(line)
		} else {
			line = s.ListItem.Render(line)
		}
		rows = append(rows, line)
	}
	if o.mode != epicEditNone {
		rows = append(rows, "", o.input.View())
	}
	rows = append(rows, "", s.Muted.Render("a add · d delete · e edit · enter apply · esc cancel"))
	return s.OverlayBorder.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
